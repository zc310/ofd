// Package webreader 包提供适合浏览器调用的 OFD 文档访问接口。
package webreader

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

const (
	defaultDPI      = 96
	maxDPI          = 600
	maxInputBytes   = 256 << 20
	maxRenderPixels = 50_000_000
	maxFontBytes    = 32 << 20
	maxRenderPages  = 64
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

// SearchResult 描述一个页面文字命中。
type SearchResult struct {
	Page  int
	Run   int
	Text  string
	Start int
	End   int
	Rects []Rect
}

// Rect 描述一个搜索命中的字符区域，坐标单位为毫米。
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
	Angle  float64
}

// RenderOptions 控制页面输出。当前支持 PNG 格式。
type RenderOptions struct {
	DPI        float64
	Background color.Color
}

// FontResource 描述文档中可注入浏览器的嵌入字体。
// Data 为空表示该字体只声明了名称，没有嵌入字体文件。
type FontResource struct {
	ID     uint64
	Family string
	Name   string
	Bold   bool
	Italic bool
	Format string
	Data   []byte
}

// FontSource 是由调用方提供给 WASM 渲染器的字体文件。
// 浏览器无法读取本机系统字体文件，因此无内嵌字体时应传入可访问的
// TTF/OTF Web Font 数据，例如 Google Fonts 的 Noto Sans SC。
type FontSource struct {
	Family string
	Name   string
	Weight int
	Italic bool
	Data   []byte
}

// OpenOptions 配置 OFD 打开行为。
type OpenOptions struct {
	FallbackFonts []FontSource
}

// Reader 是一个已打开的 OFD 文档。
// Reader 负责持有文档资源，使用完毕后必须调用 Close。
type Reader struct {
	mu             sync.Mutex
	closed         bool
	ofd            *parser.OFD
	pages          []pageRef
	fallbackFamily string
	fallbackFonts  []FontSource
	text           [][]TextRun
	textSet        []bool
	search         []searchPage
	searchSet      []bool
	options        RenderOptions
}

type searchPage struct {
	runs   [][]rune
	byRune map[rune][]int
}

type pageRef struct {
	document  *render.Document
	page      *parser.Page
	fontScope int
}

// Open 从内存中的 OFD 数据创建浏览器文档引擎。
func Open(data []byte) (*Reader, error) {
	return OpenWithOptions(data, OpenOptions{})
}

// OpenWithOptions 从内存中的 OFD 数据创建浏览器文档引擎，并注册外部回退字体。
func OpenWithOptions(data []byte, options OpenOptions) (*Reader, error) {
	if len(data) == 0 {
		return nil, errors.New("OFD 数据为空")
	}
	if len(data) > maxInputBytes {
		return nil, fmt.Errorf("OFD 数据超过大小限制 %d MB", maxInputBytes>>20)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		return nil, fmt.Errorf("解析 OFD 失败: %w", err)
	}

	r := &Reader{
		ofd:           ofd,
		fallbackFonts: cloneFontSources(options.FallbackFonts),
		options:       RenderOptions{DPI: defaultDPI, Background: color.Transparent},
	}
	validFallbacks := make([]FontSource, 0, len(options.FallbackFonts))
	for documentIndex, document := range ofd.Documents {
		renderDocument := render.NewDocument(r.options.Background, document)
		for _, source := range options.FallbackFonts {
			if len(source.Data) == 0 || source.Family == "" {
				continue
			}
			if err := renderDocument.AddFallbackFont(source.Data, source.Family, fallbackFontStyle(source)); err != nil {
				continue
			}
			if !containsFontSource(validFallbacks, source.Family, source.Weight, source.Italic) {
				validFallbacks = append(validFallbacks, source)
			}
		}
		for _, page := range renderDocument.Pages {
			r.pages = append(r.pages, pageRef{document: renderDocument, page: page, fontScope: documentIndex})
		}
	}
	r.fallbackFonts = cloneFontSources(validFallbacks)
	r.fallbackFamily = firstFallbackFamily(validFallbacks)
	if len(r.pages) == 0 {
		_ = ofd.Close()
		return nil, errors.New("OFD 文档没有页面")
	}
	r.text = make([][]TextRun, len(r.pages))
	r.textSet = make([]bool, len(r.pages))
	r.search = make([]searchPage, len(r.pages))
	r.searchSet = make([]bool, len(r.pages))
	return r, nil
}

