# 更新日志

本文件只记**已发布 API 的破坏性变更**。

组件的用法、选项与接口形态写在各自的 `README.md`——例如
[`cmd/ofd-server/README.md`](cmd/ofd-server/README.md) 是该服务的完整文档。
日常历史见 `git log`，提交信息使用 Conventional Commits。

未发布周期内新增的组件（例如 `ofd-server`）不算破坏性变更：它从未发布过，
没有可破坏的使用者。

## 未发布

- `pkg/converter` 增加图片导入 OCR 开关和语言选项；`ofd-converter` 提供 `--ocr` 与 `--ocr-language`，默认不调用 Tesseract。

### 破坏性变更

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
