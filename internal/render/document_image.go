package render

import (
	"image"
	"image/color"
	"log/slog"
	"strconv"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageasset"
	"github.com/zc310/ofd/internal/render/imageops"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
	"github.com/zc310/ofd/internal/render/paint"
)

func (p *Document) Image(ctx DrawContext, object models.ImageObject, dp *models.DrawParam, pb models.StBox) {
	p.image(ctx, object, dp, pb, nil, nil)
}

func (p *Document) image(ctx DrawContext, object models.ImageObject, _ *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path) {
	p.imageWithBudget(ctx, object, nil, pb, parentCTM, parentClip, nil)
}

func (p *Document) imageWithBudget(ctx DrawContext, object models.ImageObject, _ *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path, budget *renderBudget) {
	if !drawableGraphicUnit(object.VisibleValue(), object.CTM, parentCTM, object.Boundary, pb) {
		return
	}
	resMedia := p.Document.GetMedia(models.StID(object.ResourceID))
	if resMedia == nil && object.Substitution != 0 {
		resMedia = p.Document.GetMedia(models.StID(object.Substitution))
	}
	if resMedia == nil {
		budget.diagnose("missing_image", "找不到图像资源", models.StID(object.ResourceID))
		return
	}

	ctm := imagelayout.CTM(object)
	if parentCTM != nil {
		ctm = *parentCTM.Multiply(&ctm)
		if !ctm.IsFinite() {
			return
		}
	}
	// 图片放置的缺省 CTM 以 Boundary 缩放单位图像；毫米裁剪只使用显式
	// 对象 CTM 与父级变换（GB/T 33190—2016 §8.4 表34）。边框共用这份裁剪。
	clipCTM := models.IdentityMatrix
	if object.CTM != nil {
		clipCTM = *object.CTM
	}
	if parentCTM != nil {
		clipCTM = *parentCTM.Multiply(&clipCTM)
		if !clipCTM.IsFinite() {
			return
		}
	}
	borderClip := p.buildImageClip(object.Clips, pb.Height, object.Boundary.X, object.Boundary.Y, clipCTM)
	borderClip = intersectClips(borderClip, parentClip)

	// SVG 向量嵌入：无裁剪时直接将 SVG 画布以矢量方式嵌入页面画布，
	// 使 PDF 输出保持矢量（边框、文字等清晰），而非 96dpi 位图。
	if imageasset.IsSVG(resMedia.Format, resMedia.MediaFile) &&
		(object.Clips == nil || len(object.Clips.Clip) == 0) && parentClip == nil &&
		graphicOpacity(object.Alpha) == 255 && object.ImageMask == 0 {
		if svg, err := p.decodeSVGScene(resMedia.MediaFile, resMedia.Format); err == nil && svg.Width() > 0 && svg.Height() > 0 {
			// svg 画布为 y 向上、左下角原点坐标系，而 imagelayout.MatrixWH 面向
			// y 向下（图像像素）坐标系，需先翻转 y 轴再应用放置矩阵。
			flip := geom.Matrix{{1, 0, 0}, {0, -1, svg.Height()}}
			m := imagelayout.MatrixWH(object.Boundary, svg.Width(), svg.Height(), ctm, pb.Height).Mul(flip)
			bounds := geom.RectFromSize(0, 0, svg.Width(), svg.Height()).Transform(m)
			if finiteMatrix(m) && clipContainsRect(graphicBoundaryClip(object.Boundary, pb.Height, parentCTM), bounds) {
				m = ctx.CurrentMatrix().Mul(m)
				if finiteMatrix(m) {
					rendered := svg.RenderVector(ctx, m)
					if rendered {
						p.drawImageBorder(ctx, object, pb.Height, parentCTM, borderClip)
						return
					}
					// 非矢量后端走下面的位图路径，按像素尺寸重新计算放置矩阵。
				}
			}
		}
		// 向量嵌入失败时回退到位图路径
	}

	img, err := p.decodeImage(resMedia.MediaFile.Clean(), resMedia.Format)
	if (err != nil || img == nil || img.Bounds().Empty()) && object.Substitution != 0 && models.StID(object.Substitution) != resMedia.ID {
		if fallback := p.Document.GetMedia(models.StID(object.Substitution)); fallback != nil {
			if decoded, decodeErr := p.decodeImage(fallback.MediaFile.Clean(), fallback.Format); decodeErr == nil && decoded != nil && !decoded.Bounds().Empty() {
				img = decoded
				err = nil
			}
		}
	}
	if err != nil {
		slog.Error("decode image failed", "file", resMedia.MediaFile, "error", err)
		budget.diagnose("image_decode_failed", err.Error(), models.StID(object.ResourceID))
		return
	}
	if img == nil || img.Bounds().Empty() {
		return
	}
	if object.ImageMask != 0 {
		if maskMedia := p.Document.GetMedia(models.StID(object.ImageMask)); maskMedia != nil {
			if mask, maskErr := p.decodeImage(maskMedia.MediaFile.Clean(), maskMedia.Format); maskErr == nil && mask != nil {
				img = imageops.ApplyResourceMask(img, mask)
			}
		}
	}

	m := imagelayout.Matrix(object.Boundary, img, ctm, pb.Height)
	if !finiteMatrix(m) {
		return
	}

	clip := intersectClips(borderClip, graphicBoundaryClip(object.Boundary, pb.Height, parentCTM))
	imageMatrix := m
	m = ctx.CurrentMatrix().Mul(imageMatrix)
	if !finiteMatrix(m) {
		return
	}
	// 局部裁剪只计算一次，原生裁剪不支持时继续用同一条路径生成位图掩码。
	localClip := imagelayout.Clip(clip, imageMatrix, img.Bounds().Dx(), img.Bounds().Dy())
	if localClip != nil && !localClip.Empty() {
		if clipped, ok := ctx.(interface {
			RenderClippedImage(image.Image, geom.Matrix, *geom.Path) bool
		}); ok {
			if clipped.RenderClippedImage(applyImageAlpha(img, graphicOpacity(object.Alpha)), m, localClip) {
				p.drawImageBorder(ctx, object, pb.Height, parentCTM, borderClip)
				return
			}
		}
	}
	img = imageWithLocalClipAlpha(img, localClip, graphicOpacity(object.Alpha))
	ctx.RenderImage(img, m)
	p.drawImageBorder(ctx, object, pb.Height, parentCTM, borderClip)
}

