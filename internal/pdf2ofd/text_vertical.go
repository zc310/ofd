package pdf2ofd

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Encoding CMap 的 WMode 决定推进方向；ToUnicode 的 WMode 不用于排版。
func (p *pdfInterpreter) loadFontWritingMode(font *pdfFontInfo, dict types.Dict) {
	object, err := dereferencePDFObject(p.ctx, dict["Encoding"])
	if err != nil {
		return
	}
	if name, ok := object.(types.Name); ok {
		font.encoding = name.Value()
		font.vertical = strings.HasSuffix(font.encoding, "-V")
		if cmap, err := loadPDFPredefinedCMap(font.encoding); err != nil {
			p.report("font.cmap", err.Error())
		} else if cmap != nil {
			font.predefined = cmap
			font.vertical = cmap.vertical
		}
		return
	}
	stream := pdfMaskStream(object)
	if stream == nil {
		return
	}
	p.loadEncodingCMap(font, stream)
}

func (p *pdfInterpreter) loadCIDVerticalMetrics(font *pdfFontInfo, dict types.Dict) {
	font.defaultVY, font.defaultW1 = 880, -1000
	font.verticalMetrics = make(map[uint16]pdfVerticalMetric)
	if object := dict["DW2"]; object != nil {
		values, err := p.ctx.XRefTable.DereferenceArray(object)
		if err != nil || len(values) != 2 {
			p.report("font.vertical", "DW2 必须包含默认原点和垂直推进量")
			return
		}
		y, okY := dereferencedPDFNumber(p.ctx, values[0])
		w, okW := dereferencedPDFNumber(p.ctx, values[1])
		if !okY || !okW || !calculatorFinite(y) || !calculatorFinite(w) {
			p.report("font.vertical", "DW2 包含无效数值")
			return
		}
		font.defaultVY, font.defaultW1 = y, w
	}
	if dict["W2"] == nil {
		return
	}
	values, err := p.ctx.XRefTable.DereferenceArray(dict["W2"])
	if err != nil {
		p.report("font.vertical", "W2 不是数组")
		return
	}
	number := func(o types.Object) (float64, bool) {
		n, ok := dereferencedPDFNumber(p.ctx, o)
		return n, ok && calculatorFinite(n)
	}
	cid := func(o types.Object) (int, bool) {
		n, ok := number(o)
		return int(n), ok && n >= 0 && n <= math.MaxUint16 && n == math.Trunc(n)
	}
	metric := func(v types.Array) (pdfVerticalMetric, bool) {
		if len(v) < 3 {
			return pdfVerticalMetric{}, false
		}
		w, okW := number(v[0])
		x, okX := number(v[1])
		y, okY := number(v[2])
		return pdfVerticalMetric{w, x, y}, okW && okX && okY
	}
	for i := 0; i < len(values); {
		first, ok := cid(values[i])
		i++
		if !ok || i >= len(values) {
			p.report("font.vertical", "W2 起始 CID 无效或缺少度量")
			return
		}
		if array, err := p.ctx.XRefTable.DereferenceArray(values[i]); err == nil {
			i++
			if len(array)%3 != 0 || first+len(array)/3 > 1<<16 {
				p.report("font.vertical", "W2 度量数组不完整或超出 CID 范围")
				return
			}
			for j := 0; j < len(array); j += 3 {
				if p.control.consumeOperations(1) != nil {
					return
				}
				m, ok := metric(array[j:])
				if !ok {
					p.report("font.vertical", "W2 包含无效度量")
					return
				}
				font.verticalMetrics[uint16(first+j/3)] = m
			}
		} else {
			last, ok := cid(values[i])
			i++
			if !ok || last < first || len(values)-i < 3 {
				p.report("font.vertical", "W2 CID 区间无效或缺少度量")
				return
			}
			m, ok := metric(values[i:])
			i += 3
			if !ok {
				p.report("font.vertical", "W2 包含无效度量")
				return
			}
			for code := first; code <= last; code++ {
				if p.control.consumeOperations(1) != nil {
					return
				}
				font.verticalMetrics[uint16(code)] = m
			}
		}
	}
}

