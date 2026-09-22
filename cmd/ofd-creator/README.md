# ofd-creator

`ofd-creator` 根据 JSON、YAML 或 TOML manifest 创建 OFD 文件包。

使用本工具创建或生成文档前，请阅读项目根目录的 [免责声明](../../DISCLAIMER.md)。生成结果应根据实际业务、法律和合规要求进行独立复核。

## 构建

```bash
go build -o ofd-creator ./cmd/ofd-creator
```

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

## 导出 OFD 配置

支持将单文档体 OFD 导出为可再次用于 `ofd-creator` 的 YAML、JSON 或 TOML manifest，并将字体、图片和其他多媒体资源复制到资源目录。默认格式为 YAML：

```bash
ofd-creator export \
  --input input.ofd \
  --output exported/document.yaml \
  --asset-root assets
```

未指定 `--format` 时，会根据输出文件扩展名自动选择格式：`.json` 使用 JSON，`.toml` 使用 TOML，`.yaml`/`.yml` 使用 YAML；标准输出或没有扩展名时默认使用 YAML。也可以显式使用 `--format json` 或 `--format toml`：

```bash
ofd-creator export \
  --input input.ofd \
  --output exported/document.json \
  --format json

ofd-creator export \
  --input input.ofd \
  --output exported/document.toml \
  --format toml
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

`index.yaml`（或指定格式的 `index.json`、`index.toml`）列出每个文档体的索引、ID、标题、manifest 路径、资源目录和页数。每个子 manifest 都可以独立执行普通创建命令；批量导出要求目标目录不存在，以避免覆盖已有文件。

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

合并过程会把每个输入签名的处理结果（保留/重写/丢弃）汇总到标准错误。加 `--verify-signatures` 会在写出前解析输出文档，逐签名报告摘要与 SM2/SES 密码学验证状态：

```bash
ofd-creator merge -o merged.ofd --signatures rewrite --verify-signatures input1.ofd input2.ofd
```

注意 `rewrite` 会改写签名文件本身，可能使 SES 的数据摘要（对签名 XML 计算）失效，即使被引用文件摘要仍然匹配；这类签名需要重新签名。

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
- 私钥和密码学算法由命令负责，`ofd-creator` 不接触密钥。`--sign-id` 必须是合法的 XML `xs:ID`（默认 `sign-1`）。

重签名通常配合 `--signatures drop`（先清掉旧签名）或有 `--pages` 的模型级合并使用。

`merge` 支持与创建命令相同的压缩策略和确定性选项，并可使用 `--validate` 在写出前执行严格校验：

```bash
ofd-creator merge -o merged.ofd --compression auto --deterministic --validate input1.ofd input2.ofd
```

`--output -` 可以把合并结果写入标准输出；`merge` 不支持从标准输入读取。

`--orphans` 控制文档目录之外的条目的处理方式：`error`（默认，直接失败）、`ignore`（跳过）或 `preserve`（按原路径保留到包根）。`--signatures rewrite` 重写签名路径时会向标准错误输出警告，说明哪些签名值已失效。

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

## 压缩策略

```text
auto       图片、音频、视频、PDF 和压缩归档使用 Store，其他文件使用 Deflate
deflate    所有文件使用 Deflate
store      所有文件使用 Store
```

例如：

```bash
ofd-creator -i document.yaml -o result.ofd --compression auto
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
    - name: SimSun
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
        font: SimSun
        size: 4.23
      - type: image
        x: 20
        y: 50
        width: 40
        height: 30
        resource_id: 100
```

字体资源支持 `ttf`、`otf` 和 `ttc`。对于包含多字体 CFF 的 TTC，创建器会提取第一个字体面并保留其完整字形数据，不进行不安全的 CFF 子集化，以保证阅读器能够正常加载。

支持以下页面图元：

- `text`
- `path`
- `image`
- `composite`
- `page-block`

还支持 `draw_params`、`color_spaces`、`templates`、`layers`、`ctm`、`fill_color`、`stroke_color`、`text_codes`、`cg_transforms`、渐变和 Pattern 填充。

完整的 TOML 渐变实例见 `cmd/ofd-creator/examples/gradients.toml`，包含轴向、径向、Gouraud、LaGouraud 和 Pattern 填充。

楷体文字样式实例见 `cmd/ofd-creator/examples/kaiti-styles.yaml`，包含多种字号、字重、斜体、描边和字宽设置。

线条样式 JSON 实例见 `cmd/ofd-creator/examples/line-styles.json`，包含线宽、端点、连接、虚线、偏移、折线和透明度效果。

两页路径填充规则实例见 `cmd/ofd-creator/examples/path-fill-rules.json`，包含默认规则、`NonZero`、`Even-Odd`、嵌套轮廓、星形、多孔图形和重叠路径。

现代信息卡片 / 活动海报样式实例见 `cmd/ofd-creator/examples/modern-info-card.yaml`，包含背景层、内容卡片、进度条、标签卡和页脚信息。

图片图元的 `ctm` 会直接写入图片变换矩阵；跳转动作还支持 `left`、`top`、`right`、`bottom` 和 `zoom` 定位参数。

`page_size` 提供 `width` 和 `height` 时优先使用显式尺寸；只有在两者都未设置时，才支持使用 `name: A4`。其他名称需要同时提供宽高。

文字图元的 `fill` 可以省略、设置为 `true` 或设置为 `false`。省略时不会强制输出 `Fill` 属性。

文档级 `actions`、`bookmarks`、`outlines`、`attachments`、`extensions`、`signatures` 和 `versions` 也可以直接在 `document` 下声明。签名值和 `seal_file_base64` 使用 Base64 字符串，附件、签名文件和版本根文件使用资源相对路径。

文档元数据还支持 `creation_date`、`mod_date`、`keywords`、`custom_data`、`cover`、`area`、`default_cs`、`permissions` 和 `preferences`；页面支持 `area`、`layer_type`、`actions`、页面专属 `resources` 以及裁剪区域 `clips`。

页面的 `items` 与 `layers` 互斥；页面资源的 `images` 与原始 `file`、`data_base64`、`files` 互斥。

动作可以配置 `region`，区域命令支持 `move`、`line`、`quadratic`、`cubic`、`arc` 和 `close`。

二进制资源通常使用相对文件路径；小型内嵌资源也可以使用对应的 `data_base64` 字段，两者不能同时设置。该字段可用于字体、图片、媒体、附件、页面图片、公共资源、自定义标签、封面、扩展数据和版本根文件。

颜色空间的 ICC Profile 可使用 `profile_file` 或 `profile_base64`，并可通过 `profile_name` 指定包内文件名。

`profile_name` 必须是单层文件名，不能包含目录分隔符或路径穿越片段。

输入格式支持 JSON、YAML 和 TOML；使用 `--format auto` 时根据文件扩展名自动判断，标准输入默认按 YAML 解析。输入字段未知、资源路径越出资源根目录或资源文件不存在时，命令会失败并报告 manifest 字段位置。

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
pretty  -max-depth 10 -min-depth 2 --input advanced-features.json -output advanced-features.json
```
