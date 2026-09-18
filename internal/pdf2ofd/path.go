package pdf2ofd

import (
	"fmt"
	"math"
	"strings"

	"github.com/zc310/ofd/pkg/creator"
)

func (p *pdfInterpreter) moveText(x, y float64) {
	p.state.lineMatrix[4] += p.state.lineMatrix[0]*x + p.state.lineMatrix[2]*y
	p.state.lineMatrix[5] += p.state.lineMatrix[1]*x + p.state.lineMatrix[3]*y
	p.state.textMatrix = p.state.lineMatrix
}

func (p *pdfInterpreter) adjustText(x float64) {
	p.state.textMatrix[4] += p.state.textMatrix[0] * x
	p.state.textMatrix[5] += p.state.textMatrix[1] * x
}

func (p *pdfInterpreter) moveTo(x, y float64) {
	x, y = transformPDFPoint(x, y, p.state.ctm)
	p.path = append(p.path, pdfPathCommand{op: "M", values: []float64{x, y}})
	p.pointX, p.pointY, p.startX, p.startY = x, y, x, y
}

func (p *pdfInterpreter) lineTo(x, y float64) {
	x, y = transformPDFPoint(x, y, p.state.ctm)
	if len(p.path) == 0 {
		p.path = append(p.path, pdfPathCommand{op: "M", values: []float64{x, y}})
	} else {
		p.path = append(p.path, pdfPathCommand{op: "L", values: []float64{x, y}})
	}
	p.pointX, p.pointY = x, y
}

func (p *pdfInterpreter) curveTo(args []any) {
	values := make([]float64, 6)
	for i := range values {
		values[i] = anyFloat(args[i])
	}
	for i := 0; i < 6; i += 2 {
		values[i], values[i+1] = transformPDFPoint(values[i], values[i+1], p.state.ctm)
	}
	if len(p.path) == 0 {
		p.path = append(p.path, pdfPathCommand{op: "M", values: []float64{p.pointX, p.pointY}})
	}
	p.appendCurve(values[0], values[1], values[2], values[3], values[4], values[5])
}

func (p *pdfInterpreter) appendCurve(x1, y1, x2, y2, x3, y3 float64) {
	if len(p.path) == 0 {
		p.path = append(p.path, pdfPathCommand{op: "M", values: []float64{p.pointX, p.pointY}})
	}
	p.path = append(p.path, pdfPathCommand{op: "B", values: []float64{x1, y1, x2, y2, x3, y3}})
	p.pointX, p.pointY = x3, y3
}

