# ofd-analyzer

`ofd-analyzer` 是面向 OFD 文件的结构分析工具，用于分析文档组成、页面对象、文字、资源、附件、注解、签名和引用关系，适合文档排查、资源审计、转换问题定位以及生成结构化分析报告。默认输出纯文本报告，也可输出 Markdown、JSON、PDF 和 XLSX 报告。

JSON 报告的 `schema_version` 保持为 `1`，签名项增加 `validation` 完整验证结论与检查明细。
文本、Markdown、PDF 和 XLSX 报告也展示完整结论；两层数学签名通过不代表完整验证通过。

普通 `Type=Sign` 按 GM/T 0099 的 SM2/SM3 SignedData 验证，`verification_valid` 表示单层
数学签名，`outer_verification` 记录其证书与签名结果，不生成 SES 印章内部签名结果。
SES 继续保留两层语义。两种类型都需要检查 `validation` 的完整结论。

GM/T 0099-2020 加密包会报告“已加密且未解锁”。可见元数据检查成功时报告状态为
`partial`，JSON 可选 `encryption` 字段提供加密操作、接收者和可读映射；分析 API 同时
返回可由 `errors.Is(err, crypt.ErrLocked)` 判断的错误，CLI 退出 `1`，诊断写入 stderr。
页面、资源、目录树和签名统计尚未执行，不能将统计零值解释为原文没有这些内容。
元数据也无法检查时状态为 `failed`。当前阶段和格式约定见
[OFD-CRYPTO.md](../../docs/OFD-CRYPTO.md)。

Go API `Analyze` 还接受完整解锁的 `crypt.View`，通过独立租约读取逻辑内容，并在报告的
`encryption.state=unlocked` 中保留来源状态。部分视图仍返回 `ErrLocked` 和 `partial`，
不会把未解锁内容算作零页/无签名。视图的压缩统计为逻辑视角，不能代替原始 ZIP 的压缩大小。

使用本工具处理文档前，请阅读项目根目录的 [免责声明](../../DISCLAIMER.md)。分析结果不构成法律、合规、审计或安全结论。

## 构建

在项目根目录执行：

```bash
go build -o ofd-analyzer ./cmd/ofd-analyzer
```

需要 Go 1.26 或更高版本。提交推送到 GitHub 后，可在仓库外安装 `main` 分支版本；也可在仓库根目录安装当前检出的源码：

```bash
# 远程 main 分支
go install github.com/zc310/ofd/cmd/ofd-analyzer@main
# 当前本地检出：在仓库根目录执行
go install ./cmd/ofd-analyzer
```

程序安装到 `GOBIN`，未设置时为 `$(go env GOPATH)/bin`；请确保该目录位于 `PATH` 中。

也可以构建独立安装包：

```bash
make package-analyzer
```

安装包输出到 `dist/ofd-analyzer-<GOOS>-<GOARCH>.zip`。

## 用法

```text
ofd-analyzer [选项] input.ofd
```

查看命令帮助：

```bash
ofd-analyzer --help
```

```bash
# 默认输出纯文本
ofd-analyzer document.ofd

# 输出紧凑 JSON
ofd-analyzer --format json document.ofd

# 输出缩进后的 JSON 报告文件
ofd-analyzer --pretty -  document.ofd

# 输出纯文本、Markdown、PDF 或 XLSX 报告
ofd-analyzer --format text -o report.txt document.ofd
ofd-analyzer --format markdown -o report.md document.ofd
ofd-analyzer --format pdf --font /path/to/font.ttf -o report.pdf document.ofd
ofd-analyzer --format xlsx -o report.xlsx document.ofd

# 未指定 --format 时，根据输出文件扩展名自动选择格式
ofd-analyzer -o report.txt document.ofd
ofd-analyzer -o report.md document.ofd
ofd-analyzer -o report.json document.ofd
ofd-analyzer -o report.pdf --font /path/to/font.ttf document.ofd
ofd-analyzer -o report.xlsx document.ofd

# 跳过可选分析阶段
ofd-analyzer --no-annotations --no-signatures document.ofd

# 在报告中附加 OFD ZIP 包目录结构
ofd-analyzer --tree document.ofd
ofd-analyzer --tree --format json --pretty document.ofd
```

未指定 `-o` 或 `--output` 时，报告输出到标准输出。将输出路径设为 `-` 也表示输出到标准输出。
工具不会允许报告文件覆盖输入的 OFD 文件。

## 命令选项

