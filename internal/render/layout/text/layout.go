package text

import (
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
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
	Text   string
	X      float64
	Y      float64
	Width  float64
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

// Build 根据字体度量和对象定位构造源字符布局。face 的字号应已计入 CTM.YScale，
// nil 表示无字体，使用 Boundary 宽度和 EM 高度兜底。输入对象和增量均不修改。
func Build(face Face, object models.TextObject, code models.TextCode, codePosition int) Layout {
	box := object.Boundary
	height := object.Size
	if height <= 0 {
		height = box.Height
	}
	ctmYScale := 1.0
	if object.CTM != nil {
		ctmYScale = object.CTM.YScale()
		if ctmYScale > 0 {
			height *= ctmYScale
		}
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
	layout.Height = ascent + descent
	runes := []rune(code.Value)
	fallbackWidth := 0.0
	if len(runes) > 0 {
		fallbackWidth = box.Width / float64(len(runes))
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
		// 字体度量已经包含 CTM.YScale，原点再经 CTM 映射，宽度不重复缩放。
		glyphWidth := rawGlyphWidth * HScale(object)
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
func characterDeltas(object models.TextObject, code models.TextCode, transforms []models.CTCGTransform, codePosition int, widths []float64) models.TextCode {
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
	code.DeltaX, code.DeltaY = make(models.StArrayF, len(widths)-1), make(models.StArrayF, len(widths)-1)
	for i := range code.DeltaX {
		code.DeltaX[i], code.DeltaY[i] = xs[i+1]-xs[i], ys[i+1]-ys[i]
	}
	return code
}

// 度量使用与绘制相同的字形私有码位，非一对一变换将总字宽分配给源字符。
func applyCGTransformWidths(widths []float64, runes []rune, transforms []models.CTCGTransform, codePosition int, widthOf func(string) float64) {
	if len(transforms) == 0 || len(widths) == 0 {
		return
	}
	byPosition := transformMapPool.Get().(map[int]models.CTCGTransform) //nolint:forcetypeassert // 私有池的 New 和两处 Put 均使用同一 map 类型。
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