func firstFallbackFamily(fonts []FontSource) string {
	for _, font := range fonts {
		if font.Family != "" && len(font.Data) > 0 {
			return font.Family
		}
	}
	return ""
}

func containsFontSource(sources []FontSource, family string, weight int, italic bool) bool {
	for _, source := range sources {
		if source.Family == family && source.Weight == weight && source.Italic == italic {
			return true
		}
	}
	return false
}

// AddFallbackFont 为没有内嵌 FontFile 的文档字体添加外部字体。
// 文字和搜索快照会失效，因为它们的字形度量可能使用了不同的回退字体。
func (r *Reader) AddFallbackFont(source FontSource) error {
	slog.Info("register fallback font", "name", source.Name, "family", source.Family, "weight", source.Weight, "italic", source.Italic, "bytes", len(source.Data))
	if r == nil {
		return errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	if len(source.Data) == 0 || source.Family == "" {
		return errors.New("回退字体数据或字体族名为空")
	}
	if len(source.Data) > maxFontBytes {
		return fmt.Errorf("回退字体超过大小限制 %d MB", maxFontBytes>>20)
	}
	seenDocuments := make(map[*render.Document]struct{})
	for _, ref := range r.pages {
		if _, seen := seenDocuments[ref.document]; seen {
			continue
		}
		seenDocuments[ref.document] = struct{}{}
		if err := ref.document.AddFallbackFont(source.Data, source.Family, fallbackFontStyle(source)); err != nil {
			return err
		}
	}
	r.fallbackFamily = source.Family
	r.fallbackFonts = append(r.fallbackFonts, cloneFontSources([]FontSource{source})...)
	r.text = make([][]TextRun, len(r.pages))
	r.textSet = make([]bool, len(r.pages))
	r.search = make([]searchPage, len(r.pages))
	r.searchSet = make([]bool, len(r.pages))
	return nil
}

func fallbackFontStyle(source FontSource) canvas.FontStyle {
	style := canvas.FontRegular
	if source.Weight >= 650 {
		style = canvas.FontBold
	}
	if source.Italic {
		style |= canvas.FontItalic
	}
	return style
}

// PageCount 返回文档的总页数，页码从 0 开始。
func (r *Reader) PageCount() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0
	}
	return len(r.pages)
}

// Pages 返回所有页面的尺寸快照，尺寸单位为毫米。
func (r *Reader) Pages() ([]PageInfo, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}

	pages := make([]PageInfo, len(r.pages))
	for index, ref := range r.pages {
		if ref.page == nil {
			return nil, fmt.Errorf("第 %d 页为空", index)
		}
		ref.page.EnsurePhysicalBox()
		box := ref.page.Area.PhysicalBox
		if !finitePositive(box.Width) || !finitePositive(box.Height) {
			return nil, fmt.Errorf("第 %d 页尺寸无效", index)
		}
		pages[index] = PageInfo{Index: index, Width: box.Width, Height: box.Height}
	}
	return pages, nil
}

