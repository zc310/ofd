# ofd-archive

`ofd-archive` 是面向 GB/T 42133-2022 相关档案处理场景的 OFD 技术预检和归档准备工具。它组合现有 `ofd-validator` 与 `ofd-analyzer`，并生成档案状态、技术清单、附件记录和 SHA-256 固定性信息。

本工具不修改输入 OFD。`check`、`manifest`、`matrix` 只做判定与上报，不对文档做任何处置；唯一的例外是 `preserve`，它按 GB/T 42133 长期保存处理改造文件，但只写入 `--output` 指定的新文件，输入始终不变。本工具不自动删除动作、视频、注释或附件，不嵌入字体，不重签名，不自动解密，也不替代档案管理系统、密码学验证或人工合规判断。原始签名文件原样保存在准备目录中。

## 用法

无子命令时默认执行 `check`：

```bash
ofd-archive check --format text document.ofd
ofd-archive check --format markdown --output check.md document.ofd
ofd-archive check --format json --pretty document.ofd > check.json
ofd-archive manifest --format json document.ofd > manifest.json
ofd-archive matrix --format markdown document.ofd > matrix.md
ofd-archive matrix --format xlsx --output matrix.xlsx document.ofd
# 输出路径为 .xlsx 时，也可以省略 --format xlsx
ofd-archive matrix --output matrix.xlsx document.ofd
ofd-archive prepare document.ofd --metadata archive.json --output archive-directory/
ofd-archive preserve --doc-type OFD-A --dry-run document.ofd
ofd-archive preserve --doc-type OFD-A --output preserved.ofd document.ofd
ofd-archive verify archive-directory/
```

`archive.json` 使用 JSON，也可以使用 YAML；可选 profile 使用 `required_fields` 声明档案字段，例如：

```json
{
  "archive_code": "A-2026-001",
  "fonds_code": "F-001",
  "retention_period": "永久",
  "security_classification": "内部",
  "open_status": "开放"
}
```

附件提取默认限制为 64 MiB，可通过 `--max-attachment-size` 调整；超过限制时保留原始 OFD 中的附件记录，但不复制附件副本。

使用 profile 时，将 `--profile profile.json` 加到 `prepare` 或 `manifest` 命令中；profile 文件会在 `prepare` 结果的 `metadata/` 中原样保存并纳入固定性清单。

`--profile` 只用于档案字段，与 OFD 的 profile 无关。指定 OFD profile 用 `--doc-type`：

```bash
ofd-archive check --doc-type OFD-A document.ofd
```

`--doc-type` 与 [ofd-validator](ofd-validator/README.md#ofd-profile-校验)、`ofd-creator` 同义，取 `OFD`、`OFD-A` 或 `OFD-H`，留空时按文件声明的 `DocType` 自动判定。本工具面向 GB/T 42133 的归档预检场景，因此对声明 `OFD-A` 的文件会自动应用该标准的 profile 规则；`--doc-type` 用于在不改写文件 `DocType` 的前提下预检一份基础 OFD 是否已满足长期保存要求。规则只判定并上报，不修改文档。

### 长期保存处理（preserve）

`preserve` 把 OFD 改造成符合 GB/T 42133 长期保存要求的文件。它与 `check` 的关系是：`check` 按 profile 规则判定并上报，`preserve` 执行标准要求的转换动作，转换后的文件再用 `check` 复核。

```bash
# 先看计划改动，不写文件
ofd-archive preserve --doc-type OFD-A --dry-run document.ofd

# 确认后写出新文件
ofd-archive preserve --doc-type OFD-A --output preserved.ofd document.ofd

# JSON 输出，便于接入流水线
ofd-archive preserve --doc-type OFD-A --dry-run --format json --pretty document.ofd
```

已实现的转换动作，全部来自 GB/T 42133 第 6 章：

| 依据条款 | 作用文件 | 改动 |
|----------|----------|------|
| GB/T 42133 6.2.2 a) | `Document.xml` | 去除权限声明 `Permissions` |
| GB/T 42133 6.2.2 b) | `Document.xml` | 去除视图首选项 `VPreferences` |
| GB/T 42133 6.2.2 c) | `Document.xml` | 去除非文档内跳转的文档动作 |
| GB/T 42133 6.2.2 e) | `Document.xml` | 去除扩展信息 `Extensions` |
| GB/T 42133 6.2.5 a) | `Document.xml` | 去除大纲节点中非文档内跳转的动作 |
| GB/T 42133 6.2.3 c) | 页面 `Content.xml` | 去除非文档内跳转的页面动作 |
| GB/T 42133 6.3.3 b) | 页面 `Content.xml` | 去除图元对象中非文档内跳转的动作 |

