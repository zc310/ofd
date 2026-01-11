#!/bin/bash
# OFD 缩略图生成器安装脚本

set -e  # 遇到任何错误立即退出
set -u  # 使用未设置的变量时报错

# 检查是否为root用户
if [[ $EUID -ne 0 ]]; then
    echo "错误: 此脚本需要root权限运行" >&2
    exit 1
fi

# 定义路径
THUMBNAILER_BIN="/usr/bin/ofd-thumbnailer"
THUMBNAILER_CONF="/usr/share/thumbnailers/ofd.thumbnailer"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 检查源文件
if [[ ! -f "${SCRIPT_DIR}/ofd-thumbnailer" ]]; then
    echo "错误: 找不到 ofd-thumbnailer" >&2
    exit 1
fi

if [[ ! -f "${SCRIPT_DIR}/ofd.thumbnailer" ]]; then
    echo "错误: 找不到 ofd.thumbnailer" >&2
    exit 1
fi

echo "安装 OFD 缩略图生成器..."

# 移除旧文件
echo "移除旧文件..."
rm -f "$THUMBNAILER_BIN" 2>/dev/null && echo "已移除: $THUMBNAILER_BIN"
rm -f "$THUMBNAILER_CONF" 2>/dev/null && echo "已移除: $THUMBNAILER_CONF"

# 安装新文件
echo "安装新文件..."
install -m 755 "${SCRIPT_DIR}/ofd-thumbnailer" "$THUMBNAILER_BIN" && echo "安装: $THUMBNAILER_BIN"
install -m 644 "${SCRIPT_DIR}/ofd.thumbnailer" "$THUMBNAILER_CONF" && echo "安装: $THUMBNAILER_CONF"

echo "安装完成！"