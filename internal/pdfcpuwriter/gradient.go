package pdfcpuwriter

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// ISO 32000-1 sections 7.10.2/7.10.4 and 8.7.3.3/8.7.4.5/8.7.4.6:
// functions depend only on stops; shading Patterns also include the exact
// geometry, Extend flags and independent paint-to-default-user-space matrix.
type prototypeGradient struct {
	kind   int
	coords []float64
	extend [2]bool
	stops  canvas.Grad
}

func prototypeGradientFor(paint canvas.Paint) (prototypeGradient, error) {
	var result prototypeGradient
	switch gradient := paint.Gradient.(type) {
	case *canvas.LinearGradient:
		result = prototypeGradient{2, []float64{gradient.Start.X, gradient.Start.Y, gradient.End.X, gradient.End.Y}, gradient.Extend, gradient.Grad}
	case *canvas.RadialGradient:
		result = prototypeGradient{3, []float64{gradient.C0.X, gradient.C0.Y, gradient.R0, gradient.C1.X, gradient.C1.Y, gradient.R1}, gradient.Extend, gradient.Grad}
	default:
		return result, fmt.Errorf("%w：自定义渐变", ErrUnsupported)
	}
	for _, coord := range result.coords {
		if math.IsNaN(coord) || math.IsInf(coord, 0) {
			return result, fmt.Errorf("%w：非有限渐变坐标", ErrUnsupported)
		}
	}
	if result.kind == 3 && (result.coords[2] < 0 || result.coords[5] < 0) {
		return result, fmt.Errorf("%w：负渐变半径", ErrUnsupported)
	}
	if len(result.stops) == 0 {
		return result, fmt.Errorf("%w：空渐变色标", ErrUnsupported)
	}
	for i, stop := range result.stops {
		if math.IsNaN(stop.Offset) || stop.Offset < 0 || stop.Offset > 1 || (i > 0 && stop.Offset <= result.stops[i-1].Offset) {
			return result, fmt.Errorf("%w：非递增或越界渐变色标", ErrUnsupported)
		}
		if stop.Color.A != 255 {
			return result, fmt.Errorf("%w：透明渐变色标", ErrUnsupported)
		}
	}
	result.stops = slices.Clone(result.stops)
	if result.stops[0].Offset != 0 {
		result.stops = append(canvas.Grad{{Offset: 0, Color: result.stops[0].Color}}, result.stops...)
	}
	last := result.stops[len(result.stops)-1]
	if last.Offset != 1 {
		result.stops = append(result.stops, canvas.Stop{Offset: 1, Color: last.Color})
	}
	return result, nil
}

func prototypeStopKey(stops canvas.Grad) string {
	var key []byte
	for _, stop := range stops {
		key = binary.BigEndian.AppendUint64(key, math.Float64bits(stop.Offset))
		key = append(key, stop.Color.R, stop.Color.G, stop.Color.B, stop.Color.A)
	}
	return string(key)
}

func (r *Document) gradientFunction(stops canvas.Grad) types.IndirectRef {
	key := prototypeStopKey(stops)
	if ref, ok := r.functionCache[key]; ok {
		return ref
	}
	dict := types.Dict{"Domain": types.Array{types.Integer(0), types.Integer(1)}}
	if len(stops) == 2 {
		dict["FunctionType"] = types.Integer(2)
		dict["N"] = types.Integer(1)
		for i, name := range []string{"C0", "C1"} {
			c := stops[i].Color
			dict[name] = types.Array{types.Float(float64(c.R) / 255), types.Float(float64(c.G) / 255), types.Float(float64(c.B) / 255)}
		}
	} else {
		functions, bounds, encode := types.Array{}, types.Array{}, types.Array{}
		for i := 1; i < len(stops); i++ {
			functions = append(functions, r.gradientFunction(canvas.Grad{{Offset: 0, Color: stops[i-1].Color}, {Offset: 1, Color: stops[i].Color}}))
			encode = append(encode, types.Integer(0), types.Integer(1))
			if i < len(stops)-1 {
				bounds = append(bounds, types.Float(stops[i].Offset))
			}
		}
		dict["FunctionType"], dict["Functions"], dict["Bounds"], dict["Encode"] = types.Integer(3), functions, bounds, encode
	}
	ref := r.object(dict)
	r.functionCache[key] = ref
	return ref
}

func (r *Document) gradientPaint(paint canvas.Paint, stroke bool, matrix canvas.Matrix) {
	gradient, err := prototypeGradientFor(paint)
	if err != nil {
		r.err = err
		return
	}
	// Pattern matrices use PDF default user space (points), not the current CTM.
	matrix = canvas.Identity.Scale(prototypePointsPerMM, prototypePointsPerMM).Mul(matrix)
	values := make([]float64, 0, 1+len(gradient.coords)+6)
	values = append(values, float64(gradient.kind))
	values = append(values, gradient.coords...)
	values = append(values, matrix[0][0], matrix[1][0], matrix[0][1], matrix[1][1], matrix[0][2], matrix[1][2])
	var key bytes.Buffer
	key.WriteString(prototypeStopKey(gradient.stops))
	for _, value := range values {
		var bits [8]byte
		binary.BigEndian.PutUint64(bits[:], math.Float64bits(value))
		key.Write(bits[:])
	}
	fmt.Fprint(&key, gradient.extend)
	name, ok := r.patternCache[key.String()]
	if !ok {
		coords := make(types.Array, 0, len(gradient.coords))
		for _, coord := range gradient.coords {
			coords = append(coords, types.Float(coord))
		}
		shading := r.object(types.Dict{
			"ShadingType": types.Integer(gradient.kind), "ColorSpace": types.Name("DeviceRGB"), "Coords": coords,
			"Function": r.gradientFunction(gradient.stops), "Extend": types.Array{types.Boolean(gradient.extend[0]), types.Boolean(gradient.extend[1])},
		})
		name = fmt.Sprintf("P%d", len(r.patterns))
		r.patterns[name] = r.object(types.Dict{
			"Type": types.Name("Pattern"), "PatternType": types.Integer(2), "Shading": shading,
			"Matrix": types.Array{types.Float(matrix[0][0]), types.Float(matrix[1][0]), types.Float(matrix[0][1]), types.Float(matrix[1][1]), types.Float(matrix[0][2]), types.Float(matrix[1][2])},
		})
		r.patternCache[key.String()] = name
	}
	r.opacity(255)
	if stroke {
		fmt.Fprintf(&r.content, "/Pattern CS /%s SCN\n", name)
	} else {
		fmt.Fprintf(&r.content, "/Pattern cs /%s scn\n", name)
	}
}

// RenderTextWithPaint 使用独立画笔坐标绘制渐变文字。
// 不支持的画笔返回 false，供调用方执行现有回退；其他绘制失败通过 Err 返回。
func (r *Document) RenderTextWithPaint(text *canvas.Text, matrix canvas.Matrix, clip *canvas.Path, paint canvas.Paint, paintMatrix canvas.Matrix) bool {
	if !r.ready() {
		return true
	}
	if _, err := prototypeGradientFor(paint); err != nil {
		return false
	}
	draw := func() { r.renderText(text, matrix, &paint, paintMatrix) }
	if clip == nil {
		draw()
	} else {
		r.clip(matrix, clip, draw)
	}
	return true
}
