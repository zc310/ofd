# OFD Go Toolkit [![GoDoc](https://pkg.go.dev/badge/github.com/zc310/ofd.svg)](https://pkg.go.dev/github.com/zc310/ofd)

一个用于解析、创建、转换、渲染、校验和分析 OFD 文档的 Go 工具包。

> 使用本项目及其输出结果前，请阅读 [免责声明](DISCLAIMER.md)。本项目不保证所有 OFD 文件的解析、转换、校验或渲染结果适用于特定业务、法律或合规场景。如发现仓库、文档或示例中可能存在侵权内容，请通过 [GitHub Issues](https://github.com/zc310/ofd/issues/new) 联系维护者。

## OFD 简介

OFD（Open Fixed-layout Document）是中国电子文件领域常用的版式文档格式，主要用于保存版面固定、跨设备显示一致的电子文件。OFD 文件通常以 ZIP 容器组织 XML 文档、页面内容、文字、路径、图片、字体、附件和签名等资源；页面可以包含文字、矢量图形和栅格图像，并保留相应的布局和绘制信息。项目在 [`docs/standards/`](docs/standards/) 目录中附带了 GB/T 33190-2016《电子文件存储与交换格式 版式文档》相关标准资料，供格式学习和开发参考。

OFD 适用于电子证照、电子发票、数字档案、公文和其他需要长期保存或交换的版式文件。本项目围绕 OFD 文档提供解析、创建、渲染、转换、校验、分析和阅读能力，支持将 OFD 内容输出为 PDF、单文件 HTML、PNG、JPG、SVG、纯文本和 Markdown 等格式。不同厂商生成的 OFD 文件可能存在扩展或兼容性差异，具体支持范围请参见 [`docs/OFD-SUPPORT.md`](docs/OFD-SUPPORT.md)。

## 功能特性

| 类别           | 功能                                                                         |
|----------------|------------------------------------------------------------------------------|
| **文档转换**   | OFD 转 PDF、单文件 HTML、纯文本、Markdown 和 PNG/JPG 等图像格式              |
| **页面处理**   | 支持多文档体、多页面转换，按文档体顺序合并并使用全局页码                     |
| **灵活配置**   | 支持自定义 DPI、背景颜色和页面选择                                           |
| **OFD 校验**   | 基于 `OFD-Schema` 校验 ZIP、XML、引用和 XSD，并生成报告                      |
| **OFD 分析**   | 深入分析文档结构、页面对象、文字、资源、附件、注解、签名和引用关系，支持报告 |
| **桌面阅读**   | 提供基于 Fyne 的 Linux、Windows OFD 桌面阅读器                               |
| **安卓阅读**   | 支持 Android 文件选择、文档阅读和 APK 打包                                   |
| **浏览器阅读** | 提供基于 Web Worker 和 WASM 的 OFD 阅读器                                    |
| **性能基准**   | 提供跨语言 OFD 转 PDF 速度、文件大小、文本提取和兼容性对比测试               |
| **处理性能**   | 基于 Go 语言开发，支持高效处理                                               |

OFD 标准元素和项目能力的详细支持范围见 [`docs/OFD-SUPPORT.md`](docs/OFD-SUPPORT.md)。该清单区分了已支持、部分支持和暂不承诺完整支持的能力，不应将项目功能表述为完整符合 GB/T 33190-2016。

性能基准测试见 [`zc310/ofd-benchmark`](https://github.com/zc310/ofd-benchmark)。该项目对比多个语言和实现的 OFD 转 PDF 速度、输出文件大小、文本提取能力及兼容性；测试结果仅供参考，实际表现会受到文档样本、字体、操作系统和运行环境影响。

## 桌面与 Android 阅读器

项目提供基于 Fyne 的 `ofd-viewer` 图形化阅读器，支持 Linux 和 Windows 桌面环境，也支持构建 Android APK 阅读器。阅读器支持打开 OFD 文件、连续阅读、缩放、页面导航、双页显示、文字搜索以及导出 PDF、图片和文本等功能。

桌面版和 Android 版的详细功能、构建要求、Android APK 打包说明及截图见 [`cmd/ofd-viewer/README.md`](cmd/ofd-viewer/README.md)。已经发布的 Linux、Windows 桌面版和 Android APK 可在 [GitHub Releases](https://github.com/zc310/ofd/releases) 下载。

常用构建命令如下：

```bash
# Linux
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 make package

# Windows x86_64，需要 MinGW-w64
make package-windows-amd64

# Android APK，需要 Fyne、Android SDK 和 NDK
make package-viewer-android
```

Linux 和 Windows 桌面版需要目标平台的图形开发环境；Windows 交叉编译需要 MinGW-w64。Android 版以 APK 形式安装，构建前需要配置 Android SDK、NDK 及 Fyne 命令行工具。

## WASM 浏览器阅读器

项目提供一个无需前端框架的 OFD 浏览器阅读器示例，位于 [`cmd/ofd-wasm`](cmd/ofd-wasm)。解析和渲染运行在单个 Web Worker 中，使用一个 WASM 实例和一个 Reader 管理文档状态；页面和缩略图按可视区域附近懒加载，并分别缓存渲染结果。

在线预览：<https://zc310.github.io/ofd-wasm/>

### 构建和运行

在仓库根目录执行：

```bash
make build-wasm
python3 -m http.server 8080 --directory cmd/ofd-wasm/web
```

然后打开 <http://localhost:8080>，选择 `.ofd` 文件。浏览器不能通过 `file://` 直接加载 WASM，因此必须使用 HTTP 服务访问。

### 阅读器功能

- 连续页面阅读、缩略图导航和当前页定位
- 按需渲染正文和缩略图，支持 50% 到 300% 缩放
- 适应宽度、适应页面、页面旋转和浏览器全屏阅读模式
- 文档内文字搜索、搜索高亮和文字层复制
- 支持单页、双页和“双页，奇数页在左”阅读布局
- 按当前页、全部页面或自定义范围打印和导出，支持 PNG/JPG/PDF/TXT、DPI、背景颜色及多页图片 ZIP，支持复制文字和下载全文
- 支持拖放打开 OFD、最近打开文件、恢复上次阅读页和取消长时间操作
- 桌面端键盘导航，以及手机端单指横滑翻页和双指捏合缩放

正文和缩略图 PNG 默认使用透明背景，未绘制区域保留 Alpha 通道。WASM API 也可以显式传入透明背景：

```javascript
ofd.renderPage(0, { dpi: 96, background: '#00000000' })
ofd.renderPages([0, 1, 2], { dpi: 36, background: '#00000000' })
```

手机端工具栏按功能分行显示：打开文件和最近文件一行，页面导航和缩放控制各占一行，搜索和显示设置一行。单行内容过长时可以横向滚动，不会挤压页面或撑破屏幕。完整的 WASM API、Worker 协议、字体加载和缓存说明见 [`cmd/ofd-wasm/README.md`](cmd/ofd-wasm/README.md)。

## 命令行程序打包

根目录 `Makefile` 可以将命令行程序分别编译并打包为独立 ZIP 文件。Linux 下默认 `make` 会同时构建 Linux amd64/ARM64、macOS ARM64/x86_64、Windows x86_64/ARM64 和 Android APK ZIP；在其他系统上执行默认 `make` 时，会构建该系统支持的程序。`ofd-thumbnailer` 仅编译 Linux 版本：

```bash
# Linux 默认构建会生成 Linux amd64/ARM64、macOS、Windows x86_64/ARM64 程序和 Android APK ZIP
make package
```

Linux 默认构建 Windows x86_64 需要安装 MinGW-w64。也可以单独执行：

```bash
make package-windows-amd64
```

如果交叉编译器不在默认路径，可以通过 `WINDOWS_CC` 指定：

```bash
WINDOWS_CC=/opt/mingw/bin/x86_64-w64-mingw32-gcc make package-windows-amd64
```

编译并打包 Linux ARM64 版本。由于 ARM64 Linux 环境缺少 `ofd-viewer` 所需的部分图形库，该目标跳过 `ofd-viewer`，并使用纯 Go 模式构建其他程序：

```bash
make package-arm64
```

在 Linux 环境中交叉编译 macOS ARM64 和 x86_64 版本。由于 `ofd-viewer` 依赖 macOS 图形开发环境，这两个目标会跳过 viewer：

```bash
make package-darwin-arm64
make package-darwin-amd64
```

在 Linux 环境中交叉编译 Windows ARM64 版本。由于 ARM64 Windows 不构建依赖 CGO 的 `ofd-viewer`，该目标只生成三个命令行程序：

```bash
make package-windows-arm64
```

输出文件为 `ofd-converter-windows-arm64.zip`、`ofd-validator-windows-arm64.zip` 和 `ofd-analyzer-windows-arm64.zip`。这三个程序不使用 CGO，因此不需要安装 MinGW-w64。

Linux amd64 默认构建包含 `ofd-viewer`；Linux ARM64 和 Linux 主机交叉编译 macOS 时跳过 `ofd-viewer`。`ofd-thumbnailer` 始终只在 Linux 目标中构建，并包含 Linux ARM64 版本。

默认 `make` 会构建 Android APK 和 APK ZIP。如只需要构建 Android APK，可执行 `make package-viewer-android`；如需要 APK ZIP，可执行 `make package-viewer-android-zip`。

单独构建 OFD 校验工具：

```bash
make package-validator
```

Linux amd64 默认会生成 Linux amd64/ARM64、macOS、Windows x86_64/ARM64 程序包以及 Android APK 和 APK ZIP；
Linux ARM64 会生成四个 Linux 程序包；Windows 和 macOS 下的默认构建会生成
`ofd-viewer`、`ofd-converter`、`ofd-validator` 和 `ofd-analyzer` 四个 ZIP，不会编译
`ofd-thumbnailer`。

每个 ZIP 包都包含对应的二进制文件和 README。`ofd-thumbnailer` 的安装包还包含 `ofd.thumbnailer`；Linux 安装包额外包含 `install.sh`，解压后可执行：

```bash
cd ofd-thumbnailer
sudo ./install.sh
```

可以通过 `GOOS`、`GOARCH` 和 `CGO_ENABLED` 指定目标平台，例如：

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 make package
```

Windows 交叉编译示例：

```bash
# Debian/Ubuntu
sudo apt install gcc-mingw-w64-x86-64

GOOS=windows CGO_ENABLED=1 GOARCH=amd64 make package
```

Windows amd64 的 `ofd-viewer` 和其他启用 CGO 的目标必须使用 MinGW-w64，不能使用 Linux 的 `gcc`。如果交叉编译器不在默认路径，可以显式指定：

```bash
CC=x86_64-w64-mingw32-gcc GOOS=windows CGO_ENABLED=1 GOARCH=amd64 make package
```

Fyne 查看器需要目标平台的图形开发库。Windows 版查看器使用 `-H=windowsgui` 编译，不显示命令行窗口。

## Flatpak

仓库根目录包含 `io.github.zc310.ofd.json` 及其桌面集成文件，可用于构建 OFD Viewer 的 Flatpak 包。构建依赖已通过 `go.mod.json` 和 `modules.txt` 固定，避免 Flatpak 构建阶段联网下载 Go 模块。

```bash
flatpak run org.flatpak.Builder --user --install --force-clean --disable-rofiles-fuse build-dir io.github.zc310.ofd.json
flatpak run io.github.zc310.ofd
```

正式提交 Flathub 前，需要将 manifest 中的源码 commit 更新为包含当前 Flatpak 文件的 `v0.0.5` 发布 commit，并在本机安装 `flatpak-builder`、`appstreamcli` 后完成元数据检查。

Flatpak 版本以只读方式访问真实的 `~/.local/share/fonts` 用户字体目录和宿主机系统目录（`host-os`），用于渲染未内嵌字体的 OFD 文档。

## 快速开始

### 作为 Go 库使用

在已有的 Go 项目中添加 OFD 依赖：

```bash
go get github.com/zc310/ofd@latest
```

该命令用于将本项目作为 Go 库引入，不会安装桌面阅读器或命令行程序。桌面版和命令行程序请参考上面的构建说明及后续的打包章节。

### 创建 OFD

创建器使用 `github.com/beevik/etree` 生成 XML，并使用标准库 ZIP 写入 OFD 包；现有解析器仍使用原有 XML 模型。支持元数据、多页、文字、路径、图片和嵌入字体：

```go
package main

import (
	"log"

	"github.com/zc310/ofd/pkg/creator"
)

func main() {
	err := creator.CreateFile(creator.Document{
		ID:    "example-document",
		Title: "创建示例",
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{
				X:      20,
				Y:      30,
				Width:  100,
				Height: 10,
				Size:   4.233,
				Font:   "SimSun",
				Value:  "你好，OFD",
			},
		}}}},
	}, "output.ofd")
	if err != nil {
		log.Fatal(err)
	}
}
```

页面坐标、边界和文字字号的单位均为毫米。`PageSize` 未设置时默认使用 A4；输出 XML 使用 OFD 默认命名空间，普通属性保持无命名空间。

可以使用 `Document.DrawParams` 抽取公共线条和颜色配置。图层、文字、路径和图片通过绘制参数的 `Name` 引用；`Relative` 可声明绘制参数继承关系：

```go
document := creator.Document{
	DrawParams: []creator.DrawParam{
		{
			Name:      "base-line",
			LineWidth: 0.5,
			Cap:       "Round",
			Join:      "Miter",
		},
		{
			Name:        "blue-line",
			Relative:    "base-line",
			StrokeColor: &creator.Color{R: 20, G: 40, B: 200},
		},
	},
	Pages: []creator.Page{{Layers: []creator.Layer{{
		Type:      creator.LayerForeground,
		DrawParam: "blue-line",
		Items: []creator.Item{
			creator.Path{
				X: 1, Y: 1, Width: 80, Height: 60,
				Data: "M 0 0 L 80 60 C", DrawParam: "base-line",
			},
		},
	}}}},
}
```

创建器会自动分配资源 ID、生成 `DocumentRes.xml`，并检查引用不存在和继承循环等错误。

文字对象可以使用 `TextCodes` 生成多个文字分段，并指定分段起点和字符间距；未设置 `TextCodes` 时仍使用 `Value` 生成单个 `TextCode`。`CTM` 用于设置文字对象的六参数坐标变换矩阵：

```go
x, y := 20.0, 30.0
text := creator.Text{
	X: 10, Y: 10, Width: 100, Height: 12,
	CTM: &creator.CTM{1, 0, 0, 1, 3, 4},
	TextCodes: []creator.TextCode{
		{Value: "第一段", X: &x, Y: &y, DeltaX: []float64{1, 2, 3}},
		{Value: "第二段", DeltaY: []float64{4, 5}},
	},
}
```

创建器默认不会自动生成 `DeltaX` 和 `DeltaY`。如需根据字体度量补全多字符文字的字符间距，可通过 `CreateOptions{CompleteTextCodeDeltas: true}` 启用；命令行工具对应使用 `--complete-text-code-deltas`。

如果需要指定字形编号映射，可以使用 `CGTransforms`。`CodePosition` 是相对于整个文字对象的字符位置，从 `0` 开始；`Glyphs` 中的编号使用字体字形编号：

```go
creator.Text{
	Value: "字形映射",
	CGTransforms: []creator.CGTransform{
		{
			CodePosition: 1,
			CodeCount:    1,
			GlyphCount:   2,
			Glyphs:       []int{120, 121},
		},
	},
}
```

文字、路径和图片对象都支持六参数 `CTM` 以及 `Clips` 裁剪。`Clips` 中每个 `Clip` 可以包含一个或多个 `Area`；每个 `Area` 必须且只能设置 `Path` 或 `Text`。裁剪路径和裁剪文字也可以单独设置 `CTM`，裁剪区域可以引用文档中的 `DrawParam`：

```go
document := creator.Document{
	DrawParams: []creator.DrawParam{{Name: "clip-style", LineWidth: 0.2}},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Image{
			X: 10, Y: 10, Width: 80, Height: 60, Data: pngData,
			CTM: &creator.CTM{1, 0, 0, 1, 5, 5},
			Clips: &creator.Clips{Items: []creator.Clip{{Areas: []creator.ClipArea{{
				DrawParam: "clip-style",
				CTM:       &creator.CTM{1, 0, 0, 1, 2, 3},
				Path: &creator.ClipPath{
					Boundary: creator.Box{X: 0, Y: 0, Width: 50, Height: 40},
					Data:     "M 0 0 L 50 0 L 50 40 L 0 40 C",
					Fill:     true,
				},
			}}}}},
		},
	}}},
}
```

创建器会校验裁剪边界、路径数据、CTM、裁剪文字内容以及 `DrawParam` 引用；不存在的绘制参数不会被静默省略。`Clips` 使用路径或文字时应至少包含一个 `Clip`，每个 `Clip` 至少包含一个 `Area`。

同一页面的注解应合并到一个 `AnnotationPage` 中；创建器不允许重复声明同一个页面的注解文件。

文档还可以生成大纲和书签。`Outline.Children` 用于嵌套章节；大纲动作和书签目标中的 `Page` 同样使用从 `0` 开始的页面索引：

```go
document := creator.Document{
	Outlines: []creator.Outline{{
		Title: "第一章",
		Actions: []creator.Action{{Goto: &creator.GotoAction{Page: 0, Type: "Fit"}}},
		Children: []creator.Outline{{Title: "第一节"}},
	}},
	Bookmarks: []creator.Bookmark{{
		Name: "首页",
		Goto: creator.GotoAction{Page: 0, Type: "Fit"},
	}},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Text{X: 10, Y: 10, Width: 50, Height: 8, Value: "正文"},
	}}},
}
```

动作也可以跳转到文档书签。书签目标与页面目标互斥，引用的名称必须已经在 `Document.Bookmarks` 中声明：

```go
document := creator.Document{
	Bookmarks: []creator.Bookmark{{
		Name: "chapter-1",
		Goto: creator.GotoAction{Page: 0, Type: "Fit"},
	}},
	Actions: []creator.Action{{
		Goto: &creator.GotoAction{Bookmark: "chapter-1"},
	}},
	Pages: []creator.Page{{}},
}
```

注意，`Document.Bookmarks` 本身的 `Goto` 必须使用页面 `Dest`；只有普通 `Action.Goto` 可以使用 `Bookmark` 目标。

页面目标参数会按类型校验：`FitH` 只接受 `Top`，`FitV` 只接受 `Left`，`FitR` 必须提供边界顺序正确的完整 `Left/Top/Right/Bottom` 矩形，`Fit` 不接受位置或缩放参数。

渐变的起止位置必须是两个有限数值，渐变分段位置必须位于 `0` 到 `1` 之间。

颜色未指定 `ColorSpace` 时，`Components` 按默认 RGB 处理，因此必须提供三个分量；指定颜色空间时，分量数量必须匹配该颜色空间。

颜色分量和调色板值还必须落在 `BitsPerComponent` 对应的范围内；未设置位数时按 Schema 默认的 8 位处理。

可以通过 `Permissions` 设置编辑、导出、打印和权限有效期，通过 `Preferences` 设置阅读器打开文档时的页面模式、页面布局、界面显示和缩放方式。`ZoomMode` 与具体的 `Zoom` 比例只能二选一：

```go
document := creator.Document{
	Permissions: &creator.Permissions{
		Edit: &edit,
		Print: &creator.PrintSettings{Printable: true, Copies: &copies},
	},
	Preferences: &creator.ViewPreferences{
		PageMode:    creator.PageModeFullScreen,
		PageLayout:  creator.PageLayoutOneColumn,
		HideToolbar: &hideToolbar,
		ZoomMode:    creator.ZoomModeFitWidth,
	},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Text{X: 10, Y: 10, Width: 50, Height: 8, Value: "正文"},
	}}},
}
```

文档可以定义模板页，并在页面上通过 `TemplateRef` 引用。模板页的 `ID` 必须显式指定且不能与文档中的其他资源 ID 冲突；模板页内容支持与普通页面相同的图层、文字、路径和图片对象：

```go
document := creator.Document{
	Templates: []creator.TemplatePage{{
		ID:     10,
		Name:   "页眉模板",
		ZOrder: "Background",
		Items: []creator.Item{
			creator.Path{X: 0, Y: 0, Width: 210, Height: 5, Data: "M 0 0 L 210 0 L 210 5 C", Fill: true},
		},
	}},
	Pages: []creator.Page{{
		Templates: []creator.TemplateRef{{ID: 10, ZOrder: "Background"}},
		Items:     []creator.Item{creator.Text{X: 10, Y: 20, Width: 80, Height: 8, Value: "正文"}},
	}},
}
```

页面模板引用的 `ZOrder` 只能是 `Background` 或 `Foreground`，未设置时使用 OFD 默认值。

还可以定义复合图元资源，并在页面或模板页中通过 `Composite` 引用。复合图元资源的 `ID` 必须显式指定且不能与其他已分配资源 ID 冲突：

```go
document := creator.Document{
	Composites: []creator.CompositeGraphicUnit{{
		ID: 20, Width: 40, Height: 30,
		Items: []creator.Item{
			creator.Path{X: 0, Y: 0, Width: 40, Height: 30, Data: "M 0 0 L 40 0 L 40 30 C", Fill: true},
		},
	}},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Composite{X: 10, Y: 20, Width: 40, Height: 30, ResourceID: 20},
	}}},
}
```

颜色支持自定义颜色空间和轴向、径向渐变。颜色空间资源的 `ID` 必须显式指定；使用自定义颜色分量时，`Components` 按当前颜色空间的通道顺序填写：

```go
document := creator.Document{
	ColorSpaces: []creator.ColorSpace{{ID: 30, Type: "RGB", BitsPerComponent: 8}},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Path{
			X: 10, Y: 10, Width: 80, Height: 40,
			Data: "M 0 0 L 80 0 L 80 40 L 0 40 C", Fill: true,
			FillColor: &creator.Color{ColorSpace: 30, Components: []int{40, 100, 220}},
		},
	}}},
}
```

渐变颜色使用 `Color.Axial` 或 `Color.Radial`，每种颜色至少需要两个 `Segments`，并且不能同时设置两种渐变。

对于网格渐变，可以使用 `Color.Gouraud` 或 `Color.LaGouraud`。前者至少需要三个控制点，后者需要设置 `VerticesPerRow`，且控制点数量必须是每行顶点数的整数倍。

图案填充使用 `Color.Pattern`，图案单元中的对象配置在 `Items` 或 `Layers` 中：

```go
pattern := &creator.Pattern{
	Width: 10, Height: 10, XStep: 10, YStep: 10,
	ReflectMethod: "RowAndColumn", RelativeTo: "Object",
	Items: []creator.Item{
		creator.Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", Fill: true},
	},
}
document := creator.Document{
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Path{
			X: 10, Y: 10, Width: 80, Height: 40,
			Data: "M 0 0 L 80 0 L 80 40 C", Fill: true,
			FillColor: &creator.Color{Pattern: pattern},
		},
	}}},
}
```

图案会生成符合 OFD `CT_Pattern` 的 `CellContent`，并复用现有文字、路径、图片和复合图元 API。图案宽高必须为正数，且不能与渐变同时设置。

图案也可以通过 `Pattern.Thumbnail` 引用文档级或页面级图片资源作为单元缩略图。

颜色空间可以嵌入 Profile 文件，并会自动校验颜色分量数量：

```go
document := creator.Document{
	ColorSpaces: []creator.ColorSpace{{
		ID: 30, Type: "CMYK", BitsPerComponent: 8,
		ProfileData: []byte(iccProfileData),
	}},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Path{
			X: 10, Y: 10, Width: 40, Height: 30,
			Data: "M 0 0 L 40 0 L 40 30 C", Fill: true,
			FillColor: &creator.Color{ColorSpace: 30, Components: []int{1, 2, 3, 4}},
		},
	}}},
}
```

Profile 数据会写入 `Doc_0/Res/Profiles/`，`GRAY`、`RGB` 和 `CMYK` 分别要求 1、3 和 4 个颜色分量。

可以通过 `DefaultCS` 指定文档默认颜色空间。该 ID 必须对应 `ColorSpaces` 中声明的资源：

```go
document := creator.Document{
	ColorSpaces: []creator.ColorSpace{{ID: 30, Type: "RGB"}},
	DefaultCS:   30,
	Pages:       []creator.Page{{}},
}
```

如果需要拆分或复用资源文件，可以通过 `PublicRes` 嵌入额外的公共资源 XML。文件名相对于 `Doc_0`，内容根元素必须为 `Res`：

```go
document := creator.Document{
	PublicRes: []creator.PublicResource{{
		Name: "Public/SharedRes.xml",
		Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."></Res>`),
	}},
	Pages: []creator.Page{{}},
}
```

公共资源文件会写入 `Doc_0/Public/`，并在 `Document.xml/CommonData` 中生成 `PublicRes` 引用；路径不能绝对化、穿越或重复。

创建器还会在最终写入前检查整个 OFD ZIP 的条目路径，公共资源不能覆盖 `Document.xml`、页面、资源清单或其他系统文件。

所有最终 ZIP 条目都必须是规范化的包内相对路径，不允许绝对路径、反斜杠、NUL 或路径穿越。

显式资源 ID 不要求按输入顺序递增；创建器会预留文档、页面和原始资源中的显式 ID，并让自动分配跳过这些 ID。接近 `uint64` 上限的显式资源 ID 会被拒绝，避免生成溢出的零 ID。

所有写入 OFD XML 的数值 ID 和引用 ID 还必须符合 Schema 的 `xs:unsignedInt` 范围，即不超过 `uint32` 上限。

附件、签名和版本等 XML 标识符必须本身就是合法 XML ID，首尾空白不会被自动裁剪。

签名引用和文档版本文件清单不仅要满足路径安全规则，还必须实际指向最终 OFD ZIP 中存在的条目。

这些包内路径的首尾空白也会被拒绝，不会在校验时自动裁剪后再按原始值写入 XML。

公共资源根元素和版本根文件必须使用 OFD 命名空间；版本根文件的根元素必须是 `Document`。

每个文档版本至少需要一个文件清单项，且版本 `Index` 在同一文档中必须唯一。

文档元数据支持摘要、用途、封面、关键词和自定义数据：

```go
document := creator.Document{
	ID:       "metadata-test",
	Title:    "标题",
	Author:   "作者",
	Abstract: "摘要",
	DocUsage: "Report",
	Keywords: []string{"ofd", "metadata"},
	CustomDatas: []creator.CustomData{{
		Name: "Department", Value: "Engineering",
	}},
	CoverData: coverImageBytes,
	Pages:     []creator.Page{{}},
}
```

封面数据会写入 `Doc_0/Cover/`，并自动在 `DocInfo` 中生成 `Cover` 路径；关键词和自定义数据会按 OFD Schema 顺序写入 `DocInfo`。

页面注解通过 `Annotations` 配置，`AnnotationPage.Page` 使用从 `0` 开始的页面索引。注解必须指定唯一的 `ID`、合法的 `Type`、`Creator` 和外观内容：

```go
document := creator.Document{
	Annotations: []creator.AnnotationPage{{
		Page: 0,
		Items: []creator.Annotation{{
			ID: 50, Type: "Highlight", Creator: "ofd-creator",
			Remark: "重点内容",
			Boundary: &creator.Box{X: 10, Y: 20, Width: 40, Height: 8},
			Items: []creator.Item{
				creator.Path{X: 10, Y: 20, Width: 40, Height: 8, Data: "M 0 0 L 40 0 L 40 8 C", Fill: true},
			},
		}},
	}},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Text{X: 10, Y: 10, Width: 50, Height: 8, Value: "正文"},
	}}},
}
```

文档、页面和文字/路径/图片对象都可以配置 URI 或页面跳转动作。页面跳转的 `Page` 使用从 `0` 开始的页面索引，未指定 `Event` 时默认为点击事件：

```go
top := 40.0
document := creator.Document{
	Actions: []creator.Action{{
		Event: creator.ActionEventDO,
		URI:   &creator.URIAction{URI: "https://example.com/document", Target: "_blank"},
	}},
	Pages: []creator.Page{{
		Items: []creator.Item{
			creator.Text{
				X: 10, Y: 10, Width: 80, Height: 10,
				Value: "打开第二页",
				Actions: []creator.Action{{
					Goto: &creator.GotoAction{Page: 1, Type: "FitH", Top: &top},
				}},
			},
		},
	}}},
}
```

一个动作必须且只能设置 `URI`、`Goto`、`Sound` 或 `Movie` 之一；创建器会检查 URI 是否为空、页面索引是否越界、目标类型是否符合 OFD 规范，以及多媒体资源引用是否匹配。

动作还可以配置触发区域。区域由一个或多个 `Area` 组成，每个区域支持移动、直线、贝塞尔曲线、椭圆弧和闭合命令：

```go
document := creator.Document{
	Actions: []creator.Action{{
		Region: &creator.ActionRegion{Areas: []creator.ActionArea{{
			Start: creator.Point{X: 10, Y: 10},
			Commands: []creator.RegionCommand{
				creator.RegionMove{Point: creator.Point{X: 20, Y: 10}},
				creator.RegionLine{Point: creator.Point{X: 60, Y: 10}},
				creator.RegionLine{Point: creator.Point{X: 60, Y: 30}},
				creator.RegionClose{},
			},
		}}}},
		URI: &creator.URIAction{URI: "https://example.com"},
	}},
	Pages: []creator.Page{{}},
}
```

区域坐标和曲线参数必须是有限数值；每个区域至少需要一个路径命令。

音频和视频可以通过 `Media` 注册为文档资源，再由 `SoundAction` 或 `MovieAction` 引用：

```go
volume := 80
document := creator.Document{
	Media: []creator.Media{
		{ID: 40, Type: "Audio", Format: "mp3", Data: audioBytes},
		{ID: 41, Type: "Video", Format: "mp4", Data: videoBytes},
	},
	Actions: []creator.Action{{
		Event: creator.ActionEventDO,
		Sound: &creator.SoundAction{ResourceID: 40, Volume: &volume},
	}},
	Pages: []creator.Page{{Actions: []creator.Action{{
		Movie: &creator.MovieAction{ResourceID: 41, Operator: "Play"},
	}}}},
}
```

多媒体数据会写入 `Doc_0/Res/Media/`，资源 ID 必须唯一；声音动作只能引用音频，影片动作只能引用视频。

媒体和附件格式会去除前导点并规范化为小写；格式值不能包含路径分隔符或 NUL。

文档、附件、扩展、签名、版本和注解日期会按 XML Schema 的日期范围校验，年份必须位于 `1` 到 `9999`。

注解未设置 `LastModDate` 时，会优先使用文档修改日期、创建日期，否则使用固定的 `1970-01-01`，保证相同输入生成稳定输出。

颜色空间 Profile 条目会按颜色空间 ID 排序写入 ZIP，保证包含多个 Profile 时输出顺序稳定。

多个颜色空间可以共享相同的 Profile 数据；相同 Profile 路径但数据不同会被拒绝，避免生成冲突的 ZIP 条目。签名引用和版本文件清单必须使用规范化的包内相对路径，签名从 `Signatures` 目录返回文档根目录的 `../Document.xml` 除外。

`Marshal`/`Create` 不会把自动生成的 Profile 路径、签名摘要或图案构建缓存写回调用方的 `Document` 及其嵌套对象；同一个输入对象可以重复生成。

`Media.Type` 也可以设置为 `Image`，供图片对象或复合图元的缩略图、替代资源引用：

```go
document := creator.Document{
	Media: []creator.Media{{ID: 50, Type: "Image", Format: "png", Data: imageBytes}},
	Composites: []creator.CompositeGraphicUnit{{
		ID: 60, Width: 40, Height: 30,
		Thumbnail: 50, Substitution: 50,
		Items: []creator.Item{creator.Path{
			X: 0, Y: 0, Width: 40, Height: 30,
			Data: "M 0 0 L 40 30 C",
		}},
	}},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Image{X: 10, Y: 10, Width: 40, Height: 30, ResourceID: 50},
	}}},
}
```

页面也可以声明专属的 `PageRes` 资源。页面资源图片使用显式 `ID`，页面中的 `Image.ResourceID` 引用该图片：

```go
document := creator.Document{
	Pages: []creator.Page{{
		Resources: []creator.PageResource{{
			Images: []creator.PageImage{{
				ID: 70, Format: "PNG", Data: imageBytes,
			}},
		}},
		Items: []creator.Item{
			creator.Image{X: 10, Y: 10, Width: 40, Height: 30, ResourceID: 70},
		},
	}},
}
```

页面资源文件写入对应页面目录，页面资源 ID 必须与文档中其他资源 ID 不冲突。未设置 `ResourceID` 的 `Image` 仍会使用文档级 `DocumentRes` 自动嵌入图片数据。

同一个 `PageRes` 文件中的页面图片文件名必须唯一，避免 ZIP 路径冲突。

页面图片、附件、多媒体和封面文件名不能包含路径分隔符、路径段或 NUL 字符。

如果需要页面专属的字体、绘制参数、颜色空间、复合图元或音视频资源，可以直接设置 `PageResource.Data` 为完整的 OFD `Res` XML，并通过 `PageResource.Files` 提供该 XML 引用的二进制文件。原始资源中的 `MultiMedia`、`Font`、`ColorSpace`、`DrawParam` 和 `CompositeGraphicUnit` ID 会参与 creator 的引用校验；页面对象可以引用其中的图片、字体、颜色空间、绘制参数和复合图元。`PublicResource` 同样支持 `Files`，其路径相对于公共资源 XML 所在目录，XML 中的 `Profile`、`FontFile` 和 `MediaFile` 必须有对应文件。

注解的 `ReadOnly` 默认保持原有兼容行为；需要显式写出 `false` 时设置 `Annotation.ReadOnlyValue`，这样不会被 OFD schema 的默认值 `true` 覆盖。

扩展的 `Extension.Data` 适合文本内容；需要写入嵌套 XML 时使用 `Extension.DataXML`。两者不能同时设置。

同样的文件名规则也适用于自定义标签、扩展、签名值和文档版本根文件。

签名引用和版本文件清单使用包内相对路径；不允许绝对路径、反斜杠或路径穿越。

不同签名的签名值/盖章数据文件名必须唯一，不同版本的根文件名也必须唯一。

图片还可以通过 `Substitution` 指定替代图片资源，通过 `ImageMask` 指定蒙版图片资源；两者必须引用页面图片或 `Type` 为 `Image` 的文档多媒体资源：

```go
creator.Image{
	X: 10, Y: 10, Width: 40, Height: 30,
	Data:         imageBytes,
	Substitution: 70,
	ImageMask:    71,
}
```

文档附件通过 `Attachments` 配置：

```go
document := creator.Document{
	Attachments: []creator.Attachment{{
		ID:       "att-1",
		Name:     "说明文本",
		Format:   "txt",
		FileName: "readme.txt",
		Usage:    "Data",
		Data:     []byte("attachment content"),
	}},
	Pages: []creator.Page{{}},
}
```

创建器会生成 `Doc_0/Attachments/Attachments.xml` 和附件数据文件，并在 `Document.xml` 中生成 `Attachments` 引用。附件 ID、显示名称和数据不能为空，文件名不能包含路径分隔符。

同一文档中的附件文件名必须唯一；多媒体资源在 `Doc_0/Res/Media/` 目录中的文件名也必须唯一。

动作可以通过 `GotoA` 跳转到附件：

```go
newWindow := false
document := creator.Document{
	Attachments: []creator.Attachment{{
		ID: "att-1", Name: "说明文本", Format: "txt", Data: []byte("content"),
	}},
	Actions: []creator.Action{{
		GotoA: &creator.GotoAAction{AttachID: "att-1", NewWindow: &newWindow},
	}},
	Pages: []creator.Page{{}},
}
```

`GotoA.AttachID` 必须引用当前文档中已声明的附件；`NewWindow` 未设置时使用 OFD 默认行为。

自定义标签通过 `CustomTags` 配置：

```go
document := creator.Document{
	CustomTags: []creator.CustomTag{{
		NameSpace:  "urn:example:tags",
		SchemaName: "tags.xsd",
		DataName:   "tags.xml",
		Schema:     []byte(schemaData),
		Data:       []byte(`<Tags xmlns="urn:example:tags"><Value>one</Value></Tags>`),
	}},
	Pages: []creator.Page{{}},
}
```

创建器会生成 `Doc_0/CustomTags/CustomTags.xml`，并将标签数据和可选 Schema 写入对应子目录。命名空间和标签数据不能为空，文件名不能包含路径分隔符。

文档扩展通过 `Extensions` 配置：

```go
document := creator.Document{
	Extensions: []creator.Extension{{
		AppName: "creator-test",
		Company: "example",
		AppVersion: "1.0",
		RefID: 1,
		Properties: []creator.ExtensionProperty{{
			Name: "Mode", Type: "string", Value: "test",
		}},
		Data: "<Config><Enabled>true</Enabled></Config>",
	}},
	Pages: []creator.Page{{}},
}
```

扩展支持内联 `Data` 或外部 `ExtendData` 文件，两者只能设置一个；`AppName`、`RefID` 和扩展数据不能为空。

内联扩展不能设置 `DataName`；自定义标签只有在提供 `Schema` 数据时才能设置 `SchemaName`，避免生成悬空文件引用。

文档签名通过 `Signatures` 配置。签名引用的 `CheckValue` 为空时，创建器会根据 `CheckMethod` 对当前生成且已存在于 OFD ZIP 中的目标文件自动计算摘要；未设置时默认使用 MD5，与 OFD `Signature.xsd` 的默认值一致。签名 XML 自身及尚未生成的签名文件不能作为自动摘要目标：

```go
document := creator.Document{
	Signatures: []creator.Signature{{
		ID:           "sig-1",
		Type:         "Sign",
		ProviderName: "creator-test",
		Method:       "RSA",
		CheckMethod:  "SHA1",
		References: []creator.SignatureReference{{
			FileRef: "../Document.xml",
		}},
		SignedValue:     []byte("signed-value"),
		SignedValueName: "value.bin",
	}},
	Pages: []creator.Page{{}},
}
```

签名会生成 `OFD.xml` 中的 `Signatures` 引用、签名清单、签名 XML 和签名值文件。签名 ID 必须是合法 XML ID；`Sign` 类型必须提供 `SignedValue`，`Seal` 类型可提供 `SealFile`。

签名摘要算法支持 `MD5`、`SHA1`、`SM3` 和国密 OID；输入大小写会统一规范化为 Schema 接受的值。

文档版本通过 `Versions` 配置：

```go
document := creator.Document{
	Versions: []creator.DocumentVersion{{
		ID: "version-1", Index: 1, Current: true,
		Version: "1.0", Name: "初始版本",
		Files: []creator.VersionFile{{ID: "file-1", Path: "Document.xml"}},
		DocRoot:     []byte(versionDocumentXML),
		DocRootName: "version-document.xml",
	}},
	Pages: []creator.Page{{}},
}
```

版本会生成 `OFD.xml` 中的 `Versions` 清单、版本描述 XML 和可选版本文档根文件。版本 ID 和文件 ID 必须是合法 XML ID，最多只能有一个 `Current` 版本。

页面可以单独设置应用区域、内容区域和出血区域，并指定图层类型：

```go
page := creator.Page{
	Area: &creator.PageArea{
		ApplicationBox: &creator.Box{X: 5, Y: 5, Width: 200, Height: 287},
		ContentBox:     &creator.Box{X: 10, Y: 10, Width: 190, Height: 277},
		BleedBox:       &creator.Box{X: -3, Y: -3, Width: 216, Height: 303},
	},
	LayerType: creator.LayerForeground,
	Items: []creator.Item{
		creator.Text{Value: "前景内容"},
	},
}
```

可用图层类型为 `LayerBody`、`LayerBackground`、`LayerForeground` 和 `LayerCustom`。页面区域的 `PhysicalBox` 未设置时会使用文档的 `PageSize`。

也可以在 `Document.Area` 中设置文档默认页面区域。页面未单独设置区域时，阅读器可使用该文档级区域：

```go
document := creator.Document{
	PageSize: creator.PageSize{Width: 210, Height: 297},
	Area: &creator.PageArea{
		PhysicalBox:    &creator.Box{X: 0, Y: 0, Width: 210, Height: 297},
		ApplicationBox: &creator.Box{X: 5, Y: 5, Width: 200, Height: 287},
		ContentBox:     &creator.Box{X: 10, Y: 10, Width: 190, Height: 277},
	},
	Pages: []creator.Page{{}},
}
```

页面对象也支持嵌套 `PageBlock`，适合将一组对象作为一个结构块写入页面：

```go
document := creator.Document{
	Pages: []creator.Page{{Items: []creator.Item{
		creator.PageBlock{Items: []creator.Item{
			creator.Text{X: 10, Y: 10, Width: 60, Height: 8, Value: "嵌套内容"},
			creator.PageBlock{Items: []creator.Item{
				creator.Path{X: 10, Y: 20, Width: 30, Height: 20, Data: "M 0 0 L 30 20 C", Stroke: true},
			}},
		}},
	}}},
}
```

`PageBlock` 可以递归嵌套，创建器会为每个嵌套块分配唯一的 `ID`，并继续校验其中的字体、图片、复合图元、动作和裁剪引用。

需要多个图层时使用 `Page.Layers`。图层按照切片顺序写入页面，后写入的图层会覆盖先写入的图层；`Page.Items` 仍可用于只有一个图层的简单页面，但不能与 `Layers` 同时设置：

```go
page := creator.Page{
	Layers: []creator.Layer{
		{Type: creator.LayerBackground, Items: []creator.Item{
			creator.Path{X: 0, Y: 0, Width: 210, Height: 297, Data: "M 0 0 L 210 297 C"},
		}},
		{Type: creator.LayerForeground, Items: []creator.Item{
			creator.Text{X: 20, Y: 20, Width: 100, Height: 10, Value: "前景内容"},
		}},
	},
}
```

路径对象支持 `Stroke`、`Fill`、`Rule`、线宽、线端点、线连接、斜接限制、虚线模式和透明度等属性；图片对象还支持名称、可见性和图片边框：

```go
alpha := uint8(200)
document := creator.Document{
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Path{
			X: 10, Y: 10, Width: 80, Height: 60,
			Data: "M 0 0 L 80 0 L 80 60 C",
			Stroke: true, Fill: true, Rule: "Even-Odd",
			LineWidth: 0.5, Cap: "Round", Join: "Bevel", Alpha: &alpha,
		},
		creator.Image{
			X: 20, Y: 80, Width: 50, Height: 40,
			Data: imageBytes,
			Border: &creator.ImageBorder{LineWidth: 0.3},
		},
	}}},
}
```

文字和路径支持 RGB 填充色、描边色，图片边框支持边框颜色。颜色通道范围为 `0` 到 `255`，可选的 `Alpha` 也使用相同范围：

```go
alpha := uint8(220)
text := creator.Text{
	Value:     "红色文字",
	FillColor: &creator.Color{R: 255, G: 0, B: 0, Alpha: &alpha},
}
path := creator.Path{
	Data:       "M 0 0 L 20 20 C",
	Stroke:     true,
	StrokeColor: &creator.Color{R: 0, G: 0, B: 255},
	FillColor:   &creator.Color{R: 255, G: 255, B: 0},
}
```

如果需要明确关闭路径描边，可使用 `StrokeSet`；因为 OFD 的默认值是开启描边，单独将 `Stroke` 设置为 `false` 不会写出关闭属性：

```go
stroke := false
path := creator.Path{Data: "M 0 0 L 10 10 C", StrokeSet: &stroke}
```

使用 `Document.Fonts` 声明字体。字体的 `Name` 必须与 `Text.Font` 一致；设置 `Font.Data` 后会将 TTF、OTF 或 TTC 文件嵌入 `Doc_0/Res/Fonts/`：

```go
document := creator.Document{
	Fonts: []creator.Font{{
		Name: "Noto Sans CJK SC",
		Data: fontBytes,
	}},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Text{Font: "Noto Sans CJK SC", Value: "嵌入字体"},
	}}},
}
```

### OFD 文件校验

`ofd-validator` 是面向 OFD 文件包的完整性和规范性校验工具，用于检查容器安全、XML 结构、Schema
规范、文件引用、对象 ID 和签名摘要，帮助快速定位 OFD 文件中的结构问题和兼容性问题。

校验工具位于 `cmd/ofd-validator`，并使用通过 `go:embed` 内置的 `OFD-Schema` XSD。运行时不依赖
`xmllint`、外部模式文件或网络。校验结果会统一生成报告，支持终端查看、归档或交给 CI 和其他程序处理：

```bash
go run ./cmd/ofd-validator --format text test/testdata/helloworld.ofd
```

报告支持文本、Markdown、JSON 和 PDF 四种格式：

```bash
go run ./cmd/ofd-validator --format json --pretty test/testdata/helloworld.ofd > report.json
go run ./cmd/ofd-validator --format markdown -o report.md test/testdata/helloworld.ofd
go run ./cmd/ofd-validator --format pdf --font /path/to/SimSun.ttf \
  -o report.pdf test/testdata/helloworld.ofd
