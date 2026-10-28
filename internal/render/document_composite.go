package render

import (
	"image"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/content"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageops"
	compositelayout "github.com/zc310/ofd/internal/render/layout/composite"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
	"github.com/zc310/ofd/internal/render/pathbuild"
)

// maxCompositeDepth 限制复合图元的递归嵌套深度，防止循环引用导致无限递归。
const maxCompositeDepth = content.MaxDepth

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
	if !drawableGraphicUnit(object.VisibleValue(), object.CTM, parentCTM, object.Boundary, pb) {
		return
	}
	if compositeDepth >= maxCompositeDepth {
		budget.diagnose("composite_depth", "复合图元嵌套超过上限", models.StID(object.ResourceID))
		return
	}
	unit := p.Document.GetCompositeUnit(models.StID(object.ResourceID))
	if unit == nil {
		budget.diagnose("missing_composite", "找不到复合图元资源", models.StID(object.ResourceID))
		return
	}
	budget.ResourceID = models.StID(object.ResourceID)
	if !budget.AllowComposite(models.StID(object.ResourceID)) {
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
	if extentW, extentH, ok := compositelayout.ContentExtent(object); ok &&
		(compositelayout.ExtentMismatch(w, extentW) || compositelayout.ExtentMismatch(h, extentH)) {
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
	if p.renderCompositeVector(ctx, object, unit, dp, pb, parentCTM, parentClip, cropToInk, w, h, compositeDepth, budget) {
		return
	}
	// 半透明纯矢量组改走独立合成后，仍保留原先可复制的文字层。
	semanticGroup := !cropToInk && parentCTM == nil && parentClip == nil && object.Clips == nil &&
		graphicOpacity(object.Alpha) > 0 && graphicOpacity(object.Alpha) < 255

	// 在创建离屏画布前扣除预算，避免异常尺寸先完成分配再被限制。
	outputDPI := p.dpi.DPI()
	if semanticGroup {
		// 此分支原本输出矢量；新引入的透明度合成按目标密度采样，
		// PDF/SVG至少保留300DPI，避免以默认96DPI烘焙文字。
		outputDPI = p.paintResolution(ctx).DPI()
	}
	dpi := outputDPI * box.Width / w
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
		if rect, ok := compositelayout.InkRect(unit, w, h); ok {
			canvasBox, tight = rect, true
		}
	}
	if !budget.AllowOffscreenPixels(canvasBox.Width, canvasBox.Height, dpi) {
		return
	}

	raster := p.rasterizeCompositeUnit(unit, dp, canvasBox, tight, w, h, dpi, compositeDepth, budget)
	if tight && compositeRasterBlank(raster) {
		// 包围盒推断与实际绘制范围不符时，收缩后的画布会整幅空白。回退到
		// 整幅画布重新栅格化，保证收缩只是优化而不会丢内容。
		canvasBox, tight = models.StBox{Width: w, Height: h}, false
		if !budget.AllowOffscreenPixels(w, h, dpi) {
			return
		}
		raster = p.rasterizeCompositeUnit(unit, dp, canvasBox, false, w, h, dpi, compositeDepth, budget)
	}
	// cropToInk 为真时 tight 必为假：收缩画布只在按内容范围建画布（cropToInk
	// 为假）时发生，上面的回退分支也已把 tight 复位。这里等价于判断 cropToInk。
	if cropToInk {
		if resampled, ok := p.resampleCompositeUnit(unit, dp, raster, w, h, dpi,
			object.Boundary, outputDPI, compositeDepth, budget); ok {
			// 重画的结果已经裁到可见范围，不能再走 tight 分支：那个分支把画布
			// 映射到 Boundary 的子矩形，而这里要的正是「裁掉透明边后铺满整个
			// Boundary」。裁剪在这里做，映射留给整块 Boundary。
			raster, cropToInk, tight = resampled, false, false
		}
	}

	// 这里使用调用方传入的输出分辨率，根据复合单元在页面上的放置宽度
	// 推算离屏栅格分辨率；实际值还会受到上下限和离屏像素预算限制。
	if raster == nil || raster.Bounds().Empty() {
		return
	}

	if p.placeCompositeImage(ctx, object, raster, canvasBox, tight, w, h, cropToInk, pb, parentCTM, parentClip) && semanticGroup {
		if items, ok := copyCompositeVectorItems(unit.Content.Items, nil, 0); ok {
			p.drawCompositeSemanticText(ctx, object, items, dp, pb, w, h, budget)
		}
	}
}

