# OFD 创建器库指南

`github.com/zc310/ofd/pkg/creator` 根据 Go 文档模型生成 OFD 文件包。本指南集中说明
文字定位、资源和页面组织、附加结构及流式创建；命令行 manifest、导出、合并、替换
和水印用法见 [ofd-creator](../../cmd/ofd-creator/README.md)。

| 主题 | 入口 |
|------|------|
| 最小创建示例 | [项目快速开始](../../README.md)、[Go 文档示例](example_test.go) |
| 写出与配置 | [创建入口](#创建入口)、[生成选项](#生成选项) |
| 坐标、页面和图层 | [坐标与页面组织](#坐标与页面组织) |
| 文字与字形 | [文字定位与字体](#文字定位与字体) |
| 路径、颜色与透明度 | [绘制样式与颜色](#绘制样式与颜色) |
| 裁剪、模板和复合图元 | [裁剪](#裁剪)、[模板与复合图元](#模板与复合图元) |
| 资源复用 | [图片与资源](#图片与资源) |
| 元数据及附加结构 | [元数据与阅读偏好](#元数据与阅读偏好)、[注解与动作](#注解与动作)、[附件与扩展](#附件与扩展)、[签名与版本](#签名与版本) |
| 自动断行与分页 | [`pkg/creator/flow` 流式创作](flow/README.md)：图文与合并表格自动分页，可选普通单元格跨页续排与多栏节末页平衡，支持目录、大纲、内部引用、分节编号和横纵页面混排。 |
| 印刷主题 | [`flow` 主题选择](flow/README.md#主题选择)：默认 Material 风格、Material 2、Material 3、Fluent 2、IBM Carbon 和 Bootstrap 5 的字号层级、行距、图注和表格预设，可显式替换字体。 |
| 常规公文基础版式 | [`flow` 公文配置](flow/README.md#gbt-9704-2012-公文基础版式)：GB/T 9704—2012 版心、五类字体、正文行列、版头、长主送机关自动移至版记、不盖章落款、版记和奇偶页码。 |
| 创作字体控制 | [`flow` 字体说明](flow/README.md#中文与字体)：TTC/OTC 字体面选择、同族字重/斜体匹配与等宽缺字回退。 |
| 报告表格与图表编号 | [`flow` 表格说明](flow/README.md#自动列宽与单元格外观)：自动列宽、单元格外观、题注与续表题、可点击的编号引用。 |
| 固定图元自动分页 | [`flow` 固定区域](flow/README.md#固定图元区域)：将矢量图表、二维码图片、签字栏等原始图元作为整体插入，自动预留空间。 |
| 复用排版样式 | [`flow` 样式继承](flow/README.md#命名样式与显式零值)：命名文字样式、多层继承与显式零间距/缩进覆盖。 |
| 两端对齐 | [`flow` 文字对齐](flow/README.md#两端对齐)：按中英文断行边界伸展自动行，保留段末、手动换行及词内字体和链接。 |
| 页底脚注 | [`flow` 脚注说明](flow/README.md#页底脚注)：引用行与脚注共同分页、同页去重、表头重复脚注及双向跳转。 |
| 双栏与多栏 | [`flow` 分栏说明](flow/README.md#等宽分栏与换栏)：等宽栏自动流动、显式换栏、分节栏数切换与栏底脚注。 |
| 图表目录与标题引用 | [`flow` 图表目录](flow/README.md#图目录表目录与标题引用)：独立图目录、表目录、完整题注和章节标题自动引用。 |
| 大文档 | [流式创建超大 OFD](#流式创建超大-ofd) |
| 引用、路径和输出校验 | [校验与输出约定](#校验与输出约定) |

除完整程序和 Go 文档示例外，下文代码均为模型片段。将片段组合进包含非空 `ID`
和页面的 `creator.Document` 后调用创建入口；`fontBytes`、`imageBytes`、`pngData`、
`iccProfileData` 等变量需要由调用方提供真实资源。使用 `spec.` 常量时导入
`github.com/zc310/ofd/pkg/spec`。

## 创建入口

| 输出方式 | 默认入口 | 指定 CreateOptions |
|----------|----------|--------------------|
| 文件 | `CreateFile(document, filename)` | 打开文件，调用 `CreateWithOptions` 并处理关闭错误。 |
| io.Writer | `Create(document, writer)` | `CreateWithOptions(document, writer, options)` |
| 字节切片 | `Marshal(document)` | `MarshalWithOptions(document, options)` |
| PageProvider → 文件 | — | `CreateFileWithPages(meta, pages, filename, options)` |
| PageProvider → io.Writer | `CreateWithPages(meta, pages, writer)` | `CreateWithPagesOptions(meta, pages, writer, options)` |
| PageProvider → 字节切片 | — | `MarshalWithPages(meta, pages, options)` |

创建器会分配自动资源 ID、生成 XML 和资源清单，并检查字段、资源引用及继承循环。
文件入口返回的错误也包括文件创建和关闭错误。命令行的临时文件与原子替换策略
见 [CLI 说明](../../cmd/ofd-creator/README.md)，不要把 CLI 行为等同于库的文件入口。

## 生成选项

| CreateOptions 字段 | 用途 |
|--------------------|------|
| Compression | Auto、Deflate 或 Store 压缩策略。 |
| CompressionLevel | DEFLATE 级别 1–9，0 使用默认级别 5；只影响实际使用 Deflate 的条目。 |
| Deterministic | 固定 ZIP 元数据，便于可重复生成。输入内容和日期也需要保持稳定。 |
| PreserveEmbeddedFonts | 保留完整嵌入字体；流式页面含嵌入字体时需要设置，或由调用方预先子集化。 |
| CompleteTextCodeDeltas | 按字体度量补齐缺失的 DeltaX/DeltaY，默认关闭。 |
| DocType | 根节点的文档类型，缺省为基础 OFD；声明类型不代表自动满足该类型的全部要求。 |

## 坐标与页面组织

页面坐标以左上角为原点，边界和字号均使用毫米。`PageSize` 未设置时默认 A4。
`Document.Area` 提供文档默认区域，`Page.Area` 可以设置本页区域；PhysicalBox
缺省时由页面尺寸补齐。

```go
page := creator.Page{
	Area: &creator.PageArea{
		PhysicalBox:    &creator.Box{Width: 210, Height: 297},
		ApplicationBox: &creator.Box{X: 5, Y: 5, Width: 200, Height: 287},
		ContentBox:     &creator.Box{X: 10, Y: 10, Width: 190, Height: 277},
		BleedBox:       &creator.Box{X: -3, Y: -3, Width: 216, Height: 303},
	},
	Items: []creator.Item{
		creator.Text{X: 20, Y: 20, Width: 100, Height: 10, Value: "正文"},
	},
}
```

简单页面使用 `Page.Items`，多图层页面使用 `Page.Layers`，两者互斥。图层按切片
顺序写出；类型可取 Body、Background、Foreground 或 Custom。`Page.LayerType`
用于单图层页面。页面和模板的图元还可以通过 `PageBlock` 递归组织：

```go
page := creator.Page{
	Layers: []creator.Layer{
		{Type: spec.LayerBackground, Items: []creator.Item{
			creator.Path{X: 0, Y: 0, Width: 210, Height: 297,
				Data: "M 0 0 L 210 0 L 210 297 L 0 297 C", Fill: true},
		}},
		{Type: spec.LayerForeground, Items: []creator.Item{
			creator.PageBlock{Items: []creator.Item{
				creator.Text{X: 20, Y: 20, Width: 100, Height: 10, Value: "前景内容"},
			}},
		}},
	},
}
```

创建器为嵌套块分配 ID，并检查其中的字体、图片、动作、裁剪和复合图元引用。

## 文字定位与字体

`Text.Value` 生成一个文字段。`TextCodes` 可指定多个段，每段提供起点及逐字符
DeltaX/DeltaY；内容可以是纯空白，但不能为空字符串。X/Y 是相对文字对象边界的
毫米坐标，未指定时由创建器补齐默认起点和基线。

```go
x, y := 12.5, 8.0
text := creator.Text{
	X: 10, Y: 10, Width: 100, Height: 12, Size: 4.233,
	CTM: &creator.CTM{1, 0, 0, 1, 3, 4},
	TextCodes: []creator.TextCode{
		{Value: "第一段", X: &x, Y: &y, DeltaX: []float64{4.2, 4.2}},
		{Value: "第二段", DeltaY: []float64{4.2, 4.2}},
	},
}
```

默认不自动计算 DeltaX/DeltaY。需要根据字体度量补全时启用
`CreateOptions{CompleteTextCodeDeltas: true}`，CLI 对应 `--complete-text-code-deltas`。

`CGTransforms` 用于显式字形编号映射。CodePosition 相对于整个文字对象，从 0
开始；CodeCount 是源字符数，GlyphCount 是字形数，Glyphs 保存字体实际字形编号：

```go
text := creator.Text{
	Value: "字形映射",
	CGTransforms: []creator.CGTransform{{
		CodePosition: 1, CodeCount: 1, GlyphCount: 2,
		Glyphs: []int{120, 121},
	}},
}
```

使用 `Document.Fonts` 声明字体，`Font.Name` 是文档内唯一的资源引用名，与 `Text.Font` 一致。
`Font.FontName` 是输出到 OFD 的实际字型名，省略时使用 `Name`；`FamilyName` 是字体族名。
这些名称依据 GB/T 33190—2016 §11.1 表 44 写出，多个独立资源可以使用相同的实际字型名。
设置 Data 或 Source 后嵌入字体；仅使用逻辑字体时，显示依赖阅读器的字体替代。

```go
document := creator.Document{
	ID: "embedded-font",
	Fonts: []creator.Font{{Name: "BodyCJK", FontName: "Noto Sans CJK SC", FamilyName: "Noto Sans CJK SC", Data: fontBytes}},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Text{X: 20, Y: 20, Width: 100, Height: 10,
			Font: "BodyCJK", Value: "嵌入字体"},
	}}},
}
```

流式创作会在读取嵌入字体时自动补全未指定的 `FontName` 和 `FamilyName`，
内部 `Flow-Regular` 等资源别名保留。普通创建 API 使用上述显式字段；
清单对应 `resources.fonts[].name`、`font_name` 和 `family_name`。

流式创作还会在合并元数据后清理未引用的字体声明，避免把未使用的整份字体嵌入结果。
普通模型可在页面已物化后调用 `document.PruneUnusedFonts()`；它同时检查模板、注解、
复合图元、裁剪和底纹的文字引用，不读取字体来源。使用外部 `PageProvider` 或原始资源 XML 时保留声明。

字体数据支持 TTF、OTF、TTC；字体子集化或字体面提取可能改变实际写出的数据。
子集化会与 Unicode 字符映射核对，避免旧字体把首个 Macintosh 编码误当作 Unicode，
导致仿宋_GB2312 等字体的数字变成其他字形；显式字形引用仍保留原始 GID 的重映射关系。
TrueType 和 CFF（含多 Font DICT）按实际使用字形生成子集，并同步重映射显式 GID。
TTC 只嵌入所选字体面。非法显式字形编号及子集化错误直接返回，
具体限制见 [PDF 支持清单](../../docs/PDF-SUPPORT.md)。

## 绘制样式与颜色

路径支持 Stroke、Fill、Rule、LineWidth、Cap、Join、MiterLimit、DashPattern、
DashOffset 和 Alpha。`Stroke: false` 会显式关闭描边；StrokeSet 可覆盖 Stroke。
图片可设置名称、可见性及 `ImageBorder`。

路径语法：M 移动、L 直线、Q 二次曲线、B 三次曲线、A 椭圆弧、C 闭合。
Fill 的 Rule 使用 NonZero 或 Even-Odd；C 不是三次贝塞尔曲线。

```go
alpha := uint8(200)
path := creator.Path{
	X: 10, Y: 10, Width: 80, Height: 60,
	Data: "M 0 0 L 80 0 L 80 60 L 0 60 C",
	Stroke: true, Fill: true, Rule: "Even-Odd",
	LineWidth: 0.5, Cap: "Round", Join: "Bevel", Alpha: &alpha,
	StrokeColor: &creator.Color{R: 0, G: 0, B: 255},
	FillColor:   &creator.Color{R: 255, G: 255, B: 0},
}
```

`Document.DrawParams` 抽取公共样式，图层、图元和裁剪通过名称引用，Relative
提供样式继承。名称必须存在，继承不能循环。

```go
document := creator.Document{
	ID: "shared-style",
	DrawParams: []creator.DrawParam{
		{Name: "base-line", LineWidth: 0.5, Cap: "Round", Join: "Miter"},
		{Name: "blue-line", Relative: "base-line",
			StrokeColor: &creator.Color{R: 20, G: 40, B: 200}},
	},
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Path{X: 1, Y: 2, Width: 80, Height: 60,
			Data: "M 0 0 L 80 60", Stroke: true, DrawParam: "blue-line"},
	}}},
}
```

### 颜色空间、渐变和图案

| 配置 | 规则 |
|------|------|
| Color.R/G/B/Alpha | 通道范围 0–255，Alpha 为不透明度。 |
| Color.Components | 未指定 ColorSpace 时按默认 RGB，须提供三个分量；指定后按该空间的通道顺序填写。 |
| Document.ColorSpaces | 显式 ID；GRAY/RGB/CMYK 分别为 1/3/4 分量。分量和调色板值不得超出 BitsPerComponent 范围，缺省位深为 8。 |
| ColorSpace.ProfileData | 嵌入 Profile 到 `Doc_0/Res/Profiles/`；相同路径不能对应不同数据。 |
| Document.DefaultCS | 必须引用已声明的颜色空间 ID。 |
| Color.Axial / Color.Radial | 互斥；至少两个 Segments，位置在 0–1 之间，起止坐标必须有限。 |
| Color.Gouraud | 至少三个控制点。 |
| Color.LaGouraud | 设置 VerticesPerRow，控制点数必须是每行顶点数的整数倍。 |
| Color.Pattern | 正尺寸的图案单元，使用 Items 或 Layers；不能同时设置渐变。Thumbnail 可引用图片资源。 |

```go
pattern := &creator.Pattern{
	Width: 10, Height: 10, XStep: 10, YStep: 10,
	ReflectMethod: "RowAndColumn", RelativeTo: "Object",
	Items: []creator.Item{
		creator.Path{X: 0, Y: 0, Width: 10, Height: 10,
			Data: "M 0 0 L 10 0 L 10 10 C", Fill: true},
	},
}
path := creator.Path{
	X: 10, Y: 10, Width: 80, Height: 40,
	Data: "M 0 0 L 80 0 L 80 40 L 0 40 C", Fill: true,
	FillColor: &creator.Color{Pattern: pattern},
}
```

图案输出为 OFD CellContent，复用文字、路径、图片及复合图元模型。

## 裁剪

文字、路径、图片及裁剪区域可使用六参数 CTM。Clips 至少有一个 Clip，每个 Clip
至少有一个 Area；Area 必须且只能提供 Path 或 Text，也可引用 DrawParam。

```go
image := creator.Image{
	X: 10, Y: 10, Width: 80, Height: 60, Data: pngData,
	CTM: &creator.CTM{1, 0, 0, 1, 5, 5},
	Clips: &creator.Clips{Items: []creator.Clip{{Areas: []creator.ClipArea{{
		Path: &creator.ClipPath{
			Boundary: creator.Box{Width: 50, Height: 40},
			Data: "M 0 0 L 50 0 L 50 40 L 0 40 C", Fill: true,
		},
	}}}}},
}
```

裁剪坐标、CTM、路径数据、文字及绘制参数引用都会校验。

## 模板与复合图元

模板页、复合图元、颜色空间等资源使用显式 ID；ID 不得与其他资源冲突。
模板内容支持普通页面的图层与图元，页面通过 TemplateRef 引用，ZOrder 只能取
Background 或 Foreground，缺省使用 OFD 默认值。

```go
document := creator.Document{
	ID: "template-example",
	Templates: []creator.TemplatePage{{
		ID: 10, Name: "页眉模板", ZOrder: "Background",
		Items: []creator.Item{
			creator.Path{X: 0, Y: 0, Width: 210, Height: 5,
				Data: "M 0 0 L 210 0 L 210 5 L 0 5 C", Fill: true},
		},
	}},
	Pages: []creator.Page{{
		Templates: []creator.TemplateRef{{ID: 10, ZOrder: "Background"}},
		Items: []creator.Item{
			creator.Text{X: 10, Y: 20, Width: 80, Height: 10, Value: "正文"},
		},
	}},
}
```

复合图元用 `Document.Composites` 定义尺寸和内容，图元 `Composite.ResourceID`
引用它。复合图元可设置 Thumbnail 和 Substitution 图片资源。

```go
composite := creator.CompositeGraphicUnit{
	ID: 20, Width: 40, Height: 30,
	Items: []creator.Item{
		creator.Path{X: 0, Y: 0, Width: 40, Height: 30,
			Data: "M 0 0 L 40 0 L 40 30 C", Fill: true},
	},
}
item := creator.Composite{X: 10, Y: 20, Width: 40, Height: 30, ResourceID: 20}
```

## 图片与资源

| 资源入口 | 用途与规则 |
|----------|------------|
| Image.Data / Source | 直接提供图片；未指定 ResourceID 时自动注册文档资源。 |
| Document.Media | 显式注册 Image/Audio/Video，供图片、复合图元或声音/影片动作引用。 |
| Page.Resources | 页面专属资源；Images 使用显式 ID，页面 Image.ResourceID 引用该 ID。 |
| PublicRes | 公共资源 XML，根元素须为 OFD Res；名称相对于文档根目录。 |
| PageResource.Data / PublicResource.Data | 原始 Res XML，保留字体、绘制参数、颜色空间、复合图元和多媒体等资源。 |
| PageResource.Files / PublicResource.Files | 提供原始 XML 引用的二进制文件，路径相对于资源 XML 所在目录；FontFile、MediaFile、Profile 必须可解析。 |
| Image.Substitution / Image.ImageMask | 引用图片替代或蒙版资源，目标必须是页面图片或 Type 为 Image 的文档多媒体资源。 |

```go
document := creator.Document{
	ID: "page-image",
	Pages: []creator.Page{{
		Resources: []creator.PageResource{{Images: []creator.PageImage{
			{ID: 70, Format: "PNG", Data: imageBytes},
		}}},
		Items: []creator.Item{
			creator.Image{X: 10, Y: 10, Width: 40, Height: 30, ResourceID: 70},
		},
	}},
}
```

页面资源文件写入对应页面目录，公共资源路径例如 `Public/SharedRes.xml`。原始资源
中的 ID 参与引用校验，不能覆盖 Document.xml、页面或其他系统文件。媒体和附件格式
去除前导点并规范为小写，格式值不能含路径分隔符或 NUL。

## 元数据与阅读偏好

文档元数据可设置标题、作者、摘要、用途、关键词、CustomDatas 和 CoverData。
封面写入 `Doc_0/Cover/` 并由 DocInfo 引用。

```go
document := creator.Document{
	ID: "metadata-example", Title: "标题", Author: "作者",
	Abstract: "摘要", DocUsage: "Report", Keywords: []string{"ofd", "metadata"},
	CustomDatas: []creator.CustomData{{Name: "Department", Value: "Engineering"}},
	CoverData: coverImageBytes,
	Pages: []creator.Page{{}},
}
```

Permissions 声明编辑、导出、打印、份数和有效期；Preferences 声明页面模式、布局、
界面显示和缩放。ZoomMode 与具体 Zoom 比例互斥。声明这些字段不代表所有阅读器
都会执行，支持范围见 [OFD 支持清单](../../docs/OFD-SUPPORT.md)。

## 注解与动作

同一页面的注解合并到一个 AnnotationPage，Page 是从 0 开始的索引。注解必须指定
唯一 ID、合法 Type、Creator 和外观内容。ReadOnlyValue 可显式输出 false，避免
被 Schema 的默认 true 覆盖。

```go
annotations := []creator.AnnotationPage{{
	Page: 0,
	Items: []creator.Annotation{{
		ID: 50, Type: "Highlight", Creator: "ofd-creator", Remark: "重点内容",
		Boundary: &creator.Box{X: 10, Y: 20, Width: 40, Height: 8},
		Items: []creator.Item{
			creator.Path{X: 10, Y: 20, Width: 40, Height: 8,
				Data: "M 0 0 L 40 0 L 40 8 L 0 8 C", Fill: true},
		},
	}},
}}
```

文档、页面和文字/路径/图片等图元可以携带 Actions。一个动作只设置 URI、Goto、
GotoA、Sound 或 Movie 之一；未指定 Event 时使用默认点击事件。

| 动作或目标 | 规则 |
|------------|------|
| URI | URI 非空，可指定 Target。 |
| Goto.Page | 从 0 开始，必须在文档页数范围内。 |
| Goto.Bookmark | 引用已声明的书签，与页面目标互斥；Document.Bookmarks 自己的 Goto 必须使用页面 Dest。 |
| FitH / FitV | 分别使用 Top / Left。 |
| FitR | 完整且顺序正确的 Left/Top/Right/Bottom 矩形。 |
| Fit | 不接受位置或缩放参数。 |
| GotoA.AttachID | 引用当前文档的附件；NewWindow 缺省沿用 OFD 默认行为。 |
| Sound / Movie | ResourceID 分别引用 Audio / Video 资源。 |
| Action.Region | 至少一个区域和路径命令，坐标有限；使用 RegionMove/Line/曲线/Arc/Close 等命令。 |

```go
top := 40.0
document := creator.Document{
	ID: "page-link",
	Pages: []creator.Page{{Items: []creator.Item{
		creator.Text{X: 10, Y: 10, Width: 80, Height: 10, Value: "打开第二页",
			Actions: []creator.Action{{Goto: &creator.GotoAction{
				Page: 1, Type: "FitH", Top: &top,
			}}},
		},
	}}, {}},
}
```

Outlines 支持 Children 层级和动作，Bookmarks 提供命名目标。它们的页面目标同样
使用从 0 开始的索引；页数要包含所有被引用的页面。

## 附件与扩展

| 配置 | 规则与输出 |
|------|------------|
| Attachments | ID、Name 和数据非空；写入附件清单及数据，Document.xml 引用清单。同一文档附件文件名唯一。 |
| CustomTags | NameSpace 和标签数据非空；Schema 可选，只有提供 Schema 数据时才设置 SchemaName。写入标签清单及子文件。 |
| Extensions | AppName、RefID 和数据按模型校验；Data 与 DataXML 互斥；内联数据与外部 ExtendData 互斥。 |
| Extension.Data | 文本内容，包括需要按文本保存的 XML 字符串。 |
| Extension.DataXML | 嵌套 XML；保留原有前缀与命名空间。 |
| DataName | 用于外部扩展文件，内联扩展不能设置。 |

```go
attachments := []creator.Attachment{{
	ID: "att-1", Name: "说明文本", Format: "txt", FileName: "readme.txt",
	Usage: "Data", Data: []byte("attachment content"),
}}
```

附件、多媒体、封面、页面图片、自定义标签、扩展、签名值和版本根文件的文件名
必须满足各自的单层文件名规则，不能含分隔符、路径段或 NUL。

## 签名与版本

Signatures 生成根入口的签名引用、签名清单、Signature.xml 和签名值文件。
调用方负责提供合法签名数据；创建签名结构和计算引用摘要不等同于完成密码学签署。
外部命令签名见 [pkg/sign](../sign/) 和 [CLI 签名说明](../../cmd/ofd-creator/README.md)。

```go
signatures := []creator.Signature{{
	ID: "sig-1", Type: "Sign", ProviderName: "creator-example",
	Method: "RSA", CheckMethod: "SHA1",
	References: []creator.SignatureReference{{FileRef: "../Document.xml"}},
	SignedValue: signedValueBytes, SignedValueName: "value.bin",
}}
```

| 结构 | 规则 |
|------|------|
| Signature.ID | 合法 XML ID，不自动裁剪首尾空白。 |
| Sign / Seal | Sign 必须提供 SignedValue；Seal 可提供 SealFile。 |
| CheckValue | 为空时根据 CheckMethod 计算已存在目标条目的摘要；签名 XML 自身和尚未生成的签名条目不能作为自动摘要目标。 |
| CheckMethod | MD5、SHA1、SM3 及对应国密 OID；缺省按 Schema 使用 MD5，大小写会规范化。 |
| SignatureReference.FileRef | 从签名目录解析，允许规范的 `../Document.xml`；目标必须存在。 |
| Versions | 生成版本清单、版本描述和可选文档根文件；ID 与文件 ID 为合法 XML ID，Index 唯一，最多一个 Current 版本。 |
| Version.Files | 列出该版本所需的全部文件，包括入口、页面和资源。输入路径相对于文档目录 `Doc_0`；输出到版本描述文件时自动转换为 `/Doc_0/...` 包内绝对路径。 |
| Version.Version | 该版本适用的 OFD 格式版本，如 `1.0`，不是文档修订版本号。 |
| Version.DocRoot | 根元素为 Document，使用 OFD 命名空间。 |

```go
versions := []creator.DocumentVersion{{
	ID: "version-1", Index: 1, Current: true, Version: "1.0", Name: "初始版本",
	Files: []creator.VersionFile{
		{ID: "file-1", Path: "Versions/Files/version-document.xml"},
		{ID: "file-2", Path: "Pages/Page_0/Content.xml"},
	},
	DocRoot: versionDocumentXML, DocRootName: "version-document.xml",
}}
```

上述示例假设版本只有一个空页面；若页面使用字体、图片等资源，还需在版本入口引用资源文件并将依赖加入 `Files`。
生成的版本 `BaseLoc`、文件清单和 `DocRoot` 统一使用包内绝对路径。
直接提供的版本入口 XML 保留原内容；建议也使用 `/Doc_0/Pages/Page_0/Content.xml` 等包内绝对路径。
若使用相对路径，则从 `Doc_0/Versions/Files` 解析，例如 `../../Pages/Page_0/Content.xml`。
这些路径规则与版本文件清单要求依据 GB/T 33190—2016 §7.3、§19.2。
不同签名值/印章文件和不同版本根文件不能使用冲突的文件名。

## 流式创建超大 OFD

页面数量大或资源大时，使用 PageProvider 按索引提供页面，用 DataSource 按需读取
资源。元数据和资源放在 meta，meta.Pages 必须为空；PageAt 在 ID 预留、校验和
写出阶段会被重复调用，必须为同一索引返回相同页面。

下面片段使用外部字体文件，调用前需要准备该路径，并导入 fmt、log：

```go
meta := creator.Document{
	ID: "big-report", PageSize: creator.A4,
	Fonts: []creator.Font{{
		Name: "SourceHanSans", Format: "otf",
		Source: creator.FileDataSource("/path/to/SourceHanSans.otf"),
	}},
}
options := creator.CreateOptions{
	Compression: creator.CompressionAuto, PreserveEmbeddedFonts: true,
}
if err := creator.CreateFileWithPages(meta, reportPages{total: 10_000}, "report.ofd", options); err != nil {
	log.Fatal(err)
}
```

```go
type reportPages struct{ total int }

func (p reportPages) PageCount() int { return p.total }

func (p reportPages) PageAt(index int) (creator.Page, error) {
	return creator.Page{Items: []creator.Item{
		creator.Text{X: 20, Y: 20, Width: 170, Height: 12,
			Value: fmt.Sprintf("第 %d / %d 页", index+1, p.total),
			Font: "SourceHanSans", Size: 4.233},
	}}, nil
}
```

流式页面不能回写字体子集后的字形编号；含嵌入字体时设置 PreserveEmbeddedFonts，
或预先完成子集化。可使用 Source 的资源包括字体、图片、多媒体、附件、封面、
公共/页面资源文件、扩展数据及印章；同名 Source 优先于 Data。

PageProvider 减少页面模型的常驻内存，不能保证所有资源、ID 和引用统计都使用
常量内存；MarshalWithPages 仍把最终 OFD 保存在字节缓冲区。CLI 对应 `--stream`。

页面和模板 XML 在生成过程中分块写入 ZIP，输出缓冲区不随单页 XML 大小增长；
单次超长文本、路径和数值列表也可分块输出。页面模型、构建后的图元及辅助 XML
仍占用内存；被签名引用的条目会保留完整原文用于计算摘要。需要降低最终产物的
内存占用时，使用文件或 io.Writer 创建入口。

## 校验与输出约定

| 项目 | 约定 |
|------|------|
| 文档和资源引用 | 文档 ID 非空；资源引用必须存在且类型正确；继承和递归遵守实现限制。 |
| 数值 ID | 不要求按输入顺序递增；创建器预留显式 ID，自动分配跳过这些值。ID/引用符合 xs:unsignedInt，即不超过 uint32 范围。 |
| XML 标识符 | 附件、签名、版本等 XML ID 必须合法，首尾空白不自动裁剪。 |
| 数值和日期 | 坐标、CTM、渐变等必须有限；Schema 日期的年份范围为 1–9999。 |
| 包内路径 | 规范的相对路径，不允许绝对路径、反斜杠、NUL、路径穿越或首尾空白；签名引用按自身目录解析。 |
| 文件冲突 | 页面图片、附件、多媒体、签名和版本文件等不得产生重复或覆盖系统条目。 |
| XML 输出 | 创建器生成的元素使用 ofd: 前缀，根节点声明 `xmlns:ofd="http://www.ofdspec.org/2016"`；普通属性无命名空间。原始资源 XML 和 DataXML 保留自身前缀。 |
| 稳定输出 | Profile 条目按颜色空间 ID 排序；注解日期缺省取文档修改/创建日期，再回退到 1970-01-01。 |
| 输入模型 | Marshal/Create 不把生成的 Profile 路径、签名摘要或图案缓存写回调用方模型，同一输入可重复生成。 |

独立校验生成结果时，在仓库根目录运行：

```sh
go run ./cmd/ofd-validator --format text --mode strict /path/to/output.ofd
```

格式校验通过不等同于显示正确。含字体、裁剪或渐变的文档还需检查页面渲染和
PDF/文字输出。字段定义及其注释见 [model.go](model.go)，完整 API 可通过
`go doc github.com/zc310/ofd/pkg/creator` 查询。
