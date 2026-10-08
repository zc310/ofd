package layout

import (
	"fmt"
	"math"
	"strings"

	"github.com/zc310/ofd/pkg/creator"
)

// mmPerPoint 是 1 磅对应的毫米数。
const mmPerPoint = 25.4 / 72.0

// headingScale 是 1-6 级标题相对于正文字号的倍率。
var headingScale = [6]float64{1.8, 1.5, 1.25, 1.1, 0.95, 0.85}

// 默认配色。纯黑文字在屏幕上偏硬，正文使用近黑色。
var (
	colorText   = &creator.Color{R: 0x1f, G: 0x23, B: 0x28}
	colorLink   = &creator.Color{R: 0x0b, G: 0x57, B: 0xd0}
	colorCode   = &creator.Color{R: 0xa6, G: 0x1e, B: 0x4e}
	colorQuote  = &creator.Color{R: 0x57, G: 0x60, B: 0x6a}
	colorCodeBG = &creator.Color{R: 0xf4, G: 0xf5, B: 0xf7}
	// colorCodeLabel 是代码块语言标签的文字色，比代码正文更浅。
	colorCodeLabel = &creator.Color{R: 0x6b, G: 0x74, B: 0x80}
	// colorInlineCodeBG 是行内代码的底色，比代码块面板略深。
	colorInlineCodeBG = &creator.Color{R: 0xe9, G: 0xec, B: 0xf0}
	colorRule         = &creator.Color{R: 0xd0, G: 0xd7, B: 0xde}
	colorBorder       = &creator.Color{R: 0xc4, G: 0xcb, B: 0xd1}
	colorHeaderBG     = &creator.Color{R: 0xef, G: 0xf2, B: 0xf5}
	// colorRed 是公文版头的机关标志和分隔线颜色。
	colorRed = &creator.Color{R: 0xcc, G: 0x00, B: 0x00}
)

// atom 是断行的最小单位：一个单词、一段空白或一个全角字符。
type atom struct {
	text  string
	key   metricKey
	size  float64 // 毫米
	color *creator.Color
	width float64
	space bool
	// glue 大于 0 时，相同值的相邻段不允许断开，用于行内代码。
	glue int
	// strike 为真时在该字符段中部绘制删除线。
	strike bool
	// strikeWidth 是删除线应覆盖的宽度（不含合并进来的尾随空白）；为 0 时用 width。
	strikeWidth float64
}

// engine 在排版过程中维护当前页面、纵向游标和字体资源。
type engine struct {
	opts Options

	pages     []creator.Page
	pageIndex int

	y             float64
	contentTop    float64
	contentBottom float64
	contentLeft   float64
	contentRight  float64
	contentWidth  float64
	contentHeight float64

	fontNames map[metricKey]string
	fonts     []creator.Font
	footer    *Footer

	// official 表示按 GB/T 9704-2012 公文版式编排（设置了 Letterhead 时启用）。
	official bool
	// officialBodyStarted 记录公文正文是否已开始，用于识别主送机关行。
	officialBodyStarted bool
	// afterLandscape 表示刚结束一个横排表格区块；其后的纵向排版内容
	// 应在横排页之后另起一页，避免纵向文字落在横排页上与表格重叠。
	afterLandscape bool
	// lastKind 是上一块的类型，用于在引用和表格之间补间距。
	lastKind Kind
	// pageBase 是新页创建后已有的元素数（页码等页脚元素）。判断"本页是否已
	// 排入正文"时要与它比较，否则 newPage 写下的页码会让空白页看起来非空。
	pageBase int
}

// Build 把流式文档排版为 OFD 文档。返回的文档可继续补充元数据后写入。
func Build(doc *Document, opts Options) (*creator.Document, error) {
	if doc == nil {
		return nil, fmt.Errorf("文档为空")
	}
	// 只补齐未设置的字段，避免因为个别字段缺省而丢弃调用方已设置的参数。
	defaults := DefaultOptions()
	if opts.PageWidth <= 0 {
		opts.PageWidth = defaults.PageWidth
	}
	if opts.PageHeight <= 0 {
		opts.PageHeight = defaults.PageHeight
	}
	if opts.BodySize <= 0 {
		opts.BodySize = defaults.BodySize
	}
	if opts.MonoSize <= 0 {
		opts.MonoSize = defaults.MonoSize
	}
	if opts.LineHeight <= 0 {
		opts.LineHeight = defaults.LineHeight
	}
	if opts.CodeLineHeight <= 0 {
		opts.CodeLineHeight = defaults.CodeLineHeight
	}
	if opts.BodyFamily == "" {
		opts.BodyFamily = defaults.BodyFamily
	}
	if opts.MonoFamily == "" {
		opts.MonoFamily = defaults.MonoFamily
	}
	e := &engine{opts: opts, pageIndex: -1, fontNames: make(map[metricKey]string), footer: doc.Footer, official: doc.Letterhead != nil}
	e.computeArea()
	if doc.Letterhead != nil && (doc.Letterhead.Org != "" || doc.Letterhead.DocNo != "") {
		e.emitLetterhead(doc.Letterhead)
	}
	for i := range doc.Blocks {
		e.emitBlock(&doc.Blocks[i])
	}
	if doc.Sign != nil && (doc.Sign.Org != "" || doc.Sign.Date != "") {
		e.emitSignature(doc.Sign)
	}
	if doc.Colophon != nil && (doc.Colophon.Cc != "" || doc.Colophon.IssuedBy != "" || doc.Colophon.IssuedDate != "") {
		e.emitColophon(doc.Colophon)
	}
	if e.pageIndex < 0 {
		e.newPage()
	}
	return &creator.Document{
		ID:       "markdown",
		Title:    doc.Title,
		PageSize: creator.PageSize{Width: opts.PageWidth, Height: opts.PageHeight},
		Fonts:    e.fonts,
		Pages:    e.pages,
	}, nil
}

func (e *engine) computeArea() {
	o := e.opts
	e.contentTop = o.PageHeight - o.MarginTop
	e.contentBottom = o.MarginBottom
	e.contentLeft = o.MarginLeft
	e.contentRight = o.PageWidth - o.MarginRight
	e.contentWidth = e.contentRight - e.contentLeft
	e.contentHeight = e.contentTop - e.contentBottom
}

