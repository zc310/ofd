package render

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"log/slog"
	"math"
	"strconv"
	"strings"

	_ "github.com/dkrisman/gobig2"
	"github.com/zc310/ofd/internal/media"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func (p *Document) Image(ctx DrawContext, object models.ImageObject, dp *models.DrawParam, pb models.StBox) {
	p.image(ctx, object, dp, pb, nil, nil)
}

func (p *Document) image(ctx DrawContext, object models.ImageObject, _ *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path) {
	if !object.VisibleValue() || !object.CTM.IsFinite() || !parentCTM.IsFinite() ||
		!object.Boundary.IsFinite() || !pb.IsFinite() || !finiteFloat(pb.Height) {
		return
	}
	resMedia := p.Document.GetMedia(models.StID(object.ResourceID))
	if resMedia == nil && object.Substitution != 0 {
		resMedia = p.Document.GetMedia(models.StID(object.Substitution))
	}
	if resMedia == nil {
		return
	}

	ctm := imageCTM(object)
	if parentCTM != nil {
		ctm = *parentCTM.Multiply(&ctm)
		if !ctm.IsFinite() {
			return
		}
	}

	// SVG 向量嵌入：无裁剪时直接将 SVG 画布以矢量方式嵌入页面画布，
	// 使 PDF 输出保持矢量（边框、文字等清晰），而非 96dpi 位图。
	if isSVGFormat(resMedia.Format, resMedia.MediaFile) &&
		(object.Clips == nil || len(object.Clips.Clip) == 0) && parentClip == nil {
		if svg, err := p.decodeSVGScene(resMedia.MediaFile, resMedia.Format); err == nil && svg.Width() > 0 && svg.Height() > 0 {
			// svg 画布为 y 向上、左下角原点坐标系，而 imageMatrixWH 面向
			// y 向下（图像像素）坐标系，需先翻转 y 轴再应用放置矩阵。
			flip := geom.Matrix{{1, 0, 0}, {0, -1, svg.Height()}}
			m := imageMatrixWH(object.Boundary, svg.Width(), svg.Height(), ctm, pb.Height).Mul(flip)
			if finiteMatrix(m) {
				m = ctx.CurrentMatrix().Mul(m)
				if finiteMatrix(m) {
					p.svgMu.Lock()
					rendered := svg.RenderVector(ctx, m)
					p.svgMu.Unlock()
					if rendered {
						return
					}
					// 非矢量后端：栅格化 SVG 后再经 RenderImage 贴回，
					// 保持图形内容可见（失矢量清晰度）。
					svgImage := svg.Rasterize(geom.DPI(96))
					if svgImage != nil && !svgImage.Bounds().Empty() {
						ctx.RenderImage(svgImage, m)
						return
					}
				}
			}
		}
		// 向量嵌入失败时回退到位图路径
	}

	img, err := p.decodeImage(resMedia.MediaFile.Clean(), resMedia.Format)
	if (err != nil || img == nil || img.Bounds().Empty()) && object.Substitution != 0 && models.StID(object.Substitution) != models.StID(resMedia.ID) {
		if fallback := p.Document.GetMedia(models.StID(object.Substitution)); fallback != nil {
			if decoded, decodeErr := p.decodeImage(fallback.MediaFile.Clean(), fallback.Format); decodeErr == nil && decoded != nil && !decoded.Bounds().Empty() {
				img = decoded
				err = nil
			}
		}
	}
	if err != nil {
		slog.Error("decode image failed", "file", resMedia.MediaFile, "error", err)
		return
	}
	if img == nil || img.Bounds().Empty() {
		return
	}
	if object.ImageMask != 0 {
		if maskMedia := p.Document.GetMedia(models.StID(object.ImageMask)); maskMedia != nil {
			if mask, maskErr := p.decodeImage(maskMedia.MediaFile.Clean(), maskMedia.Format); maskErr == nil && mask != nil {
				img = applyResourceImageMask(img, mask)
			}
		}
	}

	m := imageMatrix(object.Boundary, img, ctm, pb.Height)
	if !finiteMatrix(m) {
		return
	}

	if clip := p.buildImageClip(object.Clips, pb.Height, object.Boundary.X, object.Boundary.Y, ctm); clip != nil {
		img = imageWithClip(img, clip, m)
	}
	if parentClip != nil {
		img = imageWithClip(img, parentClip, m)
	}
	m = ctx.CurrentMatrix().Mul(m)
	if !finiteMatrix(m) {
		return
	}
	ctx.RenderImage(img, m)
	p.drawImageBorder(ctx, object, pb.Height)
}

