package layout

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
)

// authorState 的字体在单次排版内固定，度量和写出使用相同的数据快照。
type authorState struct {
	ctx                     context.Context
	opts                    FlowOptions
	faces                   map[string]*sfnt.Font
	fonts                   []creator.Font
	err                     error
	headings                []authorHeading
	anchors                 map[string]creator.GotoAction
	pendingLinks            []authorPendingLink
	section                 authorSection
	sectionIndex            int
	sectionPage             int
	nextNumber              int
	pageSettings            []authorPageSettings
	fontResources           map[string]creator.Font
	fontFamilies            map[string]FlowFontFamily
	footnoteCache           map[authorFootnoteKey]*authorFootnote
	pageFootnotes           [][]authorPageFootnotes
	columnIndex, columnBase int
	officialFirstBody       bool
	balanceHardPages        map[int]bool
	columnHeights           [][]float64
}

// MaterialTheme 返回适合 A4 报告的主题，借鉴 Material 的字号层级与间距节律。
// 这里的磅值是印刷预设，不是 dp/sp 到磅的等价换算。
func MaterialTheme() Theme {
	text, border, header := *colorText, *colorBorder, *colorHeaderBG
	body := TextStyle{Font: "Flow-Regular", Size: 12, LineHeight: 1.6, SpaceAfter: ptToMM(8), Color: &text, Align: "left"}
	t := Theme{Body: body, Caption: body, Table: body, BorderColor: &border, HeaderFill: &header}
	for i, size := range []float64{28, 22, 18, 16, 14, 12} {
		t.Headings[i] = TextStyle{Font: "Flow-Bold", Size: size, LineHeight: 1.3, SpaceBefore: ptToMM(16), SpaceAfter: ptToMM(8), Color: &text, Align: "left"}
	}
	t.Caption.Size = 10
	t.Caption.Align = "center"
	t.Caption.SpaceBefore = ptToMM(4)
	t.Table.Size = 10
	t.Table.LineHeight = 1.5
	return t
}

// composePass 在创建时完成一轮分页，返回可直接交给 creator 的标准固定版式模型。
func composePass(ctx context.Context, blocks []FlowBlock, opts FlowOptions) (*creator.Document, error) {
	result, err := composePassOnce(ctx, blocks, opts)
	retry, ok := err.(*authorRecipientOverflow)
	if !ok {
		return result, err
	}
	remove := make(map[int]bool, len(retry.indices))
	for _, i := range retry.indices {
		remove[i] = true
		opts.relocatedRecipients = append(opts.relocatedRecipients, blocks[i])
	}
	visible := make([]FlowBlock, 0, len(blocks)-len(remove))
	for i, b := range blocks {
		if !remove[i] {
			visible = append(visible, b)
		}
	}
	opts.Fonts, opts.FontFaces = retry.fonts, nil
	opts.fontNamesResolved = true
	opts.officialOriginalBlocks = blocks
	return composePassOnce(ctx, visible, opts)
}