func (e *engine) newPage() {
	e.pages = append(e.pages, creator.Page{
		Area: &creator.PageArea{
			PhysicalBox: &creator.Box{Width: e.opts.PageWidth, Height: e.opts.PageHeight},
		},
	})
	e.pageIndex = len(e.pages) - 1
	e.y = e.contentTop
	if e.footer != nil && e.footer.PageNumber {
		e.emitPageNumber()
	}
	// 页脚元素先于正文落下，记录此时的元素数作为"本页尚无正文"的基线。
	e.pageBase = len(e.cur().Items)
}

// emitPageNumber 在版心下边缘之下渲染 "— 页数 —" 页码。
// 奇数页居右空一字、偶数页居左空一字，对应 GB/T 9704-2012 的页码位置。
func (e *engine) emitPageNumber() {
	size := ptToMM(e.footer.Size)
	if e.footer.Size <= 0 {
		size = ptToMM(14)
	}
	key := metricKey{fam: famSong}
	label := fmt.Sprintf("— %d —", e.pageIndex+1)
	width := measureWidth(label, size, key)
	x := e.contentLeft + size
	if e.pageIndex%2 == 0 {
		// 0 基偶数为奇数页，页码居右空一字。
		x = e.contentRight - size - width
	}
	baseline := e.contentBottom - 4
	if e.official {
		// GB/T 9704-2012 7.5：一字线上距版心下边缘 7mm。
		baseline = e.contentBottom - 7 - ascent(size, key)
	}
	fill := true
	e.add(creator.Text{
		X:         x,
		Y:         e.opts.PageHeight - baseline - size,
		Width:     width,
		Height:    size,
		Value:     label,
		Font:      e.fontName(key),
		Size:      size,
		Fill:      &fill,
		FillColor: colorText,
	})
}

func (e *engine) ensurePage() {
	if e.pageIndex < 0 {
		e.newPage()
	}
}

// breakAfterLandscape 在横排表格结束、后续内容开始排版时新起一个纵向页面，
// 保证随后的纵向文字不会落到横排页上与表格重叠。
func (e *engine) breakAfterLandscape() {
	if !e.afterLandscape {
		return
	}
	e.afterLandscape = false
	e.newPage()
}

func (e *engine) cur() *creator.Page { return &e.pages[e.pageIndex] }

func (e *engine) add(item creator.Item) {
	e.ensurePage()
	e.cur().Items = append(e.cur().Items, item)
}

// ensureHeight 在剩余空间不足时换页；已排入正文的页面才会触发换页，避免空页。
func (e *engine) ensureHeight(height float64) {
	if e.pageIndex < 0 {
		e.newPage()
		return
	}
	if e.y-height < e.contentBottom-0.01 && len(e.cur().Items) > e.pageBase {
		e.newPage()
	}
}

func (e *engine) space(height float64) {
	if e.pageIndex < 0 || height <= 0 {
		return
	}
	e.y -= height
	if e.y < e.contentBottom {
		e.y = e.contentBottom
	}
}

func (e *engine) indentStep() float64 { return 6 }

func (e *engine) blockGap() float64 {
	return ptToMM(e.opts.BodySize) * e.opts.BlockSpacing
}

// fontName 返回样式对应的逻辑字体资源名，并按需登记字体资源。
func (e *engine) fontName(key metricKey) string {
	if name, ok := e.fontNames[key]; ok {
		return name
	}
	family := e.opts.BodyFamily
	switch {
	case key.mono:
		family = e.opts.MonoFamily
	case key.fam == famHei && e.opts.HeiFamily != "":
		family = e.opts.HeiFamily
	case key.fam == famKai && e.opts.KaiFamily != "":
		family = e.opts.KaiFamily
	case key.fam == famTitle && e.opts.TitleFamily != "":
		family = e.opts.TitleFamily
	case key.fam == famSong && e.opts.SongFamily != "":
		family = e.opts.SongFamily
	}
	name := fmt.Sprintf("MD-%d", len(e.fonts))
	e.fonts = append(e.fonts, creator.Font{
		Name:       name,
		FamilyName: family,
		Charset:    "unicode",
		Bold:       key.bold,
		Italic:     key.italic,
		FixedWidth: key.mono,
	})
	e.fontNames[key] = name
	return name
}

