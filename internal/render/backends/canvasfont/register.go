// Package canvasfont 提供基于 canvas 的字体引擎，管理文档字体、字形映射、
// 回退字体及有界整形缓存。导入本包即可向 drawing 注册字体能力。
package canvasfont

import (
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
)

func init() { //nolint:gochecknoinits // 导入字体后端时注册 drawing 工厂。
	drawing.RegisterFontEngineFactory(func(doc *parser.Document) drawing.FontEngine {
		return NewFonts(doc)
	})
	drawing.RegisterFallbackFontUnregisterer(UnregisterFallbackFont)
	drawing.RegisterFallbackFontFactory(
		registerFallbackFont,
		fallbackFontData,
	)
}