```

报告包含输入文件、检测时间、校验状态、错误和警告汇总、各校验阶段状态，以及每条问题的严重级别、
校验阶段、错误代码、文件位置和 XML 路径等详细信息。JSON 报告适合 CI、脚本和其他程序读取；Markdown
适合代码评审和文档归档；PDF 适合打印或发送给其他人员。

默认使用 `strict` 模式；`compat` 模式会将 XSD 错误降级为警告，`structural` 模式跳过 XSD。
使用 `--skip-xsd` 等价于 `structural` 模式。报告正文和 CLI 提示使用中文，JSON 同时保留机器可读
的英文枚举和 `*_zh` 中文字段。退出码 `0` 表示没有错误，`1` 表示存在校验错误（使用
`--fail-on-warning` 时警告也会导致退出码 1），`2` 表示工具配置或输入错误。

### OFD 文件分析

`ofd-analyzer` 是面向 OFD 文件的结构分析工具，用于快速了解文档组成、资源使用情况和对象引用关系，适合文档排查、资源审计、转换问题定位以及生成结构化分析报告。

分析工具位于 `cmd/ofd-analyzer`，默认将纯文本报告输出到标准输出，也支持 Markdown、JSON 和 PDF。报告可以直接阅读，也可以使用 JSON 供脚本或其他工具继续处理：

```bash
# 默认输出纯文本报告
go run ./cmd/ofd-analyzer test/testdata/helloworld.ofd

