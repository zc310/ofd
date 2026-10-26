package manifest

import (
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/go-text/typesetting/segmenter"
	"github.com/zc310/ofd/internal/fontrepair"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

func (r *templateRenderer) textFont(item *Item) (*sfnt.Font, error) {
	if item.Font == "" {
		if r.defaultFont == "" {
			name := "Template-Regular"
			for {
				found := false
				for _, resource := range r.manifest.Resources.Fonts {
					if resource.Name == name {
						found = true
						break
					}
				}
				if !found {
					break
				}
				name = "_" + name
			}
			r.defaultFont = name
			r.manifest.Resources.Fonts = append(r.manifest.Resources.Fonts, Font{Name: name, FontName: "Go Regular", Format: "ttf", DataBase64: base64.StdEncoding.EncodeToString(goregular.TTF)})
		}
		item.Font = r.defaultFont
	}
	if face := r.fonts[item.Font]; face != nil {
		return face, nil
	}
	for i := range r.manifest.Resources.Fonts {
		resource := &r.manifest.Resources.Fonts[i]
		if resource.Name != item.Font {
			continue
		}
		data, err := r.store.loadData(resource.File, resource.DataBase64)
		if err != nil {
			return nil, err
		}
		face, err := sfnt.Parse(data)
		if repaired, repairErr := fontrepair.Repair(data); repairErr == nil {
			if parsed, parseErr := sfnt.Parse(repaired); parseErr == nil {
				face, data, err = parsed, repaired, nil
			}
		}
		if err != nil {
			return nil, fmt.Errorf("字体 %q 需要可解析的独立 TTF/OTF 字体: %w", item.Font, err)
		}
		resource.File, resource.DataBase64 = "", base64.StdEncoding.EncodeToString(data)
		r.fonts[item.Font] = face
		return face, nil
	}
	return nil, fmt.Errorf("绑定字体 %q 需要在 resources.fonts 中提供完整字体，不能仅依赖原始资源 XML 或逻辑字体", item.Font)
}

// bindText 使用实际字形推进量重建 TextCode，清除导出对象的旧文字、增量和显式字形映射。
func (r *templateRenderer) bindText(item *Item, text string) error {
	return r.layoutText(item, text, nil)
}

// layoutText 可同时返回实际文字高度，供明细行试排与最终输出共用同一组字形位置。
func (r *templateRenderer) layoutText(item *Item, text string, measuredHeight *float64) error {
	b := item.Bind
	face, err := r.textFont(item)
	if err != nil {
		return err
	}
	size := item.Size
	if size == 0 {
		size = 4.2
	}
	scale := item.HScale
	if scale == 0 {
		scale = 1
	}
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		return errors.New("h_scale 必须为有限正数")
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var buffer sfnt.Buffer
	advances := map[rune]float64{}
	for _, char := range text {
		if char == '\n' {
			continue
		}
		if char < 0x20 || char == 0xFFFE || char == 0xFFFF {
			return fmt.Errorf("不支持的文字字符 U+%04X", char)
		}
		if _, ok := advances[char]; ok {
			continue
		}
		glyph, err := face.GlyphIndex(&buffer, char)
		if err != nil {
			return err
		}
		if glyph == 0 {
			return fmt.Errorf("字体 %q 缺少字符 %q（U+%04X），请更换完整字体资源", item.Font, char, char)
		}
		advance, err := face.GlyphAdvance(&buffer, glyph, 1000*64, font.HintingNone)
		if err != nil {
			return err
		}
		advances[char] = float64(advance) / 64000
	}
	metrics, err := face.Metrics(&buffer, 1000*64, font.HintingNone)
	if err != nil {
		return err
	}
	ascent, descent := float64(metrics.Ascent)/64000, float64(metrics.Descent)/64000
	width := func(s string, size float64) float64 {
		w := 0.0
		for _, char := range s {
			w += advances[char] * size * scale
		}
		return w
	}
	const tolerance = 1e-7
	lines := []string{text}
	if b.Overflow != "wrap" && strings.Contains(text, "\n") {
		return errors.New("多行内容需要 overflow: wrap")
	}
	switch b.Overflow {
	case "wrap":
		lines = nil
		for _, paragraph := range strings.Split(text, "\n") {
			if paragraph == "" {
				lines = append(lines, "")
				continue
			}
			var breaker segmenter.Segmenter
			breaker.Init([]rune(paragraph))
			iterator := breaker.LineIterator()
			line := ""
			for iterator.Next() {
				segment := string(iterator.Line().Text)
				candidate := line + segment
				if width(strings.TrimRightFunc(candidate, unicode.IsSpace), size) > item.Width+tolerance {
					if line != "" {
						lines = append(lines, strings.TrimRightFunc(line, unicode.IsSpace))
						line = ""
					}
					segment = strings.TrimLeftFunc(segment, unicode.IsSpace)
					if width(strings.TrimRightFunc(segment, unicode.IsSpace), size) > item.Width+tolerance {
						return errors.New("不可断开的文字段超出区域宽度")
					}
				}
				line += segment
			}
			lines = append(lines, strings.TrimRightFunc(line, unicode.IsSpace))
		}
	case "shrink":
		ratio := 1.0
		if w := width(text, size); w > 0 {
			ratio = math.Min(ratio, item.Width/w)
		}
		if h := (ascent + descent) * size; h > 0 {
			ratio = math.Min(ratio, item.Height/h)
		}
		size = math.Max(b.MinSize, size*ratio)
	case "ellipsis":
		if width(text, size) > item.Width+tolerance {
			glyph, err := face.GlyphIndex(&buffer, '…')
			if err != nil || glyph == 0 {
				return errors.New("字体不支持省略号 …")
			}
			advance, err := face.GlyphAdvance(&buffer, glyph, 1000*64, font.HintingNone)
			if err != nil {
				return err
			}
			advances['…'] = float64(advance) / 64000
			// 以字素簇为边界截断，避免拆开组合字符。
			var breaker segmenter.Segmenter
			breaker.Init([]rune(text))
			iterator := breaker.GraphemeIterator()
			prefix := ""
			for iterator.Next() {
				candidate := prefix + string(iterator.Grapheme().Text)
				if width(candidate+"…", size) > item.Width+tolerance {
					break
				}
				prefix = candidate
			}
			lines = []string{prefix + "…"}
		}
	}
	lineHeight := b.LineHeight
	if lineHeight == 0 {
		lineHeight = 1.2
	}
	step := math.Max(lineHeight, ascent+descent) * size
	height := (ascent+descent)*size + float64(len(lines)-1)*step
	if measuredHeight != nil {
		*measuredHeight = height
	}
	if height > item.Height+tolerance || b.MaxLines > 0 && len(lines) > b.MaxLines {
		return errors.New("文字超出区域高度或 max_lines 限制")
	}
	item.Value, item.TextCodes, item.CGTransforms, item.Size = "", nil, nil, size
	for i, line := range lines {
		w := width(line, size)
		if w > item.Width+tolerance {
			return errors.New("文字超出区域宽度")
		}
		if line == "" {
			continue
		}
		x := 0.0
		if b.Align == "center" {
			x = (item.Width - w) / 2
		} else if b.Align == "right" {
			x = item.Width - w
		}
		y := ascent*size + float64(i)*step
		chars := []rune(line)
		var delta []float64
		for _, char := range chars[:len(chars)-1] {
			delta = append(delta, advances[char]*size*scale)
		}
		item.TextCodes = append(item.TextCodes, TextCode{Value: line, X: &x, Y: &y, DeltaX: delta})
	}
	return nil
}
