package layout

import "github.com/zc310/ofd/pkg/creator"

// Material2Theme 将 Material 2 Web 的 Headline 1–6、Body 和 Caption 角色用于印刷排版。
// 按 96px = 72pt 映射字号；Light 使用内置 Go Regular，Medium 使用 Go Bold，段间距与配色为印刷预设。
// 角色来源：https://github.com/material-components/material-components-web/blob/v14.0.0/packages/mdc-typography/_typography.scss
func Material2Theme() Theme {
	text := creator.Color{R: 0x21, G: 0x21, B: 0x21}
	secondary := creator.Color{R: 0x75, G: 0x75, B: 0x75}
	border := creator.Color{R: 0xe0, G: 0xe0, B: 0xe0}
	header := creator.Color{R: 0xf5, G: 0xf5, B: 0xf5}
	body := TextStyle{Font: "Flow-Regular", Size: 12, LineHeight: 1.5, SpaceAfter: ptToMM(8), Color: &text, Align: "left"}
	t := Theme{Body: body, Caption: body, Table: body, BorderColor: &border, HeaderFill: &header}
	sizes := [...]float64{96, 60, 48, 34, 24, 20}
	heights := [...]float64{96, 60, 50, 40, 32, 32}
	for i, size := range sizes {
		font := "Flow-Regular"
		if i == 5 {
			font = "Flow-Bold"
		}
		t.Headings[i] = TextStyle{Font: font, Size: size * 0.75, LineHeight: heights[i] / size,
			SpaceBefore: ptToMM(16), SpaceAfter: ptToMM(8), Color: &text, Align: "left"}
	}
	// 正文采用 Body 1，表格采用 Body 2，图注采用 Caption。
	t.Caption.Size = 9
	t.Caption.LineHeight = 20.0 / 12
	t.Caption.SpaceBefore = ptToMM(4)
	t.Caption.Color = &secondary
	t.Table.Size = 10.5
	t.Table.LineHeight = 20.0 / 14
	return t
}

// Material3Theme 将 Material 3 Web 的 Headline、Title 和 Body 角色用于印刷排版。
// 按 96px = 72pt 映射字号，段间距为印刷预设；Medium 字重使用内置 Go Bold 字体面。
// 角色来源：https://github.com/material-components/material-web/blob/main/tokens/versions/v0_192/_md-sys-typescale.scss
func Material3Theme() Theme {
	text := creator.Color{R: 0x1d, G: 0x1b, B: 0x20}
	secondary := creator.Color{R: 0x49, G: 0x45, B: 0x4f}
	border := creator.Color{R: 0xca, G: 0xc4, B: 0xd0}
	header := creator.Color{R: 0xf3, G: 0xed, B: 0xf7}
	body := TextStyle{Font: "Flow-Regular", Size: 12, LineHeight: 1.5, SpaceAfter: ptToMM(8), Color: &text, Align: "left"}
	t := Theme{Body: body, Caption: body, Table: body, BorderColor: &border, HeaderFill: &header}
	// 六级标题依次对应 Headline Large/Medium/Small 和 Title Large/Medium/Small。
	sizes := [...]float64{32, 28, 24, 22, 16, 14}
	heights := [...]float64{40, 36, 32, 28, 24, 20}
	for i, size := range sizes {
		font := "Flow-Regular"
		if i >= 4 {
			font = "Flow-Bold"
		}
		t.Headings[i] = TextStyle{Font: font, Size: size * 0.75, LineHeight: heights[i] / size,
			SpaceBefore: ptToMM(16), SpaceAfter: ptToMM(8), Color: &text, Align: "left"}
	}
	// 图注采用 Body Small，表格采用 Body Medium；均保留对应行高比例。
	t.Caption.Size = 9
	t.Caption.LineHeight = 16.0 / 12
	t.Caption.SpaceBefore = ptToMM(4)
	t.Caption.Color = &secondary
	t.Table.Size = 10.5
	t.Table.LineHeight = 20.0 / 14
	return t
}

