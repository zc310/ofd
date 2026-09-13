//go:build js && wasm

// 命令 ofd-wasm 将 OFD 文档引擎暴露给浏览器 JavaScript。
package main

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"
	"syscall/js"

	"github.com/zc310/ofd/pkg/webreader"
)

type wasmApp struct {
	mu            sync.Mutex
	reader        *webreader.Reader
	fallbackFonts []webreader.FontSource
}

const wasmMaxRenderPages = 64

func main() {
	console := js.Global().Get("console")
	console.Call("log", "%c╔══════════════════════════════════════════╗", "color: #2476bd; font-weight: bold;")
	console.Call("log", "%c║       OFD WASM 阅读器 v0.1.1              ║", "color: #2476bd; font-weight: bold;")
	console.Call("log", "%c╚══════════════════════════════════════════╝", "color: #2476bd; font-weight: bold;")
	console.Call("log", "%c基于 Go + WebAssembly 构建", "color: #667188;")
	console.Call("log", "%cGitHub: https://github.com/zc310/ofd", "color: #2f7d4a;")
	app := &wasmApp{}
	api := js.Global().Get("Object").New()
	api.Set("open", js.FuncOf(app.open))
	api.Set("addFallbackFont", js.FuncOf(app.addFallbackFont))
	api.Set("close", js.FuncOf(app.close))
	api.Set("info", js.FuncOf(app.info))
	api.Set("pageCount", js.FuncOf(app.pageCount))
	api.Set("pages", js.FuncOf(app.pages))
	api.Set("pageInfo", js.FuncOf(app.pageInfo))
	api.Set("text", js.FuncOf(app.text))
	api.Set("search", js.FuncOf(app.search))
	api.Set("renderPage", js.FuncOf(app.renderPage))
	api.Set("renderPages", js.FuncOf(app.renderPages))
	api.Set("renderPDF", js.FuncOf(app.renderPDF))
	js.Global().Set("ofd", api)

	select {}
}

func (a *wasmApp) open(_ js.Value, args []js.Value) (result any) {
	if len(args) < 1 || len(args) > 2 {
		return errorValue(errors.New("ofd.open 需要数据和可选配置参数"))
	}
	data, err := bytesFromJS(args[0])
	if err != nil {
		return errorValue(err)
	}
	options, err := openOptions(args[1:])
	if err != nil {
		return errorValue(err)
	}
	a.mu.Lock()
	for _, source := range a.fallbackFonts {
		if containsFallbackFont(options.FallbackFonts, source) {
			continue
		}
		options.FallbackFonts = append(options.FallbackFonts, cloneFontSource(source))
	}
	a.mu.Unlock()
	reader, err := webreader.OpenWithOptions(data, options)
	if err != nil {
		return errorValue(err)
	}
	pages, err := reader.Pages()
	if err != nil {
		_ = reader.Close()
		return errorValue(err)
	}
	fonts, err := reader.Fonts()
	if err != nil {
		_ = reader.Close()
		return errorValue(err)
	}

	a.mu.Lock()
	old := a.reader
	a.reader = reader
	a.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return objectValue(map[string]any{
		"pageCount": len(pages),
		"pages":     pagesValue(pages),
		"fonts":     fontsValue(fonts),
	})
}

func openOptions(args []js.Value) (webreader.OpenOptions, error) {
	if len(args) == 0 || args[0].IsUndefined() || args[0].IsNull() {
		return webreader.OpenOptions{}, nil
	}
	if args[0].Type() != js.TypeObject {
		return webreader.OpenOptions{}, errors.New("ofd.open 配置必须是对象")
	}
	value := args[0].Get("fallbackFonts")
	if value.IsUndefined() || value.IsNull() {
		return webreader.OpenOptions{}, nil
	}
	if value.Type() != js.TypeObject || value.Get("length").Type() != js.TypeNumber {
		return webreader.OpenOptions{}, errors.New("fallbackFonts 必须是数组")
	}
	length := value.Get("length").Int()
	if length < 0 || length > 16 {
		return webreader.OpenOptions{}, errors.New("fallbackFonts 数量无效")
	}
	options := webreader.OpenOptions{FallbackFonts: make([]webreader.FontSource, 0, length)}
	for index := 0; index < length; index++ {
		item := value.Index(index)
		if item.IsNull() || item.IsUndefined() || item.Type() != js.TypeObject {
			return webreader.OpenOptions{}, fmt.Errorf("fallbackFonts[%d] 必须是对象", index)
		}
		family := item.Get("family")
		if family.Type() != js.TypeString || strings.TrimSpace(family.String()) == "" {
			return webreader.OpenOptions{}, fmt.Errorf("fallbackFonts[%d].family 必须是非空字符串", index)
		}
		fontData, err := bytesFromJS(item.Get("data"))
		if err != nil {
			return webreader.OpenOptions{}, fmt.Errorf("fallbackFonts[%d].data: %w", index, err)
		}
		source := webreader.FontSource{Family: family.String(), Data: fontData}
		if weight := item.Get("weight"); !weight.IsUndefined() {
			if weight.Type() != js.TypeNumber || weight.IsNaN() || math.IsInf(weight.Float(), 0) {
				return webreader.OpenOptions{}, fmt.Errorf("fallbackFonts[%d].weight 必须是有限数字", index)
			}
			source.Weight = weight.Int()
		}
		if italic := item.Get("italic"); !italic.IsUndefined() {
			if italic.Type() != js.TypeBoolean {
				return webreader.OpenOptions{}, fmt.Errorf("fallbackFonts[%d].italic 必须是布尔值", index)
			}
			source.Italic = italic.Bool()
		}
		options.FallbackFonts = append(options.FallbackFonts, source)
	}
	return options, nil
}

