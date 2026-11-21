package flow

import "github.com/zc310/ofd/pkg/creator"

// 配色是各设计语言的浅色印刷适配，不表示它们都提供同名 Callout 组件。
// Bootstrap 5 参考 _variables.scss 的语义色及 subtle/emphasis 令牌；
// 其他主题参考各自中性、信息、警告、错误和成功颜色，黄色适当加深以便纸面阅读。
func calloutTheme(name string) CalloutTheme {
	var colors [5][3]uint32
	switch name {
	case "bootstrap5":
		colors = [5][3]uint32{{0x2b2f32, 0xe2e3e5, 0xc4c8cb}, {0x055160, 0xcff4fc, 0x9eeaf9}, {0x664d03, 0xfff3cd, 0xffe69c}, {0x58151c, 0xf8d7da, 0xf1aeb5}, {0x0a3622, 0xd1e7dd, 0xa3cfbb}}
	case "carbon":
		colors = [5][3]uint32{{0x525252, 0xf4f4f4, 0xc6c6c6}, {0x0043ce, 0xedf5ff, 0xa6c8ff}, {0x8e6a00, 0xfcf4d6, 0xf1c21b}, {0xda1e28, 0xfff1f1, 0xffb3b8}, {0x198038, 0xdefbe6, 0xa7f0ba}}
	case "fluent2":
		colors = [5][3]uint32{{0x616161, 0xf5f5f5, 0xd1d1d1}, {0x0f6cbd, 0xeff6fc, 0xb4d6fa}, {0x8a6100, 0xfff4ce, 0xfde300}, {0xb10e1c, 0xfdf3f4, 0xeeacb2}, {0x107c10, 0xf1faf1, 0x9fd89f}}
	case "material3":
		colors = [5][3]uint32{{0x49454f, 0xf3edf7, 0xcac4d0}, {0x3f51b5, 0xe8eaf6, 0xc5cae9}, {0x7d5700, 0xfff3cd, 0xefcf86}, {0xb3261e, 0xf9dedc, 0xf2b8b5}, {0x2e6b35, 0xe2f3df, 0xb6d7ae}}
	case "material2":
		colors = [5][3]uint32{{0x616161, 0xf5f5f5, 0xe0e0e0}, {0x1976d2, 0xe3f2fd, 0xbbdefb}, {0x8d6e00, 0xfff8e1, 0xffe082}, {0xc62828, 0xffebee, 0xffcdd2}, {0x2e7d32, 0xe8f5e9, 0xc8e6c9}}
	default:
		// 保留原有 Material 报告主题已使用的强调色。
		colors = [5][3]uint32{{0x6c757d, 0xf8f9fa, 0xdee2e6}, {0x0d6efd, 0xe7f1ff, 0xdee2e6}, {0x997000, 0xfffbeb, 0xdee2e6}, {0xdc3545, 0xfdedef, 0xdee2e6}, {0x198754, 0xe8f5ee, 0xdee2e6}}
	}
	palette := func(values [3]uint32) CalloutPalette {
		color := func(rgb uint32) *creator.Color {
			return &creator.Color{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb)}
		}
		return CalloutPalette{AccentColor: color(values[0]), Fill: color(values[1]), BorderColor: color(values[2])}
	}
	return CalloutTheme{Neutral: palette(colors[0]), Info: palette(colors[1]), Warning: palette(colors[2]), Danger: palette(colors[3]), Success: palette(colors[4])}
}
