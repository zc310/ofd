# Go API 使用指南

安装依赖与完整的 OFD 转 PDF 入门示例见[主 README](../README.md#go-api)。可运行的
转换示例位于 [pkg/converter/examples_test.go](../pkg/converter/examples_test.go)。

## 转换与渲染

`pkg/converter` 的转换入口首参为 `context.Context`。长任务可传入带取消或超时的
上下文。`input` 支持文件路径、`*os.File` 或字节切片；写入调用方提供的文件后，
调用方应处理关闭错误。

取消或超时会原样返回 `context.Canceled` 或 `context.DeadlineExceeded`，包括发生在
最后一页写入、输出回调或写入器关闭期间的取消。取消在页间和主要阶段的读写边界
生效，不能强行中断已经阻塞的底层 I/O。失败后应丢弃部分输出；PDF 仍会尝试完成
结构收尾，但包含 `%%EOF` 不代表转换成功，必须检查返回错误。
Text、Markdown、DOCX 转换遇到无法读取的页面时返回包含全局页码的错误，不把损坏页
当作空白页。逐页文字收集和 DOCX 图片提取都会响应取消。

`converter.Page(n)` 从 1 开始选择多文档体合并后的全局页码，`converter.DPI(dpi)`
设置栅格输出分辨率。库默认使用 96 DPI 和透明背景；CLI 默认使用 150 DPI 和白色背景。

| 输出                     | Go 入口                                             | 输出特点                                                               |
|--------------------------|-----------------------------------------------------|------------------------------------------------------------------------|
| PDF                      | `converter.PDF`                                     | 保留页面尺寸，文字和矢量内容按支持范围写入 PDF 对象。                  |
| PNG/JPG/TIFF/SVG/EPS/TeX | `converter.Image` 配合格式选项和 `converter.Writer` | 通过回调提供输出流；TIFF 写成单个多页文件。                            |
| 纯文本                   | `converter.Text`                                    | 按页面位置聚行、用空格近似对齐列，页面间使用分页符 `\f`。              |
| Markdown                 | `converter.Markdown`                                | 识别段落与标题，转义特殊字符，有文字的页面间使用 `---`；可选表格识别。 |
| DOCX                     | `converter.DOCX`                                    | 输出可编辑段落、表格和图片，版面由 Word 重新排版。                     |
| 单文件 HTML              | `converter.HTML`                                    | 内嵌 PNG/JPG/SVG 与默认透明文字层，支持选择文字、查找和打印分页。      |

完整接口见 [converter API](https://pkg.go.dev/github.com/zc310/ofd/pkg/converter)。

## OFD 加解密

`pkg/crypt.Inspect/Encrypt/Decrypt/Open` 实现 GM/T 0099-2020 元数据检查、
口令/SM2 证书、多接收者、独立条目密钥读写和多层解密；写端支持整包/条目/页面范围，
以及用旧凭据预检后追加加密层。页面选择整体纳入共用资源，可能影响其他页面。
输入支持路径、字节和 `io.Reader`，
加解密输出写调用方的 `io.Writer`；任何错误都要求丢弃部分输出。
解密结果是普通明文 OFD，可直接交给 converter、validator 或 sign 的既有接口。
构造包和签名元数据时可复用 `pkg/spec` 的 `Namespace`、`RootDocument`、
`EncryptionEntryPoint`、`IntegrityEntryPoint` 及 `SigTypeSeal/SigTypeSign` 常量。

可运行示例见 [pkg/crypt/examples_test.go](../pkg/crypt/examples_test.go)，预算、错误和
签名保护边界见 [库指南](../pkg/crypt/README.md)。文件流程可使用
[ofd-crypt](../cmd/ofd-crypt/README.md) 的口令文件/标准输入、私钥输入及原子输出。
其他工具不会自动解锁加密原包；Go 调用方可直接传入完整解锁的只读视图：

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

`webreader.OpenInput(view, webreader.OpenOptions{})` 创建自己的读取租约，关闭阅读器不关闭
原视图。`validator.Validate(ctx, view, name)` 先检查原始物理条目，再检查逻辑文档，
保留原始加密状态；`sign.Verify` 和 `analyzer.Analyze` 也直接接受完整视图。
存在外层物理签名时，validator 在原包检查其引用和摘要，解密视图用于普通文档校验。
`AllowPartial` 只用于明确检查已解锁条目，不能把部分视图交给普通渲染/验签链。

`pkg/sign` 的 `Options.Type="Sign"` 接入 SM2/SM3 SignedData 或 RSA/ECDSA CMS 数字签名，
`SignatureMethod` 与 `DigitalSigningOptions.Algorithm` 使用相同组合 OID；`CheckMethod` 可独立选择 SHA256/384/512，缺省 SM3。
`Verify` 按 Type 验证普通签名或 SES，并检查 Relative 基础签名。`FullyValid()` 是完整结论。
`sign.Sign(ctx, input, output, Options{Type: "Sign", Append: true})` 保留旧普通签名并增加记录，
`Relative` 指定基础签名，最后用 `LockSignatures: true` 固定列表；已被摘要保护的列表拒绝追加。
用法和范围见 [签名指南](../pkg/sign/README.md#普通签名追加)。
`Options{Type: "Sign", Physical: true}` 为主入口明文、没有已有签名入口的加密物理包签署；
`VerifyPhysical` 直接检查密文和控制文件，`Verify` 在解锁视图上也保留该物理读取域。
未完整解锁的视图传给 `Verify` 返回 `crypt.ErrLocked`；无凭据验外签使用 `VerifyPhysical`。
`pkg/integrity.Inspect/Protect/Verify` 提供附录 D 清单、带时间戳签名、文件集合、引用闭包及
内容覆盖检查。清单签名通过不等于所有文件内容完整；具体结果和证据要求见
[完整性保护指南](../pkg/integrity/README.md)。
验证中止时须处理返回错误，已有部分报告保持 incomplete 或已确认的 invalid。

## 导入其他格式

使用统一的 `converter.Convert` 入口，并空白导入需要的导入器。以下示例将 Markdown
排版为 OFD：

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

| 输入       | 空白导入路径                                         |
|------------|------------------------------------------------------|
| PDF        | `github.com/zc310/ofd/pkg/converter/import/pdf`      |
| Markdown   | `github.com/zc310/ofd/pkg/converter/import/markdown` |
| CSV/TSV    | `github.com/zc310/ofd/pkg/converter/import/csv`      |
| EML 邮件   | `github.com/zc310/ofd/pkg/converter/import/eml`      |
| SVG        | `github.com/zc310/ofd/pkg/converter/import/svg`      |
| Office     | `github.com/zc310/ofd/pkg/converter/import/office`   |
| HTML/MHTML | `github.com/zc310/ofd/pkg/converter/import/html`     |
| XPS        | `github.com/zc310/ofd/pkg/converter/import/xps`      |
| 图片       | `github.com/zc310/ofd/pkg/converter/import/image`    |

Markdown 会重新排版为 A4，远程图片会跳过并记录警告。Office/HTML/XPS 转 OFD 时先由
外部工具生成 PDF，再执行 PDF 导入；直接转 PDF 时使用外部工具产物。外部工具要求见
[运行依赖](BUILDING.md#运行依赖)。

CSV/TSV 以 Go 直接排版，默认 UTF-8（接受 BOM）、首行表头、自动列宽和跨页重复表头。
通过 `converter.WithCSVImportOptions(converter.CSVImportOptions{NoHeader: true,
Encoding: "gb18030", Title: "设备台账", FontFile: "fonts/cjk.ttf"})` 配置输入；
`FontData` 可替代 `FontFile` 提供字体字节，两者互斥。纸张和方向沿用
`WithPaperSize`、`WithPaperDimensions`、`WithLandscape`。字段保持文本，编号前导零和金额
小数位不会被自动转换；纯拉丁文本可使用内置字体，中文自动查找系统字体并嵌入。
范围和限制见 [CSV/TSV 支持清单](CSV-SUPPORT.md)，可运行用法见 `ExampleConvert_csv`。

EML 与 SVG 先预处理为 HTML，再通过本机 Chrome/Chromium 转换；直接输出 PDF 时使用
浏览器产物，输出 OFD 时执行 PDF 导入并沿用 PDF 资源预算。调用
`converter.Convert(ctx, "eml", "ofd", input, output)` 或将 `from` 改为 `"svg"`，
纸张、方向和浏览器选项沿用 `WithPaperSize`、`WithLandscape`、`WithChrome` 等公共配置。
EML 输出邮件头、选中的正文、CID 图片和附件清单，OFD 中同时嵌入原附件，支持通过
`webreader.Attachments` / `AttachmentData` 列出和读取；SVG 按纸张等比放置。
详见 [EML 支持清单](EML-SUPPORT.md)、[SVG 支持清单](SVG-SUPPORT.md)。

Office、HTML 转换会把调用方上下文传给外部进程，取消后结束处理并清理临时目录。
也可直接调用 Markdown 包的 `Convert(ctx, input, output)` 或
`ConvertFile(ctx, inputPath, outputPath)`；不需要取消时传 `context.Background()`。
`ConvertFile` 在入口已取消时不会创建或截断目标文件。

PDF 导入默认实施资源预算。可通过 `converter.WithPDFImportOptions` 设置预算、同步
`OnDiagnostic` 回调和 `Strict` 模式；这些设置也作用于 Office/HTML/XPS 转 OFD 的
PDF 阶段。默认值、错误判断和示例见 [PDF 资源预算与诊断](PDF-SUPPORT.md#资源预算与诊断)。

PDF 导入默认保存解码后的原始内嵌字体程序；设置
`converter.PDFImportOptions{SaveRepairedFonts: true}` 可将修复结果写入 OFD 包。
这项设置同样作用于经 PDF 的导入阶段，字形映射和度量始终使用修复后的字体。

## 接入按页阅读接口

`pkg/webreader` 提供页面渲染、文字、搜索、导航与导出接口，供界面按需调用：

```go
package main

import (
	"context"
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
	png, err := reader.RenderPage(context.Background(), 0, webreader.RenderOptions{
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

	// 长文档搜索可限制工作量，并用同一个 context 取消。
	report, err := reader.Search(context.Background(), "关键词", webreader.SearchOptions{
		MaxPages: 2000, MaxResults: 5000,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("命中 %d 处（扫描 %d 页，截断=%v）\n", len(report.Results), report.Scanned, report.Truncated)
}
```

打开后调用 `Close` 释放资源。长文档可用
`webreader.OpenWithOptions(data, webreader.OpenOptions{...})` 调整页面缓存；渲染 DPI
范围为 1–600，默认 96，单页还受像素预算限制。版本清单和切换由 `Versions`、
`SelectVersion` 提供，切换成功后需重新读取页面、字体和导航信息。
完整接口见 [webreader API](https://pkg.go.dev/github.com/zc310/ofd/pkg/webreader)。

`RenderPage(ctx, index, options)`、`RenderPages(ctx, indices, options)`、
`RenderPDF(ctx, indices, options)` 和 `RenderPDFTo(ctx, output, indices, options)`
均显式接收上下文。取消在页面加载、绘制、
栅格化、编码及页间的阶段边界生效，返回原始 `ctx.Err()`；单次底层绘制或编码仍需
执行到阶段边界。PDF 输出可能不完整，失败后应丢弃。WASM 下可取消的调用会在
阶段边界让出浏览器事件循环，使取消消息不必等待输出分块才被处理。
不需要取消或超时时，首参可传 `context.Background()`。`RenderPages` 和 `RenderPDF`
失败时不返回部分结果；需要流式输出时使用 `RenderPDFTo`。

`Text` 和 `Search` 默认包含正文、模板及其复合对象内的文字；复合单元的缩放、偏移
和裁边映射也用于字符高亮与链接热区。调用 `SetTextOptions` 可将水印、批注和 OFD
印章子文档里的文字加入覆盖层与搜索，设置会同时清理两种缓存。图片和字形轮廓
不执行 OCR。`TextRun.Source` 为 `page`、`template`、`watermark`、`annotation` 或
`seal`；印章字体在子文档中解析，返回的印章 run 不借用父文档的字体 ID。

```go
err := reader.SetTextOptions(webreader.TextOptions{
    IncludeAnnotations: true,
    IncludeWatermarks:  false,
    IncludeSeals:       true,
})
if err != nil { return err }

png, diagnostics, err := reader.RenderPageWithReport(ctx, 0, webreader.RenderOptions{DPI: 144})
if err != nil { return err }
_ = png
for _, issue := range diagnostics.Diagnostics {
    fmt.Println(issue.Code, issue.ObjectID, issue.ResourceID, issue.Count)
}
```

诊断报告属于单次调用，包含页 ID、实际绘制操作的资源用量和可恢复问题。相同原因、
对象与资源会合并计数，最多保留 128 种问题，超过部分记入 `Dropped`。记录内容包括
字体不可用或回退、图片/复合资源缺失、模板与印章加载失败，以及资源预算拒绝；
没有诊断不代表完成了 OFD 合法性校验。请求报告时模板重新绘制，以捕获本次诊断。

PDF 导出同时保留内部跳转大纲与外部 URI 大纲。选页导出会移除指向未导出页的
内部条目，外部 URI 不受选页范围影响，两种条目可在同一大纲树中混排。

## 创建、合并与签章

| 需求                     | Go 入口与说明                                                                                                                                       |
|--------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| 创建 OFD                 | `pkg/creator` 的 `CreateFile`、`Create`、`Marshal` 及流式页面接口；坐标、边界和字号以毫米计，见[创建器库指南](../pkg/creator/README.md)。           |
| 打包多个文档体           | `pkg/creator/merge.Files`、`Bytes`、`Sources` 保留文档体目录，签名处理由 `Options.Signatures` 控制。                                                        |
| 拼页、选页和重排         | `pkg/creator/merge.Pages` 解析并生成单文档 OFD，重映射资源与跳转引用；原签名和版本丢弃，见[合并命令与选页说明](../cmd/ofd-creator/README.md#合并多个-ofd)。 |
| 按页或文档体拆分         | `pkg/creator/split.Open(ctx, input, options)` 建立可复用会话，`Plan` 校验分组，`Write` 逐份写出；见[拆分库指南](../pkg/creator/split/README.md)。 |
| 条目编辑与水印           | `pkg/creator/replace.Files/Paths` 编辑包内条目，`pkg/creator/watermark.Add/Replace/Remove` 编辑水印注解。 |
| 长期保存转换             | `pkg/archive/preserve.Plan/Apply` 生成与执行按标准条款记录的长期保存改动计划。 |
| 外部命令签署             | `pkg/sign.Sign` 计算引用摘要，将 `Signature.xml` 交给外部命令，写回其输出的 `SignedValue.dat`。                                                     |
| 生成印章与签章、完整验证 | `pkg/sign.CreateSeal`、`CreateSignedValue`、`Verify`，见[签署与验证指南](../pkg/sign/README.md)。                                                   |

批量修改 XML 后检查文件依赖时，可用 `validator.NewReferenceResolver(ctx, input, options...)`
建立一次解压索引，再调用 `Resolve(ctx, overrides)`。`overrides` 仅替换既有文件，调用之间
互不影响；未修改 XML 的引用分析结果会缓存。只有返回索引的 `Complete()` 为真时，才可
根据 `Reachable` 删除其他文件。该检查不验证对象 ID 语义、摘要或密码学签名，也不执行资源
ID 粒度的裁剪；需要像拆分器一样先裁剪资源定义，再分析文件依赖。

SES 完整验证区分通过、失败和证据不足，证书信任根、CRL 与印章状态由应用提供。
分析报告和浏览器签名面板展示该结论，校验器通过 `--check-signatures` 启用对应检查。
`ofd-seal` 的显示尺寸以毫米计，支持独立配置获授权签章者。

欢迎使用自己的测试文档和证书试用。外部签名协议与 SES 结构示例见
[ofd-signer-demo](../cmd/ofd-signer-demo/README.md)，仅供演示与测试。第三方 TSA 服务的
实际互操作尚未验证；兼容性问题可通过 [Issues](https://github.com/zc310/ofd/issues/new) 反馈。

## 字体与显示

渲染优先使用修复后的内嵌字体，再尝试原始字体和外部回退；逻辑字体或缺字可能需要
可用的中文字体。SVG 为同名字体的不同子集分配独立字体族，保留普通文字与矢量轮廓。
低 DPI 图片和 SVG 的极细描边采用参考线宽，以改善标签显示；具体兼容策略见
[OFD 支持清单](OFD-SUPPORT.md)。

字体修复由仓库内的 [internal/fontrepair](../internal/fontrepair/README.md) 按 OpenType
与 Adobe CFF/Type 2 公开规范实现；与 PDF 文字提取共用
[Adobe-GB1 映射数据](../internal/cidmap/README.md)，保留 Adobe BSD 许可。
依赖维护与回归入口见[开发与维护索引](README.md#开发与维护)。
