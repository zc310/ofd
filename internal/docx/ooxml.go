package docx

import "encoding/xml"

import "strconv"

// 本文件里的结构体是 ECMA-376 中用到的复杂类型子集。字段声明顺序必须与 order.go
// 中同名常量表的顺序一致——encoding/xml 按字段声明顺序输出，所以字段顺序即输出
// 顺序。允许只声明常量表的一部分元素（子集），order_test.go 会断言两者是子集关系
// 且相对顺序一致；将来补字段时测试会指出正确的插入位置。
//
// 属性按 tag 原样透传前缀，`xml:"w:val,attr"` 会输出 w:val="..."（已实测）。
// 元素一律作为子元素出现，它的属性放在单独的元素类型上——这样 schema 规定的
// 「元素与属性的相对位置」才不会被拆散成同级。

// Val 是「元素 + 单个 w:val 属性」的通用形态。OOXML 里 ST_String、
// ST_DecimalNumber、ST_OnOff 都是把值原样写成文本，Go 不需要区分，
// 因此统一用字符串承载，由下面的构造函数在调用点转换。
type Val struct {
	Value string `xml:"w:val,attr"`
}

// StringVal 返回承载 ST_String 的 w:val。空串返回 nil，使元素整体省略。
func StringVal(value string) *Val {
	if value == "" {
		return nil
	}
	return &Val{Value: value}
}

// IntVal 返回承载 ST_DecimalNumber 的 w:val。
func IntVal(value int) *Val { return &Val{Value: strconv.Itoa(value)} }

// BoolVal 返回承载 ST_OnOff 的 w:val，按规范用 "1"/"0" 而不是 true/false。
func BoolVal(value bool) *Val {
	if value {
		return &Val{Value: "1"}
	}
	return &Val{Value: "0"}
}

// TypeVal 是「元素 + 单个 w:type 属性」的形态。多数简单类型用 w:val，但
// w:tblLayout 用的是 w:type，两者不能互换，写错 Word 会忽略整个元素并退回
// 自动布局。
type TypeVal struct {
	Value string `xml:"w:type,attr"`
}

// Text 是 w:t 的内容模型。固定带 xml:space="preserve"，否则 Word 会吞掉 OFD
// 文本里行首行尾的有意义空格。不能直接写成 Run 里的 string 字段——Go 会把属性
// 提升到外层元素，变成 <w:r xml:space="preserve">，位置就错了。
type Text struct {
	Space string `xml:"xml:space,attr"`
	Value string `xml:",chardata"`
}

// NewText 返回保留空格的 w:t。
func NewText(value string) *Text { return &Text{Space: "preserve", Value: value} }

// Run 是 w:r。一段 run 要么承载文字，要么承载图片，两者互斥，所以两个子元素
// 都是指针：只写一个才是合法形状。
type Run struct {
	Properties *RunProperties `xml:"w:rPr,omitempty"`
	Text       *Text          `xml:"w:t,omitempty"`
	Drawing    *Drawing       `xml:"w:drawing,omitempty"`
}

// RunProperties 是 CT_RPr。
type RunProperties struct {
	Fonts  *RunFonts `xml:"w:rFonts,omitempty"`
	Bold   *Val      `xml:"w:b,omitempty"`
	Italic *Val      `xml:"w:i,omitempty"`
	Color  *Val      `xml:"w:color,omitempty"`
	// Size 是半磅（half-point）为单位的字号，即 2×pt。
	Size *Val `xml:"w:sz,omitempty"`
}

// RunFonts 是 CT_Fonts，只用 ascii 与 eastAsia 两个槽位：西文与中日韩分别交给
// 打开方的系统字体解析。OFD 侧的 FontName 直接写入这里，不嵌入字体文件。
type RunFonts struct {
	ASCII    string `xml:"w:ascii,attr,omitempty"`
	EastAsia string `xml:"w:eastAsia,attr,omitempty"`
}

// Paragraph 是 w:p。
type Paragraph struct {
	Properties *ParagraphProperties `xml:"w:pPr,omitempty"`
	Runs       []Run                `xml:"w:r"`
}

// ParagraphProperties 是 CT_PPr。
type ParagraphProperties struct {
	Style         *Val            `xml:"w:pStyle,omitempty"`
	Numbering     *NumberingProps `xml:"w:numPr,omitempty"`
	Spacing       *Spacing        `xml:"w:spacing,omitempty"`
	Indent        *Indent         `xml:"w:ind,omitempty"`
	Justification *Val            `xml:"w:jc,omitempty"`
	OutlineLevel  *Val            `xml:"w:outlineLvl,omitempty"`
}

// NumberingProps 是 CT_NumPr，引用 numbering.xml 中某个 numId 的指定层级。
type NumberingProps struct {
	Level *Val `xml:"w:ilvl,omitempty"`
	NumID *Val `xml:"w:numId,omitempty"`
}

