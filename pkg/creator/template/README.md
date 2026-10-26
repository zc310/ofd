# 固定版式数据模板

`github.com/zc310/ofd/pkg/creator/template` 将现有 JSON/YAML 创作清单用作数据模板：
在文字、图片图元上增加 `bind`，在页面上增加 `repeat` 或明细 `paginate`，传入 JSON 数据后生成新的
`creator.Document`。坐标、区域尺寸、字号和 `min_size` 均使用毫米。

## 从已有 OFD 制作模板

先导出布局和资源：

```bash
ofd-creator export -i original.ofd -o invoice-template/template.yaml --asset-root assets
```

编辑 `template.yaml`：

1. 在 `pages[].layers[].items` 或 `pages[].items` 中找到要替换的文字/图片，增加 `bind`。
   原位置、区域、颜色、变换、裁剪与动作继续保留；绑定文字时自动清除原 `value`、
   `text_codes` 和 `cg_transforms`，重新生成字符位置。空字段会移除整个图元。
2. 一个业务字段若由多个文字对象组成，保留一个可绑定文字区域，并删除该字段其余旧图元，
   避免生成结果仍显示或提取出旧文字。
3. 为可变文字配置 `resources.fonts` 中的完整字体，并设置图元的 `font`。
   导出的字体可能是子集；新增字符缺字会明确报错。可新增完整字体资源，保留静态文字的原字体。
4. 需要每条数据一页时，在页面上设置 `repeat: invoices`。固定封面、尾页省略 `repeat`。

