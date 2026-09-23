package merge

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/export"
	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

// PageLimits 限制模型级合并的输入规模。
type PageLimits struct {
	// MaxInputBytes 是单个输入 OFD 的最大字节数，0 表示默认 512MB。
	MaxInputBytes int64
	// MaxTotalBytes 是所有输入 OFD 的总字节数上限，0 表示默认 1GB。
	MaxTotalBytes int64
	// MaxPages 是输出合并文档的最大页数，0 表示默认 100000。
	MaxPages int
}

const (
	defaultPageInputBytes  = int64(512 << 20)
	defaultPageTotalBytes  = int64(1024 << 20)
	defaultPageCount       = 100000
	defaultPageConcurrency = 4
)

// PageOptions 控制模型级页面合并的输出方式。
type PageOptions struct {
	// Compression 是输出 ZIP 的压缩策略，空值时使用 creator.CompressionAuto。
	Compression creator.CompressionMode
	// CompressionLevel 是 DEFLATE 压缩级别，0 使用默认级别 5，显式范围
	// 1（最快）到 9（最紧凑）。仅对实际使用 Deflate 的条目生效。
	CompressionLevel int
	// Deterministic 使用固定 ZIP 时间，生成可复现的合并结果。
	Deterministic bool
	// Pages 指定输出页序，元素为按输入顺序拼接后的 1 起始全局页码；为空时输出
	// 全部页面。允许重复与重排，越界会返回错误。与 Selectors 互斥。
	Pages []int
	// Selectors 按来源选页，元素为 1 起始的来源序号与该来源内 1 起始的页码；
	// Source 为 0 表示全局页序。与 Pages 互斥。
	Selectors []PageSelector
	// Limits 限制输入字节数与输出页数，零值使用默认限制。
	Limits PageLimits
	// Concurrency 是并行解析/转换输入的并发数，0 表示默认 4，1 表示串行。
	Concurrency int
	// OnSignature 可选，接收每个输入签名在模型级合并中被丢弃的结果。
	OnSignature func(SignatureEvent)
	// ID、Title、Author、Subject 可选覆盖输出文档元数据；为空时沿用首个来源。
	ID      string
	Title   string
	Author  string
	Subject string
}

// PageSelector 选择某个来源的页面。Source 是 1 起始的输入序号，0 表示全局页序；
// Pages 是 1 起始的页码，Source 非 0 时相对该来源的第一页，为空表示该来源全部页面。
type PageSelector struct {
	Source int
	Pages  []int
}

// ParsePages 解析 "1,3-5" 形式的全局页码表达式，返回从 1 开始的页序。
func ParsePages(spec string) ([]int, error) {
	return parsePageList(spec)
}

// ParsePageSelection 解析选页表达式，支持全局页序与按来源选择：
//
//	"1,3-5"          全局页序（拼接后从 1 开始）
//	"s1:1,3-5;s2:2"  第 1 个输入的第 1、3、4、5 页，再第 2 个输入的第 2 页
//
// 来源序号从 1 开始，按输入（`-i`/位置参数）顺序编号；空串返回空选择。
func ParsePageSelection(spec string) ([]PageSelector, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	var selectors []PageSelector
	for _, part := range strings.Split(spec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("页选择包含空段: %q", spec)
		}
		selector := PageSelector{}
		list := part
		if strings.HasPrefix(part, "s") || strings.HasPrefix(part, "S") {
			index := strings.Index(part, ":")
			if index < 0 {
				source, err := strconv.Atoi(strings.TrimSpace(part[1:]))
				if err != nil || source < 1 {
					return nil, fmt.Errorf("来源编号无效: %q", part)
				}
				selector.Source = source
				selectors = append(selectors, selector)
				continue
			}
			source, err := strconv.Atoi(strings.TrimSpace(part[1:index]))
			if err != nil || source < 1 {
				return nil, fmt.Errorf("来源编号无效: %q", part)
			}
			selector.Source = source
			list = strings.TrimSpace(part[index+1:])
			if list == "" {
				return nil, fmt.Errorf("按源选页缺少页码: %q", part)
			}
		}
		pages, err := parsePageList(list)
		if err != nil {
			return nil, err
		}
		selector.Pages = pages
		selectors = append(selectors, selector)
	}
	return selectors, nil
}

func parsePageList(spec string) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("页码列表为空")
	}
	var result []int
	for _, token := range strings.Split(spec, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, fmt.Errorf("页码列表包含空项: %q", spec)
		}
		if index := strings.Index(token, "-"); index >= 0 {
			start, startErr := strconv.Atoi(strings.TrimSpace(token[:index]))
			end, endErr := strconv.Atoi(strings.TrimSpace(token[index+1:]))
			if startErr != nil || endErr != nil || start < 1 || end < start {
				return nil, fmt.Errorf("页码范围无效: %q", token)
			}
			for page := start; page <= end; page++ {
				result = append(result, page)
			}
			continue
		}
		page, err := strconv.Atoi(token)
		if err != nil || page < 1 {
			return nil, fmt.Errorf("页码无效: %q", token)
		}
		result = append(result, page)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("页码列表为空: %q", spec)
	}
	return result, nil
}

