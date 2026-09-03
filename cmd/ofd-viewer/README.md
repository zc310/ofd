# OFD Viewer

OFD 桌面查看器，当前版本为 `v0.0.5`，支持连续阅读和按需渲染。

## 功能

- 鼠标滚轮连续翻页，页码自动同步
- 页码输入跳转，支持 `Home`、`End` 和方向键翻页
- 缩略图默认隐藏，可按需显示并点击跳转
- 支持“适应页面”“适应宽度”“适应高度”和“双页显示”四种视图
- 双页视图按两列连续排列页面
- 双页视图下缩略图也按两列显示
- 多文档体按出现顺序连续阅读，页码使用全局页码
- 页面和缩略图均按需加载
- 导出支持 PDF、TXT、JPG、PNG、SVG、EPS、TeX
- 多页图片/矢量格式导出为 ZIP，单页直接保存对应文件
- 导出支持 DPI 和透明/白色背景设置
- 信息按钮查看应用信息和 GitHub 项目地址

## 使用

启动后点击“打开 OFD”，或通过命令行打开文件：

```bash
go run . path/to/document.ofd
```

快捷键：

- `O`：打开 OFD
- `A/W/左箭头/上箭头/PageUp`：上一页
- `D/S/右箭头/下箭头/PageDown`：下一页
- `Home` / `End`：跳到第一页或最后一页
- `Q` / `Esc`：退出

## 编译

```bash
go build .
```

Linux 构建需要 OpenGL 和 X11 开发库。

## 多平台编译

以下命令在当前目录执行，编译结果保存到 `dist` 目录：

```bash
mkdir -p dist

# Linux x86_64
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
  go build -o dist/ofd-viewer-linux-amd64 .

# Linux ARM64
GOOS=linux GOARCH=arm64 CGO_ENABLED=1 \
  go build -o dist/ofd-viewer-linux-arm64 .

# Windows x86_64，需要安装 MinGW-w64
CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
  go build -ldflags "-H=windowsgui" -o dist/ofd-viewer-windows-amd64.exe .

# macOS Apple Silicon，需要在 macOS 环境中执行
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
  go build -o dist/ofd-viewer-darwin-arm64 .

# macOS Intel，需要在 macOS 环境中执行
GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 \
  go build -o dist/ofd-viewer-darwin-amd64 .
```

Fyne 桌面程序依赖 CGO 和目标平台的图形开发库。Linux 目标需要 OpenGL/X11 开发库，Windows 目标需要 MinGW-w64；macOS 目标通常应在 macOS 环境中编译。

Windows 版本使用 `-H=windowsgui` 编译，不显示命令行窗口。

## 截图

![linux.png](../../docs/screenshots/viewer/linux.png)
![windows.png](../../docs/screenshots/viewer/windows.png)
