# OFD Go Toolkit 图标

<img src="ofd-icon.svg" width="128" height="128" alt="OFD Go Toolkit 图标">

[ofd-icon.svg](ofd-icon.svg) 使用青蓝色小地鼠抱着 OFD 文档的构图，体现 Go 语言与
文档工具的主题。字母以矢量路径绘制，不依赖外部字体；需要其他尺寸时从 SVG 导出。

仓库维护 SVG 源图与本说明；多尺寸 PNG、设计预览和分享图按需在本地生成，已加入忽略规则。

## 配色

| 用途 | 十六进制 | RGB |
|---|---|---|
| 背景亮部 | `#173F60` | `23, 63, 96` |
| 背景深蓝 | `#081B33` | `8, 27, 51` |
| 角色高光青色 | `#4CDAEF` | `76, 218, 239` |
| 角色主色 | `#00A5CE` | `0, 165, 206` |
| 角色轮廓 | `#0B5976` | `11, 89, 118` |
| OFD 字样 | `#153A56` | `21, 58, 86` |
| 纸张亮部、眼白与牙齿 | `#FFFFFF` | `255, 255, 255` |
| 纸张浅蓝 | `#EFF8FC` | `239, 248, 252` |
| 文档折角 | `#A8E5F1` | `168, 229, 241` |

背景沿左上至右下从 `#173F60` 过渡到 `#081B33`；角色自上而下从 `#4CDAEF`
过渡到 `#00A5CE`；纸张自上而下从 `#FFFFFF` 过渡到 `#EFF8FC`。
背景另叠加 `#16C9EA` 的径向高光，中心不透明度为 15%，向外降至 0%。
完整的细节颜色与透明度以 SVG 源文件为准。

## 导出 PNG

以下命令从仓库根目录执行，输出放在 `/tmp/ofd-branding`。

使用 librsvg 的 `rsvg-convert`。Ubuntu/Debian 可通过
`sudo apt install librsvg2-bin` 安装，导出 512 × 512 的透明 PNG：

```sh
mkdir -p /tmp/ofd-branding
rsvg-convert -w 512 -h 512 \
  -o /tmp/ofd-branding/ofd-icon-512.png \
  docs/branding/ofd-icon.svg
```

批量导出常用尺寸：

```sh
mkdir -p /tmp/ofd-branding
for size in 32 64 128 192 256 512 1024; do
  rsvg-convert -w "$size" -h "$size" \
    -o "/tmp/ofd-branding/ofd-icon-${size}.png" \
    docs/branding/ofd-icon.svg
done
```

也可使用已安装的 Chrome 导出母版，再用 ImageMagick 缩放：

```sh
mkdir -p /tmp/ofd-branding
google-chrome --headless --disable-gpu --hide-scrollbars \
  --default-background-color=00000000 \
  --window-size=1024,1024 \
  --screenshot=/tmp/ofd-branding/ofd-icon-1024.png \
  "file://$(pwd)/docs/branding/ofd-icon.svg"

magick /tmp/ofd-branding/ofd-icon-1024.png \
  -resize 512x512 /tmp/ofd-branding/ofd-icon-512.png
```

`--default-background-color=00000000` 保留图标外角的透明背景。容器中如无法启用
Chrome 沙箱，可在上述本地 SVG 导出命令中增加 `--no-sandbox`。

## 创作来源

本稿的小地鼠和文档为重新绘制的矢量图形。Go Gopher 吉祥物概念源自
[Renée French 的 Go Gopher](https://go.dev/blog/gopher)，原作采用
[Creative Commons Attribution 3.0](https://creativecommons.org/licenses/by/3.0/) 许可。
本目录的图标设计沿用该许可，使用或再分发时请保留原作署名、许可链接和改绘说明。
