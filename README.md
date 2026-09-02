# OFD Converter [![GoDoc](https://godoc.org/github.com/zc310/fastjsonrpc?status.svg)](http://godoc.org/github.com/zc310/ofd)

一个用于将 OFD 文件转换为 PDF 和图像格式的 Go 语言工具包。

## 功能特性

- ✅ **OFD 转 PDF** - 支持将 OFD 文档转换为标准的 PDF 文件
- ✅ **OFD 转 图像** - 支持将 OFD 页面转换为 PNG、JPG 等图像格式
- ✅ **多页面支持** - 支持多页面 OFD 文档的转换
- ✅ **灵活配置** - 支持自定义 DPI、背景颜色、页面选择等参数
- ✅ **高效处理** - 基于 Go 语言开发，性能优异

## 安装

```bash
go get github.com/zc310/ofd
```

## 命令行程序打包

根目录 `Makefile` 可以将命令行程序分别编译并打包为独立 ZIP 文件。`ofd-thumbnailer` 仅编译 Linux 版本：

```bash
# 生成 dist/ofd-viewer-linux-amd64.zip
# 生成 dist/ofd-converter-linux-amd64.zip
# 生成 dist/ofd-thumbnailer-linux-amd64.zip
make package
```

Linux 下会生成三个 ZIP；Windows 和 macOS 下只生成 `ofd-viewer` 与 `ofd-converter` 两个 ZIP，不会编译 `ofd-thumbnailer`。

每个 ZIP 包都包含对应的二进制文件和 README。`ofd-thumbnailer` 的安装包还包含 `ofd.thumbnailer`；Linux 安装包额外包含 `install.sh`，解压后可执行：

```bash
cd ofd-thumbnailer
sudo ./install.sh
```

可以通过 `GOOS`、`GOARCH` 和 `CGO_ENABLED` 指定目标平台，例如：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 make package
```

Windows 交叉编译示例：

```bash
# Debian/Ubuntu
sudo apt install gcc-mingw-w64-x86-64

GOOS=windows CGO_ENABLED=1 GOARCH=amd64 make package
```

Windows 目标必须使用 MinGW-w64，不能使用 Linux 的 `gcc`。如果交叉编译器不在默认路径，可以显式指定：

```bash
CC=x86_64-w64-mingw32-gcc GOOS=windows CGO_ENABLED=1 GOARCH=amd64 make package
```

Fyne 查看器需要目标平台的图形开发库。Windows 版查看器使用 `-H=windowsgui` 编译，不显示命令行窗口。

## Flatpak

仓库根目录包含 `io.github.zc310.ofd.json` 及其桌面集成文件，可用于构建 OFD Viewer 的 Flatpak 包。构建依赖已通过 `go.mod.json` 和 `modules.txt` 固定，避免 Flatpak 构建阶段联网下载 Go 模块。

```bash
flatpak run org.flatpak.Builder --user --install --force-clean --disable-rofiles-fuse build-dir io.github.zc310.ofd.json
flatpak run io.github.zc310.ofd
```

正式提交 Flathub 前，需要将 manifest 中的源码 commit 更新为包含当前 Flatpak 文件的 `v0.0.5` 发布 commit，并在本机安装 `flatpak-builder`、`appstreamcli` 后完成元数据检查。

Flatpak 版本以只读方式访问真实的 `~/.local/share/fonts` 用户字体目录和宿主机系统目录（`host-os`），用于渲染未内嵌字体的 OFD 文档。

## 快速开始

### OFD 转 PDF

```go
package main

import (
    "os"
    "github.com/zc310/ofd/pkg/converter"
)

func main() {
    output, _ := os.Create("output.pdf")
    defer output.Close()
    
    err := converter.PDF("input.ofd", output)
    if err != nil {
        panic(err)
    }
}
```


### OFD 转图像

#### 转换为 PNG

```go
err := converter.Image("input.ofd",
    converter.Writer(func(page int) (io.WriteCloser, error) {
        return os.Create(fmt.Sprintf("output_%d.png", page))
    }),
    converter.BgColor(color.White),
    converter.PNG(),
)
```

#### 转换为 JPG

```go
err := converter.Image("input.ofd",
    converter.Writer(func(page int) (io.WriteCloser, error) {
        return os.Create(fmt.Sprintf("output_%d.jpg", page))
    }),
    converter.BgColor(color.White),
    converter.JPG(),
    converter.Page(3),        // 指定转换特定页面
    converter.DPI(300),       // 设置输出分辨率
)
```



## 注意事项

- 背景颜色默认为白色，可根据需要调整
- 支持效果见 `input.ofd` 转换结果
- 不支持 OFD 文件内字体
- 不支持 `GBT 33190-2016` 很多标准😅。。。


## 特别感谢

本项目的开发离不开以下开源项目的启发和帮助：

- [国家标准化管理委员会发布的 GB/T 33190-2016 标准](http://std.samr.gov.cn/)
- https://github.com/GreenYun/OFD-Schema
- https://github.com/itlabers/ofd-go-reference
- https://github.com/itlabers/ofd-go
- https://github.com/xiaoqidun/ofdgo

感谢所有为开源社区做出贡献的开发者！
