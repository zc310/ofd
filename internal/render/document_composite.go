package render

import (
	"image"
	"image/color"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/models"
)

// maxCompositeDepth 限制复合图元的递归嵌套深度，防止循环引用导致无限递归。
const maxCompositeDepth = 32

// Composite 绘制复合图元（CompositeObject）。
//
// CompositeGraphicUnit 的内容位于自身的 [0,Width]x[0,Height] 坐标系中，
// 先渲染完整单元，再使用 CompositeObject 的 Boundary 和 CTM 映射到页面，
// 保留单元内部坐标，不根据透明像素重新裁剪内容。
func (p *Document) Composite(ctx *canvas.Context, object models.CompositeObject, dp *models.DrawParam, pb models.StBox) {
	p.composite(ctx, object, dp, pb, nil, nil)
}

func (p *Document) composite(ctx *canvas.Context, object models.CompositeObject, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *canvas.Path) {
	if !object.VisibleValue() {
		return
	}
	if p.compositeDepth >= maxCompositeDepth {
		return
	}
	unit, ok := p.CompositeUnits[models.StID(object.ResourceID)]
	if !ok || unit == nil {
		return
	}

	w, h := unit.Width, unit.Height
	box := object.Boundary
	if w <= 0 || h <= 0 || box.Width <= 0 || box.Height <= 0 {
		return
	}
	if object.DrawParam > 0 {
		if objectDP := p.Document.GetDrawParam(models.StID(object.DrawParam)); objectDP != nil {
			dp = objectDP
		}
	}

	// 在单元自身的坐标系中绘制全部内容。
	cc := canvas.New(w, h)
	cctx := canvas.NewContext(cc)
	p.compositeDepth++
	defer func() { p.compositeDepth-- }()
	p.drawItems(cctx, unit.Content.Items, dp, models.StBox{Width: w, Height: h})

	// 栅格化分辨率以最终内容在页面上约 300dpi 为准，避免对超大单元产生过大的位图。
	dpi := 300.0 * box.Width / w
	if dpi <= 0 {
		dpi = 300.0
	}
	if dpi > 1200 {
		dpi = 1200
	}
	if dpi < 10 {
		dpi = 10
	}
	raster := rasterizer.Draw(cc, canvas.DPI(dpi), canvas.DefaultColorSpace)
	if raster == nil || raster.Bounds().Empty() {
		return
	}

	// CompositeGraphicUnit 经常使用比实际内容更大的坐标系。去掉单元四周的
	// 透明区域后，才能把实际可见面板映射到 CompositeObject 的 Boundary。
	cx0, cy0, cx1, cy1 := contentImageBounds(raster)
	if cx1 <= cx0 || cy1 <= cy0 {
		return
	}
	img := cropImage(raster, int(cx0), int(cy0), int(cx1), int(cy1))
	ctm := models.CTM{box.Width, 0, 0, box.Height, 0, 0}
	if parentCTM != nil {
		ctm = *parentCTM.Multiply(&ctm)
	}
	// 顶层 CompositeObject 的 Boundary 已经定义了页面尺寸；其 CTM 是
	// 复合单元内容使用的内部变换，不能再次作为离屏图片的整体缩放。
	m := imageMatrix(box, img, ctm, pb.Height)
	// Clip 的 Area/Path 坐标经过自身 CTM 后位于页面坐标系。buildImageClip
	// 会依据 TransFlag 决定是否叠加 CompositeObject 的 CTM，避免在 false
	// 时重复缩放裁剪区域，同时保留 true 时的对象变换。
	clipCTM := models.IdentityMatrix
	if object.CTM != nil {
		clipCTM = *object.CTM
	}
	if parentCTM != nil {
		clipCTM = *parentCTM.Multiply(&clipCTM)
	}
	if clip := p.buildImageClip(object.Clips, pb.Height, box.X, box.Y, clipCTM); clip != nil {
		img = imageWithClip(img, clip, m)
	}
	if parentClip != nil {
		img = imageWithClip(img, parentClip, m)
	}

	if object.Alpha != nil {
		img = applyImageAlpha(img, graphicOpacity(object.Alpha))
	}
	ctx.RenderImage(img, ctx.CoordSystemView().Mul(ctx.View()).Mul(m))
}

// contentImageBounds 返回离屏复合单元中非透明内容的像素包围盒。
func contentImageBounds(img image.Image) (x0, y0, x1, y1 float64) {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha <= 8*257 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if minX > maxX || minY > maxY {
		return 0, 0, 0, 0
	}
	return float64(minX), float64(minY), float64(maxX + 1), float64(maxY + 1)
}

func cropImage(img image.Image, x0, y0, x1, y1 int) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			out.Set(x-x0, y-y0, img.At(x, y))
		}
	}
	return out
}

// applyImageAlpha 将整张图片的透明度统一乘以 alpha。
func applyImageAlpha(img image.Image, alpha uint8) image.Image {
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			c.A = uint8(int(c.A) * int(alpha) / 255)
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}
