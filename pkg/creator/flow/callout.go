package flow

import (
	"errors"

	"github.com/zc310/ofd/pkg/creator"
)

// Callout 是带直线左色条的提示框。Variant 支持空值（中性）、info、warning、danger、success。
// Title 可省略；Blocks 支持段落、列表与代码，不支持嵌套、子块 ID 或子块分页控制。
// 短提示框尽量同栏，长内容自动跨页或跨栏，标题不加入大纲。
type Callout struct {
	Variant string
	Title   string
	// Text、Runs、Blocks 三者互斥，分别用于纯文本、富文本和多块正文。
	Text   string
	Runs   []Run
	Blocks []Block
	// ShowIcon 在标题或无标题时的正文首行显示内置矢量图标，仅显示一次。
	ShowIcon bool
	// Fill 覆盖当前外观背景色；AccentColor 覆盖左色条、标题与图标颜色。
	Fill        *creator.Color
	AccentColor *creator.Color
	// BorderColor 覆盖细边框颜色。
	BorderColor *creator.Color
	// Appearance 支持 outline（默认）、subtle（浅色底）、minimal（仅左色条）。
	Appearance string
	// Pagination 支持 auto（默认，短框尽量同栏）和 keep（严格同栏，超高报错）。
	Pagination string
	// RepeatTitle 在续页或续栏重复标题；ContinuationSuffix 默认 " (continued)"。
	RepeatTitle        bool
	ContinuationSuffix string
}

// CalloutPalette 定义单种提示语义的配色，nil 字段沿用内置配色；Fill 用于 subtle 外观。
type CalloutPalette struct {
	AccentColor *creator.Color
	Fill        *creator.Color
	BorderColor *creator.Color
}

// CalloutTheme 定义文档级提示框配色；单个 Callout 的配色优先。
type CalloutTheme struct {
	Neutral CalloutPalette
	Info    CalloutPalette
	Warning CalloutPalette
	Danger  CalloutPalette
	Success CalloutPalette
}

// AddCallout 添加提示框。
func (d *Document) AddCallout(callout Callout) {
	d.Blocks = append(d.Blocks, Block{Kind: "callout", Callout: &callout})
}

var (
	errCalloutChildKind   = errors.New("提示框仅支持 paragraph、list_item 和 code 子块")
	errCalloutChildFields = errors.New("提示框子块不支持 ID、嵌套内容、图标、色条或分页控制")
)

func validateCalloutChild(b Block) error {
	if b.Kind != "" && b.Kind != "paragraph" && b.Kind != "list_item" && b.Kind != "code" {
		return errCalloutChildKind
	}
	if b.ID != "" || b.Callout != nil || b.Image != nil || b.Table != nil || b.Fixed != nil || b.TOC != nil || b.Section != nil || b.LeadingIcon != nil || b.LeftBorderColor != nil || b.CodeLang != "" && b.Kind != "code" || b.Level != 0 || b.KeepTogether || b.KeepWithNext || b.PageBreakBefore || b.PageBreakAfter || b.Orphans != 0 || b.Widows != 0 {
		return errCalloutChildFields
	}
	return nil
}
