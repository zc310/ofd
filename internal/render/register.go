package render

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
)

// BackendFactory 创建后端实例，由各后端插件包通过 RegisterBackend 注册。
// width/height 为页面物理尺寸（mm），resolution 为输出分辨率。
type BackendFactory func(width, height float64, resolution canvas.Resolution) (Backend, error)

var (
	backendMu     sync.RWMutex
	backendByName = map[string]BackendFactory{}
)

// RegisterBackend 注册一个可按名字创建的渲染后端，供 NewBackend 使用。
// 插件包在自己的 init() 里调用本函数，使用
// 方通过空白导入触发注册，例如
//
//	import _ "github.com/zc310/ofd/internal/render/backends/gg"
//
// 这样基础 render 包及其全部使用者不会因为引入渲染后端而连带编译重依赖
// （gogpu/gg、FloatTech/gg、tinyskia 及其传递依赖），只有显式导入后端的
// 二进制才会包含对应实现。重复注册同名后端返回错误。
func RegisterBackend(name string, factory BackendFactory) error {
	if name == "" {
		return errors.New("渲染后端名称不能为空")
	}
	if name == BackendCanvas {
		return fmt.Errorf("渲染后端 %q 是内置后端，不能通过 RegisterBackend 注册", name)
	}
	if factory == nil {
		return fmt.Errorf("渲染后端 %q 的工厂函数为空", name)
	}
	backendMu.Lock()
	defer backendMu.Unlock()
	if _, dup := backendByName[name]; dup {
		return fmt.Errorf("渲染后端 %q 已注册", name)
	}
	backendByName[name] = factory
	return nil
}

// RegisteredBackends 返回当前已注册的可选后端名称（不含始终内置的 canvas），
// 按名称排序，主要用于错误提示。
func RegisteredBackends() []string {
	backendMu.RLock()
	defer backendMu.RUnlock()
	names := make([]string, 0, len(backendByName))
	for name := range backendByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewBackend 创建页面栅格化后端。
//
// name 为后端标识：BackendCanvas 始终可用；其它后端需先空白导入对应插件
// 包来注册（见 RegisterBackend）。width/height 是页面物理尺寸（mm），
// resolution 是输出分辨率（canvas.DPI / canvas.DPMM）。
func NewBackend(name string, width, height float64, resolution canvas.Resolution) (Backend, error) {
	if width <= 0 || height <= 0 || resolution <= 0 {
		return nil, fmt.Errorf("无效的页面尺寸或分辨率：%g×%gmm @ %v", width, height, resolution)
	}
	// 像素缓冲尺寸按 int(w*dpmm+0.5) 取整；过小的页面在低分辨率下会取整成
	// 0 像素。提前返回错误，避免各后端在 newCore 里 panic 或产出空图。
	if int(width*resolution.DPMM()+0.5) <= 0 || int(height*resolution.DPMM()+0.5) <= 0 {
		return nil, fmt.Errorf("页面尺寸 %g×%gmm 在 %v 分辨率下不足 1 像素", width, height, resolution)
	}
	if name == BackendCanvas {
		return newCanvasRasterBackend(width, height, resolution), nil
	}
	backendMu.RLock()
	factory, ok := backendByName[name]
	backendMu.RUnlock()
	if !ok {
		registered := append([]string{BackendCanvas}, RegisteredBackends()...)
		return nil, fmt.Errorf("未知渲染后端 %q（已注册 %s；如需使用，请空白导入 %q）",
			name, strings.Join(registered, "、"), "github.com/zc310/ofd/internal/render/backends/"+name)
	}
	return factory(width, height, resolution)
}
