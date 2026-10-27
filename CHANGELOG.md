# 更新日志

本文件只记录已发布接口的破坏性变更，包括 Go API、CLI 退出码、错误处理和输出契约，
每项说明升级时需要调整的内容。未发布周期内新增接口的调整不单独记录。

功能与用法见各组件的 `README.md`，完整历史见 `git log`。

## 未发布

## 已发布

### v0.1.6

- **阅读器内容操作开始执行文档权限**：`webreader.Reader.Text()`、`RenderPDF/RenderPDFTo()`、
  `AttachmentData()` 在 `Permissions/Export=false` 时返回可用 `errors.Is` 判断的
  `webreader.ErrPermissionDenied`；混合选页导出在输出前整体检查。WASM 的文字、附件、
  资源下载及流式导出对应返回 `permission_denied`，调用方需处理同步错误或 Promise 拒绝。
  保存图片、准备打印时应分别设置 `RenderOptions.Purpose`（JS 为 `purpose`）为
  `export` / `print`；文档内媒体播放需向 `ofd.mediaData` 传第四个参数 `'preview'`。
  默认图片渲染仍用于页面预览，详见 [文档权限](cmd/ofd-wasm/web/README.md#文档权限)。
  受限页面的搜索结果 `Text` 仅包含查询词，`Start/End` 相对该词，命中坐标仍保留；
  调用方应使用坐标定位，不再依赖受限页面的完整上下文。

- **校验器 PDF 报告 API 移至报告子包**：`validator.RenderPDF` 和 `validator.PDFOptions`
  移至 `pkg/validator/report`，改用
  `report.RenderPDF(ctx, writer, result, report.Options{Font: fontPath})`。
  OFD/PDF 统一使用 flow 排版，PDF 由 OFD 转换；调用方需要更新导入路径并传入上下文。

- **阅读器搜索 API 改用上下文、选项和报告**：`webreader.Reader.Search` 从
  `Search(query string) ([]SearchResult, error)` 改为
  `Search(ctx context.Context, query string, options SearchOptions) (SearchReport, error)`。
  调用方需传入上下文及 `webreader.SearchOptions{}`，并从 `report.Results` 取得命中列表。
  `MaxPages`、`MaxResults` 为 `0` 时不限制；设置上限后需检查 `report.Truncated` 和
  `report.Scanned`，不能将截断结果当作完整搜索结果。取消或超时返回原始 `ctx.Err()`。

- **WASM 搜索改为异步报告**：`ofd.search(query, requestID?)` 现在返回 Promise，解析值从
  命中数组改为 `{results, scanned, truncated}`；Worker 的 `search` 响应也采用该报告结构。
  直接调用方需使用 `await`，从 `report.results` 读取命中并处理 Promise 拒绝。
  单次搜索最多扫描 `2000` 页、返回 `5000` 个命中，需通过 `truncated` 判断结果是否完整；
  提供请求 ID 时可通过 `ofd.cancelRender(requestID)` 取消搜索。

- **阅读器读取失败不再作为空结果返回**：文字、搜索、链接和页面动作、字体及媒体资源查询、
  统计和 PDF 链接导出会传播页面、模板、注释或资源读取错误。调用方需处理返回错误，
  不再依赖空列表或缺项来判断读取失败；PDF 导出失败后应丢弃部分输出。

- **移除 TinySkia 栅格后端**：`ofd-converter --raster-backend tinyskia` 和
  `converter.RasterBackend("tinyskia")` 不再可用。请改用 `canvas`（默认）或 `ftgg`；
  删除该参数可恢复默认 Canvas 渲染。

- **Markdown 独立转换入口增加上下文参数**：`pkg/converter/import/markdown` 的
  `Convert` 改为 `Convert(ctx context.Context, input any, output io.Writer) error`，
  `ConvertFile` 改为 `ConvertFile(ctx context.Context, mdPath, ofdPath string) error`。
  调用方需在首参传入上下文；不需要取消控制时可用 `context.Background()`。

- **转换失败与取消错误**：Text、Markdown、DOCX 遇到损坏或缺失的页面时返回包含页码
  的错误，原先依赖空结果判定读取失败的调用方应处理返回错误。Office、HTML 的取消与
  超时返回标准上下文错误，依赖错误文案的调用方应改用 `errors.Is` 判断
  `context.Canceled` 或 `context.DeadlineExceeded`。

- **阅读器渲染接口增加上下文参数**：`webreader.Reader.RenderPage` 改为
  `RenderPage(ctx context.Context, index int, options RenderOptions) ([]byte, error)`；
  `RenderPages` 改为
  `RenderPages(ctx context.Context, indices []int, options RenderOptions) ([][]byte, error)`；
  `RenderPDF` 改为
  `RenderPDF(ctx context.Context, indices []int, options RenderOptions) ([]byte, error)`；
  `RenderPDFTo` 改为
  `RenderPDFTo(ctx context.Context, output io.Writer, indices []int, options RenderOptions) error`。
  调用方需在首个参数传入上下文；不需要取消控制时可用 `context.Background()`。
  取消或超时返回原始 `ctx.Err()`，失败后应丢弃部分 PDF 输出。

- **WASM 页面渲染改为异步**：直接调用 `ofd.renderPage`、`ofd.renderPages` 和单页
  `ofd.renderStream` 时，调用方需使用 `await` 获取结果，并处理渲染失败时的 Promise
  拒绝。Worker 协议保持原有结果格式，已运行的页面请求也会在渲染阶段边界响应取消。
  内部取消控制接口统一为 `ofd.cancelRender(requestID)`，替代 `cancelStream`。

- **签署 API 增加上下文参数**：`sign.Sign` 改为
  `Sign(ctx context.Context, input any, output io.Writer, options Options) error`。
  调用方需在首个参数传入上下文；不需要取消控制时可用 `context.Background()`。
  外部签名进程现在随上下文取消，调用方可用 `errors.Is` 判断取消或超时错误。

- **加密输入的错误与报告**：具有根目录解密入口的 OFD 在解析/转换/验签或引用闭包分析时
  返回可用 `errors.Is(err, crypt.ErrLocked)` 判断的错误，不再将占位明文当成原文处理。
  analyzer 对可见加密元数据返回 `partial` 报告及该错误；validator 对未解锁内容报告
  `incomplete`，没有其他错误时总状态为 `partial`，CLI 退出 `1`。依赖旧解析错误文本、
  `invalid/failed` 状态或成功处理占位内容的调用方需改用锁定诊断，详见
  [`pkg/crypt`](pkg/crypt/README.md)。

### v0.1.5

- **缩略图输出约束**：`ofd-thumbnailer` 的长边像素限定为 `1` 到 `4096`，输出始终为
  PNG 编码。原先传入更大尺寸的脚本需降低尺寸；使用 `.jpg` 等输出文件名的脚本需
  按 PNG 内容调整文件名及后续处理。OFD 页面尺寸或比例无法在像素预算内渲染时返回失败。

- **回退字体注册额度**：canvas 后端的全局回退字体注册最多保留 64 个字体族，
  各样式当前源数据合计最多 256 MiB。`webreader.RegisterFallbackFont`、WASM
  `ofd.addFallbackFont` 等入口超限时返回错误，已有注册继续可用；长会话按需调用
  `webreader.UnregisterFallbackFont` 注销不再使用的字体，仍被文档持有时等最后一个
  引用释放后归还额度。OFD 内嵌字体不受此注册额度限制。
  WASM `ofd.removeFallbackFont` 现在也注销全局来源以归还额度，重新启用需再次提供字体数据。

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

- **XML 写入前缀**：`pkg/creator` 自建元素、`pkg/merge` 生成的主入口和重写的签名、资源 XML，
  以及 `pkg/preserve` 重写的 XML 中，OFD 元素统一输出 `ofd:` 前缀，并声明
  `xmlns:ofd="http://www.ofdspec.org/2016"`。按 XML 字节或标签前缀匹配的调用方
  需调整，建议按命名空间 URI 与本地名匹配。扩展 `DataXML` 和直接提供的资源 XML
  保留原有前缀与命名空间。

- **多版本文件路径**：`pkg/creator` 生成的版本 `BaseLoc`、文件清单和 `DocRoot`
  改为包内绝对路径，例如 `/Doc_0/Document.xml`。读取这些字段的调用方需支持
  从包根目录解析；校验器现在也会检查版本入口和文件引用，旧产物可能新增缺失引用
  错误。详见 [签名与版本说明](pkg/creator/README.md#签名与版本)。

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