func (a *wasmApp) addFallbackFont(_ js.Value, args []js.Value) any {
	if len(args) < 2 || len(args) > 4 {
		return errorValue(errors.New("ofd.addFallbackFont 需要字体数据、字体族名和可选样式"))
	}
	data, err := bytesFromJS(args[0])
	if err != nil {
		return errorValue(err)
	}
	if args[1].Type() != js.TypeString || strings.TrimSpace(args[1].String()) == "" {
		return errorValue(errors.New("字体族名必须是非空字符串"))
	}
	source := webreader.FontSource{Family: args[1].String(), Data: data}
	if len(args) >= 3 {
		if args[2].Type() != js.TypeNumber || args[2].IsNaN() || math.IsInf(args[2].Float(), 0) {
			return errorValue(errors.New("字体权重必须是有限数字"))
		}
		source.Weight = args[2].Int()
	}
	if len(args) == 4 {
		if args[3].Type() != js.TypeBoolean {
			return errorValue(errors.New("字体斜体标记必须是布尔值"))
		}
		source.Italic = args[3].Bool()
	}
	a.mu.Lock()
	alreadyRegistered := containsFallbackFont(a.fallbackFonts, source)
	reader := a.reader
	a.mu.Unlock()
	if !alreadyRegistered && reader != nil {
		if err := reader.AddFallbackFont(source); err != nil {
			return errorValue(err)
		}
	}
	if !alreadyRegistered {
		a.mu.Lock()
		if !containsFallbackFont(a.fallbackFonts, source) {
			a.fallbackFonts = append(a.fallbackFonts, cloneFontSource(source))
		}
		a.mu.Unlock()
	}
	return nil
}

func containsFallbackFont(fonts []webreader.FontSource, source webreader.FontSource) bool {
	for _, font := range fonts {
		if font.Family == source.Family && font.Weight == source.Weight && font.Italic == source.Italic {
			return true
		}
	}
	return false
}

func cloneFontSource(source webreader.FontSource) webreader.FontSource {
	return webreader.FontSource{
		Family: source.Family,
		Name:   source.Name,
		Weight: source.Weight,
		Italic: source.Italic,
		Data:   append([]byte(nil), source.Data...),
	}
}

func (a *wasmApp) close(_ js.Value, _ []js.Value) any {
	a.mu.Lock()
	reader := a.reader
	a.reader = nil
	a.mu.Unlock()
	if reader == nil {
		return nil
	}
	if err := reader.Close(); err != nil {
		return errorValue(err)
	}
	return nil
}

func (a *wasmApp) info(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	info, err := reader.Info()
	if err != nil {
		return errorValue(err)
	}
	return objectValue(map[string]any{
		"docID":        info.DocID,
		"title":        info.Title,
		"author":       info.Author,
		"subject":      info.Subject,
		"abstract":     info.Abstract,
		"creationDate": info.CreationDate,
		"modDate":      info.ModDate,
		"creator":      info.Creator,
		"version":      info.Version,
	})
}

func (a *wasmApp) pageCount(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	return reader.PageCount()
}

func (a *wasmApp) pages(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	pages, err := reader.Pages()
	if err != nil {
		return errorValue(err)
	}
	return pagesValue(pages)
}

func (a *wasmApp) pageInfo(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	index, err := requiredIndex(args, "ofd.pageInfo")
	if err != nil {
		return errorValue(err)
	}
	page, err := reader.Page(index)
	if err != nil {
		return errorValue(err)
	}
	return pageValue(page)
}

func (a *wasmApp) text(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	index, err := requiredIndex(args, "ofd.text")
	if err != nil {
		return errorValue(err)
	}
	runs, err := reader.Text(index)
	if err != nil {
		return errorValue(err)
	}
	return textRunsValue(runs)
}

