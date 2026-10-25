// Package flow 提供流式创作：按顺序添加内容，自动断行分页并生成标准 OFD。
//
// 长度使用毫米，字号使用磅。MaterialTheme、Material2Theme、Material3Theme、Fluent2Theme、CarbonTheme 和 Bootstrap5Theme 提供印刷样式预设。
// GBT9704Options 提供 GB/T 9704—2012 常规公文基础版式及专用字体和页面配置。
// 当前支持水平排列的中文、英文、合并表格、图片图注、列表、标题大纲与页底脚注；复杂文字塑形、
// 浮动环绕暂不支持。默认嵌入 Go 字体，中文需显式提供覆盖
// 所用字符的字体。缺字或不可断文本越界会返回包含块下标的错误。
package flow

import (
	"context"
	"fmt"
	"io"

	"github.com/zc310/ofd/internal/contextio"
	"github.com/zc310/ofd/internal/layout"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
)

// Run 是段落中的文字片段，支持字体、粗体、斜体、删除线及链接。
// Text 中的 CRLF 即使跨越相邻片段或空片段，也只表示一次换行。
type Run struct {
	Text   string
	Font   string
	Bold   bool
	Italic bool
	Strike bool
	Href   string
	// Target 指向 Block.ID，与 Href 互斥。
	Target string
	// PageRef 显示目标内容的最终页码（默认物理页码），并可点击跳转。
	// 与 Text、Href、Target、CaptionRef、TitleRef、Footnote 互斥。
	PageRef string
	// CaptionRef 显示编号图表的标签，如 Table 1，与其他引用字段、Text、Href、Target 互斥。
	CaptionRef string
	// TitleRef 显示目标章节标题或完整图表题注，与其他引用字段、Text、Href、Target 互斥。
	TitleRef string
	// Footnote 显示脚注编号上标，使用所在文字 75% 字号并上移 0.3em；页底编号与注释齐平。
	// 与其他引用字段、Text、Href、Target 互斥。
	Footnote string
}

// TextStyle 是文字样式：Size 为磅，行高为倍率，间距与缩进为毫米。
// 非零值覆盖上层样式；零值缺省继承，可用 Set 显式覆盖。
type TextStyle struct {
	// BasedOn 引用 Options.Styles 中的命名样式，在当前字段之前应用。
	BasedOn string
	// Set 是 StyleSize 等常量的位或，用于显式设置数值零；字号和行高不允许为零。
	Set             uint32
	Font            string
	Size            float64
	LineHeight      float64
	SpaceBefore     float64
	SpaceAfter      float64
	FirstLineIndent float64
	// Align 支持 left、center、right 或 justify；justify 仅伸展自动换行的行。
	Align string
	Color *creator.Color
}

// 以下常量标记 TextStyle 中需要显式覆盖的数值字段。
const (
	StyleSize = uint32(1 << iota)
	StyleLineHeight
	StyleSpaceBefore
	StyleSpaceAfter
	StyleFirstLineIndent
)

// NamedStyle 是可复用的文字样式，Name 在文档内唯一且不含空白。
// Style.BasedOn 可引用其他命名样式，最多继承 64 层。
type NamedStyle struct {
	Name  string
	Style TextStyle
}

// Theme 定义正文、六级标题、图注、表格及边框的样式。
type Theme struct {
	Body        TextStyle
	Headings    [6]TextStyle
	Caption     TextStyle
	Table       TextStyle
	BorderColor *creator.Color
	HeaderFill  *creator.Color
}

