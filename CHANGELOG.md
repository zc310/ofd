# 更新日志

本文件只记**已发布 API 的破坏性变更**。

组件的用法、选项与接口形态写在各自的 `README.md`——例如
[`cmd/ofd-server/README.md`](cmd/ofd-server/README.md) 是该服务的完整文档。
日常历史见 `git log`，提交信息使用 Conventional Commits。

未发布周期内新增的组件（例如 `ofd-server`）不算破坏性变更：它从未发布过，
没有可破坏的使用者。

## 未发布

- `pkg/creator` 新增 `Table` 图元，可直接在页面中绘制表格；`ofd-creator` 的 manifest
  新增 `type: table` 及配套字段。

  `Table` 支持列宽（`ColumnWidths`）、表头行数（`HeaderRows`，跨页重复）、单元格合并
  （`ColSpan`/`RowSpan`，边框自动跳过合并区域内部线）、自动跨页、表题与续表题
  （`Caption`/`ContinuationCaption`，后者支持 `{caption}` 占位符）。样式上可分别控制
  横线/竖线、内部线宽、外框线宽、表头分隔线宽与表头字体字号。单元格支持对齐
  （left/center/right）、缩进（`FirstLineIndent`/`HangingIndent`）与自动换行：
  `\n` 强制换行，超宽按宽度自动断行，CJK 逐字断、ASCII 按词断。

  表格在生成阶段展开为标准的 `PathObject`（边框）与 `TextObject`（文字），不引入新的
  OFD 元素，与现有阅读器兼容。manifest 的新字段为 `rows`、`column_widths`、`header_rows`、
  `style`、`caption`、`caption_style`、`continuation_caption`、`continuation_caption_style`
  以及单元格的 `col_span`、`row_span`、`align`、`first_line_indent`、`hanging_indent`。

  单元格可设 `Format: "markdown"` 解析行内 Markdown：`**粗体**`、`*斜体*`、`` `代码` ``、
  `~~删除线~~`。样式在生成时按变化处拆成多个 `TextObject`（同一基线、X 依次累加），
  行内代码使用 `TableStyle.CodeFont` 指定的字体。manifest 对应 `format`（单元格）与
  `code_font`（样式）字段。

- `ofd-converter` 把「命令写法不对」和「文档转不出来」的退出码分开：参数、目录、格式
  与取值范围错误从 `1` 改为 `2`，与 README 早已写明的约定一致。

  受影响的场景：输入文件不存在、不支持的输出格式、`-dpi`/`-page`/`-html-format` 取值
  越界、输入与输出指向同一文件、批量模式未指定 `-format`、批量目录不存在或与输出目录
  相同。批量模式下**单个文件**转换失败仍汇总为 `1`，退出码 `0`/`130` 不变。

  分层靠 `ErrUsage` 哨兵：参数校验错误包一层，`main` 里按 `errors.Is` 分流。转换路径
  与批量并发逻辑未改动。

- `ofd-converter`、`ofd-creator`、`ofd-invoice` 新增 `--version`（`ofd-creator` 另有
  `-v`），输出发行版本号并立即返回，不读取输入文件。

  版本号仍只有 Makefile 的 `VERSION` 一个来源，经 `-X` 注入 `internal/version.Version`。
  `ofd-creator` 在命令分发前统一截获该参数，根命令与 `merge`、`replace`、`export`、
  `export-all`、`watermark`、`schema` 六个子命令行为一致——六个子命令各自构建 cobra
  树，逐个注册必然漂移。此前这三个工具传入 `--version` 会因未知选项返回 `2`。

  同时为所有命令行工具补齐原本缺失的退出码文档：`cmd/ofd-creator`（`0`/`2`/`3`/`4`/
  `5`/`6` 六档）、`cmd/ofd-seal`（`0`/`1`/`2`）、`cmd/ofd-server`（`0`/`1`/`2`，并写明
  运行期转换失败不影响退出码）、`cmd/ofd-signer-demo`（`0`/`1`，并写明它不解析输入 XML）
  与 `cmd/ofd-thumbnailer`（只有 `0`/`1`，freedesktop 缩略图规范只要求「成功 0、失败非 0」）。
  这些是纯文档补充，行为未变。

