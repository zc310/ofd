package render

import (
	"image"
	"math"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageasset"
	"github.com/zc310/ofd/pkg/spec"
)

// imagePixelDemand 按单位图像到页面的变换计算两条像素轴的输出需求。
// §10 表43、§13 表50只规定高分辨率替换用途，没有指定固定的 DPI 阈值。
// 矢量记录使用文档的目标 DPI，直接光栅绘制使用后端实际分辨率。
func (p *Document) imagePixelDemand(ctx DrawContext, ctm spec.CTM) (float64, float64) {
	resolution := p.dpi
	if raster, ok := ctx.(interface{ RasterResolution() geom.Resolution }); ok {
		resolution = raster.RasterResolution()
	}
	// CTM 与当前用户空间均为 OFD 的 Y 向下坐标；后端轴向转换已包含在
	// CurrentMatrix 中，不能再用旧的 F·L·F 共轭。源图片 Y 轴的整体反号
	// 不改变轴长，因此直接组合 CTM 的线性部分即可。
	m := ctx.CurrentMatrix().Mul(geom.Matrix{{ctm[0], ctm[2], 0}, {ctm[1], ctm[3], 0}})
	return math.Hypot(m[0][0], m[1][0]) * resolution.DPMM(), math.Hypot(m[0][1], m[1][1]) * resolution.DPMM()
}

func imageSampleQuality(img image.Image, width, height float64) float64 {
	if img == nil || img.Bounds().Empty() || width <= 0 || height <= 0 || !finiteFloat(width) || !finiteFloat(height) {
		return 0
	}
	return min(float64(img.Bounds().Dx())/width, float64(img.Bounds().Dy())/height)
}

func (p *Document) substitutionImageWithBudget(id spec.StRefID, budget *renderBudget) image.Image {
	media := p.GetMedia(spec.StID(id))
	if media == nil {
		return nil
	}
	img, err := p.decodeImageWithBudget(media.MediaFile.Clean(), media.Format, 0, 0, budget)
	if err != nil || img == nil || img.Bounds().Empty() {
		return nil
	}
	return img
}

// selectImageResource 不改变资源索引；原图够用时不读取替代图。
// SVG 保留矢量路径，不能因其预览位图尺寸较小而换成栅格图。
func (p *Document) selectImageResource(ctx DrawContext, primary, substitution spec.StRefID, ctm spec.CTM) spec.StRefID {
	var budget renderBudget
	budget.reset()
	return p.selectImageResourceWithBudget(ctx, primary, substitution, ctm, &budget)
}

func (p *Document) selectImageResourceWithBudget(ctx DrawContext, primary, substitution spec.StRefID, ctm spec.CTM, budget *renderBudget) spec.StRefID {
	media := p.GetMedia(spec.StID(primary))
	if substitution == 0 || substitution == primary || (media != nil && imageasset.IsSVG(media.Format, media.MediaFile)) {
		return primary
	}
	width, height := p.imagePixelDemand(ctx, ctm)
	original := p.substitutionImageWithBudget(primary, budget)
	quality := imageSampleQuality(original, width, height)
	if quality >= 1 {
		return primary
	}
	replacement := p.substitutionImageWithBudget(substitution, budget)
	if replacement != nil && (original == nil || imageSampleQuality(replacement, width, height) > quality) {
		return substitution
	}
	return primary
}

func (p *Document) renderCompositePreview(ctx DrawContext, object spec.CompositeObject, unit *spec.CompositeGraphicUnit, dp *spec.DrawParam, pb spec.StBox, parent *spec.CTM, clip *geom.Path, budget *renderBudget) bool {
	if unit.Thumbnail == 0 && unit.Substitution == 0 {
		return false
	}
	ctm := spec.CTM{object.Boundary.Width, 0, 0, object.Boundary.Height, 0, 0}
	if parent != nil {
		ctm = *parent.Multiply(&ctm)
	}
	width, height := p.imagePixelDemand(ctx, ctm)
	img := p.substitutionImageWithBudget(unit.Thumbnail, budget)
	if imageSampleQuality(img, width, height) < 1 {
		img = p.substitutionImageWithBudget(unit.Substitution, budget)
	}
	if imageSampleQuality(img, width, height) < 1 {
		return false
	}
	if !p.placeCompositeImage(ctx, object, img, spec.StBox{Width: unit.Width, Height: unit.Height}, false, unit.Width, unit.Height, false, pb, parent, clip, budget) {
		return false
	}
	p.drawCompositePreviewText(ctx, object, unit, dp, pb, parent, clip, budget)
	return true
}
