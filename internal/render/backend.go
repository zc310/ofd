package render

import (
	"errors"
	"image"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// Backend 是后端契约的别名：插件后端通过 drawing 共享契约，
// 通过 geom 共享几何类型。
type Backend = drawing.Backend

// BackendCanvas 是内置的 tdewolff/canvas 光栅后端名。canvas 实现位于
// internal/render/backends/canvas，需空白导入该包注册后方可使用。
const BackendCanvas = drawing.BackendCanvas

// RasterizePage 把指定页面栅格化为 RGBA 图像。backendName 选用渲染后端
// （BackendCanvas 需空白导入 internal/render/backends/canvas；其它后端见
// drawing.Backend*，需导入对应插件包）；resolution 决定输出图片的像素密度。
// 渲染文档已关闭时返回错误。
func (p *Document) RasterizePage(page *parser.Page, backendName string, resolution geom.Resolution) (*image.RGBA, error) {
	return p.rasterizePageReport(page, backendName, resolution, nil)
}

// RasterizePageWithReport 返回页面位图和本次绘制的资源用量、可恢复诊断。
func (p *Document) RasterizePageWithReport(page *parser.Page, backendName string, resolution geom.Resolution) (*image.RGBA, Report, error) {
	report := &renderReportBuilder{}
	img, err := p.rasterizePageReport(page, backendName, resolution, report)
	return img, report.Snapshot(), err
}

func (p *Document) rasterizePageReport(page *parser.Page, backendName string, resolution geom.Resolution, report *renderReportBuilder) (*image.RGBA, error) {
	if err := p.checkOpen(); err != nil {
		return nil, err
	}
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
	budget.Report = report
	if report != nil {
		report.PageID = page.ID
	}
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