func composeLayoutPass(ctx context.Context, blocks []FlowBlock, opts FlowOptions) (*creator.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.PageSize == (creator.PageSize{}) {
		opts.PageSize = creator.A4
	}
	if opts.Theme == (Theme{}) {
		opts.Theme = MaterialTheme()
	}
	for _, value := range []float64{opts.PageSize.Width, opts.PageSize.Height, opts.MarginTop, opts.MarginRight, opts.MarginBottom, opts.MarginLeft} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, fmt.Errorf("页面尺寸和页边距必须为有限非负数")
		}
	}
	if opts.PageSize.Width <= opts.MarginLeft+opts.MarginRight || opts.PageSize.Height <= opts.MarginTop+opts.MarginBottom {
		return nil, fmt.Errorf("页边距使版心宽度或高度不大于零")
	}
	if opts.PageNumbers && opts.MarginBottom < 8 {
		return nil, fmt.Errorf("显示页码时下页边距至少为 8mm")
	}
	if opts.Header != "" && opts.MarginTop < 8 {
		return nil, fmt.Errorf("显示页眉时上页边距至少为 8mm")
	}
	if opts.Footer != "" && opts.MarginBottom < 8 {
		return nil, fmt.Errorf("显示页脚时下页边距至少为 8mm")
	}
	a := &authorState{ctx: ctx, opts: opts, faces: make(map[string]*sfnt.Font), anchors: make(map[string]creator.GotoAction), nextNumber: 1, fontResources: make(map[string]creator.Font), fontFamilies: make(map[string]FlowFontFamily)}
	a.footnoteCache = make(map[authorFootnoteKey]*authorFootnote)
	if err := a.loadAuthorFonts(); err != nil {
		return nil, err
	}
	for _, name := range append([]string{opts.Theme.Body.Font}, opts.FallbackFonts...) {
		if name != "" && a.faces[name] == nil {
			return nil, fmt.Errorf("字体 %q 未注册", name)
		}
	}
	e := &engine{author: a, pageIndex: -1, opts: Options{PageWidth: opts.PageSize.Width, PageHeight: opts.PageSize.Height,
		MarginTop: opts.MarginTop, MarginRight: opts.MarginRight, MarginBottom: opts.MarginBottom, MarginLeft: opts.MarginLeft}}
	e.computeArea()
	columns, err := normalizeAuthorColumns(opts.Columns, e.contentWidth)
	if err != nil {
		return nil, err
	}
	initial := authorSection{geometry: e.opts, columns: columns, header: opts.Header, footer: opts.Footer, pageNumbers: opts.PageNumbers, numberStyle: "decimal"}
	a.section = initial
	e.setAuthorSectionArea(initial, 0)
	active := initial
	prepared := make([]authorBlock, len(blocks))
	for i, b := range blocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if b.Kind == "section" {
			next, err := active.merge(b)
			if err != nil {
				return nil, fmt.Errorf("blocks[%d].section: %w", b.sourceIndex, err)
			}
			active = next
			if opts.Official != nil {
				g := active.geometry
				if err := validateOfficialGeometry(g.PageWidth, g.PageHeight, g.MarginTop, g.MarginRight, g.MarginBottom, g.MarginLeft, active.columns); err != nil {
					return nil, fmt.Errorf("blocks[%d].section: %w", b.sourceIndex, err)
				}
				if active.header != "" || active.footer != "" || active.numberStyle != "decimal" || active.prefix != "" || b.Section.StartPageNumber != nil || active.firstHeader != nil || active.firstFooter != nil || active.hideFirstNumber {
					return nil, fmt.Errorf("gbt9704 分节保持连续阿拉伯页码，不能设置普通页眉页脚或首页例外")
				}
			}
			e.setAuthorSectionArea(active, 0)
			prepared[i] = authorBlock{block: b, section: &next}
			continue
		}
		if b.Section != nil {
			return nil, fmt.Errorf("blocks[%d]: section 配置需要 type 为 section", b.sourceIndex)
		}
		p, err := e.prepareAuthorBlock(b)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("blocks[%d]: %w", b.sourceIndex, err)
		}
		if a.err != nil {
			return nil, fmt.Errorf("blocks[%d]: %w", b.sourceIndex, a.err)
		}
		prepared[i] = p
	}
	e.setAuthorSectionArea(initial, 0)
	letterheadEmitted := false
	pendingBreak := false
	pendingColumnBreak := false
	pendingSection := false
	keepEnd := -1
	for i := range prepared {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p := &prepared[i]
		if p.section != nil {
			a.section = *p.section
			a.sectionIndex++
			a.sectionPage = 0
			if p.block.Section.StartPageNumber != nil {
				a.nextNumber = *p.block.Section.StartPageNumber
			}
			e.setAuthorSectionArea(a.section, 0)
			pendingColumnBreak = false
			pendingSection = true
			continue
		}
		if p.block.Kind == "page_break" {
			pendingBreak = true
			continue
		}
		if p.block.Kind == "column_break" {
			pendingColumnBreak = true
			continue
		}
		if pendingSection && e.pageIndex >= 0 {
			e.newPage()
		} else {
			e.ensurePage()
		}
		pendingSection = false
		if (pendingBreak || p.block.PageBreakBefore) && e.authorPageHasContent() {
			e.newPage()
		} else if pendingColumnBreak && e.authorHasContent() {
			if opts.balance != nil {
				if a.balanceHardPages == nil {
					a.balanceHardPages = make(map[int]bool)
				}
				a.balanceHardPages[e.pageIndex] = true
			}
			e.nextAuthorColumn()
		}
		pendingBreak = false
		pendingColumnBreak = false
		if !letterheadEmitted {
			if err := e.emitAuthorOfficialLetterhead(); err != nil {
				return nil, fmt.Errorf("official.letterhead: %w", err)
			}
			letterheadEmitted = true
		}
		if i >= keepEnd {
			var err error
			keepEnd, err = e.ensureAuthorKeepChain(prepared, i)
			if err != nil {
				return nil, fmt.Errorf("blocks[%d]: %w", p.block.sourceIndex, err)
			}
		}
		if p.block.KeepTogether && p.height() <= e.contentHeight {
			lines := p.lines
			if p.table != nil {
				lines = p.table.noteLines(0, len(p.table.rowOffsets)-1)
			}
			if err := e.ensureAuthorHeight(p.height(), lines); err != nil {
				return nil, fmt.Errorf("blocks[%d]: %w", p.block.sourceIndex, err)
			}
		}
		if p.fixed != nil {
			e.placeAuthorFixed(p)
		} else if p.image != nil {
			e.placeAuthorImage(p)
		} else if p.table != nil {
			if err := e.placeAuthorTable(ctx, p); err != nil {
				return nil, fmt.Errorf("blocks[%d]: %w", p.block.sourceIndex, err)
			}
		} else {
			if err := e.placeAuthorLines(ctx, p); err != nil {
				return nil, fmt.Errorf("blocks[%d]: %w", p.block.sourceIndex, err)
			}
		}
		pendingBreak = p.block.PageBreakAfter
	}
	if opts.Official != nil && !a.officialFirstBody {
		candidates := authorRecipientCandidates(blocks)
		if len(opts.relocatedRecipients) == 0 && len(candidates) > 0 {
			return nil, &authorRecipientOverflow{indices: candidates, fonts: a.fonts}
		}
		if len(opts.relocatedRecipients) > 0 {
			return nil, fmt.Errorf("主送机关移至版记后首页仍无法显示正文，请缩短版头、标题或调整内容")
		}
		if opts.Official.Letterhead != nil {
			return nil, fmt.Errorf("gbt9704 首页必须排入正文，请缩短版头、标题或调整内容")
		}
	}
	e.ensurePage()
	if err := e.emitAuthorOfficialSignature(); err != nil {
		return nil, fmt.Errorf("official.signature: %w", err)
	}
	if err := e.emitAuthorOfficialColophon(); err != nil {
		return nil, fmt.Errorf("official.colophon: %w", err)
	}
	if opts.balance != nil {
		opts.balance.capture(e)
	}
	if err := e.emitAuthorFootnotes(); err != nil {
		return nil, err
	}
	if err := e.emitAuthorPageDecorations(); err != nil {
		return nil, err
	}
	navigationBlocks := blocks
	if opts.officialOriginalBlocks != nil {
		navigationBlocks = opts.officialOriginalBlocks
	}
	bookmarks, err := e.resolveAuthorLinks(navigationBlocks)
	if err != nil {
		return nil, err
	}
	return &creator.Document{ID: "flow", PageSize: opts.PageSize, Fonts: a.fonts, Pages: e.pages, Outlines: authorOutlines(a.headings), Bookmarks: bookmarks}, nil
}