func pdfCodeVerticalMetric(code uint64, font pdfFontInfo) pdfVerticalMetric {
	if m, ok := font.verticalMetrics[pdfCIDCode(code, font)]; ok {
		return m
	}
	y, w := font.defaultVY, font.defaultW1
	if font.verticalMetrics == nil {
		y, w = 880, -1000
	}
	return pdfVerticalMetric{w, pdfCodeWidth(code, font) / 2, y}
}

// 一个代码可以映射多个 Unicode 码位，但仍只占一个 PDF 字形和一次推进。
func pdfCodeTexts(text string, codes []uint64, font pdfFontInfo) []string {
	runes := []rune(text)
	parts := make([]string, len(codes))
	position := 0
	for i, code := range codes {
		count := len([]rune(pdfCodeUnicode(code, font)))
		if count == 0 {
			count = 1
		}
		count = min(count, len(runes)-position)
		parts[i] = string(runes[position : position+count])
		position += count
	}
	return parts
}

func (p *pdfInterpreter) showPositionedText(text string, codes []uint64, font pdfFontInfo, resources types.Dict, depth int) {
	parts := pdfCodeTexts(text, codes, font)
	for i, code := range codes {
		if p.control.check() != nil {
			return
		}
		if font.substitute {
			for _, lookup := range font.verticalSubstitutions {
				if p.control.consumeOperations(int64(len(lookup))) != nil {
					return
				}
			}
		}
		if font.vertical && font.type3 == nil && !font.verticalMissingReported {
			r, _ := utf8.DecodeRuneInString(parts[i])
			if pdfVerticalPresentationRune(r) != r && (len(font.data) == 0 || (font.substitute && pdfVerticalSubstituteGlyph(parts[i], font) == font.sfnt.GlyphIndex(r))) {
				p.report("font.vertical_glyph", "缺少可用的竖排替代字形，保留原始文字并按普通字形近似")
				font.verticalMissingReported = true
				p.fonts[p.state.fontName] = font
				if p.control.check() != nil {
					return
				}
			}
		}
		matrix := p.state.textMatrix
		if font.vertical {
			m := pdfCodeVerticalMetric(code, font)
			// W2 locates the horizontal glyph origin relative to the vertical
			// origin. Tz scales the glyph itself, not this displacement.
			x, y := -m.x*p.state.fontSize/1000, -m.y*p.state.fontSize/1000
			p.state.textMatrix[4] += matrix[0]*x + matrix[2]*y
			p.state.textMatrix[5] += matrix[1]*x + matrix[3]*y
		}
		if font.type3 != nil && p.state.renderMode != 3 && p.state.renderMode != 7 {
			name := font.type3.names[byte(code)]
			object := font.type3.charProcs[name]
			if object == nil {
				object = font.type3.charProcs[".notdef"]
			}
			previous := p.setSource(object, p.state.fontName+"/"+name)
			if err := p.drawType3Glyph(font, code, resources, depth); err != nil {
				p.report("font.type3", fmt.Sprintf("字形 %d: %v", code, err))
			}
			p.source = previous
		}
		if strings.TrimSpace(parts[i]) != "" {
			item := p.makeText(parts[i], []uint64{code}, font)
			if p.textClipping() && font.type3 == nil {
				p.addTextClip(parts[i], []uint64{code}, font, item)
			}
			// Type3 的可见内容已经由 CharProc 发出；语义字体的字形为空。
			if font.type3 != nil {
				fill := true
				item.Fill = &fill
				item.Stroke = false
				item.CGTransforms = nil
			}
			p.appendText(item, font)
		}
		p.state.textMatrix = matrix
		if font.vertical {
			advance := pdfCodeVerticalMetric(code, font).advance*p.state.fontSize/1000 + p.state.charSpacing
			// Tw 只应用于单字节代码 32，不是 ToUnicode 映射后的空格。
			if pdfCodeWordSpacing(code, font) {
				advance += p.state.wordSpacing
			}
			p.adjustText(advance)
		} else {
			width := font.widths[int(code)]
			x := width*font.type3.matrix[0]*p.state.fontSize + p.state.charSpacing
			if code == 32 {
				x += p.state.wordSpacing
			}
			x *= p.state.hScale / 100
			y := width * font.type3.matrix[1] * p.state.fontSize
			p.state.textMatrix[4] += matrix[0]*x + matrix[2]*y
			p.state.textMatrix[5] += matrix[1]*x + matrix[3]*y
		}
	}
}
