# OFD Converter [![GoDoc](https://pkg.go.dev/badge/github.com/zc310/ofd.svg)](https://pkg.go.dev/github.com/zc310/ofd)

一个用于将 OFD 文件转换为 PDF、纯文本和图像格式的 Go 语言工具包。

## 功能特性

| 类别         | 功能                                                                                                     |
|--------------|----------------------------------------------------------------------------------------------------------|
| **文档转换** | OFD 转 PDF、纯文本和 PNG/JPG 等图像格式                                                                  |
| **页面处理** | 支持多文档体、多页面转换，按文档体顺序合并并使用全局页码                                                 |
| **灵活配置** | 支持自定义 DPI、背景颜色和页面选择                                                                       |
| **OFD 校验** | 基于 `OFD-Schema` 校验 ZIP、XML、引用和 XSD，并生成文本、Markdown、JSON、PDF 报告                        |
| **OFD 分析** | 深入分析文档结构、页面对象、文字、资源、附件、注解、签名和引用关系，支持文本、Markdown、JSON 和 PDF 报告 |
| **处理性能** | 基于 Go 语言开发，支持高效处理                                                                           |

## 安装

```bash
go get github.com/zc310/ofd
```

## 命令行程序打包

根目录 `Makefile` 可以将命令行程序分别编译并打包为独立 ZIP 文件。Linux 下默认 `make` 会同时构建 Linux amd64/ARM64、macOS ARM64/x86_64、Windows x86_64/ARM64 和 Android APK ZIP；在其他系统上执行默认 `make` 时，会构建该系统支持的程序。`ofd-thumbnailer` 仅编译 Linux 版本：

```bash
# Linux 默认构建会生成 Linux amd64/ARM64、macOS、Windows x86_64/ARM64 程序和 Android APK ZIP
make package
```

Linux 默认构建 Windows x86_64 需要安装 MinGW-w64。也可以单独执行：

```bash
make package-windows-amd64
```

如果交叉编译器不在默认路径，可以通过 `WINDOWS_CC` 指定：

```bash
WINDOWS_CC=/opt/mingw/bin/x86_64-w64-mingw32-gcc make package-windows-amd64
```

编译并打包 Linux ARM64 版本。由于 ARM64 Linux 环境缺少 `ofd-viewer` 所需的部分图形库，该目标跳过 `ofd-viewer`，并使用纯 Go 模式构建其他程序：

```bash
make package-arm64
```

在 Linux 环境中交叉编译 macOS ARM64 和 x86_64 版本。由于 `ofd-viewer` 依赖 macOS 图形开发环境，这两个目标会跳过 viewer：

```bash
make package-darwin-arm64
make package-darwin-amd64
```

在 Linux 环境中交叉编译 Windows ARM64 版本。由于 ARM64 Windows 不构建依赖 CGO 的 `ofd-viewer`，该目标只生成三个命令行程序：

```bash
make package-windows-arm64
```

输出文件为 `ofd-converter-windows-arm64.zip`、`ofd-validator-windows-arm64.zip` 和 `ofd-analyzer-windows-arm64.zip`。这三个程序不使用 CGO，因此不需要安装 MinGW-w64。

Linux amd64 默认构建包含 `ofd-viewer`；Linux ARM64 和 Linux 主机交叉编译 macOS 时跳过 `ofd-viewer`。`ofd-thumbnailer` 始终只在 Linux 目标中构建，并包含 Linux ARM64 版本。

默认 `make` 会构建 Android APK 和 APK ZIP。如只需要构建 Android APK，可执行 `make package-viewer-android`；如需要 APK ZIP，可执行 `make package-viewer-android-zip`。

单独构建 OFD 校验工具：

```bash
make package-validator
```

Linux amd64 默认会生成 Linux amd64/ARM64、macOS、Windows x86_64/ARM64 程序包以及 Android APK 和 APK ZIP；
Linux ARM64 会生成四个 Linux 程序包；Windows 和 macOS 下的默认构建会生成
`ofd-viewer`、`ofd-converter`、`ofd-validator` 和 `ofd-analyzer` 四个 ZIP，不会编译
`ofd-thumbnailer`。

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

Windows amd64 的 `ofd-viewer` 和其他启用 CGO 的目标必须使用 MinGW-w64，不能使用 Linux 的 `gcc`。如果交叉编译器不在默认路径，可以显式指定：

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

### OFD 文件校验

`ofd-validator` 是面向 OFD 文件包的完整性和规范性校验工具，用于检查容器安全、XML 结构、Schema
规范、文件引用、对象 ID 和签名摘要，帮助快速定位 OFD 文件中的结构问题和兼容性问题。

校验工具位于 `cmd/ofd-validator`，并使用通过 `go:embed` 内置的 `OFD-Schema` XSD。运行时不依赖
`xmllint`、外部模式文件或网络。校验结果会统一生成报告，支持终端查看、归档或交给 CI 和其他程序处理：

```bash
go run ./cmd/ofd-validator --format text test/testdata/helloworld.ofd
```

报告支持文本、Markdown、JSON 和 PDF 四种格式：

```bash
go run ./cmd/ofd-validator --format json --pretty test/testdata/helloworld.ofd > report.json
go run ./cmd/ofd-validator --format markdown -o report.md test/testdata/helloworld.ofd
go run ./cmd/ofd-validator --format pdf --font /path/to/SimSun.ttf \
  -o report.pdf test/testdata/helloworld.ofd
```

