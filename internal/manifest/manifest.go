// Package manifest 负责读取声明式 manifest 并构建 OFD 文档模型。
package manifest

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-json/jsontext"
	"github.com/goccy/go-json/v2"

	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v3"
)

// Manifest 是 ofd-creator 支持的声明式输入文件。
type Manifest struct {
	// Version 指定 manifest 格式版本。
	Version int `json:"version,omitzero" yaml:"version,omitempty" toml:"version,omitempty"`
	// Document 定义文档主体及其元数据。
	Document Document `json:"document" yaml:"document,omitempty" toml:"document,omitempty"`
	// Resources 汇总文档级资源。
	Resources Resources `json:"resources" yaml:"resources,omitempty" toml:"resources,omitempty"`
	// Templates 定义可供页面引用的模板页。
	Templates []Template `json:"templates,omitempty" yaml:"templates,omitempty" toml:"templates,omitempty"`
	// Pages 定义文档页面。
	Pages []Page `json:"pages,omitempty" yaml:"pages,omitempty" toml:"pages,omitempty"`
	// Flow 按顺序排版内容，自动生成标准 OFD 页面；与 pages 互斥。
	Flow *Flow `json:"flow,omitzero" yaml:"flow,omitempty"`
}

// Flow 定义流式创作设置；页面尺寸取 document.page_size，字体取 resources.fonts。
type Flow struct {
	// Profile 选择专用版式，支持 gbt9704；与 theme 互斥，需同时提供 official.fonts。
	Profile string `json:"profile,omitempty" yaml:"profile,omitempty"`
	// Official 是 GB/T 9704—2012 常规公文的字体、版头、落款与版记配置。
	Official *FlowOfficial `json:"official,omitzero" yaml:"official,omitempty"`
	// Theme 选择 material（默认）、material2、material3、fluent2、carbon 或 bootstrap5 印刷主题；字体资源仍由 font、heading_font 等字段配置。
	Theme string `json:"theme,omitempty" yaml:"theme,omitempty"`
	// Columns 配置等宽分栏，省略时单栏。
	Columns *FlowColumns `json:"columns,omitzero" yaml:"columns,omitempty"`
	// Styles 注册命名文字样式，可由块或单元格 style.based_on 引用。
	Styles []FlowNamedStyle `json:"styles,omitempty" yaml:"styles,omitempty"`
	// Footnotes 注册页底注释，按正文、列表或表格中的首次引用顺序编号。
	Footnotes []FlowFootnote `json:"footnotes,omitempty" yaml:"footnotes,omitempty"`
	// FootnoteStyle 覆盖默认脚注样式（9pt、1.3 倍行高），支持 based_on。
	FootnoteStyle *FlowStyle `json:"footnote_style,omitzero" yaml:"footnote_style,omitempty"`
	// Font 指定正文、图注与表格的字体资源名，缺省使用内置 Go 字体。
	Font string `json:"font,omitempty" yaml:"font,omitempty"`
	// HeadingFont 指定标题字体，缺省继承显式 font，否则使用内置 Go 粗体。
	HeadingFont string `json:"heading_font,omitempty" yaml:"heading_font,omitempty"`
	// BoldFont 是正文字体族粗体的简写，font_families 中的显式族关系优先。
	BoldFont string `json:"bold_font,omitempty" yaml:"bold_font,omitempty"`
	// ItalicFont 是正文字体族斜体的简写。
	ItalicFont string `json:"italic_font,omitempty" yaml:"italic_font,omitempty"`
	// BoldItalicFont 是正文字体族粗斜体的简写。
	BoldItalicFont string `json:"bold_italic_font,omitempty" yaml:"bold_italic_font,omitempty"`
	// FallbackFonts 是按字符覆盖依次尝试的回退字体资源名。
	FallbackFonts []string `json:"fallback_fonts,omitempty" yaml:"fallback_fonts,omitempty"`
	// FontFaces 按字体资源名选择 TTC/OTC 中从 0 开始的字体面，缺省为 0，单字体文件只接受 0。
	FontFaces map[string]int `json:"font_faces,omitempty" yaml:"font_faces,omitempty"`
	// FontFamilies 显式关联同一字体族的实际字体资源，字重与斜体在各自字体族内匹配。
	FontFamilies []FlowFontFamily `json:"font_families,omitempty" yaml:"font_families,omitempty"`
	// Margins 定义毫米单位的页边距，省略时四边均为 20mm。
	Margins *FlowMargins `json:"margins,omitzero" yaml:"margins,omitempty"`
	// BodySize 是正文字号，单位磅，省略时沿用主题（fluent2 为 10.5，其余普通主题为 12）。
	BodySize float64 `json:"body_size,omitzero" yaml:"body_size,omitempty"`
	// LineHeight 是正文行高倍率，省略时沿用主题（material 为 1.6，fluent2 为 20/14，其余普通主题为 1.5）。
	LineHeight float64 `json:"line_height,omitzero" yaml:"line_height,omitempty"`
	// PageNumbers 控制页码显示；省略或 null 时普通主题默认关闭，gbt9704 默认开启，显式 false 可关闭。
	PageNumbers *bool `json:"page_numbers,omitzero" yaml:"page_numbers,omitempty"`
	// Header 是页眉文字，支持 {page} 与 {pages} 占位符，上页边距须至少 8mm。
	Header string `json:"header,omitempty" yaml:"header,omitempty"`
	// Footer 是页脚文字，支持页码占位符；page_numbers 为真时在其后追加页码。
	Footer string `json:"footer,omitempty" yaml:"footer,omitempty"`
	// GenerateOutlines 按标题层级生成标准大纲，追加在 document.outlines 后，跳转位置由最终分页确定。
	GenerateOutlines bool `json:"generate_outlines,omitzero" yaml:"generate_outlines,omitempty"`
	// ReferencePageNumbers 选择目录与 page_ref 使用 physical（默认物理页码）或 section（节内显示页码）。
	ReferencePageNumbers string `json:"reference_page_numbers,omitempty" yaml:"reference_page_numbers,omitempty"`
	// Blocks 是按顺序排列的逻辑内容。
	Blocks []FlowBlock `json:"blocks,omitempty" yaml:"blocks,omitempty"`
}

// FlowOfficial 定义常规公文专用要素；启用时使用 A4 和 156×225mm 版心。
type FlowOfficial struct {
	// Fonts 指定五类专用字体资源；调用方负责提供对应字形。
	Fonts FlowOfficialFonts `json:"fonts" yaml:"fonts"`
	// Letterhead 定义仅在首页出现的版头。
	Letterhead *FlowOfficialLetterhead `json:"letterhead,omitzero" yaml:"letterhead,omitempty"`
	// Signature 定义不加盖印章的单一机关落款。
	Signature *FlowOfficialSignature `json:"signature,omitzero" yaml:"signature,omitempty"`
	// Colophon 定义锚定末页版心下边缘的版记。
	Colophon *FlowOfficialColophon `json:"colophon,omitzero" yaml:"colophon,omitempty"`
}

// FlowOfficialFonts 引用 resources.fonts 中已注册的字体，不按资源名猜测实际字形。
type FlowOfficialFonts struct {
	// Body 是三号正文及版记使用的仿宋字体资源。
	Body string `json:"body" yaml:"body"`
	// Title 是标题和发文机关标志使用的小标宋字体资源。
	Title string `json:"title" yaml:"title"`
	// Hei 是第一层结构序数、密级及紧急程度使用的黑体字体资源。
	Hei string `json:"hei" yaml:"hei"`
	// Kai 是第二层结构序数和签发人姓名使用的楷体字体资源。
	Kai string `json:"kai" yaml:"kai"`
	// Song 是四号半角页码使用的宋体字体资源。
	Song string `json:"song" yaml:"song"`
}

// FlowOfficialLetterhead 是首页常规版头；不含联合行文、信函或命令专用格式。
type FlowOfficialLetterhead struct {
	// Org 是发文机关标志，不自动追加“文件”。
	Org string `json:"org" yaml:"org"`
	// DocNo 是完整发文字号，由调用方提供规范文字。
	DocNo string `json:"doc_no,omitempty" yaml:"doc_no,omitempty"`
	// Signatory 是单个签发人姓名，非空时采用上行文并排布局。
	Signatory string `json:"signatory,omitempty" yaml:"signatory,omitempty"`
	// SerialNo 是份号。
	SerialNo string `json:"serial_no,omitempty" yaml:"serial_no,omitempty"`
	// Security 是密级和保密期限。
	Security string `json:"security,omitempty" yaml:"security,omitempty"`
	// Urgency 是紧急程度。
	Urgency string `json:"urgency,omitempty" yaml:"urgency,omitempty"`
	// OrgSize 是机关标志字号，单位磅，缺省 32；过宽时报错。
	OrgSize float64 `json:"org_size,omitzero" yaml:"org_size,omitempty"`
}

// FlowOfficialSignature 是不加盖印章的单一机关落款。
type FlowOfficialSignature struct {
	// Org 是发文机关署名。
	Org string `json:"org,omitempty" yaml:"org,omitempty"`
	// Date 是完整成文日期，由调用方使用规范日期文字。
	Date string `json:"date,omitempty" yaml:"date,omitempty"`
}

// FlowOfficialColophon 是末页版记，行宽不足时报错或按悬挂缩进换行。
type FlowOfficialColophon struct {
	// Cc 是抄送机关，不含自动添加的“抄送：”。
	Cc string `json:"cc,omitempty" yaml:"cc,omitempty"`
	// IssuedBy 是印发机关。
	IssuedBy string `json:"issued_by,omitempty" yaml:"issued_by,omitempty"`
	// IssuedDate 是印发日期，不含自动添加的“印发”。
	IssuedDate string `json:"issued_date,omitempty" yaml:"issued_date,omitempty"`
}

// FlowFontFamily 关联同一字体族的四种实际字体面。
type FlowFontFamily struct {
	// Regular 是必填的常规字体资源名。
	Regular string `json:"regular" yaml:"regular"`
	// Bold 是粗体字体资源名，省略时保留原字体。
	Bold string `json:"bold,omitempty" yaml:"bold,omitempty"`
	// Italic 是斜体字体资源名，省略时保留原字体。
	Italic string `json:"italic,omitempty" yaml:"italic,omitempty"`
	// BoldItalic 是粗斜体字体资源名，缺少时按粗体、斜体、原字体顺序选择。
	BoldItalic string `json:"bold_italic,omitempty" yaml:"bold_italic,omitempty"`
}

// FlowMargins 是毫米单位的页边距。
type FlowMargins struct {
	// Top 是上页边距。
	Top float64 `json:"top,omitzero" yaml:"top,omitempty"`
	// Right 是右页边距。
	Right float64 `json:"right,omitzero" yaml:"right,omitempty"`
	// Bottom 是下页边距。
	Bottom float64 `json:"bottom,omitzero" yaml:"bottom,omitempty"`
	// Left 是左页边距。
	Left float64 `json:"left,omitzero" yaml:"left,omitempty"`
}

// FlowNamedStyle 是可复用的文字样式，名称在文档内唯一且不含空白。
type FlowNamedStyle struct {
	// Name 是引用此样式的名称。
	Name string `json:"name" yaml:"name"`
	// Style 定义样式字段，可通过 based_on 继承其他命名样式，最多 64 层。
	Style FlowStyle `json:"style" yaml:"style"`
}

// FlowFootnote 定义与引用行或表格行组同栏的脚注，同栏重复引用只排一次。
type FlowFootnote struct {
	// ID 是脚注定义内唯一的名称，不含空白或控制字符。
	ID string `json:"id" yaml:"id"`
	// Text 是注释纯文本，与 runs 互斥。
	Text string `json:"text,omitempty" yaml:"text,omitempty"`
	// Runs 是带样式和链接的注释片段，不能嵌套脚注。
	Runs []FlowRun `json:"runs,omitempty" yaml:"runs,omitempty"`
	// Style 覆盖全局脚注样式，支持 based_on 和显式零间距。
	Style *FlowStyle `json:"style,omitzero" yaml:"style,omitempty"`
}

// FlowStyle 覆盖文字样式，省略或 null 字段继承；显式零间距和零缩进会清除上层值。
// 字号为磅，行高为倍率，其余长度为毫米；字号、行高不能显式设为零。
type FlowStyle struct {
	// BasedOn 是在当前字段之前应用的命名样式。
	BasedOn string `json:"based_on,omitempty" yaml:"based_on,omitempty"`
	// Font 是字体资源名。
	Font string `json:"font,omitempty" yaml:"font,omitempty"`
	// Size 是字号，单位磅。
	Size *float64 `json:"size,omitzero" yaml:"size,omitempty"`
	// LineHeight 是行高倍率。
	LineHeight *float64 `json:"line_height,omitzero" yaml:"line_height,omitempty"`
	// SpaceBefore 是段前间距。
	SpaceBefore *float64 `json:"space_before,omitzero" yaml:"space_before,omitempty"`
	// SpaceAfter 是段后间距。
	SpaceAfter *float64 `json:"space_after,omitzero" yaml:"space_after,omitempty"`
	// FirstLineIndent 是首行缩进。
	FirstLineIndent *float64 `json:"first_line_indent,omitzero" yaml:"first_line_indent,omitempty"`
	// Align 是 left、center、right 或 justify；justify 仅伸展自动换行的行，段末和显式换行保持自然宽度。
	Align string `json:"align,omitempty" yaml:"align,omitempty"`
	// Color 是文字颜色。
	Color *Color `json:"color,omitzero" yaml:"color,omitempty"`
}

// FlowRun 是段落内的同样式文字片段。
type FlowRun struct {
	// Text 是文字内容。
	Text string `json:"text" yaml:"text"`
	// Font 显式指定字体资源名，空值继承块级字体。
	Font string `json:"font,omitempty" yaml:"font,omitempty"`
	// Bold 选择 flow.bold_font 配置的字体面。
	Bold bool `json:"bold,omitzero" yaml:"bold,omitempty"`
	// Italic 选择 flow.italic_font 配置的字体面。
	Italic bool `json:"italic,omitzero" yaml:"italic,omitempty"`
	// Strike 绘制删除线。
	Strike bool `json:"strike,omitzero" yaml:"strike,omitempty"`
	// Href 定义此片段的点击链接。
	Href string `json:"href,omitempty" yaml:"href,omitempty"`
	// Target 指向内容块的 id，与 href 互斥。
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
	// PageRef 按 reference_page_numbers 显示目标最终页码并提供跳转，与 text、href、target 及其他引用字段互斥。
	PageRef string `json:"page_ref,omitempty" yaml:"page_ref,omitempty"`
	// CaptionRef 显示带编号图表的标签并跳转，与 text、href、target 及其他引用字段互斥。
	CaptionRef string `json:"caption_ref,omitempty" yaml:"caption_ref,omitempty"`
	// TitleRef 显示章节标题或完整图表题注并跳转，与 text、href、target、page_ref、caption_ref、footnote 互斥。
	TitleRef string `json:"title_ref,omitempty" yaml:"title_ref,omitempty"`
	// Footnote 显示脚注编号上标（所在文字 75% 字号、上移 0.3em），与 text、href、target 及其他引用字段互斥。
	Footnote string `json:"footnote,omitempty" yaml:"footnote,omitempty"`
}

// FlowBlock 是无需坐标的内容块。
type FlowBlock struct {
	// ID 是文档内唯一内容标识，不含空白，不能以 __flow_ 开头；换页、换栏和分节块不能设置。
	ID string `json:"id,omitempty" yaml:"id,omitempty"`
	// Type 是 paragraph、heading、recipient、list_item、image、table、fixed、toc、section、page_break 或 column_break，缺省 paragraph；recipient 为公文主送机关。
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// Text 是纯文本内容，与 runs 互斥。
	Text string `json:"text,omitempty" yaml:"text,omitempty"`
	// Runs 是带局部样式的文字片段，与 text 互斥。
	Runs []FlowRun `json:"runs,omitempty" yaml:"runs,omitempty"`
	// Level 是标题级别，heading 必须为 1 到 6。
	Level int `json:"level,omitzero" yaml:"level,omitempty"`
	// Marker 是列表标记，list_item 缺省使用圆点。
	Marker string `json:"marker,omitempty" yaml:"marker,omitempty"`
	// Style 覆盖主题样式。
	Style *FlowStyle `json:"style,omitzero" yaml:"style,omitempty"`
	// KeepWithNext 尽量与后续内容的开头保持同页；连续设置会组成跟随链，标题和公文主送机关默认启用。
	KeepWithNext bool `json:"keep_with_next,omitzero" yaml:"keep_with_next,omitempty"`
	// KeepTogether 能放入完整一页时尽量整块同页。
	KeepTogether bool `json:"keep_together,omitzero" yaml:"keep_together,omitempty"`
	// PageBreakBefore 在此块之前换页。
	PageBreakBefore bool `json:"page_break_before,omitzero" yaml:"page_break_before,omitempty"`
	// PageBreakAfter 在此块之后换页。
	PageBreakAfter bool `json:"page_break_after,omitzero" yaml:"page_break_after,omitempty"`
	// Orphans 是段落跨页时前页至少保留的行数，缺省 2。
	Orphans int `json:"orphans,omitzero" yaml:"orphans,omitempty"`
	// Widows 是段落跨页时后页至少保留的行数，缺省 2。
	Widows int `json:"widows,omitzero" yaml:"widows,omitempty"`
	// Image 是图片与图注，type 为 image 时使用。
	Image *FlowImage `json:"image,omitzero" yaml:"image,omitempty"`
	// Table 是简单网格表格，type 为 table 时使用。
	Table *FlowTable `json:"table,omitzero" yaml:"table,omitempty"`
	// Fixed 是整块分页的固定图元区域。
	Fixed *FlowFixed `json:"fixed,omitzero" yaml:"fixed,omitempty"`
	// TOC 配置 type 为 toc 的自动目录，支持多页条目和最终物理页码。
	TOC *FlowTOC `json:"toc,omitzero" yaml:"toc,omitempty"`
	// Section 配置 type 为 section 的分节块，后续内容从新页开始。
	Section *FlowSection `json:"section,omitzero" yaml:"section,omitempty"`
}

// FlowFixed 将原始图元作为固定区域插入流式文档，长度与图元字号均为毫米。
type FlowFixed struct {
	// Width 是预留宽度，省略时使用当前栏宽，单栏时为版心宽度；超宽时报错。
	Width float64 `json:"width,omitzero" yaml:"width,omitempty"`
	// Height 是必须给出的正数高度，不含图注。
	Height float64 `json:"height" yaml:"height"`
	// Align 为 left（默认）、center 或 right。
	Align string `json:"align,omitempty" yaml:"align,omitempty"`
	// Items 是相对块左上角的图元，支持 text、path、image、composite、page-block。
	Items []Item `json:"items" yaml:"items"`
	// Caption 是区域下方的图注。
	Caption string `json:"caption,omitempty" yaml:"caption,omitempty"`
	// Numbered 与 image 共用连续图编号。
	Numbered bool `json:"numbered,omitzero" yaml:"numbered,omitempty"`
	// CaptionPrefix 是编号前缀，缺省 Figure；需要 numbered。
	CaptionPrefix string `json:"caption_prefix,omitempty" yaml:"caption_prefix,omitempty"`
}

// FlowSection 按需覆盖后续页面设置；省略字段继承前节，空字符串和 false 可显式清除。
type FlowSection struct {
	// PageSize 覆盖纸张尺寸，单位毫米，须同时给出正数 width 和 height。
	PageSize *FlowSectionSize `json:"page_size,omitzero" yaml:"page_size,omitempty"`
	// Margins 覆盖四边页边距，单位毫米，零值表示零边距。
	Margins *FlowMargins `json:"margins,omitzero" yaml:"margins,omitempty"`
	// Columns 覆盖后续页面的分栏设置，省略时继承前节；count: 1 恢复单栏。
	Columns *FlowColumns `json:"columns,omitzero" yaml:"columns,omitempty"`
	// Header 覆盖页眉，空字符串清除前节页眉。
	Header *string `json:"header,omitzero" yaml:"header,omitempty"`
	// Footer 覆盖页脚，空字符串清除前节页脚。
	Footer *string `json:"footer,omitzero" yaml:"footer,omitempty"`
	// PageNumbers 控制自动页码显示，false 可隐藏封面页码。
	PageNumbers *bool `json:"page_numbers,omitzero" yaml:"page_numbers,omitempty"`
	// StartPageNumber 从正数开始重新编号，省略时接续前节。
	StartPageNumber *int `json:"start_page_number,omitzero" yaml:"start_page_number,omitempty"`
	// PageNumberStyle 是 decimal、roman 或 ROMAN，省略继承前节；罗马编号支持 1 到 3999。
	PageNumberStyle string `json:"page_number_style,omitempty" yaml:"page_number_style,omitempty"`
	// PageNumberPrefix 是编号前缀，如 A-，空字符串清除前缀。
	PageNumberPrefix *string `json:"page_number_prefix,omitzero" yaml:"page_number_prefix,omitempty"`
	// FirstPageHeader 只覆盖本节首页页眉，不继承到下一节。
	FirstPageHeader *string `json:"first_page_header,omitzero" yaml:"first_page_header,omitempty"`
	// FirstPageFooter 只覆盖本节首页页脚，不继承到下一节。
	FirstPageFooter *string `json:"first_page_footer,omitzero" yaml:"first_page_footer,omitempty"`
	// HideFirstPageNumber 隐藏本节首页自动追加的页码，首页仍占用编号。
	HideFirstPageNumber bool `json:"hide_first_page_number,omitzero" yaml:"hide_first_page_number,omitempty"`
}

