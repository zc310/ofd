# OFD 对比 API

`CompareFiles` 接受两份或多份本地 OFD 路径，每个目标与选定基准比较。输入只读，解析器与渲染字体资源在比较结束后释放；逐个处理目标，最多同时打开基准和一个目标。

```go
report, err := compare.CompareFiles(ctx,
    []string{"original.ofd", "revised.ofd", "final.ofd"},
    compare.Options{Baseline: 0, Visual: true, DPI: 72, Pages: "1-3,5"},
)
if err != nil {
    return err
}
return compare.WriteReport(writer, report, "html")
```

导入路径：`github.com/zc310/ofd/pkg/compare`。包已注册 canvas 渲染后端，调用方无需额外注册。

`Options.Language` 选择报告语言，支持 `zh-CN`（默认）和 `en`；未知语言回退到 `zh-CN`。当前本地化覆盖所有报告格式中的页面配对依据说明，JSON 的 `match.method` 等机器值保持不变。翻译资源位于 `internal/localization/compare/locales`，消息 ID 使用模块前缀，例如 `compare.page_match.manual`。


`Options` 的零值启用文件、XML、页面文字比较，`Visual` 为 false；CLI 默认启用视觉比较。`Options.Baseline` 是从 0 开始的输入索引，报告顶层 `baseline`、`target`、页面序号均从 1 开始。配置快照 `options.baseline_index` 保留 API 的零起点语义。JSON 格式版本为 `schema_version: "1"`。

`Report.Inputs` 包含完整目录／文件清单、SHA-256、页面记录和元数据。`Options.Pages` 使用 `1-3,5` 格式，在各输入中独立选择从 1 开始的全局页码，先选页再匹配。`ParsePageRanges` 可在打开输入前校验并合并区间，不将大范围展开为逐页列表。未选中的 `Page.Selected` 为 false，只保留原始页码和文档体序号，不加载该页内容，文字、尺寸和内容摘要留空。某份输入没有选中任何页面时返回错误。

`Comparisons` 按目标的输入顺序排列。文件差异状态为 `added`、`removed`、`modified`、`moved`；页面状态为 `added`、`removed`、`modified`、`unchanged`，页码变化另以 `moved` 标记。移动文件通过完全相同的解压字节匹配。`pages_equal` 只针对选中页面；顶层比较结果 `equal` 仍同时检查整个包的目录和文件。

HTML 的完整文件清单以三列表格对照基准文件、目标文件和比较状态。同路径文件按 SHA-256 判断内容是否相同，已识别的移动文件将前后路径列在同一行；修改、新增、删除和移动行标红，缺失一侧显示 `—`，各侧同时显示字节数。

完整清单支持路径搜索、只看不同、状态和文件类型筛选，多个条件同时满足才展示。路径搜索忽略大小写，匹配基准或目标任一侧路径，移动文件两侧均可查找；类型筛选匹配任一侧文件。每个目标分别保留筛选条件，显示匹配数量、无结果提示并支持重置。差异行和“查看详情”链接可展开对应明细，滚动定位并将键盘焦点移至详情区域；相同文件不提供差异跳转。筛选只改变 HTML 清单展示，不影响比较结果和页面差异导航。

文件详情顶部和底部提供“返回文件清单”，重新打开清单、滚动到原文件行并高亮，键盘焦点也回到该行；搜索、状态、类型和只看不同的条件保持原值。直接展开详情也可返回对应文件行。如果该行已被新的筛选条件隐藏，则保留条件并返回搜索框。

`Options.PageMatch` 默认为 `auto`：先匹配同文档体内相同内容、唯一相同文字，再在相邻匹配页之间寻找文字相近且无明显歧义的修改页，最后在定位范围内按顺序补齐。相似度使用前 4096 个非空白字符的相邻字符对，搜索工作量上限为 400 万；达到上限时回退到定位点和顺序配对。扫描页、大片改写、相同页眉较多的页面仍可能配错。`PageMap: "2:3,4:5"` 可以优先指定原始页码，映射分别用于每个目标，两侧页码均不可重复，且必须在所选页面中。`ParsePageMap` 可预先校验语法。`number` 按选中页的全局顺序直接配对，不支持手工映射。

`PageChange.Match`（JSON `match`）记录实际采用的配对依据，独立于页面相同判定。所有报告格式都会提供该信息，文本和 Markdown 也包含未变化页面的配对依据。HTML 对顺序兜底配对给出复核提示；这不改变 `equal`、`pages_equal` 或差异计数。