// resampleCompositeUnit 按首遍量到的可见范围重画一遍复合单元，返回裁到可见
// 范围的离屏图。
//
// 离屏 DPI 由「输出 DPI × Boundary 宽 ÷ 单元宽」推得，前提是单元内容铺满整个
// 单元坐标系。生产方经常不满足：一枚 30×20mm 的发票监制章被放进声明为整张 A4
// 的 CompositeGraphicUnit 里，于是这个前提不成立，DPI 塌到四十左右，章在离屏画布
// 上只有五十像素宽，再拉伸到输出分辨率就是一团糊——同一枚章直接画在页面上则完全
// 清晰，差别只在写法。
//
// 这里用首遍栅格化实测的可见范围替代声明尺寸：画布收缩到该范围（离屏像素同时
// 大幅下降），DPI 相应提高，使可见内容在页面上正好达到输出分辨率。首遍量到的范围
// 来自真实绘制结果，因此带旋转 CTM、带描边的单元同样适用——compositelayout.InkRect 对
// 这些情况直接放弃推断，正是它接不了的活。
//
// 两处坐标系的差别容易踩：contentImageBounds 返回图像坐标（y 自顶端向下），而
// canvasBox 与画布平移用的是画布坐标（y 向上），两者差一个 h-y-height。少这一步
// 换算，收缩后的画布会落在单元的另一端，重画结果整幅空白。
func (p *Document) resampleCompositeUnit(unit *models.CompositeGraphicUnit, dp *models.DrawParam,
	raster image.Image, w, h, dpi float64, box models.StBox, outDPI float64,
	compositeDepth int, budget *renderBudget) (image.Image, bool) {
	img, _, ok := p.resampleCompositeUnitSource(unit, dp, raster, w, h, dpi, box, outDPI, compositeDepth, budget)
	return img, ok
}

// 同时返回裁后图片覆盖的单元区域（OFD 左上原点），用于文字与链接的同坐标映射。
func (p *Document) resampleCompositeUnitSource(unit *models.CompositeGraphicUnit, dp *models.DrawParam,
	raster image.Image, w, h, dpi float64, box models.StBox, outDPI float64,
	compositeDepth int, budget *renderBudget) (image.Image, models.StBox, bool) {
	cx0, cy0, cx1, cy1 := contentImageBounds(raster)
	canvasBounds, next, ok := compositelayout.Resample(geom.Rect{X0: cx0, Y0: cy0, X1: cx1, Y1: cy1}, w, h, dpi, box, outDPI)
	if !ok {
		return nil, models.StBox{}, false
	}
	canvasBox := models.StBox{X: canvasBounds.X0, Y: canvasBounds.Y0, Width: canvasBounds.W(), Height: canvasBounds.H()}
	// 预算按两遍合计计费：首遍已经扣过一次，这里再扣收缩后这一遍。
	if !budget.AllowOffscreenPixels(canvasBox.Width, canvasBox.Height, next) {
		return nil, models.StBox{}, false
	}
	out := p.rasterizeCompositeUnit(unit, dp,
		canvasBox, true, w, h, next, compositeDepth, budget)
	if compositeRasterBlank(out) {
		return nil, models.StBox{}, false
	}
	// 裁到重画后的实际可见范围：调用方按整块 Boundary 贴图，多余的透明边会让
	// 内容整体缩小。
	nx0, ny0, nx1, ny1 := contentImageBounds(out)
	if nx1 <= nx0 || ny1 <= ny0 {
		return nil, models.StBox{}, false
	}
	step := 25.4 / next
	source := models.StBox{X: canvasBounds.X0 + nx0*step, Y: h - canvasBounds.Y1 + ny0*step, Width: (nx1 - nx0) * step, Height: (ny1 - ny0) * step}
	return cropImage(out, int(nx0), int(ny0), int(nx1), int(ny1)), source, true
}

