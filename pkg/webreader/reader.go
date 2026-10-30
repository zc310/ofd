// Package webreader 包提供适合浏览器调用的 OFD 文档访问接口。
//
// 缓存的元数据及其嵌套切片、映射、指针和字体字节均为只读共享值，调用方如需
// 修改应先复制；各方法的注释说明返回值是否共享。Text 返回可独立修改的副本。
package webreader

import (
	"errors"
	"fmt"
	"image/color"
	"sync"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/search"
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
	// maxFontUsageScan 和 maxFontUsagePages 是按字体统计使用页面时的默认上限，
	// 避免十万级页面文档在一次统计请求中解析全部页面布局。
	maxFontUsageScan  = 10_000
	maxFontUsagePages = 500
	// maxFontUsageScanHard 和 maxFontUsagePagesHard 是调用方可以请求的绝对上限，
	// 防止传入过大的参数导致一次统计请求解析全部页面。
	maxFontUsageScanHard  = 100_000
	maxFontUsagePagesHard = 5_000
	// maxAttachmentBytes 是单个附件默认允许读取的最大字节数，硬上限为 maxAttachmentBytesHard。
	maxAttachmentBytes     = 32 << 20
	maxAttachmentBytesHard = 128 << 20
	// 文字缓存只保留近期的完整字形布局；较轻的搜索索引允许覆盖常见的千页文档，
	// 并由 searchCacheBytes 限制密集文档的实际占用。
	textCacheCapacity   = 64
	searchCacheCapacity = 1024
	// textCacheBytes 与 searchCacheBytes 是上面两个缓存的字节预算。
	//
	// 预算不是可有可无的补充：一页文字的体积随文档密度差两个数量级（实测稀疏页
	// 1.4 KB、密集页 154 KB），只按页数封顶时缓存占用会在 90 KB 到 9.8 MB 之间
	// 浮动，调用方无法预判。WASM 侧的可用内存有限，这种不确定性会直接表现为
	// 某个文档能开、换个文档就 OOM。
	//
	// 取 16 MB 是因为它高于常见文档的实际占用（典型页 16 KB，64 页约 1 MB），
	// 又给密集文档（ano.ofd 每页 154 KB）留出约 100 页的余量；条目数上限仍在
	// 生效，所以稀疏文档最多也就是 64 页。
	textCacheBytes   = 16 << 20
	searchCacheBytes = 16 << 20
)

var (
	errTooManyOpenOptions = errors.New("最多传入一组 OpenOptions")
	errEmptyInput         = errors.New("OFD 数据为空")
	errInputTooLarge      = errors.New("OFD 数据超过大小限制")
)

const (
	// RenderPNG 输出 PNG 位图。为空时也使用该格式。
	RenderPNG RenderFormat = "png"
	// RenderSVG 输出 SVG 矢量文档。
	RenderSVG RenderFormat = "svg"
	// RenderJPG 输出 JPG 位图。JPG 不支持透明度，透明区域使用白色填充。
	RenderJPG RenderFormat = "jpg"
)

func (options FontUsageOptions) normalized() FontUsageOptions {
	if options.MaxScan <= 0 {
		options.MaxScan = maxFontUsageScan
	} else if options.MaxScan > maxFontUsageScanHard {
		options.MaxScan = maxFontUsageScanHard
	}
	if options.MaxPages <= 0 {
		options.MaxPages = maxFontUsagePages
	} else if options.MaxPages > maxFontUsagePagesHard {
		options.MaxPages = maxFontUsagePagesHard
	}
	return options
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
	renderDocsMu     sync.Mutex
	metadataMu       sync.Mutex
	closed           bool
	ofd              *parser.OFD
	pages            []pageRef
	renderDocs       *cache.LRU[renderDocumentKey, *render.Document]
	renderDocUsers   map[*render.Document]int // 由 renderDocsMu 保护，仅登记正在使用的视图。
	rasterBuffers    rasterBufferPool
	fallbackFamily   string
	fallbackFamilies []string
	// 缓存指针由 mu 保护；LRU 的 Get/Add 自带锁，读操作可并发填充。
	text           *cache.LRU[int, []TextRun]
	search         *cache.LRU[int, *search.Index]
	options        RenderOptions
	metadata       *readerMetadata
	navigationOnce sync.Once
	navigation     *pageNavigation
	hiddenLayers   map[int]render.PageVisibility
	textOptions    TextOptions
}

// Open 从 OFD 字节、文件路径、io.Reader 或已完全解锁的 crypt.View 创建文档引擎。
// 不传 options 时使用默认配置，最多传入一组 OpenOptions。
// Reader.Close 仅释放 Reader 自身持有的资源或租约，不关闭调用方的输入。
func Open(input any, options ...OpenOptions) (*Reader, error) {
	if len(options) > 1 {
		return nil, errTooManyOpenOptions
	}
	var config OpenOptions
	if len(options) == 1 {
		config = options[0]
	}
	if input == nil {
		return nil, errEmptyInput
	}
	if data, ok := input.([]byte); ok {
		if len(data) == 0 {
			return nil, errEmptyInput
		}
		if len(data) > maxInputBytes {
			return nil, fmt.Errorf("%w %d MB", errInputTooLarge, maxInputBytes>>20)
		}
	}
	ofd, err := parser.NewOFD(input, parser.Options{
		PageCacheCapacity: config.PageCacheCapacity,
		PageCacheBytes:    config.PageCacheBytes,
		MaxInputBytes:     maxInputBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("解析 OFD 失败: %w", err)
	}

	r := &Reader{
		ofd:      ofd,
		options:  RenderOptions{DPI: defaultDPI, Background: color.Transparent},
		metadata: &readerMetadata{},
	}
	for documentIndex, document := range ofd.Documents {
		// 无页文档体不会进入 r.pages，也就无法由 Reader.Close 追踪和关闭。
		if len(document.Pages) == 0 {
			continue
		}
		renderDocument := render.NewDocument(r.options.Background, document)
		for _, page := range renderDocument.Pages {
			r.pages = append(r.pages, pageRef{document: renderDocument, page: page, fontScope: documentIndex})
		}
	}
	if len(r.pages) == 0 {
		_ = ofd.Close()
		return nil, errors.New("OFD 文档没有页面")
	}
	r.text = newTextCache()
	r.search = newSearchCache(len(r.pages))
	return r, nil
}

// PageMediaActions 返回进入页面（PO）和打开文档（DO）时要执行的声音、影片动作。
// 页面动作挂在对应页；文档级 DO、PO 动作挂在该文档体的第一页，Page 为 -1 表示没有页面。
// 返回缓存切片及嵌套指针只读。
func (r *Reader) PageMediaActions() ([]PageLink, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata.pageMediaActionsOnce.Do(func() {
		r.metadata.pageMediaActionsVal, r.metadata.pageMediaActionsErr = r.computePageMediaActions()
	})
	return r.metadata.pageMediaActionsVal, r.metadata.pageMediaActionsErr
}