这些动作只在 `DocType` 为 `OFD-A` 或 `OFD-H` 时施加。基础 profile 不承诺满足 GB/T 42133，`preserve` 不会改动它，文件无改动可做时返回非零退出码。

关于「文档内跳转」的判定：OFD 的动作是「多种选择项之一」，`CT_Action` 并无 `Type` 属性，因此以是否含 `Goto` 子元素为准，与 `ofd-validator` 的 profile 规则同一口径。宿主划分也刻意与校验器一致——文档级与页面级只取文件根节点的**直接子** `Actions`，大纲级则逐层下探 `OutlineElem`，否则大纲内的动作会被误记到文档动作上。

动作删光后 `Actions` 容器会一并删除：XSD 里 `Action` 没有 `minOccurs`（默认 1），留下空容器会让输出通不过校验。

6.2.1 c)（删除无人引用的条目）单独处理，因为删除不可逆：

```bash
# 默认只报告有哪些条目无人引用，不删除
ofd-archive preserve --doc-type OFD-A --output out.ofd document.ofd

# 显式确认后执行删除
ofd-archive preserve --doc-type OFD-A --drop-unreferenced --output out.ofd document.ofd
```

判定「无人引用」用的是 `ofd-validator` 的同一套引用识别与路径解析逻辑（见 `validator.PackageReferences`），因此「校验通过」与「闭包完整」两个结论不会互相矛盾。引用闭包从 `OFD.xml` 出发按嵌套引用遍历，字体、图像等叶子资源也计入可达集合。

闭包不完整时**一律拒绝删除**，并在警告中说明原因。三种情况会让闭包不可信：引用指向不存在的文件、引用路径越过包根、被引用的 XML 无法解析（命名空间或根元素不符时引用收集不到）。这不是过度保守——`test/testdata/intro.ofd` 的命名空间缺 `/2016` 后缀，解析失败后它引用的字体会全部落进「无人引用」列表（该文件共 123 个条目），照单删除就是毁掉一份真实文件。完整清单用 `--format json` 查看。

尚未实现的条款：6.2.5 b)（目标书签或页面不存在时去除动作）、6.2.3 a) b) d) e)（页面设置归并、图层改名、页面块扁平化）、6.2.6（资源归并）、6.3.1、6.3.2（裁剪区几何判断）、6.3.3 c) d) e)（标准用「宜去除」，属建议，本阶段不做）、6.4、6.5、6.6（绘制参数与文字对象归并，需阈值判断）、6.13、6.14（签名去技术化）、6.15。其中 6.3.3 b) 目前只去除非 Goto 动作，图元对象上的 Goto 动作按标准应转为链接注释，该转换尚未实现，故予以保留而非删除。

行为约定：

- `--output` 必须显式给出，且不能与输入同一路径。这是本工具唯一会写文件的子命令，不设默认值以免覆盖原始文件；`--dry-run` 则相反，不接受 `--output`。
- 转换只改写文档主体 XML（路径取自 `OFD.xml` 的 `DocBody/DocRoot`，不假定 `Doc_0`），包内其余条目逐字节复制。
- 只删除 OFD 命名空间下的目标元素，外来命名空间里的同名标签不受影响。
- 写出会校验转换是否引入**新的**错误。判据不是「输出零错误」——真实 OFD 常带既有 XSD 偏差，以零错误为门槛会让本命令在多数文件上直接拒绝输出；实际做法是与转换前的问题码集合比对，只在出现新问题码时放弃写出。
- 签名条目原样保留并给出警告：文档主体被改写后，签名摘要可能失效。GB/T 42133 6.14 的签名去技术化尚未实现。
- 转换会失效既有签名的摘要，这是标准的固有要求，不是缺陷。需要留证的用户应保留原始签名文件，另行归档转换结果。

准备目录结构：

```text
archive-directory/
├── original/document.ofd
├── metadata/archive.json
├── metadata/manifest.json
├── reports/check.json
├── reports/analysis.json
├── fixity/manifest.sha256
└── attachments/
```

状态为 `passed`、`warning` 或 `failed`。退出码 `0` 表示通过或有普通警告，`1` 表示失败或指定 `--fail-on-warning`，`2` 表示参数错误，`3` 表示输出或准备目录失败。

默认情况下，只有错误会使命令返回退出码 `1`；如果希望把普通警告也作为失败处理，可使用：

```bash
ofd-archive check --fail-on-warning document.ofd
```

`verify` 除了校验归档目录中的 SHA-256 固定性清单、文件登记和路径安全外，还会严格解析
`reports/check.json` 与 `reports/analysis.json`，检查报告版本、工具信息以及输入文件信息是否与
`metadata/manifest.json` 一致。