// Options 定义页面、页边距、主题及嵌入字体。Fonts 按名称注册实际字体数据，
// FallbackFonts 按字符覆盖依次回退。FontFamilies 优先匹配同族样式，
// BoldFont 等是正文字体族的简写，缺少样式时保留可用的实际字体面。
type Options struct {
	PageSize     creator.PageSize
	MarginTop    float64
	MarginRight  float64
	MarginBottom float64
	MarginLeft   float64
	Theme        Theme
	// Official 是公文专用配置，通常通过 GBT9704Options 初始化。
	Official *Official
	// Columns 配置等宽分栏，缺省单栏；内容按从左到右的栏序流动。
	Columns Columns
	// Styles 注册可用于主题、内容块、目录条目和单元格的命名文字样式。
	Styles []NamedStyle
	// Footnotes 注册可在段落、列表和表格中引用的脚注，按首次引用顺序编号。
	Footnotes []Footnote
	// FootnoteStyle 覆盖默认脚注文字样式（9pt、1.3 倍行高）；可引用命名样式。
	FootnoteStyle *TextStyle
	// Fonts 注册排版可用字体；Build 在合并元数据后清理未引用的字体声明。
	Fonts []creator.Font
	// FontFaces 按字体资源名选择 TTC/OTC 中从 0 开始的字体面索引，缺省为 0。
	// 单字体文件只接受 0；排版先提取所选字体面，再测量与嵌入。
	FontFaces map[string]int
	// FontFamilies 显式关联同族字体面，粗体与斜体在各自的字体族内匹配。
	FontFamilies   []FontFamily
	FallbackFonts  []string
	BoldFont       string
	ItalicFont     string
	BoldItalicFont string
	PageNumbers    bool
	// Header 与 Footer 是单行页眉页脚，支持 {page} 与 {pages}。
	Header string
	Footer string
	// GenerateOutlines 按标题层级生成大纲，跳转到标题最终所在页及位置。
	// 自动大纲追加在 Metadata.Outlines 之后，缺省关闭。
	GenerateOutlines bool
	// ReferencePageNumbers 选择目录与 PageRef 的显示编号：physical（默认）或 section。
	ReferencePageNumbers string
}

// FontFamily 关联同族的实际字体资源，Regular 必填，其余可省略。
// 缺少组合字体时优先使用已注册粗体，其次斜体，再保留原字体，不合成字形。
type FontFamily struct{ Regular, Bold, Italic, BoldItalic string }

// Footnote 是页底注释。ID 在脚注定义内唯一，Text 与 Runs 互斥。
// Runs 可使用文字样式及链接，但不能嵌套脚注。每条注释须与引用行、表格行组或拆分片段同栏。
type Footnote struct {
	ID    string
	Text  string
	Runs  []Run
	Style *TextStyle
}

// Block 是顺序内容块，Kind 可取 paragraph、heading、recipient、list_item、image、table、fixed、toc、section、page_break、column_break。
// Text 与 Runs 互斥；标题默认跟随后至少两行正文，跨页段落默认前后各留两行。
// 无法在整页内满足孤行约束时允许放宽，保证排版能够完成。
type Block struct {
	// ID 是稳定内容标识，在本文档内唯一，不含空白，不能使用 __flow_ 前缀。
	ID     string
	Kind   string
	Text   string
	Runs   []Run
	Level  int
	Marker string
	Style  *TextStyle
	// KeepWithNext 让当前完整块尽量与后续开头同栏，连续设置组成跟随链；显式换页或换栏优先。
	KeepWithNext    bool
	KeepTogether    bool
	PageBreakBefore bool
	PageBreakAfter  bool
	Orphans         int
	Widows          int
	Image           *Image
	Table           *Table
	// Fixed 是作为整体参与分页的固定图元区域。
	Fixed *Fixed
	// TOC 配置 Kind="toc" 的自动目录。
	TOC *TOC
	// Section 配置 Kind="section" 的分节块，新节从下一张实际页面开始。
	Section *Section
}

// Margins 是毫米单位的页边距。
type Margins struct{ Top, Right, Bottom, Left float64 }

// Columns 定义等宽栏。Count 为 0 或 1 时单栏，支持 2 至 8 栏；Gap 为毫米单位的栏间距。
// 脚注在引用所在栏底部排版；Balance 可启用节末页栏高平衡。
type Columns struct {
	Count int
	Gap   float64
	// Balance 在节末页按合法断点重排各栏；含显式换栏的末页保持原布局。
	Balance bool
}