func (a *authorState) resolveFont(inline Inline, inherited string, r rune) string {
	name := inherited
	if inline.Font != "" {
		name = inline.Font
	}
	if a.faces[name] == nil {
		if a.err == nil {
			a.err = fmt.Errorf("字体 %q 未注册", name)
		}
		return name
	}
	resource := a.fontResources[name]
	bold, italic := inline.Bold || resource.Bold, inline.Italic || resource.Italic
	name = a.styledAuthorFont(name, bold, italic)
	if r == '\n' || r == '\r' {
		return name
	}
	var buf sfnt.Buffer
	candidates := []string{name}
	for _, fallback := range a.opts.FallbackFonts {
		selected := a.styledAuthorFont(fallback, bold, italic)
		candidates = append(candidates, selected)
		if selected != fallback {
			candidates = append(candidates, fallback)
		}
	}
	if family, ok := a.fontFamilies[name]; ok && family.Regular != name {
		candidates = append(candidates, family.Regular)
	}
	for _, candidate := range candidates {
		if resource.FixedWidth && !a.fontResources[candidate].FixedWidth {
			continue
		}
		face := a.faces[candidate]
		if face == nil {
			continue
		}
		if glyph, err := face.GlyphIndex(&buf, r); err == nil && glyph != 0 {
			return candidate
		}
	}
	if a.err == nil {
		a.err = fmt.Errorf("字体 %q 及回退字体缺少字符 %q（U+%04X）", name, r, r)
	}
	return name
}

func (a *authorState) width(text string, size float64, name string) float64 {
	face := a.faces[name]
	if face == nil {
		return 0
	}
	var buf sfnt.Buffer
	width := 0.0
	for i, r := range text {
		if i%1024 == 0 {
			if err := a.ctx.Err(); err != nil {
				a.err = err
				return 0
			}
		}
		g, err := face.GlyphIndex(&buf, r)
		if err != nil {
			if a.err == nil {
				a.err = err
			}
			continue
		}
		advance, err := face.GlyphAdvance(&buf, g, 1000*64, font.HintingNone)
		if err != nil {
			if a.err == nil {
				a.err = err
			}
			continue
		}
		width += float64(advance) / 64000 * size
	}
	return width
}

