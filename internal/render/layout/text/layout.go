package text

import (
	"math"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/spec"
)

// Source 标记文字来源，具体来源及收集策略由页面遍历层决定。
type Source string

// Character 是源字符在 OFD 坐标下的近似区域，单位为毫米。
type Character struct {
	Text   string
	X      float64
	Y      float64
	Width  float64
	Height float64
	Angle  float64
}

// Layout 描述一个 TextCode 及其源字符级几何信息。
type Layout struct {
	Source Source
	// TemplateID 非零表示字体来自该模板的独立资源视图。
	TemplateID uint64
	// EmbeddedFontLoaded 和 FallbackFontFamily 保留当前资源视图的字体选择，供文字覆盖层使用。
	EmbeddedFontLoaded bool
	FallbackFontFamily string
	Text               string
	X                  float64
	Y                  float64
	Width              float64
	// Height 为字体上伸与下延之和，无度量时使用 EM 高度。
	Height        float64
	Font          uint64
	Size          float64
	Weight        int
	ReadDirection int
	CharDirection int
	Bold          bool
	Italic        bool
	Glyphs        []Character
}

// Build 根据字体度量和对象定位构造源字符布局。face 使用对象原始字号，
// nil 表示无字体，使用 Boundary 宽度和 EM 高度兜底。输入对象和增量均不修改。
func Build(face Face, object spec.TextObject, code spec.TextCode, codePosition int) Layout {
	box := object.Boundary
	height := object.Size
	if height <= 0 {
		height = box.Height
	}
	ctmXScale, ctmYScale := 1.0, 1.0
	if object.CTM != nil {
		ctmXScale = math.Hypot((*object.CTM)[0], (*object.CTM)[1])
		ctmYScale = object.CTM.YScale()
	}
	// Size 是实际绘制字号，消费方据此建立文字覆盖层。声明字号可很小，
	// 再通过 CTM 放大（intro.ofd 的 Size=1、CTM=9.8778 即属此类）。
	effectiveSize := object.Size
	if ctmYScale > 0 {
		effectiveSize *= ctmYScale
	}
	layout := Layout{
		Text: code.Value, X: box.X + code.X, Y: box.Y + code.Y - height,
		Width: box.Width, Height: height, Size: effectiveSize, Font: uint64(object.Font),
		ReadDirection: object.ReadDirection, CharDirection: object.CharDirection,
		Bold: object.Weight >= 650, Italic: object.Italic,
	}
	widthOf := func(string) float64 { return 0 }
	// 行框使用字体排版高度，并非字符的实际轮廓边界。
	ascent, descent := height, 0.0
	if face != nil {
		widthOf = face.TextWidth
		if len(object.CGTransform) > 0 && CanRenderText(face, code.Value) {
			widthOf = func(value string) float64 {
				if fontrepair.ContainsGlyphRune(value) && face.GlyphIndex([]rune(value)[0]) == 0 {
					return 0
				}
				return face.TextWidth(value)
			}
		}
		a, d := face.VerticalMetrics()
		if finiteFloat(a) && finiteFloat(d) && finiteFloat(a+d) && a > 0 && d >= 0 {
			ascent, descent = a, d
		}
	}
	// GB/T 33190—2016 §8.1.5、§11.3 表46：字体度量和自然步进留在
	// 对象空间，原点只变换一次；字框分别使用 CTM 的 X/Y 轴长度。
	ascent *= ctmYScale
	descent *= ctmYScale
	layout.Height = ascent + descent
	runes := []rune(code.Value)
	fallbackWidth := 0.0
	if len(runes) > 0 {
		fallbackWidth = box.Width / float64(len(runes))
		if ctmXScale > 0 {
			fallbackWidth /= ctmXScale
		}
	}
	widths := make([]float64, len(runes))
	for index, value := range runes {
		widths[index] = widthOf(string(value))
		if widths[index] <= 0 {
			widths[index] = fallbackWidth
		}
	}
	transforms := models.ExpandCGTransformPositions(object.TextCode, object.CGTransform)
	applyCGTransformWidths(widths, runes, transforms, codePosition, widthOf)
	code = characterDeltas(object, code, transforms, codePosition, widths)
	posX, posY := code.X, code.Y
	angle := CharDirection(object)
	if object.CTM != nil {
		angle += object.CTM.RotationAngleDegrees()
	}
	for index, value := range runes {
		rawGlyphWidth := widths[index]
		glyphWidth := rawGlyphWidth * HScale(object) * ctmXScale
		x, y := posX, posY
		if object.CTM != nil {
			x, y = object.CTM.Transform(x, y)
		}
		layout.Glyphs = append(layout.Glyphs, Character{
			Text: string(value), X: box.X + x, Y: box.Y + y - ascent,
			Width: glyphWidth, Height: layout.Height, Angle: angle,
		})
		if index+1 < len(runes) {
			deltaX, deltaY := Advance(rawGlyphWidth, object, code, index)
			posX += deltaX
			posY += deltaY
		}
	}
	if len(layout.Glyphs) > 0 {
		layout.X = layout.Glyphs[0].X
		layout.Y = layout.Glyphs[0].Y
	}
	return layout
}

