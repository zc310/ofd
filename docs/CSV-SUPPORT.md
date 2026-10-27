# CSV / TSV 转 OFD 支持清单

`pkg/converter/import/csv` 注册 `csv`、`tsv` 两个导入器。转换使用 Go 的 CSV 解析器、
`creator/flow` 表格排版和 OFD 写出，不需要 LibreOffice、Chrome 或 OCR。
默认按 A4 纵向、20mm 页边距排版，使用 Material 表格样式，显示标题和页码。

## 已支持

| 能力 | 行为 |
|---|---|
| 输入 | 文件路径、`[]byte`、`io.Reader`；路径按 `.csv` / `.tsv` 识别，其他输入显式指定格式。 |
| 分隔符 | CSV 固定为逗号，TSV 固定为制表符；不自动猜测分隔符。 |
| 引号 | 双引号包围字段，字段内双引号写为 `""`；支持引号内分隔符及换行。 |
| 行结束 | LF、CRLF；CRLF 按 Go CSV 解析器归一为 LF，包括带引号的字段内部。空行跳过。 |
| 编码 | 默认 UTF-8，接受文件头 BOM；GB18030 / GBK 通过选项明确指定。 |
| 字段内容 | 按文字保留空字段、空格、前导零、小数位及公式文本，不执行公式，不推断数值。 |
| 表头 | 默认首条记录为表头，在续页重复；可关闭，首条记录作为普通数据。 |
| 列宽与分页 | 按内容度量自动分配列宽；单元格自动换行，长行可按完整文字行跨页续排。 |
| 标题 | 默认使用文件名（不含扩展名）；字节或 Reader 输入使用 CSV / TSV，可显式覆盖。 |
| 页面 | 使用公共纸张和方向选项，可设置横向及自定义尺寸；默认 A4。 |
| 字体 | 纯拉丁文本使用内置 Go 字体；超出覆盖范围时查找系统中文字体，或显式提供 TTF/OTF/TTC。按实际使用字符子集化嵌入，阅读时不依赖源机器字体。 |
| 输出 | OFD；也可经统一转换链输出 PDF、文本等文档格式。 |

## 限制与失败处理

- 每条记录必须与首条记录列数一致；格式错误返回解析位置，不补列或丢弃多余字段。
- 不支持分号 CSV、Excel 的 `sep=...` 提示行、编码自动识别及 UTF-16。Excel 导出时可选
  “CSV UTF-8”；其他编码请先转换或明确选择 GB18030 / GBK。
- 输入最多 32MiB、100,000 条记录（含表头）、256 列、1,000,000 个单元格；字体最多 64MiB。
  这些是输入规模限制，不等于峰值内存上限。
- 不自动拆成多组横向列。过宽的表格或不可断长文本可能排版失败，可尝试 `--landscape`
  或更大的 `--paper`；需要定制列宽、字号或业务模板时使用 creator/flow。
- TTC 使用第一个字体面。显式字体缺少字符时可能由内置 Go 字体补齐拉丁字符，仍缺字则报错。
- 中文自动查找依赖系统字体环境；无可用字体时须指定 `--csv-font`，Go 调用方也可提供 `FontData`。
- 空文件、格式错误、字体错误或排版失败发生在写出前。CLI 文件输出成功后才替换目标，失败保留
  已有文件。取消原样返回 `ctx.Err()`；直接写流失败时调用方应丢弃部分产物。

## 命令行

```bash
ofd-converter testdata/csv/inventory.csv /tmp/inventory.ofd
ofd-converter --landscape testdata/csv/inventory.tsv /tmp/inventory-wide.ofd
ofd-converter --csv-no-header --csv-title "设备台账" data.csv /tmp/table.ofd
ofd-converter --csv-encoding gb18030 --csv-font fonts/cjk.ttf data.csv /tmp/table.ofd
ofd-converter --paper A3 --landscape data.tsv /tmp/table.pdf
```

前两条示例含中文，需要本机可用中文字体，或追加 `--csv-font fonts/cjk.ttf`。
命令行参数、批量转换及退出码见 [ofd-converter](../cmd/ofd-converter/README.md)。

## Go API

```go
import (
    "github.com/zc310/ofd/pkg/converter"
    _ "github.com/zc310/ofd/pkg/converter/import/csv"
)

err := converter.Convert(ctx, "csv", "ofd", input, output,
    converter.WithCSVImportOptions(converter.CSVImportOptions{
        Title: "设备台账",
        Encoding: "utf-8",
        FontFile: "fonts/cjk.ttf",
    }),
    converter.WithLandscape(true),
)
```

`ctx` 为 `context.Context`，`output` 为 `io.Writer`；TSV 将 `from` 改为 `"tsv"`。
不需要定制时可省略选项。可运行示例见 [ExampleConvert_csv](../pkg/converter/examples_test.go)。

## 验证

```bash
go test ./pkg/converter/import/csv ./pkg/converter ./cmd/ofd-converter -count=1
go run ./cmd/ofd-validator --mode strict --format text /tmp/inventory.ofd
```
