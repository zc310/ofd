package main

import (
	"strings"

	"golang.org/x/text/width"
)

// wasmBannerWidth 是 banner 边框的显示列数，与上下边框的 ═ 数量一致。
const wasmBannerWidth = 42

// centerBanner 把标签在 banner 边框内居中，按显示列数而非字符数补空格。
//
// 「阅读器」三字在等宽终端各占两列，按 len 补齐会让中间行比上下边框宽一列；
// 版本号长度变化时手写空格同样会错位，因此统一按 East Asian Wide/Fullwidth 计算。
func centerBanner(label string) string {
	gap := wasmBannerWidth - displayWidth(label)
	if gap <= 0 {
		return label
	}
	left := gap / 2
	return strings.Repeat(" ", left) + label + strings.Repeat(" ", gap-left)
}

// displayWidth 返回字符串在等宽终端占用的列数：East Asian Wide 与 Fullwidth 记两列，
// 其余（含 Ambiguous）记一列——Ambiguous 的宽度取决于终端设置，按一列处理可保证
// 边框不会溢出。
func displayWidth(value string) int {
	total := 0
	for _, r := range value {
		kind := width.LookupRune(r).Kind()
		if kind == width.EastAsianWide || kind == width.EastAsianFullwidth {
			total += 2
			continue
		}
		total++
	}
	return total
}
