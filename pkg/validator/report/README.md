# 校验报告排版

`pkg/validator/report` 使用 `pkg/creator/flow` 的默认 Material 主题将 `validator.Report` 排版为 A4 OFD，
包含摘要、检查结果表格、问题明细表格、页码和章节大纲。PDF 由同一 OFD 通过
`converter.PDF` 转换，保留可搜索文字与矢量内容；中间 OFD 保存在内存中。
检查状态用颜色区分：未通过为红色，有警告或未完成为橙色，已通过为绿色，已跳过或未执行为灰色。
问题明细按「序号、级别／阶段、文件／XML 路径、问题说明」四列展示，前两列表头与正文水平居中，级别保留对应颜色。
说明列完整保留诊断信息、代码、引擎代码、依据和提示；空字段省略，原有顺序保持。
长路径自动换行，超长明细按完整文字行跨页续排，续页重复表头。

```go
import (
    "context"
    "io"

    "github.com/zc310/ofd/pkg/validator"
    validatorreport "github.com/zc310/ofd/pkg/validator/report"
)

func writeReport(ctx context.Context, w io.Writer, result validator.Report) error {
    return validatorreport.RenderOFD(ctx, w, result, validatorreport.Options{
        Font: "/path/to/cjk-font.ttf",
    })
}
```

`RenderPDF` 接收相同参数。`Build(ctx, result, options)` 返回已分页的
`*creator.Document`，可供预览或进一步处理。上下文取消通过 `errors.Is` 识别。

`Options.Font` 支持 TTF、OTF 和 TTC；TTC 自动选择包含中文的字体面并提取为独立字体。
省略时自动查找常见中文系统字体。报告沿用默认主题的字号、行距、间距和配色，并按字体族查找
实际中文粗体，供标题和表头使用。同族粗体不可用时沿用常规字体。
TrueType 与 CFF 字体均按实际使用字形子集化，CID CFF 子集保留正确的 GID/CID 对应关系。
字体需要覆盖中文报告标签；诊断中的缺字、控制字符和非法 XML 字符以 `\uXXXX` 或
`\UXXXXXXXX` 展示。过长的不可断片段按字素补充换行，避免长路径、XML 路径或连续字符串越界。
需要原始诊断字段时使用 JSON 报告。

报告包独立于校验核心，创建器及 flow 的测试可以继续使用 `pkg/validator` 严格校验产物。

```bash
go test ./pkg/validator/... ./cmd/ofd-validator
```