// emitLetterhead 在首页版心顶部渲染红色版头，各要素位置对应 GB/T 9704-2012：
//   - 份号、密级与紧急程度顶格版心左上角分行排列；
//   - 发文机关标志居中，上边缘距版心上边缘 35mm；
//   - 发文字号在机关标志下空二行，上行文与签发人同一行，签发人居右空一字；
//   - 红色分隔线与版心等宽，印在发文字号之下 4mm。
func (e *engine) emitLetterhead(lh *Letterhead) {
	orgSize := ptToMM(lh.OrgSize)
	if lh.OrgSize <= 0 {
		orgSize = ptToMM(56)
	}
	docNoSize := ptToMM(lh.DocNoSize)
	if lh.DocNoSize <= 0 {
		docNoSize = ptToMM(16)
	}
	issueSize := docNoSize

	e.ensurePage()
	top := e.y
	// 行距取自版心正文（GB/T："空二行"按版心行距计）。
	lineSpace := ptToMM(e.opts.BodySize) * e.opts.LineHeight

	// 涉密类标记：顶格版心左上角，份号、密级和保密期限、紧急程度自上而下分行；
	// 份号用三号数字（仿宋），密级和紧急程度用三号黑体。
	orgKey := metricKey{fam: famTitle}
	docNoKey := metricKey{}
	markHeiKey := metricKey{fam: famHei}
	var markBottom float64
	hasMarks := false
	row := 0.0
	for index, label := range []string{lh.SerialNo, lh.Security, lh.Urgency} {
		if label == "" {
			continue
		}
		key := docNoKey
		if index > 0 {
			key = markHeiKey
		}
		baseline := top - row*lineSpace - ascent(issueSize, key)
		width := measureWidth(label, issueSize, key)
		e.addText(label, e.contentLeft, baseline, atom{key: key, size: issueSize, color: colorText, width: width})
		markBottom = baseline - descent(issueSize, key)
		row++
		hasMarks = true
	}

	// 发文机关标志：居中，上边缘距版心上边缘 35mm；涉密标记较高时下移保持 4mm 间距。
	orgTop := top - 35
	if hasMarks && markBottom < orgTop-4 {
		orgTop = markBottom - 4
	}
	var orgBottom float64
	if lh.Org != "" {
		width := measureWidth(lh.Org, orgSize, orgKey)
		x := e.contentLeft + (e.contentWidth-width)/2
		baseline := orgTop - ascent(orgSize, orgKey)
		e.addText(lh.Org, x, baseline, atom{key: orgKey, size: orgSize, color: colorRed, width: width})
		orgBottom = baseline - descent(orgSize, orgKey)
	} else {
		orgBottom = top
	}

	// 发文字号：机关标志下空二行；上行文居左空一字、签发人居右空一字（同一行）。
	docBottom := orgBottom
	if lh.DocNo != "" || lh.Signatory != "" {
		docTop := orgBottom - 2*lineSpace
		baseline := docTop - ascent(docNoSize, docNoKey)
		docNoWidth := 0.0
		if lh.DocNo != "" {
			docNoWidth = measureWidth(lh.DocNo, docNoSize, docNoKey)
		}
		// "签发人："用仿宋、"姓名"用三号楷体，整段右空一字。
		signPrefix := "签发人："
		signNameKey := metricKey{fam: famKai}
		signPrefixWidth := measureWidth(signPrefix, docNoSize, docNoKey)
		signNameWidth := 0.0
		if lh.Signatory != "" {
			signNameWidth = measureWidth(lh.Signatory, docNoSize, signNameKey)
		}
		totalWidth := signPrefixWidth + signNameWidth
		if totalWidth > 0 {
			e.addText(lh.DocNo, e.contentLeft+docNoSize, baseline, atom{key: docNoKey, size: docNoSize, color: colorText, width: docNoWidth})
			signX := e.contentRight - docNoSize - totalWidth
			e.addText(signPrefix, signX, baseline, atom{key: docNoKey, size: docNoSize, color: colorText, width: signPrefixWidth})
			if lh.Signatory != "" {
				e.addText(lh.Signatory, signX+signPrefixWidth, baseline, atom{key: signNameKey, size: docNoSize, color: colorText, width: signNameWidth})
			}
		} else if lh.DocNo != "" {
			x := e.contentLeft + (e.contentWidth-docNoWidth)/2
			e.addText(lh.DocNo, x, baseline, atom{key: docNoKey, size: docNoSize, color: colorText, width: docNoWidth})
		}
		docBottom = baseline - descent(docNoSize, docNoKey)
	}

	// 红色分隔线：发文字号之下 4mm，与版心等宽。
	e.fillRect(e.contentLeft, docBottom-4-0.5, e.contentWidth, 0.5, colorRed)
	e.y = docBottom - 4 - 1
}

// emitColophon 在正文流末尾渲染公文版记：抄送行与印发机关/日期行，行间用线分隔。
// 版记排在末页版心最下方（GB/T 9704-2012 7.4）：
// 首条、末条分隔线用粗线（0.35mm）、中间分隔线用细线（0.25mm），
// 线与版心等宽；抄送左空一字，印发机关左空一字、印发日期右空一字。
// 末条分隔线下边缘压准版心下边缘，文字行带与分隔线之间各留空（约 0.2 字）。
func (e *engine) emitColophon(c *Colophon) {
	e.breakAfterLandscape()
	sizeMM := ptToMM(14)
	if c.Size > 0 {
		sizeMM = ptToMM(c.Size)
	}
	key := metricKey{}
	const (
		lineBoldW = 0.35
		lineThinW = 0.25
	)
	hasCc := strings.TrimSpace(c.Cc) != ""
	issuedBy := strings.TrimSpace(c.IssuedBy)
	issuedDate := strings.TrimSpace(c.IssuedDate)
	hasIssued := issuedBy != "" || issuedDate != ""
	if !hasCc && !hasIssued {
		return
	}
	band := sizeMM * 1.4 // 文字行带（文字上下各留 0.2 字）
	rows := 0
	if hasCc {
		rows++
	}
	if hasIssued {
		rows++
	}
	// 抄送行与印发机关/日期行之间才有中间细线。
	hasThin := hasCc && hasIssued
	colH := lineBoldW + band*float64(rows) + lineBoldW
	if hasThin {
		colH += lineThinW
	}
	e.ensureHeight(colH + 1)
	if e.y-colH >= e.contentBottom {
		// 版记锚定在版心最下方，与正文之间留白。
		e.y = e.contentBottom + colH
	}
	top := e.y

	// 首条分隔线（粗线）。
	cur := top
	e.fillRect(e.contentLeft, cur-lineBoldW, e.contentWidth, lineBoldW, colorBorder)
	cur -= lineBoldW

	writeRow := func(text string, x, width float64) {
		// 文字框顶距分隔线底约 0.2 字，基线在框底。
		baseline := cur - sizeMM*0.2 - sizeMM
		e.addText(text, x, baseline, atom{key: key, size: sizeMM, color: colorText, width: width})
		cur -= band
	}
	thinLine := func() {
		e.fillRect(e.contentLeft, cur-lineThinW, e.contentWidth, lineThinW, colorBorder)
		cur -= lineThinW
	}

	if hasCc {
		text := "抄送：" + strings.TrimSpace(c.Cc)
		writeRow(text, e.contentLeft+sizeMM, measureWidth(text, sizeMM, key))
		if hasThin {
			// 中间分隔线（细线）。
			thinLine()
		}
	}
	if hasIssued {
		// 印发机关居左、印发日期居右，共用同一行带，只消费一次行高。
		baseline := cur - sizeMM*0.2 - sizeMM
		if issuedBy != "" {
			width := measureWidth(issuedBy, sizeMM, key)
			e.addText(issuedBy, e.contentLeft+sizeMM, baseline, atom{key: key, size: sizeMM, color: colorText, width: width})
		}
		if issuedDate != "" {
			label := issuedDate + "印发"
			width := measureWidth(label, sizeMM, key)
			e.addText(label, e.contentRight-sizeMM-width, baseline, atom{key: key, size: sizeMM, color: colorText, width: width})
		}
		cur -= band
	}

	// 末条分隔线（粗线），下边缘压准版心下边缘。
	e.fillRect(e.contentLeft, cur-lineBoldW, e.contentWidth, lineBoldW, colorBorder)
	cur -= lineBoldW
	e.y = cur
}

