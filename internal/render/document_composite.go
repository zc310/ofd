package render

import (
	"image"
	"image/color"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// maxCompositeDepth 限制复合图元的递归嵌套深度，防止循环引用导致无限递归。
const maxCompositeDepth = 32

// compositeAxisEpsilon 是判定 CTM 是否为轴向缩放的阈值。
const compositeAxisEpsilon = 1e-9

// compositeExtentTolerance 是声明尺寸与 Boundary/CTM 反推范围的相对误差上限，
// 超过该比例即认为单元声明的 Width/Height 不可信。
const compositeExtentTolerance = 0.1

// 收缩离屏画布时给内容包围盒预留的边距：固定毫米数加自身尺寸的比例，
// 用于容忍字形轮廓略微溢出 Boundary。
const (
	compositeInkMarginAbs   = 0.5
	compositeInkMarginRatio = 0.02
)

// compositeInkMaxAreaRatio 是内容包围盒面积占整幅画布面积的上限。超过该比例
// 时不收缩画布，因为此时节省的离屏像素有限，却引入了毫米取整误差。
const compositeInkMaxAreaRatio = 0.5

// compositeContentExtent 由 CompositeObject 的 Boundary 与 CTM 反推复合单元的
// 内容范围，即单元坐标系里实际会被映射到 Boundary 的区域。旋转或错切下
// Boundary 与内容范围不再一一对应，此时不做反推。
func compositeContentExtent(object models.CompositeObject) (width, height float64, ok bool) {
	if object.CTM == nil || !object.CTM.IsFinite() {
		return 0, 0, false
	}
	ctm := *object.CTM
	if math.Abs(ctm[1]) > compositeAxisEpsilon || math.Abs(ctm[2]) > compositeAxisEpsilon {
		return 0, 0, false
	}
	if ctm[0] == 0 || ctm[3] == 0 {
		return 0, 0, false
	}
	width = object.Boundary.Width / math.Abs(ctm[0])
	height = object.Boundary.Height / math.Abs(ctm[3])
	if !finiteFloat(width) || !finiteFloat(height) || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// compositeExtentMismatch 判断声明尺寸与反推范围是否显著不符。
func compositeExtentMismatch(declared, extent float64) bool {
	if declared <= 0 {
		return false
	}
	return math.Abs(extent/declared-1) > compositeExtentTolerance
}

// compositeInkRect 估算复合单元全部内容在单元画布中的包围盒（毫米，y 向下）。
//
// 路径按实际轮廓取包围盒：路径数据可能远超出声明的 Boundary，只信任
// Boundary 会把可见内容裁掉。文字按 Boundary 估算并留出边距。只有可见范围
// 能可靠推断、且绘制位置随离屏画布原点一起平移时才返回结果；描边、旋转和
// 嵌套内容的实际绘制都可能越出推断范围，此时返回 ok=false，由调用方沿用
// 整幅画布。
func (p *Document) compositeInkRect(unit *models.CompositeGraphicUnit, w, h float64) (models.StBox, bool) {
	if unit == nil || w <= 0 || h <= 0 {
		return models.StBox{}, false
	}
	axisAligned := func(ctm *models.CTM) bool {
		if ctm == nil {
			return true
		}
		return ctm.IsFinite() && math.Abs(ctm[1]) <= compositeAxisEpsilon &&
			math.Abs(ctm[2]) <= compositeAxisEpsilon
	}
	var (
		x0, y0, x1, y1 float64
		seen           bool
	)
	add := func(cx0, cy0, cx1, cy1 float64) {
		if !seen {
			x0, y0, x1, y1 = cx0, cy0, cx1, cy1
			seen = true
			return
		}
		x0, y0 = math.Min(x0, cx0), math.Min(y0, cy0)
		x1, y1 = math.Max(x1, cx1), math.Max(y1, cy1)
	}
	for _, item := range unit.Content.Items {
		switch item.Kind {
		case models.PageItemText:
			if item.Text == nil || !axisAligned(item.Text.CTM) || item.Text.Stroke ||
				!item.Text.Boundary.IsFinite() {
				return models.StBox{}, false
			}
			// 单元内容的 y 轴自下而上，单元画布的 y 轴自上而下。
			b := item.Text.Boundary
			add(b.X, h-b.Y-b.Height, b.X+b.Width, h-b.Y)
		case models.PageItemPath:
			if item.Path == nil || !axisAligned(item.Path.CTM) || item.Path.Stroke.Value(true) {
				return models.StBox{}, false
			}
			// buildObjectPathWithTransform 已按 h 翻转 y 轴，得到的就是画布坐标。
			bounds := p.buildObjectPathWithTransform(*item.Path, h, nil).Bounds()
			if bounds.Empty() {
				return models.StBox{}, false
			}
			add(bounds.X0, bounds.Y0, bounds.X1, bounds.Y1)
		default:
			// 嵌套块、嵌套复合图元和图片都不参与收缩。
			//
			// 图片由 ctx.RenderImage 配合 imageMatrix 放置，该矩阵直接由
			// pb.Height 推导并已在矩阵内部完成 y 轴翻转，不经过离屏画布的
			// 坐标系，因此 surface.Translate 无法移动它；收缩画布会把图片
			// 推到画布之外。缩放方向也与文字/路径相反（文字/路径按
			// h-坐标 定位，图片按 pb.Height-坐标 定位），同一单元内无法用
			// 一次平移同时照顾两者。
			return models.StBox{}, false
		}
	}
	if !seen {
		return models.StBox{}, false
	}
	// 留出安全边距：字形轮廓可能略微溢出文字 Boundary，边距同时覆盖栅格
	// 取整误差。包围盒裁剪到画布内。
	x0 -= compositeInkMarginAbs + compositeInkMarginRatio*(x1-x0)
	x1 += compositeInkMarginAbs + compositeInkMarginRatio*(x1-x0)
	y0 -= compositeInkMarginAbs + compositeInkMarginRatio*(y1-y0)
	y1 += compositeInkMarginAbs + compositeInkMarginRatio*(y1-y0)
	x0, y0 = math.Max(x0, 0), math.Max(y0, 0)
	x1, y1 = math.Min(x1, w), math.Min(y1, h)
	iw, ih := x1-x0, y1-y0
	if !finiteFloat(iw) || !finiteFloat(ih) || iw <= 0 || ih <= 0 {
		return models.StBox{}, false
	}
	// 收益过小时不收缩：离屏画布按毫米取整，略小于原画布反而可能带来
	// 额外的取整误差。
	if iw*ih > compositeInkMaxAreaRatio*w*h {
		return models.StBox{}, false
	}
	return models.StBox{X: x0, Y: y0, Width: iw, Height: ih}, true
}

// rasterizeCompositeUnit 在 canvasBox 指定的离屏画布上绘制复合单元内容。
// canvasBox 可以只覆盖单元画布的一部分（tight 为 true），此时先平移坐标系，
// 让内容按原单元坐标绘制并落到画布左上方。逻辑坐标系始终是完整的 w×h，
// 因此无论画布是否收缩，内容的相对位置都不变。
func (p *Document) rasterizeCompositeUnit(unit *models.CompositeGraphicUnit, dp *models.DrawParam,
	canvasBox models.StBox, tight bool, w, h, dpi float64,
	compositeDepth int, budget *renderBudget) image.Image {
	surface := newOffscreenSurface(canvasBox.Width, canvasBox.Height, geom.DPI(dpi))
	if surface == nil {
		return nil
	}
	if tight {
		surface.Push()
		surface.Translate(-canvasBox.X, -canvasBox.Y)
		defer surface.Pop()
	}
	p.drawItemsWithTransform(surface, unit.Content.Items, dp,
		models.StBox{Width: w, Height: h}, nil, nil, compositeDepth+1, budget)
	return surface.Raster()
}

// compositeRasterBlank 判断离屏栅格结果是否整幅透明。
func compositeRasterBlank(raster image.Image) bool {
	if raster == nil || raster.Bounds().Empty() {
		return true
	}
	x0, y0, x1, y1 := contentImageBounds(raster)
	return x1 <= x0 || y1 <= y0
}

// Composite 绘制复合图元（CompositeObject）。
//
// CompositeGraphicUnit 的内容位于自身的 [0,Width]x[0,Height] 坐标系中，
// 先渲染完整单元，再使用 CompositeObject 的 Boundary 和 CTM 映射到页面，
// 保留单元内部坐标，不根据透明像素重新裁剪内容。
func (p *Document) Composite(ctx DrawContext, object models.CompositeObject, dp *models.DrawParam, pb models.StBox) {
	var budget renderBudget
	budget.reset()
	p.compositeWithBudget(ctx, object, dp, pb, nil, nil, 0, &budget)
}

func (p *Document) compositeWithBudget(ctx DrawContext, object models.CompositeObject, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path, compositeDepth int, budget *renderBudget) {
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

	// 复合单元声明的 Width/Height 与 Boundary/CTM 反推出的内容范围严重不符时，
	// 声明尺寸不可信：部分生产方按对象 CTM 的倒数写出单元坐标系尺寸。此时按
	// 反推范围作为内容画布，并且不按透明像素裁剪，否则单元里的小图元会被拉伸
	// 铺满整个 Boundary（y.ofd 每个单元只有一个字，裁剪后整页变成黑块）。
	cropToInk := true
	if extentW, extentH, ok := compositeContentExtent(object); ok &&
		(compositeExtentMismatch(w, extentW) || compositeExtentMismatch(h, extentH)) {
		w, h = extentW, extentH
		cropToInk = false
	}
	if object.DrawParam > 0 {
		if objectDP := p.Document.GetDrawParam(models.StID(object.DrawParam)); objectDP != nil {
			dp = objectDP
		}
	}

	if p.renderSimpleCompositeVector(ctx, object, unit, dp, pb, parentCTM, parentClip) {
		return
	}
	if p.renderCompositeTextVector(ctx, object, unit, dp, pb, parentCTM, parentClip, w, h) {
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
	// 按内容包围盒收缩离屏画布。声明坐标系常常远大于实际内容：y.ofd 的
	// 每个单元只画一行字，却按整页尺寸分配位图（每单元约 7.5M 像素），
	// 离屏像素预算每页画满约 26 个单元就耗尽，后续单元被静默丢弃，
	// 表现为 PDF 中整段文字消失。cropToInk 分支依赖“裁掉透明边后铺满
	// Boundary”的语义，收缩画布会改变最终映射，因此只在按内容范围
	// 建画布时收缩。
	canvasBox := models.StBox{Width: w, Height: h}
	tight := false
	if !cropToInk {
		if rect, ok := p.compositeInkRect(unit, w, h); ok {
			canvasBox, tight = rect, true
		}
	}
	if !budget.allowOffscreenPixels(canvasBox.Width, canvasBox.Height, dpi) {
		return
	}

	raster := p.rasterizeCompositeUnit(unit, dp, canvasBox, tight, w, h, dpi, compositeDepth, budget)
	if tight && compositeRasterBlank(raster) {
		// 包围盒推断与实际绘制范围不符时，收缩后的画布会整幅空白。回退到
		// 整幅画布重新栅格化，保证收缩只是优化而不会丢内容。
		canvasBox, tight = models.StBox{Width: w, Height: h}, false
		raster = p.rasterizeCompositeUnit(unit, dp, canvasBox, false, w, h, dpi, compositeDepth, budget)
	}

	// 这里使用调用方传入的输出分辨率，根据复合单元在页面上的放置宽度
	// 推算离屏栅格分辨率；实际值还会受到上下限和离屏像素预算限制。
	if raster == nil || raster.Bounds().Empty() {
		return
	}

	// CompositeGraphicUnit 经常使用比实际内容更大的坐标系。去掉单元四周的
	// 透明区域后，才能把实际可见面板映射到 CompositeObject 的 Boundary。
	// 声明尺寸与 Boundary/CTM 不符时画布本身已是内容范围，无需再裁剪。
	img := raster
	if cropToInk {
		cx0, cy0, cx1, cy1 := contentImageBounds(raster)
		if cx1 <= cx0 || cy1 <= cy0 {
			return
		}
		img = cropImage(raster, int(cx0), int(cy0), int(cx1), int(cy1))
	}
	ctm := models.CTM{box.Width, 0, 0, box.Height, 0, 0}
	// 离屏画布收缩到内容包围盒后，离屏图只代表单元画布的 canvasBox 子区域，
	// 必须映射回该子区域对应的 Boundary 矩形，否则内容会整体偏移。
	drawBox := box
	if tight {
		drawBox = models.StBox{
			X:      box.X + box.Width*canvasBox.X/w,
			Y:      box.Y + box.Height*(h-canvasBox.Y-canvasBox.Height)/h,
			Width:  box.Width * canvasBox.Width / w,
			Height: box.Height * canvasBox.Height / h,
		}
		ctm = models.CTM{drawBox.Width, 0, 0, drawBox.Height, 0, 0}
	}
	if parentCTM != nil {
		ctm = *parentCTM.Multiply(&ctm)
		if !ctm.IsFinite() {
			return
		}
	}
	// 顶层 CompositeObject 的 Boundary 已经定义了页面尺寸；其 CTM 是
	// 复合单元内容使用的内部变换，不能再次作为离屏图片的整体缩放。
	m := imageMatrix(drawBox, img, ctm, pb.Height)
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
	m = ctx.CurrentMatrix().Mul(m)
	if !finiteMatrix(m) {
		return
	}
	ctx.RenderImage(img, m)
}

// renderSimpleCompositeVector 将常见的单路径复合图元保持为矢量绘制。
// 裁剪、图像、渐变、嵌套复合图元以及其他需要独立绘制表面的情况，
// 仍然交由下面的栅格化回退逻辑处理。
func (p *Document) renderSimpleCompositeVector(ctx DrawContext, object models.CompositeObject, unit *models.CompositeGraphicUnit, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path) bool {
	if len(unit.Content.Items) != 1 || unit.Content.Items[0].Kind != models.PageItemPath {
		return false
	}
	pathObject := *unit.Content.Items[0].Path
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
	matrix := geom.Matrix{
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
	// 该分支只处理纯填充路径（simpleCompositePath 要求未勾边），描边缩放不适用。
	p.updateCtPathStyle(ctx, &pathObject.CtPath, dp, 1)
	ctx.DrawPath(0, 0, path)
	return true
}

// renderCompositeTextVector 把只含文字的复合图元直接画进页面矢量表面，使
// PDF/SVG 保留真实文字（可复制、可检索），而不是烘焙成位图。
//
// y.ofd 的每个复合单元只有一行文字。此前这类单元全部走栅格化回退：文字被
// 压进位图，PDF 里没有任何文字算子，pdftotext 提取 0 字符；同时离屏画布按
// 单元声明的整页尺寸分配，白白消耗预算并挤掉后续单元。
//
// 位置映射与栅格化分支保持一致：单元画布 (0,0)-(w,h) 映射到 Boundary，
// 因此这里用同一矩阵变换页面上下文，再以完整单元坐标系绘制文字。
func (p *Document) renderCompositeTextVector(ctx DrawContext, object models.CompositeObject,
	unit *models.CompositeGraphicUnit, dp *models.DrawParam, pb models.StBox,
	parentCTM *models.CTM, parentClip *geom.Path, w, h float64) bool {
	if unit == nil || len(unit.Content.Items) == 0 {
		return false
	}
	// 父级变换与父级裁剪、以及复合图元自身的裁剪，在矢量分支里需要单独还原；
	// 这些情况交回栅格化回退。
	if parentCTM != nil || parentClip != nil || object.Clips != nil {
		return false
	}
	// 只处理纯文字单元。混有路径、图像或嵌套复合图元时单元需要独立绘制
	// 表面（例如图案填充的单元格），保持栅格化。
	items := make([]models.PageItem, 0, len(unit.Content.Items))
	for _, item := range unit.Content.Items {
		if item.Kind != models.PageItemText || item.Text == nil {
			return false
		}
		text := *item.Text
		if !text.VisibleValue() || !text.CTM.IsFinite() || !text.Boundary.IsFinite() {
			return false
		}
		// Composite Alpha 表示复合图元整体透明度。矢量分支没有独立的透明度
		// 分组，与单路径分支一致地合并到文字填充色。
		if object.Alpha != nil {
			text.FillColor = cloneCompositeColor(text.FillColor, graphicOpacity(object.Alpha))
		}
		items = append(items, models.PageItem{Kind: models.PageItemText, Text: &text})
	}

	box := object.Boundary
	matrix := imageMatrixWH(box, w, h, models.CTM{box.Width, 0, 0, box.Height, 0, 0}, pb.Height)
	if !finiteMatrix(matrix) {
		return false
	}
	var budget renderBudget
	budget.reset()
	ctx.Push()
	defer ctx.Pop()
	ctx.Transform(matrix)
	p.drawItemsWithTransform(ctx, items, dp, models.StBox{Width: w, Height: h}, nil, nil, 1, &budget)
	return true
}

func cloneCompositeColor(source *models.CTColor, alpha uint8) *models.CTColor {
	if source == nil || source.Value == nil {
		return source
	}
	copy := *source
	copy.Value = &models.Color{RGBA: source.Value.RGBA}
	copy.Value.RGBA = scaleAlpha(copy.Value.RGBA, alpha)
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
