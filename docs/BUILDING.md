# 构建与打包

以下命令均在仓库根目录执行。Go 工具链要求以 [go.mod](../go.mod) 为准，发行版本由
[Makefile](../Makefile) 的 `VERSION` 统一注入。

## 从源码运行

命令行工具位于 `cmd/`，可用 `go run` 直接运行，例如：

```bash
go run ./cmd/ofd-converter testdata/helloworld.ofd /tmp/helloworld.pdf
go run ./cmd/ofd-validator --format text testdata/helloworld.ofd
```

## 构建与发布包

```bash
# 当前平台的命令行工具
make build-tools

# 命令行工具 ZIP 包
make package-tools

# 当前平台的桌面阅读器与命令行工具包
make package-desktop

# Android APK
make package-viewer-android

# 浏览器阅读器 Web 包
make package-wasm-web
```

构建输出默认位于 `/tmp/ofd/.build`，发布包位于 `/tmp/ofd/dist`，可通过 Make 变量
`BUILD_DIR`、`DIST_DIR` 覆盖。工具 ZIP 包包含二进制与对应 README。

`make package` 会构建桌面、命令行、Android 和 WASM 包，并在 Linux 主机上生成配置启用
的交叉平台包。只构建单个工具可使用 `make package-converter`、`make package-validator`、
`make package-invoice` 等目标；完整列表见 `make help`。

## 平台依赖与交叉编译

| 目标 | 要求与范围 |
|---|---|
| `package-viewer` | 当前平台桌面阅读器；Fyne 需要 CGO 和图形开发库，Linux 需要 OpenGL/X11 开发库。 |
| `package-arm64` | Linux ARM64 命令行工具，`CGO_ENABLED=0`，跳过 viewer。 |
| `package-darwin-arm64` / `package-darwin-amd64` | Linux 主机交叉编译 macOS 命令行工具，跳过 viewer。 |
| `package-windows-amd64` | Windows x86_64 桌面与命令行工具；需要 MinGW-w64，可设 `WINDOWS_CC`。 |
| `package-windows-arm64` | Windows ARM64 需要 LLVM MinGW，可设 `WINDOWS_ARM64_CC`、`WINDOWS_ARM64_CXX`，或 `CC`、`CXX`。 |
| `package-viewer-android` / `package-viewer-android-zip` | Android APK / APK ZIP，需要 Fyne、Android SDK 与 NDK。 |
| `package-wasm-web` | WASM 浏览器阅读器及 Web 资源包。 |

Windows 桌面版使用 `-H=windowsgui`，运行时不显示命令行窗口。`ofd-thumbnailer`
仅构建 Linux 版本，安装方式见[缩略图工具说明](../cmd/ofd-thumbnailer/README.md)。
Fyne 阅读器的多平台构建细节见 [ofd-viewer](../cmd/ofd-viewer/README.md#多平台编译)。

### Windows 7 兼容工具链

Windows 7 用户可尝试使用 [XTLS/go-win7](https://github.com/XTLS/go-win7) 提供的工具链，
或参考 [thongtech/go-legacy-win7](https://github.com/thongtech/go-legacy-win7) 自行编译。
工具链仍需满足 `go.mod` 的要求。在 Linux 上交叉编译命令行工具的示例：

```bash
GOTOOLCHAIN=local make GO=/path/to/go-win7/bin/go GOOS=windows GOARCH=amd64 CGO_ENABLED=0 build-tools
```

`GOTOOLCHAIN=local` 用于固定兼容工具链。若遇到 `bcrypt.dll not found`，请检查系统是否
安装 KB2533623 或包含相应 DLL 加载功能的后续更新。桌面阅读器还需要 MinGW-w64、
Universal CRT 与 OpenGL 驱动；具体功能需在目标 Windows 7 环境中验证。

## 浏览器阅读器

构建并通过 HTTP 启动 WASM 阅读器：

```bash
make build-wasm
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

打开 <http://localhost:8080>，选择本地 `.ofd` 文件。WASM 需要通过 HTTP 服务加载。
API、Worker 协议与缓存说明见 [ofd-wasm](../cmd/ofd-wasm/README.md)，脚本开发和测试见
[Web 维护说明](../cmd/ofd-wasm/web/README.md)。

Fyne 阅读器的 Web 版本打包与本地运行方式见
[ofd-viewer](../cmd/ofd-viewer/README.md#web-打包)。

## 运行依赖

常规 OFD 转 PDF、图片及 PDF 导入使用 Go 实现。以下可选转换方向需要额外工具：

| 转换方向 | 运行依赖 |
|---|---|
| Office → OFD/PDF | LibreOffice。 |
| HTML/MHTML → OFD/PDF | Chrome/Chromium。 |
| XPS → OFD/PDF | libgxps 的 `xpstopdf`。 |
| 图片 → 带 OCR 文字层的 OFD | Tesseract 及对应语言包；未启用 `--ocr` 时不需要。 |

PDF 的 OpenJPEG 解码后端为可选构建项，所需开发库、构建标签和启用方式见
[PDF 支持清单](PDF-SUPPORT.md)。