func (p *Document) drawImageBorder(ctx DrawContext, object models.ImageObject, pageHeight float64) {
	border := object.Border
	if border == nil || !object.Boundary.IsFinite() {
		return
	}
	lineWidth := border.LineWidth
	if lineWidth <= 0 {
		lineWidth = defaultLineWidth
	}
	box := object.Boundary
	rx := border.HorizonalCornerRadius
	ry := border.VerticalCornerRadius
	if rx < 0 {
		rx = 0
	}
	if ry < 0 {
		ry = 0
	}
	path := roundedImageBorder(box.Width, box.Height, rx, ry)
	if path.Empty() {
		return
	}
	ctx.Push()
	defer ctx.Pop()
	ctx.ClearFill()
	ctx.SetStrokeWidth(lineWidth)
	if len(border.DashPattern) > 0 {
		dashes := make([]float64, 0, len(border.DashPattern))
		for _, value := range border.DashPattern {
			parsed, err := strconv.ParseFloat(value, 64)
			if err != nil || parsed < 0 {
				dashes = nil
				break
			}
			dashes = append(dashes, parsed)
		}
		if len(dashes) > 0 {
			ctx.SetDashes(border.DashOffset, dashes...)
		}
	}
	stroke := color.RGBA{A: 255}
	if border.BorderColor != nil {
		stroke = p.colorRGBA(*border.BorderColor)
	}
	ctx.SetStrokeColor(stroke)
	ctx.DrawPath(box.X, pageHeight-(box.Y+box.Height), path)
}

func roundedImageBorder(width, height, rx, ry float64) *geom.Path {
	if width <= 0 || height <= 0 {
		return &geom.Path{}
	}
	if rx > width/2 {
		rx = width / 2
	}
	if ry > height/2 {
		ry = height / 2
	}
	if rx == 0 && ry == 0 {
		return geom.Rectangle(width, height)
	}
	if rx == 0 {
		rx = ry
	}
	if ry == 0 {
		ry = rx
	}
	// 局部坐标 Y 向上，从左上角顺时针绕行。geom 的 sweep 语义与
	// geom.RoundedRectangle 一致：sweep=true 只在逆时针绕行时得到凸弧，
	// 顺时针绕行必须传 sweep=false，否则圆角会反向凹进图形内部。
	path := &geom.Path{}
	path.MoveTo(rx, height)
	path.LineTo(width-rx, height)
	path.ArcTo(rx, ry, 0, false, false, width, height-ry)
	path.LineTo(width, ry)
	path.ArcTo(rx, ry, 0, false, false, width-rx, 0)
	path.LineTo(rx, 0)
	path.ArcTo(rx, ry, 0, false, false, 0, ry)
	path.LineTo(0, height-ry)
	path.ArcTo(rx, ry, 0, false, false, rx, height)
	path.Close()
	return path
}

