package webreader

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/search"
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
	// Source 为 page、template、annotation、watermark 或 seal。
	Source string
	Text   string
	X      float64
	Y      float64
	Width  float64
	// Height 是字体排版行高，可与 Size 不同；与字符框使用相同的纵向度量。
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

// TextOptions 控制文字覆盖层与搜索索引的附加来源；位图印章不执行 OCR。
type TextOptions = render.TextOptions

// SetTextOptions 同时更新复制、覆盖层和搜索的来源策略，并使相关缓存失效。
func (r *Reader) SetTextOptions(options TextOptions) error {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	if r.textOptions == options {
		return nil
	}
	r.textOptions = options
	r.text = newTextCache()
	r.search = newSearchCache(len(r.pages))
	r.metadata = &readerMetadata{}
	return nil
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
// Export=false 时返回 ErrPermissionDenied；搜索与页面预览仍可使用。
func (r *Reader) Text(index int) ([]TextRun, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	if err := r.checkPermissionLocked(PermissionCopy, []int{index}); err != nil {
		return nil, err
	}
	runs, err := r.textAt(index)
	if err != nil {
		return nil, fmt.Errorf("读取第 %d 页文字失败: %w", index, err)
	}
	return cloneTextRuns(runs), nil
}

func pageTextObjectsFromLayers(layers []*models.Layer) []*models.TextObject {
	var texts []*models.TextObject
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		walkPageItems(layer.Items, func(item models.PageItem) bool {
			if item.Kind == models.PageItemText && item.Text != nil {
				texts = append(texts, item.Text)
			}
			return true
		})
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

type pageContentSnapshot struct {
	layers    []*models.Layer
	templates []models.Template
	actions   *models.Actions
}

func loadPageContentSnapshot(page *parser.Page) (pageContentSnapshot, error) {
	var snapshot pageContentSnapshot
	err := page.WithPageContent(func(content *models.PageContent) error {
		if content == nil {
			return nil
		}
		if content.Content != nil {
			snapshot.layers = append(snapshot.layers, content.Content.Layer...)
		}
		snapshot.templates = append(snapshot.templates, content.Template...)
		snapshot.actions = content.Actions
		return nil
	})
	return snapshot, err
}

// contentLayers 返回页面正文的图层列表；兼容调用在读取失败时返回 nil。
func contentLayers(page *parser.Page) []*models.Layer {
	snapshot, _ := loadPageContentSnapshot(page)
	return snapshot.layers
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

func textRunsWithFallback(document *render.Document, page *parser.Page, fontScope int, fallbackFamily string, visibility render.PageVisibility, options ...TextOptions) ([]TextRun, error) {
	var policy TextOptions
	if len(options) > 0 {
		policy = options[0]
	}
	layouts, err := document.TextLayoutsWithOptionsE(page, visibility, policy)
	if err != nil {
		return nil, err
	}
	runs := make([]TextRun, 0, len(layouts))
	for _, layout := range layouts {
		run := TextRun{
			Source: string(layout.Source),
			Text:   layout.Text, X: layout.X, Y: layout.Y, Width: layout.Width, Height: layout.Height,
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
	return runs, nil
}

// textAt 在持有 Reader 锁时缓存不可变的文档布局。
// 向外部返回结果的调用方必须先复制结果。
func (r *Reader) textAt(index int) ([]TextRun, error) {
	if runs, ok := r.text.Get(index); ok {
		return runs, nil
	}
	runs, err := textRunsWithFallback(r.pages[index].document, r.pages[index].page, r.pages[index].fontScope, r.fallbackFamily, r.hiddenLayers[index], r.textOptions)
	if err != nil {
		return nil, err
	}
	r.text.AddWeighted(index, runs, textRunsWeight(runs))
	return runs, nil
}

// newTextCache 与 newSearchCache 构造按条目数与字节预算双重限制的缓存。
//
// 四处需要同样的构造（打开文档，以及换用/移除回退字体时重建），写成内联
// 迟早有一处漏改——漏改的那处预算会静默失效，调用方看不出区别。
func newTextCache() *cache.LRU[int, []TextRun] {
	return cache.NewWeightedLRU[int, []TextRun](textCacheCapacity, textCacheBytes, nil)
}

func newSearchCache(pageCount int) *cache.LRU[int, *search.Index] {
	capacity := minInt(pageCount, searchCacheCapacity)
	if capacity < 1 {
		capacity = 1
	}
	return cache.NewWeightedLRU[int, *search.Index](capacity, searchCacheBytes, nil)
}

// textRunsWeight 估算一页文字快照的内存占用，用作缓存的计量权重。
//
// 不用 unsafe.Sizeof：那一处会把整个包拖进 unsafe，而这个估算不需要精确到
// 字节——它只用于决定何时逐出，取整到几十字节的误差无关紧要。真正要紧的是
// 分量齐全：字形里每个 Glyph 都自带一个 Text string，漏掉它会低估一个数量级。
//
// 实测每页占用：稀疏页约 1.4 KB，密集页（ano.ofd，2200 字形）约 154 KB，
// 相差 110 倍。按页数封顶时缓存占用会在 90 KB 到 9.8 MB 之间浮动，全看文档
// 碰巧是哪一类；按字节封顶才 predictable。
const (
	// textRunWeightOverhead 是 TextRun 结构体本身的估算字节数（不含变长部分）。
	textRunWeightOverhead = 96
	// glyphWeightOverhead 是 Glyph 结构体本身的估算字节数（不含其 Text）。
	glyphWeightOverhead = 64
)

func textRunsWeight(runs []TextRun) int64 {
	var total int64
	for i := range runs {
		run := &runs[i]
		total += textRunWeightOverhead
		total += int64(len(run.Text)) + int64(len(run.FontFamily))
		for j := range run.Glyphs {
			total += glyphWeightOverhead + int64(len(run.Glyphs[j].Text))
		}
	}
	// 切片自身的底层数组
	return total + int64(len(runs))*textRunWeightOverhead
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