// Pages 把 inputs 中每个文档体的页面合并成一个单文档 OFD 并写入 w。
//
// 与 ZIP 级合并不同，模型级合并会把各源解析为 creator 模型后重新生成，
// 因此文档级资源（字体名、绘制参数名、图片/颜色空间/复合图元/模板 ID）会被
// 重新命名或编号，并同步改写引用。页面级资源保持原样，若跨源发生冲突会由
// 创建器报错。签名和版本不会保留；大纲、书签、动作和页面注解会保留，并按其
// 来源与最终页序重写跳转页索引，目标页未被保留的动作或注解会被丢弃。
func Pages(inputs []Source, w io.Writer, options PageOptions) error {
	if w == nil {
		return errors.New("OFD 输出写入器为空")
	}
	if len(inputs) == 0 {
		return errors.New("至少需要一个 OFD 输入")
	}
	createOptions, err := pageCreateOptions(options)
	if err != nil {
		return err
	}
	limits, err := resolvePageLimits(options.Limits)
	if err != nil {
		return err
	}

	if options.Concurrency < 0 {
		return errors.New("合并并发数不能为负数")
	}

	prepared, err := preparePageInputs(inputs, limits)
	if err != nil {
		return err
	}
	results := buildPageSources(prepared, limits, resolvePageConcurrency(options.Concurrency, len(prepared)))

	merged := creator.Document{}
	ids := newIDRemap()
	first := true
	sourcePages := make([]int, 0, len(results))
	for index, result := range results {
		if result.err != nil {
			return fmt.Errorf("处理输入 %s 失败: %w", prepared[index].name, result.err)
		}
		for _, built := range result.documents {
			for _, id := range built.signatures {
				if options.OnSignature != nil {
					options.OnSignature(SignatureEvent{Input: prepared[index].name, DocumentIndex: built.documentIndex, ID: id, Action: SignatureDropped})
				}
			}
			document := built.document
			remapDocument(&document, built.prefix, ids)
			mergePageDocument(&merged, document, first)
			first = false
		}
		sourcePages = append(sourcePages, result.pages)
	}
	if len(merged.Pages) == 0 {
		return errors.New("没有可合并的页面")
	}
	if len(options.Pages) > 0 && len(options.Selectors) > 0 {
		return errors.New("Pages 与 Selectors 不能同时设置")
	}
	totalPages := len(merged.Pages)
	var selectedGlobal []int
	switch {
	case len(options.Pages) > 0:
		selectedGlobal = make([]int, 0, len(options.Pages))
		for _, page := range options.Pages {
			if page < 1 || page > totalPages {
				return fmt.Errorf("页码 %d 超出范围，共 %d 页", page, totalPages)
			}
			selectedGlobal = append(selectedGlobal, page-1)
		}
	case len(options.Selectors) > 0:
		var err error
		selectedGlobal, err = selectGlobalPages(totalPages, sourcePages, options.Selectors)
		if err != nil {
			return err
		}
	default:
		selectedGlobal = make([]int, 0, totalPages)
		for index := 0; index < totalPages; index++ {
			selectedGlobal = append(selectedGlobal, index)
		}
	}
	pageMap := make(map[int]int, len(selectedGlobal))
	pages := make([]creator.Page, len(selectedGlobal))
	for outputIndex, globalIndex := range selectedGlobal {
		pageMap[globalIndex] = outputIndex
		pages[outputIndex] = merged.Pages[globalIndex]
	}
	merged.Pages = pages
	remapDocumentGotos(&merged, pageMap)
	if len(merged.Pages) > limits.MaxPages {
		return fmt.Errorf("输出页数 %d 超过上限 %d", len(merged.Pages), limits.MaxPages)
	}
	if options.ID != "" {
		merged.ID = options.ID
	}
	if options.Title != "" {
		merged.Title = options.Title
	}
	if options.Author != "" {
		merged.Author = options.Author
	}
	if options.Subject != "" {
		merged.Subject = options.Subject
	}
	if strings.TrimSpace(merged.ID) == "" {
		merged.ID = "merged-ofd"
	}
	return creator.CreateWithOptions(merged, w, createOptions)
}