// FlowColumns 定义从左到右顺序流动的等宽栏，可选择平衡节末页。
type FlowColumns struct {
	// Count 是栏数，0 或 1 表示单栏，支持 2 到 8 栏。
	Count int `json:"count,omitzero" yaml:"count,omitempty"`
	// Gap 是毫米单位的栏间距，须为有限非负数，零表示无栏间距。
	Gap float64 `json:"gap,omitzero" yaml:"gap,omitempty"`
	// Balance 在多栏节末页按合法断点重排栏高；含显式换栏的末页保持原布局。
	Balance bool `json:"balance,omitzero" yaml:"balance,omitempty"`
}

// FlowSectionSize 是毫米单位的分节纸张尺寸。
type FlowSectionSize struct {
	// Width 是纸张宽度。
	Width float64 `json:"width" yaml:"width"`
	// Height 是纸张高度。
	Height float64 `json:"height" yaml:"height"`
}

// FlowTOC 配置标题、图或表目录，默认标题目录的最大标题级别为 3。
type FlowTOC struct {
	// Title 是目录标题，显式空值表示省略标题。
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// MaxLevel 是标题目录的最大标题级别，0 表示 3，可取 1 到 6；图表目录必须省略或为 0。
	MaxLevel int `json:"max_level,omitzero" yaml:"max_level,omitempty"`
	// Source 选择 headings（默认）、figures 或 tables；图表目录仅收集带编号内容，max_level 仅用于标题目录。
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
}

// FlowImage 定义图片资源及排版尺寸。
type FlowImage struct {
	// File 是资源根目录内的图片路径，与 data_base64 互斥。
	File string `json:"file,omitempty" yaml:"file,omitempty"`
	// DataBase64 是 Base64 图片数据，与 file 互斥。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty"`
	// Width 是图片宽度，单位毫米，缺省按原图比例推算。
	Width float64 `json:"width,omitzero" yaml:"width,omitempty"`
	// Height 是图片高度，单位毫米，缺省按原图比例推算。
	Height float64 `json:"height,omitzero" yaml:"height,omitempty"`
	// Caption 是图片下方的说明文字。
	Caption string `json:"caption,omitempty" yaml:"caption,omitempty"`
	// Numbered 为图注添加连续编号，图片与表格分别计数。
	Numbered bool `json:"numbered,omitzero" yaml:"numbered,omitempty"`
	// CaptionPrefix 是编号前缀，缺省 Figure；需要 numbered。
	CaptionPrefix string `json:"caption_prefix,omitempty" yaml:"caption_prefix,omitempty"`
}

// FlowTable 定义表格，按完整行组分页，续页重复表头。
type FlowTable struct {
	// SplitRows 按完整文字行跨页或跨栏拆分普通及 ColSpan 单元格；正文 RowSpan 不支持。
	SplitRows bool `json:"split_rows,omitzero" yaml:"split_rows,omitempty"`
	// Header 是可选表头。
	Header []string `json:"header,omitempty" yaml:"header,omitempty"`
	// Rows 是等列数的数据行。
	Rows [][]string `json:"rows,omitempty" yaml:"rows,omitempty"`
	// ColumnWidths 是毫米单位的列宽，省略时均分版心。
	ColumnWidths []float64 `json:"column_widths,omitempty" yaml:"column_widths,omitempty"`
	// Grid 是支持合并、样式与链接的单元格网格，与 header/rows 互斥；被合并占据的位置无需占位。
	Grid [][]FlowTableCell `json:"grid,omitempty" yaml:"grid,omitempty"`
	// HeaderRows 是 grid 开头在续页重复的行数，合并不能跨越表头与正文边界。
	HeaderRows int `json:"header_rows,omitzero" yaml:"header_rows,omitempty"`
	// Columns 配置按内容测量的自动列宽，与 column_widths 互斥；每项对应一列。
	Columns []FlowTableColumn `json:"columns,omitempty" yaml:"columns,omitempty"`
	// CellStyle 设置全表单元格默认外观，单元格 appearance 可以覆盖。
	CellStyle *FlowCellStyle `json:"cell_style,omitzero" yaml:"cell_style,omitempty"`
	// StripeFill 设置正文从第二行开始的隔行底色，跨页保持行序。
	StripeFill *Color `json:"stripe_fill,omitzero" yaml:"stripe_fill,omitempty"`
	// Caption 是表格上方的题注。
	Caption string `json:"caption,omitempty" yaml:"caption,omitempty"`
	// Numbered 为表题添加连续编号，图片与表格分别计数。
	Numbered bool `json:"numbered,omitzero" yaml:"numbered,omitempty"`
	// CaptionPrefix 是编号前缀，缺省 Table；需要 numbered。
	CaptionPrefix string `json:"caption_prefix,omitempty" yaml:"caption_prefix,omitempty"`
	// RepeatCaption 在续页表头上方重复题注，题注与首个行组同页。
	RepeatCaption bool `json:"repeat_caption,omitzero" yaml:"repeat_caption,omitempty"`
	// ContinuationSuffix 是续表题后缀，缺省 (continued)。
	ContinuationSuffix string `json:"continuation_suffix,omitempty" yaml:"continuation_suffix,omitempty"`
}

// FlowTableColumn 定义毫米单位的列宽约束和内容换行行为。
type FlowTableColumn struct {
	// Width 为固定列宽，零表示按内容分配。
	Width float64 `json:"width,omitzero" yaml:"width,omitempty"`
	// MinWidth 是包含内边距的最小列宽。
	MinWidth float64 `json:"min_width,omitzero" yaml:"min_width,omitempty"`
	// MaxWidth 是包含内边距的最大列宽，零表示不限制。
	MaxWidth float64 `json:"max_width,omitzero" yaml:"max_width,omitempty"`
	// Weight 是自动列剩余宽度的比例，零表示 1。
	Weight float64 `json:"weight,omitzero" yaml:"weight,omitempty"`
	// NoWrap 禁止自动换行，保留显式换行；不够宽时返回错误。
	NoWrap bool `json:"no_wrap,omitzero" yaml:"no_wrap,omitempty"`
	// Align 是 left、center、right 或 justify，可由单元格 style 覆盖。
	Align string `json:"align,omitempty" yaml:"align,omitempty"`
}

// FlowCellStyle 定义单元格外观，空字段继承默认设置。
type FlowCellStyle struct {
	// Fill 是背景色，覆盖表头或隔行底色。
	Fill *Color `json:"fill,omitzero" yaml:"fill,omitempty"`
	// Padding 是四边内边距，显式空对象表示零内边距，缺省四边 1.5mm。
	Padding *FlowMargins `json:"padding,omitzero" yaml:"padding,omitempty"`
	// VerticalAlign 为 top、middle 或 bottom，缺省 top。
	VerticalAlign string `json:"vertical_align,omitempty" yaml:"vertical_align,omitempty"`
	// Borders 按侧覆盖默认边框，相邻单元格的边框各自在内部绘制。
	Borders *FlowCellBorders `json:"borders,omitzero" yaml:"borders,omitempty"`
}

// FlowCellBorders 的空边继承默认边框，显式 width: 0 隐藏该侧边框。
type FlowCellBorders struct {
	// Top 是上边框。
	Top *FlowTableBorder `json:"top,omitzero" yaml:"top,omitempty"`
	// Right 是右边框。
	Right *FlowTableBorder `json:"right,omitzero" yaml:"right,omitempty"`
	// Bottom 是下边框。
	Bottom *FlowTableBorder `json:"bottom,omitzero" yaml:"bottom,omitempty"`
	// Left 是左边框。
	Left *FlowTableBorder `json:"left,omitzero" yaml:"left,omitempty"`
}

// FlowTableBorder 定义毫米单位的实色边框。
type FlowTableBorder struct {
	// Width 是边框宽度，零表示隐藏。
	Width float64 `json:"width" yaml:"width"`
	// Color 是边框颜色，缺省继承主题边框颜色。
	Color *Color `json:"color,omitzero" yaml:"color,omitempty"`
}

// FlowTableCell 定义按空闲列依次放置的合并单元格。
type FlowTableCell struct {
	// Text 是纯文本，与 runs 互斥。
	Text string `json:"text,omitempty" yaml:"text,omitempty"`
	// Runs 是带字体、字重和链接的文字片段，与 text 互斥。
	Runs []FlowRun `json:"runs,omitempty" yaml:"runs,omitempty"`
	// ColSpan 是跨列数量，零值表示 1。
	ColSpan int `json:"col_span,omitzero" yaml:"col_span,omitempty"`
	// RowSpan 是跨行数量，零值表示 1。
	RowSpan int `json:"row_span,omitzero" yaml:"row_span,omitempty"`
	// Style 覆盖单元格文字样式，段间距位于文字上下方。
	Style *FlowStyle `json:"style,omitzero" yaml:"style,omitempty"`
	// Appearance 覆盖表格默认单元格外观。
	Appearance *FlowCellStyle `json:"appearance,omitzero" yaml:"appearance,omitempty"`
}

// Document 描述 OFD 文档及其元数据、资源和页面内容。
type Document struct {
	// ID 标识文档。
	ID string `json:"id,omitempty" yaml:"id,omitempty" toml:"id,omitempty"`
	// Title 记录文档标题。
	Title string `json:"title,omitempty" yaml:"title,omitempty" toml:"title,omitempty"`
	// Author 记录文档作者。
	Author string `json:"author,omitempty" yaml:"author,omitempty" toml:"author,omitempty"`
	// Subject 记录文档主题。
	Subject string `json:"subject,omitempty" yaml:"subject,omitempty" toml:"subject,omitempty"`
	// Abstract 记录文档摘要。
	Abstract string `json:"abstract,omitempty" yaml:"abstract,omitempty" toml:"abstract,omitempty"`
	// Creator 记录创建文档的应用名称。
	Creator string `json:"creator,omitempty" yaml:"creator,omitempty" toml:"creator,omitempty"`
	// CreatorVersion 记录创建应用的版本。
	CreatorVersion string `json:"creator_version,omitempty" yaml:"creator_version,omitempty" toml:"creator_version,omitempty"`
	// PageSize 定义默认页面尺寸。
	PageSize PageSize `json:"page_size" yaml:"page_size,omitempty" toml:"page_size,omitempty"`
	// Actions 定义文档级动作。
	Actions []Action `json:"actions,omitempty" yaml:"actions,omitempty" toml:"actions,omitempty"`
	// Bookmarks 定义文档书签。
	Bookmarks []Bookmark `json:"bookmarks,omitempty" yaml:"bookmarks,omitempty" toml:"bookmarks,omitempty"`
	// Attachments 定义文档附件。
	Attachments []Attachment `json:"attachments,omitempty" yaml:"attachments,omitempty" toml:"attachments,omitempty"`
	// Extensions 定义文档扩展信息。
	Extensions []Extension `json:"extensions,omitempty" yaml:"extensions,omitempty" toml:"extensions,omitempty"`
	// Versions 定义文档版本记录。
	Versions []Version `json:"versions,omitempty" yaml:"versions,omitempty" toml:"versions,omitempty"`
	// Annotations 定义按页面组织的批注。
	Annotations []AnnotationPage `json:"annotations,omitempty" yaml:"annotations,omitempty" toml:"annotations,omitempty"`
	// DocUsage 说明文档用途。
	DocUsage string `json:"doc_usage,omitempty" yaml:"doc_usage,omitempty" toml:"doc_usage,omitempty"`
	// Keywords 记录文档关键词。
	Keywords []string `json:"keywords,omitempty" yaml:"keywords,omitempty" toml:"keywords,omitempty"`
	// CustomData 定义文档自定义键值数据。
	CustomData []CustomData `json:"custom_data,omitempty" yaml:"custom_data,omitempty" toml:"custom_data,omitempty"`
	// Cover 指定封面文件路径。
	Cover string `json:"cover,omitempty" yaml:"cover,omitempty" toml:"cover,omitempty"`
	// CoverBase64 提供 Base64 编码的封面数据。
	CoverBase64 string `json:"cover_base64,omitempty" yaml:"cover_base64,omitempty" toml:"cover_base64,omitempty"`
	// CoverName 指定封面在文档中的名称。
	CoverName string `json:"cover_name,omitempty" yaml:"cover_name,omitempty" toml:"cover_name,omitempty"`
	// DocType 指定 OFD.xml 根节点的 DocType 取值，决定文档遵循的 profile。
	// 留空或 "OFD" 为 GB/T 33190—2016 的基础 profile；"OFD-A" 是
	// GB/T 42133—2022《信息技术 OFD档案应用指南》规定的档案长期保存 profile，
	// "OFD-H" 是 GB/T 48666-2026《电子病历版式文档技术要求》
	// 规定的电子病历 profile。取值区分大小写，未知取值在生成阶段报错。
	DocType string `json:"doc_type,omitempty" yaml:"doc_type,omitempty" toml:"doc_type,omitempty"`
	// CreationDate 记录文档创建时间。
	CreationDate string `json:"creation_date,omitempty" yaml:"creation_date,omitempty" toml:"creation_date,omitempty"`
	// ModDate 记录文档最后修改时间。
	ModDate string `json:"mod_date,omitempty" yaml:"mod_date,omitempty" toml:"mod_date,omitempty"`
	// Area 定义文档页面区域。
	Area *PageArea `json:"area,omitzero" yaml:"area,omitempty" toml:"area,omitempty"`
	// DefaultCS 指定文档默认颜色空间编号。
	DefaultCS uint64 `json:"default_cs,omitzero" yaml:"default_cs,omitempty" toml:"default_cs,omitempty"`
	// Preferences 定义文档查看偏好。
	Preferences *Preferences `json:"preferences,omitzero" yaml:"preferences,omitempty" toml:"preferences,omitempty"`
	// Permissions 定义文档访问和操作权限。
	Permissions *Permissions `json:"permissions,omitzero" yaml:"permissions,omitempty" toml:"permissions,omitempty"`
	// Outlines 定义文档大纲。
	Outlines []Outline `json:"outlines,omitempty" yaml:"outlines,omitempty" toml:"outlines,omitempty"`
	// Signatures 定义文档数字签名。
	Signatures []Signature `json:"signatures,omitempty" yaml:"signatures,omitempty" toml:"signatures,omitempty"`
}

// PageSize 描述文档页面尺寸。
type PageSize struct {
	// Name 指定预定义页面尺寸名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// Width 指定页面宽度。
	Width float64 `json:"width,omitzero" yaml:"width,omitempty" toml:"width,omitempty"`
	// Height 指定页面高度。
	Height float64 `json:"height,omitzero" yaml:"height,omitempty" toml:"height,omitempty"`
}

// Resources 汇总文档级资源。
type Resources struct {
	// Fonts 汇总字体资源。
	Fonts []Font `json:"fonts,omitempty" yaml:"fonts,omitempty" toml:"fonts,omitempty"`
	// Images 汇总图像资源。
	Images []Image `json:"images,omitempty" yaml:"images,omitempty" toml:"images,omitempty"`
	// Media 汇总媒体资源。
	Media []Media `json:"media,omitempty" yaml:"media,omitempty" toml:"media,omitempty"`
	// ColorSpaces 汇总颜色空间资源。
	ColorSpaces []ColorSpace `json:"color_spaces,omitempty" yaml:"color_spaces,omitempty" toml:"color_spaces,omitempty"`
	// DrawParams 汇总图形绘制参数。
	DrawParams []DrawParam `json:"draw_params,omitempty" yaml:"draw_params,omitempty" toml:"draw_params,omitempty"`
	// Composites 汇总复合图形单元资源。
	Composites []Composite `json:"composites,omitempty" yaml:"composites,omitempty" toml:"composites,omitempty"`
	// Public 汇总公共资源。
	Public []PublicResource `json:"public,omitempty" yaml:"public,omitempty" toml:"public,omitempty"`
	// CustomTags 汇总自定义标签资源。
	CustomTags []CustomTag `json:"custom_tags,omitempty" yaml:"custom_tags,omitempty" toml:"custom_tags,omitempty"`
}

// Font 描述文档使用的字体资源。
type Font struct {
	// Name 指定文档内唯一的字体资源引用名，与文字的 font 字段对应。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// FontName 指定 OFD 中的实际字型名；省略时普通创建使用 name，flow 从嵌入字体自动补全。
	FontName string `json:"font_name,omitempty" yaml:"font_name,omitempty"`
	// FamilyName 指定字体族名称。
	FamilyName string `json:"family_name,omitempty" yaml:"family_name,omitempty" toml:"family_name,omitempty"`
	// Charset 指定字体字符集。
	Charset string `json:"charset,omitempty" yaml:"charset,omitempty" toml:"charset,omitempty"`
	// Format 指定字体文件格式。
	Format string `json:"format,omitempty" yaml:"format,omitempty" toml:"format,omitempty"`
	// File 指定字体文件路径。
	File string `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
	// DataBase64 提供 Base64 编码的字体数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
	// Italic 指示字体是否为斜体。
	Italic bool `json:"italic,omitzero" yaml:"italic,omitempty" toml:"italic,omitempty"`
	// Bold 指示字体是否为粗体。
	Bold bool `json:"bold,omitzero" yaml:"bold,omitempty" toml:"bold,omitempty"`
	// Serif 指示字体是否为衬线字体。
	Serif bool `json:"serif,omitzero" yaml:"serif,omitempty" toml:"serif,omitempty"`
	// FixedWidth 指示字体是否为等宽字体。
	FixedWidth bool `json:"fixed_width,omitzero" yaml:"fixed_width,omitempty" toml:"fixed_width,omitempty"`
}

// Image 描述文档中的图像资源。
type Image struct {
	// ID 标识图像资源。
	ID uint64 `json:"id,omitzero" yaml:"id,omitempty" toml:"id,omitempty"`
	// Format 指定图像格式。
	Format string `json:"format,omitempty" yaml:"format,omitempty" toml:"format,omitempty"`
	// Name 指定图像名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// File 指定图像文件路径。
	File string `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
	// DataBase64 提供 Base64 编码的图像数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
}