// placeCompositeImage 把已栅格化的复合单元内容按 CompositeObject 的 Boundary
// 贴回页面：按需裁掉透明边、计算放置矩阵、应用裁剪区与整体透明度，最后经
// RenderImage 绘制。放置矩阵不可用时放弃该复合图元，返回 false。
//
// canvasBox/tight 描述离屏画布覆盖单元画布的范围：tight 为真时画布只覆盖
// canvasBox 子区域，离屏图必须映射回该子区域对应的 Boundary 矩形，否则内容
// 会整体偏移。cropToInk 为真表示单元声明尺寸可信，需要裁掉四周透明区域再把
// 可见面板铺满 Boundary。
func (p *Document) placeCompositeImage(ctx DrawContext, object models.CompositeObject, raster image.Image,
	canvasBox models.StBox, tight bool, w, h float64, cropToInk bool,
	pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path) bool {
	// CompositeGraphicUnit 经常使用比实际内容更大的坐标系。去掉单元四周的
	// 透明区域后，才能把实际可见面板映射到 CompositeObject 的 Boundary。
	// 声明尺寸与 Boundary/CTM 不符时画布本身已是内容范围，无需再裁剪。
	img := raster
	if cropToInk {
		cx0, cy0, cx1, cy1 := contentImageBounds(raster)
		if cx1 <= cx0 || cy1 <= cy0 {
			return false
		}
		img = cropImage(raster, int(cx0), int(cy0), int(cx1), int(cy1))
	}
	box := object.Boundary
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
			return false
		}
	}
	// 顶层 CompositeObject 的 Boundary 已经定义了页面尺寸；其 CTM 是
	// 复合单元内容使用的内部变换，不能再次作为离屏图片的整体缩放。
	m := imagelayout.Matrix(drawBox, img, ctm, pb.Height)
	if !finiteMatrix(m) {
		return false
	}
	// Clip 的 Area/Path 坐标经过自身 CTM 后位于页面坐标系。buildImageClip
	// 会依据 TransFlag 决定是否叠加 CompositeObject 的 CTM，避免在 false
	// 时重复缩放裁剪区域，同时保留 true 时的对象变换。
	clipCTM := models.IdentityMatrix
	if object.CTM != nil {
		if !object.CTM.IsFinite() {
			return false
		}
		clipCTM = *object.CTM
	}
	if parentCTM != nil {
		clipCTM = *parentCTM.Multiply(&clipCTM)
		if !clipCTM.IsFinite() {
			return false
		}
	}
	clip := p.buildImageClip(object.Clips, pb.Height, box.X, box.Y, clipCTM)
	clip = intersectClips(clip, parentClip)
	clip = intersectClips(clip, graphicBoundaryClip(box, pb.Height, parentCTM))
	img = imageWithClipAlpha(img, clip, m, graphicOpacity(object.Alpha))
	m = ctx.CurrentMatrix().Mul(m)
	if !finiteMatrix(m) {
		return false
	}
	ctx.RenderImage(img, m)
	return true
}

