package pdf2ofd

import (
	"fmt"
	"math"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"

	"github.com/zc310/ofd/internal/coloricc"
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
	commands := p.path
	if fill {
		// PDF 填充会隐式闭合所有子路径；OFD 阅读器只填充显式闭合的路径。
		// 不闭合时，用 m/l.../f* 绘制的细长矩形不会渲染出来。
		commands = closeSubpathsForFill(commands)
	}
	commands = p.clampExtremePath(commands)
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var data strings.Builder
	for _, command := range commands {
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
	for _, command := range commands {
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
	// PDF 的 0 线宽表示设备能画出的最细线（1 像素）。OFD 线宽是毫米且与分辨率
	// 无关，无法表达“1 像素”，用一个细线宽近似，避免渲染成默认的 1pt 粗线。
	if p.state.lineWidth <= 0 {
		lineWidth = hairlineLineWidthMM
	}
	path := creator.Path{X: minX, Y: minY, Width: width, Height: height, Data: strings.TrimSpace(data.String()), Stroke: stroke, StrokeSet: &stroke, Fill: fill, Rule: rule, LineWidth: lineWidth, StrokeColor: colorToCreator(p.state.stroke), FillColor: colorToCreator(p.state.fill)}
	// OFD 的 DashPattern 以线宽为单位（渲染端按线宽缩放），而 PDF 的 d 是绝对
	// 长度，因此这里除以生效线宽换算成倍数。线宽为 0 时渲染端用默认线宽。
	if len(p.state.dashPattern) > 0 {
		scale := p.info.userUnit * pdfPointToMillimeter * pdfMatrixScale(p.state.ctm)
		strokeWidth := lineWidth
		if strokeWidth <= 0 {
			strokeWidth = defaultPathLineWidthMM
		}
		dashes := make([]float64, len(p.state.dashPattern))
		for index, value := range p.state.dashPattern {
			dashes[index] = value * scale / strokeWidth
		}
		path.DashPattern = dashes
		path.DashOffset = p.state.dashOffset * scale / strokeWidth
	}
	// Pattern 颜色空间下 scn/SCN 选中的图案以路径边界为局部原点，需在路径
	// 坐标确定后再转换，否则渐变会整体偏移。
	if fill && p.state.fillPaint != nil {
		if shading := p.state.fillPaint.shading; shading != nil {
			if color := p.shadingColor(shading, &path); color != nil {
				path.FillColor = color
			}
		} else if tiling := p.state.fillPaint.tiling; tiling != nil {
			// 平铺图案以图片对象形式展开，不再用纯色填充路径。
			if p.emitTilingFill(tiling, p.path, [4]float64{minX, minY, maxX, maxY}) {
				fill = false
				path.Fill = false
			}
		}
	}
	if stroke && p.state.strokePaint != nil {
		if shading := p.state.strokePaint.shading; shading != nil {
			if color := p.shadingColor(shading, &path); color != nil {
				path.StrokeColor = color
			}
		}
	}
	if clips := p.buildClips(minX, minY, false, 0, 0); clips != nil {
		path.Clips = clips
	}
	path.Alpha = ofdTransparency(p.pathOpacity(fill, stroke))
	// OFD 无混合模式：把 Multiply 纯色填充近似成半透明，使文字等高对比内容透出。
	if fill && !stroke {
		p.approximateMultiplyFill(&path)
	}
	if fill || stroke {
		p.page.Items = append(p.page.Items, path)
	}
	p.path = nil
}

// closeSubpathsForFill 在每个未显式闭合的子路径末尾补一个 Z，实现 PDF 填充
// 隐式闭合子路径的语义。
func closeSubpathsForFill(commands []pdfPathCommand) []pdfPathCommand {
	result := make([]pdfPathCommand, 0, len(commands)+2)
	pending := false
	for _, command := range commands {
		switch command.op {
		case "M":
			if pending {
				result = append(result, pdfPathCommand{op: "Z"})
			}
			pending = true
		case "Z":
			pending = false
		default:
			// 绘制命令只在子路径有实际轮廓时才需要闭合。
			pending = pending || len(command.values) > 0
		}
		result = append(result, command)
	}
	if pending {
		result = append(result, pdfPathCommand{op: "Z"})
	}
	return result
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
	return pdfPagePoint(p.info, x, y)
}

// pdfPagePoint 把 PDF 用户空间坐标转换为 OFD 页面坐标（毫米，原点在页面左上角），
// 并处理页面旋转。转换页面内容、目标位置和裁剪区时共用该映射。
func pdfPagePoint(info pdfPageInfo, x, y float64) (float64, float64) {
	x, y = x*info.userUnit, y*info.userUnit
	minX, minY, maxX, maxY := info.minX*info.userUnit, info.minY*info.userUnit, info.maxX*info.userUnit, info.maxY*info.userUnit
	switch info.rotate {
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

// maxReasonableCoordinate 是路径坐标的合理上限（PDF 点）。超过该值的坐标通常
// 是"覆盖整个平面"的技巧（如把矩形画到 ±1 亿），只用于构造补集填充。
const maxReasonableCoordinate = 1e6

// defaultPathLineWidthMM 是 OFD 未指定 LineWidth 时渲染端采用的线宽（1pt）。
// OFD 的 DashPattern 以线宽为单位，换算虚线倍数时需要它。
const defaultPathLineWidthMM = 0.353

// hairlineLineWidthMM 是 PDF 0 线宽的近似值。约 1/4pt，在 150dpi 下约 1 像素，
// 与大多数阅读器绘制 0 线宽的观感接近。
const hairlineLineWidthMM = 0.1

// clampExtremePath 把含极端坐标的路径收缩到页面附近。部分 PDF（例如注解外观流
// 中"先画巨型矩形再挖洞"的补集填充）使用远超页面范围的坐标，直接输出会让
// OFD 阅读器生成跨越数百万毫米的路径并可能导致填充丢失。只在坐标明显异常时
// 收缩，正常路径保持不变。
func (p *pdfInterpreter) clampExtremePath(commands []pdfPathCommand) []pdfPathCommand {
	extreme := false
	for _, command := range commands {
		for _, value := range command.values {
			if value > maxReasonableCoordinate || value < -maxReasonableCoordinate {
				extreme = true
				break
			}
		}
		if extreme {
			break
		}
	}
	if !extreme {
		return commands
	}
	padX := (p.info.maxX - p.info.minX) * 2
	padY := (p.info.maxY - p.info.minY) * 2
	minX, maxX := p.info.minX-padX, p.info.maxX+padX
	minY, maxY := p.info.minY-padY, p.info.maxY+padY
	clamped := make([]pdfPathCommand, len(commands))
	for i, command := range commands {
		values := make([]float64, len(command.values))
		for j := 0; j+1 < len(command.values); j += 2 {
			values[j] = math.Max(minX, math.Min(maxX, command.values[j]))
			values[j+1] = math.Max(minY, math.Min(maxY, command.values[j+1]))
		}
		clamped[i] = pdfPathCommand{op: command.op, values: values}
	}
	return clamped
}

// fillOpacity 返回填充的生效不透明度：局部 ca 乘以外层 Form 的组透明度。
func (p *pdfInterpreter) fillOpacity() float64 {
	return p.state.fillAlpha * p.groupOpacity()
}

// strokeOpacity 返回描边的生效不透明度：局部 CA 乘以外层 Form 的组透明度。
func (p *pdfInterpreter) strokeOpacity() float64 {
	return p.state.strokeAlpha * p.groupOpacity()
}

// groupOpacity 返回外层 Form XObject 累积的组透明度，未进入 Form 时为 1。
func (p *pdfInterpreter) groupOpacity() float64 {
	if p.state.groupAlpha <= 0 {
		return 1
	}
	return p.state.groupAlpha
}

// pathOpacity 返回当前路径生效的 PDF 不透明度：填充用 ca，描边用 CA，
// 同时填充与描边时取较小值。
func (p *pdfInterpreter) pathOpacity(fill, stroke bool) float64 {
	switch {
	case fill && stroke:
		return math.Min(p.fillOpacity(), p.strokeOpacity())
	case stroke:
		return p.strokeOpacity()
	default:
		return p.fillOpacity()
	}
}

// approximateMultiplyFill 把 Multiply 混合的纯色填充近似为半透明叠加。选择
// 不透明度 a = op·(255−minC)/255，并反推修正色 C′，使白色背景上仍渲染出原始
// 高亮色：Normal 结果 (1−a)·255 + a·C′ = (1−op)·255 + op·C。这样深色内容以
// a·C′ 透出（黑字可见），背景仍接近原色。OFD 没有混合模式，这是标准兼容近似。
func (p *pdfInterpreter) approximateMultiplyFill(path *creator.Path) {
	if !strings.EqualFold(p.state.blendMode, "Multiply") {
		return
	}
	color := path.FillColor
	if color == nil || color.Axial != nil || color.Radial != nil || color.Gouraud != nil || color.LaGouraud != nil || color.Pattern != nil {
		return
	}
	opacity := p.fillOpacity()
	if opacity <= 0 {
		return
	}
	minimum := int(color.R)
	if int(color.G) < minimum {
		minimum = int(color.G)
	}
	if int(color.B) < minimum {
		minimum = int(color.B)
	}
	if minimum >= 255 {
		return
	}
	a := opacity * float64(255-minimum) / 255
	if a < 0.05 {
		a = 0.05
	}
	scale := opacity / a
	channel := func(value uint8) uint8 {
		result := 255 - scale*float64(255-int(value))
		if result < 0 {
			return 0
		}
		if result > 255 {
			return 255
		}
		return uint8(math.Round(result))
	}
	path.FillColor = &creator.Color{
		R: channel(color.R), G: channel(color.G), B: channel(color.B),
		Components: color.Components, ColorSpace: color.ColorSpace, Index: color.Index, Alpha: color.Alpha,
	}
	path.Alpha = ofdTransparency(a)
}

// ofdTransparency 把 PDF 不透明度（0-1）转换为 OFD 图元透明度（0-255，
// 255 表示完全透明）。完全不透明时返回 nil 以省略该属性。
func ofdTransparency(opacity float64) *uint8 {
	if opacity >= 1 {
		return nil
	}
	if opacity < 0 {
		opacity = 0
	}
	value := uint8(math.Round((1 - opacity) * 255))
	return &value
}

// ofdColorOpacity 把 PDF 不透明度应用到 OFD 颜色（Alpha 为不透明度）。
func ofdColorOpacity(source *creator.Color, opacity float64) *creator.Color {
	if source == nil || opacity >= 1 {
		return source
	}
	if opacity < 0 {
		opacity = 0
	}
	value := uint8(math.Round(opacity * 255))
	source.Alpha = &value
	return source
}

func anyFloat(value any) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case int:
		return float64(number)
	case types.Integer:
		return float64(number)
	case types.Float:
		return float64(number)
	case types.Array:
		// 兼容少数把数值包成单元素数组的写法。
		if len(number) == 1 {
			return anyFloat(number[0])
		}
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
	// 首选 ICC 转换（可通过 OFD_CMYK_ICC 或系统 CMYK 配置文件提供），
	// 不可用时回退到近似油墨模型。
	if transformer, _ := coloricc.DefaultCMYK(); transformer != nil {
		r, g, b := transformer.ToRGB([]uint8{
			byte(clamp01(c)*255 + 0.5),
			byte(clamp01(m)*255 + 0.5),
			byte(clamp01(y)*255 + 0.5),
			byte(clamp01(k)*255 + 0.5),
		})
		return pdfColor{r: r, g: g, b: b}
	}
	r, g, b := cmykInkToRGB(byte(clamp01(c)*255+0.5), byte(clamp01(m)*255+0.5), byte(clamp01(y)*255+0.5), byte(clamp01(k)*255+0.5))
	return pdfColor{r: r, g: g, b: b}
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