// emitSignature 渲染不加盖印章公文的落款（GB/T 9704-2012 7.3.5.2）：
// 发文机关署名在正文下空一行、右空二字编排；成文日期在署名下一行，
// 首字比署名首字右移二字（署名较长时日期右空二字、署名相应右移）。
func (e *engine) emitSignature(s *Signature) {
	e.breakAfterLandscape()
	size := ptToMM(s.Size)
	if s.Size <= 0 {
		size = ptToMM(16)
	}
	key := metricKey{}
	orgWidth := measureWidth(s.Org, size, key)
	dateWidth := measureWidth(s.Date, size, key)
	lineH := size * e.opts.LineHeight
	e.ensureHeight(lineH*2 + size)

	e.y -= lineH // 正文下空一行

	var orgLeft, dateLeft float64
	if dateWidth > orgWidth {
		dateLeft = e.contentRight - 2*size - dateWidth
		orgLeft = dateLeft - 2*size
	} else {
		orgLeft = e.contentRight - 2*size - orgWidth
		dateLeft = orgLeft + 2*size
	}
	if s.Org != "" {
		e.addText(s.Org, orgLeft, e.y-ascent(size, key), atom{key: key, size: size, color: colorText, width: orgWidth})
	}
	e.y -= lineH
	if s.Date != "" {
		e.addText(s.Date, dateLeft, e.y-ascent(size, key), atom{key: key, size: size, color: colorText, width: dateWidth})
	}
}

func (e *engine) emitBlock(b *Block) {
	defer func() { e.lastKind = b.Kind }()
	e.breakAfterLandscape()
	if b.Kind == KindTable && e.lastKind == KindQuote {
		e.space(e.blockGap())
	}
	switch b.Kind {
	case KindHeading:
		e.emitHeading(b)
	case KindCode:
		e.emitCode(b)
	case KindQuote:
		e.emitQuote(b)
	case KindListItem:
		e.emitListItem(b)
	case KindThematicBreak:
		e.emitRule()
	case KindTable:
		e.emitTable(b)
	case KindImage:
		e.emitImage(b)
	default:
		if b.Marker != "" {
			e.emitListItem(b)
		} else {
			e.emitParagraph(b.Inlines, b.Indent)
		}
	}
}

func (e *engine) emitParagraph(inlines []Inline, indent int) {
	if e.official && indent == 0 {
		e.emitOfficialParagraph(inlines)
		return
	}
	step := float64(indent) * e.indentStep()
	left := e.contentLeft + step
	width := e.contentWidth - step
	if width <= 0 {
		width = e.contentWidth
		left = e.contentLeft
	}
	lines := e.wrapSegments(e.segments(inlines, e.opts.BodySize, metricKey{}), width)
	for _, line := range lines {
		e.writeLine(line, left)
	}
	e.space(e.blockGap())
}

// emitOfficialParagraph 按公文正文编排：每个自然段首行左空二字、回行顶格。
// 标题后的第一个段落若以冒号结尾，视为主送机关行顶格编排（GB/T 9704 7.3.2/7.3.3）。
func (e *engine) emitOfficialParagraph(inlines []Inline) {
	segments := e.segments(inlines, e.opts.BodySize, metricKey{})
	if len(segments) == 0 {
		return
	}
	if !e.officialBodyStarted {
		e.officialBodyStarted = true
		var joined strings.Builder
		for _, inline := range inlines {
			joined.WriteString(inline.Text)
		}
		text := strings.TrimSpace(joined.String())
		if !strings.HasSuffix(text, "：") && !strings.HasSuffix(text, ":") {
			indentFirstLine(segments, 2)
		}
	} else {
		indentFirstLine(segments, 2)
	}
	lines := e.wrapSegments(segments, e.contentWidth)
	for _, line := range lines {
		e.writeLine(line, e.contentLeft)
	}
}

// indentFirstLine 给段落首行前置两组全角空白，使首行左空两字、回行顶格。
func indentFirstLine(segments [][]atom, chars int) {
	if len(segments) == 0 || len(segments[0]) == 0 {
		return
	}
	first := segments[0][0]
	pad := make([]atom, 0, chars)
	for index := 0; index < chars; index++ {
		pad = append(pad, atom{text: "　", key: first.key, size: first.size, color: colorText, width: first.size, space: true})
	}
	segments[0] = append(pad, segments[0]...)
}

func (e *engine) emitHeading(b *Block) {
	if e.official {
		e.emitOfficialHeading(b)
		return
	}
	level := b.Level
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	size := e.opts.BodySize * headingScale[level-1]
	e.space(ptToMM(e.opts.BodySize) * (e.opts.BlockSpacing + 0.4))
	lines := e.wrapSegments(e.segments(b.Inlines, size, metricKey{bold: true}), e.contentWidth)
	for _, line := range lines {
		e.writeLine(line, e.contentLeft)
	}
	e.space(e.blockGap())
}

// emitOfficialHeading 按公文标题与结构层次序数编排（GB/T 9704 7.3.1/7.3.3）：
// 一级为文件标题，二号小标宋居中排布，红色分隔线下空二行；
// 二至四级为结构层次序数行，用三号字，第一层黑体、第二层楷体、其余仿宋。
func (e *engine) emitOfficialHeading(b *Block) {
	level := b.Level
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	lineSpace := ptToMM(e.opts.BodySize) * e.opts.LineHeight
	if level == 1 {
		segments := e.segments(b.Inlines, 22, metricKey{fam: famTitle})
		if len(segments) == 0 {
			return
		}
		e.y -= 2 * lineSpace // 红色分隔线下空二行
		lines := e.wrapSegments(segments, e.contentWidth)
		for _, line := range lines {
			width := e.lineWidth(line)
			e.writeLine(line, e.contentLeft+(e.contentWidth-width)/2)
		}
		e.y -= lineSpace // 标题下空一行
		return
	}
	fam := famBody
	switch level {
	case 2: // 第一层："一、"
		fam = famHei
	case 3: // 第二层："（一）"
		fam = famKai
	}
	segments := e.segments(b.Inlines, e.opts.BodySize, metricKey{fam: fam})
	if len(segments) == 0 {
		return
	}
	indentFirstLine(segments, 2)
	lines := e.wrapSegments(segments, e.contentWidth)
	for _, line := range lines {
		e.writeLine(line, e.contentLeft)
	}
}

