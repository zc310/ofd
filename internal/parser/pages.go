package parser

import (
	"encoding/xml"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/goccy/go-json/v2"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/spec"
)

const (
	defaultPageCacheSize    = 8
	maxPageMetadataXMLBytes = 1 << 20
)

var (
	errEmptyPagePackage     = errors.New("页面文件包为空")
	errPageMetadataTooLarge = errors.New("页面尺寸元数据超过大小限制")
	errEmptyPage            = errors.New("页面为空")
)

type pageResources struct {
	media       map[models.StID]*models.MultiMedia
	drawParams  map[models.StID]*models.DrawParam
	fonts       map[models.StID]*models.Font
	colorSpaces map[models.StID]*models.ColorSpace
	composites  map[models.StID]*models.CompositeGraphicUnit
}

type Page struct {
	pageContent models.PageContent
	ID          models.StID
	pageResPath []spec.StLoc

	document          *Document
	load              func(*Page) error
	metadata          func() (models.StBox, error)
	effectiveMetadata func() (models.StBox, error)
	effectiveOnce     sync.Once
	effectiveBox      models.StBox
	effectiveErr      error
	resources         pageResources
	mu                sync.Mutex
	metadataMu        sync.Mutex
	metadataLoaded    bool
	metadataBox       models.StBox
	metadataErr       error
	loaded            bool
	loadErr           error
	pinCount          atomic.Int32
	cacheSize         atomic.Int64
}

// PhysicalBoxMetadata 只读取页面 XML 中的 Area/PhysicalBox，不加载页面内容或资源。
// 返回零值表示页面没有定义自己的物理区域，由调用方决定回退到文档默认尺寸。
func (p *Page) PhysicalBoxMetadata() (models.StBox, error) {
	if p == nil {
		return models.StBox{}, errors.New("页面为空")
	}
	p.metadataMu.Lock()
	defer p.metadataMu.Unlock()
	if p.metadataLoaded {
		return p.metadataBox, p.metadataErr
	}
	if p.metadata != nil {
		p.metadataBox, p.metadataErr = p.metadata()
	} else {
		p.mu.Lock()
		if p.pageContent.Area != nil {
			p.metadataBox = p.pageContent.Area.PhysicalBox
		}
		p.mu.Unlock()
	}
	p.metadataLoaded = true
	return p.metadataBox, p.metadataErr
}

func readPagePhysicalBox(fileCache *container.Package, pagePath spec.StLoc) (models.StBox, error) {
	area, _, err := readPageAreaMetadata(fileCache, pagePath)
	if area == nil {
		return models.StBox{}, err
	}
	return area.PhysicalBox, err
}

// readPageAreaMetadata 只读页面头部，保留模板引用与 Area 是否缺省，不加载资源或正文。
func readPageAreaMetadata(fileCache *container.Package, pagePath spec.StLoc) (*models.CtPageArea, []models.Template, error) {
	if fileCache == nil {
		return nil, nil, errEmptyPagePackage
	}
	reader, err := fileCache.Open(pagePath.String())
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = reader.Close() }()

	limited := &io.LimitedReader{R: reader, N: maxPageMetadataXMLBytes + 1}
	decoder := xml.NewDecoder(limited)
	var root xml.Name
	var token xml.Token
	for {
		token, err = decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if limited.N == 0 {
					return nil, nil, errPageMetadataTooLarge
				}
				return nil, nil, nil
			}
			return nil, nil, err
		}
		if start, ok := token.(xml.StartElement); ok {
			root = start.Name
			break
		}
	}

	var templates []models.Template
	for {
		token, err = decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if limited.N == 0 {
					return nil, nil, errPageMetadataTooLarge
				}
				return nil, templates, nil
			}
			return nil, nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "Template" {
				var ref models.Template
				if err := decoder.DecodeElement(&ref, &value); err != nil {
					return nil, nil, err
				}
				templates = append(templates, ref)
				continue
			}
			if value.Name.Local == "Area" {
				var area models.CtPageArea
				if err := decoder.DecodeElement(&area, &value); err != nil {
					return nil, nil, err
				}
				return &area, templates, nil
			}
			// Content 通常是页面 XML 中最大的部分，之后不再需要查找页面区域。
			if value.Name.Local == "Content" {
				return nil, templates, nil
			}
			if err := decoder.Skip(); err != nil {
				return nil, nil, err
			}
		case xml.EndElement:
			if value.Name == root {
				return nil, templates, nil
			}
		}
	}
}

