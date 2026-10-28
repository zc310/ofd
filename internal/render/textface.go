package render

import (
	"github.com/zc310/ofd/internal/render/drawing"
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
