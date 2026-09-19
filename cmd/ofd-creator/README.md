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
