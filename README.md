# OFD Go Toolkit [![GoDoc](https://pkg.go.dev/badge/github.com/zc310/ofd.svg)](https://pkg.go.dev/github.com/zc310/ofd)

一个用于解析、创建、转换、渲染、校验和分析 OFD 文档的 Go 工具包。

> 使用本项目及其输出结果前，请阅读 [免责声明](DISCLAIMER.md)。本项目不保证所有 OFD 文件的解析、转换、校验或渲染结果适用于特定业务、法律或合规场景。如发现仓库、文档或示例中可能存在侵权内容，请通过 [GitHub Issues](https://github.com/zc310/ofd/issues/new) 联系维护者。

## OFD 简介

OFD（Open Fixed-layout Document）是中国电子文件领域常用的版式文档格式，主要用于保存版面固定、跨设备显示一致的电子文件。OFD 文件通常以 ZIP 容器组织 XML 文档、页面内容、文字、路径、图片、字体、附件和签名等资源；页面可以包含文字、矢量图形和栅格图像，并保留相应的布局和绘制信息。项目不随仓库分发标准全文；GB/T 33190-2016《电子文件存储与交换格式 版式文档》等标准的官方查询链接见[标准资料索引](docs/README.md#标准资料)，供格式学习和开发参考。

OFD 适用于电子证照、电子发票、数字档案、公文和其他需要长期保存或交换的版式文件。本项目围绕 OFD 文档提供解析、创建、渲染、转换、校验、分析和阅读能力，支持将 OFD 内容输出为 PDF、单文件 HTML、PNG、JPG、多页 TIFF、SVG、纯文本和 Markdown 等格式。不同厂商生成的 OFD 文件可能存在扩展或兼容性差异，具体支持范围请参见 [`docs/OFD-SUPPORT.md`](docs/OFD-SUPPORT.md)。

## 功能特性

| 类别           | 功能                                                                                                      |
|----------------|-----------------------------------------------------------------------------------------------------------|
| **文档转换**   | OFD 转 PDF、单文件 HTML、纯文本、Markdown 和 PNG/JPG/TIFF 等图像格式；PDF/Markdown/Office/HTML 文档转 OFD |
| **页面处理**   | 支持多文档体、多页面转换，按文档体顺序合并并使用全局页码                                                  |
| **灵活配置**   | 支持自定义 DPI、背景颜色和页面选择                                                                        |
| **文档合并**   | ZIP 级多文档打包、模型级拼页和选页重排，支持签名处理和资源引用重映射                                     |
| **OFD 校验**   | 基于 `OFD-Schema` 校验 ZIP、XML、引用和 XSD，并生成报告                                                   |
| **OFD 分析**   | 深入分析文档结构、页面对象、文字、资源、附件、注解、签名和引用关系，支持报告                              |
| **电子签章**   | 支持 GB/T 38540-2020 SES V4 的 SM2/SM3 印章、签章生成与验证，提供 Go API 和命令行工具                     |
| **档案预检**   | 提供 OFD 档案预检、技术清单、归档准备目录和 SHA-256 固定性验证                                            |
| **发票抽取**   | 增值税电子发票和铁路电子客票附件抽取，JSON/CSV/XLSX 汇总、去重、自校验和 PDF 合并打印                    |
| **桌面阅读**   | 提供基于 Fyne 的 Linux、Windows OFD 桌面阅读器                                                            |
| **安卓阅读**   | 支持 Android 文件选择、文档阅读和 APK 打包                                                                |
| **浏览器阅读** | 提供基于 Web Worker 和 WASM 的 OFD 阅读器                                                                 |
| **性能基准**   | 提供跨语言 OFD 转 PDF 速度、文件大小、文本提取和兼容性对比测试                                            |

## 文档入口

| 内容 | 文档 |
|------|------|
| OFD 标准元素与项目能力 | [OFD 支持清单](docs/OFD-SUPPORT.md) |
| PDF 转 OFD 的能力、限制与资源预算 | [PDF 支持清单](docs/PDF-SUPPORT.md) |
| Office 转 OFD/PDF（依赖 LibreOffice） | [Office 支持清单](docs/OFFICE-SUPPORT.md) |
| HTML/MHTML 转 OFD/PDF（依赖 Chrome/Chromium） | [HTML 支持清单](docs/HTML-SUPPORT.md) |
| 使用 Go API 创建 OFD | [创建器库指南](pkg/creator/README.md) |
| 使用 Go API 生成和验证电子签章 | [签署与验证指南](pkg/sign/README.md) |
| 标准资料、截图与开发维护说明 | [文档目录](docs/README.md) |

支持清单区分已支持、部分支持和暂不支持，不代表完整的标准符合性认证。

| 工具 | 用途与完整说明 |
|------|----------------|
| [ofd-converter](cmd/ofd-converter/README.md) | 文档与图像转换、批量处理。 |
| [ofd-creator](cmd/ofd-creator/README.md) | 按 manifest 创建 OFD，导出、合并、替换和水印编辑。 |
| [ofd-validator](cmd/ofd-validator/README.md) | 格式、引用、语义和摘要校验。 |
| [ofd-analyzer](cmd/ofd-analyzer/README.md) | 文档结构分析与报告。 |
| [ofd-archive](cmd/ofd-archive/README.md) | 档案预检、归档准备与固定性验证。 |
| [ofd-invoice](cmd/ofd-invoice/README.md) | 发票与铁路电子客票抽取、汇总和合并打印。 |
| [ofd-seal](cmd/ofd-seal/README.md) | SES 电子印章封装及演示图片生成。 |
| [ofd-signer-demo](cmd/ofd-signer-demo/README.md) | 外部签名协议的演示签名器。 |
| [ofd-server](cmd/ofd-server/README.md) | 同步流输出与异步任务转换服务。 |
| [ofd-viewer](cmd/ofd-viewer/README.md) | 桌面与 Android 阅读器。 |
| [ofd-wasm](cmd/ofd-wasm/README.md) | 浏览器阅读器、WASM API 与 Worker 协议。 |
| [ofd-thumbnailer](cmd/ofd-thumbnailer/README.md) | Linux 文件管理器缩略图。 |

性能基准测试见 [`zc310/ofd-benchmark`](https://github.com/zc310/ofd-benchmark)。该项目对比多个语言和实现的 OFD 转 PDF 速度、输出文件大小、文本提取能力及兼容性；测试结果仅供参考，实际表现会受到文档样本、字体、操作系统和运行环境影响。

## 桌面与 Android 阅读器

项目提供基于 Fyne 的 `ofd-viewer` 图形化阅读器，支持 Linux 和 Windows 桌面环境，也支持构建 Android APK 阅读器。阅读器支持打开 OFD 文件、连续阅读、缩放、页面导航、双页显示、文字搜索以及导出 PDF、图片和文本等功能。

桌面版和 Android 版的详细功能、构建要求、Android APK 打包说明及截图见 [`cmd/ofd-viewer/README.md`](cmd/ofd-viewer/README.md)。已经发布的 Linux、Windows 桌面版和 Android APK 可在 [GitHub Releases](https://github.com/zc310/ofd/releases) 下载。

常用构建命令如下：

```bash
# Linux
make package-viewer

# Windows x86_64，需要 MinGW-w64
make package-windows-amd64

# Android APK，需要 Fyne、Android SDK 和 NDK
make package-viewer-android
```

Linux 和 Windows 桌面版需要目标平台的图形开发环境；Windows 交叉编译需要 MinGW-w64。Android 版以 APK 形式安装，构建前需要配置 Android SDK、NDK 及 Fyne 命令行工具。

Windows 7 用户可尝试使用 [XTLS/go-win7](https://github.com/XTLS/go-win7) 提供的兼容 Go 工具链自行编译，版本需满足 `go.mod` 的要求。以下为 Linux 下交叉编译 Windows x86_64 命令行工具的示例，将 `GO` 替换为该 SDK 中 `go` 的实际路径：

```bash
GOTOOLCHAIN=local make GO=/path/to/go-win7/bin/go GOOS=windows GOARCH=amd64 CGO_ENABLED=0 build-tools
```

`GOTOOLCHAIN=local` 用于固定兼容工具链。若遇到 `bcrypt.dll not found`，请检查系统是否安装了 KB2533623 或包含相应 DLL 加载功能的后续更新；桌面阅读器编译需 MinGW-w64，运行需 Universal CRT 和 OpenGL 驱动。具体功能的兼容性需在目标 Win7 环境中验证。

## WASM 浏览器阅读器

项目提供无需前端框架的 OFD 浏览器阅读器示例，位于 [`cmd/ofd-wasm`](cmd/ofd-wasm)，支持在浏览器中解析、阅读、搜索、打印和导出文档。

在线预览：<https://zc310.github.io/ofd/wasm/>

### 构建和运行

在仓库根目录执行：

```bash
make build-wasm
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

然后打开 <http://localhost:8080>，选择 `.ofd` 文件。浏览器不能通过 `file://` 直接加载 WASM，因此必须使用 HTTP 服务访问。

### 阅读器功能

- 连续页面阅读、缩略图导航和当前页定位
- 按需渲染正文和缩略图，支持页面缩放
- 适应宽度、适应页面、页面旋转和浏览器全屏阅读模式
- 文档内文字搜索、搜索高亮和文字层复制
- 支持单页、双页和“双页，奇数页在左”阅读布局
- 按当前页、全部页面或自定义范围打印和导出，支持 PNG/JPG/PDF/TXT、DPI、背景颜色及多页图片 ZIP，支持复制文字和下载全文
- 支持拖放打开 OFD、最近打开文件、恢复上次阅读页和取消长时间操作
- 桌面端键盘导航，以及手机端单指横滑翻页和双指捏合缩放

完整的 WASM API、Worker 协议、字体加载和缓存说明见 [`cmd/ofd-wasm/README.md`](cmd/ofd-wasm/README.md)。

<details>
<summary>命令行程序打包</summary>

根目录 [Makefile](Makefile) 将各工具打包为独立 ZIP。常用目标如下：

```bash
# 当前平台的命令行工具
make package-tools

# 当前平台的桌面阅读器和命令行工具
make package-desktop

# 默认打包：还包括 Android、WASM Web；Linux 主机额外生成交叉平台包
make package
```

| 目标 | 要求与范围 |
|------|------------|
| `package-arm64` | Linux ARM64 命令行工具，CGO 关闭，跳过桌面阅读器。 |
| `package-darwin-arm64` / `package-darwin-amd64` | Linux 主机交叉编译 macOS 命令行工具时关闭 CGO、跳过桌面阅读器。 |
| `package-windows-amd64` | Windows x86_64 桌面与命令行工具；需要 MinGW-w64，可用 `WINDOWS_CC` 指定编译器。 |
| `package-windows-arm64` | Windows ARM64 桌面与命令行工具，启用 CGO；需要 LLVM MinGW 的 clang/clang++，可用 `WINDOWS_ARM64_CC`、`WINDOWS_ARM64_CXX` 或 `CC`、`CXX` 覆盖。 |
| `package-viewer-android` / `package-viewer-android-zip` | Android APK / APK ZIP，需要 Fyne、Android SDK 和 NDK。 |
| `package-wasm-web` | 浏览器阅读器 Web 包；WASM 构建与运行见上文。 |
| `package-converter` / `package-validator` / `package-invoice` 等 | 只生成指定工具的包；全部目标见 `make help`。 |

构建输出默认在 `/tmp/ofd/.build`，发布包在 `/tmp/ofd/dist`；可以通过 Make 变量
`BUILD_DIR`、`DIST_DIR` 覆盖。ZIP 包包含二进制和对应 README；`ofd-thumbnailer` 仅用于
Linux，其安装说明见 [缩略图工具](cmd/ofd-thumbnailer/README.md)。

Fyne 查看器需要目标平台的图形开发库。Windows 版查看器使用 `-H=windowsgui` 编译，不显示命令行窗口。

</details>

## 快速开始

### 作为 Go 库使用

在已有的 Go 项目中添加 OFD 依赖：

```bash
go get github.com/zc310/ofd@latest
```

该命令引入已发布的 Go 库版本，不会安装桌面阅读器或命令行程序。桌面版和命令行程序
见上文构建与打包说明；源码分支的字体、渲染和 JPEG2000 依赖维护见
[PDF 转换维护说明](internal/pdf2ofd/README.md)。

完整可运行的调用方式见 [`ofd-viewer`](https://github.com/zc310/ofd-viewer) 与 [`ofd-toolbox`](https://github.com/zc310/ofd-toolbox)，前者是基于本库的桌面阅读器，后者是基于本库的纯前端工具箱（转换、校验、分析、发票抽取、水印与合并，整个应用编译成一个 WebAssembly 模块，文件不上传）。

#### 转换与渲染示例

转换入口的第一个参数是取消信号，命令行工具一般传 `context.Background()`，长任务应传入带超时的 `context`。以 OFD 转 PDF 为例：

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/zc310/ofd/pkg/converter"
)

func main() {
	input, err := os.Open("input.ofd")
	if err != nil {
		log.Fatal(err)
	}
	defer input.Close()

	output, err := os.Create("output.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer output.Close()

	if err := converter.PDF(context.Background(), input, output); err != nil {
		log.Fatal(err)
	}
}
```

`input` 可以是文件路径、`*os.File` 或字节切片；`converter.Image`、`converter.Text`、`converter.Markdown` 等入口用法一致，并支持 `converter.Page(n)`、`converter.DPI(dpi)` 等选项。完整示例见 [`pkg/converter/examples_test.go`](pkg/converter/examples_test.go)。

如果需要在程序里内嵌一个阅读器（按需渲染页面、取文字、导出），可以直接使用 `pkg/webreader`——桌面版 [`ofd-viewer`](https://github.com/zc310/ofd-viewer) 和浏览器版 `ofd-wasm` 都基于同一套接口实现：

```go
package main

import (
	"fmt"
	"image/color"
	"log"
	"os"

	"github.com/zc310/ofd/pkg/webreader"
)

func main() {
	data, err := os.ReadFile("input.ofd")
	if err != nil {
		log.Fatal(err)
	}

	reader, err := webreader.Open(data)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	fmt.Println("总页数：", reader.PageCount())

	// 按需渲染单页为 PNG 字节流，交给界面显示。
	png, err := reader.RenderPage(0, webreader.RenderOptions{
		DPI:        144,
		Background: color.White,
		Format:     webreader.RenderPNG,
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = png

	// 页面文字可用于搜索、复制；RenderPDFTo 可导出选中的页面。
	runs, err := reader.Text(0)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("首页文字段数：", len(runs))
}
```

`webreader.Open` 打开文档后必须调用 `Close` 释放资源。页面图像与文字都按页缓存，长文档建议在 `Open` 时通过 `webreader.OpenOptions` 调整缓存容量；渲染页建议显式指定 `DPI`（1~600），否则使用默认值。

`reader.Versions()` 返回各文档体的版本清单与入口 `DocRoot`。
调用 `reader.SelectVersion(scope, id)` 按入口加载历史页面和资源，其余文档体恢复主入口；
`id` 为空时恢复所有主入口文档。成功后重新读取 `Pages()`、`Fonts()` 和导航信息，
旧页码与缓存不再适用；缺失版本或依赖文件时返回错误并保留当前视图。

<details>
<summary>创建 OFD</summary>

使用 `pkg/creator` 的 Go 模型创建文档，支持文字、路径、图片、字体和多页内容：

```go
package main

import (
	"log"

	"github.com/zc310/ofd/pkg/creator"
)

func main() {
	document := creator.Document{
		ID: "example-document", Title: "创建示例",
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{
				X: 20, Y: 30, Width: 100, Height: 10,
				Size: 4.233, Font: "SimSun", Value: "你好，OFD",
			},
		}}},
	}
	if err := creator.CreateFile(document, "output.ofd"); err != nil {
		log.Fatal(err)
	}
}
```

坐标、边界和字号均以毫米计，页面尺寸默认 A4；示例使用逻辑字体，显示时需要可用的中文字体。

| 需求 | 入口 |
|------|------|
| 写入文件 | `CreateFile`；需要选项时打开文件后调用 `CreateWithOptions` |
| 写入 `io.Writer` 或获取字节 | `Create` / `Marshal`，以及对应的 `WithOptions` 变体 |
| 大文档按需提供页面 | `PageProvider` 与 `CreateFileWithPages` / `CreateWithPagesOptions` |

文字定位、资源、图层、裁剪、动作、签名与流式创建的详细说明见
[创建器库指南](pkg/creator/README.md)。使用 JSON/YAML/TOML 配置创建文档，见
[ofd-creator 命令说明](cmd/ofd-creator/README.md)和[示例配置](cmd/ofd-creator/examples/)。

</details>

<details>
<summary>合并 OFD</summary>

`pkg/merge` 提供 ZIP 级合并：把多个 OFD 的文档体（DocBody）打包成一个多文档 OFD，目录树原样搬运到新的 `Doc_0`、`Doc_1` … 目录，只重写 `OFD.xml` 与签名文件中的包内路径，不解析页面和资源。

```go
package main

import (
	"log"
	"os"

	"github.com/zc310/ofd/pkg/merge"
)

func main() {
	output, err := os.Create("merged.ofd")
	if err != nil {
		log.Fatal(err)
	}
	defer output.Close()

	if err := merge.Files([]string{"a.ofd", "b.ofd"}, output, merge.Options{}); err != nil {
		log.Fatal(err)
	}
}
```

入口包括 `Files`（文件路径，按需读取）、`Bytes`（内存字节）、`Sources`（`Path`/`Data`/`io.ReaderAt` 三种来源）和 `Marshal`（返回字节）。`Options.Signatures` 控制签名处理（`preserve`/`rewrite`/`drop`），`Options.Orphans` 控制文档目录之外条目的处理（`error`/`ignore`/`preserve`），`Options.Limits` 限制条目数和解压大小，`Options.OnWarning`/`Options.OnSignature` 分别接收非致命提示与签名处理结果；`VerifySignatures` 可校验输出文档每个签名的摘要与密码学签名。命令行对应 `ofd-creator merge`。

需要跨文档拼页时使用模型级合并 `Pages`：它把每个输入文档体的页面解析为 creator 模型后重新生成一个单文档 OFD，自动重命名/重编号文档级资源并改写引用；签名和版本会丢弃，大纲、书签、动作和页面注解会保留并重写跳转页索引。`PageOptions.Pages` 可按拼接页序选页/重排，`PageOptions.Selectors` 可按来源选页（来源从 1 开始），`PageOptions.Limits` 限制单输入字节数、输入总字节数与输出页数，`PageOptions.Concurrency` 控制并行解析输入的并发数（默认 4），`PageOptions.ID/Title/Author/Subject` 可覆盖输出元数据。

需要重新签名时，`pkg/sign` 提供 `Sign`：为每个文档体计算引用摘要、生成 `Signature.xml`，把该文件交给外部命令并写回其输出的 `SignedValue.dat`；`Options.Deterministic` 固定重新打包的 ZIP 时间（配合固定的 `Options.Date` 可得到字节可复现的结果），命令行对应 `ofd-creator merge --sign-cmd --deterministic`，私钥与算法由外部命令负责。仓库包含最小示例签名器 [`cmd/ofd-signer-demo`](cmd/ofd-signer-demo)，用于演示 `--sign-cmd` 协议和 SES 结构（仅演示/测试，不可用于生产）。

[`pkg/sign`](pkg/sign/README.md) 还提供 GB/T 38540-2020 SES V4 的 SM2/SM3 印章与签章生成，以及 `Verify` 完整验证接口，覆盖授权证书/证书杂凑列表、印章有效期与状态、证书用途/信任/吊销、原文绑定和可选时间戳。完整结论区分通过、失败和证据不足；证书信任根、CRL 及印章状态由应用提供。分析报告和浏览器签名面板展示该结论，校验器可通过 `--check-signatures` 启用对应检查。`ofd-seal` 的显示尺寸使用毫米，支持独立配置获授权签章者。

欢迎使用自己的测试文档和证书试用，也可在 Go 项目中导入 `github.com/zc310/ofd/pkg/sign` 接入。命令行工具可在仓库根目录编译：

```bash
go build -o ofd-seal ./cmd/ofd-seal
go build -o ofd-validator ./cmd/ofd-validator
```

第三方 TSA 服务的实际互操作尚未验证；试用中发现兼容性问题，欢迎通过 [Issues](https://github.com/zc310/ofd/issues/new) 反馈。

```go
if err := merge.Pages([]merge.Source{{Path: "a.ofd"}, {Path: "b.ofd"}}, output, merge.PageOptions{}); err != nil {
	log.Fatal(err)
}
```

</details>

<details>
<summary>OFD 文件校验</summary>

`ofd-validator` 是面向 OFD 文件包的完整性和规范性校验工具，用于检查容器安全、XML 结构、Schema
规范、文件引用、对象 ID 和签名摘要，帮助快速定位 OFD 文件中的结构问题和兼容性问题。

校验工具位于 `cmd/ofd-validator`，并使用通过 `go:embed` 内置的 `OFD-Schema` XSD。运行时不依赖
`xmllint`、外部模式文件或网络。校验结果会统一生成报告，支持终端查看、归档或交给 CI 和其他程序处理：

```bash
go run ./cmd/ofd-validator --format text testdata/helloworld.ofd
```

报告支持文本、Markdown、JSON、PDF 和 XLSX 五种格式：

```bash
go run ./cmd/ofd-validator --format json --pretty testdata/helloworld.ofd > report.json
go run ./cmd/ofd-validator --format markdown -o report.md testdata/helloworld.ofd
go run ./cmd/ofd-validator -o report.md testdata/helloworld.ofd
go run ./cmd/ofd-validator -o report.json testdata/helloworld.ofd
go run ./cmd/ofd-validator -o report.txt testdata/helloworld.ofd
go run ./cmd/ofd-validator --format pdf --font /path/to/SimSun.ttf \
  -o report.pdf testdata/helloworld.ofd
go run ./cmd/ofd-validator --format xlsx -o report.xlsx testdata/helloworld.ofd
```

未显式指定 `--format` 时，输出文件扩展名 `.txt`、`.md`/`.markdown`、`.json`、`.pdf` 和 `.xlsx`
分别对应 `text`、`markdown`、`json`、`pdf` 和 `xlsx`；显式指定的报告格式优先。标准输出、无扩展名
或其他扩展名仍默认使用 `text`。

报告包含输入文件、检测时间、校验状态、错误和警告汇总、各校验阶段状态，以及每条问题的严重级别、
校验阶段、错误代码、文件位置和 XML 路径等详细信息。JSON 报告适合 CI、脚本和其他程序读取；Markdown
适合代码评审和文档归档；PDF 适合打印或发送给其他人员；XLSX 报告以 Excel 工作簿形式提供「汇总」和
「问题」两个工作表，适合筛选、归档和二次处理。

默认使用 `strict` 模式；`compat` 模式会将 XSD 错误降级为警告，`structural` 模式跳过 XSD。
使用 `--skip-xsd` 等价于 `structural` 模式。报告正文和 CLI 提示使用中文，JSON 同时保留机器可读
的英文枚举和 `*_zh` 中文字段。退出码 `0` 表示没有错误，`1` 表示存在校验错误（使用
`--fail-on-warning` 时警告也会导致退出码 1），`2` 表示工具配置或输入错误。

</details>

<details>
<summary>OFD 文件分析</summary>

`ofd-analyzer` 是面向 OFD 文件的结构分析工具，用于快速了解文档组成、资源使用情况和对象引用关系，适合文档排查、资源审计、转换问题定位以及生成结构化分析报告。

分析工具位于 `cmd/ofd-analyzer`，默认将纯文本报告输出到标准输出，也支持 Markdown、JSON、PDF 和 XLSX。报告可以直接阅读，也可以使用 JSON 供脚本或其他工具继续处理：

```bash
# 默认输出纯文本报告
go run ./cmd/ofd-analyzer testdata/helloworld.ofd

# 输出适合程序处理的 JSON 报告
go run ./cmd/ofd-analyzer --format json testdata/helloworld.ofd

# 输出缩进后的 JSON 报告文件
go run ./cmd/ofd-analyzer --format json --pretty \
  -o analyzer-report.json testdata/multi_demo.ofd

# 输出 Markdown 报告
go run ./cmd/ofd-analyzer --format text -o analyzer-report.txt testdata/helloworld.ofd
go run ./cmd/ofd-analyzer --format markdown -o analyzer-report.md testdata/helloworld.ofd
go run ./cmd/ofd-analyzer -o analyzer-report.json testdata/helloworld.ofd

# 输出 PDF 报告；PDF 报告需要可用字体
go run ./cmd/ofd-analyzer --format pdf --font /path/to/font.ttf -o analyzer-report.pdf testdata/helloworld.ofd

# 输出 XLSX 报告
go run ./cmd/ofd-analyzer --format xlsx -o analyzer-report.xlsx testdata/helloworld.ofd

# 指定 SM2 UID、签名值格式、证书链信任根和离线 CRL
go run ./cmd/ofd-analyzer --signature-uid custom-id --signature-format auto --signature-roots roots.pem test.ofd
go run ./cmd/ofd-analyzer --signature-crls revoked.crl --signature-revocation-issuers issuer.pem test.ofd
```

分析报告主要包含：

- 文档基本信息、Schema 版本、ZIP 包统计和输入文件信息
- 文档体、页面列表以及页面尺寸等页面结构信息
- 页面对象、文字对象和字符数量统计
- 图片、字体、绘制参数、复合图元、颜色空间、模板和 Pattern 等资源统计
- 附件、注解、签名值结构解析、签名摘要校验、SES 签名密码学验证以及文件引用和 ID 引用关系
- 绘制参数的定义数、引用次数、无法解析引用数和 `Relative` 继承循环
- 字符的 Unicode code point、UTF-8 字节、空白字符和 glyph 数量

分析范围和行为：

- 模板、复合图元和 Pattern 默认只统计定义及引用，不展开内部对象，避免重复计数
- 使用 `--no-templates` 可跳过模板定义、模板引用和模板 `PageRes` 资源分析
- 使用 `--no-annotations` 可跳过注解及 Appearance 分析
- 使用 `--no-signatures` 可跳过签名清单分析
- 使用 `--signature-uid` 和 `--signature-format` 配置 SM2 签名验证；使用 `--signature-roots` 显式启用证书链校验；使用 `--signature-crls` 和 `--signature-revocation-issuers` 启用不联网的离线 CRL 吊销校验
- 使用 `--no-package` 可跳过 ZIP 条目大小统计，但保留包内路径索引
- 使用 `--fail-on-warning` 可在发现分析警告时返回退出码 `1`

资源引用使用 `doc[n]/kind:id` 形式表示文档体作用域，可以区分不同文档体中重复使用的 ID。完整的命令选项、报告字段和退出码说明见 [`cmd/ofd-analyzer/README.md`](cmd/ofd-analyzer/README.md)。

</details>

<details>
<summary>电子发票信息抽取</summary>

`ofd-invoice` 从增值税电子发票和铁路电子客票 OFD 中抽取结构化附件，输出 JSON、CSV/XLSX
对账报表，并支持去重、自校验和 PDF 合并打印。页面文字层用于补齐票面标题和大写金额。

```bash
go run ./cmd/ofd-invoice invoice.ofd
go run ./cmd/ofd-invoice --pretty -o invoice.json invoice.ofd
```

字段包括发票代码/号码、开票日期、校验码、机器编号、不含税金额、税额、价税合计（小写与大写）、税控码、收款人/复核人/开票人、购销方资料以及逐行价税明细；金额、数量、税率使用精确十进制表示。附件缺失或不可用时返回错误，不处理 PDF 等非 OFD 输入。库 API（`github.com/zc310/ofd/pkg/invoice` 的 `Extract`）可直接在外部代码中复用。完整用法见 [`cmd/ofd-invoice/README.md`](cmd/ofd-invoice/README.md)。

</details>

<details>
<summary>ofd-server 转换服务</summary>

`ofd-server` 将转换能力提供为 HTTP 服务。`output.kind=stream` 同步返回产物；目录或
远程存储输出走异步队列，提交后返回 `202` 与任务 ID，完成后可以回调通知。

```bash
make package-server
# 或
go run ./cmd/ofd-server -c ofd-server.json
```

常用端点为 `POST /v1/convert`（提交）、`GET /v1/jobs/{id}`（查询）、
`POST /v1/jobs/{id}/cancel`（取消排队中的任务）与 `/healthz`、`/readyz`。
默认监听 `:9705`，需配置数据库、输出目录和 API 令牌；`/v1/*` 请求需要认证。

与 `ofd-converter` 相比，它多了队列与并发控制、任务状态持久化、崩溃恢复和 Webhook 通知，代价是需要配置文件和数据库目录。完整用法、配置项与注意事项见 [`cmd/ofd-server/README.md`](cmd/ofd-server/README.md)。

</details>

<details>
<summary>OFD 档案预检和归档准备</summary>

`ofd-archive` 在现有校验器和分析器之上提供面向档案处理的预检、技术清单、归档准备目录和固定性验证。它会原样保留输入 OFD，记录档案著录元数据、分析报告、附件副本和 SHA-256 清单，不会自动修改签名文档或删除动作、媒体、注释和附件：

```bash
go run ./cmd/ofd-archive check --format text testdata/helloworld.ofd
go run ./cmd/ofd-archive check --format markdown testdata/helloworld.ofd > /tmp/ofd-check.md
go run ./cmd/ofd-archive manifest --format json --pretty testdata/helloworld.ofd
go run ./cmd/ofd-archive matrix --mode structural testdata/helloworld.ofd > /tmp/ofd-matrix.md
go run ./cmd/ofd-archive prepare testdata/helloworld.ofd \
  --metadata archive.json --output /tmp/ofd-archive
go run ./cmd/ofd-archive verify /tmp/ofd-archive
```

准备目录不是 GB/T 42133-2022 规定的新封装格式，也不等同于法律、合规或档案移交结论。完整用法见 [`cmd/ofd-archive/README.md`](cmd/ofd-archive/README.md)。

`matrix` 命令依据已配置的整理版条款目录逐项生成符合性矩阵，输出每条要求、状态、自动检测器、证据引用、人工确认标记和限制说明。矩阵中的 `passed` 仅表示当前技术证据支持通过，`manual_review`、`not_assessed` 和 `unsupported` 不应解释为符合；正式标准引用仍应以正式发布文本为准。

矩阵支持 Markdown、JSON 和 XLSX 输出。XLSX 文件包含 `汇总` 页和 `条文矩阵` 明细页：

```bash
go run ./cmd/ofd-archive matrix --mode structural --format xlsx \
  --output /tmp/ofd-matrix.xlsx testdata/helloworld.ofd
```

</details>

<details>
<summary>命令行批量转换</summary>

OFD 文件可以包含多个文档体。转换器和查看器按 `DocBody` 出现顺序合并页面，`Page(n)` 和命令行 `-page n` 使用合并后的全局页码。

`ofd-converter` 支持批量转换目录中的 OFD 文件，默认递归扫描并使用 4 个并发任务，输出目录保留输入目录的
相对路径结构；图片格式会为每个 OFD 建立独立的页面目录：

```bash
go run ./cmd/ofd-converter --input-dir ./ofd --output-dir ./pdf --format pdf
go run ./cmd/ofd-converter --input-dir ./ofd --output-dir ./images --format png --workers 8
# 不覆盖已有输出；已有目标会记为失败
go run ./cmd/ofd-converter --input-dir ./ofd --output-dir ./pdf --format pdf --overwrite=false
# 跳过已有输出，不计为失败
go run ./cmd/ofd-converter --input-dir ./ofd --output-dir ./pdf --format pdf --skip-existing
```

全部输入输出格式、选项与退出码见 [转换工具说明](cmd/ofd-converter/README.md)。

</details>

### OFD 转 PDF

可运行示例见上文 [转换与渲染示例](#转换与渲染示例)，其他输出选项见
[converter 示例](pkg/converter/examples_test.go)。

### PDF / Markdown / Office / HTML 文档转 OFD

使用统一的 `converter.Convert` 入口，按需空白导入对应的导入器，避免核心库链接不使用的依赖：

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/zc310/ofd/pkg/converter"
    _ "github.com/zc310/ofd/pkg/converter/import/markdown" // Markdown → OFD
)

func main() {
    output, err := os.Create("output.ofd")
    if err != nil {
        log.Fatal(err)
    }
    defer output.Close()

    // from 可省略，按文件扩展名或内容自动识别
    if err := converter.Convert(context.Background(), "md", "ofd", "input.md", output); err != nil {
        log.Fatal(err)
    }
}
```

按输入格式注册对应导入器：

| 输入 | 空白导入路径 |
|------|--------------|
| PDF | `github.com/zc310/ofd/pkg/converter/import/pdf` |
| Markdown | `github.com/zc310/ofd/pkg/converter/import/markdown` |
| Office | `github.com/zc310/ofd/pkg/converter/import/office` |
| HTML/MHTML | `github.com/zc310/ofd/pkg/converter/import/html` |
| 图片 | `github.com/zc310/ofd/pkg/converter/import/image` |

Markdown 输入会重新排版为 A4 固定版式；远程图片不会被抓取，只在日志中给出警告。

Office 文档（doc/docx/odt/rtf/wps/pptx/xlsx 等）通过 LibreOffice 命令行转换：`Convert(ctx, from, "pdf", …)` 直接输出 PDF，`Convert(ctx, from, "ofd", …)` 先转 PDF 再生成 OFD。运行环境需安装 LibreOffice，否则返回明确错误；可用 `converter.WithSoffice(path)` 与 `converter.WithOfficeTimeout(d)` 指定可执行文件与超时。

HTML/MHTML（html/htm/mhtml/mht）通过 Chrome/Chromium 打印引擎转换，默认禁止加载外部资源，纸张默认 A4；可用 `converter.WithChrome(path)`、`converter.WithPaperSize("A4")`、`converter.WithLandscape(true)`、`converter.WithAllowRemoteResources(true)` 等选项控制。

PDF 导入可通过 `converter.WithPDFImportOptions(converter.PDFImportOptions{...})`
设置资源预算、同步 `OnDiagnostic` 回调和 `Strict` 严格模式。配置也作用于 Office/HTML
转换到 OFD 时的 PDF 阶段。`Limits` 的零值字段采用默认预算；超限错误用
`errors.Is(err, converter.ErrPDFImportLimitExceeded)` 判断，严格模式的内容缺失错误用
`converter.ErrPDFImportUnsupported` 判断。也可直接调用 PDF 导入包的
`ConvertWithOptions` / `ConvertFileWithOptions`。默认值、诊断来源和示例见
[PDF 支持清单](docs/PDF-SUPPORT.md#资源预算与诊断)。

### OFD 转图像

#### 转换为 PNG

```go
err := converter.Image(context.Background(), "input.ofd",
    converter.Writer(func(page int) (io.WriteCloser, error) {
        return os.Create(fmt.Sprintf("output_%d.png", page))
    }),
    converter.BgColor(color.White),
    converter.PNG(),
)
```

#### 转换为 JPG

```go
err := converter.Image(context.Background(), "input.ofd",
    converter.Writer(func(page int) (io.WriteCloser, error) {
        return os.Create(fmt.Sprintf("output_%d.jpg", page))
    }),
    converter.BgColor(color.White),
    converter.JPG(),
    converter.Page(3),        // 指定全局第 3 页
    converter.DPI(300),       // 设置输出分辨率
)
```

### OFD 转纯文本

纯文本转换只提取文字内容，不保留字体、颜色和页面布局。不同文字对象按行输出，不同页面使用分页符分隔。

```go
import "bytes"

var output bytes.Buffer
err := converter.Text(context.Background(), "input.ofd", &output)
```

### OFD 转 Markdown

Markdown 转换只提取文字内容，不保留字体、颜色和页面布局；每个页面使用二级标题分隔，并转义 Markdown 特殊字符。

```go
var markdown bytes.Buffer
err := converter.Markdown(context.Background(), "input.ofd", &markdown)
```

### OFD 转单文件 HTML

HTML 默认将每页作为内嵌 PNG 写入单个 HTML 文件，也可以选择内嵌 JPG 或 SVG。三种模式都保留页面物理尺寸，不需要外部资源，并支持浏览器打印分页；HTML 的 `<title>` 使用 OFD 第一个文档体的非空 `DocInfo.Title`，没有标题时使用 `OFD 文档`。

```go
var htmlOutput bytes.Buffer
err := converter.HTML(context.Background(), "input.ofd", &htmlOutput,
    converter.DPI(72),
    converter.HTMLPNG(), // 默认选项
)

var jpgHTML bytes.Buffer
err = converter.HTML(context.Background(), "input.ofd", &jpgHTML,
    converter.HTMLJPG(),
)

var svgHTML bytes.Buffer
err = converter.HTML(context.Background(), "input.ofd", &svgHTML,
    converter.HTMLSVG(),
)
```

## 特别感谢

本项目的开发离不开以下项目的启发和帮助，参考其架构、设计或行为实现，本项目自身未复制其代码（第三方依赖的派生情况见各条目和第三方许可清单）：

- [国家标准化管理委员会发布的 GB/T 33190-2016 标准](http://std.samr.gov.cn/)
- [GreenYun/OFD-Schema](https://github.com/GreenYun/OFD-Schema)，校验器内嵌的 OFD XSD 模式来源
- [itlabers/ofd-go](https://github.com/itlabers/ofd-go)
- [xiaoqidun/ofdgo](https://github.com/xiaoqidun/ofdgo)，支持异常内嵌字体（缺失 `OS/2` 表、缺失字形 `cmap` 映射，以及裸 CFF 的 SFNT 包装与度量补齐）的修复。本项目通过其独立移植 [github.com/zc310/fontfix](https://github.com/zc310/fontfix) 引入该能力；fontfix 的字体修复实现派生自该项目（Copyright 2025-2026 Xiao Qi Dun），并已大幅简化与修改
- [tdewolff/canvas](https://github.com/tdewolff/canvas)、[tdewolff/font](https://github.com/tdewolff/font) 与 [lumifloat/tinyskia](https://github.com/lumifloat/tinyskia)，分别是本项目 PDF/SVG 与文字路径渲染、字体解析、可选栅格后端的上游；本项目通过派生移植 [github.com/zc310/canvas](https://github.com/zc310/canvas)、[github.com/zc310/font](https://github.com/zc310/font) 与 [github.com/zc310/tinyskia](https://github.com/zc310/tinyskia) 引入，并针对 OFD 渲染做了修复与性能优化

以下项目的测试与示例 OFD 文档被本项目取用为测试夹具，集中在 [`testdata/ofdrw/`](testdata/ofdrw/)。本项目只使用其文档文件，未使用其代码：

- [ofdrw/ofdrw](https://github.com/ofdrw/ofdrw)（Apache-2.0），取用其样例文档覆盖签名与签章、批注与外部资源、模板与附件、字体修复与图片缺失回退等回归场景：`999.ofd`（5 页真实发票类文档，含 Stamp 注解、签名、附件与二维码）、`ano.ofd`（批注与外部资源样例，55 个注解、9 种嵌入字体）、`zsbk.ofd`（双签名/印章业务文档）、`intro.ofd`（42 页大文档，嵌入字体修复与历史根命名空间）、`signout.ofd`（Stamp 注解以复合图元形式出现）、`不规范资源路径.ofd`（印章位图无透明通道需按键去白底）、`testImageNotFound.ofd`（图片资源缺失时文字字体仍能加载）。这些文件按 Apache-2.0 使用，版权归 ofdrw 项目作者所有

项目依赖的第三方库许可清单见 [THIRD-PARTY-LICENSES](THIRD-PARTY-LICENSES)。

## 许可证

本项目基于 Apache License 2.0 发布，声明见 [LICENSE](LICENSE) 和 [NOTICE](NOTICE)。
