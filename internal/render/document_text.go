package render

import (
	"image"
	"strings"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"

	"github.com/zc310/ofd/internal/models"
)

func (p *Document) Text(ctx *canvas.Context, object models.TextObject, dp *models.DrawParam, pb models.StBox) {
	p.text(ctx, object, dp, pb, nil, nil)
}

func (p *Document) text(ctx *canvas.Context, object models.TextObject, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *canvas.Path) {
	if !object.VisibleValue() {
		return
	}
	ctx.Push()
	defer ctx.Pop()

	fontFamily, err := p.fonts.LoadFont(object.Font)
	if err != nil {
		return
	}

	// OFD 中 Fill=false 表示文字不填充，本实现不绘制文字描边，直接跳过。
	if textFillDisabled(object) {
		return
	}
	fill, stroke := p.updateDrawParams(ctx, dp)
	if object.CTM != nil {
		if scale := object.CTM.YScale(); scale > 0 {
			object.Size *= scale
		}
	}
	if object.FillColor != nil {
		fill = p.updateCtColor(object.FillColor)
	}
	if object.StrokeColor != nil {
		stroke = p.updateCtColor(object.StrokeColor)
	}
	face := buildTextFace(fontFamily, object, fill)
	if !face.Fill.Has() {
		// 颜色透明（Alpha=0）时文字不可见，且 PDF 渲染器会因此输出非法的 NaN 颜色值
		// 破坏内容流，直接跳过绘制。
		return
	}
	if source := textFillColor(object, dp); isMeshColor(source) {
		if p.drawMeshText(ctx, face, source, object, pb) {
			return
		}
	}
	if stroke != nil && stroke.Gradient != nil {
		ctx.SetStrokeGradient(stroke.Gradient)
	} else if stroke != nil && stroke.Value != nil {
		ctx.SetStrokeColor(*stroke.Value)
	} else {
		ctx.SetStrokeColor(canvas.Black)
	}
	for _, code := range object.TextCode {
		p.drawTextCode(ctx, face, object, code, pb.Height, parentCTM)
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
func (p *Document) drawMeshText(ctx *canvas.Context, face *canvas.FontFace, source *models.CTColor, object models.TextObject, pb models.StBox) bool {
	if pb.Width <= 0 || pb.Height <= 0 {
		return false
	}
	transform := func(point models.StPos) canvas.Point {
		if object.CTM != nil {
			point.X, point.Y = object.CTM.TransformPoint(point)
		}
		return canvas.Point{X: point.X + object.Boundary.X, Y: pb.Height - (point.Y + object.Boundary.Y)}
	}
	gradient := p.pathGradient(source, transform)
	if gradient == nil {
		return false
	}
	page := canvas.New(pb.Width, pb.Height)
	pageCtx := canvas.NewContext(page)
	pageCtx.SetFillGradient(gradient)
	for _, code := range object.TextCode {
		p.drawMeshTextCode(pageCtx, face, object, code, pb.Height)
	}
	var textImage image.Image = rasterizer.Draw(page, canvas.DPI(meshGradientDPI), canvas.DefaultColorSpace)
	if textImage == nil || textImage.Bounds().Empty() {
		return false
	}
	if object.Alpha != nil {
		textImage = applyImageAlpha(textImage, *object.Alpha)
	}
	matrix := imageMatrix(models.StBox{Width: pb.Width, Height: pb.Height}, textImage,
		models.CTM{pb.Width, 0, 0, pb.Height, 0, 0}, pb.Height)
	ctx.RenderImage(textImage, ctx.CoordSystemView().Mul(ctx.View()).Mul(matrix))
	return true
}

func (p *Document) drawMeshTextCode(ctx *canvas.Context, face *canvas.FontFace, object models.TextObject, code models.TextCode, pageHeight float64) {
	if len(code.DeltaX) == 0 && len(code.DeltaY) == 0 {
		p.drawMeshTextGlyph(ctx, face, object, code.Value, code.X, code.Y, pageHeight)
		return
	}

	posX, posY := code.X, code.Y
	runes := []rune(code.Value)
	for i, r := range runes {
		if i > 0 {
			if len(code.DeltaX) > 0 {
				posX += valueAt(code.DeltaX, i-1)
			} else {
				posX += face.TextWidth(string(runes[i-1])) * textHScale(object)
			}
			posY += valueAt(code.DeltaY, i-1)
		}
		p.drawMeshTextGlyph(ctx, face, object, string(r), posX, posY, pageHeight)
	}
}

func (p *Document) drawMeshTextGlyph(ctx *canvas.Context, face *canvas.FontFace, object models.TextObject, value string, x, y, pageHeight float64) {
	path, _ := face.ToPath(value)
	if path == nil || path.Empty() {
		return
	}
	if object.CTM != nil {
		x, y = object.CTM.Transform(x, y)
	}
	matrix := canvas.Identity.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
	if object.CTM != nil && object.CTM.RotationAngle() != 0 {
		matrix = matrix.Rotate(-object.CTM.RotationAngleDegrees())
	}
	matrix = matrix.Scale(textHScale(object), 1)
	ctx.DrawPath(0, 0, path.Transform(matrix))
}

// buildTextFace 根据文字对象样式创建字体面。
func buildTextFace(family *canvas.FontFamily, object models.TextObject, fill *CTColor) *canvas.FontFace {
	args := make([]any, 0, 3)
	switch {
	case fill != nil && fill.Gradient != nil:
		args = append(args, fill.Gradient)
	case fill != nil && fill.Value != nil:
		args = append(args, *fill.Value)
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
	return strings.EqualFold(strings.TrimSpace(object.Fill), "false")
}

// textHScale 返回文字水平方向缩放比例，缺省值为 1。
func textHScale(object models.TextObject) float64 {
	if object.HScale > 0 {
		return object.HScale
	}
	return 1
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
func (p *Document) drawTextCode(ctx *canvas.Context, face *canvas.FontFace, object models.TextObject, code models.TextCode, pageHeight float64, parentCTM *models.CTM) {
	if len(code.DeltaX) == 0 && len(code.DeltaY) == 0 {
		p.drawTextGlyph(ctx, face, object, code.Value, code.X, code.Y, pageHeight, parentCTM)
		return
	}

	posX, posY := code.X, code.Y
	runes := []rune(code.Value)
	for i, r := range runes {
		if i > 0 {
			if len(code.DeltaX) > 0 {
				posX += valueAt(code.DeltaX, i-1)
			} else {
				posX += face.TextWidth(string(runes[i-1])) * textHScale(object)
			}
			posY += valueAt(code.DeltaY, i-1)
		}
		p.drawTextGlyph(ctx, face, object, string(r), posX, posY, pageHeight, parentCTM)
	}
}

func (p *Document) drawTextGlyph(ctx *canvas.Context, face *canvas.FontFace, object models.TextObject, value string, x, y, pageHeight float64, parentCTM *models.CTM) {
	hScale := textHScale(object)
	if parentCTM != nil {
		if object.CTM != nil {
			x, y = parentCTM.Multiply(object.CTM).Transform(x, y)
		} else {
			x, y = parentCTM.Transform(x, y)
		}
		// 对于 CellContent，Boundary 定义在父级坐标系中。
		ctx.Push()
		ctx.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
		ctx.Scale(hScale, 1)
		ctx.DrawText(0, 0, canvas.NewTextLine(face, value, canvas.Left))
		ctx.Pop()
		return
	}
	if object.CTM != nil && object.CTM.RotationAngle() != 0 {
		tx, ty := object.CTM.Transform(x, y)
		ctx.Push()
		ctx.Translate(tx+object.Boundary.X, pageHeight-(ty+object.Boundary.Y))
		ctx.Rotate(-object.CTM.RotationAngleDegrees())
		ctx.Scale(hScale, 1)
		ctx.DrawText(0, 0, canvas.NewTextLine(face, value, canvas.Left))
		ctx.Pop()
		return
	}
	if object.CTM != nil {
		x, y = object.CTM.Transform(x, y)
	}
	ctx.Push()
	ctx.Translate(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y))
	ctx.Scale(hScale, 1)
	ctx.DrawText(0, 0, canvas.NewTextLine(face, value, canvas.Left))
	ctx.Pop()
}

func valueAt(values models.StArrayF, index int) float64 {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return 0
}