// selectGlobalPages 按来源或全局页序返回选中的全局页索引（0 起始）。
func selectGlobalPages(totalPages int, sourcePages []int, selectors []PageSelector) ([]int, error) {
	starts := make([]int, len(sourcePages))
	offset := 0
	for index, count := range sourcePages {
		starts[index] = offset
		offset += count
	}
	selected := make([]int, 0, totalPages)
	for _, selector := range selectors {
		if selector.Source == 0 {
			for _, page := range selector.Pages {
				if page < 1 || page > totalPages {
					return nil, fmt.Errorf("页码 %d 超出范围，共 %d 页", page, totalPages)
				}
				selected = append(selected, page-1)
			}
			continue
		}
		if selector.Source < 1 || selector.Source > len(sourcePages) {
			return nil, fmt.Errorf("来源编号 %d 超出范围，共 %d 个输入", selector.Source, len(sourcePages))
		}
		count := sourcePages[selector.Source-1]
		start := starts[selector.Source-1]
		if count == 0 {
			return nil, fmt.Errorf("来源 s%d 没有页面", selector.Source)
		}
		pageList := selector.Pages
		if len(pageList) == 0 {
			for page := 1; page <= count; page++ {
				pageList = append(pageList, page)
			}
		}
		for _, page := range pageList {
			if page < 1 || page > count {
				return nil, fmt.Errorf("来源 s%d 的页码 %d 超出范围，共 %d 页", selector.Source, page, count)
			}
			selected = append(selected, start+page-1)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("选页结果为空")
	}
	return selected, nil
}

type pageInput struct {
	name  string
	input any
}

type builtDocument struct {
	document      creator.Document
	prefix        string
	documentIndex int
	signatures    []string
}

type builtSource struct {
	documents []builtDocument
	pages     int
	err       error
}

// preparePageInputs 校验输入来源、大小限制，并物化 Reader 输入。
func preparePageInputs(inputs []Source, limits PageLimits) ([]pageInput, error) {
	result := make([]pageInput, 0, len(inputs))
	total := int64(0)
	for index, src := range inputs {
		name, input, size, err := pageSourceInput(index, src)
		if err != nil {
			return nil, err
		}
		if size > limits.MaxInputBytes {
			return nil, fmt.Errorf("输入 %s 为 %d 字节，超过单输入上限 %d 字节", name, size, limits.MaxInputBytes)
		}
		total += size
		if total > limits.MaxTotalBytes {
			return nil, fmt.Errorf("输入总大小超过上限 %d 字节", limits.MaxTotalBytes)
		}
		result = append(result, pageInput{name: name, input: input})
	}
	return result, nil
}

// buildPageSources 并行（或串行）解析并转换每个输入；结果按输入顺序返回，保证
// 合并阶段的资源编号与输出可复现。
func buildPageSources(inputs []pageInput, limits PageLimits, concurrency int) []builtSource {
	results := make([]builtSource, len(inputs))
	if concurrency <= 1 {
		for index, input := range inputs {
			results[index] = buildSourcePages(index, input, limits)
		}
		return results
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for index, input := range inputs {
		wg.Add(1)
		go func(index int, input pageInput) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[index] = buildSourcePages(index, input, limits)
		}(index, input)
	}
	wg.Wait()
	return results
}

func buildSourcePages(index int, input pageInput, limits PageLimits) builtSource {
	ofd, err := parser.NewOFDWithOptions(input.input, parser.Options{MaxInputBytes: limits.MaxInputBytes})
	if err != nil {
		return builtSource{err: fmt.Errorf("打开 OFD 输入 %s 失败: %w", input.name, err)}
	}
	defer func() { _ = ofd.Close() }()

	var result builtSource
	for documentIndex := range ofd.Documents {
		if ofd.Documents[documentIndex] == nil {
			continue
		}
		prefix := fmt.Sprintf("s%d_%d_", index, documentIndex)
		assets := make(map[string][]byte)
		loadAsset := func(name string) ([]byte, error) {
			data, ok := assets[name]
			if !ok {
				return nil, fmt.Errorf("缺少合并资源 %s", name)
			}
			return data, nil
		}
		document, err := buildPageDocument(ofd, documentIndex, prefix, assets, loadAsset)
		if err != nil {
			return builtSource{err: fmt.Errorf("转换 %s 的文档体 %d 失败: %w", input.name, documentIndex, err)}
		}
		result.pages += len(document.Pages)
		var signatureIDs []string
		ofd.Documents[documentIndex].ForEachSignature(func(id string, _ *models.Signature) bool {
			signatureIDs = append(signatureIDs, id)
			return true
		})
		result.documents = append(result.documents, builtDocument{document: document, prefix: prefix, documentIndex: documentIndex, signatures: signatureIDs})
	}
	return result
}

func resolvePageConcurrency(value, count int) int {
	if value == 0 {
		value = defaultPageConcurrency
	}
	if value > count {
		value = count
	}
	if value < 1 {
		value = 1
	}
	return value
}

func resolvePageLimits(limits PageLimits) (PageLimits, error) {
	if limits.MaxInputBytes < 0 || limits.MaxTotalBytes < 0 || limits.MaxPages < 0 {
		return PageLimits{}, errors.New("合并规模限制不能为负数")
	}
	if limits.MaxInputBytes == 0 {
		limits.MaxInputBytes = defaultPageInputBytes
	}
	if limits.MaxTotalBytes == 0 {
		limits.MaxTotalBytes = defaultPageTotalBytes
	}
	if limits.MaxPages == 0 {
		limits.MaxPages = defaultPageCount
	}
	return limits, nil
}

func pageCreateOptions(options PageOptions) (creator.CreateOptions, error) {
	compression := options.Compression
	if compression == "" {
		compression = creator.CompressionAuto
	}
	switch compression {
	case creator.CompressionAuto, creator.CompressionDeflate, creator.CompressionStore:
	default:
		return creator.CreateOptions{}, fmt.Errorf("不支持的 ZIP 压缩策略: %q", compression)
	}
	level, err := creator.NormalizeCompressionLevel(options.CompressionLevel)
	if err != nil {
		return creator.CreateOptions{}, err
	}
	return creator.CreateOptions{Compression: compression, CompressionLevel: level, Deterministic: options.Deterministic}, nil
}

// buildPageDocument 把单个文档体导出为 creator 文档，资源收集到 assets。
func buildPageDocument(ofd *parser.OFD, index int, prefix string, assets map[string][]byte, loadAsset func(string) ([]byte, error)) (creator.Document, error) {
	value, err := export.BuildManifestFromOFD(ofd, index, export.Options{
		AssetPrefix: prefix,
		AssetSink: func(name string, data []byte) error {
			assets[path.Join(prefix, name)] = data
			return nil
		},
	})
	if err != nil {
		return creator.Document{}, err
	}
	return value.BuildWithOptions("", "", manifest.BuildOptions{LoadAsset: loadAsset})
}

// mergePageDocument 把 source 的页面与资源追加到 target。first 为真时同时复制
// 文档元数据。
func mergePageDocument(target *creator.Document, source creator.Document, first bool) {
	if first {
		target.ID = source.ID
		target.Title = source.Title
		target.Author = source.Author
		target.Subject = source.Subject
		target.Abstract = source.Abstract
		target.Creator = source.Creator
		target.CreatorVersion = source.CreatorVersion
		target.DocUsage = source.DocUsage
		target.Keywords = source.Keywords
		target.CustomDatas = source.CustomDatas
		target.CreationDate = source.CreationDate
		target.ModDate = source.ModDate
		target.PageSize = source.PageSize
		target.Area = source.Area
		target.DefaultCS = source.DefaultCS
		target.Permissions = source.Permissions
		target.Preferences = source.Preferences
	}
	base := len(target.Pages)
	offsetActions(source.Actions, base)
	offsetBookmarks(source.Bookmarks, base)
	offsetOutlines(source.Outlines, base)
	offsetAnnotations(source.Annotations, base)
	target.Actions = append(target.Actions, source.Actions...)
	target.Bookmarks = append(target.Bookmarks, source.Bookmarks...)
	target.Outlines = append(target.Outlines, source.Outlines...)
	target.Annotations = append(target.Annotations, source.Annotations...)
	target.Pages = append(target.Pages, source.Pages...)
	target.Fonts = append(target.Fonts, source.Fonts...)
	target.DrawParams = append(target.DrawParams, source.DrawParams...)
	target.Media = append(target.Media, source.Media...)
	target.ColorSpaces = append(target.ColorSpaces, source.ColorSpaces...)
	target.Composites = append(target.Composites, source.Composites...)
	target.Templates = append(target.Templates, source.Templates...)
	target.PublicRes = append(target.PublicRes, source.PublicRes...)
	target.Attachments = append(target.Attachments, source.Attachments...)
	target.Extensions = append(target.Extensions, source.Extensions...)
	target.CustomTags = append(target.CustomTags, source.CustomTags...)
}

// offsetActions 把跳转动作的页索引按来源在合并文档中的起始页偏移。
func offsetActions(actions []creator.Action, base int) {
	if base == 0 {
		return
	}
	for index := range actions {
		if actions[index].Goto != nil {
			actions[index].Goto.Page += base
		}
	}
}

func offsetBookmarks(bookmarks []creator.Bookmark, base int) {
	if base == 0 {
		return
	}
	for index := range bookmarks {
		bookmarks[index].Goto.Page += base
	}
}

func offsetOutlines(outlines []creator.Outline, base int) {
	if base == 0 {
		return
	}
	for index := range outlines {
		offsetActions(outlines[index].Actions, base)
		offsetOutlines(outlines[index].Children, base)
	}
}

func offsetAnnotations(pages []creator.AnnotationPage, base int) {
	if base == 0 {
		return
	}
	for index := range pages {
		pages[index].Page += base
	}
}

// remapDocumentGotos 按最终页序重写所有跳转动作与书签的页索引，目标页未保留的
// 动作会被丢弃。
func remapDocumentGotos(document *creator.Document, pageMap map[int]int) {
	document.Actions = filterGotoActions(document.Actions, pageMap)
	document.Bookmarks = filterBookmarks(document.Bookmarks, pageMap)
	document.Outlines = remapOutlineGotos(document.Outlines, pageMap)
	for index := range document.Pages {
		page := &document.Pages[index]
		page.Actions = filterGotoActions(page.Actions, pageMap)
		remapItemGotos(page.Items, pageMap)
		for layerIndex := range page.Layers {
			remapItemGotos(page.Layers[layerIndex].Items, pageMap)
		}
	}
	for index := range document.Templates {
		remapItemGotos(document.Templates[index].Items, pageMap)
		for layerIndex := range document.Templates[index].Layers {
			remapItemGotos(document.Templates[index].Layers[layerIndex].Items, pageMap)
		}
	}
	for index := range document.Composites {
		remapItemGotos(document.Composites[index].Items, pageMap)
	}
	document.Annotations = finalizeAnnotations(document.Annotations, pageMap)
	for index := range document.Annotations {
		for annotationIndex := range document.Annotations[index].Items {
			remapItemGotos(document.Annotations[index].Items[annotationIndex].Items, pageMap)
		}
	}
}

// finalizeAnnotations 按最终页序重写注解所属页，把同一页的注解合并为一个
// AnnotationPage 并重新编号，目标页未保留的注解丢弃。
func finalizeAnnotations(pages []creator.AnnotationPage, pageMap map[int]int) []creator.AnnotationPage {
	if len(pages) == 0 {
		return pages
	}
	byPage := make(map[int]*creator.AnnotationPage)
	order := make([]int, 0, len(pages))
	nextID := uint64(1)
	for _, page := range pages {
		mapped, ok := pageMap[page.Page]
		if !ok {
			continue
		}
		target, exists := byPage[mapped]
		if !exists {
			target = &creator.AnnotationPage{Page: mapped}
			byPage[mapped] = target
			order = append(order, mapped)
		}
		for _, annotation := range page.Items {
			annotation.ID = nextID
			nextID++
			target.Items = append(target.Items, annotation)
		}
	}
	result := make([]creator.AnnotationPage, 0, len(order))
	for _, page := range order {
		result = append(result, *byPage[page])
	}
	return result
}

func filterGotoActions(actions []creator.Action, pageMap map[int]int) []creator.Action {
	if len(actions) == 0 {
		return actions
	}
	result := make([]creator.Action, 0, len(actions))
	for _, action := range actions {
		if action.Goto != nil {
			mapped, ok := pageMap[action.Goto.Page]
			if !ok {
				continue
			}
			action.Goto.Page = mapped
		}
		result = append(result, action)
	}
	return result
}

func filterBookmarks(bookmarks []creator.Bookmark, pageMap map[int]int) []creator.Bookmark {
	if len(bookmarks) == 0 {
		return bookmarks
	}
	result := make([]creator.Bookmark, 0, len(bookmarks))
	for _, bookmark := range bookmarks {
		if bookmark.Goto.Bookmark == "" {
			mapped, ok := pageMap[bookmark.Goto.Page]
			if !ok {
				continue
			}
			bookmark.Goto.Page = mapped
		}
		result = append(result, bookmark)
	}
	return result
}

func remapOutlineGotos(outlines []creator.Outline, pageMap map[int]int) []creator.Outline {
	if len(outlines) == 0 {
		return outlines
	}
	result := make([]creator.Outline, 0, len(outlines))
	for _, outline := range outlines {
		outline.Actions = filterGotoActions(outline.Actions, pageMap)
		outline.Children = remapOutlineGotos(outline.Children, pageMap)
		result = append(result, outline)
	}
	return result
}

func remapItemGotos(items []creator.Item, pageMap map[int]int) {
	for index, item := range items {
		switch value := item.(type) {
		case creator.Text:
			value.Actions = filterGotoActions(value.Actions, pageMap)
			items[index] = value
		case *creator.Text:
			if value != nil {
				value.Actions = filterGotoActions(value.Actions, pageMap)
			}
		case creator.Path:
			value.Actions = filterGotoActions(value.Actions, pageMap)
			items[index] = value
		case *creator.Path:
			if value != nil {
				value.Actions = filterGotoActions(value.Actions, pageMap)
			}
		case creator.Image:
			value.Actions = filterGotoActions(value.Actions, pageMap)
			items[index] = value
		case *creator.Image:
			if value != nil {
				value.Actions = filterGotoActions(value.Actions, pageMap)
			}
		case creator.Composite:
			value.Actions = filterGotoActions(value.Actions, pageMap)
			items[index] = value
		case *creator.Composite:
			if value != nil {
				value.Actions = filterGotoActions(value.Actions, pageMap)
			}
		case creator.PageBlock:
			remapItemGotos(value.Items, pageMap)
			items[index] = value
		case *creator.PageBlock:
			if value != nil {
				remapItemGotos(value.Items, pageMap)
			}
		}
	}
}

// idRemap 为文档级数值资源分配全局唯一 ID。
type idRemap struct {
	next uint64
}

func newIDRemap() *idRemap {
	return &idRemap{next: 1}
}

func (r *idRemap) allocate() uint64 {
	id := r.next
	r.next++
	return id
}

// resourceRemap 记录一个文档体资源的重命名与重编号结果。
type resourceRemap struct {
	fonts       map[string]string
	drawParams  map[string]string
	attachments map[string]string
	ids         map[uint64]uint64
}

func (r *resourceRemap) font(name string) string {
	if value, ok := r.fonts[name]; ok {
		return value
	}
	return name
}

func (r *resourceRemap) drawParam(name string) string {
	if value, ok := r.drawParams[name]; ok {
		return value
	}
	return name
}

func (r *resourceRemap) id(value uint64) uint64 {
	if value == 0 {
		return 0
	}
	if mapped, ok := r.ids[value]; ok {
		return mapped
	}
	return value
}

// remapDocument 重命名/重编号文档级资源，并同步改写页面、模板、复合图元和绘制
// 参数中的引用。
func remapDocument(document *creator.Document, prefix string, ids *idRemap) {
	remap := &resourceRemap{
		fonts:       make(map[string]string),
		drawParams:  make(map[string]string),
		attachments: make(map[string]string),
		ids:         make(map[uint64]uint64),
	}
	for index := range document.PublicRes {
		resource := &document.PublicRes[index]
		if resource.Name != "" {
			resource.Name = prefix + resource.Name
		}
		if len(resource.Data) > 0 {
			if data, err := remapResXML(resource.Data, prefix, remap, ids); err == nil {
				resource.Data = data
			}
		}
	}
	for index := range document.Fonts {
		old := document.Fonts[index].Name
		if old == "" {
			continue
		}
		value := prefix + old
		remap.fonts[old] = value
		document.Fonts[index].Name = value
	}
	for index := range document.DrawParams {
		old := document.DrawParams[index].Name
		if old == "" {
			continue
		}
		value := prefix + old
		remap.drawParams[old] = value
		document.DrawParams[index].Name = value
	}
	for index := range document.Media {
		remap.ids[document.Media[index].ID] = ids.allocate()
		document.Media[index].ID = remap.ids[document.Media[index].ID]
	}
	for index := range document.ColorSpaces {
		remap.ids[document.ColorSpaces[index].ID] = ids.allocate()
		document.ColorSpaces[index].ID = remap.ids[document.ColorSpaces[index].ID]
	}
	for index := range document.Composites {
		remap.ids[document.Composites[index].ID] = ids.allocate()
		document.Composites[index].ID = remap.ids[document.Composites[index].ID]
	}
	for index := range document.Templates {
		remap.ids[document.Templates[index].ID] = ids.allocate()
		document.Templates[index].ID = remap.ids[document.Templates[index].ID]
	}
	document.DefaultCS = remap.id(document.DefaultCS)

	for index := range document.Media {
		if name := strings.TrimSpace(document.Media[index].Name); name != "" {
			document.Media[index].Name = prefix + name
		}
	}
	for index := range document.ColorSpaces {
		if profile := strings.TrimSpace(document.ColorSpaces[index].Profile); profile != "" {
			document.ColorSpaces[index].Profile = prefix + profile
		}
	}
	for index := range document.Attachments {
		attachment := &document.Attachments[index]
		if attachment.ID != "" {
			value := prefix + attachment.ID
			remap.attachments[attachment.ID] = value
			attachment.ID = value
		}
		if attachment.FileName != "" {
			attachment.FileName = prefix + attachment.FileName
		}
	}
	for index := range document.CustomTags {
		tag := &document.CustomTags[index]
		tag.NameSpace = prefix + tag.NameSpace
		if tag.SchemaName != "" {
			tag.SchemaName = prefix + tag.SchemaName
		}
		if tag.DataName != "" {
			tag.DataName = prefix + tag.DataName
		}
	}
	for index := range document.Extensions {
		extension := &document.Extensions[index]
		if extension.DataName != "" {
			extension.DataName = prefix + extension.DataName
		}
		extension.RefID = remap.id(extension.RefID)
	}

	for index := range document.DrawParams {
		remapDrawParam(&document.DrawParams[index], remap)
	}
	for index := range document.Composites {
		remapItems(document.Composites[index].Items, remap)
	}
	for index := range document.Templates {
		remapLayers(document.Templates[index].Layers, remap)
		remapItems(document.Templates[index].Items, remap)
	}
	for index := range document.Annotations {
		for annotationIndex := range document.Annotations[index].Items {
			remapItems(document.Annotations[index].Items[annotationIndex].Items, remap)
		}
	}
	for index := range document.Pages {
		remapPage(&document.Pages[index], prefix, remap, ids)
	}
}

func remapPage(page *creator.Page, prefix string, remap *resourceRemap, ids *idRemap) {
	remapPageResources(page, prefix, remap, ids)
	for index := range page.Templates {
		page.Templates[index].ID = remap.id(page.Templates[index].ID)
	}
	remapLayers(page.Layers, remap)
	remapItems(page.Items, remap)
	remapActions(page.Actions, remap)
}

// remapPageResources 重编号页面资源中的 ID、重命名其中的字体与绘制参数，并同步
// 改写页面资源 XML 内的引用。
func remapPageResources(page *creator.Page, prefix string, remap *resourceRemap, ids *idRemap) {
	for index := range page.Resources {
		resource := &page.Resources[index]
		if len(resource.Data) > 0 {
			if data, err := remapResXML(resource.Data, prefix, remap, ids); err == nil {
				resource.Data = data
			}
		}
		for imageIndex := range resource.Images {
			old := resource.Images[imageIndex].ID
			if old == 0 {
				continue
			}
			if _, exists := remap.ids[old]; !exists {
				remap.ids[old] = ids.allocate()
			}
			resource.Images[imageIndex].ID = remap.ids[old]
		}
	}
}

// remapResXML 重写页面资源 Res XML 中的 ID、FontName、DrawParam 名称及其引用。
func remapResXML(data []byte, prefix string, remap *resourceRemap, ids *idRemap) ([]byte, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, err
	}
	root := doc.Root()
	if root == nil {
		return data, nil
	}
	// 第一遍：登记 ID 定义与字体/绘制参数名称。
	walkElements(root, func(element *etree.Element) {
		if attr := element.SelectAttr("ID"); attr != nil {
			if old, err := strconv.ParseUint(strings.TrimSpace(attr.Value), 10, 64); err == nil && old > 0 {
				if _, exists := remap.ids[old]; !exists {
					remap.ids[old] = ids.allocate()
				}
			}
		}
		if attr := element.SelectAttr("FontName"); attr != nil {
			if old := strings.TrimSpace(attr.Value); old != "" {
				if _, exists := remap.fonts[old]; !exists {
					remap.fonts[old] = prefix + old
				}
			}
		}
		if localName(element) == "DrawParam" {
			if attr := element.SelectAttr("Name"); attr != nil {
				if old := strings.TrimSpace(attr.Value); old != "" {
					if _, exists := remap.drawParams[old]; !exists {
						remap.drawParams[old] = prefix + old
					}
				}
			}
		}
	})
	// 第二遍：应用重编号与重命名。
	walkElements(root, func(element *etree.Element) {
		if attr := element.SelectAttr("ID"); attr != nil {
			if old, err := strconv.ParseUint(strings.TrimSpace(attr.Value), 10, 64); err == nil {
				if value, ok := remap.ids[old]; ok {
					attr.Value = strconv.FormatUint(value, 10)
				}
			}
		}
		if attr := element.SelectAttr("FontName"); attr != nil {
			attr.Value = remap.font(attr.Value)
		}
		if localName(element) == "DrawParam" {
			if attr := element.SelectAttr("Name"); attr != nil {
				attr.Value = remap.drawParam(attr.Value)
			}
		}
		for _, key := range []string{"Font", "DrawParam", "Relative", "ResourceID", "Substitution", "ImageMask", "ColorSpace", "Thumbnail"} {
			if attr := element.SelectAttr(key); attr != nil {
				if old, err := strconv.ParseUint(strings.TrimSpace(attr.Value), 10, 64); err == nil {
					if value, ok := remap.ids[old]; ok {
						attr.Value = strconv.FormatUint(value, 10)
					}
				}
			}
		}
	})
	out, err := doc.WriteToBytes()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func remapLayers(layers []creator.Layer, remap *resourceRemap) {
	for index := range layers {
		layers[index].DrawParam = remap.drawParam(layers[index].DrawParam)
		remapItems(layers[index].Items, remap)
	}
}

func remapItems(items []creator.Item, remap *resourceRemap) {
	for index, item := range items {
		switch value := item.(type) {
		case creator.Text:
			value.Font = remap.font(value.Font)
			value.DrawParam = remap.drawParam(value.DrawParam)
			remapColor(value.FillColor, remap)
			remapColor(value.StrokeColor, remap)
			remapClips(value.Clips, remap)
			remapActions(value.Actions, remap)
			items[index] = value
		case *creator.Text:
			if value != nil {
				value.Font = remap.font(value.Font)
				value.DrawParam = remap.drawParam(value.DrawParam)
				remapColor(value.FillColor, remap)
				remapColor(value.StrokeColor, remap)
				remapClips(value.Clips, remap)
				remapActions(value.Actions, remap)
			}
		case creator.Path:
			value.DrawParam = remap.drawParam(value.DrawParam)
			remapColor(value.FillColor, remap)
			remapColor(value.StrokeColor, remap)
			remapClips(value.Clips, remap)
			remapActions(value.Actions, remap)
			items[index] = value
		case *creator.Path:
			if value != nil {
				value.DrawParam = remap.drawParam(value.DrawParam)
				remapColor(value.FillColor, remap)
				remapColor(value.StrokeColor, remap)
				remapClips(value.Clips, remap)
				remapActions(value.Actions, remap)
			}
		case creator.Image:
			value.DrawParam = remap.drawParam(value.DrawParam)
			value.ResourceID = remap.id(value.ResourceID)
			value.Substitution = remap.id(value.Substitution)
			value.ImageMask = remap.id(value.ImageMask)
			if value.Border != nil {
				remapColor(value.Border.Color, remap)
			}
			remapClips(value.Clips, remap)
			remapActions(value.Actions, remap)
			items[index] = value
		case *creator.Image:
			if value != nil {
				value.DrawParam = remap.drawParam(value.DrawParam)
				value.ResourceID = remap.id(value.ResourceID)
				value.Substitution = remap.id(value.Substitution)
				value.ImageMask = remap.id(value.ImageMask)
				if value.Border != nil {
					remapColor(value.Border.Color, remap)
				}
				remapClips(value.Clips, remap)
				remapActions(value.Actions, remap)
			}
		case creator.Composite:
			value.DrawParam = remap.drawParam(value.DrawParam)
			value.ResourceID = remap.id(value.ResourceID)
			remapClips(value.Clips, remap)
			remapActions(value.Actions, remap)
			items[index] = value
		case *creator.Composite:
			if value != nil {
				value.DrawParam = remap.drawParam(value.DrawParam)
				value.ResourceID = remap.id(value.ResourceID)
				remapClips(value.Clips, remap)
				remapActions(value.Actions, remap)
			}
		case creator.PageBlock:
			remapItems(value.Items, remap)
			items[index] = value
		case *creator.PageBlock:
			if value != nil {
				remapItems(value.Items, remap)
			}
		}
	}
}

func remapActions(actions []creator.Action, remap *resourceRemap) {
	for index := range actions {
		if actions[index].Sound != nil {
			actions[index].Sound.ResourceID = remap.id(actions[index].Sound.ResourceID)
		}
		if actions[index].Movie != nil {
			actions[index].Movie.ResourceID = remap.id(actions[index].Movie.ResourceID)
		}
		if actions[index].GotoA != nil {
			if mapped, ok := remap.attachments[actions[index].GotoA.AttachID]; ok {
				actions[index].GotoA.AttachID = mapped
			}
		}
	}
}

func remapClips(clips *creator.Clips, remap *resourceRemap) {
	if clips == nil {
		return
	}
	for clipIndex := range clips.Items {
		for areaIndex := range clips.Items[clipIndex].Areas {
			area := &clips.Items[clipIndex].Areas[areaIndex]
			area.DrawParam = remap.drawParam(area.DrawParam)
			if area.Path != nil {
				remapColor(area.Path.FillColor, remap)
				remapColor(area.Path.StrokeColor, remap)
			}
			if area.Text != nil {
				area.Text.Font = remap.font(area.Text.Font)
				remapColor(area.Text.FillColor, remap)
				remapColor(area.Text.StrokeColor, remap)
			}
		}
	}
}

func remapDrawParam(param *creator.DrawParam, remap *resourceRemap) {
	param.Relative = remap.drawParam(param.Relative)
	remapColor(param.FillColor, remap)
	remapColor(param.StrokeColor, remap)
}

func remapColor(color *creator.Color, remap *resourceRemap) {
	if color == nil {
		return
	}
	color.ColorSpace = remap.id(color.ColorSpace)
	if color.Axial != nil {
		remapColorStops(color.Axial.Segments, remap)
	}
	if color.Radial != nil {
		remapColorStops(color.Radial.Segments, remap)
	}
	if color.Gouraud != nil {
		for index := range color.Gouraud.Points {
			remapColor(&color.Gouraud.Points[index].Color, remap)
		}
		remapColor(color.Gouraud.BackColor, remap)
	}
	if color.LaGouraud != nil {
		for index := range color.LaGouraud.Points {
			remapColor(&color.LaGouraud.Points[index].Color, remap)
		}
		remapColor(color.LaGouraud.BackColor, remap)
	}
	if color.Pattern != nil {
		color.Pattern.Thumbnail = remap.id(color.Pattern.Thumbnail)
		remapItems(color.Pattern.Items, remap)
		remapLayers(color.Pattern.Layers, remap)
	}
}

func remapColorStops(stops []creator.ColorStop, remap *resourceRemap) {
	for index := range stops {
		remapColor(&stops[index].Color, remap)
	}
}

// pageSourceInput 校验公开 Source 并返回 parser 可接受的输入。
func pageSourceInput(index int, src Source) (string, any, int64, error) {
	name := strings.TrimSpace(src.Name)
	if name == "" {
		if strings.TrimSpace(src.Path) != "" {
			name = src.Path
		} else {
			name = fmt.Sprintf("第 %d 个输入", index)
		}
	}
	configured := 0
	if strings.TrimSpace(src.Path) != "" {
		configured++
	}
	if len(src.Data) > 0 {
		configured++
	}
	if src.Reader != nil {
		configured++
	}
	if configured != 1 {
		return "", nil, 0, fmt.Errorf("输入 %s 必须且只能设置 Path、Data 或 Reader 之一", name)
	}
	switch {
	case strings.TrimSpace(src.Path) != "":
		info, err := os.Stat(src.Path)
		if err != nil {
			return "", nil, 0, fmt.Errorf("读取输入 %s 失败: %w", name, err)
		}
		return name, src.Path, info.Size(), nil
	case len(src.Data) > 0:
		return name, src.Data, int64(len(src.Data)), nil
	default:
		if src.Size <= 0 {
			return "", nil, 0, fmt.Errorf("输入 %s 的 Reader 必须同时设置正数 Size", name)
		}
		data, err := io.ReadAll(io.NewSectionReader(src.Reader, 0, src.Size))
		if err != nil {
			return "", nil, 0, fmt.Errorf("读取输入 %s 失败: %w", name, err)
		}
		return name, data, int64(len(data)), nil
	}
}
