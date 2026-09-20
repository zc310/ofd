// Package webreader 包提供适合浏览器调用的 OFD 文档访问接口。
package webreader

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers"
	"github.com/tdewolff/canvas/renderers/pdf"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/utils"
)

const (
	defaultDPI        = 96
	defaultPageWidth  = 210
	defaultPageHeight = 297
	maxDPI            = 600
	maxInputBytes     = 256 << 20
	maxRenderPixels   = 50_000_000
	maxFontBytes      = 32 << 20
	maxRenderPages    = 64
	maxRenderDocs     = 4
	// textCacheCapacity 和 searchCacheCapacity 限制按页缓存的文字与搜索索引，
	// 避免浏览/搜索大文档时把所有页面的布局快照都留在内存中。
	textCacheCapacity   = 64
	searchCacheCapacity = 64
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

// RenderFormat 是页面输出格式。
type RenderFormat string

const (
	// RenderPNG 输出 PNG 位图。为空时也使用该格式。
	RenderPNG RenderFormat = "png"
	// RenderSVG 输出 SVG 矢量文档。
	RenderSVG RenderFormat = "svg"
	// RenderJPG 输出 JPG 位图。JPG 不支持透明度，透明区域使用白色填充。
	RenderJPG RenderFormat = "jpg"
)

// RenderOptions 控制页面输出。DPI 控制 PNG、JPG 以及 PDF 中复杂效果的内部栅格化分辨率；
// PDF 页面主体保留文字和矢量内容，SVG 复杂渐变等仍可能包含栅格回退。
type RenderOptions struct {
	DPI        float64
	Background color.Color
	Format     RenderFormat
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

// FontInfo 描述文档声明的一个字体，不包含嵌入字体数据。
type FontInfo struct {
	// ID 是字体在所属文档内的标识。
	ID uint64
	// Name 是 OFD 声明的字体名称（FontName）。
	Name string
	// Family 是字体族名称（FamilyName），可能为空。
	Family string
	// Bold、Italic 表示字体声明为粗体或斜体。
	Bold   bool
	Italic bool
	// Serif 表示衬线字体，FixedWidth 表示等宽字体。
	Serif      bool
	FixedWidth bool
	// Format 是嵌入字体文件的格式（如 ttf、otf），无嵌入时为空。
	Format string
	// Embedded 表示文档是否内嵌了字体文件。
	Embedded bool
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

// DocumentInfo 描述 OFD 文档的元数据信息。
type DocumentInfo struct {
	DocID        string
	Title        string
	Author       string
	Subject      string
	Abstract     string
	CreationDate string
	ModDate      string
	Creator      string
	Version      string
}

// OutlineDest 描述跳转目标的位置与缩放，单位与 OFD 页面坐标一致（毫米）。
// 字段为 nil 表示文档未指定该值。
type OutlineDest struct {
	// Type 是目标类型，如 XYZ、Fit、FitH、FitV、FitR。
	Type string
	// Left、Top、Right、Bottom 是目标视图的边界。
	Left   *float64
	Top    *float64
	Right  *float64
	Bottom *float64
	// Zoom 是目标缩放比例。
	Zoom *float64
}

// OutlineNode 描述文档大纲中的一个节点。
type OutlineNode struct {
	// Title 是大纲项标题。
	Title string
	// Page 是从 0 开始的目标页索引，-1 表示没有可跳转目标。
	Page int
	// URI 是外部链接目标，非空时点击打开链接。
	URI string
	// Dest 是页面跳转目标的位置与缩放，nil 表示没有位置信息。
	Dest *OutlineDest
	// Expanded 是文档声明的是否默认展开子项，nil 表示未声明。
	Expanded *bool
	// Children 是子大纲项。
	Children []OutlineNode
}

// Bookmark 描述文档书签（命名目标）。
type Bookmark struct {
	// Name 是书签名称。
	Name string
	// Page 是从 0 开始的目标页索引，-1 表示无法解析。
	Page int
	// Dest 是书签目标的位置与缩放。
	Dest *OutlineDest
}

// OutlineTree 描述文档大纲、书签以及文档声明的打开显示模式。
type OutlineTree struct {
	// PageMode 是文档声明的页面显示模式，例如 UseOutlines；为空表示未声明。
	PageMode string
	// Nodes 是顶层大纲项。
	Nodes []OutlineNode
	// Bookmarks 是文档书签列表。
	Bookmarks []Bookmark
}

// ViewPreferences 描述文档声明的阅读器显示偏好。
type ViewPreferences struct {
	// PageLayout 是文档声明的页面布局方式，如 OneColumn、TwoPageL、TwoPageR。
	PageLayout string
	// ZoomMode 是文档声明的缩放模式，如 FitWidth、FitHeight、FitRect。
	ZoomMode string
	// Zoom 是文档声明的自定义缩放比例。
	Zoom *float64
}

// OpenOptions 配置 OFD 打开行为。
type OpenOptions struct {
	// PageCacheCapacity 是页面缓存最多保留的页面数量，0 表示使用默认值。
	PageCacheCapacity int
	// PageCacheBytes 是页面缓存允许使用的估算最大字节数，0 表示使用默认值。
	PageCacheBytes int64
}

// Reader 是一个已打开的 OFD 文档。
// Reader 负责持有文档资源，使用完毕后必须调用 Close。
type Reader struct {
	mu               sync.RWMutex
	cacheMu          sync.Mutex
	renderDocsMu     sync.Mutex
	closed           bool
	ofd              *parser.OFD
	pages            []pageRef
	renderDocs       *utils.LRU[renderDocumentKey, *render.Document]
	fallbackFamily   string
	fallbackFamilies []string
	text             *utils.LRU[int, []TextRun]
	search           *utils.LRU[int, searchPage]
	options          RenderOptions
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

type renderDocumentKey struct {
	base  *render.Document
	dpi   float64
	red   uint32
	green uint32
	blue  uint32
	alpha uint32
}

// Open 从内存中的 OFD 数据创建浏览器文档引擎。
func Open(data []byte) (*Reader, error) {
	return OpenWithOptions(data, OpenOptions{})
}

// OpenWithOptions 从内存中的 OFD 数据创建浏览器文档引擎。
func OpenWithOptions(data []byte, options OpenOptions) (*Reader, error) {
	if len(data) == 0 {
		return nil, errors.New("OFD 数据为空")
	}
	if len(data) > maxInputBytes {
		return nil, fmt.Errorf("OFD 数据超过大小限制 %d MB", maxInputBytes>>20)
	}
	ofd, err := parser.NewOFDWithOptions(data, parser.Options{
		PageCacheCapacity: options.PageCacheCapacity,
		PageCacheBytes:    options.PageCacheBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("解析 OFD 失败: %w", err)
	}

	r := &Reader{
		ofd:     ofd,
		options: RenderOptions{DPI: defaultDPI, Background: color.Transparent},
	}
	for documentIndex, document := range ofd.Documents {
		renderDocument := render.NewDocument(r.options.Background, document)
		for _, page := range renderDocument.Pages {
			r.pages = append(r.pages, pageRef{document: renderDocument, page: page, fontScope: documentIndex})
		}
	}
	if len(r.pages) == 0 {
		_ = ofd.Close()
		return nil, errors.New("OFD 文档没有页面")
	}
	r.text = utils.NewLRU[int, []TextRun](textCacheCapacity, nil)
	r.search = utils.NewLRU[int, searchPage](searchCacheCapacity, nil)
	return r, nil
}

// RegisterFallbackFont 在进程内全局注册回退字体，与具体 Reader 无关。
// 同一字体族只注册一次（幂等）且不复制字体数据；之后可通过
// Reader.UseFallbackFont 应用到某个文档。
func RegisterFallbackFont(source FontSource) error {
	if len(source.Data) == 0 || source.Family == "" {
		return errors.New("回退字体数据或字体族名为空")
	}
	if len(source.Data) > maxFontBytes {
		return fmt.Errorf("回退字体超过大小限制 %d MB", maxFontBytes>>20)
	}
	slog.Info("register fallback font", "name", source.Name, "family", source.Family, "weight", source.Weight, "italic", source.Italic, "bytes", len(source.Data))
	return render.RegisterFallbackFont(source.Data, source.Family, fallbackFontStyle(source))
}

// UseFallbackFont 使当前文档缺失字体时使用已全局注册的回退字体族。
// 该字体族必须先通过 RegisterFallbackFont 注册。文字和搜索快照会失效，
// 因为它们的字形度量可能使用了不同的回退字体。
func (r *Reader) UseFallbackFont(family string) error {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	if family == "" {
		return errors.New("回退字体族名为空")
	}
	if _, ok := render.FallbackFontData(family); !ok {
		return fmt.Errorf("回退字体族 %q 未注册", family)
	}
	seenDocuments := make(map[*render.Document]struct{})
	for _, ref := range r.pages {
		if _, seen := seenDocuments[ref.document]; seen {
			continue
		}
		seenDocuments[ref.document] = struct{}{}
		if err := ref.document.UseFallbackFont(family); err != nil {
			return err
		}
	}
	r.fallbackFamily = family
	r.fallbackFamilies = append(r.fallbackFamilies, family)
	r.renderDocsMu.Lock()
	r.renderDocs = nil
	r.renderDocsMu.Unlock()
	r.text = utils.NewLRU[int, []TextRun](textCacheCapacity, nil)
	r.search = utils.NewLRU[int, searchPage](searchCacheCapacity, nil)
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return 0
	}
	return len(r.pages)
}

// Info 返回 OFD 文档的元数据信息。
func (r *Reader) Info() (DocumentInfo, error) {
	if r == nil {
		return DocumentInfo{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return DocumentInfo{}, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil || len(r.ofd.DocBodies) == 0 {
		return DocumentInfo{}, nil
	}
	body := r.ofd.DocBodies[0]
	info := body.DocInfo
	result := DocumentInfo{
		DocID:   info.DocID,
		Version: r.ofd.Version,
	}
	if info.Title != nil {
		result.Title = *info.Title
	}
	if info.Author != nil {
		result.Author = *info.Author
	}
	if info.Subject != nil {
		result.Subject = *info.Subject
	}
	if info.Abstract != nil {
		result.Abstract = *info.Abstract
	}
	if info.Creator != nil {
		result.Creator = *info.Creator
	}
	if info.CreationDate != nil {
		result.CreationDate = info.CreationDate.String()
	}
	if info.ModDate != nil {
		result.ModDate = info.ModDate.String()
	}
	return result, nil
}

// Outline 返回文档大纲树、书签列表以及文档声明的显示模式。
// 跳转目标按全局页索引解析（跨文档体累加），无法解析的目标为 -1。
// 没有大纲或书签时返回空切片。
func (r *Reader) Outline() (OutlineTree, error) {
	if r == nil {
		return OutlineTree{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return OutlineTree{}, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil {
		return OutlineTree{}, nil
	}
	var result OutlineTree
	base := 0
	for _, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		pageIndexes := make(map[models.StID]int, len(document.Pages))
		for index, page := range document.Pages {
			if page != nil {
				pageIndexes[page.ID] = base + index
			}
		}
		resolvePage := func(dest models.CtDest) int {
			if page, ok := pageIndexes[models.StID(dest.PageID)]; ok {
				return page
			}
			return -1
		}
		bookmarkDests := make(map[string]models.CtDest)
		if document.Bookmarks != nil {
			for _, bookmark := range document.Bookmarks.Bookmarks {
				bookmarkDests[bookmark.Name] = bookmark.Dest
				result.Bookmarks = append(result.Bookmarks, Bookmark{
					Name: bookmark.Name,
					Page: resolvePage(bookmark.Dest),
					Dest: convertOutlineDest(bookmark.Dest),
				})
			}
		}
		if document.Outlines != nil {
			result.Nodes = append(result.Nodes, outlineNodes(document.Outlines.OutlineElems, resolvePage, bookmarkDests)...)
		}
		if result.PageMode == "" && document.VPreferences != nil && document.VPreferences.PageMode != nil {
			result.PageMode = string(*document.VPreferences.PageMode)
		}
		base += len(document.Pages)
	}
	return result, nil
}

// Preferences 返回文档声明的阅读器显示偏好（取第一个声明了偏好的文档体）。
// 没有声明时返回零值。
func (r *Reader) Preferences() (ViewPreferences, error) {
	if r == nil {
		return ViewPreferences{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return ViewPreferences{}, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil {
		return ViewPreferences{}, nil
	}
	for _, document := range r.ofd.Documents {
		if document == nil || document.VPreferences == nil {
			continue
		}
		preferences := document.VPreferences
		var result ViewPreferences
		if preferences.PageLayout != nil {
			result.PageLayout = string(*preferences.PageLayout)
		}
		if preferences.Zoom != nil {
			if preferences.Zoom.Mode != nil {
				result.ZoomMode = *preferences.Zoom.Mode
			}
			result.Zoom = preferences.Zoom.Value
		}
		return result, nil
	}
	return ViewPreferences{}, nil
}

// convertOutlineDest 把 OFD 目标转换为对调用方友好的位置与缩放描述。
func convertOutlineDest(dest models.CtDest) *OutlineDest {
	return &OutlineDest{
		Type:   string(dest.Type),
		Left:   dest.Left,
		Top:    dest.Top,
		Right:  dest.Right,
		Bottom: dest.Bottom,
		Zoom:   dest.Zoom,
	}
}

// outlineNodes 递归转换大纲项；目标优先取 Goto.Dest，其次按书签名称解析，
// 没有页面目标时回退到 URI 链接。
func outlineNodes(elems []models.CTOutlineElem, resolvePage func(models.CtDest) int, bookmarks map[string]models.CtDest) []OutlineNode {
	if len(elems) == 0 {
		return nil
	}
	nodes := make([]OutlineNode, 0, len(elems))
	for _, elem := range elems {
		node := OutlineNode{Title: elem.Title, Page: -1}
		if elem.Actions != nil {
			for _, action := range elem.Actions.Actions {
				if action.URI != nil && action.URI.URI != "" && node.URI == "" {
					node.URI = action.URI.URI
				}
				if action.Goto == nil {
					continue
				}
				if action.Goto.Dest != nil {
					node.Page = resolvePage(*action.Goto.Dest)
					node.Dest = convertOutlineDest(*action.Goto.Dest)
					node.URI = ""
					break
				}
				if action.Goto.Bookmark != nil {
					if dest, ok := bookmarks[action.Goto.Bookmark.Name]; ok {
						node.Page = resolvePage(dest)
						node.Dest = convertOutlineDest(dest)
						node.URI = ""
						break
					}
				}
			}
		}
		node.Expanded = elem.Expanded
		node.Children = outlineNodes(elem.OutlineElem, resolvePage, bookmarks)
		nodes = append(nodes, node)
	}
	return nodes
}

// Pages 返回所有页面的尺寸快照，尺寸单位为毫米。
// 优先读取页面 XML 中的 Area/PhysicalBox，不加载页面内容或资源。
func (r *Reader) Pages() ([]PageInfo, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}

	pages := make([]PageInfo, len(r.pages))
	for index, ref := range r.pages {
		if ref.document == nil || ref.document.Document == nil || ref.page == nil {
			return nil, fmt.Errorf("第 %d 页所属文档为空", index)
		}
		box := ref.document.CommonData.PageArea.PhysicalBox
		pageBox, err := ref.page.PhysicalBoxMetadata()
		if err == nil && finitePositive(pageBox.Width) && finitePositive(pageBox.Height) {
			box = pageBox
		}
		if !finitePositive(box.Width) || !finitePositive(box.Height) {
			box.Width = defaultPageWidth
			box.Height = defaultPageHeight
		}
		pages[index] = PageInfo{Index: index, Width: box.Width, Height: box.Height}
	}
	return pages, nil
}

// Page 返回指定页面的真实尺寸信息。
// 与 Pages 不同，查询单页信息会按需加载该页完整内容。
func (r *Reader) Page(index int) (PageInfo, error) {
	if r == nil {
		return PageInfo{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return PageInfo{}, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return PageInfo{}, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return r.pageInfoLocked(index)
}

func (r *Reader) pageInfoLocked(index int) (PageInfo, error) {
	ref := r.pages[index]
	if ref.page == nil {
		return PageInfo{}, fmt.Errorf("第 %d 页为空", index)
	}
	box, err := ref.page.PhysicalBox()
	if err != nil {
		return PageInfo{}, fmt.Errorf("读取第 %d 页失败: %w", index, err)
	}
	if !finitePositive(box.Width) || !finitePositive(box.Height) {
		return PageInfo{}, fmt.Errorf("第 %d 页尺寸无效", index)
	}
	return PageInfo{Index: index, Width: box.Width, Height: box.Height}, nil
}

// Text 返回指定页面的文字对象快照。文字顺序与 OFD 页面绘制顺序一致。
func (r *Reader) Text(index int) ([]TextRun, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	resources := make([]FontResource, 0)
	seen := make(map[string]struct{})
	for documentIndex, document := range r.ofd.Documents {
		document.ForEachFont(func(id models.StID, font *models.Font) bool {
			if font == nil || font.FontFile == "" {
				return true
			}
			key := fmt.Sprintf("%d:%d:%s", documentIndex, id, font.FontFile)
			if _, ok := seen[key]; ok {
				return true
			}
			seen[key] = struct{}{}
			data, err := document.FileCache.ReadLimit(string(font.FontFile), maxFontBytes)
			if err != nil {
				return true
			}
			if fixed, fixErr := fontfix.Repair(data); fixErr == nil {
				data = fixed
			}
			resources = append(resources, FontResource{
				ID: uint64(id), Family: browserFontFamily(documentIndex, uint64(id)), Name: font.FontName,
				Bold: font.Bold, Italic: font.Italic, Format: fontFormat(font.FontFile), Data: append([]byte(nil), data...),
			})
			return true
		})
	}
	return resources, nil
}

// FontList 返回文档声明的全部字体（含没有嵌入文件的逻辑字体），不读取字体数据。
func (r *Reader) FontList() ([]FontInfo, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil {
		return nil, nil
	}
	fonts := make([]FontInfo, 0)
	seen := make(map[string]struct{})
	for documentIndex, document := range r.ofd.Documents {
		document.ForEachFont(func(id models.StID, font *models.Font) bool {
			if font == nil {
				return true
			}
			key := fmt.Sprintf("%d:%d", documentIndex, id)
			if _, ok := seen[key]; ok {
				return true
			}
			seen[key] = struct{}{}
			embedded := font.FontFile != ""
			format := ""
			if embedded {
				format = fontFormat(font.FontFile)
			}
			fonts = append(fonts, FontInfo{
				ID:         uint64(id),
				Name:       font.FontName,
				Family:     font.FamilyName,
				Bold:       font.Bold,
				Italic:     font.Italic,
				Serif:      font.Serif,
				FixedWidth: font.FixedWidth,
				Format:     format,
				Embedded:   embedded,
			})
			return true
		})
	}
	return fonts, nil
}

// Search 在所有页面的文字对象中查找 query，匹配不区分大小写。
func (r *Reader) Search(query string) ([]SearchResult, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	query = strings.TrimSpace(query)
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
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

// RenderPage 将指定页面渲染为 PNG、JPG 或 SVG 数据。
func (r *Reader) RenderPage(index int, options RenderOptions) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	return r.renderPage(index, options)
}

// RenderPages 将多个页面按传入顺序渲染为 PNG、JPG 或 SVG 数据。
// 所有页面共享一次 Reader 锁和同一份文档状态；渲染本身仍按顺序执行。
func (r *Reader) RenderPages(indices []int, options RenderOptions) ([][]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	if len(indices) > maxRenderPages {
		return nil, fmt.Errorf("批量渲染页面数量超过限制 %d", maxRenderPages)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	results := make([][]byte, 0, len(indices))
	for _, index := range indices {
		data, err := r.renderPage(index, options)
		if err != nil {
			return nil, err
		}
		results = append(results, data)
	}
	return results, nil
}

// RenderPDF 将多个页面按传入顺序写入一个保留文字和矢量内容的 PDF 文档。
// 该接口会把完整 PDF 保存在内存中，需要导出大量页面时应使用 RenderPDFTo。
func (r *Reader) RenderPDF(indices []int, options RenderOptions) (outputBytes []byte, err error) {
	var output bytes.Buffer
	err = r.RenderPDFTo(&output, indices, options)
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// RenderPDFTo 将多个页面按传入顺序写入 output，保留文字和矢量内容。
// output 会在 PDF 生成过程中接收数据，适合流式保存大 PDF，因此不限制页数。
func (r *Reader) RenderPDFTo(output io.Writer, indices []int, options RenderOptions) (err error) {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	if output == nil {
		return errors.New("PDF 输出为空")
	}
	if len(indices) == 0 {
		return errors.New("PDF 页面列表为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	background := options.Background
	if background == nil {
		background = color.Transparent
	}
	dpi := options.DPI
	if dpi == 0 {
		dpi = defaultDPI
	}
	if dpi < 1 || dpi > maxDPI || math.IsNaN(dpi) || math.IsInf(dpi, 0) {
		return fmt.Errorf("DPI 必须在 1 到 %d 之间", maxDPI)
	}
	// TrueType 字体应进行子集化，避免将完整中文字体写入每个 PDF。
	// CFF/TTC 回退字体暂时不能交给 canvas 的 CFF 子集器；对这类字体
	// 关闭子集化仍会保留 ToUnicode 和原生文字对象，避免 Close 时 panic。
	pdfOptions := &pdf.Options{Compress: true, SubsetFonts: !r.hasUnsafeFallbackFontSubset()}
	var document *pdf.PDF
	defer func() {
		if document == nil {
			return
		}
		if closeErr := document.Close(); closeErr != nil {
			if err == nil {
				err = fmt.Errorf("关闭 PDF 文档失败: %w", closeErr)
			}
		}
	}()
	for position, index := range indices {
		page, pageErr := r.pdfPage(index, background, canvas.DPI(dpi))
		if pageErr != nil {
			return fmt.Errorf("处理 PDF 第 %d 页失败: %w", position+1, pageErr)
		}
		if document == nil {
			document = pdf.New(output, page.W, page.H, pdfOptions)
		} else {
			document.NewPage(page.W, page.H)
		}
		resolution := canvas.DPI(dpi)
		width := page.W * resolution.DPMM()
		height := page.H * resolution.DPMM()
		if math.IsNaN(width) || math.IsInf(width, 0) || math.IsNaN(height) || math.IsInf(height, 0) || width*height > maxRenderPixels {
			return fmt.Errorf("第 %d 页 PDF 渲染尺寸过大", position+1)
		}
		page.RenderTo(document)
	}
	if document == nil {
		return errors.New("PDF 文档创建失败")
	}
	return nil
}

func (r *Reader) hasUnsafeFallbackFontSubset() bool {
	for _, family := range r.fallbackFamilies {
		data, ok := render.FallbackFontData(family)
		if !ok || len(data) < 4 {
			continue
		}
		signature := string(data[:4])
		if signature == "OTTO" || signature == "ttcf" {
			return true
		}
	}
	return false
}

// pdfPage 获取 PDF 渲染所需的页面画布；调用方必须持有 Reader 读锁。
func (r *Reader) pdfPage(index int, background color.Color, dpi canvas.Resolution) (*canvas.Canvas, error) {
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	ref := r.pages[index]
	lease, err := ref.page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil {
		return nil, errors.New("页面内容为空")
	}
	document, err := r.pageDocument(ref, background, dpi)
	if err != nil {
		return nil, err
	}
	return document.Page(ref.page)
}

// renderPage 将页面渲染为 PNG、JPG 或 SVG；调用方必须持有 Reader 读锁。
func (r *Reader) renderPage(index int, options RenderOptions) ([]byte, error) {
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	if options.DPI == 0 {
		options.DPI = defaultDPI
	}
	if options.DPI < 1 || options.DPI > maxDPI || math.IsNaN(options.DPI) || math.IsInf(options.DPI, 0) {
		return nil, fmt.Errorf("DPI 必须在 1 到 %d 之间", maxDPI)
	}
	format := RenderFormat(strings.ToLower(strings.TrimSpace(string(options.Format))))
	if format == "" {
		format = RenderPNG
	}
	if format != RenderPNG && format != RenderSVG && format != RenderJPG {
		return nil, fmt.Errorf("不支持的页面输出格式: %q", format)
	}

	ref := r.pages[index]
	lease, err := ref.page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil {
		return nil, fmt.Errorf("页面内容为空")
	}
	box := content.Area.PhysicalBox
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
	document, err := r.pageDocument(ref, background, canvas.DPI(options.DPI))
	if err != nil {
		return nil, err
	}
	page, err := document.Page(ref.page)
	if err != nil {
		return nil, fmt.Errorf("渲染第 %d 页失败: %w", index, err)
	}

	var output bytes.Buffer
	if format == RenderSVG {
		if err := page.Write(&output, renderers.SVG()); err != nil {
			return nil, fmt.Errorf("编码第 %d 页 SVG 失败: %w", index, err)
		}
		return output.Bytes(), nil
	}
	var rendered image.Image = render.Rasterize(page, canvas.DPI(options.DPI), canvas.DefaultColorSpace)
	if format == RenderJPG {
		rendered = opaqueImage(rendered, color.White)
		if err := jpeg.Encode(&output, rendered, &jpeg.Options{Quality: 90}); err != nil {
			return nil, fmt.Errorf("编码第 %d 页 JPG 失败: %w", index, err)
		}
		return output.Bytes(), nil
	}
	if err := png.Encode(&output, rendered); err != nil {
		return nil, fmt.Errorf("编码第 %d 页 PNG 失败: %w", index, err)
	}
	return output.Bytes(), nil
}

func opaqueImage(source image.Image, background color.Color) image.Image {
	bounds := source.Bounds()
	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, &image.Uniform{C: background}, image.Point{}, draw.Src)
	draw.Draw(result, bounds, source, bounds.Min, draw.Over)
	return result
}

// pageDocument 获取页面对应的渲染文档；调用方必须持有 Reader 读锁。
func (r *Reader) pageDocument(ref pageRef, background color.Color, dpi canvas.Resolution) (*render.Document, error) {
	document := ref.document
	if document == nil || document.Document == nil {
		return nil, errors.New("页面渲染上下文为空")
	}
	red, green, blue, alpha := background.RGBA()
	key := renderDocumentKey{base: document, dpi: dpi.DPI(), red: red, green: green, blue: blue, alpha: alpha}
	r.renderDocsMu.Lock()
	defer r.renderDocsMu.Unlock()
	if r.renderDocs != nil {
		if cached, ok := r.renderDocs.Get(key); ok && cached != nil {
			return cached, nil
		}
	}
	document = render.NewDocumentWithDPI(background, ref.document.Document, dpi)
	for _, family := range r.fallbackFamilies {
		// 字体已在打开/注册时全局登记，这里只把已锁定字体族应用到该文档。
		_ = document.UseFallbackFont(family)
	}
	if r.renderDocs == nil {
		r.renderDocs = utils.NewLRU[renderDocumentKey, *render.Document](maxRenderDocs, nil)
	}
	r.renderDocs.Add(key, document)
	return document, nil
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
	r.search = nil
	r.fallbackFamily = ""
	r.fallbackFamilies = nil
	r.renderDocsMu.Lock()
	r.renderDocs = nil
	r.renderDocsMu.Unlock()
	ofd := r.ofd
	r.ofd = nil
	if ofd == nil {
		return nil
	}
	return ofd.Close()
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

func (r *Reader) searchAt(index int, runs []TextRun) searchPage {
	if indexed, ok := r.search.Get(index); ok {
		return indexed
	}
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
	r.search.Add(index, indexed)
	return indexed
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
