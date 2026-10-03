package render

import (
	"image"
	"math"
	"strings"
	"sync"

	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func (p *Document) Text(ctx DrawContext, object models.TextObject, dp *models.DrawParam, pb models.StBox) {
	var budget renderBudget
	budget.reset()
	p.textWithBudget(ctx, object, dp, pb, nil, nil, &budget)
}

func (p *Document) textWithBudget(ctx DrawContext, object models.TextObject, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path, budget *renderBudget) {
	if !drawableGraphicUnit(object.VisibleValue(), object.CTM, parentCTM, object.Boundary, pb) ||
		!finiteFloat(object.Size) {
		return
	}
	if parentCTM != nil && object.CTM != nil && !parentCTM.Multiply(object.CTM).IsFinite() {
		return
	}
	ctx.Push()
	defer ctx.Pop()

	fontFamily, err := p.fonts.LoadFont(object.Font)
	if err != nil || fontFamily == nil {
		return
	}
	fontLock := p.fonts.RenderLock(fontFamily)
	fontLock.Lock()
	defer fontLock.Unlock()

	// OFD 中 Fill=false 表示文字不填充；当 Stroke=true 时仍需绘制描边。
	if textFillDisabled(object) && !object.Stroke {
		return
	}
	fill, stroke := p.updateDrawParams(ctx, dp)
	if dp == nil {
		ctx.SetStrokeWidth(defaultLineWidth)
	}
	// 字号保持 OFD 的原始 Size（对象坐标毫米）；CTM 的线性部分（缩放、旋转、
	// 倾斜）统一由 textCTMLinearMatrix 作用到字形上，不能在这里再乘一次
	// CTM.YScale，否则缩放会被应用两次，文字明显变小。
	if !finiteFloat(object.Size) {
		return
	}
	// 空心字（仅描边，不填充）：将填充置空，保留描边颜色。
	if textFillDisabled(object) && object.Stroke {
		fill = nil
	} else {
		if object.FillColor != nil {
			fill = p.updateCtColor(object.FillColor)
		}
	}
	if object.StrokeColor != nil {
		stroke = p.updateCtColor(object.StrokeColor)
	}
	if object.Alpha != nil {
		if fill != nil && fill.HasValue {
			fill = &CTColor{Value: scaleAlpha(fill.Value, graphicOpacity(object.Alpha)), HasValue: true, Gradient: fill.Gradient}
		}
		if stroke != nil && stroke.HasValue {
			stroke = &CTColor{Value: scaleAlpha(stroke.Value, graphicOpacity(object.Alpha)), HasValue: true, Gradient: stroke.Gradient}
		}
	}
	source := textFillColor(object, dp)
	// 矢量后端（PDF/SVG）能把原生 Linear/RadialGradient 序列化为文字着色图案。
	// 对可原生表达的轴向/径向渐变（含 Extend=0/1/2），改用带两端延伸位的原生
	// 渐变作为文字填充：文字保留真实文本 + 原生 Shading，PDF 中仍可复制。
	// Repeat/Reflect、椭圆、焦点径向、网格等仍走栅格化。
	if fill != nil {
		if bt, ok := ctx.(shadingTextBackend); ok && bt.ShadingText() && isAxialRadialShading(source) {
			if gradient := p.textShadingGradient(source); gradient != nil {
				fill.Gradient = gradient
			}
		}
	}
	face := p.fonts.FaceObject(fontFamily, object, fill)
	if face == nil {
		return
	}
	if !face.Fill().Has() && !(textFillDisabled(object) && object.Stroke) {
		// 颜色透明（Alpha=0）时文字不可见，且 PDF 渲染器会因此输出非法的 NaN 颜色值
		// 破坏内容流，直接跳过绘制。
		return
	}
	nativeShadingText := false
	if bt, ok := ctx.(shadingTextBackend); ok && bt.ShadingText() && nativeShadingGradient(fill) {
		nativeShadingText = !textNeedsPathDrawing(face, object)
	}
	if isShadingColor(source) && !nativeShadingText {
		if p.drawMeshText(ctx, face, source, object, pb, budget) {
			return
		}
	}
	if stroke != nil && stroke.Gradient != nil {
		ctx.SetStrokeGradient(stroke.Gradient)
	} else if stroke != nil && stroke.HasValue {
		ctx.SetStrokeColor(stroke.Value)
	} else {
		ctx.SetStrokeColor(geom.Black)
	}
	faces := newTextFaces(p.fonts, fontFamily, object, fill, face, textHScale(object))
	faces.native = nativeShadingText
	codePosition := 0
	for _, code := range object.TextCode {
		if !finiteFloat(code.X) || !finiteFloat(code.Y) || !finiteArray(code.DeltaX) || !finiteArray(code.DeltaY) {
			continue
		}
		p.drawTextCode(ctx, faces, object, code, pb.Height, parentCTM, codePosition)
		codePosition += len([]rune(code.Value))
	}
}

func textFillColor(object models.TextObject, dp *models.DrawParam) *models.CTColor {
	if object.FillColor != nil {
		return object.FillColor
	}
	if dp != nil {
		return dp.FillColor
	}
	return nil
}

// nativeShadingGradient 判断填充渐变是否是可以直接写成 PDF 原生 Shading 的
// Linear/RadialGradient（Extend=3、非焦点径向）。自定义渐变类型返回 false。
func nativeShadingGradient(fill *CTColor) bool {
	if fill == nil || fill.Gradient == nil {
		return false
	}
	switch fill.Gradient.(type) {
	case *geom.LinearGradient, *geom.RadialGradient:
		return true
	default:
		return false
	}
}

// textNeedsPathDrawing 判断文字是否必须走轮廓路径绘制：描边、逐字旋转，或无法
// 按 Unicode 整形的嵌入子集/CFF 字体。这类文字无法用原生文字 + Shading 表达。
func textNeedsPathDrawing(face FontFace, object models.TextObject) bool {
	if face == nil {
		return true
	}
	if object.Stroke || textCharDirectionDegrees(object) != 0 {
		return true
	}
	var text strings.Builder
	for _, code := range object.TextCode {
		if face.DirectPath(code.Value) != nil {
			return true
		}
		text.WriteString(code.Value)
	}
	if face.IsCFF() && !fontCanRenderText(face, text.String()) {
		return true
	}
	return false
}

// drawMeshText 将网格渐变文字先栅格化，避免 PDF/SVG 直接序列化不支持的自定义渐变。
func (p *Document) drawMeshText(ctx DrawContext, face FontFace, source *models.CTColor, object models.TextObject, pb models.StBox, budget *renderBudget) bool {
	if pb.Width <= 0 || pb.Height <= 0 || !pb.IsFinite() || !object.Boundary.IsFinite() {
		return false
	}
	transform := gradientBoundaryTransform(object.Boundary, pb.Height)
	gradient := p.pathGradient(source, transform, gradientShadingArea(object.Boundary))
	if gradient == nil {
		return false
	}
	// 只为文字自身区域创建离屏画布：整页位图会让每个渐变文字对象都承担整页
	// 栅格化成本。文字 Boundary 未必包含升/降部，向外留出一个字号避免裁字；
	// 字号按 CTM 的 Y 轴缩放换算成页面毫米。
	pad := object.Size
	if object.CTM != nil {
		if scale := object.CTM.YScale(); scale > 0 {
			pad *= scale
		}
	}
	if pad <= 0 || !finiteFloat(pad) {
		pad = 1
	}
	x0 := object.Boundary.X - pad
	x1 := object.Boundary.X + object.Boundary.Width + pad
	// Boundary.Y 自页顶起算，画布坐标自底向上，先翻转再留余量。
	y0 := pb.Height - (object.Boundary.Y + object.Boundary.Height) - pad
	y1 := pb.Height - object.Boundary.Y + pad
	width, height := x1-x0, y1-y0
	if !finiteFloat(x0) || !finiteFloat(y0) || !finiteFloat(width) || !finiteFloat(height) || width <= 0 || height <= 0 {
		return false
	}
	if !budget.allowOffscreenPixels(width, height, meshGradientDPI) {
		return false
	}
	surface := newOffscreenSurface(width, height, geom.DPI(meshGradientDPI))
	surface.SetFillGradient(translateGradient(gradient, x0, y0))
	for _, code := range object.TextCode {
		if !finiteFloat(code.X) || !finiteFloat(code.Y) || !finiteArray(code.DeltaX) || !finiteArray(code.DeltaY) {
			continue
		}
		p.drawMeshTextCode(surface, face, object, code, pb.Height, x0, y0)
	}
	var textImage image.Image = surface.Raster()
	if textImage == nil || textImage.Bounds().Empty() {
		return false
	}
	if object.Alpha != nil {
		textImage = applyImageAlpha(textImage, graphicOpacity(object.Alpha))
	}
	box := models.StBox{X: x0, Y: pb.Height - y1, Width: width, Height: height}
	matrix := imageMatrix(box, textImage, models.CTM{width, 0, 0, height, 0, 0}, pb.Height)
	matrix = ctx.CurrentMatrix().Mul(matrix)
	if !finiteMatrix(matrix) {
		return false
	}
	ctx.RenderImage(textImage, matrix)
	return true
}

func (p *Document) drawMeshTextCode(ctx DrawContext, face FontFace, object models.TextObject, code models.TextCode, pageHeight, originX, originY float64) {
	if len(code.DeltaX) == 0 && len(code.DeltaY) == 0 && textDirectionsZero(object) {
		p.drawMeshTextGlyph(ctx, face, object, code.Value, code.X, code.Y, pageHeight, originX, originY)
		return
	}

	posX, posY := code.X, code.Y
	runes := []rune(code.Value)
	for i, r := range runes {
		if i > 0 {
			deltaX, deltaY := textAdvance(face.TextWidth(string(runes[i-1])), object, code, i-1)
			posX += deltaX
			posY += deltaY
		}
		p.drawMeshTextGlyph(ctx, face, object, string(r), posX, posY, pageHeight, originX, originY)
	}
}

// drawMeshTextGlyph 在离屏画布上绘制一个字形；originX/originY 是离屏画布原点
// 在页面画布（自底向上）坐标中的位置，字形位置需整体减去它。
func (p *Document) drawMeshTextGlyph(ctx DrawContext, face FontFace, object models.TextObject, value string, x, y, pageHeight, originX, originY float64) {
	if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(pageHeight) ||
		!object.Boundary.IsFinite() || !object.CTM.IsFinite() {
		return
	}
	path := face.DirectPath(value)
	if path == nil {
		path = face.ToPath(value)
	}
	if path == nil || path.Empty() {
		return
	}
	if object.CTM != nil {
		x, y = object.CTM.Transform(x, y)
	}
	if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(x+object.Boundary.X) || !finiteFloat(pageHeight-(y+object.Boundary.Y)) {
		return
	}
	matrix := geom.Identity.Translate(x+object.Boundary.X-originX, pageHeight-(y+object.Boundary.Y)-originY)
	// 与原生文字分支一致：CTM 的线性部分整体参与变换，倾斜和缩放都不能丢。
	if m := textCTMLinearMatrix(object.CTM); m != nil {
		matrix = matrix.Mul(*m)
	}
	if direction := textCharDirectionDegrees(object); direction != 0 {
		matrix = matrix.Rotate(-direction)
	}
	matrix = matrix.Scale(textHScale(object), 1)
	if !finiteMatrix(matrix) {
		return
	}
	// path 可能是字体轮廓缓存里的共享对象，Path.Transform 原地修改；不复制
	// 会把平移累积到缓存上，同一字形在后续页面/重渲染时越偏越远。
	ctx.DrawPath(0, 0, path.Copy().Transform(matrix))
}

// textFillDisabled 判断文字对象是否明确禁止填充。
func textFillDisabled(object models.TextObject) bool {
	return !object.Fill.Value(true)
}

// textHScale 返回文字水平方向缩放比例，缺省值为 1。
func textHScale(object models.TextObject) float64 {
	if object.HScale > 0 {
		return object.HScale
	}
	return 1
}

// textAdvanceDiffers 判断显式步进是否无法用同一文本串的字体自然步进表达：
// 纵向位移非零、横向明显回退（换行回到行首），或前进步进与字体自然步进相差
// 过大。最后一种情况常见于嵌入式子集字体的 hmtx 只是占位宽度（例如全角），
// 此时按字体字宽排版会忽略 TextCode 的 DeltaX，造成字距错误；小幅字距仍然
// 合并，以保留整段绘制、避免文本提取时插入空格。
func textAdvanceDiffers(deltaX, deltaY, naturalX float64) bool {
	if math.Abs(deltaY) > 0.01 {
		return true
	}
	if deltaX < -math.Max(math.Abs(naturalX), 0.01) {
		return true
	}
	if deltaX > 0 && math.Abs(naturalX) > 0 {
		if tolerance := math.Max(0.2, math.Abs(naturalX)*0.05); math.Abs(deltaX-naturalX) > tolerance {
			return true
		}
	}
	return false
}

// normalizeTextDirection 将 OFD 方向归一化为最接近的标准象限方向。
// OFD 标准使用 0、90、180、270 度；对非标准输入采用邻近象限，避免
// 产生未定义的斜向排版结果。
func normalizeTextDirection(direction int) int {
	direction %= 360
	if direction < 0 {
		direction += 360
	}
	switch {
	case direction >= 45 && direction < 135:
		return 90
	case direction >= 135 && direction < 225:
		return 180
	case direction >= 225 && direction < 315:
		return 270
	default:
		return 0
	}
}

func textDirectionsZero(object models.TextObject) bool {
	return normalizeTextDirection(object.ReadDirection) == 0 && normalizeTextDirection(object.CharDirection) == 0
}

func textReadAdvance(advance float64, direction int) (float64, float64) {
	switch normalizeTextDirection(direction) {
	case 90:
		return 0, advance
	case 180:
		return -advance, 0
	case 270:
		return 0, -advance
	default:
		return advance, 0
	}
}

func textCharDirectionDegrees(object models.TextObject) float64 {
	return float64(normalizeTextDirection(object.CharDirection))
}

// textAdvance 返回当前字形到下一个字形的 OFD 坐标增量。显式 DeltaX/
// DeltaY 优先；两者都未指定时，按 ReadDirection 使用字体字宽作为默认步进。
func textAdvance(glyphWidth float64, object models.TextObject, code models.TextCode, index int) (float64, float64) {
	if len(code.DeltaX) > 0 || len(code.DeltaY) > 0 {
		var deltaX, deltaY float64
		if len(code.DeltaX) > 0 {
			deltaX = valueAt(code.DeltaX, index)
		}
		if len(code.DeltaY) > 0 {
			deltaY = valueAt(code.DeltaY, index)
		}
		return deltaX, deltaY
	}
	return textReadAdvance(glyphWidth*textHScale(object), object.ReadDirection)
}

// drawTextCode 按字符间距绘制一段文字。
type textGlyph struct {
	value string
}

func (p *Document) drawTextCode(ctx DrawContext, faces *textFaces, object models.TextObject, code models.TextCode, pageHeight float64, parentCTM *models.CTM, codePosition int) {
	if len(object.CGTransform) == 0 && len(code.DeltaX) == 0 && len(code.DeltaY) == 0 && textDirectionsZero(object) {
		if renderableTextValue(code.Value) {
			p.drawTextGlyph(ctx, faces, object, code.Value, code.X, code.Y, pageHeight, parentCTM)
		}
		return
	}

	// 字形宽度与字形映射与填充无关，统一使用共享字体面。
	face := faces.base
	runes := []rune(code.Value)
	glyphs := textCodeGlyphs(face, runes, object.CGTransform, codePosition)
	if len(glyphs) == 0 {
		return
	}

	// 将可正常整形的连续字形合并为一个文本串一次性绘制。逐字调用 DrawText 会让
	// PDF 中每个字成为独立定位的文本块，提取器会把这些文本块之间的间隙当成空格，
	// 导致中文提取结果每个字之间插入空格。私有区字形无法参与正常整形，单独绘制并
	// 断开当前文本串。
	posX, posY := code.X, code.Y
	var run []rune
	runX, runY := posX, posY
	flushRun := func() {
		if len(run) == 0 {
			return
		}
		p.drawTextGlyph(ctx, faces, object, string(run), runX, runY, pageHeight, parentCTM)
		run = run[:0]
	}
	for i, glyph := range glyphs {
		if i > 0 {
			prevWidth := textGlyphWidth(face, glyphs[i-1])
			glyphWidth := 0.0
			if len(code.DeltaX) == 0 && len(code.DeltaY) == 0 {
				glyphWidth = prevWidth
			}
			deltaX, deltaY := textAdvance(glyphWidth, object, code, i-1)
			posX += deltaX
			posY += deltaY
			if len(run) > 0 {
				naturalX := prevWidth * textHScale(object)
				if textAdvanceDiffers(deltaX, deltaY, naturalX) {
					flushRun()
				}
			}
		}
		// 控制字符没有可见字形，绘制时会被字体替换成 .notdef 方块；
		// 这里跳过绘制但仍按前面的增量推进，保持后续字形位置不变。
		if !renderableTextValue(glyph.value) {
			continue
		}
		if containsPrivateGlyphRune(glyph.value) {
			flushRun()
			p.drawTextGlyph(ctx, faces, object, glyph.value, posX, posY, pageHeight, parentCTM)
			continue
		}
		if len(run) == 0 {
			runX, runY = posX, posY
		}
		run = append(run, []rune(glyph.value)...)
	}
	flushRun()
}

// renderableTextValue 判断文字是否包含可见字符。C0/C1 控制字符、DEL 和
// U+FFFD 替换符没有可见字形，交给字体绘制会显示成 .notdef 方块。
func renderableTextValue(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '\uFFFD' {
			continue
		}
		return true
	}
	return false
}

