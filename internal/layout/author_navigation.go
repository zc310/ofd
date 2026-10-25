package layout

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"unicode"

	"github.com/zc310/ofd/pkg/creator"
)

const authorNavigationPasses = 12
const authorIDPrefix = "__flow_"

type authorPendingLink struct {
	page, item int
	target     string
}

// Compose 先排版，再按最终物理页码回填目录与引用；连续两轮目标页码一致才交付。
// 字体来源仅在首轮读取，此后的排版使用同一份字体字节快照。
func Compose(ctx context.Context, source []FlowBlock, opts FlowOptions) (*creator.Document, error) {
	if err := validateOfficialOptions(opts); err != nil {
		return nil, err
	}
	styled, settings, err := prepareAuthorStyles(ctx, source, opts)
	if err != nil {
		return nil, err
	}
	source, opts = styled, settings
	opts, err = prepareAuthorFootnotes(ctx, source, opts)
	if err != nil {
		return nil, err
	}
	if opts.ReferencePageNumbers != "" && opts.ReferencePageNumbers != "physical" && opts.ReferencePageNumbers != "section" {
		return nil, fmt.Errorf("reference_page_numbers 必须为 physical 或 section")
	}
	numbered, labels, err := prepareAuthorCaptions(source)
	if err != nil {
		return nil, err
	}
	opts.captionLabels = labels
	opts.titleLabels = make(map[string]string)
	blocks, dynamic, err := prepareAuthorNavigation(numbered, opts)
	if err != nil {
		return nil, err
	}
	previous := map[string]int{}
	previousLabels := map[string]string{}
	for pass := 0; pass < authorNavigationPasses; pass++ {
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
	return nil, fmt.Errorf("目录或页码引用在 %d 轮排版后仍未稳定，请调整版心、字号或引用位置", authorNavigationPasses)
}

func prepareAuthorNavigation(source []FlowBlock, opts FlowOptions) ([]FlowBlock, bool, error) {
	blocks := append([]FlowBlock(nil), source...)
	ids := map[string]bool{}
	dynamic := false
	configs := make(map[int]FlowTOC)
	wanted := make(map[string]bool)
	for i, block := range source {
		if block.Kind == "toc" {
			config, err := authorTOCConfig(block.TOC)
			if err != nil {
				return nil, false, fmt.Errorf("blocks[%d].toc: %w", i, err)
			}
			configs[i] = config
			wanted[config.Source] = true
		}
	}
	for i := range blocks {
		b := &blocks[i]
		b.sourceIndex = i
		if b.Kind == "toc_entry" {
			return nil, false, fmt.Errorf("blocks[%d]: toc_entry 是内部类型", i)
		}
		if b.ID != "" {
			if err := ValidateFlowText(b.ID); err != nil {
				return nil, false, fmt.Errorf("blocks[%d].id: %w", i, err)
			}
			if strings.HasPrefix(b.ID, authorIDPrefix) || strings.IndexFunc(b.ID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
				return nil, false, fmt.Errorf("blocks[%d]: ID 不得包含空白、控制字符或保留前缀 %q", i, authorIDPrefix)
			}
			if ids[b.ID] {
				return nil, false, fmt.Errorf("blocks[%d]: ID %q 重复", i, b.ID)
			}
			if b.Kind == "page_break" || b.Kind == "column_break" || b.Kind == "section" {
				return nil, false, fmt.Errorf("blocks[%d]: 换页或分节块不能设置 ID", i)
			}
			ids[b.ID] = true
		}
		if wanted["headings"] && b.Kind == "heading" && b.ID == "" && authorHeadingTitle(*b) != "" {
			b.ID = fmt.Sprintf("%sheading_%d", authorIDPrefix, i)
			ids[b.ID] = true
		}
		if b.ID == "" && wanted[authorCaptionSource(*b)] {
			b.ID = fmt.Sprintf("%scatalog_%d", authorIDPrefix, i)
			ids[b.ID] = true
		}
		if b.ID != "" {
			opts.titleLabels[b.ID] = authorReferenceTitle(*b)
			if b.captionLabel != "" {
				opts.captionLabels[b.ID] = b.captionLabel
			}
		}
	}
	checkRuns := func(runs []Inline) error {
		for _, run := range runs {
			if run.TitleRef != "" {
				if run.Text != "" || run.Href != "" || run.Target != "" || run.PageRef != "" || run.CaptionRef != "" || run.Footnote != "" {
					return fmt.Errorf("title_ref 与 text、href、target、page_ref、caption_ref、footnote 互斥")
				}
				if opts.titleLabels[run.TitleRef] == "" {
					return fmt.Errorf("title_ref 的目标 %q 必须为非空章节标题或图表题注", run.TitleRef)
				}
			}
			if run.Footnote != "" {
				if run.Text != "" || run.Href != "" || run.Target != "" || run.PageRef != "" || run.CaptionRef != "" {
					return fmt.Errorf("footnote 与 text、href、target、page_ref、caption_ref 互斥")
				}
				if opts.noteNumbers[run.Footnote] == 0 {
					return fmt.Errorf("脚注 %q 不存在", run.Footnote)
				}
			}
			if run.CaptionRef != "" {
				if run.Text != "" || run.Href != "" || run.Target != "" || run.PageRef != "" {
					return fmt.Errorf("caption_ref 与 text、href、target、page_ref 互斥")
				}
				if opts.captionLabels[run.CaptionRef] == "" {
					return fmt.Errorf("caption_ref 的目标 %q 必须是带编号的图或表", run.CaptionRef)
				}
			}
			if run.PageRef != "" {
				if run.Text != "" || run.Href != "" || run.Target != "" {
					return fmt.Errorf("page_ref 与 text、href、target 互斥")
				}
				dynamic = true
			}
			if run.Href != "" && run.Target != "" {
				return fmt.Errorf("href 与 target 不能同时设置")
			}
			for _, target := range []string{run.Target, run.PageRef, run.CaptionRef, run.TitleRef} {
				if target != "" && !ids[target] {
					return fmt.Errorf("引用的内容 ID %q 不存在", target)
				}
			}
		}
		return nil
	}
	for i, b := range blocks {
		if b.Section != nil && b.Kind != "section" {
			return nil, false, fmt.Errorf("blocks[%d]: section 配置需要 type 为 section", i)
		}
		if b.TOC != nil && b.Kind != "toc" {
			return nil, false, fmt.Errorf("blocks[%d]: toc 配置需要 type 为 toc", i)
		}
		if err := checkRuns(b.Runs); err != nil {
			return nil, false, fmt.Errorf("blocks[%d]: %w", i, err)
		}
		if b.Kind == "heading" {
			for _, run := range b.Runs {
				if run.PageRef != "" || run.CaptionRef != "" || run.TitleRef != "" || run.Footnote != "" {
					return nil, false, fmt.Errorf("blocks[%d]: 标题不支持 page_ref、caption_ref、title_ref 或 footnote，请放在正文中", i)
				}
			}
		}
		if b.Table != nil {
			for r, row := range b.Table.Grid {
				for c, cell := range row {
					if err := checkRuns(cell.Runs); err != nil {
						return nil, false, fmt.Errorf("blocks[%d].grid[%d][%d]: %w", i, r, c, err)
					}
				}
			}
		}
	}
	for i, note := range opts.Footnotes {
		if err := checkRuns(note.Runs); err != nil {
			return nil, false, fmt.Errorf("footnotes[%d]: %w", i, err)
		}
	}
	var expanded []FlowBlock
	for i, b := range blocks {
		if b.Kind != "toc" {
			expanded = append(expanded, b)
			continue
		}
		dynamic = true
		if b.Text != "" || len(b.Runs) > 0 || b.Image != nil || b.Table != nil || b.Fixed != nil {
			return nil, false, fmt.Errorf("blocks[%d]: 目录块不能携带 text、runs、image、table 或 fixed", i)
		}
		config := configs[i]
		var entries []FlowBlock
		for _, target := range blocks {
			level := 1
			if config.Source == "headings" {
				if target.Kind != "heading" || target.Level > config.MaxLevel || authorHeadingTitle(target) == "" {
					continue
				}
				level = target.Level
			} else if authorCaptionSource(target) != config.Source {
				continue
			}
			entries = append(entries, FlowBlock{Kind: "toc_entry", Level: level, Text: authorReferenceTitle(target), tocTarget: target.ID, Style: b.Style, Orphans: 1, Widows: 1, sourceIndex: i})
		}
		if len(entries) == 0 {
			message := map[string]string{"headings": "目录范围内没有非空标题", "figures": "图目录范围内没有带编号的图片或固定图元", "tables": "表目录范围内没有带编号的表格"}[config.Source]
			return nil, false, fmt.Errorf("blocks[%d]: %s", i, message)
		}
		if config.Title != "" {
			style := opts.Theme.Headings[0]
			if opts.Theme == (Theme{}) {
				style = MaterialTheme().Headings[0]
			}
			expanded = append(expanded, FlowBlock{ID: b.ID, Kind: "paragraph", Text: config.Title, Style: &style, KeepWithNext: true, PageBreakBefore: b.PageBreakBefore, sourceIndex: i})
		} else {
			entries[0].ID = b.ID
			entries[0].PageBreakBefore = b.PageBreakBefore
		}
		entries[len(entries)-1].PageBreakAfter = b.PageBreakAfter
		expanded = append(expanded, entries...)
	}
	return expanded, dynamic, nil
}

func (e *engine) recordAuthorAnchor(id string) {
	if id == "" {
		return
	}
	top := e.opts.PageHeight - e.y
	e.author.anchors[id] = e.authorDestination(top)
}

// 文内链接写标准 Goto/Dest（GB/T 33190—2016 §14.2），内容 ID 写标准 Bookmark
// （标准印刷页 16–17 的图 11、表 10）。
func (e *engine) resolveAuthorLinks(blocks []FlowBlock) ([]creator.Bookmark, error) {
	var bookmarks []creator.Bookmark
	for _, b := range blocks {
		if b.ID == "" {
			continue
		}
		target, ok := e.author.anchors[b.ID]
		if !ok {
			return nil, fmt.Errorf("内容 ID %q 没有可定位的内容", b.ID)
		}
		bookmarks = append(bookmarks, creator.Bookmark{Name: b.ID, Goto: target})
		label := fmt.Sprint(target.Page + 1)
		if e.author.opts.ReferencePageNumbers == "section" {
			label = e.author.pageSettings[target.Page].label
		}
		e.author.opts.resolvedLabels[b.ID] = label
	}
	for _, link := range e.author.pendingLinks {
		target, ok := e.author.anchors[link.target]
		if !ok {
			return nil, fmt.Errorf("内容 ID %q 没有可定位的内容", link.target)
		}
		text := e.pages[link.page].Items[link.item].(creator.Text)
		text.Actions = []creator.Action{{Event: creator.ActionEventClick, Goto: &target}}
		e.pages[link.page].Items[link.item] = text
	}
	return bookmarks, nil
}