报告包含输入文件、检测时间、校验状态、错误和警告汇总、各校验阶段状态，以及每条问题的严重级别、
校验阶段、错误代码、文件位置和 XML 路径等详细信息。JSON 报告适合 CI、脚本和其他程序读取；Markdown
适合代码评审和文档归档；PDF 适合打印或发送给其他人员。

默认使用 `strict` 模式；`compat` 模式会将 XSD 错误降级为警告，`structural` 模式跳过 XSD。
使用 `--skip-xsd` 等价于 `structural` 模式。报告正文和 CLI 提示使用中文，JSON 同时保留机器可读
的英文枚举和 `*_zh` 中文字段。退出码 `0` 表示没有错误，`1` 表示存在校验错误（使用
`--fail-on-warning` 时警告也会导致退出码 1），`2` 表示工具配置或输入错误。

### OFD 文件分析

`ofd-analyzer` 是面向 OFD 文件的结构分析工具，用于快速了解文档组成、资源使用情况和对象引用关系，适合文档排查、资源审计、转换问题定位以及生成结构化分析报告。

分析工具位于 `cmd/ofd-analyzer`，默认将纯文本报告输出到标准输出，也支持 Markdown、JSON 和 PDF。报告可以直接阅读，也可以使用 JSON 供脚本或其他工具继续处理：

```bash
# 默认输出纯文本报告
go run ./cmd/ofd-analyzer test/testdata/helloworld.ofd

# 输出适合程序处理的 JSON 报告
go run ./cmd/ofd-analyzer --format json test/testdata/helloworld.ofd

# 输出缩进后的 JSON 报告文件
go run ./cmd/ofd-analyzer --format json --pretty \
  -o analyzer-report.json test/testdata/multi_demo.ofd

# 输出 Markdown 报告
go run ./cmd/ofd-analyzer --format text -o analyzer-report.txt test/testdata/helloworld.ofd
go run ./cmd/ofd-analyzer --format markdown -o analyzer-report.md test/testdata/helloworld.ofd

# 输出 PDF 报告；PDF 报告需要可用字体
go run ./cmd/ofd-analyzer --format pdf --font /path/to/font.ttf -o analyzer-report.pdf test/testdata/helloworld.ofd
```

分析报告主要包含：

- 文档基本信息、Schema 版本、ZIP 包统计和输入文件信息
- 文档体、页面列表以及页面尺寸等页面结构信息
- 页面对象、文字对象和字符数量统计
- 图片、字体、绘制参数、复合图元、颜色空间、模板和 Pattern 等资源统计
- 附件、注解、签名以及文件引用和 ID 引用关系
- 绘制参数的定义数、引用次数、无法解析引用数和 `Relative` 继承循环
- 字符的 Unicode code point、UTF-8 字节、空白字符和 glyph 数量

分析范围和行为：

- 模板、复合图元和 Pattern 默认只统计定义及引用，不展开内部对象，避免重复计数
- 使用 `--no-templates` 可跳过模板定义、模板引用和模板 `PageRes` 资源分析
- 使用 `--no-annotations` 可跳过注解及 Appearance 分析
- 使用 `--no-signatures` 可跳过签名清单分析
- 使用 `--no-package` 可跳过 ZIP 条目大小统计，但保留包内路径索引
- 使用 `--fail-on-warning` 可在发现分析警告时返回退出码 `1`

资源引用使用 `doc[n]/kind:id` 形式表示文档体作用域，可以区分不同文档体中重复使用的 ID。完整的命令选项、报告字段和退出码说明见 [`cmd/ofd-analyzer/README.md`](cmd/ofd-analyzer/README.md)。

OFD 文件可以包含多个文档体。转换器和查看器按 `DocBody` 出现顺序合并页面，`Page(n)` 和命令行 `-page n` 使用合并后的全局页码。

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

### OFD 转纯文本

纯文本转换只提取文字内容，不保留字体、颜色和页面布局。不同文字对象按行输出，不同页面使用分页符分隔。

```go
import "bytes"

var output bytes.Buffer
err := converter.Text("input.ofd", &output)
```

#### 转换为 JPG

```go
err := converter.Image("input.ofd",
    converter.Writer(func(page int) (io.WriteCloser, error) {
        return os.Create(fmt.Sprintf("output_%d.jpg", page))
    }),
    converter.BgColor(color.White),
    converter.JPG(),
    converter.Page(3),        // 指定全局第 3 页
    converter.DPI(300),       // 设置输出分辨率
)
```



## 注意事项

- 背景颜色默认为白色，可根据需要调整
- 支持效果见 `input.ofd` 转换结果
- 不支持 `GBT 33190-2016` 很多标准😅。。。


## 特别感谢

本项目的开发离不开以下开源项目的启发和帮助：

- [国家标准化管理委员会发布的 GB/T 33190-2016 标准](http://std.samr.gov.cn/)
- https://github.com/GreenYun/OFD-Schema
- https://github.com/itlabers/ofd-go-reference
- https://github.com/itlabers/ofd-go
- https://github.com/xiaoqidun/ofdgo

感谢所有为开源社区做出贡献的开发者！
