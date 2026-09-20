# ofd-converter

OFD 文档转换命令行工具，支持将 OFD 文件转换为 PDF、纯文本、Markdown、单文件 HTML 和图像格式，也支持将 PDF、Markdown、Office 文档（doc/docx/odt/rtf/wps/pptx/xlsx 等）和 HTML/MHTML 转换为 OFD 或 PDF。

使用本工具处理文档前，请阅读项目根目录的 [免责声明](../../DISCLAIMER.md)。转换结果不保证适用于特定业务、法律或合规场景。

## 编译

```bash
go build .
```

## 用法

```
ofd-converter [选项] <输入文件> [输出文件或目录]

ofd-converter [选项] --input-dir <输入目录> --output-dir <输出目录>
```

查看命令帮助：

```bash
ofd-converter --help
```

## 选项

| 选项                   | 说明                                                                                                                            |
|------------------------|---------------------------------------------------------------------------------------------------------------------------------|
| `-o`, `-output`        | 输出文件路径或目录，多页图片时可为 `.zip` 文件或目录                                                                            |
| `-input-dir`           | 批量转换的输入目录；需要同时指定 `-output-dir`                                                                                  |
| `-output-dir`          | 批量转换的输出目录；保留输入目录的相对路径结构                                                                                  |
| `-format`              | 输出格式: `ofd`, `pdf`, `txt`, `md`, `markdown`, `html`, `png`, `jpg`, `svg`, `eps`, `tex`                                      |
| `-from`                | 输入格式（可选）: `pdf`, `md`, `docx`, `doc`, `odt`, `rtf`, `wps`, `pptx`, `xlsx`, `mhtml`, `html` 等；缺省按输入文件扩展名推断 |
| `-html-format`         | HTML 页面格式: `png`, `jpg` 或 `svg`，默认 `png`                                                                                |
| `-dpi`                 | 输出分辨率 (1-1200)，默认 150                                                                                                   |
| `-page`                | 指定全局页码 (从 1 开始)，0 表示全部文档体的页面；仅对 OFD 输入的 PDF/文本/Markdown/图片输出生效                                |
| `-bg`                  | 背景颜色: `transparent`, `white`, `black`，默认 `white`                                                                         |
| `-dir`                 | 不压缩，将多页图片直接保存到输出目录下的多个文件                                                                                |
| `-workers`             | 批量转换并发数，默认 `4`                                                                                                        |
| `-external-workers`    | 外部工具（LibreOffice/Chrome）批量转换并发数，默认 `2`                                                                          |
| `-soffice`             | LibreOffice 可执行文件路径；缺省按 `OFD_SOFFICE`、`PATH` 和常见安装路径查找；仅 Office 文档输入使用                             |
| `-chrome`              | Chrome/Chromium 可执行文件路径；缺省按 `OFD_CHROME`、`PATH` 查找，其余常见安装目录由 chromedp 处理；仅 HTML/MHTML 输入使用      |
| `-temp-dir`            | 外部工具（LibreOffice/Chrome）临时文件目录；缺省使用系统临时目录                                                                |
| `-office-timeout`      | Office/HTML 文档转换超时秒数；`0` 表示默认 120 秒                                                                               |
| `-paper`               | 打印纸张尺寸: `A4`, `A3`, `A5`, `Letter`, `Legal`, `B5`, `16开`；**仅 HTML/MHTML 经 Chrome 打印时生效**，默认 `A4`              |
| `-landscape`           | 横向打印，交换纸张宽高；**仅 HTML/MHTML 经 Chrome 打印时生效**，需与 `-paper` 配合使用                                          |
| `-no-print-background` | 不打印背景颜色和图片；仅 HTML/MHTML 输入生效                                                                                    |
| `-allow-remote`        | 允许加载外部资源；仅 HTML/MHTML 输入生效，默认禁止                                                                              |
| `-chrome-no-sandbox`   | 禁用 Chrome 沙箱（容器或 root 环境可能需要）；仅 HTML/MHTML 输入生效                                                            |
| `-md-tables`           | OFD 转 Markdown 时按文字位置识别无边框表格并输出 GFM 表格；默认关闭，双栏正文或公式排版可能误判                                  |
| `-recursive`           | 批量转换时递归扫描输入目录，默认开启；可使用 `-recursive=false` 关闭                                                            |
| `-overwrite`           | 批量转换时覆盖已有输出，默认开启；使用 `-overwrite=false` 将已有输出记为失败                                                    |
| `-skip-existing`       | 批量转换时跳过已有输出，不计为失败；不能与 `-overwrite=false` 同时使用                                                          |

输出格式可通过 `-format` 指定，也可根据输出文件扩展名自动推断（`.zip` 需要显式指定 `-format`）。输入默认为 OFD，PDF 等其它输入按文件扩展名识别为对应导入器，也可用 `-from` 显式指定。多个文档体按出现顺序合并，页码从所有文档体的第一张页面开始连续计算。
批量转换时必须通过 `-format` 指定统一的输出格式；默认使用 4 个并发任务，单个文件失败后会继续转换其他文件，全部任务完成后返回失败汇总。

### 选项作用范围

