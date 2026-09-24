package canvas

import (
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/font"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/backends/canvas/canvasconv"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// 本文件实现 canvas 字体面适配 canvasFontFace（ToPath/DirectPath/TextWidth/
// GlyphIndex/Fill/ShapedRun 等），以及 Fonts.FaceObject、buildTextFace、
// textFontStyle、directTextPath 与中性/ canvas 字体样式转换。

func (p *Fonts) shapedTextPath(face *canvas.FontFace, value string) *canvas.Path {
	if face == nil || face.Font == nil || value == "" {
		return nil
	}
	key := fontPathKey{font: face.Font, size: face.Size, style: drawing.FontStyle(face.Style), variant: face.Variant, value: value}
	p.pathCacheMu.Lock()
	if p.pathCache != nil {
		if path := p.pathCache[key]; path != nil {
			p.pathCacheMu.Unlock()
			return path
		}
	}
	path, _ := face.ToPath(value)
	if path == nil || path.Empty() {
		p.pathCacheMu.Unlock()
		return nil
	}
	if len(p.pathCache) >= 2048 {
		p.pathCache = make(map[fontPathKey]*canvas.Path)
	} else if p.pathCache == nil {
		p.pathCache = make(map[fontPathKey]*canvas.Path)
	}
	p.pathCache[key] = path
	p.pathCacheMu.Unlock()
	return path
}

// shapedTextLine 返回 (face, value) 经 canvas 原生整形的文字行，带缓存。
// canvas 后端的 DrawText 每次 NewTextLine 都会重新整形/分项，是明显热点；
// 相同字体数据 + 字号 + 样式 + 纯色画笔 + 文本的整形成果完全一致，直接复用。
// 渐变/图案画笔的文字不缓存，避免不同文本对象互相污染。返回的 canvas.Text
// 只被读取渲染，跨对象复用安全；缓存有界，超出后整体清空。
func (p *Fonts) shapedTextLine(face *canvas.FontFace, value string) *canvas.Text {
	if face == nil || face.Font == nil || value == "" {
		return nil
	}
	if !face.Fill.IsColor() {
		return canvas.NewTextLine(face, value, canvas.Left)
	}
	key := textLineKey{
		font:    face.Font,
		size:    face.Size,
		style:   drawing.FontStyle(face.Style),
		variant: face.Variant,
		fill:    face.Fill.Color,
		value:   value,
	}
	p.textLineMu.Lock()
	if p.textLineCache != nil {
		if line := p.textLineCache[key]; line != nil {
			p.textLineMu.Unlock()
			return line
		}
	}
	line := canvas.NewTextLine(face, value, canvas.Left)
	if len(p.textLineCache) >= 2048 {
		p.textLineCache = make(map[textLineKey]*canvas.Text)
	} else if p.textLineCache == nil {
		p.textLineCache = make(map[textLineKey]*canvas.Text)
	}
	p.textLineCache[key] = line
	p.textLineMu.Unlock()
	return line
}

type canvasFontFace struct {
	fonts *Fonts
	face  *canvas.FontFace
}

func (f canvasFontFace) ToPath(value string) *geom.Path {
	if f.fonts != nil {
		if path := f.fonts.shapedTextPath(f.face, value); path != nil {
			return canvasconv.FromCanvasPath(path)
		}
		return nil
	}
	path, _ := f.face.ToPath(value)
	if path == nil || path.Empty() {
		return nil
	}
	return canvasconv.FromCanvasPath(path)
}

func (f canvasFontFace) DirectPath(value string) *geom.Path {
	path := f.fonts.directTextPath(f.face, value)
	if path == nil {
		return nil
	}
	return canvasconv.FromCanvasPath(path)
}

func (f canvasFontFace) TextWidth(value string) float64 { return f.face.TextWidth(value) }

func (f canvasFontFace) GlyphIndex(r rune) uint16 {
	if f.face == nil || f.face.Font == nil {
		return 0
	}
	return f.face.Font.GlyphIndex(r)
}

func (f canvasFontFace) IsCFF() bool {
	return f.face != nil && f.face.Font != nil && f.face.Font.SFNT != nil && f.face.Font.SFNT.IsCFF
}

func (f canvasFontFace) Fill() geom.Paint { return canvasconv.FromCanvasPaint(f.face.Fill) }

func (f canvasFontFace) Size() float64 { return f.face.Size }

func (f canvasFontFace) ShapedRun(value string) drawing.TextRun {
	if f.fonts == nil {
		return nil
	}
	if line := f.fonts.shapedTextLine(f.face, value); line != nil {
		return line
	}
	return nil
}

// FaceObject 为字体族与文字对象创建一个中性字体面。fill 为文字填充色/渐变。
func (p *Fonts) FaceObject(family drawing.FontFamily, object models.TextObject, fill *drawing.CTColor) drawing.FontFace {
	cf, ok := family.(*canvas.FontFamily)
	if !ok || cf == nil {
		return nil
	}
	face := buildTextFace(cf, object, fill)
	if face == nil {
		return nil
	}
	return canvasFontFace{fonts: p, face: face}
}

// buildTextFace 根据文字对象样式创建 canvas 字体面。
func buildTextFace(family *canvas.FontFamily, object models.TextObject, fill *drawing.CTColor) *canvas.FontFace {
	args := make([]any, 0, 3)
	switch {
	case fill != nil && fill.Gradient != nil:
		args = append(args, canvasconv.ToCanvasGradient(fill.Gradient))
	case fill != nil && fill.HasValue:
		args = append(args, fill.Value)
	default:
		// OFD 未指定 FillColor 时，文字填充默认为黑色。
		// 不能使用透明色，否则 PDF 渲染器会输出非法的 NaN 颜色值破坏内容流。
		args = append(args, canvas.Black)
	}
	style := textFontStyle(object.Weight, object.Italic)
	args = append(args, canvasStyle(style), canvas.FontNormal)
	return family.Face(object.Size*2.83465, args...)
}

// textFontStyle 将 OFD 字重映射为 canvas 支持的标准字重。
func textFontStyle(weight int, italic bool) drawing.FontStyle {
	if weight <= 0 {
		weight = 400
	}

	style := drawing.FontRegular
	switch {
	case weight < 150:
		style = drawing.FontThin
	case weight < 250:
		style = drawing.FontExtraLight
	case weight < 350:
		style = drawing.FontLight
	case weight < 450:
		style = drawing.FontRegular
	case weight < 550:
		style = drawing.FontMedium
	case weight < 650:
		style = drawing.FontSemiBold
	case weight < 750:
		style = drawing.FontBold
	case weight < 850:
		style = drawing.FontExtraBold
	default:
		style = drawing.FontBlack
	}
	if italic {
		style |= drawing.FontItalic
	}
	return style
}

// directTextPath 处理 cmap 可通过 GlyphIndex 使用、但无法被文字整形器使用的
// 子集字体。普通文字仍会在后续使用 Canvas 的文字整形功能；这里对整形成功的
// 情形返回 nil（示意“无需按字形映射构建”），并利用缓存后的轮廓判断，避免
// 与后续 drawTextInline 重复计算同一字形轮廓。
func (p *Fonts) directTextPath(face *canvas.FontFace, value string) *canvas.Path {
	var path *canvas.Path
	if !containsPrivateGlyphRune(value) {
		path := p.shapedTextPath(face, value)
		if path != nil {
			return nil
		}
	}
	if face.Font == nil || face.Font.SFNT == nil {
		return nil
	}
	path = &canvas.Path{}
	advance := 0.0
	for _, r := range []rune(value) {
		glyphID := face.Font.GlyphIndex(r)
		if glyphID == 0 {
			return nil
		}
		if err := face.Font.GlyphPath(path, glyphID, face.PPEM(canvas.DefaultResolution), face.MmPerEm*advance, 0, face.MmPerEm, font.NoHinting); err != nil {
			return nil
		}
		advance += float64(face.Font.SFNT.GlyphAdvance(glyphID))
	}
	if path.Empty() {
		return nil
	}
	return path
}

// canvasStyle 把中性字体样式转换为 canvas 字体样式（两者取值一致）。
func canvasStyle(style drawing.FontStyle) canvas.FontStyle { return canvas.FontStyle(style) }
