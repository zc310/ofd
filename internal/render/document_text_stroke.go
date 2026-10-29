package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
	"github.com/zc310/ofd/internal/render/paint"
)

// drawTextStroke 在字体锁内完成文字描边。GB/T 33190—2016 §8.2 表21规定图元
// 属性覆盖引用参数；展平轮廓后按 CTM 换算线宽、虚线及斜接限制，再进行裁剪。
func (p *Document) drawTextStroke(ctx DrawContext, face FontFace, object models.TextObject, dp *models.DrawParam, pb models.StBox, parent *models.CTM, clip *geom.Path, semantic bool, budget *renderBudget) {
	ctx.Push()
	defer ctx.Pop()
	outline := p.textStrokeOutline(ctx, face, object, dp, pb, parent, clip)
	if outline == nil || outline.Empty() {
		return
	}
	source := textStrokeColor(object, dp)
	gradient := p.textPageGradient(source, object.Boundary, pb.Height, parent)
	if gradient != nil && (isMeshColor(source) || paint.NeedsRaster(gradient, object.Alpha)) {
		if !p.drawMeshPaintGradient(ctx, outline, gradient, pb, object.Alpha, budget) {
			return
		}
	} else {
		if gradient != nil {
			ctx.SetFillGradient(paint.ScaleOpacity(gradient, graphicOpacity(object.Alpha)))
		} else {
			ctx.CopyStrokeToFill()
		}
		ctx.ClearStroke()
		ctx.SetFillRule(geom.NonZero)
		ctx.DrawPath(0, 0, outline)
	}
	if semantic {
		p.drawRasterTextSemantic(ctx, face, object, pb.Height, parent)
	}
}

// textStrokeOutline 在已有字体锁内按 run 扩展描边，供普通颜色、渐变和底纹共用。
// 调用方保存上下文；底纹随后在字体锁外绘制。
func (p *Document) textStrokeOutline(ctx DrawContext, face FontFace, object models.TextObject, dp *models.DrawParam, pb models.StBox, parent *models.CTM, clip *geom.Path) *geom.Path {
	transform := textPageTransform(object, pb.Height, parent)
	if transform == nil {
		return nil
	}
	ctx.SetDashes(0)
	pathObject := models.CtPath{CTGraphicUnit: object.CTGraphicUnit, Stroke: models.NewOptionalBool(true), StrokeColor: object.StrokeColor}
	style := p.updateCtPathStyle(ctx, &pathObject, dp, paint.StrokeScale(object.CTM, parent))
	var outline *geom.Path
	textlayout.WalkOutlines(face, object, transform, func(path *geom.Path) {
		path = dashedStrokePath(ctx, path, style)
		if outline == nil {
			outline = path
		} else {
			outline = outline.Or(path)
		}
	})
	if outline == nil || outline.Empty() {
		return nil
	}
	if clip != nil && !clipContainsRect(clip, outline.Bounds()) {
		outline = outline.And(clip)
	}
	return outline
}
