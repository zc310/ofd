# 发布包下载指南

在本次 Release 页面的 **Assets** 中下载文件。先按用途选工具，再按操作系统和处理器架构选包。
只想打开、阅读 OFD 文件，选择 `ofd-viewer`；需要批量转换，选择 `ofd-converter`。

## 阅读器选哪个文件

| 你的设备 | 下载文件 | 使用方式 |
|---|---|---|
| Windows，Intel 或 AMD 64 位电脑 | `ofd-viewer-windows-amd64.zip` | 解压后运行 `ofd-viewer.exe` |
| Windows，ARM64 电脑，例如搭载骁龙处理器的设备 | `ofd-viewer-windows-arm64.zip` | 解压后运行 `ofd-viewer.exe` |
| Mac，Apple Silicon（M 系列芯片） | `ofd-viewer-darwin-arm64.zip` | 解压后运行 `ofd-viewer` |
| Mac，Intel 处理器 | `ofd-viewer-darwin-amd64.zip` | 解压后运行 `ofd-viewer` |
| Linux，Intel 或 AMD 64 位电脑 | `ofd-viewer-linux-amd64.zip` | 解压后运行 `ofd-viewer`；也可选下面的 DEB/RPM 安装包 |
| Linux，ARM64 设备 | `ofd-viewer-linux-arm64.zip` | 解压后运行 `ofd-viewer`；也可选下面的 DEB/RPM 安装包 |
| Android 手机或平板 | `ofd-viewer-android.apk` | 下载后安装 APK |
| 使用浏览器阅读 | 无需下载桌面包 | 打开[在线阅读器](https://zc310.github.io/ofd/wasm/) |

桌面 ZIP 包内是可执行程序和工具说明；macOS 包内提供 `ofd-viewer` 可执行文件。
Linux 桌面阅读器需要 OpenGL、X11/Wayland 等运行库；DEB/RPM 安装包会声明相应依赖，
适合通过系统包管理器安装。系统要求和完整用法见[阅读器说明](https://github.com/zc310/ofd/blob/main/cmd/ofd-viewer/README.md)。

## 文件名里的系统和架构

普通 ZIP 包采用 `工具名-操作系统-架构.zip`，例如：

```text
ofd-converter-windows-amd64.zip
└─ 转换工具    └─ Windows └─ Intel/AMD 64 位
```

| 名称 | 含义 |
|---|---|
| `windows` | Windows 系统 |
| `darwin` | macOS 系统，这是 Go 使用的平台名称 |
| `linux` | Linux 系统 |
| `amd64`、`x86_64` | 同一种 64 位 x86 架构，Intel 和 AMD 的常见电脑均使用；`amd64` 也适用于 Intel 处理器 |
| `arm64`、`aarch64` | 同一种 64 位 ARM 架构，例如 Apple Silicon、Windows ARM 电脑和 ARM64 Linux 设备 |

选择时同时核对系统与架构：`darwin-arm64` 对应 Apple Silicon Mac，`windows-arm64` 对应 Windows ARM 电脑。
Windows 可在“设置 → 系统 → 关于 → 系统类型”查看；Mac 可在“关于本机”查看芯片或处理器；
Linux 可运行 `uname -m`，`x86_64` 选 `amd64`，`aarch64` 选 `arm64`。
当前自动发布的桌面包均为 64 位版本。

## 工具名前缀表示什么

下面的命令行工具使用相同的平台后缀。例如 Windows Intel/AMD 电脑使用
`ofd-converter-windows-amd64.zip`，Apple Silicon Mac 使用 `ofd-converter-darwin-arm64.zip`。
每个 ZIP 是一个独立工具包，解压后可查看其中的 `README.md`。

| 工具名前缀 | 用途 |
|---|---|
| `ofd-viewer` | 带图形界面的 OFD 阅读器 |
| `ofd-converter` | OFD/PDF/图片/Office 等格式转换 |
| `ofd-creator` | 从 JSON/YAML 清单创建 OFD，以及合并、拆分、编辑等操作 |
| `ofd-validator` | 校验 OFD 文档结构、引用和语义 |
| `ofd-analyzer` | 分析页面、资源、字体、签名等文档结构 |
| `ofd-compare` | 比较文档内容与页面画面，生成差异报告 |
| `ofd-archive` | 档案预检、长期保存处理和固定性验证 |
| `ofd-invoice` | 发票信息提取与汇总 |
| `ofd-seal` | 电子印章工具 |
| `ofd-signer-demo` | 外部签署协议示例程序 |
| `ofd-crypt` | OFD 加密、解密和完整性检查 |
| `ofd-server` | HTTP 服务 |
| `ofd-thumbnailer` | Linux 文件管理器缩略图工具，仅提供 Linux 包 |

Office、HTML、OCR 等部分转换功能需要额外程序，见[运行依赖](https://github.com/zc310/ofd/blob/main/docs/BUILDING.md#运行依赖)。

## Linux 的 DEB 和 RPM 怎么选

DEB 适用于 Debian、Ubuntu 等使用 APT 的发行版；RPM 适用于 Fedora 等使用 RPM/DNF 的发行版。
阅读器安装包包含桌面入口、图标和 OFD 文件类型定义；缩略图安装包用于文件管理器的 OFD/ZIP 缩略图。

下表中的 `<版本>` 对应本次 Release 的版本号，去掉开头的 `v`；`-1` 是安装包的发行号。

| 文件名 | 系统与架构 |
|---|---|
| `zc310-ofd-viewer_<版本>-1_amd64.deb` | Debian/Ubuntu，Intel/AMD 64 位，阅读器 |
| `zc310-ofd-viewer_<版本>-1_arm64.deb` | Debian/Ubuntu，ARM64，阅读器 |
| `zc310-ofd-viewer-<版本>-1.x86_64.rpm` | RPM 发行版，Intel/AMD 64 位，阅读器 |
| `zc310-ofd-viewer-<版本>-1.aarch64.rpm` | RPM 发行版，ARM64，阅读器 |
| `zc310-ofd-thumbnailer_<版本>-1_amd64.deb` | Debian/Ubuntu，Intel/AMD 64 位，缩略图工具 |
| `zc310-ofd-thumbnailer_<版本>-1_arm64.deb` | Debian/Ubuntu，ARM64，缩略图工具 |
| `zc310-ofd-thumbnailer-<版本>-1.x86_64.rpm` | RPM 发行版，Intel/AMD 64 位，缩略图工具 |
| `zc310-ofd-thumbnailer-<版本>-1.aarch64.rpm` | RPM 发行版，ARM64，缩略图工具 |

在下载目录中安装，文件名替换为实际下载的名称：

```bash
sudo apt install ./zc310-ofd-viewer_版本-1_amd64.deb
# 或
sudo dnf install ./zc310-ofd-viewer-版本-1.x86_64.rpm
```

执行命令前替换 `<版本>`。系统包的依赖要求由包管理器检查；较旧的 Linux 发行版可参考
[构建指南](https://github.com/zc310/ofd/blob/main/docs/BUILDING.md)在本机编译。

## Android、Web 和源码附件

| 文件名或附件名称 | 内容与用途 |
|---|---|
| `ofd-viewer-android.apk` | 可直接安装的 Android 阅读器 |
| `ofd-viewer-android.zip` | 包含同一个 APK 和许可说明，解压后安装 APK |
| `ofd-wasm-web.zip` | 用于自行部署浏览器阅读器的网页、脚本和 WASM 资源；通过 HTTP 服务访问 |
| `Source code (zip)` / `Source code (tar.gz)` | GitHub 自动生成的源码归档，用于开发或自行编译 |

普通 ZIP 文件名不包含版本号，版本以所在 Release 的标签为准。以上名称对应当前自动发布流程，
较早版本的附件可能有所不同，以该版本 Assets 列表为准。
