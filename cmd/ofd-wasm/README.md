# OFD WASM

OFD WASM 将 OFD 文档解析和渲染能力提供给浏览器 JavaScript 使用。

使用阅读器处理不可信或敏感文档前，请阅读项目根目录的 [免责声明](../../DISCLAIMER.md)。在线示例不替代生产环境的文件安全、权限控制和隐私保护措施。

浏览器阅读器示例和可直接部署的 Web 页面位于 [`web`](web) 目录。

## 截图

![OFD WASM 网页阅读器](../../docs/screenshots/wasm/webreader_1.png)

![OFD WASM 网页阅读器 双页模式](../../docs/screenshots/wasm/webreader_2.png)

示例使用 `worker.js` 将解析和渲染放在 Web Worker 中；页面使用 `IntersectionObserver` 按可视区域附近懒加载，并分别缓存正文页和缩略图的 Blob URL。工具栏支持 50% 到 300% 缩放；默认正文按 72 DPI 渲染并在缩放时使用 CSS 放大，也可以开启“清晰度优先”按缩放比例重新渲染正文，缩略图保持 15 DPI。底层 `renderPage` 和 `renderPages` API 支持通过 `format` 配置输出 PNG、JPG 或 SVG，默认输出 PNG。


## 快速开始

在仓库根目录执行：

```bash
make build-wasm
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

然后打开 <http://localhost:8080>，选择一个 `.ofd` 文件即可开始阅读。

浏览器不能通过 `file://` 直接加载 WASM，运行阅读器时需要使用 HTTP 服务。完整的阅读器功能、WASM API、Worker 协议和部署说明见 [`web/README.md`](web/README.md)。
