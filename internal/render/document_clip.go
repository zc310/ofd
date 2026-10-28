package render

import (
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func strokeOutlineWidth(ctx DrawContext) float64 {
	width := ctx.StrokeWidth()
	if raster, ok := ctx.(interface{ RasterResolution() geom.Resolution }); ok {
		scale := math.Sqrt(math.Abs(ctx.CurrentMatrix().Det()))
		if scale > 0 && finiteFloat(scale) {
			width = max(width, geom.MinimumRasterStrokeWidth(raster.RasterResolution())/scale)
		}
	}
	return width
}

func strokeBounds(ctx DrawContext, bounds geom.Rect) geom.Rect {
	padding := strokeOutlineWidth(ctx) / 2
	if join, ok := ctx.StrokeJoiner().(geom.MiterJoiner); ok && finiteFloat(join.Limit) {
		padding *= max(1, join.Limit)
	}
	return bounds.Expand(padding)
}

// graphicClip 遵循 GB/T 33190—2016 §8.4、表34：Boundary 限定当前空间，
// Clips 限定对象空间；它们与容器裁剪共同约束实际绘制区域。
func (p *Document) graphicClip(unit models.CTGraphicUnit, pageHeight float64, parent *models.CTM, inherited *geom.Path) *geom.Path {
	clip := p.buildPathClip(unit.Clips, unit.Boundary, pageHeight, unit.CTM, parent)
	clip = intersectClips(clip, inherited)
	return intersectClips(clip, graphicBoundaryClip(unit.Boundary, pageHeight, parent))
}

// pathGraphicClip 将 Boundary 应用于路径的填充范围，同时给描边留出自身外沿。
// OFD 中路径 Boundary 常按路径中心线范围填写；直接裁剪会切掉描边的半边，
// 例如边界恰好落在矩形描边中心线上时，矩形的四边都会被裁窄。
func (p *Document) pathGraphicClip(object models.PathObject, pageHeight float64, parent *models.CTM, inherited *geom.Path, ctx DrawContext) *geom.Path {
	unit := object.CTGraphicUnit
	clip := p.buildPathClip(unit.Clips, unit.Boundary, pageHeight, unit.CTM, parent)
	clip = intersectClips(clip, inherited)
	boundary := graphicBoundaryClip(unit.Boundary, pageHeight, parent)
	if boundary != nil && object.Stroke.Value(true) {
		bounds := strokeBounds(ctx, boundary.Bounds())
		boundary = geom.Rectangle(bounds.W(), bounds.H()).Translate(bounds.X0, bounds.Y0)
	}
	return intersectClips(clip, boundary)
}

func graphicBoundaryClip(box models.StBox, pageHeight float64, parent *models.CTM) *geom.Path {
	// 历史文件会省略或写入零宽高 Boundary；没有有效矩形时保留显式裁剪。
	if !box.IsFinite() || box.Width <= 0 || box.Height <= 0 {
		return nil
	}
	matrix := geom.Matrix{{1, 0, box.X}, {0, -1, pageHeight - box.Y}}
	if parent != nil {
		matrix = matrix.Mul(geom.Matrix{{parent[0], parent[2], parent[4]}, {parent[1], parent[3], parent[5]}})
	}
	return geom.Rectangle(box.Width, box.Height).Transform(matrix)
}

func intersectClips(a, b *geom.Path) *geom.Path {
	return geom.IntersectClips(a, b)
}

// clipContainsRect 只对严格覆盖的轴对齐矩形走快路径，不能把带洞或近似
// 矩形的裁剪当成完整覆盖。普通文字与路径无需因此执行昂贵的轮廓求交。
func clipContainsRect(clip *geom.Path, bounds geom.Rect) bool {
	return geom.ClipContainsRect(clip, bounds)
}

// localTextClip 把在文字对象入口保存的设备空间裁剪映回当前字形空间。
func localTextClip(ctx DrawContext, clip *geom.Path) *geom.Path {
	if clip == nil {
		return nil
	}
	matrix := ctx.CurrentMatrix()
	if !finiteMatrix(matrix) || geom.Equal(matrix.Det(), 0) {
		return &geom.Path{}
	}
	return clip.Copy().Transform(matrix.Inv())
}

func (p *Document) drawTextOutline(ctx DrawContext, path, clip *geom.Path, object models.TextObject) {
	if path == nil || path.Empty() {
		return
	}
	clip = localTextClip(ctx, clip)
	bounds := path.Bounds()
	if object.Stroke {
		bounds = strokeBounds(ctx, bounds)
	}
	if clipContainsRect(clip, bounds) {
		ctx.DrawPath(0, 0, path)
		return
	}
	if clip.Empty() || !bounds.Overlaps(clip.Bounds()) {
		return
	}
	p.drawClippedPath(ctx, path, clip, models.PathObject{CtPath: models.CtPath{
		Fill: !textFillDisabled(object), Stroke: models.NewOptionalBool(object.Stroke),
	}})
}
