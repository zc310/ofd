package flow

// 图标为自定义几何轮廓，使用 OFD 紧缩路径；通过外形而非仅颜色区分语义。
func calloutIcon(variant string) *Icon {
	icon := &Icon{Width: 16, Height: 16, Rule: "Even-Odd"}
	switch variant {
	case "success":
		icon.Data = "M 8 0 L 16 8 L 8 16 L 0 8 C M 3 8 L 6 11 L 13 4 L 14 5 L 6 13 L 2 9 C"
	case "warning":
		icon.Data = "M 8 0 L 16 16 L 0 16 C M 7 5 L 9 5 L 9 11 L 7 11 C M 7 13 L 9 13 L 9 15 L 7 15 C"
	case "danger":
		icon.Data = "M 5 0 L 11 0 L 16 5 L 16 11 L 11 16 L 5 16 L 0 11 L 0 5 C M 4 3 L 8 7 L 12 3 L 13 4 L 9 8 L 13 12 L 12 13 L 8 9 L 4 13 L 3 12 L 7 8 L 3 4 C"
	default:
		icon.Data = "M 8 0 B 12.418 0 16 3.582 16 8 B 16 12.418 12.418 16 8 16 B 3.582 16 0 12.418 0 8 B 0 3.582 3.582 0 8 0 C M 7 3 L 9 3 L 9 5 L 7 5 C M 7 7 L 9 7 L 9 13 L 7 13 C"
	}
	return icon
}
