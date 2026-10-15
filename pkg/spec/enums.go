package spec

// 路径填充规则（PathObject/@Rule、Clip/Area/Path/@Rule）。
const (
	// FillRuleNonZero 使用非零绕数规则。
	FillRuleNonZero = "NonZero"
	// FillRuleEvenOdd 使用奇偶规则。
	FillRuleEvenOdd = "Even-Odd"
)

// 路径端点样式（CT_GraphicUnit/@Cap）。
const (
	// LineCapButt 使用平头端点。
	LineCapButt = "Butt"
	// LineCapRound 使用圆头端点。
	LineCapRound = "Round"
	// LineCapSquare 使用方头端点。
	LineCapSquare = "Square"
)

// 路径连接样式（CT_GraphicUnit/@Join）。
const (
	// LineJoinMiter 使用斜接连接。
	LineJoinMiter = "Miter"
	// LineJoinRound 使用圆角连接。
	LineJoinRound = "Round"
	// LineJoinBevel 使用斜角连接。
	LineJoinBevel = "Bevel"
)

// 图层类型（Layer/@Type）。创建、文字提取和渲染都要判断图层是否为背景层，
// 取值集中在这里避免各包各写一份字面量。
const (
	// LayerBody 普通正文图层。
	LayerBody = "Body"
	// LayerBackground 背景图层。
	LayerBackground = "Background"
	// LayerForeground 前景图层。
	LayerForeground = "Foreground"
	// LayerCustom 自定义图层。
	LayerCustom = "Custom"
)

// 模板页叠放顺序（TemplatePage/@ZOrder、Template/@ZOrder）。
const (
	// ZOrderBackground 模板页绘制在页面内容之后（背景层）。
	ZOrderBackground = "Background"
	// ZOrderForeground 模板页绘制在页面内容之前（前景层）。
	ZOrderForeground = "Foreground"
)

// 签名类型（Signature/@Type）。
const (
	// SigTypeSeal 签章。
	SigTypeSeal = "Seal"
	// SigTypeSign 签名。
	SigTypeSign = "Sign"
)

// 文档视图首选项 CT_VPreferences 的取值。
const (
	// PageModeNone 使用默认页面显示模式。
	PageModeNone = "None"
	// PageModeFullScreen 以全屏模式显示文档。
	PageModeFullScreen = "FullScreen"
	// PageModeUseOutlines 显示文档大纲面板。
	PageModeUseOutlines = "UseOutlines"
	// PageModeUseThumbs 显示页面缩略图面板。
	PageModeUseThumbs = "UseThumbs"
	// PageModeUseCustomTags 显示自定义标签面板。
	PageModeUseCustomTags = "UseCustomTags"
	// PageModeUseLayers 显示图层面板。
	PageModeUseLayers = "UseLayers"
	// PageModeUseAttatchs 显示附件面板。拼写沿用 OFD 规范原文（Attatchs）。
	PageModeUseAttatchs = "UseAttatchs"
	// PageModeUseBookmarks 显示书签面板。
	PageModeUseBookmarks = "UseBookmarks"

	// PageLayoutOnePage 单页显示。
	PageLayoutOnePage = "OnePage"
	// PageLayoutOneColumn 单列连续显示。
	PageLayoutOneColumn = "OneColumn"
	// PageLayoutTwoPageL 双页显示，页面从左向右排列。
	PageLayoutTwoPageL = "TwoPageL"
	// PageLayoutTwoColumnL 双列连续显示，页面从左向右排列。
	PageLayoutTwoColumnL = "TwoColumnL"
	// PageLayoutTwoPageR 双页显示，页面从右向左排列。
	PageLayoutTwoPageR = "TwoPageR"
	// PageLayoutTwoColumnR 双列连续显示，页面从右向左排列。
	PageLayoutTwoColumnR = "TwoColumnR"

	// TabDisplayDocTitle 标签页显示文档标题。
	TabDisplayDocTitle = "DocTitle"
	// TabDisplayFileName 标签页显示文件名。
	TabDisplayFileName = "FileName"

	// ZoomModeDefault 使用默认缩放。
	ZoomModeDefault = "Default"
	// ZoomModeFitHeight 适应高度。
	ZoomModeFitHeight = "FitHeight"
	// ZoomModeFitWidth 适应宽度。
	ZoomModeFitWidth = "FitWidth"
	// ZoomModeFitRect 适应页面。
	ZoomModeFitRect = "FitRect"
)