// Media 描述文档中的媒体资源。
type Media struct {
	// ID 标识媒体资源。
	ID uint64 `json:"id,omitzero" yaml:"id,omitempty" toml:"id,omitempty"`
	// Type 指定媒体类型。
	Type string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	// Format 指定媒体格式。
	Format string `json:"format,omitempty" yaml:"format,omitempty" toml:"format,omitempty"`
	// Name 指定媒体名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// File 指定媒体文件路径。
	File string `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
	// DataBase64 提供 Base64 编码的媒体数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
}

// Page 描述文档页面及其图层、图元和页面资源。
type Page struct {
	// Repeat 是模板渲染时从根数据读取的数组路径；每条记录生成一页，空数组不生成本页。
	Repeat string `json:"repeat,omitempty" yaml:"repeat,omitempty"`
	// Paginate 将当前记录中的明细数组按固定区域容量分页，可与 repeat 组合。
	Paginate *DetailPagination `json:"paginate,omitzero" yaml:"paginate,omitempty"`
	// Variants 为明细分页选择首页、续页、末页或单页附加版式；仅与 paginate 配合使用。
	Variants *PageVariants `json:"variants,omitzero" yaml:"variants,omitempty"`
	// Templates 指定页面引用的模板页。
	Templates []TemplateRef `json:"templates,omitempty" yaml:"templates,omitempty" toml:"templates,omitempty"`
	// Layers 定义页面图层。
	Layers []Layer `json:"layers,omitempty" yaml:"layers,omitempty" toml:"layers,omitempty"`
	// Items 定义页面图元。
	Items []Item `json:"items,omitempty" yaml:"items,omitempty" toml:"items,omitempty"`
	// Area 定义页面区域。
	Area *PageArea `json:"area,omitzero" yaml:"area,omitempty" toml:"area,omitempty"`
	// LayerType 指定页面图层类型。
	LayerType string `json:"layer_type,omitempty" yaml:"layer_type,omitempty" toml:"layer_type,omitempty"`
	// Actions 定义页面级动作。
	Actions []Action `json:"actions,omitempty" yaml:"actions,omitempty" toml:"actions,omitempty"`
	// Resources 定义页面级资源。
	Resources []PageResource `json:"resources,omitempty" yaml:"resources,omitempty" toml:"resources,omitempty"`
}

// DetailPagination 描述固定区域明细表；列宽之和必须等于 area.width，所有尺寸均为毫米。
type DetailPagination struct {
	// Path 是当前记录中的明细数组路径；$. 前缀从根数据读取。
	Path string `json:"path" yaml:"path"`
	// Area 是含表头的明细区域，放不下的完整行转入下一页。
	Area Box `json:"area" yaml:"area"`
	// Columns 定义表头、列宽和逐行文字绑定。
	Columns []DetailColumn `json:"columns" yaml:"columns"`
	// Font 指定完整字体资源，省略时嵌入 Go Regular。
	Font string `json:"font,omitempty" yaml:"font,omitempty"`
	// Size 是明细字号，默认 3.5mm。
	Size float64 `json:"size,omitzero" yaml:"size,omitempty"`
	// HeaderHeight 是重复表头高度，0 表示按文字实际高度计算且至少 8mm。
	HeaderHeight float64 `json:"header_height,omitzero" yaml:"header_height,omitempty"`
	// RowHeight 是最小行高，默认 8mm；换行会增高整行，不拆分单行。
	RowHeight float64 `json:"row_height,omitzero" yaml:"row_height,omitempty"`
	// Padding 是单元格四边内边距，默认 1.5mm，可显式设为 0。
	Padding *float64 `json:"padding,omitzero" yaml:"padding,omitempty"`
	// LineWidth 是网格线宽，默认 0.2mm。
	LineWidth float64 `json:"line_width,omitzero" yaml:"line_width,omitempty"`
	// BorderColor 是网格颜色，默认中性灰。
	BorderColor *Color `json:"border_color,omitzero" yaml:"border_color,omitempty"`
	// HeaderFill 是表头背景色，省略时使用浅灰。
	HeaderFill *Color `json:"header_fill,omitzero" yaml:"header_fill,omitempty"`
}

// DetailColumn 定义明细列；bind.path 相对于当前明细行，默认 overflow 为 wrap。
type DetailColumn struct {
	// Title 是每页重复的表头文字。
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// Width 是列宽，必须大于两倍 padding。
	Width float64 `json:"width" yaml:"width"`
	// Bind 读取明细字段，可使用 @index/@number 表示整张单据内的行序号。
	Bind Binding `json:"bind" yaml:"bind"`
}

// PageVariants 定义明细分页的附加内容和明细区域。
type PageVariants struct {
	// First 用于多页单据的首页。
	First *PageVariant `json:"first,omitzero" yaml:"first,omitempty"`
	// Continuation 用于中间页，也作为未配置 last 时的末页版式。
	Continuation *PageVariant `json:"continuation,omitzero" yaml:"continuation,omitempty"`
	// Last 用于多页单据的末页。
	Last *PageVariant `json:"last,omitzero" yaml:"last,omitempty"`
	// Single 用于单页单据；省略时按 last、first 的顺序回退。
	Single *PageVariant `json:"single,omitzero" yaml:"single,omitempty"`
}

// PageVariant 追加到公共页面；不替换公共图元或模板引用。
type PageVariant struct {
	// Templates 是该版式附加的 OFD 模板页引用。
	Templates []TemplateRef `json:"templates,omitempty" yaml:"templates,omitempty"`
	// Items 是该版式附加的固定图元，与 layers 互斥。
	Items []Item `json:"items,omitempty" yaml:"items,omitempty"`
	// Layers 是该版式附加的图层，与 items 互斥。
	Layers []Layer `json:"layers,omitempty" yaml:"layers,omitempty"`
	// DetailArea 覆盖明细区域的位置和高度；宽度必须等于公共明细区域宽度。
	DetailArea *Box `json:"detail_area,omitzero" yaml:"detail_area,omitempty"`
}

// PublicResource 描述文档中的公共资源。
type PublicResource struct {
	// Name 指定公共资源名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// File 指定公共资源文件路径。
	File string `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
	// DataBase64 提供 Base64 编码的公共资源数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
	// Files 定义公共资源包中的文件。
	Files []ResourceFile `json:"files,omitempty" yaml:"files,omitempty" toml:"files,omitempty"`
}

// PageResource 描述页面级资源文件或嵌入式图像。
type PageResource struct {
	// File 指定页面资源文件路径。
	File string `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
	// DataBase64 提供 Base64 编码的页面资源数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
	// Images 定义资源中的图像。
	Images []PageImage `json:"images,omitempty" yaml:"images,omitempty" toml:"images,omitempty"`
	// Files 定义页面资源包中的文件。
	Files []ResourceFile `json:"files,omitempty" yaml:"files,omitempty" toml:"files,omitempty"`
}

// PageImage 描述页面资源中的图像。
type PageImage struct {
	// ID 标识页面图像资源。
	ID uint64 `json:"id,omitzero" yaml:"id,omitempty" toml:"id,omitempty"`
	// Format 指定图像格式。
	Format string `json:"format,omitempty" yaml:"format,omitempty" toml:"format,omitempty"`
	// Name 指定图像名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// File 指定图像文件路径。
	File string `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
	// DataBase64 提供 Base64 编码的图像数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
}

// ResourceFile 描述资源包中的文件。
type ResourceFile struct {
	// Path 指定文件在资源包中的路径。
	Path string `json:"path,omitempty" yaml:"path,omitempty" toml:"path,omitempty"`
	// File 指定源文件路径。
	File string `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
	// DataBase64 提供 Base64 编码的文件数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
}

// CustomTag 描述文档自定义标签及其架构和数据。
type CustomTag struct {
	// NameSpace 指定自定义标签命名空间。
	NameSpace string `json:"name_space,omitempty" yaml:"name_space,omitempty" toml:"name_space,omitempty"`
	// Schema 指定架构文件路径。
	Schema string `json:"schema,omitempty" yaml:"schema,omitempty" toml:"schema,omitempty"`
	// SchemaBase64 提供 Base64 编码的架构数据。
	SchemaBase64 string `json:"schema_base64,omitempty" yaml:"schema_base64,omitempty" toml:"schema_base64,omitempty"`
	// SchemaName 指定架构在文档中的名称。
	SchemaName string `json:"schema_name,omitempty" yaml:"schema_name,omitempty" toml:"schema_name,omitempty"`
	// Data 指定自定义数据文件路径或文本数据。
	Data string `json:"data,omitempty" yaml:"data,omitempty" toml:"data,omitempty"`
	// DataBase64 提供 Base64 编码的自定义数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
	// DataName 指定自定义数据在文档中的名称。
	DataName string `json:"data_name,omitempty" yaml:"data_name,omitempty" toml:"data_name,omitempty"`
}

// Permissions 描述文档的访问和操作权限。
type Permissions struct {
	// Edit 指示是否允许编辑文档。
	Edit *bool `json:"edit,omitzero" yaml:"edit,omitempty" toml:"edit,omitempty"`
	// Annot 指示是否允许添加或修改批注。
	Annot *bool `json:"annot,omitzero" yaml:"annot,omitempty" toml:"annot,omitempty"`
	// Export 指示是否允许导出文档。
	Export *bool `json:"export,omitzero" yaml:"export,omitempty" toml:"export,omitempty"`
	// Signature 指示是否允许签名。
	Signature *bool `json:"signature,omitzero" yaml:"signature,omitempty" toml:"signature,omitempty"`
	// Watermark 指示是否允许修改水印。
	Watermark *bool `json:"watermark,omitzero" yaml:"watermark,omitempty" toml:"watermark,omitempty"`
	// PrintScreen 指示是否允许屏幕截图。
	PrintScreen *bool `json:"print_screen,omitzero" yaml:"print_screen,omitempty" toml:"print_screen,omitempty"`
	// Print 定义打印权限和设置。
	Print *PrintSettings `json:"print,omitzero" yaml:"print,omitempty" toml:"print,omitempty"`
	// ValidPeriod 定义权限的有效时间范围。
	ValidPeriod *ValidPeriod `json:"valid_period,omitzero" yaml:"valid_period,omitempty" toml:"valid_period,omitempty"`
}

// PrintSettings 描述文档打印设置。
type PrintSettings struct {
	// Printable 指示是否允许打印。
	Printable bool `json:"printable,omitzero" yaml:"printable,omitempty" toml:"printable,omitempty"`
	// Copies 指定允许或默认的打印份数。
	Copies *int `json:"copies,omitzero" yaml:"copies,omitempty" toml:"copies,omitempty"`
}

// ValidPeriod 描述权限的有效时间范围。
type ValidPeriod struct {
	// Start 指定权限生效时间。
	Start string `json:"start,omitempty" yaml:"start,omitempty" toml:"start,omitempty"`
	// End 指定权限失效时间。
	End string `json:"end,omitempty" yaml:"end,omitempty" toml:"end,omitempty"`
}

// Template 描述可供页面引用的模板页。
type Template struct {
	// ID 标识模板页。
	ID uint64 `json:"id,omitzero" yaml:"id,omitempty" toml:"id,omitempty"`
	// Name 指定模板页名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// ZOrder 指定模板页的层叠顺序。
	ZOrder string `json:"z_order,omitempty" yaml:"z_order,omitempty" toml:"z_order,omitempty"`
	// Layers 定义模板页图层。
	Layers []Layer `json:"layers,omitempty" yaml:"layers,omitempty" toml:"layers,omitempty"`
	// Items 定义模板页图元。
	Items []Item `json:"items,omitempty" yaml:"items,omitempty" toml:"items,omitempty"`
	// Area 定义模板页区域。
	Area *PageArea `json:"area,omitzero" yaml:"area,omitempty" toml:"area,omitempty"`
}

// TemplateRef 描述页面对模板页的引用。
type TemplateRef struct {
	// ID 指定所引用的模板页。
	ID uint64 `json:"id,omitzero" yaml:"id,omitempty" toml:"id,omitempty"`
	// ZOrder 指定模板引用的层叠顺序。
	ZOrder string `json:"z_order,omitempty" yaml:"z_order,omitempty" toml:"z_order,omitempty"`
}

// Layer 描述页面或模板中的图层。
type Layer struct {
	// Type 指定图层类型。
	Type string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	// DrawParam 指定图层使用的绘制参数名称。
	DrawParam string `json:"draw_param,omitempty" yaml:"draw_param,omitempty" toml:"draw_param,omitempty"`
	// Items 定义图层中的图元。
	Items []Item `json:"items,omitempty" yaml:"items,omitempty" toml:"items,omitempty"`
}

// PageArea 描述页面的物理、应用、内容和出血区域。
type PageArea struct {
	// PhysicalBox 定义页面物理区域。
	PhysicalBox *Box `json:"physical_box,omitzero" yaml:"physical_box,omitempty" toml:"physical_box,omitempty"`
	// ApplicationBox 定义页面应用区域。
	ApplicationBox *Box `json:"application_box,omitzero" yaml:"application_box,omitempty" toml:"application_box,omitempty"`
	// ContentBox 定义页面内容区域。
	ContentBox *Box `json:"content_box,omitzero" yaml:"content_box,omitempty" toml:"content_box,omitempty"`
	// BleedBox 定义页面出血区域。
	BleedBox *Box `json:"bleed_box,omitzero" yaml:"bleed_box,omitempty" toml:"bleed_box,omitempty"`
}

// CustomData 描述文档自定义键值数据。
type CustomData struct {
	// Name 指定自定义数据名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// Value 指定自定义数据值。
	Value string `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
}

// Preferences 描述文档查看偏好设置。
type Preferences struct {
	// PageMode 指定页面显示模式。
	PageMode string `json:"page_mode,omitempty" yaml:"page_mode,omitempty" toml:"page_mode,omitempty"`
	// PageLayout 指定页面布局方式。
	PageLayout string `json:"page_layout,omitempty" yaml:"page_layout,omitempty" toml:"page_layout,omitempty"`
	// TabDisplay 指定文档标签页显示方式。
	TabDisplay string `json:"tab_display,omitempty" yaml:"tab_display,omitempty" toml:"tab_display,omitempty"`
	// HideToolbar 指示是否隐藏工具栏。
	HideToolbar *bool `json:"hide_toolbar,omitzero" yaml:"hide_toolbar,omitempty" toml:"hide_toolbar,omitempty"`
	// HideMenubar 指示是否隐藏菜单栏。
	HideMenubar *bool `json:"hide_menubar,omitzero" yaml:"hide_menubar,omitempty" toml:"hide_menubar,omitempty"`
	// HideWindowUI 指示是否隐藏窗口界面元素。
	HideWindowUI *bool `json:"hide_window_ui,omitzero" yaml:"hide_window_ui,omitempty" toml:"hide_window_ui,omitempty"`
	// ZoomMode 指定缩放模式。
	ZoomMode string `json:"zoom_mode,omitempty" yaml:"zoom_mode,omitempty" toml:"zoom_mode,omitempty"`
	// Zoom 指定页面缩放比例。
	Zoom *float64 `json:"zoom,omitzero" yaml:"zoom,omitempty" toml:"zoom,omitempty"`
}

// ColorSpace 描述文档使用的颜色空间资源。
type ColorSpace struct {
	// ID 标识颜色空间资源。
	ID uint64 `json:"id,omitzero" yaml:"id,omitempty" toml:"id,omitempty"`
	// Type 指定颜色空间类型。
	Type string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	// BitsPerComponent 指定每个颜色分量的位数。
	BitsPerComponent int `json:"bits_per_component,omitzero" yaml:"bits_per_component,omitempty" toml:"bits_per_component,omitempty"`
	// Palette 定义颜色索引到颜色分量的映射。
	Palette []string `json:"palette,omitempty" yaml:"palette,omitempty" toml:"palette,omitempty"`
	// ProfileFile 指定颜色配置文件路径。
	ProfileFile string `json:"profile_file,omitempty" yaml:"profile_file,omitempty" toml:"profile_file,omitempty"`
	// ProfileBase64 提供 Base64 编码的颜色配置数据。
	ProfileBase64 string `json:"profile_base64,omitempty" yaml:"profile_base64,omitempty" toml:"profile_base64,omitempty"`
	// ProfileName 指定颜色配置文件名称。
	ProfileName string `json:"profile_name,omitempty" yaml:"profile_name,omitempty" toml:"profile_name,omitempty"`
}

// DrawParam 描述图形绘制参数。
type DrawParam struct {
	// Name 指定绘制参数名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// Relative 指定参数相对的坐标或对象。
	Relative string `json:"relative,omitempty" yaml:"relative,omitempty" toml:"relative,omitempty"`
	// LineWidth 指定线宽。
	LineWidth float64 `json:"line_width,omitzero" yaml:"line_width,omitempty" toml:"line_width,omitempty"`
	// Join 指定线段连接样式。
	Join string `json:"join,omitempty" yaml:"join,omitempty" toml:"join,omitempty"`
	// Cap 指定线段端点样式。
	Cap string `json:"cap,omitempty" yaml:"cap,omitempty" toml:"cap,omitempty"`
	// DashOffset 指定虚线起始偏移量。
	DashOffset float64 `json:"dash_offset,omitzero" yaml:"dash_offset,omitempty" toml:"dash_offset,omitempty"`
	// DashPattern 指定虚线长度模式。
	DashPattern []float64 `json:"dash_pattern,omitempty" yaml:"dash_pattern,omitempty" toml:"dash_pattern,omitempty"`
	// MiterLimit 指定斜接长度限制。
	MiterLimit float64 `json:"miter_limit,omitzero" yaml:"miter_limit,omitempty" toml:"miter_limit,omitempty"`
	// FillColor 指定填充颜色。
	FillColor *Color `json:"fill_color,omitzero" yaml:"fill_color,omitempty" toml:"fill_color,omitempty"`
	// StrokeColor 指定描边颜色。
	StrokeColor *Color `json:"stroke_color,omitzero" yaml:"stroke_color,omitempty" toml:"stroke_color,omitempty"`
}

// Composite 描述复合图形单元资源。
type Composite struct {
	// ID 标识复合图形单元。
	ID uint64 `json:"id,omitzero" yaml:"id,omitempty" toml:"id,omitempty"`
	// Width 指定复合图形宽度。
	Width float64 `json:"width,omitzero" yaml:"width,omitempty" toml:"width,omitempty"`
	// Height 指定复合图形高度。
	Height float64 `json:"height,omitzero" yaml:"height,omitempty" toml:"height,omitempty"`
	// Thumbnail 指定缩略图资源编号。
	Thumbnail uint64 `json:"thumbnail,omitzero" yaml:"thumbnail,omitempty" toml:"thumbnail,omitempty"`
	// Substitution 指定替代图形资源编号。
	Substitution uint64 `json:"substitution,omitzero" yaml:"substitution,omitempty" toml:"substitution,omitempty"`
	// Items 定义复合图形中的图元。
	Items []Item `json:"items,omitempty" yaml:"items,omitempty" toml:"items,omitempty"`
}

// Color 描述颜色及其渐变或图案填充信息。
type Color struct {
	// R 指定红色分量。
	R uint8 `json:"r,omitzero" yaml:"r,omitempty" toml:"r,omitempty"`
	// G 指定绿色分量。
	G uint8 `json:"g,omitzero" yaml:"g,omitempty" toml:"g,omitempty"`
	// B 指定蓝色分量。
	B uint8 `json:"b,omitzero" yaml:"b,omitempty" toml:"b,omitempty"`
	// Components 指定颜色空间分量值。
	Components []int `json:"components,omitempty" yaml:"components,omitempty" toml:"components,omitempty"`
	// ColorSpace 指定颜色空间资源编号。
	ColorSpace uint64 `json:"color_space,omitzero" yaml:"color_space,omitempty" toml:"color_space,omitempty"`
	// Index 指定调色板颜色索引。
	Index *int `json:"index,omitzero" yaml:"index,omitempty" toml:"index,omitempty"`
	// Alpha 指定颜色透明度。
	Alpha *uint8 `json:"alpha,omitzero" yaml:"alpha,omitempty" toml:"alpha,omitempty"`
	// Axial 定义轴向渐变。
	Axial *AxialShading `json:"axial,omitzero" yaml:"axial,omitempty" toml:"axial,omitempty"`
	// Radial 定义径向渐变。
	Radial *RadialShading `json:"radial,omitzero" yaml:"radial,omitempty" toml:"radial,omitempty"`
	// Gouraud 定义三角网格渐变。
	Gouraud *Gouraud `json:"gouraud,omitzero" yaml:"gouraud,omitempty" toml:"gouraud,omitempty"`
	// LaGouraud 定义四边形网格渐变。
	LaGouraud *LaGouraud `json:"la_gouraud,omitzero" yaml:"la_gouraud,omitempty" toml:"la_gouraud,omitempty"`
	// Pattern 定义图案填充。
	Pattern *Pattern `json:"pattern,omitzero" yaml:"pattern,omitempty" toml:"pattern,omitempty"`
}

// ColorStop 描述渐变中的颜色停止点。
type ColorStop struct {
	// Position 指定颜色在渐变中的位置。省略时不写出，由渲染按首尾补齐。
	Position *float64 `json:"position,omitzero" yaml:"position,omitempty" toml:"position,omitempty"`
	// Color 指定该位置的颜色。
	Color Color `json:"color" yaml:"color,omitempty" toml:"color,omitempty"`
}

// AxialShading 描述轴向渐变。
type AxialShading struct {
	// MapType 指定渐变映射类型。
	MapType string `json:"map_type,omitempty" yaml:"map_type,omitempty" toml:"map_type,omitempty"`
	// MapUnit 指定渐变映射单位。
	MapUnit float64 `json:"map_unit,omitzero" yaml:"map_unit,omitempty" toml:"map_unit,omitempty"`
	// Extend 指定渐变端点的延伸方式。
	Extend int `json:"extend,omitzero" yaml:"extend,omitempty" toml:"extend,omitempty"`
	// StartPoint 指定渐变起点。
	StartPoint string `json:"start_point,omitempty" yaml:"start_point,omitempty" toml:"start_point,omitempty"`
	// EndPoint 指定渐变终点。
	EndPoint string `json:"end_point,omitempty" yaml:"end_point,omitempty" toml:"end_point,omitempty"`
	// Segments 定义渐变颜色停止点。
	Segments []ColorStop `json:"segments,omitempty" yaml:"segments,omitempty" toml:"segments,omitempty"`
}

// RadialShading 描述径向渐变。
type RadialShading struct {
	// MapType 指定渐变映射类型。
	MapType string `json:"map_type,omitempty" yaml:"map_type,omitempty" toml:"map_type,omitempty"`
	// MapUnit 指定渐变映射单位。
	MapUnit float64 `json:"map_unit,omitzero" yaml:"map_unit,omitempty" toml:"map_unit,omitempty"`
	// Eccentricity 指定径向渐变椭圆离心率。
	Eccentricity float64 `json:"eccentricity,omitzero" yaml:"eccentricity,omitempty" toml:"eccentricity,omitempty"`
	// Angle 指定径向渐变旋转角度。
	Angle float64 `json:"angle,omitzero" yaml:"angle,omitempty" toml:"angle,omitempty"`
	// StartPoint 指定渐变起点圆心。
	StartPoint string `json:"start_point,omitempty" yaml:"start_point,omitempty" toml:"start_point,omitempty"`
	// StartRadius 指定渐变起始半径。
	StartRadius float64 `json:"start_radius,omitzero" yaml:"start_radius,omitempty" toml:"start_radius,omitempty"`
	// EndPoint 指定渐变终点圆心。
	EndPoint string `json:"end_point,omitempty" yaml:"end_point,omitempty" toml:"end_point,omitempty"`
	// EndRadius 指定渐变结束半径。
	EndRadius float64 `json:"end_radius,omitzero" yaml:"end_radius,omitempty" toml:"end_radius,omitempty"`
	// Extend 指定渐变端点的延伸方式。
	Extend int `json:"extend,omitzero" yaml:"extend,omitempty" toml:"extend,omitempty"`
	// Segments 定义渐变颜色停止点。
	Segments []ColorStop `json:"segments,omitempty" yaml:"segments,omitempty" toml:"segments,omitempty"`
}

