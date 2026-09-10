# ofd-creator

`ofd-creator` 根据 JSON 或 YAML manifest 创建 OFD 文件包。

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

## Manifest 示例

```yaml
version: 1

document:
  id: demo-document
  title: 示例文档
  author: creator
  pageSize:
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
        resourceId: 100
```

字体资源支持 `ttf`、`otf` 和 `ttc`。对于包含多字体 CFF 的 TTC，创建器会提取第一个字体面并保留其完整字形数据，不进行不安全的 CFF 子集化，以保证阅读器能够正常加载。

支持以下页面图元：

- `text`
- `path`
- `image`
- `composite`
- `page-block`

还支持 `drawParams`、`colorSpaces`、`templates`、`layers`、`ctm`、`fillColor`、`strokeColor`、`textCodes`、`cgTransforms`、渐变和 Pattern 填充。

完整的 TOML 渐变实例见 `cmd/ofd-creator/examples/gradients.toml`，包含轴向、径向、Gouraud、LaGouraud 和 Pattern 填充。

楷体文字样式实例见 `cmd/ofd-creator/examples/kaiti-styles.yaml`，包含多种字号、字重、斜体、描边和字宽设置。

线条样式 JSON 实例见 `cmd/ofd-creator/examples/line-styles.json`，包含线宽、端点、连接、虚线、偏移、折线和透明度效果。

两页路径填充规则实例见 `cmd/ofd-creator/examples/path-fill-rules.json`，包含默认规则、`NonZero`、`Even-Odd`、嵌套轮廓、星形、多孔图形和重叠路径。

现代信息卡片 / 活动海报样式实例见 `cmd/ofd-creator/examples/modern-info-card.yaml`，包含背景层、内容卡片、进度条、标签卡和页脚信息。

图片图元的 `ctm` 会直接写入图片变换矩阵；跳转动作还支持 `left`、`top`、`right`、`bottom` 和 `zoom` 定位参数。

`pageSize` 提供 `width` 和 `height` 时优先使用显式尺寸；只有在两者都未设置时，才支持使用 `name: A4`。其他名称需要同时提供宽高。

文字图元的 `fill` 可以省略、设置为 `true` 或设置为 `false`。省略时不会强制输出 `Fill` 属性。

文档级 `actions`、`bookmarks`、`outlines`、`attachments`、`extensions`、`signatures` 和 `versions` 也可以直接在 `document` 下声明。签名值和 `sealFileBase64` 使用 Base64 字符串，附件、签名文件和版本根文件使用资源相对路径。

文档元数据还支持 `creationDate`、`modDate`、`keywords`、`customData`、`cover`、`area`、`defaultCS`、`permissions` 和 `preferences`；页面支持 `area`、`layerType`、`actions`、页面专属 `resources` 以及裁剪区域 `clips`。

页面的 `items` 与 `layers` 互斥；页面资源的 `images` 与原始 `file`、`dataBase64`、`files` 互斥。

动作可以配置 `region`，区域命令支持 `move`、`line`、`quadratic`、`cubic`、`arc` 和 `close`。

二进制资源通常使用相对文件路径；小型内嵌资源也可以使用对应的 `dataBase64` 字段，两者不能同时设置。该字段可用于字体、图片、媒体、附件、页面图片、公共资源、自定义标签、封面、扩展数据和版本根文件。

颜色空间的 ICC Profile 可使用 `profileFile` 或 `profileBase64`，并可通过 `profileName` 指定包内文件名。

`profileName` 必须是单层文件名，不能包含目录分隔符或路径穿越片段。

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