func (e *engine) emitListItem(b *Block) {
	inlines := make([]Inline, 0, len(b.Inlines)+1)
	if b.Marker != "" {
		inlines = append(inlines, Inline{Text: b.Marker + " "})
	}
	inlines = append(inlines, b.Inlines...)
	e.emitParagraph(inlines, b.Indent)
}

// codeTabWidth 是代码块中制表符展开成的空格数。
const codeTabWidth = 4

// emitCode 绘制代码块。面板按页分段：每页能容纳的连续代码行共用一个圆角矩形，
// 跨页时下一页另起一块，这样既能拿到整块背景，又不破坏流式分页。
func (e *engine) emitCode(b *Block) {
	lines := splitCodeLines(b.Code)
	if len(lines) == 0 {
		return
	}
	size := ptToMM(e.opts.MonoSize)
	lineHeight := size * e.opts.CodeLineHeight
	key := metricKey{mono: true}
	paddingX := e.opts.CodePaddingX
	paddingY := e.opts.CodePaddingY

	// 语言标签只出现在代码块第一段，且占用面板顶部的一部分高度。
	label := strings.TrimSpace(b.CodeLang)
	labelHeight := e.codeLabelHeight(label)

	for start := 0; start < len(lines); {
		head := 0.0
		if start == 0 {
			head = labelHeight
		}
		e.ensureHeight(paddingY + head + lineHeight)
		// 面板顶部留白也要落在版心内，否则紧贴页顶时面板会伸进页边距。
		if limit := e.contentTop - paddingY; e.y > limit {
			e.y = limit
		}
		count := int(math.Floor((e.y - e.contentBottom - paddingY - head + 0.001) / lineHeight))
		if count < 1 {
			count = 1
		}
		if count > len(lines)-start {
			count = len(lines) - start
		}

		// 面板覆盖标签、本页全部代码行和上下内边距：游标自底向上，故下边界是
		// e.y 减去标签、代码行和下内边距。
		body := head + float64(count)*lineHeight
		e.fillRoundedPanel(e.contentLeft, e.y-body-paddingY, e.contentWidth, body+paddingY*2,
			e.opts.CodeRadius, colorCodeBG)
		if head > 0 {
			e.emitCodeLabel(label, e.contentLeft+paddingX, e.y, e.opts.CodeLabelSize)
		}
		for _, line := range lines[start : start+count] {
			text := expandCodeTabs(line)
			baseline := e.y - ascent(size, key)
			if strings.TrimSpace(text) != "" {
				e.addText(text, e.contentLeft+paddingX, baseline, atom{
					text:  text,
					key:   key,
					size:  size,
					color: colorCode,
					width: measureWidth(text, size, key),
				})
			}
			e.y -= lineHeight
		}
		start += count
	}
	e.space(e.blockGap())
}

// codeLabelHeight 返回语言标签连同其下间距占用的总高度；无标签时为 0。
func (e *engine) codeLabelHeight(label string) float64 {
	if label == "" || e.opts.CodeLabelSize <= 0 {
		return 0
	}
	return ptToMM(e.opts.CodeLabelSize)*e.opts.LineHeight + e.opts.CodeLabelGap
}

// emitCodeLabel 在代码面板顶部绘制语言标记。labelTop 是标签基线以上的高度起点，
// 标签用等宽字体的较小字号，与代码正文左对齐。
func (e *engine) emitCodeLabel(label string, x, top, sizePT float64) {
	size := ptToMM(sizePT)
	key := metricKey{mono: true}
	e.addText(label, x, top-ascent(size, key), atom{
		text:  label,
		key:   key,
		size:  size,
		color: colorCodeLabel,
		width: measureWidth(label, size, key),
	})
	e.y = top - size*e.opts.LineHeight - e.opts.CodeLabelGap
}

// splitCodeLines 按行拆分代码块文本，去掉尾随空行；行内保留前导空白。
func splitCodeLines(code string) []string {
	raw := strings.Split(strings.ReplaceAll(code, "\r\n", "\n"), "\n")
	for len(raw) > 0 && strings.TrimSpace(raw[len(raw)-1]) == "" {
		raw = raw[:len(raw)-1]
	}
	return raw
}

// expandCodeTabs 把行首制表符展开为固定宽度的空格，其余制表符替换为单个空格。
func expandCodeTabs(line string) string {
	if !strings.ContainsRune(line, '\t') {
		return line
	}
	var builder strings.Builder
	leading := true
	for _, r := range line {
		switch {
		case r == '\t' && leading:
			builder.WriteString(strings.Repeat(" ", codeTabWidth))
		case r == '\t':
			builder.WriteByte(' ')
		default:
			builder.WriteRune(r)
		}
		if r != ' ' && r != '\t' {
			leading = false
		}
	}
	return builder.String()
}

func (e *engine) emitQuote(b *Block) {
	segments := e.segments(b.Inlines, e.opts.BodySize, metricKey{})
	for i := range segments {
		for j := range segments[i] {
			segments[i][j].color = colorQuote
		}
	}
	step := e.indentStep()
	lines := e.wrapSegments(segments, e.contentWidth-step)
	for _, line := range lines {
		height := e.lineHeight(line)
		e.ensureHeight(height)
		e.fillRect(e.contentLeft+1, e.y-height, 1.2, height, colorRule)
		e.writeLine(line, e.contentLeft+step)
	}
	e.space(e.blockGap())
}

func (e *engine) emitRule() {
	e.space(ptToMM(e.opts.BodySize) * 0.6)
	e.ensureHeight(2)
	e.fillRect(e.contentLeft, e.y-1, e.contentWidth, 0.3, colorRule)
	e.space(ptToMM(e.opts.BodySize) * 0.6)
}