// Gouraud 描述 Gouraud 三角网格渐变。
type Gouraud struct {
	// Extend 指定渐变边界的延伸方式。
	Extend int `json:"extend,omitzero" yaml:"extend,omitempty" toml:"extend,omitempty"`
	// Points 定义三角网格顶点。
	Points []GouraudPoint `json:"points,omitempty" yaml:"points,omitempty" toml:"points,omitempty"`
	// BackColor 指定网格背景颜色。
	BackColor *Color `json:"back_color,omitzero" yaml:"back_color,omitempty" toml:"back_color,omitempty"`
}

// GouraudPoint 描述 Gouraud 渐变网格中的顶点。
type GouraudPoint struct {
	// X 指定顶点横坐标。
	X float64 `json:"x,omitzero" yaml:"x,omitempty" toml:"x,omitempty"`
	// Y 指定顶点纵坐标。
	Y float64 `json:"y,omitzero" yaml:"y,omitempty" toml:"y,omitempty"`
	// EdgeFlag 指定顶点边标志。
	EdgeFlag int `json:"edge_flag,omitzero" yaml:"edge_flag,omitempty" toml:"edge_flag,omitempty"`
	// Color 指定顶点颜色。
	Color Color `json:"color,omitempty" yaml:"color,omitempty" toml:"color,omitempty"`
}

// LaGouraud 描述 LaGouraud 四边形网格渐变。
type LaGouraud struct {
	// VerticesPerRow 指定每行顶点数。
	VerticesPerRow int `json:"vertices_per_row,omitzero" yaml:"vertices_per_row,omitempty" toml:"vertices_per_row,omitempty"`
	// Extend 指定渐变边界的延伸方式。
	Extend int `json:"extend,omitzero" yaml:"extend,omitempty" toml:"extend,omitempty"`
	// Points 定义四边形网格顶点。
	Points []LaGouraudPoint `json:"points,omitempty" yaml:"points,omitempty" toml:"points,omitempty"`
	// BackColor 指定网格背景颜色。
	BackColor *Color `json:"back_color,omitzero" yaml:"back_color,omitempty" toml:"back_color,omitempty"`
}

// LaGouraudPoint 描述 LaGouraud 渐变网格中的顶点。
type LaGouraudPoint struct {
	// X 指定顶点横坐标。
	X float64 `json:"x,omitzero" yaml:"x,omitempty" toml:"x,omitempty"`
	// Y 指定顶点纵坐标。
	Y float64 `json:"y,omitzero" yaml:"y,omitempty" toml:"y,omitempty"`
	// Color 指定顶点颜色。
	Color Color `json:"color,omitempty" yaml:"color,omitempty" toml:"color,omitempty"`
}

// Pattern 描述图案填充及其内容。
type Pattern struct {
	// Width 指定图案单元宽度。
	Width float64 `json:"width,omitzero" yaml:"width,omitempty" toml:"width,omitempty"`
	// Height 指定图案单元高度。
	Height float64 `json:"height,omitzero" yaml:"height,omitempty" toml:"height,omitempty"`
	// XStep 指定图案在横向的重复步长。
	XStep float64 `json:"x_step,omitzero" yaml:"x_step,omitempty" toml:"x_step,omitempty"`
	// YStep 指定图案在纵向的重复步长。
	YStep float64 `json:"y_step,omitzero" yaml:"y_step,omitempty" toml:"y_step,omitempty"`
	// ReflectMethod 指定图案反射方式。
	ReflectMethod string `json:"reflect_method,omitempty" yaml:"reflect_method,omitempty" toml:"reflect_method,omitempty"`
	// RelativeTo 指定图案定位所依据的对象。
	RelativeTo string `json:"relative_to,omitempty" yaml:"relative_to,omitempty" toml:"relative_to,omitempty"`
	// CTM 指定图案的坐标变换矩阵。
	CTM []float64 `json:"ctm,omitempty" yaml:"ctm,omitempty" toml:"ctm,omitempty"`
	// Thumbnail 指定图案缩略图资源编号。
	Thumbnail uint64 `json:"thumbnail,omitzero" yaml:"thumbnail,omitempty" toml:"thumbnail,omitempty"`
	// Items 定义图案中的图元。
	Items []Item `json:"items,omitempty" yaml:"items,omitempty" toml:"items,omitempty"`
	// Layers 定义图案中的图层。
	Layers []Layer `json:"layers,omitempty" yaml:"layers,omitempty" toml:"layers,omitempty"`
}

// TextCode 描述文本内容及其定位信息。
type TextCode struct {
	// Value 指定文本内容。
	Value string `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
	// X 指定文本代码横坐标。
	X *float64 `json:"x,omitzero" yaml:"x,omitempty" toml:"x,omitempty"`
	// Y 指定文本代码纵坐标。
	Y *float64 `json:"y,omitzero" yaml:"y,omitempty" toml:"y,omitempty"`
	// DeltaX 指定连续字符的横向偏移量。
	DeltaX []float64 `json:"delta_x,omitempty" yaml:"delta_x,omitempty" toml:"delta_x,omitempty"`
	// DeltaY 指定连续字符的纵向偏移量。
	DeltaY []float64 `json:"delta_y,omitempty" yaml:"delta_y,omitempty" toml:"delta_y,omitempty"`
}

// Action 描述文档、页面或图元触发的动作。
type Action struct {
	// Event 指定触发动作的事件。
	Event string `json:"event,omitempty" yaml:"event,omitempty" toml:"event,omitempty"`
	// Region 定义动作触发区域。
	Region *ActionRegion `json:"region,omitzero" yaml:"region,omitempty" toml:"region,omitempty"`
	// URI 定义打开或跳转到 URI 的动作。
	URI *URIAction `json:"uri,omitzero" yaml:"uri,omitempty" toml:"uri,omitempty"`
	// Goto 定义跳转到页面或位置的动作。
	Goto *GotoAction `json:"goto,omitzero" yaml:"goto,omitempty" toml:"goto,omitempty"`
	// GotoA 定义跳转到附件的动作。
	GotoA *GotoAAction `json:"goto_a,omitzero" yaml:"goto_a,omitempty" toml:"goto_a,omitempty"`
	// Sound 定义播放声音资源的动作。
	Sound *SoundAction `json:"sound,omitzero" yaml:"sound,omitempty" toml:"sound,omitempty"`
	// Movie 定义播放媒体资源的动作。
	Movie *MovieAction `json:"movie,omitzero" yaml:"movie,omitempty" toml:"movie,omitempty"`
}

// ActionRegion 描述动作的触发区域。
type ActionRegion struct {
	// Areas 定义动作区域中的路径。
	Areas []ActionArea `json:"areas,omitempty" yaml:"areas,omitempty" toml:"areas,omitempty"`
}

// ActionArea 描述触发区域中的一条路径。
type ActionArea struct {
	// Start 指定路径起点。
	Start Point `json:"start,omitempty" yaml:"start,omitempty" toml:"start,omitempty"`
	// Commands 定义路径绘制命令。
	Commands []RegionCommand `json:"commands,omitempty" yaml:"commands,omitempty" toml:"commands,omitempty"`
}

// Point 描述二维坐标点。
type Point struct {
	// X 指定横坐标。
	X float64 `json:"x,omitzero" yaml:"x,omitempty" toml:"x,omitempty"`
	// Y 指定纵坐标。
	Y float64 `json:"y,omitzero" yaml:"y,omitempty" toml:"y,omitempty"`
}

// RegionCommand 描述动作区域路径中的绘制命令。
type RegionCommand struct {
	// Type 指定路径命令类型。
	Type string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	// X 指定命令终点横坐标。
	X float64 `json:"x,omitzero" yaml:"x,omitempty" toml:"x,omitempty"`
	// Y 指定命令终点纵坐标。
	Y float64 `json:"y,omitzero" yaml:"y,omitempty" toml:"y,omitempty"`
	// ControlX 指定二次贝塞尔曲线控制点横坐标。
	ControlX float64 `json:"control_x,omitzero" yaml:"control_x,omitempty" toml:"control_x,omitempty"`
	// ControlY 指定二次贝塞尔曲线控制点纵坐标。
	ControlY float64 `json:"control_y,omitzero" yaml:"control_y,omitempty" toml:"control_y,omitempty"`
	// Control1X 指定三次贝塞尔曲线第一个控制点横坐标。
	Control1X float64 `json:"control_1_x,omitzero" yaml:"control_1_x,omitempty" toml:"control_1_x,omitempty"`
	// Control1Y 指定三次贝塞尔曲线第一个控制点纵坐标。
	Control1Y float64 `json:"control_1_y,omitzero" yaml:"control_1_y,omitempty" toml:"control_1_y,omitempty"`
	// Control2X 指定三次贝塞尔曲线第二个控制点横坐标。
	Control2X float64 `json:"control_2_x,omitzero" yaml:"control_2_x,omitempty" toml:"control_2_x,omitempty"`
	// Control2Y 指定三次贝塞尔曲线第二个控制点纵坐标。
	Control2Y float64 `json:"control_2_y,omitzero" yaml:"control_2_y,omitempty" toml:"control_2_y,omitempty"`
	// SweepDirection 指定椭圆弧的扫描方向。
	SweepDirection bool `json:"sweep_direction,omitzero" yaml:"sweep_direction,omitempty" toml:"sweep_direction,omitempty"`
	// LargeArc 指示椭圆弧是否取大弧段。
	LargeArc bool `json:"large_arc,omitzero" yaml:"large_arc,omitempty" toml:"large_arc,omitempty"`
	// RotationAngle 指定椭圆弧旋转角度。
	RotationAngle float64 `json:"rotation_angle,omitzero" yaml:"rotation_angle,omitempty" toml:"rotation_angle,omitempty"`
	// EllipseWidth 指定椭圆弧椭圆宽度。
	EllipseWidth float64 `json:"ellipse_width,omitzero" yaml:"ellipse_width,omitempty" toml:"ellipse_width,omitempty"`
	// EllipseHeight 指定椭圆弧椭圆高度。
	EllipseHeight float64 `json:"ellipse_height,omitzero" yaml:"ellipse_height,omitempty" toml:"ellipse_height,omitempty"`
}

// URIAction 描述打开或跳转到 URI 的动作。
type URIAction struct {
	// URI 指定目标 URI。
	URI string `json:"uri,omitempty" yaml:"uri,omitempty" toml:"uri,omitempty"`
	// Base 指定 URI 基础地址。
	Base string `json:"base,omitempty" yaml:"base,omitempty" toml:"base,omitempty"`
	// Target 指定 URI 打开目标。
	Target string `json:"target,omitempty" yaml:"target,omitempty" toml:"target,omitempty"`
}

// GotoAction 描述跳转到页面、书签或页面位置的动作。
type GotoAction struct {
	// Page 指定跳转目标页码。
	Page int `json:"page,omitzero" yaml:"page,omitempty" toml:"page,omitempty"`
	// Type 指定跳转目标类型。
	Type string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	// Bookmark 指定跳转目标书签名称。
	Bookmark string `json:"bookmark,omitempty" yaml:"bookmark,omitempty" toml:"bookmark,omitempty"`
	// Left 指定目标视图左边界。
	Left *float64 `json:"left,omitzero" yaml:"left,omitempty" toml:"left,omitempty"`
	// Top 指定目标视图上边界。
	Top *float64 `json:"top,omitzero" yaml:"top,omitempty" toml:"top,omitempty"`
	// Right 指定目标视图右边界。
	Right *float64 `json:"right,omitzero" yaml:"right,omitempty" toml:"right,omitempty"`
	// Bottom 指定目标视图下边界。
	Bottom *float64 `json:"bottom,omitzero" yaml:"bottom,omitempty" toml:"bottom,omitempty"`
	// Zoom 指定目标视图缩放比例。
	Zoom *float64 `json:"zoom,omitzero" yaml:"zoom,omitempty" toml:"zoom,omitempty"`
}

// GotoAAction 描述跳转到附件的动作。
type GotoAAction struct {
	// AttachID 指定跳转目标附件标识。
	AttachID string `json:"attach_id,omitempty" yaml:"attach_id,omitempty" toml:"attach_id,omitempty"`
	// NewWindow 指示是否在新窗口打开附件。
	NewWindow *bool `json:"new_window,omitzero" yaml:"new_window,omitempty" toml:"new_window,omitempty"`
}

// SoundAction 描述播放声音资源的动作。
type SoundAction struct {
	// ResourceID 指定声音资源编号。
	ResourceID uint64 `json:"resource_id,omitzero" yaml:"resource_id,omitempty" toml:"resource_id,omitempty"`
	// Volume 指定播放音量。
	Volume *int `json:"volume,omitzero" yaml:"volume,omitempty" toml:"volume,omitempty"`
	// Repeat 指示是否循环播放。
	Repeat *bool `json:"repeat,omitzero" yaml:"repeat,omitempty" toml:"repeat,omitempty"`
	// Synchronous 指示是否同步播放。
	Synchronous *bool `json:"synchronous,omitzero" yaml:"synchronous,omitempty" toml:"synchronous,omitempty"`
}

// MovieAction 描述播放媒体资源的动作。
type MovieAction struct {
	// ResourceID 指定媒体资源编号。
	ResourceID uint64 `json:"resource_id,omitzero" yaml:"resource_id,omitempty" toml:"resource_id,omitempty"`
	// Operator 指定媒体操作类型。
	Operator string `json:"operator,omitempty" yaml:"operator,omitempty" toml:"operator,omitempty"`
}

// Bookmark 描述文档书签及其跳转目标。
type Bookmark struct {
	// Name 指定书签名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// Goto 指定书签跳转目标。
	Goto GotoAction `json:"goto,omitempty" yaml:"goto,omitempty" toml:"goto,omitempty"`
}

// Attachment 描述文档附件。
type Attachment struct {
	// ID 标识附件。
	ID string `json:"id,omitempty" yaml:"id,omitempty" toml:"id,omitempty"`
	// Name 指定附件名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// Format 指定附件格式。
	Format string `json:"format,omitempty" yaml:"format,omitempty" toml:"format,omitempty"`
	// Usage 说明附件用途。
	Usage string `json:"usage,omitempty" yaml:"usage,omitempty" toml:"usage,omitempty"`
	// File 指定附件文件路径。
	File string `json:"file,omitempty" yaml:"file,omitempty" toml:"file,omitempty"`
	// DataBase64 提供 Base64 编码的附件数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
	// FileName 指定附件在文档中的文件名。
	FileName string `json:"file_name,omitempty" yaml:"file_name,omitempty" toml:"file_name,omitempty"`
	// CreationDate 记录附件创建时间。
	CreationDate string `json:"creation_date,omitempty" yaml:"creation_date,omitempty" toml:"creation_date,omitempty"`
	// ModDate 记录附件最后修改时间。
	ModDate string `json:"mod_date,omitempty" yaml:"mod_date,omitempty" toml:"mod_date,omitempty"`
	// Visible 指示附件是否可见。
	Visible *bool `json:"visible,omitzero" yaml:"visible,omitempty" toml:"visible,omitempty"`
}

// Extension 描述文档扩展信息。
type Extension struct {
	// AppName 指定扩展信息来源应用名称。
	AppName string `json:"app_name,omitempty" yaml:"app_name,omitempty" toml:"app_name,omitempty"`
	// Company 指定扩展信息来源公司。
	Company string `json:"company,omitempty" yaml:"company,omitempty" toml:"company,omitempty"`
	// AppVersion 指定来源应用版本。
	AppVersion string `json:"app_version,omitempty" yaml:"app_version,omitempty" toml:"app_version,omitempty"`
	// Date 记录扩展信息生成时间。
	Date string `json:"date,omitempty" yaml:"date,omitempty" toml:"date,omitempty"`
	// RefID 指定扩展信息引用标识。
	RefID uint64 `json:"ref_id,omitzero" yaml:"ref_id,omitempty" toml:"ref_id,omitempty"`
	// Data 指定扩展数据。
	Data string `json:"data,omitempty" yaml:"data,omitempty" toml:"data,omitempty"`
	// DataXML 指定扩展 XML 数据。
	DataXML string `json:"data_xml,omitempty" yaml:"data_xml,omitempty" toml:"data_xml,omitempty"`
	// DataFile 指定扩展数据文件路径。
	DataFile string `json:"data_file,omitempty" yaml:"data_file,omitempty" toml:"data_file,omitempty"`
	// DataFileBase64 提供 Base64 编码的扩展文件数据。
	DataFileBase64 string `json:"data_file_base64,omitempty" yaml:"data_file_base64,omitempty" toml:"data_file_base64,omitempty"`
	// DataName 指定扩展数据文件名称。
	DataName string `json:"data_name,omitempty" yaml:"data_name,omitempty" toml:"data_name,omitempty"`
	// Properties 定义扩展属性。
	Properties []ExtensionProperty `json:"properties,omitempty" yaml:"properties,omitempty" toml:"properties,omitempty"`
}

// ExtensionProperty 描述扩展信息的属性。
type ExtensionProperty struct {
	// Name 指定属性名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// Type 指定属性类型。
	Type string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	// Value 指定属性值。
	Value string `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
}

// Version 描述文档的一个版本。
type Version struct {
	// ID 标识文档版本。
	ID string `json:"id,omitempty" yaml:"id,omitempty" toml:"id,omitempty"`
	// Index 指定版本序号。
	Index int `json:"index,omitzero" yaml:"index,omitempty" toml:"index,omitempty"`
	// Current 指示是否为当前版本。
	Current bool `json:"current,omitzero" yaml:"current,omitempty" toml:"current,omitempty"`
	// Version 指定版本号。
	Version string `json:"version" yaml:"version" toml:"version"`
	// Name 指定版本名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// CreationDate 记录版本创建时间。
	CreationDate string `json:"creation_date,omitempty" yaml:"creation_date,omitempty" toml:"creation_date,omitempty"`
	// DocRoot 指定版本文档根文件路径。
	DocRoot string `json:"doc_root,omitempty" yaml:"doc_root,omitempty" toml:"doc_root,omitempty"`
	// DocRootBase64 提供 Base64 编码的版本文档根数据。
	DocRootBase64 string `json:"doc_root_base64,omitempty" yaml:"doc_root_base64,omitempty" toml:"doc_root_base64,omitempty"`
	// DocRootName 指定版本文档根文件名称。
	DocRootName string `json:"doc_root_name,omitempty" yaml:"doc_root_name,omitempty" toml:"doc_root_name,omitempty"`
	// Files 定义版本包含的文件。
	Files []VersionFile `json:"files,omitempty" yaml:"files,omitempty" toml:"files,omitempty"`
}

// VersionFile 描述文档版本中的文件。
type VersionFile struct {
	// ID 标识版本文件。
	ID string `json:"id,omitempty" yaml:"id,omitempty" toml:"id,omitempty"`
	// Path 指定文件在版本中的路径。
	Path string `json:"path,omitempty" yaml:"path,omitempty" toml:"path,omitempty"`
}

// Outline 描述文档大纲节点及其子节点。
type Outline struct {
	// Title 指定大纲节点标题。
	Title string `json:"title,omitempty" yaml:"title,omitempty" toml:"title,omitempty"`
	// Count 指定大纲节点包含的条目数。
	Count *int `json:"count,omitzero" yaml:"count,omitempty" toml:"count,omitempty"`
	// Expanded 指示大纲节点是否展开。
	Expanded *bool `json:"expanded,omitzero" yaml:"expanded,omitempty" toml:"expanded,omitempty"`
	// Actions 定义大纲节点动作。
	Actions []Action `json:"actions,omitempty" yaml:"actions,omitempty" toml:"actions,omitempty"`
	// Children 定义大纲子节点。
	Children []Outline `json:"children,omitempty" yaml:"children,omitempty" toml:"children,omitempty"`
}

