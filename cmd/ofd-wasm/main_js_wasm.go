//go:build js && wasm

// 命令 ofd-wasm 将 OFD 文档引擎暴露给浏览器 JavaScript。
package main

import (
	"errors"
	"fmt"
	"github.com/klauspost/compress/zip"
	"image/color"
	"math"
	"runtime"
	"runtime/debug"
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
	streamsMu     sync.Mutex
	streams       map[uint64]*jsChunkWriter
}

const (
	// wasmMaxRenderPages 限制 renderPages 批量位图渲染的页数，避免主线程同时持有过多图像。
	wasmMaxRenderPages = 64
	// wasmMaxStreamPages 仅用于防止异常调用分配过大的索引数组；流式导出本身不限页数。
	wasmMaxStreamPages = 1 << 16
)

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
	api.Set("renderStream", js.FuncOf(app.renderStream))
	api.Set("streamAck", js.FuncOf(app.streamAck))
	api.Set("cancelStream", js.FuncOf(app.cancelStream))
	api.Set("memStats", js.FuncOf(app.memStats))
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
	if err := a.closeCurrentReader(); err != nil {
		return errorValue(err)
	}
	reader, err := webreader.OpenWithOptions(data, options)
	if err != nil {
		return errorValue(err)
	}
	a.mu.Lock()
	fallbackFonts := append([]webreader.FontSource(nil), a.fallbackFonts...)
	a.mu.Unlock()
	for _, source := range fallbackFonts {
		if err := reader.UseFallbackFont(source.Family); err != nil {
			_ = reader.Close()
			return errorValue(err)
		}
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
	a.reader = reader
	a.mu.Unlock()
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
	value := args[0]
	options := webreader.OpenOptions{}
	if capacity := value.Get("pageCacheCapacity"); !capacity.IsUndefined() && !capacity.IsNull() {
		if capacity.Type() != js.TypeNumber || capacity.IsNaN() || math.IsInf(capacity.Float(), 0) ||
			capacity.Float() != math.Trunc(capacity.Float()) ||
			capacity.Float() < 0 || capacity.Float() > 1<<20 {
			return webreader.OpenOptions{}, errors.New("pageCacheCapacity 必须是 0 到 1048576 之间的整数")
		}
		options.PageCacheCapacity = capacity.Int()
	}
	if bytes := value.Get("pageCacheBytes"); !bytes.IsUndefined() && !bytes.IsNull() {
		if bytes.Type() != js.TypeNumber || bytes.IsNaN() || math.IsInf(bytes.Float(), 0) ||
			bytes.Float() != math.Trunc(bytes.Float()) ||
			bytes.Float() < 0 || bytes.Float() > 1<<40 {
			return webreader.OpenOptions{}, errors.New("pageCacheBytes 必须是 0 到 1099511627776 之间的整数")
		}
		options.PageCacheBytes = int64(bytes.Float())
	}
	return options, nil
}

