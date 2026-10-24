# OFD WASM 示例

这个目录是一个不依赖前端框架的网页示例，调用当前目录对应的 `ofd-wasm` 导出的浏览器 API。

>使用阅读器处理不可信或敏感文档前，请阅读项目根目录的 [免责声明](../../../DISCLAIMER.md)。在线示例不替代生产环境的文件安全、权限控制和隐私保护措施；如发现仓库、文档或示例中可能存在侵权内容，请通过 [GitHub Issues](https://github.com/zc310/ofd/issues/new) 联系维护者。

## 功能

### 加密文档

- 支持 GM/T 0099-2020 口令方案 `1.1.1` 和 SM2 证书方案 `1.1.2`；整包加密、部分条目加密、独立条目密钥及明文/密文映射表均通过现有只读解密视图读取。
- 本地文件、拖放、最近文件和远程 `?file=` 都会在需要凭据时显示解锁对话框。输入口令，或切换至“SM2 私钥”选择 PKCS#8 PEM/DER 私钥文件；加密私钥可填写私钥密码。可填写接收者名称，留空时按库的尝试预算自动匹配。
- 保留口令的空格和 Unicode，允许空口令。错误凭据可重新输入；取消或切换文件会结束旧的解锁流程。元数据损坏、不支持的方案或预算超限直接报告错误。
- 口令包含换行等无法在单行输入框输入的字符时，选择“口令文件”，读取最多 64 KiB 的 UTF-8 文件，保留 BOM、空格及换行；空文件表示空口令，非法 UTF-8 被拒绝。
- 只有完整解锁后才进入页面浏览；多层加密须能使用本次提供的凭据全部解锁。解锁后支持页面、文字、搜索、打印和导出，源文件保持原始加密内容。
- 文档口令、私钥和私钥密码只用于本次解锁，不写入地址栏、最近文档或阅读设置；最近文件缓存的是原始加密 OFD，再次打开需重新解锁。WASM 不生成明文 ZIP，关闭 Reader 后释放解密视图。解锁本身不代表签名或完整性验证通过。
- WASM 默认使用 `pkg/crypt` 兼容模式，支持 ofdrw 的数字操作 ID 等已知格式差异。缺少 `EncryptCaseId` 时，按用户提供的口令选择 `1.1.1`，或按 SM2 私钥选择 `1.1.2`；首次未提供凭据时显示解锁框。已声明的加密方案保持原值，未知方案仍报错。
- 输入 OFD 仍限 256 MiB；文档口令和私钥密码最多 64 KiB UTF-8，私钥文件最多 4 MiB，其余解密预算沿用 `pkg/crypt` 默认值。

### 打印与导出

- **打印**：支持打印当前页、全部页面或自定义范围，例如 `1-3,5`。
- **导出**：示例界面支持选择页面范围、DPI、PNG/JPG/PDF/TXT 格式和背景颜色；底层 API 另支持直接输出 SVG。
- 单页图片直接下载，多页图片打包为 ZIP；支持 File System Access API 的浏览器会在保存时让用户选择目标文件并分块写入。
- PDF 按所选 DPI 渲染复杂效果并保持页面物理尺寸；文字和矢量内容保持为 PDF 原生对象，可复制文字。
- TXT 导出为一个文本文档。
- 导出逐页执行并显示进度，可在当前页面完成后取消。

### 导航

- 左侧栏页签为“缩略图”“大纲”“书签”，以及“更多”菜单下的“字体”“版本”“附件”“资源”“注解”“签名”；大纲以可折叠树展示，点击条目跳转到对应页面。
- 版本页签显示文档版本清单；点击按版本 `DocRoot` 加载该版本的页面、字体、图片、大纲、注解和附件，再次点击恢复主文档。支持包内绝对路径和相对于版本描述文件的路径，切换时清理页面、缩略图、文字与搜索缓存，阅读记录按版本隔离。缺失入口或依赖文件时报告失败并保留原版本。
- 字体页签列出文档声明的字体清单，可按标题输入框过滤字体名/字体族，点击字体项的“定位使用页”可查找并跳转到使用该字体的页面（统计按文档规模限制扫描页数，截断时提示“仅覆盖前 N 页”）。
- 附件页签列出文档附件（名称、格式/大小、`Usage` 与隐藏/缺失徽标），可按名称过滤；可见附件提供“下载”，图片/PDF/文本/音视频等可预览类型额外提供“预览”（新标签页打开），缺失文件禁用操作。
- 资源页签以缩略图网格列出文档内多媒体资源（图片/音频/视频），图片进入可视区域时懒加载缩略图并显示格式、尺寸与大小，点击在新标签页预览；音视频显示类型图标。
- 各内容面板（大纲/书签/字体/附件/资源/注解/签名）的滚动位置按文档保存在浏览器本地，重新切换页签或再次打开文档时会恢复到上次位置。

- SES 签名面板区分“签名值通过”和 GB/T 38540 完整验证结论，并可展开格式、用章授权、有效期、证书信任/吊销、印章状态、文档完整性与时间戳的逐项明细。缺少验证证据时显示“完整验证未完成”。`ofd.signatures()` 的 SES 结果新增 `validation: { status, checks: [{ code, status, message }] }`；整体状态为 `valid`、`invalid` 或 `incomplete`，原有分项字段保留。

- 附带时间戳时，明细中的“时间戳时刻的签章者证书”（`timestamp.signing_time`）检查签章者证书在该时刻的有效期和吊销状态。显式编码扩展默认值 `critical=FALSE` 会导致完整格式检查失败，原始数学签名结果仍单独展示。
- 注解页签按页列出文档注解（类型/子类型、创建者、日期、备注与隐藏徽标），点击跳转到对应页面的注解位置。

- “更多 → 图层”以页面为父节点、正文 `Content/Layer` 及背景/前景模板引用为子节点展示树形列表。点击小三角展开/折叠，点击页名定位，勾选子节点控制显示；模板名称和 ID 可用于辨认内容，对象数量放在悬停提示中。默认展开当前页，其他页展开时才加载；超过 100 页时按组展示，可用顶部“上一组”“下一组”和“当前页”导航。每页独立保留当前会话中的设置；关闭某页模板不会影响其他共用该模板的页面。仅有一个正文层的文档会整体显示/隐藏该层。注解和电子签章不属于此列表。
- 切换正文层或模板会刷新该页及缩略图，其中的文字不再参与选择、复制或搜索，链接/媒体点击热区也会移除；已有搜索结果清除后需重新搜索。PNG/JPG/SVG、PDF、TXT 导出及打印使用各页当前显示设置。源 OFD 不改写；重新打开或成功切换版本后恢复全部显示，版本切换失败时保留原设置。
- 签名页签列出文档签名（提供者/公司、算法、签名时间、摘要一致/签名值通过/可信及完整验证徽标），并显示每个签章所在页码与印章缩略图，点击跳转到签章位置；点击“签名范围”可展开签名覆盖的文件引用与逐项摘要校验（数据摘要一致/不一致），点击“证书详情”可展开印章/外层证书的主体、签发者、序列号、有效期、公钥、算法与签名/证书/证书链/吊销校验状态，并可导出证书（DER/PEM）；签名项提供“导出签名值”（SignedValue.dat）。
- 大纲项带目标位置时按 `Dest` 的 `Top`/`Left`/`Zoom` 定位，`FitR` 先按矩形适配缩放，并按当前页面旋转换算坐标；带 URI 的条目在新标签页打开链接。
- 页面上的可点击链接会叠加半透明热区：链接来自注解（`Type="Link"`）与页面正文/模板图元的 `CLICK` 动作；外部链接在新标签页打开，内部跳转按目标页与 `Dest` 定位，附件动作（`GotoA`）对图片/PDF/文本/音视频等可预览类型在新标签页打开、其它类型下载，并随页面旋转/缩放重新布局。
- 页面上的签章图片可点击：签名清单里每个带有效位置的签章会在对应页面的印章区域叠加热区，点击弹出“签章属性”浮层，内容与签名页签一致（提供者/公司、算法、签名时间、摘要/验签/可信徽标、签名范围、证书详情、导出签名值），并按页面旋转/缩放重新布局；骑缝章的点击热区和侧栏跳转高亮仅覆盖当前页经 `Clip` 裁剪后可见的切片。热区位于链接热区之下，重叠时链接优先。移动端浮层为底部抽屉；滚动、缩放、切换页面或文档时自动收起。
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

- 显示或隐藏左侧栏（缩略图、大纲、书签，以及“更多”菜单下的字体、版本、附件、资源、注解和签名）。
- 显示或隐藏文字层。
- 文字选择层与搜索高亮使用字体上伸、下延组成的排版行框；行高可与字号不同，字体不提供度量时使用 EM 高度。水平相邻的搜索命中按行合并，跨行、远距离间隔和旋转字形保留独立区域。
- 开启深色阅读背景。
- PNG/JPG 正文始终按缩放比例提高渲染 DPI（96–300），放大时会重新栅格化；SVG 保持矢量渲染。
- 页面渲染格式可选 PNG 位图、JPG 图片或 SVG 矢量，默认使用 PNG；JPG 不支持透明度，透明区域使用白色。
- 设置文档背景色，支持白色、透明、自定义颜色以及暗夜紫灰、晨雾暖沙、复古深棕、极光钢蓝、柔光羊皮、半岛墨蓝和晴空浅灰主题。
- 点击工具栏的缩放比例可打开预设缩放菜单（50%–300%、适应宽度、适应页面）。
- 底部页码胶囊包含上一页、页码输入/总页数、下一页，可直接输入页码跳转；可在“显示设置”中隐藏，或点击胶囊上的关闭按钮隐藏。
- 打印、导出、复制当前页文字、复制全文收在工具栏最右侧的“文档操作”菜单（`more_vert` 图标）中；移动端该按钮固定在最右侧一行。
- 文档信息面板列出文档声明的字体（名称、字体族、粗体/斜体、衬线/等宽、嵌入或逻辑、格式），并汇总资源统计（字体/附件/多媒体/注解页/签名数量）；页面资源读取失败时仍显示文档元数据，并单独提示字体列表错误。
- 选择单页、双页或“双页，奇数页在左”布局。
- 可选“使用系统字体”：仅在勾选且已打开文档时，于用户手势内请求浏览器授权读取系统字体（`queryLocalFonts`），只取文档声明但未嵌入的字体族并注册为回退字体后重新渲染；取消勾选或关闭/切换文档时移除注册并恢复内嵌/默认字体，注册的字体仅在当前文档内有效。不支持该 API 的浏览器会禁用此选项。
- “使用系统字体”的**勾选状态**会跨会话保留（`ofd-local-fonts`），打开新文档时勾选框已是你上次的意愿，**文档就绪后自动读取**，不需要再点一次选项。早期实现要求 `queryLocalFonts` 必须在用户手势的同步任务内发起（否则以 `SecurityError` 拒绝且不弹授权框），实测的浏览器已放开该限制；代码不假定任一种行为，而是先直接调用，仅当失败被判定为手势问题时才挂一次性的 `pointerdown`/`keydown` 监听退到首次交互。每次切换/关闭文档重置该退路的使用记录，避免“点一次失败一次”。恢复出来的勾选在生效前被手动点击时按激活处理，而不是取消勾选。不支持该 API 的浏览器不恢复勾选状态。
- 系统字体的读取放在**首次渲染之前**并等待至多 3 秒（`localFontsFirstRenderTimeout`）。补字体需要清空页面缓存并把可见页全部重新栅格化，若不等就会先以默认字体渲染一帧再跳变，且整篇渲染两遍；等在渲染前则只需一遍且第一帧即为最终字体。超时后照常渲染（用户不响应授权弹窗时不能让文档一直不出现），字体到位后再补一次并触发重渲染。等待期间状态栏显示“正在读取系统字体...”。
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
- 缩略图使用 30 DPI 渲染，正文页面根据当前缩放比例动态调整 DPI。
- 导出 PDF、TXT、图片和 ZIP 时，在支持 File System Access API 的浏览器中会直接分块写入用户选择的文件，避免再创建完整 Blob；其他浏览器继续使用 Blob 下载。
- 保存完成后会显示保存成功；不支持文件写入流的浏览器会显示已开始下载，用户取消选择保存位置时会显示已取消保存。
- PDF 由 WASM 按块生成并直接转发到保存流；多页图片 ZIP 逐页写入，Central Directory 在最后写出。浏览器不支持文件写入流时，仍会在导出结束后使用 Blob 下载。



## 脚本测试

浏览器脚本没有构建步骤，也没有 npm 依赖。`test/` 下的测试用 Node 直接运行：从
`viewer.js` **原文按括号配平切出待测函数**，在 `with (state)` 环境里用桩驱动，
因此测的是页面上实际运行的那份代码，而不是复制一份逻辑。

```bash
make test-wasm-web                                    # 运行全部
node cmd/ofd-wasm/web/test/run.mjs                    # 等价
node cmd/ofd-wasm/web/test/local-fonts.test.mjs       # 单个文件
```

约定：

- 新增/移动/改名 `viewer.js` 里的函数后，测试里对应的 `sliceFunction` 要同步；
  切不到会直接报出函数名，不会静默跳过。
- `harness.mjs` 只提供切片与断言工具，不要在测试文件里重抄被测逻辑。
- 桩一律写成闭包引用 `state`，不要写成对象方法：`with (state)` 里
  `localStorage.getItem(...)` 会把 `this` 绑到 `state.localStorage` 上。
- 改动 `viewer.js` 的断行、`?file=` 远程入口或系统字体入口时必须运行本套测试。
- 少数只能从源码断言顺序/守卫的地方（行为测试覆盖不到的调用点）标为"结构守卫"，
  在测试里注明原因。

## 构建

在仓库根目录执行：

```bash
make build-wasm
```

构建默认自动检测 `wasm-opt`：找到时优化 WASM，未找到时显示警告并使用未优化版本。可用 `WASM_OPT_ENABLED=0` 显式跳过优化：

```bash
make build-wasm WASM_OPT_ENABLED=0
```

该命令会生成：

```text
cmd/ofd-wasm/web/ofd.wasm
cmd/ofd-wasm/web/wasm_exec.js
cmd/ofd-wasm/web/service-worker.js
```

其中 `service-worker.js` 由模板 `service-worker.tmpl.js` 生成（模板入库，产物不入库）：按 `index.html`、`viewer.js`、`worker.js`、`wasm_exec.js`、`ofd.wasm`、图标字体的内容哈希把缓存名写成 `ofd-reader-shell_<hash>`。

## 运行

浏览器不能直接通过 `file://` 加载 WASM。可以在 `cmd/ofd-wasm/web` 目录启动任意静态 HTTP 服务：

```bash
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

然后打开 <http://localhost:8080>，选择一个 `.ofd` 文件。

## 安装为浏览器 App 与离线使用

阅读器是可安装的 PWA。通过 HTTPS 部署后（本机 `http://localhost` 也属于浏览器认可的安全上下文），先联网打开一次并等待页面加载完成，再从浏览器菜单安装：

- Chrome 桌面版：地址栏右侧的“安装 OFD 阅读器”图标，或菜单“保存并分享 → 安装 OFD 阅读器”。
- Microsoft Edge：地址栏“应用程序可用”图标，或菜单“应用 → 将此站点安装为应用”。
- Android Chrome：菜单中的“安装应用”或“添加到主屏幕”。不同浏览器和版本的菜单名称可能不同。
- iPhone/iPad Safari：使用“分享 → 添加到主屏幕”；iOS 不支持本项目的 `.ofd` 系统文件关联，需从 App 内打开或拖放（若系统支持拖放）。

安装后的窗口以独立 App 模式启动。支持 File Handling API 的 Chromium 浏览器还会注册 `.ofd` 文件类型；可在系统文件管理器中将 OFD 文件设为由阅读器打开，浏览器会把文件交给已安装的 App。该文件关联能力并非所有浏览器都支持；不支持时仍可在 App 中使用“打开文件”或拖放文件。

Service Worker 会在首次访问时缓存阅读器外壳（页面、WASM、Worker、图标和字体文件）。等页面加载完成、Service Worker 已安装并激活后，之后可以断网启动已访问过的阅读器，并通过“打开文件”或拖放方式打开设备上的 OFD 文件；文档本身不需要上传到服务器。安装 PWA 本身不会自动下载尚未访问过的网页资源，因此首次访问必须联网。

离线限制：需要联网下载的远程 `?file=` 文档、尚未缓存的回退字体，以及浏览器未缓存的其他外部资源在离线时不可用。浏览器可能在存储空间紧张时清理站点缓存；重要文档应保存在设备本地，不要把 Service Worker 缓存当作文档备份。更新阅读器也需要联网，更新会在 Service Worker 安装后提示重新加载。

## 用 URL 参数直接打开远程文档

页面地址带上 `?file=<url>` 时，打开页面会自动下载并打开该文档，不需要再手动选择文件：

```text
http://localhost:8080/?file=docs/sample.ofd
http://localhost:8080/?file=https%3A%2F%2Fexample.com%2Fsample.ofd&name=%E6%A0%B7%E4%BE%8B.ofd
```

- `file` 支持与页面同源的相对路径和 `http`/`https` 绝对地址；非 HTTP 协议会被拒绝。
- `name` 可选，用于覆盖从地址推断出的文件名（例如下载地址没有 `.ofd` 后缀时）。不带 `name` 时取 URL 路径的最后一段，路径为空则用 `document.ofd`。
- 下载不等待 WASM 初始化，与模块和字体加载并行，最长等待 `remoteFileTimeout`（120 秒），超过 `remoteFileMaxBytes`（256 MB）直接失败。
- 打开成功后地址栏会改写为 `?file=`（`history.replaceState`），因此链接可以直接分享，刷新也会重新加载同一文档；用户在页面内使用本地文件、最近文件或拖放打开时，这两个参数会被清除。
- 浏览器前进/后退（`popstate`）会按新地址重新加载对应的远程文档。
- 远程文档同样进入“最近打开”，沿用 `recentFileMaxBytes`（64 MB）上限，超限的文件不写入 IndexedDB。
- 下载请求使用 `cache: 'no-store'`，`service-worker.js` 据此让文档响应绕过 shell 缓存，因此同一地址不会读到陈旧副本。
- 跨域地址需要目标服务器返回 `Access-Control-Allow-Origin`，否则浏览器会阻止读取；同源部署（含把 `.ofd` 放在 `web` 目录或其子目录下的静态服务）没有这个限制。
- 下载失败只提示错误，不会锁死页面：启动页和「打开文件」、拖放入口保持可用。

## 内存诊断面板

页面地址带上 `?debug=1` 时，左上角会出现一个常驻的内存诊断面板，默认隐藏，适合排查大文档、系统字体或长时间浏览时的内存占用：

```text
http://localhost:8080/?debug=1
```

- 面板同时显示 **WASM 线性内存（保留）**、Go 运行时的存活堆/已用堆/系统堆/已归还 OS、GC 次数，以及浏览器侧的页面缓存、缩略图缓存、文字缓存、挂载页数和回退字体字节；Chromium 下还会显示 JS 堆。
- **WASM 线性内存只增不减**：`WebAssembly.Memory` 只支持增长，GC 只把空闲页标记为可复用，不会把地址空间还给浏览器。因此「线性内存（保留）」大于「Go 存活堆」是正常的，面板底部有同样的说明。
- 内嵌字体逐页补充字形映射时，只缓存同一字体的最新映射版本，并清理旧字体的整形和模板缓存；正在绘制的页面仍持有原字体，直到绘制结束后释放。
- 同一文档的正文、缩略图和文字测量共享字体解析与整形缓存，改变 DPI 或背景不会重复创建字体引擎；最后一个视图关闭时释放共享字体。
- 替换同一回退字体族、同一样式的数据时，只保留当前来源；移除回退字体会清理对应的整形缓存，注销且最后一个持有者释放后可回收解析结果。
- 全局回退字体最多注册 64 个字体族，各样式当前源数据合计最多 256 MiB；超过额度时返回错误并保留已有字体。此限制只针对显式注册的回退字体，与 OFD 内嵌字体和 Go GC 内存设置无关。
- 面板每 1.5 秒自动刷新，也可手动点「刷新」；点「GC」会触发 `ofd.gc()`（Go `runtime.GC()` + `debug.FreeOSMemory()`），可观察 `heapReleased` 上升但线性内存不变。
- 拖动标题即可移动面板，位置会记忆到 `localStorage`（`ofd-debug-pos`），窗口尺寸变化时自动夹回视口内。
- 开关会记忆到 `localStorage`（`ofd-debug`）：`?debug=1` 打开并记住，`?debug=0` 关闭并记住；关闭按钮与控制台的 `window.__debug(true/false)` 也会记住选择。控制台仍可用 `window.__readMemory()` 打印一次完整快照。
- 修改 `viewer.js`/`worker.js`/`index.html` 后需要重算 `CACHE_NAME`（见下节），否则旧 shell 缓存仍会命中。

## 资源缓存与更新

阅读器同时受到浏览器 HTTP 缓存、Service Worker 缓存和 Web Worker 脚本缓存影响。`service-worker.js` 使用 `cache-first` 策略：资源已经进入 Cache Storage 后，普通刷新可能仍然使用旧版本；`Ctrl+F5` 也不一定能绕过 Service Worker。

页面入口、页面脚本、Web Worker、`wasm_exec.js` 和 `ofd.wasm` 都使用不带查询参数的固定路径，缓存版本由 Service Worker 缓存名管理。`make build-wasm` / `make package-wasm-web` 会计算 `index.html`、`viewer.js`、`worker.js`、`wasm_exec.js`、`ofd.wasm` 的内容哈希，由 `service-worker.tmpl.js` 生成 `service-worker.js` 并把 `CACHE_NAME` 写成 `ofd-reader-shell_<hash>`：只要任一资源内容变化，缓存名就变化，新 Service Worker 安装后会删除旧缓存并重新缓存全部资源。

`?file=` 下载的远程文档不参与这套缓存：请求带 `cache: 'no-store'`，Service Worker 直接放行，既不写入 shell 缓存也不读取缓存，因此不会占用 shell 缓存空间或命中陈旧文档。

整个 shell（`index.html`、`viewer.js`、`worker.js`、`wasm_exec.js`、`ofd.wasm` 和图标字体）统一走缓存优先，保证任何时刻页面上的 HTML 与 JS/WASM 一定是同一版本。代价是换版必须由用户点一次：Service Worker 换新只影响**下一次**打开，当前这一页已经在内存里跑着旧资源。

因此 `viewer.js` 监听 Service Worker 的更新，在新 SW `activated` 后给出提示：

- 顶部横幅（`#update-banner`）写明"重新加载即可使用最新功能"，带「立即更新」按钮；关掉后本次会话不再打扰（记在 `sessionStorage`，刷新换版后自然失效）。
- 工具栏文件名后的刷新按钮（`#refresh-button`）同时出现并呼吸提示，关闭横幅后仍可从它主动换版。**无更新时该按钮不渲染也不占位**（HTML 里常驻 `hidden`），所以它不是常驻的"手动刷新"入口。

  注意 `#refresh-button` 的 `hidden` 必须配一条 `#refresh-button[hidden] { display: none }`：元素自己设了 `display: inline-flex`，优先级高于 `hidden` 属性的默认 `display: none`，漏掉这条按钮就会在无更新时照样显示。`test/refresh-button.test.mjs` 守住了这一点。
- `navigator.serviceWorker.controller` 为 `null` 说明是首次访问，`updatefound` 只是新 SW 刚装上、页面资源本来就是网络最新的，这种情况不提示；`statechange` 只在 `activated` 时提示，`redundant`（SW 字节相同被丢弃）不是更新。

客户端不依赖服务器配置的兜底：

- 注册时使用 `{ updateViaCache: 'none' }`（`viewer.js`），浏览器检查 Service Worker 更新时绕过 HTTP 缓存，发布后第一次导航就能检测到新版本。
- 安装预缓存逐个 `cache.add` 而不是 `cache.addAll`（`service-worker.js`）。`addAll` 是原子的，任一资源取不到就整批失败、新 SW 装不上，浏览器会永久继续用旧缓存且没有任何提示；逐个添加失败只影响那一个资源，并在控制台列出失败项。
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
ofd.embeddedFonts() // 按需读取嵌入字体二进制数据
ofd.pageCount()
ofd.pages()
ofd.pageInfo(0)
ofd.outline()
ofd.preferences()
ofd.text(0)
ofd.search('关键词')
ofd.pageLinks() // 页面正文图元上的可点击链接
ofd.annotations() // 注解清单（含 Link 注解的 uri/target_page/dest）
await ofd.renderPage(0, { format: 'png', dpi: 72, background: '#00000000' })
await ofd.renderPage(0, { format: 'jpg', dpi: 72 })
await ofd.renderPage(0, { format: 'svg' })
await ofd.renderPages([0, 1, 2], { format: 'png', dpi: 36, background: '#00000000' })
await ofd.renderStream([0], { format: 'png', dpi: 36 }) // Promise 完成后取得单页图片
await ofd.renderStream([0, 1, 2], { format: 'png', dpi: 36 }, (chunk, sequence) => {
  // 多页图片 ZIP 按顺序返回分块。
})
await ofd.renderStream([0, 1], { format: 'pdf', background: '#ffffff' }, (chunk, sequence) => {
  // PDF 按文件顺序返回分块。
})

ofd.close()
```

`ofd.open(data, options)` 的 `options` 除 `pageCacheCapacity` 和 `pageCacheBytes` 外，
还可指定 `password`、`userName`、`privateKey` 和 `privateKeyPassword`：

```javascript
const result = ofd.open(new Uint8Array(await file.arrayBuffer()), {
  password: '123456', // 省略表示未提供；空字符串表示空口令
  userName: 'User',  // 可选，按接收者名称匹配
})
if (result.error) console.error(result.code, result.error)

// SM2 证书加密文档：支持未加密或密码保护的 PKCS#8 PEM/DER 私钥。
ofd.open(await file.arrayBuffer(), {
  privateKey: new Uint8Array(await privateKeyFile.arrayBuffer()),
  privateKeyPassword: 'key-password', // 加密私钥的密码；未加密私钥省略
})
```

`privateKey` 接受 `Uint8Array` 或 `ArrayBuffer`。底层 API 不弹出对话框，调用方处理
`{ error, code }`，可重新提供文件数据与凭据调用 `open`。加密相关代码为：

| code | 含义 |
|---|---|
| `credentials_required` | 尚未提供所需口令或私钥 |
| `decrypt_failed` | 凭据不匹配或密文损坏 |
| `invalid_credentials` | 口令、接收者或私钥参数无效 |
| `invalid_encryption` | 加密元数据不合法 |
| `unsupported_encryption` | 加密功能或方案不支持 |
| `limit_exceeded` | 解密资源预算超限 |

解锁后 Reader 持有解密视图的独立租约；调用 `ofd.close()` 或打开其他文档时释放。
调用方自行管理传入的凭据数据；示例阅读器在请求完成后清理临时凭据引用和私钥缓冲。

### 页面与字体

- 工具栏图标使用本地 `material-symbols-outlined-subset.woff2`，只包含当前页面使用的 Material Symbols 图标连字，不依赖 Google Fonts 远程加载。
- 侧栏页签（缩略图 `grid_view`、大纲 `account_tree`、书签 `bookmark`、更多 `expand_more`、搜索 `search`）采用图标在上、文字在下的纵向排布。侧栏页签是 `flex: 1 1 0` 平分宽度，横向排布时图标会把"缩略图"这类三字标签挤成省略号；纵向排布靠 `#sidebar-tabs > button.sidebar-has-icon { flex-direction: column }` 实现。文字必须包在 `.sidebar-tab-label` 里，且 `#sidebar-tab-more-label` 这个 id 不能改——JS 用它在选中"更多"子项时把文字换成对应名称。
- 「更多」菜单里的字体、版本、附件、资源、注解、签名是图标在左、文字在右的横向排布（菜单项本来就是 flex，只需加 `gap`）。图标尺寸统一由 `.sidebar-tab-icon` / `.sidebar-menu-icon` 控制，不要在 `#sidebar-tab-more .material-symbols-outlined` 这类高特异性选择器里再写 `font-size`，否则单个页签的图标会比其它的小一号。
- 搜索页签只有图标没有文字，仍走 `sidebar-has-icon` 纵向布局，但内容顶部对齐（`justify-content: flex-start`），否则图标在整行里垂直居中，会与其它页签的图标错开半个行高。
- 图标清单的唯一来源是 `icons.json`：字体按它子集化，`test/icons.test.mjs` 检查代码里用到的每个图标都在清单内，以及 CSS 的 `font-variation-settings` 和 `@font-face` 的 `font-weight` 与清单的 `axes` 一致。缺图标的症状是页面上显示为空白，所以新增图标必须走这条流程：

```bash
# 1. 在 icons.json 的 icons 里加图标名，按字典序插入
# 2. 重新生成子集字体
make wasm-icon-font
# 3. 重算 CACHE_NAME，否则 Service Worker 字节不变、浏览器不会换版
make build-wasm
# 4. 验证
make test-wasm-web
```

  `@font-face` 的 `font-weight` 必须等于 `axes.wght`（当前 500）。Google Fonts 按 `icon_names` 返回的是该字重的实例，写成 400 会导致浏览器匹配不到该字重而回退字体，整片图标变成空白——这是替换字体时最容易漏的一步。
- 本地 Material Symbols 字体来源于 Google Material Symbols，遵循 Apache License 2.0；第三方声明见 [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md)。
- `ofd.pages()` 返回所有页面的页数和尺寸。
- `ofd.pages()` 只使用所属文档的 `CommonData.PageArea`，打开时不扫描各页面 XML；仍无效时回退为 A4。
- 页面挂载或调用 `ofd.pageInfo(index)` 时才读取页面内容，并以页面自有 `Area/PhysicalBox` 校正尺寸。
- 多个文档体分别使用各自的页面尺寸。
- 页面实际展示或调用 `ofd.pageInfo(index)` 时，才按需读取指定页面的完整内容。
- `ofd.outline()` 返回 `{ page_mode, nodes, bookmarks }`：`nodes` 是文档大纲树，每个节点为 `{ title, page, uri, dest, expanded, children }`；`page` 是从 0 开始的全局页索引，无法解析的目标为 `-1`；`uri` 是外部链接；`dest` 为 `{ type, left, top, right, bottom, zoom }`（未指定的字段为 `null`）；`expanded` 为文档声明的默认展开状态（`null` 表示未声明，按展开处理）。`bookmarks` 是 `{ name, page, dest }` 列表；`page_mode` 是文档声明的显示模式（如 `UseOutlines`）。
- `ofd.customTags()` 返回自定义标签 XML 树。每个标签为 `{ namespace, path, root }`，节点为 `{ name, namespace, attributes, text, children, target }`；符合 `ObjectRef PageRef="...">对象 ID</ObjectRef>` 的节点会带有 `{ page, page_id, object_id, x, y, width, height, object_text }` 定位目标，`page` 是从 0 开始的全局页索引。
- 大纲跳转目标优先取 `Goto.Dest` 的页面引用，其次按 `Goto.Bookmark` 的书签名称解析，没有页面目标时回退到 URI；没有大纲或书签时对应数组为空。
- `ofd.preferences()` 返回 `{ page_layout, zoom_mode, zoom }`：分别是文档声明的页面布局、缩放模式和自定义缩放比例；未声明时为空串或 `null`。
- `ofd.info()` 在文档元数据之外返回 `fonts`：`{ id, scope, name, family, bold, italic, serif, fixed_width, format, embedded }`，包含没有嵌入文件的逻辑字体，且不传输字体二进制数据；`scope` 是字体所属文档体索引，与 `id` 一起唯一标识一个字体。
- `ofd.fontUsage(scope, fontID, maxScan, maxPages)` 返回 `{ pages, scanned, truncated }`：`pages` 是使用该字体的页面索引（升序），`scanned` 是实际扫描的页数，`truncated` 表示因扫描页数或结果上限而可能不完整。`ofd.fontUsageAll(maxScan, maxPages)` 一次扫描返回 `{ fonts: [{ scope, id, pages }], scanned, truncated }`。两个上限都可省略，默认 `maxScan=10000`、`maxPages=500`，绝对上限为 `100000`/`5000`；示例阅读器按文档页数选择扫描上限。按 `(scope, id)` 匹配可避免多文档体之间字体 ID 冲突。
- `ofd.open()` 不读取嵌入字体二进制数据；`ofd.embeddedFonts()` 按需返回嵌入字体数据、浏览器字体族名和样式。网页阅读器在页面开始构建后后台加载字体，不阻塞文档首屏。
- `ofd.addFallbackFont(data, family, weight, italic)` 可注册外部 TTF、OTF、WOFF 或 WOFF2 字体，并同时用于 WASM 渲染和文字层。
- `ofd.removeFallbackFont(family)` 取消先前注册的回退字体族：从当前 WASM 实例的回退字体列表移除（后续文档不再沿用），并让已打开文档立即恢复内嵌/默认字体；同步注销全局来源，最后一个持有者释放后归还注册额度。重新使用时需再次 `addFallbackFont`。
- 示例阅读器在页面加载时预加载配置的回退字体，并使用 Cache Storage 持久缓存。每个字体应配置独立的 `family`，例如 `楷体`、`黑体` 或 `宋体`。
- 后续文档会复用缓存，不受字符数量限制；未找到可用内嵌字体的文字会优先按 OFD 字体名选择匹配的回退字体，匹配不到时使用第一个适配样式的回退字体。
- 自托管字体需要允许当前页面跨域访问；HTTPS 页面不能加载 HTTP 字体 URL。
- 字体下载或注册失败时会停止打开文档，避免静默导出无文字 PDF。
- 缓存内容会校验字体签名，网络失败时下一次打开会重新尝试。
- 生产环境建议将字体自托管，并配置允许访问字体 CDN/CORS。

### 页面渲染

- `ofd.layers(index)` 按需返回指定页的正文图层与模板引用数组 `{ key, id, kind, name, type, objects, visible }`。`kind` 为 `layer` 或 `template`；正文 `type` 缺省为 `Body`，模板 `type` 按页面引用及模板声明确定为 `Background` 或 `Foreground`。`name` 为模板名称（未声明时为空），`objects` 为直接包含的顶层图元/块数量，模板为各图层顶层对象数之和。`id` 是原始标识的字符串表示，`key` 才是开关标识：正文层为 ID，模板为 `template:<ID>`，两者不会因 ID 相同而相互影响。
- `ofd.setHiddenLayers(index, ids)` 使用 `layers()` 返回的 `key` 字符串数组替换该页的隐藏列表，`[]` 恢复全部显示；未知页码/条目返回错误，失败时不改变设置。显示状态由当前 Reader 持有，影响后续页面渲染、PDF 导出、文字、搜索与链接查询。`open` 和成功的 `selectVersion` 会重置设置，设置不改变 OFD 内的原始数据。例如 `setHiddenLayers(0, ['template:9'])` 可关闭 `999.ofd` 第一页的背景模板，保留正文。
- 直接调用 API 的网页需自行清除该页图片、缩略图、文字和链接缓存，并重新搜索；示例阅读器已处理异步旧响应及缓存失效。导出多页期间应保持图层设置稳定。
- `renderPage` 和 `renderPages` 的 `format` 支持 `png`、`jpg` 和 `svg`，省略时默认为 `png`。
- `renderPage`、`renderPages`、`renderStream` 的有效渲染请求均返回 Promise，使用 `await` 等待结果；渲染期间失败或取消时 Promise 拒绝。
- 单页结果中，PNG 为 PNG `Uint8Array`，JPG 为 JPEG `Uint8Array`，SVG 为 SVG XML 的 UTF-8 `Uint8Array`。
- `renderPages` 按传入索引顺序返回数组，最多处理 64 页。
- `renderStream` 单页 PNG/JPG 时 Promise 完成值为 `Uint8Array`，不需要回调；多页 PNG/JPG 在 WASM 内生成 ZIP 并通过回调返回分块，PDF 始终通过回调返回分块。
- `renderStream` 使用流式输出，不限制页数；生成过程按块回调，WASM 不会同时保留所有页面图像或完整 PDF。
- `dpi` 对 PNG 和 JPG 有效；JPG 不支持透明度，透明区域使用白色。
- SVG 主要保留页面中的矢量内容，但复杂渐变、裁剪或其他不适合直接序列化的效果仍可能包含栅格图像。
- `renderStream` 使用 `format: 'pdf'` 时通过回调按顺序返回 PDF 分块，使用页面物理尺寸并保留可复制的文字对象。

### 文字与错误

- `ofd.text()` 返回的文字对象包含对应的 `scope`、`fontFamily`、`weight`、`bold` 和 `italic`。
- `ofd.versions()` 返回版本清单，`doc_root` 为版本入口包内路径，`pages` 仅表示与当前视图重合的页面。`ofd.selectVersion(scope, id)` 按入口切换并返回 `{ pageCount, pages, fonts }`（`fonts` 为空，不同步读取字体数据）；空 `id` 恢复主文档。切换后页面立即构建，嵌入字体后台加载，独立历史入口不继承主入口签章。Worker 对应命令为 `selectVersion`，参数为 `scope` 和 `versionID`。
- `ofd.attachments()` 返回附件清单：`{ scope, id, name, format, size, has_size, actual_size, usage, visible, exists }`；`size` 是声明的字节数（`has_size=false` 时无效），`actual_size` 是包内实际字节数（文件缺失或未知时为 0），`visible` 未声明时为 true。
- `ofd.attachmentData(scope, id, maxBytes)` 读取附件二进制内容（返回可转移的 `ArrayBuffer`）；`maxBytes` 省略时默认 32 MiB，硬上限 128 MiB，超过上限返回错误。
- `ofd.media()` 返回多媒体资源清单：`{ scope, id, name, type, format, size, exists }`；`type` 通常为 `Image`/`Audio`/`Video`，`name` 是资源文件名。`ofd.mediaData(scope, id, maxBytes)` 读取资源二进制内容，大小限制同附件。
- `ofd.annotations()` 返回注解清单：`{ scope, page, id, type, subtype, creator, last_mod_date, visible, remark, uri, target_page, dest, boundary }`；`page` 是从 0 开始的全局页索引，`boundary` 为 `{ x, y, width, height }`（毫米）或 `null`。`Link` 注解的 `uri` 是外部链接（无则空串），`target_page` 是跳转目标页全局索引（无页面目标为 `-1`），`dest` 为 `{ type, left, top, right, bottom, zoom }`（无位置信息为 `null`）。
- `ofd.pageLinks()` 返回页面正文图元上的可点击链接清单：`{ scope, page, id, uri, target_page, dest, boundary, attachment_id, attachment_name, media_id, media_kind, operator, repeat, volume, event }`。链接可能定义在页面的模板页上，会叠加到每个使用该模板的页面；承载链接的图元不可见时仍可点击。`media_kind` 为 `sound` 或 `movie` 时表示点击播放声音或影片，`media_id` 对应 `ofd.media()` 的资源 ID，`operator` 为影片的 `Play`/`Stop`/`Pause`/`Resume`，`volume` 为 0 到 100 或 `null`。`attachment_id` 非空时表示附件动作（`GotoA`），点击后按 `ofd.attachmentData()` 读取：可预览类型在新标签页打开、其它类型下载，`attachment_name` 是附件名称（可能为空）。
- `ofd.pageLinksForPage(index)` 返回指定页面的可点击链接，字段与 `ofd.pageLinks()` 相同；网页阅读器按页面挂载懒加载此接口，避免打开大文档时扫描全部页面。
- `ofd.pageMediaActions()` 返回进入页面（`PO`）和打开文档（`DO`）时执行的声音、影片动作，字段与 `pageLinks` 的媒体字段相同，`event` 为 `PO` 或 `DO`。
- `ofd.pageMediaActionsForPage(index)` 按页返回页面进入动作及文档首页的 DO/PO 动作；网页阅读器按需调用，避免打开大文档时扫描全部页面。
- `ofd.signatures()` 返回签名清单：`{ scope, id, provider, company, version, method, date, has_digest, digest_valid, digest_method, has_verification, verified, trusted, trust_checked, verification_error, has_data_hash, data_hash_match, references, stamps, certificates, validation }`，其中 SES 的 `validation` 为 `{ status, checks: [{ code, status, message }] }`，`references` 为 `{ file_ref, exists, match, error }`，`stamps` 为 `{ page, id, has_seal, seal_type, boundary }`，`certificates` 为 `{ slot, subject, issuer, common_name, organization, organizational_unit, country, locality, province, serial_number, not_before, not_after, public_key, algorithm, signature_format, signature_valid, certificate_valid, trust_checked, trusted, trust_error, revocation_checked, revocation_status, revocation_error, error }`。`ofd.signatureSeal(scope, id, stampIndex)` 返回签章印章文件内容（可转移 `ArrayBuffer`）。`ofd.signatureCertificate(scope, id, slot)` 按层级（`seal`/`outer`）返回证书 DER；`ofd.signatureValue(scope, id)` 返回签名值（SignedValue.dat）内容。
- `ofd.stats()` 返回资源数量汇总：`{ fonts, attachments, media, annotation_pages, signatures }`，只读取声明，不加载资源内容。

`ofd.signatures()` 的每个 `stamp` 还包含 `visible_boundary`（`{ x, y, width, height }`，
页面坐标、毫米），表示应用 `Clip` 后的可见区域，供点击热区和跳转定位使用。
`boundary` 保留整枚印章的原始边界；没有有效裁剪时两个边界相同。
- 同步 API 或渲染参数检查失败时，API 返回 `{ error: string, code?: string }`，网页调用方应检查该字段；异步渲染期间失败则拒绝 Promise。加密错误带有上表中的 `code`。

## Worker 协议

网页 Worker 接收以下命令：

```text
open       data: ArrayBuffer, options?: { pageCacheCapacity, pageCacheBytes, password, userName, privateKey, privateKeyPassword }
close
cancel     target: request id
memStats
info
addFallbackFont data: ArrayBuffer, family: string, weight: number, italic: boolean
removeFallbackFont family: string
pageInfo   index: number
layers     index: number
setHiddenLayers index: number, ids: string[]
outline
preferences
fontUsage  scope: number, fontID: number[, maxScan, maxPages]
fontUsageAll [maxScan, maxPages]
attachments
attachmentData scope: number, id: number[, maxBytes]
media
mediaData  scope: number, id: number[, maxBytes]
pageLinks
pageMediaActions
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

- Worker 的错误回复为 `{ id, ok: false, error, code? }`，页面客户端在拒绝请求的 Error 上保留 `code`。`open` 的凭据参数与直接 API 相同；每次重试须提供新的文件缓冲，因为传输后原 ArrayBuffer 已被转移。
- 页面和缩略图渲染结果以可转移的 `ArrayBuffer` 返回，避免在主线程和 Worker 之间复制 PNG/SVG 数据。
- PDF 和多页图片均通过流式接口使用 `stream-chunk` 消息按顺序转发分块。
- 正文页缓存上限为 256 MiB，缩略图缓存上限为 64 MiB，均由浏览器端使用 LRU 策略管理。
- 切换页面渲染格式时会取消未完成的渲染请求并清理两类图片缓存，然后按新格式重新加载可视区域。
- 页面滚动或缩放时会取消旧渲染任务，Worker 同一时间只执行一个任务。
- 已运行的渲染也会在阶段边界响应取消；单次底层绘制或编码需执行到阶段边界，取消后的结果不会再更新页面。

`renderStream` 多页或 PDF 的回调参数为 `(chunk, sequence)`；网页 Worker 内部会在目标文件流完成当前块写入后发送 ACK，WASM 才继续生成下一块。多页图片的 ZIP 条目和 Central Directory 均在 WASM 内生成。写入失败或取消会发送错误 ACK，并终止生成。

`streamAck` 和 `cancelRender` 是 Worker 使用的内部控制接口，普通网页调用方不需要直接调用。
Worker 给 `renderPage(index, options, requestID)`、`renderPages(indices, options, requestID)`
和 `renderStream(indices, options, callback, requestID)` 传入唯一的正整数请求 ID，
再用 `cancelRender(requestID)` 取消对应渲染；单页 `renderStream` 的回调位置传 `null`。
未指定 ID 的直接调用也会在关闭文档时一起取消。

流式导出取消会解除 ACK 等待，并在页面加载、绘制、栅格化、编码及页间的阶段边界
停止生成；即使尚未产生输出块，Worker 也有机会处理取消消息。已进入的单次底层绘制
或编码需执行到阶段边界。取消后不再发送新的输出块，已写入的部分文件应丢弃。

`text` 返回页面文字对象，`x/y` 是页面左上角原点的覆盖层坐标，`glyphs` 提供字符级区域；`search` 返回 `{ page, run, text, start, end, rects }` 命中列表。`glyphs`/`rects` 的 `angle` 可直接用于浏览器 CSS 的 `rotate()`。示例页面会将搜索结果所在页面滚动到视口，并使用引擎返回的字符矩形显示高亮。
