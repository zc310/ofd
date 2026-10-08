# 更新日志

本文件只记**已发布 API 的破坏性变更**。

组件的用法、选项与接口形态写在各自的 `README.md`——例如
[`cmd/ofd-server/README.md`](cmd/ofd-server/README.md) 是该服务的完整文档。
日常历史见 `git log`，提交信息使用 Conventional Commits。

未发布周期内新增的组件（例如 `ofd-server`）不算破坏性变更：它从未发布过，
没有可破坏的使用者。

## 未发布

- OFD 转 PDF 现在会输出可点击的链接注解。此前 PDF 导出完全丢弃链接，转出的 PDF
  视觉上无异常，但所有链接都点不动。

  外部链接（URI）与页面跳转（Goto/Dest）都支持。链接热区取自 OFD 的三处来源：
  页面正文图层图元、页面所用模板页图层，以及页面注解文件中 `Link` 注解的
  `Appearance`；热区用图元或注解 `Appearance` 自身的 `Boundary`。热区图元常写成
  `Visible="false"`——热区只是点击范围，不该画出来——不可见不影响链接导出。宽高
  为零或坐标非有限的边界会被丢弃，负宽高会归一化。

  页面跳转的目标页按输出页序解析，映射在写页之前建好；页 ID 只在文档体内唯一，
  多文档体合并时按文档体分别建立映射。被页码范围裁掉的页不会出现在输出里，指向
  它们的链接被丢弃，而不是写出指向不存在页面的注解。指向排在来源页之前的页同样
  支持。

  附件跳转、声音与影片动作暂不支持：PDF 没有对应物。

  五种目标类型（`Fit`/`FitH`/`FitV`/`XYZ`/`FitR`）与 `Dest@Zoom` 缩放比例均精确
  写入命名目标。

  链接导出只影响 PDF。PNG 等图像输出不含交互信息，PDF 文字提取结果不变
  （链接注解的 `/Contents` 是非打印的悬停提示，`pdftotext` 不参与提取）。

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
