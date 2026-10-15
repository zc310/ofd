package pdf2ofd

import (
	"fmt"
	"math"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

// pdfShading 表示已解析的 PDF 渐变着色（ShadingType 2/3）。
type pdfShading struct {
	shadingType int
	colorSpace  *pdfColorSpace
	coords      []float64
	functions   []*pdfFunction
	extend      [2]bool
	// matrix 是 PatternType 2 图案的 Matrix，把图案空间映射到页面默认坐标
	// 空间。sh 操作符直接使用着色时保持单位矩阵。
	matrix [6]float64
	// pattern 表示该着色来自 PatternType 2 图案。图案坐标位于页面默认坐标
	// 空间，绘制时不再叠加当前 CTM；sh 操作符的着色位于当前用户空间，需要叠加。
	pattern bool
	// mesh 是 ShadingType 4/5/6/7 网格着色的原始数据。
	mesh *pdfMeshData
	// meshBackground 是网格着色的 Background 颜色；存在时作为 OFD 的
	// BackColor 并在网格外延展。
	meshBackground    pdfColor
	hasMeshBackground bool
}

// shadingSampleCount 是渐变转换为 OFD Segment 时的采样数。OFD 只支持分段
// 线性插值，采样越密越接近 PDF 的函数渐变。
const shadingSampleCount = 32

// paintShading 处理 sh 操作符：用当前裁剪区作为填充范围，把着色绘制到页面。
func (p *pdfInterpreter) paintShading(name string, resources types.Dict) {
	if p.page == nil || p.ctx == nil || resources == nil || name == "" {
		return
	}
	shadings, ok := dereferencedSubDict(p.ctx, resources, "Shading")
	if !ok {
		return
	}
	object, found := shadings.Find(name)
	if !found {
		return
	}
	previousSource := p.setSource(object, name)
	defer func() { p.source = previousSource }()
	shading := p.parseShading(object, 0)
	if shading == nil {
		p.report("shading.unsupported", "着色资源无法完整转换")
		return
	}
	// 网格着色（Type 4/5/6/7）优先输出为矢量网格，超出上限或裁剪区无法还原
	// 为路径时再回退为位图。
	if shading.mesh != nil {
		matrix := multiplyPDFMatrix(p.state.ctm, shading.matrix)
		if path, ok := p.shadingFillPath(); ok {
			if color, ok := p.meshVectorColor(shading, matrix, path); ok {
				path.Fill = true
				path.Rule = spec.FillRuleNonZero
				path.FillColor = color
				// 与普通着色一样必须显式关闭描边，避免沿路径边界描黑边。
				stroke := false
				path.Stroke = false
				path.StrokeSet = &stroke
				if clips := p.buildClips(path.X, path.Y); clips != nil {
					path.Clips = clips
				}
				path.Alpha = ofdAlpha(p.fillOpacity())
				p.appendItem(*path)
				return
			}
		}
		if image := p.meshImage(shading, matrix); image != nil {
			p.appendItem(*image)
		}
		return
	}
	path, ok := p.shadingFillPath()
	if !ok {
		return
	}
	fillColor := p.shadingColor(shading, path.X, path.Y)
	if fillColor == nil {
		return
	}
	path.Fill = true
	path.Rule = spec.FillRuleNonZero
	path.FillColor = fillColor
	// 必须显式关闭描边：creator 在 StrokeSet 为空时输出 Stroke="true"，
	// 会把着色边界描成黑色细线。
	stroke := false
	path.Stroke = false
	path.StrokeSet = &stroke
	if clips := p.buildClips(path.X, path.Y); clips != nil {
		path.Clips = clips
	}
	path.Alpha = ofdAlpha(p.fillOpacity())
	p.appendItem(*path)
}

// parseShading 解析着色字典。仅支持轴向（Type 2）与径向（Type 3）渐变。
func (p *pdfInterpreter) parseShading(object types.Object, depth int) *pdfShading {
	if depth > 8 {
		return nil
	}
	resolved, err := dereferencePDFObject(p.ctx, object)
	if err != nil || resolved == nil {
		return nil
	}
	// 着色对象通常是字典；网格着色（Type 4/5/6/7）是流对象。
	var dict types.Dict
	switch value := resolved.(type) {
	case types.Dict:
		dict = value
	case types.StreamDict:
		dict = value.Dict
	case *types.StreamDict:
		dict = value.Dict
	default:
		return nil
	}
	if dict == nil {
		return nil
	}
	shadingType, _ := dereferencedPDFNumber(p.ctx, dict["ShadingType"])
	if shadingType < 2 || shadingType > 7 {
		p.report("shading.type", fmt.Sprintf("Shading Type %g 尚未支持", shadingType))
		return nil
	}
	spaces := p.parseColorSpaceObject(dict, "ColorSpace", depth)
	if spaces == nil {
		return nil
	}
	shading := &pdfShading{
		shadingType: int(shadingType),
		colorSpace:  spaces,
		coords:      pdfNumberArray(p.ctx, dict["Coords"]),
		matrix:      identityPDFMatrix(),
	}
	// /Function 必须对网格着色同样解析：Type 4/5/6/7 的流里编码的是函数**输入**，
	// 颜色要由函数求值得到（tensor-allflags-withfunction.pdf 就是 1 入 3 出的
	// 指数函数）。此前网格分支提前 return，函数从未被解析，颜色分量按颜色空间
	// 分量数直接读，既错色又错位。
	var functions []*pdfFunction
	if functionObject, hasFunction := dict.Find("Function"); hasFunction {
		functionResolved, err := dereferencePDFObject(p.ctx, functionObject)
		if err != nil {
			return nil
		}
		if array, ok := functionResolved.(types.Array); ok {
			for _, item := range array {
				function := p.parseFunction(item, depth+1)
				if function == nil {
					return nil
				}
				functions = append(functions, function)
			}
		} else {
			function := p.parseFunction(functionObject, depth+1)
			if function == nil {
				return nil
			}
			functions = append(functions, function)
		}
		shading.functions = functions
	}
	// 网格着色（Type 4/5/6/7）保存原始数据，绘制时再展开为 OFD 三角网格。
	if shadingType >= 4 {
		shading.mesh = p.parseMeshData(object, dict)
		if shading.mesh == nil {
			return nil
		}
		// 可选的 Background 颜色在绘制网格前铺满裁剪区，对应 OFD 的
		// BackColor；存在时网格外区域延展为该背景色。
		if values := pdfNumberArray(p.ctx, dict["Background"]); len(values) >= spaces.components {
			components := make([]float64, spaces.components)
			copy(components, values)
			if color, ok := spaces.colorFromComponents(components); ok {
				shading.meshBackground = color
				shading.hasMeshBackground = true
			}
		}
		return shading
	}
	if len(functions) == 0 {
		return nil
	}
	if value, err := dereferencePDFObject(p.ctx, dict["Extend"]); err == nil {
		if array, ok := value.(types.Array); ok {
			for index := 0; index < 2 && index < len(array); index++ {
				if flag, ok := array[index].(types.Boolean); ok {
					shading.extend[index] = bool(flag)
				}
			}
		}
	}
	return shading
}

// parseMeshData 读取网格着色的位图数据与解码参数。
func (p *pdfInterpreter) parseMeshData(object types.Object, dict types.Dict) *pdfMeshData {
	stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || stream == nil {
		return nil
	}
	if err := p.decodeStream(stream, 0); err != nil {
		p.report("shading.decode", err.Error())
		return nil
	}
	data := stream.Content
	if len(data) == 0 {
		return nil
	}
	mesh := &pdfMeshData{
		data:              append([]byte(nil), data...),
		bitsPerCoordinate: 8,
		bitsPerComponent:  8,
	}
	var shadingType float64
	shadingType, _ = dereferencedPDFNumber(p.ctx, dict["ShadingType"])
	mesh.kind = int(shadingType)
	if value, ok := integerValue(dict["BitsPerCoordinate"]); ok && value > 0 {
		mesh.bitsPerCoordinate = value
	}
	if value, ok := integerValue(dict["BitsPerComponent"]); ok && value > 0 {
		mesh.bitsPerComponent = value
	}
	if value, ok := integerValue(dict["BitsPerFlag"]); ok && value >= 0 {
		mesh.bitsPerFlag = value
	}
	if mesh.kind == 5 {
		if value, ok := integerValue(dict["VerticesPerRow"]); ok {
			mesh.verticesPerRow = value
		}
	}
	mesh.decode = pdfNumberArray(p.ctx, dict["Decode"])
	// 函数型网格着色的流里存的是函数输入，Decode 的分量对数即输入维数。
	// 缺省按 [0 1] 处理，规范不规定对数，取最常见的 1 维。
	if len(mesh.decode) > 4 {
		mesh.streamComponents = (len(mesh.decode) - 4) / 2
	} else {
		mesh.streamComponents = 1
	}
	if mesh.bitsPerCoordinate > 32 || mesh.bitsPerComponent > 16 || mesh.bitsPerFlag > 8 {
		return nil
	}
	return mesh
}

func (p *pdfInterpreter) parseColorSpaceObject(dict types.Dict, key string, depth int) *pdfColorSpace {
	object, found := dict.Find(key)
	if !found {
		return nil
	}
	return p.parseColorSpace(object, depth+1)
}

// shadingColor 把着色转换为 OFD 渐变填充；不支持的类型返回 nil。OFD 渐变
// 坐标是相对对象边界左上角的局部坐标，因此需要减去对象的 X/Y（路径边界或
// 文字对象边界）。
// emitMeshFill 把网格着色图案（PatternType 2 + ShadingType 4/5/6/7）作为填充
// 图案发出。sh 操作符走 emitShading，这里是 scn 选中图案后的等价路径。
//
// 此前这类图案在 shadingColor 里直接返回 nil，路径保留 defaultW 的黑色填充，
// 整页渐变背景就变成一块黑板（tensor-allflags-withfunction.pdf）。矢量网格
// 放不下时回退为位图，两条路都失败才让调用方保留原状。
func (p *pdfInterpreter) emitMeshFill(shading *pdfShading, path *creator.Path) bool {
	if p.page == nil {
		return false
	}
	// PatternType 2 的图案空间经图案 Matrix 映射到页面默认坐标空间，不叠加 CTM；
	// 与 shadingColor 的取法保持一致。
	matrix := shading.matrix
	// Gouraud/LaGouraud 只在网格范围内有意义，超出部分阅读器会把边缘颜色延展
	// 到整条路径边界（tensor-allflags-withfunction.pdf 的整页渐变就是这么来的）。
	// 网格之外在 PDF 里根本没有着色，因此按网格包围盒裁剪。
	bounds, bounded := p.meshBoundsMM(shading, matrix)
	if color, ok := p.meshVectorColor(shading, matrix, path); ok {
		path.Fill = true
		path.Rule = spec.FillRuleNonZero
		path.FillColor = color
		stroke := false
		path.Stroke = false
		path.StrokeSet = &stroke
		if bounded {
			restrictPathToBounds(path, bounds)
		}
		if clips := p.buildClips(path.X, path.Y); clips != nil {
			path.Clips = clips
		}
		path.Alpha = ofdAlpha(p.fillOpacity())
		p.appendItem(*path)
		return true
	}
	if image := p.meshImage(shading, matrix); image != nil {
		p.appendItem(*image)
		return true
	}
	return false
}

// meshBoundsMM 返回网格控制点在页面毫米坐标下的包围盒。
func (p *pdfInterpreter) meshBoundsMM(shading *pdfShading, matrix [6]float64) ([4]float64, bool) {
	points, _, ok := decodeMeshPoints(shading)
	if !ok || len(points) == 0 {
		return [4]float64{}, false
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, point := range points {
		deviceX, deviceY := transformPDFPoint(point.x, point.y, matrix)
		pageX, pageY := p.pagePoint(deviceX, deviceY)
		minX, minY = math.Min(minX, pageX), math.Min(minY, pageY)
		maxX, maxY = math.Max(maxX, pageX), math.Max(maxY, pageY)
	}
	if math.IsInf(minX, 0) || minX > maxX || minY > maxY {
		return [4]float64{}, false
	}
	// 外扩一个极窄的余量，避免因取整把边界像素整掉。
	pad := 0.02
	return [4]float64{minX - pad, minY - pad, maxX + pad, maxY + pad}, true
}

// restrictPathToBounds 把填充路径收缩为网格包围盒与原路径的交集。
//
// Gouraud 渐变只在网格控制点围成的范围内有意义，范围之外阅读器会把边缘颜色
// 延展到整条路径边界，整页渐变因此会铺满全页；而网格之外在 PDF 里根本没有
// 着色。这里直接改写路径数据而不是挂 Clips——裁剪区的坐标换算在各阅读器之间
// 并不一致，写成矩形路径最稳妥。取交集保证不会把填充范围扩大到原路径之外。
func restrictPathToBounds(path *creator.Path, bounds [4]float64) {
	minX, minY := math.Max(bounds[0], path.X), math.Max(bounds[1], path.Y)
	maxX, maxY := math.Min(bounds[2], path.X+path.Width), math.Min(bounds[3], path.Y+path.Height)
	if minX >= maxX || minY >= maxY {
		// 网格完全落在原路径之外，不绘制。
		path.Width, path.Height = 0, 0
		path.Data = ""
		return
	}
	// 外扩一个极窄的余量，避免取整把边缘像素整掉。
	minX, minY, maxX, maxY = minX-0.02, minY-0.02, maxX+0.02, maxY+0.02
	ox, oy := minX-path.X, minY-path.Y
	width, height := maxX-minX, maxY-minY
	path.Data = fmt.Sprintf("M %.4f %.4f L %.4f %.4f L %.4f %.4f L %.4f %.4f C",
		ox, oy+height, ox+width, oy+height, ox+width, oy, ox, oy)
	path.Width, path.Height = width, height
}

// meshColorAt 用函数型网格着色解码出的输入值求颜色。Type 4/5/6/7 的流里
// 编码的是 /Function 的输入，函数输出才是颜色空间中的颜色。
func (s *pdfShading) meshColorAt(values []float64) (pdfColor, bool) {
	switch {
	case len(s.functions) == 1:
		output, ok := s.functions[0].eval(values)
		if !ok {
			return pdfColor{}, false
		}
		return s.colorSpace.colorFromComponents(output)
	case len(s.functions) == len(values) && len(values) > 1:
		// 每个函数取各自的一个输入（颜色空间分量各一个函数）。
		outputs := make([]float64, 0, len(values))
		for index, function := range s.functions {
			output, ok := function.eval(values[index : index+1])
			if !ok || len(output) == 0 {
				return pdfColor{}, false
			}
			outputs = append(outputs, output[0])
		}
		return s.colorSpace.colorFromComponents(outputs)
	case len(values) == 1:
		components := make([]float64, 0, len(s.functions))
		for _, function := range s.functions {
			output, ok := function.eval(values)
			if !ok || len(output) == 0 {
				return pdfColor{}, false
			}
			components = append(components, output[0])
		}
		return s.colorSpace.colorFromComponents(components)
	default:
		return pdfColor{}, false
	}
}

func (p *pdfInterpreter) shadingColor(shading *pdfShading, originX, originY float64) *creator.Color {
	// PatternType 2 图案空间先经图案 Matrix 映射到页面默认坐标空间（不叠加
	// 当前 CTM）；sh 操作符的着色位于当前用户空间，需要叠加 CTM。
	matrix := shading.matrix
	if !shading.pattern {
		matrix = multiplyPDFMatrix(p.state.ctm, matrix)
	}
	if shading.mesh != nil {
		// 网格图案填充暂不支持。
		return nil
	}
	stops := make([]creator.ColorStop, 0, shadingSampleCount+1)
	for index := 0; index <= shadingSampleCount; index++ {
		position := float64(index) / shadingSampleCount
		color, ok := shading.colorAt(position)
		if !ok {
			return nil
		}
		stops = append(stops, creator.ColorStop{Position: position, PositionSet: true, Color: creator.Color{R: color.r, G: color.g, B: color.b}})
	}
	extend := 0
	if shading.extend[0] {
		extend |= 1
	}
	if shading.extend[1] {
		extend |= 2
	}
	toLocal := func(x, y float64) (float64, float64) {
		deviceX, deviceY := transformPDFPoint(x, y, matrix)
		pageX, pageY := p.pagePoint(deviceX, deviceY)
		return pageX - originX, pageY - originY
	}
	scale := pdfMatrixScale(matrix) * p.info.userUnit * pdfPointToMillimeter
	point := func(x, y float64) string {
		lx, ly := toLocal(x, y)
		return fmt.Sprintf("%.4f %.4f", lx, ly)
	}
	switch shading.shadingType {
	case 2:
		if len(shading.coords) < 4 {
			return nil
		}
		return &creator.Color{Axial: &creator.AxialShading{
			Extend:     extend,
			StartPoint: point(shading.coords[0], shading.coords[1]),
			EndPoint:   point(shading.coords[2], shading.coords[3]),
			Segments:   stops,
		}}
	case 3:
		if len(shading.coords) < 6 {
			return nil
		}
		return &creator.Color{Radial: &creator.RadialShading{
			Extend:      extend,
			StartPoint:  point(shading.coords[0], shading.coords[1]),
			StartRadius: shading.coords[2] * scale,
			EndPoint:    point(shading.coords[3], shading.coords[4]),
			EndRadius:   shading.coords[5] * scale,
			Segments:    stops,
		}}
	default:
		return nil
	}
}

// colorAt 在参数 t 处求值着色函数并转换为设备 RGB。
func (s *pdfShading) colorAt(t float64) (pdfColor, bool) {
	if len(s.functions) == 0 {
		return pdfColor{}, false
	}
	if len(s.functions) == 1 {
		output, ok := s.functions[0].eval([]float64{t})
		if !ok {
			return pdfColor{}, false
		}
		return s.colorSpace.colorFromComponents(output)
	}
	components := make([]float64, 0, len(s.functions))
	for _, function := range s.functions {
		output, ok := function.eval([]float64{t})
		if !ok || len(output) == 0 {
			return pdfColor{}, false
		}
		components = append(components, output[0])
	}
	return s.colorSpace.colorFromComponents(components)
}

// shadingFillPath 用当前裁剪区构造填充路径。着色只能绘制在裁剪区内；
// 没有裁剪区时退化为整页矩形。
func (p *pdfInterpreter) shadingFillPath() (*creator.Path, bool) {
	if len(p.state.clips) == 0 {
		if p.page == nil || p.page.Area == nil || p.page.Area.PhysicalBox == nil {
			return nil, false
		}
		box := p.page.Area.PhysicalBox
		return &creator.Path{
			X:      0,
			Y:      0,
			Width:  box.Width,
			Height: box.Height,
			Data:   fmt.Sprintf("M 0 0 L %.4f 0 L %.4f %.4f L 0 %.4f C", box.Width, box.Width, box.Height, box.Height),
		}, true
	}
	region := p.state.clips[len(p.state.clips)-1]
	clipPath, ok := p.clipPathFor(region, 0, 0)
	if !ok {
		return nil, false
	}
	return &creator.Path{
		X:      clipPath.Boundary.X,
		Y:      clipPath.Boundary.Y,
		Width:  clipPath.Boundary.Width,
		Height: clipPath.Boundary.Height,
		Data:   strings.TrimSpace(clipPath.Data),
	}, true
}