// characterDeltas 将字形定位分配给搜索层源字符：连字按相邻字形距离等分，
// 一字符的多个字形合并为一个字符位置。绘制仍使用原字形队列。
func characterDeltas(object spec.TextObject, code spec.TextCode, transforms []spec.CTCGTransform, codePosition int, widths []float64) spec.TextCode {
	if len(transforms) == 0 {
		return code
	}
	glyphs := CodeGlyphs(nil, []rune(code.Value), transforms, codePosition)
	if !UsesGlyphDeltas(code, glyphs) {
		return code
	}
	xs, ys := make([]float64, len(widths)), make([]float64, len(widths))
	x, y := 0.0, 0.0
	for i := 0; i < len(glyphs); {
		start := glyphs[i].CodePosition
		end := i + 1
		for end < len(glyphs) && glyphs[end].CodePosition == start {
			end++
		}
		nextCode := len(widths)
		if end < len(glyphs) {
			nextCode = glyphs[end].CodePosition
		}
		nextX, nextY := x, y
		if end < len(glyphs) {
			for j := i; j < end; j++ {
				nextX += valueAt(code.DeltaX, glyphs[j].GlyphPosition)
				nextY += valueAt(code.DeltaY, glyphs[j].GlyphPosition)
			}
		}
		for j := start; j < nextCode; j++ {
			if end < len(glyphs) {
				fraction := float64(j-start) / float64(nextCode-start)
				xs[j], ys[j] = x+(nextX-x)*fraction, y+(nextY-y)*fraction
			} else {
				xs[j], ys[j] = nextX, nextY
				dx, dy := ReadAdvance(widths[j]*HScale(object), object.ReadDirection)
				nextX, nextY = nextX+dx, nextY+dy
			}
		}
		x, y, i = nextX, nextY, end
	}
	code.DeltaX, code.DeltaY = make(spec.StArray[float64], len(widths)-1), make(spec.StArray[float64], len(widths)-1)
	for i := range code.DeltaX {
		code.DeltaX[i], code.DeltaY[i] = xs[i+1]-xs[i], ys[i+1]-ys[i]
	}
	return code
}

// 度量使用与绘制相同的字形私有码位，非一对一变换将总字宽分配给源字符。
func applyCGTransformWidths(widths []float64, runes []rune, transforms []spec.CTCGTransform, codePosition int, widthOf func(string) float64) {
	if len(transforms) == 0 || len(widths) == 0 {
		return
	}
	byPosition := transformMapPool.Get().(map[int]spec.CTCGTransform) //nolint:forcetypeassert // 私有池的 New 和两处 Put 均使用同一 map 类型。
	defer transformMapPool.Put(byPosition)
	clear(byPosition)
	for _, transform := range transforms {
		byPosition[transform.CodePosition] = transform
	}
	for index := 0; index < len(runes); {
		transform, ok := byPosition[codePosition+index]
		if !ok {
			index++
			continue
		}
		count := transform.CodeCount
		if count <= 0 {
			count = 1
		}
		if remaining := len(runes) - index; count > remaining {
			count = remaining
		}
		glyphs := transform.Glyphs
		if transform.GlyphCount > 0 && transform.GlyphCount < len(glyphs) {
			glyphs = glyphs[:transform.GlyphCount]
		}
		advances := make([]float64, 0, len(glyphs))
		for _, glyph := range glyphs {
			if glyph >= 0 && glyph <= 0xffff {
				if advance := widthOf(string(fontrepair.GlyphRune(uint16(glyph)))); advance > 0 {
					advances = append(advances, advance)
				}
			}
		}
		switch {
		case len(advances) == count:
			for offset, advance := range advances {
				widths[index+offset] = advance
			}
		case len(advances) > 0:
			advance := 0.0
			for _, width := range advances {
				advance += width
			}
			advance /= float64(count)
			for offset := 0; offset < count; offset++ {
				widths[index+offset] = advance
			}
		}
		index += count
	}
}