// renderSimpleCompositeVector 将常见的单路径复合图元保持为矢量绘制。
// 裁剪、图像、渐变、嵌套复合图元以及其他需要独立绘制表面的情况，
// 仍然交由下面的栅格化回退逻辑处理。
func (p *Document) renderSimpleCompositeVector(ctx DrawContext, object models.CompositeObject, unit *models.CompositeGraphicUnit, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path) bool {
	if unit == nil || len(unit.Content.Items) != 1 || unit.Content.Items[0].Kind != models.PageItemPath || unit.Content.Items[0].Path == nil {
		return false
	}
	w, h, cropToInk := unit.Width, unit.Height, true
	// 与 compositeWithBudget 的兼容处理一致：声明尺寸不可信时保留完整内容
	// 坐标系，不能把单个小路径的墨迹包围盒拉伸铺满 Boundary。
	if extentW, extentH, ok := compositelayout.ContentExtent(object); ok &&
		(compositelayout.ExtentMismatch(w, extentW) || compositelayout.ExtentMismatch(h, extentH)) {
		w, h, cropToInk = extentW, extentH, false
	}
	pathObject := *unit.Content.Items[0].Path
	if parentCTM != nil || parentClip != nil || !simpleCompositePath(pathObject) {
		return false
	}

	path := pathbuild.Object(pathObject, h, nil)
	pathBounds := path.Bounds()
	if pathBounds.Empty() || pathBounds.W() <= 0 || pathBounds.H() <= 0 ||
		!finiteFloat(pathBounds.X0) || !finiteFloat(pathBounds.Y0) || !finiteFloat(pathBounds.X1) || !finiteFloat(pathBounds.Y1) {
		return false
	}
	if !cropToInk {
		pathBounds = geom.RectFromSize(0, 0, w, h)
	}

	matrix, ok := content.MapBox(
		models.StBox{X: pathBounds.X0, Y: pathBounds.Y0, Width: pathBounds.W(), Height: pathBounds.H()},
		models.StBox{X: object.Boundary.X, Y: pb.Height - object.Boundary.Y - object.Boundary.Height, Width: object.Boundary.Width, Height: object.Boundary.Height},
	)
	if !ok {
		return false
	}
	path.Transform(matrix)

	// Composite Alpha 表示复合图元整体透明度。当前情况只有一个纯色填充，
	// 合并到图元 Alpha，保留原始颜色指针供缓存复用。
	pathObject.Alpha = compositeItemAlpha(pathObject.Alpha, object.Alpha)
	ctx.Push()
	defer ctx.Pop()
	// 该分支只处理纯填充路径（simpleCompositePath 要求未勾边），描边缩放不适用。
	p.updateCtPathStyle(ctx, &pathObject.CtPath, dp, 1)
	clip := p.graphicClip(object.CTGraphicUnit, pb.Height, nil, nil)
	if clipContainsRect(clip, path.Bounds()) {
		ctx.DrawPath(0, 0, path)
	} else {
		p.drawClippedPath(ctx, path, clip, pathObject)
	}
	return true
}

// renderCompositeVector 把纯矢量内容（文字、路径、嵌套块）的复合图元直接画进
// 页面矢量表面，使 PDF/SVG 保留真实文字与矢量轮廓，而不是烘焙成位图。
//
// y.ofd 的每个复合单元只有一行文字，火车票印章则是描边圆环加旋转文字并包在
// PageBlock 里。此前只有纯文字单元能走矢量分支，印章这类"路径+文字+块"的
// 内容全部走栅格化：离屏画布按声明尺寸分配，一枚 30mm 的章被放进 210mm 的
// 坐标系，DPI 塌到输出 DPI 的七分之一，缩放回页面就是一团糊。
//
// 位置映射与栅格化分支保持一致：单元画布 (0,0)-(w,h) 映射到 Boundary，因此
// 这里用同一矩阵变换页面上下文，再以完整单元坐标系绘制内容。图像与裁剪区
// 仍需独立绘制表面，交回栅格化回退。
func (p *Document) renderCompositeVector(ctx DrawContext, object models.CompositeObject,
	unit *models.CompositeGraphicUnit, dp *models.DrawParam, pb models.StBox,
	parentCTM *models.CTM, parentClip *geom.Path, cropToInk bool, w, h float64, compositeDepth int, budget *renderBudget) bool {
	if unit == nil || len(unit.Content.Items) == 0 {
		return false
	}
	// 父级变换与父级裁剪、以及复合图元自身的裁剪，在矢量分支里需要单独还原；
	// 这些情况交回栅格化回退。
	if parentCTM != nil || parentClip != nil || object.Clips != nil {
		return false
	}
	// 表34的Alpha作用于复合对象整体；逐子对象乘Alpha会让重叠区域变深。
	// 后端没有透明度组时，交给离屏分支合成后只应用一次Alpha。
	if graphicOpacity(object.Alpha) < 255 {
		return false
	}
	// cropToInk 为真表示声明尺寸可信、内容铺满单元坐标系，栅格化分支会裁掉
	// 透明边后把可见面板拉伸铺满 Boundary（intro.ofd 依赖该行为）。矢量分支
	// 不做裁边缩放，直接按单元坐标系映射，这里交回栅格化保持原语义。
	if cropToInk {
		return false
	}
	items, ok := copyCompositeVectorItems(unit.Content.Items, nil, 0)
	if !ok {
		return false
	}

	box := object.Boundary
	matrix, valid := content.MapBox(models.StBox{Width: w, Height: h}, models.StBox{
		X: box.X, Y: pb.Height - box.Y - box.Height, Width: box.Width, Height: box.Height,
	})
	if !valid {
		return false
	}
	ctx.Push()
	defer ctx.Pop()
	ctx.Transform(matrix)
	// 与离屏画布一致，矢量组的内容也不能溢出单元范围（表50）。
	p.drawItemsWithTransform(ctx, items, dp, models.StBox{Width: w, Height: h}, nil, geom.Rectangle(w, h), compositeDepth+1, budget)
	return true
}