- `pkg/invoice` 新增多票汇总与合并打印能力，`cmd/ofd-invoice` 的位置参数不再限一个。

  新增 `ExtractBatch`（批量抽取并汇总）、`Summarize`（合计与按销售方/类型/月份分组）、
  `WriteCSV` 与 `WriteXLSX`（对账报表）、`MergePDF`（合并为一个 PDF，可选首页汇总对账表）、
  `SortItems`（统一页序与报表次序）。`Decimal` 补上 `Add`/`Sub`/`Mul`/`Sum`/`Round`/`Cmp`
  与公开构造入口 `NewDecimal`。

  金额口径按最小货币单位四舍五入到分后求和——不做舍入的求和会展开成 64 位小数那样的长数字，
  财务无法对账；红字（红冲）发票的负金额照常参与合计；`nil` 表示「票面上没有这个数」，
  参与运算时结果仍是 `nil`，与金额为零区分开。

  `Invoice.Check` 新增自校验，比对同一张票内部的金额关系（不含税金额 + 税额是否等于价税合计、
  逐行明细汇总是否等于票面金额），不自洽的发票在结果与报表的异常列中标出。

  附件必须带**发票代码或价税合计**（取或）才算发票，两者皆无时返回新增的
  `ErrNotInvoice`。Attachs 目录里可能放着版式说明、签章属性之类的 XML，用"有任意内容
  就抽"判定会给这类文档兜一个几乎全空的 `Invoice`——调用方看不出出错，汇总还会安静地
  算出 0 元。`ErrNotInvoice` 与 `ErrNoAttachment` 不互相满足：后者只在包内确实没有
  任何 attach XML 时返回，调用方靠 `errors.Is` 区分"附件放错地方"与"这不是发票"。

  命令行新增 `--input-dir`、`--format`、`--skip-broken`、`--dedup`、`--sort`、`--merge-pdf`、
  `--cover`、`--cover-title` 与 `--version`。布尔选项不接受单独的值：写 `--cover` 而不是
  `--cover true`，否则 `true` 会被当成输入文件路径；位置参数里混进 `true`/`false` 时错误
  信息会直接指出这一点。

  批量默认严格：任一输入抽不出发票结构化附件即整批报错，避免金额在无人察觉时少算，
  `--skip-broken` 才继续；重复发票默认只标记不丢弃，`--dedup` 才移出合计。

  `MergePDF` 新增版面参数：`--pages`（每张票取 `all`/`first`/`face` 页或页码表达式，
  `face` 靠文字层识别票面页，认不出时退回首页）、`--paper`（非 A4 的票等比缩放居中
  补白到指定纸张）、`--orientation`（默认 `auto`）、`--nup`（一张纸放 1/2/3/4/6/8/9 页）、
  `--nup-gap` 与 `--cell-border`（裁切线）。为了按票裁页与拼版，合并改为自行按版面输出
  PDF，不再走 `converter.EncodeDocuments`：每张纸先并行渲染成矢量表面再按页序串行写出，页面
  渲染并发度与转换器一致。不给任何版面参数时逐页 1:1 输出，与之前逐页同构。
  `--fit` 只提供 `contain`（等比缩放居中补白），不提供填满裁切——裁切会切掉票面边缘的
  印章与价税合计。大纲改为每张票一条并指向它所在的那张纸与格子。
  拼版网格固定为纵向：两格、三格是单列纵向拼接，四格起才分列。方向默认 `auto`，按
  **占用率**（源页面缩放后占格子的面积比）比较纵向与横向取高者——依据是格子长宽比与
  票源长宽比是否匹配，而不是格子行列数：210×140 的卷式票在 A4 纵向两格下能 1:1 铺满
  （94%），四格 2×2 却是横向更优（94% vs 47%），同一个票源两者的最优方向相反。
  可用 `--orientation portrait|landscape` 固定。
  票面**永不被放大**：源页比格子小时按 1:1 印、余下留白，避免打印件与票面物理尺寸不
  一致；`--fit` 仍只有 `contain`，不提供填满裁切。汇总页**独占一整张纸**，不参与
  `--nup` 的格位分配——跟着九宫格挤进一格后字只有原来的三分之一，等于白生成；指定
  `--paper` 时汇总页与票页用同一张纸。

  XLSX 报表是三张工作表（明细、分组汇总、异常与跳过），统一版式：标题、副标题
  写明张数与三项合计、第 4 行表头、冻结窗格、隔行底色、自动筛选与列宽；金额写成
  带两位小数的**数值**单元格（文本金额会被 Excel 的 SUM 整列跳过，对账表就没有
  意义了），负数由数字格式标红，红字发票、重复票、抽取失败分别用不同底色标出，
  合计行用粗顶线与明细分开。自动筛选只覆盖表头与明细，不含合计行——合计跟着筛选
  变动会被读成「筛选后的合计」。

  **不兼容变更**：多个位置参数过去是参数错误（退出码 `2`），现在是合法的多票输入。单个输入
  配合 JSON 输出仍返回单个 `Invoice` 对象，历史脚本无需改动；一旦给了多个输入、改了处理
  口径（`--skip-broken` / `--dedup`）或要 CSV/XLSX 报表，输出变为 `{items, skipped, summary}`。
  `Extract` 对"有附件但不是发票"的文档由返回空壳改为返回 `ErrNotInvoice`，靠
  `errors.Is(err, ErrNoAttachment)` 判断"可能是附件放错了地方"的调用方需要同时检查新错误。

