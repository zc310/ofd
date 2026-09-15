# OFD WASM 示例

这个目录是一个不依赖前端框架的网页示例，调用当前目录对应的 `ofd-wasm` 导出的浏览器 API。

>使用阅读器处理不可信或敏感文档前，请阅读项目根目录的 [免责声明](../../../DISCLAIMER.md)。在线示例不替代生产环境的文件安全、权限控制和隐私保护措施；如发现仓库、文档或示例中可能存在侵权内容，请通过 [GitHub Issues](https://github.com/zc310/ofd/issues/new) 联系维护者。

## 功能

### 打印与导出

- **打印**：支持打印当前页、全部页面或自定义范围，例如 `1-3,5`。
- **导出**：示例界面支持选择页面范围、DPI、PNG/JPG/PDF/TXT 格式和背景颜色；底层 API 另支持直接输出 SVG。
- 单页图片直接下载，多页图片打包为 ZIP。
- PDF 按所选 DPI 栅格化，并保持页面物理尺寸。
- TXT 导出为一个文本文档。
- 导出逐页执行并显示进度，可在当前页面完成后取消。

### 显示设置

- 显示或隐藏缩略图。
- 显示或隐藏文字层。
- 开启深色阅读背景。
- 可选“清晰度优先”：未选中时正文始终按 72 DPI 渲染，缩放使用 CSS 放大；选中后按缩放比例重新渲染以提高图像清晰度。
- 页面渲染格式可选 PNG 位图、JPG 图片或 SVG 矢量，默认使用 PNG；JPG 不支持透明度，透明区域使用白色。
- 设置文档背景色，支持白色、透明、自定义颜色以及暗夜紫灰、晨雾暖沙、复古深棕、极光钢蓝、柔光羊皮、半岛墨蓝和晴空浅灰主题。
- 选择单页、双页或“双页，奇数页在左”布局。
- 显示设置和布局设置会保存在浏览器本地。

### 页面布局

- 普通双页模式将第 1 页放在右侧，页面排列为“空白+1、2+3”。
- “双页，奇数页在左”模式的页面排列为“1+2、3+4”。
- 双页布局下，桌面端左侧缩略图也按两列显示，并与页面 spread 对齐。
- 单页布局保持一列，手机端继续使用顶部横向缩略图栏。

### 响应式阅读

- 桌面端使用全宽布局，缩略图栏贴近窗口左侧，右侧阅读区占用剩余宽度。
- 手机端自动切换为顶部横向缩略图栏。

### 虚拟列表

- 页面和缩略图使用窗口化虚拟列表，只保留可视区域附近的 DOM 节点。
- 未挂载节点通过虚拟轨道占位，保持完整文档的滚动位置。
- 页面滚动会取消离开窗口的未完成渲染请求。
- 缩略图滚动单独维护自己的虚拟窗口，避免快速拖动主页面滚动条时批量渲染中间页面的缩略图。
- 缩略图使用 15 DPI 渲染，正文页面根据当前缩放比例动态调整 DPI。


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

同时会按 `viewer.js`、`worker.js`、`wasm_exec.js`、`ofd.wasm` 的内容哈希把 `service-worker.js` 的缓存名写为 `ofd-reader-shell_<hash>`。

## 运行

浏览器不能直接通过 `file://` 加载 WASM。可以在 `cmd/ofd-wasm/web` 目录启动任意静态 HTTP 服务：

```bash
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

然后打开 <http://localhost:8080>，选择一个 `.ofd` 文件。

在支持 File Handling API 的 Chromium 浏览器中，将阅读器安装为 PWA 后，manifest 会将 `.ofd` 注册为可打开的文件类型。用户可以在系统文件管理器中将 `.ofd` 文件设置为使用该阅读器打开；浏览器通过 `launchQueue` 将文件交给页面。未安装为 PWA 或浏览器不支持该 API 时，仍可使用“打开文件”和拖放方式。

## 资源缓存与更新

阅读器同时受到浏览器 HTTP 缓存、Service Worker 缓存和 Web Worker 脚本缓存影响。`service-worker.js` 使用 `cache-first` 策略：资源已经进入 Cache Storage 后，普通刷新可能仍然使用旧版本；`Ctrl+F5` 也不一定能绕过 Service Worker。

页面入口、页面脚本、Web Worker、`wasm_exec.js` 和 `ofd.wasm` 都使用不带查询参数的固定路径，缓存版本由 Service Worker 缓存名管理。`make build-wasm` / `make package-wasm-web` 会计算 `index.html`、`viewer.js`、`worker.js`、`wasm_exec.js`、`ofd.wasm` 的内容哈希，把 `service-worker.js` 的 `CACHE_NAME` 写成 `ofd-reader-shell_<hash>`：只要任一资源内容变化，缓存名就变化，新 Service Worker 安装后会删除旧缓存并重新缓存全部资源。

客户端已经做了两处兜底，不依赖服务器配置即可保证更新：

- 注册时使用 `{ updateViaCache: 'none' }`（`viewer.js`），浏览器检查 Service Worker 更新时绕过 HTTP 缓存，发布后第一次导航就能检测到新版本。
- 安装预缓存时通过 `{ cache: 'no-cache' }` 的请求写入新缓存（`service-worker.js`），导航请求同样强制 `no-cache`，确保新缓存写入的是最新字节且 `index.html` 每次导航都会重新校验。
- 独立的 `ofd-fonts` 缓存（`viewer.js` 维护的回退字体，内容寻址不可变）在应用更新时会被保留，不会被清理；每次构建只淘汰 `ofd-reader-shell_*` 旧缓存。


建议生产环境配置以下响应头：

```text
index.html          Cache-Control: no-cache, must-revalidate
service-worker.js   Cache-Control: no-cache, must-revalidate
viewer.js           Cache-Control: no-cache, must-revalidate
worker.js           Cache-Control: no-cache, must-revalidate
ofd.wasm            Cache-Control: no-cache, must-revalidate
wasm_exec.js        Cache-Control: no-cache, must-revalidate
```

如果生产服务器不方便配置上面的响应头，可以改用内容 hash 文件名方案：给 `viewer.js`、`worker.js`、`ofd.wasm` 等生成带内容哈希的文件名并设置长期缓存，但此时不再符合“固定路径 + 哈希缓存名”的默认约定，`service-worker.js` 的预缓存列表也需要同步使用这些文件名。

如果浏览器仍然显示旧版本，可以在开发者工具中打开 **Application**，执行 **Service Workers -> Unregister** 和 **Storage -> Clear site data**，然后重新加载页面。也可以在控制台检查当前控制页面的 Service Worker 和 Worker URL：

```javascript
navigator.serviceWorker.controller?.scriptURL
performance.getEntriesByType('resource').filter(item => item.name.includes('worker.js'))
```

## JavaScript API

WASM 启动后会注册 `window.ofd`：

```javascript
ofd.addFallbackFont(fontData, 'Noto Sans SC', 400, false) // WASM 生命周期内注册一次
ofd.open(new Uint8Array(await file.arrayBuffer()))
ofd.pageCount()
ofd.pages()
ofd.pageInfo(0)
ofd.text(0)
ofd.search('关键词')
ofd.renderPage(0, { format: 'png', dpi: 72, background: '#00000000' })
ofd.renderPage(0, { format: 'jpg', dpi: 72 })
ofd.renderPage(0, { format: 'svg' })
ofd.renderPages([0, 1, 2], { format: 'png', dpi: 36, background: '#00000000' })
ofd.renderPDF([0, 1], { background: '#ffffff' })

// background omitted or set to #00000000 produces a transparent PNG.
ofd.close()
```

`ofd.pages()` 返回所有页面的页数和尺寸；尺寸优先从每个页面的 `Content.xml` 轻量读取 `Area/PhysicalBox`，不会加载页面内容、资源或字体。页面没有有效的独立尺寸时，回退到所属文档的 `CommonData.PageArea`，再无效时回退为 A4。多个文档体分别使用各自的尺寸。页面实际展示或调用 `ofd.pageInfo(index)` 时才按需读取指定页面的完整内容。`ofd.open()` 返回的 `fonts` 包含嵌入字体的二进制数据、浏览器字体族名和样式；`ofd.addFallbackFont(data, family, weight, italic)` 可注册外部 TTF/OTF/WOFF/WOFF2 字体，并同时用于 WASM 渲染和文字层。示例阅读器在页面加载时预加载完整的 Noto Sans CJK 简体中文 Regular OTF，并使用 Cache Storage 持久缓存；后续文档复用缓存，不受字符数量限制。所有未找到可用内嵌字体的文字，包括粗体文字，都使用 `NotoSansCJKsc-Regular.otf` 回退。缓存内容会校验字体签名，网络失败时下一次打开会重新尝试。生产环境建议将字体自托管，并配置允许访问字体 CDN/CORS。`ofd.text()` 返回的文字对象包含对应的 `fontFamily`、`weight`、`bold` 和 `italic`。发生错误时，API 返回 `{ error: string }`，网页调用方应检查该字段。
`renderPage` 和 `renderPages` 的配置项 `format` 支持 `png`、`jpg` 和 `svg`，省略时默认为 `png`。PNG 返回 PNG `Uint8Array`，JPG 返回 JPEG `Uint8Array`，SVG 返回 SVG XML 的 UTF-8 `Uint8Array`；`renderPages` 按传入索引顺序返回数组，最多处理 64 页。`dpi` 对 PNG 和 JPG 有效，JPG 不支持透明度，透明区域使用白色；SVG 主要保留页面中的矢量内容，但复杂渐变、裁剪或其他不适合直接序列化的效果仍可能包含栅格图像。`renderPDF` 返回单个 PDF `Uint8Array`，使用页面物理尺寸，DPI 控制嵌入页面图像的分辨率。

## Worker 协议

网页 Worker 接收以下命令：

```text
open       data: ArrayBuffer
close
cancel     target: request id
addFallbackFont data: ArrayBuffer, family: string, weight: number, italic: boolean
pageInfo   index: number
renderPage index: number, options: object
renderPages indices: number[], options: object
renderPDF indices: number[], options: object
text       index: number
search     query: string
```

页面和缩略图渲染结果以可转移的 `ArrayBuffer` 返回，避免在主线程和 Worker 之间复制 PNG/SVG 数据；PDF 导出结果也以可转移的 `ArrayBuffer` 返回。正文页缓存上限为 256 MiB，缩略图缓存上限为 64 MiB，均由浏览器端使用 LRU 策略管理。切换页面渲染格式时会取消未完成的渲染请求并清理两类图片缓存，然后按新格式重新加载可视区域。页面滚动或缩放时会取消尚未开始的旧渲染任务，Worker 同一时间只执行一个任务；已经进入同步 WASM 调用的任务无法被底层中断，但其结果不会再更新页面。

`text` 返回页面文字对象，`x/y` 是页面左上角原点的覆盖层坐标，`glyphs` 提供字符级区域；`search` 返回 `{ page, run, text, start, end, rects }` 命中列表。`glyphs`/`rects` 的 `angle` 可直接用于浏览器 CSS 的 `rotate()`。示例页面会将搜索结果所在页面滚动到视口，并使用引擎返回的字符矩形显示高亮。
