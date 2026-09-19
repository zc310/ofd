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

// Letterhead 是公文首页的红色版头配置，对应 GB/T 9704-2012 的版头要素。
// 机关标志为红色、加粗并水平居中，上边缘距版心上边缘 35mm；
// 下行文/平行文发文字号在机关标志下空二行居中，上行文设置了 Signatory 时
// 发文字号居左空一字、签发人姓名居右空一字（同一行）；红色分隔线印在
// 发文字号之下 4mm。份号、密级和紧急程度依次顶格堆叠在版心左上角。
// 版头只在首页渲染。
type Letterhead struct {
	// Org 是发文机关标志，如 "××省档案局文件"。
	Org string
	// DocNo 是发文字号，如 "×档发〔2026〕1号"。
	DocNo string
	// Signatory 是签发人姓名；非空时发文字号左移、签发人右移（上行文版式）。
	Signatory string
	// SerialNo 是份号，如 "000001"；非空时在版心左上角第一行显示。
	SerialNo string
	// Security 是密级和保密期限，如 "绝密★5年"；非空时在份号下方显示。
	Security string
	// Urgency 是紧急程度，如 "特急"；非空时在密级下方显示。
	Urgency string
	// Height 是版头区最小高度（毫米），仅当内容较短时用于撑高版头区；0 忽略。
	Height float64
	// OrgSize 是机关标志字号（磅）；0 时使用默认值 56。
	OrgSize float64
	// DocNoSize 是发文字号、签发人与涉密标记字号（磅）；0 时使用默认值 16，对应三号。
	DocNoSize float64
}

// Footer 是公文页脚的页码配置，为空则不渲染页码。
type Footer struct {
	// PageNumber 指示是否在每页版心下边缘之下渲染页码。
	PageNumber bool
	// Size 是页码字号（磅）；0 时使用默认值 14，对应四号。
	Size float64
}

// Colophon 是公文末页的版记配置，为空则不渲染。
// 版记跟随正文流排在最后一面，含抄送与印发机关/日期两条，行间用分隔线。
type Colophon struct {
	// Cc 是抄送机关，如 "省委办公厅，省政府办公厅。"。
	Cc string
	// IssuedBy 是印发机关，如 "××省档案局办公室"。
	IssuedBy string
	// IssuedDate 是印发日期，如 "2026年9月19日"。
	IssuedDate string
	// Size 是版记字号（磅）；0 时使用默认值 14，对应四号。
	Size float64
}

// Signature 是不加盖印章公文的落款（发文机关署名与成文日期）配置，
// 对应 GB/T 9704-2012 的 7.3.5.2：署名在正文（或附件说明）下空一行、右空二字；
// 成文日期在署名下一行，首字比署名首字右移二字。
type Signature struct {
	// Org 是发文机关署名。
	Org string
	// Date 是成文日期，如 "2026年9月19日"。
	Date string
	// Size 是落款字号（磅）；0 时使用默认值 16，对应三号。
	Size float64
}

// Document 是与版式无关的流式文档。
type Document struct {
	// Title 是文档标题。
	Title string
	// Letterhead 是首页红色版头；为空则不渲染。设置后按 GB/T 9704-2012 编排。
	Letterhead *Letterhead
	// Sign 是落款（署名与成文日期）；为空则不渲染。
	Sign *Signature
	// Footer 是页脚页码；为空则不渲染。
	Footer *Footer
	// Colophon 是末页版记；为空则不渲染。
	Colophon *Colophon
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
	// HeiFamily、KaiFamily、TitleFamily、SongFamily 是公文版式专用字族名，
	// 分别对应黑体、楷体、小标宋（文件标题/发文机关标志）与宋体（页码）；
	// 为空时回退到 BodyFamily。
	HeiFamily   string
	KaiFamily   string
	TitleFamily string
	SongFamily  string
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

// GBTOptions 返回 A4 公文版式（GB/T 9704-2012）的排版参数：
// 天头 37mm、订口 28mm、右/下白边 26/35mm，版心 156mm×225mm，正文三号仿宋、
// 每面 22 行。字体族按标准引用：正文仿宋、标题小标宋、序号黑体/楷体、页码宋体；
// OFD 以逻辑字体族名引用，阅读器缺字时回退到自身字体。
func GBTOptions() Options {
	return Options{
		PageWidth:    210,
		PageHeight:   297,
		MarginTop:    37,
		MarginRight:  26,
		MarginBottom: 35,
		MarginLeft:   28,
		BodySize:     16, // 三号
		MonoSize:     10.5,
		// 每面 22 行撑满版心高度 225mm。
		LineHeight:     (225.0 / 22.0) / ptToMM(16),
		CodeLineHeight: 1.3,
		BlockSpacing:   0,
		BodyFamily:     "FangSong",
		MonoFamily:     "FangSong",
		HeiFamily:      "SimHei",
		KaiFamily:      "KaiTi",
		TitleFamily:    "STZhongsong",
		SongFamily:     "SimSun",
	}
}
