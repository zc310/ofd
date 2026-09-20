// Package textdoc 把 OFD 页面中的文字对象提取为带位置的条目，供文本、Markdown
// 等编码器复用。它只依赖 OFD 模型与解析器，不感知任何输出格式。
package textdoc

import (
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const maxTextCompositeDepth = 32

// 默认页面尺寸（毫米），仅在页面物理区域缺失或非法时使用。
const (
	defaultPageWidth  = 210.0
	defaultPageHeight = 297.0
)

// Entry 是一个待输出的文字对象及其在页面中的位置与尺寸（毫米，原点在页面左上角）。
type Entry struct {
	Text  string
	X     float64
	Y     float64
	Size  float64
	Width float64
}

// Page 保存单页的文字条目以及该页物理尺寸，用于按列对齐和页眉页脚判定。
type Page struct {
	Entries []Entry
	Width   float64
	Height  float64
}

// Count 统计所有文档体的非空页面总数。
func Count(documents []*parser.Document) int {
	count := 0
	for _, doc := range documents {
		if doc == nil {
			continue
		}
		for _, p := range doc.Pages {
			if p != nil {
				count++
			}
		}
	}
	return count
}

// Collect 提取全局页索引范围 [start, end) 内的页面。调用方需保证 start/end
// 合法（通常先经页码校验）；end 超出总页数时按总页数截断。
func Collect(documents []*parser.Document, start, end int) []Page {
	pages := make([]Page, 0, max(end-start, 0))
	index := 0
	for _, doc := range documents {
		if doc == nil {
			continue
		}
		for _, p := range doc.Pages {
			if p == nil {
				continue
			}
			if index >= start && index < end {
				pages = append(pages, ExtractPage(doc, p))
			}
			index++
			if index >= end {
				return pages
			}
		}
	}
	return pages
}

// ExtractPage 提取单个页面的文字条目和物理尺寸。
func ExtractPage(doc *parser.Document, page *parser.Page) Page {
	result := Page{Width: defaultPageWidth, Height: defaultPageHeight}
	if page == nil {
		return result
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return result
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil {
		return result
	}
	content.EnsurePhysicalBox()
	if area := content.Area; area != nil {
		if width := area.PhysicalBox.Width; Finite(width) && width > 0 {
			result.Width = width
		}
		if height := area.PhysicalBox.Height; Finite(height) && height > 0 {
			result.Height = height
		}
	}

	entries := make([]Entry, 0)
	for _, template := range content.Template {
		if templateContent := doc.GetTemplate(models.StID(template.TemplateID)); templateContent != nil {
			appendPageContentText(doc, templateContent.Content, &entries, 0)
		}
	}
	appendPageContentText(doc, content.Content, &entries, 0)
	if annot := doc.GetAnnotation(page.ID); annot != nil {
		for _, item := range annot.Annots {
			if item == nil || !item.Visible.Value(true) || item.Appearance == nil {
				continue
			}
			appendTextItems(doc, item.Appearance.Items, &entries, 0)
		}
	}
	result.Entries = entries
	return result
}

func appendPageContentText(doc *parser.Document, content *models.Content, entries *[]Entry, depth int) {
	if content == nil {
		return
	}
	// 与渲染顺序保持一致：背景层先于其他图层处理。
	for _, layer := range content.Layer {
		if layer != nil && layer.Type == "Background" {
			appendTextItems(doc, layer.Items, entries, depth)
		}
	}
	for _, layer := range content.Layer {
		if layer != nil && layer.Type != "Background" {
			appendTextItems(doc, layer.Items, entries, depth)
		}
	}
}

func appendTextItems(doc *parser.Document, items []models.PageItem, entries *[]Entry, depth int) {
	if depth > maxTextCompositeDepth {
		return
	}
	for _, item := range items {
		switch item.Kind {
		case models.PageItemText:
			if !item.Text.VisibleValue() || textFillDisabled(item.Text) {
				continue
			}
			var text strings.Builder
			for _, code := range item.Text.TextCode {
				text.WriteString(code.Value)
			}
			if text.Len() == 0 {
				continue
			}
			x, y := textEntryPosition(item.Text)
			value := EnsureUTF8(text.String())
			*entries = append(*entries, Entry{Text: value, X: x, Y: y, Size: item.Text.Size, Width: textEntryWidth(item.Text, value)})
		case models.PageItemBlock:
			appendTextItems(doc, item.Block.Items, entries, depth)
		case models.PageItemComposite:
			if !item.Composite.VisibleValue() {
				continue
			}
			unit := doc.GetCompositeUnit(models.StID(item.Composite.ResourceID))
			if unit != nil {
				appendTextItems(doc, unit.Content.Items, entries, depth+1)
			}
		}
	}
}

// textEntryPosition 计算文字对象首个 TextCode 的页面锚点（毫米，原点在页面
// 左上角），与渲染时 TextCode 起点加 Boundary 的规则保持一致。
func textEntryPosition(object models.TextObject) (float64, float64) {
	x, y := 0.0, 0.0
	for _, code := range object.TextCode {
		x, y = code.X, code.Y
		break
	}
	if object.CTM != nil {
		x, y = object.CTM.Transform(x, y)
	}
	return x + object.Boundary.X, y + object.Boundary.Y
}

// textEntryWidth 返回文字对象的水平宽度（毫米）。优先使用 Boundary 宽度，
// 缺失时按字符显示宽度和字号估算，供表格列定位使用。
func textEntryWidth(object models.TextObject, text string) float64 {
	if width := object.Boundary.Width; Finite(width) && width > 0 {
		return width
	}
	size := object.Size
	if !Finite(size) || size <= 0 {
		size = 6
	}
	return float64(DisplayWidth(text)) * size * 0.5
}

func textFillDisabled(object models.TextObject) bool {
	return !object.Fill.Value(true)
}

// Finite 判断浮点数是否为有限值（非 NaN、非 Inf）。
func Finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// EnsureUTF8 保证输出文本是合法 UTF-8。OFD 规范要求文字为 UTF-8，但部分第三方
// 文档会写入 GBK 等本地编码的原始字节；这里先尝试按 GBK 解码，失败时再把无法
// 识别的字节替换为 U+FFFD，避免 .txt/.md 出现非法 UTF-8 导致乱码。
func EnsureUTF8(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	if decoded, err := simplifiedchinese.GBK.NewDecoder().String(value); err == nil && utf8.ValidString(decoded) {
		return decoded
	}
	return strings.ToValidUTF8(value, "\uFFFD")
}

// DisplayWidth 返回按终端显示列数计算的宽度：CJK 等全角字符占两列，其余字符
// 占一列，保证多栏文字的列位置对齐。
func DisplayWidth(text string) int {
	width := 0
	for _, r := range text {
		if wideRune(r) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

// wideRune 判断字符是否按全角宽度显示。
func wideRune(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		(r >= 0x2E80 && r <= 0x2EFF) ||
		(r >= 0x3000 && r <= 0x303F) ||
		(r >= 0x3100 && r <= 0x312F) ||
		(r >= 0x3200 && r <= 0x4DBF) ||
		(r >= 0xA960 && r <= 0xA97F) ||
		(r >= 0xAC00 && r <= 0xD7FF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE10 && r <= 0xFE1F) ||
		(r >= 0xFE30 && r <= 0xFE4F) ||
		(r >= 0xFF00 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x20000 && r <= 0x3FFFD)
}

// sortByYThenX 按 Y 升序、Y 相同按 X 升序排序。
func sortByYThenX(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Y != entries[j].Y {
			return entries[i].Y < entries[j].Y
		}
		return entries[i].X < entries[j].X
	})
}