// Spacing 是 CT_Spacing，单位为二十分之一磅（twip）。
type Spacing struct {
	Before *int `xml:"w:before,attr,omitempty"`
	After  *int `xml:"w:after,attr,omitempty"`
	Line   *int `xml:"w:line,attr,omitempty"`
	// LineRule 取 "auto"（行距倍数）或 "exact"/"atLeast"（绝对行高）。
	LineRule string `xml:"w:lineRule,attr,omitempty"`
}

// Indent 是 CT_Ind，单位同样是 twip。FirstLine 与 Hanging 互斥。
type Indent struct {
	Left      *int `xml:"w:left,attr,omitempty"`
	Right     *int `xml:"w:right,attr,omitempty"`
	FirstLine *int `xml:"w:firstLine,attr,omitempty"`
	Hanging   *int `xml:"w:hanging,attr,omitempty"`
}

// Table 是 w:tbl。Grid 给出各列宽度（twip），必须与每行的单元格数一致。
type Table struct {
	Properties *TableProperties `xml:"w:tblPr,omitempty"`
	Grid       *TableGrid       `xml:"w:tblGrid,omitempty"`
	Rows       []TableRow       `xml:"w:tr"`
}

// TableGrid 是 w:tblGrid，声明各列宽度。OOXML 的列宽同时出现在 w:tblGrid 的
// w:gridCol/@w:w 和 w:tc/w:tcPr/w:tcW/@w:w 两处，Word 以 tblGrid 为准。
type TableGrid struct {
	Columns []GridColumn `xml:"w:gridCol"`
}

// GridColumn 是 CT_TblGridCol。宽度是 w:w 属性，不是元素文本。
type GridColumn struct {
	Width int `xml:"w:w,attr"`
}

// NewTableGrid 由各列宽度（twip）构造 w:tblGrid。
func NewTableGrid(widths []int) *TableGrid {
	grid := &TableGrid{}
	for _, width := range widths {
		grid.Columns = append(grid.Columns, GridColumn{Width: width})
	}
	return grid
}

// TableRow 是 w:tr。
type TableRow struct {
	Properties *TableRowProperties `xml:"w:trPr,omitempty"`
	Cells      []TableCell         `xml:"w:tc"`
}

// TableRowProperties 是 CT_TrPr 的最小子集：只用到「跨页重复表头」。
type TableRowProperties struct {
	TableHeader *Val `xml:"w:tblHeader,omitempty"`
}

// TableCell 是 w:tc。ColumnSpan 与 RowSpan 输出为 w:gridSpan 与 w:vMerge。
type TableCell struct {
	Properties *TableCellProperties `xml:"w:tcPr,omitempty"`
	Paragraphs []Paragraph          `xml:"w:p"`
}

// TableCellProperties 是 CT_TcPr。
type TableCellProperties struct {
	Width      *Measure       `xml:"w:tcW,omitempty"`
	ColumnSpan *Val           `xml:"w:gridSpan,omitempty"`
	RowSpan    *VerticalMerge `xml:"w:vMerge,omitempty"`
	Vertical   *Val           `xml:"w:vAlign,omitempty"`
}

// VerticalMerge 是 CT_VMerge。Restart 为 true 表示开启一段新的纵向合并区。
type VerticalMerge struct {
	Restart *Val `xml:"w:val,attr,omitempty"`
}

// Measure 是 CT_TblW 与 CT_TcW 共用的宽度属性对。Value 单位是 twip。
type Measure struct {
	Value int    `xml:"w:w,attr"`
	Type  string `xml:"w:type,attr"`
}

// NewMeasure 返回 twip 单位的绝对宽度。
func NewMeasure(twips int) *Measure { return &Measure{Value: twips, Type: "dxa"} }

// TableProperties 是 CT_TblPr。
//
// 注意 w:tblStyle 是本元素的属性，而 w:tblLayout 与 w:tblLook 是子元素
// （<w:tblLayout w:type="fixed"/>）。实测踩过：把 w:tblLayout 写成属性后
// LibreOffice 会输出 <w:tblPr w:tblLayout="fixed">，表格之后的分页符随之失效。
type TableProperties struct {
	Style   string        `xml:"w:tblStyle,attr,omitempty"`
	Width   *Measure      `xml:"w:tblW"`
	Borders *TableBorders `xml:"w:tblBorders,omitempty"`
	// Layout 取 "fixed" 时严格按 w:tblGrid 分列，"autofit" 时由 Word 重算列宽。
	Layout *TypeVal `xml:"w:tblLayout,omitempty"`
	// Look 是 CT_TblLook 的十六进制开关位串，例如 "04A0" 表示首行与带状行开启。
	Look *Val `xml:"w:tblLook,omitempty"`
}

