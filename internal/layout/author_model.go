package layout

import "github.com/zc310/ofd/pkg/creator"

// TextStyle 是创作主题中的文字样式。字号为磅，段间距与缩进为毫米。
type TextStyle struct {
	BasedOn         string
	Set             uint32
	Font            string
	Size            float64
	LineHeight      float64
	SpaceBefore     float64
	SpaceAfter      float64
	FirstLineIndent float64
	Align           string
	Color           *creator.Color
}

const (
	StyleSize = uint32(1 << iota)
	StyleLineHeight
	StyleSpaceBefore
	StyleSpaceAfter
	StyleFirstLineIndent
)

// FlowNamedStyle 注册可复用的文字样式。
type FlowNamedStyle struct {
	Name  string
	Style TextStyle
}

// FlowFootnote 是与引用行同页的页底注释。
type FlowFootnote struct {
	ID, Text string
	Runs     []Inline
	Style    *TextStyle
}

// Theme 定义正文、各级标题、图注与表格的印刷样式。
type Theme struct {
	Body        TextStyle
	Headings    [6]TextStyle
	Caption     TextStyle
	Table       TextStyle
	BorderColor *creator.Color
	HeaderFill  *creator.Color
}

// FlowBlock 是顺序创作的内容块。Kind 为 paragraph、heading、recipient、list_item、image、table、fixed、toc、section、page_break 或 column_break。
type FlowBlock struct {
	ID              string
	Kind            string
	Text            string
	Runs            []Inline
	Level           int
	Marker          string
	Style           *TextStyle
	KeepWithNext    bool
	KeepTogether    bool
	PageBreakBefore bool
	PageBreakAfter  bool
	Orphans         int
	Widows          int
	Image           *FlowImage
	Table           *FlowTable
	Fixed           *FlowFixed
	TOC             *FlowTOC
	Section         *FlowSection
	// tocTarget 仅用于目录展开后的内部条目。
	tocTarget    string
	captionLabel string
	sourceIndex  int
}

// FlowTOC 在当前位置生成标题或图表目录，标题目录的 MaxLevel 缺省为 3。
type FlowTOC struct {
	Title    string
	MaxLevel int
	Source   string
}

// FlowMargins 是毫米单位的页边距，指针用于区分继承和显式的零边距。
type FlowMargins struct{ Top, Right, Bottom, Left float64 }

// FlowColumns 定义等宽分栏，Count 缺省为 1，Gap 为毫米单位的栏间距。
type FlowColumns struct {
	Count int
	Gap   float64
	// Balance 启用节末页栏高平衡，含显式换栏的末页保持原布局。
	Balance bool
}

// FlowSection 覆盖后续页面设置，分页后只以标准物理页、文字和路径输出。
type FlowSection struct {
	PageSize            *creator.PageSize
	Margins             *FlowMargins
	Columns             *FlowColumns
	Header              *string
	Footer              *string
	PageNumbers         *bool
	StartPageNumber     *int
	PageNumberStyle     string
	PageNumberPrefix    *string
	FirstPageHeader     *string
	FirstPageFooter     *string
	HideFirstPageNumber bool
}

// FlowImage 是图片及图注，尺寸为毫米；未给尺寸时按 96 DPI 推算并限制在版心内。
type FlowImage struct {
	Data          []byte
	Width         float64
	Height        float64
	Caption       string
	Numbered      bool
	CaptionPrefix string
}

// FlowFixed 是作为整体参与分页的固定区域，内部图元使用左上角为原点的毫米坐标。
type FlowFixed struct {
	Width, Height float64
	Align         string
	Items         []creator.Item
	Caption       string
	Numbered      bool
	CaptionPrefix string
}

// FlowTable 是简单网格表格，单元格支持自动换行，Header 在续页重复。
type FlowTable struct {
	// SplitRows 按完整文字行续排普通及 ColSpan 单元格，正文 RowSpan 不支持。
	SplitRows    bool
	Header       []string
	Rows         [][]string
	ColumnWidths []float64
	// Grid 与 Header/Rows 互斥，合并单元格占据的位置无需额外占位。
	Grid               [][]FlowTableCell
	HeaderRows         int
	Columns            []FlowTableColumn
	CellStyle          *FlowCellStyle
	StripeFill         *creator.Color
	Caption            string
	Numbered           bool
	CaptionPrefix      string
	RepeatCaption      bool
	ContinuationSuffix string
}

// FlowTableColumn 配置固定宽或按内容分配的列宽，长度为毫米，Weight 为剩余宽度比例。
type FlowTableColumn struct {
	Width, MinWidth, MaxWidth, Weight float64
	NoWrap                            bool
	Align                             string
}

// FlowCellStyle 定义单元格的外观，nil 字段继承表格默认设置。
type FlowCellStyle struct {
	Fill          *creator.Color
	Padding       *FlowMargins
	VerticalAlign string
	Borders       *FlowCellBorders
}

// FlowCellBorders 的空边继承默认边框，Width 为零时隐藏该侧边框。
type FlowCellBorders struct{ Top, Right, Bottom, Left *FlowTableBorder }

// FlowTableBorder 是位于单元格内部的实色边框。
type FlowTableBorder struct {
	Width float64
	Color *creator.Color
}

// FlowTableCell 是支持局部样式、链接和跨行跨列的单元格。
type FlowTableCell struct {
	Text       string
	Runs       []Inline
	ColSpan    int
	RowSpan    int
	Style      *TextStyle
	Appearance *FlowCellStyle
}

// FlowFontFamily 关联同一字体族的四种实际字体资源。
type FlowFontFamily struct{ Regular, Bold, Italic, BoldItalic string }

// FlowOptions 控制流式创作的页面、主题与字体。页面长度为毫米。
type FlowOptions struct {
	PageSize             creator.PageSize
	MarginTop            float64
	MarginRight          float64
	MarginBottom         float64
	MarginLeft           float64
	Theme                Theme
	Official             *FlowOfficial
	Columns              FlowColumns
	Styles               []FlowNamedStyle
	Footnotes            []FlowFootnote
	FootnoteStyle        *TextStyle
	Fonts                []creator.Font
	FontFaces            map[string]int
	FontFamilies         []FlowFontFamily
	FallbackFonts        []string
	BoldFont             string
	ItalicFont           string
	BoldItalicFont       string
	PageNumbers          bool
	Header               string
	Footer               string
	GenerateOutlines     bool
	previousPages        map[string]int
	ReferencePageNumbers string
	previousLabels       map[string]string
	resolvedLabels       map[string]string
	captionLabels        map[string]string
	titleLabels          map[string]string
	noteNumbers          map[string]int
	noteTargets          map[string]FlowFootnote
	// 字体快照已从原始字体读取名称，重排时不再采纳修复器补造的名称。
	fontNamesResolved bool
	balance           *authorBalancePass
	// 主送迁移仅在一次 Compose 内重排，不修改公开的创作源或版记配置。
	relocatedRecipients    []FlowBlock
	officialOriginalBlocks []FlowBlock
}
