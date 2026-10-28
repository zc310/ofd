package text

import (
	"strings"
	"sync"
	"unicode"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
)

// Face 提供布局所需的字体查询。调用方负责字体选择和必要的锁保护。
type Face interface {
	TextWidth(value string) float64
	GlyphIndex(r rune) uint16
	IsCFF() bool
	VerticalMetrics() (float64, float64)
}

// Glyph 记录实际绘制值及其对应的源字符、字形定位范围。
// Value 可以是 Unicode 文本或 fontrepair 字形私有码位，Semantic 保留其源文字。
type Glyph struct {
	Value         string
	Semantic      string
	CodePosition  int
	GlyphPosition int
	GlyphCount    int
}

// UsesGlyphDeltas 判断定位数组是否明确对应变换后的字形数量。
// GB/T 33190 表 46 按绘制字形定位，部分文档则保留变换前的字符增量。
func UsesGlyphDeltas(code models.TextCode, glyphs []Glyph) bool {
	if len(glyphs) == 0 {
		return false
	}
	last := glyphs[len(glyphs)-1]
	count := last.GlyphPosition + last.GlyphCount
	if count == len([]rune(code.Value)) {
		return false
	}
	return (len(code.DeltaX) > 0 || len(code.DeltaY) > 0) &&
		(len(code.DeltaX) == 0 || len(code.DeltaX) == count-1) &&
		(len(code.DeltaY) == 0 || len(code.DeltaY) == count-1)
}

// Renderable 判断文本是否包含可见字符。C0/C1、DEL 和 U+FFFD 不参与字形绘制。
func Renderable(value string) bool {
	for _, r := range value {
		if RenderableRune(r) {
			return true
		}
	}
	return false
}

// RenderableRune 判断码位是否可交给字体绘制，避免控制字符显示为 .notdef。
func RenderableRune(r rune) bool {
	return r >= 0x20 && r != 0x7f && (r < 0x80 || r > 0x9f) && r != '\uFFFD'
}

// VisibleText 过滤交给整串整形的文字，不改动源 TextCode 和定位队列。
func VisibleText(value string) string {
	if strings.IndexFunc(value, func(r rune) bool { return !RenderableRune(r) }) < 0 {
		return value
	}
	return strings.Map(func(r rune) rune {
		if RenderableRune(r) {
			return r
		}
		return -1
	}, value)
}

// CanRenderRune 判断字体是否有指定 Unicode 码位的字形。
func CanRenderRune(face Face, r rune) bool { return face != nil && face.GlyphIndex(r) != 0 }

// CanRenderText 判断文本是否可按 Unicode 整形：不含字形私有码位，且每个可见
// 字符都能在字体中找到字形。控制字符不参与覆盖查询。
func CanRenderText(face Face, value string) bool {
	if fontrepair.ContainsGlyphRune(value) {
		return false
	}
	rendered := false
	for _, r := range value {
		if !RenderableRune(r) {
			continue
		}
		if !CanRenderRune(face, r) {
			return false
		}
		rendered = true
	}
	return rendered
}

var transformMapPool = sync.Pool{
	New: func() any { return make(map[int]models.CTCGTransform) },
}

// CodeGlyphs 将一个 TextCode 映射为绘制字形，保留连字与一字符多字形的源文字。
// transforms 应已通过 models.ExpandCGTransformPositions 展开对象内的位置。
func CodeGlyphs(face Face, runes []rune, transforms []models.CTCGTransform, codePosition int) []Glyph {
	if len(transforms) == 0 {
		glyphs := make([]Glyph, len(runes))
		for i, r := range runes {
			glyphs[i] = Glyph{Value: string(r), CodePosition: i, GlyphPosition: i, GlyphCount: 1}
		}
		return glyphs
	}
	byPosition := transformMapPool.Get().(map[int]models.CTCGTransform) //nolint:forcetypeassert // 私有池的 New 和两处 Put 均使用同一 map 类型。
	defer transformMapPool.Put(byPosition)
	clear(byPosition)
	for _, transform := range transforms {
		byPosition[transform.CodePosition] = transform
	}
	glyphs := make([]Glyph, 0, len(runes))
	for i := 0; i < len(runes); {
		transform, ok := byPosition[codePosition+i]
		if !ok {
			glyphs = append(glyphs, Glyph{Value: string(runes[i]), CodePosition: i})
			i++
			continue
		}
		ids := transform.Glyphs
		if transform.GlyphCount > 0 && transform.GlyphCount < len(ids) {
			ids = ids[:transform.GlyphCount]
		}
		codeCount := transform.CodeCount
		if codeCount <= 0 {
			codeCount = 1
		}
		// 表 48 定义消耗的字符数量。错误描述截到当前 TextCode 剩余字符，
		// 先比较剩余数量，避免 i+codeCount 溢出。
		if remaining := len(runes) - i; codeCount > remaining {
			codeCount = remaining
		}
		if codeCount == len(ids) {
			for k := 0; k < codeCount; k++ {
				id := ids[k]
				if id < 0 || id > 0xffff {
					continue
				}
				r := runes[i+k]
				// 修复字体可能重排 GID，有 Unicode 时优先原文；ASCII CFF
				// 子集和有原始映射的组合符号仍直接绘制，避免独立整形插入 dotted circle。
				directCFFGlyph := face != nil && face.IsCFF() && r >= 0x21 && r <= 0x7e
				directMark := face != nil && unicode.IsMark(r) && face.GlyphIndex(fontrepair.GlyphRune(uint16(id))) != 0
				if Renderable(string(r)) && face != nil && !directCFFGlyph && !directMark && CanRenderRune(face, r) {
					glyphs = append(glyphs, Glyph{Value: string(r), CodePosition: i + k})
					continue
				}
				semantic := ""
				if Renderable(string(r)) {
					semantic = string(r)
				}
				glyphs = append(glyphs, Glyph{Value: string(fontrepair.GlyphRune(uint16(id))), Semantic: semantic, CodePosition: i + k})
			}
		} else {
			semantic := string(runes[i : i+codeCount])
			for _, id := range ids {
				if id >= 0 && id <= 0xffff {
					glyphs = append(glyphs, Glyph{Value: string(fontrepair.GlyphRune(uint16(id))), Semantic: semantic, CodePosition: i})
					semantic = ""
				}
			}
		}
		i += codeCount
	}
	for i := range glyphs {
		glyphs[i].GlyphPosition = i
		glyphs[i].GlyphCount = 1
	}
	// GID 只在原字体中有意义（11.4.1）。替代字体没有私有映射时按源文本
	// 整形，保留原字形位置和数量，后续步进仍消费原来的定位队列。
	if face != nil {
		out := glyphs[:0]
		for i := 0; i < len(glyphs); {
			glyph := glyphs[i]
			end := i + 1
			for end < len(glyphs) && glyphs[end].CodePosition == glyph.CodePosition {
				end++
			}
			if fontrepair.ContainsGlyphRune(glyph.Value) && glyph.Semantic != "" &&
				(len([]rune(glyph.Semantic)) > 1 || end > i+1) &&
				face.GlyphIndex([]rune(glyph.Value)[0]) == 0 && CanRenderText(face, glyph.Semantic) {
				glyph.Value, glyph.Semantic = glyph.Semantic, ""
				glyph.GlyphCount = end - i
				out = append(out, glyph)
				i = end
			} else {
				out = append(out, glyph)
				i++
			}
		}
		glyphs = out
	}
	return glyphs
}