// Fluent2Theme 将 Fluent 2 Web 的字体角色和行高比例用于印刷排版。
// 按 96px = 72pt 映射字号，段间距为印刷预设；内置字体使用 Go Regular/Bold。
// 角色来源：https://fluent2.microsoft.design/typography#type-ramp
func Fluent2Theme() Theme {
	text := creator.Color{R: 0x24, G: 0x24, B: 0x24}
	secondary := creator.Color{R: 0x61, G: 0x61, B: 0x61}
	border := creator.Color{R: 0xd1, G: 0xd1, B: 0xd1}
	header := creator.Color{R: 0xf5, G: 0xf5, B: 0xf5}
	body := TextStyle{Font: "Flow-Regular", Size: 10.5, LineHeight: 20.0 / 14, SpaceAfter: ptToMM(6), Color: &text, Align: "left"}
	t := Theme{Body: body, Caption: body, Table: body, BorderColor: &border, HeaderFill: &header}
	// 六级标题依次采用 Title 1/2/3、Subtitle 1/2、Body 1 Strong 的字号角色。
	sizes := [...]float64{32, 28, 24, 20, 16, 14}
	heights := [...]float64{40, 36, 32, 26, 22, 20}
	for i, size := range sizes {
		t.Headings[i] = TextStyle{Font: "Flow-Bold", Size: size * 0.75, LineHeight: heights[i] / size,
			SpaceBefore: ptToMM(12), SpaceAfter: ptToMM(6), Color: &text, Align: "left"}
	}
	t.Caption.Size = 9
	t.Caption.LineHeight = 16.0 / 12
	t.Caption.SpaceBefore = ptToMM(3)
	t.Caption.Color = &secondary
	return t
}

// CarbonTheme 将 Carbon 的静态 heading-06 至 heading-01、body-02、body-compact-01 与 label-01 映射到印刷样式。
// 按 96px = 72pt 映射字号；Light 使用 Go Regular，Semibold 使用 Go Bold，段间距为印刷预设。
// 角色来源：https://github.com/carbon-design-system/carbon/blob/main/packages/type/scss/_styles.scss
func CarbonTheme() Theme {
	text := creator.Color{R: 0x16, G: 0x16, B: 0x16}
	secondary := creator.Color{R: 0x52, G: 0x52, B: 0x52}
	border := creator.Color{R: 0xc6, G: 0xc6, B: 0xc6}
	header := creator.Color{R: 0xe0, G: 0xe0, B: 0xe0}
	body := TextStyle{Font: "Flow-Regular", Size: 12, LineHeight: 1.5, SpaceAfter: ptToMM(8), Color: &text, Align: "left"}
	t := Theme{Body: body, Caption: body, Table: body, BorderColor: &border, HeaderFill: &header}
	sizes := [...]float64{42, 32, 28, 20, 16, 14}
	// heading-06 保留源令牌的 1.199 倍行高，其余采用角色的标称行高。
	heights := [...]float64{42 * 1.199, 40, 36, 28, 24, 20}
	for i, size := range sizes {
		font := "Flow-Regular"
		if i >= 4 {
			font = "Flow-Bold"
		}
		t.Headings[i] = TextStyle{Font: font, Size: size * 0.75, LineHeight: heights[i] / size,
			SpaceBefore: ptToMM(16), SpaceAfter: ptToMM(8), Color: &text, Align: "left"}
	}
	t.Caption.Size, t.Caption.LineHeight = 9, 16.0/12
	t.Caption.SpaceBefore, t.Caption.Color = ptToMM(4), &secondary
	t.Table.Size, t.Table.LineHeight = 10.5, 18.0/14
	return t
}

// Bootstrap5Theme 按 Bootstrap 5.3 的 H1–H6、正文及 figure-caption 定义印刷样式。
// 根字号按 16px = 12pt 映射，保留标题 1.2 倍行高与段后 1rem；Medium 使用 Go Bold 近似。
// 来源：https://github.com/twbs/bootstrap/blob/v5.3.8/scss/_variables.scss
func Bootstrap5Theme() Theme {
	text := creator.Color{R: 0x21, G: 0x25, B: 0x29}
	// body-secondary-color 为正文 75% 不透明度，此处折算为白底实色。
	secondary := creator.Color{R: 0x59, G: 0x5c, B: 0x5f}
	border := creator.Color{R: 0xde, G: 0xe2, B: 0xe6}
	// 印刷表头使用 gray-100，方便与正文行区分。
	header := creator.Color{R: 0xf8, G: 0xf9, B: 0xfa}
	body := TextStyle{Font: "Flow-Regular", Size: 12, LineHeight: 1.5, SpaceAfter: ptToMM(12), Color: &text, Align: "left"}
	t := Theme{Body: body, Caption: body, Table: body, BorderColor: &border, HeaderFill: &header}
	for i, size := range [...]float64{30, 24, 21, 18, 15, 12} {
		t.Headings[i] = TextStyle{Font: "Flow-Bold", Size: size, LineHeight: 1.2,
			Set: StyleSpaceBefore, SpaceAfter: ptToMM(6), Color: &text, Align: "left"}
	}
	t.Caption.Size = 10.5 // figure-caption 使用正文的 87.5%。
	t.Caption.SpaceBefore, t.Caption.Color = ptToMM(6), &secondary
	return t
}
