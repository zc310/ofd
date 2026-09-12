package render

import (
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// GlyphLayout 描述一个文字字符在页面上的近似区域。坐标单位为毫米，
// 使用页面左上角作为原点。
type GlyphLayout struct {
	Text   string
	X      float64
	Y      float64
	Width  float64
	Height float64
	Angle  float64
}

// TextLayout 描述一个 TextCode 及其字符级几何信息。
type TextLayout struct {
	Text          string
	X             float64
	Y             float64
	Width         float64
	Height        float64
	Font          uint64
	Size          float64
	Weight        int
	ReadDirection int
	CharDirection int
	Bold          bool
	Italic        bool
	Glyphs        []GlyphLayout
}

// TextLayouts 返回页面文字的字符级布局。字符步进使用渲染器的字体度量、
// DeltaX/DeltaY、ReadDirection 和 CTM 基础计算，避免网页字体度量造成高亮偏移。
func (p *Document) TextLayouts(page *parser.Page) []TextLayout {
	if p == nil || page == nil {
		return nil
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return nil
	}
	defer lease.Release()
	layouts := make([]TextLayout, 0)
	content := lease.Content()
	if content == nil {
		return nil
	}
	for _, template := range content.Template {
		if content := p.Document.GetTemplate(models.StID(template.TemplateID)); content != nil {
			collectTextLayouts(content.Content, &layouts, p)
		}
	}
	if content.Content != nil {
		for _, layer := range content.Content.Layer {
			if layer != nil {
				collectTextLayoutBlock(&layer.CTPageBlock, &layouts, p)
			}
		}
	}
	return layouts
}

func collectTextLayouts(content *models.Content, layouts *[]TextLayout, document *Document) {
	if content == nil {
		return
	}
	for _, layer := range content.Layer {
		if layer != nil {
			collectTextLayoutBlock(&layer.CTPageBlock, layouts, document)
		}
	}
}

func collectTextLayoutBlock(block *models.CTPageBlock, layouts *[]TextLayout, document *Document) {
	if block == nil {
		return
	}
	for _, item := range block.Items {
		switch item.Kind {
		case models.PageItemText:
			if !item.Text.VisibleValue() {
				continue
			}
			codePosition := 0
			for _, code := range item.Text.TextCode {
				if code.Value != "" {
					*layouts = append(*layouts, buildTextLayout(document, item.Text, code, codePosition))
				}
				codePosition += len([]rune(code.Value))
			}
		case models.PageItemBlock:
			collectTextLayoutBlock(&item.Block.CTPageBlock, layouts, document)
		}
	}
}

func buildTextLayout(document *Document, object models.TextObject, code models.TextCode, codePosition int) TextLayout {
	box := object.Boundary
	height := object.Size
	if height <= 0 {
		height = box.Height
	}
	// 渲染器会先按 CTM 的 Y 轴缩放字号，再进行度量。
	ctmYScale := 1.0
	if object.CTM != nil {
		ctmYScale = object.CTM.YScale()
		if ctmYScale > 0 {
			height *= ctmYScale
		}
	}
	layout := TextLayout{
		Text:          code.Value,
		X:             box.X + code.X,
		Y:             box.Y + code.Y - height,
		Width:         box.Width,
		Height:        height,
		Font:          uint64(object.Font),
		Size:          object.Size,
		Weight:        object.Weight,
		ReadDirection: object.ReadDirection,
		CharDirection: object.CharDirection,
		Bold:          object.Weight >= 650,
		Italic:        object.Italic,
	}

	widthOf := func(string) float64 { return 0 }
	if document != nil && document.fonts != nil {
		if family, err := document.fonts.LoadFont(object.Font); err == nil && family != nil {
			fontLock := document.fonts.renderLock(family)
			fontLock.Lock()
			defer fontLock.Unlock()
			fontObject := object
			if ctmYScale > 0 {
				fontObject.Size *= ctmYScale
			}
			face := buildTextFace(family, fontObject, nil)
			widthOf = face.TextWidth
		}
	}
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
	applyCGTransformWidths(widths, runes, object.CGTransform, codePosition, widthOf)
	posX, posY := code.X, code.Y
	angle := textCharDirectionDegrees(object)
	if object.CTM != nil {
		angle += object.CTM.RotationAngleDegrees()
	}
	for index, value := range runes {
		rawGlyphWidth := widths[index]
		// 上面的字号度量已经包含 CTM.YScale。渲染器会将矩阵应用到字形原点，
		// 不会再次沿 X 轴缩放字形宽度。
		glyphWidth := rawGlyphWidth * textHScale(object)
		x, y := posX, posY
		if object.CTM != nil {
			x, y = object.CTM.Transform(x, y)
		}
		layout.Glyphs = append(layout.Glyphs, GlyphLayout{
			Text:   string(value),
			X:      box.X + x,
			Y:      box.Y + y - height,
			Width:  glyphWidth,
			Height: height,
			Angle:  angle,
		})
		if index+1 < len(runes) {
			deltaX, deltaY := textAdvance(rawGlyphWidth, object, code, index)
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

// applyCGTransformWidths 在度量每个源字符时使用渲染器实际绘制的字形，
// 从而保留搜索所需的源字符到字形对应关系。一个变换可能将多个源字符映射
// 到一个字形，因此该字形的步进宽度需要分摊到这些源字符上。
func applyCGTransformWidths(widths []float64, runes []rune, transforms []models.CTCGTransform, codePosition int, widthOf func(string) float64) {
	if len(transforms) == 0 || len(widths) == 0 {
		return
	}
	byPosition := make(map[int]models.CTCGTransform, len(transforms))
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
				// 渲染器使用转换为私有字符的字形 ID。
				// 查询宽度时必须使用相同的值，而不是源文本。
				if advance := widthOf(string(fontfix.GlyphRune(uint16(glyph)))); advance > 0 {
					advances = append(advances, advance)
				}
			}
		}
		switch {
		case len(advances) == count:
			for offset, advance := range advances {
				widths[index+offset] = advance
			}
		case len(advances) == 1:
			// 一个字形可能表示多个源字符。此时无法得到每个字符的精确边界，
			// 因此将字形步进宽度平均分配。
			advance := advances[0] / float64(count)
			for offset := 0; offset < count; offset++ {
				widths[index+offset] = advance
			}
		}
		index += count
	}
}
