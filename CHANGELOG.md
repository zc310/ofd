# 更新日志

本文件只记**已发布 API 的破坏性变更**。

组件的用法、选项与接口形态写在各自的 `README.md`——例如
[`cmd/ofd-server/README.md`](cmd/ofd-server/README.md) 是该服务的完整文档。
日常历史见 `git log`，提交信息使用 Conventional Commits。

未发布周期内新增的组件（例如 `ofd-server`）不算破坏性变更：它从未发布过，
没有可破坏的使用者。

## 未发布

- `pkg/creator` 的 `Clips` 增加 `TransFlag *bool` 字段，用于写出 OFD
  `Clips@TransFlag`。该属性此前无法表达：`Clips` 在 XSD 中缺少此属性定义，
  写出后会被结构校验判为非法，而真实 OFD（含官方阅读器样例）与
  `internal/models` 的解析侧一直按 `false` 处理。缺省（规范为 `true`）时
  阅读器会把图元 CTM 叠加到裁剪路径上，裁剪路径已在图元局部毫米坐标时会
  被放大 CTM 倍。

  `pdf2ofd` 转换的 PDF 现在对全部裁剪区写 `TransFlag="false"`。

- `pkg/converter` 增加图片导入 OCR 开关和语言选项；`ofd-converter` 提供 `--ocr` 与 `--ocr-language`，默认不调用 Tesseract。

- `ofd-converter` 新增 `--password`，`pkg/converter` 新增 `WithPassword` 选项：
  加密 PDF 输入此前一律报 "encryption setup: please provide the correct
  password"。口令放在 `Converter` 上而非 `Importer` 接口参数上，与
  `SofficePath()`、`ChromePath()` 同一套路——给三个接口都加参数要动二十来处
  实现，并不因此多出任何能力。

  `pdf2ofd` 另导出 `ErrEncrypted` 与 `ErrWrongPassword`：前者补个口令能继续，
  后者是口令给错了、重试同一份不会变，两者要能分开判断。

  OFD 的包级加密（GB/T 33190）仍不支持。`--password` 的值会出现在进程命令行，
  同主机其他用户可从 `ps` 读到。


### 破坏性变更

- `pkg/converter/import/pdf` 与 `internal/pdf2ofd` 的 `Convert`、`ConvertFile`
  新增末位参数 `password string`。

  ```go
  // 之前
  pdf.Convert(ctx, input, output)
  // 现在
  pdf.Convert(ctx, input, output, password)
  ```

  传 `""` 保持原行为。

  口令错误用 `errors.Is` 匹配 pdfcpu 的 `ErrWrongPassword` /
  `ErrOwnerPasswordRequired`，不比对消息文本——库改一次措辞，字符串方案就失效，
  而失效方式是"加密文档不再被识别成加密文档"。

- `pkg/converter/import/pdf` 的 `Convert` 与 `ConvertFile` 新增首个参数
  `context.Context`。

  ```go
  // 之前
  pdf.Convert(input, output)
  pdf.ConvertFile(pdfPath, ofdPath)
  // 现在
  pdf.Convert(ctx, input, output)
  pdf.ConvertFile(ctx, pdfPath, ofdPath)
  ```

  调用方传 `context.Background()` 即可保持原行为。

  起因是 pdfcpu 0.16 给 `api.ReadContext`、`api.ValidateContext`、`PageDict`
  等函数都加了 `context.Context` 首参。PDF→OFD 的读取与校验两步在损坏或超大
  文件上本身就耗时很久，能在那里被取消比事后检查 `ctx.Err()` 有用。PDF 导入器
  从注册表收到 `*converter.Converter`，直接传 `conv.Context()`，与包内其余入口
  的取消语义一致。

- `pkg/converter` 的转换入口新增首个参数 `context.Context`。

  ```go
  // 之前
  converter.Convert("ofd", "pdf", input, output, opts...)
  // 现在
  converter.Convert(ctx, "ofd", "pdf", input, output, opts...)
  ```

  涉及 `Convert`、`Encode`、`EncodeDocuments`，以及各格式的 `PDF`/`Text`/
  `Markdown`/`HTML`/`Image` 与它们的 `*Document(s)` 变体。`Encoder`、
  `Importer`、`Transformer` 三个接口的签名未变。

  调用方传 `context.Background()` 即可保持原行为；若能传入带超时的
  `context`，大文档在页与页之间即可被取消，不必渲染完全部页。

- `ofd-converter` 在用户按 `Ctrl-C` 时以退出码 `130` 退出（原为 `0` 或 `1`）。
  脚本若依赖退出码判断成败，需要把 `130` 也视为"用户主动停止"。用法见
  [`cmd/ofd-converter/README.md`](cmd/ofd-converter/README.md)。
