package pdf2ofd

import (
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// renderAnnotations 把页面注解的外观流（/AP /N）绘制到页面上。没有外观流的
// 注解（例如部分仅有弹窗内容的文本注解）跳过。
func (p *pdfInterpreter) renderAnnotations(pageDict types.Dict) {
	object, found := pageDict.Find("Annots")
	if !found {
		return
	}
	resolved, err := p.ctx.XRefTable.Dereference(object)
	if err != nil || resolved == nil {
		return
	}
	array, ok := resolved.(types.Array)
	if !ok {
		return
	}
	for _, entry := range array {
		annotation, err := p.ctx.XRefTable.DereferenceDict(entry)
		if err != nil || annotation == nil {
			continue
		}
		p.renderAnnotation(annotation)
	}
}

func (p *pdfInterpreter) renderAnnotation(annot types.Dict) {
	if flags, ok := integerValue(annot["F"]); ok {
		// Bit 2 Hidden、bit 6 NoView 的注解不显示。
		if flags&2 != 0 || flags&32 != 0 {
			return
		}
	}
	if name, ok := annot["Subtype"].(types.Name); ok {
		switch name.Value() {
		// Popup 是文本注解的弹窗、Link/Widget 无独立外观，跳过。
		case "Popup", "Link", "Widget":
			return
		}
	}
	appearance := p.annotationAppearance(annot)
	if appearance == nil {
		return
	}
	form, _, err := p.ctx.XRefTable.DereferenceStreamDict(appearance)
	if err != nil || form == nil {
		return
	}
	rect := pdfNumberArray(p.ctx, annot["Rect"])
	if len(rect) < 4 {
		return
	}
	bbox := pdfNumberArray(p.ctx, form.Dict["BBox"])
	if len(bbox) < 4 {
		return
	}
	if form.Decode() != nil {
		return
	}
	matrix := identityPDFMatrix()
	if values := pdfNumberArray(p.ctx, form.Dict["Matrix"]); len(values) == 6 {
		matrix = [6]float64{values[0], values[1], values[2], values[3], values[4], values[5]}
	}
	placement := appearanceMatrix([4]float64{rect[0], rect[1], rect[2], rect[3]}, [4]float64{bbox[0], bbox[1], bbox[2], bbox[3]}, matrix)
	resources, _ := dereferencedSubDict(p.ctx, form.Dict, "Resources")

	state := pdfGraphicsState{
		ctm: placement, textMatrix: identityPDFMatrix(), lineMatrix: identityPDFMatrix(), fontSize: 12,
		fill: pdfColor{}, stroke: pdfColor{}, lineWidth: 1, hScale: 100, fillAlpha: 1, strokeAlpha: 1, groupAlpha: 1,
	}
	saved := p.state
	// 注解外观可以有自己的资源字典，并允许使用与页面同名的字体资源（如 /F1）。
	// 字体缓存按资源名而非资源字典索引，若不复位会把页面同名资源误用为外观
	// 字体：例如把页面 Identity-H 的正文宋体当作外观 UniGB-UCS2-H 的浅灰水印，
	// 使水印文本按错误编码解码、字形映射到错误字形而不可见。外观自带资源时
	// 用独立缓存解析，解析完恢复页面缓存。
	savedFonts, savedAliases := p.fonts, p.fontAliases
	if resources != nil {
		p.fonts = map[string]pdfFontInfo{}
		p.fontAliases = map[string]string{}
	}
	err = p.parse(form.Content, resources, &state, 0)
	p.fonts, p.fontAliases = savedFonts, savedAliases
	if err != nil {
		p.state = saved
		return
	}
	p.state = saved
}

// annotationAppearance 返回注解 /AP /N 的外观流对象。/N 可以是外观流，也可以是
// 外观状态字典（如 /On、/Off），此时用 /AS 选择对应状态。
func (p *pdfInterpreter) annotationAppearance(annot types.Dict) types.Object {
	apObject, found := annot.Find("AP")
	if !found {
		return nil
	}
	ap, err := p.ctx.XRefTable.DereferenceDict(apObject)
	if err != nil || ap == nil {
		return nil
	}
	normal, found := ap.Find("N")
	if !found {
		return nil
	}
	resolved, err := p.ctx.XRefTable.Dereference(normal)
	if err != nil || resolved == nil {
		return nil
	}
	dict, ok := resolved.(types.Dict)
	if !ok {
		return resolved
	}
	if name, ok := annot["AS"].(types.Name); ok {
		if stream, found := dict.Find(name.Value()); found {
			return stream
		}
	}
	for _, key := range []string{"On", "Yes", "Off"} {
		if stream, found := dict.Find(key); found {
			return stream
		}
	}
	for _, stream := range dict {
		return stream
	}
	return nil
}

// appearanceMatrix 计算把外观流 BBox（先经 Matrix 变换）映射到注解 Rect 的矩阵。
func appearanceMatrix(rect, bbox [4]float64, matrix [6]float64) [6]float64 {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, corner := range [][2]float64{{bbox[0], bbox[1]}, {bbox[2], bbox[1]}, {bbox[0], bbox[3]}, {bbox[2], bbox[3]}} {
		x, y := transformPDFPoint(corner[0], corner[1], matrix)
		minX, minY = math.Min(minX, x), math.Min(minY, y)
		maxX, maxY = math.Max(maxX, x), math.Max(maxY, y)
	}
	rectMinX, rectMinY := math.Min(rect[0], rect[2]), math.Min(rect[1], rect[3])
	rectWidth, rectHeight := math.Abs(rect[2]-rect[0]), math.Abs(rect[3]-rect[1])
	scaleX, scaleY := 1.0, 1.0
	if maxX > minX {
		scaleX = rectWidth / (maxX - minX)
	}
	if maxY > minY {
		scaleY = rectHeight / (maxY - minY)
	}
	result := identityPDFMatrix()
	result = multiplyPDFMatrix(result, [6]float64{1, 0, 0, 1, rectMinX, rectMinY})
	result = multiplyPDFMatrix(result, [6]float64{scaleX, 0, 0, scaleY, 0, 0})
	result = multiplyPDFMatrix(result, [6]float64{1, 0, 0, 1, -minX, -minY})
	result = multiplyPDFMatrix(result, matrix)
	return result
}
