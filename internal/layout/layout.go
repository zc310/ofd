// Package layout 把与版式无关的流式文档模型排版为 OFD 固定版式页面。
//
// Markdown、DOCX 等流式格式的导入器只负责把源格式解析为 Document，
// 断行、分页、字体选择和 OFD 生成由本包统一处理。当前实现以 Markdown
// 常见元素（段落、标题、列表、引用、代码、表格、图片）为范围。
package layout

// Kind 是块级元素的类型。
type Kind int

const (
	// KindParagraph 是普通段落。
	KindParagraph Kind = iota
	// KindHeading 是标题。
	KindHeading
	// KindCode 是代码块。
	KindCode
	// KindQuote 是引用块。
	KindQuote
	// KindListItem 是列表项。
	KindListItem
	// KindThematicBreak 是分隔线。
	KindThematicBreak
	// KindTable 是表格。
	KindTable
	// KindImage 是块级图片。
	KindImage
)

// Inline 是行内文本的一段，具有相同的字符样式。
// Text 为 "\n" 时表示硬换行。
type Inline struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
	Strike bool
	Link   bool
}

// Cell 是一个表格单元格的行内内容。
type Cell []Inline

// Align 是单元格水平对齐方式。
type Align int

const (
	// AlignLeft 左对齐。
	AlignLeft Align = iota
	// AlignCenter 居中。
	AlignCenter
	// AlignRight 右对齐。
	AlignRight
)

// Table 是表格的与版式无关表示。
type Table struct {
	Header []Cell
	Rows   [][]Cell
	Align  []Align
}

// Image 是块级图片。
type Image struct {
	// Source 是原始地址，用于诊断信息。
	Source string
	// Alt 是替代文本。
	Alt string
	// Data 是图片二进制数据。
	Data []byte
	// Format 是图片格式，如 PNG、JPEG。
	Format string
	// PixelWidth 和 PixelHeight 是图片像素尺寸。
	PixelWidth  int
	PixelHeight int
}

// Block 是块级元素。
type Block struct {
	Kind Kind
	// Level 是标题级别 1-6。
	Level int
	// Marker 是列表标记，如 "•" 或 "1."。
	Marker string
	// Indent 是缩进层级，用于列表和引用。
	Indent int
	// Inlines 是行内内容。
	Inlines []Inline
	// Code 是代码块纯文本。
	Code string
	// Image 是块级图片。
	Image *Image
	// Table 是表格。
	Table *Table
}

// Document 是与版式无关的流式文档。
type Document struct {
	// Title 是文档标题。
	Title string
	// Blocks 是按顺序排列的块级元素。
	Blocks []Block
}

// Options 控制页面尺寸、字体与间距。长度单位为毫米，字号单位为磅。
type Options struct {
	// PageWidth 和 PageHeight 是页面物理尺寸。
	PageWidth  float64
	PageHeight float64
	// MarginTop、MarginRight、MarginBottom、MarginLeft 是页边距。
	MarginTop    float64
	MarginRight  float64
	MarginBottom float64
	MarginLeft   float64
	// BodySize 是正文字号，MonoSize 是代码字号。
	BodySize float64
	MonoSize float64
	// LineHeight 是行高倍率。
	LineHeight float64
	// CodeLineHeight 是代码块的行高倍率；小于 LineHeight 可以让代码更紧凑。
	CodeLineHeight float64
	// BlockSpacing 是块间距，单位为正文行高的倍率。
	BlockSpacing float64
	// BodyFamily 和 MonoFamily 是逻辑字体族名。
	BodyFamily string
	MonoFamily string
}

// DefaultOptions 返回 A4、20 毫米页边距、12 磅正文的默认排版参数。
func DefaultOptions() Options {
	return Options{
		PageWidth:    210,
		PageHeight:   297,
		MarginTop:    20,
		MarginRight:  20,
		MarginBottom: 20,
		MarginLeft:   20,
		BodySize:     12,
		MonoSize:     10.5,
		LineHeight:   1.5,
		// 代码行距比正文更紧凑，避免代码块显得松散。
		CodeLineHeight: 1.3,
		BlockSpacing:   0.6,
		BodyFamily:     "sans-serif",
		MonoFamily:     "monospace",
	}
}
