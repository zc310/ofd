# 更新日志

本文件只记录已发布接口的破坏性变更，包括 Go API、CLI 退出码、错误处理和输出契约，
每项说明升级时需要调整的内容。未发布周期内新增接口的调整不单独记录。

功能与用法见各组件的 `README.md`，完整历史见 `git log`。

## 未发布

- **创建器验签报告文本**：`--verify-signatures` 将「摘要有效」改为「摘要匹配」，
  将「密码学签名有效」改为「两层密码学签名通过」，并追加完整验签状态及失败或
  未完成原因。解析标准错误文本的脚本需调整匹配规则；业务接受签章应使用
  [`sign.Verify`](pkg/sign/README.md#完整验证) 的 `Result.FullyValid()`。

- **创建器移除 TOML**：`ofd-creator` 的输入、`export` 和 `export-all` 仅支持 JSON、
  YAML，`--format toml` 及自动识别的 `.toml` 清单路径会返回参数错误（退出码 `2`）。
  原有 TOML 清单需转换为 YAML 或 JSON；渐变示例已改为
  [`gradients.yaml`](cmd/ofd-creator/examples/gradients.yaml)。导出时未指定格式且输出文件
  使用其他扩展名也会报错；请使用 `.json`、`.yaml`、`.yml` 或显式指定格式。

- **转换命令退出码**：`ofd-converter` 的参数、输入路径、输出格式及取值范围错误
  从退出码 `1` 改为 `2`。依赖退出码的脚本需区分参数错误与转换失败；单个文件
  转换失败仍为 `1`，用户取消为 `130`。详见
  [CLI 说明](cmd/ofd-converter/README.md)。

- **发票错误与 JSON 输出**：`invoice.Extract` 对“有附件但不是发票”的文档改为返回
  `ErrNotInvoice`，不再返回空壳 `Invoice`。调用方需用 `errors.Is` 区分
  `ErrNotInvoice` 与表示缺少附件的 `ErrNoAttachment`。
  `ofd-invoice` 的单输入 JSON 输出仍为 `Invoice`；多个输入或启用 `--skip-broken`、
  `--dedup` 时改为 `{items, skipped, summary}`。详见
  [发票工具说明](cmd/ofd-invoice/README.md)。

- **PDF 导入资源限制**：PDF 导入默认启用输入大小、页数、对象数、解压数据、图片、
  字体、输出图元及处理量预算，超过限制会返回错误。需要处理更大文档时，通过
  `converter.WithPDFImportOptions`、PDF 包的 `ConvertWithOptions` 或 CLI 对应选项
  调整；当前默认值与配置方式见 [PDF 支持说明](docs/PDF-SUPPORT.md)。

- **创建器 XML 前缀**：`pkg/creator` 自建元素统一输出 `ofd:` 前缀，并声明
  `xmlns:ofd="http://www.ofdspec.org/2016"`。按 XML 字节或标签前缀匹配的调用方
  需调整，建议按命名空间 URI 与本地名匹配。扩展 `DataXML` 和直接提供的资源 XML
  保留原有前缀与命名空间。

- **多版本文件路径**：`pkg/creator` 生成的版本 `BaseLoc`、文件清单和 `DocRoot`
  改为包内绝对路径，例如 `/Doc_0/Document.xml`。读取这些字段的调用方需支持
  从包根目录解析；校验器现在也会检查版本入口和文件引用，旧产物可能新增缺失引用
  错误。详见 [签名与版本说明](pkg/creator/README.md#签名与版本)。

## 已发布

### v0.1.4

- **转换 API 增加上下文**：`pkg/converter` 的转换入口增加首个参数
  `context.Context`，涉及 `Convert`、`Encode`、`EncodeDocuments`，以及各格式的
  `PDF`、`Text`、`Markdown`、`HTML`、`Image` 和对应的 `*Document(s)` 变体。
  原有调用需补入上下文，例如：

  ```go
  // 之前
  converter.Convert("ofd", "pdf", input, output, opts...)
  // v0.1.4 起
  converter.Convert(context.Background(), "ofd", "pdf", input, output, opts...)
  ```

- **PDF 导入 API 增加上下文和口令**：`pkg/converter/import/pdf` 的 `Convert`、
  `ConvertFile` 增加首个参数 `context.Context` 和末位参数 `password string`。
  不需要取消或口令时分别传 `context.Background()` 和 `""`：

  ```go
  // 之前
  pdf.Convert(input, output)
  pdf.ConvertFile(pdfPath, ofdPath)
  // v0.1.4 起
  pdf.Convert(context.Background(), input, output, "")
  pdf.ConvertFile(context.Background(), pdfPath, ofdPath, "")
  ```

- **取消退出码**：`ofd-converter` 在用户按 `Ctrl-C` 时以 `130` 退出，原为 `0` 或 `1`。
  脚本需将 `130` 识别为用户取消。
