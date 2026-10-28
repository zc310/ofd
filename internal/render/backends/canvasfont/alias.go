package canvasfont

import "github.com/zc310/ofd/internal/render/drawing"

// 字体样式别名用于包内字体加载与测试。
type (
	FontStyle  = drawing.FontStyle
	FontFamily = drawing.FontFamily
	FontFace   = drawing.FontFace
	CTColor    = drawing.CTColor
)

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