| 选项                             | 默认值   | 说明                                                    |
|----------------------------------|----------|---------------------------------------------------------|
| `-o`, `--output PATH`            | 标准输出 | 报告输出路径，使用 `-` 输出到标准输出                   |
| `--format FORMAT`                | `text`   | 报告格式：`text`、`markdown`、`json`、`pdf` 或 `xlsx`；未显式指定时可根据输出扩展名自动推断 |
| `--font PATH`                    | 自动     | PDF 报告使用的字体文件；仅适用于 PDF                    |
| `--signature-uid ID`             | 空       | SM2 签名用户标识；为空时使用默认 UID                    |
| `--signature-format`             | 自动     | SM2 签名值格式：`auto`、`der` 或 `raw`                  |
| `--signature-roots`              | 空       | PEM 信任根文件；指定后执行证书链校验                    |
| `--signature-crls`               | 空       | 离线 PEM/DER CRL 文件；指定后执行证书吊销校验           |
| `--signature-revocation-issuers` | 空       | 验证 CRL 签名使用的 PEM 签发者证书文件                  |
| `--pretty`                       | 关闭     | 缩进 JSON 输出                                          |
| `--no-templates`                 | 关闭     | 跳过模板定义、引用和 PageRes 资源分析，但保留模板元数据 |
| `--no-annotations`               | 关闭     | 跳过注解及 Appearance 分析                              |
| `--no-signatures`                | 关闭     | 跳过签名清单分析                                        |
| `--tree`                         | 关闭     | 输出 OFD ZIP 包目录结构                                 |
| `--fail-on-warning`              | 关闭     | 有警告时返回退出码 `1`                                  |
| `--version`                      | 关闭     | 输出 analyzer 版本                                      |
| `-h`, `--help`                   | 关闭     | 显示命令帮助                                            |

未显式指定 `--format` 时，输出文件扩展名 `.txt`、`.md`/`.markdown`、`.json`、`.pdf` 和 `.xlsx`
分别对应 `text`、`markdown`、`json`、`pdf` 和 `xlsx`；显式指定的格式优先。标准输出、无扩展名或
其他扩展名仍默认使用 `text`。

## 报告

签名报告新增 `validation`，包含完整验证状态 `valid`、`invalid` 或 `incomplete`，
以及逐项 `checks`（`code`、`status`、`message`）；文本、Markdown、PDF 和 XLSX
报告显示完整结论。两层签名的原有数学验证字段保留。缺少证书信任、吊销证据、
印章状态或无法确认保护范围时，不会报告完整通过；完整配置入口见
[`pkg/sign.Verify`](../../pkg/sign/README.md#完整验证)。

XLSX 报告为 Excel 工作簿，包含「汇总」页，以及「文档体」「页面」「资源」「附件」「注解」「签名」
「目录树」「文件引用」「ID 引用」等明细页。各明细页带有自动筛选和冻结表头，适合在 Excel 中进行
筛选、归档和二次处理。

报告包含 Schema 版本、输入信息、OFD 和 ZIP 包统计、文档体和页面列表、对象和文字统计、资源定义与使用情况、
附件、注解、签名值结构解析、签名摘要校验结果、SES 签名密码学验证结果以及文件和 ID 引用关系。签名报告包含印章 ID、名称、有效期、图片类型、
内外层签名算法 OID 和签名值编码格式，以及每个 `Reference` 的实际摘要、期望摘要、
文件存在性和匹配结果；对 SES 电子印章签名还会报告 `TBS_Sign.DataHash`、印章内部签名和外层签名校验结果。密码学验证使用签名时间检查证书有效期，
默认不加载系统根证书，因此证书信任显示为“未校验”，也不代表证书链信任或吊销状态。底层解析器可由调用方显式提供信任根执行证书链验证；使用 `--signature-crls` 和 `--signature-revocation-issuers` 可执行不联网的离线 CRL 吊销校验。工具不会访问证书中的 CRL 地址；未提供匹配 CRL 时状态为“未知”。SM2 验证支持默认 UID、调用方指定 UID，以及 DER `SEQUENCE {r,s}` 和 64 字节 `r||s` 签名值格式，三类配置可以组合使用。报告中的资源引用使用文档体作用域，例如
`doc[0]/font:3`，避免多个文档体使用相同 ID 时发生混淆。

报告始终包含 OFD ZIP 包统计；使用 `--tree` 时，报告会在文本和 Markdown 中附加 ZIP 包目录树，在 JSON 的 `package.tree` 中提供嵌套目录节点。
目录树只表示包内实际路径，不展开 XML 节点；文件引用关系仍单独输出。目录按目录优先、名称排序，文件节点包含类型和解压后大小。

## 截图

![ofd-analyzer Markdown 报告](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-analyzer-markdown.png)

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