func applyResourceImageMask(img, mask image.Image) image.Image {
	bounds := img.Bounds()
	if bounds.Empty() || mask == nil || mask.Bounds().Empty() {
		return img
	}
	src := decodedImage(img)
	mask = decodedImage(mask)
	out := image.NewNRGBA(bounds)
	maskBounds := mask.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		my := maskBounds.Min.Y + (y-bounds.Min.Y)*maskBounds.Dy()/bounds.Dy()
		if my >= maskBounds.Max.Y {
			my = maskBounds.Max.Y - 1
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			mx := maskBounds.Min.X + (x-bounds.Min.X)*maskBounds.Dx()/bounds.Dx()
			if mx >= maskBounds.Max.X {
				mx = maskBounds.Max.X - 1
			}
			c := nrgbaAt(src, x, y)
			mc := nrgbaAt(mask, mx, my)
			luma := (299*uint32(mc.R) + 587*uint32(mc.G) + 114*uint32(mc.B)) / 1000
			if mc.A == 0 {
				luma = 0
			}
			c.A = uint8(uint32(c.A) * luma / 255)
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	switch source := img.(type) {
	case *image.NRGBA:
		return source.NRGBAAt(x, y)
	case *image.RGBA:
		return color.NRGBAModel.Convert(source.RGBAAt(x, y)).(color.NRGBA)
	default:
		return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	}
}

func decodedImage(img image.Image) image.Image {
	type lazyImage interface {
		Image() (image.Image, error)
	}
	for range 4 {
		lazy, ok := img.(lazyImage)
		if !ok {
			return img
		}
		decoded, err := lazy.Image()
		if err != nil || decoded == nil || decoded == img {
			return img
		}
		img = decoded
	}
	return img
}

// imageCTM 返回图片对象使用的变换矩阵。
func imageCTM(object models.ImageObject) models.CTM {
	if object.CTM != nil {
		return *object.CTM
	}
	return models.CTM{object.Boundary.Width, 0, 0, object.Boundary.Height, 0, 0}
}

// imageMatrix 将图片像素坐标映射到 OFD 页面坐标。
func imageMatrix(box models.StBox, img image.Image, ctm models.CTM, pageHeight float64) geom.Matrix {
	imgW := float64(img.Bounds().Dx())
	imgH := float64(img.Bounds().Dy())
	return imageMatrixWH(box, imgW, imgH, ctm, pageHeight)
}

// imageMatrixWH 将宽高为 (w, h) 的坐标空间映射到 OFD 页面坐标。
func imageMatrixWH(box models.StBox, w, h float64, ctm models.CTM, pageHeight float64) geom.Matrix {
	return geom.Matrix{
		{ctm[0] / w, -ctm[2] / h, box.X + ctm[2] + ctm[4]},
		{-ctm[1] / w, ctm[3] / h, pageHeight - box.Y - ctm[3] - ctm[5]},
	}
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

func isSVGFormat(format string, file models.StLoc) bool {
	return strings.EqualFold(format, "SVG") || strings.EqualFold(file.Ext(), ".svg")
}

func (p *Document) decodeSVGScene(file models.StLoc, format string) (SVGScene, error) {
	key := file.Clean().String()
	imageLock := p.imageLock(key)
	defer p.releaseImageLock(key, imageLock)
	imageLock.mu.Lock()
	defer imageLock.mu.Unlock()
	if cached, ok := p.svgCanvases.Get(key); ok {
		return cached, nil
	}
	if !isSVGFormat(format, file) {
		return nil, errors.New("not SVG")
	}
	data, err := p.Document.FileCache.Read(key)
	if err != nil {
		return nil, err
	}
	scene, err := parseSVGScene(data)
	if err != nil {
		return nil, err
	}
	p.svgCanvases.AddWeighted(key, scene, svgCanvasWeight(data))
	return scene, nil
}

// svgCanvasWeight 估算解析后 SVG 画布的内存占用。画布内部结构无法直接度量，
// 使用源 SVG 字节数作为与复杂度相关的代理值，保证至少为 1。
func svgCanvasWeight(data []byte) int64 {
	if len(data) <= 0 {
		return 1
	}
	return int64(len(data))
}

// imageCacheWeight 估算图片在缓存中的字节占用。懒加载图片同时保留压缩字节和
// 按需解码后的像素，因此两者一并计入；其他图片按解码后的像素估算。
func imageCacheWeight(img image.Image) int64 {
	if img == nil {
		return 1
	}
	if lazy, ok := img.(*EncodedImage); ok {
		return lazy.Weight()
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return 1
	}
	return int64(bounds.Dx()) * int64(bounds.Dy()) * imageBytesPerPixel(img.ColorModel())
}

// imageBytesPerPixel 估算每种颜色模型的每像素字节数，用于图片缓存权重。
func imageBytesPerPixel(model color.Model) int64 {
	switch model {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model, color.YCbCrModel:
		return 2
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	default:
		return 4
	}
}

func (p *Document) decodeImage(file models.StLoc, format string) (image.Image, error) {
	key := file.Clean().String()
	imageLock := p.imageLock(key)
	defer p.releaseImageLock(key, imageLock)
	imageLock.mu.Lock()
	defer imageLock.mu.Unlock()
	if cached, ok := p.images.Get(key); ok {
		return cached, nil
	}
	if strings.EqualFold(format, "SVG") || strings.EqualFold(file.Ext(), ".svg") {
		data, err := p.Document.FileCache.Read(key)
		if err != nil {
			return nil, err
		}
		scene, err := parseSVGScene(data)
		if err != nil {
			return nil, err
		}
		img := scene.Rasterize(geom.DPI(96))
		p.images.AddWeighted(key, img, imageCacheWeight(img))
		return img, nil
	}
	data, err := p.Document.FileCache.Read(key)
	if err != nil {
		return nil, err
	}
	img, err := decodeRasterImage(data)
	if err != nil {
		return nil, err
	}
	p.images.AddWeighted(key, img, imageCacheWeight(img))
	return img, nil
}

var pngSignature = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// decodeRasterImage 对 JPEG/PNG 使用懒解码并保留原始编码字节的
// EncodedImage，供 canvas PDF 写入器等按原字节内嵌。
// PDF 渲染器可以按原字节嵌入（DCT 等过滤），避免解码后重新转成 RGB 再 flate 压缩。
func decodeRasterImage(data []byte) (image.Image, error) {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return newEncodedImage("jpeg", data), nil
	}
	if len(data) >= len(pngSignature) && bytes.Equal(data[:len(pngSignature)], pngSignature) {
		return newEncodedImage("png", data), nil
	}
	return media.DecodeBytes(data)
}

func (p *Document) buildImageClip(clips *models.Clips, pageH, bx, by float64, objectCTM models.CTM) *geom.Path {
	if clips == nil || len(clips.Clip) == 0 {
		return nil
	}
	var result *geom.Path
	for _, clip := range clips.Clip {
		current := p.buildImageClipRegion(clip, clips.TransFlag, pageH, bx, by, objectCTM)
		if current != nil {
			if result == nil {
				result = current
			} else {
				result = result.And(current)
			}
		}
	}
	return result
}

// buildImageClipRegion 合并同一个 Clip 中的所有 Area。
func (p *Document) buildImageClipRegion(clip models.CtClip, transFlag *bool, pageH, bx, by float64, objectCTM models.CTM) *geom.Path {
	var result *geom.Path
	for _, area := range clip.Area {
		if area.Path == nil {
			continue
		}

		areaCTM := models.IdentityMatrix
		if area.CTM != nil {
			if !area.CTM.IsFinite() {
				continue
			}
			areaCTM = *area.CTM
		}
		if transFlag == nil || *transFlag {
			areaCTM = *objectCTM.Multiply(&areaCTM)
		}
		pathCTM := areaCTM
		if area.Path.CTM != nil {
			if !area.Path.CTM.IsFinite() {
				continue
			}
			pathCTM = *areaCTM.Multiply(area.Path.CTM)
		}
		if !pathCTM.IsFinite() {
			continue
		}

		clipPath := p.newPath(area.Path, func(pt models.StPos) (float64, float64) {
			// Path 的 AbbreviatedData 使用 Path 自身的局部坐标，必须先加上
			// Boundary 偏移，再应用 Area/Path CTM。忽略 Boundary 会让裁剪区
			// 在复合图元等场景中整体偏移，通常表现为右侧或左侧露出一条边。
			pt.X += area.Path.Boundary.X
			pt.Y += area.Path.Boundary.Y
			x, y := pathCTM.TransformPoint(pt)
			return x + bx, pageH - (y + by)
		})
		clipPath.Close()
		if result == nil {
			result = clipPath
		} else {
			result = result.Or(clipPath)
		}
	}
	return result
}

func imageWithClip(img image.Image, clip *geom.Path, m geom.Matrix) image.Image {
	if img == nil || clip == nil || !finiteMatrix(m) || geom.Equal(m.Det(), 0) {
		return img
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return img
	}
	if clipCoversImage(clip, m, w, h) {
		return img
	}
	mask := imageClipMask(clip, m, w, h)
	return applyImageMask(img, mask)
}

// clipCoversImage 判断裁剪路径在图像像素坐标下是否完全覆盖图像范围。
// 常见的由生产者生成的 OFD 会为图片附上与原图等大的矩形裁切（多为版面出血
// 裁切：贴边图四周留 1-3px 的出血边），此时掩码不会改变任何有效像素，跳过
// 昂贵的全分辨率掩码合成，让 JPEG/PNG 源图可以按原字节直传 PDF。
// 容差按图像短边的 1.5% 计（下限 2px）：允许一个极细的出血边框，小图
// （短边 <~130px）仍走掩码路径，避免把真实内容裁切误判为可跳过。
func clipCoversImage(clip *geom.Path, m geom.Matrix, width, height int) bool {
	if clip == nil || width <= 0 || height <= 0 || !finiteMatrix(m) || geom.Equal(m.Det(), 0) {
		return false
	}
	inverse := m.Inv()
	if !finiteMatrix(inverse) {
		return false
	}
	maskPath := clip.Copy().Transform(inverse)
	b := maskPath.Bounds()
	eps := math.Max(2.0, 0.015*float64(min(width, height)))
	if b.X0 > eps || b.Y0 > eps || b.X1 < float64(width)-eps || b.Y1 < float64(height)-eps {
		return false
	}
	area := polygonArea(maskPath.Coords())
	boxArea := (b.X1 - b.X0) * (b.Y1 - b.Y0)
	if boxArea <= 0 {
		return false
	}
	return area >= 0.99*boxArea
}

func polygonArea(points []geom.Point) float64 {
	if len(points) < 3 {
		return 0
	}
	var sum float64
	last := points[len(points)-1]
	for _, point := range points {
		sum += last.X*point.Y - point.X*last.Y
		last = point
	}
	return math.Abs(sum) / 2
}

// imageClipMask 将页面裁剪路径转换为图片像素掩码。
func imageClipMask(clip *geom.Path, m geom.Matrix, width, height int) *image.RGBA {
	inverse := m.Inv()
	if !finiteMatrix(inverse) {
		return image.NewRGBA(image.Rect(0, 0, width, height))
	}
	maskPath := clip.Copy().Transform(inverse)
	surface := newOffscreenSurface(float64(width), float64(height), geom.DPMM(1))
	surface.SetFillColor(color.White)
	surface.DrawPath(0, 0, maskPath)
	return surface.Raster()
}

// applyImageMask 将掩码透明度合成到原图片。
// 对常见像素格式使用类型化快速路径，避免逐像素的 color.Model 接口分发。
func applyImageMask(img image.Image, mask *image.RGBA) image.Image {
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	src := img
	if lazy, ok := img.(*EncodedImage); ok {
		if decoded, err := lazy.Image(); err == nil {
			src = decoded
		}
	}
	switch source := src.(type) {
	case *image.YCbCr:
		applyMaskYCbCr(source, mask, out, bounds.Min, mask.Rect.Min)
	case *image.NRGBA:
		applyMaskNRGBA(source, mask, out, bounds.Min, mask.Rect.Min)
	case *image.RGBA:
		applyMaskRGBA(source, mask, out, bounds.Min, mask.Rect.Min)
	default:
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
				ma := mask.RGBAAt(x-bounds.Min.X, y-bounds.Min.Y).A
				c.A = uint8(int(c.A) * int(ma) / 255)
				out.SetNRGBA(x, y, c)
			}
		}
	}
	return out
}