func (a *authorState) metrics(name string, size float64) (float64, float64) {
	face := a.faces[name]
	if face == nil {
		return 0, 0
	}
	var buf sfnt.Buffer
	m, err := face.Metrics(&buf, 1000*64, font.HintingNone)
	if err != nil {
		if a.err == nil {
			a.err = err
		}
		return 0, 0
	}
	return float64(m.Ascent) / 64000 * size, float64(m.Descent) / 64000 * size
}

type authorLine struct {
	atoms []atom
	// gaps 记录自动换行中可伸展的 UAX #14 段边界，以后一个字符的序号表示。
	// 段末与显式换行不设置边界，跨页或换栏后仍保留该行的对齐方式。
	gaps    []int
	indent  float64
	height  float64
	ascent  float64
	descent float64
}

type authorBlock struct {
	block       FlowBlock
	style       TextStyle
	lines       []authorLine
	image       *creator.Image
	table       *authorTable
	marker      []authorLine
	markerWidth float64
	extraHeight float64
	section     *authorSection
	tocPage     []authorLine
	tocWidth    float64
	fixed       *FlowFixed
}

func (p *authorBlock) height() float64 {
	h := p.style.SpaceBefore + p.style.SpaceAfter + p.extraHeight
	for _, l := range p.lines {
		h += l.height
	}
	if p.image != nil {
		h += p.image.Height
	}
	if p.fixed != nil {
		h += p.fixed.Height
	}
	if p.table != nil {
		h += p.table.height()
	}
	return h
}

func (p *authorBlock) minimumHeight() float64 {
	if p.image != nil || p.fixed != nil {
		return p.height()
	}
	if p.table != nil {
		return p.style.SpaceBefore + p.table.minimumHeight()
	}
	n := p.minimumLineCount()
	h := p.style.SpaceBefore
	for i := 0; i < len(p.lines) && i < n; i++ {
		h += p.lines[i].height
	}
	return h
}

func mergeTextStyle(base, override TextStyle) TextStyle {
	if override.Font != "" {
		base.Font = override.Font
	}
	if override.Size != 0 || override.Set&StyleSize != 0 {
		base.Size = override.Size
	}
	if override.LineHeight != 0 || override.Set&StyleLineHeight != 0 {
		base.LineHeight = override.LineHeight
	}
	if override.SpaceBefore != 0 || override.Set&StyleSpaceBefore != 0 {
		base.SpaceBefore = override.SpaceBefore
	}
	if override.SpaceAfter != 0 || override.Set&StyleSpaceAfter != 0 {
		base.SpaceAfter = override.SpaceAfter
	}
	if override.FirstLineIndent != 0 || override.Set&StyleFirstLineIndent != 0 {
		base.FirstLineIndent = override.FirstLineIndent
	}
	if override.Align != "" {
		base.Align = override.Align
	}
	if override.Color != nil {
		base.Color = override.Color
	}
	base.Set |= override.Set
	return base
}

func validateTextStyle(s TextStyle) error {
	for _, value := range []float64{s.Size, s.LineHeight, s.SpaceBefore, s.SpaceAfter, s.FirstLineIndent} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return fmt.Errorf("样式尺寸必须为有限非负数")
		}
	}
	if s.Size <= 0 || s.LineHeight <= 0 {
		return fmt.Errorf("字号和行高必须大于零")
	}
	switch s.Align {
	case "", "left", "center", "right", "justify":
	default:
		return fmt.Errorf("不支持的对齐方式 %q", s.Align)
	}
	return nil
}

