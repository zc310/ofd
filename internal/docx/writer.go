package docx

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/klauspost/compress/zip"
)

// WordprocessingML、OPC 关系与包级命名空间。
const (
	namespaceWordprocessing = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	namespaceRelationships  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	namespaceContentTypes   = "http://schemas.openxmlformats.org/package/2006/content-types"
	namespacePackageRels    = "http://schemas.openxmlformats.org/package/2006/relationships"
	namespaceCoreProps      = "http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
	namespaceDC             = "http://purl.org/dc/elements/1.1/"
)

// ZIP 条目名。[Content_Types].xml 排在最前是 OPC 惯例，部分阅读器依赖这一点。
const (
	partContentTypes = "[Content_Types].xml"
	partRootRels     = "_rels/.rels"
	partDocRels      = "word/_rels/document.xml.rels"
	partDocument     = "word/document.xml"
	partStyles       = "word/styles.xml"
	partNumbering    = "word/numbering.xml"
	partCoreProps    = "docProps/core.xml"
)

// 默认版式：A4 纵向、四边 25.4mm、页眉页脚 12.7mm。
const (
	defaultPageWidth  = 11906
	defaultPageHeight = 16838
	defaultMargin     = 1440
	defaultHFMargin   = 720
)

// twipsPerMillimeter 是 twip 与毫米的换算系数：1mm = 1440/25.4 twip。
const twipsPerMillimeter = 1440.0 / 25.4

// Twips 把毫米换算成 twip（1/20 磅），OOXML 里所有长度单位都是 twip。
func Twips(mm float64) int { return int(mm*twipsPerMillimeter + 0.5) }

// TwipsFromPoints 把磅换算成 twip。
func TwipsFromPoints(pt float64) int { return int(pt*20 + 0.5) }

// HalfPoints 把磅换算成 w:sz 使用的半磅单位。字号与长度不同，w:sz 取 2×pt，
// 直接传 twip 会让字号放大 10 倍（实测标题变成 160pt，一行放不下就竖排换行）。
func HalfPoints(pt float64) int { return int(pt*2 + 0.5) }

// Options 控制文档级设置。零值表示使用 A4 纵向与默认页边距。
type Options struct {
	// Title 与 Author 写入 docProps/core.xml。
	Title  string
	Author string

	PageWidth  int
	PageHeight int
	// Landscape 为 true 时交换页宽页高并写入 w:orient="landscape"。
	Landscape bool

	MarginTop    int
	MarginRight  int
	MarginBottom int
	MarginLeft   int

	// Columns 是分栏数，1 或 0 表示不分栏。
	Columns int
}

// section 返回 sectPr 的页面设置，负值字段回落到默认页边距。
func (o Options) section() *SectionProperties {
	width, height := o.PageWidth, o.PageHeight
	if width <= 0 {
		width = defaultPageWidth
	}
	if height <= 0 {
		height = defaultPageHeight
	}
	orient := ""
	if o.Landscape && width < height {
		width, height = height, width
		orient = "landscape"
	}
	// 零值回落默认：Options{} 表示「用默认版式」，而不是「页边距为 0」。
	// 确实需要零页边距时传负值以外的手段不在本结构支持范围内。
	margin := func(v, fallback int) int {
		if v <= 0 {
			return fallback
		}
		return v
	}
	sect := &SectionProperties{
		PageSize: &PageSize{Width: width, Height: height, Orient: orient},
		PageMargin: &PageMargin{
			Top:    margin(o.MarginTop, defaultMargin),
			Right:  margin(o.MarginRight, defaultMargin),
			Bottom: margin(o.MarginBottom, defaultMargin),
			Left:   margin(o.MarginLeft, defaultMargin),
			Header: defaultHFMargin,
			Footer: defaultHFMargin,
		},
	}
	if o.Columns > 1 {
		sect.Columns = &Columns{Num: o.Columns, Space: 425}
	}
	return sect
}