func applyMaskYCbCr(src *image.YCbCr, mask *image.RGBA, out *image.NRGBA, imgMin, maskMin image.Point) {
	for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
		yi := (y - src.Rect.Min.Y) * src.YStride
		ci := y - src.Rect.Min.Y
		for x := src.Rect.Min.X; x < src.Rect.Max.X; x++ {
			yy := src.Y[yi+x-src.Rect.Min.X]
			cidx := cOffset(src, ci, x-src.Rect.Min.X)
			r, g, b := color.YCbCrToRGB(yy, src.Cb[cidx], src.Cr[cidx])
			oi := out.PixOffset(x, y)
			mi := mask.PixOffset(x-imgMin.X+maskMin.X, y-imgMin.Y+maskMin.Y)
			out.Pix[oi+0] = r
			out.Pix[oi+1] = g
			out.Pix[oi+2] = b
			out.Pix[oi+3] = mask.Pix[mi+3]
		}
	}
}

func cOffset(src *image.YCbCr, row, col int) int {
	switch src.SubsampleRatio {
	case image.YCbCrSubsampleRatio444:
		return row*src.CStride + col
	case image.YCbCrSubsampleRatio422:
		return row*src.CStride + col>>1
	case image.YCbCrSubsampleRatio420:
		return row>>1*src.CStride + col>>1
	case image.YCbCrSubsampleRatio440:
		return row>>1*src.CStride + col
	case image.YCbCrSubsampleRatio410:
		return row>>2*src.CStride + col>>2
	case image.YCbCrSubsampleRatio411:
		return row*src.CStride + col>>2
	default:
		return row*src.CStride + col
	}
}

