# ofd-creator

`ofd-creator` 根据 JSON 或 YAML manifest 创建 OFD 文件包。

在 Go 程序中直接使用文档模型和创建 API，见 [创建器库指南](../../pkg/creator/README.md)。

使用本工具创建或生成文档前，请阅读项目根目录的 [免责声明](../../DISCLAIMER.md)。生成结果应根据实际业务、法律和合规要求进行独立复核。

## 构建

```bash
go build -o ofd-creator ./cmd/ofd-creator
```

需要 Go 1.26 或更高版本。提交推送到 GitHub 后，可在仓库外安装 `main` 分支版本；也可在仓库根目录安装当前检出的源码：

```bash
# 远程 main 分支
go install github.com/zc310/ofd/cmd/ofd-creator@main
# 当前本地检出：在仓库根目录执行
go install ./cmd/ofd-creator
```

程序安装到 `GOBIN`，未设置时为 `$(go env GOPATH)/bin`；请确保该目录位于 `PATH` 中。

## 用法

```bash
ofd-creator \
  --input document.yaml \
  --output result.ofd \
  --validate
```

只检查 manifest 和资源，不写出文件：

```bash
ofd-creator --input document.yaml --check
```

资源路径默认相对于 manifest 所在目录，也可以显式指定资源根目录：

```bash
ofd-creator -i document.yaml -o result.ofd --asset-root ./assets
```

输出目录不存在时会自动创建。生成文件先写入同目录临时文件，成功后再原子替换目标文件。