var transformMapPool = sync.Pool{
	New: func() any {
		return make(map[int]models.CTCGTransform)
	},
}

func textCodeGlyphs(face FontFace, runes []rune, transforms []models.CTCGTransform, codePosition int) []textGlyph {
	if len(transforms) == 0 {
		glyphs := make([]textGlyph, len(runes))
		for i, r := range runes {
			glyphs[i] = textGlyph{value: string(r)}
		}
		return glyphs
	}
	byPosition := transformMapPool.Get().(map[int]models.CTCGTransform)
	defer transformMapPool.Put(byPosition)
	clear(byPosition)
	for _, transform := range transforms {
		byPosition[transform.CodePosition] = transform
	}
	glyphs := make([]textGlyph, 0, len(runes))
	for i := 0; i < len(runes); {
		transform, ok := byPosition[codePosition+i]
		if !ok {
			glyphs = append(glyphs, textGlyph{value: string(runes[i])})
			i++
			continue
		}
		ids := transform.Glyphs
		if transform.GlyphCount > 0 && transform.GlyphCount < len(ids) {
			ids = ids[:transform.GlyphCount]
		}
		codeCount := transform.CodeCount
		if codeCount <= 0 {
			codeCount = 1
		}
		// 一对一变换（码位数与字形数相等）：逐码位与字形按序配对。字体能按
		// 原始 Unicode 成形时优先使用原文本，让 PDF 等输出保留可复制、可搜索
		// 的文字；否则退回字形私有区引用（仅能按轮廓绘制）。
		if codeCount == len(ids) && i+codeCount <= len(runes) {
			for k := 0; k < codeCount; k++ {
				id := ids[k]
				if id < 0 || id > 0xffff {
					continue
				}
				if r := runes[i+k]; renderableTextValue(string(r)) && fontCanRenderRune(face, r) {
					glyphs = append(glyphs, textGlyph{value: string(r)})
					continue
				}
				glyphs = append(glyphs, textGlyph{value: string(fontfix.GlyphRune(uint16(id)))})
			}
		} else {
			for _, id := range ids {
				if id >= 0 && id <= 0xffff {
					glyphs = append(glyphs, textGlyph{value: string(fontfix.GlyphRune(uint16(id)))})
				}
			}
		}
		remaining := len(runes) - i
		if codeCount >= remaining {
			break
		}
		i += codeCount
	}
	return glyphs
}

