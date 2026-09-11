# OFD WASM

OFD WASM 将 OFD 文档解析和渲染能力提供给浏览器 JavaScript 使用。

浏览器阅读器示例和可直接部署的 Web 页面位于 [`web`](web) 目录。

## 快速开始

在仓库根目录执行：

```bash
make build-wasm
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

然后打开 <http://localhost:8080>，选择一个 `.ofd` 文件即可开始阅读。

浏览器不能通过 `file://` 直接加载 WASM，运行阅读器时需要使用 HTTP 服务。完整的阅读器功能、WASM API、Worker 协议和部署说明见 [`web/README.md`](web/README.md)。