| `match.method` | 含义 |
|---|---|
| `manual` | 使用手工页映射，优先于自动匹配 |
| `content` | 同文档体内页面模型内容摘要相同；共享资源或画面仍可能变化 |
| `text` | 同文档体内剩余页面中，非空文字在两侧均唯一且完全相同 |
| `text_similarity` | 根据相邻定位页和文字特征相似度关联修改页 |
| `sequence` | 自动匹配后按顺序兜底，建议核对页面对应关系 |
| `number` | 显式选择 `PageMatch: "number"`，按所选页顺序关联 |
| `unmatched` | 未匹配到另一侧页面，属于新增或删除 |

`match.text_similarity` 仅在 `text_similarity` 方法下提供，范围 0–1，保存配对时实际使用的文字特征分数，不代表正确匹配概率、页面完全相同或视觉相似度。`match.search_limited: true` 表示相似度搜索达到工作量上限后回退，只标记随后顺序配对或未匹配的页面；此前的精确匹配不受影响。

```go
for _, page := range report.Comparisons[0].Pages {
    fmt.Printf("%d → %d: %s\n", page.Before, page.After, page.Match.Method)
}
```

HTML 的“调整页配对并导出 --page-map”编辑器按目标独立保存草稿，预填当前所有已配对页，也列出未匹配的基准页。目标页使用数字输入框，避免大文档为每一行重复创建全部页码选项。展开编辑器后才初始化校验；只看变化页面的开关不会隐藏编辑器中的基准页。

修改后自动校验正整数、选页范围与重复目标页，错误行标红，参数输出清空，复制和下载禁用。清空一行表示不固定该页，重新运行时自动匹配，并不强制产生新增／删除。支持恢复原配对、清空全部映射、复制参数和下载 UTF-8 `.txt`；剪贴板不可用时选中参数，支持手工复制。草稿在切换目标时保留，刷新报告恢复原配对；可用下载保存参数。

导出参数按基准页码排序，包含 `--page-match auto --page-map "…"`；有选页范围时同时包含 `--pages`。参数仅适用于当前基准与当前目标，重新运行时将基准放在第一位、目标放在第二位，并生成新的报告。编辑器不重新计算当前报告的文字或画面差异，原配对依据和预览图保持对应原结果。例如：

```bash
ofd-compare --page-match auto --pages "2-4" --page-map "2:4,3:2" \
  -o remapped.html baseline.ofd target.ofd
```

`File.XMLSHA256` 是结构字段的摘要，仅在需要结构分析且成功时填充，未分析时留空。相同文件和只移动路径的文件不触发解析；基准侧的已分析摘要在多个目标之间保留。新增和删除的 XML 也会分析以给出诊断。每次只持有当前文件两侧的字段表，文件比较结束后释放。`XMLNote` 说明 XML 无效或超过结构分析限制。XML 差异位置采用命名空间展开名和同名元素序号；`#children` 记录元素顺序，`#text[n]` 记录子元素之间的文字。`ValueChange` 的 nil 一侧表示值不存在，空字符串表示存在但为空。`XMLTruncated` 标记明细达到上限。

`FileChange.XMLView`（JSON `xml_view`）提供格式化 XML 左右对照。`kind` 为 `format`（仅格式或其他不影响当前结构字段的变化，如前缀、属性顺序、缩进、注释）、`structure`（结构或内容变化）、`added`、`removed` 或 `unavailable`。分类依据完整的结构字段摘要，不依赖 `MaxChanges` 截断后的明细。HTML 和文本报告会显示分类；结构分析失败或受限时明确说明不可用。

对照内容统一两侧命名空间前缀、属性顺序与缩进，忽略注释和 XML 声明，保留元素顺序、属性值、`TextCode`、`xml:space` 和混合内容中的有效空白。文字中的换行、回车、制表符显示为 XML 字符引用。该视图用于阅读，原始字节仍以输入文件为准。`blocks` 包含相同或变化行组，`lines` 的 `before_number`／`after_number` 是格式化后的行号，0 表示该侧缺行。行号不对应源文件行号；拼接各侧非零行号对应的文本可还原该侧的规范化显示。

HTML 用红、绿分别标出基准和目标侧变化；连续相同行超过 8 行时保留首尾各 3 行上下文，默认折叠中间部分。所有相同片段（包括短片段和上下文）都可逐段展开／折叠，或使用“展开全部相同行”“折叠相同部分”统一操作。按钮旁显示折叠状态，已全部展开／折叠时禁用对应按钮，没有相同行时两个按钮均禁用。原有字段差异仍单独保留，可独立折叠查看。

