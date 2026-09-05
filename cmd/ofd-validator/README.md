# ofd-validator

`ofd-validator` 是用于校验 OFD 文件包的命令行工具。

## 功能

- 校验 ZIP 容器、路径穿越、重复条目、条目数量和解压大小限制。
- 解析 XML，并校验根元素、OFD 命名空间、节点数量和嵌套深度。
- 使用内置 XSD 校验 OFD、Document、Page、Res、Signature 等 XML 文件。
- 校验 OFD 包内的文档、页面、资源、附件、注解和签名文件引用。
- 校验 OFD 对象 ID 的重复声明和未解析引用。
- 校验签名摘要，支持 MD5、SHA1、SM3 和 SM3 OID `1.2.156.10197.1.401`。
- 可扫描未被主引用链直接引用的 OFD 命名空间 XML；外部命名空间 XML 内容不会参与 OFD
  引用和语义检查。
- 输出中文文本、Markdown、JSON 和带中文字体的可搜索 PDF 报告。
- PDF 报告每页底部显示页码，例如 `第 1 页 / 共 3 页`。

## 构建

在项目根目录执行：

```bash
go build -o ofd-validator ./cmd/ofd-validator
```

也可以使用 Makefile 构建 Linux 当前平台的校验器安装包：

```bash
make package-validator
```

安装包输出到 `dist/ofd-validator-<GOOS>-<GOARCH>.zip`，其中包含校验器二进制文件和本说明文档。

## 快速开始

命令格式：

```text
ofd-validator [选项] input.ofd
```

```bash
# 默认以 strict 模式输出中文文本
ofd-validator --format text document.ofd

# 输出缩进后的 JSON 报告
ofd-validator --format json --pretty document.ofd > report.json

# 输出 Markdown 报告文件
ofd-validator --format markdown -o report.md document.ofd

# 输出 PDF 报告；字体必须支持中文
ofd-validator --format pdf --font /path/to/cjk-font.ttf -o report.pdf document.ofd
```

未指定 `-o` 或 `--output` 时，报告输出到标准输出。将输出路径设为 `-` 也表示输出到标准输出。
工具不会允许报告文件覆盖输入的 OFD 文件。

## 校验模式

- `strict`：执行 ZIP、XML、内置 XSD、文件引用、语义 ID 和摘要校验。XSD 错误会使报告失败。
- `compat`：执行相同校验，但将 XSD 错误降级为警告；ZIP、XML、引用、语义和摘要错误仍然会失败。
- `structural`：执行 ZIP、XML、文件引用和语义检查，不执行 XSD 校验。没有错误时报告状态通常为
  `partial`（部分通过），因为 XSD 阶段被跳过。

`--skip-xsd` 会跳过 XSD 阶段；在默认的 `strict` 模式下等价于使用 `structural` 模式。

## 命令选项

| 选项                                 | 默认值      | 说明                                                   |
|--------------------------------------|-------------|--------------------------------------------------------|
| `--format text\|markdown\|json\|pdf` | `text`      | 报告格式                                               |
| `--mode strict\|compat\|structural`  | `strict`    | 校验模式                                               |
| `-o`, `--output PATH`                | 标准输出    | 报告输出路径，使用 `-` 输出到标准输出                  |
| `--font PATH`                        | 自动查找    | PDF 使用的中文字体文件；只能与 `--format pdf` 一起使用 |
| `--pretty`                           | 关闭        | 缩进 JSON 输出                                         |
| `--skip-xsd`                         | 关闭        | 跳过 XSD 校验                                          |
| `--no-digest`                        | 关闭        | 跳过签名摘要校验                                       |
| `--no-scan-xml`                      | 关闭        | 只解析由 OFD 引用到的 XML 文件                         |
| `--fail-on-warning`                  | 关闭        | 有警告时也返回退出码 `1`                               |
| `--max-errors N`                     | `100`       | 最多记录的校验错误数量；`0` 表示不限制                 |
| `--max-file-size BYTES`              | `67108864`  | 单个 ZIP 条目解压后的最大字节数，即 64 MiB             |
| `--max-total-size BYTES`             | `536870912` | OFD 包解压后的最大总字节数，即 512 MiB                 |
| `--max-entries N`                    | `10000`     | ZIP 条目的最大数量                                     |
| `--max-xml-bytes BYTES`              | `67108864`  | 单个 XML 文件的最大字节数，即 64 MiB                   |
| `--max-xml-nodes N`                  | `2000000`   | 单个 XML 文件的最大节点数量                            |
| `--max-xml-depth N`                  | `1000`      | 单个 XML 文件的最大嵌套深度                            |
| `--version`                          | 关闭        | 输出工具版本                                           |
| `-h`, `--help`                       | 关闭        | 显示命令帮助                                           |

大小参数使用字节数，不支持 `64M`、`512M` 等单位后缀。限制参数不能为负数。

## 报告格式

- `text`：适合终端查看的中文纯文本报告。
- `markdown`：包含检查结果和逐条问题详情的 Markdown 报告。
- `json`：保留英文机器字段和枚举值，同时提供 `status_zh`、`severity_zh`、`stage_zh` 等中文字段；
  适合 CI、脚本和其他程序处理。
- `pdf`：A4 可搜索文本报告。需要能够显示中文的字体；可使用 `--font` 显式指定字体。每一页底部
  会显示当前页码和总页数。

PDF 示例：

```bash
ofd-validator --format pdf \
  --font /path/to/SimSun.ttf \
  --output validation-report.pdf \
  document.ofd
```

未指定 `--font` 时，工具会先查找常见中文系统字体，再扫描系统字体目录。如果环境中没有可用中文字体，
PDF 输出会失败；文本、Markdown 和 JSON 报告不受此限制。

## 退出码

- `0`：校验没有错误；默认情况下存在警告仍返回 `0`。
- `1`：存在校验错误，或指定 `--fail-on-warning` 且存在警告。
- `2`：命令参数、输入文件、报告输出路径、字体或其他工具配置无效。

## CI 示例

严格校验并保存机器可读报告：

```bash
ofd-validator --format json --pretty \
  --output validation-report.json \
  document.ofd
status=$?
test "$status" -eq 0
```

对历史文件使用兼容模式，但仍要求不能有任何警告：

```bash
ofd-validator --mode compat --fail-on-warning document.ofd
```

只做结构检查，跳过可能不兼容的 XSD 校验：

```bash
ofd-validator --mode structural document.ofd
```