func (a *wasmApp) addFallbackFont(_ js.Value, args []js.Value) any {
	if len(args) < 3 || len(args) > 4 {
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
	if alreadyRegistered {
		return nil
	}
	// 以 app 持有的唯一副本参与全局注册，注册表只引用同一份字节，不重复持有。
	a.mu.Lock()
	if containsFallbackFont(a.fallbackFonts, source) {
		a.mu.Unlock()
		return nil
	}
	a.fallbackFonts = append(a.fallbackFonts, cloneFontSource(source))
	stored := a.fallbackFonts[len(a.fallbackFonts)-1]
	a.mu.Unlock()
	if err := webreader.RegisterFallbackFont(stored); err != nil {
		// 失败留下可重试状态：不把失败字体保留为已注册。
		a.mu.Lock()
		a.fallbackFonts = a.fallbackFonts[:len(a.fallbackFonts)-1]
		a.mu.Unlock()
		return errorValue(err)
	}
	if reader != nil {
		if err := reader.UseFallbackFont(stored.Family); err != nil {
			return errorValue(err)
		}
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
	if err := a.closeCurrentReader(); err != nil {
		return errorValue(err)
	}
	return nil
}

func (a *wasmApp) closeCurrentReader() error {
	a.streamsMu.Lock()
	streams := a.streams
	a.streams = nil
	a.streamsMu.Unlock()
	for _, writer := range streams {
		writer.cancel()
	}

	a.mu.Lock()
	reader := a.reader
	a.reader = nil
	a.mu.Unlock()
	if reader == nil {
		runtime.GC()
		return nil
	}
	err := reader.Close()
	reader = nil
	runtime.GC()
	debug.FreeOSMemory()
	return err
}

// memStats 返回 Go 运行时内存统计，用于诊断阅读器内存占用。
func (a *wasmApp) memStats(_ js.Value, _ []js.Value) any {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return objectValue(map[string]any{
		"heapAlloc":    stats.HeapAlloc,
		"heapInuse":    stats.HeapInuse,
		"heapSys":      stats.HeapSys,
		"heapReleased": stats.HeapReleased,
		"heapObjects":  stats.HeapObjects,
		"stackSys":     stats.StackSys,
		"totalAlloc":   stats.TotalAlloc,
		"sys":          stats.Sys,
		"numGC":        stats.NumGC,
	})
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
	indices, err := jsIndices(args[0], wasmMaxRenderPages)
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

func (a *wasmApp) renderStream(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 || len(args) > 4 {
		return errorValue(errors.New("ofd.renderStream 需要页面索引数组、可选配置和输出回调"))
	}
	indices, err := jsIndices(args[0], wasmMaxStreamPages)
	if err != nil {
		return errorValue(err)
	}
	options, err := renderStreamOptions(args[1:2])
	if err != nil {
		return errorValue(err)
	}
	if options.Format != webreader.RenderFormat("pdf") && len(indices) == 1 {
		if len(args) > 2 {
			return errorValue(errors.New("ofd.renderStream 单页时不需要输出回调"))
		}
		data, renderErr := reader.RenderPage(indices[0], options)
		if renderErr != nil {
			return errorValue(renderErr)
		}
		result := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(result, data)
		return result
	}
	if options.Format != webreader.RenderFormat("pdf") && len(indices) < 2 {
		return errorValue(errors.New("ofd.renderStream 页面列表为空"))
	}
	if options.Format != webreader.RenderFormat("pdf") && options.Format != webreader.RenderPNG && options.Format != webreader.RenderJPG {
		return errorValue(errors.New("ofd.renderStream 多页时只支持 PDF、PNG 或 JPG"))
	}
	if len(args) < 3 || args[2].Type() != js.TypeFunction {
		return errorValue(errors.New("ofd.renderStream 需要输出回调"))
	}
	var streamID uint64
	if len(args) == 4 {
		if args[3].Type() != js.TypeNumber || args[3].Int() <= 0 {
			return errorValue(errors.New("ofd.renderStream 的流 ID 无效"))
		}
		streamID = uint64(args[3].Int())
	}
	writer := newJSChunkWriter(args[2], streamID)
	if streamID != 0 {
		a.streamsMu.Lock()
		if a.streams == nil {
			a.streams = make(map[uint64]*jsChunkWriter)
		}
		a.streams[streamID] = writer
		a.streamsMu.Unlock()
	}

	promiseExecutor := js.FuncOf(func(_ js.Value, promiseArgs []js.Value) any {
		resolve, reject := promiseArgs[0], promiseArgs[1]
		go func() {
			var err error
			defer func() {
				if recovered := recover(); recovered != nil {
					err = fmt.Errorf("输出流生成失败: %v", recovered)
				}
				if streamID != 0 {
					a.streamsMu.Lock()
					delete(a.streams, streamID)
					a.streamsMu.Unlock()
				}
				if err != nil {
					_ = invokeJSCallback(reject, js.Global().Get("Error").New(err.Error()))
					return
				}
				_ = invokeJSCallback(resolve, js.Null())
			}()
			if options.Format == webreader.RenderFormat("pdf") {
				err = reader.RenderPDFTo(writer, indices, options)
			} else {
				archive := zip.NewWriter(writer)
				for position, index := range indices {
					data, renderErr := reader.RenderPage(index, options)
					if renderErr != nil {
						err = fmt.Errorf("处理图片第 %d 页失败: %w", position+1, renderErr)
						return
					}
					header := &zip.FileHeader{
						Name:   fmt.Sprintf("page-%04d.%s", index+1, options.Format),
						Method: zip.Store,
					}
					entry, createErr := archive.CreateHeader(header)
					if createErr != nil {
						err = fmt.Errorf("创建图片 ZIP 条目失败: %w", createErr)
						return
					}
					if _, writeErr := entry.Write(data); writeErr != nil {
						err = fmt.Errorf("写入图片 ZIP 条目失败: %w", writeErr)
						return
					}
				}
				err = archive.Close()
			}
			if err == nil {
				err = writer.flush()
			}
		}()
		return nil
	})
	promise := js.Global().Get("Promise").New(promiseExecutor)
	promiseExecutor.Release()
	return promise
}

const wasmOutputChunkSize = 64 << 10

type jsChunkWriter struct {
	callback   js.Value
	streamID   uint64
	buffer     []byte
	ackMu      sync.Mutex
	acks       map[uint64]chan error
	cancelCh   chan struct{}
	cancelOnce sync.Once
	sequence   uint64
}

func newJSChunkWriter(callback js.Value, streamID uint64) *jsChunkWriter {
	return &jsChunkWriter{
		callback: callback,
		streamID: streamID,
		acks:     make(map[uint64]chan error),
		cancelCh: make(chan struct{}),
	}
}

func (w *jsChunkWriter) Write(data []byte) (int, error) {
	select {
	case <-w.cancelCh:
		return 0, errors.New("输出流已取消")
	default:
	}
	written := len(data)
	for len(data) > 0 {
		space := wasmOutputChunkSize - len(w.buffer)
		count := min(len(data), space)
		w.buffer = append(w.buffer, data[:count]...)
		data = data[count:]
		if len(w.buffer) == wasmOutputChunkSize {
			if err := w.flush(); err != nil {
				return 0, err
			}
		}
	}
	return written, nil
}

func (w *jsChunkWriter) flush() error {
	if len(w.buffer) == 0 {
		return nil
	}
	data := js.Global().Get("Uint8Array").New(len(w.buffer))
	js.CopyBytesToJS(data, w.buffer)
	w.sequence++
	sequence := w.sequence
	var ack chan error
	if w.streamID != 0 {
		ack = make(chan error, 1)
		w.ackMu.Lock()
		w.acks[sequence] = ack
		w.ackMu.Unlock()
	}
	if err := invokeJSCallback(w.callback, data, js.ValueOf(sequence)); err != nil {
		w.removeAck(sequence)
		return err
	}
	if ack != nil {
		select {
		case err := <-ack:
			w.removeAck(sequence)
			if err != nil {
				return err
			}
		case <-w.cancelCh:
			w.removeAck(sequence)
			return errors.New("输出流已取消")
		}
	}
	w.buffer = w.buffer[:0]
	return nil
}

func (w *jsChunkWriter) removeAck(sequence uint64) {
	w.ackMu.Lock()
	delete(w.acks, sequence)
	w.ackMu.Unlock()
}

func (w *jsChunkWriter) ack(sequence uint64, err error) {
	w.ackMu.Lock()
	ack := w.acks[sequence]
	w.ackMu.Unlock()
	if ack != nil {
		select {
		case ack <- err:
		default:
		}
	}
}

func (w *jsChunkWriter) cancel() {
	w.cancelOnce.Do(func() { close(w.cancelCh) })
}

func invokeJSCallback(callback js.Value, args ...js.Value) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("输出回调失败: %v", recovered)
		}
	}()
	values := make([]any, len(args))
	for index, arg := range args {
		values[index] = arg
	}
	callback.Invoke(values...)
	return nil
}

