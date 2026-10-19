# Go API 使用指南

安装依赖与完整的 OFD 转 PDF 入门示例见[主 README](../README.md#go-api)。可运行的
转换示例位于 [pkg/converter/examples_test.go](../pkg/converter/examples_test.go)。

## 转换与渲染

`pkg/converter` 的转换入口首参为 `context.Context`。长任务可传入带取消或超时的
上下文。`input` 支持文件路径、`*os.File` 或字节切片；写入调用方提供的文件后，
调用方应处理关闭错误。

`converter.Page(n)` 从 1 开始选择多文档体合并后的全局页码，`converter.DPI(dpi)`
设置栅格输出分辨率。库默认使用 96 DPI 和透明背景；CLI 默认使用 150 DPI 和白色背景。

| 输出 | Go 入口 | 输出特点 |
|---|---|---|
| PDF | `converter.PDF` | 保留页面尺寸，文字和矢量内容按支持范围写入 PDF 对象。 |
| PNG/JPG/TIFF/SVG/EPS/TeX | `converter.Image` 配合格式选项和 `converter.Writer` | 通过回调提供输出流；TIFF 写成单个多页文件。 |
| 纯文本 | `converter.Text` | 按页面位置聚行、用空格近似对齐列，页面间使用分页符 `\f`。 |
| Markdown | `converter.Markdown` | 识别段落与标题，转义特殊字符，有文字的页面间使用 `---`；可选表格识别。 |
| DOCX | `converter.DOCX` | 输出可编辑段落、表格和图片，版面由 Word 重新排版。 |
| 单文件 HTML | `converter.HTML` | 内嵌 PNG/JPG/SVG 与默认透明文字层，支持选择文字、查找和打印分页。 |

完整接口见 [converter API](https://pkg.go.dev/github.com/zc310/ofd/pkg/converter)。

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

| 输入 | 空白导入路径 |
|---|---|
| PDF | `github.com/zc310/ofd/pkg/converter/import/pdf` |
| Markdown | `github.com/zc310/ofd/pkg/converter/import/markdown` |
| Office | `github.com/zc310/ofd/pkg/converter/import/office` |
| HTML/MHTML | `github.com/zc310/ofd/pkg/converter/import/html` |
| XPS | `github.com/zc310/ofd/pkg/converter/import/xps` |
| 图片 | `github.com/zc310/ofd/pkg/converter/import/image` |

Markdown 会重新排版为 A4，远程图片会跳过并记录警告。Office/HTML/XPS 转 OFD 时先由
外部工具生成 PDF，再执行 PDF 导入；直接转 PDF 时使用外部工具产物。外部工具要求见
[运行依赖](BUILDING.md#运行依赖)。

PDF 导入默认实施资源预算。可通过 `converter.WithPDFImportOptions` 设置预算、同步
`OnDiagnostic` 回调和 `Strict` 模式；这些设置也作用于 Office/HTML/XPS 转 OFD 的
PDF 阶段。默认值、错误判断和示例见 [PDF 资源预算与诊断](PDF-SUPPORT.md#资源预算与诊断)。

## 接入按页阅读接口

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

打开后调用 `Close` 释放资源。长文档可用
`webreader.OpenWithOptions(data, webreader.OpenOptions{...})` 调整页面缓存；渲染 DPI
范围为 1–600，默认 96，单页还受像素预算限制。版本清单和切换由 `Versions`、
`SelectVersion` 提供，切换成功后需重新读取页面、字体和导航信息。
完整接口见 [webreader API](https://pkg.go.dev/github.com/zc310/ofd/pkg/webreader)。

## 创建、合并与签章

| 需求 | Go 入口与说明 |
|---|---|
| 创建 OFD | `pkg/creator` 的 `CreateFile`、`Create`、`Marshal` 及流式页面接口；坐标、边界和字号以毫米计，见[创建器库指南](../pkg/creator/README.md)。 |
| 打包多个文档体 | `pkg/merge.Files`、`Bytes`、`Sources` 保留文档体目录，签名处理由 `Options.Signatures` 控制。 |
| 拼页、选页和重排 | `pkg/merge.Pages` 解析并生成单文档 OFD，重映射资源与跳转引用；原签名和版本丢弃，见[合并命令与选页说明](../cmd/ofd-creator/README.md#合并多个-ofd)。 |
| 外部命令签署 | `pkg/sign.Sign` 计算引用摘要，将 `Signature.xml` 交给外部命令，写回其输出的 `SignedValue.dat`。 |
| 生成印章与签章、完整验证 | `pkg/sign.CreateSeal`、`CreateSignedValue`、`Verify`，见[签署与验证指南](../pkg/sign/README.md)。 |

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