// copyCompositeVectorItems 复制复合单元里可矢量绘制的内容，供矢量分支直接
// 绘制，避免就地修改文档模型。图像、嵌套复合图元以及非法/不可见图元无法可靠
// 地并入父级绘制表面，返回 ok=false 交回栅格化。多图元透明度组不能逐图元
// 分摊Alpha；直接绘制与语义层调用均传nil，保留子图元自身透明度。
func copyCompositeVectorItems(items []models.PageItem, alpha *uint8, depth int) ([]models.PageItem, bool) {
	if depth > maxCompositeDepth {
		return nil, false
	}
	out := make([]models.PageItem, 0, len(items))
	for _, item := range items {
		switch item.Kind {
		case models.PageItemText:
			if item.Text == nil {
				return nil, false
			}
			text := *item.Text
			if !text.VisibleValue() || !text.CTM.IsFinite() || !text.Boundary.IsFinite() {
				return nil, false
			}
			text.Alpha = compositeItemAlpha(text.Alpha, alpha)
			out = append(out, models.PageItem{Kind: models.PageItemText, Text: &text})
		case models.PageItemPath:
			if item.Path == nil {
				return nil, false
			}
			pathObject := *item.Path
			if !pathObject.VisibleValue() || !pathObject.CTM.IsFinite() || !pathObject.Boundary.IsFinite() {
				return nil, false
			}
			pathObject.Alpha = compositeItemAlpha(pathObject.Alpha, alpha)
			out = append(out, models.PageItem{Kind: models.PageItemPath, Path: &pathObject})
		case models.PageItemBlock:
			if item.Block == nil {
				return nil, false
			}
			sub, ok := copyCompositeVectorItems(item.Block.Items, alpha, depth+1)
			if !ok {
				return nil, false
			}
			out = append(out, models.PageItem{Kind: models.PageItemBlock, Block: &models.PageBlock{
				CTPageBlock: models.CTPageBlock{Items: sub},
			}})
		default:
			return nil, false
		}
	}
	return out, true
}

// compositeItemAlpha 合并父子图元透明度，不复制颜色或修改资源模型。
func compositeItemAlpha(child, parent *uint8) *uint8 {
	if parent == nil {
		return child
	}
	alpha := uint8(uint16(graphicOpacity(child)) * uint16(*parent) / 255)
	return &alpha
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
	return imageops.ContentBounds(img)
}

func cropImage(img image.Image, x0, y0, x1, y1 int) image.Image {
	return imageops.Crop(img, x0, y0, x1, y1)
}

// applyImageAlpha 将整张图片的透明度统一乘以 alpha。
func applyImageAlpha(img image.Image, alpha uint8) image.Image {
	return imageops.ApplyAlpha(img, alpha)
}