// Writer 逐块写出 word/document.xml。
//
// .docx 是 ZIP 容器、需要中央目录，所以无法像 PDF 那样边生成边落盘，
// 由 Write 在内部管理 ZIP 生命周期，调用方通过 build 回调追加内容。
type Writer struct {
	options Options
	zip     *zip.Writer
	encoder *xml.Encoder
	blocks  int
	closed  bool
	// images 记录已内嵌的图片。ZIP 是顺序流格式，同一时刻只能有一个条目处于
	// 打开状态，而 word/document.xml 从 build 开始就一直是打开的，所以图片
	// 字节必须先攒在内存里，等文档条目关闭后再统一写入。
	images []mediaPart
	// pictureID 是 wp:docPr/@id 的递增计数器，要求全文唯一。
	pictureID int
}

// mediaPart 是一个待写入 ZIP 的图片部件及其 OPC 关系 ID。
type mediaPart struct {
	name       string
	relationID string
	data       []byte
}

// Write 把 build 生成的文档写进 output。
//
// build 返回的错误会连同已写完的 ZIP 一并返回，但部分写入的结果仍可能留给调用方，
// 所以调用方应把 build 的错误视为最终结果，忽略其输出。
func Write(output io.Writer, options Options, build func(*Writer) error) error {
	if output == nil {
		return errors.New("未设置 DOCX 输出参数")
	}
	archive := zip.NewWriter(output)
	w := &Writer{options: options, zip: archive}

	fail := func(err error) error {
		archive.Close()
		return err
	}
	// [Content_Types].xml 必须最先写：它声明后续所有 part 的 MIME 类型。
	// 因此 numbering.xml 恒定输出（见 writeNumbering），不能在 build 结束后
	// 才决定要不要写它。
	if err := w.writeContentTypes(); err != nil {
		return fail(err)
	}
	if err := w.writeRootRels(); err != nil {
		return fail(err)
	}
	if err := w.openDocument(); err != nil {
		return fail(err)
	}

	buildErr := build(w)

	if err := w.closeDocument(); err != nil {
		return fail(err)
	}
	// 图片条目必须在 word/document.xml 关闭之后才能写，ZIP 不允许条目交错。
	for _, media := range w.images {
		if err := w.writeRawPart(media.name, media.data); err != nil {
			return fail(err)
		}
	}
	if err := w.writeDocRels(); err != nil {
		return fail(err)
	}
	if err := w.writeStyles(); err != nil {
		return fail(err)
	}
	if err := w.writeNumbering(); err != nil {
		return fail(err)
	}
	if err := w.writeCoreProps(); err != nil {
		return fail(err)
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return buildErr
}

var (
	documentName = xml.Name{Local: "w:document"}
	bodyName     = xml.Name{Local: "w:body"}
)

// openDocument 打开 word/document.xml 并写出根元素。
//
// 根元素手动写而不是靠 struct tag：Go 无法在 struct 上同时给出 w: 前缀和
// xmlns 声明，而子元素的 w: 前缀依赖根上的 xmlns:w 才能解析。
func (w *Writer) openDocument() error {
	entry, err := w.createPart(partDocument)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(entry, xml.Header); err != nil {
		return err
	}
	encoder := xml.NewEncoder(entry)
	// wp/a/pic 是内嵌图片用的 DrawingML 前缀。这里无条件声明：条件声明要求
	// 在流出正文之后再判断有没有图片，那时根元素早已写出，命名空间也就无法
	// 补上；缺声明的 XML 不是命名空间良构的，LibreOffice 直接拒绝加载
	// （实测报 "source file could not be loaded"）。
	root := xml.StartElement{
		Name: documentName,
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "xmlns:w"}, Value: namespaceWordprocessing},
			{Name: xml.Name{Local: "xmlns:r"}, Value: namespaceRelationships},
			{Name: xml.Name{Local: "xmlns:wp"}, Value: namespaceWordprocessingDrawing},
			{Name: xml.Name{Local: "xmlns:a"}, Value: namespaceDrawingML},
			{Name: xml.Name{Local: "xmlns:pic"}, Value: namespaceDrawingMLPicture},
		},
	}
	for _, token := range []xml.Token{root, xml.StartElement{Name: bodyName}} {
		if err := encoder.EncodeToken(token); err != nil {
			return err
		}
	}
	w.encoder = encoder
	return nil
}