// EffectivePhysicalBoxMetadata 按 §7.7 表12依次读取页面、模板和文档的物理区域。
// 只访问元数据，不加载图元和资源；PhysicalBoxMetadata 仍只返回页面自己的声明。
func (p *Page) EffectivePhysicalBoxMetadata() (models.StBox, error) {
	if p == nil {
		return models.StBox{}, errEmptyPage
	}
	p.effectiveOnce.Do(func() {
		if p.effectiveMetadata != nil {
			p.effectiveBox, p.effectiveErr = p.effectiveMetadata()
		} else {
			p.effectiveBox, p.effectiveErr = p.PhysicalBoxMetadata()
		}
	})
	return p.effectiveBox, p.effectiveErr
}

// PhysicalBoxOr 返回页面有效物理区域；元数据缺失或无效时使用 fallback，仍无效则回退 A4。
// 查询只读取页面与模板区域元数据，不加载页面内容或资源。
func (p *Page) PhysicalBoxOr(fallback models.CtPageArea) models.StBox {
	box, err := p.EffectivePhysicalBoxMetadata()
	if err == nil && box.Width > 0 && box.Height > 0 {
		return box
	}
	models.EnsurePhysicalBox(&fallback)
	return fallback.PhysicalBox
}

// PageLease 表示页面的一次使用租约。
// 租约使用完毕后必须调用 Release，推荐使用 defer lease.Release()。
// 未释放的租约会阻止所属文档关闭，以避免正在使用的页面内容被清理。
type PageLease struct {
	page     *Page
	once     sync.Once
	released atomic.Bool
}

// WithPageContent 在页面租约期间访问页面内容。
// 回调返回的错误会原样返回给调用方。
func (p *Page) WithPageContent(fn func(*models.PageContent) error) error {
	if p == nil {
		return errors.New("页面为空")
	}
	if fn == nil {
		return errors.New("页面内容回调为空")
	}
	lease, err := p.AcquireLease()
	if err != nil {
		return err
	}
	defer lease.Release()
	return lease.WithContent(fn)
}

// AcquireLease 加载并固定页面，返回可显式释放的页面租约。
// 租约持有期间页面内容不会被缓存淘汰或文档清理。
func (p *Page) AcquireLease() (*PageLease, error) {
	if err := p.acquire(); err != nil {
		return nil, err
	}
	return &PageLease{page: p}, nil
}

// WithContent 在租约保护和页面锁保护下访问页面内容。
func (l *PageLease) WithContent(fn func(*models.PageContent) error) error {
	if l == nil || l.page == nil {
		return errors.New("页面租约为空")
	}
	if fn == nil {
		return errors.New("页面内容回调为空")
	}
	if l.released.Load() {
		return errors.New("页面租约已经释放")
	}
	l.page.mu.Lock()
	defer l.page.mu.Unlock()
	if l.released.Load() || !l.page.loaded || l.page.loadErr != nil {
		return errors.New("页面内容为空")
	}
	return fn(&l.page.pageContent)
}

// Content 返回租约保护中的页面内容。
// 返回指针只能在租约释放前使用；并发修改页面时应使用 WithContent。
func (l *PageLease) Content() *models.PageContent {
	if l == nil || l.page == nil || l.released.Load() {
		return nil
	}
	if !l.page.loaded || l.page.loadErr != nil {
		return nil
	}
	return &l.page.pageContent
}

// Release 释放页面租约。重复调用 Release 是安全的。
func (l *PageLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.released.Store(true)
		l.page.release()
	})
}

// PhysicalBox 返回页面物理区域的值副本，不暴露页面内部指针。
func (p *Page) PhysicalBox() (models.StBox, error) {
	var box models.StBox
	err := p.WithPageContent(func(content *models.PageContent) error {
		content.EnsurePhysicalBox()
		if content.Area == nil {
			return errors.New("页面区域为空")
		}
		box = content.Area.PhysicalBox
		return nil
	})
	return box, err
}

