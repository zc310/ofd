<p align="center">
  <a href="docs/branding/README.md">
    <img src="docs/branding/ofd-icon.svg" width="128" height="128" alt="OFD Go Toolkit 图标">
  </a>
</p>

<h1 align="center">OFD Go Toolkit</h1>

<p align="center">
  <a href="https://pkg.go.dev/github.com/zc310/ofd"><img src="https://pkg.go.dev/badge/github.com/zc310/ofd.svg" alt="GoDoc"></a>
  <a href="https://github.com/zc310/ofd/releases/latest"><img src="https://img.shields.io/github/v/release/zc310/ofd" alt="GitHub Release"></a>
  <a href="https://github.com/zc310/ofd/actions/workflows/go.yml"><img src="https://github.com/zc310/ofd/actions/workflows/go.yml/badge.svg" alt="Go Build &amp; Test"></a>
  <a href="https://coveralls.io/github/zc310/ofd?branch=main"><img src="https://coveralls.io/repos/github/zc310/ofd/badge.svg?branch=main" alt="Coverage Status"></a>
  <a href="https://github.com/zc310/ofd/stargazers"><img src="https://img.shields.io/github/stars/zc310/ofd" alt="GitHub Stars"></a>
</p>

用 Go 解析、创建、转换、渲染、校验和分析 OFD 文档。提供 Go API、命令行工具、
HTTP 服务，以及桌面、Android 和浏览器阅读器。

OFD 转 PDF 默认不新增有损图片压缩；可用 `ofd-converter --pdf-lossy-images input.ofd output.pdf`
或 Go 选项 `converter.PDFLossyImages(true)` 允许图片颜色转 JPEG（质量 75），透明蒙版仍无损保存。

