# 阅读器内部支持

`pkg/reader` 提供公共文档读取接口；本目录承载阅读器应用的内部支持，不作为公共 Go API。

| 包 | 职责 |
|----|------|
| `contract` | 浏览器与桌面共用的前端字段映射及空数组语义 |
| `preview` | 图片格式、大小和尺寸检查，静态格式转换、缩略图及 OFD 印章光栅化 |
| `session` | 文档生命周期、并发限制、取消与关闭等待、权限复核、当前文档管理和页面 HTTP 适配 |

会话层返回业务类型，不依赖 `contract`；WASM、桌面适配层在传输前完成字段转换。
`preview` 不持有父文档会话，也不代替父文档权限检查；资源读取、权限复核和并发限制由调用方负责。
不同用途的格式白名单保持独立：印章大图只接受 PNG/JPEG 或受限 OFD，资源预览还可将其他受支持的光栅格式转换为静态 PNG。

`session/session.go` 只负责生命周期；底层文档接口、元数据、扫描、附件和签名按职责分文件。
页面和 PDF 渲染共用有界执行入口：排队与执行同时监听请求取消和会话关闭，底层迟到的成功结果仍按取消返回。
取消或失败时已写入 `io.Writer` 的产物可能不完整，调用方必须丢弃；关闭会等待底层操作退出后才释放文档。

验证：

```sh
go test ./internal/reader/... ./cmd/ofd-viewer-wails -count=1
go test -race ./internal/reader/... ./cmd/ofd-viewer-wails -count=1
go vet ./internal/reader/... ./cmd/ofd-viewer-wails
```

涉及 WASM 适配层时，还需按 `cmd/ofd-wasm/AGENTS.md` 验证 WASM 构建、Go 测试和共享网页测试。