func (p *pdfInterpreter) paintPath(stroke, fill bool, rule string) {
	if len(p.path) == 0 {
		return
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var data strings.Builder
	for _, command := range p.path {
		if command.op == "Z" {
			continue
		}
		for i := 0; i+1 < len(command.values); i += 2 {
			x, y := p.pagePoint(command.values[i], command.values[i+1])
			minX, minY = math.Min(minX, x), math.Min(minY, y)
			maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
		}
	}
	if math.IsInf(minX, 1) {
		p.path = nil
		return
	}
	for _, command := range p.path {
		if command.op == "Z" {
			data.WriteString(" C")
			continue
		}
		values := make([]float64, len(command.values))
		for i := 0; i+1 < len(command.values); i += 2 {
			values[i], values[i+1] = p.pagePoint(command.values[i], command.values[i+1])
			values[i] -= minX
			values[i+1] -= minY
		}
		if command.op == "M" {
			data.WriteString(fmt.Sprintf(" M %.4f %.4f", values[0], values[1]))
		} else if command.op == "L" {
			data.WriteString(fmt.Sprintf(" L %.4f %.4f", values[0], values[1]))
		} else {
			data.WriteString(fmt.Sprintf(" B %.4f %.4f %.4f %.4f %.4f %.4f", values[0], values[1], values[2], values[3], values[4], values[5]))
		}
	}
	width, height := maxX-minX, maxY-minY
	if width == 0 {
		width = 0.001
	}
	if height == 0 {
		height = 0.001
	}
	lineWidth := p.state.lineWidth * p.info.userUnit * pdfPointToMillimeter * pdfMatrixScale(p.state.ctm)
	path := creator.Path{X: minX, Y: minY, Width: width, Height: height, Data: strings.TrimSpace(data.String()), Stroke: stroke, StrokeSet: &stroke, Fill: fill, Rule: rule, LineWidth: lineWidth, StrokeColor: colorToCreator(p.state.stroke), FillColor: colorToCreator(p.state.fill)}
	if clips := p.buildClips(minX, minY, false, 0, 0); clips != nil {
		path.Clips = clips
	}
	p.page.Items = append(p.page.Items, path)
	p.path = nil
}

// buildClips 把当前图形状态中的裁剪区转换为 OFD Clips。
// objX、objY 是图元边界（与图元 X/Y 相同的毫米坐标）。isImage 为 true 时
// 图片对象默认带有 {Width,0,0,Height,0,0} 的 CTM，需要为裁剪面积设置逆缩放
// 的 CTM 来抵消，使裁剪数据统一使用毫米坐标。
func (p *pdfInterpreter) buildClips(objX, objY float64, isImage bool, imageWidth, imageHeight float64) *creator.Clips {
	if len(p.state.clips) == 0 || p.page == nil || p.page.Area == nil || p.page.Area.PhysicalBox == nil {
		return nil
	}
	items := make([]creator.Clip, 0, len(p.state.clips))
	for _, region := range p.state.clips {
		clipPath, ok := p.clipPathFor(region, objX, objY)
		if !ok {
			continue
		}
		area := creator.ClipArea{Path: clipPath}
		if isImage && imageWidth > 0 && imageHeight > 0 {
			area.CTM = &creator.CTM{1 / imageWidth, 0, 0, 1 / imageHeight, 0, 0}
		}
		items = append(items, creator.Clip{Areas: []creator.ClipArea{area}})
	}
	if len(items) == 0 {
		return nil
	}
	return &creator.Clips{Items: items}
}

// clipPathFor 把设备坐标的裁剪路径转换为相对图元边界的 OFD 裁剪路径。
// 阅读器计算裁剪时使用 页面 Y = 页高 - (局部 Y + 图元 Y)，而图元（路径/图片）
// 的页面范围是 [图元 Y, 图元 Y+高]（顶部为图元 Y）。因此局部 Y 取
// 页面顶部坐标减去图元 Y，路径和图片一致。图片额外用 CTM 抵消默认缩放。
func (p *pdfInterpreter) clipPathFor(region pdfClipRegion, objX, objY float64) (*creator.ClipPath, bool) {
	local := func(x, y float64) (float64, float64) {
		mx, my := p.pagePoint(x, y)
		return mx - objX, my - objY
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, command := range region.commands {
		if command.op == "Z" {
			continue
		}
		for i := 0; i+1 < len(command.values); i += 2 {
			x, y := local(command.values[i], command.values[i+1])
			minX, minY = math.Min(minX, x), math.Min(minY, y)
			maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
		}
	}
	if math.IsInf(minX, 1) {
		return nil, false
	}
	var data strings.Builder
	for _, command := range region.commands {
		if command.op == "Z" {
			data.WriteString(" C")
			continue
		}
		values := make([]float64, len(command.values))
		for i := 0; i+1 < len(command.values); i += 2 {
			x, y := local(command.values[i], command.values[i+1])
			values[i], values[i+1] = x-minX, y-minY
		}
		switch command.op {
		case "M":
			data.WriteString(fmt.Sprintf(" M %.4f %.4f", values[0], values[1]))
		case "L":
			data.WriteString(fmt.Sprintf(" L %.4f %.4f", values[0], values[1]))
		default:
			data.WriteString(fmt.Sprintf(" B %.4f %.4f %.4f %.4f %.4f %.4f", values[0], values[1], values[2], values[3], values[4], values[5]))
		}
	}
	width, height := maxX-minX, maxY-minY
	if width == 0 {
		width = 0.001
	}
	if height == 0 {
		height = 0.001
	}
	return &creator.ClipPath{
		Boundary: creator.Box{X: minX, Y: minY, Width: width, Height: height},
		Data:     strings.TrimSpace(data.String()),
	}, true
}

func (p *pdfInterpreter) pagePoint(x, y float64) (float64, float64) {
	x, y = x*p.info.userUnit, y*p.info.userUnit
	minX, minY, maxX, maxY := p.info.minX*p.info.userUnit, p.info.minY*p.info.userUnit, p.info.maxX*p.info.userUnit, p.info.maxY*p.info.userUnit
	switch p.info.rotate {
	case 90:
		return (y - minY) * pdfPointToMillimeter, (x - minX) * pdfPointToMillimeter
	case 180:
		return (maxX - x) * pdfPointToMillimeter, (y - minY) * pdfPointToMillimeter
	case 270:
		return (maxY - y) * pdfPointToMillimeter, (maxX - x) * pdfPointToMillimeter
	default:
		return (x - minX) * pdfPointToMillimeter, (maxY - y) * pdfPointToMillimeter
	}
}

func identityPDFMatrix() [6]float64 { return [6]float64{1, 0, 0, 1, 0, 0} }

func multiplyPDFMatrix(a, b [6]float64) [6]float64 {
	return [6]float64{a[0]*b[0] + a[2]*b[1], a[1]*b[0] + a[3]*b[1], a[0]*b[2] + a[2]*b[3], a[1]*b[2] + a[3]*b[3], a[0]*b[4] + a[2]*b[5] + a[4], a[1]*b[4] + a[3]*b[5] + a[5]}
}

func transformPDFPoint(x, y float64, matrix [6]float64) (float64, float64) {
	return matrix[0]*x + matrix[2]*y + matrix[4], matrix[1]*x + matrix[3]*y + matrix[5]
}

// pdfMatrixScale 返回 PDF 变换矩阵对长度的平均缩放系数，用于把线宽换算到
// 页面坐标系；忽略该系数会把在缩放 CTM 下绘制的细线画得过粗。
func pdfMatrixScale(matrix [6]float64) float64 {
	determinant := matrix[0]*matrix[3] - matrix[1]*matrix[2]
	return math.Sqrt(math.Abs(determinant))
}

func anyFloat(value any) float64 {
	if number, ok := value.(float64); ok {
		return number
	}
	if number, ok := value.(int); ok {
		return float64(number)
	}
	return 0
}

func anyName(value any) string {
	if name, ok := value.(pdfName); ok {
		return string(name)
	}
	return ""
}

func anyBytes(value any) []byte {
	if data, ok := value.(pdfString); ok {
		return []byte(data)
	}
	return nil
}

func rgbColor(r, g, b float64) pdfColor {
	return pdfColor{uint8(clamp01(r) * 255), uint8(clamp01(g) * 255), uint8(clamp01(b) * 255)}
}

func cmykColor(c, m, y, k float64) pdfColor {
	return pdfColor{uint8((1 - clamp01(c)) * (1 - clamp01(k)) * 255), uint8((1 - clamp01(m)) * (1 - clamp01(k)) * 255), uint8((1 - clamp01(y)) * (1 - clamp01(k)) * 255)}
}

func clamp01(value float64) float64 { return math.Max(0, math.Min(1, value)) }

func pdfColorFromComponents(args []any) (pdfColor, bool) {
	values := make([]float64, 0, len(args))
	for _, arg := range args {
		switch arg.(type) {
		case float64, int:
			values = append(values, anyFloat(arg))
		case pdfName:
			// 图案颜色空间的名称分量无法直接映射到 RGB，忽略。
		default:
			return pdfColor{}, false
		}
	}
	switch len(values) {
	case 1:
		return rgbColor(values[0], values[0], values[0]), true
	case 3:
		return rgbColor(values[0], values[1], values[2]), true
	case 4:
		return cmykColor(values[0], values[1], values[2], values[3]), true
	default:
		return pdfColor{}, false
	}
}

func colorToCreator(value pdfColor) *creator.Color {
	return &creator.Color{R: value.r, G: value.g, B: value.b}
}
