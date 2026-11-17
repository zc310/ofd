# Go API 使用指南

本指南按接入任务介绍公开 Go API。依赖安装和完整的 OFD 转 PDF 程序见
[主 README](../README.md#go-api)；接口签名和全部选项以各包的 Go 文档为准。
应用代码应使用 `pkg/*`，不依赖 `internal/*`。

## 选择入口

| 任务 | 包与说明 | 可运行示例 |
|---|---|---|
| OFD 转 PDF、图像、文字等格式 | [`converter`](#转换与渲染) | [转换示例](../pkg/converter/examples_test.go) |
| 导入 PDF、Markdown、Office 等格式 | [`converter` 导入器](#导入其他格式) | [导入示例](../pkg/converter/examples_test.go) |
| 按页渲染、文字、搜索和导航 | [`reader`](#接入按页阅读接口) | [阅读器示例](../pkg/reader/example_test.go) |
| 创建、合并、拆分和编辑 | [`creator` 及其子包](#创建合并与签章) | [创建示例](../pkg/creator/example_test.go) |
| 签名、加密和完整性保护 | [安全能力指南](SECURITY.md)、[OFD 加解密](#ofd-加解密) | [加解密示例](../pkg/crypt/examples_test.go) |
| 解码标准 XML 声明 | [`spec`](#标准数据模型) | [模型示例](../pkg/spec/examples_test.go) |
| 评估文档权限声明 | [`permission`](#阅读器权限策略) | [权限示例](../pkg/permission/examples_test.go) |

## 输入、输出与错误

- `converter` 和 `reader.Open` 接受文件路径、OFD 字节、`io.Reader` 或完整解锁的
  `crypt.View`。不同工具的输入范围以各自接口为准，不要把此约定套用到所有包。
- 写入调用方提供的 `io.Writer` 时，调用方负责关闭它并处理关闭错误。
  `reader.Open` 返回的阅读器使用完后应调用 `Close`。
- 转换、渲染和搜索等长任务显式接收 `context.Context`；不需要取消时使用
  `context.Background()`。取消或超时返回原始 `ctx.Err()`。
- 取消在页间及读写、绘制、编码等阶段边界检查，不能强行中断已经阻塞的底层 I/O。
  返回错误后应丢弃部分输出；文件包含 PDF 的 `%%EOF` 也不能代替成功返回值。
- 回调在失败前可能已经执行，库不会撤销已经交付的字节或回调副作用。

本文的短代码片段假设已导入对应包，`ctx`、`input`、`output`、`r` 等由调用方提供；
需要独立运行的完整程序时，使用上表链接的示例。

## 转换与渲染

`converter.Encode(ctx, format, input, output, options...)` 按格式名导出 OFD；
常用格式也有独立入口：

| 输出 | Go 入口 | 输出特点 |
|---|---|---|
| PDF | `converter.PDF` | 按支持范围保留页面尺寸、文字和矢量内容。 |
| PNG/JPG/TIFF/SVG | `converter.Image` | 配合格式选项及 `Writer` 或 `ImageWriter` 输出；TIFF 写成单个多页文件。 |
| 纯文本 | `converter.Text` | 按页面位置聚行，用空格近似对齐列，页间使用分页符 `\f`。 |
| Markdown | `converter.Markdown` | 识别段落与标题，转义特殊字符，有文字的页面间使用 `---`；可选表格识别。 |
| DOCX | `converter.DOCX` | 输出可编辑段落、表格和图片，版面由 Word 重新排版。 |
| 单文件 HTML | `converter.HTML` | 内嵌图像和默认透明文字层，支持选择文字、查找和打印分页。 |

```go
err := converter.PDF(ctx, input, output, converter.Page(1))
```

### 页码与输出选项

| 选项 | 约定 |
|---|---|
| `Page(n)` | 从 **1** 开始，按多文档体合并后的全局页码选页。 |
| `DPI(dpi)` | 控制栅格输出分辨率。库默认 96 DPI、透明背景；CLI 默认 150 DPI、白色背景。 |
| `PNGCompressionLevel(level)` | PNG 无损压缩级别：1 优先速度，9 优先压缩率，0 使用默认值 7。 |
| `PDFLossyImages(true)` | 允许 OFD 转 PDF 时将图片颜色数据编码为 JPEG；默认关闭。 |
| `Writer` / `ImageWriter` | `Image` 必须配置的输出回调，分别提供编码流或解码后的图像。 |

PDF 有损图片选项不影响文字和矢量内容，透明蒙版仍无损保存，已有 JPEG 尽量不重复压缩；
新编码 JPEG 使用固定质量 75，不保证产物更小。该选项不控制 PDF 导入或外部工具的直接转换。

文件输入和已解析输入使用同一组公开入口，不需要构造内部渲染类型。
Text、Markdown、DOCX 遇到无法读取的页面会返回包含全局页码的错误，不把损坏页当空白页。
完整选项见 [converter API](https://pkg.go.dev/github.com/zc310/ofd/pkg/converter)。

## 导入其他格式

使用 `converter.Convert(ctx, from, to, input, output, options...)`，并空白导入所需导入器。
`from` 为空时按输入文件扩展名或内容识别格式。

```go
import (
	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/import/markdown"
)
```

将 Markdown 排版为 OFD：

```go
err := converter.Convert(ctx, "md", "ofd", "input.md", output)
```

| 输入 | 空白导入路径 | 说明 |
|---|---|---|
| PDF | `github.com/zc310/ofd/pkg/converter/import/pdf` | [支持范围、预算与诊断](PDF-SUPPORT.md) |
| Markdown | `github.com/zc310/ofd/pkg/converter/import/markdown` | [排版、主题与字体](../pkg/converter/import/markdown/README.md) |
| CSV/TSV | `github.com/zc310/ofd/pkg/converter/import/csv` | [编码、表头与分页](CSV-SUPPORT.md) |
| EML 邮件 | `github.com/zc310/ofd/pkg/converter/import/eml` | [正文、CID 图片与附件](EML-SUPPORT.md) |
| SVG | `github.com/zc310/ofd/pkg/converter/import/svg` | [支持范围](SVG-SUPPORT.md) |
| Office | `github.com/zc310/ofd/pkg/converter/import/office` | [支持范围](OFFICE-SUPPORT.md) |
| HTML/MHTML | `github.com/zc310/ofd/pkg/converter/import/html` | [支持范围与打印选项](HTML-SUPPORT.md) |
| XPS | `github.com/zc310/ofd/pkg/converter/import/xps` | 依赖外部 PDF 转换工具。 |
| 图片 | `github.com/zc310/ofd/pkg/converter/import/image` | 图片导入；OCR 需显式启用。 |

### 排版与外部工具

- Markdown 直接映射到 `creator/flow`，默认重新排版为 A4、保留逻辑字体；远程图片跳过并记录警告。
  直接调用 `markdown.Convert` / `ConvertFile` 可传入 `flow.Options`，设置字体和主题。
  宽表格自动分配列宽并换行，超宽不可断文本报错，不自动旋转页面。
- CSV/TSV 直接排版，默认 UTF-8（接受 BOM）、首行表头、自动列宽和跨页重复表头。
  `WithCSVImportOptions` 配置编码、标题和字体；`FontFile` 与 `FontData` 互斥。
  字段按文本保留，不自动转换编号前导零或金额小数位。
- EML、SVG 先预处理为 HTML，再交给 Chrome/Chromium；Office、HTML、XPS 转 OFD
  先生成 PDF，再执行 PDF 导入。直接转 PDF 时使用外部工具产物。
  外部进程接收调用方上下文，取消后结束处理并清理临时目录。
- EML 的原附件同时嵌入 OFD，可通过 `Reader.Attachments()` / `AttachmentData` 读取。
  纸张、方向和浏览器配置使用 `WithPaperSize`、`WithLandscape`、`WithChrome` 等公共选项。

运行依赖见 [构建指南](BUILDING.md#运行依赖)。

### PDF 导入预算与字体

`WithPDFImportOptions` 配置资源预算、同步 `OnDiagnostic` 回调和 `Strict` 模式；
这些设置也作用于 Office/HTML/XPS 转 OFD 的 PDF 阶段。
默认值、错误判断和示例见 [PDF 资源预算与诊断](PDF-SUPPORT.md#资源预算与诊断)。

PDF 导入默认保存解码后的原始内嵌字体程序；`PDFImportOptions{SaveRepairedFonts: true}`
可将修复结果写入 OFD。字形映射和度量始终使用修复后的字体。

## 接入按页阅读接口

`pkg/reader` 面向阅读器或预览界面，提供按需加载的页面渲染、文字、搜索、导航和导出。
与 `converter.Page` 不同，**Reader 的页面索引从 0 开始**。

```go
r, err := reader.Open(input, reader.OpenOptions{PageCacheCapacity: 8})
if err != nil {
	return err
}
defer r.Close()

var png bytes.Buffer
report, err := r.RenderPageWithReport(ctx, &png, 0, reader.RenderOptions{
	DPI: 144, Background: color.White, Format: reader.RenderPNG,
})
if err != nil {
	return err
}
// png.Bytes() 是编码结果；report 是本次绘制诊断。
fmt.Println(len(png.Bytes()), report.Stats.ObjectsVisited)
```

完整可运行示例见 [reader/example_test.go](../pkg/reader/example_test.go)，
全部接口见 [reader API](https://pkg.go.dev/github.com/zc310/ofd/pkg/reader)。

### 渲染与资源生命周期

- `Open` 最多接收一组 `OpenOptions`，可配置页面缓存和权限策略。
  关闭基于 `crypt.View` 创建的阅读器，不会关闭调用方持有的原视图。
- `RenderPage` 和 `RenderPDF` 将编码结果写入 `io.Writer`。
  `RenderPages` 按页同步回调 `consume(ctx, position, pageIndex, data)`。
  `data` 在回调返回后会复用，需要保留时必须复制；回调内不要重入同一 Reader。
- 渲染 DPI 范围为 1–600，默认 96；单页还受像素预算限制。
  图像和复合单元的替代资源按放置变换及输出分辨率选择，文字和链接仍从矢量描述提取。
- `Versions` / `SelectVersion` 提供版本清单与切换；切换成功后需重新读取页面、
  字体和导航信息。
- 可取消的渲染在页面加载、绘制、栅格化、编码和页间检查上下文；
  WASM 会在阶段边界让出浏览器事件循环，以便接收取消消息。

### 文字、搜索与诊断

`Text(index)` 和 `Search(ctx, query, options)` 默认包含正文、模板及其复合对象内的文字。
图片和字形轮廓不执行 OCR。长文档搜索可用 `MaxPages`、`MaxResults` 限制工作量。

```go
err := r.SetTextOptions(reader.TextOptions{
	IncludeAnnotations: true,
	IncludeWatermarks:  false,
	IncludeSeals:       true,
})
```

设置会同时清理文字和搜索缓存。`TextRun.Source` 区分 `page`、`template`、
`watermark`、`annotation` 和 `seal`；印章文字的字体在子文档中解析，不借用父文档字体 ID。
复合单元的缩放、偏移和裁边映射也用于字符高亮与链接热区。

`RenderPageWithReport` 与 `RenderPage` 使用相同的权限和取消规则，另返回单次调用的
页 ID、绘制用量及可恢复问题。相同原因、对象与资源会合并计数，最多保留 128 种问题，
超出部分记入 `Dropped`。报告可包含字体回退、资源缺失、加载失败及预算拒绝；
**没有诊断不代表通过 OFD 校验**。请求报告时模板重新绘制，缓存命中仍保留应报告的诊断。
诊断消息最多 512 字节且不截断 UTF-8；可恢复问题不会在库内输出 Warn/Error 日志。

### 元数据、图层与导航

| 需求 | 入口与约定 |
|---|---|
| 阅读偏好 | `Preferences()` 返回首个声明偏好的文档体；`TabDisplay` 默认 `FileName`。界面隐藏标志只是可撤销的初始建议，不是访问权限。 |
| 注解 | `AnnotationsForPage(index)` 只加载目标页，`Annotations()` 返回全量清单；读取失败返回错误。`Print/NoZoom/NoRotate/ReadOnly` 描述备注，不控制外观图元。 |
| 图层显示 | `Layers` / `SetHiddenLayers` 查询和控制图层显示；隐藏设置不改写源 OFD。 |
| 字体及媒体 | 模板私有资源用 `(Scope, TemplateID, ID)` 区分；媒体数据通过 `TemplateMediaData` 读取，模板 ID 为零等同 `MediaData`。 |
| 字体使用统计 | `FontUsage` / `FontUsageAll` 接收上下文，在页间响应取消，取消时不返回部分结果。 |
| 页面链接 | `PageLinksForPage` 返回页面坐标热区；书签、附件和媒体按来源文档体解析。 |
| 页面进入动作 | `PageEntryActionsForPage` 返回进入页面时的动作，页索引从 0 开始。 |
| PDF 大纲 | PDF 导出保留内部跳转和外部 URI 大纲；选页会移除指向未导出页的内部条目。 |

`Preferences()` 的 `DocTitle` 使用同一文档体的标题；无标题时由展示端回退输入文件名。
模板资源视图只继承公共/文档资源，不借用正文页资源；字体列表也包含视图中继承的字体。

`PageLink.Region` 是相对 `Boundary` 归一化到 `[0,1]` 的 SVG 路径，不能只用外接矩形
判断复杂热区。`Actions` 非空时按顺序执行命中区域内的动作，不要再执行一次根节点保留的
首条动作字段。复合资源的热区和影片 `AppearanceBoundary` 已映射到页面坐标，不需再次变换；
影片窗口使用外观边界，不按启动区域缩小。非循环的同步声音应播放完再继续，切换文档时取消旧序列。
动作覆盖范围及 PDF 限制见 [标准支持清单](OFD-SUPPORT.md#标准语义与动作边界)。

阅读器结果类型提供 JSON tag；JSON v2 默认将 nil 切片编码为空数组，其他编码器可能不同。

## 创建、合并与签章

| 需求 | Go 入口与说明 |
|---|---|
| 创建 OFD | `creator.CreateFile/Create/Marshal` 及流式页面接口；坐标、边界和字号以毫米计，见[创建器库指南](../pkg/creator/README.md)。 |
| 流式排版 | `creator/flow` 提供断行、分页、表格和多栏等能力，见[流式创作指南](../pkg/creator/flow/README.md)。 |
| 打包多个文档体 | `merge.Files/Bytes/Sources/Marshal` 保留文档体目录，签名处理由 `Options.Signatures` 控制。 |
| 拼页、重排和页面叠加 | `merge.Pages` 生成单文档 OFD，重映射资源与跳转引用；支持 `PageOptions.Compositions`，原签名和版本丢弃，见[合并说明](../cmd/ofd-creator/README.md#合并多个-ofd)。 |
| 按页或文档体拆分 | `split.Open` 建立会话，`Plan` 校验分组，`Write` 写出分卷，见[拆分库指南](../pkg/creator/split/README.md)。 |
| 条目编辑与水印 | `replace.Files/Paths` 编辑包内条目；`watermark.Add/Replace/Remove` 编辑水印注解。 |
| 长期保存转换 | `preserve.Plan/Apply` 生成与执行按标准条款记录的改动计划。 |
| 外部命令签署 | `sign.Sign` 计算引用摘要，将 `Signature.xml` 交给外部命令，写回其 `SignedValue.dat` 输出。 |
| 印章、签章及验证 | `sign.CreateSeal/CreateSignedValue/Verify`，见[签署与验证指南](../pkg/sign/README.md)。 |

内容编辑应在签署前完成。SES 完整验证区分通过、失败和证据不足，证书信任根、CRL 与
印章状态由应用提供；验签接受结论使用 `FullyValid()`。保护顺序、追加普通签名及加密物理包
签名的适用条件见[安全能力指南](SECURITY.md)，不要把摘要通过等同于完整验证通过。
[ofd-signer-demo](../cmd/ofd-signer-demo/README.md) 仅供演示与测试，不提供生产信任链。

批量编辑 XML 后可用 `validator.NewReferenceResolver` 建立一次解压索引，再调用
`Resolve(ctx, overrides)`。`overrides` 只替换既有文件，调用之间互不影响。
只有结果的 `Complete()` 为真时，才可依据 `Reachable` 删除其他文件。
该检查不验证对象 ID、摘要或密码学签名，也不代替资源 ID 粒度的裁剪。

## OFD 加解密

`pkg/crypt` 的 `Inspect/Encrypt/Decrypt/Open` 提供 GM/T 0099—2020 元数据检查、
口令/SM2 证书、多接收者及多层解密等能力。输入支持路径、字节和 `io.Reader`，
输出写入 `io.Writer`；任何错误都要求丢弃部分输出。

其他工具不会自动解锁加密原包。Go 调用方可先取得完整解锁的只读视图：

```go
view, err := crypt.Open(ctx, input, crypt.OpenOptions{
	DecryptOptions: crypt.DecryptOptions{Password: password},
})
if err != nil {
	return err
}
defer view.Close()

err = converter.PDF(ctx, view, output)
```

完整视图也可交给 `reader.Open`、`validator.Validate`、`sign.Verify` 和
`analyzer.Analyze`。检查原始物理保护时仍保留原包读取域；未完整解锁的视图不能交给
普通渲染或验签链。`AllowPartial` 仅用于明确检查已经解锁的条目。

加密范围、独立条目密钥、追加加密层及预算见[加解密库指南](../pkg/crypt/README.md)；
清单、时间戳、引用闭包与内容覆盖见[完整性保护指南](../pkg/integrity/README.md)。
解密成功不代表签名可信，清单签名通过也不等于所有文件内容完整。

## 阅读器权限策略

`pkg/permission` 评估 OFD 中的权限声明和有效期，不提供加密、DRM 或操作系统级防复制能力。
`Policy` 零值执行声明，可直接配置给阅读器、转换器或支持该选项的文档加工入口：

```go
r, err := reader.Open(input, reader.OpenOptions{
	PermissionPolicy: permission.Policy{},
})
```

普通访问不因禁止导出或打印而拒绝，但仍受有效期限制。复制使用 `Export` 声明，
编辑、注释、签名和水印分别判断；错误可用 `errors.Is(err, permission.ErrDenied)` 识别。
权限快照仅代表查询时刻，长任务会复查有效期，运行中拒绝后应丢弃部分输出。

各入口的检查范围、错误原因、显式授权的忽略策略及归档用途例外，统一见
[权限策略指南](../pkg/permission/README.md)。不要把未列出的加工入口视为已经全面门控。

## 标准数据模型

`pkg/spec` 提供 OFD 及已支持密码应用规范的标准常量、基础值类型和 XML 声明，
可直接使用 `encoding/xml` 解码。它适用于读取原始声明，不负责加载引用、渲染、
执行权限、解密或验签。

常见类型包括主入口 `OFD`、文档 `Document`、页面 `PageContent`、资源 `Res`、
权限 `Permissions`、签名 `Signature`，以及几何、颜色和路径类型。
结构模型不承诺 `xml.Marshal` 能生成完整标准文档，创建 OFD 应使用 `pkg/creator`。

`spec.Bool` 是可选布尔值，零值表示未设置，`Value(defaultValue)` 由调用方指定缺省值；
显式 false 用 `spec.NewBool(false)` 构造。三态用法见
[`ExampleBool`](../pkg/spec/examples_test.go)。

模型分类、可选状态、XML/JSON 编码、原始路径及兼容解码的边界，见
[标准声明模型指南](../pkg/spec/README.md)；可运行用法见
[模型示例](../pkg/spec/examples_test.go)。
解码成功不代表严格符合标准或通过安全验证。

## 字体与显示

渲染优先使用修复后的内嵌字体，再尝试原始字体和外部回退；逻辑字体或缺字可能需要
可用的中文字体。SVG 为同名字体的不同子集分配独立字体族，保留普通文字与矢量轮廓。
低 DPI 图片和 SVG 的极细描边采用参考线宽，具体兼容策略见 [OFD 支持清单](OFD-SUPPORT.md)。

字体修复依据及实现见 [fontrepair](../internal/fontrepair/README.md)，与 PDF 文字提取
共用 [Adobe-GB1 映射数据](../internal/cidmap/README.md)。维护入口见
[开发与维护索引](README.md#开发与维护)。
