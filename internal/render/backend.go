package render

import (
	"errors"
	"image"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// Backend 是后端契约的别名：契约定义在与绘制库无关的 drawing 包，插件后端
// （gg/ftgg/tinyskia/draw2d）直接依赖 drawing，不必依赖 render/canvas。
type Backend = drawing.Backend

// BackendCanvas 是内置的 tdewolff/canvas 光栅后端名。canvas 实现位于
// internal/render/backends/canvas，需空白导入该包注册后方可使用。
const BackendCanvas = drawing.BackendCanvas

// RasterizePage 把指定页面栅格化为 RGBA 图像。backendName 选用渲染后端
// （BackendCanvas 需空白导入 internal/render/backends/canvas；其它后端见
// drawing.Backend*，需导入对应插件包）；resolution 决定输出图片的像素密度。
func (p *Document) RasterizePage(page *parser.Page, backendName string, resolution geom.Resolution) (*image.RGBA, error) {
	if page == nil {
		return nil, errors.New("页面为空")
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	var budget renderBudget
	budget.reset()
	content := lease.Content()
	if content == nil {
		return nil, errors.New("页面内容为空")
	}
	box := content.Area.PhysicalBox
	b, err := NewBackend(backendName, box.Width, box.Height, resolution)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, errors.New("未注册栅格后端")
	}
	p.drawPage(b, page, content, &budget)
	return b.Raster(), nil
}

var _ = image.Rect
