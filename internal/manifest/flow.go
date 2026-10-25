package manifest

import (
	"context"
	"fmt"
	"math"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

func (f *Flow) build(meta creator.Document, store assetStore) (creator.Document, error) {
	opts := flow.DefaultOptions()
	if f.Profile != "" {
		if f.Profile != "gbt9704" {
			return creator.Document{}, fmt.Errorf("未知 profile %q", f.Profile)
		}
		if f.Theme != "" {
			return creator.Document{}, fmt.Errorf("profile 与 theme 互斥")
		}
		if f.Official == nil {
			return creator.Document{}, fmt.Errorf("gbt9704 需要 official.fonts 配置")
		}
		fonts := f.Official.Fonts
		opts = flow.GBT9704Options(flow.OfficialFonts{Body: fonts.Body, Title: fonts.Title, Hei: fonts.Hei, Kai: fonts.Kai, Song: fonts.Song})
		if f.Official.Letterhead != nil {
			v := flow.OfficialLetterhead(*f.Official.Letterhead)
			opts.Official.Letterhead = &v
		}
		if f.Official.Signature != nil {
			v := flow.OfficialSignature(*f.Official.Signature)
			opts.Official.Signature = &v
		}
		if f.Official.Colophon != nil {
			v := flow.OfficialColophon(*f.Official.Colophon)
			opts.Official.Colophon = &v
		}
		if f.Font != "" || f.HeadingFont != "" {
			return creator.Document{}, fmt.Errorf("gbt9704 请通过 official.fonts 指定分工字体")
		}
		if f.BodySize != 0 && f.BodySize != 16 || f.LineHeight != 0 && math.Abs(f.LineHeight-opts.Theme.Body.LineHeight) > 0.000001 {
			return creator.Document{}, fmt.Errorf("gbt9704 基础正文使用三号与 22 行版式；局部特殊调整请使用块 style")
		}
	} else if f.Official != nil {
		return creator.Document{}, fmt.Errorf("official 需要 profile: gbt9704")
	}
	switch f.Theme {
	case "", "material":
	case "material2":
		opts.Theme = flow.Material2Theme()
	case "material3":
		opts.Theme = flow.Material3Theme()
	case "fluent2":
		opts.Theme = flow.Fluent2Theme()
	case "carbon":
		opts.Theme = flow.CarbonTheme()
	case "bootstrap5":
		opts.Theme = flow.Bootstrap5Theme()
	default:
		return creator.Document{}, fmt.Errorf("未知主题 %q，可用 material、material2、material3、fluent2、carbon 或 bootstrap5", f.Theme)
	}
	if f.Columns != nil {
		opts.Columns = flow.Columns(*f.Columns)
	}
	for i, named := range f.Styles {
		style, err := buildFlowTextStyle(&named.Style)
		if err != nil {
			return creator.Document{}, fmt.Errorf("styles[%d].style: %w", i, err)
		}
		opts.Styles = append(opts.Styles, flow.NamedStyle{Name: named.Name, Style: *style})
	}
	if f.FootnoteStyle != nil {
		style, err := buildFlowTextStyle(f.FootnoteStyle)
		if err != nil {
			return creator.Document{}, fmt.Errorf("footnote_style: %w", err)
		}
		opts.FootnoteStyle = style
	}
	for i, definition := range f.Footnotes {
		note := flow.Footnote{ID: definition.ID, Text: definition.Text}
		if definition.Style != nil {
			style, err := buildFlowTextStyle(definition.Style)
			if err != nil {
				return creator.Document{}, fmt.Errorf("footnotes[%d].style: %w", i, err)
			}
			note.Style = style
		}
		for _, r := range definition.Runs {
			note.Runs = append(note.Runs, flow.Run{Text: r.Text, Font: r.Font, Bold: r.Bold, Italic: r.Italic, Strike: r.Strike, Href: r.Href, Target: r.Target, PageRef: r.PageRef, CaptionRef: r.CaptionRef, TitleRef: r.TitleRef, Footnote: r.Footnote})
		}
		opts.Footnotes = append(opts.Footnotes, note)
	}
	if meta.PageSize != (creator.PageSize{}) {
		opts.PageSize = meta.PageSize
	}
	opts.Fonts = append(opts.Fonts, meta.Fonts...)
	opts.FallbackFonts = f.FallbackFonts
	opts.FontFaces = f.FontFaces
	for _, family := range f.FontFamilies {
		opts.FontFamilies = append(opts.FontFamilies, flow.FontFamily(family))
	}
	if f.Margins != nil {
		opts.MarginTop = f.Margins.Top
		opts.MarginRight = f.Margins.Right
		opts.MarginBottom = f.Margins.Bottom
		opts.MarginLeft = f.Margins.Left
	}
	if f.PageNumbers != nil {
		opts.PageNumbers = *f.PageNumbers
	}
	opts.GenerateOutlines = f.GenerateOutlines
	opts.ReferencePageNumbers = f.ReferencePageNumbers
	opts.Header, opts.Footer = f.Header, f.Footer
	if f.Font != "" {
		opts.Theme.Body.Font = f.Font
		opts.Theme.Caption.Font = f.Font
		opts.Theme.Table.Font = f.Font
		for i := range opts.Theme.Headings {
			opts.Theme.Headings[i].Font = f.Font
		}
		opts.BoldFont, opts.ItalicFont, opts.BoldItalicFont = "", "", ""
	}
	if f.HeadingFont != "" {
		for i := range opts.Theme.Headings {
			opts.Theme.Headings[i].Font = f.HeadingFont
		}
	}
	if f.BoldFont != "" {
		opts.BoldFont = f.BoldFont
	}
	if f.ItalicFont != "" {
		opts.ItalicFont = f.ItalicFont
	}
	if f.BoldItalicFont != "" {
		opts.BoldItalicFont = f.BoldItalicFont
	}
	if f.BodySize != 0 {
		opts.Theme.Body.Size = f.BodySize
	}
	if f.LineHeight != 0 {
		opts.Theme.Body.LineHeight = f.LineHeight
	}
	meta.Fonts, meta.PageSize = nil, creator.PageSize{}
	d := flow.New(opts)
	d.Metadata = meta
	for i, b := range f.Blocks {
		block := flow.Block{ID: b.ID, Kind: b.Type, Text: b.Text, Level: b.Level, Marker: b.Marker, KeepWithNext: b.KeepWithNext, KeepTogether: b.KeepTogether,
			PageBreakBefore: b.PageBreakBefore, PageBreakAfter: b.PageBreakAfter, Orphans: b.Orphans, Widows: b.Widows}
		for _, r := range b.Runs {
			block.Runs = append(block.Runs, flow.Run{Text: r.Text, Font: r.Font, Bold: r.Bold, Italic: r.Italic, Strike: r.Strike, Href: r.Href, Target: r.Target, PageRef: r.PageRef, CaptionRef: r.CaptionRef, TitleRef: r.TitleRef, Footnote: r.Footnote})
		}
		if b.Style != nil {
			style, err := buildFlowTextStyle(b.Style)
			if err != nil {
				return creator.Document{}, fmt.Errorf("blocks[%d].style: %w", i, err)
			}
			block.Style = style
		}
		if b.Image != nil {
			img := b.Image
			data, err := store.loadData(img.File, img.DataBase64)
			if err != nil {
				return creator.Document{}, fmt.Errorf("blocks[%d].image: %w", i, err)
			}
			block.Image = &flow.Image{Data: data, Width: img.Width, Height: img.Height, Caption: img.Caption, Numbered: img.Numbered, CaptionPrefix: img.CaptionPrefix}
		}
		if b.Fixed != nil {
			v := b.Fixed
			items, err := buildItems(v.Items)
			if err != nil {
				return creator.Document{}, fmt.Errorf("blocks[%d].fixed: %w", i, err)
			}
			block.Fixed = &flow.Fixed{Width: v.Width, Height: v.Height, Align: v.Align, Items: items, Caption: v.Caption, Numbered: v.Numbered, CaptionPrefix: v.CaptionPrefix}
		}
		if b.TOC != nil {
			toc := flow.TOC(*b.TOC)
			block.TOC = &toc
		}
		if b.Section != nil {
			s := b.Section
			block.Section = &flow.Section{Header: s.Header, Footer: s.Footer, PageNumbers: s.PageNumbers, StartPageNumber: s.StartPageNumber, PageNumberStyle: s.PageNumberStyle,
				PageNumberPrefix: s.PageNumberPrefix, FirstPageHeader: s.FirstPageHeader, FirstPageFooter: s.FirstPageFooter, HideFirstPageNumber: s.HideFirstPageNumber}
			if s.Columns != nil {
				c := flow.Columns(*s.Columns)
				block.Section.Columns = &c
			}
			if s.PageSize != nil {
				block.Section.PageSize = &creator.PageSize{Width: s.PageSize.Width, Height: s.PageSize.Height}
			}
			if s.Margins != nil {
				block.Section.Margins = &flow.Margins{Top: s.Margins.Top, Right: s.Margins.Right, Bottom: s.Margins.Bottom, Left: s.Margins.Left}
			}
		}
		if b.Table != nil {
			appearance, err := buildFlowCellStyle(b.Table.CellStyle)
			if err != nil {
				return creator.Document{}, fmt.Errorf("blocks[%d].table.cell_style: %w", i, err)
			}
			stripe, err := buildColor(b.Table.StripeFill)
			if err != nil {
				return creator.Document{}, fmt.Errorf("blocks[%d].table.stripe_fill: %w", i, err)
			}
			block.Table = &flow.Table{Header: b.Table.Header, Rows: b.Table.Rows, ColumnWidths: b.Table.ColumnWidths, HeaderRows: b.Table.HeaderRows,
				CellStyle: appearance, StripeFill: stripe, Caption: b.Table.Caption, Numbered: b.Table.Numbered, CaptionPrefix: b.Table.CaptionPrefix,
				RepeatCaption: b.Table.RepeatCaption, ContinuationSuffix: b.Table.ContinuationSuffix, SplitRows: b.Table.SplitRows}
			for _, column := range b.Table.Columns {
				block.Table.Columns = append(block.Table.Columns, flow.TableColumn(column))
			}
			if b.Table.Grid != nil {
				block.Table.Grid = make([][]flow.TableCell, len(b.Table.Grid))
			}
			for r, row := range b.Table.Grid {
				for c, cell := range row {
					appearance, err := buildFlowCellStyle(cell.Appearance)
					if err != nil {
						return creator.Document{}, fmt.Errorf("blocks[%d].table.grid[%d][%d].appearance: %w", i, r, c, err)
					}
					value := flow.TableCell{Text: cell.Text, ColSpan: cell.ColSpan, RowSpan: cell.RowSpan, Appearance: appearance}
					for _, run := range cell.Runs {
						value.Runs = append(value.Runs, flow.Run{Text: run.Text, Font: run.Font, Bold: run.Bold, Italic: run.Italic, Strike: run.Strike, Href: run.Href, Target: run.Target, PageRef: run.PageRef, CaptionRef: run.CaptionRef, TitleRef: run.TitleRef, Footnote: run.Footnote})
					}
					if cell.Style != nil {
						style, err := buildFlowTextStyle(cell.Style)
						if err != nil {
							return creator.Document{}, fmt.Errorf("blocks[%d].table.grid[%d][%d].style: %w", i, r, c, err)
						}
						value.Style = style
					}
					block.Table.Grid[r] = append(block.Table.Grid[r], value)
				}
			}
		}
		d.Blocks = append(d.Blocks, block)
	}
	result, err := d.Build(context.Background())
	if err != nil {
		return creator.Document{}, err
	}
	return *result, nil
}

func buildFlowTextStyle(s *FlowStyle) (*flow.TextStyle, error) {
	color, err := buildColor(s.Color)
	if err != nil {
		return nil, fmt.Errorf("color: %w", err)
	}
	v := &flow.TextStyle{BasedOn: s.BasedOn, Font: s.Font, Align: s.Align, Color: color}
	for i, field := range []*float64{s.Size, s.LineHeight, s.SpaceBefore, s.SpaceAfter, s.FirstLineIndent} {
		if field == nil {
			continue
		}
		*[]*float64{&v.Size, &v.LineHeight, &v.SpaceBefore, &v.SpaceAfter, &v.FirstLineIndent}[i] = *field
		v.Set |= uint32(1 << i)
	}
	return v, nil
}

func buildFlowCellStyle(s *FlowCellStyle) (*flow.CellStyle, error) {
	if s == nil {
		return nil, nil
	}
	fill, err := buildColor(s.Fill)
	if err != nil {
		return nil, fmt.Errorf("fill: %w", err)
	}
	v := &flow.CellStyle{Fill: fill, VerticalAlign: s.VerticalAlign}
	if s.Padding != nil {
		p := flow.Margins(*s.Padding)
		v.Padding = &p
	}
	if s.Borders != nil {
		v.Borders = &flow.CellBorders{}
		for i, b := range []*FlowTableBorder{s.Borders.Top, s.Borders.Right, s.Borders.Bottom, s.Borders.Left} {
			if b == nil {
				continue
			}
			color, err := buildColor(b.Color)
			if err != nil {
				return nil, fmt.Errorf("borders.%s.color: %w", []string{"top", "right", "bottom", "left"}[i], err)
			}
			*[]**flow.TableBorder{&v.Borders.Top, &v.Borders.Right, &v.Borders.Bottom, &v.Borders.Left}[i] = &flow.TableBorder{Width: b.Width, Color: color}
		}
	}
	return v, nil
}