func (e *engine) emitImage(b *Block) {
	img := b.Image
	if img == nil || len(img.Data) == 0 {
		return
	}
	width := float64(img.PixelWidth) / 96 * 25.4
	height := float64(img.PixelHeight) / 96 * 25.4
	if width <= 0 || height <= 0 {
		width = e.contentWidth * 0.6
		height = width * 0.6
	}
	if width > e.contentWidth {
		ratio := e.contentWidth / width
		width = e.contentWidth
		height *= ratio
	}
	if maxHeight := e.contentTop - e.contentBottom - ptToMM(e.opts.BodySize)*2; height > maxHeight && maxHeight > 0 {
		ratio := maxHeight / height
		height = maxHeight
		width *= ratio
	}
	e.ensureHeight(height)
	e.add(creator.Image{X: e.contentLeft, Y: e.opts.PageHeight - e.y, Width: width, Height: height, Data: img.Data, Format: img.Format})
	e.y -= height
	e.space(e.blockGap())
}

func (e *engine) emitTable(b *Block) {
	table := b.Table
	if table == nil {
		return
	}
	cols := len(table.Header)
	for _, row := range table.Rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return
	}
	sizeMM := ptToMM(e.opts.BodySize)
	const padding = 1.5
	widths := make([]float64, cols)
	for index := 0; index < cols; index++ {
		width := 0.0
		if index < len(table.Header) {
			width = math.Max(width, e.cellWidth(table.Header[index], sizeMM, true))
		}
		for _, row := range table.Rows {
			if index < len(row) {
				width = math.Max(width, e.cellWidth(row[index], sizeMM, false))
			}
		}
		widths[index] = width + padding*2
	}
	total := 0.0
	for _, width := range widths {
		total += width
	}
	if table.Landscape || total > e.contentWidth {
		e.emitTableLandscape(table, widths, sizeMM, padding)
		return
	}
	extra := (e.contentWidth - total) / float64(cols)
	for index := range widths {
		widths[index] += extra
	}
	e.drawTableRow(table.Header, widths, padding, sizeMM, true, table.Align)
	for _, row := range table.Rows {
		e.drawTableRow(row, widths, padding, sizeMM, false, table.Align)
	}
	e.space(e.blockGap())
}

// emitTableLandscape 按 GB/T 9704-2012 第 8 条横排表格编排：把列方向旋转到
// 页面纵向（占版心高），行方向横跨版心宽，表头（首行）始终位于页面左侧——
// 奇数页对应订口一边、偶数页对应切口一边；页码与公文其他页码保持一致。
func (e *engine) emitTableLandscape(table *Table, widths []float64, sizeMM, padding float64) {
	cols := len(widths)
	lineHeight := sizeMM * e.opts.LineHeight
	rowHeights := make([]float64, 0, len(table.Rows)+1)
	rowHeights = append(rowHeights, e.tableRowHeight(table.Header, widths, padding, sizeMM, lineHeight, true))
	for _, row := range table.Rows {
		rowHeights = append(rowHeights, e.tableRowHeight(row, widths, padding, sizeMM, lineHeight, false))
	}
	totalW := 0.0
	for _, width := range widths {
		totalW += width
	}
	totalH := 0.0
	for _, height := range rowHeights {
		totalH += height
	}
	if totalW <= 0 || totalH <= 0 {
		return
	}
	// 整体等比缩放，使列方向不超出版心高、行方向不超出版心宽；能放下则不缩放。
	scale := math.Min(1, math.Min(e.contentHeight/totalW, e.contentWidth/totalH))
	scaledW := make([]float64, cols)
	for index := range widths {
		scaledW[index] = widths[index] * scale
	}
	scaledH := make([]float64, len(rowHeights))
	for index := range rowHeights {
		scaledH[index] = rowHeights[index] * scale
	}
	usedW := totalW * scale
	e.ensureHeight(usedW + ptToMM(e.opts.BodySize))
	// 表头（首行）居页面左侧、第 0 列在下方，列自下而上排布，读者顺时针转页阅读。
	x0 := e.contentLeft
	bottom := e.opts.PageHeight - e.y + usedW
	rowTop := 0.0
	e.drawTableRowLandscape(table.Header, scaledW, rowTop, scaledH[0], usedW, scale, padding, true, table.Align, x0, bottom)
	rowTop += scaledH[0]
	for index, row := range table.Rows {
		e.drawTableRowLandscape(row, scaledW, rowTop, scaledH[index+1], usedW, scale, padding, false, table.Align, x0, bottom)
		rowTop += scaledH[index+1]
	}
	e.y -= usedW
	e.space(e.blockGap())
	e.afterLandscape = true
}

