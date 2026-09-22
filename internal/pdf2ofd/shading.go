package pdf2ofd

import (
	"fmt"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/pkg/creator"
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
	shading := p.parseShading(object, 0)
	if shading == nil {
		return
	}
	// 网格着色（Type 4/5/6/7）优先输出为矢量网格，超出上限或裁剪区无法还原
	// 为路径时再回退为位图。
	if shading.mesh != nil {
		matrix := multiplyPDFMatrix(p.state.ctm, shading.matrix)
		if path, ok := p.shadingFillPath(); ok {
			if color, ok := p.meshVectorColor(shading, matrix, path); ok {
				path.Fill = true
				path.Rule = "NonZero"
				path.FillColor = color
				// 与普通着色一样必须显式关闭描边，避免沿路径边界描黑边。
				stroke := false
				path.Stroke = false
				path.StrokeSet = &stroke
				if clips := p.buildClips(path.X, path.Y, false, 0, 0); clips != nil {
					path.Clips = clips
				}
				path.Alpha = ofdTransparency(p.fillOpacity())
				p.page.Items = append(p.page.Items, *path)
				return
			}
		}
		if image := p.meshImage(shading, matrix); image != nil {
			p.page.Items = append(p.page.Items, *image)
		}
		return
	}
	path, ok := p.shadingFillPath()
	if !ok {
		return
	}
	fillColor := p.shadingColor(shading, path)
	if fillColor == nil {
		return
	}
	path.Fill = true
	path.Rule = "NonZero"
	path.FillColor = fillColor
	// 必须显式关闭描边：creator 在 StrokeSet 为空时输出 Stroke="true"，
	// 会把着色边界描成黑色细线。
	stroke := false
	path.Stroke = false
	path.StrokeSet = &stroke
	if clips := p.buildClips(path.X, path.Y, false, 0, 0); clips != nil {
		path.Clips = clips
	}
	p.page.Items = append(p.page.Items, *path)
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
	functionObject, found := dict.Find("Function")
	if !found {
		return nil
	}
	var functions []*pdfFunction
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
	data := pdfStreamContent(stream)
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
// 坐标是相对路径边界左上角的局部坐标，因此需要减去 path 的 X/Y。
func (p *pdfInterpreter) shadingColor(shading *pdfShading, path *creator.Path) *creator.Color {
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
		stops = append(stops, creator.ColorStop{Position: position, Color: creator.Color{R: color.r, G: color.g, B: color.b}})
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
		return pageX - path.X, pageY - path.Y
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