// Section 覆盖后续页面设置。指针为 nil 时继承前节，可显式设置空文字、false 和零边距。
// StartPageNumber 为空时接续前节，正数重新编号；PageNumberStyle 支持 decimal、roman、ROMAN。
// 首页特殊设置仅用于本节，不继承到下一节；隐藏首页页码仍占一个编号。
type Section struct {
	PageSize *creator.PageSize
	Margins  *Margins
	// Columns 为空时继承前节；Count 为 1 时恢复单栏。
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

// TOC 在当前位置生成标题、图或表目录；标题目录的 MaxLevel 缺省为 3。
// Title 为空时省略目录标题；AddTOC() 无参数时使用 Contents。
type TOC struct {
	Title    string
	MaxLevel int
	// Source 选择 headings（默认）、figures 或 tables；MaxLevel 仅用于 headings。
	Source string
}

// Image 是块级图片与图注；Width/Height 为毫米，单边缺省时保持图片比例。
type Image struct {
	Data    []byte
	Width   float64
	Height  float64
	Caption string
	// Numbered 为图注添加独立连续编号，CaptionPrefix 缺省为 Figure。
	Numbered      bool
	CaptionPrefix string
}

// Fixed 将 creator 图元放入流式文档。Width/Height 及内部图元的长度和字号均为毫米。
// Width 缺省为版心宽度，Height 必须为正数；区域及下方题注整块分页，不自动缩放。
// Items 使用相对区域左上角的坐标，支持 Text、Path、Image、Composite 和 PageBlock（最多嵌套 64 层）。
// 每个图元须给出位于区域内的正数边界；文字空 Font 继承块级字体，资源通过 Options/Metadata 注册。
// 水平文字的 Value 简写按字体度量在声明高度内垂直居中，显式 TextCodes 保留原始定位。
type Fixed struct {
	Width, Height float64
	// Align 是 left（默认）、center 或 right。
	Align string
	Items []creator.Item
	// Caption 在区域下方显示图注；Numbered 与图片共用图序号，前缀缺省为 Figure。
	Caption       string
	Numbered      bool
	CaptionPrefix string
}

// Table 支持纯文本简写与合并网格。ColumnWidths 与 Columns 均为空时均分版心。
// 默认按整行或跨行关联行组分页；SplitRows 可启用普通单元格的跨页续排。
type Table struct {
	// SplitRows 按完整文字行跨页或跨栏拆分普通及 ColSpan 单元格；正文 RowSpan 不支持。
	// 各片段独立绘制四边边框、内边距和上下段间距，垂直对齐作用于片段内。
	SplitRows    bool
	Header       []string
	Rows         [][]string
	ColumnWidths []float64
	// Grid 与 Header/Rows 互斥。按从左到右的空闲位置放置单元格，
	// 被跨行或跨列占据的位置不需要占位单元格。
	Grid [][]TableCell
	// HeaderRows 是 Grid 开头需要在续页重复的行数；合并不能跨越表头与正文边界。
	HeaderRows int
	// Columns 与 ColumnWidths 互斥；启用按内容测量的列宽分配。
	Columns []TableColumn
	// CellStyle 设置全表单元格默认外观；StripeFill 设置正文隔行底色。
	CellStyle  *CellStyle
	StripeFill *creator.Color
	// Caption 显示在表格上方，Numbered 为其添加独立连续编号。
	Caption  string
	Numbered bool
	// CaptionPrefix 缺省为 Table。RepeatCaption 在续页重复题注，后缀缺省为 (continued)。
	CaptionPrefix      string
	RepeatCaption      bool
	ContinuationSuffix string
}

// TableColumn 定义固定宽或自动列宽。宽度单位为毫米，零表示自动或不限制。
// 自动列先满足不可断内容与 MinWidth，再向内容理想宽度增长，剩余宽度按 Weight（默认 1）分配。
// Width 非零时固定宽；MaxWidth 是上限。NoWrap 保留显式换行，仅禁止自动换行。
type TableColumn struct {
	Width, MinWidth, MaxWidth, Weight float64
	NoWrap                            bool
	// Align 是 left、center、right 或 justify，单元格 Style 可覆盖。
	Align string
}

// CellStyle 定义单元格外观。空字段继承默认值，Padding 显式给出时允许零内边距。
// 默认内边距为四边 1.5mm，VerticalAlign 支持 top（默认）、middle、bottom。
type CellStyle struct {
	Fill          *creator.Color
	Padding       *Margins
	VerticalAlign string
	Borders       *CellBorders
}

// CellBorders 按侧覆盖默认边框。nil 继承，非空且 Width=0 隐藏该侧边框。
// 相邻单元格的边框各自绘制在内部，隐藏共享边时须同时隐藏两侧。
type CellBorders struct{ Top, Right, Bottom, Left *TableBorder }

// TableBorder 定义毫米单位的实色边框，Color 为空时继承主题边框颜色。
type TableBorder struct {
	Width float64
	Color *creator.Color
}

// TableCell 是带文字样式的合并单元格。Text 与 Runs 互斥。
// ColSpan/RowSpan 为零时表示 1；Style 的段间距作用于单元格文字上下方。
type TableCell struct {
	Text    string
	Runs    []Run
	ColSpan int
	RowSpan int
	Style   *TextStyle
	// Appearance 覆盖表格默认外观。
	Appearance *CellStyle
}

// Document 保存可继续编辑的创作内容。Metadata 提供文档元数据和附加结构，
// Pages、Fonts 和 PageSize 由 Build 的排版结果生成，调用方应通过 Options 配置。
type Document struct {
	Metadata creator.Document
	Options  Options
	Blocks   []Block
}

// MaterialTheme 返回适合 A4 报告的 Material 风格印刷主题。
// 这是独立的字号层级与间距预设；按官方角色映射的主题见 Material2Theme 与 Material3Theme。
func MaterialTheme() Theme {
	return themeFromLayout(layout.MaterialTheme())
}

// Material2Theme 返回按 Material 2 Web 字体角色映射的印刷主题。
// 正文为 12pt、1.5 倍行高，六级标题为 72/45/36/25.5/18/15pt，字距沿用实际字体推进量。
// 内置 Go Regular/Bold 分别近似 Light/Medium；准确字重可通过主题的 Font 字段指定字体资源。
func Material2Theme() Theme {
	return themeFromLayout(layout.Material2Theme())
}

// Material3Theme 返回按 Material 3 Web 字体角色映射的印刷主题。
// 正文为 12pt、1.5 倍行高，前四级标题使用常规字体；字距沿用实际字体推进量。
func Material3Theme() Theme {
	return themeFromLayout(layout.Material3Theme())
}

// Fluent2Theme 返回借鉴 Fluent 2 Web 字体角色的紧凑印刷主题。
// 正文为 10.5pt、20/14 倍行高；默认使用内嵌 Go 字体，可通过主题字段替换。
func Fluent2Theme() Theme {
	return themeFromLayout(layout.Fluent2Theme())
}

// CarbonTheme 返回按 IBM Carbon 静态字体角色映射的印刷主题。
// 正文 12pt、1.5 倍行高，表格采用紧凑的 10.5pt／13.5pt 行高；前四级标题使用常规字体。
// 内置 Go Regular/Bold 近似 Light/Semibold，可注册准确字重并覆盖主题的 Font。
func CarbonTheme() Theme {
	return themeFromLayout(layout.CarbonTheme())
}

// Bootstrap5Theme 返回按 Bootstrap 5.3 默认根字号与 H1–H6 比例映射的印刷主题。
// 正文 12pt、1.5 倍行高、段后 12pt；六级标题为 30/24/21/18/15/12pt。
// 标题的 Medium 字重用 Go Bold 近似；字距沿用实际字体推进量，不按页面宽度动态缩放字号。
func Bootstrap5Theme() Theme {
	return themeFromLayout(layout.Bootstrap5Theme())
}

func themeFromLayout(t layout.Theme) Theme {
	result := Theme{Body: TextStyle(t.Body), Caption: TextStyle(t.Caption), Table: TextStyle(t.Table), BorderColor: t.BorderColor, HeaderFill: t.HeaderFill}
	for i := range t.Headings {
		result.Headings[i] = TextStyle(t.Headings[i])
	}
	return result
}

// DefaultOptions 返回 A4、20mm 页边距与 Material 主题，并嵌入四种 Go 字体面。
// 这些内置字体用于拉丁文字；中文文档请注册自己的中文字体并配置主题字体。
func DefaultOptions() Options {
	return Options{PageSize: creator.A4, MarginTop: 20, MarginRight: 20, MarginBottom: 20, MarginLeft: 20,
		Theme: MaterialTheme(), BoldFont: "Flow-Bold", ItalicFont: "Flow-Italic", BoldItalicFont: "Flow-BoldItalic",
		Fonts: []creator.Font{
			{Name: "Flow-Regular", FamilyName: "Go", Data: goregular.TTF},
			{Name: "Flow-Bold", FamilyName: "Go", Bold: true, Data: gobold.TTF},
			{Name: "Flow-Italic", FamilyName: "Go", Italic: true, Data: goitalic.TTF},
			{Name: "Flow-BoldItalic", FamilyName: "Go", Bold: true, Italic: true, Data: gobolditalic.TTF},
		}}
}

// New 创建流式文档。无参数时使用 DefaultOptions；传入选项时以该选项为准。
func New(options ...Options) *Document {
	opts := DefaultOptions()
	if len(options) > 0 {
		opts = options[0]
	}
	return &Document{Metadata: creator.Document{ID: "flow"}, Options: opts}
}

// AddParagraph 添加正文段落，可传入一个块级样式覆盖。
func (d *Document) AddParagraph(text string, style ...TextStyle) {
	b := Block{Kind: "paragraph", Text: text}
	if len(style) > 0 {
		s := style[0]
		b.Style = &s
	}
	d.Blocks = append(d.Blocks, b)
}

// AddHeading 添加 1 至 6 级标题，默认与后续正文开头保持同页。
func (d *Document) AddHeading(level int, text string) {
	d.Blocks = append(d.Blocks, Block{Kind: "heading", Level: level, Text: text})
}

// AddList 添加有序或无序列表，每项支持悬挂缩进与跨页。
func (d *Document) AddList(ordered bool, items []string) {
	for i, text := range items {
		marker := "•"
		if ordered {
			marker = fmt.Sprintf("%d.", i+1)
		}
		d.Blocks = append(d.Blocks, Block{Kind: "list_item", Marker: marker, Text: text})
	}
}

// AddImage 添加图片及图注。
func (d *Document) AddImage(image Image) {
	d.Blocks = append(d.Blocks, Block{Kind: "image", Image: &image})
}

// AddFixed 添加固定内容块，自动为图元区域与图注预留高度。
func (d *Document) AddFixed(fixed Fixed) {
	d.Blocks = append(d.Blocks, Block{Kind: "fixed", Fixed: &fixed})
}

// AddTable 添加可跨页并重复表头的表格。
func (d *Document) AddTable(table Table) {
	d.Blocks = append(d.Blocks, Block{Kind: "table", Table: &table})
}

// AddPageBreak 在后续内容之前显式换页，不创建尾部空白页。
func (d *Document) AddPageBreak() { d.Blocks = append(d.Blocks, Block{Kind: "page_break"}) }

// AddColumnBreak 在后续内容前换栏，最后一栏后换页；单栏时等同换页，不生成空白尾页。
func (d *Document) AddColumnBreak() { d.Blocks = append(d.Blocks, Block{Kind: "column_break"}) }

// AddTOC 添加自动目录，缺省为标题目录，条目及页码都可点击。目录可以跨页或跨栏。
func (d *Document) AddTOC(options ...TOC) {
	config := TOC{Title: "Contents", MaxLevel: 3}
	if len(options) > 0 {
		config = options[0]
	}
	d.Blocks = append(d.Blocks, Block{Kind: "toc", TOC: &config})
}

// AddFigureList 添加带编号图片与固定图元的目录，缺省标题为 Figures，显式空标题可省略标题。
func (d *Document) AddFigureList(title ...string) {
	name := "Figures"
	if len(title) > 0 {
		name = title[0]
	}
	d.AddTOC(TOC{Title: name, Source: "figures"})
}

// AddTableList 添加带编号表格的目录，缺省标题为 Tables，显式空标题可省略标题。
func (d *Document) AddTableList(title ...string) {
	name := "Tables"
	if len(title) > 0 {
		name = title[0]
	}
	d.AddTOC(TOC{Title: name, Source: "tables"})
}

// AddSection 添加分节块，不额外生成空白尾页；节内设置持续作用到下一个分节。
func (d *Document) AddSection(section Section) {
	d.Blocks = append(d.Blocks, Block{Kind: "section", Section: &section})
}

// Build 完成字体解析、断行与分页，返回供预览和标准 OFD 写出共用的固定版式模型。
// 排版不修改输入。传入的 context 不得为空，取消错误可用 errors.Is 识别。
func (d *Document) Build(ctx context.Context) (*creator.Document, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("文档为空")
	}
	if len(d.Metadata.Pages) > 0 || len(d.Metadata.Fonts) > 0 || d.Metadata.PageSize != (creator.PageSize{}) {
		return nil, fmt.Errorf("Metadata 中的 Pages、Fonts、PageSize 应通过流式内容和 Options 配置")
	}
	for _, text := range []string{d.Options.Header, d.Options.Footer} {
		if err := layout.ValidateFlowText(text); err != nil {
			return nil, err
		}
	}
	for i, b := range d.Blocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		texts := []string{b.Text, b.Marker}
		for _, r := range b.Runs {
			texts = append(texts, r.Text)
		}
		if b.Image != nil {
			texts = append(texts, b.Image.Caption)
		}
		if b.Fixed != nil {
			texts = append(texts, b.Fixed.Caption)
		}
		if b.Table != nil {
			texts = append(texts, b.Table.Caption)
			texts = append(texts, b.Table.Header...)
			for _, row := range b.Table.Rows {
				texts = append(texts, row...)
			}
			for _, row := range b.Table.Grid {
				for _, cell := range row {
					texts = append(texts, cell.Text)
					for _, run := range cell.Runs {
						texts = append(texts, run.Text)
					}
				}
			}
		}
		for _, text := range texts {
			if err := layout.ValidateFlowText(text); err != nil {
				return nil, fmt.Errorf("blocks[%d]: %w", i, err)
			}
		}
	}
	blocks := make([]layout.FlowBlock, len(d.Blocks))
	for i, b := range d.Blocks {
		block := layout.FlowBlock{ID: b.ID, Kind: b.Kind, Text: b.Text, Level: b.Level, Marker: b.Marker, KeepWithNext: b.KeepWithNext, KeepTogether: b.KeepTogether,
			PageBreakBefore: b.PageBreakBefore, PageBreakAfter: b.PageBreakAfter, Orphans: b.Orphans, Widows: b.Widows}
		for _, r := range b.Runs {
			block.Runs = append(block.Runs, layout.Inline{Text: r.Text, Font: r.Font, Bold: r.Bold, Italic: r.Italic, Strike: r.Strike, Href: r.Href, Target: r.Target, PageRef: r.PageRef, CaptionRef: r.CaptionRef, TitleRef: r.TitleRef, Footnote: r.Footnote, Link: r.Href != ""})
		}
		if b.Style != nil {
			s := layout.TextStyle(*b.Style)
			block.Style = &s
		}
		if b.Image != nil {
			image := layout.FlowImage(*b.Image)
			block.Image = &image
		}
		if b.Fixed != nil {
			fixed := layout.FlowFixed(*b.Fixed)
			block.Fixed = &fixed
		}
		if b.TOC != nil {
			toc := layout.FlowTOC(*b.TOC)
			block.TOC = &toc
		}
		if b.Section != nil {
			s := b.Section
			block.Section = &layout.FlowSection{PageSize: s.PageSize, Header: s.Header, Footer: s.Footer, PageNumbers: s.PageNumbers, StartPageNumber: s.StartPageNumber,
				PageNumberStyle: s.PageNumberStyle, PageNumberPrefix: s.PageNumberPrefix, FirstPageHeader: s.FirstPageHeader, FirstPageFooter: s.FirstPageFooter, HideFirstPageNumber: s.HideFirstPageNumber}
			if s.Columns != nil {
				c := layout.FlowColumns(*s.Columns)
				block.Section.Columns = &c
			}
			if s.Margins != nil {
				m := layout.FlowMargins(*s.Margins)
				block.Section.Margins = &m
			}
		}
		if b.Table != nil {
			table := layout.FlowTable{SplitRows: b.Table.SplitRows, Header: b.Table.Header, Rows: b.Table.Rows, ColumnWidths: b.Table.ColumnWidths, HeaderRows: b.Table.HeaderRows,
				CellStyle: layoutCellStyle(b.Table.CellStyle), StripeFill: b.Table.StripeFill, Caption: b.Table.Caption, Numbered: b.Table.Numbered,
				CaptionPrefix: b.Table.CaptionPrefix, RepeatCaption: b.Table.RepeatCaption, ContinuationSuffix: b.Table.ContinuationSuffix}
			for _, column := range b.Table.Columns {
				table.Columns = append(table.Columns, layout.FlowTableColumn(column))
			}
			if b.Table.Grid != nil {
				table.Grid = make([][]layout.FlowTableCell, len(b.Table.Grid))
			}
			for r, row := range b.Table.Grid {
				for _, cell := range row {
					value := layout.FlowTableCell{Text: cell.Text, ColSpan: cell.ColSpan, RowSpan: cell.RowSpan, Appearance: layoutCellStyle(cell.Appearance)}
					for _, run := range cell.Runs {
						value.Runs = append(value.Runs, layout.Inline{Text: run.Text, Font: run.Font, Bold: run.Bold, Italic: run.Italic, Strike: run.Strike, Href: run.Href, Target: run.Target, PageRef: run.PageRef, CaptionRef: run.CaptionRef, TitleRef: run.TitleRef, Footnote: run.Footnote, Link: run.Href != ""})
					}
					if cell.Style != nil {
						style := layout.TextStyle(*cell.Style)
						value.Style = &style
					}
					table.Grid[r] = append(table.Grid[r], value)
				}
			}
			block.Table = &table
		}
		blocks[i] = block
	}
	opts := d.Options
	theme := layout.Theme{Body: layout.TextStyle(opts.Theme.Body), Caption: layout.TextStyle(opts.Theme.Caption), Table: layout.TextStyle(opts.Theme.Table), BorderColor: opts.Theme.BorderColor, HeaderFill: opts.Theme.HeaderFill}
	for i := range theme.Headings {
		theme.Headings[i] = layout.TextStyle(opts.Theme.Headings[i])
	}
	settings := layout.FlowOptions{PageSize: opts.PageSize, MarginTop: opts.MarginTop, MarginRight: opts.MarginRight, MarginBottom: opts.MarginBottom, MarginLeft: opts.MarginLeft,
		Theme: theme, Fonts: opts.Fonts, FontFaces: opts.FontFaces, FallbackFonts: opts.FallbackFonts, BoldFont: opts.BoldFont, ItalicFont: opts.ItalicFont, BoldItalicFont: opts.BoldItalicFont,
		PageNumbers: opts.PageNumbers, Header: opts.Header, Footer: opts.Footer, GenerateOutlines: opts.GenerateOutlines, ReferencePageNumbers: opts.ReferencePageNumbers}
	settings.Columns = layout.FlowColumns(opts.Columns)
	settings.Official = officialToLayout(opts.Official)
	for _, style := range opts.Styles {
		settings.Styles = append(settings.Styles, layout.FlowNamedStyle{Name: style.Name, Style: layout.TextStyle(style.Style)})
	}
	if opts.FootnoteStyle != nil {
		s := layout.TextStyle(*opts.FootnoteStyle)
		settings.FootnoteStyle = &s
	}
	for _, note := range opts.Footnotes {
		n := layout.FlowFootnote{ID: note.ID, Text: note.Text}
		if note.Style != nil {
			s := layout.TextStyle(*note.Style)
			n.Style = &s
		}
		for _, r := range note.Runs {
			n.Runs = append(n.Runs, layout.Inline{Text: r.Text, Font: r.Font, Bold: r.Bold, Italic: r.Italic, Strike: r.Strike, Href: r.Href, Target: r.Target, PageRef: r.PageRef, CaptionRef: r.CaptionRef, TitleRef: r.TitleRef, Footnote: r.Footnote, Link: r.Href != ""})
		}
		settings.Footnotes = append(settings.Footnotes, n)
	}
	for _, family := range opts.FontFamilies {
		settings.FontFamilies = append(settings.FontFamilies, layout.FlowFontFamily(family))
	}
	result, err := layout.Compose(ctx, blocks, settings)
	if err != nil {
		return nil, err
	}
	meta := d.Metadata
	if meta.ID == "" {
		meta.ID = "flow"
	}
	meta.Pages, meta.Fonts, meta.PageSize = result.Pages, result.Fonts, result.PageSize
	meta.Outlines = append(append([]creator.Outline(nil), meta.Outlines...), result.Outlines...)
	bookmarkNames := make(map[string]bool)
	for _, bookmark := range meta.Bookmarks {
		bookmarkNames[bookmark.Name] = true
	}
	for _, bookmark := range result.Bookmarks {
		if bookmarkNames[bookmark.Name] {
			return nil, fmt.Errorf("内容 ID %q 与 Metadata.Bookmarks 重复", bookmark.Name)
		}
	}
	meta.Bookmarks = append(append([]creator.Bookmark(nil), meta.Bookmarks...), result.Bookmarks...)
	meta.PruneUnusedFonts()
	return &meta, nil
}

func layoutCellStyle(s *CellStyle) *layout.FlowCellStyle {
	if s == nil {
		return nil
	}
	v := &layout.FlowCellStyle{Fill: s.Fill, VerticalAlign: s.VerticalAlign}
	if s.Padding != nil {
		p := layout.FlowMargins(*s.Padding)
		v.Padding = &p
	}
	if s.Borders != nil {
		v.Borders = &layout.FlowCellBorders{Top: (*layout.FlowTableBorder)(s.Borders.Top), Right: (*layout.FlowTableBorder)(s.Borders.Right), Bottom: (*layout.FlowTableBorder)(s.Borders.Bottom), Left: (*layout.FlowTableBorder)(s.Borders.Left)}
	}
	return v
}

// Write 排版后写出标准 OFD，创建选项由调用方指定；传入 Deterministic 可固定 ZIP 元数据。
func (d *Document) Write(ctx context.Context, w io.Writer, options creator.CreateOptions) error {
	if w == nil {
		return fmt.Errorf("OFD 输出不能为空")
	}
	document, err := d.Build(ctx)
	if err != nil {
		return err
	}
	err = creator.CreateWithOptions(*document, contextio.Writer(ctx, w), options)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
