package webreader

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

// PageInfo 描述一个可渲染页面。尺寸单位为毫米。
type PageInfo struct {
	Index  int
	Width  float64
	Height float64
}

// TextRun 描述页面中的一个文字对象。坐标和尺寸单位为毫米。
// X/Y 是网页覆盖层使用的左上角坐标，不是 TextCode 的基线坐标。
type TextRun struct {
	Text   string
	X      float64
	Y      float64
	Width  float64
	Height float64
	// Scope 是文字所属文档体的索引，与 Font 一起唯一标识使用的字体。
	Scope         int
	Font          uint64
	Size          float64
	Weight        int
	ReadDirection int
	CharDirection int
	FontFamily    string
	Bold          bool
	Italic        bool
	Glyphs        []Glyph
}

// Glyph 描述一个字符的页面区域，坐标单位为毫米。
type Glyph struct {
	Text   string
	X      float64
	Y      float64
	Width  float64
	Height float64
	Angle  float64
}

// Text 返回指定页面的文字对象快照。文字顺序与 OFD 页面绘制顺序一致。
func (r *Reader) Text(index int) ([]TextRun, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return cloneTextRuns(r.textAt(index)), nil
}

func pageTextObjects(page *parser.Page) []*models.TextObject {
	var texts []*models.TextObject
	var walk func([]models.PageItem)
	walk = func(items []models.PageItem) {
		for _, item := range items {
			if item.Kind == models.PageItemBlock && item.Block != nil {
				walk(item.Block.Items)
				continue
			}
			if item.Kind == models.PageItemText && item.Text != nil {
				texts = append(texts, item.Text)
			}
		}
	}
	for _, layer := range contentLayers(page) {
		if layer != nil {
			walk(layer.Items)
		}
	}
	return texts
}

func textMatching(texts []*models.TextObject, needle string) *models.TextObject {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return nil
	}
	for _, text := range texts {
		if strings.Contains(textValue(text), needle) {
			return text
		}
	}
	return nil
}

func firstUnusedText(texts []*models.TextObject, links []PageLink, page int) *models.TextObject {
	used := map[string]bool{}
	for _, link := range links {
		if link.Page == page {
			used[link.ID] = true
		}
	}
	for _, text := range texts {
		if !used[formatStID(text.ID)] {
			return text
		}
	}
	return nil
}

func textValue(text *models.TextObject) string {
	if text == nil {
		return ""
	}
	var value strings.Builder
	for _, code := range text.TextCode {
		value.WriteString(code.Value)
	}
	return value.String()
}

// contentLayers 返回页面正文的图层列表；读取失败时返回 nil。
func contentLayers(page *parser.Page) []*models.Layer {
	var layers []*models.Layer
	_ = page.WithPageContent(func(content *models.PageContent) error {
		if content != nil && content.Content != nil {
			layers = content.Content.Layer
		}
		return nil
	})
	return layers
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func browserFontFamily(scope int, id uint64) string {
	return fmt.Sprintf("OFDFont-%d-%d", scope, id)
}

func fontFormat(file models.StLoc) string {
	name := strings.ToLower(string(file))
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		return name[index+1:]
	}
	return ""
}

func textRunsWithFallback(document *render.Document, page *parser.Page, fontScope int, fallbackFamily string) []TextRun {
	layouts := document.TextLayouts(page)
	runs := make([]TextRun, 0, len(layouts))
	for _, layout := range layouts {
		run := TextRun{
			Text: layout.Text, X: layout.X, Y: layout.Y, Width: layout.Width, Height: layout.Height,
			Scope: fontScope, Font: layout.Font, Size: layout.Size, ReadDirection: layout.ReadDirection,
			Weight:        layout.Weight,
			CharDirection: layout.CharDirection, FontFamily: textFontFamily(document, fontScope, layout.Font, fallbackFamily),
			Bold: layout.Bold, Italic: layout.Italic,
		}
		for _, glyph := range layout.Glyphs {
			run.Glyphs = append(run.Glyphs, Glyph{Text: glyph.Text, X: glyph.X, Y: glyph.Y, Width: glyph.Width, Height: glyph.Height, Angle: glyph.Angle})
		}
		runs = append(runs, run)
	}
	return runs
}

// textAt 在持有 Reader 锁时缓存不可变的文档布局。
// 向外部返回结果的调用方必须先复制结果。
func (r *Reader) textAt(index int) []TextRun {
	if runs, ok := r.text.Get(index); ok {
		return runs
	}
	runs := textRunsWithFallback(r.pages[index].document, r.pages[index].page, r.pages[index].fontScope, r.fallbackFamily)
	r.text.Add(index, runs)
	return runs
}

func textFontFamily(document *render.Document, scope int, id uint64, fallback string) string {
	if document != nil {
		if family := document.FallbackFontFamily(models.StRefID(id)); family != "" {
			return family
		}
		if document.HasLoadedEmbeddedFont(models.StRefID(id)) {
			return browserFontFamily(scope, id)
		}
	}
	return fallback
}

func cloneTextRuns(source []TextRun) []TextRun {
	cloned := make([]TextRun, len(source))
	for index, run := range source {
		cloned[index] = run
		cloned[index].Glyphs = append([]Glyph(nil), run.Glyphs...)
	}
	return cloned
}
