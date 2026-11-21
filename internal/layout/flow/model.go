package flow

import "github.com/zc310/ofd/pkg/creator"

// Run 是行内文字及其样式和引用；Color 供代码高亮等内部处理使用。
type Run struct {
	Text       string
	Font       string
	Bold       bool
	Italic     bool
	Code       bool
	Strike     bool
	Color      *creator.Color
	Link       bool
	Href       string
	Target     string
	PageRef    string
	CaptionRef string
	TitleRef   string
	Footnote   string
}

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

// NamedStyle 注册可复用的文字样式。
type NamedStyle struct {
	Name  string
	Style TextStyle
}

// Footnote 是与引用行同页的页底注释。
type Footnote struct {
	ID, Text string
	Runs     []Run
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
	Callouts    CalloutTheme
}

// Block 是顺序创作的内容块，另支持 Markdown 的 quote、code 和 thematic_break。
type Block struct {
	ID              string
	Kind            string
	Text            string
	Runs            []Run
	Level           int
	Marker          string
	Indent          float64
	LeftBorderColor *creator.Color
	LeadingIcon     *Icon
	CodeLang        string
	Style           *TextStyle
	KeepWithNext    bool
	KeepTogether    bool
	PageBreakBefore bool
	PageBreakAfter  bool
	Orphans         int
	Widows          int
	Image           *Image
	Table           *Table
	Fixed           *Fixed
	TOC             *TOC
	Section         *Section
	Callout         *Callout
	// callout 标记展开后的提示框子块，同一容器共用指针，准备完成后重新合并。
	callout *Callout
	// tocTarget 仅用于目录展开后的内部条目。
	tocTarget    string
	captionLabel string
	sourceIndex  int
}

// Callout 是流式提示容器，容纳段落、列表与代码。
type Callout struct {
	Variant, Title         string
	Text                   string
	Runs                   []Run
	ShowIcon               bool
	Blocks                 []Block
	Fill, AccentColor      *creator.Color
	BorderColor            *creator.Color
	Appearance, Pagination string
	RepeatTitle            bool
	ContinuationSuffix     string
	outerIndent            float64
	outerStyle             *TextStyle
}

// CalloutPalette 是单种提示语义的文档配色。
type CalloutPalette struct {
	AccentColor, Fill, BorderColor *creator.Color
}

// CalloutTheme 保存五种提示语义的配色。
type CalloutTheme struct {
	Neutral, Info, Warning, Danger, Success CalloutPalette
}

// Icon 是首行前置的矢量图标，路径使用原始视口坐标。
type Icon struct {
	Width, Height float64
	Data          string
	Rule          string
}

// TOC 在当前位置生成标题或图表目录，标题目录的 MaxLevel 缺省为 3。
type TOC struct {
	Title    string
	MaxLevel int
	Source   string
}

// Margins 是毫米单位的页边距，指针用于区分继承和显式的零边距。
type Margins struct{ Top, Right, Bottom, Left float64 }

// Columns 定义等宽分栏，Count 缺省为 1，Gap 为毫米单位的栏间距。
type Columns struct {
	Count int
	Gap   float64
	// Balance 启用节末页栏高平衡，含显式换栏的末页保持原布局。
	Balance bool
}

// Section 覆盖后续页面设置，分页后只以标准物理页、文字和路径输出。
type Section struct {
	PageSize            *creator.PageSize
	Margins             *Margins
	Columns             *Columns
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

// Image 是图片及图注，尺寸为毫米；未给尺寸时按 96 DPI 推算并限制在版心内。
type Image struct {
	Data          []byte
	Width         float64
	Height        float64
	Caption       string
	Numbered      bool
	CaptionPrefix string
}

// Fixed 是作为整体参与分页的固定区域，内部图元使用左上角为原点的毫米坐标。
type Fixed struct {
	Width, Height float64
	Align         string
	Items         []creator.Item
	Caption       string
	Numbered      bool
	CaptionPrefix string
}

// Table 是简单网格表格，单元格支持自动换行，Header 在续页重复。
type Table struct {
	// SplitRows 按完整文字行续排普通及 ColSpan 单元格，正文 RowSpan 不支持。
	SplitRows    bool
	Header       []string
	Rows         [][]string
	ColumnWidths []float64
	// Grid 与 Header/Rows 互斥，合并单元格占据的位置无需额外占位。
	Grid               [][]TableCell
	HeaderRows         int
	Columns            []TableColumn
	CellStyle          *CellStyle
	StripeFill         *creator.Color
	Caption            string
	Numbered           bool
	CaptionPrefix      string
	RepeatCaption      bool
	ContinuationSuffix string
}

// TableColumn 配置固定宽或按内容分配的列宽，长度为毫米，Weight 为剩余宽度比例。
type TableColumn struct {
	Width, MinWidth, MaxWidth, Weight float64
	NoWrap                            bool
	Align                             string
}

// CellStyle 定义单元格的外观，nil 字段继承表格默认设置。
type CellStyle struct {
	Fill          *creator.Color
	Padding       *Margins
	VerticalAlign string
	Borders       *CellBorders
}

// CellBorders 的空边继承默认边框，Width 为零时隐藏该侧边框。
type CellBorders struct{ Top, Right, Bottom, Left *TableBorder }

// TableBorder 是位于单元格内部的实色边框。
type TableBorder struct {
	Width float64
	Color *creator.Color
}

// TableCell 是支持局部样式、链接和跨行跨列的单元格。
type TableCell struct {
	Text       string
	Runs       []Run
	ColSpan    int
	RowSpan    int
	Style      *TextStyle
	Appearance *CellStyle
}

// FontFamily 关联同一字体族的四种实际字体资源。
type FontFamily struct{ Regular, Bold, Italic, BoldItalic string }