- 渲染与转换在解析页面 `PageRes` 时新增一条路径回退：先按页面内容文件所在目录
  解析（规范基准），该路径在包内不存在时再回退到文档体目录 `Doc_N`。

  部分生产者把 `PageRes` 写成 `Pages/Page_0/PageRes.xml` 这类相对文档体目录的
  路径，此前渲染直接报 `file does not exist: Doc_0/Pages/Page_0/Pages/Page_0/
  PageRes.xml` 并整页失败，现在可以正常渲染。两种写法同时命中时仍以页面目录
  为准，绝对路径不参与回退，两处都不存在时错误信息里的路径保持规范基准。

  这条容错只在渲染与转换链路生效：校验器的 `reference.missing` 和分析器仍按
  规范基准报告，靠校验拦这类文件的行为不变。

- OFD 转 PDF 现在会输出可点击的链接注解。此前 PDF 导出完全丢弃链接，转出的 PDF
  视觉上无异常，但所有链接都点不动。

  外部链接（URI）与页面跳转（Goto/Dest）都支持。链接热区取自 OFD 的三处来源：
  页面正文图层图元、页面所用模板页图层，以及页面注解文件中 `Link` 注解的
  `Appearance`；热区用图元或注解 `Appearance` 自身的 `Boundary`。热区图元常写成
  `Visible="false"`——热区只是点击范围，不该画出来——不可见不影响链接导出。宽高
  为零或坐标非有限的边界会被丢弃，负宽高会归一化。

  页面跳转的目标页按输出页序解析，映射在写页之前建好；页 ID 只在文档体内唯一，
  多文档体合并时按文档体分别建立映射。被页码范围裁掉的页不会出现在输出里，指向
  它们的链接被丢弃，而不是写出指向不存在页面的注解。指向排在来源页之前的页同样
  支持。

  附件跳转、声音与影片动作暂不支持：PDF 没有对应物。

  五种目标类型（`Fit`/`FitH`/`FitV`/`XYZ`/`FitR`）与 `Dest@Zoom` 缩放比例均精确
  写入命名目标。

  链接导出只影响 PDF。PNG 等图像输出不含交互信息，PDF 文字提取结果不变
  （链接注解的 `/Contents` 是非打印的悬停提示，`pdftotext` 不参与提取）。

- OFD 转 PDF 现在会输出可跳转的大纲树。此前 PDF 导出完全丢弃文档大纲，
  转出的 PDF 没有书签目录。

  五种 `Dest` 目标类型（`Fit`/`FitH`/`FitV`/`XYZ`/`FitR`）与 `Zoom` 缩放比例均精确
  保留，嵌套层级按文档大纲展开。目标解析优先级与阅读器一致：先取 `Goto.Dest`，
  其次按书签名在 `Document.Bookmarks` 中查找。

  PDF 的每个大纲条目都必须有目标页，无法落到页面的条目会被丢弃——目录里列出标题
  却点了跳到别处，比没有这条目录更糟。被丢弃的条目不留下层级空洞，其子项上移到
  父项原有的层级上。