func (e *engine) prepareAuthorBlock(b FlowBlock) (authorBlock, error) {
	p := authorBlock{block: b, style: e.author.opts.Theme.Body}
	if b.Orphans < 0 || b.Widows < 0 {
		return p, fmt.Errorf("孤行控制的行数不能为负数")
	}
	if b.Text != "" && len(b.Runs) != 0 {
		return p, fmt.Errorf("text 与 runs 不能同时设置")
	}
	if b.Kind == "" {
		p.block.Kind = "paragraph"
	}
	switch p.block.Kind {
	case "heading":
		if b.Level < 1 || b.Level > 6 {
			return p, fmt.Errorf("标题 level 必须为 1 到 6")
		}
		p.style = mergeTextStyle(p.style, e.author.opts.Theme.Headings[b.Level-1])
	case "image":
		if b.Image == nil {
			return p, fmt.Errorf("缺少 image 内容")
		}
	case "table":
		if b.Table == nil {
			return p, fmt.Errorf("缺少 table 内容")
		}
		p.style = mergeTextStyle(p.style, e.author.opts.Theme.Table)
	case "fixed":
		if b.Fixed == nil {
			return p, fmt.Errorf("缺少 fixed 内容")
		}
	case "recipient":
		if e.author.opts.Official == nil {
			return p, fmt.Errorf("recipient 仅用于 gbt9704 公文配置")
		}
		p.style.FirstLineIndent = 0
		p.style.Align = "left"
		p.block.KeepWithNext = true
	case "paragraph", "list_item", "page_break", "column_break":
	case "toc_entry":
	default:
		return p, fmt.Errorf("未知内容类型 %q", b.Kind)
	}
	if b.Style != nil {
		p.style = mergeTextStyle(p.style, *b.Style)
	}
	if err := validateTextStyle(p.style); err != nil {
		return p, err
	}
	if e.author.faces[p.style.Font] == nil {
		return p, fmt.Errorf("字体 %q 未注册", p.style.Font)
	}
	if b.Image != nil && p.block.Kind != "image" || b.Table != nil && p.block.Kind != "table" {
		return p, fmt.Errorf("image/table 内容与块类型不一致")
	}
	if b.Fixed != nil && p.block.Kind != "fixed" {
		return p, fmt.Errorf("fixed 内容与块类型不一致")
	}
	if p.block.Kind == "page_break" || p.block.Kind == "column_break" {
		if b.Text != "" || len(b.Runs) > 0 || b.Marker != "" {
			return p, fmt.Errorf("page_break/column_break 不能携带文字")
		}
		return p, nil
	}
	if (p.block.Kind == "image" || p.block.Kind == "table" || p.block.Kind == "fixed") && (b.Text != "" || len(b.Runs) > 0) {
		return p, fmt.Errorf("图片、表格和固定块不能同时设置 text 或 runs")
	}
	if p.block.Kind == "image" {
		return e.prepareAuthorImage(p)
	}
	if p.block.Kind == "table" {
		return e.prepareAuthorTable(p)
	}
	if p.block.Kind == "fixed" {
		return e.prepareAuthorFixed(p)
	}
	runs := b.Runs
	if b.Text != "" {
		runs = []Inline{{Text: b.Text}}
	}
	width := e.contentWidth
	if p.block.Kind == "toc_entry" {
		p.style.Align = "left"
		p.markerWidth = float64(b.Level-1) * ptToMM(p.style.Size)
		p.tocPage = e.authorLines([]Inline{{Text: e.author.referenceLabel(b.tocTarget), Target: b.tocTarget}}, p.style, width, 0)
		if len(p.tocPage) != 1 {
			return p, fmt.Errorf("目录页码无法排入一行")
		}
		p.tocWidth = e.lineWidth(p.tocPage[0].atoms) + ptToMM(p.style.Size)*2
		width -= p.markerWidth + p.tocWidth
		p.block.KeepTogether = true
		for i := range p.tocPage[0].atoms {
			p.tocPage[0].atoms[i].target = b.tocTarget
		}
		runs = []Inline{{Text: b.Text, Target: b.tocTarget}}
	}
	if p.block.Kind == "list_item" {
		marker := b.Marker
		if marker == "" {
			marker = "•"
		}
		p.marker = e.authorLines([]Inline{{Text: marker}}, p.style, width, 0)
		if len(p.marker) != 1 {
			return p, fmt.Errorf("列表标记必须能放入一行")
		}
		p.markerWidth = e.lineWidth(p.marker[0].atoms) + ptToMM(p.style.Size)
		width -= p.markerWidth
	}
	if width <= p.style.FirstLineIndent {
		return p, fmt.Errorf("缩进超过可用行宽")
	}
	p.lines = e.authorLines(runs, p.style, width, p.style.FirstLineIndent)
	for _, line := range p.lines {
		if e.lineWidth(line.atoms)+line.indent > width+0.001 {
			return p, fmt.Errorf("不可断开的文本超过行宽，请增加版心或插入换行")
		}
		if line.height > e.contentHeight {
			return p, fmt.Errorf("文字行高超过整页版心")
		}
	}
	return p, nil
}

