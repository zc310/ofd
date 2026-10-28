package drawing

import (
	"errors"
	"slices"
	"sync"
)

var errNoFontEngine = errors.New("未注册字体引擎（请空白导入 internal/render/backends/canvas）")
var errNoFontImplementation = errors.New("未导入字体引擎实现")

// 回退字体注册转发：具体实现由 canvas 字体引擎通过 RegisterFallbackFontFactory
// 注册，核心经字体引擎接口调用。
var (
	registerFallbackFontFn func(data []byte, family string, style FontStyle) error
	fallbackFontDataFn     func(family string) ([]byte, bool)
)

// RegisterFallbackFontFactory 注册回退字体的全局注册/查询实现。
func RegisterFallbackFontFactory(register func(data []byte, family string, style FontStyle) error, data func(family string) ([]byte, bool)) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
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
	factoryMu.RLock()
	register := registerFallbackFontFn
	factoryMu.RUnlock()
	if register == nil {
		return errNoFontEngine
	}
	if err := register(data, family, style); err != nil {
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
	factoryMu.RLock()
	data := fallbackFontDataFn
	factoryMu.RUnlock()
	if data == nil {
		return nil, false
	}
	return data(family)
}

// unregisterFallbackFontFn 是后端提供的反注册实现，由字体引擎实现在导入时登记。
var unregisterFallbackFontFn func(family string) error

// RegisterFallbackFontUnregisterer 登记反注册实现。
func RegisterFallbackFontUnregisterer(unregisterer func(family string) error) {
	factoryMu.Lock()
	defer factoryMu.Unlock()
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
	factoryMu.RLock()
	unregister := unregisterFallbackFontFn
	factoryMu.RUnlock()
	if unregister == nil {
		return errNoFontImplementation
	}
	if err := unregister(family); err != nil {
		return err
	}
	registeredFallbackMu.Lock()
	defer registeredFallbackMu.Unlock()
	registeredFallbacks = slices.DeleteFunc(registeredFallbacks, func(name string) bool { return name == family })
	return nil
}
