# 语法：https://docs.docker.com/reference/dockerfile/
#
# ofd-server 运行镜像：Ubuntu 基础 + LibreOffice + Google Chrome，
# 用于 Office 文档与 HTML 的 heavy 通道转换。
#
# 构建（在仓库根目录）：
#   docker build -f cmd/ofd-server/Dockerfile -t ofd-server:0.1.2 .
#
# 运行（令牌必填，见下）：
#   docker run -d --name ofd-server -p 9705:9705 \
#     -e OFD_SERVER_API_KEY="$(openssl rand -hex 32)" \
#     -v ofd-data:/var/lib/ofd-server \
#     -v ofd-logs:/var/log/ofd-server \
#     -v ofd-tmp:/var/tmp/ofd-server \
#     ofd-server:0.1.2
#
# 内置的 docker-config.json 把 api_key 留空，所以不给令牌时容器会直接
# 拒绝启动。这是刻意的：放一个占位符进去等于给镜像发布了一把公开的钥匙。
#
# arm64 主机：把 --platform 加到 docker run 上。
#   docker run --platform linux/arm64 ...

# ---------------------------------------------------------------- 构建阶段
# CGO_ENABLED=0：cmd/ofd-server 不需要 cgo 依赖（已验证），静态二进制可以
# 直接放进 Ubuntu 运行阶段，不必带 Go 工具链或 glibc 兼容问题。
FROM golang:1.26-bookworm AS builder

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOFLAGS=-trimpath

WORKDIR /src

# 先只拷贝依赖清单，依赖层可跨源码改动复用缓存。
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# -trimpath 与 -s -w 与 Makefile 的 build-ofd-server 保持一致。
#
# 不注入版本号：cmd/ofd-server 还没有 --version 也没有 applicationVersion
# 变量，-X 指向不存在的符号不会报错，只是静默丢弃。ofd-server 的版本
# 识别目前依赖镜像 tag，不在二进制里。等命令支持 --version 后再在这里
# 加 -X main.applicationVersion。
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w" -o /out/ofd-server ./cmd/ofd-server

# ---------------------------------------------------------------- 运行阶段
# Ubuntu 24.04（noble）：LibreOffice 版本较新，字体包完整。
FROM ubuntu:24.04

# 避免交互式 apt 停在 timezone 询问上。
ENV DEBIAN_FRONTEND=noninteractive

# curl 是 Chrome deb 下载与健康检查都要用的，体积只有几 MB。
#
# 字体是渲染质量的硬依赖，不是可选项：
#   fonts-noto-cjk     逻辑字体回退首选（Noto Sans/Serif/Mono CJK SC）
#   fonts-noto-core    拉丁正文字体
#   fonts-dejavu-core  代码块等宽回退（DejaVu Sans Mono）
#   fonts-liberation   Arial / Times New Roman / Courier New 的 metric 兼容替代
#                      OFD 文档大量引用这些字体名，缺失会回退到字形不一的字体
#   fonts-wqy-microhei 文泉驿微米黑，document_font.go 的默认系统字体候选里
#                      显式列了 "WenQuanYi Micro Hei"，装上它该候选才命中
#   fonts-wqy-zenhei   文泉驿正黑，作为额外的中文兜底
#   fontconfig         Go 侧 LoadSystemFont 与 Chrome 都通过 fontconfig 查字体
# 缺 fonts-noto-cjk 的表现是：中文字符渲染为空白，但任务仍然成功——排版正常、
# 字宽计算正常，只有字形画不出来，属于很难从日志发现的静默错误。
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        fontconfig \
        fonts-dejavu-core \
        fonts-liberation \
        fonts-noto-cjk \
        fonts-noto-core \
        fonts-wqy-microhei \
        fonts-wqy-zenhei \
        libreoffice-calc \
        libreoffice-core \
        libreoffice-impress \
        libreoffice-writer \
        tzdata \
        tesseract-ocr tesseract-ocr-chi-sim  tesseract-ocr-chi-sim-vert tesseract-ocr-chi-tra tesseract-ocr-chi-tra-vert \
        && fc-cache -f \
    && rm -rf /var/lib/apt/lists/*

# Chrome：只有 amd64 官方 deb。arm64 改用 chromium，
# 因此这里按架构选择来源。
ARG TARGETARCH
RUN set -eux; \
    if [ "$TARGETARCH" = "amd64" ]; then \
        curl -fsSL -o /tmp/chrome.deb \
            https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb; \
    else \
        apt-get update; \
        apt-get install -y --no-install-recommends chromium; \
        rm -rf /var/lib/apt/lists/*; \
    fi; \
    if [ -f /tmp/chrome.deb ]; then \
        apt-get update; \
        # apt install ./x.deb 会自动拉齐 Chrome 依赖的共享库，
        # 比手工列出 libnss3 / libatk 那一长串可靠。
        apt-get install -y --no-install-recommends /tmp/chrome.deb; \
        rm -f /tmp/chrome.deb; \
        rm -rf /var/lib/apt/lists/*; \
    fi

# LibreOffice 首次运行要建用户配置目录，非 root 下 HOME 必须可写；
# Chrome 也需要 HOME 存放崩溃上报与字体缓存。
RUN groupadd --gid 10001 ofd \
    && useradd --uid 10001 --gid ofd --create-home --home-dir /home/ofd --shell /usr/sbin/nologin ofd

COPY --from=builder /out/ofd-server /usr/local/bin/ofd-server
# ofd-server 用 --check-config 启动自检确认二进制可执行。
RUN /usr/local/bin/ofd-server --help > /dev/null

# 与 docker-config.json 的默认路径一致，声明为 volume 便于持久化与交接。
# 三个目录都要 ofd 用户可写：数据库、转换产物与日志分别落在这里，
# 缺权限会让服务在启动建目录时直接失败。
RUN mkdir -p /var/lib/ofd-server/out /var/log/ofd-server /var/tmp/ofd-server \
    && chown -R ofd:ofd /var/lib/ofd-server /var/log/ofd-server /var/tmp/ofd-server

ENV HOME=/home/ofd \
    TMPDIR=/var/tmp/ofd-server \
    LANG=C.UTF-8

USER ofd
WORKDIR /var/lib/ofd-server

EXPOSE 9705

# 探 /readyz 而不是 /healthz：/healthz 只证明进程活着，/readyz 还要过
# 服务自己的 ready 回调，数据库打不开或队列未恢复时返回 503。
#
# start-period 给足 30s：启动时要扫队列恢复 in-flight 任务，任务多时
# 一两秒内 ready 不会翻转，过早判定失败会导致编排系统反复重启容器。
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD curl -fsS http://127.0.0.1:9705/readyz || exit 1

# 镜像里预置一份能直接跑起来的配置（无任何凭据），用户挂载自己的配置
# 覆盖即可。用 docker-config.json 而不是 example-config.json：后者带着
# REPLACE_WITH_... 的假凭据、示例域名和 /var、/tmp 绝对路径，装进镜像
# 容易被当成"改个密码就能上线"。
RUN mkdir -p /etc/ofd-server \
    && cp /src/cmd/ofd-server/docker-config.json /etc/ofd-server/config.json \
    && chown -R ofd:ofd /etc/ofd-server

ENTRYPOINT ["/usr/local/bin/ofd-server"]
CMD ["-c", "/etc/ofd-server/config.json"]