// closeDocument 补上 sectPr 并闭合 w:body 与 w:document。
func (w *Writer) closeDocument() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.encoder == nil {
		return nil
	}
	if err := encodeBlock(w.encoder, "w:sectPr", w.options.section()); err != nil {
		return err
	}
	for _, token := range []xml.Token{
		xml.EndElement{Name: bodyName},
		xml.EndElement{Name: documentName},
	} {
		if err := w.encoder.EncodeToken(token); err != nil {
			return err
		}
	}
	return w.encoder.Flush()
}

// AddParagraph 追加一个段落。
func (w *Writer) AddParagraph(p *Paragraph) error {
	if w.encoder == nil {
		return errors.New("DOCX 写入器未打开")
	}
	if p == nil {
		p = &Paragraph{}
	}
	w.blocks++
	return encodeBlock(w.encoder, "w:p", p)
}

// AddTable 追加一个表格。列宽数量必须与每行的单元格数一致。
func (w *Writer) AddTable(t *Table) error {
	if w.encoder == nil {
		return errors.New("DOCX 写入器未打开")
	}
	if t == nil {
		return errors.New("表格为空")
	}
	if t.Grid == nil || len(t.Grid.Columns) == 0 {
		return errors.New("表格缺少列宽定义（w:tblGrid）")
	}
	for i, row := range t.Rows {
		if len(row.Cells) != len(t.Grid.Columns) {
			return fmt.Errorf("表格第%d行有%d个单元格，列宽定义了%d列",
				i+1, len(row.Cells), len(t.Grid.Columns))
		}
	}
	w.blocks++
	return encodeBlock(w.encoder, "w:tbl", t)
}

// AddPageBreak 追加一个只含分页符的段落。
func (w *Writer) AddPageBreak() error {
	if w.encoder == nil {
		return errors.New("DOCX 写入器未打开")
	}
	w.blocks++
	// w:br 是 w:r 的子元素，而 Run.Text 必填，无法用 struct 表达，只能手写。
	br := xml.StartElement{
		Name: xml.Name{Local: "w:br"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "w:type"}, Value: "page"}},
	}
	for _, token := range []xml.Token{
		xml.StartElement{Name: xml.Name{Local: "w:p"}},
		xml.StartElement{Name: xml.Name{Local: "w:r"}},
		br,
		xml.EndElement{Name: br.Name},
		xml.EndElement{Name: xml.Name{Local: "w:r"}},
		xml.EndElement{Name: xml.Name{Local: "w:p"}},
	} {
		if err := w.encoder.EncodeToken(token); err != nil {
			return err
		}
	}
	return nil
}

// 图片缓冲的额度上限。DOCX 会被 Word 整份载入内存，而 .docx 转换目前是同步
// 写出、不经过项目通用的限额层，所以这里自带一道闸。
const (
	maxMediaBytes      = 32 << 20 // 单张图片
	maxMediaTotalBytes = 256 << 20
)