func textGlyphWidth(face FontFace, glyph textGlyph) float64 {
	return face.TextWidth(glyph.value)
}

func (p *Document) drawTextGlyph(ctx DrawContext, faces *textFaces, object models.TextObject, value string, x, y, pageHeight float64, parentCTM *models.CTM) {
	face := faces.run(x, y)
	hScale := textHScale(object)
	if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(pageHeight) || !finiteFloat(hScale) ||
		!object.Boundary.IsFinite() || !object.CTM.IsFinite() || !parentCTM.IsFinite() {
		return
	}
	// 某些嵌入式 CFF 字体经过 OFD 子集修复后不能安全地交给 PDF
	// 子集器，因此对无法按调用文本成形的文字保留路径回退；已经注入文档
	// Unicode 映射、字体可以成形的文字走正常文字接口，以保留复制和搜索能力。
	isEmbeddedFont := p.fonts.FallbackFontFamily(object.Font) == ""
	if isEmbeddedFont && face.IsCFF() && !fontCanRenderText(face, value) {
		p.drawCFFTextPath(ctx, faces, object, value, x, y, pageHeight, parentCTM, hScale)
		return
	}
	if path := face.DirectPath(value); path != nil {
		p.drawTextPath(ctx, faces, path, object, x, y, pageHeight, parentCTM, hScale)
		return
	}
	// CharDirection 需要逐字旋转，Canvas 的文字排版接口不能对单个字形
	// 应用该变换，因此退回到路径绘制。
	if textCharDirectionDegrees(object) != 0 {
		if path := face.ToPath(value); path != nil && !path.Empty() {
			p.drawTextPath(ctx, faces, path, object, x, y, pageHeight, parentCTM, hScale)
			return
		}
	}
	// 只要需要描边就必须走路径绘制：Canvas 的原生文字接口用 DefaultStyle
	// 只复制 Fill（canvas/text.go 的 renderLineTo），FontFace 也没有描边字段，
	// 因此 DrawText 会静默丢弃描边。空心字与"填充+描边"文字都受此限制。
	if object.Stroke {
		if path := face.ToPath(value); path != nil && !path.Empty() {
			p.drawTextPath(ctx, faces, path, object, x, y, pageHeight, parentCTM, hScale)
			return
		}
	}
	if parentCTM != nil {
		if object.CTM != nil {
			x, y = parentCTM.Multiply(object.CTM).Transform(x, y)
		} else {
			x, y = parentCTM.Transform(x, y)
		}
		if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(x+object.Boundary.X) || !finiteFloat(pageHeight-(y+object.Boundary.Y)) {
			return
		}
		// 对于 CellContent，Boundary 定义在父级坐标系中。
		ctx.Push()
		ctx.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
		if m := textCTMLinearMatrix(object.CTM); m != nil {
			ctx.Transform(*m)
		}
		ctx.Rotate(-textCharDirectionDegrees(object))
		ctx.Scale(hScale, 1)
		p.drawTextInline(ctx, face, value)
		ctx.Pop()
		return
	}
	if object.CTM != nil {
		x, y = object.CTM.Transform(x, y)
	}
	if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(x+object.Boundary.X) || !finiteFloat(pageHeight-(y+object.Boundary.Y)) {
		return
	}
	// CTM 的线性部分必须整体作用到字形上：只用 Rotate 只能表达纯旋转，
	// 水平倾斜（CTM="1 0 0.3 1 0 0"）和非等比缩放会被静默丢弃，字形仍是直立的。
	ctx.Push()
	ctx.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
	if m := textCTMLinearMatrix(object.CTM); m != nil {
		ctx.Transform(*m)
	}
	ctx.Rotate(-textCharDirectionDegrees(object))
	ctx.Scale(hScale, 1)
	p.drawTextInline(ctx, face, value)
	ctx.Pop()
}