XML 对照另设固定展示上限：每侧源文件 256 KiB、4000 个元素节点、格式化内容 4000 行及 1 MiB，且仍受已有 XML 字段表和报告累计预算约束。超过展示上限时 `blocks` 省略，`note` 说明原因，结构分类和字段差异保留；调高 XML 分析限制不会取消该展示上限。行对齐复用有界 Myers 搜索，编辑距离超过 512 步或扫描超过 400 万工作量时回退为完整变化段落，并设置 `coarse: true`，保留双方格式化内容。

`PageChange.text_diff` 是按 Unicode 字符计算的操作序列：`equal` 为共同文字，`delete` 为基准删除段，`insert` 为目标新增段。拼接 equal+delete 可还原基准，equal+insert 可还原目标，空白完整保留。文字相同时省略该字段。编辑距离搜索限制为 512 步、400 万扫描工作量；超过时以完整变化段落替换，并设置 `text_diff_coarse: true`。

`VisualChange` 的 `bounds` 是全部变化像素的矩形 `[x0,y0,x1,y1)`，`ratio` 范围为 0–1，`width`／`height` 是配对图像共同覆盖的像素范围。图片默认是 PNG data URI，仅画面变化时生成。按白底合成后比较 RGB，页面缺失或尺寸不同造成的范围差异同样计入。

`regions` 保存多个区域，每个区域有 `bounds` 和精确的 `changed_pixels`。区域按约 1 毫米网格的八邻域聚合，框覆盖变化网格，可能略大于实际变化像素。`MinRegionPixels` 默认 1，仅过滤区域展示；`ignored_region_pixels` 记录过滤掉的小区域像素，不改变原始差异统计或相同判定。`region_count` 是过滤后的区域总数，最多保留其中变化像素最多的 200 个，再按阅读顺序排列；超过时设置 `regions_truncated`。局部预览由 HTML 从整页图动态裁出，JSON 不重复嵌入局部图片。

`CompareFiles` 原样保留 `context.Canceled` 和 `context.DeadlineExceeded` 的可识别性。取消检查位于包读取、XML 分析、页面循环和像素行循环；单次底层解析／渲染不能在内部被打断。视觉预算超限返回错误；XML 结构分析受限则保留原始摘要并记录 `XMLNote`。

资源限制的零值使用默认值：`MaxXMLBytes` 为 8 MiB，`MaxXMLStructureBytes` 为每个 XML 字段表估算 32 MiB，`MaxPreviewBytes` 为全部预览图累计编码后的 PNG 64 MiB，`MaxReportBytes` 为报告数据及输出 256 MiB。预览预算包括页面三图和资源预览，多目标共用同一预算，重复展示的 PNG 也计入；外置图片仍受这个限制。报告数据在保留输入及差异条目时按紧凑 JSON 字节数累计检查，输出时再按实际格式的字节数检查。因此数据阶段通过后，HTML 重复展示或 JSON 缩进仍可能触发输出限制。它们约束报告数据与输出体积，不等同于整个进程的内存上限。

`BaselineCacheBytes` 默认 64 MiB，`-1` 禁用。在至少有两个目标时，按实际 RGBA 缓冲区字节数缓存基准页，并按最近最少使用淘汰；单页超过容量时不缓存。缓存仅存在于本次比较，不跨输入、DPI 或调用共享。`MaxTotalPixels` 默认 10 亿，只累计实际渲染的页面像素，缓存命中不重复扣减，淘汰后的重渲染仍扣减。缓存不会改变差异统计或图像内容。

设置 `Options.ImageWriter` 可将 PNG 写入外部目录，避免在报告对象中累计 base64。回调接收本次编码的 PNG，返回例如 `images/<sha256>.png` 的相对 URL；HTML 和 JSON 中的图片字段会保存该地址。只接受相对 PNG 路径，禁止绝对路径、网络协议、上级路径、查询和片段。回调写入失败会原样保留错误的可识别性；调用方负责创建目录和失败清理，并在共享 HTML 时同时携带图片目录。

```go
options := compare.Options{
    Visual: true,
    ImageWriter: func(png []byte) (string, error) {
        name := fmt.Sprintf("%x.png", sha256.Sum256(png))
        err := os.WriteFile(filepath.Join(imageDir, name), png, 0600)
        return "images/" + name, err
    },
}
```

示例需要导入 `crypto/sha256`、`fmt`、`os`、`path/filepath`，并事先在报告旁创建 `images` 目录，将其绝对路径赋给 `imageDir`。`WriteReport` 默认仍输出独立报告；传入外置图片的报告时，HTML 保留这些相对引用。

全部 CLI 选项、资源限制、报告格式和能力范围见 [ofd-compare](../../cmd/ofd-compare/README.md)。
