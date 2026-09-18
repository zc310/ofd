package pdf2ofd

import (
	"math"
	"strings"

	"github.com/zc310/ofd/pkg/creator"
)

// 合并相邻单字 TextObject 的间距上下限（相对字号）。
// 上限用于断开分栏、大间隔等非连续文字；下限允许轻微字距重叠。
const (
	mergeTextMaxGapRatio = 0.8
	mergeTextMinGapRatio = -0.5
)

// textGlyphUnit 是一个可合并的文字单元：单个字符及其可选字形映射。
type textGlyphUnit struct {
	value     string
	transform *creator.CGTransform
	codeLen   int
}

// mergeAdjacentTextItems 合并同一样式、同一基线且间距合理的相邻单字
// TextObject，并用合成的 DeltaX 记录逐字步进。许多 PDF 生产者逐字符
// 输出 Tj，会产生大量单字文字对象；合并后逐字定位不变，但对象数量、
// 重复属性（字体/颜色/CGTransform）显著减少。
func mergeAdjacentTextItems(items []creator.Item) []creator.Item {
	if len(items) < 2 {
		return items
	}
	merged := make([]creator.Item, 0, len(items))
	var run *textMergeRun
	flush := func() {
		if run != nil {
			merged = append(merged, run.build())
			run = nil
		}
	}
	for _, item := range items {
		text, ok := item.(creator.Text)
		if !ok {
			flush()
			merged = append(merged, item)
			continue
		}
		unit, ok := singleTextUnit(text)
		if !ok {
			flush()
			merged = append(merged, item)
			continue
		}
		if run != nil && run.canAppend(text) {
			run.append(text, unit)
			continue
		}
		flush()
		run = newTextMergeRun(text, unit)
	}
	flush()
	return merged
}

// singleTextUnit 判断文字对象是否为"单字符且字形映射简单"，可作为合并单元。
// 只接受水平方向、无变换/裁剪/动作的对象。
func singleTextUnit(text creator.Text) (textGlyphUnit, bool) {
	if text.CTM != nil || text.Clips != nil || len(text.Actions) > 0 {
		return textGlyphUnit{}, false
	}
	if text.ReadDirection != 0 || text.CharDirection != 0 {
		return textGlyphUnit{}, false
	}
	value := text.Value
	if len(text.TextCodes) > 0 {
		if len(text.TextCodes) != 1 {
			return textGlyphUnit{}, false
		}
		code := text.TextCodes[0]
		if len(code.DeltaX) > 0 || len(code.DeltaY) > 0 {
			return textGlyphUnit{}, false
		}
		value = code.Value
	}
	runes := []rune(value)
	if len(runes) != 1 || !renderablePDFRune(runes[0]) {
		return textGlyphUnit{}, false
	}
	switch len(text.CGTransforms) {
	case 0:
		return textGlyphUnit{value: value, codeLen: 1}, true
	case 1:
		transform := text.CGTransforms[0]
		if transform.CodePosition != 0 || transform.CodeCount != 1 || transform.GlyphCount != 1 || len(transform.Glyphs) != 1 {
			return textGlyphUnit{}, false
		}
		glyph := transform
		return textGlyphUnit{value: value, transform: &glyph, codeLen: 1}, true
	default:
		return textGlyphUnit{}, false
	}
}

// renderablePDFRune 判断字符是否有可见字形（控制符与替换符跳过）。
func renderablePDFRune(r rune) bool {
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '\uFFFD' {
		return false
	}
	return true
}

// textMergeRun 是一段正在累积的合并文字。
type textMergeRun struct {
	first   creator.Text
	items   []creator.Text
	units   []textGlyphUnit
	endX    float64
	maxEndX float64
	minX    float64
}

func newTextMergeRun(text creator.Text, unit textGlyphUnit) *textMergeRun {
	end := text.X + text.Width
	return &textMergeRun{
		first:   text,
		items:   []creator.Text{text},
		units:   []textGlyphUnit{unit},
		endX:    end,
		maxEndX: end,
		minX:    text.X,
	}
}