func applyMaskNRGBA(src *image.NRGBA, mask *image.RGBA, out *image.NRGBA, imgMin, maskMin image.Point) {
	for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
		for x := src.Rect.Min.X; x < src.Rect.Max.X; x++ {
			si := src.PixOffset(x, y)
			oi := out.PixOffset(x, y)
			mi := mask.PixOffset(x-imgMin.X+maskMin.X, y-imgMin.Y+maskMin.Y)
			out.Pix[oi+0] = src.Pix[si+0]
			out.Pix[oi+1] = src.Pix[si+1]
			out.Pix[oi+2] = src.Pix[si+2]
			out.Pix[oi+3] = alphaMul(src.Pix[si+3], mask.Pix[mi+3])
		}
	}
}

func applyMaskRGBA(src *image.RGBA, mask *image.RGBA, out *image.NRGBA, imgMin, maskMin image.Point) {
	for y := src.Rect.Min.Y; y < src.Rect.Max.Y; y++ {
		for x := src.Rect.Min.X; x < src.Rect.Max.X; x++ {
			si := src.PixOffset(x, y)
			oi := out.PixOffset(x, y)
			mi := mask.PixOffset(x-imgMin.X+maskMin.X, y-imgMin.Y+maskMin.Y)
			r, g, b, a := src.Pix[si+0], src.Pix[si+1], src.Pix[si+2], src.Pix[si+3]
			if a == 0xff {
				out.Pix[oi+0] = r
				out.Pix[oi+1] = g
				out.Pix[oi+2] = b
				out.Pix[oi+3] = mask.Pix[mi+3]
				continue
			}
			// 与 color.NRGBAModel.Convert(color.RGBA) 一致：先去除预乘再叠加掩码。
			out.Pix[oi+3] = alphaMul(a, mask.Pix[mi+3])
			if a == 0 {
				out.Pix[oi+0], out.Pix[oi+1], out.Pix[oi+2] = 0, 0, 0
				continue
			}
			r16 := uint32(r) * 0xffff / uint32(a)
			g16 := uint32(g) * 0xffff / uint32(a)
			b16 := uint32(b) * 0xffff / uint32(a)
			out.Pix[oi+0] = uint8(r16 >> 8)
			out.Pix[oi+1] = uint8(g16 >> 8)
			out.Pix[oi+2] = uint8(b16 >> 8)
		}
	}
}

func alphaMul(a, b uint8) uint8 {
	return uint8(int(a) * int(b) / 255)
}
