package flow

import (
	"context"
	"fmt"
	"maps"
	"math"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/sfnt"
)

// composePass 在创建时完成一轮分页，返回可直接交给 creator 的标准固定版式模型。
func composePass(ctx context.Context, blocks []Block, opts Options) (*creator.Document, error) {
	result, err := composePassOnce(ctx, blocks, opts)
	retry, ok := err.(*recipientOverflow)
	if !ok {
		return result, err
	}
	remove := make(map[int]bool, len(retry.indices))
	for _, i := range retry.indices {
		remove[i] = true
		opts.relocatedRecipients = append(opts.relocatedRecipients, blocks[i])
	}
	visible := make([]Block, 0, len(blocks)-len(remove))
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

func composeLayoutPass(ctx context.Context, blocks []Block, opts Options) (*creator.Document, error) {
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
	a := &layoutState{ctx: ctx, opts: opts, faces: make(map[string]*sfnt.Font), anchors: make(map[string]creator.GotoAction), nextNumber: 1, fontResources: make(map[string]creator.Font), fontFamilies: make(map[string]FontFamily)}
	a.footnoteCache = make(map[footnoteKey]*preparedFootnote)
	if err := a.loadFonts(); err != nil {
		return nil, err
	}
	for _, name := range append([]string{opts.Theme.Body.Font}, opts.FallbackFonts...) {
		if name != "" && a.faces[name] == nil {
			return nil, fmt.Errorf("字体 %q 未注册", name)
		}
	}
	e := &engine{state: a, pageIndex: -1, geometry: pageGeometry{PageWidth: opts.PageSize.Width, PageHeight: opts.PageSize.Height,
		MarginTop: opts.MarginTop, MarginRight: opts.MarginRight, MarginBottom: opts.MarginBottom, MarginLeft: opts.MarginLeft}}
	e.computeArea()
	columns, err := normalizeColumns(opts.Columns, e.contentWidth)
	if err != nil {
		return nil, err
	}
	initial := sectionState{geometry: e.geometry, columns: columns, header: opts.Header, footer: opts.Footer, pageNumbers: opts.PageNumbers, numberStyle: "decimal"}
	a.section = initial
	e.setSectionArea(initial, 0)
	active := initial
	prepared := make([]preparedBlock, len(blocks))
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
			e.setSectionArea(active, 0)
			prepared[i] = preparedBlock{block: b, section: &next}
			continue
		}
		if b.Section != nil {
			return nil, fmt.Errorf("blocks[%d]: section 配置需要 type 为 section", b.sourceIndex)
		}
		p, err := e.prepareBlock(b)
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
	prepared, err = e.mergeCallouts(prepared, opts.Theme.Body)
	if err != nil {
		return nil, err
	}
	e.setSectionArea(initial, 0)
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
			e.setSectionArea(a.section, 0)
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
		if (pendingBreak || p.block.PageBreakBefore) && e.pageHasContent() {
			e.newPage()
		} else if pendingColumnBreak && e.hasContent() {
			if opts.balance != nil {
				if a.balanceHardPages == nil {
					a.balanceHardPages = make(map[int]bool)
				}
				a.balanceHardPages[e.pageIndex] = true
			}
			e.nextColumn()
		}
		pendingBreak = false
		pendingColumnBreak = false
		if !letterheadEmitted {
			if err := e.emitOfficialLetterhead(); err != nil {
				return nil, fmt.Errorf("official.letterhead: %w", err)
			}
			letterheadEmitted = true
		}
		if i >= keepEnd {
			var err error
			keepEnd, err = e.ensureKeepChain(prepared, i)
			if err != nil {
				return nil, fmt.Errorf("blocks[%d]: %w", p.block.sourceIndex, err)
			}
		}
		if p.block.KeepTogether && p.height() <= e.contentHeight {
			lines := p.lines
			if p.table != nil {
				lines = p.table.noteLines(0, len(p.table.rowOffsets)-1)
			}
			if err := e.ensureHeight(p.height(), lines); err != nil {
				return nil, fmt.Errorf("blocks[%d]: %w", p.block.sourceIndex, err)
			}
		}
		switch {
		case p.block.Kind == "callout":
			if err := e.placeCallout(ctx, p); err != nil {
				return nil, fmt.Errorf("blocks[%d].callout: %w", p.block.sourceIndex, err)
			}
		case p.fixed != nil:
			e.placeFixed(p)
		case p.image != nil:
			e.placeImage(p)
		case p.table != nil:
			if err := e.placeTable(ctx, p); err != nil {
				return nil, fmt.Errorf("blocks[%d]: %w", p.block.sourceIndex, err)
			}
		default:
			if err := e.placeLines(ctx, p); err != nil {
				return nil, fmt.Errorf("blocks[%d]: %w", p.block.sourceIndex, err)
			}
		}
		pendingBreak = p.block.PageBreakAfter
	}
	if opts.Official != nil && !a.officialFirstBody {
		candidates := recipientCandidates(blocks)
		if len(opts.relocatedRecipients) == 0 && len(candidates) > 0 {
			return nil, &recipientOverflow{indices: candidates, fonts: a.fonts}
		}
		if len(opts.relocatedRecipients) > 0 {
			return nil, fmt.Errorf("主送机关移至版记后首页仍无法显示正文，请缩短版头、标题或调整内容")
		}
		if opts.Official.Letterhead != nil {
			return nil, fmt.Errorf("gbt9704 首页必须排入正文，请缩短版头、标题或调整内容")
		}
	}
	e.ensurePage()
	if err := e.emitOfficialSignature(); err != nil {
		return nil, fmt.Errorf("official.signature: %w", err)
	}
	if err := e.emitOfficialColophon(); err != nil {
		return nil, fmt.Errorf("official.colophon: %w", err)
	}
	if opts.balance != nil {
		opts.balance.capture(e)
	}
	if err := e.emitFootnotes(); err != nil {
		return nil, err
	}
	if err := e.emitPageDecorations(); err != nil {
		return nil, err
	}
	navigationBlocks := blocks
	if opts.officialOriginalBlocks != nil {
		navigationBlocks = opts.officialOriginalBlocks
	}
	bookmarks, err := e.resolveLinks(navigationBlocks)
	if err != nil {
		return nil, err
	}
	return &creator.Document{ID: "flow", PageSize: opts.PageSize, Fonts: a.fonts, Pages: e.pages, Outlines: outlines(a.headings), Bookmarks: bookmarks}, nil
}