// authorLines 复用 UAX #14 分段；首行缩进仅影响首行的可用宽度。
func (e *engine) authorLines(runs []Inline, style TextStyle, width, indent float64) []authorLine {
	copyRuns := append([]Inline(nil), runs...)
	previousCR := false
	for i := range copyRuns {
		if copyRuns[i].TitleRef != "" {
			copyRuns[i].Text = e.author.opts.titleLabels[copyRuns[i].TitleRef]
			copyRuns[i].Target = copyRuns[i].TitleRef
		}
		if copyRuns[i].Footnote != "" {
			number := e.author.opts.noteNumbers[copyRuns[i].Footnote]
			copyRuns[i].Text = fmt.Sprintf("[%d]", number)
			copyRuns[i].Target = authorFootnoteTarget(number)
		}
		if copyRuns[i].CaptionRef != "" {
			copyRuns[i].Text = e.author.opts.captionLabels[copyRuns[i].CaptionRef]
			copyRuns[i].Target = copyRuns[i].CaptionRef
		}
		if copyRuns[i].PageRef != "" {
			copyRuns[i].Text = e.author.referenceLabel(copyRuns[i].PageRef)
			copyRuns[i].Target = copyRuns[i].PageRef
		}
		text := copyRuns[i].Text
		if text != "" {
			// CRLF 可以跨越样式或空片段；按完整文字流识别一次换行。
			// 引用先展开，可见的引用文字会截断跨片段的 CRLF 配对。
			endsCR := strings.HasSuffix(text, "\r")
			if previousCR && strings.HasPrefix(text, "\n") {
				text = text[1:]
			}
			previousCR = endsCR
		}
		copyRuns[i].Text = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\t", "    ")
	}
	segments := e.segments(copyRuns, style.Size, metricKey{font: style.Font})
	if e.author.opts.Official != nil {
		for _, segment := range segments {
			for i := range segment {
				if math.Abs(segment[i].size-ptToMM(16)) < 0.000001 {
					// 保留三号的纵向字号，用标准 HScale 将 28 个全角字撑满 156mm（§5.2.3）。
					segment[i].hscale = officialCharWidth / ptToMM(16)
					segment[i].width *= segment[i].hscale
				}
			}
		}
	}
	var raw []authorLine
	var current []atom
	var gaps []int
	runeCount := 0
	used := indent
	flush := func(wrapped bool) {
		for len(current) > 0 {
			last := &current[len(current)-1]
			trimmed := strings.TrimRightFunc(last.text, unicode.IsSpace)
			if trimmed == last.text {
				break
			}
			runeCount -= utf8.RuneCountInString(last.text) - utf8.RuneCountInString(trimmed)
			if trimmed == "" {
				current = current[:len(current)-1]
				continue
			}
			last.text = trimmed
			last.width = e.author.width(trimmed, last.size, last.key.font) * authorAtomScale(*last)
			break
		}
		for len(gaps) > 0 && gaps[len(gaps)-1] >= runeCount {
			gaps = gaps[:len(gaps)-1]
		}
		line := authorLine{atoms: current}
		if wrapped {
			line.gaps = gaps
		}
		raw = append(raw, line)
		current, gaps, runeCount, used = nil, nil, 0, 0
	}
	for _, segment := range segments {
		if len(segment) == 0 {
			flush(false)
			continue
		}
		whitespace := segmentWhitespaceOnly(segment)
		if len(current) == 0 && whitespace {
			continue
		}
		w := e.lineWidth(segment)
		// 尾空白只在后续确有文字时占位；不能因空格越界提前换行或产生空白行。
		if len(current) > 0 && !whitespace && used+e.authorWidthWithoutTrailingSpace(segment) > width+0.001 {
			flush(true)
		}
		if style.Align == "justify" && len(current) > 0 {
			gaps = append(gaps, runeCount)
		}
		current = append(current, segment...)
		for _, item := range segment {
			runeCount += utf8.RuneCountInString(item.text)
		}
		used += w
	}
	if len(current) > 0 {
		flush(false)
	}
	var result []authorLine
	for i, line := range raw {
		line.height = ptToMM(style.Size) * style.LineHeight
		if i == 0 {
			line.indent = indent
		}
		line.ascent, line.descent = e.author.metrics(style.Font, ptToMM(style.Size))
		upper, lower := line.ascent, line.descent
		for _, item := range line.atoms {
			ascent, descent := e.author.metrics(item.key.font, item.size)
			if item.rise == 0 {
				line.ascent = math.Max(line.ascent, ascent)
				line.descent = math.Max(line.descent, descent)
			}
			upper = math.Max(upper, ascent+item.rise)
			lower = math.Max(lower, descent-item.rise)
		}
		line.height = math.Max(line.height, line.ascent+line.descent)
		// 优先使用正文既有的基线和行距；上标超出行框时才增加必要的上下留白。
		baseline := line.ascent + (line.height-line.ascent-line.descent)/2
		above, below := math.Max(0, upper-baseline), math.Max(0, lower-(line.height-baseline))
		line.ascent += above
		line.descent += below
		line.height += above + below
		result = append(result, line)
	}
	return result
}

