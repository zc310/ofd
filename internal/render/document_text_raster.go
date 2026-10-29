package render

import (
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
)

// drawMeshText 在已有字体锁内构造真实字形轮廓，复用路径渐变的裁剪、离屏预算
// 和放置逻辑。GB/T 33190—2016 §8.3.4 允许渐变用于文字填充与勾边；这里仅
// 绘制填充，调用方随后绘制描边及不可见文字。
func (p *Document) drawMeshText(ctx DrawContext, face FontFace, source *models.CTColor, object models.TextObject, pb models.StBox, parent *models.CTM, clip *geom.Path, budget *renderBudget) bool {
	transform := textPageTransform(object, pb.Height, parent)
	if transform == nil {
		return false
	}
	path := textlayout.Outline(face, object, transform)
	if path == nil || path.Empty() {
		return false
	}
	if clip != nil && !clipContainsRect(clip, path.Bounds()) {
		path = path.And(clip)
		if path.Empty() {
			return true
		}
	}
	gradient := p.textPageGradient(source, object.Boundary, pb.Height, parent)
	return p.drawMeshPaintGradient(ctx, path, gradient, pb, object.Alpha, budget)
}

// drawRasterTextSemantic 沿用同一字形定位队列补充 PDF 可提取文字，不再次获取字体锁。
// 颜色与对象透明度由可见栅格结果承担，语义文字不重复绘制颜色或描边。
func (p *Document) drawRasterTextSemantic(ctx DrawContext, face FontFace, object models.TextObject, pageHeight float64, parent *models.CTM) {
	if _, ok := ctx.(semanticTextDrawer); !ok {
		return
	}
	object.CGTransform = models.ExpandCGTransformPositions(object.TextCode, object.CGTransform)
	position := 0
	for _, code := range object.TextCode {
		if finiteFloat(code.X) && finiteFloat(code.Y) && finiteArray(code.DeltaX) && finiteArray(code.DeltaY) {
			textlayout.WalkRuns(face, object, code, position, func(run textlayout.Run) {
				if !fontrepair.ContainsGlyphRune(run.Value) {
					p.drawSemanticText(ctx, face, object, run.Value, run.X, run.Y, pageHeight, parent)
				}
			})
		}
		position += len([]rune(code.Value))
	}
}
