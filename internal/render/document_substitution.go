package render

import (
	"image"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageasset"
)

// imagePixelDemand 按单位图像到页面的变换计算两条像素轴的输出需求。
// §10 表43、§13 表50只规定高分辨率替换用途，没有指定固定的 DPI 阈值。
// 矢量记录使用文档的目标 DPI，直接光栅绘制使用后端实际分辨率。
func (p *Document) imagePixelDemand(ctx DrawContext, ctm models.CTM) (float64, float64) {
	resolution := p.dpi
	if raster, ok := ctx.(interface{ RasterResolution() geom.Resolution }); ok {
		resolution = raster.RasterResolution()
	}
	m := ctx.CurrentMatrix().Mul(geom.Matrix{{ctm[0], -ctm[2], 0}, {-ctm[1], ctm[3], 0}})
	return math.Hypot(m[0][0], m[1][0]) * resolution.DPMM(), math.Hypot(m[0][1], m[1][1]) * resolution.DPMM()
}

func imageSampleQuality(img image.Image, width, height float64) float64 {
	if img == nil || img.Bounds().Empty() || width <= 0 || height <= 0 || !finiteFloat(width) || !finiteFloat(height) {
		return 0
	}
	return min(float64(img.Bounds().Dx())/width, float64(img.Bounds().Dy())/height)
}

func (p *Document) substitutionImageWithBudget(id models.StRefID, budget *renderBudget) image.Image {
	media := p.GetMedia(models.StID(id))
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
func (p *Document) selectImageResource(ctx DrawContext, primary, substitution models.StRefID, ctm models.CTM) models.StRefID {
	var budget renderBudget
	budget.reset()
	return p.selectImageResourceWithBudget(ctx, primary, substitution, ctm, &budget)
}

func (p *Document) selectImageResourceWithBudget(ctx DrawContext, primary, substitution models.StRefID, ctm models.CTM, budget *renderBudget) models.StRefID {
	media := p.GetMedia(models.StID(primary))
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

func (p *Document) renderCompositePreview(ctx DrawContext, object models.CompositeObject, unit *models.CompositeGraphicUnit, dp *models.DrawParam, pb models.StBox, parent *models.CTM, clip *geom.Path, budget *renderBudget) bool {
	if unit.Thumbnail == 0 && unit.Substitution == 0 {
		return false
	}
	ctm := models.CTM{object.Boundary.Width, 0, 0, object.Boundary.Height, 0, 0}
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
	if !p.placeCompositeImage(ctx, object, img, models.StBox{Width: unit.Width, Height: unit.Height}, false, unit.Width, unit.Height, false, pb, parent, clip, budget) {
		return false
	}
	p.drawCompositePreviewText(ctx, object, unit, dp, pb, parent, clip, budget)
	return true
}
