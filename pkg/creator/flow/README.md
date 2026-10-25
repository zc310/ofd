# 流式创作标准 OFD

`github.com/zc310/ofd/pkg/creator/flow` 按内容顺序创建文档，自动测量、断行和分页，
返回 `creator.Document` 或直接写出符合 GB/T 33190—2016 结构的 OFD。

```go
doc := flow.New()
doc.Metadata.Title = "Quarterly report"
doc.AddHeading(1, "Quarterly report")
doc.AddParagraph("Text wraps automatically inside the page margins.")
doc.AddList(true, []string{"Prepare content", "Generate standard OFD"})
doc.AddTable(flow.Table{
    Header: []string{"Item", "Status"},
    Rows: [][]string{{"Report", "Ready"}},
})
err := doc.Write(ctx, output, creator.CreateOptions{Deterministic: true})
```

`ctx` 为非空 `context.Context`，`output` 为 `io.Writer`。完整可运行用法见
[Go 示例](example_test.go)。

## 页面、主题与单位

`DefaultOptions()` 使用 A4、四边 20mm 页边距、12pt 正文和 Material 风格主题。
内置主题均为印刷预设，各主题的字体角色和 Web 单位映射见下文。
主题包含 `Body`、六级 `Headings`、`Caption`、`Table`、`BorderColor`、`HeaderFill`。

```go
opts := flow.DefaultOptions()
opts.Theme.Body.Size = 12
opts.Theme.Body.LineHeight = 1.7
opts.Theme.Body.FirstLineIndent = 2 * 12 * 25.4 / 72 // 2em，换算为毫米
opts.Theme.Headings[0].Size = 26
opts.PageNumbers = true
opts.GenerateOutlines = true
opts.Header = "Quarterly report"
opts.Footer = "Internal"
doc := flow.New(opts)
```

页面、页边距、图片、段间距、缩进与列宽均用 **mm**；文字字号用 **pt**；行高用倍率。
Go 样式中的数值零缺省继承，可用 `TextStyle.Set` 标记显式覆盖；清单中省略字段继承，显式 `0` 覆盖。

## 主题选择

