package pdf2ofd

import (
	"fmt"
	"math"

	"github.com/benoitkugler/textlayout/fonts/simpleencodings"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	fontparser "github.com/tdewolff/font"
)

type pdfType3Font struct {
	matrix    [6]float64
	charProcs types.Dict
	resources types.Dict
	names     [256]string
}

func (p *pdfInterpreter) loadType3Font(font *pdfFontInfo, dict types.Dict) {
	matrix := pdfNumberArray(p.ctx, dict["FontMatrix"])
	if len(matrix) != 6 {
		p.report("font.type3", "Type3 FontMatrix 必须包含六个有限数值")
		return
	}
	for _, v := range matrix {
		if !calculatorFinite(v) {
			p.report("font.type3", "Type3 FontMatrix 包含无效数值")
			return
		}
	}
	procs, err := p.ctx.XRefTable.DereferenceDict(dict["CharProcs"])
	if err != nil || procs == nil {
		p.report("font.type3", "Type3 CharProcs 不是可用字典")
		return
	}
	f := &pdfType3Font{charProcs: procs, names: [256]string(simpleencodings.AdobeStandard)}
	copy(f.matrix[:], matrix)
	if dict["Resources"] != nil {
		f.resources, err = p.ctx.XRefTable.DereferenceDict(dict["Resources"])
		if err != nil {
			p.report("font.type3", "Type3 Resources 无效")
			return
		}
	}
	encoding, err := dereferencePDFObject(p.ctx, dict["Encoding"])
	if err != nil {
		p.report("font.type3", "Type3 Encoding 无效")
		return
	}
	base := "StandardEncoding"
	if name, ok := encoding.(types.Name); ok {
		base = name.Value()
	}
	if d, ok := encoding.(types.Dict); ok {
		if name, ok := dereferenceName(p.ctx, d["BaseEncoding"]); ok {
			base = name
		}
	}
	switch base {
	case "WinAnsiEncoding":
		f.names = [256]string(simpleencodings.WinAnsi)
	case "MacRomanEncoding":
		f.names = [256]string(simpleencodings.MacRoman)
	case "MacExpertEncoding":
		f.names = [256]string(simpleencodings.MacExpert)
	}
	if d, ok := encoding.(types.Dict); ok && d["Differences"] != nil {
		values, err := p.ctx.XRefTable.DereferenceArray(d["Differences"])
		if err != nil {
			p.report("font.type3", "Type3 Differences 无效")
			return
		}
		code := -1
		for _, object := range values {
			if p.control.consumeOperations(1) != nil {
				return
			}
			value, err := dereferencePDFObject(p.ctx, object)
			if err != nil {
				p.report("font.type3", "Type3 Differences 引用无效")
				return
			}
			if n, ok := numberValue(value); ok {
				if n < 0 || n > 255 || n != math.Trunc(n) {
					p.report("font.type3", "Type3 Differences 代码无效")
					return
				}
				code = int(n)
				continue
			}
			name, ok := value.(types.Name)
			if !ok || code < 0 || code > 255 {
				p.report("font.type3", "Type3 Differences 字形名无效")
				return
			}
			f.names[code] = name.Value()
			code++
		}
	}
	// 空轮廓 TrueType 字体仅承载 Unicode：可见字形由 CharProcs 绘制，文字层
	// 仍能作为原生文字写入往返 PDF，且不会叠加替代字体的可见笔画。
	data, err := pdfSemanticFont(font.toUnicode, p.control)
	if err != nil {
		p.report("font.type3", err.Error())
		return
	}
	font.type3 = f
	font.data, font.format, font.familyName = data, "ttf", "PDFType3Text"
	font.sfnt, _ = fontparser.ParseSFNT(data, 0)
	font.bold, font.italic = false, false
}

func (p *pdfInterpreter) drawType3Glyph(font pdfFontInfo, code uint16, pageResources types.Dict, depth int) error {
	f := font.type3
	name := f.names[byte(code)]
	object := f.charProcs[name]
	if object == nil {
		object = f.charProcs[".notdef"]
	}
	if object == nil {
		return fmt.Errorf("CharProcs 缺少字形 %s", name)
	}
	stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || stream == nil {
		return fmt.Errorf("CharProc %s 不是可用内容流", name)
	}
	maximum := int64(0)
	if p.control != nil {
		maximum = p.control.options.Limits.MaxFontBytes
	}
	if err := p.decodeStream(stream, maximum); err != nil {
		return err
	}
	// d0/d1 必须是第一个操作符；度量只作校验，推进量以字体 Widths 为准。
	tokens := newPDFContentTokenizer(stream.Content)
	tokens.control = p.control
	args := []any{}
	uncolored := p.type3Uncolored
	for {
		value, ok, err := tokens.next()
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("CharProc %s 缺少 d0/d1", name)
		}
		if op, ok := value.(string); ok {
			if (op != "d0" && op != "d1") || (op == "d0" && len(args) != 2) || (op == "d1" && len(args) != 6) {
				return fmt.Errorf("CharProc %s 必须以 d0/d1 开始", name)
			}
			for _, arg := range args {
				v, ok := arg.(float64)
				if !ok || !calculatorFinite(v) {
					return fmt.Errorf("CharProc %s 度量无效", name)
				}
			}
			if anyFloat(args[1]) != 0 {
				return fmt.Errorf("CharProc %s 垂直字宽必须为零", name)
			}
			if op == "d1" {
				uncolored = true
			}
			break
		}
		args = append(args, value)
		if len(args) > 6 {
			return fmt.Errorf("CharProc %s 度量参数过多", name)
		}
	}
	resources := f.resources
	if resources == nil {
		resources = pageResources
	}
	child := newPDFInterpreter(p.ctx, p.page, p.document, p.info, p.fontCache, p.imageCache)
	child.control = p.control
	child.type3Uncolored = uncolored
	child.source = p.source
	child.setSource(object, p.state.fontName+"/"+name)
	state := p.state
	text := multiplyPDFMatrix(state.textMatrix, [6]float64{state.fontSize * state.hScale / 100, 0, 0, state.fontSize, 0, state.textRise})
	state.ctm = multiplyPDFMatrix(state.ctm, multiplyPDFMatrix(text, f.matrix))
	// 文本渲染模式不作用于 Type3 CharProc 中的图形绘制；内部 BT 可另设 Tr。
	state.renderMode = 0
	return child.parse(stream.Content, resources, &state, depth+1)
}