// Page 返回指定页面的尺寸信息。
func (r *Reader) Page(index int) (PageInfo, error) {
	pages, err := r.Pages()
	if err != nil {
		return PageInfo{}, err
	}
	if index < 0 || index >= len(pages) {
		return PageInfo{}, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return pages[index], nil
}

// Text 返回指定页面的文字对象快照。文字顺序与 OFD 页面绘制顺序一致。
func (r *Reader) Text(index int) ([]TextRun, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return cloneTextRuns(r.textAt(index)), nil
}

// Fonts 返回文档中声明的字体资源。只有包含 FontFile 的字体会带有 Data，
// 调用方可以将这些数据交给浏览器 FontFace 构造器进行注入。
func (r *Reader) Fonts() ([]FontResource, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	resources := make([]FontResource, 0)
	seen := make(map[string]struct{})
	for documentIndex, document := range r.ofd.Documents {
		for id, font := range document.FontRes {
			if font == nil || font.FontFile == "" {
				continue
			}
			key := fmt.Sprintf("%d:%d:%s", documentIndex, id, font.FontFile)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			data, err := document.FileCache.ReadLimit(string(font.FontFile), maxFontBytes)
			if err != nil {
				continue
			}
			if fixed, fixErr := fontfix.Repair(data); fixErr == nil {
				data = fixed
			}
			resources = append(resources, FontResource{
				ID: uint64(id), Family: browserFontFamily(documentIndex, uint64(id)), Name: font.FontName,
				Bold: font.Bold, Italic: font.Italic, Format: fontFormat(font.FontFile), Data: append([]byte(nil), data...),
			})
		}
	}
	return resources, nil
}

// Search 在所有页面的文字对象中查找 query，匹配不区分大小写。
func (r *Reader) Search(query string) ([]SearchResult, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	query = strings.TrimSpace(query)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if query == "" {
		return []SearchResult{}, nil
	}
	needle := strings.ToLower(query)
	needleRunes := []rune(needle)
	results := make([]SearchResult, 0)
	for pageIndex := range r.pages {
		runs := r.textAt(pageIndex)
		indexed := r.searchAt(pageIndex, runs)
		for _, runIndex := range indexed.byRune[needleRunes[0]] {
			text := indexed.runs[runIndex]
			run := runs[runIndex]
			start := 0
			for {
				found := indexRunes(text[start:], needleRunes)
				if found < 0 {
					break
				}
				found += start
				end := found + len(needleRunes)
				result := SearchResult{Page: pageIndex, Run: runIndex, Text: run.Text, Start: found, End: end}
				glyphStart := minInt(found, len(run.Glyphs))
				glyphEnd := minInt(end, len(run.Glyphs))
				for _, glyph := range run.Glyphs[glyphStart:glyphEnd] {
					result.Rects = append(result.Rects, Rect{X: glyph.X, Y: glyph.Y, Width: glyph.Width, Height: glyph.Height, Angle: glyph.Angle})
				}
				results = append(results, result)
				start = end
				if start >= len(text) {
					break
				}
			}
		}
	}
	return results, nil
}

func indexRunes(text, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	for index := 0; index+len(needle) <= len(text); index++ {
		matched := true
		for offset := range needle {
			if text[index+offset] != needle[offset] {
				matched = false
				break
			}
		}
		if matched {
			return index
		}
	}
	return -1
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// RenderPage 将指定页面渲染为 PNG 数据。
func (r *Reader) RenderPage(index int, options RenderOptions) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	return r.renderPageLocked(index, options)
}

// RenderPages 将多个页面按传入顺序渲染为 PNG 数据。
// 所有页面共享一次 Reader 锁和同一份文档状态；渲染本身仍按顺序执行。
func (r *Reader) RenderPages(indices []int, options RenderOptions) ([][]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	if len(indices) > maxRenderPages {
		return nil, fmt.Errorf("批量渲染页面数量超过限制 %d", maxRenderPages)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	results := make([][]byte, 0, len(indices))
	for _, index := range indices {
		data, err := r.renderPageLocked(index, options)
		if err != nil {
			return nil, err
		}
		results = append(results, data)
	}
	return results, nil
}

func (r *Reader) renderPageLocked(index int, options RenderOptions) ([]byte, error) {
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	if options.DPI == 0 {
		options.DPI = defaultDPI
	}
	if options.DPI < 1 || options.DPI > maxDPI || math.IsNaN(options.DPI) || math.IsInf(options.DPI, 0) {
		return nil, fmt.Errorf("DPI 必须在 1 到 %d 之间", maxDPI)
	}

	ref := r.pages[index]
	ref.page.EnsurePhysicalBox()
	box := ref.page.Area.PhysicalBox
	if !finitePositive(box.Width) || !finitePositive(box.Height) {
		return nil, fmt.Errorf("第 %d 页尺寸无效", index)
	}
	pixels := box.Width * options.DPI / 25.4 * box.Height * options.DPI / 25.4
	if math.IsNaN(pixels) || math.IsInf(pixels, 0) || pixels > maxRenderPixels {
		return nil, fmt.Errorf("第 %d 页渲染尺寸过大", index)
	}

	background := options.Background
	if background == nil {
		background = color.Transparent
	}
	// NewDocument 保证页面背景和渲染内容使用同一个文档级渲染上下文。
	document := ref.document
	if document == nil || document.Document == nil {
		return nil, errors.New("页面渲染上下文为空")
	}
	if !sameColor(background, r.options.Background) {
		document = render.NewDocument(background, ref.document.Document)
		for _, source := range r.fallbackFonts {
			_ = document.AddFallbackFont(source.Data, source.Family, fallbackFontStyle(source))
		}
	}
	page, err := document.Page(ref.page)
	if err != nil {
		return nil, fmt.Errorf("渲染第 %d 页失败: %w", index, err)
	}

	var output bytes.Buffer
	image := rasterizer.Draw(page, canvas.DPI(options.DPI), canvas.DefaultColorSpace)
	if err := png.Encode(&output, image); err != nil {
		return nil, fmt.Errorf("编码第 %d 页失败: %w", index, err)
	}
	return output.Bytes(), nil
}

// Close 释放文档资源。Close 可以安全地重复调用。
func (r *Reader) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.pages = nil
	r.text = nil
	r.textSet = nil
	r.search = nil
	r.searchSet = nil
	if r.ofd == nil {
		return nil
	}
	return r.ofd.Close()
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

func textRuns(document *render.Document, page *parser.Page, fontScope int) []TextRun {
	return textRunsWithFallback(document, page, fontScope, "")
}

func textRunsWithFallback(document *render.Document, page *parser.Page, fontScope int, fallbackFamily string) []TextRun {
	layouts := document.TextLayouts(page)
	runs := make([]TextRun, 0, len(layouts))
	for _, layout := range layouts {
		run := TextRun{
			Text: layout.Text, X: layout.X, Y: layout.Y, Width: layout.Width, Height: layout.Height,
			Font: layout.Font, Size: layout.Size, ReadDirection: layout.ReadDirection,
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
	if !r.textSet[index] {
		r.text[index] = textRunsWithFallback(r.pages[index].document, r.pages[index].page, r.pages[index].fontScope, r.fallbackFamily)
		r.textSet[index] = true
	}
	return r.text[index]
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

func cloneFontSources(sources []FontSource) []FontSource {
	cloned := make([]FontSource, len(sources))
	for index, source := range sources {
		cloned[index] = source
		cloned[index].Data = append([]byte(nil), source.Data...)
	}
	return cloned
}

func (r *Reader) searchAt(index int, runs []TextRun) searchPage {
	if !r.searchSet[index] {
		indexed := searchPage{
			runs:   make([][]rune, len(runs)),
			byRune: make(map[rune][]int),
		}
		for runIndex, run := range runs {
			indexed.runs[runIndex] = []rune(strings.ToLower(run.Text))
			seen := make(map[rune]struct{})
			for _, value := range indexed.runs[runIndex] {
				if _, ok := seen[value]; ok {
					continue
				}
				seen[value] = struct{}{}
				indexed.byRune[value] = append(indexed.byRune[value], runIndex)
			}
		}
		r.search[index] = indexed
		r.searchSet[index] = true
	}
	return r.search[index]
}

func cloneTextRuns(source []TextRun) []TextRun {
	cloned := make([]TextRun, len(source))
	for index, run := range source {
		cloned[index] = run
		cloned[index].Glyphs = append([]Glyph(nil), run.Glyphs...)
	}
	return cloned
}

func sameColor(left, right color.Color) bool {
	if left == nil || right == nil {
		return left == right
	}
	lr, lg, lb, la := left.RGBA()
	rr, rg, rb, ra := right.RGBA()
	return lr == rr && lg == rg && lb == rb && la == ra
}