func (e *engine) authorWidthWithoutTrailingSpace(atoms []atom) float64 {
	width := e.lineWidth(atoms)
	for i := len(atoms) - 1; i >= 0; i-- {
		item := atoms[i]
		trimmed := strings.TrimRightFunc(item.text, unicode.IsSpace)
		if trimmed == "" {
			width -= item.width
			continue
		}
		if trimmed != item.text {
			width -= e.author.width(item.text[len(trimmed):], item.size, item.key.font) * authorAtomScale(item)
		}
		break
	}
	return width
}

func (e *engine) drawAuthorLine(line authorLine, style TextStyle, left, width, top float64) {
	x := left + line.indent
	w := e.lineWidth(line.atoms)
	switch style.Align {
	case "center":
		x += (width - line.indent - w) / 2
	case "right":
		x = left + width - w
	}
	baseline := e.opts.PageHeight - top + line.ascent + (line.height-line.ascent-line.descent)/2
	for _, positioned := range e.authorLineRuns(line, x, width-line.indent, style.Align == "justify") {
		run := positioned.textRun
		if strings.TrimSpace(run.text) == "" {
			continue
		}
		size := run.size
		runAscent, runDescent := e.author.metrics(run.key.font, size)
		runBaseline := baseline - run.rise
		runTop := runBaseline - runAscent
		if _, note := e.author.opts.noteTargets[run.target]; note {
			back := authorFootnoteBackTarget(run.target, e.pageIndex, e.author.columnIndex)
			if _, exists := e.author.anchors[back]; !exists {
				e.author.anchors[back] = e.authorDestination(runTop)
			}
			run.target = authorFootnotePageTarget(run.target, e.pageIndex, e.author.columnIndex)
		}
		x0, y0 := 0.0, runAscent
		color := style.Color
		if (run.href != "" || run.target != "") && e.author.opts.Official == nil {
			color = colorLink
		}
		fill := true
		resource := e.author.fontResources[run.key.font]
		// 字体资源与文字对象使用一致样式（GB/T 33190—2016 §11.1、§11.2），
		// 使阅读端直接选择所嵌入的字体面，避免再次反向合成斜体或字重。
		weight := 400
		if resource.Bold {
			weight = 700
		}
		if run.target != "" {
			e.author.pendingLinks = append(e.author.pendingLinks, authorPendingLink{page: e.pageIndex, item: len(e.cur().Items), target: run.target})
		}
		e.add(creator.Text{X: run.x, Y: runTop, Width: run.width, Height: runAscent + runDescent,
			Font: run.key.font, Size: size, HScale: run.hscale, Weight: weight, Italic: resource.Italic, Fill: &fill, FillColor: color,
			TextCodes: []creator.TextCode{{Value: run.text, X: &x0, Y: &y0, DeltaX: positioned.deltas}}, Actions: linkActions(run.href)})
		if run.strike {
			e.fillRect(run.x, e.opts.PageHeight-runBaseline+size*0.3, run.width, size*0.05, color)
		}
	}
}

