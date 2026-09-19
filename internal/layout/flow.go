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
	colorText     = &creator.Color{R: 0x1f, G: 0x23, B: 0x28}
	colorLink     = &creator.Color{R: 0x0b, G: 0x57, B: 0xd0}
	colorCode     = &creator.Color{R: 0xa6, G: 0x1e, B: 0x4e}
	colorQuote    = &creator.Color{R: 0x57, G: 0x60, B: 0x6a}
	colorCodeBG   = &creator.Color{R: 0xf4, G: 0xf5, B: 0xf7}
	colorRule     = &creator.Color{R: 0xd0, G: 0xd7, B: 0xde}
	colorBorder   = &creator.Color{R: 0xc4, G: 0xcb, B: 0xd1}
	colorHeaderBG = &creator.Color{R: 0xef, G: 0xf2, B: 0xf5}
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

	fontNames map[metricKey]string
	fonts     []creator.Font
	footer    *Footer

	// official 表示按 GB/T 9704-2012 公文版式编排（设置了 Letterhead 时启用）。
	official bool
	// officialBodyStarted 记录公文正文是否已开始，用于识别主送机关行。
	officialBodyStarted bool
}

// Build 把流式文档排版为 OFD 文档。返回的文档可继续补充元数据后写入。
func Build(doc *Document, opts Options) (*creator.Document, error) {
	if doc == nil {
		return nil, fmt.Errorf("文档为空")
	}
	if opts.PageWidth <= 0 || opts.PageHeight <= 0 || opts.BodySize <= 0 {
		opts = DefaultOptions()
	}
	if opts.LineHeight <= 0 {
		opts.LineHeight = 1.5
	}
	if opts.CodeLineHeight <= 0 {
		opts.CodeLineHeight = 1.3
	}
	if opts.BodyFamily == "" {
		opts.BodyFamily = "sans-serif"
	}
	if opts.MonoFamily == "" {
		opts.MonoFamily = "monospace"
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

func (e *engine) cur() *creator.Page { return &e.pages[e.pageIndex] }

func (e *engine) add(item creator.Item) {
	e.ensurePage()
	e.cur().Items = append(e.cur().Items, item)
}

// ensureHeight 在剩余空间不足时换页；已写内容的页面才会触发换页，避免空页。
func (e *engine) ensureHeight(height float64) {
	if e.pageIndex < 0 {
		e.newPage()
		return
	}
	if e.y-height < e.contentBottom-0.01 && len(e.cur().Items) > 0 {
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
// emitColophon 在末页版心最下方编排版记（GB/T 9704-2012 7.4）：
// 首条、末条分隔线用粗线（0.35mm）、中间分隔线用细线（0.25mm），
// 线与版心等宽；抄送左空一字，印发机关左空一字、印发日期右空一字。
// 末条分隔线下边缘压准版心下边缘，文字行带与分隔线之间各留空（约 0.2 字）。
func (e *engine) emitColophon(c *Colophon) {
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
	band := sizeMM * 1.4 // 文字行带（文字上下各留 0.2 字）
	rows := 1
	if hasCc {
		rows++
	}
	colH := lineBoldW + band*float64(rows) + lineThinW + lineBoldW
	if !hasCc {
		colH -= lineThinW
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
		// 中间分隔线（细线）。
		thinLine()
	}
	var issuedWidth float64
	if strings.TrimSpace(c.IssuedBy) != "" {
		issuedWidth += measureWidth(c.IssuedBy, sizeMM, key)
	}
	if strings.TrimSpace(c.IssuedDate) != "" {
		label := strings.TrimSpace(c.IssuedDate) + "印发"
		width := measureWidth(label, sizeMM, key)
		writeRow(label, e.contentRight-sizeMM-width, width)
		cur += band // 印发日期居右，机关同名在同一行，行带只计一次
	}
	if strings.TrimSpace(c.IssuedBy) != "" {
		writeRow(c.IssuedBy, e.contentLeft+sizeMM, issuedWidth)
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
		e.emitParagraph(b.Inlines, b.Indent)
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
		text := ""
		for _, inline := range inlines {
			text += inline.Text
		}
		text = strings.TrimSpace(text)
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

func (e *engine) emitCode(b *Block) {
	size := ptToMM(e.opts.MonoSize)
	lineHeight := size * e.opts.CodeLineHeight
	key := metricKey{mono: true}
	raw := strings.Split(strings.ReplaceAll(b.Code, "\r\n", "\n"), "\n")
	for len(raw) > 0 && strings.TrimSpace(raw[len(raw)-1]) == "" {
		raw = raw[:len(raw)-1]
	}
	for _, line := range raw {
		text := strings.ReplaceAll(line, "\t", "    ")
		e.ensureHeight(lineHeight)
		e.fillRect(e.contentLeft, e.y-lineHeight, e.contentWidth, lineHeight, colorCodeBG)
		baseline := e.y - ascent(size, key)
		item := atom{text: text, key: key, size: size, color: colorCode, width: measureWidth(text, size, key)}
		if strings.TrimSpace(text) != "" {
			e.addText(text, e.contentLeft+3, baseline, item)
		}
		e.y -= lineHeight
	}
	e.space(e.blockGap())
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
			width = math.Max(width, e.cellWidth(table.Header[index], sizeMM))
		}
		for _, row := range table.Rows {
			if index < len(row) {
				width = math.Max(width, e.cellWidth(row[index], sizeMM))
			}
		}
		widths[index] = width + padding*2
	}
	total := 0.0
	for _, width := range widths {
		total += width
	}
	if total > e.contentWidth {
		scale := e.contentWidth / total
		for index := range widths {
			widths[index] *= scale
		}
	} else {
		extra := (e.contentWidth - total) / float64(cols)
		for index := range widths {
			widths[index] += extra
		}
	}
	e.drawTableRow(table.Header, widths, padding, sizeMM, true, table.Align)
	for _, row := range table.Rows {
		e.drawTableRow(row, widths, padding, sizeMM, false, table.Align)
	}
	e.space(e.blockGap())
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

func (e *engine) cellWidth(cell Cell, sizeMM float64) float64 {
	width := 0.0
	for _, inline := range cell {
		key := metricKey{bold: inline.Bold, italic: inline.Italic, mono: inline.Code}
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
	for _, run := range mergeRuns(line, left) {
		e.addText(run.text, run.x, baseline, atom{text: run.text, key: run.key, size: run.size, color: run.color, width: run.width})
	}
	e.y -= height
}

// textRun 是同一行内相邻、样式一致且可合并的文本片段。
type textRun struct {
	text  string
	key   metricKey
	size  float64
	color *creator.Color
	x     float64
	width float64
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
			if last.key == item.key && last.size == item.size && last.color == item.color {
				last.text += item.text
				last.width += item.width
				x += item.width
				continue
			}
		}
		runs = append(runs, textRun{text: item.text, key: item.key, size: item.size, color: item.color, x: x, width: item.width})
		x += item.width
	}
	return runs
}

func (e *engine) addText(text string, x, baseline float64, item atom) {
	fill := true
	e.add(creator.Text{
		X: x,
		// creator 的文字 Y 是文本框顶部（从页顶量起），基线在框底，
		// 因此需从"基线距页顶的距离"再减去字号。
		Y:         e.opts.PageHeight - baseline - item.size,
		Width:     item.width,
		Height:    item.size,
		Value:     text,
		Font:      e.fontName(item.key),
		Size:      item.size,
		Fill:      &fill,
		FillColor: item.color,
	})
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

func rectPath(width, height float64) string {
	return fmt.Sprintf("M 0 0 L %g 0 L %g %g L 0 %g C", width, width, height, height)
}

func ptToMM(pt float64) float64 { return pt * mmPerPoint }