// Signature 描述文档数字签名。
type Signature struct {
	// ID 标识数字签名。
	ID string `json:"id,omitempty" yaml:"id,omitempty" toml:"id,omitempty"`
	// Type 指定签名类型。
	Type string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	// ProviderName 指定签名提供方名称。
	ProviderName string `json:"provider_name,omitempty" yaml:"provider_name,omitempty" toml:"provider_name,omitempty"`
	// ProviderVersion 指定签名提供方版本。
	ProviderVersion string `json:"provider_version,omitempty" yaml:"provider_version,omitempty" toml:"provider_version,omitempty"`
	// Company 指定签名提供方公司。
	Company string `json:"company,omitempty" yaml:"company,omitempty" toml:"company,omitempty"`
	// Method 指定签名方法。
	Method string `json:"method,omitempty" yaml:"method,omitempty" toml:"method,omitempty"`
	// Date 记录签名时间。
	Date string `json:"date,omitempty" yaml:"date,omitempty" toml:"date,omitempty"`
	// CheckMethod 指定签名校验方法。
	CheckMethod string `json:"check_method,omitempty" yaml:"check_method,omitempty" toml:"check_method,omitempty"`
	// References 定义签名引用及其校验值。
	References []SignatureReference `json:"references,omitempty" yaml:"references,omitempty" toml:"references,omitempty"`
	// StampAnnots 定义签名盖章区域。
	StampAnnots []SignatureStamp `json:"stamp_annots,omitempty" yaml:"stamp_annots,omitempty" toml:"stamp_annots,omitempty"`
	// SealFile 指定印章文件路径。
	SealFile string `json:"seal_file,omitempty" yaml:"seal_file,omitempty" toml:"seal_file,omitempty"`
	// SealFileBase64 提供 Base64 编码的印章数据。
	SealFileBase64 string `json:"seal_file_base64,omitempty" yaml:"seal_file_base64,omitempty" toml:"seal_file_base64,omitempty"`
	// SealName 指定印章文件名称。
	SealName string `json:"seal_name,omitempty" yaml:"seal_name,omitempty" toml:"seal_name,omitempty"`
	// SignedValue 提供 Base64 编码的签名值。
	SignedValue string `json:"signed_value,omitempty" yaml:"signed_value,omitempty" toml:"signed_value,omitempty"`
	// SignedValueName 指定签名值文件名称。
	SignedValueName string `json:"signed_value_name,omitempty" yaml:"signed_value_name,omitempty" toml:"signed_value_name,omitempty"`
}

// SignatureReference 描述数字签名引用的文件及校验值。
type SignatureReference struct {
	// FileRef 指定签名引用的文件路径。
	FileRef string `json:"file_ref,omitempty" yaml:"file_ref,omitempty" toml:"file_ref,omitempty"`
	// CheckValue 提供 Base64 编码的文件校验值。
	CheckValue string `json:"check_value,omitempty" yaml:"check_value,omitempty" toml:"check_value,omitempty"`
}

// SignatureStamp 描述数字签名在页面上的盖章区域。
type SignatureStamp struct {
	// ID 标识签名盖章区域。
	ID string `json:"id,omitempty" yaml:"id,omitempty" toml:"id,omitempty"`
	// Page 指定盖章所在页码。
	Page int `json:"page,omitzero" yaml:"page,omitempty" toml:"page,omitempty"`
	// Boundary 指定盖章区域边界。
	Boundary Box `json:"boundary,omitempty" yaml:"boundary,omitempty" toml:"boundary,omitempty"`
	// Clip 指定盖章区域裁剪框。
	Clip *Box `json:"clip,omitzero" yaml:"clip,omitempty" toml:"clip,omitempty"`
}

// Box 描述矩形区域。
type Box struct {
	// X 指定矩形左上角横坐标。
	X float64 `json:"x,omitzero" yaml:"x,omitempty" toml:"x,omitempty"`
	// Y 指定矩形左上角纵坐标。
	Y float64 `json:"y,omitzero" yaml:"y,omitempty" toml:"y,omitempty"`
	// Width 指定矩形宽度。
	Width float64 `json:"width,omitzero" yaml:"width,omitempty" toml:"width,omitempty"`
	// Height 指定矩形高度。
	Height float64 `json:"height,omitzero" yaml:"height,omitempty" toml:"height,omitempty"`
}

// AnnotationPage 描述某一页面上的批注集合。
type AnnotationPage struct {
	// Page 指定批注所属页码。
	Page int `json:"page,omitzero" yaml:"page,omitempty" toml:"page,omitempty"`
	// Items 定义该页面上的批注。
	Items []Annotation `json:"items,omitempty" yaml:"items,omitempty" toml:"items,omitempty"`
}

// Annotation 描述文档批注。
type Annotation struct {
	// ID 标识批注。
	ID uint64 `json:"id,omitzero" yaml:"id,omitempty" toml:"id,omitempty"`
	// Type 指定批注类型。
	Type string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	// Creator 指定批注创建者。
	Creator string `json:"creator,omitempty" yaml:"creator,omitempty" toml:"creator,omitempty"`
	// LastModDate 记录批注最后修改时间。
	LastModDate string `json:"last_mod_date,omitempty" yaml:"last_mod_date,omitempty" toml:"last_mod_date,omitempty"`
	// Visible 指示批注是否可见。
	Visible *bool `json:"visible,omitzero" yaml:"visible,omitempty" toml:"visible,omitempty"`
	// Subtype 指定批注子类型。
	Subtype string `json:"subtype,omitempty" yaml:"subtype,omitempty" toml:"subtype,omitempty"`
	// Print 指示批注是否随文档打印。
	Print *bool `json:"print,omitzero" yaml:"print,omitempty" toml:"print,omitempty"`
	// NoZoom 指示批注是否不随页面缩放。
	NoZoom bool `json:"no_zoom,omitzero" yaml:"no_zoom,omitempty" toml:"no_zoom,omitempty"`
	// NoRotate 指示批注是否不随页面旋转。
	NoRotate bool `json:"no_rotate,omitzero" yaml:"no_rotate,omitempty" toml:"no_rotate,omitempty"`
	// ReadOnly 指示批注是否只读。
	ReadOnly *bool `json:"read_only,omitzero" yaml:"read_only,omitempty" toml:"read_only,omitempty"`
	// Remark 记录批注说明。
	Remark string `json:"remark,omitempty" yaml:"remark,omitempty" toml:"remark,omitempty"`
	// Parameters 定义批注自定义参数。
	Parameters []AnnotationParameter `json:"parameters,omitempty" yaml:"parameters,omitempty" toml:"parameters,omitempty"`
	// Boundary 指定批注边界。
	Boundary *Box `json:"boundary,omitzero" yaml:"boundary,omitempty" toml:"boundary,omitempty"`
	// Items 定义批注包含的图元。
	Items []Item `json:"items,omitempty" yaml:"items,omitempty" toml:"items,omitempty"`
}

// AnnotationParameter 描述批注的自定义参数。
type AnnotationParameter struct {
	// Name 指定批注参数名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// Value 指定批注参数值。
	Value string `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
}

// CGTransform 描述文本代码与字形之间的映射关系。
type CGTransform struct {
	// CodePosition 指定文本代码起始位置。
	CodePosition int `json:"code_position,omitzero" yaml:"code_position,omitempty" toml:"code_position,omitempty"`
	// CodeCount 指定映射的文本代码数量。
	CodeCount int `json:"code_count,omitzero" yaml:"code_count,omitempty" toml:"code_count,omitempty"`
	// GlyphCount 指定对应的字形数量。
	GlyphCount int `json:"glyph_count,omitzero" yaml:"glyph_count,omitempty" toml:"glyph_count,omitempty"`
	// Glyphs 指定对应字形编号列表。
	Glyphs []int `json:"glyphs,omitempty" yaml:"glyphs,omitempty" toml:"glyphs,omitempty"`
}

// Item 描述页面、模板或复合图形中的图元。
type Item struct {
	// Bind 定义 text/image 图元的数据绑定，仅在模板渲染时展开。
	Bind *Binding `json:"bind,omitzero" yaml:"bind,omitempty"`
	// Type 指定图元类型。
	Type string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty"`
	// X 指定图元左上角横坐标。
	X float64 `json:"x,omitzero" yaml:"x,omitempty" toml:"x,omitempty"`
	// Y 指定图元左上角纵坐标。
	Y float64 `json:"y,omitzero" yaml:"y,omitempty" toml:"y,omitempty"`
	// Width 指定图元宽度。
	Width float64 `json:"width,omitzero" yaml:"width,omitempty" toml:"width,omitempty"`
	// Height 指定图元高度。
	Height float64 `json:"height,omitzero" yaml:"height,omitempty" toml:"height,omitempty"`
	// Value 指定图元文本或其他值。
	Value string `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
	// Font 指定文本图元使用的字体名称。
	Font string `json:"font,omitempty" yaml:"font,omitempty" toml:"font,omitempty"`
	// Size 指定文本字号。
	Size float64 `json:"size,omitzero" yaml:"size,omitempty" toml:"size,omitempty"`
	// Data 指定图元数据或文件路径。
	Data string `json:"data,omitempty" yaml:"data,omitempty" toml:"data,omitempty"`
	// DataBase64 提供 Base64 编码的图元数据。
	DataBase64 string `json:"data_base64,omitempty" yaml:"data_base64,omitempty" toml:"data_base64,omitempty"`
	// Format 指定图元数据格式。
	Format string `json:"format,omitempty" yaml:"format,omitempty" toml:"format,omitempty"`
	// Name 指定图元名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// Visible 指示图元是否可见。
	Visible *bool `json:"visible,omitzero" yaml:"visible,omitempty" toml:"visible,omitempty"`
	// ResourceID 指定图元引用的资源编号。
	ResourceID uint64 `json:"resource_id,omitzero" yaml:"resource_id,omitempty" toml:"resource_id,omitempty"`
	// Stroke 指示是否绘制轮廓线。
	Stroke bool `json:"stroke,omitzero" yaml:"stroke,omitempty" toml:"stroke,omitempty"`
	// Fill 指示是否填充图元。
	Fill *bool `json:"fill,omitzero" yaml:"fill,omitempty" toml:"fill,omitempty"`
	// StrokeSet 指示是否显式设置描边属性。
	StrokeSet *bool `json:"stroke_set,omitzero" yaml:"stroke_set,omitempty" toml:"stroke_set,omitempty"`
	// Rule 指定填充规则。
	Rule string `json:"rule,omitempty" yaml:"rule,omitempty" toml:"rule,omitempty"`
	// LineWidth 指定描边线宽。
	LineWidth float64 `json:"line_width,omitzero" yaml:"line_width,omitempty" toml:"line_width,omitempty"`
	// Cap 指定描边端点样式。
	Cap string `json:"cap,omitempty" yaml:"cap,omitempty" toml:"cap,omitempty"`
	// Join 指定描边连接样式。
	Join string `json:"join,omitempty" yaml:"join,omitempty" toml:"join,omitempty"`
	// MiterLimit 指定斜接长度限制。
	MiterLimit float64 `json:"miter_limit,omitzero" yaml:"miter_limit,omitempty" toml:"miter_limit,omitempty"`
	// DashOffset 指定虚线起始偏移量。
	DashOffset float64 `json:"dash_offset,omitzero" yaml:"dash_offset,omitempty" toml:"dash_offset,omitempty"`
	// DashPattern 指定虚线长度模式。
	DashPattern []float64 `json:"dash_pattern,omitempty" yaml:"dash_pattern,omitempty" toml:"dash_pattern,omitempty"`
	// Alpha 指定图元透明度。
	Alpha *uint8 `json:"alpha,omitzero" yaml:"alpha,omitempty" toml:"alpha,omitempty"`
	// DrawParam 指定图元使用的绘制参数名称。
	DrawParam string `json:"draw_param,omitempty" yaml:"draw_param,omitempty" toml:"draw_param,omitempty"`
	// CTM 指定图元的坐标变换矩阵。
	CTM []float64 `json:"ctm,omitempty" yaml:"ctm,omitempty" toml:"ctm,omitempty"`
	// FillColor 指定填充颜色。
	FillColor *Color `json:"fill_color,omitzero" yaml:"fill_color,omitempty" toml:"fill_color,omitempty"`
	// StrokeColor 指定描边颜色。
	StrokeColor *Color `json:"stroke_color,omitzero" yaml:"stroke_color,omitempty" toml:"stroke_color,omitempty"`
	// TextCodes 定义文本代码及其定位信息。
	TextCodes []TextCode `json:"text_codes,omitempty" yaml:"text_codes,omitempty" toml:"text_codes,omitempty"`
	// CGTransforms 定义文本代码到字形的映射。
	CGTransforms []CGTransform `json:"cg_transforms,omitempty" yaml:"cg_transforms,omitempty" toml:"cg_transforms,omitempty"`
	// Actions 定义图元触发的动作。
	Actions []Action `json:"actions,omitempty" yaml:"actions,omitempty" toml:"actions,omitempty"`
	// Items 定义复合或页面块图元的子图元。
	Items []Item `json:"items,omitempty" yaml:"items,omitempty" toml:"items,omitempty"`
	// HScale 指定文本水平方向缩放比例。
	HScale float64 `json:"h_scale,omitzero" yaml:"h_scale,omitempty" toml:"h_scale,omitempty"`
	// ReadDirection 指定文本阅读方向。
	ReadDirection int `json:"read_direction,omitzero" yaml:"read_direction,omitempty" toml:"read_direction,omitempty"`
	// CharDirection 指定字符排列方向。
	CharDirection int `json:"char_direction,omitzero" yaml:"char_direction,omitempty" toml:"char_direction,omitempty"`
	// Weight 指定文本字重。
	Weight int `json:"weight,omitzero" yaml:"weight,omitempty" toml:"weight,omitempty"`
	// Italic 指示文本是否为斜体。
	Italic bool `json:"italic,omitzero" yaml:"italic,omitempty" toml:"italic,omitempty"`
	// Clips 定义图元裁剪区域。
	Clips []Clip `json:"clips,omitempty" yaml:"clips,omitempty" toml:"clips,omitempty"`
	// Substitution 指定图像替代资源编号。
	Substitution uint64 `json:"substitution,omitzero" yaml:"substitution,omitempty" toml:"substitution,omitempty"`
	// ImageMask 指定图像掩码资源编号。
	ImageMask uint64 `json:"image_mask,omitzero" yaml:"image_mask,omitempty" toml:"image_mask,omitempty"`
	// Border 定义图像边框样式。
	Border *ImageBorder `json:"border,omitzero" yaml:"border,omitempty" toml:"border,omitempty"`
	// Rows 定义表格行（仅当 type 为 "table" 时有效）。
	Rows []TableRow `json:"rows,omitempty" yaml:"rows,omitempty" toml:"rows,omitempty"`
	// ColumnWidths 定义各列宽度（仅当 type 为 "table" 时有效）。
	ColumnWidths []float64 `json:"column_widths,omitempty" yaml:"column_widths,omitempty" toml:"column_widths,omitempty"`
	// HeaderRows 定义表头行数（仅当 type 为 "table" 时有效）。
	HeaderRows int `json:"header_rows,omitzero" yaml:"header_rows,omitempty" toml:"header_rows,omitempty"`
	// Style 定义表格样式（仅当 type 为 "table" 时有效）。
	Style *TableStyle `json:"style,omitzero" yaml:"style,omitempty" toml:"style,omitempty"`
	// Caption 定义表题文字（仅当 type 为 "table" 时有效）。
	Caption string `json:"caption,omitempty" yaml:"caption,omitempty" toml:"caption,omitempty"`
	// CaptionStyle 定义表题样式（仅当 type 为 "table" 时有效）。
	CaptionStyle *CaptionStyle `json:"caption_style,omitzero" yaml:"caption_style,omitempty" toml:"caption_style,omitempty"`
	// ContinuationCaption 定义续表题文字（仅当 type 为 "table" 时有效）。
	ContinuationCaption string `json:"continuation_caption,omitempty" yaml:"continuation_caption,omitempty" toml:"continuation_caption,omitempty"`
	// ContinuationCaptionStyle 定义续表题样式（仅当 type 为 "table" 时有效）。
	ContinuationCaptionStyle *CaptionStyle `json:"continuation_caption_style,omitzero" yaml:"continuation_caption_style,omitempty" toml:"continuation_caption_style,omitempty"`
	// Align 指定图片与说明文字共用的水平对齐（仅当 type 为 "figure" 时有效）。
	Align string `json:"align,omitempty" yaml:"align,omitempty" toml:"align,omitempty"`
	// ImageWidth 定义图片宽度（仅当 type 为 "figure" 时有效）。
	ImageWidth float64 `json:"image_width,omitzero" yaml:"image_width,omitempty" toml:"image_width,omitempty"`
	// ImageFillWidth 指示 image_width 是否按图块宽度的比例解释（仅当 type 为 "figure" 时有效）。
	ImageFillWidth bool `json:"image_fill_width,omitzero" yaml:"image_fill_width,omitempty" toml:"image_fill_width,omitempty"`
	// ImageHeight 定义图片高度（仅当 type 为 "figure" 时有效）。
	ImageHeight float64 `json:"image_height,omitzero" yaml:"image_height,omitempty" toml:"image_height,omitempty"`
	// Fit 指定图片在给定尺寸内的适配方式：contain 保持比例，stretch 拉伸（仅当 type 为 "figure" 时有效）。
	Fit string `json:"fit,omitempty" yaml:"fit,omitempty" toml:"fit,omitempty"`
}

// Binding 定义模板字段；长度和字号均为毫米，数据路径使用点分隔对象键。
type Binding struct {
	// Path 从当前记录读取字段；$. 前缀读取根数据；内置序号 @index/@number、页码 @page/@pages、单据页码 @detail_page/@detail_pages。
	Path string `json:"path" yaml:"path"`
	// Aggregate 是 page_sum 或 record_sum，对当前页或整张单据的明细字段精确求和，仅用于 paginate 页面。
	Aggregate string `json:"aggregate,omitempty" yaml:"aggregate,omitempty"`
	// DecimalPlaces 是求和结果的小数位数（0—18），默认 2，精确求和后四舍五入。
	DecimalPlaces *int `json:"decimal_places,omitzero" yaml:"decimal_places,omitempty"`
	// Required 默认 true；false 时缺失或 null 字段移除该图元，空字符串也移除图元。
	Required *bool `json:"required,omitzero" yaml:"required,omitempty"`
	// Default 是缺失或 null 字段的默认字符串，优先于 required。
	Default *string `json:"default,omitzero" yaml:"default,omitempty"`
	// Overflow 是 error（默认）、wrap、shrink 或 ellipsis，仅用于文字。
	Overflow string `json:"overflow,omitempty" yaml:"overflow,omitempty"`
	// Align 是 left（默认）、center 或 right，仅用于文字。
	Align string `json:"align,omitempty" yaml:"align,omitempty"`
	// MinSize 是 shrink 的最小字号，单位毫米；shrink 时必须显式指定。
	MinSize float64 `json:"min_size,omitzero" yaml:"min_size,omitempty"`
	// MaxLines 是 wrap 的最大行数，0 表示仅按区域高度限制。
	MaxLines int `json:"max_lines,omitzero" yaml:"max_lines,omitempty"`
	// LineHeight 是 wrap 的行高倍率，默认 1.2。
	LineHeight float64 `json:"line_height,omitzero" yaml:"line_height,omitempty"`
}

// TableRow 定义表格行。
type TableRow struct {
	// Cells 定义该行包含的单元格。
	Cells []TableCell `json:"cells,omitempty" yaml:"cells,omitempty" toml:"cells,omitempty"`
}

// TableCell 定义表格单元格。
type TableCell struct {
	// Value 定义单元格文本内容。
	Value string `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
	// Font 定义字体。
	Font string `json:"font,omitempty" yaml:"font,omitempty" toml:"font,omitempty"`
	// Size 定义字号。
	Size float64 `json:"size,omitzero" yaml:"size,omitempty" toml:"size,omitempty"`
	// Weight 定义字重。
	Weight int `json:"weight,omitzero" yaml:"weight,omitempty" toml:"weight,omitempty"`
	// Italic 指示是否斜体。
	Italic bool `json:"italic,omitzero" yaml:"italic,omitempty" toml:"italic,omitempty"`
	// Align 定义对齐方式（left/center/right）。
	Align string `json:"align,omitempty" yaml:"align,omitempty" toml:"align,omitempty"`
	// ColSpan 定义横向合并的列数。
	ColSpan int `json:"col_span,omitzero" yaml:"col_span,omitempty" toml:"col_span,omitempty"`
	// RowSpan 定义纵向合并的行数。
	RowSpan int `json:"row_span,omitzero" yaml:"row_span,omitempty" toml:"row_span,omitempty"`
	// FirstLineIndent 定义首行缩进。
	FirstLineIndent float64 `json:"first_line_indent,omitzero" yaml:"first_line_indent,omitempty" toml:"first_line_indent,omitempty"`
	// HangingIndent 定义续行缩进。
	HangingIndent float64 `json:"hanging_indent,omitzero" yaml:"hanging_indent,omitempty" toml:"hanging_indent,omitempty"`
	// Format 定义文本格式（text/markdown）。
	Format string `json:"format,omitempty" yaml:"format,omitempty" toml:"format,omitempty"`
	// FillColor 定义单元格背景色。
	FillColor *Color `json:"fill_color,omitzero" yaml:"fill_color,omitempty" toml:"fill_color,omitempty"`
	// Actions 定义单元格的点击动作，作用范围是整个单元格。
	Actions []Action `json:"actions,omitempty" yaml:"actions,omitempty" toml:"actions,omitempty"`
}

