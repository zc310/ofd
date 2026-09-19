package render

import (
	"image"
	"image/color"

	"github.com/tdewolff/canvas"
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
	var budget renderBudget
	budget.reset()
	p.compositeWithBudget(ctx, object, dp, pb, nil, nil, 0, &budget)
}

func (p *Document) compositeWithBudget(ctx *canvas.Context, object models.CompositeObject, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *canvas.Path, compositeDepth int, budget *renderBudget) {
	if !object.VisibleValue() || !object.CTM.IsFinite() || !parentCTM.IsFinite() ||
		!object.Boundary.IsFinite() || !pb.IsFinite() || !finiteFloat(pb.Height) {
		return
	}
	if compositeDepth >= maxCompositeDepth {
		return
	}
	unit := p.Document.GetCompositeUnit(models.StID(object.ResourceID))
	ok := unit != nil
	if !ok || unit == nil {
		return
	}
	if !budget.allowComposite(models.StID(object.ResourceID)) {
		return
	}

	w, h := unit.Width, unit.Height
	box := object.Boundary
	if w <= 0 || h <= 0 || box.Width <= 0 || box.Height <= 0 || !finiteFloat(w) || !finiteFloat(h) {
		return
	}
	if object.DrawParam > 0 {
		if objectDP := p.Document.GetDrawParam(models.StID(object.DrawParam)); objectDP != nil {
			dp = objectDP
		}
	}

	if p.renderSimpleCompositeVector(ctx, object, unit, dp, pb, parentCTM, parentClip) {
		return
	}

	// 在创建离屏画布前扣除预算，避免异常尺寸先完成分配再被限制。
	dpi := p.dpi.DPI() * box.Width / w
	if dpi <= 0 {
		dpi = defaultRenderDPI
	}
	if dpi > 1200 {
		dpi = 1200
	}
	if dpi < 10 {
		dpi = 10
	}
	if !budget.allowOffscreenPixels(w, h, dpi) {
		return
	}

	// 在单元自身的坐标系中绘制全部内容。
	cc := canvas.New(w, h)
	cctx := canvas.NewContext(cc)
	p.drawItemsWithTransform(cctx, unit.Content.Items, dp, models.StBox{Width: w, Height: h}, nil, nil, compositeDepth+1, budget)

	// 这里使用调用方传入的输出分辨率，根据复合单元在页面上的放置宽度
	// 推算离屏栅格分辨率；实际值还会受到上下限和离屏像素预算限制。
	var raster image.Image = Rasterize(cc, canvas.DPI(dpi), canvas.DefaultColorSpace)
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
		if !ctm.IsFinite() {
			return
		}
	}
	// 顶层 CompositeObject 的 Boundary 已经定义了页面尺寸；其 CTM 是
	// 复合单元内容使用的内部变换，不能再次作为离屏图片的整体缩放。
	m := imageMatrix(box, img, ctm, pb.Height)
	if !finiteMatrix(m) {
		return
	}
	// Clip 的 Area/Path 坐标经过自身 CTM 后位于页面坐标系。buildImageClip
	// 会依据 TransFlag 决定是否叠加 CompositeObject 的 CTM，避免在 false
	// 时重复缩放裁剪区域，同时保留 true 时的对象变换。
	clipCTM := models.IdentityMatrix
	if object.CTM != nil {
		if !object.CTM.IsFinite() {
			return
		}
		clipCTM = *object.CTM
	}
	if parentCTM != nil {
		clipCTM = *parentCTM.Multiply(&clipCTM)
		if !clipCTM.IsFinite() {
			return
		}
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
	m = ctx.CoordSystemView().Mul(ctx.View()).Mul(m)
	if !finiteMatrix(m) {
		return
	}
	ctx.RenderImage(img, m)
}

// renderSimpleCompositeVector 将常见的单路径复合图元保持为矢量绘制。
// 裁剪、图像、渐变、嵌套复合图元以及其他需要独立绘制表面的情况，
// 仍然交由下面的栅格化回退逻辑处理。
func (p *Document) renderSimpleCompositeVector(ctx *canvas.Context, object models.CompositeObject, unit *models.CompositeGraphicUnit, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *canvas.Path) bool {
	if len(unit.Content.Items) != 1 || unit.Content.Items[0].Kind != models.PageItemPath {
		return false
	}
	pathObject := unit.Content.Items[0].Path
	if parentCTM != nil || parentClip != nil || !simpleCompositePath(pathObject) {
		return false
	}

	path := p.buildObjectPathWithTransform(pathObject, unit.Height, nil)
	pathBounds := path.Bounds()
	if pathBounds.Empty() || pathBounds.W() <= 0 || pathBounds.H() <= 0 ||
		!finiteFloat(pathBounds.X0) || !finiteFloat(pathBounds.Y0) || !finiteFloat(pathBounds.X1) || !finiteFloat(pathBounds.Y1) {
		return false
	}

	widthScale := object.Boundary.Width / pathBounds.W()
	heightScale := object.Boundary.Height / pathBounds.H()
	matrix := canvas.Matrix{
		{widthScale, 0, object.Boundary.X - pathBounds.X0*widthScale},
		{0, heightScale, pb.Height - object.Boundary.Y - object.Boundary.Height - pathBounds.Y0*heightScale},
	}
	if !finiteFloat(widthScale) || !finiteFloat(heightScale) || !finiteMatrix(matrix) {
		return false
	}
	path.Transform(matrix)

	// Composite Alpha 表示复合图元整体透明度。当前情况只有一个纯色填充，
	// 因此可以直接合并到填充颜色中，不需要创建独立的透明度分组。
	if object.Alpha != nil {
		pathObject.FillColor = cloneCompositeColor(pathObject.FillColor, graphicOpacity(object.Alpha))
	}
	ctx.Push()
	defer ctx.Pop()
	p.updateCtPathStyle(ctx, &pathObject.CtPath, dp)
	ctx.DrawPath(0, 0, path)
	return true
}

func cloneCompositeColor(source *models.CTColor, alpha uint8) *models.CTColor {
	if source == nil || source.Value == nil {
		return source
	}
	copy := *source
	copy.Value = &models.Color{RGBA: source.Value.RGBA}
	copy.Value.A = uint8(uint16(copy.Value.A) * uint16(alpha) / 255)
	return &copy
}

func simpleCompositePath(object models.PathObject) bool {
	if !object.VisibleValue() || !object.CTM.IsFinite() || object.Clips != nil || !object.Fill || object.Stroke.Value(true) {
		return false
	}
	if object.FillColor == nil || object.FillColor.Value == nil {
		return false
	}
	return object.FillColor.Pattern == nil &&
		object.FillColor.AxialShd == nil &&
		object.FillColor.RadialShd == nil &&
		object.FillColor.GouraudShd == nil &&
		object.FillColor.LaGourandShd == nil &&
		object.FillColor.LaGouraudShd == nil
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
