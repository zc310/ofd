package canvas

// IsAxisAlignedRect reports whether the path is exactly one closed, nondegenerate
// rectangle with edges parallel to the coordinate axes. Holes, extra subpaths,
// curves, and approximately aligned edges are not rectangles.
func (p *Path) IsAxisAlignedRect() bool {
	const (
		commandSize = 4
		verticesEnd = 4 * commandSize
	)
	if p == nil || len(p.d) != verticesEnd+commandSize || p.d[0] != MoveToCmd || p.d[verticesEnd] != CloseCmd {
		return false
	}

	for i := commandSize; i < verticesEnd; i += commandSize {
		if p.d[i] != LineToCmd {
			return false
		}
	}

	for i := 0; i < verticesEnd; i += commandSize {
		j := (i + commandSize) % verticesEnd
		k := (j + commandSize) % verticesEnd
		vertical := p.d[i+1] == p.d[j+1]
		horizontal := p.d[i+2] == p.d[j+2]

		if vertical == horizontal || vertical == (p.d[j+1] == p.d[k+1]) {
			return false
		}
	}

	return true
}
