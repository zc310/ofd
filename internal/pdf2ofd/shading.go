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
	dict, err := dereferenceDict(p.ctx, object, true)
	if err != nil || dict == nil {
		return nil
	}
	shadingType, _ := dereferencedPDFNumber(p.ctx, dict["ShadingType"])
	if shadingType != 2 && shadingType != 3 {
		return nil
	}
	spaces := p.parseColorSpaceObject(dict, "ColorSpace", depth)
	if spaces == nil {
		return nil
	}
	functionObject, found := dict.Find("Function")
	if !found {
		return nil
	}
	var functions []*pdfFunction
	resolved, err := dereferencePDFObject(p.ctx, functionObject)
	if err != nil {
		return nil
	}
	if array, ok := resolved.(types.Array); ok {
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
	extend := [2]bool{}
	if value, err := dereferencePDFObject(p.ctx, dict["Extend"]); err == nil {
		if array, ok := value.(types.Array); ok {
			for index := 0; index < 2 && index < len(array); index++ {
				if flag, ok := array[index].(types.Boolean); ok {
					extend[index] = bool(flag)
				}
			}
		}
	}
	return &pdfShading{
		shadingType: int(shadingType),
		colorSpace:  spaces,
		coords:      pdfNumberArray(p.ctx, dict["Coords"]),
		functions:   functions,
		extend:      extend,
	}
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
		deviceX, deviceY := transformPDFPoint(x, y, p.state.ctm)
		pageX, pageY := p.pagePoint(deviceX, deviceY)
		return pageX - path.X, pageY - path.Y
	}
	scale := pdfMatrixScale(p.state.ctm) * p.info.userUnit * pdfPointToMillimeter
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