# 输出适合程序处理的 JSON 报告
go run ./cmd/ofd-analyzer --format json test/testdata/helloworld.ofd

# 输出缩进后的 JSON 报告文件
go run ./cmd/ofd-analyzer --format json --pretty \
  -o analyzer-report.json test/testdata/multi_demo.ofd

# 输出 Markdown 报告
go run ./cmd/ofd-analyzer --format text -o analyzer-report.txt test/testdata/helloworld.ofd
go run ./cmd/ofd-analyzer --format markdown -o analyzer-report.md test/testdata/helloworld.ofd

# 输出 PDF 报告；PDF 报告需要可用字体
go run ./cmd/ofd-analyzer --format pdf --font /path/to/font.ttf -o analyzer-report.pdf test/testdata/helloworld.ofd

# 指定 SM2 UID、签名值格式、证书链信任根和离线 CRL
go run ./cmd/ofd-analyzer --signature-uid custom-id --signature-format auto --signature-roots roots.pem test.ofd
go run ./cmd/ofd-analyzer --signature-crls revoked.crl --signature-revocation-issuers issuer.pem test.ofd
```

分析报告主要包含：

- 文档基本信息、Schema 版本、ZIP 包统计和输入文件信息
- 文档体、页面列表以及页面尺寸等页面结构信息
- 页面对象、文字对象和字符数量统计
- 图片、字体、绘制参数、复合图元、颜色空间、模板和 Pattern 等资源统计
- 附件、注解、签名值结构解析、签名摘要校验、SES 签名密码学验证以及文件引用和 ID 引用关系
- 绘制参数的定义数、引用次数、无法解析引用数和 `Relative` 继承循环
- 字符的 Unicode code point、UTF-8 字节、空白字符和 glyph 数量

分析范围和行为：

- 模板、复合图元和 Pattern 默认只统计定义及引用，不展开内部对象，避免重复计数
- 使用 `--no-templates` 可跳过模板定义、模板引用和模板 `PageRes` 资源分析
- 使用 `--no-annotations` 可跳过注解及 Appearance 分析
- 使用 `--no-signatures` 可跳过签名清单分析
- 使用 `--signature-uid` 和 `--signature-format` 配置 SM2 签名验证；使用 `--signature-roots` 显式启用证书链校验；使用 `--signature-crls` 和 `--signature-revocation-issuers` 启用不联网的离线 CRL 吊销校验
- 使用 `--no-package` 可跳过 ZIP 条目大小统计，但保留包内路径索引
- 使用 `--fail-on-warning` 可在发现分析警告时返回退出码 `1`

资源引用使用 `doc[n]/kind:id` 形式表示文档体作用域，可以区分不同文档体中重复使用的 ID。完整的命令选项、报告字段和退出码说明见 [`cmd/ofd-analyzer/README.md`](cmd/ofd-analyzer/README.md)。

OFD 文件可以包含多个文档体。转换器和查看器按 `DocBody` 出现顺序合并页面，`Page(n)` 和命令行 `-page n` 使用合并后的全局页码。

### OFD 转 PDF

```go
package main

