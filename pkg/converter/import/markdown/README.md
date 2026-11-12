# Markdown 导入

解析链路为 **goldmark AST → `creator/flow.Document` → OFD**，不使用独立的 Markdown 排版引擎。

```go
import _ "github.com/zc310/ofd/pkg/converter/import/markdown"

err := converter.Convert(ctx, "markdown", "ofd", input, output)
```

`input` 支持路径、字节和 `io.Reader`。路径输入以文件所在目录解析相对图片；
内联 base64 PNG/JPEG 可用，远程图片不下载，无法读取的图片记录警告并跳过。
原始 HTML 不渲染，混在文字中的图片使用替代文本。

## 内容与版式

- 支持六级标题、段落、粗斜体、删除线、链接、嵌套列表、任务标记、引用、分隔线和 GFM 表格。
- 代码块使用等宽字体和浅灰圆角背景，保留缩进和内部空行；每页／栏一块背景，语言只标在首段。
  围栏标注已知语言（如 `go`、`python`、`js`、`json`、`yaml`、`sh`）时，使用 Chroma v3 的 GitHub 浅色主题进行语法高亮；
  支持语言别名（如 `golang`、`javascript`），跨行注释和字符串保持颜色。
  未标注语言、未知语言、缩进代码块及 `text`／`plaintext` 默认灰底灰字，不自动猜测语言；分词失败时也退回灰色纯文本。
  高亮仅改变文字颜色，不改变代码内容、字号、缩进或分页。行内代码有底色，片段不可拆分，不做语法高亮。
  超长代码行按面板宽度自动折行，优先在 Unicode 断点处换行；超长命令、URL 或字符串按字素折行，不裁剪原有空白。
- 表格使用自动列宽、单元格换行、跨页拆行和重复表头，不自动旋转宽表格。
- 默认 A4、Material 印刷主题，采用 flow 的标题跟随与孤行控制。图片按比例适配宽度；
  超高图片、无法放入版心的单个代码字素，或正文中不可断开的长词／URL 会返回错误，需要调整尺寸、字号或显式换行。
- 链接目标原样保留；空目标和纯 `#锚点` 只着色，不生成动作。尚未生成 Markdown 标题锚点 ID。

当前 Chroma v3 仍为预发布版本，具体依赖固定于根目录 `go.mod`；升级不改变本包公开 API。

代码块示例（高亮也适用于 Alerts 内的围栏代码）：

````markdown
```go
func main() {
    println("hello", 42)
}
```

```
未标记语言的代码默认灰底灰字。
```
````

## Alerts（GitHub 风格提示）

```markdown
> [!NOTE]
> 这里是提示正文，可以包含 **粗体**、链接、列表、表格和代码块。
```

支持 `NOTE`、`TIP`、`IMPORTANT`、`WARNING`、`CAUTION`，分别显示
Note／Tip／Important／Warning／Caution 标题及蓝／绿／紫／黄褐／红色左侧色条。
标题前分别显示信息圈、灯泡、提示气泡、警告三角形、八角停止标志。
图标采用内嵌 SVG 原创轮廓，转换为同色 OFD 矢量路径，随标题字号缩放，不依赖图标字体或网络资源。
色条中段平直，顶部和底部向右微弯，呈细括号状；连续段落与表格行之间不重复绘制弧形。
正文保留原有 Markdown 内容，不整段染色；标题尽量与正文开头同页，长提示可跨页／栏，续页保留色条。
图标仅在标题处绘制一次，不在续页重复；类型标题始终写入可提取的文字层。

标记须大写、独占引用块第一行；空提示只显示标题。未知类型、小写标记、转义标记、
行内／围栏代码中的标记及自定义同行标题仍按普通 Markdown 处理。

## 字体与自定义 flow 选项

默认显式启用 `AllowLogicalFonts`，维持中文输入无需提供字体文件的用法。
输出只声明逻辑字体，由阅读端回退；本机候选字体仅用于度量，不被嵌入。
相同环境下采用确定性 ZIP 输出，不保证不同机器的排版一致。

直接调用可传一份完整 `flow.Options`，替代默认页面、主题和字体设置：

```go
opts := flow.DefaultOptions()
opts.Theme = flow.Bootstrap5Theme()
opts.GenerateOutlines = true
err := markdown.Convert(ctx, input, output, opts)
// 按路径写出：markdown.ConvertFile(ctx, mdPath, ofdPath, opts)
```

此时沿用 flow 的严格字体检查。中文需注册实际中文字体及回退字体；代码另需覆盖内容的等宽字体。
完整配置见 [flow 字体说明](../../../creator/flow/README.md#中文与字体)。
注册导入器的 `converter.Convert` 保持默认选项；CLI 的 `-paper`、`-landscape` 不作用于 Markdown。

## 公文 front matter

开头 YAML 支持 `letterhead`、`sign`、`footer`、`colophon`，内容字段沿用现有名称。
存在版头、落款或版记时使用 flow 的 GB/T 9704 基础配置：A4、37/26/35/28mm 页边距、
三号正文及公文装饰；页码仍需 `footer.page_number: true` 显式开启。
仅设置 `footer` 时保留普通版式，使用 flow 的页码格式。

`letterhead` 支持 `org`、`doc_no`、`signatory`、`serial_no`、`security`、`urgency`、
`height`、`org_size`、`doc_no_size`；`sign` 支持 `org`、`date`、`size`；
`footer` 支持 `page_number`、`size`；`colophon` 支持 `cc`、`issued_by`、`issued_date`、`size`。
长度为毫米，字号为磅。机关标志默认 32pt；修改显式尺寸后需自行验收公文版式。
首页版头必须能同时容纳标题和正文；单行公文要素过宽会报错。
显式传入严格字体选项时，应使用 `flow.GBT9704Options` 并注册五类实际公文字体。

取消返回原始 `ctx.Err()`；读取、排版或写出失败时，不应使用部分输出。