// drawTextInline 优先使用后端原生文字绘制（语义与后端 DrawText 一致，
// 保留文字整形和 PDF 文字可复制性）。后端不支持时回退到字形轮廓路径。
func (p *Document) drawTextInline(ctx DrawContext, face FontFace, value string) {
	if td, ok := ctx.(textDrawer); ok {
		if run := face.ShapedRun(value); run != nil {
			td.DrawTextLine(run)
		}
		return
	}
	if path := face.ToPath(value); path != nil && !path.Empty() {
		// 路径绘制使用当前填充样式：原生 DrawText 使用字体面内嵌的
		// 画笔且只填充、绝不描边（空心字由 drawTextPath 单独处理），
		// 这里同样显式套用画笔并清除残存的描边状态（例如上一条路径
		// 图元留下的描边），否则字形会被描边成更粗更黑的笔画。
		ctx.ClearStroke()
		ctx.SetFillPaint(face.Fill())
		ctx.DrawPath(0, 0, path)
	}
}

// drawCFFTextPath 避免将修复后的裸 CFF 字体交给 PDF 字体子集器，
// 因为子集器无法安全地序列化某些嵌入式 CFF 程序。
func (p *Document) drawCFFTextPath(ctx DrawContext, faces *textFaces, object models.TextObject, value string, x, y, pageHeight float64, parentCTM *models.CTM, hScale float64) {
	face := faces.run(x, y)
	path := face.DirectPath(value)
	if path == nil {
		path = face.ToPath(value)
	}
	if path == nil || path.Empty() {
		return
	}
	p.drawTextPath(ctx, faces, path, object, x, y, pageHeight, parentCTM, hScale)
}

