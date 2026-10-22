package render

import (
	"errors"
	"slices"
	"sync"

	"github.com/zc310/ofd/internal/parser"
)

var errNoFontEngine = errors.New("未注册字体引擎（请空白导入 internal/render/backends/canvas）")

// newFontEngine 是当前字体引擎工厂；默认未注册，需空白导入 canvas 后端包
// （internal/render/backends/canvas）或调用 RegisterFontEngineFactory 注入。
var newFontEngine func(doc *parser.Document) FontEngine

// 同一解析文档的多个 DPI 视图共用字体；引用计数让视图按任意顺序关闭。
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
// 注册，核心经字体引擎接口调用。
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

// 已注册回退字体族名，按注册顺序去重。
//
// 这里是核心侧记账而不是问 canvas 的注册表：RegisterFallbackFont 本就是
// 全局注册的唯一入口，在这一层记下族名，枚举就不必依赖具体字体引擎实现。
var (
	registeredFallbackMu  sync.Mutex
	registeredFallbacks   []string
	registeredFallbackOps map[string]*fallbackOperation
)

type fallbackOperation struct {
	mu    sync.Mutex
	users int // 正在执行或等待同族操作的调用数，由 registeredFallbackMu 保护。
}

// 同族操作包含后端调用和列表更新，避免注册/注销交错后列表落后于后端。
// 等待同族操作及解析字体时不持全局列表锁；最后一个调用完成后删除锁条目。
func lockFallbackOperation(family string) func() {
	registeredFallbackMu.Lock()
	operation := registeredFallbackOps[family]
	if operation == nil {
		operation = &fallbackOperation{}
		if registeredFallbackOps == nil {
			registeredFallbackOps = make(map[string]*fallbackOperation)
		}
		registeredFallbackOps[family] = operation
	}
	operation.users++
	registeredFallbackMu.Unlock()
	operation.mu.Lock()
	return func() {
		operation.mu.Unlock()
		registeredFallbackMu.Lock()
		operation.users--
		if operation.users == 0 {
			delete(registeredFallbackOps, family)
		}
		registeredFallbackMu.Unlock()
	}
}

// RegisterFallbackFont 在进程内全局注册回退字体；需已导入字体引擎实现。
func RegisterFallbackFont(data []byte, family string, style FontStyle) error {
	unlock := lockFallbackOperation(family)
	defer unlock()
	if registerFallbackFontFn == nil {
		return errNoFontEngine
	}
	if err := registerFallbackFontFn(data, family, style); err != nil {
		return err
	}
	registeredFallbackMu.Lock()
	defer registeredFallbackMu.Unlock()
	if slices.Contains(registeredFallbacks, family) {
		return nil
	}
	registeredFallbacks = append(registeredFallbacks, family)
	return nil
}

// RegisteredFallbackFamilies 返回已全局注册的回退字体族名，按注册顺序。
//
// 供那些不走 Reader、直接拿 *Document 编码的调用方使用：字体注册进引擎
// 只是一半，另一半是把族名 UseFallbackFont 到具体文档上，否则文档渲染时
// 仍按缺字体处理，页面上一个字都没有。
func RegisteredFallbackFamilies() []string {
	registeredFallbackMu.Lock()
	defer registeredFallbackMu.Unlock()
	families := make([]string, len(registeredFallbacks))
	copy(families, registeredFallbacks)
	return families
}

// FallbackFontData 返回已全局注册回退字体族的首个来源数据。
// 返回注册表共享的只读字节，调用方不得修改。
func FallbackFontData(family string) ([]byte, bool) {
	if fallbackFontDataFn == nil {
		return nil, false
	}
	return fallbackFontDataFn(family)
}

// unregisterFallbackFontFn 是后端提供的反注册实现，由字体引擎实现在导入时登记。
var unregisterFallbackFontFn func(family string) error

// RegisterFallbackFontUnregisterer 登记反注册实现。
func RegisterFallbackFontUnregisterer(unregisterer func(family string) error) {
	unregisterFallbackFontFn = unregisterer
}

// UnregisterFallbackFont 声明某个字体族不再需要，释放其字节。
//
// 仍被打开的文档引用着它时不会立即删除，而是等最后一个文档放手——那些文档
// 还要用它渲染，提前释放会出问题。因此界面上的"删除"对已打开的文档是延迟
// 生效的，这是有意的。
func UnregisterFallbackFont(family string) error {
	unlock := lockFallbackOperation(family)
	defer unlock()
	if unregisterFallbackFontFn == nil {
		return errors.New("未导入字体引擎实现")
	}
	if err := unregisterFallbackFontFn(family); err != nil {
		return err
	}
	registeredFallbackMu.Lock()
	defer registeredFallbackMu.Unlock()
	registeredFallbacks = slices.DeleteFunc(registeredFallbacks, func(name string) bool { return name == family })
	return nil
}