// Area 返回页面区域。
func (p *Page) Area() *models.CtPageArea {
	var area *models.CtPageArea
	_ = p.WithPageContent(func(content *models.PageContent) error {
		area = content.Area
		return nil
	})
	return area
}

// Content 返回页面内容对象。
func (p *Page) Content() *models.Content {
	var contentValue *models.Content
	_ = p.WithPageContent(func(content *models.PageContent) error {
		contentValue = content.Content
		return nil
	})
	return contentValue
}

// Template 返回页面模板引用。
func (p *Page) Template() []models.Template {
	var templates []models.Template
	_ = p.WithPageContent(func(content *models.PageContent) error {
		templates = content.Template
		return nil
	})
	return templates
}

// PageRes 返回页面资源文件列表。
func (p *Page) PageRes() []spec.StLoc {
	var resources []spec.StLoc
	_ = p.WithPageContent(func(content *models.PageContent) error {
		resources = content.PageRes
		return nil
	})
	return resources
}

// PageResourceLocations 返回已经解析为包内路径的页面资源文件位置。
func (p *Page) PageResourceLocations() []spec.StLoc {
	if p == nil {
		return nil
	}
	if err := p.EnsureLoaded(); err != nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]spec.StLoc(nil), p.pageResPath...)
}

// Actions 返回页面动作集合。
func (p *Page) Actions() *models.Actions {
	var actions *models.Actions
	_ = p.WithPageContent(func(content *models.PageContent) error {
		actions = content.Actions
		return nil
	})
	return actions
}

// NewPage 创建一个已加载的页面，主要用于构造测试页面。
func NewPage(content models.PageContent) *Page {
	page := &Page{}
	page.setPageContent(content)
	return page
}

func (p *Page) setPageContent(content models.PageContent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	content.EnsurePhysicalBox()
	p.pageContent = content
	p.cacheSize.Store(0)
	p.loaded = true
	p.loadErr = nil
	p.load = nil
}

func (p *Page) acquire() error {
	if p == nil {
		return errors.New("页面为空")
	}
	if p.document != nil {
		return p.document.acquirePage(p)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loadErr != nil {
		return p.loadErr
	}
	if !p.loaded && p.load != nil {
		p.loadErr = p.load(p)
	}
	p.pageContent.EnsurePhysicalBox()
	p.loaded = true
	if p.loadErr != nil {
		return p.loadErr
	}
	p.pinCount.Add(1)
	return nil
}

func (p *Page) release() {
	if p == nil {
		return
	}
	released := false
	for {
		count := p.pinCount.Load()
		if count <= 0 {
			break
		}
		if p.pinCount.CompareAndSwap(count, count-1) {
			released = count == 1
			break
		}
	}
	if released && p.document != nil {
		p.document.pageCacheMu.Lock()
		if p.document.pageLeaseCond != nil {
			p.document.pageLeaseCond.Broadcast()
		}
		if p.document.pageCache != nil {
			p.document.pageCache.Trim()
		}
		p.document.pageCacheMu.Unlock()
	}
}

// EnsureLoaded 在首次访问页面时读取页面内容，并缓存读取结果。
func (p *Page) EnsureLoaded() error {
	if p == nil {
		return errors.New("页面为空")
	}
	if p.document != nil {
		return p.document.ensurePageLoaded(p)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded {
		return p.loadErr
	}
	if p.load != nil {
		p.loadErr = p.load(p)
	}
	p.pageContent.EnsurePhysicalBox()
	p.loaded = true
	return p.loadErr
}

func pageSize(page *Page) int64 {
	if page == nil {
		return 1
	}
	if size := page.cacheSize.Load(); size > 0 {
		return size
	}
	page.mu.Lock()
	defer page.mu.Unlock()
	if size := page.cacheSize.Load(); size > 0 {
		return size
	}
	size := pageContentSize(&page.pageContent)
	page.cacheSize.Store(size)
	return size
}

func pageContentSize(content *models.PageContent) int64 {
	if content == nil {
		return 1
	}
	data, err := json.Marshal(content)
	if err != nil || len(data) == 0 {
		return 1
	}
	return int64(len(data))
}
