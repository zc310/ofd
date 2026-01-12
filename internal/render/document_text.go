package render

import (
	"github.com/tdewolff/canvas"

	"github.com/zc310/ofd/internal/models"
)

func (p *Document) Text(ctx *canvas.Context, object models.TextObject, dp *models.DrawParam, pb models.StBox) {
	ctx.Push()
	defer ctx.Pop()

	var ft *canvas.FontFamily
	var err error
	ft, err = p.fonts.LoadFont(object.Font)
	if err != nil {
		return
	}

	if object.Weight == 0 {
		object.Weight = 400
	}
	if object.HScale == 0 {
		object.HScale = 1.0
	}
	fontStyle := canvas.FontRegular
	if object.Weight >= 700 {
		fontStyle |= canvas.FontBold
	}
	if object.Italic {
		fontStyle |= canvas.FontItalic
	}

	strokeColor := canvas.Black

	fill, stroke := p.updateDrawParams(ctx, dp)

	if object.CTM != nil {
		if scale := object.CTM.YScale(); scale > 0 {
			object.Size *= scale
		}
	}

	var argsFont []interface{}
	if object.FillColor != nil {
		fill = p.updateCtColor(object.FillColor)
	}
	if object.StrokeColor != nil {
		stroke = p.updateCtColor(object.StrokeColor)
	}
	if fill != nil {
		if fill.Value != nil {
			argsFont = append(argsFont, *fill.Value)
		}

		if fill.AxialShd != nil {
			argsFont = append(argsFont, fill.AxialShd)
		}
	}
	if stroke != nil && stroke.Value != nil {
		strokeColor = *stroke.Value
	}
	ctx.SetStrokeColor(strokeColor)

	argsFont = append(argsFont, fontStyle)
	argsFont = append(argsFont, canvas.FontNormal)
	face := ft.Face(object.Size*2.83465, argsFont...)

	bx, by := object.Boundary.X, object.Boundary.Y
	h := pb.Height

	for _, code := range object.TextCode {
		posX, posY := code.X, code.Y
		for i, r := range []rune(code.Value) {
			s := string(r)
			if i > 0 {
				if di := i - 1; di < len(code.DeltaX) {
					posX += code.DeltaX[di]
				}
				if di := i - 1; di < len(code.DeltaY) {
					posY += code.DeltaY[di]
				}
			}
			var cX, cY float64
			if object.CTM == nil {
				cX, cY = posX+bx, h-(posY+by)
				ctx.DrawText(cX, cY, canvas.NewTextLine(face, s, canvas.Left))
			} else {
				if object.CTM.RotationAngle() != 0 {
					ctx.Push()
					angle := object.CTM.RotationAngleDegrees()
					tx, ty := object.CTM.Transform(posX, posY)
					finalX, finalY := tx+bx, h-(ty+by)
					ctx.Translate(finalX, finalY)
					ctx.Rotate(-angle)
					ctx.DrawText(0, 0, canvas.NewTextLine(face, s, canvas.Left))
					ctx.Pop()
				} else {
					tx, ty := object.CTM.Transform(posX, posY)
					cX, cY = tx+bx, h-(ty+by)
					ctx.DrawText(cX, cY, canvas.NewTextLine(face, s, canvas.Left))
				}
			}

		}
	}
}
