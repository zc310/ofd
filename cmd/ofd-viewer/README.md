# OFD Viewer

OFD 桌面查看器，支持连续阅读和按需渲染。版本号由根目录 `Makefile` 的 `VERSION` 决定，运行中的版本见「关于」对话框。

使用本工具打开不可信或敏感文档前，请阅读项目根目录的 [免责声明](../../DISCLAIMER.md)。阅读器不替代生产环境的文件安全、权限控制和隐私保护措施。

## 系统要求

系统支持范围由 Go 工具链和 Fyne 共同决定，依赖版本以根目录 [go.mod](../../go.mod) 为准。下表按官方 Go 工具链的支持范围列出，桌面端还需要可用的图形环境（OpenGL）。Windows 7 兼容工具链的试编译说明见[主 README](../../README.md#构建与打包)。

| 平台    | 最低版本                                                               |
|---------|------------------------------------------------------------------------|
| Windows | Windows 10 及以上（含 Windows Server 2016 及以上）；不支持 Windows 7/8 |
| macOS   | macOS 12 Monterey 及以上，按 Go 1.26 工具链要求                        |
| Linux   | 内核 3.2 及以上，需要 X11 或 Wayland 与 OpenGL；无特定发行版版本要求   |
| Android | Android 5.0（API 21）及以上                                            |
| Web     | 支持 WebAssembly、WebGL 和文件选择取消事件的现代浏览器                 |

支持桌面端、Android 和 Web，不提供 iOS 版本。

## 功能

- 鼠标滚轮连续翻页，页码自动同步
- 页码输入跳转，支持 `Home`、`End` 和方向键翻页
- 缩略图默认隐藏，可按需显示并点击跳转
- 支持“适应页面”“适应宽度”“适应高度”和“双页显示”四种视图
- 启动窗口宽度固定 700，高度按“首屏放得下整页 A4”算出（另加工具栏高度和页面边距），默认“适应宽度”下一屏正好一整页，不用拉滚动条
- 双页视图按两列连续排列页面
- 双页视图下缩略图也按两列显示
- 阅读区背景色支持白色、透明、7 种预设色和自定义取色，预设与浏览器阅读器保持一致，选择会被记住
- 多文档体按出现顺序连续阅读，页码使用全局页码
- 页面和缩略图均按需加载，可见页两侧各预取一页，滚动换页不易出现白屏
- 阅读区使用布局虚拟化：显示帧数量只与视口有关，上万页文档也只常驻十余个页面对象
- 阅读区使用位图显示，可选择 96、150、200 或 300 DPI，设置会被记住；页面内原有位图仍以图片形式保留
- 页面图像和缩略图按内存预算限流，超出后淘汰没有显示帧引用的页面，滚动过长文档不会耗尽内存
- 导出支持 PDF、TXT、JPG、PNG、SVG、EPS、TeX
- 多页图片/矢量格式导出为 ZIP，单页直接保存对应文件
- 导出支持 DPI 和透明/白色背景设置；DPI 上限按页面尺寸和单页内存预算收紧
- 支持 Android 文件选择、文档查看和 APK 打包
- Web 版本通过浏览器选择本地 OFD，导出完成后下载文件；文档在浏览器内处理
- Android 返回键连续按两次后确认退出程序
- 信息按钮查看应用信息和 GitHub 项目地址
- 支持简体中文和英文，可在语言菜单切换并记住选择
- Linux 启动时检查自己是不是 `.ofd` 的默认阅读器，不是就询问是否设置，可勾选“不再提示”并被记住
- 右侧菜单提供“设为默认阅读器”，已是默认阅读器时打勾；不支持的系统不显示该项
- 支持 GM/T 0099-2020 加密文档，通过口令、UTF-8 口令文件或 SM2 私钥解锁后阅读和导出

## 加密文档

打开加密 OFD 时会显示解锁对话框，支持以下方式：

- **口令**：输入文档口令，保留空格及 Unicode，允许空口令。
- **口令文件**：选择 UTF-8 文件，保留 BOM、空格和换行；空文件表示空口令，最大 64 KiB。
  口令末尾有换行时应使用此方式。
- **SM2 私钥**：选择与接收者证书对应的未加密 PKCS#8 PEM（`PRIVATE KEY`）或 DER 文件，
  最大 4 MiB。暂不支持带口令保护的私钥文件。

接收者名称可选，留空时按解密库的尝试预算匹配。错误凭据可重试；取消解锁时保留当前已打开
的文档，切换文档或关闭窗口会取消旧加载并清理尚未提交的凭据。口令和私钥不写入应用偏好。

读取默认使用兼容模式，支持 ofdrw 的数字操作 ID 等已知格式差异；缺少 `EncryptCaseId` 时，
按用户选择的口令或私钥确定方案，不覆盖源文件中已声明的方案。
整包、局部条目、明文/密文映射及独立条目密钥均通过只读解密视图读取；多层加密须能用本次
提供的凭据全部解锁。解锁成功后使用原有页面、缩略图及 PDF/TXT/图片等导出功能。

阅读器不生成明文 OFD ZIP，关闭文档并等待在途渲染或导出完成后释放解密资源。
输入 OFD 限 512 MiB，其余解密资源限制沿用 `pkg/crypt` 默认预算。解锁成功不等于签名或完整性
验证通过，具体能力与限制见 [密码功能说明](../../docs/OFD-CRYPTO.md)。

## 界面语言

右侧菜单的“语言 / Language”可选择“简体中文”或“English”，切换立即生效，
当前文档、页码、视图和背景色保持不变。语言偏好以 `viewer-language` 保存，
桌面、Android 和 Web 版本共用同一套翻译。

首次启动时，中文系统使用中文，其他系统使用英文；之后优先使用保存的选择。
加载或导出过程中语言菜单暂时禁用，操作完成后可以切换。
文档原有标题、正文和文件名保持原文，系统文件选择器和 Fyne 内置控件使用各自的系统语言。

英文翻译集中在 `internal/localization/viewer/locales/en.json`，以中文源文案作为消息键；
译文需保留 `%d`、`%s` 等格式占位符。

## 阅读背景色

右侧菜单的“背景色”提供白色、透明、7 种预设色（暗夜紫灰、晨雾暖沙、复古深棕、极光钢蓝、柔光羊皮、半岛墨蓝、晴空浅灰）和“自定义…”。每项左侧有色卡，能直接看出颜色；“自定义”打开取色器，可以选任意颜色（含透明度）。预设与 WASM 阅读器的“文档背景色”是同一套，两边不会漂移；选择记在应用偏好里，下次启动沿用。

背景色只改页面纸张矩形的填充色：页面本身按透明背景渲染，未上色区域（例如径向渐变 Extend=0 时起始圆以内的区域）因此显示所选颜色而不是白色。切换立即生效，不需要重新渲染页面，加载和导出期间也可以切换。

## 默认阅读器

只有 Linux（不含 Flatpak）支持自动检测和设置文件关联：

- 检测顺序是 `xdg-mime`、`gio`，最后直接读 `$XDG_CONFIG_HOME/mimeapps.list`（只取 `[Default Applications]` 段）。探测不出结果时不弹提示，菜单项也不打勾——缺命令的精简系统上弹了也只能失败。
- 设置通过 `xdg-mime default` 或 `gio mime --default` 完成，由命令保留 `mimeapps.list` 里已有的分组和格式；命令失败会把命令输出带进错误信息。
- 启动时带文件参数运行（`ofd-viewer doc.ofd`）不提示：命令行用法是明确的意图。
- “不再提示”记在应用偏好里（`default-reader-prompt-dismissed`），成功设置默认阅读器后也会自动记住。删除偏好才能恢复提示。
- macOS、Windows、Android 和 Flatpak 的文件关联由各自的系统设置或安装流程管理，右侧菜单不显示“设为默认阅读器”。

## 使用

启动后点击“打开 OFD”，或通过命令行打开文件：

```bash
go run . path/to/document.ofd
```

Android 版本可通过系统文件选择器打开 OFD 文件。右侧菜单中的“关闭文档”用于返回未加载状态；未加载文档时选择“退出程序”，或连续按两次 Android 返回键并确认，即可退出应用。

快捷键：

- `O`：打开 OFD
- `A` / `W` / 左箭头 / 上箭头：上一页
- `D` / `S` / 右箭头 / 下箭头：下一页
- `PageUp` / `PageDown`：按一屏翻页，并吸附到页边界
- `Home` / `End`：跳到第一页或最后一页
- `Q` / `Esc`：逐层退出——已加载文档时先关闭文档，未加载文档时退出程序（与菜单和 Android 返回键一致）

## 编译

```bash
go build .
```

Linux 构建需要 OpenGL、X11 和 Wayland 开发库。

## 制作 DEB / RPM

在仓库根目录执行，打包配置和缓存更新脚本位于本目录的 `nfpm.yaml`、`update-caches.sh`：

```bash
make install-nfpm
export PATH="$(go env GOPATH)/bin:$PATH"

make package-viewer-deb      # 仅 DEB
make package-viewer-rpm      # 仅 RPM
make package-viewer-linux    # 同时生成 DEB 和 RPM
```

支持 `GOOS=linux GOARCH=amd64` 和 `GOOS=linux GOARCH=arm64`，版本来自根目录
`Makefile` 的 `VERSION`，可通过 `make package-viewer-linux VERSION=...` 覆盖。
默认产物目录为 `/tmp/ofd/dist`，可用 `DIST_DIR` 覆盖，nFPM 路径可用 `NFPM` 指定。
系统包始终使用 `CGO_ENABLED=1` 编译，需准备目标架构的 C/C++ 编译器、OpenGL/X11/Wayland
开发库和 binutils（`readelf`，可通过 `READELF` 指定）；ARM64 建议在 ARM64 Linux 上原生打包。
Release 工作流在两个架构的原生 Ubuntu 24.04 runner 上构建并发布，
打包时从 ELF 自动提取最低 glibc 版本并写入依赖；
较旧发行版可在本机从源码构建。

安装包名为 `zc310-ofd-viewer`，包含 `/usr/bin/ofd-viewer`、桌面菜单入口、
`application/ofd` 文件类型、hicolor SVG 图标及许可证。依赖包含 OpenGL/X11/Wayland 运行库、
Zenity 文件选择器和桌面集成工具；安装、升级和卸载时刷新桌面、MIME 和图标缓存。
默认阅读器仍通过应用内的“设为默认阅读器”选择。

```bash
# 安装：选择与系统架构对应的包
sudo apt install ./zc310-ofd-viewer_版本-1_amd64.deb
sudo dnf install ./zc310-ofd-viewer-版本-1.x86_64.rpm

# 卸载
sudo apt remove zc310-ofd-viewer
sudo dnf remove zc310-ofd-viewer
```

具备 nFPM、`dpkg-deb` 和 `rpm` 时，可运行真实包内容检查（只构建和读取安装包）：

```bash
go test ./cmd/ofd-viewer -run '^TestViewer(NativePackages|PackageCacheUpdates)$' -count=1
```

## 多平台编译

以下命令在当前目录执行，编译结果保存到 `dist` 目录：

```bash
mkdir -p dist

# Linux x86_64
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
  go build -o dist/ofd-viewer-linux-amd64 .

# Linux ARM64
GOOS=linux GOARCH=arm64 CGO_ENABLED=1 \
  go build -o dist/ofd-viewer-linux-arm64 .

# Windows x86_64，需要安装 MinGW-w64
CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
  go build -ldflags "-H=windowsgui" -o dist/ofd-viewer-windows-amd64.exe .

# macOS Apple Silicon，需要在 macOS 环境中执行
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
  go build -o dist/ofd-viewer-darwin-arm64 .

# macOS Intel，需要在 macOS 环境中执行
GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 \
  go build -o dist/ofd-viewer-darwin-amd64 .
```

Fyne 桌面程序依赖 CGO 和目标平台的图形开发库。Linux 目标需要 OpenGL/X11 开发库，Windows 目标需要 MinGW-w64；macOS 目标通常应在 macOS 环境中编译。

Windows 版本使用 `-H=windowsgui` 编译，不显示命令行窗口。

Android APK 需要安装 Fyne 命令行工具、Android SDK 和 NDK。以下 Make 命令在仓库根目录执行：

```bash
make package-viewer-android
```

如果只需要 Android 压缩包，可执行：

```bash
make package-viewer-android-zip
```

## Web 打包

[在线体验 Fyne Web 阅读器](https://zc310.github.io/ofd/fyne/)。该版本由 GitHub Pages
工作流构建并部署在 `/ofd/fyne/`，浏览器阅读器位于 [/ofd/wasm/](https://zc310.github.io/ofd/wasm/)。

Web/WASM 版本右侧菜单提供“使用默认回退字体（Noto Sans SC）”，默认关闭。
启用后优先读取浏览器 IndexedDB 中的字体；没有可用缓存时，下载
[Noto Sans SC Regular TTF](https://cdn.jsdelivr.net/npm/@reogrid/font-sc@2.0.0/NotoSansSC-Regular.ttf)，
下载或解析失败再尝试
[WOFF2 备用字体](https://cdn.jsdelivr.net/npm/@fontsource/noto-sans-sc/files/noto-sans-sc-chinese-simplified-400-normal.woff2)。
只有解析成功的字体才写入 IndexedDB 数据库 `ofd-viewer-fonts` 的 `fonts` 存储，
后续启动直接使用缓存，不再请求字体地址；损坏的缓存会重新下载并替换。

选项会被记住。字体加载在后台进行，成功后 Fyne 菜单、对话框等界面文字也使用同一份
Noto Sans SC 字体，并刷新当前页面和缩略图；后续打开的文档和导出也使用该回退字体，
OFD 内嵌字体优先。界面使用下载的 Regular 字重，等宽和符号文字沿用原主题字体。
关闭选项会恢复原主题和文档的默认字体选择，保留
IndexedDB 副本供下次启用。字体加载失败后可再次启用重试；缓存写入失败会显示提示，
当前会话仍可使用已加载的字体。桌面和 Android 版本不显示此选项。
字体来源及许可证见 [Web 阅读器第三方声明](../ofd-wasm/web/THIRD_PARTY_NOTICES.md)。

在仓库根目录安装 Fyne 命令行工具并构建（工具版本与 Pages 工作流一致）：

```bash
go install fyne.io/tools/cmd/fyne@latest
make build-viewer-web
```

生成的 `cmd/ofd-viewer/wasm/` 目录包含页面、WASM 和运行时资源，应用版本从根目录
Makefile 的 `VERSION` 注入。构建需要 Python 3，Makefile 内嵌的 Python 命令将 WASM 的
SHA-256 前 16 位十六进制哈希附加到下载 URL，避免浏览器或 CDN 继续使用旧缓存。
资源使用相对路径，可整体部署到静态站点的子目录。
本地预览可在仓库根目录执行：

```bash
python3 -m http.server 8080 --directory cmd/ofd-viewer/wasm
```

打开 <http://localhost:8080>，浏览器需要启用 WebGL。也可在 `cmd/ofd-viewer` 目录运行
`fyne serve` 本地预览。

点击“打开 OFD”选择本地文件，取消选择会返回阅读器。导出时按原文件名和所选格式
生成下载，保存位置由浏览器设置决定，多页图片/矢量格式仍下载为 ZIP。
Web 导出会先在内存中生成完整文件，大文档需要相应的浏览器可用内存。

## 截图

![linux.png](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-viewer-linux.png)
![windows.png](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-viewer-windows.png)
![android.png](https://github.com/zc310/ofd/releases/download/v0.1.4/ofd-viewer-android.png)