// AddImage 内嵌一张图片并返回一个只含该图片的段落。
//
// data 是图片的原始编码字节（PNG/JPEG 等），extension 是不含点的扩展名，
// 决定 MIME 类型与 word/media 里的文件名；widthMM 与 heightMM 是显示尺寸
// （毫米），由调用方按图片在页面上的 Boundary 给出。
//
// 部件在调用时立刻写入 ZIP，条目名与关系 ID 都按顺序确定，因此文档关系表
// 与 [Content_Types].xml 的图片声明都可以在 build 结束后一次性写完。
func (w *Writer) AddImage(data []byte, extension string, widthMM, heightMM float64) error {
	if w.encoder == nil {
		return errors.New("DOCX 写入器未打开")
	}
	if len(data) == 0 {
		return errors.New("图片内容为空")
	}
	if len(data) > maxMediaBytes {
		return fmt.Errorf("图片超过 %d 字节上限", maxMediaBytes)
	}
	if !isKnownImageExtension(extension) {
		return fmt.Errorf("不支持的图片格式 %q", extension)
	}
	if !textdocPositive(widthMM) || !textdocPositive(heightMM) {
		return fmt.Errorf("图片显示尺寸无效：%g × %g mm", widthMM, heightMM)
	}

	total := 0
	for _, media := range w.images {
		total += len(media.data)
	}
	if total+len(data) > maxMediaTotalBytes {
		return fmt.Errorf("内嵌图片总量超过 %d 字节上限", maxMediaTotalBytes)
	}

	w.pictureID++
	index := len(w.images) + 1
	name := fmt.Sprintf("word/media/image%d.%s", index, extension)
	// 关系 ID 从 docxFixedRelations 之后开始：rId1 是 styles、rId2 是
	// numbering，第一张图片必须拿 rId3。
	relationID := fmt.Sprintf("rId%d", docxFixedRelations+1+len(w.images))
	w.images = append(w.images, mediaPart{name: name, relationID: relationID, data: data})

	drawing := NewInlineImage(relationID, widthMM, heightMM, w.pictureID, drawingName(index))
	return w.AddParagraph(&Paragraph{Runs: []Run{{Drawing: drawing}}})
}

// docxFixedRelations 是固定占用前几个的关系 ID 数量（styles 与 numbering），
// 图片关系从其后开始分配，避免冲突。
const docxFixedRelations = 2

// writeRawPart 把原始字节写成 ZIP 条目。
func (w *Writer) writeRawPart(name string, data []byte) error {
	entry, err := w.createPart(name)
	if err != nil {
		return err
	}
	if _, err := entry.Write(data); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", name, err)
	}
	return nil
}

// isKnownImageExtension 报告扩展名是否是 [Content_Types].xml 里预声明的图形格式。
func isKnownImageExtension(extension string) bool {
	_, ok := imageContentTypes[extension]
	return ok
}

// imageContentTypes 是预声明的图形格式。声明成 Default 而不是逐张 Override，
// 这样 [Content_Types].xml 可以在 build 之前写出（OPC 惯例要求它排最前），
// 图片部件的数量与格式都不影响包结构。
var imageContentTypes = map[string]string{
	"png":  "image/png",
	"jpeg": "image/jpeg",
	"jpg":  "image/jpeg",
	"gif":  "image/gif",
	"bmp":  "image/bmp",
	"tif":  "image/tiff",
	"tiff": "image/tiff",
	"emf":  "image/x-emf",
	"wmf":  "image/x-wmf",
}

func textdocPositive(value float64) bool {
	return value > 0 && value == value
}

// createPart 打开一个新的 ZIP 条目。Modified 取零值让输出可复现。
func (w *Writer) createPart(name string) (io.Writer, error) {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Time{}}
	entry, err := w.zip.CreateHeader(header)
	if err != nil {
		return nil, fmt.Errorf("创建 DOCX 部件 %s 失败: %w", name, err)
	}
	return entry, nil
}

// contentTypes 是 [Content_Types].xml 的根。
type contentTypes struct {
	XMLName   xml.Name          `xml:"Types"`
	Namespace string            `xml:"xmlns,attr"`
	Defaults  []contentDefault  `xml:"Default"`
	Overrides []contentOverride `xml:"Override"`
}