import (
    "os"
    "github.com/zc310/ofd/pkg/converter"
)

func main() {
    output, _ := os.Create("output.pdf")
    defer output.Close()
    
    err := converter.PDF("input.ofd", output)
    if err != nil {
        panic(err)
    }
}
```


### OFD 转图像

#### 转换为 PNG

```go
err := converter.Image("input.ofd",
    converter.Writer(func(page int) (io.WriteCloser, error) {
        return os.Create(fmt.Sprintf("output_%d.png", page))
    }),
    converter.BgColor(color.White),
    converter.PNG(),
)
```

### OFD 转纯文本

纯文本转换只提取文字内容，不保留字体、颜色和页面布局。不同文字对象按行输出，不同页面使用分页符分隔。

```go
import "bytes"

var output bytes.Buffer
err := converter.Text("input.ofd", &output)
```

### OFD 转 Markdown

Markdown 转换只提取文字内容，不保留字体、颜色和页面布局；每个页面使用二级标题分隔，并转义 Markdown 特殊字符。

```go
var markdown bytes.Buffer
err := converter.Markdown("input.ofd", &markdown)
```

#### 转换为 JPG

```go
err := converter.Image("input.ofd",
    converter.Writer(func(page int) (io.WriteCloser, error) {
        return os.Create(fmt.Sprintf("output_%d.jpg", page))
    }),
    converter.BgColor(color.White),
    converter.JPG(),
    converter.Page(3),        // 指定全局第 3 页
    converter.DPI(300),       // 设置输出分辨率
)
```

### OFD 转单文件 HTML

HTML 默认将每页作为内嵌 PNG 写入单个 HTML 文件，也可以选择内嵌 JPG 或 SVG。三种模式都保留页面物理尺寸，不需要外部资源，并支持浏览器打印分页；HTML 的 `<title>` 使用 OFD 第一个文档体的非空 `DocInfo.Title`，没有标题时使用 `OFD 文档`。

```go
var htmlOutput bytes.Buffer
err := converter.HTML("input.ofd", &htmlOutput,
    converter.DPI(72),
    converter.HTMLPNG(), // 默认选项
)

var jpgHTML bytes.Buffer
err = converter.HTML("input.ofd", &jpgHTML,
    converter.HTMLJPG(),
)

var svgHTML bytes.Buffer
err = converter.HTML("input.ofd", &svgHTML,
    converter.HTMLSVG(),
)
```



## 注意事项

- 背景颜色默认为白色，可根据需要调整
- 支持效果见 `input.ofd` 转换结果
- 项目尚未完整实现 GB/T 33190-2016 的全部标准能力，具体范围见 [`docs/OFD-SUPPORT.md`](docs/OFD-SUPPORT.md)


## 特别感谢

本项目的开发离不开以下开源项目的启发和帮助：

- [国家标准化管理委员会发布的 GB/T 33190-2016 标准](http://std.samr.gov.cn/)
- https://github.com/GreenYun/OFD-Schema
- https://github.com/itlabers/ofd-go-reference
- https://github.com/itlabers/ofd-go
- https://github.com/xiaoqidun/ofdgo

感谢所有为开源社区做出贡献的开发者！