func (e *engine) placeAuthorLines(ctx context.Context, p *authorBlock) error {
	if len(p.lines) == 0 {
		return nil
	}
	orphans, widows := p.block.Orphans, p.block.Widows
	if orphans == 0 {
		orphans = 2
	}
	if widows == 0 {
		widows = 2
	}
	before := p.style.SpaceBefore
	for start := 0; start < len(p.lines); {
		if err := ctx.Err(); err != nil {
			return err
		}
		gap := before
		if !e.authorHasContent() {
			gap = 0
		}
		count, err := e.fitAuthorLines(p.lines[start:], gap)
		if err != nil {
			return err
		}
		remaining := len(p.lines) - start
		if count < remaining {
			if remaining-count < widows {
				count = remaining - widows
			}
			if count < orphans && start == 0 && e.authorHasContent() {
				e.nextAuthorColumn()
				continue
			}
			if e.authorFlowBottom() > e.opts.MarginBottom && count < orphans && start == 0 {
				return fmt.Errorf("平衡栏高无法满足段首孤行约束")
			}
		}
		if count <= 0 {
			if e.authorHasContent() {
				e.nextAuthorColumn()
				continue
			}
			// 空页无法同时满足孤行约束时优先保证内容向前推进。
			if e.authorFlowBottom() > e.opts.MarginBottom {
				return fmt.Errorf("平衡栏高无法满足段尾孤行约束")
			}
			count, err = e.fitAuthorLines(p.lines[start:], gap)
			if err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("文字行及其脚注无法同时放入整页版心，请减小字号或缩短脚注")
			}
		}
		plan, err := e.planAuthorFootnotes(p.lines[start:start+count], e.currentAuthorFootnotes())
		if err != nil {
			return err
		}
		e.commitAuthorFootnotes(plan)
		e.y -= gap
		if start == 0 {
			e.recordAuthorAnchor(p.block.ID)
		}
		if start == 0 && p.block.Kind == "heading" && e.author.opts.GenerateOutlines {
			if title := authorHeadingTitle(p.block); title != "" {
				e.author.headings = append(e.author.headings, authorHeading{level: p.block.Level, title: title, destination: e.authorDestination(e.opts.PageHeight - e.y)})
			}
		}
		for j := 0; j < count; j++ {
			line := p.lines[start+j]
			if e.author.opts.Official != nil && e.pageIndex == 0 && len(line.atoms) > 0 && (p.block.Kind == "paragraph" || p.block.Kind == "list_item" || p.block.Kind == "heading" && p.block.Level > 1) {
				e.author.officialFirstBody = true
			}
			if start+j == 0 && len(p.marker) > 0 {
				e.drawAuthorLine(p.marker[0], p.style, e.contentLeft, p.markerWidth, e.y)
			}
			e.drawAuthorLine(line, p.style, e.contentLeft+p.markerWidth, e.contentWidth-p.markerWidth-p.tocWidth, e.y)
			if start+j == len(p.lines)-1 && len(p.tocPage) > 0 {
				s := p.style
				s.Align = "right"
				e.drawAuthorLine(p.tocPage[0], s, e.contentLeft, e.contentWidth, e.y)
				left := e.contentLeft + p.markerWidth + e.lineWidth(line.atoms) + ptToMM(s.Size)*0.5
				right := e.contentRight - e.lineWidth(p.tocPage[0].atoms) - ptToMM(s.Size)*0.5
				if right > left {
					e.fillRect(left, e.y-line.height*0.75, right-left, 0.12, e.author.opts.Theme.BorderColor)
				}
			}
			e.y -= line.height
		}
		start += count
		before = 0
		if start < len(p.lines) {
			e.nextAuthorColumn()
		}
	}
	e.space(p.style.SpaceAfter)
	return e.author.err
}

func (e *engine) prepareAuthorImage(p authorBlock) (authorBlock, error) {
	img := p.block.Image
	for _, v := range []float64{img.Width, img.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return p, fmt.Errorf("图片尺寸必须为有限非负数")
		}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil {
		return p, fmt.Errorf("图片无法解析: %w", err)
	}
	w, h := img.Width, img.Height
	if w == 0 && h == 0 {
		w = float64(config.Width) / 96 * 25.4
		h = float64(config.Height) / 96 * 25.4
	}
	if w == 0 {
		w = h * float64(config.Width) / float64(config.Height)
	}
	if h == 0 {
		h = w * float64(config.Height) / float64(config.Width)
	}
	if w > e.contentWidth {
		h *= e.contentWidth / w
		w = e.contentWidth
	}
	p.image = &creator.Image{Width: w, Height: h, Data: img.Data, Format: format}
	p, err = e.prepareAuthorFigureCaption(p, img.Caption)
	if err != nil {
		return p, err
	}
	if p.height() > e.contentHeight {
		return p, fmt.Errorf("图片与图注总高超过整页版心，请减小图片尺寸")
	}
	return p, nil
}

func (e *engine) placeAuthorImage(p *authorBlock) {
	e.ensureHeight(p.height())
	if e.authorHasContent() {
		e.space(p.style.SpaceBefore)
	}
	e.recordAuthorAnchor(p.block.ID)
	img := *p.image
	img.X = e.contentLeft + (e.contentWidth-img.Width)/2
	img.Y = e.opts.PageHeight - e.y
	e.add(img)
	e.y -= img.Height
	e.drawAuthorFigureCaption(p)
}

// ValidateFlowText 检查 UTF-8 与控制字符，供公开创作入口与清单入口复用。
func ValidateFlowText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("文字不是有效 UTF-8")
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return fmt.Errorf("不支持的控制字符 U+%04X", r)
		}
	}
	return nil
}
