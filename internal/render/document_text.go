package render

import (
	"github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/models"
)

func (p *Document) Text(ctx *canvas.Context, object models.TextObject, dp *models.DrawParam, pb models.StBox) {
	ctx.Push()
	defer ctx.Pop()

	fontFamily, err := p.fonts.LoadFont(object.Font)
	if err != nil {
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
	if stroke != nil && stroke.Value != nil {
		ctx.SetStrokeColor(*stroke.Value)
	} else {
		ctx.SetStrokeColor(canvas.Black)
	}
	for _, code := range object.TextCode {
		p.drawTextCode(ctx, face, object, code, pb.Height)
	}
}

// buildTextFace 根据文字对象样式创建字体面。
func buildTextFace(family *canvas.FontFamily, object models.TextObject, fill *CTColor) *canvas.FontFace {
	style := canvas.FontRegular
	if object.Weight == 0 || object.Weight >= 700 {
		if object.Weight == 0 {
			object.Weight = 400
		}
		if object.Weight >= 700 {
			style |= canvas.FontBold
		}
	}
	if object.Italic {
		style |= canvas.FontItalic
	}
	args := make([]interface{}, 0, 3)
	if fill != nil {
		if fill.Value != nil {
			args = append(args, *fill.Value)
		}
		if fill.AxialShd != nil {
			args = append(args, fill.AxialShd)
		}
	}
	args = append(args, style, canvas.FontNormal)
	return family.Face(object.Size*2.83465, args...)
}

// drawTextCode 按字符间距绘制一段文字。
func (p *Document) drawTextCode(ctx *canvas.Context, face *canvas.FontFace, object models.TextObject, code models.TextCode, pageHeight float64) {
	posX, posY := code.X, code.Y
	for i, r := range []rune(code.Value) {
		if i > 0 {
			posX += valueAt(code.DeltaX, i-1)
			posY += valueAt(code.DeltaY, i-1)
		}
		p.drawTextGlyph(ctx, face, object, string(r), posX, posY, pageHeight)
	}
}

func (p *Document) drawTextGlyph(ctx *canvas.Context, face *canvas.FontFace, object models.TextObject, value string, x, y, pageHeight float64) {
	if object.CTM != nil && object.CTM.RotationAngle() != 0 {
		tx, ty := object.CTM.Transform(x, y)
		ctx.Push()
		ctx.Translate(tx+object.Boundary.X, pageHeight-(ty+object.Boundary.Y))
		ctx.Rotate(-object.CTM.RotationAngleDegrees())
		ctx.DrawText(0, 0, canvas.NewTextLine(face, value, canvas.Left))
		ctx.Pop()
		return
	}
	if object.CTM != nil {
		x, y = object.CTM.Transform(x, y)
	}
	ctx.DrawText(x+object.Boundary.X, pageHeight-(y+object.Boundary.Y), canvas.NewTextLine(face, value, canvas.Left))
}

func valueAt(values models.StArrayF, index int) float64 {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return 0
}