func containsPrivateGlyphRune(value string) bool {
	for _, r := range value {
		if r >= 0xF0000 && r <= 0xFFFFD {
			return true
		}
	}
	return false
}

func (p *Document) drawTextPath(ctx DrawContext, faces *textFaces, path *geom.Path, object models.TextObject, x, y, pageHeight float64, parentCTM *models.CTM, hScale float64) {
	// runX/runY 是 run 起点在图元 Boundary 内的坐标，渐变必须以它为基准还原
	// 取样空间；后续分支会改写 x、y，因此先单独保留。
	runX, runY := x, y
	face := faces.run(runX, runY)
	if path == nil || !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(pageHeight) || !finiteFloat(hScale) ||
		!object.Boundary.IsFinite() || !object.CTM.IsFinite() || !parentCTM.IsFinite() {
		return
	}
	if parentCTM != nil {
		// 该分支用上下文变换绘制路径，后端在 run 排版空间中取样渐变，
		// 与原生文字分支一致。
		p.applyTextPathFill(ctx, faces.runPaint(runX, runY, face.Fill()), object)
		if object.CTM != nil {
			x, y = parentCTM.Multiply(object.CTM).Transform(x, y)
		} else {
			x, y = parentCTM.Transform(x, y)
		}
		if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(x+object.Boundary.X) || !finiteFloat(pageHeight-(y+object.Boundary.Y)) {
			return
		}
		ctx.Push()
		ctx.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
		if m := textCTMLinearMatrix(object.CTM); m != nil {
			ctx.Transform(*m)
		}
		ctx.Scale(hScale, 1)
		ctx.DrawPath(0, 0, path)
		ctx.Pop()
		return
	}
	if object.CTM != nil {
		x, y = object.CTM.Transform(x, y)
	}
	if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(x+object.Boundary.X) || !finiteFloat(pageHeight-(y+object.Boundary.Y)) {
		return
	}
	matrix := geom.Identity.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
	// 与原生文字、网格文字分支保持一致：CTM 的线性部分整体参与变换，
	// 只调用 Rotate 会把倾斜和非等比缩放静默丢弃。
	if m := textCTMLinearMatrix(object.CTM); m != nil {
		matrix = matrix.Mul(*m)
	}
	if direction := textCharDirectionDegrees(object); direction != 0 {
		matrix = matrix.Rotate(-direction)
	}
	matrix = matrix.Scale(hScale, 1)
	if !finiteMatrix(matrix) {
		return
	}
	// 排版矩阵已烘焙进轮廓，后端按设备坐标取样渐变，因此画笔要用矩阵逆
	// 映回 Boundary 空间；矩阵退化时退回 run 空间画笔。
	paint, ok := faces.devicePaint(matrix, faces.runPaint(runX, runY, face.Fill()))
	if !ok {
		paint = faces.runPaint(runX, runY, face.Fill())
	}
	p.applyTextPathFill(ctx, paint, object)
	// path 可能来自字体轮廓缓存（同一字形/字体跨对象、跨页面复用），而
	// Path.Transform 是原地修改。必须先复制，否则平移会累积到缓存对象上，
	// 后续渲染同一字形时坐标持续偏移，最终移出页面。
	ctx.DrawPath(0, 0, path.Copy().Transform(matrix))
}

