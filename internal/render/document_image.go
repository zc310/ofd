package render

import (
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageasset"
	"github.com/zc310/ofd/internal/render/imageops"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
	"github.com/zc310/ofd/internal/render/paint"
	"github.com/zc310/ofd/pkg/spec"
)

var errMissingImageMask = errors.New("找不到图像掩码资源")

func (p *Document) Image(ctx DrawContext, object spec.ImageObject, dp *spec.DrawParam, pb spec.StBox) {
	if p.beginOperation() != nil {
		return
	}
	defer p.operations.Done()
	p.image(ctx, object, dp, pb, nil, nil)
}

func (p *Document) image(ctx DrawContext, object spec.ImageObject, _ *spec.DrawParam, pb spec.StBox, parentCTM *spec.CTM, parentClip *geom.Path) {
	p.imageWithBudget(ctx, object, nil, pb, parentCTM, parentClip, nil)
}

func (p *Document) imageWithBudget(ctx DrawContext, object spec.ImageObject, _ *spec.DrawParam, pb spec.StBox, parentCTM *spec.CTM, parentClip *geom.Path, budget *renderBudget) {
	if budget == nil {
		budget = &renderBudget{}
		budget.reset()
	}
	if !drawableGraphicUnit(object.VisibleValue(), object.CTM, parentCTM, object.Boundary, pb) {
		return
	}
	if !finiteMatrix(ctx.CurrentMatrix()) {
		return
	}
	ctm := imagelayout.CTM(object)
	if parentCTM != nil {
		ctm = *parentCTM.Multiply(&ctm)
		if !ctm.IsFinite() {
			return
		}
	}
	selected := p.selectImageResourceWithBudget(ctx, object.ResourceID, object.Substitution, ctm, budget)
	resMedia := p.GetMedia(spec.StID(selected))
	if resMedia == nil {
		budget.diagnose("missing_image", "找不到图像资源", spec.StID(object.ResourceID))
		return
	}
	// 图片放置的缺省 CTM 以 Boundary 缩放单位图像；毫米裁剪只使用显式
	// 对象 CTM 与父级变换（GB/T 33190—2016 §8.4 表34）。边框共用这份裁剪。
	clipCTM := spec.IdentityCTM()
	if object.CTM != nil {
		clipCTM = *object.CTM
	}
	if parentCTM != nil {
		clipCTM = *parentCTM.Multiply(&clipCTM)
		if !clipCTM.IsFinite() {
			return
		}
	}
	borderClip := p.buildImageClip(object.Clips, object.Boundary.X, object.Boundary.Y, clipCTM)
	borderClip = intersectClips(borderClip, parentClip)

	// SVG 向量嵌入：无裁剪时直接将 SVG 画布以矢量方式嵌入页面画布，
	// 使 PDF 输出保持矢量（边框、文字等清晰），而非 96dpi 位图。
	if imageasset.IsSVG(resMedia.Format, resMedia.MediaFile) &&
		(object.Clips == nil || len(object.Clips.Clip) == 0) && parentClip == nil &&
		graphicOpacity(object.Alpha) == 255 && object.ImageMask == 0 {
		if svg, err := p.decodeSVGScene(resMedia.MediaFile, resMedia.Format); err == nil && svg.Width() > 0 && svg.Height() > 0 {
			// 保留 SVG 场景与位图路径已有的源轴向适配；页面坐标的轴向转换
			// 由 CurrentMatrix 在后端边界统一完成。
			flip := geom.Matrix{{1, 0, 0}, {0, -1, svg.Height()}}
			m := imagelayout.MatrixWH(object.Boundary, svg.Width(), svg.Height(), ctm).Mul(flip)
			bounds := geom.RectFromSize(0, 0, svg.Width(), svg.Height()).Transform(m)
			if finiteMatrix(m) && clipContainsRect(graphicBoundaryClip(object.Boundary, parentCTM), bounds) {
				m = ctx.CurrentMatrix().Mul(m)
				if finiteMatrix(m) {
					rendered := svg.RenderVector(ctx, m)
					if rendered {
						p.drawImageBorder(ctx, object, parentCTM, borderClip)
						return
					}
					// 非矢量后端走下面的位图路径，按像素尺寸重新计算放置矩阵。
				}
			}
		}
		// 向量嵌入失败时回退到位图路径
	}

	width, height := p.imagePixelDemand(ctx, ctm)
	img, err := p.decodeImageWithBudget(resMedia.MediaFile.Clean(), resMedia.Format, width, height, budget)
	if (err != nil || img == nil || img.Bounds().Empty()) && object.Substitution != 0 && spec.StID(object.Substitution) != resMedia.ID {
		if fallback := p.GetMedia(spec.StID(object.Substitution)); fallback != nil {
			if decoded, decodeErr := p.decodeImageWithBudget(fallback.MediaFile.Clean(), fallback.Format, width, height, budget); decodeErr == nil && decoded != nil && !decoded.Bounds().Empty() {
				img = decoded
				err = nil
			}
		}
	}
	if err != nil {
		budget.diagnose("image_decode_failed", err.Error(), spec.StID(object.ResourceID))
		return
	}
	if img == nil || img.Bounds().Empty() {
		return
	}
	if object.ImageMask != 0 {
		masked, maskErr := p.maskedImage(img, object.ImageMask, budget)
		if maskErr != nil {
			budget.diagnose("image_mask_failed", maskErr.Error(), spec.StID(object.ImageMask))
			return
		}
		img = masked
	}

	m := imagelayout.Matrix(object.Boundary, img, ctm)
	if !finiteMatrix(m) {
		return
	}

	clip := intersectClips(borderClip, graphicBoundaryClip(object.Boundary, parentCTM))
	imageMatrix := m
	m = ctx.CurrentMatrix().Mul(imageMatrix)
	if !finiteMatrix(m) {
		return
	}
	// 局部裁剪只计算一次，原生裁剪不支持时继续用同一条路径生成位图掩码。
	localClip := imagelayout.Clip(clip, imageMatrix, img.Bounds().Dx(), img.Bounds().Dy())
	if localClip != nil && localClip.Empty() {
		p.drawImageBorder(ctx, object, parentCTM, borderClip)
		return
	}
	opacity := graphicOpacity(object.Alpha)
	if opacity == 0 {
		return
	}
	if opacity != 255 {
		if !allowImageAllocation(img, 1, budget) {
			return
		}
		img = applyImageAlpha(img, opacity)
	}
	if localClip != nil && !localClip.Empty() {
		if clipped, ok := ctx.(interface {
			RenderClippedImage(image.Image, geom.Matrix, *geom.Path) bool
		}); ok {
			if clipped.RenderClippedImage(img, m, localClip) {
				p.drawImageBorder(ctx, object, parentCTM, borderClip)
				return
			}
		}
		// 软件裁剪会同时创建覆盖率掩码和输出像素，必须先为两份分配预留额度。
		if !allowImageAllocation(img, 2, budget) {
			return
		}
	}
	img = imageWithLocalClipAlpha(img, localClip, 255)
	ctx.RenderImage(img, m)
	p.drawImageBorder(ctx, object, parentCTM, borderClip)
}