func (a *wasmApp) streamAck(_ js.Value, args []js.Value) any {
	if len(args) < 2 || len(args) > 3 || args[0].Type() != js.TypeNumber || args[1].Type() != js.TypeNumber {
		return errorValue(errors.New("ofd.streamAck 参数无效"))
	}
	streamID, sequence := uint64(args[0].Int()), uint64(args[1].Int())
	if streamID == 0 || sequence == 0 {
		return errorValue(errors.New("ofd.streamAck 的流 ID 或序号无效"))
	}
	var err error
	if len(args) == 3 && !args[2].IsNull() && !args[2].IsUndefined() {
		err = errors.New(args[2].String())
	}
	a.streamsMu.Lock()
	writer := a.streams[streamID]
	a.streamsMu.Unlock()
	if writer == nil {
		return nil
	}
	writer.ack(sequence, err)
	return nil
}

func (a *wasmApp) cancelStream(_ js.Value, args []js.Value) any {
	if len(args) != 1 || args[0].Type() != js.TypeNumber || args[0].Int() <= 0 {
		return errorValue(errors.New("ofd.cancelStream 参数无效"))
	}
	a.streamsMu.Lock()
	writer := a.streams[uint64(args[0].Int())]
	a.streamsMu.Unlock()
	if writer != nil {
		writer.cancel()
	}
	return nil
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
	return parseRenderOptions(args, false)
}

func renderStreamOptions(args []js.Value) (webreader.RenderOptions, error) {
	return parseRenderOptions(args, true)
}

func parseRenderOptions(args []js.Value, allowPDF bool) (webreader.RenderOptions, error) {
	options := webreader.RenderOptions{DPI: 72, Background: color.Transparent, Format: webreader.RenderPNG}
	if len(args) == 0 || args[0].IsUndefined() || args[0].IsNull() {
		return options, nil
	}
	if args[0].Type() != js.TypeObject {
		return options, errors.New("renderPage 配置必须是对象")
	}
	value := args[0]
	if format := value.Get("format"); !format.IsUndefined() && !format.IsNull() {
		if format.Type() != js.TypeString {
			return options, errors.New("format 必须是 pdf、png、jpg 或 svg")
		}
		switch strings.ToLower(strings.TrimSpace(format.String())) {
		case "pdf":
			if !allowPDF {
				return options, errors.New("format 必须是 png、jpg 或 svg")
			}
			options.Format = webreader.RenderFormat("pdf")
		case string(webreader.RenderPNG):
			options.Format = webreader.RenderPNG
		case string(webreader.RenderSVG):
			options.Format = webreader.RenderSVG
		case string(webreader.RenderJPG):
			options.Format = webreader.RenderJPG
		default:
			return options, errors.New("format 必须是 pdf、png、jpg 或 svg")
		}
	}
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

func jsIndices(value js.Value, limit int) ([]int, error) {
	if value.Type() != js.TypeObject || value.Get("length").Type() != js.TypeNumber {
		return nil, errors.New("页面索引必须是数组")
	}
	length := value.Get("length").Int()
	if length < 0 || length > limit {
		return nil, fmt.Errorf("批量渲染页面数量超过限制 %d", limit)
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
