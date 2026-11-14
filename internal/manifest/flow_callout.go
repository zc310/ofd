package manifest

import (
	"errors"
	"fmt"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

var errCalloutChild = errors.New("提示框子块不支持 ID、嵌套内容或分页控制")

func buildFlowCallout(c *FlowCallout) (*flow.Callout, error) {
	fill, err := buildColor(c.Fill)
	if err != nil {
		return nil, fmt.Errorf("fill: %w", err)
	}
	accent, err := buildColor(c.AccentColor)
	if err != nil {
		return nil, fmt.Errorf("accent_color: %w", err)
	}
	border, err := buildColor(c.BorderColor)
	if err != nil {
		return nil, fmt.Errorf("border_color: %w", err)
	}
	result := &flow.Callout{Variant: c.Variant, Title: c.Title, Text: c.Text, ShowIcon: c.ShowIcon, Fill: fill, AccentColor: accent, BorderColor: border, Appearance: c.Appearance, Pagination: c.Pagination, RepeatTitle: c.RepeatTitle, ContinuationSuffix: c.ContinuationSuffix}
	for _, r := range c.Runs {
		result.Runs = append(result.Runs, flow.Run{Text: r.Text, Font: r.Font, Bold: r.Bold, Italic: r.Italic, Strike: r.Strike, Href: r.Href, Target: r.Target, PageRef: r.PageRef, CaptionRef: r.CaptionRef, TitleRef: r.TitleRef, Footnote: r.Footnote})
	}
	for i, b := range c.Blocks {
		if b.ID != "" || b.Callout != nil || b.Level != 0 || b.Image != nil || b.Table != nil || b.Fixed != nil || b.TOC != nil || b.Section != nil || b.KeepTogether || b.KeepWithNext || b.PageBreakBefore || b.PageBreakAfter || b.Orphans != 0 || b.Widows != 0 {
			return nil, fmt.Errorf("blocks[%d]: %w", i, errCalloutChild)
		}
		var style *flow.TextStyle
		if b.Style != nil {
			style, err = buildFlowTextStyle(b.Style)
			if err != nil {
				return nil, fmt.Errorf("blocks[%d].style: %w", i, err)
			}
		}
		child := flow.Block{Kind: b.Type, Text: b.Text, Marker: b.Marker, Style: style, CodeLang: b.CodeLang}
		for _, r := range b.Runs {
			child.Runs = append(child.Runs, flow.Run{Text: r.Text, Font: r.Font, Bold: r.Bold, Italic: r.Italic, Strike: r.Strike, Href: r.Href, Target: r.Target, PageRef: r.PageRef, CaptionRef: r.CaptionRef, TitleRef: r.TitleRef, Footnote: r.Footnote})
		}
		result.Blocks = append(result.Blocks, child)
	}
	return result, nil
}

func buildFlowCalloutTheme(src *FlowCalloutTheme, dst *flow.CalloutTheme) error {
	if src == nil {
		return nil
	}
	for _, pair := range []struct {
		name string
		src  FlowCalloutPalette
		dst  *flow.CalloutPalette
	}{
		{"neutral", src.Neutral, &dst.Neutral}, {"info", src.Info, &dst.Info}, {"warning", src.Warning, &dst.Warning}, {"danger", src.Danger, &dst.Danger}, {"success", src.Success, &dst.Success},
	} {
		for _, field := range []struct {
			name string
			src  *Color
			dst  **creator.Color
		}{
			{"accent_color", pair.src.AccentColor, &pair.dst.AccentColor}, {"fill", pair.src.Fill, &pair.dst.Fill}, {"border_color", pair.src.BorderColor, &pair.dst.BorderColor},
		} {
			if field.src == nil {
				continue
			}
			color, err := buildColor(field.src)
			if err != nil {
				return fmt.Errorf("callouts.%s.%s: %w", pair.name, field.name, err)
			}
			*field.dst = color
		}
	}
	return nil
}