// Compose 先排版，再按最终物理页码回填目录与引用；连续两轮目标页码一致才交付。
// 字体来源仅在首轮读取，此后的排版使用同一份字体字节快照。
func Compose(ctx context.Context, source []Block, opts Options) (*creator.Document, error) {
	if err := validateOfficialOptions(opts); err != nil {
		return nil, err
	}
	styled, settings, err := prepareStyles(ctx, source, opts)
	if err != nil {
		return nil, err
	}
	source, opts = styled, settings
	source, err = expandCallouts(ctx, source, opts.Theme.Callouts)
	if err != nil {
		return nil, err
	}
	opts, err = prepareFootnotes(ctx, source, opts)
	if err != nil {
		return nil, err
	}
	if opts.ReferencePageNumbers != "" && opts.ReferencePageNumbers != "physical" && opts.ReferencePageNumbers != "section" {
		return nil, fmt.Errorf("reference_page_numbers 必须为 physical 或 section")
	}
	numbered, labels, err := prepareCaptions(source)
	if err != nil {
		return nil, err
	}
	opts.captionLabels = labels
	opts.titleLabels = make(map[string]string)
	blocks, dynamic, err := prepareNavigation(numbered, opts)
	if err != nil {
		return nil, err
	}
	previous := map[string]int{}
	previousLabels := map[string]string{}
	for pass := 0; pass < navigationPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opts.previousPages = previous
		opts.previousLabels = previousLabels
		opts.resolvedLabels = make(map[string]string)
		result, err := composePass(ctx, blocks, opts)
		if err != nil {
			return nil, err
		}
		if !dynamic {
			return result, nil
		}
		current := make(map[string]int, len(result.Bookmarks))
		for _, bookmark := range result.Bookmarks {
			current[bookmark.Name] = bookmark.Goto.Page
		}
		if maps.Equal(previous, current) && maps.Equal(previousLabels, opts.resolvedLabels) {
			return result, nil
		}
		previous = current
		previousLabels = opts.resolvedLabels
		opts.Fonts = result.Fonts
		opts.fontNamesResolved = true
		// 首轮已把集合提取为单字体，后续排版不得再套用原 TTC 索引。
		opts.FontFaces = nil
	}
	return nil, fmt.Errorf("目录或页码引用在 %d 轮排版后仍未稳定，请调整版心、字号或引用位置", navigationPasses)
}
