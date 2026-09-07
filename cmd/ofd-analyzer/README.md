# ofd-analyzer

`ofd-analyzer` 是面向 OFD 文件的结构分析工具，用于分析文档组成、页面对象、文字、资源、附件、注解、签名和引用关系，适合文档排查、资源审计、转换问题定位以及生成结构化分析报告。默认输出纯文本报告。

## 构建

在项目根目录执行：

```bash
go build -o ofd-analyzer ./cmd/ofd-analyzer
```

也可以构建独立安装包：

```bash
make package-analyzer
```

安装包输出到 `dist/ofd-analyzer-<GOOS>-<GOARCH>.zip`。

## 用法

```text
ofd-analyzer [选项] input.ofd
```

```bash
# 默认输出纯文本
ofd-analyzer document.ofd

# 输出紧凑 JSON
ofd-analyzer --format json document.ofd

# 输出缩进后的 JSON 报告文件
ofd-analyzer --pretty --output report.json document.ofd

# 输出纯文本、Markdown 或 PDF 报告
ofd-analyzer --format text -o report.txt document.ofd
ofd-analyzer --format markdown -o report.md document.ofd
ofd-analyzer --format pdf --font /path/to/font.ttf -o report.pdf document.ofd

# 跳过可选分析阶段
ofd-analyzer --no-annotations --no-signatures document.ofd
```

未指定 `-o` 或 `--output` 时，报告输出到标准输出。将输出路径设为 `-` 也表示输出到标准输出。
工具不会允许报告文件覆盖输入的 OFD 文件。

## 命令选项

| 选项                  | 默认值   | 说明                                                    |
|-----------------------|----------|---------------------------------------------------------|
| `-o`, `--output PATH` | 标准输出 | 报告输出路径，使用 `-` 输出到标准输出                   |
| `--format FORMAT`     | `text`   | 报告格式：`text`、`markdown`、`json` 或 `pdf`           |
| `--font PATH`         | 自动     | PDF 报告使用的字体文件；仅适用于 PDF                    |
| `--pretty`            | 关闭     | 缩进 JSON 输出                                          |
| `--no-templates`      | 关闭     | 跳过模板定义、引用和 PageRes 资源分析，但保留模板元数据 |
| `--no-annotations`    | 关闭     | 跳过注解及 Appearance 分析                              |
| `--no-signatures`     | 关闭     | 跳过签名清单分析                                        |
| `--no-package`        | 关闭     | 跳过 ZIP 条目统计，但保留包内路径索引                   |
| `--fail-on-warning`   | 关闭     | 有警告时返回退出码 `1`                                  |
| `--version`           | 关闭     | 输出 analyzer 版本                                      |
| `-h`, `--help`        | 关闭     | 显示命令帮助                                            |

## 报告

报告包含 Schema 版本、输入信息、OFD 和 ZIP 包统计、文档体和页面列表、对象和文字统计、资源定义与使用情况、
附件、注解、签名以及文件和 ID 引用关系。报告中的资源引用使用文档体作用域，例如
`doc[0]/font:3`，避免多个文档体使用相同 ID 时发生混淆。

`resources` 是所有资源类别的总汇总，包含图片、字体、绘制参数、复合图元、颜色空间、模板和 Pattern；
各资源类别也通过对应的独立字段提供详细统计。字体专用字段为 `embedded`，绘制参数专用字段为
`inheritance_cycles`，不会出现在其他资源类别中。

模板、复合图元和 Pattern 默认只统计定义及引用，不展开其内部对象；因此它们的定义内容不会重复计入页面对象和文字汇总。
使用 `--no-templates` 时，模板定义、页面模板引用和模板 `PageRes` 资源均不参与分析，但文档体中的 `template_count` 和文档到模板文件的结构引用仍会保留。

绘制参数统计包括定义数、引用次数、唯一引用数、无法解析引用数和 `Relative` 继承循环数；引用次数包含
页面、模板、注解对象以及绘制参数继承产生的引用。

页面对象按 `Layer.Items` 的文档顺序递归统计。模板和注解 Appearance 的对象、文字及资源统计分别记录，不会混入普通页面对象。
字符统计同时提供 Unicode code point、UTF-8 字节、空白字符和 glyph 数量。

## 退出码

- `0`：分析完成；默认情况下存在警告仍返回 `0`。
- `1`：OFD 无法分析，或指定 `--fail-on-warning` 且报告包含警告。
- `2`：命令参数、输入文件或报告输出路径无效。
