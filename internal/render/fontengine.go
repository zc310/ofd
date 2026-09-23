package render

import (
	"errors"

	"github.com/zc310/ofd/internal/parser"
)

var errNoFontEngine = errors.New("未注册字体引擎（请空白导入 internal/render/backends/canvas）")

// newFontEngine 是当前字体引擎工厂；默认未注册，需空白导入 canvas 后端包
// （internal/render/backends/canvas）或调用 RegisterFontEngineFactory 注入。
var newFontEngine func(doc *parser.Document) FontEngine

// RegisterFontEngineFactory 注册字体引擎工厂，供 canvas 后端注册或测试注入。
func RegisterFontEngineFactory(fn func(doc *parser.Document) FontEngine) {
	if fn != nil {
		newFontEngine = fn
	}
}

// NewFontEngine 按已注册工厂创建字体引擎；未注册时返回 nil。
func NewFontEngine(doc *parser.Document) FontEngine {
	if newFontEngine == nil {
		return nil
	}
	return newFontEngine(doc)
}

// 回退字体注册转发：具体实现由 canvas 字体引擎通过 RegisterFallbackFontFactory
// 注册。核心不依赖 canvas。
var (
	registerFallbackFontFn func(data []byte, family string, style FontStyle) error
	fallbackFontDataFn     func(family string) ([]byte, bool)
)

// RegisterFallbackFontFactory 注册回退字体的全局注册/查询实现。
func RegisterFallbackFontFactory(register func(data []byte, family string, style FontStyle) error, data func(family string) ([]byte, bool)) {
	if register != nil {
		registerFallbackFontFn = register
	}
	if data != nil {
		fallbackFontDataFn = data
	}
}

// RegisterFallbackFont 在进程内全局注册回退字体；需已导入字体引擎实现。
func RegisterFallbackFont(data []byte, family string, style FontStyle) error {
	if registerFallbackFontFn == nil {
		return errNoFontEngine
	}
	return registerFallbackFontFn(data, family, style)
}

// FallbackFontData 返回已全局注册回退字体族的首个来源数据。
func FallbackFontData(family string) ([]byte, bool) {
	if fallbackFontDataFn == nil {
		return nil, false
	}
	return fallbackFontDataFn(family)
}