// maskedImage 对显式指定但不可用的掩码采用关闭式失败，禁止显示无遮罩的原图。
func (p *Document) maskedImage(img image.Image, resource spec.StRefID, budget *renderBudget) (image.Image, error) {
	media := p.GetMedia(spec.StID(resource))
	if media == nil {
		return nil, fmt.Errorf("%w %d", errMissingImageMask, resource)
	}
	mask, err := p.decodeImageWithBudget(media.MediaFile.Clean(), media.Format, float64(img.Bounds().Dx()), float64(img.Bounds().Dy()), budget)
	if err != nil {
		return nil, err
	}
	mask, err = imageasset.Decoded(mask)
	if err != nil {
		return nil, err
	}
	img, err = imageasset.Decoded(img)
	if err != nil {
		return nil, err
	}
	if !allowImageAllocation(img, 1, budget) {
		return nil, imageasset.ErrPixelBudget
	}
	return imageops.ApplyResourceMask(img, mask), nil
}

func allowImageAllocation(img image.Image, copies int, budget *renderBudget) bool {
	bounds := img.Bounds()
	return imageasset.CheckSize(bounds.Dx(), bounds.Dy()) == nil &&
		budget.AllowOffscreenPixels(float64(bounds.Dx()), float64(bounds.Dy())*float64(copies), 25.4)
}

func (p *Document) drawImageBorder(ctx DrawContext, object spec.ImageObject, parent *spec.CTM, clip *geom.Path) {
	border := object.Border
	if border == nil || !object.Boundary.IsFinite() {
		return
	}
	// GB/T 33190—2016 §10 表43：缺省 0.353mm，显式 0 不绘制。
	lineWidth := paint.DefaultLineWidth
	if border.LineWidth != nil {
		lineWidth = *border.LineWidth
	}
	if lineWidth <= 0 || !finiteFloat(lineWidth) {
		return
	}
	scale := paint.StrokeScale(nil, parent)
	lineWidth *= scale
	if !finiteFloat(lineWidth) {
		return
	}
	path := imagelayout.BorderPath(object, parent)
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
	style := spec.CTGraphicUnit{}
	if len(border.DashPattern) > 0 {
		dashes := make(spec.StArray[float64], 0, len(border.DashPattern))
		for _, value := range border.DashPattern {
			if !finiteFloat(value) || value < 0 {
				dashes = nil
				break
			}
			dashes = append(dashes, value*scale)
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
	p.drawClippedPathStyle(ctx, path, clip, spec.PathObject{CtPath: spec.CtPath{Stroke: spec.NewBool(true)}}, style)
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

func (p *Document) buildImageClip(clips *spec.Clips, bx, by float64, objectCTM spec.CTM) *geom.Path {
	return p.buildPathClip(clips, spec.StBox{X: bx, Y: by}, &objectCTM, nil)
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
	if img == nil || imageasset.CheckSize(img.Bounds().Dx(), img.Bounds().Dy()) != nil {
		return image.NewNRGBA(image.Rectangle{})
	}
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
	if imageasset.CheckSize(width, height) != nil {
		return nil
	}
	surface := newAlphaMaskSurface(float64(width), float64(height), geom.DPMM(1))
	if surface == nil {
		return nil
	}
	surface.SetFillColor(color.White)
	surface.DrawPath(0, 0, clip)
	return surface.RasterAlpha()
}

func applyImageMaskAlpha(img image.Image, mask *image.Alpha, alpha uint8) image.Image {
	return imageops.ApplyMaskAlpha(img, mask, alpha)
}
