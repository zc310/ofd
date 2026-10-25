package layout

import (
	"fmt"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
)

func (e *engine) prepareAuthorFixed(p authorBlock) (authorBlock, error) {
	fixed := *p.block.Fixed
	for _, size := range []float64{fixed.Width, fixed.Height} {
		if size < 0 || math.IsNaN(size) || math.IsInf(size, 0) {
			return p, fmt.Errorf("fixed 尺寸必须为有限非负数")
		}
	}
	if fixed.Width == 0 {
		fixed.Width = e.contentWidth
	}
	if fixed.Height <= 0 {
		return p, fmt.Errorf("fixed.height 必须为正数")
	}
	if fixed.Width > e.contentWidth+0.001 {
		return p, fmt.Errorf("fixed.width 超出版心")
	}
	switch fixed.Align {
	case "", "left", "center", "right":
	default:
		return p, fmt.Errorf("fixed.align 必须为 left、center 或 right")
	}
	items, err := e.prepareFixedItems(fixed.Items, fixed.Width, fixed.Height, p.style.Font, 0)
	if err != nil {
		return p, fmt.Errorf("fixed: %w", err)
	}
	fixed.Items = items
	p.fixed = &fixed
	p, err = e.prepareAuthorFigureCaption(p, fixed.Caption)
	if err != nil {
		return p, err
	}
	if p.height() > e.contentHeight+0.001 {
		return p, fmt.Errorf("固定区域与图注总高超过整页版心")
	}
	return p, nil
}

func (e *engine) prepareFixedItems(items []creator.Item, width, height float64, inherited string, depth int) ([]creator.Item, error) {
	if depth > 64 {
		return nil, fmt.Errorf("page-block 嵌套超过 64 层或包含循环")
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("items 不能为空")
	}
	result := make([]creator.Item, len(items))
	for i, item := range items {
		if err := e.author.ctx.Err(); err != nil {
			return nil, err
		}
		var box creator.Box
		var ctm *creator.CTM
		switch v := item.(type) {
		case creator.Text:
			if v.Font == "" {
				v.Font = inherited
			}
			if e.author.faces[v.Font] == nil {
				return nil, fmt.Errorf("items[%d]: 字体 %q 未注册", i, v.Font)
			}
			if v.Size <= 0 || math.IsNaN(v.Size) || math.IsInf(v.Size, 0) {
				return nil, fmt.Errorf("items[%d]: 文字字号必须为有限正数（毫米）", i)
			}
			texts := []string{v.Value}
			for _, code := range v.TextCodes {
				texts = append(texts, code.Value)
			}
			for _, text := range texts {
				if err := ValidateFlowText(text); err != nil {
					return nil, fmt.Errorf("items[%d]: %w", i, err)
				}
			}
			if err := e.validateFixedTextGlyphs(v); err != nil {
				return nil, fmt.Errorf("items[%d]: %w", i, err)
			}
			// 水平 Value 简写尚未给出基线，按实际嵌入字体把基线放在声明边界内，
			// 避免默认落在底边后裁掉 Q、g 或下划线的下伸部分。显式文字定位不改写。
			if len(v.TextCodes) == 0 && v.Value != "" && v.ReadDirection == 0 && v.CharDirection == 0 {
				var buf sfnt.Buffer
				metrics, err := e.author.faces[v.Font].Metrics(&buf, 1000*64, font.HintingNone)
				if err != nil {
					return nil, fmt.Errorf("items[%d]: 字体度量失败: %w", i, err)
				}
				ascent, descent := float64(metrics.Ascent)/64000*v.Size, float64(metrics.Descent)/64000*v.Size
				if ascent+descent > v.Height+0.001 {
					return nil, fmt.Errorf("items[%d]: 文字高度不足以容纳字体，请增加 height 或显式设置 text_codes", i)
				}
				x, y := 0.0, (v.Height+ascent-descent)/2
				v.TextCodes = []creator.TextCode{{Value: v.Value, X: &x, Y: &y}}
			}
			box, ctm, item = creator.Box{X: v.X, Y: v.Y, Width: v.Width, Height: v.Height}, v.CTM, v
		case creator.Path:
			if _, err := models.ParsePathData(v.Data); err != nil {
				return nil, fmt.Errorf("items[%d].data: %w", i, err)
			}
			box, ctm = creator.Box{X: v.X, Y: v.Y, Width: v.Width, Height: v.Height}, v.CTM
		case creator.Image:
			box, ctm = creator.Box{X: v.X, Y: v.Y, Width: v.Width, Height: v.Height}, v.CTM
		case creator.Composite:
			box, ctm = creator.Box{X: v.X, Y: v.Y, Width: v.Width, Height: v.Height}, v.CTM
		case creator.PageBlock:
			nested, err := e.prepareFixedItems(v.Items, width, height, inherited, depth+1)
			if err != nil {
				return nil, fmt.Errorf("items[%d]: %w", i, err)
			}
			result[i] = creator.PageBlock{Items: nested}
			continue
		default:
			return nil, fmt.Errorf("items[%d]: 固定区域不支持图元类型 %T，可用 Text、Path、Image、Composite、PageBlock", i, item)
		}
		for _, value := range []float64{box.X, box.Y, box.Width, box.Height} {
			if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("items[%d]: 边界必须为有限非负数", i)
			}
		}
		if box.Width <= 0 || box.Height <= 0 || box.X+box.Width > width+0.001 || box.Y+box.Height > height+0.001 {
			return nil, fmt.Errorf("items[%d]: 图元边界必须为正且位于固定区域内", i)
		}
		if ctm != nil {
			for _, value := range ctm {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					return nil, fmt.Errorf("items[%d].ctm: 变换必须为有限数值", i)
				}
			}
		}
		result[i] = item
	}
	return result, nil
}

