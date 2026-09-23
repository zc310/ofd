package render

import (
	"fmt"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// BackendFactory 是 drawing.BackendFactory 的别名：创建后端实例，由各后端
// 插件包通过 RegisterBackend 注册。width/height 为页面物理尺寸（mm），
// resolution 为输出分辨率（geom.Resolution，与绘制库无关）。
type BackendFactory = drawing.BackendFactory

// RegisterBackend / RegisteredBackends 转发到 drawing 包的注册表。插件包在
// 自己的 init() 里注册，例：
//
//	import _ "github.com/zc310/ofd/internal/render/backends/canvas"  // 内置 canvas
//	import _ "github.com/zc310/ofd/internal/render/backends/gg"      // gogpu/gg
//
// 只有显式导入后端的二进制才会包含对应实现。
var (
	RegisterBackend    = drawing.RegisterBackend
	RegisteredBackends = drawing.RegisteredBackends
)

// NewBackend 创建页面栅格化后端。
//
// name 为后端标识；各后端（含内置的 BackendCanvas）都需先空白导入对应插件
// 包来注册（见 RegisterBackend）。width/height 是页面物理尺寸（mm），
// resolution 是输出分辨率（geom.DPI / geom.DPMM）。
func NewBackend(name string, width, height float64, resolution geom.Resolution) (Backend, error) {
	if width <= 0 || height <= 0 || resolution <= 0 {
		return nil, fmt.Errorf("无效的页面尺寸或分辨率：%g×%gmm @ %v", width, height, resolution)
	}
	// 像素缓冲尺寸按 int(w*dpmm+0.5) 取整；过小的页面在低分辨率下会取整成
	// 0 像素。提前返回错误，避免各后端在 newCore 里 panic 或产出空图。
	if int(width*resolution.DPMM()+0.5) <= 0 || int(height*resolution.DPMM()+0.5) <= 0 {
		return nil, fmt.Errorf("页面尺寸 %g×%gmm 在 %v 分辨率下不足 1 像素", width, height, resolution)
	}
	factory, ok := drawing.LookupBackend(name)
	if !ok {
		return nil, fmt.Errorf("未知渲染后端 %q（已注册 %s；如需使用，请空白导入 %q）",
			name, drawing.JoinNames(drawing.BackendNames()), drawing.BackendPluginPath(name))
	}
	return factory(width, height, resolution)
}
