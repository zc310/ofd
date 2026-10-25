package layout

import "fmt"

// 编号只取决于逻辑内容顺序，在目录及页码迭代前生成；不修改调用方的图片或表格。
func prepareAuthorCaptions(source []FlowBlock) ([]FlowBlock, map[string]string, error) {
	blocks := append([]FlowBlock(nil), source...)
	labels := make(map[string]string)
	figures, tables := 0, 0
	for i := range blocks {
		b := &blocks[i]
		var caption, prefix *string
		numbered := false
		kind, number := "", 0
		if b.Kind == "image" && b.Image != nil {
			img := *b.Image
			b.Image = &img
			caption, prefix, numbered, kind = &img.Caption, &img.CaptionPrefix, img.Numbered, "Figure"
			if numbered {
				figures++
				number = figures
			}
		}
		if b.Kind == "fixed" && b.Fixed != nil {
			fixed := *b.Fixed
			b.Fixed = &fixed
			caption, prefix, numbered, kind = &fixed.Caption, &fixed.CaptionPrefix, fixed.Numbered, "Figure"
			if numbered {
				figures++
				number = figures
			}
		}
		if b.Kind == "table" && b.Table != nil {
			table := *b.Table
			b.Table = &table
			caption, prefix, numbered, kind = &table.Caption, &table.CaptionPrefix, table.Numbered, "Table"
			if numbered {
				tables++
				number = tables
			}
			if table.RepeatCaption && !numbered && table.Caption == "" {
				return nil, nil, fmt.Errorf("blocks[%d]: repeat_caption 需要非空题注或 numbered", i)
			}
			if err := ValidateFlowText(table.ContinuationSuffix); err != nil {
				return nil, nil, fmt.Errorf("blocks[%d].continuation_suffix: %w", i, err)
			}
		}
		if caption == nil {
			continue
		}
		for _, text := range []string{*caption, *prefix} {
			if err := ValidateFlowText(text); err != nil {
				return nil, nil, fmt.Errorf("blocks[%d].caption: %w", i, err)
			}
		}
		if !numbered && *prefix != "" {
			return nil, nil, fmt.Errorf("blocks[%d]: caption_prefix 需要 numbered", i)
		}
		if !numbered {
			continue
		}
		label := *prefix
		if label == "" {
			label = kind
		}
		label = fmt.Sprintf("%s %d", label, number)
		b.captionLabel = label
		if b.ID != "" {
			labels[b.ID] = label
		}
		if *caption != "" {
			*caption = label + "  " + *caption
		} else {
			*caption = label
		}
	}
	return blocks, labels, nil
}

func (e *engine) prepareTableCaptions(t *authorTable, table *FlowTable) error {
	if table.Caption == "" {
		return nil
	}
	t.captionStyle = mergeTextStyle(e.author.opts.Theme.Body, e.author.opts.Theme.Caption)
	if err := validateTextStyle(t.captionStyle); err != nil {
		return err
	}
	t.caption = e.authorLines([]Inline{{Text: table.Caption}}, t.captionStyle, e.contentWidth, 0)
	if table.RepeatCaption {
		suffix := table.ContinuationSuffix
		if suffix == "" {
			suffix = "(continued)"
		}
		t.continuation = e.authorLines([]Inline{{Text: table.Caption + " " + suffix}}, t.captionStyle, e.contentWidth, 0)
	}
	for _, lines := range [][]authorLine{t.caption, t.continuation} {
		for _, line := range lines {
			if e.lineWidth(line.atoms) > e.contentWidth+0.001 {
				return fmt.Errorf("表题文本超过行宽")
			}
		}
	}
	return nil
}

func (e *engine) drawTableCaption(t *authorTable, continued bool) {
	lines := t.caption
	if continued {
		lines = t.continuation
	}
	if len(lines) == 0 {
		return
	}
	e.y -= t.captionStyle.SpaceBefore
	for _, line := range lines {
		e.drawAuthorLine(line, t.captionStyle, e.contentLeft, e.contentWidth, e.y)
		e.y -= line.height
	}
	e.y -= t.captionStyle.SpaceAfter
}