包内自动生成的字体、图片等资源文件名使用短数字编号，例如 `Res/Fonts/1.ttf`，
以缩短 ZIP 条目和 XML 引用路径。字体名称、附件显示名称及显式指定的包内文件名会保留；
具体规则见 [图片与资源](../../pkg/creator/README.md#图片与资源)。

除创建外，`ofd-creator` 还提供 `export`、`export-all`、`template render`、`merge`、`split`、`replace`、`watermark`、`sign`、`remove-signatures` 和 `schema` 子命令，分别用于导出 manifest、批量导出、数据模板生成、合并 OFD、拆分 OFD、编辑条目、处理水印、独立签署、清理已有签名以及输出字段参考。详见下文各节。

## 流式创作

清单使用 `flow` 按顺序添加内容，自动断行、分页并生成标准 OFD 页面。
`flow` 与 `pages` 互斥，schema 版本仍为 `version: 1`，无需额外命令行开关。

```yaml
version: 1
document:
  id: report
  title: Quarterly report
  page_size: {name: A4}
flow:
  theme: material
  page_numbers: true
  blocks:
    - {type: heading, level: 1, text: Quarterly report}
    - text: Paragraphs wrap automatically inside the page margins.
    - {type: list_item, marker: "1.", text: Prepare content.}
    - type: table
      table:
        header: [Item, Status]
        rows: [[Report, Ready]]
```

完整示例：[flow-report.yaml](examples/flow-report.yaml)，
包含跨页表格、图注、局部粗体、链接和页码。
仓库保留 YAML 示例；JSON 输入使用相同字段与层级，测试时由 YAML 临时转换并验证两种格式。
合并单元格、多行表头和自动大纲示例见
[flow-merged-report.yaml](examples/flow-merged-report.yaml)。
封面、自动目录、内部引用、跨页表格和附录组成的完整报告见
[flow-navigation-report.yaml](examples/flow-navigation-report.yaml)。
封面隐藏页码、分节重新编号、横向表格和附录示例见
[flow-sections-report.yaml](examples/flow-sections-report.yaml)。
字体族与局部样式示例见 [flow-fonts.yaml](examples/flow-fonts.yaml)。
自动列宽、单元格外观和图表编号示例见
[flow-tables-report.yaml](examples/flow-tables-report.yaml)。
矢量图表、图片卡片与签字栏接入流式排版的示例见
[flow-fixed-report.yaml](examples/flow-fixed-report.yaml)。
命名样式、多层继承和显式零间距示例见
[flow-styles-report.yaml](examples/flow-styles-report.yaml)。
页底脚注、双向跳转和跨页表格脚注示例见
[flow-footnotes-report.yaml](examples/flow-footnotes-report.yaml)。
分节单栏/双栏切换、栏底脚注和跨栏表格示例见
[flow-columns-report.yaml](examples/flow-columns-report.yaml)。
单元格跨页／跨栏拆分、续表题、片段脚注和末页栏高平衡组合示例见
[flow-pagination-report.yaml](examples/flow-pagination-report.yaml)。
标题目录、图目录、表目录和完整标题引用示例见
[flow-catalogs-report.yaml](examples/flow-catalogs-report.yaml)。
两端对齐、首行缩进、手动换行、分栏与表格脚注示例见
[flow-justify-report.yaml](examples/flow-justify-report.yaml)。
Fluent 2 印刷主题、左对齐图注与紧凑字号示例见
[flow-fluent2-report.yaml](examples/flow-fluent2-report.yaml)。
Material 2 的 Headline 1–6 大标题、Body/Caption 角色和中性灰表格示例见
[flow-material2-report.yaml](examples/flow-material2-report.yaml)。
Material 3 的 Headline/Title/Body 角色、常规字重大标题和浅色表格示例见
[flow-material3-report.yaml](examples/flow-material3-report.yaml)。
IBM Carbon 的静态标题角色、紧凑表格和灰色表头示例见
[flow-carbon-report.yaml](examples/flow-carbon-report.yaml)；Bootstrap 5 的标题比例、段间距与图注示例见
[flow-bootstrap5-report.yaml](examples/flow-bootstrap5-report.yaml)。
中文综合报告见 [flow-chinese-report.json](examples/flow-chinese-report.json)，包含六级标题、真实粗体、编号表格、
矢量进度图、脚注、大纲与内部引用。字体资源使用 `google/NotoSansSC-Regular.ttf` 和
`google/NotoSansSC-Bold.ttf`；将其放在资源根下，或修改 `resources.fonts[].file` 后指定对应的 `--asset-root`。

```bash
go run ./cmd/ofd-creator -i cmd/ofd-creator/examples/flow-chinese-report.json --asset-root "$HOME/.local/share/fonts" --check
go run ./cmd/ofd-creator -i cmd/ofd-creator/examples/flow-chinese-report.json --asset-root "$HOME/.local/share/fonts" -o /tmp/flow-chinese-report.ofd --validate --deterministic
```

以上中文示例命令从仓库根目录运行。清单的流式字号单位为磅，固定图元中的原始文字 `size` 单位为毫米。

GB/T 9704—2012 常规公文基础版式示例见
[flow-gbt9704-notice.yaml](examples/flow-gbt9704-notice.yaml)。该可运行样例用内嵌 Go 字体验证几何；正式公文需配置对应中文字体。
使用本机五类公文字体的长主送迁移示例见 [flow-gbt9704-recipients.yaml](examples/flow-gbt9704-recipients.yaml)，
运行时指定 `--asset-root "$HOME/.local/share/fonts"`。

```bash
ofd-creator -i examples/flow-report.yaml --check
ofd-creator -i examples/flow-report.yaml -o report.ofd --validate --deterministic
```

上述路径以本工具目录为起点。从仓库根目录运行时使用
`go run ./cmd/ofd-creator -i cmd/ofd-creator/examples/flow-report.yaml ...`。

固定版式图片的 `border.line_width` 在 YAML 和 JSON 中均区分省略与显式零：
省略时使用标准默认线宽 0.353mm，`border: {line_width: 0}`（JSON 为
`"border": {"line_width": 0}`）关闭边框。导出再导入时会保留这一区别。

`flow.theme` 支持 `material`（原有默认）、`material2`、`material3`、`fluent2`、`carbon` 和 `bootstrap5`。
Material 2 预设按官方 Web 的 Headline 1–6、Body 1/2、Caption 角色映射字号，正文为 12pt、1.5 倍行高，
六级标题为 72/45/36/25.5/18/15pt，图注为 9pt、15pt 行高，表格为 10.5pt。
前两级 Light 标题使用 Go Regular 近似，第六级 Medium 使用 Go Bold 近似；准确字重可注册字体并覆盖标题 `Font`。
一级、二级字号适合封面与章节页，紧凑报告可覆盖标题样式；`material` 是独立的原有报告预设，不代表 Material 2。
Material 3 预设按官方 Web 的 Headline/Title/Body 角色映射字号，正文为 12pt、1.5 倍行高，
六级标题为 24/21/18/16.5/12/10.5pt，前四级使用常规字体面，后两级的 Medium 角色采用内置 Go Bold。
图注为 9pt 左对齐，表格为 10.5pt，配合浅色中性色与淡紫灰表头；字距沿用实际字体推进量。
Fluent 2 预设参考官方 Web 字体层级，按 `96px = 72pt` 映射字号，采用 10.5pt 正文、20/14 倍行高、
24/21/18/15/12/10.5pt 六级标题与 9pt 左对齐图注。段间距为印刷调整。
Carbon 预设采用静态 `heading-06` 至 `heading-01`，标题为 31.5/24/21/15/12/10.5pt，
正文为 12pt／18pt 行高，表格为 10.5pt／13.5pt 行高，图注为 9pt／12pt 行高。
Bootstrap 5 按默认 16px 根字号与 H1–H6 比例映射，标题为 30/24/21/18/15/12pt、1.2 倍行高，
正文及表格为 12pt／18pt 行高、正文段后 12pt，图注为 10.5pt／15.75pt 行高。
这两个预设保留固定印刷字号；Carbon 的段间距及两者的表格配色按纸面排版调整。
六种主题默认均嵌入 Go 字体，可用 `font`、`heading_font` 指定字体资源；Fluent/Carbon 的 Semibold、
Bootstrap 的 Medium 标题采用 Go Bold，Carbon 的 Light 采用 Go Regular 近似。
主题不会自动加载 Segoe UI、Roboto、IBM Plex Sans、Bootstrap 系统字体栈或系统中文字体。详细映射见
[流式主题指南](../../pkg/creator/flow/README.md#主题选择)。
`body_size`、`line_height` 与局部 `style` 可继续覆盖所选主题。输出长度统一为毫米；
`flow.body_size`、块级 `style.size` 使用磅，`line_height` 为倍率。

公文使用 `flow.profile: gbt9704` 配合 `flow.official`，与 `theme` 互斥。
配置 `official.fonts: {body: FangSong, title: SmallTitleSong, hei: Hei, kai: Kai, song: Song}`，
引用 `resources.fonts` 中实际的仿宋、小标宋、黑体、楷体和宋体资源；资源名不会替代字形检查。
该配置自动使用 A4、37/26/35/28mm 页边距、156×225mm 版心、16pt 正文、22 行与 28 全角字网格及奇右偶左的 14pt 页码。
用 `official.letterhead` 配置首页版头，`type: recipient` 添加顶格主送机关；一级标题为文件标题，二至五级对应结构层次。
`official.signature` 提供不盖章的单一机关落款，`official.colophon` 提供末页版记。日期、发文字号和层次序号由调用方提供规范文字。
基础版式不接受多栏、纸张/页边距变化、普通页眉页脚及分节页码重置；存在版头时首页必须显示正文。
正文前的长 `recipient` 挤掉首页正文时，自动移至版记并重排；短主送保持标题后原位。
版记主送使用四号仿宋，标签为“主送：”，续行与冒号后首字对齐，排在抄送上方且两者之间不加分隔线。
迁移保留 ID、链接及最终页码引用，未配置 `colophon` 时也可生成主送版记；不会覆盖显式分页边界。
迁移后首页仍无正文、版记超高或迁移主送包含脚注时会报错，需调整创作内容。
开头分节先应用设置再生成版头，连续开头分节采用最后生效的设置；正文后的分节仍另起一页。
公文默认显示页码，`flow.page_numbers: false` 可显式关闭；省略或 `null` 沿用版式默认值，
普通主题默认关闭。`section.page_numbers` 可按节覆盖，隐藏页仍占用连续编号。
发文字号、“签发人：”与楷体姓名保持同一基线，红线按整行最低可见字形留出 4mm。
版记空间不足或末页含脚注时会另起一页。联合行文、信函、命令、纪要、印章和横排表格奇偶旋转需另行编排。
完整字段、标准条款及使用范围见 [公文基础版式说明](../../pkg/creator/flow/README.md#gbt-9704-2012-公文基础版式)。
`--validate` 只验证标准 OFD 结构，不代替公文字体、文义分行及印制装订验收。

普通主题模式还可设置 `margins: {top: 20, right: 20, bottom: 20, left: 20}`，
以及 `header`、`footer`（支持 `{page}`、`{pages}` 占位符）。
`generate_outlines: true` 按标题层级自动生成大纲，跳转到最终分页后的标题位置；
自动大纲追加到 `document.outlines` 后面。
块级 `style` 支持字体、字号、段前后间距、首行缩进、对齐和颜色。
正文、列表、单元格和脚注中的 `\n`、`\r\n`、`\r` 表示手动换行。
`\r\n` 跨越相邻的 `runs` 项或空项时仍只换行一次，连续的独立换行会保留空行。
`style.align` 可取 `left`、`center`、`right`、`justify`。`justify` 仅将自动换行的行铺满可用宽度，
段末、显式换行和只有一个不可分单词的行保持自然宽度。英文在词间、中文在 UAX #14 允许的字符边界平均加间距，
词内字体或链接边界不增加间距。首行缩进、列表标记、单元格内边距与脚注编号先占位，多栏时按当前栏宽对齐。
文字最终固化为标准 `TextCode.DeltaX` 及对象位置，导出可保留；完整说明见
[`flow` 两端对齐](../../pkg/creator/flow/README.md#两端对齐)。
`flow.styles` 注册命名样式，块和单元格的 `style.based_on` 引用名称；本层字段覆盖父样式：

```yaml
flow:
  styles:
    - {name: body, style: {size: 11, line_height: 1.5, space_after: 3, first_line_indent: 6}}
    - {name: compact, style: {based_on: body, space_before: 0, space_after: 0, first_line_indent: 0}}
  blocks:
    - {text: Inherited paragraph., style: {based_on: body}}
    - {text: Compact paragraph., style: {based_on: compact}}
    - {text: Local override., style: {based_on: body, first_line_indent: 0, align: right}}
```

该片段需与 `version`、`document` 合并。数值字段省略或 `null` 时继承，显式 `0` 可以清除段前/后间距和缩进。
`size`、`line_height` 必须为正数，显式零值报错。父样式的显式零值会传递给子样式。
名称区分大小写、文档内唯一且不含空白；支持前向引用和最多 64 层继承。
未定义、重复名称、循环继承与无效尺寸均在分页前报错，未使用的定义也会检查。
样式依次按内容类型主题、命名样式的父至子、当前局部字段应用；
目录的 `style` 控制目录条目，单元格的命名样式可以覆盖列对齐。
样式最终固化为 OFD 文字属性和位置，`export` 返回固定页清单；后续编辑应保留原始清单。

`keep_with_next`、`keep_together`、`orphans`、`widows` 与显式换页属性控制分页。
连续 `keep_with_next`、标题和公文主送机关组成跟随链，连同末块正文开头、脚注、表题与首个表格行组一起试算，
空间不足时整体换页或换栏。末块 `keep_together: true` 时按完整高度预留；显式分页、换栏和分节优先，
超出整栏容量的长链会适当放宽。默认孤行约束下，三行短段落按整段预留。

`flow.columns` 配置等宽分栏，分节可通过 `section.columns` 覆盖：

```yaml
flow:
  columns: {count: 2, gap: 8}
  blocks:
    - text: Content in the first column.
    - {type: column_break}
    - text: Start the next column here.
    - {type: section, section: {columns: {count: 1}}}
    - {type: heading, level: 1, text: Full-width appendix}
```

该片段需与 `version`、`document` 合并。`count` 为 0 或 1 时单栏，支持 2 至 8 栏；
`gap` 为毫米单位的有限非负栏间距，零表示无间距。
栏宽为 `(版心总宽-(count-1)*gap)/count`，宽度不大于零时报错。
省略分节的 `columns` 继承前节，显式 `count: 1` 恢复单栏。分节始终从新物理页开始。
内容按从上到下、从左到右的栏序流动；`column_break` 换栏，最后一栏后换页，单栏时等同换页。
开头、连续和结尾的换栏不产生空白尾页；`page_break` 和块级换页属性始终进入新物理页第一栏。
图片、固定区域、目录和表格使用当前栏宽；换栏表格重复表头与续表题，合并行组保持同栏。
脚注放在所属栏底部并按栏去重，页眉页脚和页码仍按整页输出。
目录、大纲与文内链接保留目标栏的标准 XYZ 目的地，显示页码仍按物理页或节内编号。
`columns.balance: true` 可启用每节末页栏高平衡，默认关闭，支持 2～8 栏。
按文字、表格片段及脚注高度重排，保留孤行、整体块、跟随链、显式分页和分节边界；
含显式换栏的末页保留原布局。平衡不改变其他页面的内容分配与物理页数，受约束时允许栏高不同。
分节省略 `columns` 时继承，显式配置时按新值设置；通栏标题或宽表格仍通过分节切换单栏。

脚注在 `flow.footnotes` 定义，用独立的 `runs[].footnote` 引用：

```yaml
flow:
  footnote_style: {size: 9, line_height: 1.35, space_after: 1}
  footnotes:
    - {id: source, text: The source dataset is retained with this report.}
  blocks:
    - runs: [{text: The results use the archived dataset}, {footnote: source}, {text: '.'}]
```

该片段需与 `version`、`document` 合并。脚注定义的 `id` 必须唯一且不含空白，`text` 与 `runs` 互斥。
编号按首次引用顺序连续生成，分节不重置。正文显示同字号的 `[1]` 标记，页底显示分隔线和注释。
单栏同页重复引用只排一次；多栏按栏分别去重和显示。点击正文编号跳到脚注，点击脚注编号返回该栏首次引用。
正文、列表和单元格中的编号使用引用处 75% 的字号，基线上移 0.3em，断行与点击区域随实际上标尺寸计算。
正常行距下保留正文基线与行距；紧凑行距只补足容纳上标所需的高度。页底编号与注释正文保持同字号、同基线。
正文、列表和 `table.grid` 单元格均支持引用，重复表头中的脚注也随表头在续页显示。
`footnote` 与 `text`、`href`、`target`、`page_ref`、`caption_ref` 互斥；标题中不能引用脚注。
注释正文支持富文本、链接及页码/图表引用，不能嵌套脚注。
`footnote_style` 和每条脚注的 `style` 支持命名样式与显式零间距；缺省使用正文字体、9pt、1.3 倍行高。
脚注占用正文版心内的页底空间，与引用行或表格行组保持同页，超长、不可断或无法同页容纳时报错。

默认嵌入的 Go 字体用于拉丁文字。中文内容在 `resources.fonts` 注册字体，
再通过 `flow.font` 指定；标题可通过 `heading_font` 单独配置：

```yaml
resources:
  fonts:
    - name: BodyCJK
      file: fonts/NotoSansCJKsc-Regular.otf
flow:
  font: BodyCJK
  blocks:
    - {type: heading, level: 1, text: 工作报告}
    - text: 正文自动换行和分页，最终生成标准 OFD。
```

该片段需要与 `version`、`document` 合并，并在资源根目录准备真实字体。
字体和图片的 `file`/`data_base64` 互斥，路径沿用现有资源根限制。
`bold_font`、`italic_font`、`bold_italic_font` 选择已注册的实际字体面；
`fallback_fonts` 按字符覆盖依次回退。指定 `font` 后未配置的字重沿用该字体。
`font_faces` 按资源名选择 TTC/OTC 字体面的索引（从 0 开始），默认 0；单字体文件只接受 0。
`font_families` 关联同族实际字体面，例如：

```yaml
resources:
  fonts:
    - {name: BodySC, file: fonts/NotoSansCJK-Regular.ttc}
    - {name: BodySCBold, file: fonts/NotoSansCJK-Bold.ttc}
flow:
  font: BodySC
  heading_font: BodySCBold
  font_faces: {BodySC: 2, BodySCBold: 2}
  font_families:
    - {regular: BodySC, bold: BodySCBold}
  blocks:
    - runs:
        - {text: "正文与"}
        - {text: "同族粗体", bold: true}
```

该片段需与 `version`、`document` 合并，并准备对应字体；索引按实际集合顺序确认。
还可配置 `italic`、`bold_italic`。每个角色需要独立资源名，重复归属、缺少 regular、
未注册资源或无效索引都会报错。显式字体族优先于正文的 `bold_font` 等简写。
局部 `font` 选择某一资源后，字重和斜体仍在它所属的字体族内匹配；
缺字回退会尝试回退字体族的对应样式，等宽内容只使用等宽回退。
集合在排版前提取为独立字体，测量与嵌入使用相同数据。
缺字、超高图片、超高表格行和不可断文本越界会报告 `flow.blocks[n]`。

生成的 OFD 已固化分页与坐标；`export` 输出固定页清单。保留原始 YAML/JSON 以继续流式编辑。
`--stream` 仍表示资源/文件的流式写出，流式排版会先在内存中完成。
表格支持纯文本 `header`/`rows` 简写，也支持 `grid` 合并网格，二者互斥：

```yaml
flow:
  generate_outlines: true
  blocks:
    - {type: heading, level: 1, text: Regional report}
    - type: table
      table:
        header_rows: 2
        column_widths: [40, 65, 65]
        grid:
          - [{text: Region, row_span: 2}, {text: Performance, col_span: 2}]
          - [{text: Actual}, {text: Plan}]
          - [{text: North, row_span: 2}, {text: "120"}, {text: "150"}]
          - [{text: "135"}, {text: "155"}]
```

该片段需要与 `version`、`document` 合并。`grid` 每行从左到右填充空闲位置，
被合并占据的位置无需占位。跨行单元格关联的行组整体分页，续页重复所有
`header_rows` 行；合并不能跨越表头和正文边界。单元格可以使用 `runs` 和 `style`，
支持字体、字重、链接、对齐与文字颜色。超高行组、重叠和未覆盖的网格位置会报错。

`table.split_rows: true` 可让普通及 ColSpan 单元格在完整文字行之间跨页或跨栏续排，默认关闭。
各列按可用空间输出自己的文字片段，短列文字只输出一次；片段各自绘制四边边框、内边距及上下段间距，
垂直对齐作用于片段内部。每栏重复表头及配置的续表题，脚注按实际片段预留，不提前占用后段脚注空间。
启用时正文 `row_span > 1` 返回带网格位置的错误；表头内部的跨行合并仍支持。
若表题、表头、文字行与脚注连一个合法片段都无法装入整栏，仍返回明确错误。
可以与 `columns.balance: true` 组合；JSON 使用相同字段与层级，完整示例见
[flow-pagination-report.yaml](examples/flow-pagination-report.yaml)。

`table.columns` 启用按内容测量的列宽分配，与 `column_widths` 互斥。每项配置固定 `width`，
或自动列的 `min_width`、`max_width`、`weight`（剩余宽度比例，默认 1）。宽度包含内边距，单位毫米。
`no_wrap: true` 适用于金额、日期和编号，保留显式换行，宽度不足时报错；`align` 可取 left/center/right/justify。
`justify` 按扣除单元格内边距后的宽度伸展自动行，段末和显式换行不伸展；`no_wrap: true` 时保持自然字宽。
单元格的 `style.align` 可覆盖列对齐；跨列合并区域参与宽度测量。

```yaml
flow:
  blocks:
    - runs: [{text: "See "}, {caption_ref: schedule}, {text: " on page "}, {page_ref: schedule}]
    - id: schedule
      type: table
      table:
        caption: Delivery schedule
        numbered: true
        repeat_caption: true
        columns: [{width: 20}, {min_width: 45, weight: 3}, {min_width: 25, max_width: 35, no_wrap: true, align: right}]
        stripe_fill: {r: 242, g: 247, b: 253}
        cell_style:
          vertical_align: middle
          padding: {top: 2, right: 2, bottom: 2, left: 2}
          borders: {left: {width: 0}, right: {width: 0}}
        header: [ID, Description, Amount]
        rows: [[A01, A description that can wrap., '12,345.67']]
```

该片段需与 `version`、`document` 合并。`cell_style` 设置全表默认外观，`grid` 单元格的 `appearance`
按字段覆盖，支持 `fill`、四边 `padding`、`vertical_align: top|middle|bottom` 和四边 `borders`。
显式 `padding: {}` 设置零内边距；边框 `width: 0` 隐藏该侧，`color` 缺省沿用主题颜色。
相邻单元格分别在内部绘制边框，隐藏共享边时需同时关闭对应两侧。
`stripe_fill` 从正文第二行开始，跨页保持行序；显式 `fill` 优先于表头底色和隔行底色。

图片和表格均支持 `caption`、`numbered`、`caption_prefix`，分别从 1 连续编号，分节不重置。
前缀缺省分别为 Figure/Table，中文可设置为 图/表。`runs[].caption_ref` 显示目标编号标签并跳转，
与 `text`、`href`、`target`、`page_ref` 互斥，目标必须是带编号的图表；正文和单元格均可引用。
表格 `repeat_caption` 在续页表头前重复题注并添加 `continuation_suffix`（缺省 `(continued)`），
题注与表头、首个完整行组保持同页；`split_rows: true` 时与首个行片段保持同栏。
编号与页码引用均落到最终物理位置，表格 ID 指向首次表题起点。

自动目录使用 `type: toc`，通过 `toc.title` 和 `toc.max_level` 配置。
`toc.source` 选择 headings（默认标题目录）、figures（图目录）或 tables（表目录）。
图目录包含带编号的图片与固定图元，表目录包含带编号的表格；匿名图表自动获得内部书签。
图表目录显示完整编号题注和最终页码，续表或跨栏片段只收录一次，支持多页、分栏和节内编号。
`max_level` 只用于标题目录，图表目录中应省略或设为 0；空范围和未知来源会报错。

```yaml
flow:
  blocks:
    - {type: toc, toc: {title: Figures, source: figures}}
    - {type: toc, toc: {title: Tables, source: tables}}
    - runs: [{text: "See "}, {title_ref: analysis}, {text: " and "}, {title_ref: results}]
```

该片段需与 `version`、`document` 及对应的标题、编号图表合并。
`runs[].title_ref` 显示章节标题或完整图表题注，并提供跳转；
例如 `caption_ref` 显示 Table 1，`title_ref` 显示 Table 1 Delivery results。
`title_ref` 可用于正文、列表、单元格和脚注，与 `text`、`href`、`target`、`page_ref`、`caption_ref`、`footnote` 互斥。
目标必须是带 ID 的非空章节标题或题注，未编号图表的非空题注也可引用。
标题中的富文本转为纯文字，空白折叠，样式由引用处决定；标题中不能嵌入标题引用。
目录标题不会进入自动大纲，修改源标题或题注后，下次生成会更新目录与引用。

固定内容使用 `type: fixed`：

```yaml
flow:
  blocks:
    - id: signature
      type: fixed
      fixed:
        width: 80
        height: 30
        align: center
        caption: Approval area
        numbered: true
        items:
          - {type: text, x: 2, 'y': 2, width: 76, height: 7, size: 3.5, value: Prepared by}
          - {type: path, x: 2, 'y': 20, width: 76, height: 1, data: 'M 0 0.5 L 76 0.5', stroke: true, line_width: 0.3}
```

该片段需与 `version`、`document` 合并。`fixed.width` 缺省为当前节版心宽度，`height` 必须为正数，
不含下方图注；区域与图注整块分页，超宽或整块超高时报错。
`align` 为 left（默认）、center 或 right。`items` 使用相对区域左上角的坐标，
支持 text、path、image、composite、page-block，叶图元的正数边界须落在区域内。
`page-block` 可嵌套至 64 层，子图元使用同一区域坐标。
固定图元的长度及 `size` 均为毫米；空字体继承块级字体。
水平文字的 `value` 简写按字体度量在声明高度内垂直居中，显式 `text_codes` 保留原始定位。
图片、复合图元等引用现有 `resources`，变换、裁剪和动作沿用原始图元语义。
固定区域与图片共用图编号，`caption_ref` 和 `page_ref` 均支持指向固定块的 ID。

内容块的 `id` 定义稳定目标，`runs[].target` 创建内部链接，
`runs[].page_ref` 默认显示目标的最终物理页码（包含封面和目录），并提供跳转：

```yaml
flow:
  blocks:
    - {type: toc, toc: {title: Contents, max_level: 3}, page_break_after: true}
    - runs:
        - {text: "See "}
        - {text: appendix, target: appendix}
        - {text: " on page "}
        - {page_ref: appendix}
    - {id: appendix, type: heading, level: 1, text: Appendix, page_break_before: true}
```

该片段需与 `version`、`document` 合并。ID 在文档内唯一，不含空白或控制字符，
不能以 `__flow_` 开头，且不能与 `document.bookmarks` 重名。`target` 与 `href` 互斥；
`page_ref` 与 `text`、`href`、`target` 互斥。段落和网格单元格都支持这些引用。
目录默认收录三级以内的全部非空标题，可跨页、自动回填页码并创建跳转。
没有 `toc` 配置时标题为 Contents，显式 `toc: {title: ""}` 省略目录标题。
目录标题不进入目录或自动大纲。目录与页码引用最多迭代 12 轮以确定分页，
无法稳定则报错。导出清单会保留最终页面、书签与标准跳转动作。

分节使用 `type: section`，设置会持续作用于后续页面：

```yaml
flow:
  reference_page_numbers: section
  blocks:
    - {type: section, section: {page_numbers: false, header: ""}}
    - {text: Cover}
    - type: section
      section:
        page_numbers: true
        start_page_number: 1
        page_number_style: roman
    - {type: toc, toc: {title: Contents}}
    - type: section
      section:
        start_page_number: 1
        page_number_style: decimal
        header: Body
        first_page_header: Opening
        hide_first_page_number: true
    - {id: body, type: heading, level: 1, text: Body}
```

该片段需与 `version`、`document` 合并。新节从新页开始，空节不会额外生成尾页。
`section.page_size` 用毫米宽高设置横向或纵向纸张；`margins` 同时设置四边边距。
省略字段继承前节，`header: ""`、`footer: ""`、`page_numbers: false` 可以明确清除对应显示。
`start_page_number` 为正数，省略接续前节；`page_number_style` 支持 decimal、roman、ROMAN，
`page_number_prefix` 支持 `A-` 等前缀。罗马数字范围为 1–3999。
`first_page_header`、`first_page_footer` 和 `hide_first_page_number` 只影响本节，
不会继承到下一节；隐藏首页自动页码仍占用一个编号。

`reference_page_numbers` 缺省为 physical；section 模式让目录与 `page_ref` 使用节内显示编号。
两种模式的跳转均使用真实物理页面。页眉页脚支持 `{page}`（显示编号）、`{pages}`（物理总页数）、
`{physical_page}`、`{section_page}`（节内页序）、`{section_pages}`（本节页数）。
自动页码沿用 `{page} / {pages}`；自定义节内总页数时可关闭自动页码并配置
`footer: "{page} / {section_pages}"`。

复杂文字塑形、跨页拆分单个单元格和浮动环绕暂不支持。
Go API 与完整说明见 [流式创作库指南](../../pkg/creator/flow/README.md)。

## 移除与清理已有签名

`remove-signatures` 直接处理已有 OFD，无需签名器，不调整文档目录或资源路径。

```bash
# 清除所有文档体的全部签名，输出前严格校验。
ofd-creator remove-signatures -o /tmp/unsigned.ofd --validate signed.ofd

# 只移除指定签名；--id 可重复，同名 ID 在各文档体中均移除。
ofd-creator remove-signatures --id approval-2 -o /tmp/removed.ofd signed.ofd

# 清除全部签名，并清理惯用路径中的残留，允许签名列表或描述损坏。
ofd-creator remove-signatures --cleanup -o /tmp/cleaned.ofd signed.ofd
```

默认按主入口和签名列表定位并删除签名描述、签名值、独立印章及印模图片。
最后一个签名移除后，同步删除列表和 `OFD.xml` 的引用。正文与资源原字节保持不变，
保留签名共用的印章/图片及同目录的普通文件；输出 ZIP 重新打包。
按 ID 删除时，保留签名的依赖或摘要保护范围受影响会报错，需要一并移除相关签名或清除全部。
ID 不存在也报错。此检查不代替验签；无签名文档可重复清理。

`--cleanup` 只能用于全部清除，不能与 `--id` 组合。它还会删除 `Signatures/`、`Signs/`
目录内的所有文件和惯用签名文件名 `Signatures.xml`、`Signs.xml`、`Signature.xml`，
包括这些路径中的普通文件。损坏元数据导致无法定位的自定义路径残留会保留；
包外路径仍拒绝。正文里嵌入的印章图片、普通 `Stamp` 注解不在清理范围内。
加密输入及含完整性清单的输入需先单独处理。

输入使用位置参数或 `-i`，输出使用 `-o`，`-o -` 写标准输出。
禁止覆盖输入文件；处理或校验失败保留已有输出。`--deterministic` 固定 ZIP 时间，
`--validate` 启用 GM/T 0099 扩展执行严格校验。
退出码：`0` 成功，`2` 参数错误，`4` 输入、移除或保护范围检查失败，`5` 输出失败，`6` 严格校验失败。
Go API 见 [`sign.Remove`](../../pkg/sign/README.md#移除与清理已有签名)。

## 独立签署与普通签名追加

`sign` 对现有 OFD 签署，复用下文的 `--sign-*` 外部签名协议；缺省类型为 `Sign`。
`--append` 保留已有普通签名并增加新记录，首次签署也应设置该选项。

### 连续签署示例

以下示例对 `input.ofd` 连续签署两次。假设 `ofd-creator` 已加入 PATH，
演示签名器 `ofd-signer-demo` 位于当前目录；可从仓库根目录构建：

```bash
go build -o ./ofd-signer-demo ./cmd/ofd-signer-demo

# 首次签署，允许后续追加。
ofd-creator sign --append --sign-id approval-1 \
  --sign-cmd ./ofd-signer-demo --validate -o first.ofd input.ofd

# 第二次签署，依赖首次签名，并锁定最终列表。
ofd-creator sign --append --sign-id approval-2 --relative approval-1 \
  --lock-signatures --sign-cmd ./ofd-signer-demo --validate -o final.ofd first.ofd
```

`first.ofd` 包含首次签名，`final.ofd` 同时保留两次签名。
若还需要第三次或更多次签署，中间步骤省略 `--lock-signatures`，仅在最后一次设置；
每一步以此前签署的输出文件作为输入，并使用新的 `--sign-id`。

上述 demo 现场生成自签名证书，用于验证结构和数学签名；完整信任验证需要生产签名器及证据。
`--sign-id` 省略时自动分配未使用的 `sign-N`。`--relative` 指定依赖的已有签名；
不设置时为独立联签。`--lock-signatures` 把最终签名列表纳入摘要并结束追加，
最后再通过 `ofd-crypt integrity protect` 为整个包生成带时间戳的清单保护。
追加过程中尚未锁定的列表仍可修改，其完整性覆盖结论保持未完成。

追加前检查已有签名和引用摘要，并保留原文、签名值及图片的原始字节。
旧签名已经保护当前列表或需修改的 OFD.xml 时拒绝追加；原始加密包、已有完整性清单、
SES 混合签名及无效旧签名也会拒绝。未设置 `--append` 时重新签署并替换既有签名。
`--relative`、`--lock-signatures` 均要求 `--append`；追加仅支持 `--sign-type Sign`。
`--validate` 使用 GM/T 0099 扩展执行严格结构和摘要校验，完整信任验证见
[签名库指南](../../pkg/sign/README.md#完整验证)。

输入可用 `-i` 或位置参数，`-o -` 将 OFD 写标准输出，诊断写标准错误。
文件输出成功后原子替换目标，不能覆盖输入路径；失败保留已有输出。
退出码：`0` 成功，`2` 参数错误，`4` 输入或签署失败，`5` 输出失败，`6` 严格校验失败。

### 加密后签署物理包

`--physical` 为密文、映射表、密钥描述文件及保留的明文计算摘要，并签署最终入口和列表。
例如对未签署的 `input.ofd` 先加密，再用演示签名器验证物理签署流程：

```bash
ofd-crypt encrypt --keep-root --password-file password.txt -o encrypted.ofd input.ofd
ofd-creator sign --physical --sign-cmd ./ofd-signer-demo --validate \
  -o signed-encrypted.ofd encrypted.ofd
```

`--keep-root` 保留明文 OFD.xml，输入不能已有签名入口或完整性清单。
`--physical` 不组合 `--append`、`--relative`、`--lock-signatures`、引用筛选或印模选项。
本模式的 `--validate` 验证物理签名结构、数学签名和文件摘要；解密后的文档结构、引用闭包
及完整信任结论由 `ofd-crypt integrity verify` 配合解密凭据和证据检查。
完整流程在物理签署之后调用 `ofd-crypt integrity protect`，清单签名器必须提供时间戳；
演示签名器只用于本段物理签署示例。原始加密包的物化解密会破坏外签，需使用只读视图检查。

## manifest 字段参考

`schema` 子命令输出 manifest 全部字段的类型和说明，用于查证某个字段怎么写、含义是什么：

```bash
ofd-creator schema              # 输出 Markdown 到标准输出
ofd-creator schema -o schema.md # 写入文件
ofd-creator schema --format json
```

字段说明取自 `internal/manifest/manifest.go` 的文档注释，由 `go generate ./internal/manifest` 生成到 `internal/manifest/fielddocs.go`，因此不会与实现脱节。改动 manifest 的字段、类型或注释后必须重新生成，`internal/manifest` 的同步守卫测试会检查生成结果与源码是否一致。

结构体按首次到达的路径分组。manifest 的字段路径构成一棵树，而结构体构成一张图：复合对象可以嵌套复合对象，同一个结构体（例如 `Item`）会在几十条路径下出现，因此每个结构体只列一次，表中给出的路径是首次到达它的示例路径：

```markdown
## Item

描述页面、模板或复合图形中的图元。

示例路径：`templates[].items[]`。

| 字段    | 类型     | 可省略 | 说明                           |
|---------|----------|:------:|--------------------------------|
| `type`  | `string` |   是   | 图元类型。                     |
| `items` | `[]Item` |   是   | 定义复合或页面块图元的子图元。 |
```

字段的完整路径是在示例路径后追加字段名，结构体字段还要追加 `[]`，例如 `pages[].items[].fill_color.axial.segments[]`。

“可省略”列表示 manifest tag 是否带 `omitempty` 或 `omitzero`，它只描述序列化行为，不等于“必填”：`version` 带 `omitzero`，但缺省或不是 1 仍会被拒绝。

`--format json` 输出同样的内容，供其它工具消费，字段名使用 `omitempty` 而不是含义模糊的 `required`。

## 导出 OFD 配置

支持将单文档体 OFD 导出为可再次用于 `ofd-creator` 的 YAML 或 JSON manifest，并将字体、图片和其他多媒体资源复制到资源目录。默认格式为 YAML：

```bash
ofd-creator export \
  --input input.ofd \
  --output exported/document.yaml \
  --asset-root assets
```

未指定 `--format` 时，会根据输出文件扩展名自动选择格式：`.json` 使用 JSON，`.yaml`/`.yml` 使用 YAML；标准输出或没有扩展名时默认使用 YAML，其他扩展名会被拒绝。也可以显式使用 `--format json` 或 `--format yaml`：

```bash
ofd-creator export \
  --input input.ofd \
  --output exported/document.json \
  --format json

ofd-creator export \
  --input input.ofd \
  --output exported/document.yaml \
  --format yaml
```

JSON 默认使用紧凑格式；需要便于阅读时增加 `--json-indent`，使用 2 个空格缩进：

```bash
ofd-creator export -i input.ofd -o exported/document.json --json-indent
```

导出的资源路径相对于 manifest 文件所在目录。当前导出支持文档元数据、页面尺寸、模板页、页面模板引用、动作、书签、大纲、文字、路径、图片、复合图元、字体、多媒体、绘制参数和基础坐标变换；不保证原始 OFD 的字节级还原。签名、附件、扩展和版本信息不会在第一版中完整保留。

包含多个文档体时，可以使用从 `0` 开始的索引选择其中一个文档体：

```bash
ofd-creator export \
  --input input.ofd \
  --document 1 \
  --output exported/document-1.yaml
```

不指定 `--document` 时，多文档输入会被拒绝，避免将不同文档体的元数据和资源错误合并。

也可以一次性导出全部文档体为目录包：

```bash
ofd-creator export-all \
  --input input.ofd \
  --output exported/
```

目录包结构如下：

```text
exported/
  index.yaml
  documents/
    000.yaml
    001.yaml
    000-assets/
    001-assets/
```

`index.yaml`（或指定格式的 `index.json`）列出每个文档体的索引、ID、标题、manifest 路径、资源目录和页数。每个子 manifest 都可以独立执行普通创建命令；批量导出要求目标目录不存在，以避免覆盖已有文件。

## 数据模板与重复页面

基于现有导出能力制作数据模板：

```bash
ofd-creator export -i original.ofd -o invoice-template/template.yaml --asset-root assets
```

编辑导出的 JSON/YAML 清单，在需要变化的 `text`、`image` 图元上增加 `bind`，
在需要重复的页面上增加 `repeat`。固定内容继续使用原有布局和资源：

```yaml
pages:
  - repeat: invoices
    items:
      - type: text
        x: 30
        y: 40
        width: 130
        height: 10
        size: 4
        bind: {path: buyer_name, overflow: shrink, min_size: 2.8}
```

导出的图元也可能位于 `pages[].layers[].items` 或嵌套页面块中，绑定方式相同。
绑定文字会清除原有 `value`、`text_codes` 和 `cg_transforms`，重新计算字符位置。
一个字段被拆成多个原图元时，需在制作模板时删除多余旧图元。

业务数据为单个 JSON 对象，例如：

```json
{"invoices":[{"buyer_name":"Alpha & Beta"},{"buyer_name":"Other Company"}]}
```

生成与检查：

```bash
ofd-creator template render -i invoice-template/template.yaml --data data.json --check
ofd-creator template render -i invoice-template/template.yaml --data data.json -o invoices.ofd --validate --deterministic

# 简写：普通创建命令增加 --data，行为相同
ofd-creator -i invoice-template/template.yaml --data data.json -o invoices.ofd --validate
```

`--check` 展开数据并检查绑定、文字容量和资源，不写出文件；`--validate` 对生成的 OFD
执行严格校验。默认最多生成 10000 页，可通过 `--max-pages` 修改。
`--data -` 可从标准输入读取 JSON，不能同时使用 `-i -`。
输出文件不能覆盖模板或数据文件，失败保留已有输出。

字段与重复规则：

| 字段               | 说明                                                                           |
|--------------------|--------------------------------------------------------------------------------|
| `repeat`           | 根对象中的数组路径；每条记录生成一页，空数组跳过本页；固定封面/尾页省略此字段  |
| `bind.path`        | 点分隔路径，如 `buyer.name`；`$.seller` 读取公共根数据；不支持表达式和数组索引 |
| `bind.required`    | 默认 true；缺失或 null 报错，false 时移除图元                                  |
| `bind.default`     | 缺失或 null 的默认字符串，优先于 required                                      |
| `bind.overflow`    | `error`（默认）、`wrap`、`shrink`、`ellipsis`                                  |
| `bind.align`       | `left`（默认）、`center`、`right`                                              |
| `bind.min_size`    | shrink 必填的最小字号，单位毫米                                                |
| `bind.max_lines`   | wrap 的最大行数，0 表示按高度限制                                              |
| `bind.line_height` | wrap 的行高倍率，默认 1.2                                                      |

`@index`、`@number` 分别为 0 起/1 起的当前记录序号，`@page` 为最终物理页码，
`@pages` 为最终总页数；YAML 中需加引号，如 `path: '@page'`。
空字符串移除图元，0 和 false 正常显示。数字不经过 float64，金额可使用十进制字符串。
图片数据可为相对路径字符串，或 `{file, format}` / `{data_base64, format}` 对象，
`file` 与 `data_base64` 互斥；沿用模板资源根目录和路径检查。

坐标、字号、最小字号均为毫米。未指定字体的绑定文字使用内嵌 Go Regular；中文需要在
`resources.fonts` 中提供完整 TTF/OTF 字体，并设置绑定图元的 `font`。
原导出字体可能只含旧文字的字形，新增字符缺字会报错。

模板支持固定 `pages`，可按记录重复整页，也可按明细区域容量分页；不支持 `flow`。共享模板页的字段使用根数据，
逐页变化的字段放在页面自身。原签名和版本不能直接带入模板，生成后可重新签署。
页面资源共享、跳转重写和 Go API 详见 [数据模板库指南](../../pkg/creator/template/README.md)。

完整票据示例：
[template-invoice.yaml](examples/template-invoice.yaml) 与
[template-invoice-data.json](examples/template-invoice-data.json)。从仓库根目录运行：

```bash
go run ./cmd/ofd-creator template render -i cmd/ofd-creator/examples/template-invoice.yaml --data cmd/ofd-creator/examples/template-invoice-data.json --check
go run ./cmd/ofd-creator template render -i cmd/ofd-creator/examples/template-invoice.yaml --data cmd/ofd-creator/examples/template-invoice-data.json -o /tmp/template-invoices.ofd --validate --deterministic
```

该示例生成两页，展示共享底版、公共数据、每页字段、可选图片、备注换行和最终页码。

需要明细表自动续页时，设置 `pages[].paginate`：

```yaml
paginate:
  path: lines
  area: {x: 15, y: 52, width: 180, height: 65}
  size: 3.5
  row_height: 9
  columns:
    - {title: '#', width: 12, bind: {path: '@number'}}
    - {title: Description, width: 125, bind: {path: description}}
    - {title: Amount, width: 43, bind: {path: amount, align: right}}
```

`path` 从当前记录读取明细数组。每行按实际字宽换行，整行放不下时转入下一页；
重复表头，行序号跨单据页连续。列内默认 wrap，不能使用页码字段或 aggregate。
空明细保留一页，缺失明细报错；最终页数受 `--max-pages` 限制。

`pages[].variants` 可分别配置 `first`、`continuation`、`last`、`single` 的附加模板引用、
图元或图层，并通过 `detail_area` 调整明细区高度，为末页合计和签字栏预留空间。
`@detail_page` / `@detail_pages` 表示当前单据的页码和页数。

本页小计和整单合计使用文字绑定：

```yaml
bind: {path: amount, aggregate: page_sum, decimal_places: 2, align: right}
# 放在 last/single 版式中的整单合计
bind: {path: amount, aggregate: record_sum, decimal_places: 2, align: right}
```

金额精确求和后统一四舍五入，支持十进制字符串和 JSON 数字。`decimal_places` 为 0—18，默认 2；
缺失金额默认报错，`required: false` 可跳过，`default` 可提供默认值。
版式回退、容量规则和汇总细节见 [模板库指南](../../pkg/creator/template/README.md#明细表分页)。

完整四页明细示例：
[template-invoice-details.yaml](examples/template-invoice-details.yaml) 与
[template-invoice-details-data.json](examples/template-invoice-details-data.json)。
第一张单据生成首页、续页、末页，第二张单据使用单页版式：

```bash
go run ./cmd/ofd-creator template render -i cmd/ofd-creator/examples/template-invoice-details.yaml --data cmd/ofd-creator/examples/template-invoice-details-data.json --check
go run ./cmd/ofd-creator template render -i cmd/ofd-creator/examples/template-invoice-details.yaml --data cmd/ofd-creator/examples/template-invoice-details-data.json -o /tmp/template-invoice-details.ofd --validate --deterministic
```

## 合并多个 OFD

`merge` 子命令把多个 OFD 的文档体（DocBody）打包成一个多文档 OFD：

```bash
ofd-creator merge \
  --output merged.ofd \
  input1.ofd \
  input2.ofd
```

也可以用 `--input`/`-i` 重复指定输入文件：

```bash
ofd-creator merge -i input1.ofd -i input2.ofd -o merged.ofd
```

合并采用 ZIP 级方式：每个输入文档体的目录树原样复制到新的 `Doc_0`、`Doc_1` … 目录，只重写 `OFD.xml` 中的 `DocRoot`、`Signatures`、版本 `BaseLoc`；页面、资源等内部 XML 不被解析或重写。因此：

- 各文档体的字体、图片等资源 ID 保持独立作用域，不需要重映射。
- 若合并后出现重复的 `DocID`，会为重复项自动生成新的 ID。

输入文件的文档目录必须在包内，且除 `OFD.xml` 外不能存在文档目录之外的条目，否则合并会失败。

### 签名处理

`--signatures` 控制签名文件的处理方式：

```text
preserve   默认。签名文件字节保持不变。签名使用相对路径且文档目录名未变时签名完全有效；
           若签名使用包内绝对路径（如 /Doc_0/Pages/...）且文档被改名，无法同时保持引用有效
           和签名不变，此时直接报错，避免产出签名已失效的结果。
rewrite    重写签名文件中的包内绝对路径，使引用指向新目录。引用与摘要仍有效，但签名值
           （SignedValue）覆盖签名清单，重写后原签名值失效，需要重新签名。
drop       删除签名目录以及 OFD.xml 中的签名引用，产出无签名文档。
```

```bash
ofd-creator merge --signatures rewrite -o merged.ofd input1.ofd input2.ofd
ofd-creator merge --signatures drop -o merged.ofd input1.ofd input2.ofd
```

合并过程会把每个输入签名的处理结果（保留/重写/丢弃）汇总到标准错误。加 `--verify-signatures` 会在写出前解析输出文档，逐签名报告摘要匹配情况、SM2/SES 两层密码学签名结果，以及完整验签通过、失败或未完成的状态与原因：

```bash
ofd-creator merge -o merged.ofd --signatures rewrite --verify-signatures input1.ofd input2.ofd
```

摘要匹配、两层密码学签名通过仍可能伴随完整验签失败或未完成，例如算法声明
不一致、保护范围未确认或缺少证书信任及状态证据。此选项只报告结果，完整验签
失败或未完成本身不改变退出码；需要业务接受判断时，使用
[`sign.Verify`](../../pkg/sign/README.md#完整验证) 并检查 `Result.FullyValid()`。

注意 `rewrite` 会改写签名文件本身，可能使 SES 的数据摘要（对签名 XML 计算）失效，即使被引用文件摘要仍然匹配；这类签名需要重新签名。

签名清单和签名描述文件按主入口及清单中的路径定位，支持自定义文件名。
`drop` 只移除清单、签名描述、签名值和印章文件，共用目录中的普通文件会保留。
`preserve` 会核对搬运后的引用；签名保护了 `OFD.xml` 且合并将改变其字节时，返回错误，需选择 `rewrite` 后重签或 `drop`。
合并生成的主入口以及重写的签名、资源 XML 的 OFD 元素统一使用 `ofd:` 前缀。
ZIP 级合并读取的主入口和签名 XML，以及模型级合并重映射的资源 XML，使用 UTF-8 XML 1.0；DOCTYPE、自定义实体及非 UTF-8 编码会返回读取错误。

### 外部命令重签名

合并会重写资源名和路径，旧签名无法沿用。`--sign-cmd` 在合并完成后调用外部命令，为每个文档体追加一个新签名：

```bash
ofd-creator merge -o signed.ofd --pages 1 --sign-cmd ./ofd-signer --sign-id sign-1 --verify-signatures input.ofd
```

约定：

- `ofd-creator` 已为每个文档体计算被引用文件的摘要，生成最终的 `Signature.xml`（`SignedInfo`，含各 `Reference@FileRef` 与 `CheckValue`，以及 `SignedValue` 路径），通过**标准输入**传给命令；
- 命令向**标准输出**写 `SignedValue.dat` 字节（SES 结构，`DataHash` 是对输入 `Signature.xml` 字节的摘要），错误写标准错误；
- 命令按空白拆分参数，不支持引号或 shell 语法；
- 元数据通过环境变量传入：`OFD_SIGN_DOCUMENT`、`OFD_SIGN_ID`、`OFD_SIGN_PROVIDER`、`OFD_SIGN_PROVIDER_VERSION`、`OFD_SIGN_COMPANY`、`OFD_SIGN_SIGNATURE_METHOD`、`OFD_SIGN_CHECK_METHOD`、`OFD_SIGN_TIME`；
- 私钥和密码学算法由命令负责，`ofd-creator` 不接触密钥。`--sign-id` 必须是合法的 XML `xs:ID`（默认 `sign-1`）；
- `--sign-provider`、`--sign-provider-version`、`--sign-company`、`--sign-method`、`--sign-check-method` 分别设置 `SignedInfo` 的 `Provider` 名称/版本/公司、`SignatureMethod`（默认 `1.2.156.10197.1.501`）和 `References@CheckMethod`（默认 `SM3`）；这些值同时通过上面的环境变量传给命令。
- `--sign-type Seal|Sign` 缺省为 `Seal`；`Sign` 要求外部命令返回 SM2/SM3 GB/T 35275 SignedData 或 RSA/ECDSA CMS SignedData，环境变量 `OFD_SIGN_TYPE` 同步传递。`--sign-method` 使用组合签名 OID，写入前校验其与实际 CMS 一致；算法表及签名器约定见 [签名 API](../../pkg/sign/README.md#rsaecdsa-普通数字签名)。普通签名缺省保护最终 OFD.xml 和当前签名列表，文档目录范围仍可通过引用选项控制。
- `--sign-check-method` 支持 SHA256、SHA384、SHA512；普通签名还可用 SM3 及 SM3 OID，缺省仍为 SM3。文件引用摘要与 SignedData 内部摘要独立，算法名写出为大写。SHA-2/SM3 为内置 XSD 支持的扩展，原版 GB/T 33190 附录 A 枚举仅含 MD5/SHA1。
- `--sign-image PATH` 写入 GM/T 0099 附录 B 的 `Seal/ImageLoc`，并将图片纳入摘要；配合 `--sign-stamp` 或骑缝章选项指定显示位置。校验这些扩展时使用 `ofd-validator --gmt0099`。
- 重签会删除文档体已有的签名文件，包括当前布局的 `Signatures/` 和历史生产者使用的 `Signs/`（`Signatures.xml`/`Signs.xml` 及其目录），避免旧签名残留。
- `--sign-stamp` 会在 `Signature.xml` 写入 `StampAnnot`，让阅读器把 `SignedValue.dat` 里的印章图片绘制到页面上；默认放在文档体首页右下角（40mm），可用 `--sign-stamp-page` 指定页面 ID、`--sign-stamp-boundary "x y width height"`（毫米）指定位置。真实印章图片来自签名值（外部命令输出的 `SES_ESPictrueInfo`），演示签名器 `ofd-signer-demo` 会运行时渲染带「非正式印章」声明的测试章 PNG。
- `--sign-stamp-seams` 会把同一枚印章按参与页面数量切成条带，在同一文档体每页边缘写入一个 `StampAnnot`；`--sign-stamp-seam-edge left|right|top|bottom|all` 选择边缘，默认 `right`，`all` 会在每页同时覆盖四个边缘；左右边缘使用竖向条带，顶部和底部使用横向条带；`--sign-stamp-seam-pages all|odd|even|1,3,5-7|11-|-10` 选择参与页面，默认 `all`，页码从 1 开始，`11-` 表示第 11 页到末页，`-10` 表示第 1 页到第 10 页；`--sign-stamp-seam-group-pages 20` 可将参与页面按每 20 页分组，每组独立拼成一枚章；分组模式不能同时指定 `--sign-stamp-seam-pieces`。总页数不整除分组页数时，末尾不足两页的组按完整印章处理（单页无法自成一枚骑缝章，例如 5 页按 2 页分组得到 2+2+1，最后一页画整章）。未分组时，`--sign-stamp-seam-pieces` 可显式指定拆分份数，必须与参与页面数一致，省略时自动使用参与页面数。每个标注通过 `Clip` 显示不同条带，所有页面拼回完整骑缝章。定位按每页自身 `Area/PhysicalBox` 计算，缺失时回退文档公共 `PageArea`；印章默认沿对应边缘居中；`--sign-stamp-seam-x` 控制上下边缘的水平位置，`--sign-stamp-seam-y` 控制左右边缘的垂直位置，负值表示居中。`--sign-stamp-seam-size` 设置边长（默认 40mm），`--sign-stamp-seam-min-strip` 设置每页裁片最小宽度（默认 2mm，建议 4 或 8mm）；不足时直接报错。该模式与 `--sign-stamp` 互斥，并要求阅读器支持 `StampAnnot.Clip`。
- `--sign-seal <路径>` 指定独立的电子印章文件（`.esl`，可由 `ofd-seal` 生成）。提供后签名目录会同时包含 `Seal.esl`，且 `Signature.xml` 写入 `<Seal BaseLoc="Seal.esl"/>`，印章图片优先从该文件解析。

仓库提供最小示例签名器 `cmd/ofd-signer-demo`，可用来验证整条链路（它现场生成 SM2 自签名证书，签署 SES 印章与 `TBS_Sign`）：

```bash
go build -o /tmp/ofd-signer-demo ./cmd/ofd-signer-demo
go run ./cmd/ofd-creator merge -o /tmp/signed.ofd --pages 1 \
  --sign-cmd /tmp/ofd-signer-demo --sign-stamp --verify-signatures testdata/ofd-creator/hello.ofd
```

多页文档可以按 OFDRW 样例使用 `Clip` 将同一枚印章切成条带，贴到每页右边缘：

```bash
go run ./cmd/ofd-creator merge -o /tmp/seam-signed.ofd \
  --sign-cmd /tmp/ofd-signer-demo \
  --sign-stamp-seams --sign-stamp-seam-size 40 \
  --verify-signatures testdata/ofdrw/999.ofd
```

每页写入一个 `StampAnnot`，所有标注引用同一份签章数据；页面尺寸可不同，位置会按每页自身的 `Area/PhysicalBox` 计算。

指定页码列表或范围：

```bash
go run ./cmd/ofd-creator merge -o /tmp/seam-selected.ofd \
  --sign-cmd /tmp/ofd-signer-demo \
  --sign-stamp-seams \
  --sign-stamp-seam-edge right \
  --sign-stamp-seam-pages 1,3,5-7 \
  --verify-signatures testdata/ofdrw/999.ofd
```

每 20 页重新拼一枚骑缝章。40mm 印章每页条带为 2mm，达到默认最小条带宽度；如果希望更清晰，可将印章边长提高到 80mm：

```bash
go run ./cmd/ofd-creator merge -o /tmp/seam-group20.ofd \
  --sign-cmd /tmp/ofd-signer-demo \
  --sign-stamp-seams \
  --sign-stamp-seam-edge right \
  --sign-stamp-seam-pages 5- \
  --sign-stamp-seam-group-pages 20 \
  --sign-stamp-seam-size 40 \
  --verify-signatures testdata/pdf2ofd/GBT_33190-2016.ofd
```

四边同时添加条带：

```bash
go run ./cmd/ofd-creator merge -o /tmp/seam-all-edges.ofd \
  --sign-cmd /tmp/ofd-signer-demo \
  --sign-stamp-seams --sign-stamp-seam-edge all \
  --sign-stamp-seam-size 40 \
  --verify-signatures testdata/ofdrw/999.ofd
```

重签名通常配合 `--signatures drop`（先清掉旧签名）或有 `--pages` 的模型级合并使用。

`--deterministic` 同样作用于签名后的重新打包（固定 ZIP 条目时间）；但签名时间默认取当前时间，写入 `Signature.xml` 的 `SignatureDateTime` 会变化，若要完全可复现需要使用固定时间的签名命令，或直接用 `pkg/sign` 的 `Options.Date`。

`merge` 支持与创建命令相同的压缩策略和确定性选项，并可使用 `--validate` 在写出前执行严格校验：

```bash
ofd-creator merge -o merged.ofd --compression auto --deterministic --validate input1.ofd input2.ofd
```

`--output -` 可以把合并结果写入标准输出；`merge` 不支持从标准输入读取。

`--orphans` 控制文档目录之外的条目的处理方式：`error`（默认，直接失败）、`ignore`（跳过）或 `preserve`（按原路径保留到包根）。`--signatures rewrite` 重写签名路径时会向标准错误输出警告，说明哪些签名值已失效。

输入中的各文档体目录必须互不重叠；相同目录或父子嵌套目录会返回错误。

为防止恶意或异常文档造成解压放大，合并默认限制条目数 10000、单条解压 64MB、解压总计 512MB，可分别用 `--max-entries`、`--max-entry-mb`、`--max-total-mb` 调整。

合并逻辑同时以库的形式公开在 `pkg/merge`：`Files`（文件路径、输入按需读取）、`Bytes`（内存字节）、`Sources`（`Path`/`Data`/`io.ReaderAt` 三种来源）和 `Marshal`（返回完整字节）。

### 选页与重排

`--pages` 使用模型级合并，把各输入的页面解析后重新生成一个单文档 OFD，并按给出的页序输出。支持全局页序和按来源两种写法，来源序号从 `1` 开始、按 `-i`/位置参数顺序编号：

```bash
# 全局页序：按输入顺序拼接后的第 1、3-5 页
ofd-creator merge -o selected.ofd --pages 1,3-5 input1.ofd input2.ofd

# 按来源：第 1 个输入的第 2 页，再第 2 个输入的全部页面
ofd-creator merge -o selected.ofd --pages "s1:2;s2" input1.ofd input2.ofd

# 重排
ofd-creator merge -o reversed.ofd --pages 2,1 input1.ofd input2.ofd
```

模型级合并会自动重命名/重编号文档级资源（字体名、绘制参数名、图片/颜色空间/复合图元/模板 ID）并改写引用；签名和版本会丢弃，大纲、书签、动作和页面注解会保留并重写跳转页索引。因此不能与 `--signatures`、`--orphans`、`--max-*` 同时使用。输出文档元数据默认沿用首个来源，可用 `--document-id`、`--title`、`--author` 覆盖；`--workers` 控制并行解析输入的并发数（默认 4），与 `ofd-converter --workers` 命名一致。

选页保留首个来源的封面及相同的 `DocType`；不同 `DocType` 输入拒绝模型级合并。
已删除书签对应的动作也会清理。页码表达式最多展开 100000 项。

## 拆分 OFD

```bash
# 每页一个文件（省略 --every 时也是此行为）
ofd-creator split input.ofd --every 1 --output-dir parts --validate

# 每 10 页一份，最后一份允许不足 10 页
ofd-creator split input.ofd --every 10 --output-dir parts

# 分组取页，允许重排；同组不能重复，不同组可以重叠
ofd-creator split input.ofd --ranges "1-3;4-8;10" --output-dir parts

# 一个 DocBody 一份，保留原包内路径
ofd-creator split input.ofd --by-document --output-dir documents

# 多文档输入按页拆分时显式选择文档体（从 1 开始）
ofd-creator split input.ofd --document 2 --every 5 --output-dir parts
```

`--every`、`--ranges`、`--by-document` 互斥。输出默认为 `input-001.ofd`、
`input-002.ofd` 等，可用 `--prefix` 修改前缀。所有页码、分组和输出文件名在写入前检查；
已有输出文件拒绝覆盖，包括符号链接。每份写入同目录临时文件，关闭并可选严格校验后，
通过不覆盖已有路径的硬链接发布；输出文件系统需要支持硬链接。失败的临时文件会清理，
已经完成的分卷保留，错误信息给出已完成数量。标准输出逐行列出成功文件路径，
移除内容的提示和错误写入标准错误。批量输出不承诺跨文件事务。

页面拆分保留页面内容、原 ID 和路径，递归保留必需模板与资源，清理分卷外跳转、
无效书签/大纲及其他页的注解。元数据、封面、权限和 `DocType` 保留，新分卷生成新 DocID；
`--deterministic` 固定 ZIP 时间并按来源 DocID、文档体和页序生成确定性标识。
分卷的自定义元数据记录来源 DocID 和页序，便于后续追溯。
保留的二进制资源整体复制，拆分不执行字体子集化或元数据清理。

页面拆分默认移除整份附件、自定义标引、扩展、历史版本、数字签名及其印章外观，并报告。
`--keep-attachments` 可保留整份附件集；普通页面图片不受签名移除影响。
完整文档体拆分保留这些文档内容，默认 `--signatures preserve`：若签名保护了变化后的入口
或分卷外文件则失败，可用 `--signatures drop` 明确移除。页面模式默认 drop，
显式 preserve 遇到签名时失败。拆分后重签可调用 `ofd-creator sign`。

加密输入先独立解密；发现完整性清单时必须用 `--drop-integrity` 明确允许移除，之后对结果
重新签署并生成清单。`--validate` 在每份结果发布前执行严格校验；原文件的专用类型声明
不会自动证明分卷满足 OFD-A/OFD-H 要求。

默认限制：输入 512 MiB、输入/每份输出 10000 个 ZIP 条目、单条解压 64 MiB、输入/单份输出解压总量 512 MiB、
最多 10000 份、每文档体/分卷 100000 页、累计压缩输出 2048 MiB。
可通过 `--max-input-mb`、`--max-entry-mb`、`--max-total-mb`、`--max-parts`、
`--max-pages`、`--max-entries`、`--max-output-mb` 调整。压缩配置复用 `--compression` 和 `--compression-level`。
`--validate` 沿用配置的条目数、单条与总解压大小限制，输出压缩大小按累计输出上限检查。
库接口和内存使用说明见 [`pkg/split`](../../pkg/split/README.md)。

split 退出码：`0` 成功，`2` 参数/分组错误，`4` 输入解析、拆分生成或签名保护检查失败，
`5` 输出目录、文件发布或结果列表写入失败，`6` 严格校验失败。

## 替换、新增与删除包内条目

`replace` 子命令按包内路径替换、新增或删除条目，不解析页面或资源模型，其余条目原样搬运：

```bash
ofd-creator replace -i in.ofd -o out.ofd \
  --set 'Doc_0/Pages/Page_0/Content.xml=./content.xml' \
  --set 'Doc_0/Res/Image_3.png=./logo.png' \
  --add 'Doc_0/Res/extra.txt=./extra.txt' \
  --delete 'Doc_0/Thumbnail.xml'
```

- 条目路径为包内路径，不带前导斜杠，例如 `Doc_0/Pages/Page_0/Content.xml`。
- `--set` 要求目标已存在（不存在报错），`--add` 要求目标不存在（已存在报错），`--delete` 只给路径。三者均可重复；同一路径出现在多个操作中会报错。执行顺序为 `delete` → `set` → `add`。
- `NAME=FILE` 以第一个 `=` 切分，`FILE` 可以含 `=`；`FILE` 为 `-` 时从标准输入读取内容。
- 任何字节改动都会使已有签名摘要失效，因此 `--signatures` 默认 `drop`（丢弃签名目录），也可显式选择 `preserve` 或 `rewrite`；`rewrite` 与 `preserve` 等价，因为路径不会改变。
- 新内容若命中 `.xml` 条目（`--set`/`--add`），默认会先解析校验良构性（含根元素）；可用 `--no-validate` 关闭。`--validate` 则是在替换后对整体输出执行严格 OFD 校验，两者作用不同。
- `--verify-signatures` 在替换后校验输出文档的签名摘要与密码学签名：只报告结果不因摘要失效而失败，签名结构损坏等输出级错误返回资源错误退出码（与 `merge --verify-signatures` 一致）。
- 也可以像 `merge` 一样在替换后直接追加签名：`--sign-cmd` 调用外部命令为输出签名，`--sign-id`/`--sign-provider`/`--sign-provider-version`/`--sign-company`/`--sign-method`/`--sign-check-method`/`--sign-stamp`/`--sign-stamp-page`/`--sign-stamp-boundary`/`--sign-stamp-seams`/`--sign-stamp-seam-edge`/`--sign-stamp-seam-pages`/`--sign-stamp-seam-pieces`/`--sign-stamp-seam-size`/`--sign-stamp-seam-x`/`--sign-stamp-seam-y`/`--sign-seal`/`--sign-include`/`--sign-exclude`/`--sign-root` 的含义与 `merge` 的 `--sign-*` 一致。`--signatures` 默认 `drop` 会先丢弃旧签名，再按替换后的内容签署新签名。
- 支持 `--compression`、`--deterministic`、`--validate`，以及 `--max-entries`/`--max-entry-mb`/`--max-total-mb` 解压规模限制。
- `--output -` 可以把结果写入标准输出；`replace` 不支持从标准输入读取 OFD。

替换逻辑同时以库的形式公开在 `pkg/replace`：`Files`（任意输入 + `[]Operation`）和 `Paths`（`name -> 本地文件` 映射，等价于一组 `set`）。

## 添加、替换与删除水印

`watermark` 子命令在文档级或页面级操作 `Type="Watermark"` 注解，自动维护各页注解文件、`Annotations.xml` 索引与 `Document.xml` 的 `<Annotations>` 声明，无需手工指定包内路径：

```bash
# 在全部页面上添加一条文字水印，平铺铺满整页（默认布局）
ofd-creator watermark add -i in.ofd -o out.ofd --text "保密资料" --font-size 9

# 单条文字水印居中放置并降低不透明度
ofd-creator watermark add -i in.ofd -o out.ofd --text "定稿" --layout center --opacity 50

# 文字水印整体倾斜 45 度（屏幕顺时针：左边往上、右边往下），绕文本中心旋转
ofd-creator watermark add -i in.ofd -o out.ofd --text "保密资料" --rotate 45

# 右端上抬（屏幕逆时针）用负值
ofd-creator watermark add -i in.ofd -o out.ofd --text "保密资料" --rotate -45

# 图片水印：自动嵌入图片资源并平铺，PNG 支持 --opacity 烘焙半透明
ofd-creator watermark add -i in.ofd -o out.ofd --image logo.png --image-width 40 --opacity 60

# 只修改第 1 页（下标 0）上 ID 为 5 的水印，外观使用自备 XML 片段
ofd-creator watermark replace -i in.ofd -o out.ofd --page 0 --match-id 5 \
  --appearance ./watermark.xml

# 删除文档体 0 中 ID 为 6 的水印
ofd-creator watermark remove -i in.ofd -o out.ofd --document 0 --match-id 6
```

- 操作与目标：`--document` 选择文档体（默认 `-1` 全部）；`--page` 是 0 起的页面下标，可重复，省略表示全部页面；`--match-id` 仅对 `replace`/`remove` 生效，省略表示匹配全部水印注解。加水印在目标范围内每个页面各加一条。
- 外观三选一：`--text` 自动生成文字外观（可配 `--font`/`--font-size`/`--color "R G B"`/`--text-x`/`--text-y`/`--boundary "X Y Width Height"`/`--opacity`/`--layout`/`--rotate`），`--image` 生成图片水印（自动写入 `DocumentRes.xml` 的 `<MultiMedias>` 与图片条目），`--appearance` 直接读取外观 XML 片段文件；三者互斥。都不给时外观为空 `<Appearance/>`。`replace`/`remove` 会回收不再被任何页面引用、且由水印命名的图片条目；多页共享同一图片时仍被引用则不回收。
- 布局与透明度：`--layout tile`（默认，按 `--boundary` 区域平铺网格）或 `center`（区域内单条居中）；`--text-x`/`--text-y` 在平铺/居中时作为整体偏移。`--opacity` 取值 0-100（`0` 表示不设置）：文字水印写成 `Alpha` 属性；PNG 图片水印烘焙进图片 alpha 通道（`--image-width` 毫米，高度按图片像素比例推算）。
- 文字旋转：`--rotate` 以度为单位，让文字绕每个文本实例自身的中心倾斜，旋转时位置不偏移；正值屏幕顺时针（左边往上、右边往下，`\` 形），负值右端上抬（屏幕逆时针，`/` 形）。仅对 `--text` 生效。
- 字体：文字水印的 `--font` 接受文档资源中（同一文档体的 `PublicRes` 与 `DocumentRes`）的字体 ID 或名称，命中名称时会解析为对应数值 ID 再写入外观；省略 `--font` 时自动选用文档中最小 ID 的字体。显式指定且文档中不存在时直接报错，不会写出非法的默认字体引用。
- 注解属性：`--id`（0 自动分配，取 `MaxUnitID` 与页面内既有注解 ID 之后）、`--creator`、`--subtype`、`--visible`/`--print`/`--no-zoom`/`--no-rotate`、`--read-only`、`--remark`、`--parameter Name=Value`（可重复）。
- 默认门控：文档 `Permissions/Watermark=false` 时拒绝任何修改（`--skip-permissions-check` 跳过）；`ReadOnly` 缺省或为 `true` 的水印拒绝 `replace`/`remove`（`--skip-readonly-check` 跳过）。本命令写出的水印显式标记 `ReadOnly=false`，可直接再删。
- 清理：`remove` 后某页无任何注解会删除该页注解文件，索引与 `Document.xml` 的 `<Annotations>` 同步清理。
- 复用 `replace` 框架，因此 `--compression`/`--compression-level`/`--deterministic`/`--validate`/`--max-entries`/`--max-entry-mb`/`--max-total-mb`/`--signatures` 行为一致：任何修改都会使既有签名摘要失效，`--signatures` 默认 `drop`。也可以像 `replace`/`merge` 一样在修改后直接追加签名：`--sign-cmd` 调用外部命令为输出签名，`--sign-id`/`--sign-provider`/`--sign-provider-version`/`--sign-company`/`--sign-method`/`--sign-check-method`/`--sign-stamp`/`--sign-stamp-page`/`--sign-stamp-boundary`/`--sign-stamp-seams`/`--sign-stamp-seam-edge`/`--sign-stamp-seam-pages`/`--sign-stamp-seam-pieces`/`--sign-stamp-seam-size`/`--sign-stamp-seam-x`/`--sign-stamp-seam-y`/`--sign-seal`/`--sign-include`/`--sign-exclude`/`--sign-root` 的含义与 `merge`/`replace` 的 `--sign-*` 一致；`--verify-signatures` 在写出前校验输出文档的签名摘要与密码学签名。`--output -` 写标准输出，不支持从标准输入读取。
- 保留原始命名空间风格：默认 `xmlns` 文档的水印使用无前缀元素，`xmlns:ofd` 文档的新增索引/页面文件以及 `--appearance` 原始外观片段均套用 `ofd` 前缀；无既有注解索引时从 `Document.xml` 根元素继承前缀。自备外观片段无法解析时直接报错，不会静默写成空 `<Appearance/>`。
- 水印编辑逻辑以库的形式公开在 `pkg/watermark`：`Add`、`Replace`、`Remove` 三个函数接收任意输入（路径/字节/`io.Reader`/`*container.Package`）与 `Target`，外观自动分配 ID；`Watermark.Appearance` 可直接放入原始 XML 片段，或用 `watermark.TextAppearance`/`watermark.ImageAppearance` 生成平铺/居中的文字或图片外观（`TextOptions.Rotation` 指定文字旋转角度，`TextOptions.CTM` 直接透传变换矩阵、优先级更高）；`Watermark.Image` 提供图片水印的资源嵌入能力。

## OFD profile（DocType）

`OFD.xml` 根节点的 `DocType` 属性声明文档遵循的 profile，取值区分大小写：

```text
OFD     基础版式文档（GB/T 33190—2016），默认值
OFD-A   档案长期保存（GB/T 42133—2022《信息技术 OFD档案应用指南》）
OFD-H   电子病历版式文档（GB/T 48666-2026《电子病历版式文档技术要求》）
```

GB/T 33190 把该属性固定为 `OFD`，后两份标准在它之上把取值收紧为 `OFD-A` 与 `OFD-H`，因此生成这两类文件必须显式声明。`OFD-A` 与 `OFD-H` 是标准之间的叠加关系：`OFD-H` 在 `OFD-A` 的基础上追加医疗领域要求。

取值来源有两处，命令行优先：

```bash
# 命令行指定
ofd-creator -i document.yaml -o result.ofd --doc-type OFD-A

# 或在 manifest 的 document 下声明
```

```yaml
version: 1
document:
  id: archive-001
  doc_type: OFD-A
```

两处都留空时使用 `OFD`。传入未知取值（包括大小写错误，如 `ofd-a`）会报错并列出可用取值，不会静默写入。

`ofd-creator export` 在导出 manifest 时会保留非基础取值，因此 `export` 后再 `ofd-creator` 重建不会把 profile 退回 `OFD`。

`--validate` 会按生成的 `DocType` 自动应用对应校验：`OFD-A` 与 `OFD-H` 除 GB/T 33190 基础模式与 XSD 外，还会执行 [ofd-validator 的 profile 规则](../ofd-validator/README.md#ofd-profile-校验)。但标准的「去除×××」一类条款本质是转换动作而非合规条件，归档处理流水线负责，校验器只判定并上报；字型子集化、图像插值、扫描件分层等需要阈值或启发式判断的条款也尚未实现。

## 压缩策略

```text
auto       图片、音频、视频、PDF 和压缩归档使用 Store，其他文件使用 Deflate
deflate    所有文件使用 Deflate
store      所有文件使用 Store
```

`--compression-level` 控制 Deflate 的压缩级别：`0`（默认）使用默认级别 `5`，显式范围 `1`（最快、压缩率最低）到 `9`（最慢、压缩率最高）。级别只影响实际使用 Deflate 的条目；`--compression store` 或 `auto` 选择 Store 的条目（图片、PDF 等）不受影响。

例如：

```bash
ofd-creator -i document.yaml -o result.ofd --compression deflate --compression-level 1
```

## 流式创建超大 OFD

默认情况下，通过 `file` 引用的图片、多媒体、附件和封面会先读入内存，整个 OFD 也会先在内存中生成后写出。创建包含大资源或超大页数的 OFD 时，可以使用 `--stream`：

```bash
ofd-creator -i document.yaml -o result.ofd --stream --validate
```

`--stream` 会：

- 通过 `file` 引用的封面、图片、多媒体、附件、页面图片、字体、公共/页面资源文件、扩展数据文件和印章文件使用惰性文件来源，按需流式读取，不整体驻留内存。
- 直接以流式方式写出 OFD 到目标文件，不再把整个文件包驻留内存。

字体默认会按实际使用的字形子集化，此时会逐个字体读取来源并物化后再子集化，因此常驻内存上限约为单个原始字体加全部子集结果之和；不使用子集化时字体按来源流式写入。

流式模式要求 `--output` 是文件路径，不能配合 `-o -` 写入标准输出。启用 `--validate` 时会在临时文件上执行严格校验，通过后再原子替换目标文件。

## Manifest 示例

```yaml
version: 1

document:
  id: demo-document
  title: 示例文档
  author: creator
  page_size:
    name: A4

resources:
  fonts:
    - name: Body
      font_name: SimSun
      family_name: SimSun
      file: fonts/simsun.ttf
      format: ttf
  images:
    - id: 100
      file: images/logo.png
      format: PNG

pages:
  - items:
      - type: text
        x: 20
        y: 30
        width: 100
        height: 10
        value: 你好，OFD
        font: Body
        size: 4.23
      - type: image
        x: 20
        y: 50
        width: 40
        height: 30
        resource_id: 100
```

字体的 `name` 是清单内的资源引用名，文字的 `font` 引用它；`font_name` 是写入 OFD 的实际字型名，
`family_name` 是字体族名。上面的字体声明也可写为 JSON：

```json
{"name":"Body","font_name":"SimSun","family_name":"SimSun","file":"fonts/simsun.ttf","format":"ttf"}
```

该 JSON 对象放在 `resources.fonts` 数组中。普通创建省略 `font_name` 时使用 `name`；
`flow` 会从所选嵌入字体自动补全未指定的实际字型名和族名，读取不到字型名时使用资源引用名。
因此默认 `Flow-Regular` 等别名继续用于清单引用，输出的 `FontName` 表达实际 Go 字体面。
流式排版会在合并元数据后清理未引用的字体声明，实际使用的字体再生成子集；
例如公文没有三级标题或签发人时，只为这些要素配置的楷体不会被完整带入输出。
原始资源 XML 使用数字引用时保守保留字体声明。
导出时若多个字体同名，清单只对资源引用名追加后缀，实际字型名通过 `font_name` 保留。

字体资源支持 `ttf`、`otf` 和 `ttc`。TTC 默认提取第一个字体面，流式创作可通过 `font_faces` 选择字体面。
TrueType 和 CFF（含多 Font DICT）按实际使用字形生成子集，并同步重映射显式字形编号。
子集化错误直接返回。

支持以下页面图元：

- `text`
- `path`
- `image`
- `composite`
- `page-block`
- `table`

`table` 图元在页面上绘制表格，支持列宽（`column_widths`）、表头行数（`header_rows`，跨页重复）、单元格合并（单元格的 `col_span`/`row_span`）、自动跨页、表题与续表题（`caption`/`continuation_caption`，后者支持 `{caption}` 占位符）。`style` 可分别控制横线/竖线开关、内部线宽（`line_width`）、外框线宽（`outer_line_width`）、表头分隔线宽（`header_line_width`）与表头字体字号（`header_font`/`header_size`/`header_weight`）。单元格支持对齐（`align`：left/center/right）、缩进（`first_line_indent`/`hanging_indent`，单位毫米）与自动换行：`value` 中的 `\n` 强制换行，超宽时按宽度自动断行（CJK 逐字断、ASCII 按词断）。单元格设 `format: markdown` 时按行内 Markdown 解析：`**粗体**`、`*斜体*`、`` `代码` ``（用 `style.code_font` 指定的字体）、`~~删除线~~`；样式变化处会拆成多个文字对象。表格在生成阶段展开为标准的 `PathObject` 与 `TextObject`，不引入新的 OFD 元素。完整示例见 `cmd/ofd-creator/examples/table.yaml`。

还支持 `draw_params`、`color_spaces`、`templates`、`layers`、`ctm`、`fill_color`、`stroke_color`、`text_codes`、`cg_transforms`、渐变和 Pattern 填充。

完整的 YAML 渐变实例见 `cmd/ofd-creator/examples/gradients.yaml`，包含轴向、径向、Gouraud、LaGouraud 和 Pattern 填充。

楷体文字样式实例见 `cmd/ofd-creator/examples/kaiti-styles.yaml`，包含多种字号、字重、斜体、描边和字宽设置。

字体样式矩阵实例见 `cmd/ofd-creator/examples/font-styles.yaml`，覆盖 100--900 字重、斜体、HScale、填充/描边及轴向/径向渐变文字。

绘制参数实例见 `cmd/ofd-creator/examples/drawparam.yaml`，覆盖线宽、Cap/Join、MiterLimit、DashPattern/DashOffset、颜色及 Relative 三级继承。

着色合集实例见 `cmd/ofd-creator/examples/shading.yaml`，覆盖 Axial/Radial/Gouraud/LaGouraud 及 Direct/Repeat/Reflect 映射。

三角网格渐变实例见 `cmd/ofd-creator/examples/gouraud.yaml`，单页六格覆盖单三角形、EdgeFlag=1 三角带、EdgeFlag=2 扇形、EdgeFlag=0 独立三角对，以及 Extend=0 与 Extend=1+Back Color 的未覆盖区域对照。

底纹翻转与图案填充实例见 `cmd/ofd-creator/examples/pattern-reflect.yaml`，第 1 页覆盖底纹 ReflectMethod 的 Normal/Row/Column/RowAndColumn 平铺，第 2 页为 Pattern 图案填充示例（自 shading.yaml 移入）。

径向渐变综合实例见 `examples/radial_demo.yaml`，覆盖 Eccentricity/Angle、轴向与径向的 Extend/MapType。

径向渐变 Extend 与 MapType / MapUnit 实例见 `cmd/ofd-creator/examples/radial-extend.yaml`，5 页 22 格覆盖 MapType=Direct/Repeat/Reflect × Extend 0/1/2/3（MapUnit=12，红→黄→蓝三色段）及 start_point 偏离圆心的偏心径向对照（含 Repeat/Reflect 平铺镜像与起始圆白色圆孔）；末两页几何相同、仅 Extend 不同，可在两圆外离与内含两种偏心几何下对照 Extend 的延伸行为。

封面与复合图元缩略图实例见 `cmd/ofd-creator/examples/cover-thumbnail.yaml`，包含 Cover 封面（base64 内嵌小图写入 `Doc_0/Cover/`）与复合图元 Thumbnail 缩略图引用。

文档默认颜色空间实例见 `cmd/ofd-creator/examples/default-cs.yaml`，声明 DefaultCS 指向 RGB 颜色空间，并演示 GRAY 分量色的显式 color_space 引用。

动作与附件实例见 `cmd/ofd-creator/examples/actions.yaml`，覆盖 URI、书签、音频与影片动作，并包含三条带 `NewWindow` 新窗口参数的 GotoA 附件动作（覆盖 `true`/`false` 两种取值）：文本附件、内嵌的最小单页 OFD 测试文件与同窗口文本附件。

线条样式 JSON 实例见 `cmd/ofd-creator/examples/line-styles.json`，包含线宽、端点、连接、虚线、偏移、折线和透明度效果。

两页路径填充规则实例见 `cmd/ofd-creator/examples/path-fill-rules.json`，包含默认规则、`NonZero`、`Even-Odd`、嵌套轮廓、星形、多孔图形和重叠路径。

现代信息卡片 / 活动海报样式实例见 `cmd/ofd-creator/examples/modern-info-card.yaml`，包含背景层、内容卡片、进度条、标签卡和页脚信息。

图片图元的 `ctm` 会直接写入图片变换矩阵；跳转动作还支持 `left`、`top`、`right`、`bottom` 和 `zoom` 定位参数。

`page_size` 提供 `width` 和 `height` 时优先使用显式尺寸；只有在两者都未设置时，才支持使用 `name: A4`。其他名称需要同时提供宽高。

文字图元的 `fill` 可以省略、设置为 `true` 或设置为 `false`。省略时不会强制输出 `Fill` 属性。

文档级 `actions`、`bookmarks`、`outlines`、`attachments`、`extensions`、`signatures` 和 `versions` 也可以直接在 `document` 下声明。签名值和 `seal_file_base64` 使用 Base64 字符串，附件、签名文件和版本根文件使用资源相对路径。

多版本示例见 `examples/versions.yaml`。按 GB/T 33190—2016 §19.2，`versions[].version`
表示适用的 OFD 格式版本（如 `1.0`）；`files` 应列出该版本入口、页面及资源依赖。
`files[].path` 输入相对于 OFD 文档目录 `Doc_0`，creator 写入版本描述时转换为
`/Doc_0/...` 包内绝对路径；版本的 `BaseLoc` 和 `DocRoot` 也使用包内绝对路径。
`doc_root_base64` 中的版本入口 XML 保留原内容，建议使用包内绝对路径；若使用相对路径，
则从 `Doc_0/Versions/Files` 解析。字体等资源必须由该入口或页面明确引用。
示例采用版本序号 `1、2` 和包内绝对路径，此组合的复测文件已在官方及第三方阅读器中确认可显示。

文档元数据还支持 `creation_date`、`mod_date`、`keywords`、`custom_data`、`cover`、`area`、`default_cs`、`permissions` 和 `preferences`；页面支持 `area`、`layer_type`、`actions`、页面专属 `resources` 以及裁剪区域 `clips`。

页面的 `items` 与 `layers` 互斥；页面资源的 `images` 与原始 `file`、`data_base64`、`files` 互斥。

动作可以配置 `region`，区域命令支持 `move`、`line`、`quadratic`、`cubic`、`arc` 和 `close`。

二进制资源通常使用相对文件路径；小型内嵌资源也可以使用对应的 `data_base64` 字段，两者不能同时设置。该字段可用于字体、图片、媒体、附件、页面图片、公共资源、自定义标签、封面、扩展数据和版本根文件。

颜色空间的 ICC Profile 可使用 `profile_file` 或 `profile_base64`，并可通过 `profile_name` 指定包内文件名。

`profile_name` 必须是单层文件名，不能包含目录分隔符或路径穿越片段。

输入格式支持 JSON 和 YAML；使用 `--format auto` 时根据 `.json`、`.yaml`、`.yml` 扩展名自动判断，标准输入或没有扩展名时默认按 YAML 解析。格式不支持时返回参数错误（退出码 `2`）。输入字段未知、资源路径越出资源根目录或资源文件不存在时，命令会失败并报告 manifest 字段位置。

## 退出码

| 退出码 | 含义                                                                                                      |
|--------|-----------------------------------------------------------------------------------------------------------|
| `0`    | 创建、导出、合并、拆分、替换、签名清理或校验成功；`--version` 输出版本号也算成功                                |
| `2`    | 参数错误：未知选项、缺少必填项、`-i`/`-o` 组合非法、`--format` 取值不支持、`--shape` 非法、输出会覆盖输入 |
| `3`    | 构建失败：manifest 已加载但无法生成 OFD（例如路径图元或资源不合法）                                       |
| `4`    | 资源错误：输入资源读取、解析失败，模板绑定/文字容量检查失败，或签名移除的依赖与保护范围检查失败           |
| `5`    | 输出失败：输出目录不可写、写出 OFD 或报告失败                                                             |
| `6`    | 校验失败：`--validate` 发现生成的文档不合法                                                               |

`2` 与其余各档的界线是「命令要不要改」：`2` 是命令行写错了，其余是文档本身或环境
的问题。`3`～`6` 分离出来是为了让 CI 能区分「manifest 有 bug」「文件没找到」
「磁盘/权限问题」「产物不合格」——这四类的处理方式完全不同。

`--version`（及 `-v`）对根命令和所有子命令一致生效，输出发行版本号后立即返回。

## 标准输入和标准输出

```bash
cat document.yaml | ofd-creator -i - -o result.ofd
ofd-creator -i document.yaml -o - > result.ofd
```

错误信息始终写入标准错误，不会污染标准输出中的 OFD 二进制数据。

## 格式化JSON

```shell
go install github.com/zc310/pretty/cmd/pretty@latest

pretty  -max-depth 10 -min-depth 2 --input text-directions.json -output text-directions.json
pretty  -max-depth 10 -min-depth 2 --input path-fill-rules.json -output path-fill-rules.json
pretty  -max-depth 10 -min-depth 2 --input line-styles.json -output line-styles.json
```