func (a *wasmApp) search(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) != 1 || args[0].Type() != js.TypeString {
		return errorValue(errors.New("ofd.search 需要一个字符串参数"))
	}
	results, err := reader.Search(args[0].String())
	if err != nil {
		return errorValue(err)
	}
	return searchResultsValue(results)
}

func (a *wasmApp) renderPage(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 || len(args) > 2 {
		return errorValue(errors.New("ofd.renderPage 需要页面索引和可选配置"))
	}
	index, err := jsIndex(args[0])
	if err != nil {
		return errorValue(err)
	}
	options, err := renderOptions(args[1:])
	if err != nil {
		return errorValue(err)
	}
	data, err := reader.RenderPage(index, options)
	if err != nil {
		return errorValue(err)
	}
	result := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(result, data)
	return result
}

func (a *wasmApp) renderPages(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 || len(args) > 2 {
		return errorValue(errors.New("ofd.renderPages 需要页面索引数组和可选配置"))
	}
	indices, err := jsIndices(args[0])
	if err != nil {
		return errorValue(err)
	}
	options, err := renderOptions(args[1:])
	if err != nil {
		return errorValue(err)
	}
	data, err := reader.RenderPages(indices, options)
	if err != nil {
		return errorValue(err)
	}
	result := js.Global().Get("Array").New(len(data))
	for index, page := range data {
		image := js.Global().Get("Uint8Array").New(len(page))
		js.CopyBytesToJS(image, page)
		result.SetIndex(index, image)
	}
	return result
}

func (a *wasmApp) renderPDF(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 || len(args) > 2 {
		return errorValue(errors.New("ofd.renderPDF 需要页面索引数组和可选配置"))
	}
	indices, err := jsIndices(args[0])
	if err != nil {
		return errorValue(err)
	}
	options, err := renderOptions(args[1:])
	if err != nil {
		return errorValue(err)
	}
	data, err := reader.RenderPDF(indices, options)
	if err != nil {
		return errorValue(err)
	}
	result := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(result, data)
	return result
}

func (a *wasmApp) currentReader() (*webreader.Reader, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reader == nil {
		return nil, errors.New("尚未打开 OFD 文档")
	}
	return a.reader, nil
}

func bytesFromJS(value js.Value) ([]byte, error) {
	if value.IsUndefined() || value.IsNull() {
		return nil, errors.New("OFD 数据为空")
	}
	if value.Type() != js.TypeObject {
		return nil, errors.New("OFD 数据必须是 Uint8Array 或 ArrayBuffer")
	}
	if value.Get("byteLength").Type() != js.TypeNumber {
		return nil, errors.New("OFD 数据必须是 Uint8Array 或 ArrayBuffer")
	}
	// CopyBytesToGo 要求源值是 Uint8Array；统一包装也能正确处理
	// ArrayBuffer 以及带 byteOffset 的 Uint8Array。
	bytes := js.Global().Get("Uint8Array").New(value)
	length := bytes.Get("byteLength").Int()
	if length <= 0 {
		return nil, errors.New("OFD 数据为空")
	}
	result := make([]byte, length)
	if js.CopyBytesToGo(result, bytes) != length {
		return nil, errors.New("读取 OFD 数据失败")
	}
	return result, nil
}

func renderOptions(args []js.Value) (webreader.RenderOptions, error) {
	options := webreader.RenderOptions{DPI: 96, Background: color.Transparent}
	if len(args) == 0 || args[0].IsUndefined() || args[0].IsNull() {
		return options, nil
	}
	if args[0].Type() != js.TypeObject {
		return options, errors.New("renderPage 配置必须是对象")
	}
	value := args[0]
	if dpi := value.Get("dpi"); dpi.Type() == js.TypeNumber && !dpi.IsNaN() {
		options.DPI = dpi.Float()
	} else if !dpi.IsUndefined() {
		return options, errors.New("dpi 必须是数字")
	}
	if background := value.Get("background"); !background.IsUndefined() && !background.IsNull() {
		parsed, err := parseColor(background.String())
		if err != nil {
			return options, err
		}
		options.Background = parsed
	}
	return options, nil
}

func parseColor(value string) (color.Color, error) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
	if len(value) == 3 {
		value = string(value[0]) + string(value[0]) + string(value[1]) + string(value[1]) + string(value[2]) + string(value[2])
	}
	if len(value) != 6 && len(value) != 8 {
		return nil, fmt.Errorf("背景颜色必须是 #RRGGBB 或 #RRGGBBAA")
	}
	decode := func(start int) (uint8, error) {
		parsed, err := strconv.ParseUint(value[start:start+2], 16, 8)
		return uint8(parsed), err
	}
	r, err := decode(0)
	if err != nil {
		return nil, errors.New("背景颜色格式无效")
	}
	g, err := decode(2)
	if err != nil {
		return nil, errors.New("背景颜色格式无效")
	}
	b, err := decode(4)
	if err != nil {
		return nil, errors.New("背景颜色格式无效")
	}
	a := uint8(255)
	if len(value) == 8 {
		a, err = decode(6)
		if err != nil {
			return nil, errors.New("背景颜色格式无效")
		}
	}
	return color.RGBA{R: r, G: g, B: b, A: a}, nil
}