| 想做什么 | 入口 |
|---|---|
| 在线体验 | [OFD Toolbox](https://zc310.github.io/ofd-toolbox/)（覆盖本库大部分功能）；[浏览器阅读器](https://zc310.github.io/ofd/wasm/)；[Fyne Web 阅读器](https://zc310.github.io/ofd/fyne/) |
| 下载应用或命令行工具 | [GitHub Releases](https://github.com/zc310/ofd/releases)、[发布包下载指南](docs/RELEASE-ASSETS.md)（文件名、系统与架构对应表） |
| 转换、创建和检查文档 | [快速开始](#快速开始)、[功能特性](#功能特性) |
| 让 AI 调用命令行工具 | [CLI Skill](SKILL.md)（工具选择、命令示例与安全边界） |
| 接入自己的 Go 项目 | [Go API](#go-api)、[库使用指南](docs/GO-API.md) |
| 组合签名、加密与完整性保护 | [安全能力指南](docs/SECURITY.md) |
| 从源码运行和打包 | [构建与打包](#构建与打包)、[构建指南](docs/BUILDING.md) |
| 查询兼容性与标准资料 | [支持清单](#支持清单与工具文档)、[文档目录](docs/README.md) |

## 快速开始

下载 [Release](https://github.com/zc310/ofd/releases) 中对应平台的命令行程序后运行；
文件名中的 `darwin` 表示 macOS，`amd64` 适用于 Intel/AMD 64 位电脑，完整对应表见
[发布包下载指南](docs/RELEASE-ASSETS.md)：

```bash
# OFD 转 PDF
ofd-converter input.ofd output.pdf

# 首页输出为 PNG；页码从 1 开始
ofd-converter --page 1 --dpi 150 input.ofd page1.png

# PNG 优先编码速度；压缩级别 1-9，默认 7，像素均为无损
ofd-converter --page 1 --png-compression-level 1 input.ofd page1.png

# 多页图片输出为 ZIP
ofd-converter --format png input.ofd pages.zip

# PDF 转 OFD
ofd-converter input.pdf output.ofd

# CSV / TSV 表格转 OFD，长表自动分页并重复表头
ofd-converter inventory.csv /tmp/inventory.ofd
ofd-converter --landscape inventory.tsv /tmp/inventory.ofd

# 邮件和 SVG 转 OFD，需要本机 Chrome/Chromium
ofd-converter message.eml /tmp/message.ofd
ofd-converter --landscape drawing.svg /tmp/drawing.ofd

# 从 YAML 或 JSON 清单创建 OFD
ofd-creator -i document.yaml -o result.ofd --validate

# 校验与分析
ofd-validator --format text input.ofd
ofd-analyzer --format json input.ofd

# 加密与解密；口令文件按原始 UTF-8 字节读取
ofd-crypt encrypt --password-file password.txt -o encrypted.ofd input.ofd
ofd-crypt decrypt --password-file password.txt -o plain.ofd encrypted.ofd

# 多份文档与第一份比较，生成可离线查看的图文差异报告
ofd-compare -o comparison.html original.ofd revised.ofd final.ofd
```

从源码运行时，在仓库根目录将程序名替换为 `go run ./cmd/工具名`，例如：

```bash
go run ./cmd/ofd-converter testdata/ofd-creator/helloworld.ofd /tmp/helloworld.pdf
```

GM/T 0099-2020 加解密使用不提供认证的 CBC；解密成功不证明内容未被篡改、证书可信或签名有效。
重要场景请独立验证签名和完整性证据，详见[密码功能与实现约定](docs/OFD-CRYPTO.md)及[免责声明](DISCLAIMER.md)。

`ofd-converter` 默认使用 150 DPI 和白色背景，`--page` 使用多文档体合并后的全局页码。
批量转换、完整选项和退出码见 [转换工具说明](cmd/ofd-converter/README.md)；
创建清单见 [creator 示例](cmd/ofd-creator/examples)，大对比报告可使用
[`--external-images`](cmd/ofd-compare/README.md) 将图片保存到报告旁。

常规 OFD 转 PDF、图片及 PDF、CSV/TSV 导入使用 Go 实现。Office、HTML、EML、SVG、XPS 和 OCR 的
外部工具要求见 [运行依赖](docs/BUILDING.md#运行依赖)，PDF 的可选 OpenJPEG 后端见
[PDF 支持清单](docs/PDF-SUPPORT.md)。

## 功能特性

| 场景 | 能力与工具 |
|---|---|
| 文档转换 | [ofd-converter](cmd/ofd-converter/README.md)：OFD 转 PDF、DOCX、HTML、文本、Markdown、PNG/JPG、TIFF、SVG；EML/SVG/CSV/TSV/PDF/XPS/Markdown/Office/HTML/图片转 OFD。 |
| 创建与编辑 | [ofd-creator](cmd/ofd-creator/README.md)：JSON/YAML 清单创建、流式自动排版、Material/Material 2/Material 3/Fluent 2/Carbon/Bootstrap 5 主题与命名样式、info/warning/danger 提示框、GB/T 9704—2012 常规公文基础版式、两端对齐、等宽分栏、页底脚注、固定图元区域、合并表格与自动列宽、图表编号与续表题、标题/图/表目录及大纲、标题与页码引用、分节编号与横纵页面混排、配置导出、数据模板填充、明细分页与重复页面、水印与条目替换；支持多文档打包、模型级拼页、选页、重排和页面叠加。 |
| 校验与分析 | [ofd-validator](cmd/ofd-validator/README.md) 检查 ZIP、XML、XSD、引用和语义，OFD/PDF 报告以跨页表格展示问题明细；[ofd-analyzer](cmd/ofd-analyzer/README.md) 分析结构、页面、资源、附件与签名，提供结论摘要、分阶段状态、结构化诊断、附件大小检查、静态 URI 审计和资源依赖闭包，OFD/PDF 以编号摘要、中文阶段表和可跨页的诊断定位表突出结论与检查范围，字体明细以跨页表格展示名称、嵌入文件、样式和使用次数。分析器不执行文档规范校验，该职责由独立的 validator 承担。两者均支持文本、Markdown、JSON、OFD、PDF 和 XLSX 报告，OFD/PDF 共用 flow 默认 Material 主题。 |
| 文档对比 | [ofd-compare](cmd/ofd-compare/README.md)：比较目录、摘要、XML、文字和画面，支持页配对、局部放大与外置图片；输出 HTML、JSON、文本和 Markdown。 |
| 档案预检与处理 | [ofd-archive](cmd/ofd-archive/README.md)：技术清单、归档准备目录、符合性矩阵与 SHA-256 固定性验证；[preserve](cmd/ofd-archive/README.md#长期保存处理preserve) 对单文档执行长期保存转换，保留页面实际尺寸，清理页面、模板及资源内的图元动作，并按转换后引用分析孤立文件。 |
| 发票处理 | [ofd-invoice](cmd/ofd-invoice/README.md)：增值税电子发票、铁路电子客票抽取，支持结构化附件与自定义标引，JSON/CSV/XLSX 汇总、去重、自校验与 PDF 合并打印。 |
| 电子签章 | [ofd-seal](cmd/ofd-seal/README.md)：SES V4 SM2/SM3 电子印章；[签署与验证 API](pkg/sign/README.md) 支持签章生成、外部签署与完整验证，接受签章使用 `Result.FullyValid()`。`sign.Remove` / [`ofd-creator remove-signatures`](cmd/ofd-creator/README.md#移除与清理已有签名) 支持全部或按 ID 移除及历史残留清理。[creator 验签报告](cmd/ofd-creator/README.md#签名处理) 展示完整状态及原因。外部协议示例见 [ofd-signer-demo](cmd/ofd-signer-demo/README.md)。 |
| OFD 加解密 | [ofd-crypt](cmd/ofd-crypt/README.md) 与 [`pkg/crypt`](pkg/crypt/README.md)：口令/证书、多接收者、独立条目密钥、多层解密和只读解密视图；写端支持整包/条目/页面范围及追加加密层。 |
| 普通数字签名与完整性保护 | [`pkg/sign`](pkg/sign/README.md) 支持 SM2/SM3 SignedData、RSA PKCS#1 v1.5/ECDSA CMS、SHA256/384/512 文件引用摘要、附录 B 扩展及普通多签追加；RSA/ECDSA CMS 已通过本地 OpenSSL 双向验证。`ofd-creator sign --append` 保留旧签名，末次可锁定列表；`sign --physical` 支持主入口明文、没有已有签名入口的加密物理包。校验器分别检查物理签名和解密后的文档引用。[`pkg/integrity`](pkg/integrity/README.md) 和 `ofd-crypt integrity` 分别检查带时间戳清单、物理集合、内容覆盖及引用闭包。 |
| 阅读器 | [ofd-viewer](cmd/ofd-viewer/README.md)：桌面、Android 与 Fyne Web 阅读器，Linux 提供 amd64/arm64 DEB、RPM 安装包及桌面集成；[ofd-wasm](cmd/ofd-wasm/README.md)：浏览器阅读器、WASM API 与 Worker 协议。 |
| HTTP 服务 | [ofd-server](cmd/ofd-server/README.md)：同步流输出、异步转换任务、远程产物传输及 OpenAPI 在线文档。 |
| Linux 桌面集成 | [ofd-thumbnailer](cmd/ofd-thumbnailer/README.md)：文件管理器 OFD 首页及 ZIP 包内图片缩略图，加密 OFD 显示固定挂锁图案；提供 amd64/arm64 的 ZIP、DEB 和 RPM 安装包。 |

## Go API

`pkg/spec` 提供共用标准常量及已公开的权限、日期、主入口、元数据、版本、包内路径、标识与几何、颜色与路径、文档与页树、页面图元与资源、动作与导航、视图首选项、附件、自定义标引、注释、签名、扩展及密码应用声明模型；权限策略直接接收
`*spec.Permissions`，公开范围及使用边界见[标准数据模型](docs/GO-API.md#标准数据模型)。

阅读器默认按文档权限声明检查访问有效期；共用 `pkg/permission` 策略与显式诊断授权方式见
[阅读器权限策略](docs/GO-API.md#阅读器权限策略)。条目编辑、水印、签名、OFD格式导出、合并、拆分及清单导出也执行对应权限。归档检查、原件保存与长期保存处理属于独立用途，不因声明或过期拒绝；归档授权与访问控制由业务方负责，普通阅读/导出权限不因此豁免。具体边界见[标准支持清单](docs/OFD-SUPPORT.md)。

在已有 Go 项目中添加依赖，Go 工具链要求以 [go.mod](go.mod) 为准：

```bash
go get github.com/zc310/ofd@latest
```

下面是一个完整的 OFD 转 PDF 示例。转换入口的首参是 `context.Context`，长任务可传入
带取消或超时的上下文：

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

| 需求 | Go 入口 |
|---|---|
| 转换与渲染 | [`pkg/converter`](https://pkg.go.dev/github.com/zc310/ofd/pkg/converter)、[可运行示例](pkg/converter/examples_test.go) |
| 导入 PDF、Markdown 等格式 | [按需注册导入器](docs/GO-API.md#导入其他格式) |
| 按页渲染、文字、搜索与导航 | [`pkg/reader`](https://pkg.go.dev/github.com/zc310/ofd/pkg/reader)、[接入示例](docs/GO-API.md#接入按页阅读接口) |
| 创建 OFD、自动排版与流式写出 | [`pkg/creator` 库指南](pkg/creator/README.md)、[`pkg/creator/flow` 流式创作](pkg/creator/flow/README.md) |
| 多文档合并、拼页和重排 | [`pkg/creator/merge`](https://pkg.go.dev/github.com/zc310/ofd/pkg/creator/merge)、[合并与选页说明](cmd/ofd-creator/README.md#合并多个-ofd) |
| 按范围、每 N 页或文档体拆分 | [`pkg/creator/split` 库指南](pkg/creator/split/README.md)、[拆分命令](cmd/ofd-creator/README.md#拆分-ofd) |
| 条目编辑与水印 | [`pkg/creator/replace`](https://pkg.go.dev/github.com/zc310/ofd/pkg/creator/replace)、[`pkg/creator/watermark`](https://pkg.go.dev/github.com/zc310/ofd/pkg/creator/watermark) |
| 签署、生成印章和验证 | [`pkg/sign` 库指南](pkg/sign/README.md) |
| 检查加密元数据与加解密 OFD | [`pkg/crypt` 库指南](pkg/crypt/README.md)、[密码功能与实现约定](docs/OFD-CRYPTO.md) |
| 整包完整性检查及安全能力组合 | [`pkg/integrity` 库指南](pkg/integrity/README.md)、[安全能力指南](docs/SECURITY.md) |
| 文档对比 | [`pkg/compare` 库指南](pkg/compare/README.md) |
| 长期保存转换与改动计划 | [`pkg/archive/preserve`](https://pkg.go.dev/github.com/zc310/ofd/pkg/archive/preserve)、[输入限制与处理边界](cmd/ofd-archive/README.md#长期保存处理preserve) |

输入类型、页码、DPI、格式导入、缓存与字体等细节见 [Go API 使用指南](docs/GO-API.md)。

Markdown 导入复用 [`creator/flow`](pkg/creator/flow/README.md) 的流式排版，
代码块支持 GitHub 浅色语法高亮及超长行自动折行，未标注或未知语言默认灰底灰字；
可通过 Go 直接入口指定主题和嵌入字体，限制见 [Markdown 导入说明](pkg/converter/import/markdown/README.md)。

Callout 支持中性、info、warning、danger、success，配色随主题选择，可覆盖文档和单框配色；
提供矢量图标、Text/Runs 简写、三种外观、严格同栏策略、续页标题和框内代码块，见 [流式创作说明](pkg/creator/flow/README.md#提示框-callout)。

JSON 请求、配置和创建清单的字段名区分大小写，同一对象中的字段不能重复。
JSON 报告与服务响应应按字段解析，map 键不保证输出顺序。

按页阅读接口支持复合对象内文字的搜索、选取和链接定位；`SetTextOptions` 可选择
加入批注、水印与 OFD 印章文字，`RenderPageWithReport` 返回有界的单次渲染诊断。
PDF 导出保留内部跳转和外部 URI 大纲，示例见[按页阅读接口](docs/GO-API.md#接入按页阅读接口)。
基于本库的独立应用示例有 [ofd-viewer](https://github.com/zc310/ofd-viewer) 与
[ofd-toolbox](https://github.com/zc310/ofd-toolbox)。

## 阅读器

### 桌面版系统要求对照

系统支持范围同时受 Go 工具链、界面框架和实际构建产物的动态库依赖限制，不能只按框架的最低版本判断。
依赖版本以 [go.mod](go.mod) 为准；下表是运行条件，不代表所有系统组合均已实机验证。

| 平台 | Fyne 版 `ofd-viewer` | Wails 版 `ofd-viewer-wails` |
|------|---------------------|---------------------|
| Windows | Windows 10/11，需要可用的 OpenGL 图形驱动，不依赖 WebView2 | Windows 10/11，需要 WebView2 Runtime；当前安装包不捆绑该运行时 |
| macOS | Go 1.26 构建的工具链下限为 macOS 12；Go 1.27 构建为 macOS 13 | 同样受 Go 工具链限制，另需系统 WKWebView 支持界面所用的网页功能 |
| Linux | X11/Wayland、OpenGL 及相应运行库，不依赖 GTK/WebKit | 默认 GTK4、WebKitGTK 6.0 及桌面图形环境；以 Ubuntu 24.04+ 作为发行适配目标，具体包仍需在目标系统构建和验证 |
| Android | 已提供 APK 入口，项目目标为 Android 5.0 / API 21 及以上，实际要求以 APK 为准 | 实验性 APK/AAB，Android 7.0 / API 24 及以上，ARM64 / x86_64，需现代系统 WebView；暂不支持原生打印 |

两版正式 Windows 构建均不支持 Windows 7/8/8.1；特殊兼容工具链不属于正式支持范围。
Linux 两版的 CGO 产物都可能依赖构建机的 glibc 和图形库版本：在新发行版编译，不代表能在旧发行版运行。
例如 Ubuntu 26.04 环境中构建的 Wails 包可能要求 glibc ≥2.38，不能直接用于默认 glibc 2.35 的 Ubuntu 22.04；
即使 glibc 满足，也要核对 GTK/WebKit 等库的符号版本。发行时应在计划支持的最老目标系统上构建并验证。
详细条件见 [Fyne 系统要求](cmd/ofd-viewer/README.md#系统要求) 与 [Wails 系统要求](cmd/ofd-viewer-wails/README.md#系统要求)。

### 阅读器入口

**桌面与 Android**：[ofd-viewer](cmd/ofd-viewer/README.md) 提供连续阅读、缩略图、
四种页面视图、简体中文/英文界面，以及 PDF、图片和文本导出。安装包见
[Releases](https://github.com/zc310/ofd/releases)，系统要求与截图见对应文档。

**实验性 Wails 桌面阅读器**：[ofd-viewer-wails](cmd/ofd-viewer-wails/README.md) 与浏览器共用界面，
支持命令行传入单个 OFD、系统文件关联与单实例打开、简体中文/英文切换，以及独立的 ZIP、Linux DEB/RPM、Windows NSIS 和 macOS `.app` 打包入口；默认应用由用户确认设置，不在安装时抢占。
使用原生 Go 渲染；支持 PNG、JPG、SVG 页面预览、文字选择、搜索、大纲、书签、自定义标签定位、历史版本切换、页面内链接跳转、图层显示控制、页面注解备注和有界注解清单、字体使用页统计、附件和多媒体资源清单、光栅资源预览、按导出权限查看不超过 1 MiB 的 UTF-8 文本附件，以及签名验证明细，部分功能尚未接入。支持正文附件链接，复用内置预览或原生另存为；外部 HTTP/HTTPS 链接每次经原生确认后交给系统浏览器。PNG/JPEG/GIF/BMP/TIFF/WebP 图片最多 8 MiB、2500 万像素，额外格式只显示静态首帧/页；包内音视频（含模板）及附件音视频最多 32 MiB，资源按访问权限、附件按导出权限预览，编码支持取决于系统 WebView，清单点击不自动播放。支持原生拖放单个 OFD、持久化最近五条文档记录，以及按导出权限另存不超过 64 MiB 的附件和资源（禁止覆盖当前输入文档及其链接，不自动打开或执行）。原生打开对话框记住上次选择目录，并在重启后恢复。加密 OFD 可使用口令、原生选择 UTF-8 口令文件或 SM2 PKCS#8 私钥解锁，不落地明文 ZIP、不持久化凭据。

Wails 支持按当前图层导出 PNG/JPG/SVG/PDF/TXT，并由原生对话框选择保存位置；所有格式均支持当前页、全部页面及自定义范围，每次最多 200 页，全部所选页面都必须允许导出。单页图片直接保存，多页图片按所选页序打包为 ZIP；PDF 保留矢量与可用文字，TXT 额外检查复制权限并以换页符分隔页面。PNG/JPG 每页最多 2500 万像素，所有输出最多 64 MiB（含 ZIP 目录）。桌面打印支持相同页面范围，按打印权限准备 150 DPI 位图，在独立原生窗口预览并调用系统打印对话框；不要求导出权限，最多 200 页、合计 64 MiB，正数打印份额需在系统对话框自行设置。

Wails 另提供实验性 Android 7.0+（ARM64 / x86_64）构建：`make package-wails-android` 生成 APK，
`make bundle-wails-android` 生成 AAB，需要 Android SDK/NDK/JDK。继续使用原生 Go 与共享网页界面，
通过系统文档选择器导入副本、保存导出，适配返回键及后台媒体清理；暂不支持原生打印和外部文件打开入口。
权限、私有副本清理、签名与构建参数见 [Android 说明](cmd/ofd-viewer-wails/README.md#android-构建与差异)。

签名面板默认显示 PNG/JPEG/OFD 印章缩略图，点击重新检查访问权限并查看大图。PNG/JPEG 最多 8 MiB、2500 万像素；OFD 子文档第一页渲染为长边最多 2048 像素的大图，并限制输入和解压预算。也可按导出权限通过原生对话框保存原始签名值及 DER/PEM 证书（最多 64 MiB），不导出私钥；预览或导出不代表签名有效或证书可信。

两端共用签章展示与可见区域缩略图队列（最多两个在途任务）；桌面资源和印章缩略图由 Go 等比例缩小到长边最多 256 像素，不再向网页传输完整原图作为缩略图。
PNG/JPEG 资源和印章大图共用预览弹窗，支持适应窗口、100% 原始大小、按钮或滚轮缩放、放大后拖动及键盘平移；关闭时释放图片与交互状态，不影响正文缩放。

**浏览器**：[在线阅读器](https://zc310.github.io/ofd/wasm/)在本地处理 OFD，支持文字选择、
搜索、页面导航、图层与模板显隐、附件和签名检查，以及 PNG/JPG/PDF/TXT 导出。
WASM 阅读器支持使用口令或 SM2 私钥解锁 GM/T 0099-2020 加密文档后浏览。
本地启动方式见 [WASM 构建与运行](docs/BUILDING.md#浏览器阅读器)；API 与 Worker 协议见
[ofd-wasm](cmd/ofd-wasm/README.md)。

**Fyne Web**：[在线体验](https://zc310.github.io/ofd/fyne/)与桌面、Android 版本共用阅读界面，
支持选择本地 OFD 和导出下载，文档在浏览器内处理，需要 WebAssembly 和 WebGL。
构建与运行方式见 [Web 打包](cmd/ofd-viewer/README.md#web-打包)。

## 构建与打包

在仓库根目录执行，发行版本由 [Makefile](Makefile) 的 `VERSION` 统一注入：

```bash
# 当前平台的命令行工具
make build-tools

# 当前平台的命令行工具 ZIP 包
make package-tools

# 当前平台的桌面阅读器与命令行工具包
make package-desktop
```

构建输出默认位于 `/tmp/ofd/.build`，发布包位于 `/tmp/ofd/dist`，可通过
`BUILD_DIR`、`DIST_DIR` 覆盖。默认 `make` 还会生成 Linux 缩略图 DEB 和 RPM，需先安装
nFPM（`make install-nfpm` 并将 Go bin 目录加入 PATH，或设置 `NFPM`）。
Android、WASM、交叉编译、系统依赖与 Windows 7
兼容工具链的试编译说明见 [构建指南](docs/BUILDING.md)。

可选用 `go build -tags pdfcpu ./cmd/ofd-converter` 注册实验性 pdfcpu PDF 写入后端；
不加标签仍使用 canvas。实验后端支持链接、大纲和有损图片选项，其他限制详见
[pdfcpu 写入包](internal/pdfcpuwriter/README.md#构建标签注册)。

## 支持清单与工具文档

支持清单区分已支持、部分支持和暂不支持，记录能力边界与兼容处理：

| 格式 | 支持范围与限制 |
|---|---|
| OFD | [解析、创建、渲染、校验与阅读](docs/OFD-SUPPORT.md) |
| PDF | [PDF 转 OFD、资源预算与诊断](docs/PDF-SUPPORT.md) |
| Office | [Office 转 OFD/PDF](docs/OFFICE-SUPPORT.md) |
| HTML/MHTML | [浏览器打印与格式转换](docs/HTML-SUPPORT.md) |
| CSV/TSV | [表格导入、编码与字体](docs/CSV-SUPPORT.md) |
| EML 邮件 | [邮件正文、CID 图片与原附件保留](docs/EML-SUPPORT.md) |
| SVG | [矢量图导入与纸张设置](docs/SVG-SUPPORT.md) |

工具的参数与退出码见[功能表中的文档](#功能特性)。标准资料、截图与维护入口汇总在
[文档目录](docs/README.md)；已发布接口的破坏性变更见 [CHANGELOG](CHANGELOG.md)。
指定版本的性能测试见 [ofd-performance](https://github.com/zc310/ofd-performance)，结果见[性能趋势](https://zc310.github.io/ofd-performance/)。
交叉库性能对比见 [ofd-benchmark](https://github.com/zc310/ofd-benchmark)，实际表现取决于
文档样本、字体、操作系统和运行环境。

## OFD 简介

OFD（Open Fixed-layout Document）是中国电子文件领域常用的版式文档格式，适用于
电子证照、电子发票、数字档案和公文。文件通常以 ZIP 容器组织 XML、页面、文字、
路径、图片、字体、附件和签名等资源，并保留相应的布局和绘制信息。

GB/T 33190-2016 等标准的官方查询链接见[标准资料索引](docs/README.md#标准资料)。
项目不随仓库分发标准全文；不同生产方的扩展与兼容处理见 [OFD 支持清单](docs/OFD-SUPPORT.md)。

## 许可证

本项目基于 Apache License 2.0 发布，声明见 [LICENSE](LICENSE) 和 [NOTICE](NOTICE)。
第三方依赖的来源与许可见 [THIRD-PARTY-LICENSES](THIRD-PARTY-LICENSES)，测试样例来源见
[testdata/README.md](testdata/README.md)，项目图标的来源与许可见[图标说明](docs/branding/README.md)。

使用本项目及其输出结果前，请阅读 [免责声明](DISCLAIMER.md)。本项目不保证所有 OFD
文件的解析、转换、校验或渲染结果适用于特定业务、法律或合规场景。如发现仓库、文档
或示例中可能存在侵权内容，请通过 [GitHub Issues](https://github.com/zc310/ofd/issues/new)
联系维护者。
# 内置绘图与字体库维护

OFD 使用的 canvas 精简代码位于 `internal/thirdparty/canvas`，在本仓库维护，
不再依赖外部 `github.com/tdewolff/canvas` 模块或本地 workspace。
原始代码来源、MIT 许可、裁剪范围及测试说明见
[`README.ofd.md`](internal/thirdparty/canvas/README.ofd.md)。

字体解析与子集化代码同样迁入 `internal/thirdparty/font`，不再依赖外部
`github.com/tdewolff/font` 模块；来源、MIT 许可与测试资源说明见
[`README.ofd.md`](internal/thirdparty/font/README.ofd.md)。两者均不附带字体文件；
文档内嵌字体和系统字体回退策略不变。