func (p *Document) drawImageBorder(ctx DrawContext, object models.ImageObject, pageHeight float64, parent *models.CTM, clip *geom.Path) {
	border := object.Border
	if border == nil || !object.Boundary.IsFinite() {
		return
	}
	lineWidth := border.LineWidth
	if lineWidth <= 0 {
		lineWidth = paint.DefaultLineWidth
	}
	if !finiteFloat(lineWidth) {
		return
	}
	scale := paint.StrokeScale(nil, parent)
	lineWidth *= scale
	if !finiteFloat(lineWidth) {
		return
	}
	path := imagelayout.BorderPath(object, pageHeight, parent)
	if path.Empty() {
		return
	}
	ctx.Push()
	defer ctx.Pop()
	ctx.ClearFill()
	ctx.SetStrokeWidth(lineWidth)
	ctx.SetStrokeCapper(geom.ButtCap)
	ctx.SetStrokeJoiner(geom.MiterJoin)
	ctx.SetDashes(0)
	style := models.CTGraphicUnit{}
	if len(border.DashPattern) > 0 {
		dashes := make(models.StArrayF, 0, len(border.DashPattern))
		for _, value := range border.DashPattern {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || !finiteFloat(parsed) || parsed < 0 {
				dashes = nil
				break
			}
			dashes = append(dashes, parsed*scale)
		}
		if paint.ValidDashPattern(border.DashOffset*scale, &dashes) {
			style.DashOffset, style.DashPattern = border.DashOffset*scale, &dashes
			ctx.SetDashes(style.DashOffset, dashes...)
		}
	}
	stroke := color.RGBA{A: 255}
	if border.BorderColor != nil {
		stroke = p.colorRGBA(*border.BorderColor)
	}
	ctx.SetStrokeColor(paint.ScaleAlpha(stroke, graphicOpacity(object.Alpha)))
	if clipContainsRect(clip, strokeBounds(ctx, path.Bounds())) {
		ctx.DrawPath(0, 0, path)
		return
	}
	// 描边在 Boundary 中心线上，保留自身外沿；对象及继承裁剪仍约束整条描边。
	p.drawClippedPathStyle(ctx, path, clip, models.PathObject{CtPath: models.CtPath{Stroke: models.NewOptionalBool(true)}}, style)
}

func finiteMatrix(matrix geom.Matrix) bool {
	for _, row := range matrix {
		for _, value := range row {
			if !finiteFloat(value) {
				return false
			}
		}
	}
	return true
}

func (p *Document) buildImageClip(clips *models.Clips, pageH, bx, by float64, objectCTM models.CTM) *geom.Path {
	return p.buildPathClip(clips, models.StBox{X: bx, Y: by}, pageH, &objectCTM, nil)
}

func imageWithClip(img image.Image, clip *geom.Path, m geom.Matrix) image.Image {
	return imageWithClipAlpha(img, clip, m, 255)
}

// imageWithClipAlpha 只创建一份掩码和一份输出，在同一次像素循环中合成
// 源Alpha、裁剪覆盖率和对象Alpha。未裁剪的不透明图片继续保留原始编码。
func imageWithClipAlpha(img image.Image, clip *geom.Path, m geom.Matrix, alpha uint8) image.Image {
	if img == nil {
		return nil
	}
	return imageWithLocalClipAlpha(img, imagelayout.Clip(clip, m, img.Bounds().Dx(), img.Bounds().Dy()), alpha)
}

func imageWithLocalClipAlpha(img image.Image, clip *geom.Path, alpha uint8) image.Image {
	if clip == nil {
		return applyImageAlpha(img, alpha)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return img
	}
	if clip.Empty() {
		return image.NewNRGBA(bounds)
	}
	mask := imageClipMask(clip, w, h)
	if mask == nil {
		// 无法实现裁剪时跳过图片内容，避免显示原本应被裁掉的区域。
		return image.NewNRGBA(bounds)
	}
	return applyImageMaskAlpha(img, mask, alpha)
}

// imageClipMask 将图片局部裁剪路径栅格化为单通道覆盖率掩码。
func imageClipMask(clip *geom.Path, width, height int) *image.Alpha {
	surface := newAlphaMaskSurface(float64(width), float64(height), geom.DPMM(1))
	if surface == nil {
		return nil
	}
	surface.SetFillColor(color.White)
	surface.DrawPath(0, 0, clip)
	return surface.RasterAlpha()
}

// applyImageMask 将掩码透明度合成到原图片。
// 对常见像素格式使用类型化快速路径，避免逐像素的 color.Model 接口分发。
func applyImageMask(img image.Image, mask *image.Alpha) image.Image {
	return applyImageMaskAlpha(img, mask, 255)
}

func applyImageMaskAlpha(img image.Image, mask *image.Alpha, alpha uint8) image.Image {
	return imageops.ApplyMaskAlpha(img, mask, alpha)
}
