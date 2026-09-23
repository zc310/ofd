package render

import (
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// 字体相关契约定义在 drawing 包，render 通过别名保持既有调用点可用。
type (
	FontFamily = drawing.FontFamily
	FontFace   = drawing.FontFace
	TextRun    = drawing.TextRun
	FontEngine = drawing.FontEngine
	FontStyle  = drawing.FontStyle
)

// 字重常量转发，便于本包与调用方继续使用 render.FontRegular 等。
const (
	FontRegular    = drawing.FontRegular
	FontThin       = drawing.FontThin
	FontExtraLight = drawing.FontExtraLight
	FontLight      = drawing.FontLight
	FontMedium     = drawing.FontMedium
	FontSemiBold   = drawing.FontSemiBold
	FontBold       = drawing.FontBold
	FontExtraBold  = drawing.FontExtraBold
	FontBlack      = drawing.FontBlack
	FontItalic     = drawing.FontItalic
)

// fontCanRenderRune 判断字体是否存在该 Unicode 码位的字形。缺少 Unicode cmap
// 的子集字体在注入文档映射前会返回 false，此时调用方回退到字形私有区绘制。
func fontCanRenderRune(face FontFace, r rune) bool {
	if face == nil {
		return false
	}
	return face.GlyphIndex(r) != 0
}

// fontCanRenderText 判断字体能否按给定文本成形：文本必须不含私有区字形引用，
// 且每个可见字符都有对应字形。只有这样才能安全地用正常文字接口绘制。
func fontCanRenderText(face FontFace, value string) bool {
	if containsPrivateGlyphRune(value) {
		return false
	}
	rendered := false
	for _, r := range value {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '\uFFFD' {
			continue
		}
		if !fontCanRenderRune(face, r) {
			return false
		}
		rendered = true
	}
	return rendered
}

var _ = geom.Path{}
