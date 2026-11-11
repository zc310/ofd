//go:build js && wasm

package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"math"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall/js"

	json "github.com/goccy/go-json/v2"
	"github.com/klauspost/compress/zip"

	readercontract "github.com/zc310/ofd/internal/reader/contract"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/internal/version"
	ofdreader "github.com/zc310/ofd/pkg/reader"
)

type wasmApp struct {
	mu            sync.Mutex
	reader        *ofdreader.Reader
	encrypted     bool // 原文件是否加密；仅在 Reader 成功打开后设置。
	fallbackFonts []ofdreader.FontSource
	streamsMu     sync.Mutex
	streams       map[uint64]*jsChunkWriter
	requestsMu    sync.Mutex
	requests      map[*wasmRenderRequest]struct{}
	renderBuffers sync.Pool
}

const (
	// wasmMaxRenderPages 限制 renderPages 批量位图渲染的页数，避免主线程同时持有过多图像。
	wasmMaxRenderPages = 64
	// wasmMaxStreamPages 仅用于防止异常调用分配过大的索引数组；流式导出本身不限页数。
	wasmMaxStreamPages = 1 << 16
	// 搜索在 Worker 内同步执行，必须限制单次扫描量和返回量，避免大文档长期占住 Worker。
	wasmMaxSearchPages   = 2000
	wasmMaxSearchResults = 5000
)

func main() {
	console := js.Global().Get("console")
	title := "color: #2476bd; font-weight: bold;"
	console.Call("log", "%c╔%s╗", title, strings.Repeat("═", wasmBannerWidth))
	console.Call("log", "%c║%s║", title, centerBanner(fmt.Sprintf("OFD WASM 阅读器 v%s", version.Version)))
	console.Call("log", "%c╚%s╝", title, strings.Repeat("═", wasmBannerWidth))
	console.Call("log", "%c基于 Go + WebAssembly 构建 · 全部处理在本机完成，文件不上传", "color: #667188;")
	console.Call("log", "%cGitHub: https://github.com/zc310/ofd", "color: #2f7d4a;")
	app := &wasmApp{}
	api := js.Global().Get("Object").New()
	api.Set("version", version.Version)
	api.Set("open", js.FuncOf(app.open))
	api.Set("addFallbackFont", js.FuncOf(app.addFallbackFont))
	api.Set("removeFallbackFont", js.FuncOf(app.removeFallbackFont))
	api.Set("close", js.FuncOf(app.close))
	api.Set("info", js.FuncOf(app.info))
	api.Set("permissions", js.FuncOf(app.permissions))
	api.Set("checkPermission", js.FuncOf(app.checkPermission))
	api.Set("embeddedFonts", js.FuncOf(app.embeddedFonts))
	api.Set("outline", js.FuncOf(app.outline))
	api.Set("customTags", js.FuncOf(app.customTags))
	api.Set("preferences", js.FuncOf(app.preferences))
	api.Set("fontUsage", js.FuncOf(app.fontUsage))
	api.Set("fontUsageAll", js.FuncOf(app.fontUsageAll))
	api.Set("attachments", js.FuncOf(app.attachments))
	api.Set("versions", js.FuncOf(app.versions))
	api.Set("selectVersion", js.FuncOf(app.selectVersion))
	api.Set("attachmentData", js.FuncOf(app.attachmentData))
	api.Set("media", js.FuncOf(app.media))
	api.Set("mediaData", js.FuncOf(app.mediaData))
	api.Set("annotations", js.FuncOf(app.annotations))
	api.Set("pageLinks", js.FuncOf(app.pageLinks))
	api.Set("pageLinksForPage", js.FuncOf(app.pageLinksForPage))
	api.Set("pageMediaActions", js.FuncOf(app.pageMediaActions))
	api.Set("pageMediaActionsForPage", js.FuncOf(app.pageMediaActionsForPage))
	api.Set("pageEntryActionsForPage", js.FuncOf(app.pageEntryActionsForPage))
	api.Set("signatures", js.FuncOf(app.signatures))
	api.Set("signatureSeal", js.FuncOf(app.signatureSeal))
	api.Set("signatureSealPreview", js.FuncOf(app.signatureSealPreview))
	api.Set("signatureCertificate", js.FuncOf(app.signatureCertificate))
	api.Set("signatureValue", js.FuncOf(app.signatureValue))
	api.Set("stats", js.FuncOf(app.stats))
	api.Set("pageCount", js.FuncOf(app.pageCount))
	api.Set("pages", js.FuncOf(app.pages))
	api.Set("pageInfo", js.FuncOf(app.pageInfo))
	api.Set("layers", js.FuncOf(app.layers))
	api.Set("setHiddenLayers", js.FuncOf(app.setHiddenLayers))
	api.Set("text", js.FuncOf(app.text))
	api.Set("search", js.FuncOf(app.search))
	api.Set("renderPage", js.FuncOf(app.renderPage))
	api.Set("renderPages", js.FuncOf(app.renderPages))
	api.Set("renderStream", js.FuncOf(app.renderStream))
	api.Set("streamAck", js.FuncOf(app.streamAck))
	api.Set("cancelRender", js.FuncOf(app.cancelRender))
	api.Set("memStats", js.FuncOf(app.memStats))
	api.Set("gc", js.FuncOf(app.gc))
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
	credentials, err := decryptOptions(args[1:])
	if err != nil {
		return errorValue(err)
	}
	defer clear(credentials.Password)
	if err := a.closeCurrentReader(); err != nil {
		return errorValue(err)
	}
	reader, encrypted, err := openWASMDocumentSecurity(context.Background(), data, options, credentials)
	if err != nil {
		return errorValue(err)
	}
	a.mu.Lock()
	fallbackFonts := append([]ofdreader.FontSource(nil), a.fallbackFonts...)
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
	permissions, err := reader.Permissions()
	if err != nil {
		_ = reader.Close()
		return errorValue(err)
	}
	permissionsJS, err := permissionsValue(permissions)
	if err != nil {
		_ = reader.Close()
		return errorValue(err)
	}
	pagesJS, err := jsonValue(pages)
	if err != nil {
		_ = reader.Close()
		return errorValue(err)
	}
	a.mu.Lock()
	a.reader = reader
	a.encrypted = encrypted
	a.mu.Unlock()
	return objectValue(map[string]any{
		"pageCount":   len(pages),
		"pages":       pagesJS,
		"fonts":       fontsValue(nil),
		"permissions": permissionsJS,
	})
}