部分选项只在特定转换方向生效，容易误用。下表列出常见选项的适用范围：

| 选项                                                                     | 生效的转换方向                    | 说明                                                 |
|--------------------------------------------------------------------------|-----------------------------------|------------------------------------------------------|
| `-paper`、`-landscape`                                                   | HTML/MHTML → PDF/OFD              | 作为打印参数交给 Chrome，控制纸张尺寸和方向          |
| `-no-print-background`、`-allow-remote`、`-chrome-no-sandbox`、`-chrome` | HTML/MHTML → PDF/OFD              | 仅影响 Chrome 渲染行为                               |
| `-soffice`                                                               | Office 文档 → PDF/OFD             | 仅影响 LibreOffice 调用                              |
| `-office-timeout`                                                        | Office 文档、HTML/MHTML → PDF/OFD | 单次转换超时秒数，默认 120                           |
| `-temp-dir`                                                              | Office/HTML/MHTML 输入            | 外部工具临时目录                                     |
| `-page` | OFD → PDF/txt/md/图片 | 选择要转换的页面；对导入类输入不生效 |
| `-md-tables` | OFD → Markdown | 识别无边框表格并输出 GFM 表格，默认关闭 |
| `-dpi`、`-bg`                                                            | OFD → 图片、OFD → HTML            | 渲染分辨率和背景色；PDF 为矢量输出，不受 `-dpi` 影响 |
| `-html-format`                                                           | OFD → HTML                        | 选择内嵌 `png`/`jpg`/`svg`                           |

需要注意：

- `-paper` 和 `-landscape` **不会**改变 OFD → PDF、Markdown → OFD、Office → OFD/PDF 的页面尺寸。OFD → PDF 保留原页面尺寸；Markdown → OFD 使用固定 A4 版式；Office → OFD/PDF 使用源文档自身的页面设置。
- 因此，例如把 `-landscape` 用在 `ofd-converter input.ofd output.pdf` 上不会有任何效果。

## 退出码

| 退出码 | 含义                                                                       |
|--------|----------------------------------------------------------------------------|
| `0`    | 转换成功                                                                   |
| `1`    | 转换失败；批量模式下表示至少有一个文件失败，具体失败文件与原因写入标准错误 |
| `2`    | 参数错误，例如缺少输入文件、未知选项、批量模式参数冲突或不支持的格式       |

## 示例

### PDF 转 OFD

```bash
ofd-converter input.pdf output.ofd
ofd-converter -from pdf -format ofd input.pdf output.ofd
```

### Markdown 转 OFD

Markdown 输入会被重新排版为 A4 固定版式，支持标题、段落、列表、引用、代码块、GFM 表格和本地图片；代码块使用系统等宽字体（优先 Noto Sans Mono CJK SC / DejaVu Sans Mono 等）。出于安全和确定性考虑，**不会抓取远程图片**（`http`/`https` 地址会被跳过并记录警告）。相对图片路径以 Markdown 文件所在目录为基准。`-paper` 和 `-landscape` 对 Markdown 输入不生效。

```bash
ofd-converter input.md output.ofd
ofd-converter -from md -format ofd input.md output.ofd
```

### Office 文档转 OFD / PDF

通过 LibreOffice 命令行转换 Office 文档：转 OFD 时先转 PDF 再复用 PDF→OFD（保留文字层），转 PDF 时直接输出。需要目标机器已安装 LibreOffice；未安装会返回明确错误。支持 `doc/docx/odt/rtf/wps`、`ppt/pptx/odp`、`xls/xlsx/ods` 等常见格式。页面尺寸沿用源文档设置，`-paper` 和 `-landscape` 在此方向不生效。

```bash
ofd-converter report.docx report.pdf
ofd-converter --from docx --format ofd report.docx report.ofd
ofd-converter report.doc report.ofd
ofd-converter --soffice /opt/libreoffice/program/soffice --office-timeout 300 report.pptx report.ofd
```

批量转换 Office 文档或 HTML/MHTML 时通过 `-external-workers`（默认 2）限制同时运行的 LibreOffice/Chrome 进程数，普通输入仍使用 `-workers`。

### HTML/MHTML 转 OFD / PDF

通过 Chrome/Chromium 打印引擎渲染 HTML/MHTML：转 OFD 时先转 PDF 再复用 PDF→OFD，转 PDF 时直接输出。需要目标机器已安装 Chrome/Chromium；未安装会返回明确错误。支持 `html/htm/xhtml` 与 `mhtml/mht`。默认禁止加载外部资源（仅本地与 `data:`），纸张默认 A4。

```bash
ofd-converter page.html page.pdf
ofd-converter --format ofd page.html page.ofd
ofd-converter --paper A3 --landscape page.html page.pdf
ofd-converter mail.mht mail.pdf
ofd-converter --chrome-no-sandbox --allow-remote page.html page.pdf
```

### OFD 转 PDF

```bash
ofd-converter input.ofd output.pdf
```

### OFD 转纯文本

只提取页面中的文字内容，不保留字体、颜色和页面布局。文字对象按 Y 坐标聚成行、行内按 X 坐标排序，并根据水平位置用空格补位对齐列（CJK 等全角字符按两列宽度计算）；不同页面之间使用空行分隔。