func requiredIndex(args []js.Value, name string) (int, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%s 需要一个页面索引", name)
	}
	return jsIndex(args[0])
}

func jsIndex(value js.Value) (int, error) {
	if value.Type() != js.TypeNumber || value.IsNaN() || value.Int() < 0 || value.Float() != float64(value.Int()) {
		return 0, errors.New("页面索引必须是非负整数")
	}
	return value.Int(), nil
}

func jsIndices(value js.Value) ([]int, error) {
	if value.Type() != js.TypeObject || value.Get("length").Type() != js.TypeNumber {
		return nil, errors.New("页面索引必须是数组")
	}
	length := value.Get("length").Int()
	if length < 0 || length > wasmMaxRenderPages {
		return nil, fmt.Errorf("批量渲染页面数量超过限制 %d", wasmMaxRenderPages)
	}
	indices := make([]int, length)
	for index := range indices {
		parsed, err := jsIndex(value.Index(index))
		if err != nil {
			return nil, fmt.Errorf("页面索引 %d: %w", index, err)
		}
		indices[index] = parsed
	}
	return indices, nil
}

func pagesValue(pages []webreader.PageInfo) js.Value {
	result := js.Global().Get("Array").New(len(pages))
	for index, page := range pages {
		result.SetIndex(index, pageValue(page))
	}
	return result
}

func pageValue(page webreader.PageInfo) js.Value {
	return objectValue(map[string]any{
		"index":  page.Index,
		"width":  page.Width,
		"height": page.Height,
	})
}

func textRunsValue(runs []webreader.TextRun) js.Value {
	result := js.Global().Get("Array").New(len(runs))
	for index, run := range runs {
		result.SetIndex(index, objectValue(map[string]any{
			"text":          run.Text,
			"x":             run.X,
			"y":             run.Y,
			"width":         run.Width,
			"height":        run.Height,
			"font":          run.Font,
			"size":          run.Size,
			"weight":        run.Weight,
			"readDirection": run.ReadDirection,
			"charDirection": run.CharDirection,
			"fontFamily":    run.FontFamily,
			"bold":          run.Bold,
			"italic":        run.Italic,
			"glyphs":        glyphsValue(run.Glyphs),
		}))
	}
	return result
}

func searchResultsValue(results []webreader.SearchResult) js.Value {
	result := js.Global().Get("Array").New(len(results))
	for index, match := range results {
		result.SetIndex(index, objectValue(map[string]any{
			"page":  match.Page,
			"run":   match.Run,
			"text":  match.Text,
			"start": match.Start,
			"end":   match.End,
			"rects": rectsValue(match.Rects),
		}))
	}
	return result
}

func glyphsValue(glyphs []webreader.Glyph) js.Value {
	result := js.Global().Get("Array").New(len(glyphs))
	for index, glyph := range glyphs {
		result.SetIndex(index, objectValue(map[string]any{
			"text": glyph.Text, "x": glyph.X, "y": glyph.Y,
			"width": glyph.Width, "height": glyph.Height, "angle": glyph.Angle,
		}))
	}
	return result
}

func rectsValue(rects []webreader.Rect) js.Value {
	result := js.Global().Get("Array").New(len(rects))
	for index, rect := range rects {
		result.SetIndex(index, objectValue(map[string]any{
			"x": rect.X, "y": rect.Y, "width": rect.Width,
			"height": rect.Height, "angle": rect.Angle,
		}))
	}
	return result
}

func fontsValue(fonts []webreader.FontResource) js.Value {
	result := js.Global().Get("Array").New(len(fonts))
	for index, font := range fonts {
		data := js.Global().Get("Uint8Array").New(len(font.Data))
		js.CopyBytesToJS(data, font.Data)
		result.SetIndex(index, objectValue(map[string]any{
			"id": font.ID, "family": font.Family, "name": font.Name,
			"bold": font.Bold, "italic": font.Italic, "format": font.Format, "data": data,
		}))
	}
	return result
}

func objectValue(values map[string]any) js.Value {
	result := js.Global().Get("Object").New()
	for key, value := range values {
		result.Set(key, js.ValueOf(value))
	}
	return result
}

func errorValue(err error) js.Value {
	result := js.Global().Get("Object").New()
	result.Set("error", err.Error())
	return result
}