- `pkg/creator` 的 `Clips` 增加 `TransFlag *bool` 字段，用于写出 OFD
  `Clips@TransFlag`。该属性此前无法表达：`Clips` 在 XSD 中缺少此属性定义，
  写出后会被结构校验判为非法，而真实 OFD（含官方阅读器样例）与
  `internal/models` 的解析侧一直按 `false` 处理。缺省（规范为 `true`）时
  阅读器会把图元 CTM 叠加到裁剪路径上，裁剪路径已在图元局部毫米坐标时会
  被放大 CTM 倍。

  `pdf2ofd` 转换的 PDF 现在对全部裁剪区写 `TransFlag="false"`。

- `pkg/converter` 增加图片导入 OCR 开关和语言选项；`ofd-converter` 提供 `--ocr` 与 `--ocr-language`，默认不调用 Tesseract。

- `ofd-converter` 新增 `--password`，`pkg/converter` 新增 `WithPassword` 选项：
  加密 PDF 输入此前一律报 "encryption setup: please provide the correct
  password"。口令放在 `Converter` 上而非 `Importer` 接口参数上，与
  `SofficePath()`、`ChromePath()` 同一套路——给三个接口都加参数要动二十来处
  实现，并不因此多出任何能力。

  `pdf2ofd` 另导出 `ErrEncrypted` 与 `ErrWrongPassword`：前者补个口令能继续，
  后者是口令给错了、重试同一份不会变，两者要能分开判断。

  OFD 的包级加密（GB/T 33190）仍不支持。`--password` 的值会出现在进程命令行，
  同主机其他用户可从 `ps` 读到。


### 破坏性变更

- `pkg/converter/import/pdf` 与 `internal/pdf2ofd` 的 `Convert`、`ConvertFile`
  新增末位参数 `password string`。

  ```go
  // 之前
  pdf.Convert(ctx, input, output)
  // 现在
  pdf.Convert(ctx, input, output, password)
  ```

  传 `""` 保持原行为。

  口令错误用 `errors.Is` 匹配 pdfcpu 的 `ErrWrongPassword` /
  `ErrOwnerPasswordRequired`，不比对消息文本——库改一次措辞，字符串方案就失效，
  而失效方式是"加密文档不再被识别成加密文档"。

- `pkg/converter/import/pdf` 的 `Convert` 与 `ConvertFile` 新增首个参数
  `context.Context`。

  ```go
  // 之前
  pdf.Convert(input, output)
  pdf.ConvertFile(pdfPath, ofdPath)
  // 现在
  pdf.Convert(ctx, input, output)
  pdf.ConvertFile(ctx, pdfPath, ofdPath)
  ```

  调用方传 `context.Background()` 即可保持原行为。

  起因是 pdfcpu 0.16 给 `api.ReadContext`、`api.ValidateContext`、`PageDict`
  等函数都加了 `context.Context` 首参。PDF→OFD 的读取与校验两步在损坏或超大
  文件上本身就耗时很久，能在那里被取消比事后检查 `ctx.Err()` 有用。PDF 导入器
  从注册表收到 `*converter.Converter`，直接传 `conv.Context()`，与包内其余入口
  的取消语义一致。

- `pkg/converter` 的转换入口新增首个参数 `context.Context`。

  ```go
  // 之前
  converter.Convert("ofd", "pdf", input, output, opts...)
  // 现在
  converter.Convert(ctx, "ofd", "pdf", input, output, opts...)
  ```

  涉及 `Convert`、`Encode`、`EncodeDocuments`，以及各格式的 `PDF`/`Text`/
  `Markdown`/`HTML`/`Image` 与它们的 `*Document(s)` 变体。`Encoder`、
  `Importer`、`Transformer` 三个接口的签名未变。

  调用方传 `context.Background()` 即可保持原行为；若能传入带超时的
  `context`，大文档在页与页之间即可被取消，不必渲染完全部页。

- `ofd-converter` 在用户按 `Ctrl-C` 时以退出码 `130` 退出（原为 `0` 或 `1`）。
  脚本若依赖退出码判断成败，需要把 `130` 也视为"用户主动停止"。用法见
  [`cmd/ofd-converter/README.md`](cmd/ofd-converter/README.md)。
