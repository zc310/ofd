#!/bin/bash
# OFD 缩略图生成器安装脚本

set -e  # 遇到任何错误立即退出
set -u  # 使用未设置的变量时报错

# 定义路径
THUMBNAILER_BIN="/usr/bin/zc310-ofd-thumbnailer"
THUMBNAILER_CONF="/usr/share/thumbnailers/zc310-ofd.thumbnailer"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ ! -f "${SCRIPT_DIR}/zc310-ofd.thumbnailer" ]]; then
    echo "错误: 找不到 zc310-ofd.thumbnailer" >&2
    exit 1
fi

THUMBNAILER_SOURCE="${SCRIPT_DIR}/ofd-thumbnailer"
if [[ -f "${SCRIPT_DIR}/main.go" && -f "${SCRIPT_DIR}/../../go.mod" ]]; then
    if ! command -v go >/dev/null 2>&1; then
        echo "错误: 从源码安装需要 Go，请先安装 go.mod 要求的 Go 版本" >&2
        exit 1
    fi
    BUILD_TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/ofd-thumbnailer.XXXXXX")"
    trap 'rm -rf -- "$BUILD_TMP_DIR"' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    THUMBNAILER_SOURCE="${BUILD_TMP_DIR}/ofd-thumbnailer"
    echo "编译 OFD 缩略图生成器..."
    CGO_ENABLED=0 go -C "${SCRIPT_DIR}/../.." build -trimpath -o "$THUMBNAILER_SOURCE" ./cmd/ofd-thumbnailer
elif [[ ! -f "$THUMBNAILER_SOURCE" ]]; then
    echo "错误: 找不到源码或发行包中的 ofd-thumbnailer" >&2
    exit 1
fi

# 编译使用当前用户，仅安装系统文件时提权。
INSTALL_COMMAND=(install)
if [[ $EUID -ne 0 ]]; then
    if ! command -v sudo >/dev/null 2>&1; then
        echo "错误: 安装需要 root 权限，请安装 sudo 或以 root 运行此脚本" >&2
        exit 1
    fi
    INSTALL_COMMAND=(sudo install)
fi

echo "安装 OFD 缩略图生成器..."

# 安装新文件
echo "安装新文件..."
"${INSTALL_COMMAND[@]}" -d /usr/share/thumbnailers
"${INSTALL_COMMAND[@]}" -m 755 "$THUMBNAILER_SOURCE" "$THUMBNAILER_BIN"
echo "安装: $THUMBNAILER_BIN"
"${INSTALL_COMMAND[@]}" -m 644 "${SCRIPT_DIR}/zc310-ofd.thumbnailer" "$THUMBNAILER_CONF"
echo "安装: $THUMBNAILER_CONF"

echo "安装完成！"