func openOptions(args []js.Value) (ofdreader.OpenOptions, error) {
	if len(args) == 0 || args[0].IsUndefined() || args[0].IsNull() {
		return ofdreader.OpenOptions{}, nil
	}
	if args[0].Type() != js.TypeObject {
		return ofdreader.OpenOptions{}, errors.New("ofd.open 配置必须是对象")
	}
	value := args[0]
	options := ofdreader.OpenOptions{}
	if capacity := value.Get("pageCacheCapacity"); !capacity.IsUndefined() && !capacity.IsNull() {
		if capacity.Type() != js.TypeNumber || capacity.IsNaN() || math.IsInf(capacity.Float(), 0) ||
			capacity.Float() != math.Trunc(capacity.Float()) ||
			capacity.Float() < 0 || capacity.Float() > 1<<20 {
			return ofdreader.OpenOptions{}, errors.New("pageCacheCapacity 必须是 0 到 1048576 之间的整数")
		}
		options.PageCacheCapacity = capacity.Int()
	}
	if bytes := value.Get("pageCacheBytes"); !bytes.IsUndefined() && !bytes.IsNull() {
		if bytes.Type() != js.TypeNumber || bytes.IsNaN() || math.IsInf(bytes.Float(), 0) ||
			bytes.Float() != math.Trunc(bytes.Float()) ||
			bytes.Float() < 0 || bytes.Float() > 1<<40 {
			return ofdreader.OpenOptions{}, errors.New("pageCacheBytes 必须是 0 到 1099511627776 之间的整数")
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
	source := ofdreader.FontSource{Family: args[1].String(), Data: data}
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
	if err := ofdreader.RegisterFallbackFont(stored); err != nil {
		// 失败留下可重试状态：不把失败字体保留为已注册。
		a.mu.Lock()
		a.fallbackFonts[len(a.fallbackFonts)-1] = ofdreader.FontSource{}
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

func containsFallbackFont(fonts []ofdreader.FontSource, source ofdreader.FontSource) bool {
	for _, font := range fonts {
		if font.Family == source.Family && font.Weight == source.Weight && font.Italic == source.Italic {
			return true
		}
	}
	return false
}

// removeFallbackFont 移除指定字体族：从本实例的回退字体列表移除（后续打开的文档
// 不再自动应用），并让当前 Reader 取消该回退字体，使已打开文档立即恢复内嵌/默认
// 字体。同步注销全局来源，最后一个字体持有者释放后归还注册额度。
func (a *wasmApp) removeFallbackFont(_ js.Value, args []js.Value) any {
	if len(args) != 1 || args[0].Type() != js.TypeString || strings.TrimSpace(args[0].String()) == "" {
		return errorValue(errors.New("ofd.removeFallbackFont 需要字体族名"))
	}
	family := args[0].String()
	a.mu.Lock()
	kept := a.fallbackFonts[:0]
	removed := false
	for _, source := range a.fallbackFonts {
		if source.Family != family {
			kept = append(kept, source)
		} else {
			removed = true
		}
	}
	clear(a.fallbackFonts[len(kept):])
	a.fallbackFonts = kept
	reader := a.reader
	a.mu.Unlock()
	if reader != nil {
		if err := reader.RemoveFallbackFont(family); err != nil {
			return errorValue(err)
		}
	}
	if removed {
		if err := ofdreader.UnregisterFallbackFont(family); err != nil {
			return errorValue(err)
		}
	}
	return nil
}

func cloneFontSource(source ofdreader.FontSource) ofdreader.FontSource {
	return ofdreader.FontSource{
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
	a.requestsMu.Lock()
	for request := range a.requests {
		request.cancel()
	}
	a.requests = nil
	a.requestsMu.Unlock()

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
	a.encrypted = false
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

// gc 主动触发 Go 运行时垃圾回收并尝试把空闲内存归还操作系统，返回回收后的
// 统计。只影响内存，不关闭当前文档，用于内存诊断面板的「强制回收」按钮。
// 注意 WASM 线性内存只会增长，FreeOSMemory 只能让空闲页可复用，不会缩小
// 浏览器实际保留的地址空间。
func (a *wasmApp) gc(_ js.Value, _ []js.Value) any {
	runtime.GC()
	debug.FreeOSMemory()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return objectValue(map[string]any{
		"heapAlloc":    stats.HeapAlloc,
		"heapInuse":    stats.HeapInuse,
		"heapReleased": stats.HeapReleased,
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
	fonts, err := reader.FontList()
	fontError := ""
	if err != nil {
		fontError = err.Error()
	}
	permissions, err := reader.Permissions()
	if err != nil {
		return errorValue(err)
	}
	permissionsJS, err := permissionsValue(permissions)
	if err != nil {
		return errorValue(err)
	}
	fontsJS, err := jsonValue(fonts)
	if err != nil {
		return errorValue(err)
	}
	infoJS, err := jsonValue(info)
	if err != nil {
		return errorValue(err)
	}
	a.mu.Lock()
	encrypted := a.encrypted
	a.mu.Unlock()
	state := "none"
	if encrypted {
		state = "unlocked"
	}
	infoJS.Set("fonts", fontsJS)
	infoJS.Set("fonts_error", fontError)
	infoJS.Set("security", objectValue(map[string]any{"encrypted": encrypted, "state": state}))
	infoJS.Set("permissions", permissionsJS)
	return infoJS
}

func (a *wasmApp) embeddedFonts(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	fonts, err := reader.Fonts()
	if err != nil {
		return errorValue(err)
	}
	return fontsValue(fonts)
}

func (a *wasmApp) outline(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	tree, err := reader.Outline()
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(tree)
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) customTags(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	tags, err := reader.CustomTags()
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(readercontract.CustomTags(tags))
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) preferences(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	preferences, err := reader.Preferences()
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(preferences)
	if err != nil {
		return errorValue(err)
	}
	return value
}

// jsonValue 使用阅读器类型上的 JSON tag 生成 JS 值。
func jsonValue(value any) (js.Value, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return js.Undefined(), err
	}
	return js.Global().Get("JSON").Call("parse", string(data)), nil
}

func (a *wasmApp) fontUsage(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 2 || args[0].Type() != js.TypeNumber || args[1].Type() != js.TypeNumber {
		return errorValue(errors.New("ofd.fontUsage 需要字体作用域和标识两个数字参数"))
	}
	ref := ofdreader.FontRef{Scope: int(args[0].Float()), ID: uint64(args[1].Float())}
	if len(args) > 4 && args[4].Type() == js.TypeNumber {
		ref.TemplateID = uint64(args[4].Float())
	}
	usage, err := reader.FontUsage(context.Background(), ref, fontUsageOptions(args[2:]))
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(usage)
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) fontUsageAll(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	report, err := reader.FontUsageAll(context.Background(), fontUsageOptions(args))
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(report)
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) versions(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	infos, err := reader.Versions()
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(readercontract.Versions(infos))
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) selectVersion(_ js.Value, args []js.Value) any {
	if len(args) != 2 || args[0].Type() != js.TypeNumber || args[0].IsNaN() ||
		math.IsInf(args[0].Float(), 0) || args[0].Float() != math.Trunc(args[0].Float()) ||
		args[0].Float() < 0 || args[0].Float() > 1<<20 || args[1].Type() != js.TypeString {
		return errorValue(errors.New("ofd.selectVersion 需要文档体索引和版本 ID（空字符串恢复主入口）"))
	}
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if err := reader.SelectVersion(args[0].Int(), args[1].String()); err != nil {
		return errorValue(err)
	}
	pages, err := reader.Pages()
	if err != nil {
		return errorValue(err)
	}
	permissions, err := reader.Permissions()
	if err != nil {
		return errorValue(err)
	}
	permissionsJS, err := permissionsValue(permissions)
	if err != nil {
		return errorValue(err)
	}
	pagesJS, err := jsonValue(pages)
	if err != nil {
		return errorValue(err)
	}
	return objectValue(map[string]any{"pageCount": len(pages), "pages": pagesJS, "fonts": fontsValue(nil), "permissions": permissionsJS})
}

func (a *wasmApp) attachments(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	infos, err := reader.Attachments()
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(infos)
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) attachmentData(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 2 || args[0].Type() != js.TypeNumber || args[1].Type() != js.TypeString {
		return errorValue(errors.New("ofd.attachmentData 需要作用域和附件 ID 参数"))
	}
	maxBytes := int64(0)
	if len(args) > 2 && args[2].Type() == js.TypeNumber {
		maxBytes = int64(args[2].Float())
	}
	data, err := reader.AttachmentData(int(args[0].Float()), args[1].String(), maxBytes)
	if err != nil {
		return errorValue(err)
	}
	result := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(result, data)
	return result
}

func (a *wasmApp) media(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	infos, err := reader.Media()
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(infos)
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) mediaData(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 2 || args[0].Type() != js.TypeNumber || args[1].Type() != js.TypeNumber {
		return errorValue(errors.New("ofd.mediaData 需要作用域和资源 ID 两个数字参数"))
	}
	maxBytes := int64(0)
	if len(args) > 2 && args[2].Type() == js.TypeNumber {
		maxBytes = int64(args[2].Float())
	}
	// 原始资源下载默认检查 Export；文档内预览/播放显式标记用途。
	preview := len(args) >= 4 && args[3].Type() == js.TypeString && args[3].String() == "preview"
	if len(args) > 5 || (len(args) >= 4 && !preview) || (len(args) == 5 && args[4].Type() != js.TypeNumber) {
		return errorValue(errors.New("媒体用途仅支持 preview；下载时省略"))
	}
	if !preview {
		if err := reader.CheckDocumentPermission(ofdreader.PermissionExport, int(args[0].Float())); err != nil {
			return errorValue(err)
		}
	}
	templateID := uint64(0)
	if len(args) == 5 {
		templateID = uint64(args[4].Float())
	}
	data, err := reader.TemplateMediaData(int(args[0].Float()), templateID, uint64(args[1].Float()), maxBytes)
	if err != nil {
		return errorValue(err)
	}
	result := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(result, data)
	return result
}

var errAnnotationPageArgument = errors.New("ofd.annotations 的可选页码必须是非负整数")

func (a *wasmApp) annotations(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	var infos []ofdreader.AnnotationInfo
	if len(args) > 1 || (len(args) == 1 && args[0].Type() != js.TypeUndefined && (args[0].Type() != js.TypeNumber || args[0].IsNaN() || math.IsInf(args[0].Float(), 0) || args[0].Float() < 0 || args[0].Float() > math.MaxInt32 || args[0].Float() != math.Trunc(args[0].Float()))) {
		return errorValue(errAnnotationPageArgument)
	}
	if len(args) == 1 && args[0].Type() == js.TypeNumber {
		infos, err = reader.AnnotationsForPage(args[0].Int())
	} else {
		infos, err = reader.Annotations()
	}
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(infos)
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) pageLinks(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	links, err := reader.PageLinks()
	if err != nil {
		return errorValue(err)
	}
	return pageLinksValue(links)
}

func (a *wasmApp) pageLinksForPage(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 {
		return errorValue(errors.New("ofd.pageLinksForPage 需要页面索引"))
	}
	index, err := jsIndex(args[0])
	if err != nil {
		return errorValue(err)
	}
	links, err := reader.PageLinksForPage(index)
	if err != nil {
		return errorValue(err)
	}
	return pageLinksValue(links)
}

func (a *wasmApp) pageMediaActionsForPage(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 {
		return errorValue(errors.New("ofd.pageMediaActionsForPage 需要页面索引"))
	}
	index, err := jsIndex(args[0])
	if err != nil {
		return errorValue(err)
	}
	actions, err := reader.PageMediaActionsForPage(index)
	if err != nil {
		return errorValue(err)
	}
	return pageLinksValue(actions)
}

func (a *wasmApp) pageEntryActionsForPage(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 {
		return errorValue(errors.New("ofd.pageEntryActionsForPage 需要页面索引"))
	}
	index, err := jsIndex(args[0])
	if err != nil {
		return errorValue(err)
	}
	actions, err := reader.PageEntryActionsForPage(index)
	if err != nil {
		return errorValue(err)
	}
	return pageLinksValue(actions)
}

func pageLinksValue(links []ofdreader.PageLink) any {
	value, err := jsonValue(readercontract.PageLinks(links))
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) pageMediaActions(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	links, err := reader.PageMediaActions()
	if err != nil {
		return errorValue(err)
	}
	return pageLinksValue(links)
}

func (a *wasmApp) signatures(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	infos, err := reader.Signatures()
	if err != nil {
		return errorValue(err)
	}
	return signaturesValue(infos)
}

// 签名字段仅在 readercontract 维护，浏览器和桌面使用同一 JSON 契约。
func signaturesValue(infos []ofdreader.SignatureInfo) js.Value {
	value, err := jsonValue(readercontract.Signatures(infos))
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) signatureSeal(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 3 || args[0].Type() != js.TypeNumber || args[1].Type() != js.TypeString || args[2].Type() != js.TypeNumber {
		return errorValue(errors.New("ofd.signatureSeal 需要作用域、签名 ID 和签章索引参数"))
	}
	data, _, err := reader.SignatureSeal(int(args[0].Float()), args[1].String(), int(args[2].Float()))
	if err != nil {
		return errorValue(err)
	}
	result := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(result, data)
	return result
}

func (a *wasmApp) signatureCertificate(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 3 || args[0].Type() != js.TypeNumber || args[1].Type() != js.TypeString || args[2].Type() != js.TypeString {
		return errorValue(errors.New("ofd.signatureCertificate 需要作用域、签名 ID 和证书层级参数"))
	}
	data, err := reader.SignatureCertificate(int(args[0].Float()), args[1].String(), args[2].String())
	if err != nil {
		return errorValue(err)
	}
	result := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(result, data)
	return result
}

func (a *wasmApp) signatureValue(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 2 || args[0].Type() != js.TypeNumber || args[1].Type() != js.TypeString {
		return errorValue(errors.New("ofd.signatureValue 需要作用域和签名 ID 参数"))
	}
	data, err := reader.SignatureValue(int(args[0].Float()), args[1].String())
	if err != nil {
		return errorValue(err)
	}
	result := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(result, data)
	return result
}

func (a *wasmApp) stats(_ js.Value, _ []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	stats, err := reader.Stats()
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(stats)
	if err != nil {
		return errorValue(err)
	}
	return value
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
	value, err := jsonValue(pages)
	if err != nil {
		return errorValue(err)
	}
	return value
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
	value, err := jsonValue(page)
	if err != nil {
		return errorValue(err)
	}
	return value
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
	value, err := jsonValue(readercontract.TextRuns(runs))
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) layers(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	index, err := requiredIndex(args, "ofd.layers")
	if err != nil {
		return errorValue(err)
	}
	layers, err := reader.Layers(index)
	if err != nil {
		return errorValue(err)
	}
	value, err := jsonValue(layers)
	if err != nil {
		return errorValue(err)
	}
	return value
}

func (a *wasmApp) setHiddenLayers(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) != 2 {
		return errorValue(errors.New("ofd.setHiddenLayers 需要页面索引和图层 ID 数组"))
	}
	index, err := requiredIndex(args[:1], "ofd.setHiddenLayers")
	if err != nil {
		return errorValue(err)
	}
	if !js.Global().Get("Array").Call("isArray", args[1]).Bool() {
		return errorValue(errors.New("图层 ID 必须为字符串数组"))
	}
	ids := make([]string, args[1].Length())
	for i := range ids {
		if args[1].Index(i).Type() != js.TypeString {
			return errorValue(errors.New("图层 ID 必须为字符串"))
		}
		ids[i] = args[1].Index(i).String()
	}
	if err := reader.SetHiddenLayers(index, ids); err != nil {
		return errorValue(err)
	}
	return js.ValueOf(true)
}

func (a *wasmApp) search(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 || len(args) > 2 || args[0].Type() != js.TypeString {
		return errorValue(errors.New("ofd.search 需要查询字符串和可选请求 ID"))
	}
	id, err := renderRequestID(args[1:])
	if err != nil {
		return errorValue(err)
	}
	query := args[0].String()
	return a.renderAsync(id, func(ctx context.Context) (js.Value, error) {
		report, searchErr := reader.Search(ctx, query, ofdreader.SearchOptions{
			MaxPages: wasmMaxSearchPages, MaxResults: wasmMaxSearchResults,
		})
		if searchErr != nil {
			return js.Undefined(), searchErr
		}
		return jsonValue(report)
	})
}

func (a *wasmApp) renderPage(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 || len(args) > 3 {
		return errorValue(errors.New("ofd.renderPage 需要页面索引和可选配置"))
	}
	index, err := jsIndex(args[0])
	if err != nil {
		return errorValue(err)
	}
	options, err := renderOptions(args[1:min(len(args), 2)])
	if err != nil {
		return errorValue(err)
	}
	id, err := renderRequestID(args[min(len(args), 2):])
	if err != nil {
		return errorValue(err)
	}
	return a.renderAsync(id, func(ctx context.Context) (js.Value, error) {
		buffer := a.acquireWASMRenderBuffer()
		defer a.releaseWASMRenderBuffer(buffer)
		if err := reader.RenderPage(ctx, buffer, index, options); err != nil {
			return js.Undefined(), err
		}
		data := buffer.Bytes()
		result := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(result, data)
		return result, nil
	})
}

func (a *wasmApp) renderPages(_ js.Value, args []js.Value) any {
	reader, err := a.currentReader()
	if err != nil {
		return errorValue(err)
	}
	if len(args) < 1 || len(args) > 3 {
		return errorValue(errors.New("ofd.renderPages 需要页面索引数组和可选配置"))
	}
	indices, err := jsIndices(args[0], wasmMaxRenderPages)
	if err != nil {
		return errorValue(err)
	}
	options, err := renderOptions(args[1:min(len(args), 2)])
	if err != nil {
		return errorValue(err)
	}
	id, err := renderRequestID(args[min(len(args), 2):])
	if err != nil {
		return errorValue(err)
	}
	return a.renderAsync(id, func(ctx context.Context) (js.Value, error) {
		result := js.Global().Get("Array").New(len(indices))
		err := reader.RenderPages(ctx, indices, options, func(_ context.Context, position, _ int, page []byte) error {
			image := js.Global().Get("Uint8Array").New(len(page))
			js.CopyBytesToJS(image, page)
			result.SetIndex(position, image)
			return nil
		})
		if err != nil {
			return js.Undefined(), err
		}
		return result, nil
	})
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
	options, err := renderStreamOptions(args[1:min(len(args), 2)])
	if err != nil {
		return errorValue(err)
	}
	// renderStream 始终为导出；单页和多页在写入任何结果之前统一检查。
	options.Purpose = ofdreader.PermissionExport
	if err := reader.CheckPermission(ofdreader.PermissionExport, indices); err != nil {
		return errorValue(err)
	}
	streamID, err := renderRequestID(args[min(len(args), 3):])
	if err != nil {
		return errorValue(err)
	}
	if options.Format != ofdreader.RenderFormat("pdf") && len(indices) == 1 {
		if len(args) > 2 && !args[2].IsNull() && !args[2].IsUndefined() {
			return errorValue(errors.New("ofd.renderStream 单页时不需要输出回调"))
		}
		return a.renderAsync(streamID, func(ctx context.Context) (js.Value, error) {
			buffer := a.acquireWASMRenderBuffer()
			defer a.releaseWASMRenderBuffer(buffer)
			if err := reader.RenderPage(ctx, buffer, indices[0], options); err != nil {
				return js.Undefined(), err
			}
			data := buffer.Bytes()
			result := js.Global().Get("Uint8Array").New(len(data))
			js.CopyBytesToJS(result, data)
			return result, nil
		})
	}
	if options.Format != ofdreader.RenderFormat("pdf") && len(indices) < 2 {
		return errorValue(errors.New("ofd.renderStream 页面列表为空"))
	}
	if options.Format != ofdreader.RenderFormat("pdf") && options.Format != ofdreader.RenderPNG && options.Format != ofdreader.RenderJPG {
		return errorValue(errors.New("ofd.renderStream 多页时只支持 PDF、PNG 或 JPG"))
	}
	if len(args) < 3 || args[2].Type() != js.TypeFunction {
		return errorValue(errors.New("ofd.renderStream 需要输出回调"))
	}
	callback := args[2]
	return a.renderAsync(streamID, func(ctx context.Context) (js.Value, error) {
		writer := newJSChunkWriter(ctx, callback, streamID)
		defer writer.cancel()
		if streamID != 0 {
			a.streamsMu.Lock()
			if a.streams == nil {
				a.streams = make(map[uint64]*jsChunkWriter)
			}
			a.streams[streamID] = writer
			a.streamsMu.Unlock()
		}

		defer func() {
			if streamID != 0 {
				a.streamsMu.Lock()
				if a.streams[streamID] == writer {
					delete(a.streams, streamID)
				}
				a.streamsMu.Unlock()
			}
		}()
		var err error
		if options.Format == ofdreader.RenderFormat("pdf") {
			// writer.ctx derives from ctx and also cancels when the stream consumer fails.
			//nolint:contextcheck
			err = reader.RenderPDF(writer.ctx, writer, indices, options)
		} else {
			archive := zip.NewWriter(writer)
			pageBuffer := a.acquireWASMRenderBuffer()
			defer a.releaseWASMRenderBuffer(pageBuffer)
			for position, index := range indices {
				header := &zip.FileHeader{
					Name:   fmt.Sprintf("page-%04d.%s", index+1, options.Format),
					Method: zip.Store,
				}
				entry, createErr := archive.CreateHeader(header)
				if createErr != nil {
					err = fmt.Errorf("创建图片 ZIP 条目失败: %w", createErr)
					return js.Undefined(), err
				}
				pageBuffer.Reset()
				// writer.ctx derives from ctx and also cancels when the stream consumer fails.
				//nolint:contextcheck
				if renderErr := reader.RenderPage(writer.ctx, pageBuffer, index, options); renderErr != nil {
					err = fmt.Errorf("处理图片第 %d 页失败: %w", position+1, renderErr)
					return js.Undefined(), err
				}
				if _, writeErr := entry.Write(pageBuffer.Bytes()); writeErr != nil {
					err = fmt.Errorf("写入图片 ZIP 条目失败: %w", writeErr)
					return js.Undefined(), err
				}
			}
			err = archive.Close()
		}
		if err == nil {
			err = writer.flush()
		}
		return js.Null(), err
	})
}

const wasmOutputChunkSize = 64 << 10

type jsChunkWriter struct {
	callback   js.Value
	streamID   uint64
	buffer     []byte
	ackMu      sync.Mutex
	acks       map[uint64]chan error
	ctx        context.Context
	cancelFunc context.CancelFunc
	sequence   uint64
}

func newJSChunkWriter(parent context.Context, callback js.Value, streamID uint64) *jsChunkWriter {
	ctx, cancel := context.WithCancel(parent)
	return &jsChunkWriter{
		callback:   callback,
		streamID:   streamID,
		acks:       make(map[uint64]chan error),
		ctx:        ctx,
		cancelFunc: cancel,
	}
}

func (w *jsChunkWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	written := len(data)
	for len(data) > 0 {
		space := wasmOutputChunkSize - len(w.buffer)
		count := min(len(data), space)
		w.buffer = append(w.buffer, data[:count]...)
		data = data[count:]
		if len(w.buffer) == wasmOutputChunkSize {
			if err := w.flush(); err != nil {
				return written - len(data), err
			}
		}
	}
	return written, nil
}

func (w *jsChunkWriter) flush() error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
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
		case <-w.ctx.Done():
			w.removeAck(sequence)
			return w.ctx.Err()
		}
	}
	if err := w.ctx.Err(); err != nil {
		return err
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
	w.cancelFunc()
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
	streamID, idErr := renderRequestID(args[:1])
	sequence, seqErr := renderRequestID(args[1:2])
	if idErr != nil || seqErr != nil || streamID == 0 || sequence == 0 {
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

func (a *wasmApp) currentReader() (*ofdreader.Reader, error) {
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

func renderOptions(args []js.Value) (ofdreader.RenderOptions, error) {
	return parseRenderOptions(args, false)
}

func renderStreamOptions(args []js.Value) (ofdreader.RenderOptions, error) {
	return parseRenderOptions(args, true)
}

func parseRenderOptions(args []js.Value, allowPDF bool) (ofdreader.RenderOptions, error) {
	options := ofdreader.RenderOptions{
		DPI: 72, Background: color.Transparent, Format: ofdreader.RenderPNG,
		PNGCompressionLevel: 1,
	}
	if len(args) == 0 || args[0].IsUndefined() || args[0].IsNull() {
		return options, nil
	}
	if args[0].Type() != js.TypeObject {
		return options, errors.New("renderPage 配置必须是对象")
	}
	value := args[0]
	if purpose := value.Get("purpose"); !purpose.IsUndefined() && !purpose.IsNull() {
		if purpose.Type() != js.TypeString || (purpose.String() != "export" && purpose.String() != "print") {
			return options, errors.New("purpose 必须为 export 或 print；预览时省略")
		}
		options.Purpose = ofdreader.PermissionAction(purpose.String())
	}
	if format := value.Get("format"); !format.IsUndefined() && !format.IsNull() {
		if format.Type() != js.TypeString {
			return options, errors.New("format 必须是 pdf、png、jpg 或 svg")
		}
		switch strings.ToLower(strings.TrimSpace(format.String())) {
		case "pdf":
			if !allowPDF {
				return options, errors.New("format 必须是 png、jpg 或 svg")
			}
			options.Format = ofdreader.RenderFormat("pdf")
		case string(ofdreader.RenderPNG):
			options.Format = ofdreader.RenderPNG
		case string(ofdreader.RenderSVG):
			options.Format = ofdreader.RenderSVG
		case string(ofdreader.RenderJPG):
			options.Format = ofdreader.RenderJPG
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

func fontsValue(fonts []ofdreader.FontResource) js.Value {
	result := js.Global().Get("Array").New(len(fonts))
	for index, font := range fonts {
		data := js.Global().Get("Uint8Array").New(len(font.Data))
		js.CopyBytesToJS(data, font.Data)
		result.SetIndex(index, objectValue(map[string]any{
			"id": font.ID, "template_id": font.TemplateID, "family": font.Family, "name": font.Name,
			"bold": font.Bold, "italic": font.Italic, "format": font.Format, "data": data,
		}))
	}
	return result
}

// fontUsageOptions 从 JS 参数读取字体统计上限；缺省或非数字时使用 Reader 默认值。
func fontUsageOptions(args []js.Value) ofdreader.FontUsageOptions {
	options := ofdreader.FontUsageOptions{}
	if len(args) > 0 && args[0].Type() == js.TypeNumber {
		options.MaxScan = int(args[0].Float())
	}
	if len(args) > 1 && args[1].Type() == js.TypeNumber {
		options.MaxPages = int(args[1].Float())
	}
	return options
}

func intSliceValue(values []int) js.Value {
	result := js.Global().Get("Array").New(len(values))
	for index, value := range values {
		result.SetIndex(index, value)
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
	permissionErrorDetails(result, err)
	if code := wasmErrorCode(err); code != "" {
		result.Set("code", code)
	}
	return result
}
