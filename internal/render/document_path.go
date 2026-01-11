package render

import (
	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
)

func (p *Document) Path(ctx *canvas.Context, object models.PathObject, dp *models.DrawParam, pb models.StBox) {
	ctx.Push()
	defer ctx.Pop()
	box := object.Boundary

	height := pb.Height
	offsetX, offsetY := box.X, box.Y

	pa := p.newPath(&object.CtPath, func(pt models.StPos) (float64, float64) {
		if object.CTM == nil {
			return pt.X + offsetX, height - (pt.Y + offsetY)
		}
		tx, ty := object.CTM.TransformPoint(pt)
		return tx + offsetX, height - (ty + offsetY)
	})

	p.updateCtPathStyle(ctx, &object.CtPath, dp)
	ctx.DrawPath(0, 0, pa)

	if object.Clips == nil || len(object.Clips.Clip) == 0 {
		return
	}

	for _, clip := range object.Clips.Clip {
		var paR canvas.Paths
		for _, area := range clip.Area {
			if area.Path != nil {
				if area.DrawParam != nil {
					p.updateCtPathStyle(ctx, area.Path, p.Document.GetDrawParam(models.StID(*area.DrawParam)))
				} else {
					p.updateCtPathStyle(ctx, area.Path, nil)
				}

				paa := p.newPath(area.Path, func(pt models.StPos) (float64, float64) {
					if area.CTM == nil {
						return pt.X + offsetX, height - (pt.Y + offsetY)
					}
					tx, ty := area.CTM.TransformPoint(pt)
					return tx + offsetX, height - (ty + offsetY)
				})

				box = area.Path.Boundary

				cp := models.CtPath{}
				cp.AbbreviatedData.AddCommand(models.PathCommand{Type: models.MoveTo, Points: []models.StPos{{X: box.X, Y: box.Y}}})
				cp.AbbreviatedData.AddCommand(models.PathCommand{Type: models.LineTo, Points: []models.StPos{{X: box.X + box.Width, Y: box.Y}}})
				cp.AbbreviatedData.AddCommand(models.PathCommand{Type: models.LineTo, Points: []models.StPos{{X: box.X + box.Width, Y: box.Y + box.Height}}})
				cp.AbbreviatedData.AddCommand(models.PathCommand{Type: models.LineTo, Points: []models.StPos{{X: box.X, Y: box.Y + box.Height}}})
				cp.AbbreviatedData.AddCommand(models.PathCommand{Type: models.Close})

				p1 := p.newPath(&cp, func(pt models.StPos) (float64, float64) {
					if area.CTM == nil {
						return pt.X + offsetX, height - (pt.Y + offsetY)
					}
					tx, ty := area.CTM.TransformPoint(pt)
					return tx + offsetX, height - (ty + offsetY)
				})
				paR = append(paR, paa.And(p1))

			}

			if len(paR) > 0 {
				var p0 *canvas.Path
				for i, p2 := range paR {
					if i == 0 {
						p0 = p2
					} else {
						p0 = p0.And(p2)
					}
				}
				ctx.DrawPath(0, 0, p0)
			}
		}
	}
}

func (p *Document) newPath(cp *models.CtPath, transform func(pt models.StPos) (float64, float64)) *canvas.Path {
	pa := &canvas.Path{}
	for _, cmd := range cp.AbbreviatedData {
		switch cmd.Type {
		case models.MoveTo, "S":
			x, y := transform(cmd.Points[0])
			pa.MoveTo(x, y)

		case models.LineTo:
			x, y := transform(cmd.Points[0])
			pa.LineTo(x, y)

		case models.QuadTo:
			cpx, cpy := transform(cmd.Points[0])
			x, y := transform(cmd.Points[1])
			pa.QuadTo(cpx, cpy, x, y)

		case models.CubicBezier:
			x1, y1 := transform(cmd.Points[0])
			x2, y2 := transform(cmd.Points[1])
			x3, y3 := transform(cmd.Points[2])
			pa.CubeTo(x1, y1, x2, y2, x3, y3)

		case models.ArcTo:
			endX, endY := transform(cmd.Arc.EndPoint)
			pa.ArcTo(
				cmd.Arc.RX,
				cmd.Arc.RY,
				cmd.Arc.XAxisRotation,
				cmd.Arc.LargeArcFlag,
				cmd.Arc.SweepFlag,
				endX,
				endY,
			)

		case models.Close:
			pa.Close()
		}
	}
	return pa
}