// TableStyle 定义表格样式。
type TableStyle struct {
	// HorizontalLines 指示是否绘制横线。
	HorizontalLines bool `json:"horizontal_lines,omitzero" yaml:"horizontal_lines,omitempty" toml:"horizontal_lines,omitempty"`
	// VerticalLines 指示是否绘制竖线。
	VerticalLines bool `json:"vertical_lines,omitzero" yaml:"vertical_lines,omitempty" toml:"vertical_lines,omitempty"`
	// LineWidth 定义内部线宽。
	LineWidth float64 `json:"line_width,omitzero" yaml:"line_width,omitempty" toml:"line_width,omitempty"`
	// StrokeColor 定义边框颜色。
	StrokeColor *Color `json:"stroke_color,omitzero" yaml:"stroke_color,omitempty" toml:"stroke_color,omitempty"`
	// OuterLineWidth 定义外框线宽。
	OuterLineWidth float64 `json:"outer_line_width,omitzero" yaml:"outer_line_width,omitempty" toml:"outer_line_width,omitempty"`
	// HeaderLineWidth 定义表头分隔线宽。
	HeaderLineWidth float64 `json:"header_line_width,omitzero" yaml:"header_line_width,omitempty" toml:"header_line_width,omitempty"`
	// HeaderFillColor 定义表头背景色。
	HeaderFillColor *Color `json:"header_fill_color,omitzero" yaml:"header_fill_color,omitempty" toml:"header_fill_color,omitempty"`
	// HeaderFont 定义表头字体。
	HeaderFont string `json:"header_font,omitempty" yaml:"header_font,omitempty" toml:"header_font,omitempty"`
	// HeaderSize 定义表头字号。
	HeaderSize float64 `json:"header_size,omitzero" yaml:"header_size,omitempty" toml:"header_size,omitempty"`
	// HeaderWeight 定义表头字重。
	HeaderWeight int `json:"header_weight,omitzero" yaml:"header_weight,omitempty" toml:"header_weight,omitempty"`
	// StrikeColor 定义 Markdown 删除线的颜色；不设时与边框同色。
	StrikeColor *Color `json:"strike_color,omitzero" yaml:"strike_color,omitempty" toml:"strike_color,omitempty"`
	// LinkColor 定义 Markdown 行内链接的文字颜色；不设时用默认链接蓝。
	LinkColor *Color `json:"link_color,omitzero" yaml:"link_color,omitempty" toml:"link_color,omitempty"`
	// RuleColor 定义 Markdown 分隔线的颜色；不设时与边框同色。
	RuleColor *Color `json:"rule_color,omitzero" yaml:"rule_color,omitempty" toml:"rule_color,omitempty"`
	// CodeFont 定义 Markdown 行内代码使用的字体。
	CodeFont string `json:"code_font,omitempty" yaml:"code_font,omitempty" toml:"code_font,omitempty"`
}

// CaptionStyle 定义表题样式。
type CaptionStyle struct {
	// Font 定义字体。
	Font string `json:"font,omitempty" yaml:"font,omitempty" toml:"font,omitempty"`
	// Size 定义字号。
	Size float64 `json:"size,omitzero" yaml:"size,omitempty" toml:"size,omitempty"`
	// Weight 定义字重。
	Weight int `json:"weight,omitzero" yaml:"weight,omitempty" toml:"weight,omitempty"`
	// Italic 指示是否斜体。
	Italic bool `json:"italic,omitzero" yaml:"italic,omitempty" toml:"italic,omitempty"`
	// Align 定义对齐方式。
	Align string `json:"align,omitempty" yaml:"align,omitempty" toml:"align,omitempty"`
	// Color 定义文字颜色。
	Color *Color `json:"color,omitzero" yaml:"color,omitempty" toml:"color,omitempty"`
	// Gap 定义说明文字与所标注对象之间的间距，单位毫米。
	Gap float64 `json:"gap,omitzero" yaml:"gap,omitempty" toml:"gap,omitempty"`
}

// Clip 描述图元的裁剪区域集合。
type Clip struct {
	// Areas 定义裁剪区域。
	Areas []ClipArea `json:"areas,omitempty" yaml:"areas,omitempty" toml:"areas,omitempty"`
}

// ClipArea 描述裁剪区域中的一个路径或文本区域。
type ClipArea struct {
	// DrawParam 指定裁剪区域使用的绘制参数名称。
	DrawParam string `json:"draw_param,omitempty" yaml:"draw_param,omitempty" toml:"draw_param,omitempty"`
	// CTM 指定裁剪区域的坐标变换矩阵。
	CTM []float64 `json:"ctm,omitempty" yaml:"ctm,omitempty" toml:"ctm,omitempty"`
	// Path 定义路径裁剪内容。
	Path *ClipPath `json:"path,omitzero" yaml:"path,omitempty" toml:"path,omitempty"`
	// Text 定义文本裁剪内容。
	Text *ClipText `json:"text,omitzero" yaml:"text,omitempty" toml:"text,omitempty"`
}

// ClipPath 描述用于裁剪的路径。
type ClipPath struct {
	// Boundary 指定裁剪路径边界。
	Boundary Box `json:"boundary,omitempty" yaml:"boundary,omitempty" toml:"boundary,omitempty"`
	// Name 指定裁剪路径名称。
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`
	// Visible 指示裁剪路径是否可见。
	Visible *bool `json:"visible,omitzero" yaml:"visible,omitempty" toml:"visible,omitempty"`
	// CTM 指定裁剪路径的坐标变换矩阵。
	CTM []float64 `json:"ctm,omitempty" yaml:"ctm,omitempty" toml:"ctm,omitempty"`
	// Data 指定裁剪路径数据。
	Data string `json:"data,omitempty" yaml:"data,omitempty" toml:"data,omitempty"`
	// Stroke 指示是否绘制裁剪路径轮廓。
	Stroke bool `json:"stroke,omitzero" yaml:"stroke,omitempty" toml:"stroke,omitempty"`
	// StrokeSet 指示是否显式设置描边属性。
	StrokeSet *bool `json:"stroke_set,omitzero" yaml:"stroke_set,omitempty" toml:"stroke_set,omitempty"`
	// Fill 指示是否填充裁剪路径。
	Fill bool `json:"fill,omitzero" yaml:"fill,omitempty" toml:"fill,omitempty"`
	// Rule 指定裁剪路径填充规则。
	Rule string `json:"rule,omitempty" yaml:"rule,omitempty" toml:"rule,omitempty"`
	// LineWidth 指定裁剪路径描边线宽。
	LineWidth float64 `json:"line_width,omitzero" yaml:"line_width,omitempty" toml:"line_width,omitempty"`
	// Cap 指定裁剪路径端点样式。
	Cap string `json:"cap,omitempty" yaml:"cap,omitempty" toml:"cap,omitempty"`
	// Join 指定裁剪路径连接样式。
	Join string `json:"join,omitempty" yaml:"join,omitempty" toml:"join,omitempty"`
	// MiterLimit 指定斜接长度限制。
	MiterLimit float64 `json:"miter_limit,omitzero" yaml:"miter_limit,omitempty" toml:"miter_limit,omitempty"`
	// DashOffset 指定虚线起始偏移量。
	DashOffset float64 `json:"dash_offset,omitzero" yaml:"dash_offset,omitempty" toml:"dash_offset,omitempty"`
	// DashPattern 指定虚线长度模式。
	DashPattern []float64 `json:"dash_pattern,omitempty" yaml:"dash_pattern,omitempty" toml:"dash_pattern,omitempty"`
	// Alpha 指定裁剪路径透明度。
	Alpha *uint8 `json:"alpha,omitzero" yaml:"alpha,omitempty" toml:"alpha,omitempty"`
	// StrokeColor 指定裁剪路径描边颜色。
	StrokeColor *Color `json:"stroke_color,omitzero" yaml:"stroke_color,omitempty" toml:"stroke_color,omitempty"`
	// FillColor 指定裁剪路径填充颜色。
	FillColor *Color `json:"fill_color,omitzero" yaml:"fill_color,omitempty" toml:"fill_color,omitempty"`
}

// ClipText 描述用于裁剪的文本。
type ClipText struct {
	// Boundary 指定裁剪文本边界。
	Boundary Box `json:"boundary" yaml:"boundary,omitempty" toml:"boundary,omitempty"`
	// CTM 指定裁剪文本的坐标变换矩阵。
	CTM []float64 `json:"ctm,omitempty" yaml:"ctm,omitempty" toml:"ctm,omitempty"`
	// Font 指定裁剪文本使用的字体名称。
	Font string `json:"font,omitempty" yaml:"font,omitempty" toml:"font,omitempty"`
	// Size 指定裁剪文本字号。
	Size float64 `json:"size,omitzero" yaml:"size,omitempty" toml:"size,omitempty"`
	// Value 指定裁剪文本内容。
	Value string `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
	// TextCodes 定义裁剪文本代码。
	TextCodes []TextCode `json:"text_codes,omitempty" yaml:"text_codes,omitempty" toml:"text_codes,omitempty"`
	// Stroke 指示是否绘制文本轮廓。
	Stroke bool `json:"stroke,omitzero" yaml:"stroke,omitempty" toml:"stroke,omitempty"`
	// Fill 指示是否填充文本。
	Fill *bool `json:"fill,omitzero" yaml:"fill,omitempty" toml:"fill,omitempty"`
	// HScale 指定文本水平方向缩放比例。
	HScale float64 `json:"h_scale,omitzero" yaml:"h_scale,omitempty" toml:"h_scale,omitempty"`
	// ReadDirection 指定文本阅读方向。
	ReadDirection int `json:"read_direction,omitzero" yaml:"read_direction,omitempty" toml:"read_direction,omitempty"`
	// CharDirection 指定字符排列方向。
	CharDirection int `json:"char_direction,omitzero" yaml:"char_direction,omitempty" toml:"char_direction,omitempty"`
	// Weight 指定文本字重。
	Weight int `json:"weight,omitzero" yaml:"weight,omitempty" toml:"weight,omitempty"`
	// Italic 指示文本是否为斜体。
	Italic bool `json:"italic,omitzero" yaml:"italic,omitempty" toml:"italic,omitempty"`
	// FillColor 指定文本填充颜色。
	FillColor *Color `json:"fill_color,omitzero" yaml:"fill_color,omitempty" toml:"fill_color,omitempty"`
	// StrokeColor 指定文本描边颜色。
	StrokeColor *Color `json:"stroke_color,omitzero" yaml:"stroke_color,omitempty" toml:"stroke_color,omitempty"`
}

// ImageBorder 描述图像边框的绘制样式。
type ImageBorder struct {
	// LineWidth 指定图像边框线宽；省略使用 0.353mm，显式 0 关闭边框。
	LineWidth *float64 `json:"line_width,omitzero" yaml:"line_width,omitempty" toml:"line_width,omitempty"`
	// HorizontalRadius 指定边框水平圆角半径。
	HorizontalRadius float64 `json:"horizontal_radius,omitzero" yaml:"horizontal_radius,omitempty" toml:"horizontal_radius,omitempty"`
	// VerticalRadius 指定边框垂直圆角半径。
	VerticalRadius float64 `json:"vertical_radius,omitzero" yaml:"vertical_radius,omitempty" toml:"vertical_radius,omitempty"`
	// DashOffset 指定边框虚线起始偏移量。
	DashOffset float64 `json:"dash_offset,omitzero" yaml:"dash_offset,omitempty" toml:"dash_offset,omitempty"`
	// DashPattern 指定边框虚线长度模式。
	DashPattern []float64 `json:"dash_pattern,omitempty" yaml:"dash_pattern,omitempty" toml:"dash_pattern,omitempty"`
	// Color 指定边框颜色。
	Color *Color `json:"color,omitzero" yaml:"color,omitempty" toml:"color,omitempty"`
}

// Load 读取并解析 JSON 或 YAML manifest。
func Load(path, format string) (Manifest, string, error) {
	var data []byte
	var err error
	baseDir := ""
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
		if err == nil {
			baseDir, err = os.Getwd()
		}
	} else {
		data, err = os.ReadFile(path)
		if err == nil {
			baseDir, err = filepath.Abs(filepath.Dir(path))
		}
	}
	if err != nil {
		return Manifest{}, "", fmt.Errorf("读取 manifest 失败: %w", err)
	}
	if strings.TrimSpace(format) == "" || strings.EqualFold(format, "auto") {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if path == "-" || format == "" {
			format = "yaml"
		}
	}
	result, err := Parse(data, format)
	if err != nil {
		return Manifest{}, "", err
	}
	if baseDir == "" {
		return Manifest{}, "", errors.New("无法确定 manifest 所在目录")
	}
	return result, baseDir, nil
}

// Parse 从内存解析 JSON/YAML 清单；拒绝未知字段和多个顶层文档。
func Parse(data []byte, format string) (Manifest, error) {
	var result Manifest
	switch strings.ToLower(format) {
	case "json":
		decoder := jsontext.NewDecoder(bytes.NewReader(data), json.RejectUnknownMembers(true))
		if err := json.UnmarshalDecode(decoder, &result); err != nil {
			return Manifest{}, fmt.Errorf("解析 JSON manifest 失败: %w", err)
		}
		var extra any
		if err := json.UnmarshalDecode(decoder, &extra); !errors.Is(err, io.EOF) {
			return Manifest{}, errors.New("JSON manifest 必须只有一个顶层文档")
		}
	case "yaml", "yml":
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&result); err != nil {
			return Manifest{}, fmt.Errorf("解析 YAML manifest 失败: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return Manifest{}, errors.New("YAML manifest 必须只有一个顶层文档")
		}
	default:
		return Manifest{}, fmt.Errorf("不支持的 manifest 格式: %q", format)
	}
	return result, nil
}

// BuildOptions 控制 manifest 到 creator 文档的转换行为。
type BuildOptions struct {
	// StreamAssets 对通过 file 引用的封面、图片、多媒体、附件和页面图片使用
	// 惰性数据来源，避免创建超大 OFD 时把资源整体读入内存。
	StreamAssets bool
	// LoadAsset 可选，从内存读取通过 file 引用的资源；为空时从磁盘资源根目录读取。
	// 设置后 StreamAssets 的惰性来源会退化为内存字节来源。
	LoadAsset func(name string) ([]byte, error)
}

// Build 将 manifest 转换为 creator 文档模型，并加载资源文件。
func (m Manifest) Build(baseDir, assetRoot string) (creator.Document, error) {
	return m.BuildWithOptions(baseDir, assetRoot, BuildOptions{})
}

