package flow

import (
	"fmt"

	"github.com/zc310/ofd/pkg/creator"
)

func resolveCalloutAppearance(c *Callout, theme CalloutTheme) error {
	accent, err := calloutAccent(c.Variant)
	if err != nil {
		return err
	}
	var palette CalloutPalette
	var subtle creator.Color
	switch c.Variant {
	case "":
		palette, subtle = theme.Neutral, creator.Color{R: 248, G: 249, B: 250}
	case "info":
		palette, subtle = theme.Info, creator.Color{R: 231, G: 241, B: 255}
	case "warning":
		palette, subtle = theme.Warning, creator.Color{R: 255, G: 251, B: 235}
	case "danger":
		palette, subtle = theme.Danger, creator.Color{R: 253, G: 237, B: 239}
	case "success":
		palette, subtle = theme.Success, creator.Color{R: 232, G: 245, B: 238}
	}
	switch c.Appearance {
	case "", "outline", "subtle", "minimal":
	default:
		return fmt.Errorf("未知提示框外观 %q: %w", c.Appearance, errContent)
	}
	switch c.Pagination {
	case "", "auto", "keep":
	default:
		return fmt.Errorf("未知提示框分页策略 %q: %w", c.Pagination, errContent)
	}
	if c.AccentColor == nil {
		c.AccentColor = palette.AccentColor
	}
	if c.AccentColor == nil {
		c.AccentColor = &accent
	}
	if c.Fill == nil && c.Appearance == "subtle" {
		c.Fill = palette.Fill
	}
	if c.Fill == nil && c.Appearance == "subtle" {
		c.Fill = &subtle
	}
	if c.BorderColor == nil {
		c.BorderColor = palette.BorderColor
	}
	if c.BorderColor == nil {
		c.BorderColor = &creator.Color{R: 222, G: 226, B: 230}
	}
	return nil
}