// TableBorders 是 CT_TblBorders。Size 单位是八分之一磅，Val 取边框样式名。
type TableBorders struct {
	Top     *Border `xml:"w:top,omitempty"`
	Left    *Border `xml:"w:left,omitempty"`
	Bottom  *Border `xml:"w:bottom,omitempty"`
	Right   *Border `xml:"w:right,omitempty"`
	InsideH *Border `xml:"w:insideH,omitempty"`
	InsideV *Border `xml:"w:insideV,omitempty"`
}

// Border 是 CT_Border。Color 是不带 # 的 RRGGBB。
type Border struct {
	Val   string `xml:"w:val,attr"`
	Size  int    `xml:"w:sz,attr"`
	Color string `xml:"w:color,attr"`
}

// SectionProperties 是 CT_SectPr，描述最后一节的页面设置。
type SectionProperties struct {
	PageSize   *PageSize   `xml:"w:pgSz,omitempty"`
	PageMargin *PageMargin `xml:"w:pgMar,omitempty"`
	Columns    *Columns    `xml:"w:cols,omitempty"`
}

// PageSize 是 CT_PageSz，单位 twip。Orient 取 "portrait"/"landscape"。
type PageSize struct {
	Width  int    `xml:"w:w,attr"`
	Height int    `xml:"w:h,attr"`
	Orient string `xml:"w:orient,attr,omitempty"`
}

// PageMargin 是 CT_PageMar，单位 twip。
type PageMargin struct {
	Top    int `xml:"w:top,attr"`
	Right  int `xml:"w:right,attr"`
	Bottom int `xml:"w:bottom,attr"`
	Left   int `xml:"w:left,attr"`
	Header int `xml:"w:header,attr,omitempty"`
	Footer int `xml:"w:footer,attr,omitempty"`
	Gutter int `xml:"w:gutter,attr,omitempty"`
}

// Columns 是 CT_Columns。Num 为栏数，Space 为栏间距（twip）。
type Columns struct {
	Num   int `xml:"w:num,attr,omitempty"`
	Space int `xml:"w:space,attr,omitempty"`
}

// Styles 是 CT_Styles。XMLName 与 Namespace 让本类型可作为独立 part 序列化。
type Styles struct {
	XMLName   xml.Name     `xml:"w:styles"`
	Namespace string       `xml:"xmlns:w,attr"`
	Defaults  *DocDefaults `xml:"w:docDefaults,omitempty"`
	List      []Style      `xml:"w:style"`
}

// DocDefaults 是 CT_DocDefaults，设定整篇的默认字号与中西文字体。
type DocDefaults struct {
	Runs *RunProperties `xml:"w:rPrDefault>w:rPr"`
}

// Style 是 CT_Style。Type 与 StyleID 是本元素的属性，而 w:name、w:basedOn、
// w:next 都是子元素（<w:basedOn w:val="Normal"/>），形状相似容易写反。
type Style struct {
	Type       string               `xml:"w:type,attr"`
	StyleID    string               `xml:"w:styleId,attr"`
	Name       *Val                 `xml:"w:name,omitempty"`
	BasedOn    *Val                 `xml:"w:basedOn,omitempty"`
	Next       *Val                 `xml:"w:next,omitempty"`
	Properties *ParagraphProperties `xml:"w:pPr,omitempty"`
	Runs       *RunProperties       `xml:"w:rPr,omitempty"`
	// Default 标记该类型的默认样式，同一类型只能有一个。
	Default bool `xml:"w:default,attr,omitempty"`
	// Custom 标记自定义样式，避免 Word 把它当未知内置样式丢弃。
	Custom bool `xml:"w:customStyle,attr,omitempty"`
}

// NumberingRoot 是 numbering.xml 的根。
type NumberingRoot struct {
	XMLName   xml.Name             `xml:"w:numbering"`
	Namespace string               `xml:"xmlns:w,attr"`
	Abstract  []*AbstractNumbering `xml:"w:abstractNum"`
	Numbers   []*NumberInstance    `xml:"w:num"`
}

// AbstractNumbering 是 CT_AbstractNum。
type AbstractNumbering struct {
	AbstractID int      `xml:"w:abstractNumId,attr"`
	Levels     []*Level `xml:"w:lvl"`
}

// NumberInstance 是 CT_Num。
type NumberInstance struct {
	NumID      int `xml:"w:numId,attr"`
	AbstractID int `xml:"w:abstractNumId,attr"`
}

// Level 是 CT_Lvl。
type Level struct {
	Index      int                  `xml:"w:ilvl,attr"`
	Start      *Val                 `xml:"w:start,omitempty"`
	Format     *Val                 `xml:"w:numFmt,omitempty"`
	Text       *Val                 `xml:"w:lvlText,omitempty"`
	Justify    *Val                 `xml:"w:lvlJc,omitempty"`
	Properties *ParagraphProperties `xml:"w:pPr,omitempty"`
	Runs       *RunProperties       `xml:"w:rPr,omitempty"`
}