func (m Manifest) BuildWithOptions(baseDir, assetRoot string, options BuildOptions) (creator.Document, error) {
	if m.Version != 1 {
		return creator.Document{}, fmt.Errorf("不支持的 manifest 版本: %d", m.Version)
	}
	if m.Flow != nil && len(m.Pages) > 0 {
		return creator.Document{}, errors.New("flow 与 pages 不能同时设置")
	}
	if assetRoot == "" {
		assetRoot = baseDir
	} else if !filepath.IsAbs(assetRoot) {
		assetRoot = filepath.Join(baseDir, assetRoot)
	}
	// 资源根目录为空表示所有资源都内联（DataBase64 或由 LoadAsset 提供），
	// 根本没有需要落到磁盘的路径。此时不能调 filepath.Abs：它对空串求值等
	// 于取当前工作目录，而 js/wasm 环境没有工作目录，
	// os.Getwd 会返回 "getwd: not implemented on js"，把一个纯内存的构建
	// 挡在门外。浏览器里 merge.Pages 正是这条路径。
	root := ""
	if assetRoot != "" {
		abs, err := filepath.Abs(assetRoot)
		if err != nil {
			return creator.Document{}, fmt.Errorf("资源根目录无效: %w", err)
		}
		root = abs
	}
	store := assetStore{root: root, loader: options.LoadAsset}
	loadResource := func(file, encoded string) ([]byte, creator.DataSource, error) {
		return store.loadOptional(file, encoded, options.StreamAssets)
	}
	document := creator.Document{
		ID: m.Document.ID, Title: m.Document.Title, Author: m.Document.Author,
		Subject: m.Document.Subject, Abstract: m.Document.Abstract,
		Creator: m.Document.Creator, CreatorVersion: m.Document.CreatorVersion,
		PageSize: creator.PageSize{Width: m.Document.PageSize.Width, Height: m.Document.PageSize.Height},
		DocUsage: m.Document.DocUsage, Keywords: m.Document.Keywords, DefaultCS: m.Document.DefaultCS,
	}
	if m.Document.Permissions != nil {
		permissions, err := buildPermissions(m.Document.Permissions)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.permissions: %w", err)
		}
		document.Permissions = permissions
	}
	for index, resource := range m.Resources.Public {
		data, err := store.loadData(resource.File, resource.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.public[%d].file: %w", index, err)
		}
		files, err := store.buildResourceFiles(resource.Files, options.StreamAssets)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.public[%d].files: %w", index, err)
		}
		document.PublicRes = append(document.PublicRes, creator.PublicResource{Name: resource.Name, Data: data, Files: files})
	}
	for index, tag := range m.Resources.CustomTags {
		schema, err := store.loadData(tag.Schema, tag.SchemaBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.custom_tags[%d].schema: %w", index, err)
		}
		data, err := store.loadData(tag.Data, tag.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.custom_tags[%d].data: %w", index, err)
		}
		document.CustomTags = append(document.CustomTags, creator.CustomTag{NameSpace: tag.NameSpace, Schema: schema, SchemaName: tag.SchemaName, Data: data, DataName: tag.DataName})
	}
	if m.Document.Cover != "" || m.Document.CoverBase64 != "" {
		data, source, err := loadResource(m.Document.Cover, m.Document.CoverBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.cover: %w", err)
		}
		document.CoverData, document.CoverSource, document.CoverName = data, source, m.Document.CoverName
	}
	if m.Document.CreationDate != "" {
		date, err := parseDate(m.Document.CreationDate)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.creation_date: %w", err)
		}
		document.CreationDate = date
	}
	if m.Document.ModDate != "" {
		date, err := parseDate(m.Document.ModDate)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.mod_date: %w", err)
		}
		document.ModDate = date
	}
	if m.Document.Area != nil {
		document.Area = buildPageArea(m.Document.Area)
	}
	for _, data := range m.Document.CustomData {
		document.CustomDatas = append(document.CustomDatas, creator.CustomData{Name: data.Name, Value: data.Value})
	}
	if m.Document.Preferences != nil {
		document.Preferences = &creator.ViewPreferences{PageMode: m.Document.Preferences.PageMode, PageLayout: m.Document.Preferences.PageLayout, TabDisplay: m.Document.Preferences.TabDisplay, HideToolbar: m.Document.Preferences.HideToolbar, HideMenubar: m.Document.Preferences.HideMenubar, HideWindowUI: m.Document.Preferences.HideWindowUI, ZoomMode: m.Document.Preferences.ZoomMode, Zoom: m.Document.Preferences.Zoom}
	}
	outlines, err := buildOutlines(m.Document.Outlines)
	if err != nil {
		return creator.Document{}, fmt.Errorf("document.outlines: %w", err)
	}
	document.Outlines = outlines
	for index, signature := range m.Document.Signatures {
		value, err := store.buildSignature(signature, options.StreamAssets)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.signatures[%d]: %w", index, err)
		}
		document.Signatures = append(document.Signatures, value)
	}
	for index, action := range m.Document.Actions {
		converted, err := buildAction(action)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.actions[%d]: %w", index, err)
		}
		document.Actions = append(document.Actions, converted)
	}
	for _, bookmark := range m.Document.Bookmarks {
		document.Bookmarks = append(document.Bookmarks, creator.Bookmark{Name: bookmark.Name, Goto: buildGoto(bookmark.Goto)})
	}
	for index, attachment := range m.Document.Attachments {
		data, source, err := loadResource(attachment.File, attachment.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.attachments[%d].file: %w", index, err)
		}
		value := creator.Attachment{ID: attachment.ID, Name: attachment.Name, Format: attachment.Format, Usage: attachment.Usage, FileName: attachment.FileName, Visible: attachment.Visible, Data: data, Source: source}
		if attachment.CreationDate != "" {
			value.CreationDate, err = parseDate(attachment.CreationDate)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.attachments[%d].creation_date: %w", index, err)
			}
		}
		if attachment.ModDate != "" {
			value.ModDate, err = parseDate(attachment.ModDate)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.attachments[%d].mod_date: %w", index, err)
			}
		}
		document.Attachments = append(document.Attachments, value)
	}
	for index, extension := range m.Document.Extensions {
		value := creator.Extension{AppName: extension.AppName, Company: extension.Company, AppVersion: extension.AppVersion, RefID: extension.RefID, Data: extension.Data, DataXML: []byte(extension.DataXML), DataName: extension.DataName}
		if extension.Date != "" {
			value.Date, err = parseDate(extension.Date)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.extensions[%d].date: %w", index, err)
			}
		}
		for _, property := range extension.Properties {
			value.Properties = append(value.Properties, creator.ExtensionProperty{Name: property.Name, Type: property.Type, Value: property.Value})
		}
		if extension.DataFile != "" || extension.DataFileBase64 != "" {
			data, source, err := store.loadOptional(extension.DataFile, extension.DataFileBase64, options.StreamAssets)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.extensions[%d].data_file: %w", index, err)
			}
			value.DataFile, value.DataFileSource = data, source
		}
		document.Extensions = append(document.Extensions, value)
	}
	for index, version := range m.Document.Versions {
		value := creator.DocumentVersion{ID: version.ID, Index: version.Index, Current: version.Current, Version: version.Version, Name: version.Name, DocRootName: version.DocRootName}
		if version.CreationDate != "" {
			parsed, err := parseDate(version.CreationDate)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.versions[%d].creation_date: %w", index, err)
			}
			value.CreationDate = parsed
		}
		if version.DocRoot != "" || version.DocRootBase64 != "" {
			data, err := store.loadData(version.DocRoot, version.DocRootBase64)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.versions[%d].doc_root: %w", index, err)
			}
			value.DocRoot = data
		}
		for _, file := range version.Files {
			value.Files = append(value.Files, creator.VersionFile{ID: file.ID, Path: file.Path})
		}
		document.Versions = append(document.Versions, value)
	}
	if document.PageSize.Width == 0 && document.PageSize.Height == 0 && m.Document.PageSize.Name != "" {
		if strings.EqualFold(m.Document.PageSize.Name, "A4") {
			document.PageSize = creator.A4
		} else {
			return creator.Document{}, fmt.Errorf("document.page_size.name: 不支持的页面尺寸 %q，请提供 width 和 height", m.Document.PageSize.Name)
		}
	}
	for index, font := range m.Resources.Fonts {
		data, source, err := loadResource(font.File, font.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.fonts[%d].file: %w", index, err)
		}
		document.Fonts = append(document.Fonts, creator.Font{Name: font.Name, FontName: font.FontName, FamilyName: font.FamilyName, Charset: font.Charset, Format: font.Format, Italic: font.Italic, Bold: font.Bold, Serif: font.Serif, FixedWidth: font.FixedWidth, Data: data, Source: source})
	}
	for index, image := range m.Resources.Images {
		data, source, err := loadResource(image.File, image.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.images[%d]: %w", index, err)
		}
		if image.ID == 0 {
			return creator.Document{}, fmt.Errorf("resources.images[%d].id: ID 不能为空", index)
		}
		document.Media = append(document.Media, creator.Media{ID: image.ID, Type: "Image", Format: image.Format, Name: image.Name, Data: data, Source: source})
	}
	for index, media := range m.Resources.Media {
		data, source, err := loadResource(media.File, media.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.media[%d].file: %w", index, err)
		}
		document.Media = append(document.Media, creator.Media{ID: media.ID, Type: media.Type, Format: media.Format, Name: media.Name, Data: data, Source: source})
	}
	for index, space := range m.Resources.ColorSpaces {
		value := creator.ColorSpace{ID: space.ID, Type: space.Type, BitsPerComponent: space.BitsPerComponent, Palette: space.Palette}
		if space.ProfileFile != "" || space.ProfileBase64 != "" {
			data, err := store.loadData(space.ProfileFile, space.ProfileBase64)
			if err != nil {
				return creator.Document{}, fmt.Errorf("resources.color_spaces[%d].profile_file: %w", index, err)
			}
			value.ProfileData = data
			profileName := space.ProfileName
			if profileName == "" && space.ProfileFile != "" {
				profileName = filepath.Base(filepath.FromSlash(space.ProfileFile))
			}
			if profileName != "" {
				if err := validateLeafName(profileName); err != nil {
					return creator.Document{}, fmt.Errorf("resources.color_spaces[%d].profile_name: %w", index, err)
				}
				value.Profile = filepath.Join("Profiles", profileName)
			}
		}
		document.ColorSpaces = append(document.ColorSpaces, value)
	}
	for index, param := range m.Resources.DrawParams {
		fill, err := buildColor(param.FillColor)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.draw_params[%d].fill_color: %w", index, err)
		}
		stroke, err := buildColor(param.StrokeColor)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.draw_params[%d].stroke_color: %w", index, err)
		}
		document.DrawParams = append(document.DrawParams, creator.DrawParam{Name: param.Name, Relative: param.Relative, LineWidth: param.LineWidth, Join: param.Join, Cap: param.Cap, DashOffset: param.DashOffset, DashPattern: param.DashPattern, MiterLimit: param.MiterLimit, FillColor: fill, StrokeColor: stroke})
	}
	for index, composite := range m.Resources.Composites {
		items, err := buildItems(composite.Items)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.composites[%d].items: %w", index, err)
		}
		document.Composites = append(document.Composites, creator.CompositeGraphicUnit{ID: composite.ID, Width: composite.Width, Height: composite.Height, Thumbnail: composite.Thumbnail, Substitution: composite.Substitution, Items: items})
	}
	for index, template := range m.Templates {
		if len(template.Layers) > 0 && len(template.Items) > 0 {
			return creator.Document{}, fmt.Errorf("templates[%d]: layers 不能与 items 同时设置", index)
		}
		layers, err := buildLayers(template.Layers)
		if err != nil {
			return creator.Document{}, fmt.Errorf("templates[%d].layers: %w", index, err)
		}
		items, err := buildItems(template.Items)
		if err != nil {
			return creator.Document{}, fmt.Errorf("templates[%d].items: %w", index, err)
		}
		document.Templates = append(document.Templates, creator.TemplatePage{ID: template.ID, Name: template.Name, ZOrder: template.ZOrder, Area: buildPageArea(template.Area), Layers: layers, Items: items})
	}
	for index, page := range m.Pages {
		if page.Repeat != "" || page.Paginate != nil || page.Variants != nil {
			return creator.Document{}, fmt.Errorf("pages[%d]: 模板 repeat/paginate/variants 尚未展开，请使用 template render 或提供 --data", index)
		}
		if len(page.Layers) > 0 && len(page.Items) > 0 {
			return creator.Document{}, fmt.Errorf("pages[%d]: layers 不能与 items 同时设置", index)
		}
		pageActions, err := buildActions(page.Actions)
		if err != nil {
			return creator.Document{}, fmt.Errorf("pages[%d].actions: %w", index, err)
		}
		converted := creator.Page{Area: buildPageArea(page.Area), LayerType: page.LayerType, Actions: pageActions}
		for resourceIndex, resource := range page.Resources {
			if len(resource.Images) > 0 && (resource.File != "" || resource.DataBase64 != "" || len(resource.Files) > 0) {
				return creator.Document{}, fmt.Errorf("pages[%d].resources[%d]: images 不能与 file、data_base64 或 files 同时设置", index, resourceIndex)
			}
			data, err := store.loadData(resource.File, resource.DataBase64)
			if err != nil {
				return creator.Document{}, fmt.Errorf("pages[%d].resources[%d].file: %w", index, resourceIndex, err)
			}
			files, err := store.buildPageResourceFiles(resource.Files, options.StreamAssets)
			if err != nil {
				return creator.Document{}, fmt.Errorf("pages[%d].resources[%d].files: %w", index, resourceIndex, err)
			}
			convertedResource := creator.PageResource{Data: data, Files: files}
			for imageIndex, image := range resource.Images {
				imageData, imageSource, err := loadResource(image.File, image.DataBase64)
				if err != nil {
					return creator.Document{}, fmt.Errorf("pages[%d].resources[%d].images[%d]: %w", index, resourceIndex, imageIndex, err)
				}
				convertedResource.Images = append(convertedResource.Images, creator.PageImage{ID: image.ID, Format: image.Format, Name: image.Name, Data: imageData, Source: imageSource})
			}
			converted.Resources = append(converted.Resources, convertedResource)
		}
		for _, template := range page.Templates {
			converted.Templates = append(converted.Templates, creator.TemplateRef{ID: template.ID, ZOrder: template.ZOrder})
		}
		layers, err := buildLayers(page.Layers)
		if err != nil {
			return creator.Document{}, fmt.Errorf("pages[%d].layers: %w", index, err)
		}
		converted.Layers = layers
		if len(layers) == 0 {
			converted.Items, err = buildItems(page.Items)
			if err != nil {
				return creator.Document{}, fmt.Errorf("pages[%d].items: %w", index, err)
			}
		}
		document.Pages = append(document.Pages, converted)
	}
	for index, annotationPage := range m.Document.Annotations {
		items := make([]creator.Annotation, 0, len(annotationPage.Items))
		for itemIndex, annotation := range annotationPage.Items {
			converted, err := annotation.build()
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.annotations[%d].items[%d]: %w", index, itemIndex, err)
			}
			items = append(items, converted)
		}
		document.Annotations = append(document.Annotations, creator.AnnotationPage{Page: annotationPage.Page, Items: items})
	}
	// flow 构建会清理未引用字体，注解必须先加入模型以保留其专用嵌入字体。
	if m.Flow != nil {
		composed, err := m.Flow.build(document, store)
		if err != nil {
			return creator.Document{}, fmt.Errorf("flow: %w", err)
		}
		document = composed
	}
	return document, nil
}

func (i Item) build() (creator.Item, error) {
	if i.Bind != nil {
		return nil, fmt.Errorf("bind %q 尚未展开，请使用 template render 或提供 --data", i.Bind.Path)
	}
	var ctm *creator.CTM
	if len(i.CTM) > 0 {
		if len(i.CTM) != 6 {
			return nil, errors.New("ctm 必须包含 6 个数值")
		}
		value := creator.CTM{i.CTM[0], i.CTM[1], i.CTM[2], i.CTM[3], i.CTM[4], i.CTM[5]}
		ctm = &value
	}
	switch strings.ToLower(strings.TrimSpace(i.Type)) {
	case "text":
		codes := make([]creator.TextCode, 0, len(i.TextCodes))
		for _, code := range i.TextCodes {
			codes = append(codes, creator.TextCode{Value: code.Value, X: code.X, Y: code.Y, DeltaX: code.DeltaX, DeltaY: code.DeltaY})
		}
		transforms := make([]creator.CGTransform, 0, len(i.CGTransforms))
		for _, transform := range i.CGTransforms {
			transforms = append(transforms, creator.CGTransform{CodePosition: transform.CodePosition, CodeCount: transform.CodeCount, GlyphCount: transform.GlyphCount, Glyphs: transform.Glyphs})
		}
		fillColor, err := buildColor(i.FillColor)
		if err != nil {
			return nil, fmt.Errorf("fill_color: %w", err)
		}
		strokeColor, err := buildColor(i.StrokeColor)
		if err != nil {
			return nil, fmt.Errorf("stroke_color: %w", err)
		}
		actions, err := buildActions(i.Actions)
		if err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		}
		clips, err := buildClips(i.Clips)
		if err != nil {
			return nil, fmt.Errorf("clips: %w", err)
		}
		return creator.Text{X: i.X, Y: i.Y, Width: i.Width, Height: i.Height, Value: i.Value, Font: i.Font, Size: i.Size, Visible: i.Visible, Fill: i.Fill, Stroke: i.Stroke, HScale: i.HScale, ReadDirection: i.ReadDirection, CharDirection: i.CharDirection, Weight: i.Weight, Italic: i.Italic, DrawParam: i.DrawParam, CTM: ctm, FillColor: fillColor, StrokeColor: strokeColor, TextCodes: codes, CGTransforms: transforms, Actions: actions, Clips: clips}, nil
	case "path":
		fillColor, err := buildColor(i.FillColor)
		if err != nil {
			return nil, fmt.Errorf("fill_color: %w", err)
		}
		strokeColor, err := buildColor(i.StrokeColor)
		if err != nil {
			return nil, fmt.Errorf("stroke_color: %w", err)
		}
		clips, err := buildClips(i.Clips)
		if err != nil {
			return nil, fmt.Errorf("clips: %w", err)
		}
		actions, err := buildActions(i.Actions)
		if err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		}
		fill := false
		if i.Fill != nil {
			fill = *i.Fill
		}
		return creator.Path{X: i.X, Y: i.Y, Width: i.Width, Height: i.Height, Data: i.Data, Name: i.Name, Visible: i.Visible, Stroke: i.Stroke, StrokeSet: i.StrokeSet, Fill: fill, Rule: i.Rule, LineWidth: i.LineWidth, Cap: i.Cap, Join: i.Join, MiterLimit: i.MiterLimit, DashOffset: i.DashOffset, DashPattern: i.DashPattern, Alpha: i.Alpha, DrawParam: i.DrawParam, CTM: ctm, FillColor: fillColor, StrokeColor: strokeColor, Actions: actions, Clips: clips}, nil
	case "image":
		if i.ResourceID == 0 && i.DataBase64 == "" {
			return nil, errors.New("image.resource_id 或 data_base64 至少设置一个")
		}
		if i.ResourceID != 0 && i.DataBase64 != "" {
			return nil, errors.New("image.resource_id 不能与 data_base64 同时设置")
		}
		clips, err := buildClips(i.Clips)
		if err != nil {
			return nil, fmt.Errorf("clips: %w", err)
		}
		border, err := buildImageBorder(i.Border)
		if err != nil {
			return nil, fmt.Errorf("border: %w", err)
		}
		data, err := decodeOptionalBytes(i.DataBase64)
		if err != nil {
			return nil, fmt.Errorf("data_base64: %w", err)
		}
		actions, err := buildActions(i.Actions)
		if err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		}
		return creator.Image{X: i.X, Y: i.Y, Width: i.Width, Height: i.Height, Data: data, ResourceID: i.ResourceID, Substitution: i.Substitution, ImageMask: i.ImageMask, Format: i.Format, Name: i.Name, Visible: i.Visible, LineWidth: i.LineWidth, Cap: i.Cap, Join: i.Join, MiterLimit: i.MiterLimit, DashOffset: i.DashOffset, DashPattern: i.DashPattern, Alpha: i.Alpha, DrawParam: i.DrawParam, CTM: ctm, Actions: actions, Clips: clips, Border: border}, nil
	case "composite":
		if i.ResourceID == 0 {
			return nil, errors.New("composite.resource_id 不能为空")
		}
		clips, err := buildClips(i.Clips)
		if err != nil {
			return nil, fmt.Errorf("clips: %w", err)
		}
		actions, err := buildActions(i.Actions)
		if err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		}
		return creator.Composite{X: i.X, Y: i.Y, Width: i.Width, Height: i.Height, ResourceID: i.ResourceID, Name: i.Name, Visible: i.Visible, DrawParam: i.DrawParam, CTM: ctm, Actions: actions, Clips: clips}, nil
	case "page-block", "pageblock":
		items, err := buildItems(i.Items)
		if err != nil {
			return nil, err
		}
		return creator.PageBlock{Items: items}, nil
	case "table":
		return i.buildTable()
	case "figure":
		return i.buildFigure()
	default:
		return nil, fmt.Errorf("不支持的图元类型 %q，仅支持 text、path、image、composite、page-block、table、figure", i.Type)
	}
}

func (i Item) buildTable() (creator.Table, error) {
	table := creator.Table{
		X:                   i.X,
		Y:                   i.Y,
		Width:               i.Width,
		ColumnWidths:        i.ColumnWidths,
		HeaderRows:          i.HeaderRows,
		Caption:             i.Caption,
		ContinuationCaption: i.ContinuationCaption,
	}

	if i.Style != nil {
		style := creator.TableStyle{
			HorizontalLines: i.Style.HorizontalLines,
			VerticalLines:   i.Style.VerticalLines,
			LineWidth:       i.Style.LineWidth,
			OuterLineWidth:  i.Style.OuterLineWidth,
			HeaderLineWidth: i.Style.HeaderLineWidth,
			HeaderFont:      i.Style.HeaderFont,
			HeaderSize:      i.Style.HeaderSize,
			HeaderWeight:    i.Style.HeaderWeight,
			CodeFont:        i.Style.CodeFont,
		}
		if i.Style.StrikeColor != nil {
			c, err := buildColor(i.Style.StrikeColor)
			if err != nil {
				return creator.Table{}, fmt.Errorf("style.strike_color: %w", err)
			}
			style.StrikeColor = c
		}
		if i.Style.RuleColor != nil {
			c, err := buildColor(i.Style.RuleColor)
			if err != nil {
				return creator.Table{}, fmt.Errorf("style.rule_color: %w", err)
			}
			style.RuleColor = c
		}
		if i.Style.LinkColor != nil {
			c, err := buildColor(i.Style.LinkColor)
			if err != nil {
				return creator.Table{}, fmt.Errorf("style.link_color: %w", err)
			}
			style.LinkColor = c
		}
		if i.Style.StrokeColor != nil {
			c, err := buildColor(i.Style.StrokeColor)
			if err != nil {
				return creator.Table{}, fmt.Errorf("style.stroke_color: %w", err)
			}
			style.StrokeColor = c
		}
		if i.Style.HeaderFillColor != nil {
			c, err := buildColor(i.Style.HeaderFillColor)
			if err != nil {
				return creator.Table{}, fmt.Errorf("style.header_fill_color: %w", err)
			}
			style.HeaderFillColor = c
		}
		table.Style = style
	}

	if i.CaptionStyle != nil {
		cs, err := i.CaptionStyle.build()
		if err != nil {
			return creator.Table{}, fmt.Errorf("caption_style: %w", err)
		}
		table.CaptionStyle = cs
	}

	if i.ContinuationCaptionStyle != nil {
		cs, err := i.ContinuationCaptionStyle.build()
		if err != nil {
			return creator.Table{}, fmt.Errorf("continuation_caption_style: %w", err)
		}
		table.ContinuationCaptionStyle = cs
	}

	for _, row := range i.Rows {
		var cells []creator.TableCell
		for _, cell := range row.Cells {
			tc := creator.TableCell{
				Value:           cell.Value,
				Font:            cell.Font,
				Size:            cell.Size,
				Weight:          cell.Weight,
				Italic:          cell.Italic,
				Align:           cell.Align,
				ColSpan:         cell.ColSpan,
				RowSpan:         cell.RowSpan,
				FirstLineIndent: cell.FirstLineIndent,
				HangingIndent:   cell.HangingIndent,
				Format:          cell.Format,
			}
			if cell.FillColor != nil {
				c, err := buildColor(cell.FillColor)
				if err != nil {
					return creator.Table{}, fmt.Errorf("cell.fill_color: %w", err)
				}
				tc.FillColor = c
			}
			actions, err := buildActions(cell.Actions)
			if err != nil {
				return creator.Table{}, fmt.Errorf("cell.actions: %w", err)
			}
			tc.Actions = actions
			cells = append(cells, tc)
		}
		table.Rows = append(table.Rows, creator.TableRow{Cells: cells})
	}

	return table, nil
}

func (cs CaptionStyle) build() (*creator.CaptionStyle, error) {
	style := &creator.CaptionStyle{
		Font:   cs.Font,
		Size:   cs.Size,
		Weight: cs.Weight,
		Italic: cs.Italic,
		Align:  cs.Align,
		Gap:    cs.Gap,
	}
	if cs.Color != nil {
		c, err := buildColor(cs.Color)
		if err != nil {
			return nil, err
		}
		style.Color = c
	}
	return style, nil
}