// applyTextPathFill 设置走路径文字的填充与描边状态。
func (p *Document) applyTextPathFill(ctx DrawContext, paint geom.Paint, object models.TextObject) {
	// ToPath 只返回几何路径；与 DrawText 不同，它不会应用 FontFace 的画笔，
	// 因此需要将文字填充样式复制到路径绘制状态。
	if textFillDisabled(object) && object.Stroke {
		// 空心字：仅描边，不填充。
		ctx.ClearFill()
		return
	}
	ctx.SetFillPaint(paint)
	if !object.Stroke {
		// 只有不描边的文字才清除描边。textWithBudget 已按 StrokeColor
		// 设置好描边画笔与线宽，若在此清除，"填充+描边" 文字的描边会被
		// 静默丢弃（text-directions.ofd 第 3 页 ID=51 即是此情况）。
		ctx.ClearStroke()
	}
}

func finiteArray(values models.StArrayF) bool {
	for _, value := range values {
		if !finiteFloat(value) {
			return false
		}
	}
	return true
}

func valueAt(values models.StArrayF, index int) float64 {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return 0
}

// textCTMLinearMatrix 返回图元 CTM 的线性部分（不含平移），供字形变换使用。
// CTM 只变换文字原点时，倾斜与非等比缩放不会作用到字形轮廓上；字形必须
// 显式乘上同一个线性部分。CTM 为 nil 或退化为单位变换时返回 nil。
//
// OFD 的 CTM 定义在对象空间（Y 轴向下），而字形轮廓是 font 的 Y 轴向上坐标，
// 因此要把线性部分对 Y 翻转做一次共轭（F·L·F），即取反对角项的相反数，否则
// 倾斜和旋转方向会被上下镜像：CTM="1 0 0.3 1 0 0" 会表现为上端偏右（应为上端
// 偏左），旋转正角也会反向。
func textCTMLinearMatrix(ctm *models.CTM) *geom.Matrix {
	if ctm == nil {
		return nil
	}
	a, b, c, d := (*ctm)[0], (*ctm)[1], (*ctm)[2], (*ctm)[3]
	if !finiteFloat(a) || !finiteFloat(b) || !finiteFloat(c) || !finiteFloat(d) {
		return nil
	}
	if a == 1 && b == 0 && c == 0 && d == 1 {
		return nil
	}
	matrix := geom.Matrix{{a, -c, 0}, {-b, d, 0}}
	if !finiteMatrix(matrix) {
		return nil
	}
	return &matrix
}
