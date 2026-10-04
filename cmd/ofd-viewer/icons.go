package main

import "fyne.io/fyne/v2"

// 菜单图标一律用内联 SVG 而不是 theme 包里的通用图标：Fyne 自带图标只有"放大/
// 缩小/全屏"这类粗粒度含义，解释不了"适应宽度"和"适应高度"的区别，而色卡更不可能
// 靠现成图标表达。这里手绘一套 16×16 的图形，和背景色色卡共用同一套封装。

// svgIconHead/svgIconTail 是内联图标的固定外壳。SVG 解析器按 viewBox 缩放到菜单项
// 的图标尺寸，因此外壳不写死像素宽高。
const (
	svgIconHead = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" width="16" height="16">`
	svgIconTail = `</svg>`
)

// svgIconStroke 是线性图标的统一描边色和宽度，和背景色色卡的边框色保持一致。
const svgIconStroke = `#5b6068`

// svgIcon 把一段 16×16 的 SVG 片段包成图标资源。name 必须唯一：Fyne 的
// StaticResource 会按资源名缓存，同名不同内容在下次启动时会拿到旧图标。
func svgIcon(name, body string) fyne.Resource {
	return fyne.NewStaticResource(name, []byte(svgIconHead+body+svgIconTail))
}

// svgStroke 把一组 SVG 子路径包成统一描边的图形。图形不填充，避免图标在小尺寸下糊
// 成实心块。
func svgStroke(body string) string {
	return `<g fill="none" stroke="` + svgIconStroke + `" stroke-width="1.2" ` +
		`stroke-linecap="round" stroke-linejoin="round">` + body + `</g>`
}

// viewModeIcons 是四种视图模式各自的内联图标，与 viewFitPageLabel 等标签一一对应。
// 图形约定：外框是页面，里面的箭头说明缩放方向，"双页显示"直接画成两页。
var viewModeIcons = map[pageViewMode]string{
	viewFitPage:    "view-fit-page.svg",
	viewFitWidth:   "view-fit-width.svg",
	viewFitHeight:  "view-fit-height.svg",
	viewDoublePage: "view-double-page.svg",
}

// viewModeIcon 返回视图模式对应的菜单图标。图标固定不变，按模式缓存一次即可；未知
// 模式返回 nil，让菜单项保持无图标而不是画一个错的图形。
func viewModeIcon(mode pageViewMode) fyne.Resource {
	name, ok := viewModeIcons[mode]
	if !ok {
		return nil
	}
	switch mode {
	case viewFitPage:
		// 页面被外框包住：整页都放进可视区。
		return svgIcon(name, svgStroke(
			`<rect x="3.5" y="1.5" width="9" height="13" rx="1"/>`+
				`<path d="M1.5 5.5 V1.5 H5.5"/><path d="M10.5 1.5 H14.5 V5.5"/>`+
				`<path d="M14.5 10.5 V14.5 H10.5"/><path d="M5.5 14.5 H1.5 V10.5"/>`))
	case viewFitWidth:
		// 只管宽度：横向双向箭头。
		return svgIcon(name, svgStroke(
			`<rect x="1.5" y="1.5" width="13" height="13" rx="1"/>`+
				`<path d="M4.5 8 H11.5"/><path d="M6.5 5.5 L4 8 L6.5 10.5"/>`+
				`<path d="M9.5 5.5 L12 8 L9.5 10.5"/>`))
	case viewFitHeight:
		// 只管高度：纵向双向箭头。
		return svgIcon(name, svgStroke(
			`<rect x="1.5" y="1.5" width="13" height="13" rx="1"/>`+
				`<path d="M8 4.5 V11.5"/><path d="M5.5 6.5 L8 4 L10.5 6.5"/>`+
				`<path d="M5.5 9.5 L8 12 L10.5 9.5"/>`))
	case viewDoublePage:
		// 两页并排。
		return svgIcon(name, svgStroke(
			`<rect x="1.5" y="3.5" width="5.5" height="9" rx="1"/>`+
				`<rect x="9" y="3.5" width="5.5" height="9" rx="1"/>`))
	}
	return nil
}
