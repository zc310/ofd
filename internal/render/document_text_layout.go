package render

import (
	"github.com/zc310/ofd/internal/fontrepair"
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
	Source TextSource
	Text   string
	X      float64
	Y      float64
	Width  float64
	// Height 是字体上伸与下延之和，用于覆盖层行高；无字体度量时按 EM 高度兜底。
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
// 按背景模板、正文、前景模板的绘制顺序返回，与 TextValues 的 run 索引一致。
func (p *Document) TextLayouts(page *parser.Page) []TextLayout {
	return p.TextLayoutsWithVisibility(page, PageVisibility{})
}

// TextLayoutsWithVisibility 返回过滤正文图层和模板后的布局。
func (p *Document) TextLayoutsWithVisibility(page *parser.Page, visibility PageVisibility) []TextLayout {
	layouts, _ := p.TextLayoutsWithVisibilityE(page, visibility)
	return layouts
}

// TextLayoutsWithVisibilityE 返回过滤后的文字布局，并保留页面或模板加载错误。
func (p *Document) TextLayoutsWithVisibilityE(page *parser.Page, visibility PageVisibility) ([]TextLayout, error) {
	return p.textLayouts(page, nil, visibility)
}

// TextLayoutsWithOptionsE 按同一来源策略返回文字布局，保留资源加载错误。
func (p *Document) TextLayoutsWithOptionsE(page *parser.Page, visibility PageVisibility, options TextOptions) ([]TextLayout, error) {
	return p.textLayoutsOptions(page, nil, visibility, options)
}

// TextLayoutsForRuns 返回指定 run 的字符级布局，结果按页面绘制顺序排列。
// run 索引与 TextValues 返回的顺序一致。
func (p *Document) TextLayoutsForRuns(page *parser.Page, runIndices []int) []TextLayout {
	return p.TextLayoutsForRunsWithVisibility(page, runIndices, PageVisibility{})
}

// TextLayoutsForRunsWithVisibility 的 run 索引与过滤后的 TextValues 保持一致。
func (p *Document) TextLayoutsForRunsWithVisibility(page *parser.Page, runIndices []int, visibility PageVisibility) []TextLayout {
	layouts, _ := p.TextLayoutsForRunsWithVisibilityE(page, runIndices, visibility)
	return layouts
}

// TextLayoutsForRunsWithVisibilityE 返回指定 run 的过滤后布局，并保留加载错误。
func (p *Document) TextLayoutsForRunsWithVisibilityE(page *parser.Page, runIndices []int, visibility PageVisibility) ([]TextLayout, error) {
	return p.TextLayoutsForRunsWithOptionsE(page, runIndices, visibility, TextOptions{})
}

// TextLayoutsForRunsWithOptionsE 的 run 索引与相同选项的 TextValuesWithOptionsE 一致。
func (p *Document) TextLayoutsForRunsWithOptionsE(page *parser.Page, runIndices []int, visibility PageVisibility, options TextOptions) ([]TextLayout, error) {
	if len(runIndices) == 0 {
		return nil, nil
	}
	selected := make(map[int]struct{}, len(runIndices))
	for _, index := range runIndices {
		if index >= 0 {
			selected[index] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	return p.textLayoutsOptions(page, selected, visibility, options)
}

func (p *Document) textLayouts(page *parser.Page, selected map[int]struct{}, visibility PageVisibility) ([]TextLayout, error) {
	return p.textLayoutsOptions(page, selected, visibility, TextOptions{})
}

func (p *Document) textLayoutsOptions(page *parser.Page, selected map[int]struct{}, visibility PageVisibility, options TextOptions) ([]TextLayout, error) {
	layouts := make([]TextLayout, 0)
	runIndex := 0
	err := p.walkPageText(page, visibility, options, func(frame contentFrame, item models.PageItem) {
		if item.Kind != models.PageItemText || !item.Text.VisibleValue() {
			return
		}
		var local []TextLayout
		collectObjectTextLayouts(*item.Text, &local, frame.document, selected, &runIndex)
		for _, layout := range local {
			layouts = append(layouts, transformTextLayout(layout, frame))
		}
	})
	if err != nil {
		return nil, err
	}
	return layouts, nil
}

// TextValues 返回页面中可见的非空 TextCode 文本，顺序与 TextLayouts 一致。
// 搜索等只需读取文字内容的调用方可避免构建字符级几何信息。
func (p *Document) TextValues(page *parser.Page) []string {
	return p.TextValuesWithVisibility(page, PageVisibility{})
}

// TextValuesWithVisibility 返回过滤正文图层和模板后的文字，供搜索索引使用。
func (p *Document) TextValuesWithVisibility(page *parser.Page, visibility PageVisibility) []string {
	values, _ := p.TextValuesWithVisibilityE(page, visibility)
	return values
}

// TextValuesWithVisibilityE 返回过滤后的文字值，并保留页面或模板加载错误。
func (p *Document) TextValuesWithVisibilityE(page *parser.Page, visibility PageVisibility) ([]string, error) {
	return p.TextValuesWithOptionsE(page, visibility, TextOptions{})
}

// TextValuesWithOptionsE 与布局共用来源、遍历顺序和复合对象展开规则。
func (p *Document) TextValuesWithOptionsE(page *parser.Page, visibility PageVisibility, options TextOptions) ([]string, error) {
	values := make([]string, 0)
	err := p.walkPageText(page, visibility, options, func(_ contentFrame, item models.PageItem) {
		if item.Kind != models.PageItemText || !item.Text.VisibleValue() {
			return
		}
		for _, code := range item.Text.TextCode {
			if code.Value != "" {
				values = append(values, code.Value)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

func collectTextValuesBlock(block *models.CTPageBlock, values *[]string) {
	if block == nil {
		return
	}
	for _, item := range block.Items {
		if !pageItemPresent(item) {
			continue
		}
		switch item.Kind {
		case models.PageItemText:
			if !item.Text.VisibleValue() {
				continue
			}
			for _, code := range item.Text.TextCode {
				if code.Value != "" {
					*values = append(*values, code.Value)
				}
			}
		case models.PageItemBlock:
			collectTextValuesBlock(&item.Block.CTPageBlock, values)
		}
	}
}

func collectTextLayoutBlock(block *models.CTPageBlock, layouts *[]TextLayout, document *Document, selected map[int]struct{}, runIndex *int) {
	if block == nil {
		return
	}
	for _, item := range block.Items {
		if !pageItemPresent(item) {
			continue
		}
		switch item.Kind {
		case models.PageItemText:
			if !item.Text.VisibleValue() {
				continue
			}
			collectObjectTextLayouts(*item.Text, layouts, document, selected, runIndex)
		case models.PageItemBlock:
			collectTextLayoutBlock(&item.Block.CTPageBlock, layouts, document, selected, runIndex)
		}
	}
}

// 同一文字对象的全部 TextCode 使用相同字体选择，只在确实请求布局时创建一次
// 字体面并持锁度量；不跨对象复用，以保留按实际文字覆盖选择回退字体的语义。
func collectObjectTextLayouts(object models.TextObject, layouts *[]TextLayout, document *Document, selected map[int]struct{}, runIndex *int) {
	var face FontFace
	var release func()
	defer func() {
		if release != nil {
			release()
		}
	}()
	position := 0
	for _, code := range object.TextCode {
		if code.Value != "" {
			_, wanted := selected[*runIndex]
			if selected == nil || wanted {
				if release == nil {
					face, release = loadTextLayoutFace(document, object)
				}
				*layouts = append(*layouts, buildTextLayoutWithFace(face, object, code, position))
			}
			*runIndex = *runIndex + 1
		}
		position += len([]rune(code.Value))
	}
}

func loadTextLayoutFace(document *Document, object models.TextObject) (FontFace, func()) {
	if document == nil || document.fonts == nil {
		return nil, func() {}
	}
	family, err := document.fonts.LoadFont(object.Font)
	if err != nil || family == nil {
		return nil, func() {}
	}
	lock := document.fonts.RenderLock(family)
	lock.Lock()
	if object.CTM != nil {
		if scale := object.CTM.YScale(); scale > 0 {
			object.Size *= scale
		}
	}
	return document.fonts.FaceObject(family, object, nil), lock.Unlock
}

func buildTextLayout(document *Document, object models.TextObject, code models.TextCode, codePosition int) TextLayout {
	face, release := loadTextLayoutFace(document, object)
	defer release()
	return buildTextLayoutWithFace(face, object, code, codePosition)
}

func buildTextLayoutWithFace(face FontFace, object models.TextObject, code models.TextCode, codePosition int) TextLayout {
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
	// Size 必须是**实际绘制尺寸**，而不是 Object 里声明的原始 Size。
	// OFD 允许用很小的 Size 配一个放大倍数的 CTM 来排大标题——intro.ofd 里
	// 就是 Size="1" 配 CTM="9.8778 0 0 9.8778 0 0"，画出 9.88mm 的字。
	// 这里若原样返回 object.Size，消费方拿它换算字号就会小 9.88 倍：
	// HTML 文字层会是一条 1mm 的细线，WASM 阅读器同理。渲染路径的
	// textCTMLinearMatrix 也按同一系数放大，两者必须一致。
	effectiveSize := object.Size
	if ctmYScale > 0 {
		effectiveSize *= ctmYScale
	}
	layout := TextLayout{
		Text:          code.Value,
		X:             box.X + code.X,
		Y:             box.Y + code.Y - height,
		Width:         box.Width,
		Height:        height,
		Size:          effectiveSize,
		Font:          uint64(object.Font),
		ReadDirection: object.ReadDirection,
		CharDirection: object.CharDirection,
		Bold:          object.Weight >= 650,
		Italic:        object.Italic,
	}

	widthOf := func(string) float64 { return 0 }
	// 字符框和整段覆盖层使用相同的字体排版高度。它是行框的近似范围，
	// 不代表每个字符的实际轮廓；不支持度量时保留基线上方的 EM 盒。
	ascent, descent := height, 0.0
	if face != nil {
		widthOf = face.TextWidth
		if len(object.CGTransform) > 0 && fontCanRenderText(face, code.Value) {
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
	code = textCodeCharacterDeltas(object, code, transforms, codePosition, widths)
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
			Y:      box.Y + y - ascent,
			Width:  glyphWidth,
			Height: layout.Height,
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

// textCodeCharacterDeltas 将字形定位分配给搜索层的源字符。连字中的字符共享
// 同一个字形，按相邻字形的距离等分；一字符的多个字形在此合并为一个字符位置。
// 绘制仍使用原字形队列，不能把这份字符级近似定位写回只读页面内容。
func textCodeCharacterDeltas(object models.TextObject, code models.TextCode, transforms []models.CTCGTransform, codePosition int, widths []float64) models.TextCode {
	if len(transforms) == 0 {
		return code
	}
	glyphs := textCodeGlyphs(nil, []rune(code.Value), transforms, codePosition)
	if !textCodeUsesGlyphDeltas(code, glyphs) {
		return code
	}
	xs, ys := make([]float64, len(widths)), make([]float64, len(widths))
	x, y := 0.0, 0.0
	for i := 0; i < len(glyphs); {
		start := glyphs[i].codePosition
		end := i + 1
		for end < len(glyphs) && glyphs[end].codePosition == start {
			end++
		}
		nextCode := len(widths)
		if end < len(glyphs) {
			nextCode = glyphs[end].codePosition
		}
		nextX, nextY := x, y
		if end < len(glyphs) {
			for j := i; j < end; j++ {
				nextX += valueAt(code.DeltaX, glyphs[j].glyphPosition)
				nextY += valueAt(code.DeltaY, glyphs[j].glyphPosition)
			}
		}
		for j := start; j < nextCode; j++ {
			if end < len(glyphs) {
				fraction := float64(j-start) / float64(nextCode-start)
				xs[j], ys[j] = x+(nextX-x)*fraction, y+(nextY-y)*fraction
			} else {
				xs[j], ys[j] = nextX, nextY
				dx, dy := textReadAdvance(widths[j]*textHScale(object), object.ReadDirection)
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

// applyCGTransformWidths 在度量每个源字符时使用渲染器实际绘制的字形，
// 从而保留搜索所需的源字符到字形对应关系。一个变换可能将多个源字符映射
// 到一个字形，因此该字形的步进宽度需要分摊到这些源字符上。
func applyCGTransformWidths(widths []float64, runes []rune, transforms []models.CTCGTransform, codePosition int, widthOf func(string) float64) {
	if len(transforms) == 0 || len(widths) == 0 {
		return
	}
	byPosition := transformMapPool.Get().(map[int]models.CTCGTransform)
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
				// 渲染器使用转换为私有字符的字形 ID。
				// 查询宽度时必须使用相同的值，而不是源文本。
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
			// 非一对一变换无法给出逐字符边界，将所有字形的总步进平均分配。
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
