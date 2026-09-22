# ofd-invoice

`ofd-invoice` 是 OFD 电子发票信息抽取工具，从增值税电子发票 OFD 文件中读取结构化附件
（通常为 `Doc_0/Attachs/original_invoice.xml`）并输出 JSON，适合财务入账、对账和自动化处理。

使用本工具处理文档前，请阅读项目根目录的 [免责声明](../../DISCLAIMER.md)。抽取结果来自
文件内容本身，不代表文件真实、完整、可信或具有法律效力。

## 功能

- 优先读取 OFD 包内嵌的发票结构化附件（税控开具的电子普票/专票通常自带）。
- 规范路径 `Doc_0/Attachs/original_invoice.xml` 不存在时，自动扫描所有名称含 `attach`
  的 XML 条目，取第一个能解析出有效发票数据的文件；命名空间差异不影响字段匹配。
- 抽取字段包括：发票标题、类型、机器编号、代码、号码、开票日期、校验码、不含税金额、
  税额、价税合计（小写与大写）、税控码、收款人/复核人/开票人、购销方资料（名称、税号、
  地址电话、开户账号）以及逐行价税明细（名称、规格、单位、数量、单价、金额、税率、税额）。
- 金额、数量、税率按十进制精确表示（基于任意精度有理数），税率兼容 `13%`、`13` 和 `0.13`
  三种写法并归一化为小数。
- 页面文字层只用于补齐票面标题与价税合计大写金额，并按标题推断类型（普通/专用/通行费）；
  页面读取失败不中断附件抽取，只记录警告。
- 附件缺失或没有可用的发票结构化附件时返回错误；不处理 PDF 等非 OFD 输入。

## 构建

在项目根目录执行：

```bash
go build -o ofd-invoice ./cmd/ofd-invoice
```

## 快速开始

命令格式：

```text
ofd-invoice [选项] input.ofd
```

```bash
# 输出紧凑 JSON 到标准输出
ofd-invoice invoice.ofd

# 输出缩进后的 JSON
ofd-invoice --pretty invoice.ofd

# 写入文件
ofd-invoice -o invoice.json invoice.ofd
ofd-invoice --pretty -o invoice.json invoice.ofd
```

未指定 `-o` / `--output` 时输出到标准输出；输出路径为 `-` 同样表示标准输出。
工具不允许输出文件覆盖输入的 OFD 文件。JSON 字段与 `github.com/zc310/ofd/pkg/invoice`
的 `Invoice` 模型一致，字段缺失时对应值为空或不出现，供脚本稳定处理。

## 命令选项

| 选项                      | 默认值   | 说明                                   |
|---------------------------|----------|----------------------------------------|
| `-o`, `--output PATH`     | 标准输出 | JSON 输出路径，使用 `-` 输出到标准输出 |
| `--pretty`                | 关闭     | 缩进 JSON 输出                         |
| `-h`, `--help`            | 关闭     | 显示命令帮助                           |

## 退出码

- `0`：抽取成功，结果已输出。
- `1`：抽取或输出失败（例如未找到发票结构化附件、解析失败、写出文件失败）。
- `2`：命令参数或输入输出配置无效（缺少输入、参数过多、输出覆盖输入等）。

## CI 示例

```bash
ofd-invoice --pretty -o invoice.json invoice.ofd
status=$?
test "$status" -eq 0
```