package layout

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// 在目录展开与多轮分页之前解析样式，只复制需要修改的主题、块和表格。
func prepareAuthorStyles(ctx context.Context, source []FlowBlock, opts FlowOptions) ([]FlowBlock, FlowOptions, error) {
	styles := make(map[string]TextStyle, len(opts.Styles))
	visiting := make(map[string]bool)
	for i, named := range opts.Styles {
		if err := ctx.Err(); err != nil {
			return nil, opts, err
		}
		if named.Name == "" || strings.IndexFunc(named.Name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return nil, opts, fmt.Errorf("styles[%d].name: 样式名不能为空或包含空白、控制字符", i)
		}
		if err := ValidateFlowText(named.Name); err != nil {
			return nil, opts, fmt.Errorf("styles[%d].name: %w", i, err)
		}
		if _, exists := styles[named.Name]; exists {
			return nil, opts, fmt.Errorf("styles[%d]: 样式名 %q 重复", i, named.Name)
		}
		styles[named.Name] = named.Style
	}
	var resolve func(TextStyle, int) (TextStyle, error)
	resolve = func(style TextStyle, depth int) (TextStyle, error) {
		if err := ctx.Err(); err != nil {
			return TextStyle{}, err
		}
		if err := validateStyleOverride(style); err != nil {
			return TextStyle{}, err
		}
		if style.BasedOn == "" {
			return style, nil
		}
		name := style.BasedOn
		parent, exists := styles[name]
		if !exists {
			return TextStyle{}, fmt.Errorf("based_on 引用的样式 %q 不存在", name)
		}
		if visiting[name] {
			return TextStyle{}, fmt.Errorf("based_on 样式 %q 包含循环继承", name)
		}
		if depth >= 64 {
			return TextStyle{}, fmt.Errorf("based_on 继承超过 64 层")
		}
		visiting[name] = true
		base, err := resolve(parent, depth+1)
		delete(visiting, name)
		if err != nil {
			return TextStyle{}, fmt.Errorf("样式 %q: %w", name, err)
		}
		style.BasedOn = ""
		return mergeTextStyle(base, style), nil
	}
	// 即使暂未使用，也验证所有定义；顺序固定，错误不依赖 map 遍历。
	for i, named := range opts.Styles {
		if _, err := resolve(TextStyle{BasedOn: named.Name}, 0); err != nil {
			return nil, opts, fmt.Errorf("styles[%d]: %w", i, err)
		}
	}
	if opts.Theme == (Theme{}) {
		opts.Theme = MaterialTheme()
	}
	for i, ptr := range []*TextStyle{&opts.Theme.Body, &opts.Theme.Caption, &opts.Theme.Table, &opts.Theme.Headings[0], &opts.Theme.Headings[1], &opts.Theme.Headings[2], &opts.Theme.Headings[3], &opts.Theme.Headings[4], &opts.Theme.Headings[5]} {
		style, err := resolve(*ptr, 0)
		if err != nil {
			return nil, opts, fmt.Errorf("theme.%s: %w", []string{"body", "caption", "table", "headings[0]", "headings[1]", "headings[2]", "headings[3]", "headings[4]", "headings[5]"}[i], err)
		}
		*ptr = style
	}
	if opts.FootnoteStyle != nil {
		style, err := resolve(*opts.FootnoteStyle, 0)
		if err != nil {
			return nil, opts, fmt.Errorf("footnote_style: %w", err)
		}
		opts.FootnoteStyle = &style
	}
	opts.Footnotes = append([]FlowFootnote(nil), opts.Footnotes...)
	for i := range opts.Footnotes {
		if opts.Footnotes[i].Style == nil {
			continue
		}
		style, err := resolve(*opts.Footnotes[i].Style, 0)
		if err != nil {
			return nil, opts, fmt.Errorf("footnotes[%d].style: %w", i, err)
		}
		opts.Footnotes[i].Style = &style
	}
	blocks := append([]FlowBlock(nil), source...)
	for i := range blocks {
		b := &blocks[i]
		if b.Style != nil {
			style, err := resolve(*b.Style, 0)
			if err != nil {
				return nil, opts, fmt.Errorf("blocks[%d].style: %w", i, err)
			}
			b.Style = &style
		}
		if b.Table == nil || b.Table.Grid == nil {
			continue
		}
		table := *b.Table
		table.Grid = make([][]FlowTableCell, len(b.Table.Grid))
		for r, row := range b.Table.Grid {
			table.Grid[r] = append([]FlowTableCell(nil), row...)
			for c := range table.Grid[r] {
				cell := &table.Grid[r][c]
				if cell.Style == nil {
					continue
				}
				style, err := resolve(*cell.Style, 0)
				if err != nil {
					return nil, opts, fmt.Errorf("blocks[%d].table.grid[%d][%d].style: %w", i, r, c, err)
				}
				cell.Style = &style
			}
		}
		b.Table = &table
	}
	return blocks, opts, nil
}

func validateStyleOverride(s TextStyle) error {
	if s.Set & ^uint32(StyleSize|StyleLineHeight|StyleSpaceBefore|StyleSpaceAfter|StyleFirstLineIndent) != 0 {
		return fmt.Errorf("样式 Set 含未知字段标记")
	}
	// 部分样式省略的字号和行高仅用于验证时补正，显式零值仍返回错误。
	if s.Size == 0 && s.Set&StyleSize == 0 {
		s.Size = 1
	}
	if s.LineHeight == 0 && s.Set&StyleLineHeight == 0 {
		s.LineHeight = 1
	}
	return validateTextStyle(s)
}
