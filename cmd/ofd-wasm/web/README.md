# OFD WASM 示例

这个目录是一个不依赖前端框架的网页示例，调用当前目录对应的 `ofd-wasm` 导出的浏览器 API。

阅读器的“打印”按钮支持打印当前页、全部页面或自定义范围（例如 `1-3,5`）。

示例使用 `worker.js` 将解析和渲染放在 Web Worker 中；页面使用 `IntersectionObserver` 按可视区域附近懒加载，并分别缓存正文页和缩略图的 Blob URL。工具栏支持 50% 到 300% 缩放，页面缩放时按 DPI 重新渲染正文，缩略图保持 36 DPI。

## 构建

在仓库根目录执行：

```bash
make build-wasm
```

该命令会生成：

```text
cmd/ofd-wasm/web/ofd.wasm
cmd/ofd-wasm/web/wasm_exec.js
```

## 运行

浏览器不能直接通过 `file://` 加载 WASM。可以在 `cmd/ofd-wasm/web` 目录启动任意静态 HTTP 服务：

```bash
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

然后打开 <http://localhost:8080>，选择一个 `.ofd` 文件。

## JavaScript API

WASM 启动后会注册 `window.ofd`：

```javascript
ofd.open(new Uint8Array(await file.arrayBuffer()), { fallbackFonts: [] })
ofd.addFallbackFont(fontData, 'Noto Sans SC', 400, false)
ofd.pageCount()
ofd.pages()
ofd.pageInfo(0)
ofd.text(0)
ofd.search('关键词')
ofd.renderPage(0, { dpi: 96, background: '#00000000' })
ofd.renderPages([0, 1, 2], { dpi: 36, background: '#00000000' })

// background omitted or set to #00000000 produces a transparent PNG.
ofd.close()
```

`renderPage` 和 `renderPages` 返回 PNG `Uint8Array`；`renderPages` 按传入索引顺序返回数组，最多 64 页。`ofd.open()` 返回的 `fonts` 包含嵌入字体的二进制数据、浏览器字体族名和样式；`ofd.addFallbackFont(data, family, weight, italic)` 可注册外部 TTF/OTF/WOFF/WOFF2 字体，并同时用于 WASM PNG 渲染和文字层。示例阅读器在页面加载时预加载完整的 Noto Sans CJK 简体中文 Regular OTF，并使用 Cache Storage 持久缓存；后续文档复用缓存，不受字符数量限制。所有未找到可用内嵌字体的文字，包括粗体文字，都使用 `NotoSansCJKsc-Regular.otf` 回退。缓存内容会校验字体签名，网络失败时下一次打开会重新尝试。生产环境建议将字体自托管，并配置允许访问字体 CDN 的 CSP/CORS。`ofd.text()` 返回的文字对象包含对应的 `fontFamily`、`weight`、`bold` 和 `italic`。发生错误时，API 返回 `{ error: string }`，网页调用方应检查该字段。

## Worker 协议

网页 Worker 接收以下命令：

```text
open       data: ArrayBuffer
close
cancel     target: request id
addFallbackFont data: ArrayBuffer, family: string, weight: number, italic: boolean
renderPage index: number, options: object
renderPages indices: number[], options: object
text       index: number
search     query: string
```

页面和缩略图渲染结果以可转移的 `ArrayBuffer` 返回，避免在主线程和 Worker 之间复制 PNG 数据。正文页缓存上限为 64 MiB，缩略图缓存上限为 16 MiB。缩放或切换文档时会取消尚未开始的旧渲染任务，Worker 同一时间只执行一个任务；已经进入同步 WASM 调用的任务无法被底层中断，但其结果不会再更新页面。

`text` 返回页面文字对象，`x/y` 是页面左上角原点的覆盖层坐标，`glyphs` 提供字符级区域；`search` 返回 `{ page, run, text, start, end, rects }` 命中列表。`glyphs`/`rects` 的 `angle` 可直接用于浏览器 CSS 的 `rotate()`。示例页面会将搜索结果所在页面滚动到视口，并使用引擎返回的字符矩形显示高亮。