func buildItems(values []Item) ([]creator.Item, error) {
	items := make([]creator.Item, 0, len(values))
	for index, value := range values {
		item, err := value.build()
		if err != nil {
			return nil, fmt.Errorf("items[%d]: %w", index, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func buildLayers(values []Layer) ([]creator.Layer, error) {
	layers := make([]creator.Layer, 0, len(values))
	for index, value := range values {
		items, err := buildItems(value.Items)
		if err != nil {
			return nil, fmt.Errorf("图层 %d: %w", index+1, err)
		}
		layers = append(layers, creator.Layer{Type: value.Type, DrawParam: value.DrawParam, Items: items})
	}
	return layers, nil
}

func buildPageArea(value *PageArea) *creator.PageArea {
	if value == nil {
		return nil
	}
	return &creator.PageArea{PhysicalBox: buildBox(value.PhysicalBox), ApplicationBox: buildBox(value.ApplicationBox), ContentBox: buildBox(value.ContentBox), BleedBox: buildBox(value.BleedBox)}
}

func buildBox(value *Box) *creator.Box {
	if value == nil {
		return nil
	}
	return &creator.Box{X: value.X, Y: value.Y, Width: value.Width, Height: value.Height}
}

func buildImageBorder(value *ImageBorder) (*creator.ImageBorder, error) {
	if value == nil {
		return nil, nil
	}
	color, err := buildColor(value.Color)
	if err != nil {
		return nil, err
	}
	return &creator.ImageBorder{LineWidth: value.LineWidth, HorizontalRadius: value.HorizontalRadius, VerticalRadius: value.VerticalRadius, DashOffset: value.DashOffset, DashPattern: value.DashPattern, Color: color}, nil
}

func buildClips(values []Clip) (*creator.Clips, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := &creator.Clips{Items: make([]creator.Clip, 0, len(values))}
	for clipIndex, value := range values {
		clip := creator.Clip{Areas: make([]creator.ClipArea, 0, len(value.Areas))}
		for areaIndex, area := range value.Areas {
			ctm, err := buildCTM(area.CTM)
			if err != nil {
				return nil, fmt.Errorf("[%d].areas[%d].ctm: %w", clipIndex, areaIndex, err)
			}
			converted := creator.ClipArea{DrawParam: area.DrawParam, CTM: ctm}
			if area.Path != nil {
				fill, err := buildColor(area.Path.FillColor)
				if err != nil {
					return nil, err
				}
				stroke, err := buildColor(area.Path.StrokeColor)
				if err != nil {
					return nil, err
				}
				pathCTM, err := buildCTM(area.Path.CTM)
				if err != nil {
					return nil, err
				}
				converted.Path = &creator.ClipPath{Boundary: *buildBox(&area.Path.Boundary), Name: area.Path.Name, Visible: area.Path.Visible, CTM: pathCTM, Data: area.Path.Data, Stroke: area.Path.Stroke, StrokeSet: area.Path.StrokeSet, Fill: area.Path.Fill, Rule: area.Path.Rule, LineWidth: area.Path.LineWidth, Cap: area.Path.Cap, Join: area.Path.Join, MiterLimit: area.Path.MiterLimit, DashOffset: area.Path.DashOffset, DashPattern: area.Path.DashPattern, Alpha: area.Path.Alpha, StrokeColor: stroke, FillColor: fill}
			}
			if area.Text != nil {
				fill, err := buildColor(area.Text.FillColor)
				if err != nil {
					return nil, err
				}
				stroke, err := buildColor(area.Text.StrokeColor)
				if err != nil {
					return nil, err
				}
				textCTM, err := buildCTM(area.Text.CTM)
				if err != nil {
					return nil, err
				}
				codes := make([]creator.TextCode, 0, len(area.Text.TextCodes))
				for _, code := range area.Text.TextCodes {
					codes = append(codes, creator.TextCode{Value: code.Value, X: code.X, Y: code.Y, DeltaX: code.DeltaX, DeltaY: code.DeltaY})
				}
				converted.Text = &creator.ClipText{Boundary: *buildBox(&area.Text.Boundary), CTM: textCTM, Font: area.Text.Font, Size: area.Text.Size, Value: area.Text.Value, TextCodes: codes, Stroke: area.Text.Stroke, Fill: area.Text.Fill, HScale: area.Text.HScale, ReadDirection: area.Text.ReadDirection, CharDirection: area.Text.CharDirection, Weight: area.Text.Weight, Italic: area.Text.Italic, FillColor: fill, StrokeColor: stroke}
			}
			clip.Areas = append(clip.Areas, converted)
		}
		result.Items = append(result.Items, clip)
	}
	return result, nil
}

func parseDate(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if result, err := time.Parse(layout, value); err == nil {
			return result, nil
		}
	}
	return time.Time{}, fmt.Errorf("日期格式无效: %q", value)
}

// assetStore 解析 manifest 引用的资源，可来自磁盘目录或内存加载器。
type assetStore struct {
	root   string
	loader func(name string) ([]byte, error)
}

// read 读取资源字节。使用内存加载器时仍校验路径必须是相对、安全的。
func (s assetStore) read(name string) ([]byte, error) {
	if s.loader == nil {
		return readAsset(s.root, name)
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("资源文件路径不能为空")
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return nil, errors.New("资源文件路径必须是资源根目录下的相对路径")
	}
	return s.loader(name)
}

func (s assetStore) loadData(file, encoded string) ([]byte, error) {
	if file != "" && encoded != "" {
		return nil, errors.New("file 不能与 data_base64 同时设置")
	}
	if file != "" {
		return s.read(file)
	}
	return decodeOptionalBytes(encoded)
}

func decodeOptionalBytes(encoded string) ([]byte, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 数据无效: %w", err)
	}
	return data, nil
}

func validateLeafName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.Base(filepath.FromSlash(value)) != value || strings.ContainsAny(value, `/\\\x00`) {
		return fmt.Errorf("文件名无效: %q", value)
	}
	return nil
}

func (s assetStore) buildResourceFiles(values []ResourceFile, stream bool) ([]creator.PublicResourceFile, error) {
	result := make([]creator.PublicResourceFile, 0, len(values))
	for index, value := range values {
		data, source, err := s.loadOptional(value.File, value.DataBase64, stream)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", index, err)
		}
		result = append(result, creator.PublicResourceFile{Path: value.Path, Data: data, Source: source})
	}
	return result, nil
}

func (s assetStore) buildPageResourceFiles(values []ResourceFile, stream bool) ([]creator.PageResourceFile, error) {
	result := make([]creator.PageResourceFile, 0, len(values))
	for index, value := range values {
		data, source, err := s.loadOptional(value.File, value.DataBase64, stream)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", index, err)
		}
		result = append(result, creator.PageResourceFile{Path: value.Path, Data: data, Source: source})
	}
	return result, nil
}

// loadOptional 读取可选资源。stream 为真且来自磁盘时返回惰性文件来源；
// 使用内存加载器时退化为内存字节来源。
func (s assetStore) loadOptional(file, encoded string, stream bool) ([]byte, creator.DataSource, error) {
	if file != "" && encoded != "" {
		return nil, nil, errors.New("file 不能与 data_base64 同时设置")
	}
	if file == "" {
		data, err := decodeOptionalBytes(encoded)
		return data, nil, err
	}
	if stream && s.loader == nil {
		source, err := loadSource(s.root, file)
		return nil, source, err
	}
	data, err := s.read(file)
	if err != nil {
		return nil, nil, err
	}
	if stream {
		return nil, creator.BytesDataSource(data), nil
	}
	return data, nil, nil
}

func buildPermissions(value *Permissions) (*creator.Permissions, error) {
	result := &creator.Permissions{Edit: value.Edit, Annot: value.Annot, Export: value.Export, Signature: value.Signature, Watermark: value.Watermark, PrintScreen: value.PrintScreen}
	if value.Print != nil {
		result.Print = &creator.PrintSettings{Printable: value.Print.Printable, Copies: value.Print.Copies}
	}
	if value.ValidPeriod != nil {
		start, err := parseDate(value.ValidPeriod.Start)
		if err != nil {
			return nil, fmt.Errorf("valid_period.start: %w", err)
		}
		end, err := parseDate(value.ValidPeriod.End)
		if err != nil {
			return nil, fmt.Errorf("valid_period.end: %w", err)
		}
		result.ValidPeriod = &creator.ValidPeriod{Start: start, End: end}
	}
	return result, nil
}

func buildColor(value *Color) (*creator.Color, error) {
	if value == nil {
		return nil, nil
	}
	result := &creator.Color{R: value.R, G: value.G, B: value.B, Components: value.Components, ColorSpace: value.ColorSpace, Index: value.Index, Alpha: value.Alpha}
	if value.Axial != nil {
		segments, err := buildColorStops(value.Axial.Segments)
		if err != nil {
			return nil, fmt.Errorf("axial.segments: %w", err)
		}
		result.Axial = &creator.AxialShading{MapType: value.Axial.MapType, MapUnit: value.Axial.MapUnit, Extend: value.Axial.Extend, StartPoint: value.Axial.StartPoint, EndPoint: value.Axial.EndPoint, Segments: segments}
	}
	if value.Radial != nil {
		segments, err := buildColorStops(value.Radial.Segments)
		if err != nil {
			return nil, fmt.Errorf("radial.segments: %w", err)
		}
		result.Radial = &creator.RadialShading{MapType: value.Radial.MapType, MapUnit: value.Radial.MapUnit, Eccentricity: value.Radial.Eccentricity, Angle: value.Radial.Angle, StartPoint: value.Radial.StartPoint, StartRadius: value.Radial.StartRadius, EndPoint: value.Radial.EndPoint, EndRadius: value.Radial.EndRadius, Extend: value.Radial.Extend, Segments: segments}
	}
	if value.Gouraud != nil {
		points := make([]creator.GouraudPoint, 0, len(value.Gouraud.Points))
		for index, point := range value.Gouraud.Points {
			color, err := buildColor(&point.Color)
			if err != nil {
				return nil, fmt.Errorf("gouraud.points[%d].color: %w", index, err)
			}
			points = append(points, creator.GouraudPoint{X: point.X, Y: point.Y, EdgeFlag: point.EdgeFlag, Color: *color})
		}
		backColor, err := buildColor(value.Gouraud.BackColor)
		if err != nil {
			return nil, fmt.Errorf("gouraud.back_color: %w", err)
		}
		result.Gouraud = &creator.GouraudShading{Extend: value.Gouraud.Extend, Points: points, BackColor: backColor}
	}
	if value.LaGouraud != nil {
		points := make([]creator.LaGouraudPoint, 0, len(value.LaGouraud.Points))
		for index, point := range value.LaGouraud.Points {
			color, err := buildColor(&point.Color)
			if err != nil {
				return nil, fmt.Errorf("la_gouraud.points[%d].color: %w", index, err)
			}
			points = append(points, creator.LaGouraudPoint{X: point.X, Y: point.Y, Color: *color})
		}
		backColor, err := buildColor(value.LaGouraud.BackColor)
		if err != nil {
			return nil, fmt.Errorf("la_gouraud.back_color: %w", err)
		}
		result.LaGouraud = &creator.LaGouraudShading{VerticesPerRow: value.LaGouraud.VerticesPerRow, Extend: value.LaGouraud.Extend, Points: points, BackColor: backColor}
	}
	if value.Pattern != nil {
		items, err := buildItems(value.Pattern.Items)
		if err != nil {
			return nil, fmt.Errorf("pattern.items: %w", err)
		}
		layers, err := buildLayers(value.Pattern.Layers)
		if err != nil {
			return nil, fmt.Errorf("pattern.layers: %w", err)
		}
		ctm, err := buildCTM(value.Pattern.CTM)
		if err != nil {
			return nil, fmt.Errorf("pattern.ctm: %w", err)
		}
		result.Pattern = &creator.Pattern{Width: value.Pattern.Width, Height: value.Pattern.Height, XStep: value.Pattern.XStep, YStep: value.Pattern.YStep, ReflectMethod: value.Pattern.ReflectMethod, RelativeTo: value.Pattern.RelativeTo, CTM: ctm, Thumbnail: value.Pattern.Thumbnail, Items: items, Layers: layers}
	}
	return result, nil
}

func buildColorStops(values []ColorStop) ([]creator.ColorStop, error) {
	result := make([]creator.ColorStop, 0, len(values))
	for index, value := range values {
		color, err := buildColor(&value.Color)
		if err != nil {
			return nil, fmt.Errorf("[%d].color: %w", index, err)
		}
		stop := creator.ColorStop{Color: *color}
		if value.Position != nil {
			stop.Position = *value.Position
			stop.PositionSet = true
		}
		result = append(result, stop)
	}
	return result, nil
}

func buildCTM(values []float64) (*creator.CTM, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) != 6 {
		return nil, errors.New("ctm 必须包含 6 个数值")
	}
	value := creator.CTM{values[0], values[1], values[2], values[3], values[4], values[5]}
	return &value, nil
}

func buildAction(value Action) (creator.Action, error) {
	result := creator.Action{Event: creator.ActionEvent(value.Event)}
	if value.Region != nil {
		region, err := buildActionRegion(value.Region)
		if err != nil {
			return creator.Action{}, err
		}
		result.Region = region
	}
	if value.URI != nil {
		result.URI = &creator.URIAction{URI: value.URI.URI, Base: value.URI.Base, Target: value.URI.Target}
	}
	if value.Goto != nil {
		gotoValue := buildGoto(*value.Goto)
		result.Goto = &gotoValue
	}
	if value.GotoA != nil {
		result.GotoA = &creator.GotoAAction{AttachID: value.GotoA.AttachID, NewWindow: value.GotoA.NewWindow}
	}
	if value.Sound != nil {
		result.Sound = &creator.SoundAction{ResourceID: value.Sound.ResourceID, Volume: value.Sound.Volume, Repeat: value.Sound.Repeat, Synchronous: value.Sound.Synchronous}
	}
	if value.Movie != nil {
		result.Movie = &creator.MovieAction{ResourceID: value.Movie.ResourceID, Operator: value.Movie.Operator}
	}
	return result, nil
}

func buildActionRegion(value *ActionRegion) (*creator.ActionRegion, error) {
	result := &creator.ActionRegion{}
	for _, area := range value.Areas {
		converted := creator.ActionArea{Start: creator.Point{X: area.Start.X, Y: area.Start.Y}}
		for _, command := range area.Commands {
			switch strings.ToLower(command.Type) {
			case "move":
				converted.Commands = append(converted.Commands, creator.RegionMove{Point: creator.Point{X: command.X, Y: command.Y}})
			case "line":
				converted.Commands = append(converted.Commands, creator.RegionLine{Point: creator.Point{X: command.X, Y: command.Y}})
			case "quadratic", "quadratic-bezier":
				converted.Commands = append(converted.Commands, creator.RegionQuadraticBezier{Control: creator.Point{X: command.ControlX, Y: command.ControlY}, End: creator.Point{X: command.X, Y: command.Y}})
			case "cubic", "cubic-bezier":
				converted.Commands = append(converted.Commands, creator.RegionCubicBezier{Control1: creator.Point{X: command.Control1X, Y: command.Control1Y}, Control2: creator.Point{X: command.Control2X, Y: command.Control2Y}, End: creator.Point{X: command.X, Y: command.Y}})
			case "arc":
				converted.Commands = append(converted.Commands, creator.RegionArc{SweepDirection: command.SweepDirection, LargeArc: command.LargeArc, RotationAngle: command.RotationAngle, EllipseSize: creator.Point{X: command.EllipseWidth, Y: command.EllipseHeight}, EndPoint: creator.Point{X: command.X, Y: command.Y}})
			case "close":
				converted.Commands = append(converted.Commands, creator.RegionClose{})
			default:
				return nil, fmt.Errorf("不支持的动作区域命令类型 %q", command.Type)
			}
		}
		result.Areas = append(result.Areas, converted)
	}
	return result, nil
}

func buildActions(values []Action) ([]creator.Action, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]creator.Action, 0, len(values))
	for index, value := range values {
		converted, err := buildAction(value)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", index, err)
		}
		result = append(result, converted)
	}
	return result, nil
}

func buildOutlines(values []Outline) ([]creator.Outline, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]creator.Outline, 0, len(values))
	for index, value := range values {
		actions, err := buildActions(value.Actions)
		if err != nil {
			return nil, fmt.Errorf("[%d].actions: %w", index, err)
		}
		children, err := buildOutlines(value.Children)
		if err != nil {
			return nil, fmt.Errorf("[%d].children: %w", index, err)
		}
		result = append(result, creator.Outline{Title: value.Title, Count: value.Count, Expanded: value.Expanded, Actions: actions, Children: children})
	}
	return result, nil
}

func (s assetStore) buildSignature(value Signature, stream bool) (creator.Signature, error) {
	result := creator.Signature{ID: value.ID, Type: value.Type, ProviderName: value.ProviderName, ProviderVersion: value.ProviderVersion, Company: value.Company, Method: value.Method, CheckMethod: value.CheckMethod, SealName: value.SealName, SignedValueName: value.SignedValueName}
	var err error
	if value.Date != "" {
		result.Date, err = parseDate(value.Date)
		if err != nil {
			return creator.Signature{}, fmt.Errorf("date: %w", err)
		}
	}
	if value.SealFile != "" || value.SealFileBase64 != "" {
		result.SealFile, result.SealSource, err = s.loadOptional(value.SealFile, value.SealFileBase64, stream)
		if err != nil {
			return creator.Signature{}, fmt.Errorf("seal_file: %w", err)
		}
	}
	if value.SignedValue != "" {
		result.SignedValue, err = decodeBytes(value.SignedValue)
		if err != nil {
			return creator.Signature{}, fmt.Errorf("signed_value: %w", err)
		}
	}
	for index, reference := range value.References {
		checkValue, err := decodeBytes(reference.CheckValue)
		if err != nil {
			return creator.Signature{}, fmt.Errorf("references[%d].check_value: %w", index, err)
		}
		result.References = append(result.References, creator.SignatureReference{FileRef: reference.FileRef, CheckValue: checkValue})
	}
	for _, stamp := range value.StampAnnots {
		result.StampAnnots = append(result.StampAnnots, creator.SignatureStamp{ID: stamp.ID, Page: stamp.Page, Boundary: creator.Box{X: stamp.Boundary.X, Y: stamp.Boundary.Y, Width: stamp.Boundary.Width, Height: stamp.Boundary.Height}, Clip: buildBox(stamp.Clip)})
	}
	return result, nil
}

func decodeBytes(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("base64 数据无效: %w", err)
	}
	return data, nil
}

func buildGoto(value GotoAction) creator.GotoAction {
	return creator.GotoAction{Page: value.Page, Type: value.Type, Bookmark: value.Bookmark, Left: value.Left, Top: value.Top, Right: value.Right, Bottom: value.Bottom, Zoom: value.Zoom}
}

func (value Annotation) build() (creator.Annotation, error) {
	items, err := buildItems(value.Items)
	if err != nil {
		return creator.Annotation{}, fmt.Errorf("items: %w", err)
	}
	result := creator.Annotation{ID: value.ID, Type: value.Type, Creator: value.Creator, Visible: value.Visible, Subtype: value.Subtype, Print: value.Print, NoZoom: value.NoZoom, NoRotate: value.NoRotate, ReadOnlyValue: value.ReadOnly, Remark: value.Remark, Items: items}
	if value.LastModDate != "" {
		date, err := parseDate(value.LastModDate)
		if err != nil {
			return creator.Annotation{}, fmt.Errorf("last_mod_date: %w", err)
		}
		result.LastModDate = date
	}
	if value.Boundary != nil {
		result.Boundary = &creator.Box{X: value.Boundary.X, Y: value.Boundary.Y, Width: value.Boundary.Width, Height: value.Boundary.Height}
	}
	for _, parameter := range value.Parameters {
		result.Parameters = append(result.Parameters, creator.AnnotationParameter{Name: parameter.Name, Value: parameter.Value})
	}
	return result, nil
}

func readAsset(root, name string) ([]byte, error) {
	path, err := resolveAssetPath(root, name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %q 失败: %w", name, err)
	}
	return data, nil
}

// loadSource 校验资源路径后返回惰性文件来源，不读取文件内容。
// 仍会检查文件存在，使创建失败尽早暴露。
func loadSource(root, name string) (creator.DataSource, error) {
	path, err := resolveAssetPath(root, name)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("读取 %q 失败: %w", name, err)
	}
	return creator.FileDataSource(path), nil
}

func resolveAssetPath(root, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("资源文件路径不能为空")
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return "", errors.New("资源文件路径必须是资源根目录下的相对路径")
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return "", fmt.Errorf("资源路径无效: %w", err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("资源路径不能离开资源根目录")
	}
	return path, nil
}

// buildFigure 把 manifest 图元转换为带说明文字的图片块。
//
// 图片数据沿用 image 图元那套字段（data / data_base64 / resource_id），
// 图块宽度沿用 width，几何由 figure 自己的字段决定。
func (i Item) buildFigure() (creator.Figure, error) {
	if i.ResourceID == 0 && i.DataBase64 == "" {
		return creator.Figure{}, errors.New("figure.resource_id 或 data_base64 至少设置一个")
	}
	if i.ResourceID != 0 && i.DataBase64 != "" {
		return creator.Figure{}, errors.New("figure.resource_id 不能与 data_base64 同时设置")
	}
	clips, err := buildClips(i.Clips)
	if err != nil {
		return creator.Figure{}, fmt.Errorf("clips: %w", err)
	}
	border, err := buildImageBorder(i.Border)
	if err != nil {
		return creator.Figure{}, fmt.Errorf("border: %w", err)
	}
	data, err := decodeOptionalBytes(i.DataBase64)
	if err != nil {
		return creator.Figure{}, fmt.Errorf("data_base64: %w", err)
	}
	ctm, err := buildCTM(i.CTM)
	if err != nil {
		return creator.Figure{}, fmt.Errorf("ctm: %w", err)
	}
	var captionStyle *creator.CaptionStyle
	if i.CaptionStyle != nil {
		captionStyle, err = i.CaptionStyle.build()
		if err != nil {
			return creator.Figure{}, fmt.Errorf("caption_style: %w", err)
		}
	}
	figure := creator.Figure{
		X:              i.X,
		Y:              i.Y,
		Width:          i.Width,
		Align:          i.Align,
		ImageWidth:     i.ImageWidth,
		ImageFillWidth: i.ImageFillWidth,
		ImageHeight:    i.ImageHeight,
		Fit:            i.Fit,
		Caption:        i.Caption,
		CaptionStyle:   captionStyle,
		Image: creator.Image{
			Data:         data,
			ResourceID:   i.ResourceID,
			Format:       i.Format,
			Name:         i.Name,
			Visible:      i.Visible,
			Substitution: i.Substitution,
			ImageMask:    i.ImageMask,
			Alpha:        i.Alpha,
			DrawParam:    i.DrawParam,
			CTM:          ctm,
			Clips:        clips,
			Border:       border,
		},
	}
	return figure, nil
}
