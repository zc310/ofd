// Package webreader 包提供适合浏览器调用的 OFD 文档访问接口。
package webreader

import (
	"errors"
	"fmt"
	"image/color"
	"regexp"
	"sync"

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
	// textCacheCapacity 和 searchCacheCapacity 限制按页缓存的文字与搜索索引，
	// 避免浏览/搜索大文档时把所有页面的布局快照都留在内存中。
	textCacheCapacity   = 64
	searchCacheCapacity = 64
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
	cacheMu          sync.RWMutex
	renderDocsMu     sync.Mutex
	metadataMu       sync.Mutex
	closed           bool
	ofd              *parser.OFD
	pages            []pageRef
	renderDocs       *utils.LRU[renderDocumentKey, *render.Document]
	fallbackFamily   string
	fallbackFamilies []string
	text             *utils.LRU[int, []TextRun]
	search           *utils.LRU[int, searchPage]
	options          RenderOptions
	metadata         *readerMetadata
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
		ofd:      ofd,
		options:  RenderOptions{DPI: defaultDPI, Background: color.Transparent},
		metadata: &readerMetadata{},
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

var versionPagePattern = regexp.MustCompile(`(?i)(?:^|/)page_(\d+)(?:/|$)`)

// PageMediaActions 返回进入页面（PO）和打开文档（DO）时要执行的声音、影片动作。
// 页面动作挂在对应页；文档级 DO、PO 动作挂在该文档体的第一页，Page 为 -1 表示没有页面。
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
