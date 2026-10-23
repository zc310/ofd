# Linux OFD / ZIP 文件缩略图

本程序仅支持 Linux。

使用本工具处理文档前，请阅读项目根目录的 [免责声明](../../DISCLAIMER.md)。本工具不保证缩略图结果适用于特定业务、法律或合规场景。

## 用法

```bash
ofd-thumbnailer <输入文件> <输出 PNG 路径> <长边像素>
```

输入按扩展名判断：`.ofd`（不区分大小写）渲染文档的第一页；`.zip` 按包内条目顺序
取第一张可以解码的图片生成缩略图，损坏的图片条目会跳过。其它扩展名也尝试按 ZIP
包读取，不直接读取独立的 PNG/JPEG 等图片文件。

加密 OFD 输出固定的「OFD 文档 + 金色挂锁」缩略图，成功返回 `0`。整包加密和
部分加密均使用该图案，按请求的长边像素等比缩放，无需口令、私钥或系统字体。
图案内嵌在程序中，安装时无需额外复制图片文件。

加密标识依据解析器识别的根目录 `Encryptions.xml`；该入口存在但元数据无法解析时
也显示挂锁。图标仅表示检测到加密入口，未加密的损坏 OFD 仍按失败处理。

长边像素必须在 `1` 到 `4096` 之间，保持宽高比。OFD 在栅格化前按目标尺寸计算
分辨率，页面栅格最多 `4096 × 4096` 像素；无法在预算内渲染的页面尺寸或比例会报错。
输出始终采用 PNG 编码，文件名可以没有扩展名。输入输出不能指向同一文件，包括
符号链接和硬链接；编码成功后才原子替换输出文件。

## 安装

### DEB / RPM 安装包

从 [Releases](https://github.com/zc310/ofd/releases) 下载与系统架构对应的安装包，
支持 amd64（RPM 名称为 x86_64）和 arm64（RPM 名称为 aarch64）。安装命令：

```bash
# Debian / Ubuntu，文件名按实际下载版本替换
sudo apt install ./zc310-ofd-thumbnailer_版本-1_amd64.deb

# Fedora / Rocky Linux 等
sudo dnf install ./zc310-ofd-thumbnailer-版本-1.x86_64.rpm
```

安装包包含静态程序、桌面缩略图配置、说明及许可证，目标机器无需 Go。
升级时安装新版包；卸载使用 `sudo apt remove zc310-ofd-thumbnailer` 或
`sudo dnf remove zc310-ofd-thumbnailer`。

### 源码或 ZIP 安装

在源码仓库根目录执行：

```bash
./cmd/ofd-thumbnailer/install.sh
```

脚本使用当前用户的 Go 工具链，以 `CGO_ENABLED=0` 编译到 `mktemp` 创建的临时目录，
编译成功后安装程序和桌面缩略图配置。普通用户在安装阶段通过 `sudo` 提权，也可
直接以 root 运行。需要安装 `go.mod` 要求的 Go 版本；无需事先手工编译。
临时目录遵循 `TMPDIR`，未设置时使用 `/tmp`，脚本成功或失败退出时都会清理。

从发行包安装时，在解压目录执行 `./install.sh`；脚本直接安装包内编译好的程序。

安装后命令为 `zc310-ofd-thumbnailer`，桌面缩略图配置安装到
`/usr/share/thumbnailers/zc310-ofd.thumbnailer`。

## 制作 DEB / RPM

在仓库根目录执行：

```bash
# 安装 Makefile 固定版本的 nFPM，将 Go bin 目录加入 PATH
make install-nfpm
export PATH="$(go env GOPATH)/bin:$PATH"

# 单独制作或同时制作两种格式
make package-thumbnailer-deb
make package-thumbnailer-rpm
make package-thumbnailer-linux

# 交叉制作 ARM64 安装包
make package-thumbnailer-linux GOOS=linux GOARCH=arm64
```

配置位于本目录的 [`nfpm.yaml`](nfpm.yaml)。版本取 Makefile
的 `VERSION`，可传 `VERSION=...` 覆盖；产物默认在 `/tmp/ofd/dist`，可传 `DIST_DIR=...`。
`NFPM=/path/to/nfpm` 可指定打包工具。系统包始终以 `CGO_ENABLED=0` 单独构建，
不依赖 ZIP 包的构建选项。现有 `package-thumbnailer` 继续生成 ZIP。

默认 `make`（`all`）还生成当前 Linux amd64/arm64 架构的 DEB 和 RPM；
Linux 主机启用 ARM64 交叉打包时，也生成 ARM64 的两种格式，同一架构只打包一次。
执行前须安装 nFPM 或设置 `NFPM`。`make package`、`make package-desktop`、
`make package-tools` 和 `make package-arm64` 保持原有范围，不附加 DEB/RPM。

发布工作流在 Linux amd64、arm64 任务中生成并检查 DEB/RPM，随后上传到 GitHub Release。
本地运行完整内容检查需 nFPM、`dpkg-deb` 和 `rpm`：

```bash
go test ./cmd/ofd-thumbnailer -run '^TestThumbnailerNativePackages$' -count=1 -v
```

## 退出码

| 退出码 | 含义                                                                                              |
|--------|---------------------------------------------------------------------------------------------------|
| `0`    | 缩略图生成成功                                                                                    |
| `1`    | 失败：参数个数不对、尺寸超出 1–4096、输入输出为同一文件、页面无法安全渲染、输入读取或图片解码失败、输出写不了、超时（60 秒） |

只有 `0` 与 `1` 两档：freedesktop 缩略图规范只要求「成功返回 0、失败返回非 0」，
桌面环境据此决定是采用新缩略图还是继续用旧的，因此参数错误与渲染失败不再细分。
诊断信息只有一行 `slog` 错误日志（标准错误），不写文件；退出码本身不携带原因，
排查要看这行日志。

渲染 OFD 需要可用的中文字体等渲染依赖；缩略图命令超时固定 60 秒，避免大文档卡住
文件管理器。

## 截图

![gnome.Nautilus.png](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-thumbnailer-gnome.Nautilus.png)