```bash
ofd-converter input.ofd output.txt
ofd-converter -format txt input.ofd output.txt
```

### OFD 转 Markdown

Markdown 转换只提取文字内容，不保留字体、颜色和页面布局：

- 首行一级标题使用 OFD 文档体的 `DocInfo.Title`；没有标题时使用 `OFD 文档`。
- 段落之间会补空行：按段首缩进、段落标记（`第 X 条`、`第 X 编`、`(一)`、`一、` 等）以及纵向段间距识别段落起点，避免 GFM 把多行软换行合并成同一段。
- 不同页面之间使用水平分隔线 `---` 分隔；若首个页面没有文字，不会在正文前输出多余分隔线。
- 以 `第 X 章` 开头的行（支持阿拉伯数字和中文数字）输出为二级标题 `##`。
- 其余标题根据字号比例以及编号（如 `1.`、`1.2.`、`1.2.3.`）识别为 `###` 及更深层级，最深不超过六级。
- 只有位于页面顶部或底部的纯页码行（如 `12`、`- 12 -`、`— 12 —`）会被过滤；页面中部的纯数字行（如代码行号、数据）会保留。
- 正文中的 Markdown 特殊字符（`\`、`` ` ``、`*`、`_`、`[`、`]`、`<`、`>`、`|`、`~` 及行首标记）会被转义。

使用 `-md-tables` 时，还会根据文字的水平位置和空白间隔识别**无边框表格**（OFD 没有表格语义，表格只是文字对象的位置组合），输出 GFM 表格：第一行作为表头，跨多行的单元格会按纵向位置合并到同一行。该识别依赖版面位置，双栏正文、公式排版等可能被误判，因此默认关闭。

```bash
ofd-converter input.ofd output.md
ofd-converter -format markdown input.ofd output.md
ofd-converter -format markdown -md-tables input.ofd output.md
```

### 转换为指定格式的图片

```bash
ofd-converter -format png input.ofd output.png
ofd-converter -format jpg -bg white input.ofd output.jpg
ofd-converter -format svg input.ofd output.svg
```

### OFD 转单文件 HTML

HTML 默认将每页渲染为内嵌 PNG；使用 `-html-format jpg` 或 `-html-format svg` 时则分别使用内嵌 JPG 或 SVG 写入 HTML。三种模式都不依赖外部资源，并保留页面尺寸和浏览器打印分页。HTML 的 `<title>` 优先使用 OFD 第一个文档体的非空 `DocInfo.Title`，没有标题时使用 `OFD 文档`。

```bash
ofd-converter -format html input.ofd output.html
ofd-converter -format html -html-format jpg input.ofd output.html
ofd-converter -format html -html-format svg input.ofd output.html
```

### 转换指定页面

```bash
ofd-converter -format png -page 3 input.ofd page3.png
```

### 多页输出为 zip 包或目录

```bash
ofd-converter -format png -o pages.zip input.ofd
ofd-converter -format png -o pages/ input.ofd
ofd-converter -format png -dir -o pages input.ofd
```

### 批量转换

批量模式递归查找输入目录下扩展名为 `.ofd` 的普通文件，以及所有已注册导入器的输入文件（PDF、Markdown、Office 文档等），扩展名大小写不敏感。文本、Markdown、HTML、PDF、OFD
每个输入生成一个文件，并保留输入目录的相对路径；图片格式每个输入使用独立目录保存页面图片：

```bash
# 默认使用 4 个并发任务，递归转换为 PDF
ofd-converter --input-dir ./ofd --output-dir ./pdf --format pdf

# 使用 8 个并发任务转换为 Markdown
ofd-converter --input-dir ./ofd --output-dir ./markdown --format md --workers 8

# 多页 OFD 转换为 PNG，每个 OFD 一个页面目录
ofd-converter --input-dir ./ofd --output-dir ./images --format png

# 只扫描输入目录的第一层
ofd-converter --input-dir ./ofd --output-dir ./pdf --format pdf --recursive=false

# 批量把 Office 文档转换为 OFD，限制 LibreOffice 并发为 2
ofd-converter --input-dir ./docs --output-dir ./ofd --format ofd --external-workers 2
```

例如，`input/nested/demo.ofd` 会生成 `output/nested/demo.pdf`；转换为 PNG 时会生成
`output/nested/demo/page-0001.png` 等页面文件。批量转换不会覆盖输入目录中的同名文件；如果有文件转换失败，
命令会在所有任务完成后返回退出码 `1`，并列出失败文件及错误原因。默认直接覆盖已有输出；使用
`-overwrite=false` 时，已有输出会记为失败且不会覆盖；使用 `-skip-existing` 时，已有输出会跳过且不计为失败。

### 输出到标准输出

```bash
ofd-converter input.ofd - > output.pdf
ofd-converter -format txt input.ofd - > output.txt
ofd-converter -format md input.ofd - > output.md
ofd-converter -format png -page 1 input.ofd - > page1.png
```

多页图片输出时文件名格式为 `page-0001.png` 等。