// 固定文字使用声明的字体和原始定位，不自动切换回退字体。
// GB/T 33190—2016 §11.4 表 48：显式字形映射覆盖的字符不再依赖 Unicode cmap。
func (e *engine) validateFixedTextGlyphs(text creator.Text) error {
	runes := []rune(text.Value)
	if len(text.TextCodes) > 0 {
		runes = nil
		for _, code := range text.TextCodes {
			runes = append(runes, []rune(code.Value)...)
		}
	}
	face := e.author.faces[text.Font]
	var mapped []bool
	if len(text.CGTransforms) > 0 {
		mapped = make([]bool, len(runes))
	}
	for i, transform := range text.CGTransforms {
		if err := e.author.ctx.Err(); err != nil {
			return err
		}
		position, count := transform.CodePosition, transform.CodeCount
		if count == 0 {
			count = 1
		}
		if position < 0 || position >= len(runes) || count < 1 || count > len(runes)-position {
			return fmt.Errorf("CGTransform[%d]: 字符范围无效", i)
		}
		glyphCount := transform.GlyphCount
		if glyphCount == 0 {
			glyphCount = 1
		}
		if glyphCount < 1 || glyphCount > len(transform.Glyphs) {
			return fmt.Errorf("CGTransform[%d]: 字形数量无效", i)
		}
		for _, glyph := range transform.Glyphs[:glyphCount] {
			if glyph < 0 || glyph >= face.NumGlyphs() {
				return fmt.Errorf("CGTransform[%d]: 字形编号超出字体范围: %d", i, glyph)
			}
		}
		for j := position; j < position+count; j++ {
			mapped[j] = true
		}
	}
	var buffer sfnt.Buffer
	for position, r := range runes {
		if err := e.author.ctx.Err(); err != nil {
			return err
		}
		if len(mapped) > 0 && mapped[position] || r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		glyph, err := face.GlyphIndex(&buffer, r)
		if err != nil {
			return fmt.Errorf("字体 %q 的字符 %q（U+%04X）查询失败: %w", text.Font, r, r, err)
		}
		if glyph == 0 {
			return fmt.Errorf("字体 %q 缺少字符 %q（U+%04X）", text.Font, r, r)
		}
	}
	return nil
}

func (e *engine) placeAuthorFixed(p *authorBlock) {
	e.ensureHeight(p.height())
	if e.authorHasContent() {
		e.space(p.style.SpaceBefore)
	}
	x := e.contentLeft
	switch p.fixed.Align {
	case "center":
		x += (e.contentWidth - p.fixed.Width) / 2
	case "right":
		x += e.contentWidth - p.fixed.Width
	}
	e.recordAuthorAnchor(p.block.ID)
	// GB/T 33190—2016 §8.1：只平移页面空间中的 Boundary，局部 CTM、裁剪和路径坐标保持原语义。
	// 使用标准 §7.7 表 16 的 PageBlock 容器组织图元。
	e.add(creator.PageBlock{Items: offsetFixedItems(p.fixed.Items, x, e.opts.PageHeight-e.y)})
	e.y -= p.fixed.Height
	e.drawAuthorFigureCaption(p)
}

func offsetFixedItems(items []creator.Item, x, y float64) []creator.Item {
	result := make([]creator.Item, len(items))
	for i, item := range items {
		switch v := item.(type) {
		case creator.Text:
			v.X += x
			v.Y += y
			result[i] = v
		case creator.Path:
			v.X += x
			v.Y += y
			result[i] = v
		case creator.Image:
			v.X += x
			v.Y += y
			result[i] = v
		case creator.Composite:
			v.X += x
			v.Y += y
			result[i] = v
		case creator.PageBlock:
			result[i] = creator.PageBlock{Items: offsetFixedItems(v.Items, x, y)}
		}
	}
	return result
}

func (e *engine) prepareAuthorFigureCaption(p authorBlock, text string) (authorBlock, error) {
	if text == "" {
		return p, nil
	}
	style := mergeTextStyle(e.author.opts.Theme.Body, e.author.opts.Theme.Caption)
	if err := validateTextStyle(style); err != nil {
		return p, err
	}
	p.lines = e.authorLines([]Inline{{Text: text}}, style, e.contentWidth, 0)
	p.extraHeight = style.SpaceBefore
	for _, line := range p.lines {
		if e.lineWidth(line.atoms) > e.contentWidth+0.001 {
			return p, fmt.Errorf("图注文本超过行宽")
		}
	}
	return p, nil
}

func (e *engine) drawAuthorFigureCaption(p *authorBlock) {
	style := mergeTextStyle(e.author.opts.Theme.Body, e.author.opts.Theme.Caption)
	if len(p.lines) > 0 {
		e.space(style.SpaceBefore)
	}
	for _, line := range p.lines {
		e.drawAuthorLine(line, style, e.contentLeft, e.contentWidth, e.y)
		e.y -= line.height
	}
	e.space(p.style.SpaceAfter)
}