导出格式的保留范围沿用 [export 说明](../../../cmd/ofd-creator/README.md#导出-ofd-配置)。
模板采用固定 `pages`，与 `flow` 不同时使用。携带既有 `signatures`/`versions` 的清单会被拒绝，
应在生成新文档后签署。

## 模板与数据

```yaml
version: 1
document:
  id: invoice-template
  page_size: {name: A4}
pages:
  - repeat: invoices
    items:
      - type: text
        x: 30
        y: 40
        width: 130
        height: 10
        size: 4
        bind:
          path: buyer_name
          overflow: shrink
          min_size: 2.8
      - type: text
        x: 130
        y: 80
        width: 40
        height: 10
        size: 4
        bind: {path: total, align: right}
```

```json
{
  "invoices": [
    {"buyer_name": "Alpha & Beta", "total": "1234.50"},
    {"buyer_name": "Other Company", "total": "5678.00"}
  ]
}
```

未指定 `font` 的可变文字使用内嵌 Go Regular，适合拉丁文字。中文需指定包含对应字符的完整
TTF/OTF 字体资源。当前绑定排版支持水平文字，按实际字形推进量计算区域容量。

## 数据路径与缺省值

| 配置 | 含义 |
|---|---|
| `bind.path: buyer.name` | 从当前记录读取嵌套对象字段；固定页面和共享资源中从根对象读取 |
| `bind.path: $.seller.name` | 始终从根数据读取 |
| `bind.path: '@index'` | 当前记录的 0 起序号；固定页为 0 |
| `bind.path: '@number'` | 当前记录的 1 起序号；固定页为 1 |
| `bind.path: '@page'` | 最终物理页码，从 1 开始，仅能用于页面及其注解 |
| `bind.path: '@pages'` | 最终总页数 |
| `bind.path: '@detail_page'` | 当前单据的页码，从 1 开始，仅用于明细分页页面及其注解 |
| `bind.path: '@detail_pages'` | 当前单据的总页数 |
| `bind.required` | 默认 `true`，缺失或 null 字段报错；`false` 时移除该图元 |
| `bind.default` | 缺失或 null 时使用的默认字符串，优先于 `required` |

显式空字符串会移除图元；`false` 和数值 `0` 正常显示。文字字段接受字符串、数字和布尔值。
JSON 数字保留原有十进制表示，不经过 float64；金额可直接提供格式化的十进制字符串。
路径只支持点分隔对象键，无表达式求值、插值或数组索引。

共享 OFD 模板页、复合图元和图案中的绑定使用根数据。每条记录不同的内容应放在页面自身。

## 文字容纳规则

| 字段 | 规则 |
|---|---|
| `overflow: error` | 默认；单行内容宽度或高度超出时报错 |
| `overflow: wrap` | 按 Unicode 换行边界在固定区域内排版；显式换行也生效；不可拆分的长词超宽时报错 |
| `overflow: shrink` | 单行文字缩小到能容纳的字号，必须配置 `min_size`；最小字号仍放不下时报错 |
| `overflow: ellipsis` | 单行超宽时按字素簇截断并添加 `…`；区域连省略号都放不下时报错 |
| `align` | `left`（默认）、`center`、`right`，逐行应用 |
| `max_lines` | 仅用于 wrap；0 表示仅按区域高度限制 |
| `line_height` | 仅用于 wrap；默认 1.2 倍字号，实际步进至少容纳字体的上升部和下降部 |

## 图片绑定

图片沿用原图元区域和变换；按区域尺寸显示。绑定后清除旧 `resource_id`、替代图片和蒙版引用，
注册新图片资源。字段值可以是资源路径字符串，或以下对象：

```json
{"file": "logos/customer.png", "format": "PNG"}
```

也可使用 `{"data_base64":"...","format":"PNG"}`，与 `file` 互斥。
路径以模板目录或 `--asset-root` 为基准，拒绝绝对路径、反斜杠和路径穿越。

## 重复页与引用

`pages[].repeat` 从根数据读取数组，每条记录生成一个页面；空数组跳过该原型页。
全部原型展开后无页面时报错。默认最多 10000 页，可通过 CLI 的 `--max-pages` 或库选项修改。

页面资源在展开前提升为共享资源，避免重复注册字体、图片等 ID。图元 ID 由创建器重新分配。
页面注解跟随重复页复制，新增副本使用新的批注 ID。

跳转页索引以原始清单为基准：自链接指向当前页面副本；其他链接、大纲和书签指向目标原型的
第一个副本。指向因空数组被移除的页面时报错。

## 明细表分页

`pages[].paginate` 将当前记录的明细数组按区域高度生成多页，可与 `repeat` 组合，
实现多张单据各自分页。示例片段：

```yaml
pages:
  - repeat: invoices
    paginate:
      path: lines
      area: {x: 20, y: 50, width: 170, height: 120}
      size: 3.5
      row_height: 8
      columns:
        - {title: '#', width: 12, bind: {path: '@number'}}
        - {title: Description, width: 118, bind: {path: description}}
        - {title: Amount, width: 40, bind: {path: amount, align: right}}
    variants:
      last:
        detail_area: {x: 20, y: 50, width: 170, height: 90}
        items:
          - type: text
            x: 120
            y: 155
            width: 70
            height: 10
            size: 4
            bind: {path: amount, aggregate: record_sum, align: right}
    items:
      - type: text
        x: 120
        y: 180
        width: 70
        height: 10
        size: 4
        bind: {path: amount, aggregate: page_sum, align: right}
```

业务数据中的每张单据提供 `lines` 数组，行记录包含 `description` 和 `amount`。
`paginate.path` 相对于当前单据，`$.` 前缀读取根对象中的明细数组。

| 字段 | 规则 |
|---|---|
| `area` | 包含表头的矩形区域，x/y/width/height 均为毫米 |
| `columns` | 列宽必须大于两倍 padding，列宽之和必须等于 area.width |
| `columns[].bind` | 复用文字绑定，路径相对于当前明细行，默认 overflow 为 wrap |
| `font` / `size` | 明细及表头使用的完整字体资源；默认 Go Regular、3.5mm |
| `header_height` | 每页重复表头的高度；默认按文字计算且至少 8mm |
| `row_height` | 最小行高，默认 8mm；文字换行会增高整行 |
| `padding` | 默认 1.5mm，可显式设为 0 |
| `line_width` | 默认 0.2mm |
| `border_color` / `header_fill` | 网格与表头背景颜色，默认中性灰和浅灰 |

行高使用实际字体度量和断行结果，试排与输出共用字符位置。单行整体转页，不拆分到两页；
单行在所需版式中仍放不下时，报告明细行和列的位置。每页重新绘制表头和网格。
列内 `@index` / `@number` 是整张单据的行序号，跨页连续，新单据重新开始。
列内不能使用页码或 aggregate，避免最终页数反过来影响行高和分页。

空明细数组生成一页空表及零合计；缺失或 null 数组报错。最终物理页数统一受 `MaxPages` /
`--max-pages` 限制。分页表生成标准路径和文字图元，导出后仍可作为普通创作清单再次生成。

## 首页、续页与末页

`pages[].variants` 可配置 `first`、`continuation`、`last`、`single`：

- 多页单据依次选择 first、零个或多个 continuation、last。
- 单页单据选择 single；未提供时依次回退到 last、first、公共页面。
- 多页末页未提供 last 时回退到 continuation；未提供的其他版式使用公共页面。
- 每个版式的 `templates`、`items` 或 `layers` 追加到公共页面，items 与 layers 互斥。
- `detail_area` 可改变明细区的位置、高度，宽度保持与公共 area 相同。

分页会先检查剩余行能否放入末页，并按末页预留空间决定分配。可以把整单合计、签字栏等
放在 last 和 single 的附加内容中。如果某一行只能放入首页/续页，而末页只能容纳表头与合计，
允许生成独立的合计末页。`@page` / `@pages` 是整个文件的物理页码，
`@detail_page` / `@detail_pages` 是每张单据自己的页码。

## 本页小计与整单合计

文字绑定可配置 `aggregate: page_sum` 或 `record_sum`，`path` 指向明细行的数值字段。
`decimal_places` 控制显示小数位（0—18，默认 2）。先使用精确十进制求和，最后统一四舍五入；
正负半单位均向远离零的方向舍入，不经过 float64。例如 `0.004 + 0.004` 显示为 `0.01`。

字段接受 JSON 数字或未加千分位、货币符号的十进制字符串，可含科学计数法。
文本最长 4096 字节、指数范围 -1000—1000。缺失/null 默认报错；`required: false` 时跳过，
`default` 可为缺失金额提供默认值。空数组结果为零，显式空字符串金额会报错。
汇总仅用于 paginate 页面及其注解；record_sum 按当前单据独立计算。

完整四页示例见
[template-invoice-details.yaml](../../../cmd/ofd-creator/examples/template-invoice-details.yaml) 与
[template-invoice-details-data.json](../../../cmd/ofd-creator/examples/template-invoice-details-data.json)，
覆盖首页、续页、末页、单页和长描述换行。

## Go API

```go
tmpl, err := template.Load("invoice-template/template.yaml")
if err != nil {
    return err
}
data, err := os.Open("data.json")
if err != nil {
    return err
}
defer data.Close()
document, err := tmpl.Render(data, template.Options{MaxPages: 1000})
if err != nil {
    return err
}
return creator.CreateFile(document, "invoices.ofd")
```

内存使用 `template.Parse(data, "yaml" 或 "json", baseDir)`。`Options.LoadAsset` 可加载内存资源，
`AssetRoot` 可覆盖资源根目录，`StreamAssets` 可为静态文件资源使用惰性来源。
每次 Render 使用独立清单副本，同一模板可并发渲染不同数据。

CLI 入口、退出码和可运行示例见 [ofd-creator 数据模板](../../../cmd/ofd-creator/README.md#数据模板与重复页面)。
