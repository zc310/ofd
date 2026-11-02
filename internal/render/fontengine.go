package render

import (
	"sync"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
)

// 同一解析文档的多个 DPI 视图共用字体，最后一个视图关闭时释放。
type sharedFontEngine struct {
	mu     sync.Mutex
	engine FontEngine
	refs   int
}

func (f *sharedFontEngine) release() {
	f.mu.Lock()
	if f.refs <= 0 {
		f.mu.Unlock()
		return
	}
	f.refs--
	last := f.refs == 0
	f.mu.Unlock()
	if last && f.engine != nil {
		f.engine.Close()
	}
}

// RegisterFontEngineFactory 将字体引擎工厂登记到 drawing。
func RegisterFontEngineFactory(fn func(doc *parser.Document) FontEngine) {
	if fn != nil {
		drawing.RegisterFontEngineFactory(fn)
	}
}

// NewFontEngine 按已注册工厂创建字体引擎，未注册时返回 nil。
func NewFontEngine(doc *parser.Document) FontEngine { return drawing.NewFontEngine(doc) }

// RegisterFallbackFont 注册进程内的回退字体。
func RegisterFallbackFont(data []byte, family string, style FontStyle) error {
	return drawing.RegisterFallbackFont(data, family, style)
}

// RegisteredFallbackFamilies 返回已注册的回退字体族，按注册顺序排列。
func RegisteredFallbackFamilies() []string { return drawing.RegisteredFallbackFamilies() }

// FallbackFontData 返回字体族的首个来源，字节只读共享。
func FallbackFontData(family string) ([]byte, bool) { return drawing.FallbackFontData(family) }

// UnregisterFallbackFont 声明字体族不再需要，在最后一个文档释放后回收资源。
func UnregisterFallbackFont(family string) error { return drawing.UnregisterFallbackFont(family) }
