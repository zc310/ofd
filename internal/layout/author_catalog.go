package layout

import (
	"fmt"
	"strings"
)

func authorTOCConfig(value *FlowTOC) (FlowTOC, error) {
	config := FlowTOC{Title: "Contents", MaxLevel: 3, Source: "headings"}
	if value != nil {
		config = *value
	}
	if config.Source == "" {
		config.Source = "headings"
	}
	switch config.Source {
	case "headings":
		if config.MaxLevel == 0 {
			config.MaxLevel = 3
		}
		if config.MaxLevel < 1 || config.MaxLevel > 6 {
			return config, fmt.Errorf("目录 max_level 必须为 1 到 6")
		}
	case "figures", "tables":
		if config.MaxLevel != 0 {
			return config, fmt.Errorf("max_level 仅用于 headings 标题目录")
		}
	default:
		return config, fmt.Errorf("目录 source 必须为 headings、figures 或 tables")
	}
	if err := ValidateFlowText(config.Title); err != nil {
		return config, err
	}
	return config, nil
}

// 图目录与表目录仅收集带编号的逻辑块，重复表头、续表题及跨栏片段不会增加条目。
func authorCaptionSource(block FlowBlock) string {
	if block.captionLabel == "" {
		return ""
	}
	switch block.Kind {
	case "image", "fixed":
		return "figures"
	case "table":
		return "tables"
	}
	return ""
}

// 标题引用统一为纯文字，格式由引用所在位置决定，空白按标题目录规则折叠。
func authorReferenceTitle(block FlowBlock) string {
	if block.Kind == "heading" {
		return authorHeadingTitle(block)
	}
	caption := ""
	switch {
	case block.Kind == "image" && block.Image != nil:
		caption = block.Image.Caption
	case block.Kind == "fixed" && block.Fixed != nil:
		caption = block.Fixed.Caption
	case block.Kind == "table" && block.Table != nil:
		caption = block.Table.Caption
	}
	return strings.Join(strings.Fields(caption), " ")
}