提供六个独立预设：`MaterialTheme()` 是默认的原有 Material 风格，`Material2Theme()` 按
[Material 2 官方 Web 字体角色](https://github.com/material-components/material-components-web/blob/v14.0.0/packages/mdc-typography/_typography.scss)
映射文档样式，`Material3Theme()` 按
[Material 3 官方 Web 字体角色](https://github.com/material-components/material-web/blob/main/tokens/versions/v0_192/_md-sys-typescale.scss)
映射文档样式，`Fluent2Theme()` 参考 [Fluent 2 官方 Web 字体层级](https://fluent2.microsoft.design/typography#type-ramp)。
`CarbonTheme()` 参考 [IBM Carbon 静态字体角色](https://github.com/carbon-design-system/carbon/blob/main/packages/type/scss/_styles.scss)，
`Bootstrap5Theme()` 参考 [Bootstrap 5.3 的默认字体变量](https://github.com/twbs/bootstrap/blob/v5.3.8/scss/_variables.scss)。
这五种官方角色映射均按 `96px = 72pt` 换算字号；段间距与表格配色的印刷调整见下文。
默认的 `MaterialTheme()` 是独立的报告预设，不代表 Material 2。

```go
opts := flow.DefaultOptions()
opts.Theme = flow.CarbonTheme() // 也可选择 flow.Bootstrap5Theme() 或其他预设
doc := flow.New(opts)
doc.AddHeading(1, "Delivery report")
doc.AddParagraph("The selected theme controls type sizes, line heights and spacing.")
```

| 设置 | `MaterialTheme()`，默认 | `Material2Theme()` | `Material3Theme()` | `Fluent2Theme()` |
|---|---|---|---|---|
| 正文 | 12pt，1.6 倍行高 | 12pt，1.5 倍行高（18pt） | 12pt，1.5 倍行高（18pt） | 10.5pt，20/14 倍行高（15pt） |
| 一至六级标题 | 28、22、18、16、14、12pt | 72、45、36、25.5、18、15pt | 24、21、18、16.5、12、10.5pt | 24、21、18、15、12、10.5pt |
| 标题行高 | 统一 1.3 倍 | 72、45、37.5、30、24、24pt | 30、27、24、21、18、15pt | 30、27、24、19.5、16.5、15pt |
| 内置标题字体面 | Go Bold | 前五级 Go Regular，第六级 Go Bold | 前四级 Go Regular，后两级 Go Bold | Go Bold |
| 正文段后间距 | 8pt | 8pt | 8pt | 6pt |
| 图注 | 10pt，居中 | 9pt、15pt 行高，左对齐、次要文字色 | 9pt，左对齐、次要文字色 | 9pt，左对齐、次要文字色 |
| 表格正文 | 10pt，1.5 倍行高 | 10.5pt，20/14 倍行高 | 10.5pt，20/14 倍行高 | 10.5pt，20/14 倍行高 |
| 边框与表头 | 原有 Material 风格配色 | 中性灰边框与浅灰表头 | Material 3 浅色中性色，淡紫灰表头 | 中性灰边框与浅灰表头 |

Carbon 与 Bootstrap 5 的参数如下：

| 设置 | `CarbonTheme()` | `Bootstrap5Theme()` |
|---|---|---|
| 正文 | 12pt，1.5 倍行高（18pt） | 12pt，1.5 倍行高（18pt） |
| 一至六级标题 | 31.5、24、21、15、12、10.5pt | 30、24、21、18、15、12pt |
| 标题行高 | 约 37.77、30、27、21、18、15pt | 统一 1.2 倍，实际高度仍容纳字体度量 |
| 内置标题字体面 | 前四级 Go Regular，后两级 Go Bold | Go Bold |
| 正文段后间距 | 8pt，印刷预设 | 12pt，对应默认 1rem |
| 标题段前／段后间距 | 16pt／8pt，印刷预设 | 0／6pt，对应默认 0／0.5rem |
| 图注 | 9pt，12pt 行高，左对齐 | 10.5pt，15.75pt 行高，左对齐 |
| 表格正文 | 10.5pt，13.5pt 行高 | 12pt，18pt 行高 |
| 边框与表头 | 中性灰边框与灰色表头 | 浅灰边框与 gray-100 表头，表头底色为印刷调整 |

表中的行高为主题预设；实际字体度量或上标超出该高度时，排版会扩展行框以容纳字形。

Carbon 的标题从大到小选取 `heading-06` 至 `heading-01`，正文、表格、图注分别对应
`body-02`、`body-compact-01`、`label-01`。一级标题保留源令牌的 1.199 倍行高，
其余采用角色的标称行高；Light、Semibold 分别以 Go Regular、Go Bold 近似。
使用静态角色，不根据页面宽度应用 fluid typography，未额外应用 Web tracking。

Bootstrap 5 按默认 16px 根字号换算为 12pt，六级标题对应 H1–H6，图注对应 `figure-caption` 的 87.5% 正文字号。
标题的 Medium 用 Go Bold 近似；正文与表格保留默认字号和行高。标题不应用响应式字号缩放，
次要文字色折算为白底实色；表格背景与边框采用适合纸面的浅灰预设。

Material 2 的六级标题依次对应 Headline 1–6，正文、表格、图注分别对应 Body 1、Body 2、Caption。
一级和二级标题保留较大的官方字号层级，适合封面与章节页；需要紧凑报告时可显式覆盖标题样式。
官方前两级为 Light，第六级为 Medium；内置预设分别使用 Go Regular、Go Bold 近似，
需要准确字重时注册对应字体并覆盖各级标题的 `Font`。配色为中性印刷预设，字距沿用实际字体推进量，
未额外应用 Web tracking；Subtitle、Button、Overline 等其余 UI 角色未映射到六级标题。

Material 3 的六级标题依次对应 Headline Large、Medium、Small 和 Title Large、Medium、Small，
正文、表格、图注分别对应 Body Large、Medium、Small。这个印刷预设映射文档所需的角色，
不包含 Display、Label 等其余 UI 角色，字距沿用实际字体推进量，未额外应用 Web tracking。
后两级标题的 Medium 字重使用内置 Go Bold 字体面；需要准确字重时，可注册对应字体并覆盖各级标题或块的 `Font`。

Fluent 2 的六级标题依次对应 Web 的 Title 1、Title 2、Title 3、Subtitle 1、Subtitle 2、Body 1 Strong，
图注对应 Caption 1，正文和表格对应 Body 1。实际行高仍会容纳所嵌入字体的度量和上标边界。
六种主题默认都使用内嵌 Go Regular/Bold 字体；Fluent 官方部分标题使用 Semibold，
本预设使用可用的 Go Bold 字体面。需要指定字体家族或对应字重字形时，注册字体并覆盖主题的 `Font` 字段；
中文字体配置见下文。主题选择不会自动加载 Segoe UI、Roboto、IBM Plex Sans、Bootstrap 系统字体栈或系统中文字体。

JSON/YAML 用 `flow.theme: material2`、`material3`、`fluent2`、`carbon` 或 `bootstrap5` 选择；省略或 `material` 使用原有默认主题。
`font`、`heading_font`、`body_size`、`line_height` 在主题选择之后应用，命名样式与局部 `style` 继续覆盖相应字段。

```yaml
flow:
  theme: material3
  blocks:
    - {type: heading, level: 1, text: Delivery report}
    - text: Body text uses the selected print theme.
```

该片段需与 `version`、`document` 合并。Material 2 完整示例见
[YAML](../../../cmd/ofd-creator/examples/flow-material2-report.yaml)；Material 3 完整示例见
[YAML](../../../cmd/ofd-creator/examples/flow-material3-report.yaml)；
Fluent 2 示例见 [YAML](../../../cmd/ofd-creator/examples/flow-fluent2-report.yaml)。
Carbon 示例见 [YAML](../../../cmd/ofd-creator/examples/flow-carbon-report.yaml)，Bootstrap 5 示例见
[YAML](../../../cmd/ofd-creator/examples/flow-bootstrap5-report.yaml)。
示例包含主题字号、目录、表格、左对齐图注、上标脚注与内部引用。
仓库保留 YAML 示例；JSON 使用相同字段与层级，测试时临时转换并验证两种格式。
导出 OFD 时保留已固化的字号、颜色和位置；继续编辑主题时保留原始流式清单。

## GB/T 9704-2012 公文基础版式

公文通过专用配置启用，不作为普通配色主题。`GBT9704Options` 按仓库内
[GB/T 9704—2012](../../../testdata/standards/GBT_9704-2012.pdf) 提供常规单一机关公文的基础排版：

| 要素 | 默认布局与条款 |
|---|---|
| 页面和版心 | A4，上/右/下/左 37/26/35/28mm，版心 156×225mm，§5.1、§5.2.1。 |
| 正文 | 三号 16pt、黑色、首行空两字，续行顶格；一般 22 行、28 个全角字，§5.2、§7.3.3。 |
| 标题与结构层级 | 一级标题为二号 22pt 小标宋居中；二级为三号黑体、三级为三号楷体，四级以后为三号仿宋，§7.3.1、§7.3.3。 |
| 主送机关 | `AddRecipient` / `recipient` 顶格排布；名称过长挤掉首页正文时自动移至版记，§7.3.2、§7.4.2。 |
| 版头 | 首页机关标志可见上缘距版心上缘 35mm；份号、密级、紧急程度与单个签发人，红线距发文字号下缘 4mm，§7.2。 |
| 落款 | 不加盖印章的单一机关署名和日期，正文后空一行，§7.3.5.2。 |
| 版记 | 四号 14pt 仿宋；抄送悬挂缩进、印发机关与日期，粗细线 0.35/0.25mm，末条线压准版心下边缘，§7.4。 |
| 页码 | 四号 14pt 半角宋体，两侧一字线，奇右偶左、空一字；一字线上缘距版心底边 7mm，§7.5。 |

```go
opts := flow.GBT9704Options(flow.OfficialFonts{
    Body: "FangSong", Title: "SmallTitleSong", Hei: "Hei", Kai: "Kai", Song: "Song",
})
// 将五类实际字体资源注册到 opts.Fonts；TTC 可通过 FontFaces 选择字体面。
opts.Fonts = append(opts.Fonts, creator.Font{Name: "FangSong", Data: fangSongBytes} /* 其余资源 */)
opts.Official.Letterhead = &flow.OfficialLetterhead{
    Org: "某某机关文件", DocNo: "某字〔2026〕1号", OrgSize: 32,
}
opts.Official.Signature = &flow.OfficialSignature{Org: "某某机关", Date: "2026年10月25日"}
opts.Official.Colophon = &flow.OfficialColophon{
    Cc: "相关单位。", IssuedBy: "某某机关办公室", IssuedDate: "2026年10月25日",
}
doc := flow.New(opts)
doc.AddHeading(1, "关于开展资料复核工作的通知")
doc.AddRecipient("各有关单位：")
doc.AddParagraph("现就资料复核工作有关事项通知如下。")
doc.AddHeading(2, "一、工作要求")
doc.AddParagraph("逐项核对原始资料，保留审核记录。")
```

上面的 Go 代码是需要补全五类字体字节的模型片段。字体资源名只是引用，不是字形证明；
应提供实际仿宋、小标宋、黑体、楷体及宋体。小标宋和普通宋体分别配置，不能用重命名代替字体。
`heading` 的层次序号文字由调用方提供，不自动把阿拉伯序号改为中文序号。

正文在三号纵向字号下使用轻微的标准 `HScale` 调整，使 28 个标准全角字撑满 156mm；
`DeltaX` 使用同一缩放后的实际字宽，首行预留两格。英文仍按词断行，短段末行保留自然宽度。
22 行网格按 `225/22`mm 分配；版头、标题和落款的空行单位另按 §3.2 计算。
字号、字形、词意分行、标题的梯形或菱形排列仍需要结合实际字体和标题人工检查，可用显式换行控制。

JSON/YAML 使用 `profile: gbt9704` 和 `official`，与 `theme` 互斥：

```yaml
flow:
  profile: gbt9704
  official:
    fonts: {body: FangSong, title: SmallTitleSong, hei: Hei, kai: Kai, song: Song}
    letterhead: {org: 某某机关文件, doc_no: 某字〔2026〕1号, org_size: 32}
    signature: {org: 某某机关, date: 2026年10月25日}
    colophon: {cc: 相关单位。, issued_by: 某某机关办公室, issued_date: 2026年10月25日}
  blocks:
    - {type: heading, level: 1, text: 关于开展资料复核工作的通知}
    - {type: recipient, text: 各有关单位：}
    - text: 现就有关工作通知如下。
```

片段需与 `version`、`document`、`resources.fonts` 合并。版式自动选择 A4、专用边距及公文页码；
五类字体引用必填，普通 `font`/`heading_font` 不覆盖它们。基础配置不接受多栏、其他纸张/边距、
普通页眉页脚或分节页码重置；局部特殊文字仍可用块 `style` 调整。
存在版头时首页必须实际排入正文，不能只留下版头和标题。
首页正文前的 `recipient` 太长，使正常排版无法在首页显示正文时，会自动移至版记并重新排版；
能排下的短主送保持原位。迁移按 §7.3.2、§7.4.2 使用四号仿宋、“主送：”标签、左右一字留白，
续行与冒号后首字对齐；主体末尾的全角/半角冒号替换为版记句号。多个连续主送块按原顺序排列，标签仅显示一次。
主送排在抄送上方，两者之间没有分隔线；未配置版记时也会生成仅含主送的版记。
原块的 ID、外部链接、内部链接及页码引用仍指向迁移后的最终位置，原始内容和配置不会被修改。
迁移不跨越显式换页、换栏、分节或块前后分页；含脚注的主送迁移会报错，应将脚注放入正文。
迁移后首页仍排不下正文，或版记超过整页版心时，返回明确错误供调用方调整。
开头的一个或连续多个 `section` 会在生成首页版头之前依次应用，采用最后生效的设置；
已有正文之后的分节仍从新页开始，续页不重复版头。
公文页码默认开启，Go 可设置 `Options.PageNumbers = false`，清单可设置 `flow.page_numbers: false` 关闭；
清单省略或 `null` 沿用公文默认值。`section.page_numbers` 可以覆盖后续节的显示，隐藏页仍占用连续编号。
上行文的发文字号、“签发人：”及楷体姓名使用统一基线，红线按整行可见字形下缘留出 4mm。
版记空间不足或末页有脚注时，会另起一页排版记，避免覆盖正文或脚注。

本入口覆盖常规版头、主体、不盖章落款、版记和页码，不包含联合行文、多签发人、印章自动避让、
信函/命令/纪要特定格式及横排表格的奇偶页旋转。附件可显式换页编排，其标签和标题位置需自行配置。
纸张、双面印刷、油墨、装订及最终公文内容不由 OFD 结构校验验收；`--validate` 验证的是 GB/T 33190 OFD 结构。
可运行的 [YAML](../../../cmd/ofd-creator/examples/flow-gbt9704-notice.yaml) 使用 Go 字体演示几何、分页及导出，
正式中文公文应替换为上述对应字体。

例如资源根目录为本机字体目录时，可按实际文件注册：

```yaml
resources:
  fonts:
    - {name: FangSong, file: FangSong_GB2312.ttf}
    - {name: SmallTitleSong, file: FZXiaoBiaoSong-B05S.ttf}
    - {name: Hei, file: windows/simhei.ttf}
    - {name: Kai, file: KaiTi_GB2312.ttf}
    - {name: Song, file: windows/simsun.ttc}
```

这些文件由本机提供，不随仓库分发。与前面的 `flow` 片段组合后，可通过
`--asset-root "$HOME/.local/share/fonts"` 指定资源根目录；字体路径仍受清单资源根约束。
使用这组字体的长主送迁移示例见 [YAML](../../../cmd/ofd-creator/examples/flow-gbt9704-recipients.yaml)，可在仓库根目录运行：

```bash
go run ./cmd/ofd-creator -i cmd/ofd-creator/examples/flow-gbt9704-recipients.yaml --asset-root "$HOME/.local/share/fonts" --check
go run ./cmd/ofd-creator -i cmd/ofd-creator/examples/flow-gbt9704-recipients.yaml --asset-root "$HOME/.local/share/fonts" -o /tmp/recipients.ofd --validate
```

## 命名样式与显式零值

`Options.Styles` 注册可复用的文字样式，`TextStyle.BasedOn` 引用名称。
样式先继承父样式，再应用本层字段，最后覆盖当前内容类型的主题样式。
名称区分大小写、文档内唯一，不能为空或包含空白与控制字符。
支持前向引用，最多继承 64 层；缺失名称、重复名称、循环继承及无效尺寸在分页前报错，未使用的定义也会校验。

```go
opts := flow.DefaultOptions()
opts.Styles = []flow.NamedStyle{
    {Name: "body", Style: flow.TextStyle{
        Size: 11, LineHeight: 1.5, SpaceAfter: 3, FirstLineIndent: 6,
    }},
    {Name: "compact", Style: flow.TextStyle{
        BasedOn: "body", Size: 10,
        Set: flow.StyleSpaceBefore | flow.StyleSpaceAfter | flow.StyleFirstLineIndent,
    }},
}
doc := flow.New(opts)
doc.AddParagraph("Inherited body paragraph.", flow.TextStyle{BasedOn: "body"})
doc.AddParagraph("Compact paragraph without indent or paragraph gaps.",
    flow.TextStyle{BasedOn: "compact"})
doc.AddParagraph("Local fields override the named style.",
    flow.TextStyle{BasedOn: "body", Align: "right", Set: flow.StyleFirstLineIndent})
```

Go 的非零字段自动覆盖，零值默认继承；需要显式设为零时在 `Set` 中按位或对应常量：

| 常量 | 字段 | 允许的值 |
|---|---|---|
| `StyleSpaceBefore` | `SpaceBefore` | 有限非负数，零清除段前间距 |
| `StyleSpaceAfter` | `SpaceAfter` | 有限非负数，零清除段后间距 |
| `StyleFirstLineIndent` | `FirstLineIndent` | 有限非负数，零取消首行缩进 |
| `StyleSize` | `Size` | 有限正数，显式零返回错误 |
| `StyleLineHeight` | `LineHeight` | 有限正数，显式零返回错误 |

父样式中的显式零会传递给所有派生样式；子样式可以用新的数值再次覆盖。
`Font`、`Align` 的空字符串继承，`Color: nil` 继承；要恢复黑色或左对齐，应显式设置黑色颜色对象或 `Align: "left"`。
块级 `Style`、`TableCell.Style` 和目录条目样式均支持 `BasedOn` 与 `Set`。
Go 主题的正文、各级标题、表格和图注也可引用命名样式；主题本身的字段同样在命名样式之后覆盖。
表格单元格仍先清除表格块的段间距与首行缩进，再应用单元格样式；这些块级间距作用于整张表。
列对齐先于单元格的命名样式与局部字段，因此单元格可以明确改回左对齐。

清单在 `flow.styles` 定义样式，用 `style.based_on` 引用。数值字段省略或设为 `null` 时继承，
显式 `space_before: 0`、`space_after: 0`、`first_line_indent: 0` 清除对应值，`size: 0` 或 `line_height: 0` 返回错误。
解析后的显式零值可在重新序列化为 YAML/JSON 时保留，无需额外字段标记。

```yaml
flow:
  styles:
    - name: body
      style: {size: 11, space_after: 3, first_line_indent: 6}
    - name: compact
      style: {based_on: body, space_before: 0, space_after: 0, first_line_indent: 0}
  blocks:
    - {text: A body paragraph., style: {based_on: body}}
    - {text: A compact paragraph., style: {based_on: compact}}
    - {text: Local zero indent., style: {based_on: body, first_line_indent: 0}}
```

该片段需与 `version`、`document` 合并。命名样式在目录展开、自动列宽和多轮页码回填之前解析，
最终写出标准 OFD 文字属性及位置；OFD 导出返回固定页清单。继续编辑时保留原始样式定义。
样式解析不修改调用方的主题、命名样式或内容块。
完整示例见 [YAML](../../../cmd/ofd-creator/examples/flow-styles-report.yaml)，包含多层继承、显式零值、目录、表格与内部引用。
支持左对齐、居中、右对齐和两端对齐。页眉、页脚支持 `{page}`、`{pages}`；
`PageNumbers` 会在页脚后追加当前页和总页数。对应页边距需要能容纳整行文字。

## 两端对齐

`TextStyle.Align: "justify"` 将自动换行的行铺满可用宽度。段末、显式换行（`\n`、`\r\n`、`\r`）
结束的行保持自然宽度并左对齐；只有一个不可分单词、没有可伸展边界的行也保持原宽。
四种对齐方式都会在断行和计算行宽时排除行尾空白，词间空白仍按原字体宽度保留；
添加尾空格或制表符不会额外换行、分页。空白并入相邻文字对象时，其原字体推进量仍写入字符定位。
跨页或跨栏不改变段落语义：继续排版的自动行仍两端对齐，只有实际段末不伸展。

```go
opts := flow.DefaultOptions()
opts.Theme.Body.Align = "justify"
opts.Theme.Body.FirstLineIndent = 6
doc := flow.New(opts)
doc.AddParagraph("The first line is indented. Automatically wrapped lines fill the available width, while the final line retains its natural spacing.")
doc.AddParagraph("A manually ended line.\nThe next line starts here.")
```

间距平均分配在 UAX #14 允许的断行边界：英文按词间边界，中文按可断字符边界。
不会仅因粗体、字体或链接变化而伸展词内间距，也不会拆开组合字符和脚注编号。
首行缩进、列表标记、单元格内边距及脚注编号先占位，剩余宽度再用于对齐；多栏时使用当前栏宽。
文字样式可在主题、命名样式、块、单元格、列或脚注中设置；表格 `NoWrap` 只保留显式换行，因此不伸展。
目录条目继续按其专用布局排标题与右侧页码。

清单通过 `style.align: justify` 或 `table.columns[].align: justify` 设置，例如：

```yaml
flow:
  styles:
    - {name: report, style: {align: justify, first_line_indent: 6}}
  blocks:
    - text: Automatically wrapped lines fill the available width. The final line keeps its natural spacing.
      style: {based_on: report}
```

该片段需与 `version`、`document` 合并。最终字符位置写入标准 `TextCode.DeltaX` 和文字对象边界
（GB/T 33190—2016 §11.3 表 46），链接和删除线随实际位置输出；导出后的固定页清单保留这些位置。
完整示例见 [YAML](../../../cmd/ofd-creator/examples/flow-justify-report.yaml)，包括缩进正文、手动换行、富文本链接、列表、分栏、表格与脚注。

## 分节、横向页面与重新编号

使用 `AddSection` 或 `Kind: "section"` 创建分节。新节的内容从新页面开始，
连续分节或结尾分节不会增加空白尾页。每个节可覆盖纸张、四边页边距、页眉页脚和页码。

```go
show := false
doc.AddSection(flow.Section{PageNumbers: &show})
doc.AddParagraph("Cover")

showBody, start, opening := true, 1, "Opening page"
doc.AddSection(flow.Section{
    PageNumbers: &showBody,
    StartPageNumber: &start,
    PageNumberStyle: "decimal",
    FirstPageHeader: &opening,
})
doc.AddHeading(1, "Body")

doc.AddSection(flow.Section{
    PageSize: &creator.PageSize{Width: 297, Height: 210},
    Margins: &flow.Margins{Top: 20, Right: 20, Bottom: 20, Left: 20},
})
doc.AddHeading(1, "Wide tables")
```

指针字段为 `nil` 时继承前节，可通过空字符串、`false`、零边距明确覆盖。
`PageSize` 需要同时给出宽高，下一节继续继承该尺寸；恢复纵向时再次设置。
`StartPageNumber` 是正数，省略时接续前节。`PageNumberStyle` 支持 `decimal`、
`roman`、`ROMAN`，罗马数字范围为 1–3999；`PageNumberPrefix` 可设置 `A-` 等前缀。

`FirstPageHeader`、`FirstPageFooter` 只覆盖本节首页，空字符串表示首页不显示对应内容。
`HideFirstPageNumber` 隐藏首页自动追加的页码；首页仍占用一个编号。
这三项首页特殊设置不继承到下一节。`PageNumbers: false` 会隐藏自动页码，
仍保留自定义页脚文字以及后续连续编号。

页眉页脚可使用以下占位符：

| 占位符 | 含义 |
|---|---|
| `{page}` | 当前显示编号，包含前缀与罗马数字格式。 |
| `{pages}` | 整份文档的物理总页数，包含封面和目录。 |
| `{physical_page}` | 当前物理页码，从 1 开始。 |
| `{section_page}` | 在本节内的页序，从 1 开始，不受重新编号值影响。 |
| `{section_pages}` | 本节实际页数。 |

自动页码沿用 `{page} / {pages}`。需要显示“本节第几页／本节总页数”时，关闭自动页码，
用 `Footer` 自定义，例如 `"{page} / {section_pages}"`。
分节不写入自定义 OFD 节点，所有页面通过标准 `Page.Area` 表达实际尺寸，页脚是普通文字。

## 等宽分栏与换栏

`Options.Columns` 定义初始分栏，`Section.Columns` 覆盖后续节的设置：

```go
opts := flow.DefaultOptions()
opts.Columns = flow.Columns{Count: 2, Gap: 8}
doc := flow.New(opts)
doc.AddParagraph("Text flows down the first column and continues in the next.")
doc.AddColumnBreak()
doc.AddHeading(2, "Start the next column")
doc.AddParagraph("A new topic starts at this column top.")
doc.AddSection(flow.Section{Columns: &flow.Columns{Count: 1}})
doc.AddHeading(1, "Full-width appendix")
```

`Count` 为 0 或 1 时单栏，可设置 2 至 8 栏。`Gap` 为毫米单位的有限非负栏间距，
零表示无间距；所有栏宽相同，计算为 `(版心总宽 - (Count-1)*Gap) / Count`。
间距使栏宽不大于零时返回错误。`Section.Columns: nil` 继承前节，
显式 `Columns{Count: 1}` 恢复单栏；更换纸张或页边距后，会重新计算并校验栏宽。
分节仍从新物理页开始。

内容从左栏顶部向下排，满栏后进入右侧下一栏，最后一栏满后创建新页。
`AddColumnBreak()` / `column_break` 在下一内容块之前显式换栏，最后一栏后换页，
单栏时等同换页；开头、连续或结尾的换栏不会增加空白尾页。
`AddPageBreak()`、`PageBreakBefore`、`PageBreakAfter` 始终作用于物理页，从新页第一栏开始。
标题跟随、整块同页和孤行控制在多栏时以当前栏为可用区域。

正文、列表、目录、图注和表格都使用当前栏宽。图片按当前栏宽等比适配；
固定区域的默认宽度也是当前栏宽，显式尺寸超宽时报错。
表格换栏时重复完整表头及续表题，合并行组保持在同一栏。
页眉、页脚和页码仍按整张页面排版，每页只输出一组。
默认按顺序填栏。`Columns.Balance: true`（清单 `columns.balance: true`）启用每节末页栏高平衡，
文档结束也视为节结束；2～8 栏均可使用。分节省略 `Columns` 时继承，显式配置时按新值设置。
平衡按实际文字、表格片段与脚注高度重新试排，不修改字号、行距或内容阅读顺序；
保留孤行、整体块和跟随链约束，因此受约束的末页可能仍不等高。
显式分页及分节边界保持，含显式换栏的末页整页保留原布局。
仅调整末页，不改变其他页的内容分配和物理页数；需要通栏标题或宽表格时，通过新节切换单栏。
先最多试排 24 次寻找可用栏高，再至多两轮按栏调整余量，减少末栏空白。
试排重用同一份字体解析快照，并在目录／页码回填的每轮更新最终目的地。

多栏脚注放在引用所在栏底部，单栏按页、多栏按栏去重。
同一脚注在左右栏均被引用时，两栏分别显示，编号保持不变；
点击脚注编号返回该栏第一次引用。新增脚注只占用所属栏的高度，
另一栏的可用高度保持独立。脚注不能跨栏拆分。

分栏后的目录、书签、大纲和脚注链接使用标准 `Goto/XYZ`，同时保留目标栏的左边界和内容顶部；
单栏沿用 `Goto/FitH`。显示页码仍是物理页码或所配置的节内编号，不把栏当作新页。
输出遵循 GB/T 33190—2016 §8.1 的页面坐标和 §14.2 表 54 的目的地定义，页面 `ContentBox` 保留完整物理版心。

清单使用 `flow.columns` 或 `section.columns`：

```yaml
flow:
  columns: {count: 2, gap: 8}
  blocks:
    - text: Content in the first column.
    - {type: column_break}
    - text: A topic starting in the next column.
    - {type: section, section: {columns: {count: 1}}}
    - {type: heading, level: 1, text: Full-width appendix}
```

该片段需与 `version`、`document` 合并。分栏仅在创作时决定断行、位置与分页，
OFD 导出保留固化后的图元和目的地。完整示例见
[YAML](../../../cmd/ofd-creator/examples/flow-columns-report.yaml)，包括单栏目录、双栏正文、栏底脚注、跨栏表格和单栏附录。
单元格拆分与末页平衡组合示例见 [flow-pagination-report.yaml](../../../cmd/ofd-creator/examples/flow-pagination-report.yaml)。

```go
opts := flow.DefaultOptions()
opts.Columns = flow.Columns{Count: 2, Gap: 8, Balance: true}
doc := flow.New(opts)
doc.AddParagraph("Content is balanced across the last page of this section.")
```

## 中文与字体

默认嵌入 Go Regular、Bold、Italic 和 BoldItalic，适用于拉丁文字。
中文必须提供覆盖实际内容的字体文件或字节。排版不会依赖系统字体查询。

```go
opts := flow.DefaultOptions()
opts.Fonts = append(opts.Fonts, creator.Font{
    Name: "BodyCJK",
    Source: creator.FileDataSource("/path/to/NotoSansCJKsc-Regular.otf"),
})
opts.Theme.Body.Font = "BodyCJK"
opts.Theme.Caption.Font = "BodyCJK"
opts.Theme.Table.Font = "BodyCJK"
for i := range opts.Theme.Headings {
    opts.Theme.Headings[i].Font = "BodyCJK"
}
// 提供真实粗体/斜体字体时，注册后设置对应名称。
// 留空则沿用当前块的实际字体，不合成另一种字体面。
opts.BoldFont, opts.ItalicFont, opts.BoldItalicFont = "", "", ""
doc := flow.New(opts)
doc.AddHeading(1, "工作报告")
doc.AddParagraph("正文自动换行，并在创建时完成分页。")
```

也可保留默认正文，设置 `FallbackFonts: []string{"BodyCJK"}`，按缺字情况切换实际字体。
度量和写出使用同一字体数据快照；`Source` 在排版时读取，后续不再重新读取字体文件。
`Font.Name`（如 `Flow-Regular`、`BodyCJK`）是文档内的资源别名，主题、片段和 `font_faces` 继续引用它。
排版从所选嵌入字体的 `name` 表补全实际 `FontName` 和 `FamilyName`，输出 OFD 使用真实字型名；
例如默认四种字体面输出为 Go 的常规、粗体、斜体和粗斜体名称，而不是 `Flow-*` 别名。
实际名称优先读取完整字型名，再尝试 PostScript 名；字体族优先读取排印族名，再尝试传统族名。
名称缺失或无法读取时，`FontName` 使用资源别名；显式提供的 `FontName`、`FamilyName` 优先保留。
这些名称在字体修复前读取，避免把修复器补造的名称当作原始身份；TTC/OTC 使用所选字体面的名称。
`Build` 在完成分页、脚注、公文装饰并合并元数据后，清理未引用的字体声明；
例如没有三级标题或签发人的公文不会携带只为这些要素配置的楷体，也不携带未使用的默认 Go 字体。
固定图元、模板、注解、复合图元、裁剪和底纹中的文字引用也计入保留范围，实际使用的字体再按字形子集化。
原始资源 XML 可能使用数字字体引用，此类模型保守保留字体声明。
文字写出包含明确的 `TextCode` 字符位置，依据 GB/T 33190—2016 §11.1—§11.3。
找不到字体、缺字、无效资源或无法容纳的内容均返回错误，内容错误包含 `blocks[n]`。

### 选择 TTC / OTC 字体面

`Options.FontFaces` 按已注册资源名设置从 0 开始的字体面索引。集合缺省选择第 0 面，
单字体 TTF/OTF 只接受 0。字体面会在排版前提取为独立字体，度量、字符位置和
最终嵌入均使用这份数据；后续目录迭代复用该快照，不会重新套用集合索引。

```go
opts.Fonts = append(opts.Fonts, creator.Font{
    Name: "BodySC",
    Source: creator.FileDataSource("/path/to/NotoSansCJK-Regular.ttc"),
})
opts.FontFaces = map[string]int{"BodySC": 2}
opts.Theme.Body.Font = "BodySC"
```

该索引需要按实际字体集合确认。例如常见 Noto CJK 集合的第 2 面是简体中文字体，
但不同集合的顺序可能不同。负数、越界或未注册资源名会返回错误。
输出使用标准 OFD 字体资源，TTC 索引只属于创作配置，不写自定义节点。

### 按字体族匹配粗体与斜体

`FontFamilies` 显式关联同族的实际字体资源。`Regular` 必填，其余角色可省略。
每个资源名只能属于一个字体族、一个角色；不要把同一资源名同时声明为常规和粗体。

```go
opts.Fonts = append(opts.Fonts,
    creator.Font{Name: "BodyBold", Source: creator.FileDataSource("/path/to/Body-Bold.otf")},
    creator.Font{Name: "Code", Data: codeRegularBytes, FixedWidth: true},
    creator.Font{Name: "CodeBold", Data: codeBoldBytes, FixedWidth: true},
)
opts.FontFamilies = []flow.FontFamily{
    {Regular: "BodySC", Bold: "BodyBold"},
    {Regular: "Code", Bold: "CodeBold"},
}
doc := flow.New(opts)
doc.Blocks = append(doc.Blocks, flow.Block{Runs: []flow.Run{
    {Text: "正文粗体", Bold: true},
    {Text: "Code", Font: "Code", Bold: true},
}})
```

文件路径与字节需要调用方提供。`Run.Font` 指定一个资源后，`Bold`/`Italic` 仍会在该资源
所属字体族中匹配。以粗体资源为起点再设置斜体时，会选择同族粗斜体。
没有精确组合时，按已注册粗体、斜体、原字体顺序保留可用样式；不合成粗体或斜体字形。
字体族配置负责选择与标注，实际字形样式取决于提供的字体文件。

`BoldFont`、`ItalicFont`、`BoldItalicFont` 继续作为正文的简写；正文所属的显式
`FontFamilies` 优先。简写只影响正文字体族，不会替换其他字体族。
将默认正文改成自定义字体后，默认 Go 粗体/斜体不会自动套到该字体上。

缺字时按 `FallbackFonts` 顺序尝试对应样式，再尝试该回退资源；仍缺字时可以使用
原字体族的常规面。`FixedWidth` 内容只尝试等宽回退资源，不能用比例字体替换。
同族的等宽/衬线类别跟随常规面配置。资源的 Bold、Italic、FixedWidth 等信息使用
GB/T 33190—2016 §11.1 的标准属性写出。

可直接运行的字体样式示例见 [YAML](../../../cmd/ofd-creator/examples/flow-fonts.yaml)。

## 内容与分页

| 内容 | 用法与规则 |
|---|---|
| 段落 | `AddParagraph`；使用 `Block.Runs` 可以混排字体、粗体、斜体、删除线和链接。`Text` 与 `Runs` 互斥。 |
| 标题 | `AddHeading(1..6, text)`；默认尽量与后续正文开头至少两行保持同页。 |
| 列表 | `AddList` 或 `list_item` 块；标记与正文分开，续行对齐正文起点。 |
| 图片 | `AddImage`；支持 PNG/JPEG/GIF，尺寸缺省按 96 DPI 推算；单边指定时保持比例，超宽等比缩小。图片与多行图注整块分页，超高报错。 |
| 固定区域 | `AddFixed` 或 `fixed` 块；插入原始文字、路径、图片和复合图元，按声明宽高预留空间，区域与图注整块分页。 |
| 脚注 | `Options.Footnotes` 定义，`Run.Footnote` 引用；注释与引用行或表格行组同页，页底自动预留空间，编号标记可双向跳转。 |
| 表格 | `AddTable`；支持纯文本简写、合并网格、局部样式与链接，固定或自动列宽、单元格外观、表题与续表题，续页重复完整表头。默认超高行组报错；`SplitRows` 可拆分普通及 ColSpan 单元格。 |
| 目录 | `AddTOC` 或 `toc` 块；按标题层级生成可跨页目录，条目与页码可跳转。 |
| 换页 | `AddPageBreak`、`PageBreakBefore`、`PageBreakAfter`；连续或结尾的显式换页不增加空白尾页。 |
| 换栏 | `AddColumnBreak` 或 `column_break` 块；进入下一栏，最后一栏后换页，单栏时等同换页。 |
| 分节 | `AddSection` 或 `section` 块；覆盖后续页面、页眉页脚、编号，支持横纵页面混排。 |

文字中的 `\n`、`\r\n`、`\r` 都表示手动换行。富文本按完整文字流识别换行，
即使 `\r` 与 `\n` 分处相邻 Run，或中间包含空 Run，也只换行一次；连续的独立换行仍保留空行。
这一规则同样适用于列表、表格单元格与脚注，粗体和链接仍归属于各自的文字片段。
空白行占用的高度也参与页面与栏的剩余空间判断；其后的显式换页、换栏仍然生效，
正文、图片和固定区域会按实际剩余空间继续排版。

`Block.KeepTogether` 在整块能放进完整一页时尽量同页；`KeepWithNext` 尽量与下一块开头同页。
连续的 `KeepWithNext`、默认跟随的标题及公文主送机关会形成跟随链：中间块完整计入，
末块按实际能开始排版的最少行数计入；末块设置 `KeepTogether` 且能放入整栏时计入完整高度。
脚注、表题、重复表头及首个表格行组也一起试算，剩余空间不足时将这段内容整体移到下一页或下一栏。
无法按默认 2+2 孤行约束拆开的三行短段落，会按完整三行预留。
显式换页、换栏、块前后换页或分节会截断跟随链；链条超过整栏容量时按可容纳的前缀放宽，保证排版继续。
`Orphans` 与 `Widows` 默认均为 2，分别控制跨页段落在前页和后页的最少行数。
如果整页容量也无法满足约束，会放宽孤行约束以保证内容继续排版。
超过整行宽度且无法按 Unicode 规则断开的长词或 URL 会报错，调用方可调整列宽或明确断行。

当前支持水平排列的中文和英文。复杂文字塑形、双向文字、跨页拆分单个单元格、
浮动环绕暂不支持。

## 页底脚注

通过 `Options.Footnotes` 定义注释，再用独立的 `Run{Footnote: "id"}` 在正文、列表或表格中引用：

```go
opts := flow.DefaultOptions()
opts.Footnotes = []flow.Footnote{
    {ID: "source", Text: "The source dataset is retained with this report."},
    {ID: "term", Runs: []flow.Run{
        {Text: "Delivery unit", Bold: true},
        {Text: " means one reviewed and accepted item."},
    }},
}
doc := flow.New(opts)
doc.Blocks = append(doc.Blocks, flow.Block{Runs: []flow.Run{
    {Text: "The results use the archived dataset"}, {Footnote: "source"},
    {Text: " and count accepted delivery units"}, {Footnote: "term"}, {Text: "."},
}})
```

脚注 ID 在脚注定义内唯一，不能为空或含空白、控制字符，与内容块 ID 分别管理。
注释的 `Text` 与 `Runs` 互斥；`Runs` 支持字体、粗体、斜体、删除线、外部链接及对内容块的
`Target`、`PageRef`、`CaptionRef`，不能嵌套脚注。
引用片段的 `Footnote` 与 `Text`、`Href`、`Target`、`PageRef`、`CaptionRef` 互斥。
标题中不支持脚注，图注及固定区域中的原始文字使用各自的原始图元语义。

编号按全篇逻辑内容中的首次引用顺序从 1 连续生成，分节不重置，与定义顺序无关。
正文、列表和单元格中的 `[1]` 标记采用所在文字 **75% 的字号**，基线向上偏移 **0.3em**；
断行、自动列宽与点击边界均使用实际的上标尺寸。常规行距下保持正文基线和行距稳定，
紧凑行距不足以容纳上标时仅补足必要高度，避免裁切或覆盖相邻行。
页底编号保持注释正文的字号与基线，后接带悬挂缩进的注释文字。
同栏重复引用同一脚注只排一条注释；另页或另一栏再次引用时仍用原编号，并在该栏再次显示。
单栏时按页去重。点击正文标记跳转到同栏脚注，点击页底编号返回该栏第一次上标引用的实际位置。

默认脚注使用正文字体、9pt、1.3 倍行高，注释之间留 1mm。
`Options.FootnoteStyle` 覆盖页底编号和注释的全局样式，`Footnote.Style` 继续局部覆盖；
正文上标始终按引用处字号和字体生成。
两者都支持 `BasedOn` 和 `Set`，长度与字号单位沿用流式样式规则。
页底在正文版心内绘制分隔线并预留空间，页眉页脚仍位于页边距中。

分页同时计算正文行和新增脚注高度，不把脚注排进已占用的正文空间。
脚注不拆到下一页：引用行与整条注释必须同页；太长或无法断开的注释返回错误。
多条脚注同时出现在一行时一起计算，孤行调整后仅保留实际留在该页的脚注。
标题跟随、整段同页、分节尺寸和最终页码回填均纳入脚注高度。

`Table.Grid` 单元格中的 `Runs` 可以引用脚注，默认完整行组及其脚注保持同页。
启用 `SplitRows` 后按实际片段中的文字行预留脚注，后段脚注不会提前占用当前栏。
重复表头中的脚注会在每个续页或续栏重新显示，同栏与其他引用合并去重；
表题、完整表头、行组和所需脚注无法同时容纳时返回可定位错误。
表格文本简写的 `Header`/`Rows` 仅包含纯文本，引用脚注请使用 `Grid`。

清单对应 `flow.footnotes`、`flow.footnote_style` 和 `runs[].footnote`：

```yaml
flow:
  footnote_style: {size: 9, line_height: 1.35, space_after: 1}
  footnotes:
    - {id: source, text: The source dataset is retained with this report.}
  blocks:
    - runs:
        - {text: The results use the archived dataset}
        - {footnote: source}
        - {text: '.'}
```

该片段需与 `version`、`document` 合并。脚注与分隔线使用标准文字和路径，双向跳转使用
GB/T 33190—2016 §14.2 的 `Goto/Dest`，不增加自定义 OFD 节点。
`export` 保留最终文字、路径和跳转；继续编辑脚注定义时应保留原始流式源文件。
完整示例见 [YAML](../../../cmd/ofd-creator/examples/flow-footnotes-report.yaml)，覆盖正文、列表、重复引用、跨页表格与脚注内页码引用。

## 合并单元格与多行表头

使用 `Table.Grid` 描述单元格，`ColSpan`、`RowSpan` 分别控制跨列和跨行。
每行的单元格自动放入从左到右的空闲位置，被合并覆盖的位置无需占位。
`Grid` 与纯文本简写 `Header`/`Rows` 互斥。

```go
doc.AddTable(flow.Table{
    HeaderRows: 2,
    ColumnWidths: []float64{40, 65, 65},
    Grid: [][]flow.TableCell{
        {{Text: "Region", RowSpan: 2}, {Text: "Performance", ColSpan: 2}},
        {{Text: "Actual"}, {Text: "Plan"}},
        {{Text: "North", RowSpan: 2}, {Text: "120"}, {Text: "150"}},
        {{Text: "135"}, {Text: "155"}},
    },
})
```

`HeaderRows` 指定网格开头在每个续页重复的行数。合并区域不能跨越表头和正文边界。
跨行单元格关联的行组整体分页；重叠的跨行关系会合并为更大的行组。
表头与任一行组无法同时放入完整一页时，创建返回错误。
合并区域的文字按总宽度换行，所需高度分配给覆盖的行，内部不绘制网格线。

### 单元格跨页与跨栏拆分

`Table.SplitRows: true`（清单 `table.split_rows: true`）允许普通及 ColSpan 单元格在完整文字行之间续排。
默认关闭；启用后正文中的 `RowSpan > 1` 返回带网格位置的错误，表头内部的 RowSpan 仍可使用。
一行中各列按可用空间排入不同数量的文字行，短列排完后保留空背景，不重复文字。
每个片段独立绘制四边边框、内边距及单元格的上下段间距，垂直对齐作用于片段内部。
跨栏／跨页重复完整表头，按 `RepeatCaption` 重复续表题；表格编号与目录仍只有一个逻辑条目，
表格 ID 定位首次表题／表头起点，引用与脚注按最终片段的位置生成。
表题、表头及脚注过高，导致连一个合法片段也无法放入整栏时，仍返回明确错误。

```yaml
flow:
  columns: {count: 2, gap: 8, balance: true}
  blocks:
    - id: details
      type: table
      table:
        split_rows: true
        repeat_caption: true
        caption: Details
        header: [Item, Description]
        rows:
          - [A01, "First line\nSecond line\nFurther details"]
```

片段需与 `version`、`document` 合并。Go API 对应 `flow.Table{SplitRows: true}`，
JSON 使用相同层级的 `"split_rows": true` 与 `"columns": {"count": 2, "gap": 8, "balance": true}`。
完整示例见 [flow-pagination-report.yaml](../../../cmd/ofd-creator/examples/flow-pagination-report.yaml)。

`TableCell.Runs` 支持字体、粗体、斜体、删除线和链接，与 `Text` 互斥；
`Style` 覆盖字号、行高、文字颜色、对齐和首行缩进。单元格样式中的段前/后间距
位于文字的上下方；表格块的段前/后间距仍作用于整张表。
零跨度表示 1，负数、越界、重叠与未覆盖的网格位置会返回可定位的错误。
没有显式列宽时按网格首行的跨列总数确定列数，再均分版心。

## 自动列宽与单元格外观

`Table.Columns` 每项对应一列，启用按实际内容测量的列宽分配，与 `ColumnWidths` 互斥。
两者均省略时仍按首行推导列数并均分版心。

```go
doc.AddTable(flow.Table{
    Columns: []flow.TableColumn{
        {Width: 18},
        {MinWidth: 40, MaxWidth: 100, Weight: 3},
        {MinWidth: 25, MaxWidth: 35, NoWrap: true, Align: "right"},
    },
    Header: []string{"ID", "Description", "Amount"},
    Rows: [][]string{{"A01", "A longer description that can wrap.", "12,345.67"}},
    StripeFill: &creator.Color{R: 242, G: 247, B: 253},
    CellStyle: &flow.CellStyle{
        Padding: &flow.Margins{Top: 2, Right: 2, Bottom: 2, Left: 2},
        VerticalAlign: "middle",
    },
})
```

宽度包含左右内边距。`Width` 为正时固定；否则先满足 `MinWidth` 和不可断内容需要的宽度，
再向不自动换行的理想宽度增长。剩余空间按 `Weight` 分配（默认 1），达到 `MaxWidth` 的列停止增长。
所有列均达到上限时，允许表格窄于版心。最小总宽超出版心、固定宽或上限不能容纳内容时返回错误。
测量包含表头粗体、局部字体、回退字体、段落缩进以及跨列合并区域的约束。

`NoWrap` 禁止自动换行，保留显式 `\n`；可用于金额、日期和编号列，宽度不足时不会静默拆开。
合并单元格覆盖的任一列设置 `NoWrap` 时，该合并单元格也禁止自动换行。
普通列继续使用 UAX #14 换行，长词不能从中间强行拆开。
`Columns[].Align` 控制水平对齐；跨列单元格继承起始列设置，单元格 `Style.Align` 可以覆盖。

`Table.CellStyle` 设置全表默认外观，`TableCell.Appearance` 按字段覆盖：

- `Fill` 覆盖背景色，优先于主题表头底色和 `StripeFill`。隔行底色从正文第二行开始，跨页继续计数；跨行单元格按起始行取色。
- `Padding` 为四边内边距，缺省均为 1.5mm；显式 `&flow.Margins{}` 可设为零。
- `VerticalAlign` 可取 `top`、`middle`、`bottom`，默认 `top`，对合并单元格也有效。
- `Borders` 按 `Top`、`Right`、`Bottom`、`Left` 分别覆盖，空边继承默认 0.2mm 边框。
  `&flow.TableBorder{Width: 0}` 隐藏该侧；`Color` 为空时沿用主题边框颜色。

边框分别绘制在单元格内部，隐藏共享边时需同时关闭相邻单元格的对应两侧。
背景、边框均输出为 GB/T 33190—2016 §9.1 的标准填充路径，页面坐标遵循 §8.1；不增加 OFD 节点类型。

## 图表题注、编号与引用

图片的 `Caption` 位于下方，表格的 `Caption` 位于上方，文字使用主题的 `Caption` 样式。
设置 `Numbered: true` 后，图和表分别按全篇逻辑顺序从 1 编号；未编号内容不占号，分节不重置编号。
`CaptionPrefix` 缺省分别为 `Figure` 和 `Table`，中文可设置为 `图` 和 `表`。
即使 `Caption` 为空，仍会显示编号标签。

```go
doc.Blocks = append(doc.Blocks,
    flow.Block{Runs: []flow.Run{
        {Text: "详见"}, {CaptionRef: "schedule"},
        {Text: "，位于第"}, {PageRef: "schedule"}, {Text: "页。"},
    }},
    flow.Block{ID: "schedule", Kind: "table", Table: &flow.Table{
        Caption: "交付安排", Numbered: true, CaptionPrefix: "表",
        RepeatCaption: true, ContinuationSuffix: "（续）",
        Header: []string{"事项", "状态"},
        Rows: [][]string{{"报告", "就绪"}},
    }},
)
```

该中文片段需先注册覆盖中文的字体。`CaptionRef` 显示标签（如 `表 1`），`PageRef` 显示最终页码，
两者都可点击，并支持前向、后向引用；编号引用也可放在表格单元格中。
`CaptionRef` 的目标必须是设置 ID 且 `Numbered: true` 的图或表，
与 `Text`、`Href`、`Target`、`PageRef` 互斥；标题中不支持编号引用。
编号在分页前确定，目录或页码回填不会重复增加编号，也不会修改输入。

表格设置 `RepeatCaption: true` 后，续页显示原表题并追加 `ContinuationSuffix`，默认 `(continued)`。
表题、重复表头和第一个完整数据行组保持同页；启用 `SplitRows` 时与第一个可容纳的行片段保持同栏。
无法放进完整一栏时返回错误。
表格 ID 指向首次出现的表题起点，图片 ID 仍指向图片起点；跳转写标准 `Goto/FitH` 或多栏的 `Goto/XYZ`（§14.2）。

完整示例见 [YAML](../../../cmd/ofd-creator/examples/flow-tables-report.yaml)，包含自动列宽、多行表头、合并单元格、
隔行底色、编号图片、跨页续表题和可点击的编号/页码引用。

## 固定图元区域

`Fixed` 把已经绘制好的 creator 图元接入流式排版，适合矢量图表、二维码图片、签字栏、
印刷卡片等内容。区域内部按左上角为原点、X 向右、Y 向下的坐标排布：

```go
doc.AddFixed(flow.Fixed{
    Width: 80, Height: 30, Align: "center",
    Caption: "Approval area", Numbered: true,
    Items: []creator.Item{
        creator.Text{X: 2, Y: 2, Width: 76, Height: 7, Size: 3.5, Value: "Prepared by"},
        creator.Path{X: 2, Y: 20, Width: 76, Height: 1,
            Data: "M 0 0.5 L 76 0.5", Stroke: true, LineWidth: 0.3},
    },
})
```

`Width` 缺省为当前节的版心宽度，`Height` 必须为正数，且不含下方图注。
`Align` 可取 `left`（默认）、`center`、`right`，控制区域在版心内的位置。
区域、图注和相关间距一并计算高度，跟随标题、显式换页与分节设置；超宽或整块超高返回带块下标的错误。
内部图元按调用方提供的尺寸和文字定位输出，排版器根据声明边界检查占位。

所有内部图元的长度和 `creator.Text.Size` **均为毫米**，流式文字样式的 `TextStyle.Size` 则为磅。
原始文字的 `Font` 为空时继承块级字体，显式字体须在 `Options.Fonts` 注册。
水平文字仅使用 `Value` 简写时，按实际字体度量在声明高度内垂直居中，避免基线位于底边后裁掉下伸字形；
高度不足时报错。显式 `TextCodes` 保留原始定位，用于精确基线、字符间距或其他文字方向。
`Value` 和 `TextCodes` 中未被 `CGTransforms` 映射覆盖的字符，必须由声明的嵌入字体提供字形，
缺字时返回包含块、图元位置和字符码位的错误。固定文字不自动切换回退字体，以保持原始定位；
显式映射按 §11.4 表 48 检查字符范围和实际 GID，未映射的其余字符仍检查字体覆盖。
`Fill`、字重、文字编码、变换和动作等属性遵循原始 creator 图元的语义。

支持值类型的 `creator.Text`、`Path`、`Image`、`Composite` 和 `PageBlock`。
`PageBlock` 的子图元使用相同区域坐标，可嵌套至 64 层；区域和嵌套容器的 `Items` 不能为空。
每个叶图元须给出正数宽高，且声明边界位于区域内。
需要跨页的表格使用 `flow.Table`，图片自动适配使用 `flow.Image`。

图片可以直接提供 `creator.Image.Data`/`Source`，或通过 `ResourceID` 引用 `Metadata.Media`；
复合图元引用 `Metadata.Composites`，绘制参数和颜色空间也通过 `Metadata` 注册。
例如，调用方生成二维码 PNG 后，可将其作为区域内的 `creator.Image`，与文字和边框一起排布。
在 JSON/YAML 中直接复用 `resources.images`、`resources.composites` 和现有图元字段；
图片使用 `resource_id` 或 `data_base64`，资源文件继续受原有资源根约束。

`Caption`、`Numbered`、`CaptionPrefix` 与图片用法相同，固定区域与图片共用连续图编号，
可由 `CaptionRef` 引用。固定块的 ID 指向区域实际起点，`PageRef` 按最终分页回填。
Build 会复制并平移图元，不改变调用方的原始坐标或切片。

输出使用 GB/T 33190—2016 §7.7 表 16 的标准 `PageBlock` 与原有图元；
按 §8.1 平移页面空间中的边界，路径、局部 CTM、裁剪和动作保留各自坐标语义。
完整示例见 [YAML](../../../cmd/ofd-creator/examples/flow-fixed-report.yaml)，包含柱状图、图片卡片、签字栏、资源复用和内部跳转。

## 标题自动生成大纲

设置 `Options.GenerateOutlines = true` 后，标题按 1–6 级构成标准 OFD 大纲树。
跳级标题挂在最近的较低级标题下；没有较低级标题时成为根节点。
大纲文字来自完整标题（含 `Runs`），空白标题不生成节点。

排版完成后，大纲使用标题实际所在的页面和位置生成 `Goto/FitH` 或多栏的 `Goto/XYZ` 动作，
遵循 GB/T 33190—2016 §7.8、§14.2；表格续页和标题跟随引起的换页会反映到目标中。
自动大纲追加在 `Metadata.Outlines` 之后，调用方的元数据保持可重复使用。
默认关闭此选项。

## 内容 ID、文内链接与页码引用

为内容块设置 `ID`，即可在正文或表格的 `Run` 中引用。ID 在文档内唯一，
不含空白或控制字符，不能以保留前缀 `__flow_` 开头，也不能与元数据中的书签同名。
段落、标题、列表、图片、表格、固定区域和目录都可以设置 ID；换页、换栏和分节块不能设置。
ID 会生成标准 OFD 书签，位置取内容经过分页与跟随约束后的实际起点。

```go
doc.Blocks = append(doc.Blocks,
    flow.Block{Runs: []flow.Run{
        {Text: "See "},
        {Text: "statistics", Target: "statistics"},
        {Text: " on page "},
        {PageRef: "statistics"},
    }},
    flow.Block{ID: "statistics", Kind: "table", Table: &flow.Table{
        Header: []string{"Item", "Result"},
        Rows: [][]string{{"Report", "Ready"}},
    }},
)
```

`Target` 与外部链接 `Href` 互斥。`PageRef` 是独立文字片段，自动填入目标的
最终页码，与 `Text`、`Href`、`Target` 互斥。`Options.ReferencePageNumbers` 缺省为
`physical`，表示从 1 开始、包含封面和目录的物理页码；设为 `section` 后显示目标所在节的
编号，如 `i`、`1`、`A-1`。目录使用相同的编号模式。隐藏页脚页码不会移除目标的编号。
两者均支持前向、后向引用，并写出标准 `Goto/FitH` 或多栏的 `Goto/XYZ` 跳转。
表格跨页时，其 ID 指向表格的首次出现；图片 ID 指向图片起点。
标题不支持嵌入 `PageRef`，页码引用放在正文或表格内。

## 自动文内目录

```go
doc.AddTOC(flow.TOC{Title: "Contents", MaxLevel: 3})
doc.AddPageBreak()
doc.Blocks = append(doc.Blocks, flow.Block{
    ID: "overview", Kind: "heading", Level: 1, Text: "Overview",
})
```

目录包含全篇不超过 `MaxLevel` 的非空标题，缺省上限为 3。
不指定 ID 的标题会获得内部书签。目录标题不进入目录或自动大纲。
`AddTOC()` 默认标题为 `Contents`；显式 `TOC{Title: ""}` 可省略目录标题。
目录块的 `Style` 控制条目样式，标题层级映射为缩进，页码右对齐，末行用引导线连接。
长标题自动换行、目录自动跨页，标题文字和页码均可点击。
需要目录独占页面时，在前后添加换页或设置对应的块级换页属性。

目录和页码引用会影响分页，因此引擎重复排版、回填显示编号，直到连续两轮的
目标物理页码和显示编号均一致。最多 12 轮，无法稳定时返回错误；每次 Build 的字体来源只读一次，
之后使用同一份字体字节。不存在的 ID、重复 ID、空内容锚点和空目录均报错。
跳转始终指向目标的实际物理页面和位置，重新编号不会改变跳转目标。

完整报告示例见 [YAML](../../../cmd/ofd-creator/examples/flow-navigation-report.yaml)。

## 图目录、表目录与标题引用

`AddFigureList()` 生成图目录，默认标题为 `Figures`；`AddTableList()` 生成表目录，默认标题为 `Tables`。
可以传入自定义标题，如 `AddFigureList("图目录")`，传入空字符串会省略目录标题。
底层仍使用 `toc` 块，通过 `TOC.Source` 选择 `headings`（默认）、`figures` 或 `tables`。

图目录收集 `Numbered: true` 的图片与固定图元，表目录收集 `Numbered: true` 的表格。
条目按逻辑内容顺序排列，显示完整编号题注及最终页码；续表、重复表头和跨栏片段只生成一条目录项。
没有 ID 的图表会获得内部书签，调用方的原始 ID 和题注保持不变。
需要在正文中引用目标时，应显式提供稳定的 `Block.ID`。

目录沿用条目换行、引导线、右对齐页码和多轮分页回填，支持跨页、跨栏及节内显示编号。
条目文字和页码都可点击，位置取目标实际所在的页和栏。
`MaxLevel` 只用于标题目录；图表目录设置非零 `MaxLevel`、未知 `Source` 或空目录范围会返回错误。
目录标题作为普通段落输出，不进入自动大纲或其他标题目录。

`Run.TitleRef` 显示目标章节标题或完整图表题注；`CaptionRef` 显示图表编号标签，`PageRef` 显示页码：

```go
doc := flow.New()
doc.AddTableList()
doc.Blocks = append(doc.Blocks,
    flow.Block{ID: "analysis", Kind: "heading", Level: 1, Text: "Delivery analysis"},
    flow.Block{ID: "results", Kind: "table", Table: &flow.Table{
        Caption: "Delivery results", Numbered: true,
        Header: []string{"Item", "Value"}, Rows: [][]string{{"Report", "Ready"}},
    }},
    flow.Block{Runs: []flow.Run{
        {Text: "See "}, {TitleRef: "analysis"},
        {Text: "; "}, {CaptionRef: "results"},
        {Text: " is titled "}, {TitleRef: "results"},
        {Text: " on page "}, {PageRef: "results"},
    }},
)
```

这里的引用分别显示 `Delivery analysis`、`Table 1` 和 `Table 1 Delivery results`。
编号图表的完整题注包含编号与自定义前缀，未编号图表也可通过非空题注使用 `TitleRef`。
章节标题中的富文本会转为纯文字，连续空白和换行折叠为空格，字号、字体和颜色由引用所在位置决定。
修改目标文字或编号后，下次 Build 会更新所有引用；目标图表的首次出现决定跳转位置。

`TitleRef` 可用于正文、列表、单元格和脚注，支持前向与后向引用，
与 `Text`、`Href`、`Target`、`PageRef`、`CaptionRef`、`Footnote` 互斥。
目标必须是带 ID 的非空章节标题或图表题注；正文段落、空标题、目录块不能作为标题引用目标。
标题中禁止嵌入 `TitleRef`，避免标题间产生递归依赖。
引用和目录均使用标准 `Goto/FitH` 或多栏的 `Goto/XYZ`（GB/T 33190—2016 §14.2）。

JSON/YAML 通过 `toc.source` 和 `runs[].title_ref` 配置。例如为报告添加两种目录：

```yaml
flow:
  blocks:
    - {type: toc, toc: {title: Figures, source: figures}}
    - {type: toc, toc: {title: Tables, source: tables}}
    - runs:
        - {text: "See "}
        - {title_ref: analysis}
        - {text: " and "}
        - {title_ref: results}
```

该片段需与 `version`、`document` 及实际标题、编号图表合并；`toc.title` 显式为空时不显示目录标题。
完整示例见 [YAML](../../../cmd/ofd-creator/examples/flow-catalogs-report.yaml)，包含标题目录、图目录、表目录、匿名图表与正文/单元格/脚注中的标题引用。
含隐藏封面页码、罗马数字目录、正文从 1 开始、横向表格和附录编号的示例见
[YAML](../../../cmd/ofd-creator/examples/flow-sections-report.yaml)。

## 预览、写出与编辑

`doc.Build(ctx)` 返回确定分页后的 `*creator.Document`，可用于预览或补充其他附加结构，
然后调用 `creator.CreateWithOptions`。预览和保存应复用这份模型。
`doc.Write` 为便捷入口，每次调用会重新排版。

最终文件由标准文字、路径和图片对象组成。保留创作源可继续编辑；
从 OFD 导出的清单反映已固化的页面，不恢复原来的流式结构。
格式严格校验还应结合页面显示和文字提取检查；公文版式要求需要单独验收。

流式排版与 `PageProvider` 的按页流式写出是不同能力。本入口先完成整份文档排版，
页面模型会驻留内存；`--stream` 不会把这一步变成常量内存算法。

YAML/JSON 输入见 [CLI 流式创建说明](../../../cmd/ofd-creator/README.md#流式创作)。
