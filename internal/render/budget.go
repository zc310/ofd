package render

import (
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/renderstate"
	"github.com/zc310/ofd/pkg/spec"
)

// 页面显示选项和模板策略由编排层持有，计量及诊断委托给 renderstate。
type renderBudget struct {
	renderstate.Budget

	visibility        PageVisibility
	templateCacheable bool
	pageClip          *geom.Path
}

func (b *renderBudget) reset() {
	b.templateCacheable = true
	b.pageClip = nil
	b.Reset()
}

// 图片等公开入口允许不收集报告，保留空预算接收者的诊断行为。
func (b *renderBudget) diagnose(code, message string, resource spec.StID) {
	if b != nil {
		b.Diagnose(code, message, resource)
	}
}