// canAppend 判断下一个单字对象能否并入当前合并段。
func (r *textMergeRun) canAppend(text creator.Text) bool {
	if !sameMergeTextStyle(r.first, text) {
		return false
	}
	size := r.first.Size
	if size <= 0 {
		size = 1
	}
	gap := text.X - r.endX
	if gap < mergeTextMinGapRatio*size || gap > mergeTextMaxGapRatio*size {
		return false
	}
	return true
}

func (r *textMergeRun) append(text creator.Text, unit textGlyphUnit) {
	r.items = append(r.items, text)
	r.units = append(r.units, unit)
	r.endX = text.X + text.Width
	if end := text.X + text.Width; end > r.maxEndX {
		r.maxEndX = end
	}
}

// build 生成合并后的文字对象；DeltaX 为相邻单字的实际步进（毫米）。
func (r *textMergeRun) build() creator.Text {
	if len(r.items) == 1 {
		return r.first
	}
	var value strings.Builder
	value.Grow(len(r.items))
	deltas := make([]float64, 0, len(r.items)-1)
	transforms := make([]creator.CGTransform, 0, len(r.items))
	position := 0
	for index, item := range r.items {
		if index > 0 {
			deltas = append(deltas, item.X-r.items[index-1].X)
		}
		value.WriteString(r.units[index].value)
		if r.units[index].transform != nil {
			transform := *r.units[index].transform
			transform.CodePosition = position
			transforms = append(transforms, transform)
		}
		position += r.units[index].codeLen
	}
	merged := r.first
	merged.X = r.minX
	merged.Width = r.maxEndX - r.minX
	if merged.Width < 0.001 {
		merged.Width = 0.001
	}
	merged.Value = value.String()
	merged.TextCodes = []creator.TextCode{{Value: merged.Value, DeltaX: deltas}}
	if len(transforms) > 0 {
		merged.CGTransforms = transforms
	} else {
		merged.CGTransforms = nil
	}
	return merged
}

// sameMergeTextStyle 判断两个文字对象是否可视为同一样式。
func sameMergeTextStyle(a, b creator.Text) bool {
	if a.Font != b.Font || a.DrawParam != b.DrawParam {
		return false
	}
	if !nearPDFFloat(a.Size, b.Size, 1e-6) || !nearPDFFloat(a.Y, b.Y, 1e-4) || !nearPDFFloat(a.Height, b.Height, 1e-6) {
		return false
	}
	if a.Stroke != b.Stroke || !sameOptionalBool(a.Fill, b.Fill) {
		return false
	}
	if !sameOptionalColor(a.FillColor, b.FillColor) || !sameOptionalColor(a.StrokeColor, b.StrokeColor) {
		return false
	}
	if a.HScale != b.HScale || a.ReadDirection != b.ReadDirection || a.CharDirection != b.CharDirection {
		return false
	}
	if a.Weight != b.Weight || a.Italic != b.Italic {
		return false
	}
	return sameOptionalBool(a.Visible, b.Visible)
}

func nearPDFFloat(a, b, epsilon float64) bool {
	return math.Abs(a-b) <= epsilon
}

func sameOptionalBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameOptionalColor(a, b *creator.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.R != b.R || a.G != b.G || a.B != b.B || a.ColorSpace != b.ColorSpace {
		return false
	}
	if len(a.Components) != 0 || len(b.Components) != 0 {
		return false
	}
	return a.Index == nil && b.Index == nil && a.Alpha == nil && b.Alpha == nil &&
		a.Axial == nil && b.Axial == nil && a.Radial == nil && b.Radial == nil &&
		a.Gouraud == nil && b.Gouraud == nil && a.LaGouraud == nil && b.LaGouraud == nil &&
		a.Pattern == nil && b.Pattern == nil
}