// tableRowHeight 计算一行文字换行后占用的行高；header 为真时按加粗度量，
// 与 drawTableRowLandscape 的表头绘制方式保持一致。
func (e *engine) tableRowHeight(cells []Cell, widths []float64, padding, sizeMM, lineHeight float64, header bool) float64 {
	maxLines := 1
	for index := 0; index < len(widths); index++ {
		var cell Cell
		if index < len(cells) {
			cell = cells[index]
		}
		lines := e.wrapSegments(e.segments(cell, e.opts.BodySize, metricKey{bold: header}), math.Max(widths[index]-padding*2, sizeMM))
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	return float64(maxLines) * lineHeight
}

// drawTableRowLandscape 在横排坐标中绘制一行。列方向对应页面自下而上、行方向
// 对应页面从左向右，表头行位于页面左侧；字符直接落到最终页面位置，并以
// CharDirection=270 使字形在读者顺时针转页后保持正立。传入的宽度均已按整表
// 等比缩放（scale 是缩放系数），字符自身尺寸同样按 scale 缩放。
func (e *engine) drawTableRowLandscape(cells []Cell, widths []float64, rowTop, rowHeight, usedW, scale, padding float64, header bool, aligns []Align, x0, bottom float64) {
	cols := len(widths)
	// 换行按未缩放的自然列宽计算，分隔线/行高随后等比缩放。
	naturalW := make([]float64, cols)
	for index := range widths {
		naturalW[index] = widths[index] / scale
	}
	wrapped := make([][][]atom, cols)
	for index := 0; index < cols; index++ {
		var cell Cell
		if index < len(cells) {
			cell = cells[index]
		}
		wrapped[index] = e.wrapSegments(e.segments(cell, e.opts.BodySize, metricKey{bold: header}), math.Max(naturalW[index]-2*padding, ptToMM(e.opts.BodySize)))
	}
	if header {
		e.fillRectPage(x0, bottom-usedW, rowHeight, usedW, colorHeaderBG)
	}
	lineHeight := ptToMM(e.opts.BodySize) * e.opts.LineHeight * scale
	colX := 0.0
	for index := 0; index < cols; index++ {
		align := AlignLeft
		if index < len(aligns) {
			align = aligns[index]
		}
		cursor := rowTop
		for _, line := range wrapped[index] {
			lineWidth := e.lineWidth(line) * scale
			startX := colX + padding*scale
			switch align {
			case AlignCenter:
				startX = colX + (widths[index]-lineWidth)/2
			case AlignRight:
				startX = colX + widths[index] - padding*scale - lineWidth
			}
			posX := startX
			for _, item := range line {
				if strings.TrimSpace(item.text) != "" {
					sizeS := item.size * scale
					ascentS := ascent(item.size, item.key) * scale
					fill := true
					e.add(creator.Text{
						X:             x0 + cursor + padding*scale + ascentS,
						Y:             bottom - posX - sizeS,
						Width:         item.width * scale,
						Height:        sizeS,
						Value:         item.text,
						Font:          e.fontName(item.key),
						Size:          sizeS,
						Fill:          &fill,
						FillColor:     item.color,
						CharDirection: 270,
					})
				}
				posX += item.width * scale
			}
			cursor += lineHeight
		}
		colX += widths[index]
	}
	// 行底分隔线铺满列向范围；列分隔线沿本行条带延伸。
	e.fillRectPage(x0+rowTop+rowHeight-0.2*scale, bottom-usedW, 0.2*scale, usedW, colorBorder)
	verticalX := 0.0
	for index := 0; index <= cols; index++ {
		e.fillRectPage(x0+rowTop, bottom-verticalX, rowHeight, 0.2*scale, colorBorder)
		if index < cols {
			verticalX += widths[index]
		}
	}
}

func (e *engine) drawTableRow(cells []Cell, widths []float64, padding, sizeMM float64, header bool, aligns []Align) {
	cols := len(widths)
	wrapped := make([][][]atom, cols)
	maxLines := 1
	for index := 0; index < cols; index++ {
		var cell Cell
		if index < len(cells) {
			cell = cells[index]
		}
		lines := e.wrapSegments(e.segments(cell, e.opts.BodySize, metricKey{bold: header}), math.Max(widths[index]-padding*2, sizeMM))
		wrapped[index] = lines
		if len(lines) > maxLines {
			maxLines = len(lines)
		}
	}
	lineHeight := sizeMM * e.opts.LineHeight
	rowHeight := float64(maxLines) * lineHeight
	e.ensureHeight(rowHeight)
	top := e.y
	if header {
		e.fillRect(e.contentLeft, top-rowHeight, e.contentWidth, rowHeight, colorHeaderBG)
	}
	x := e.contentLeft
	for index := 0; index < cols; index++ {
		align := AlignLeft
		if index < len(aligns) {
			align = aligns[index]
		}
		cursor := top
		for _, line := range wrapped[index] {
			lineWidth := e.lineWidth(line)
			startX := x + padding
			switch align {
			case AlignCenter:
				startX = x + (widths[index]-lineWidth)/2
			case AlignRight:
				startX = x + widths[index] - padding - lineWidth
			}
			maxSize, maxKey := e.lineMetrics(line)
			if maxSize <= 0 {
				maxSize = sizeMM
			}
			baseline := cursor - ascent(maxSize, maxKey)
			posX := startX
			for _, item := range line {
				if strings.TrimSpace(item.text) != "" {
					e.addText(item.text, posX, baseline, item)
				}
				posX += item.width
			}
			cursor -= lineHeight
		}
		x += widths[index]
	}
	e.fillRect(e.contentLeft, top-rowHeight, e.contentWidth, 0.2, colorBorder)
	verticalX := e.contentLeft
	for index := 0; index < cols; index++ {
		e.fillRect(verticalX, top-rowHeight, 0.2, rowHeight, colorBorder)
		verticalX += widths[index]
	}
	e.fillRect(verticalX-0.2, top-rowHeight, 0.2, rowHeight, colorBorder)
	e.y -= rowHeight
}

// cellWidth 计算单元格内容宽度；header 为真时按表头加粗度量，与绘制保持一致。
func (e *engine) cellWidth(cell Cell, sizeMM float64, header bool) float64 {
	width := 0.0
	for _, inline := range cell {
		key := metricKey{bold: inline.Bold || header, italic: inline.Italic, mono: inline.Code}
		width += measureWidth(inline.Text, sizeMM, key)
	}
	return width
}

func (e *engine) lineWidth(line []atom) float64 {
	width := 0.0
	for _, item := range line {
		width += item.width
	}
	return width
}

func (e *engine) lineHeight(line []atom) float64 {
	size, _ := e.lineMetrics(line)
	if size <= 0 {
		size = ptToMM(e.opts.BodySize)
	}
	return size * e.opts.LineHeight
}

func (e *engine) lineMetrics(line []atom) (float64, metricKey) {
	size := 0.0
	key := metricKey{}
	for _, item := range line {
		if item.text == "" {
			continue
		}
		if item.size > size {
			size = item.size
			key = item.key
		}
	}
	return size, key
}

func (e *engine) writeLine(line []atom, left float64) {
	size, key := e.lineMetrics(line)
	if size <= 0 {
		size = ptToMM(e.opts.BodySize)
	}
	height := size * e.opts.LineHeight
	e.ensureHeight(height)
	baseline := e.y - ascent(size, key)
	// 底色必须先于文字进入图元顺序，否则会被文字盖住。
	e.drawInlineCodeBackgrounds(line, left, baseline)
	for _, run := range mergeRuns(line, left) {
		e.addText(run.text, run.x, baseline, atom{text: run.text, key: run.key, size: run.size, color: run.color, width: run.width, strike: run.strike, strikeWidth: run.strikeWidth})
	}
	e.y -= height
}

// drawInlineCodeBackgrounds 给行内代码绘制底色矩形。segment.go 为每个行内代码
// 片段分配了唯一的 glue 值并禁止跨段断开，因此同一行内 glue 相同的相邻 atom
// 恰好构成一个完整的行内代码区间，x 的累加方式与 mergeRuns 保持一致。
func (e *engine) drawInlineCodeBackgrounds(line []atom, left, baseline float64) {
	padX := e.opts.InlineCodePaddingX
	padY := e.opts.InlineCodePaddingY
	if padX <= 0 && padY <= 0 {
		return
	}
	x := left
	glue, size := 0, 0.0
	startX, endX := 0.0, 0.0
	flush := func() {
		if glue > 0 && endX > startX {
			mono := metricKey{mono: true}
			// 游标自底向上：底边在基线之下 descent+内边距，顶边在基线之上
			// ascent+内边距，故 fillRect 的 y（下边界）要减去 descent。
			bottom := baseline - descent(size, mono) - padY
			height := ascent(size, mono) + descent(size, mono) + padY*2
			e.fillRect(startX-padX, bottom, endX-startX+2*padX, height, colorInlineCodeBG)
		}
	}
	for _, item := range line {
		if item.glue > 0 && item.glue != glue {
			flush()
			glue, startX, size = item.glue, x, item.size
		}
		if item.glue > 0 {
			endX = x + item.width
		}
		x += item.width
	}
	flush()
}

// textRun 是同一行内相邻、样式一致且可合并的文本片段。
type textRun struct {
	text        string
	key         metricKey
	size        float64
	color       *creator.Color
	x           float64
	width       float64
	strike      bool
	strikeWidth float64
}

// mergeRuns 合并相邻的同样式文本，并把空白并入前一个片段，避免生成
// 只含空白的非法文字对象，同时显著减少文字对象数量。
func mergeRuns(line []atom, left float64) []textRun {
	var runs []textRun
	x := left
	for _, item := range line {
		if item.space || strings.TrimSpace(item.text) == "" {
			if len(runs) > 0 {
				runs[len(runs)-1].text += item.text
				runs[len(runs)-1].width += item.width
			}
			x += item.width
			continue
		}
		if len(runs) > 0 {
			last := &runs[len(runs)-1]
			if last.key == item.key && last.size == item.size && last.color == item.color && last.strike == item.strike {
				last.text += item.text
				last.width += item.width
				last.strikeWidth += item.width
				x += item.width
				continue
			}
		}
		runs = append(runs, textRun{text: item.text, key: item.key, size: item.size, color: item.color, x: x, width: item.width, strike: item.strike, strikeWidth: item.width})
		x += item.width
	}
	return runs
}

func (e *engine) addText(text string, x, baseline float64, item atom) {
	fill := true
	e.add(creator.Text{
		X: x,
		// baseline 是基线距页底的距离；creator 的文字 Y 是文本框顶部（从页顶量起），
		// 基线在框底，故 Y = 页高 - 基线距页底 - 字号。
		Y:         e.opts.PageHeight - baseline - item.size,
		Width:     item.width,
		Height:    item.size,
		Value:     text,
		Font:      e.fontName(item.key),
		Size:      item.size,
		Italic:    item.key.italic,
		Fill:      &fill,
		FillColor: item.color,
	})
	if item.strike {
		// 删除线只覆盖实际字符，不含合并进来的尾随空白。
		strikeWidth := item.strikeWidth
		if strikeWidth <= 0 {
			strikeWidth = item.width
		}
		if strikeWidth > 0 {
			e.fillRect(x, baseline+item.size*0.3, strikeWidth, item.size*0.05, item.color)
		}
	}
}

// fillRect 绘制填充矩形。y 是矩形在排版游标（自底向上）中的下边界，
// creator 的路径 Y 是距页面顶部的位置，且路径数据向下延伸，故在此翻转。
func (e *engine) fillRect(x, y, width, height float64, color *creator.Color) {
	if width <= 0 || height <= 0 {
		return
	}
	stroke := false
	e.add(creator.Path{
		X:         x,
		Y:         e.opts.PageHeight - (y + height),
		Width:     width,
		Height:    height,
		Data:      rectPath(width, height),
		Fill:      true,
		Stroke:    stroke,
		StrokeSet: &stroke,
		LineWidth: 0,
		FillColor: color,
	})
}

// fillRectPage 以页面坐标（距页顶距离）直接绘制填充矩形，用于横排表格中
// 已经过坐标换算的条带、分隔线等。
func (e *engine) fillRectPage(x, y, width, height float64, color *creator.Color) {
	if width <= 0 || height <= 0 {
		return
	}
	stroke := false
	e.add(creator.Path{
		X:         x,
		Y:         y,
		Width:     width,
		Height:    height,
		Data:      rectPath(width, height),
		Fill:      true,
		Stroke:    stroke,
		StrokeSet: &stroke,
		LineWidth: 0,
		FillColor: color,
	})
}

func rectPath(width, height float64) string {
	return fmt.Sprintf("M 0 0 L %g 0 L %g %g L 0 %g C", width, width, height, height)
}

// fillRoundedPanel 绘制圆角填充面板。y 是面板下边界（排版游标，自底向上），
// 坐标换算与 fillRect 相同；半径超过短边一半时收敛到一半，避免相邻圆角
// 的弧线互相穿插。
func (e *engine) fillRoundedPanel(x, y, width, height, radius float64, color *creator.Color) {
	if width <= 0 || height <= 0 {
		return
	}
	radius = math.Min(radius, math.Min(width, height)/2)
	if radius <= 0 {
		e.fillRect(x, y, width, height, color)
		return
	}
	stroke := false
	e.add(creator.Path{
		X:         x,
		Y:         e.opts.PageHeight - (y + height),
		Width:     width,
		Height:    height,
		Data:      roundedRectPath(width, height, radius),
		Fill:      true,
		Stroke:    stroke,
		StrokeSet: &stroke,
		LineWidth: 0,
		FillColor: color,
	})
}

// roundedRectPath 生成顺时针圆角矩形路径：起点在左上角圆角的切点，四段直线
// 之间用半径为 r 的四分之一圆弧连接，sweep 恒为 1。
func roundedRectPath(width, height, r float64) string {
	return fmt.Sprintf(
		"M %g %g L %g %g A %g %g 0 0 1 %g %g L %g %g A %g %g 0 0 1 %g %g L %g %g A %g %g 0 0 1 %g %g L %g %g A %g %g 0 0 1 %g %g C",
		r, 0.0,
		width-r, 0.0,
		r, r, width, r,
		width, height-r,
		r, r, width-r, height,
		r, height,
		r, r, 0.0, height-r,
		0.0, r,
		r, r, r, 0.0,
	)
}

func ptToMM(pt float64) float64 { return pt * mmPerPoint }
