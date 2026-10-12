package pdf2ofd

import (
	fontparser "github.com/zc310/font"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/pkg/creator"
)

func (p *pdfInterpreter) textClipping() bool {
	return p.state.renderMode >= 4 && p.state.renderMode <= 7
}

func (p *pdfInterpreter) commitTextClip() {
	if !p.textClipActive {
		return
	}
	region := p.textClip
	region.empty = len(region.commands) == 0 && len(region.texts) == 0
	p.state.clips = append(append([]pdfClipRegion(nil), p.state.clips...), region)
	p.textClip, p.textClipActive = pdfClipRegion{}, false
}

func pdfCodeGlyph(code uint64, text string, font pdfFontInfo) uint16 {
	if font.substitute {
		return pdfVerticalSubstituteGlyph(text, font)
	}
	if g, ok := font.codeToGID[uint32(code)]; ok {
		return g
	}
	if font.codeBytes == 2 || font.predefined != nil || font.encodingCMap != nil {
		cid := pdfCIDCode(code, font)
		g := cid
		if font.cidToGID != nil {
			g = font.cidToGID[cid]
		} else if font.sfnt != nil && font.sfnt.IsCFF {
			// CID-keyed CFF charsets can have nonidentity CID→GID even when the
			// CID is numerically within NumGlyphs. Repair preserves that mapping.
			if mapped := font.sfnt.GlyphIndex(fontfix.GlyphRune(cid)); mapped != 0 {
				return mapped
			}
		}
		if uint32(g) >= embeddedFontGlyphCount(font.data) {
			return font.sfnt.GlyphIndex(fontfix.GlyphRune(g))
		}
		return g
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return 0
	}
	return font.sfnt.GlyphIndex(runes[0])
}

func (p *pdfInterpreter) addTextClip(text string, codes []uint64, font pdfFontInfo, item creator.Text) {
	parts := pdfCodeTexts(text, codes, font)
	advance := 0.0
	for i, code := range codes {
		if p.control.check() != nil {
			return
		}
		if font.sfnt != nil && font.sfnt.Head != nil && font.sfnt.Head.UnitsPerEm > 0 {
			matrix := multiplyPDFMatrix(p.state.ctm, p.state.textMatrix)
			matrix = multiplyPDFMatrix(matrix, [6]float64{p.state.hScale / 100, 0, 0, 1, advance, p.state.textRise})
			path := pdfGlyphPather{control: p.control, matrix: matrix}
			g := pdfCodeGlyph(code, parts[i], font)
			err := font.sfnt.GlyphPath(&path, g, 0, 0, 0, p.state.fontSize/float64(font.sfnt.Head.UnitsPerEm), fontparser.NoHinting)
			if err != nil {
				p.report("text.clip", err.Error())
				return
			}
			p.textClip.commands = append(p.textClip.commands, path.commands...)
		} else if parts[i] != "" {
			// 非内嵌字体由 OFD 阅读器取得轮廓。每个代码单独定位，避免忽略 PDF 字宽。
			matrix := p.state.textMatrix
			p.state.textMatrix[4] += matrix[0] * advance
			p.state.textMatrix[5] += matrix[1] * advance
			clipText := p.makeText(parts[i], []uint64{code}, font)
			p.state.textMatrix = matrix
			p.textClip.texts = append(p.textClip.texts, clipText)
		}
		advance += pdfTextWidth([]uint64{code}, font, p.state.fontSize, p.state.charSpacing, p.state.wordSpacing, p.state.hScale)
	}
}

// Pather 直接写 PDF 设备坐标，二次曲线精确升阶为三次曲线。
type pdfGlyphPather struct {
	control              *conversionControl
	matrix               [6]float64
	commands             []pdfPathCommand
	x, y, startX, startY float64
}

func (p *pdfGlyphPather) append(op string, values ...float64) {
	if p.control.consumeOperations(1) != nil {
		return
	}
	for i := 0; i+1 < len(values); i += 2 {
		values[i], values[i+1] = transformPDFPoint(values[i], values[i+1], p.matrix)
	}
	p.commands = append(p.commands, pdfPathCommand{op, values})
}
func (p *pdfGlyphPather) MoveTo(x, y float64) {
	p.append("M", x, y)
	p.x, p.y, p.startX, p.startY = x, y, x, y
}
func (p *pdfGlyphPather) LineTo(x, y float64) { p.append("L", x, y); p.x, p.y = x, y }
func (p *pdfGlyphPather) QuadTo(x1, y1, x, y float64) {
	p.CubeTo(p.x+2*(x1-p.x)/3, p.y+2*(y1-p.y)/3, x+2*(x1-x)/3, y+2*(y1-y)/3, x, y)
}
func (p *pdfGlyphPather) CubeTo(x1, y1, x2, y2, x, y float64) {
	p.append("B", x1, y1, x2, y2, x, y)
	p.x, p.y = x, y
}
func (p *pdfGlyphPather) Close() { p.append("Z"); p.x, p.y = p.startX, p.startY }
