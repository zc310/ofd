package render

import (
	"image"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/tdewolff/font"

	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
)

func (p *Document) Text(ctx *canvas.Context, object models.TextObject, dp *models.DrawParam, pb models.StBox) {
	var budget renderBudget
	budget.reset()
	p.textWithBudget(NewCanvasBackend(ctx), object, dp, pb, nil, nil, &budget)
}

func (p *Document) textWithBudget(ctx DrawContext, object models.TextObject, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *canvas.Path, budget *renderBudget) {
	if !object.VisibleValue() || !object.CTM.IsFinite() || !parentCTM.IsFinite() ||
		!object.Boundary.IsFinite() || !pb.IsFinite() || !finiteFloat(pb.Height) || !finiteFloat(object.Size) {
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
	fontLock := p.fonts.renderLock(fontFamily)
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
	if object.CTM != nil {
		if scale := object.CTM.YScale(); scale > 0 {
			object.Size *= scale
		}
	}
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
			value := fill.Value
			value.A = uint8(uint16(value.A) * uint16(graphicOpacity(object.Alpha)) / 255)
			fill = &CTColor{Value: value, HasValue: true, Gradient: fill.Gradient}
		}
		if stroke != nil && stroke.HasValue {
			value := stroke.Value
			value.A = uint8(uint16(value.A) * uint16(graphicOpacity(object.Alpha)) / 255)
			stroke = &CTColor{Value: value, HasValue: true, Gradient: stroke.Gradient}
		}
	}
	face := buildTextFace(fontFamily, object, fill)
	if !face.Fill.Has() && !(textFillDisabled(object) && object.Stroke) {
		// 颜色透明（Alpha=0）时文字不可见，且 PDF 渲染器会因此输出非法的 NaN 颜色值
		// 破坏内容流，直接跳过绘制。
		return
	}
	if source := textFillColor(object, dp); isMeshColor(source) {
		if p.drawMeshText(ctx, face, source, object, pb, budget) {
			return
		}
	}
	if stroke != nil && stroke.Gradient != nil {
		ctx.SetStrokeGradient(stroke.Gradient)
	} else if stroke != nil && stroke.HasValue {
		ctx.SetStrokeColor(stroke.Value)
	} else {
		ctx.SetStrokeColor(canvas.Black)
	}
	codePosition := 0
	for _, code := range object.TextCode {
		if !finiteFloat(code.X) || !finiteFloat(code.Y) || !finiteArray(code.DeltaX) || !finiteArray(code.DeltaY) {
			continue
		}
		p.drawTextCode(ctx, face, object, code, pb.Height, parentCTM, codePosition)
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

// drawMeshText 将网格渐变文字先栅格化，避免 PDF/SVG 直接序列化不支持的自定义渐变。
func (p *Document) drawMeshText(ctx DrawContext, face *canvas.FontFace, source *models.CTColor, object models.TextObject, pb models.StBox, budget *renderBudget) bool {
	if pb.Width <= 0 || pb.Height <= 0 || !pb.IsFinite() || !object.Boundary.IsFinite() {
		return false
	}
	if !budget.allowOffscreenPixels(pb.Width, pb.Height, meshGradientDPI) {
		return false
	}
	transform := gradientBoundaryTransform(object.Boundary, pb.Height)
	gradient := p.pathGradient(source, transform)
	if gradient == nil {
		return false
	}
	page := canvas.New(pb.Width, pb.Height)
	pageCtx := canvas.NewContext(page)
	pageCtx.SetFillGradient(gradient)
	for _, code := range object.TextCode {
		if !finiteFloat(code.X) || !finiteFloat(code.Y) || !finiteArray(code.DeltaX) || !finiteArray(code.DeltaY) {
			continue
		}
		p.drawMeshTextCode(NewCanvasBackend(pageCtx), face, object, code, pb.Height)
	}
	if !finiteFloat(pb.Width) || !finiteFloat(pb.Height) {
		return false
	}
	var textImage image.Image = rasterizer.Draw(page, canvas.DPI(meshGradientDPI), canvas.DefaultColorSpace)
	if textImage == nil || textImage.Bounds().Empty() {
		return false
	}
	if object.Alpha != nil {
		textImage = applyImageAlpha(textImage, graphicOpacity(object.Alpha))
	}
	matrix := imageMatrix(models.StBox{Width: pb.Width, Height: pb.Height}, textImage,
		models.CTM{pb.Width, 0, 0, pb.Height, 0, 0}, pb.Height)
	matrix = ctx.CurrentMatrix().Mul(matrix)
	if !finiteMatrix(matrix) {
		return false
	}
	ctx.RenderImage(textImage, matrix)
	return true
}

func (p *Document) drawMeshTextCode(ctx DrawContext, face *canvas.FontFace, object models.TextObject, code models.TextCode, pageHeight float64) {
	if len(code.DeltaX) == 0 && len(code.DeltaY) == 0 && textDirectionsZero(object) {
		p.drawMeshTextGlyph(ctx, face, object, code.Value, code.X, code.Y, pageHeight)
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
		p.drawMeshTextGlyph(ctx, face, object, string(r), posX, posY, pageHeight)
	}
}

func (p *Document) drawMeshTextGlyph(ctx DrawContext, face *canvas.FontFace, object models.TextObject, value string, x, y, pageHeight float64) {
	if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(pageHeight) ||
		!object.Boundary.IsFinite() || !object.CTM.IsFinite() {
		return
	}
	path := p.fonts.directTextPath(face, value)
	if path == nil {
		path = p.fonts.shapedTextPath(face, value)
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
	matrix := canvas.Identity.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
	if object.CTM != nil && object.CTM.RotationAngle() != 0 {
		matrix = matrix.Rotate(-object.CTM.RotationAngleDegrees())
	}
	if direction := textCharDirectionDegrees(object); direction != 0 {
		matrix = matrix.Rotate(-direction)
	}
	matrix = matrix.Scale(textHScale(object), 1)
	if !finiteMatrix(matrix) {
		return
	}
	ctx.DrawPath(0, 0, path.Transform(matrix))
}

// buildTextFace 根据文字对象样式创建字体面。
func buildTextFace(family *canvas.FontFamily, object models.TextObject, fill *CTColor) *canvas.FontFace {
	args := make([]any, 0, 3)
	switch {
	case fill != nil && fill.Gradient != nil:
		args = append(args, fill.Gradient)
	case fill != nil && fill.HasValue:
		args = append(args, fill.Value)
	default:
		// OFD 未指定 FillColor 时，文字填充默认为黑色。
		// 不能使用透明色，否则 PDF 渲染器会输出非法的 NaN 颜色值破坏内容流。
		args = append(args, canvas.Black)
	}
	style := textFontStyle(object.Weight, object.Italic)
	args = append(args, style, canvas.FontNormal)
	return family.Face(object.Size*2.83465, args...)
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

// textFontStyle 将 OFD 字重映射为 canvas 支持的标准字重。
func textFontStyle(weight int, italic bool) canvas.FontStyle {
	if weight <= 0 {
		weight = 400
	}

	style := canvas.FontRegular
	switch {
	case weight < 150:
		style = canvas.FontThin
	case weight < 250:
		style = canvas.FontExtraLight
	case weight < 350:
		style = canvas.FontLight
	case weight < 450:
		style = canvas.FontRegular
	case weight < 550:
		style = canvas.FontMedium
	case weight < 650:
		style = canvas.FontSemiBold
	case weight < 750:
		style = canvas.FontBold
	case weight < 850:
		style = canvas.FontExtraBold
	default:
		style = canvas.FontBlack
	}
	if italic {
		style |= canvas.FontItalic
	}
	return style
}

// drawTextCode 按字符间距绘制一段文字。
type textGlyph struct {
	value string
}

func (p *Document) drawTextCode(ctx DrawContext, face *canvas.FontFace, object models.TextObject, code models.TextCode, pageHeight float64, parentCTM *models.CTM, codePosition int) {
	runes := []rune(code.Value)
	glyphs := textCodeGlyphs(face, runes, object.CGTransform, codePosition)
	if len(glyphs) == 0 {
		return
	}
	if len(object.CGTransform) == 0 && len(code.DeltaX) == 0 && len(code.DeltaY) == 0 && textDirectionsZero(object) {
		if !renderableTextValue(code.Value) {
			return
		}
		p.drawTextGlyph(ctx, face, object, code.Value, code.X, code.Y, pageHeight, parentCTM)
		return
	}

	posX, posY := code.X, code.Y
	for i, glyph := range glyphs {
		if i > 0 {
			// 有显式 DeltaX/DeltaY 时不需要按字体字宽步进，
			// 避免为每个字形额外做一次文字整形。
			glyphWidth := 0.0
			if len(code.DeltaX) == 0 && len(code.DeltaY) == 0 {
				glyphWidth = textGlyphWidth(face, glyphs[i-1])
			}
			deltaX, deltaY := textAdvance(glyphWidth, object, code, i-1)
			posX += deltaX
			posY += deltaY
		}
		// 控制字符没有可见字形，绘制时会被字体替换成 .notdef 方块；
		// 这里跳过绘制但仍按前面的增量推进，保持后续字形位置不变。
		if !renderableTextValue(glyph.value) {
			continue
		}
		p.drawTextGlyph(ctx, face, object, glyph.value, posX, posY, pageHeight, parentCTM)
	}
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

func textCodeGlyphs(face *canvas.FontFace, runes []rune, transforms []models.CTCGTransform, codePosition int) []textGlyph {
	if len(transforms) == 0 {
		glyphs := make([]textGlyph, len(runes))
		for i, r := range runes {
			glyphs[i] = textGlyph{value: string(r)}
		}
		return glyphs
	}
	byPosition := make(map[int]models.CTCGTransform, len(transforms))
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
		for _, id := range ids {
			if id >= 0 && id <= 0xffff {
				// 单码位对单字形且字体能按原 Unicode 成形时，优先使用原文本，
				// 让 PDF 等输出保留可复制、可搜索的文字；否则退回字形私有区。
				if len(ids) == 1 && transform.CodeCount == 1 && i < len(runes) &&
					renderableTextValue(string(runes[i])) && fontCanRenderRune(face, runes[i]) {
					glyphs = append(glyphs, textGlyph{value: string(runes[i])})
					continue
				}
				glyphs = append(glyphs, textGlyph{value: string(fontfix.GlyphRune(uint16(id)))})
			}
		}
		codeCount := transform.CodeCount
		if codeCount <= 0 {
			codeCount = 1
		}
		remaining := len(runes) - i
		if codeCount >= remaining {
			break
		}
		i += codeCount
	}
	return glyphs
}

// fontCanRenderRune 判断字体是否存在该 Unicode 码位的字形。缺少 Unicode cmap
// 的子集字体在注入文档映射前会返回 false，此时调用方回退到字形私有区绘制。
func fontCanRenderRune(face *canvas.FontFace, r rune) bool {
	if face == nil || face.Font == nil {
		return false
	}
	return face.Font.GlyphIndex(r) != 0
}

// fontCanRenderText 判断字体能否按给定文本成形：文本必须不含私有区字形引用，
// 且每个可见字符都有对应字形。只有这样才能安全地用正常文字接口绘制。
func fontCanRenderText(face *canvas.FontFace, value string) bool {
	if containsPrivateGlyphRune(value) {
		return false
	}
	rendered := false
	for _, r := range value {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '\uFFFD' {
			continue
		}
		if !fontCanRenderRune(face, r) {
			return false
		}
		rendered = true
	}
	return rendered
}

func textGlyphWidth(face *canvas.FontFace, glyph textGlyph) float64 {
	return face.TextWidth(glyph.value)
}

func (p *Document) drawTextGlyph(ctx DrawContext, face *canvas.FontFace, object models.TextObject, value string, x, y, pageHeight float64, parentCTM *models.CTM) {
	hScale := textHScale(object)
	if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(pageHeight) || !finiteFloat(hScale) ||
		!object.Boundary.IsFinite() || !object.CTM.IsFinite() || !parentCTM.IsFinite() {
		return
	}
	// 某些嵌入式 CFF 字体经过 OFD 子集修复后不能安全地交给 PDF
	// 子集器，因此对无法按调用文本成形的文字保留路径回退；已经注入文档
	// Unicode 映射、字体可以成形的文字走正常文字接口，以保留复制和搜索能力。
	isEmbeddedFont := p.fonts.FallbackFontFamily(object.Font) == ""
	if isEmbeddedFont && face.Font != nil && face.Font.SFNT != nil && face.Font.SFNT.IsCFF && !fontCanRenderText(face, value) {
		p.drawCFFTextPath(ctx, face, object, value, x, y, pageHeight, parentCTM, hScale)
		return
	}
	if path := p.fonts.directTextPath(face, value); path != nil {
		p.drawTextPath(ctx, face, path, object, x, y, pageHeight, parentCTM, hScale)
		return
	}
	// CharDirection 需要逐字旋转，Canvas 的文字排版接口不能对单个字形
	// 应用该变换，因此退回到路径绘制。
	if textCharDirectionDegrees(object) != 0 {
		if path, _ := face.ToPath(value); path != nil && !path.Empty() {
			p.drawTextPath(ctx, face, path, object, x, y, pageHeight, parentCTM, hScale)
			return
		}
	}
	// 空心字必须走路径绘制才能描边，DrawText 无法单独描边。
	if textFillDisabled(object) && object.Stroke {
		if path, _ := face.ToPath(value); path != nil && !path.Empty() {
			p.drawTextPath(ctx, face, path, object, x, y, pageHeight, parentCTM, hScale)
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
		ctx.Rotate(-textCharDirectionDegrees(object))
		ctx.Scale(hScale, 1)
		p.drawTextInline(ctx, face, value)
		ctx.Pop()
		return
	}
	if object.CTM != nil && object.CTM.RotationAngle() != 0 {
		tx, ty := object.CTM.Transform(x, y)
		if !finiteFloat(tx) || !finiteFloat(ty) || !finiteFloat(tx+object.Boundary.X) || !finiteFloat(pageHeight-(ty+object.Boundary.Y)) {
			return
		}
		ctx.Push()
		ctx.Translate(tx+object.Boundary.X, pageHeight-(ty+object.Boundary.Y))
		ctx.Rotate(-object.CTM.RotationAngleDegrees())
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
	ctx.Push()
	ctx.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
	ctx.Rotate(-textCharDirectionDegrees(object))
	ctx.Scale(hScale, 1)
	p.drawTextInline(ctx, face, value)
	ctx.Pop()
}

// drawTextInline 优先使用后端原生文字绘制（与 canvas DrawText 语义一致，
// 保留文字整形和 PDF 文字可复制性）。后端不支持时回退到字形轮廓路径。
func (p *Document) drawTextInline(ctx DrawContext, face *canvas.FontFace, value string) {
	if td, ok := ctx.(textDrawer); ok {
		if line := p.fonts.shapedTextLine(face, value); line != nil {
			td.DrawTextLine(line)
		}
		return
	}
	if path := p.fonts.shapedTextPath(face, value); path != nil && !path.Empty() {
		// 路径绘制使用当前填充样式：原生 DrawText 使用 FontFace 内嵌的
		// 画笔且只填充、绝不描边（空心字由 drawTextPath 单独处理），
		// 这里同样显式套用画笔并清除残存的描边状态（例如上一条路径
		// 图元留下的描边），否则字形会被描边成更粗更黑的笔画。
		ctx.ClearStroke()
		ctx.SetFillPaint(face.Fill)
		ctx.DrawPath(0, 0, path)
	}
}

// drawCFFTextPath 避免将修复后的裸 CFF 字体交给 PDF 字体子集器，
// 因为子集器无法安全地序列化某些嵌入式 CFF 程序。
func (p *Document) drawCFFTextPath(ctx DrawContext, face *canvas.FontFace, object models.TextObject, value string, x, y, pageHeight float64, parentCTM *models.CTM, hScale float64) {
	path := p.fonts.directTextPath(face, value)
	if path == nil {
		path = p.fonts.shapedTextPath(face, value)
	}
	if path == nil || path.Empty() {
		return
	}
	p.drawTextPath(ctx, face, path, object, x, y, pageHeight, parentCTM, hScale)
}

// directTextPath 处理 cmap 可通过 GlyphIndex 使用、但无法被文字整形器使用的
// 子集字体。普通文字仍会在后续使用 Canvas 的文字整形功能；这里对整形成功的
// 情形返回 nil（示意“无需按字形映射构建”），并利用缓存后的轮廓判断，避免
// 与后续 drawTextInline 重复计算同一字形轮廓。
func (p *Fonts) directTextPath(face *canvas.FontFace, value string) *canvas.Path {
	var path *canvas.Path
	if !containsPrivateGlyphRune(value) {
		path := p.shapedTextPath(face, value)
		if path != nil {
			return nil
		}
	}
	if face.Font == nil || face.Font.SFNT == nil {
		return nil
	}
	path = &canvas.Path{}
	advance := 0.0
	for _, r := range []rune(value) {
		glyphID := face.Font.GlyphIndex(r)
		if glyphID == 0 {
			return nil
		}
		if err := face.Font.GlyphPath(path, glyphID, face.PPEM(canvas.DefaultResolution), face.MmPerEm*advance, 0, face.MmPerEm, font.NoHinting); err != nil {
			return nil
		}
		advance += float64(face.Font.SFNT.GlyphAdvance(glyphID))
	}
	if path.Empty() {
		return nil
	}
	return path
}

func containsPrivateGlyphRune(value string) bool {
	for _, r := range value {
		if r >= 0xF0000 && r <= 0xFFFFD {
			return true
		}
	}
	return false
}

func (p *Document) drawTextPath(ctx DrawContext, face *canvas.FontFace, path *canvas.Path, object models.TextObject, x, y, pageHeight float64, parentCTM *models.CTM, hScale float64) {
	if path == nil || !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(pageHeight) || !finiteFloat(hScale) ||
		!object.Boundary.IsFinite() || !object.CTM.IsFinite() || !parentCTM.IsFinite() {
		return
	}
	// ToPath 只返回几何路径；与 DrawText 不同，它不会应用 FontFace 的画笔，
	// 因此需要将文字填充样式复制到路径绘制状态。
	if textFillDisabled(object) && object.Stroke {
		// 空心字：仅描边，不填充。
		ctx.ClearFill()
	} else {
		ctx.SetFillPaint(face.Fill)
		ctx.ClearStroke()
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
		ctx.Push()
		ctx.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
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
	matrix := canvas.Identity.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
	if object.CTM != nil && object.CTM.RotationAngle() != 0 {
		matrix = matrix.Rotate(-object.CTM.RotationAngleDegrees())
	}
	if direction := textCharDirectionDegrees(object); direction != 0 {
		matrix = matrix.Rotate(-direction)
	}
	matrix = matrix.Scale(hScale, 1)
	if !finiteMatrix(matrix) {
		return
	}
	ctx.DrawPath(0, 0, path.Transform(matrix))
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
