package canvas

import "github.com/zc310/ofd/internal/render/drawing"

// 字体样式常量与类型的本地别名，便于包内代码与测试直接引用。
type (
	FontStyle  = drawing.FontStyle
	FontFamily = drawing.FontFamily
	FontFace   = drawing.FontFace
	TextRun    = drawing.TextRun
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
