package canvas

import (
	"github.com/srwiley/scanx"
)

// ToScanxScanner rasterizes the path to *scanx.Scanner using the given rasterizer, height, and resolution.
func (p *Path) ToScanxScanner(ras *scanx.Scanner, dy float64, resolution Resolution) {
	dpmm := resolution.DPMM()
	tolerance := PixelTolerance / dpmm
	var q *Path
	for i := 0; i < len(p.d); {
		cmd := p.d[i]
		switch cmd {
		case MoveToCmd:
			ras.Start(fixedPoint26_6(p.d[i+1]*dpmm, dy-p.d[i+2]*dpmm))
		case LineToCmd:
			ras.Line(fixedPoint26_6(p.d[i+1]*dpmm, dy-p.d[i+2]*dpmm))
		case QuadToCmd, CubeToCmd, ArcToCmd:
			if q == nil {
				q = &Path{d: make([]float64, 0, 32)}
			}
			q.d = q.d[:0]
			var start Point
			if 0 < i {
				start = Point{p.d[i-3], p.d[i-2]}
			}
			if cmd == QuadToCmd {
				flattenQuadraticBezierTo(q, start, Point{p.d[i+1], p.d[i+2]}, Point{p.d[i+3], p.d[i+4]}, tolerance)
			} else if cmd == CubeToCmd {
				flattenCubicBezierTo(q, start, Point{p.d[i+1], p.d[i+2]}, Point{p.d[i+3], p.d[i+4]}, Point{p.d[i+5], p.d[i+6]}, tolerance)
			} else {
				large, sweep := toArcFlags(p.d[i+4])
				q = flattenEllipticArc(start, p.d[i+1], p.d[i+2], p.d[i+3], large, sweep, Point{p.d[i+5], p.d[i+6]}, tolerance)
			}
			for j := 4; j < len(q.d); j += 4 {
				ras.Line(fixedPoint26_6(q.d[j+1]*dpmm, dy-q.d[j+2]*dpmm))
			}
		case CloseCmd:
			ras.Line(fixedPoint26_6(p.d[i+1]*dpmm, dy-p.d[i+2]*dpmm))
		default:
			panic("quadratic and cubic Béziers and arcs should have been replaced")
		}
		i += cmdLen(cmd)
	}
}