type contentDefault struct {
	Extension   string `xml:"Extension,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type contentOverride struct {
	PartName    string `xml:"PartName,attr"`
	ContentType string `xml:"ContentType,attr"`
}

func (w *Writer) writeContentTypes() error {
	payload := &contentTypes{
		Namespace: namespaceContentTypes,
		// 基础两项 + 预声明的图形格式。图片扩展名按固定顺序声明，
		// 保证同一份输入产出字节相同的包。
		Defaults: append([]contentDefault{
			{Extension: "rels", ContentType: "application/vnd.openxmlformats-package.relationships+xml"},
			{Extension: "xml", ContentType: "application/xml"},
		}, imageDefaults()...),
		Overrides: []contentOverride{
			{PartName: "/word/document.xml", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"},
			{PartName: "/word/styles.xml", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"},
		},
	}
	if w.options.Title != "" || w.options.Author != "" {
		payload.Overrides = append(payload.Overrides, contentOverride{
			PartName:    "/docProps/core.xml",
			ContentType: "application/vnd.openxmlformats-package.core-properties+xml",
		})
	}
	payload.Overrides = append(payload.Overrides, contentOverride{
		PartName:    "/word/numbering.xml",
		ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml",
	})
	return w.writePart(partContentTypes, payload)
}

// relationship 是 OPC 关系项。
type relationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

type relationships struct {
	XMLName   xml.Name       `xml:"Relationships"`
	Namespace string         `xml:"xmlns,attr"`
	List      []relationship `xml:"Relationship"`
}

// imageDefaults 返回预声明的图形格式 Default 项，顺序固定。
func imageDefaults() []contentDefault {
	out := make([]contentDefault, 0, len(imageContentTypes))
	for _, extension := range imageExtensionOrder {
		out = append(out, contentDefault{Extension: extension, ContentType: imageContentTypes[extension]})
	}
	return out
}

// imageExtensionOrder 是 imageContentTypes 的固定遍历顺序。
var imageExtensionOrder = []string{"png", "jpeg", "jpg", "gif", "bmp", "tif", "tiff", "emf", "wmf"}

func (w *Writer) writeRootRels() error {
	list := []relationship{{
		ID:     "rId1",
		Type:   namespaceRelationships + "/officeDocument",
		Target: "word/document.xml",
	}}
	if w.options.Title != "" || w.options.Author != "" {
		list = append(list, relationship{
			ID:     "rId2",
			Type:   namespacePackageRels + "/metadata/core-properties",
			Target: "docProps/core.xml",
		})
	}
	return w.writePart(partRootRels, &relationships{
		Namespace: namespacePackageRels,
		List:      list,
	})
}

// writeDocRels 声明 document.xml 指向 styles.xml 与 numbering.xml 的关系。
func (w *Writer) writeDocRels() error {
	list := []relationship{{
		ID:     "rId1",
		Type:   namespaceRelationships + "/styles",
		Target: "styles.xml",
	}}
	list = append(list, relationship{
		ID:     "rId2",
		Type:   namespaceRelationships + "/numbering",
		Target: "numbering.xml",
	})
	// 图片关系的目标是相对 word/document.xml 的路径，去掉 "word/" 前缀。
	for _, media := range w.images {
		list = append(list, relationship{
			ID:     media.relationID,
			Type:   namespaceRelationships + "/image",
			Target: strings.TrimPrefix(media.name, "word/"),
		})
	}
	return w.writePart(partDocRels, &relationships{
		Namespace: namespacePackageRels,
		List:      list,
	})
}

// headingSizes 是 Heading1..6 的字号梯度（磅）。必须用定长切片而非 map，
// map 迭代顺序随机会让同一份输入产出字节不同的 styles.xml。
var headingSizes = []int{16, 14, 13, 12, 11, 10}

// writeStyles 输出 docDefaults 与 Normal、Heading1..6。固定集合便于输出可复现。
func (w *Writer) writeStyles() error {
	payload := &Styles{
		XMLName:   xml.Name{Local: "w:styles"},
		Namespace: namespaceWordprocessing,
		Defaults: &DocDefaults{Runs: &RunProperties{
			Fonts: &RunFonts{ASCII: "Calibri", EastAsia: "宋体"},
			Size:  IntVal(21), // 半磅，即 10.5pt
		}},
	}
	payload.List = append(payload.List, Style{
		Type: "paragraph", StyleID: "Normal", Name: StringVal("Normal"), Default: true,
		Properties: &ParagraphProperties{Spacing: &Spacing{Before: intPtr(0), After: intPtr(0)}},
	})
	for i, size := range headingSizes {
		level := i + 1
		payload.List = append(payload.List, Style{
			Type: "paragraph", StyleID: fmt.Sprintf("Heading%d", level),
			Name:    StringVal(fmt.Sprintf("heading %d", level)),
			BasedOn: StringVal("Normal"), Next: StringVal("Normal"),
			Properties: &ParagraphProperties{
				Justification: StringVal("left"),
				OutlineLevel:  IntVal(level - 1),
				Spacing:       &Spacing{Before: intPtr(120), After: intPtr(120)},
			},
			Runs: &RunProperties{Bold: BoolVal(true), Size: IntVal(HalfPoints(float64(size)))},
		})
	}
	return w.writePart(partStyles, payload)
}

// writeNumbering 输出一个十进制单层编号定义，供 w:numId=1 引用。
//
// 恒定输出，不按需生成：[Content_Types].xml 要在 build 之前写出并声明全部
// part，而编号部件是否存在只有 build 跑完才知道。一个未被引用的编号定义对
// Word 无副作用，代价只有几百字节，换来的是包结构完全确定、可复现。
func (w *Writer) writeNumbering() error {
	payload := &NumberingRoot{
		XMLName:   xml.Name{Local: "w:numbering"},
		Namespace: namespaceWordprocessing,
		Abstract: []*AbstractNumbering{{
			AbstractID: 0,
			Levels: []*Level{{
				Index:  0,
				Start:  IntVal(1),
				Format: StringVal("decimal"),
				Text:   StringVal("%1."),
				Properties: &ParagraphProperties{
					Indent: &Indent{Left: intPtr(420), Hanging: intPtr(420)},
				},
			}},
		}},
		Numbers: []*NumberInstance{{NumID: 1, AbstractID: 0}},
	}
	return w.writePart(partNumbering, payload)
}

// coreProps 是 docProps/core.xml 的根。
type coreProps struct {
	XMLName xml.Name `xml:"cp:coreProperties"`
	CP      string   `xml:"xmlns:cp,attr"`
	DC      string   `xml:"xmlns:dc,attr"`
	Title   *dcText  `xml:"dc:title,omitempty"`
	Creator *dcText  `xml:"dc:creator,omitempty"`
}

type dcText struct {
	Value string `xml:",chardata"`
}

// writeCoreProps 输出文档属性，标题与作者都为空时整体省略该 part。
func (w *Writer) writeCoreProps() error {
	if w.options.Title == "" && w.options.Author == "" {
		return nil
	}
	payload := &coreProps{CP: namespaceCoreProps, DC: namespaceDC}
	if w.options.Title != "" {
		payload.Title = &dcText{Value: w.options.Title}
	}
	if w.options.Author != "" {
		payload.Creator = &dcText{Value: w.options.Author}
	}
	return w.writePart(partCoreProps, payload)
}

// writePart 序列化一个静态 part：写 XML 声明、缩进两格、封口。
func (w *Writer) writePart(name string, payload any) error {
	entry, err := w.createPart(name)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(entry, xml.Header); err != nil {
		return err
	}
	encoder := xml.NewEncoder(entry)
	// 缩进只影响 part 之间的空白；正文 document.xml 走流式编码，不缩进，
	// 免得在 w:t 的文本里插入空白。
	encoder.Indent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		return err
	}
	return encoder.Flush()
}

// encodeBlock 序列化一个块级元素。Go 的 EncodeElement 要求显式给出起始元素，
// 这里集中一处，保证正文里每个块都带 w: 前缀。
func encodeBlock(encoder *xml.Encoder, name string, value any) error {
	return encoder.EncodeElement(value, xml.StartElement{Name: xml.Name{Local: name}})
}

func intPtr(v int) *int { return &v }
