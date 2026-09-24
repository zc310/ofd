# OFD WASM 示例

这个目录是一个不依赖前端框架的网页示例，调用当前目录对应的 `ofd-wasm` 导出的浏览器 API。

>使用阅读器处理不可信或敏感文档前，请阅读项目根目录的 [免责声明](../../../DISCLAIMER.md)。在线示例不替代生产环境的文件安全、权限控制和隐私保护措施；如发现仓库、文档或示例中可能存在侵权内容，请通过 [GitHub Issues](https://github.com/zc310/ofd/issues/new) 联系维护者。

## 功能

### 打印与导出

- **打印**：支持打印当前页、全部页面或自定义范围，例如 `1-3,5`。
- **导出**：示例界面支持选择页面范围、DPI、PNG/JPG/PDF/TXT 格式和背景颜色；底层 API 另支持直接输出 SVG。
- 单页图片直接下载，多页图片打包为 ZIP；支持 File System Access API 的浏览器会在保存时让用户选择目标文件并分块写入。
- PDF 按所选 DPI 渲染复杂效果并保持页面物理尺寸；文字和矢量内容保持为 PDF 原生对象，可复制文字。
- TXT 导出为一个文本文档。
- 导出逐页执行并显示进度，可在当前页面完成后取消。

### 导航

- 左侧栏页签为“缩略图”“大纲”“书签”，以及“更多”菜单下的“字体”“附件”“资源”“注解”“签名”；大纲以可折叠树展示，点击条目跳转到对应页面。
- 字体页签列出文档声明的字体清单，可按标题输入框过滤字体名/字体族，点击字体项的“定位使用页”可查找并跳转到使用该字体的页面（统计按文档规模限制扫描页数，截断时提示“仅覆盖前 N 页”）。
- 附件页签列出文档附件（名称、格式/大小、`Usage` 与隐藏/缺失徽标），可按名称过滤；可见附件提供“下载”，图片/PDF/文本/音视频等可预览类型额外提供“预览”（新标签页打开），缺失文件禁用操作。
- 资源页签以缩略图网格列出文档内多媒体资源（图片/音频/视频），图片进入可视区域时懒加载缩略图并显示格式、尺寸与大小，点击在新标签页预览；音视频显示类型图标。
- 各内容面板（大纲/书签/字体/附件/资源/注解/签名）的滚动位置按文档保存在浏览器本地，重新切换页签或再次打开文档时会恢复到上次位置。
- 注解页签按页列出文档注解（类型/子类型、创建者、日期、备注与隐藏徽标），点击跳转到对应页面的注解位置。
- 签名页签列出文档签名（提供者/公司、算法、签名时间、摘要一致/验签通过/可信徽标），并显示每个签章所在页码与印章缩略图，点击跳转到签章位置；点击“签名范围”可展开签名覆盖的文件引用与逐项摘要校验（数据摘要一致/不一致），点击“证书详情”可展开印章/外层证书的主体、签发者、序列号、有效期、公钥、算法与签名/证书/证书链/吊销校验状态，并可导出证书（DER/PEM）；签名项提供“导出签名值”（SignedValue.dat）。
- 大纲项带目标位置时按 `Dest` 的 `Top`/`Left`/`Zoom` 定位，`FitR` 先按矩形适配缩放，并按当前页面旋转换算坐标；带 URI 的条目在新标签页打开链接。
- 页面上的可点击链接会叠加半透明热区：链接来自注解（`Type="Link"`）与页面正文/模板图元的 `CLICK` 动作；外部链接在新标签页打开，内部跳转按目标页与 `Dest` 定位，并随页面旋转/缩放重新布局。
- 文档声明 `PageMode=UseOutlines` / `UseBookmarks` 且对应内容存在时默认打开相应页签；页签选择保存在浏览器本地。
- 没有大纲或书签的文档在对应页签显示占位提示（页签始终可用，不会自动跳回缩略图），无跳转目标的大纲项不可点击；当前阅读页对应的大纲项/书签会高亮。
- 侧栏宽度可用分隔条拖拽调整并保存在本地；大纲/书签条目右侧显示目标页码，并支持按标题过滤（保留命中项及其祖先）。
- 大纲项按文档声明的 `Expanded` 决定默认展开状态，用户手动折叠/展开会按文档保存在本地并覆盖声明值；大纲页签提供“展开全部/折叠全部”（过滤时禁用），大纲/书签面板支持方向键与 Home/End 移动焦点，页签支持左右方向键切换。
- 打开字体页签时会批量统计所有字体的使用页数并显示在“定位使用页”按钮上，无需逐个点击；统计结果按文档缓存。
- 用户尚未保存布局/缩放偏好时，采用文档声明的 `PageLayout`（OneColumn/TwoPageL/TwoPageR 等近似映射为单页/双页）和 `ZoomMode`/自定义 `Zoom`；`FitHeight`/`FitRect` 近似为“适应页面”。用户保存过的偏好优先。

### 键盘快捷键

- `ArrowLeft` / `PageUp`：上一页；`ArrowRight` / `PageDown`：下一页。
- `Home` / `End`：第一页 / 最后一页。
- `+` / `=`：放大；`-`：缩小；`0`：恢复 100%。
- `f`：适应宽度；`Shift+F`：适应页面。
- `Ctrl/Cmd+F`：打开搜索；`Ctrl/Cmd+Shift+C`：复制当前页文字。
- `Ctrl/Cmd+B`：显示/隐藏侧栏；`[` / `]`：切换上一个/下一个侧栏面板（缩略图/大纲/书签/字体/附件/资源/注解/签名）。
- 在输入框内输入或存在 `Ctrl/Cmd` 等修饰键时不会触发上述翻页/缩放快捷键。

### 显示设置

- 显示或隐藏缩略图。
- 显示或隐藏文字层。
- 开启深色阅读背景。
- 可选“清晰度优先”：未选中时正文始终按 72 DPI 渲染，缩放使用 CSS 放大；选中后按缩放比例重新渲染以提高图像清晰度。
- 页面渲染格式可选 PNG 位图、JPG 图片或 SVG 矢量，默认使用 PNG；JPG 不支持透明度，透明区域使用白色。
- 设置文档背景色，支持白色、透明、自定义颜色以及暗夜紫灰、晨雾暖沙、复古深棕、极光钢蓝、柔光羊皮、半岛墨蓝和晴空浅灰主题。
- 点击工具栏的缩放比例可打开预设缩放菜单（50%–300%、适应宽度、适应页面）。
- 底部页码胶囊包含上一页、页码输入/总页数、下一页，可直接输入页码跳转；可在“显示设置”中隐藏，或点击胶囊上的关闭按钮隐藏。
- 打印、导出、复制当前页文字、复制全文收在工具栏最右侧的“文档操作”菜单（`more_vert` 图标）中；移动端该按钮固定在最右侧一行。
- 文档信息面板列出文档声明的字体（名称、字体族、粗体/斜体、衬线/等宽、嵌入或逻辑、格式），并汇总资源统计（字体/附件/多媒体/注解页/签名数量）。
- 选择单页、双页或“双页，奇数页在左”布局。
- 可选“使用本机字体”：仅在勾选且已打开文档时，于用户手势内请求浏览器授权读取本机字体（`queryLocalFonts`），只取文档声明但未嵌入的字体族并注册为回退字体后重新渲染；取消勾选或关闭/切换文档时移除注册并恢复内嵌/默认字体，本机字体仅在当前文档内有效。不支持该 API 的浏览器会禁用此选项。
- 显示设置和布局设置会保存在浏览器本地。

### 页面布局

- 普通双页模式将第 1 页放在右侧，页面排列为“空白+1、2+3”。
- “双页，奇数页在左”模式的页面排列为“1+2、3+4”。
- 双页布局下，桌面端左侧缩略图也按两列显示，并与页面 spread 对齐。
- 单页布局保持一列，手机端继续使用顶部横向缩略图栏。
- 缩略图页签顶部提供尺寸滑块（桌面端，可用宽度的 40%–100%），拖动即时调整并保存在本地；手机端使用固定尺寸。

### 响应式阅读

- 桌面端使用全宽布局，缩略图栏贴近窗口左侧，右侧阅读区占用剩余宽度。
- 手机端侧栏是一个统一的底部抽屉（最多 62vh，圆角、拖拽把手、半透明遮罩）：页签、过滤框与全部面板（含横向滚动的缩略图栏）都在抽屉内，顶部只保留文档，避免页签在顶部/底部之间跳变。
- 手机端左下角“侧栏”按钮开合抽屉；点击把手、遮罩或点击大纲/书签/注解/签名条目跳转后自动收起。抽屉开合状态单独保存在浏览器本地。
- 手机端页签与条目标签加大到触控尺寸（≥40px）。

### 虚拟列表

- 页面和缩略图使用窗口化虚拟列表，只保留可视区域附近的 DOM 节点。
- 未挂载节点通过虚拟轨道占位，保持完整文档的滚动位置。
- 页面滚动会取消离开窗口的未完成渲染请求。
- 缩略图滚动单独维护自己的虚拟窗口，避免快速拖动主页面滚动条时批量渲染中间页面的缩略图。
- 缩略图使用 15 DPI 渲染，正文页面根据当前缩放比例动态调整 DPI。
- 导出 PDF、TXT、图片和 ZIP 时，在支持 File System Access API 的浏览器中会直接分块写入用户选择的文件，避免再创建完整 Blob；其他浏览器继续使用 Blob 下载。
- 保存完成后会显示保存成功；不支持文件写入流的浏览器会显示已开始下载，用户取消选择保存位置时会显示已取消保存。
- PDF 由 WASM 按块生成并直接转发到保存流；多页图片 ZIP 逐页写入，Central Directory 在最后写出。浏览器不支持文件写入流时，仍会在导出结束后使用 Blob 下载。


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
ofd.removeFallbackFont('Noto Sans SC') // 取消该回退字体族，当前文档恢复内嵌/默认字体
ofd.open(new Uint8Array(await file.arrayBuffer()))
ofd.info() // 元数据 + fonts 字体清单（不含嵌入数据）
ofd.pageCount()
ofd.pages()
ofd.pageInfo(0)
ofd.outline()
ofd.preferences()
ofd.text(0)
ofd.search('关键词')
ofd.pageLinks() // 页面正文图元上的可点击链接
ofd.annotations() // 注解清单（含 Link 注解的 uri/target_page/dest）
ofd.renderPage(0, { format: 'png', dpi: 72, background: '#00000000' })
ofd.renderPage(0, { format: 'jpg', dpi: 72 })
ofd.renderPage(0, { format: 'svg' })
ofd.renderPages([0, 1, 2], { format: 'png', dpi: 36, background: '#00000000' })
ofd.renderStream([0], { format: 'png', dpi: 36 }) // 单页直接返回图片
ofd.renderStream([0, 1, 2], { format: 'png', dpi: 36 }, (chunk, sequence) => {
  // 多页图片 ZIP 按顺序返回分块。
})
ofd.renderStream([0, 1], { format: 'pdf', background: '#ffffff' }, (chunk, sequence) => {
  // PDF 按文件顺序返回分块。
})

ofd.close()
```

### 页面与字体

- 工具栏图标使用本地 `material-symbols-outlined-subset.woff2`，只包含当前页面使用的 Material Symbols 图标连字，不依赖 Google Fonts 远程加载。
- 本地 Material Symbols 字体来源于 Google Material Symbols，遵循 Apache License 2.0；第三方声明见 [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md)。
- `ofd.pages()` 返回所有页面的页数和尺寸。
- 页面尺寸优先从每个页面的 `Content.xml` 轻量读取 `Area/PhysicalBox`，不会加载页面内容、资源或字体。
- 页面没有有效的独立尺寸时，回退到所属文档的 `CommonData.PageArea`；仍无效时回退为 A4。
- 多个文档体分别使用各自的页面尺寸。
- 页面实际展示或调用 `ofd.pageInfo(index)` 时，才按需读取指定页面的完整内容。
- `ofd.outline()` 返回 `{ page_mode, nodes, bookmarks }`：`nodes` 是文档大纲树，每个节点为 `{ title, page, uri, dest, expanded, children }`；`page` 是从 0 开始的全局页索引，无法解析的目标为 `-1`；`uri` 是外部链接；`dest` 为 `{ type, left, top, right, bottom, zoom }`（未指定的字段为 `null`）；`expanded` 为文档声明的默认展开状态（`null` 表示未声明，按展开处理）。`bookmarks` 是 `{ name, page, dest }` 列表；`page_mode` 是文档声明的显示模式（如 `UseOutlines`）。
- 大纲跳转目标优先取 `Goto.Dest` 的页面引用，其次按 `Goto.Bookmark` 的书签名称解析，没有页面目标时回退到 URI；没有大纲或书签时对应数组为空。
- `ofd.preferences()` 返回 `{ page_layout, zoom_mode, zoom }`：分别是文档声明的页面布局、缩放模式和自定义缩放比例；未声明时为空串或 `null`。
- `ofd.info()` 在文档元数据之外返回 `fonts`：`{ id, scope, name, family, bold, italic, serif, fixed_width, format, embedded }`，包含没有嵌入文件的逻辑字体，且不传输字体二进制数据；`scope` 是字体所属文档体索引，与 `id` 一起唯一标识一个字体。
- `ofd.fontUsage(scope, fontID, maxScan, maxPages)` 返回 `{ pages, scanned, truncated }`：`pages` 是使用该字体的页面索引（升序），`scanned` 是实际扫描的页数，`truncated` 表示因扫描页数或结果上限而可能不完整。`ofd.fontUsageAll(maxScan, maxPages)` 一次扫描返回 `{ fonts: [{ scope, id, pages }], scanned, truncated }`。两个上限都可省略，默认 `maxScan=10000`、`maxPages=500`，绝对上限为 `100000`/`5000`；示例阅读器按文档页数选择扫描上限。按 `(scope, id)` 匹配可避免多文档体之间字体 ID 冲突。
- `ofd.open()` 返回的 `fonts` 包含嵌入字体的二进制数据、浏览器字体族名和样式。
- `ofd.addFallbackFont(data, family, weight, italic)` 可注册外部 TTF、OTF、WOFF 或 WOFF2 字体，并同时用于 WASM 渲染和文字层。
- `ofd.removeFallbackFont(family)` 取消先前注册的回退字体族：从当前 WASM 实例的回退字体列表移除（后续文档不再沿用），并让已打开文档立即恢复内嵌/默认字体。全局字体注册表不回滚。
- 示例阅读器在页面加载时预加载配置的回退字体，并使用 Cache Storage 持久缓存。每个字体应配置独立的 `family`，例如 `楷体`、`黑体` 或 `宋体`。
- 后续文档会复用缓存，不受字符数量限制；未找到可用内嵌字体的文字会优先按 OFD 字体名选择匹配的回退字体，匹配不到时使用第一个适配样式的回退字体。
- 自托管字体需要允许当前页面跨域访问；HTTPS 页面不能加载 HTTP 字体 URL。
- 字体下载或注册失败时会停止打开文档，避免静默导出无文字 PDF。
- 缓存内容会校验字体签名，网络失败时下一次打开会重新尝试。
- 生产环境建议将字体自托管，并配置允许访问字体 CDN/CORS。

### 页面渲染

- `renderPage` 和 `renderPages` 的 `format` 支持 `png`、`jpg` 和 `svg`，省略时默认为 `png`。
- PNG 返回 PNG `Uint8Array`，JPG 返回 JPEG `Uint8Array`，SVG 返回 SVG XML 的 UTF-8 `Uint8Array`。
- `renderPages` 按传入索引顺序返回数组，最多处理 64 页。
- `renderStream` 单页 PNG/JPG 时直接返回 `Uint8Array`，不需要回调；多页 PNG/JPG 在 WASM 内生成 ZIP 并通过回调返回分块，PDF 始终通过回调返回分块。
- `renderStream` 使用流式输出，不限制页数；生成过程按块回调，WASM 不会同时保留所有页面图像或完整 PDF。
- `dpi` 对 PNG 和 JPG 有效；JPG 不支持透明度，透明区域使用白色。
- SVG 主要保留页面中的矢量内容，但复杂渐变、裁剪或其他不适合直接序列化的效果仍可能包含栅格图像。
- `renderStream` 使用 `format: 'pdf'` 时通过回调按顺序返回 PDF 分块，使用页面物理尺寸并保留可复制的文字对象。

### 文字与错误

- `ofd.text()` 返回的文字对象包含对应的 `scope`、`fontFamily`、`weight`、`bold` 和 `italic`。
- `ofd.attachments()` 返回附件清单：`{ scope, id, name, format, size, has_size, actual_size, usage, visible, exists }`；`size` 是声明的字节数（`has_size=false` 时无效），`actual_size` 是包内实际字节数（文件缺失或未知时为 0），`visible` 未声明时为 true。
- `ofd.attachmentData(scope, id, maxBytes)` 读取附件二进制内容（返回可转移的 `ArrayBuffer`）；`maxBytes` 省略时默认 32 MiB，硬上限 128 MiB，超过上限返回错误。
- `ofd.media()` 返回多媒体资源清单：`{ scope, id, name, type, format, size, exists }`；`type` 通常为 `Image`/`Audio`/`Video`，`name` 是资源文件名。`ofd.mediaData(scope, id, maxBytes)` 读取资源二进制内容，大小限制同附件。
- `ofd.annotations()` 返回注解清单：`{ scope, page, id, type, subtype, creator, last_mod_date, visible, remark, uri, target_page, dest, boundary }`；`page` 是从 0 开始的全局页索引，`boundary` 为 `{ x, y, width, height }`（毫米）或 `null`。`Link` 注解的 `uri` 是外部链接（无则空串），`target_page` 是跳转目标页全局索引（无页面目标为 `-1`），`dest` 为 `{ type, left, top, right, bottom, zoom }`（无位置信息为 `null`）。
- `ofd.pageLinks()` 返回页面正文图元上的可点击链接清单：`{ scope, page, id, uri, target_page, dest, boundary }`，字段含义同上。链接可能定义在页面的模板页上，会叠加到每个使用该模板的页面；承载链接的图元不可见时仍可点击。
- `ofd.signatures()` 返回签名清单：`{ scope, id, provider, company, version, method, date, has_digest, digest_valid, digest_method, has_verification, verified, trusted, trust_checked, verification_error, has_data_hash, data_hash_match, references, stamps, certificates }`，其中 `references` 为 `{ file_ref, exists, match, error }`，`stamps` 为 `{ page, id, has_seal, seal_type, boundary }`，`certificates` 为 `{ slot, subject, issuer, common_name, organization, organizational_unit, country, locality, province, serial_number, not_before, not_after, public_key, algorithm, signature_format, signature_valid, certificate_valid, trust_checked, trusted, trust_error, revocation_checked, revocation_status, revocation_error, error }`。`ofd.signatureSeal(scope, id, stampIndex)` 返回签章印章文件内容（可转移 `ArrayBuffer`）。`ofd.signatureCertificate(scope, id, slot)` 按层级（`seal`/`outer`）返回证书 DER；`ofd.signatureValue(scope, id)` 返回签名值（SignedValue.dat）内容。
- `ofd.stats()` 返回资源数量汇总：`{ fonts, attachments, media, annotation_pages, signatures }`，只读取声明，不加载资源内容。
- 发生错误时，API 返回 `{ error: string }`，网页调用方应检查该字段。

## Worker 协议

网页 Worker 接收以下命令：

```text
open       data: ArrayBuffer
close
cancel     target: request id
memStats
info
addFallbackFont data: ArrayBuffer, family: string, weight: number, italic: boolean
removeFallbackFont family: string
pageInfo   index: number
outline
preferences
fontUsage  scope: number, fontID: number[, maxScan, maxPages]
fontUsageAll [maxScan, maxPages]
attachments
attachmentData scope: number, id: number[, maxBytes]
media
mediaData  scope: number, id: number[, maxBytes]
pageLinks
annotations
signatures
signatureSeal scope: number, id: number, stampIndex: number
signatureCertificate scope: number, id: number, slot: string
signatureValue scope: number, id: number
stats
renderPage index: number, options: object
renderPages indices: number[], options: object
renderStream indices: number[], options: object[, callback: function]
text       index: number
search     query: string
```

### 数据传输与缓存

- 页面和缩略图渲染结果以可转移的 `ArrayBuffer` 返回，避免在主线程和 Worker 之间复制 PNG/SVG 数据。
- PDF 和多页图片均通过流式接口使用 `stream-chunk` 消息按顺序转发分块。
- 正文页缓存上限为 256 MiB，缩略图缓存上限为 64 MiB，均由浏览器端使用 LRU 策略管理。
- 切换页面渲染格式时会取消未完成的渲染请求并清理两类图片缓存，然后按新格式重新加载可视区域。
- 页面滚动或缩放时会取消尚未开始的旧渲染任务，Worker 同一时间只执行一个任务。
- 已经进入同步 WASM 调用的任务无法被底层中断，但其结果不会再更新页面。

`renderStream` 多页或 PDF 的回调参数为 `(chunk, sequence)`；网页 Worker 内部会在目标文件流完成当前块写入后发送 ACK，WASM 才继续生成下一块。多页图片的 ZIP 条目和 Central Directory 均在 WASM 内生成。写入失败或取消会发送错误 ACK，并终止生成。

`streamAck` 和 `cancelStream` 是 Worker 使用的内部控制接口，普通网页调用方不需要直接调用。

`text` 返回页面文字对象，`x/y` 是页面左上角原点的覆盖层坐标，`glyphs` 提供字符级区域；`search` 返回 `{ page, run, text, start, end, rects }` 命中列表。`glyphs`/`rects` 的 `angle` 可直接用于浏览器 CSS 的 `rotate()`。示例页面会将搜索结果所在页面滚动到视口，并使用引擎返回的字符矩形显示高亮。
