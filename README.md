<p align="center">
  <a href="docs/branding/README.md">
    <img src="docs/branding/ofd-icon.svg" width="128" height="128" alt="OFD Go Toolkit 图标">
  </a>
</p>

# OFD Go Toolkit [![GoDoc](https://pkg.go.dev/badge/github.com/zc310/ofd.svg)](https://pkg.go.dev/github/zc310/ofd) [![Go Build & Test](https://github.com/zc310/ofd/actions/workflows/go.yml/badge.svg)](https://github.com/zc310/ofd/actions/workflows/go.yml) [![Coverage Status](https://coveralls.io/repos/github/zc310/ofd/badge.svg?branch=main)](https://coveralls.io/github/zc310/ofd?branch=main)

用于解析、创建、转换、渲染、校验和分析 OFD 文档的 Go 工具包，提供命令行工具、桌面与 Android 阅读器，以及 WASM 浏览器阅读器。

> 使用本项目及其输出结果前，请阅读 [免责声明](DISCLAIMER.md)。本项目不保证所有 OFD 文件的解析、转换、校验或渲染结果适用于特定业务、法律或合规场景。如发现仓库、文档或示例中可能存在侵权内容，请通过 [GitHub Issues](https://github.com/zc310/ofd/issues/new) 联系维护者。

| 使用方式 | 入口 |
|---|---|
| 在线阅读 OFD | [WASM 阅读器](https://zc310.github.io/ofd/wasm/) |
| 下载命令行工具、桌面版或 Android APK | [GitHub Releases](https://github.com/zc310/ofd/releases) |
| 从源码运行或构建 | [快速开始](#快速开始)、[构建与打包](#构建与打包) |
| 在 Go 项目中接入 | [Go API](#go-api)、[API 文档](https://pkg.go.dev/github.com/zc310/ofd) |
| 查询格式兼容性和限制 | [支持清单与工具文档](#支持清单与工具文档) |

## OFD 简介

OFD（Open Fixed-layout Document）是中国电子文件领域常用的版式文档格式，适用于电子证照、电子发票、数字档案和公文等场景。文件通常以 ZIP 容器组织 XML、页面、文字、路径、图片、字体、附件和签名等资源，并保留相应的布局和绘制信息。

项目不随仓库分发标准全文；GB/T 33190-2016《电子文件存储与交换格式 版式文档》等标准的官方查询链接见[标准资料索引](docs/README.md#标准资料)。不同厂商生成的文件可能存在扩展或兼容性差异，具体实现范围见 [OFD 支持清单](docs/OFD-SUPPORT.md)。

## 功能特性

| 类别 | 功能 |
|---|---|
| 文档转换 | OFD 转 PDF、DOCX、单文件 HTML、纯文本、Markdown、PNG/JPG、多页 TIFF、SVG、EPS 和 TeX；PDF/XPS/Markdown/Office/HTML/图片转 OFD |
| 创建与编辑 | Go API 或 JSON/YAML manifest 创建 OFD，支持页面、资源、图层、水印和包内条目替换 |
| 页面与合并 | 多文档体按顺序使用全局页码；支持 ZIP 级打包、模型级拼页、选页重排和资源引用重映射 |
| 校验与分析 | 内嵌 OFD-Schema，检查 ZIP、XML、引用、XSD 与语义；分析文字、资源、附件、注解和签名，生成多种格式的报告 |
| 文档对比 | 多份 OFD 按基准比较目录、文件摘要、XML、文字多处修改与画面；支持文件清单搜索筛选及往返导航、XML 左右对照、选页、页配对依据与映射编辑导出、差异区域局部放大与外置图片，输出 HTML、JSON、文本和 Markdown 报告 |
| 电子签章 | GB/T 38540-2020 SES V4 的 SM2/SM3 印章与签章生成、外部命令签署和完整验证 |
| 档案预检 | 技术清单、归档准备目录、符合性矩阵和 SHA-256 固定性验证 |
| 发票处理 | 增值税电子发票、铁路电子客票附件抽取，JSON/CSV/XLSX 汇总、去重、自校验和 PDF 合并打印 |
| 阅读器 | Fyne 桌面与 Android 阅读器，以及基于 Web Worker 和 WASM 的浏览器阅读器 |

## 快速开始

已下载的命令行程序可直接运行以下命令；从源码运行时，在仓库根目录将程序名替换为 `go run ./cmd/工具名`，例如 `go run ./cmd/ofd-converter input.ofd output.pdf`。源码构建的 Go 版本要求以 [go.mod](go.mod) 为准。

```bash
# OFD 转 PDF
ofd-converter input.ofd output.pdf

# 首页输出为 150 DPI PNG；页码从 1 开始
ofd-converter --page 1 --dpi 150 input.ofd page1.png

# 多页图片输出为 ZIP
ofd-converter --format png input.ofd pages.zip

# PDF 转 OFD
ofd-converter input.pdf output.ofd

# 校验包结构并查看文档信息
ofd-validator --format text input.ofd
ofd-analyzer --format json input.ofd

# 多份 OFD 与第一份比较，生成可离线查看的图文差异报告
ofd-compare -o comparison.html original.ofd revised.ofd final.ofd

# 大报告将图片保存到报告旁的独立目录
ofd-compare --external-images -o comparison.html original.ofd revised.ofd
```

`ofd-converter` 默认输出分辨率为 150 DPI；`--page` 使用多个文档体合并后的全局页码。批量转换目录使用 `--input-dir`、`--output-dir` 和 `--format`，完整选项与退出码见 [ofd-converter](cmd/ofd-converter/README.md)。

部分输入格式需要额外的运行工具：

| 转换方向 | 运行依赖 |
|---|---|
| Office → OFD/PDF | LibreOffice |
| HTML/MHTML → OFD/PDF | Chrome/Chromium |
| XPS → OFD/PDF | libgxps 的 `xpstopdf` |
| 图片 → 带 OCR 文字层的 OFD | Tesseract 及对应语言包；未启用 `--ocr` 时不需要 |

OFD 转 PDF、图片及常规 PDF 导入使用 Go 实现。PDF 的 OpenJPEG 解码后端是可选构建项，启用方法见 [PDF 支持清单](docs/PDF-SUPPORT.md)。

## 支持清单与工具文档

支持清单区分已支持、部分支持和暂不支持，记录能力边界与兼容处理。

| 内容 | 文档 |
|---|---|
| OFD 标准元素与项目能力 | [OFD 支持清单](docs/OFD-SUPPORT.md) |
| PDF 转 OFD 的能力、限制与资源预算 | [PDF 支持清单](docs/PDF-SUPPORT.md) |
| Office 转 OFD/PDF | [Office 支持清单](docs/OFFICE-SUPPORT.md) |
| HTML/MHTML 转 OFD/PDF | [HTML 支持清单](docs/HTML-SUPPORT.md) |
| Go API 创建 OFD | [创建器库指南](pkg/creator/README.md) |
| Go API 生成和验证电子签章 | [签署与验证指南](pkg/sign/README.md) |
| 标准资料、截图与开发维护说明 | [文档目录](docs/README.md) |

| 工具 | 用途与完整说明 |
|---|---|
| [ofd-converter](cmd/ofd-converter/README.md) | 文档与图像转换、批量处理。 |
| [ofd-creator](cmd/ofd-creator/README.md) | 按 manifest 创建 OFD，导出、合并、替换和水印编辑。 |
| [ofd-validator](cmd/ofd-validator/README.md) | 格式、引用、语义和摘要校验。 |
| [ofd-analyzer](cmd/ofd-analyzer/README.md) | 文档结构分析与报告。 |
| [ofd-compare](cmd/ofd-compare/README.md) | 多文件、目录、XML、页面文字与视觉差异对比；Go API 见 [pkg/compare](pkg/compare/README.md)。 |
| [ofd-archive](cmd/ofd-archive/README.md) | 档案预检、归档准备与固定性验证。 |
| [ofd-invoice](cmd/ofd-invoice/README.md) | 发票与铁路电子客票抽取、汇总和合并打印。 |
| [ofd-seal](cmd/ofd-seal/README.md) | SES 电子印章封装及演示图片生成。 |
| [ofd-signer-demo](cmd/ofd-signer-demo/README.md) | 外部签名协议的演示签名器。 |
| [ofd-server](cmd/ofd-server/README.md) | 同步流输出与异步任务转换服务。 |
| [ofd-viewer](cmd/ofd-viewer/README.md) | 桌面与 Android 阅读器。 |
| [ofd-wasm](cmd/ofd-wasm/README.md) | 浏览器阅读器、WASM API 与 Worker 协议。 |
| [ofd-thumbnailer](cmd/ofd-thumbnailer/README.md) | Linux 文件管理器缩略图。 |

性能基准见 [`zc310/ofd-benchmark`](https://github.com/zc310/ofd-benchmark)，对比多个语言和实现的 OFD 转 PDF 速度、文件大小、文本提取及兼容性。实际表现取决于文档样本、字体、操作系统和运行环境。

## 阅读器

### 桌面与 Android

仓库内的 Fyne 阅读器 `ofd-viewer` 支持 Linux、Windows 和 Android，提供连续阅读、缩放、页面导航、双页显示及 PDF、图片、文本导出。下载见 [GitHub Releases](https://github.com/zc310/ofd/releases)，功能、系统要求、构建说明和截图见 [阅读器文档](cmd/ofd-viewer/README.md)。

### WASM 浏览器阅读器

[在线阅读器](https://zc310.github.io/ofd/wasm/)在浏览器本地解析和渲染 OFD，支持连续阅读、缩略图、大纲、文字搜索与复制、单双页布局、打印，以及 PNG/JPG/PDF/TXT 导出。

页面渲染可选 PNG、JPG 或 SVG；“更多 → 图层”可控制各页正文图层和背景/前景模板的显示，搜索、链接、缩略图、导出和打印同步采用当前设置。字体、版本、附件、资源、注解及签名面板提供文档检查入口。

在仓库根目录构建并启动本地服务：

```bash
make build-wasm
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

然后打开 <http://localhost:8080> 并选择 `.ofd` 文件。WASM 需要通过 HTTP 服务加载。完整功能、API、Worker 协议、字体与缓存说明见 [WASM 文档](cmd/ofd-wasm/README.md)。

## 构建与打包

以下命令在仓库根目录执行；Make 目标使用的版本号由 [Makefile](Makefile) 的 `VERSION` 决定。

```bash
# 仅构建当前平台命令行工具
make build-tools

# 将当前平台命令行工具分别打包为 ZIP
make package-tools

# 当前平台的桌面阅读器和命令行工具
make package-desktop

# Android APK，需要 Fyne、Android SDK 和 NDK
make package-viewer-android

# 浏览器阅读器 Web 包
make package-wasm-web
```

构建输出默认位于 `/tmp/ofd/.build`，发布包位于 `/tmp/ofd/dist`；可通过 Make 变量 `BUILD_DIR`、`DIST_DIR` 覆盖。工具 ZIP 包包含二进制和对应 README。`make package` 会构建桌面、命令行、Android 和 WASM 包，并在 Linux 主机上额外生成交叉平台包，所需工具链见下表。

<details>
<summary>平台依赖与交叉编译</summary>

| 目标 | 要求与范围 |
|---|---|
| `package-viewer` | 当前平台桌面阅读器；Fyne 需要 CGO 和目标平台图形开发库，Linux 需要 OpenGL/X11 开发库。 |
| `package-arm64` | Linux ARM64 命令行工具，CGO 关闭，跳过桌面阅读器。 |
| `package-darwin-arm64` / `package-darwin-amd64` | Linux 主机交叉编译 macOS 命令行工具时关闭 CGO、跳过桌面阅读器。 |
| `package-windows-amd64` | Windows x86_64 桌面与命令行工具；需要 MinGW-w64，可用 `WINDOWS_CC` 指定编译器。 |
| `package-windows-arm64` | Windows ARM64 桌面与命令行工具；需要 LLVM MinGW 的 clang/clang++，可用 `WINDOWS_ARM64_CC`、`WINDOWS_ARM64_CXX` 或 `CC`、`CXX` 覆盖。 |
| `package-viewer-android` / `package-viewer-android-zip` | Android APK / APK ZIP，需要 Fyne、Android SDK 和 NDK。 |
| `package-converter` / `package-validator` / `package-invoice` 等 | 只生成指定工具的包；常用目标见 `make help`。 |

Windows 桌面版使用 `-H=windowsgui` 编译，不显示命令行窗口。`ofd-thumbnailer` 仅用于 Linux，见[缩略图安装说明](cmd/ofd-thumbnailer/README.md)。

Windows 7 用户可尝试使用 [XTLS/go-win7](https://github.com/XTLS/go-win7) 提供的兼容 Go 工具链，或参考 [thongtech/go-legacy-win7](https://github.com/thongtech/go-legacy-win7) 的兼容方案自行编译；所用 Go 版本需满足 `go.mod` 的要求。Linux 下交叉编译 Windows x86_64 命令行工具的示例：

```bash
GOTOOLCHAIN=local make GO=/path/to/go-win7/bin/go GOOS=windows GOARCH=amd64 CGO_ENABLED=0 build-tools
```

`GOTOOLCHAIN=local` 用于固定兼容工具链。若遇到 `bcrypt.dll not found`，请检查系统是否安装了 KB2533623 或包含相应 DLL 加载功能的后续更新；桌面阅读器编译需 MinGW-w64，运行需 Universal CRT 和 OpenGL 驱动。具体功能需在目标 Win7 环境中验证。

</details>

## Go API

在已有 Go 项目中添加依赖：

```bash
go get github.com/zc310/ofd@latest
```

这会引入已发布的 Go 库版本。可运行的应用示例见 [`ofd-viewer`](https://github.com/zc310/ofd-viewer) 和 [`ofd-toolbox`](https://github.com/zc310/ofd-toolbox)：前者是基于本库的独立桌面阅读器，后者是将转换、校验、分析、发票抽取、水印与合并编译为 WebAssembly 的前端工具箱。

### 转换与渲染

转换入口首参为 `context.Context`，长任务可传入带取消或超时的上下文。OFD 转 PDF 示例：

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/zc310/ofd/pkg/converter"
)

func main() {
	output, err := os.Create("output.pdf")
	if err != nil {
		log.Fatal(err)
	}

	err = converter.PDF(context.Background(), "input.ofd", output)
	closeErr := output.Close()
	if err != nil {
		log.Fatal(err)
	}
	if closeErr != nil {
		log.Fatal(closeErr)
	}
}
```

`input` 可使用文件路径、`*os.File` 或字节切片。`converter.Page(n)` 从 1 开始选择全局页码，`converter.DPI(dpi)` 设置栅格输出分辨率。库默认使用 96 DPI 和透明背景；CLI 默认使用 150 DPI 和白色背景。更多调用见[可运行示例](pkg/converter/examples_test.go)和 [converter API](https://pkg.go.dev/github.com/zc310/ofd/pkg/converter)。

| 输出 | Go 入口 | 输出特点 |
|---|---|---|
| PDF | `converter.PDF` | 保留页面尺寸，文字和矢量内容按支持范围写入 PDF 对象。 |
| PNG/JPG/TIFF/SVG/EPS/TeX | `converter.Image` 配合格式选项和 `converter.Writer` | 通过回调提供输出流；TIFF 写成单个多页文件。 |
| 纯文本 | `converter.Text` | 按页面位置聚行、用空格近似对齐列，页面间使用分页符 `\f`。 |
| Markdown | `converter.Markdown` | 识别段落与标题，转义特殊字符，有文字的页面间使用 `---`；可选表格识别。 |
| DOCX | `converter.DOCX` | 输出可编辑段落、表格和图片，版面由 Word 重新排版。 |
| 单文件 HTML | `converter.HTML` | 内嵌 PNG/JPG/SVG 与默认透明文字层，支持选择文字、查找和打印分页。 |

### 导入其他格式

使用统一的 `converter.Convert` 入口，并空白导入需要的导入器。以下示例将 Markdown 排版为 OFD：

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/import/markdown"
)

func main() {
	output, err := os.Create("output.ofd")
	if err != nil {
		log.Fatal(err)
	}

	err = converter.Convert(context.Background(), "md", "ofd", "input.md", output)
	closeErr := output.Close()
	if err != nil {
		log.Fatal(err)
	}
	if closeErr != nil {
		log.Fatal(closeErr)
	}
}
```

`from` 为空时按输入文件扩展名或内容识别格式。导入器按需注册：

| 输入 | 空白导入路径 |
|---|---|
| PDF | `github.com/zc310/ofd/pkg/converter/import/pdf` |
| Markdown | `github.com/zc310/ofd/pkg/converter/import/markdown` |
| Office | `github.com/zc310/ofd/pkg/converter/import/office` |
| HTML/MHTML | `github.com/zc310/ofd/pkg/converter/import/html` |
| XPS | `github.com/zc310/ofd/pkg/converter/import/xps` |
| 图片 | `github.com/zc310/ofd/pkg/converter/import/image` |

Markdown 会重新排版为 A4，远程图片会跳过并记录警告。Office/HTML/XPS 转 OFD 时先由外部工具生成 PDF，再执行 PDF 导入；直接转 PDF 时使用外部工具产物。

PDF 导入默认实施资源预算。可通过 `converter.WithPDFImportOptions` 设置预算、同步 `OnDiagnostic` 回调和 `Strict` 模式；这些设置也作用于 Office/HTML/XPS 转 OFD 的 PDF 阶段。默认值、错误判断和示例见 [PDF 资源预算与诊断](docs/PDF-SUPPORT.md#资源预算与诊断)。

### 接入按页阅读接口

`pkg/webreader` 提供页面渲染、文字、搜索、导航与导出接口，供界面按需调用：

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
	// webreader 的页索引从 0 开始。
	png, err := reader.RenderPage(0, webreader.RenderOptions{
		DPI: 144, Background: color.White, Format: webreader.RenderPNG,
	})
	if err != nil {
		log.Fatal(err)
	}
	_ = png // PNG 字节可交给界面显示。

	runs, err := reader.Text(0)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("首页文字段数：", len(runs))
}
```

打开后调用 `Close` 释放资源。长文档可用 `webreader.OpenWithOptions(data, webreader.OpenOptions{...})` 调整页面缓存；渲染 DPI 范围为 1–600，默认 96，单页还受像素预算限制。版本清单和切换由 `Versions`、`SelectVersion` 提供，切换成功后需重新读取页面、字体和导航信息。完整接口见 [webreader API](https://pkg.go.dev/github.com/zc310/ofd/pkg/webreader)。

### 创建、合并与签章

| 需求 | Go 入口与说明 |
|---|---|
| 创建 OFD | `pkg/creator` 的 `CreateFile`、`Create`、`Marshal` 及流式页面接口；坐标、边界和字号以毫米计，见[创建器库指南](pkg/creator/README.md)。 |
| 打包多个文档体 | `pkg/merge.Files`、`Bytes`、`Sources` 保留文档体目录，签名处理由 `Options.Signatures` 控制。 |
| 拼页、选页和重排 | `pkg/merge.Pages` 解析并生成单文档 OFD，重映射资源与跳转引用；原签名和版本丢弃，见[合并命令与选页说明](cmd/ofd-creator/README.md#合并多个-ofd)。 |
| 外部命令签署 | `pkg/sign.Sign` 计算引用摘要，将 `Signature.xml` 交给外部命令，写回其输出的 `SignedValue.dat`。 |
| 生成印章与签章、完整验证 | `pkg/sign.CreateSeal`、`CreateSignedValue`、`Verify`，见[签署与验证指南](pkg/sign/README.md)。 |

SES 完整验证区分通过、失败和证据不足，证书信任根、CRL 与印章状态由应用提供。分析报告和浏览器签名面板展示该结论，校验器通过 `--check-signatures` 启用对应检查。`ofd-seal` 的显示尺寸以毫米计，支持独立配置获授权签章者。

欢迎使用自己的测试文档和证书试用。命令行工具可在仓库根目录编译：

```bash
go build -o ofd-seal ./cmd/ofd-seal
go build -o ofd-validator ./cmd/ofd-validator
```

仓库中的 [ofd-signer-demo](cmd/ofd-signer-demo/README.md) 用于演示外部签名协议和 SES 结构，仅供演示与测试。第三方 TSA 服务的实际互操作尚未验证；兼容性问题可通过 [Issues](https://github.com/zc310/ofd/issues/new) 反馈。

### 字体与显示

渲染优先使用修复后的内嵌字体，再尝试原始字体和外部回退；逻辑字体或缺字可能需要可用的中文字体。SVG 为同名字体的不同子集分配独立字体族，保留普通文字与矢量轮廓。低 DPI 图片和 SVG 的极细描边采用参考线宽，以改善标签显示；具体兼容策略见 [OFD 支持清单](docs/OFD-SUPPORT.md)。

字体修复由仓库内的 [internal/fontrepair](internal/fontrepair/README.md) 按 OpenType 与 Adobe CFF/Type 2 公开规范实现；与 PDF 文字提取共用 [Adobe-GB1 映射数据](internal/cidmap/README.md)，保留 Adobe BSD 许可。依赖维护与回归入口见[开发与维护索引](docs/README.md#开发与维护)。

## 许可证

本项目基于 Apache License 2.0 发布，声明见 [LICENSE](LICENSE) 和 [NOTICE](NOTICE)。
第三方依赖的来源与许可见 [THIRD-PARTY-LICENSES](THIRD-PARTY-LICENSES)，测试样例的来源说明见 [testdata/README.md](testdata/README.md)。
