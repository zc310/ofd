package parser

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/utils"
)

type Document struct {
	BaseLoc   models.StLoc
	FileCache *core.Package
	models.Document
	Pages           []*Page
	pageCacheMu     sync.Mutex
	pageLeaseCond   *sync.Cond
	pageCache       *utils.LRU[*Page, int64]
	pageCacheSize   int
	pageCacheBytes  int64
	closing         bool
	resourceCacheMu sync.Mutex
	resourceCache   *utils.LRU[string, *models.Res]
	resourceErrors  *utils.LRU[string, error]

	templateMu          sync.Mutex
	templateLocations   map[models.StID]models.StLoc
	templateCache       *utils.LRU[models.StID, *models.PageContent]
	templates           map[models.StID]*models.PageContent
	templateErrors      map[models.StID]error
	drawParams          map[models.StID]*models.DrawParam
	res                 map[models.StID]*models.MultiMedia
	fontRes             map[models.StID]*models.Font
	compositeUnits      map[models.StID]*models.CompositeGraphicUnit
	publicRes           []*models.Res
	documentRes         []*models.Res
	signs               map[string]*models.Signature
	signedValues        map[string]*SignedValue
	signedValueErrors   map[string]error
	digestResults       map[string]*SignatureDigestResult
	verificationResults map[string]*SignatureVerificationResult
	verificationErrors  map[string]error
	seals               map[models.StID][]*SealInfo
	annotations         map[models.StID]*models.PageAnnot
	annotationLocations map[models.StID]models.StLoc
	annotationLoaded    map[models.StID]bool
	annotationErrors    map[models.StID]error
	annotationMu        sync.Mutex
	attachments         *models.Attachments
	attachmentsPath     models.StLoc
	attachmentsLoaded   bool
	attachmentsErr      error
	attachmentsMu       sync.Mutex
	customTags          *models.CustomTags
	customTagsPath      models.StLoc
	customTagsLoaded    bool
	customTagsErr       error
	extensions          *models.Extensions
	extensionsPath      models.StLoc
	extensionsLoaded    bool
	extensionsErr       error
	versions            map[string]*models.DocVersion
	versionLocations    map[string]models.StLoc
	versionLoaded       map[string]bool
	versionErrors       map[string]error
	metadataMu          sync.Mutex
	signatureMu         sync.RWMutex
	resourcesMu         sync.RWMutex
}

const maxPageXMLBytes = 64 << 20

func (p *Document) Init(fileCache *core.Package, dir models.StLoc) {
	p.FileCache = fileCache
	p.BaseLoc = models.StLoc(path.Dir(dir.String()))
}

func (p *Document) clearCaches() {
	p.pageCacheMu.Lock()
	p.closing = true
	cond := p.pageLeaseConditionLocked()
	for p.hasPageLeases() {
		cond.Wait()
	}
	for _, page := range p.Pages {
		if page == nil {
			continue
		}
		page.metadataMu.Lock()
		page.metadataLoaded = false
		page.metadataBox = models.StBox{}
		page.metadataErr = nil
		page.metadataMu.Unlock()
		page.mu.Lock()
		page.pageContent = models.PageContent{}
		page.pageResPath = nil
		page.cacheSize.Store(0)
		page.resources = pageResources{}
		page.loaded = false
		page.loadErr = nil
		page.mu.Unlock()
	}
	p.pageCache = nil
	p.pageCacheMu.Unlock()

	p.resourceCacheMu.Lock()
	p.resourceCache = nil
	p.resourceErrors = nil
	p.resourceCacheMu.Unlock()

	p.templateMu.Lock()
	p.templateCache = nil
	p.templates = nil
	p.templateErrors = nil
	p.templateLocations = nil
	p.templateMu.Unlock()

	p.resourcesMu.Lock()
	p.drawParams = nil
	p.res = nil
	p.fontRes = nil
	p.compositeUnits = nil
	p.publicRes = nil
	p.documentRes = nil
	p.resourcesMu.Unlock()

	p.annotationMu.Lock()
	p.annotations = nil
	p.annotationLocations = nil
	p.annotationLoaded = nil
	p.annotationErrors = nil
	p.annotationMu.Unlock()

	p.attachmentsMu.Lock()
	p.attachments = nil
	p.attachmentsPath = ""
	p.attachmentsLoaded = false
	p.attachmentsErr = nil
	p.attachmentsMu.Unlock()

	p.metadataMu.Lock()
	p.customTags = nil
	p.customTagsPath = ""
	p.customTagsLoaded = false
	p.customTagsErr = nil
	p.extensions = nil
	p.extensionsPath = ""
	p.extensionsLoaded = false
	p.extensionsErr = nil
	p.versions = nil
	p.versionLocations = nil
	p.versionLoaded = nil
	p.versionErrors = nil
	p.metadataMu.Unlock()

	p.signatureMu.Lock()
	p.signs = nil
	p.signedValues = nil
	p.signedValueErrors = nil
	p.digestResults = nil
	p.verificationResults = nil
	p.verificationErrors = nil
	p.seals = nil
	p.signatureMu.Unlock()
}

func (p *Document) pageLeaseConditionLocked() *sync.Cond {
	if p.pageLeaseCond == nil {
		p.pageLeaseCond = sync.NewCond(&p.pageCacheMu)
	}
	return p.pageLeaseCond
}

func (p *Document) hasPageLeases() bool {
	for _, page := range p.Pages {
		if page != nil && page.pinCount.Load() > 0 {
			return true
		}
	}
	return false
}

// PageCount 返回文档页面数量。
func (p *Document) PageCount() int {
	return len(p.Pages)
}

// GetPage 返回指定索引的页面。
func (p *Document) GetPage(index int) (*Page, error) {
	if p == nil || index < 0 || index >= len(p.Pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return p.Pages[index], nil
}

// PageList 返回页面指针的快照。
func (p *Document) PageList() []*Page {
	if p == nil {
		return nil
	}
	pages := make([]*Page, len(p.Pages))
	copy(pages, p.Pages)
	return pages
}

// ForEachPage 按文档顺序遍历页面。
// 回调返回 false 时停止遍历。
func (p *Document) ForEachPage(fn func(index int, page *Page) bool) {
	if p == nil || fn == nil {
		return
	}
	for index, page := range p.Pages {
		if !fn(index, page) {
			return
		}
	}
}

// collectCompositeUnits 收集资源中的复合图元定义。
func (p *Document) collectCompositeUnits(pr *models.Res) {
	if pr.CompositeGraphicUnits == nil {
		return
	}
	for _, unit := range pr.CompositeGraphicUnits.CompositeGraphicUnit {
		u := unit
		p.compositeUnits[u.ID] = &u
	}
}

func (p *Document) parsePublicRes() error {
	if len(p.CommonData.PublicRes) == 0 {
		return nil
	}
	p.publicRes = make([]*models.Res, len(p.CommonData.PublicRes))
	for i, res := range p.CommonData.PublicRes {
		pr, err := p.parseResourceFile(res, false)
		if err != nil {
			return err
		}
		p.publicRes[i] = pr
	}
	return nil
}

func (p *Document) parseDocumentRes() error {
	if len(p.CommonData.DocumentRes) == 0 {
		return nil
	}
	p.documentRes = make([]*models.Res, len(p.CommonData.DocumentRes))
	for i, res := range p.CommonData.DocumentRes {
		pr, err := p.parseResourceFile(res, true)
		if err != nil {
			return err
		}
		p.documentRes[i] = pr
	}
	return nil
}

// parseResourceFile 解析单个资源文件，并把其中的图片、复合图元、绘制参数与字体登记到
// 对应表中。resolveMedia 控制是否把图片的相对路径转换为绝对路径（Document 资源需要，
// Public 资源不需要）。
func (p *Document) parseResourceFile(res models.StLoc, resolveMedia bool) (*models.Res, error) {
	return p.parseResourceFilePath(res.Resolve(p.BaseLoc), resolveMedia)
}

func (p *Document) parsePageResourceFilePath(filePath models.StLoc, resolveMedia bool, resources *pageResources) (*models.Res, error) {
	p.resourceCacheMu.Lock()
	defer p.resourceCacheMu.Unlock()
	if p.resourceCache == nil {
		p.resourceCache = utils.NewWeightedLRU[string, *models.Res](64, 32<<20, nil)
	}
	if p.resourceErrors == nil {
		p.resourceErrors = utils.NewLRU[string, error](64, nil)
	}
	if cached, ok := p.resourceCache.Get(filePath.String()); ok {
		p.registerPageResource(cached, filePath, resolveMedia, resources)
		return cached, nil
	}
	if cachedErr, ok := p.resourceErrors.Get(filePath.String()); ok {
		return nil, cachedErr
	}
	var resource models.Res
	if err := p.FileCache.ReadXML(filePath.String(), &resource); err != nil {
		p.resourceErrors.Add(filePath.String(), err)
		return nil, err
	}
	weight := resourceSize(&resource)
	if entry, ok := p.FileCache.Lookup(filePath.String()); ok && entry.UncompressedSize > 0 {
		weight = int64(entry.UncompressedSize)
	}
	p.resourceCache.AddWeighted(filePath.String(), &resource, weight)
	p.registerPageResource(&resource, filePath, resolveMedia, resources)
	return &resource, nil
}

func (p *Document) registerPageResource(resource *models.Res, filePath models.StLoc, resolveMedia bool, resources *pageResources) {
	if resource == nil || resources == nil {
		return
	}
	p.resourcesMu.Lock()
	defer p.resourcesMu.Unlock()
	if resource.MultiMedias != nil {
		for _, media := range resource.MultiMedias.MultiMedia {
			if resolveMedia && !strings.HasPrefix(media.MediaFile.String(), "/") {
				base := filePath.Dir()
				if resource.BaseLoc != "" && resource.BaseLoc != "." {
					base = base.Join(string(resource.BaseLoc))
				}
				media.MediaFile = base.Join(string(media.MediaFile)).Clean()
			}
			p.res[media.ID] = media
			resources.media[media.ID] = media
		}
	}
	if resource.CompositeGraphicUnits != nil {
		for _, unit := range resource.CompositeGraphicUnits.CompositeGraphicUnit {
			u := unit
			p.compositeUnits[u.ID] = &u
			resources.composites[u.ID] = &u
		}
	}
	if resource.DrawParams != nil {
		for _, param := range resource.DrawParams.DrawParam {
			p.drawParams[param.ID] = param
			resources.drawParams[param.ID] = param
		}
	}
	if resource.Fonts != nil {
		for _, font := range resource.Fonts.Font {
			if font.FontFile != "" {
				font.FontFile = font.FontFile.Resolve(filePath.Dir().Join(string(resource.BaseLoc)))
			}
			p.fontRes[font.ID] = &font
			resources.fonts[font.ID] = &font
		}
	}
}

func resourceSize(resource *models.Res) int64 {
	if resource == nil {
		return 1
	}
	size := int64(1)
	if resource.MultiMedias != nil {
		size += int64(len(resource.MultiMedias.MultiMedia))
	}
	if resource.Fonts != nil {
		size += int64(len(resource.Fonts.Font))
	}
	if resource.DrawParams != nil {
		size += int64(len(resource.DrawParams.DrawParam))
	}
	if resource.CompositeGraphicUnits != nil {
		size += int64(len(resource.CompositeGraphicUnits.CompositeGraphicUnit))
	}
	return size
}

func pageArchiveSize(fileCache *core.Package, pagePath models.StLoc, pageResources []models.StLoc) int64 {
	if fileCache == nil {
		return 0
	}
	paths := make(map[string]struct{}, len(pageResources)+1)
	paths[pagePath.String()] = struct{}{}
	for _, resource := range pageResources {
		paths[pagePath.Dir().Join(resource.String()).String()] = struct{}{}
	}
	const maxInt64Uint = uint64(^uint64(0) >> 1)
	var size uint64
	for filePath := range paths {
		entry, ok := fileCache.Lookup(filePath)
		if !ok || entry.UncompressedSize == 0 {
			continue
		}
		if size > maxInt64Uint-entry.UncompressedSize {
			return int64(maxInt64Uint)
		}
		size += entry.UncompressedSize
	}
	return int64(size)
}

func (p *Document) parseResourceFilePath(path models.StLoc, resolveMedia bool) (*models.Res, error) {
	var pr models.Res
	if err := p.FileCache.ReadXML(path.String(), &pr); err != nil {
		return nil, err
	}

	if pr.MultiMedias != nil {
		for _, media := range pr.MultiMedias.MultiMedia {
			if resolveMedia && !strings.HasPrefix(media.MediaFile.String(), "/") {
				base := path.Dir()
				if pr.BaseLoc != "" && pr.BaseLoc != "." {
					base = base.Join(string(pr.BaseLoc))
				}
				media.MediaFile = base.Join(string(media.MediaFile)).Clean()
			}
			p.res[media.ID] = media
		}
	}

	p.collectCompositeUnits(&pr)

	if pr.DrawParams != nil {
		for _, param := range pr.DrawParams.DrawParam {
			p.drawParams[param.ID] = param
		}
	}
	if pr.Fonts != nil {
		for _, font := range pr.Fonts.Font {
			if font.FontFile != "" {
				font.FontFile = font.FontFile.Resolve(path.Dir().Join(string(pr.BaseLoc)))
			}
			p.fontRes[font.ID] = &font
		}
	}
	return &pr, nil
}

func (p *Document) forgetPageResources(page *Page) {
	if page == nil {
		return
	}
	page.mu.Lock()
	resources := page.resources
	page.resources = pageResources{}
	page.mu.Unlock()
	p.forgetResourceOwnership(resources, page)
}

func (p *Document) forgetResourceOwnership(resources pageResources, excluded *Page) {
	used := make([]pageResources, 0, len(p.Pages))
	for _, page := range p.Pages {
		if page == nil || page == excluded {
			continue
		}
		page.mu.Lock()
		used = append(used, page.resources)
		page.mu.Unlock()
	}
	p.resourcesMu.Lock()
	defer p.resourcesMu.Unlock()
	for id, value := range resources.media {
		if p.res[id] == value {
			if replacement, ok := pageResourceReplacement(used, "media", id); ok {
				p.res[id] = replacement.(*models.MultiMedia)
			} else {
				delete(p.res, id)
			}
		}
	}
	for id, value := range resources.drawParams {
		if p.drawParams[id] == value {
			if replacement, ok := pageResourceReplacement(used, "drawParams", id); ok {
				p.drawParams[id] = replacement.(*models.DrawParam)
			} else {
				delete(p.drawParams, id)
			}
		}
	}
	for id, value := range resources.fonts {
		if p.fontRes[id] == value {
			if replacement, ok := pageResourceReplacement(used, "fonts", id); ok {
				p.fontRes[id] = replacement.(*models.Font)
			} else {
				delete(p.fontRes, id)
			}
		}
	}
	for id, value := range resources.composites {
		if p.compositeUnits[id] == value {
			if replacement, ok := pageResourceReplacement(used, "composites", id); ok {
				p.compositeUnits[id] = replacement.(*models.CompositeGraphicUnit)
			} else {
				delete(p.compositeUnits, id)
			}
		}
	}
}

func pageResourceReplacement(resources []pageResources, kind string, id models.StID) (any, bool) {
	for _, resource := range resources {
		var candidate any
		switch kind {
		case "media":
			candidate = resource.media[id]
		case "drawParams":
			candidate = resource.drawParams[id]
		case "fonts":
			candidate = resource.fonts[id]
		case "composites":
			candidate = resource.composites[id]
		}
		if candidate != nil {
			return candidate, true
		}
	}
	return nil, false
}

func (p *Document) acquirePage(page *Page) error {
	p.pageCacheMu.Lock()
	defer p.pageCacheMu.Unlock()
	if p.closing {
		return fmt.Errorf("文档已关闭")
	}
	page.mu.Lock()
	if page.loaded {
		if page.loadErr != nil {
			page.mu.Unlock()
			return page.loadErr
		}
		page.pinCount.Add(1)
		page.mu.Unlock()
		if p.pageCache != nil {
			_, _ = p.pageCache.Get(page)
		}
		return page.loadErr
	}
	page.pinCount.Add(1)
	if page.load != nil {
		page.loadErr = page.load(page)
	}
	page.pageContent.EnsurePhysicalBox()
	page.loaded = true
	if page.loadErr != nil {
		page.pinCount.Add(-1)
		page.mu.Unlock()
		return page.loadErr
	}
	page.mu.Unlock()
	p.addPageToCacheLocked(page)
	return nil
}

func (p *Document) addPageToCacheLocked(page *Page) {
	maxWeight := p.pageCacheBytes
	if maxWeight <= 0 {
		maxWeight = 64 << 20
	}
	capacity := p.pageCacheSize
	if capacity <= 0 {
		capacity = defaultPageCacheSize
	}
	if p.pageCache == nil {
		p.pageCache = utils.NewWeightedLRU[*Page, int64](capacity, maxWeight, func(victim *Page, _ int64) {
			p.forgetPageResources(victim)
			victim.mu.Lock()
			victim.pageContent = models.PageContent{}
			victim.loaded = false
			victim.loadErr = nil
			victim.mu.Unlock()
		}, func(victim *Page, _ int64) bool {
			return victim.pinCount.Load() == 0
		})
	}
	p.pageCache.AddWeighted(page, pageSize(page), pageSize(page))
}

func (p *Document) ensurePageLoaded(page *Page) error {
	p.pageCacheMu.Lock()
	defer p.pageCacheMu.Unlock()
	if p.closing {
		return fmt.Errorf("文档已关闭")
	}
	page.mu.Lock()
	if page.loaded {
		err := page.loadErr
		page.mu.Unlock()
		if p.pageCache != nil {
			_, _ = p.pageCache.Get(page)
		}
		return err
	}
	if page.load != nil {
		page.loadErr = page.load(page)
	}
	page.pageContent.EnsurePhysicalBox()
	page.loaded = true
	err := page.loadErr
	page.mu.Unlock()
	if err != nil {
		return err
	}
	p.addPageToCacheLocked(page)
	return nil
}

func (p *Document) parse(body models.DocBody) error {
	var err error
	if err = p.FileCache.ReadXML(body.DocRoot.Resolve("/").String(), &p.Document); err != nil {
		return err
	}
	p.Pages = make([]*Page, 0, len(p.Document.Pages.Pages))
	for _, page := range p.Document.Pages.Pages {
		pageDef := page
		pagePath := pageDef.BaseLoc.Resolve(p.BaseLoc)
		p.Pages = append(p.Pages, &Page{
			ID: pageDef.ID,
			metadata: func() (models.StBox, error) {
				return readPagePhysicalBox(p.FileCache, pagePath)
			},
			document: p,
			load: func(target *Page) error {
				target.cacheSize.Store(0)
				var content models.PageContent
				if err := p.FileCache.ReadXMLLimit(pagePath.String(), &content, maxPageXMLBytes); err != nil {
					return err
				}
				if content.Area == nil {
					content.Area = &p.CommonData.PageArea
				}
				target.pageContent = content
				target.resources = pageResources{
					media:      make(map[models.StID]*models.MultiMedia),
					drawParams: make(map[models.StID]*models.DrawParam),
					fonts:      make(map[models.StID]*models.Font),
					composites: make(map[models.StID]*models.CompositeGraphicUnit),
				}
				for _, resource := range content.PageRes {
					resourcePath := pagePath.Dir().Join(resource.String())
					target.pageResPath = append(target.pageResPath, resourcePath)
					if _, resourceErr := p.parsePageResourceFilePath(resourcePath, true, &target.resources); resourceErr != nil {
						return resourceErr
					}
				}
				if size := pageArchiveSize(p.FileCache, pagePath, content.PageRes); size > 0 {
					target.cacheSize.Store(size)
				}
				return nil
			},
		})
	}
	if err = p.parseTemplates(); err != nil {
		return err
	}
	p.drawParams = make(map[models.StID]*models.DrawParam)
	p.res = make(map[models.StID]*models.MultiMedia)
	p.fontRes = make(map[models.StID]*models.Font)
	p.compositeUnits = make(map[models.StID]*models.CompositeGraphicUnit)
	if err = p.parsePublicRes(); err != nil {
		slog.Error(err.Error())
	}
	if err = p.parseDocumentRes(); err != nil {
		return err
	}
	if err = p.parseAnnotations(); err != nil {
		return err
	}
	if p.Document.Attachments != nil {
		p.attachmentsPath = p.Document.Attachments.Resolve(p.BaseLoc)
	}
	if p.Document.CustomTags != nil {
		p.customTagsPath = p.Document.CustomTags.Resolve(p.BaseLoc)
	}
	if p.Document.Extensions != nil {
		p.extensionsPath = p.Document.Extensions.Resolve(p.BaseLoc)
	}
	p.versions = make(map[string]*models.DocVersion)
	p.versionLocations = make(map[string]models.StLoc)
	p.versionLoaded = make(map[string]bool)
	if body.Versions != nil {
		for _, version := range body.Versions.VersionList {
			p.versionLocations[version.ID] = version.BaseLoc.Resolve("/")
		}
	}

	return nil
}

func (p *Document) parseTemplates() error {
	p.templates = make(map[models.StID]*models.PageContent)
	p.templateErrors = make(map[models.StID]error)
	p.templateLocations = make(map[models.StID]models.StLoc, len(p.Document.CommonData.TemplatePages))
	for _, page := range p.Document.CommonData.TemplatePages {
		p.templateLocations[page.ID] = page.BaseLoc.Resolve(p.BaseLoc)
	}
	return nil
}

// PublicResourceList 返回公共资源文件的快照。
func (p *Document) PublicResourceList() []*models.Res {
	result := make([]*models.Res, len(p.publicRes))
	copy(result, p.publicRes)
	return result
}

// DocumentResourceList 返回文档资源文件的快照。
func (p *Document) DocumentResourceList() []*models.Res {
	result := make([]*models.Res, len(p.documentRes))
	copy(result, p.documentRes)
	return result
}

// LoadCustomTags 按需读取自定义标签。
func (p *Document) LoadCustomTags() (*models.CustomTags, error) {
	p.metadataMu.Lock()
	defer p.metadataMu.Unlock()
	if p.customTagsLoaded {
		return p.customTags, p.customTagsErr
	}
	p.customTagsLoaded = true
	if p.customTagsPath == "" {
		return nil, nil
	}
	var value models.CustomTags
	if err := p.FileCache.ReadXML(p.customTagsPath.String(), &value); err != nil {
		p.customTagsErr = err
		slog.Warn("读取自定义标签失败", "file", p.customTagsPath.String(), "error", err)
		return nil, err
	}
	p.customTags = &value
	return p.customTags, nil
}

// GetCustomTags 返回自定义标签，读取失败时返回 nil。
func (p *Document) GetCustomTags() *models.CustomTags {
	value, _ := p.LoadCustomTags()
	return value
}

// LoadExtensions 按需读取扩展数据。
func (p *Document) LoadExtensions() (*models.Extensions, error) {
	p.metadataMu.Lock()
	defer p.metadataMu.Unlock()
	if p.extensionsLoaded {
		return p.extensions, p.extensionsErr
	}
	p.extensionsLoaded = true
	if p.extensionsPath == "" {
		return nil, nil
	}
	var value models.Extensions
	if err := p.FileCache.ReadXML(p.extensionsPath.String(), &value); err != nil {
		p.extensionsErr = err
		slog.Warn("读取扩展数据失败", "file", p.extensionsPath.String(), "error", err)
		return nil, err
	}
	p.extensions = &value
	return p.extensions, nil
}

// GetExtensions 返回扩展数据，读取失败时返回 nil。
func (p *Document) GetExtensions() *models.Extensions {
	value, _ := p.LoadExtensions()
	return value
}

// LoadVersion 按需读取指定版本。
func (p *Document) LoadVersion(id string) (*models.DocVersion, error) {
	p.metadataMu.Lock()
	defer p.metadataMu.Unlock()
	if p.versionLoaded == nil {
		p.versionLoaded = make(map[string]bool)
		p.versionErrors = make(map[string]error)
	}
	if p.versions == nil {
		p.versions = make(map[string]*models.DocVersion)
	}
	if p.versionLoaded[id] {
		return p.versions[id], p.versionErrors[id]
	}
	p.versionLoaded[id] = true
	location, ok := p.versionLocations[id]
	if !ok {
		return nil, nil
	}
	var value models.DocVersion
	if err := p.FileCache.ReadXML(location.String(), &value); err != nil {
		p.versionErrors[id] = err
		slog.Warn("读取文档版本失败", "version_id", id, "file", location.String(), "error", err)
		return nil, err
	}
	p.versions[id] = &value
	return &value, nil
}

// GetVersion 返回指定版本，读取失败时返回 nil。
func (p *Document) GetVersion(id string) *models.DocVersion {
	value, _ := p.LoadVersion(id)
	return value
}

// LoadAttachments 按需读取文档附件清单。
func (p *Document) LoadAttachments() (*models.Attachments, error) {
	p.attachmentsMu.Lock()
	defer p.attachmentsMu.Unlock()
	if p.attachmentsLoaded {
		return p.attachments, p.attachmentsErr
	}
	p.attachmentsLoaded = true
	if p.attachmentsPath == "" {
		return nil, nil
	}
	var attachments models.Attachments
	if err := p.FileCache.ReadXML(p.attachmentsPath.String(), &attachments); err != nil {
		p.attachmentsErr = err
		slog.Warn("读取附件清单失败", "file", p.attachmentsPath.String(), "error", err)
		return nil, err
	}
	p.attachments = &attachments
	return p.attachments, nil
}

// GetAttachments 返回附件清单，读取失败时返回 nil。
func (p *Document) GetAttachments() *models.Attachments {
	value, _ := p.LoadAttachments()
	return value
}

// LoadTemplate 按需读取并缓存指定模板页。
func (p *Document) LoadTemplate(id models.StID) (*models.PageContent, error) {
	p.templateMu.Lock()
	defer p.templateMu.Unlock()
	if content, ok := p.templates[id]; ok && content != nil {
		return content, nil
	}
	if err, ok := p.templateErrors[id]; ok {
		return nil, err
	}
	location, ok := p.templateLocations[id]
	if !ok {
		return nil, nil
	}
	var content models.PageContent
	if err := p.FileCache.ReadXML(location.String(), &content); err != nil {
		p.templateErrors[id] = err
		slog.Warn("读取模板页失败", "template_id", id, "file", location.String(), "error", err)
		return nil, err
	}
	if p.templateCache == nil {
		p.templateCache = utils.NewWeightedLRU[models.StID, *models.PageContent](32, 32<<20, func(id models.StID, _ *models.PageContent) {
			delete(p.templates, id)
		})
	}
	p.templates[id] = &content
	p.templateCache.AddWeighted(id, &content, pageContentSize(&content))
	return &content, nil
}

// GetTemplate 返回指定模板页，读取失败时返回 nil。
func (p *Document) GetTemplate(id models.StID) *models.PageContent {
	content, _ := p.LoadTemplate(id)
	return content
}

// ForEachSignedValue 遍历已解析的签名值。
// 回调返回 false 时停止遍历。
func (p *Document) ForEachSignedValue(fn func(id string, value *SignedValue) bool) {
	if p == nil || fn == nil {
		return
	}
	p.signatureMu.RLock()
	values := make([]struct {
		id    string
		value *SignedValue
	}, 0, len(p.signedValues))
	for id, value := range p.signedValues {
		values = append(values, struct {
			id    string
			value *SignedValue
		}{id: id, value: value})
	}
	p.signatureMu.RUnlock()
	for _, item := range values {
		if !fn(item.id, item.value) {
			return
		}
	}
}

// SignedValuesSnapshot 返回签名值的快照。
func (p *Document) SignedValuesSnapshot() map[string]*SignedValue {
	result := make(map[string]*SignedValue)
	p.ForEachSignedValue(func(id string, value *SignedValue) bool {
		result[id] = value
		return true
	})
	return result
}

// SetVerificationResult 保存签名验证结果。
func (p *Document) SetVerificationResult(id string, value *SignatureVerificationResult) {
	p.signatureMu.Lock()
	defer p.signatureMu.Unlock()
	if p.verificationResults == nil {
		p.verificationResults = make(map[string]*SignatureVerificationResult)
	}
	p.verificationResults[id] = value
}

// DeleteVerificationResult 删除签名验证结果。
func (p *Document) DeleteVerificationResult(id string) {
	p.signatureMu.Lock()
	defer p.signatureMu.Unlock()
	delete(p.verificationResults, id)
}

// SetVerificationError 保存签名验证错误。
func (p *Document) SetVerificationError(id string, err error) {
	p.signatureMu.Lock()
	defer p.signatureMu.Unlock()
	if p.verificationErrors == nil {
		p.verificationErrors = make(map[string]error)
	}
	p.verificationErrors[id] = err
}

// DeleteVerificationError 删除签名验证错误。
func (p *Document) DeleteVerificationError(id string) {
	p.signatureMu.Lock()
	defer p.signatureMu.Unlock()
	delete(p.verificationErrors, id)
}

// GetSignature 返回指定签名。
func (p *Document) GetSignature(id string) *models.Signature {
	p.signatureMu.RLock()
	defer p.signatureMu.RUnlock()
	return p.signs[id]
}

// GetSignedValue 返回指定签名的解析值。
func (p *Document) GetSignedValue(id string) *SignedValue {
	p.signatureMu.RLock()
	defer p.signatureMu.RUnlock()
	return p.signedValues[id]
}

// GetSignedValueError 返回指定签名值的解析错误。
func (p *Document) GetSignedValueError(id string) error {
	p.signatureMu.RLock()
	defer p.signatureMu.RUnlock()
	return p.signedValueErrors[id]
}

// GetDigestResult 返回指定签名的摘要校验结果。
func (p *Document) GetDigestResult(id string) *SignatureDigestResult {
	p.signatureMu.RLock()
	defer p.signatureMu.RUnlock()
	return p.digestResults[id]
}

// GetVerificationResult 返回指定签名的验证结果。
func (p *Document) GetVerificationResult(id string) *SignatureVerificationResult {
	p.signatureMu.RLock()
	defer p.signatureMu.RUnlock()
	return p.verificationResults[id]
}

// GetVerificationError 返回指定签名的验证错误。
func (p *Document) GetVerificationError(id string) error {
	p.signatureMu.RLock()
	defer p.signatureMu.RUnlock()
	return p.verificationErrors[id]
}

// GetSeals 返回指定页面的电子印章信息副本。
func (p *Document) GetSeals(pageID models.StID) []*SealInfo {
	p.signatureMu.RLock()
	defer p.signatureMu.RUnlock()
	values := p.seals[pageID]
	return append([]*SealInfo(nil), values...)
}

// ForEachMedia 遍历当前文档已登记的多媒体资源。
// 回调返回 false 时停止遍历。
func (p *Document) ForEachMedia(fn func(id models.StID, media *models.MultiMedia) bool) {
	if p == nil || fn == nil {
		return
	}
	p.resourcesMu.RLock()
	resources := make([]struct {
		id    models.StID
		media *models.MultiMedia
	}, 0, len(p.res))
	for id, media := range p.res {
		resources = append(resources, struct {
			id    models.StID
			media *models.MultiMedia
		}{id: id, media: media})
	}
	p.resourcesMu.RUnlock()
	for _, resource := range resources {
		if !fn(resource.id, resource.media) {
			return
		}
	}
}

// GetMedia 返回指定 ID 的多媒体资源。
func (p *Document) GetMedia(id models.StID) *models.MultiMedia {
	p.resourcesMu.RLock()
	defer p.resourcesMu.RUnlock()
	return p.res[id]
}

// GetFont 返回指定 ID 的字体资源。
func (p *Document) GetFont(id models.StID) *models.Font {
	p.resourcesMu.RLock()
	defer p.resourcesMu.RUnlock()
	return p.fontRes[id]
}

// GetCompositeUnit 返回指定 ID 的复合图元资源。
func (p *Document) GetCompositeUnit(id models.StID) *models.CompositeGraphicUnit {
	p.resourcesMu.RLock()
	defer p.resourcesMu.RUnlock()
	return p.compositeUnits[id]
}

// ForEachFont 遍历当前文档已登记的字体资源。
// 回调返回 false 时停止遍历。
func (p *Document) ForEachFont(fn func(id models.StID, font *models.Font) bool) {
	if p == nil || fn == nil {
		return
	}
	p.resourcesMu.RLock()
	fonts := make([]struct {
		id   models.StID
		font *models.Font
	}, 0, len(p.fontRes))
	for id, font := range p.fontRes {
		fonts = append(fonts, struct {
			id   models.StID
			font *models.Font
		}{id: id, font: font})
	}
	p.resourcesMu.RUnlock()
	for _, resource := range fonts {
		if !fn(resource.id, resource.font) {
			return
		}
	}
}

// Fonts 返回当前文档已登记字体资源的快照。
func (p *Document) Fonts() map[models.StID]*models.Font {
	result := make(map[models.StID]*models.Font)
	p.ForEachFont(func(id models.StID, font *models.Font) bool {
		result[id] = font
		return true
	})
	return result
}

// GetDrawParam 返回指定 ID 的绘制参数，并解析继承关系。
func (p *Document) GetDrawParam(id models.StID) *models.DrawParam {
	return p.resolveDrawParam(id, make(map[models.StID]bool))
}

func (p *Document) resolveDrawParam(id models.StID, resolving map[models.StID]bool) *models.DrawParam {
	p.resourcesMu.RLock()
	dp, ok := p.drawParams[id]
	p.resourcesMu.RUnlock()
	if !ok || resolving[id] {
		return nil
	}
	resolving[id] = true
	defer delete(resolving, id)

	result := models.DrawParam{}
	if dp.Relative > 0 {
		if relative := p.resolveDrawParam(models.StID(dp.Relative), resolving); relative != nil {
			result = *relative
		}
	}
	result.ID = dp.ID
	result.Relative = dp.Relative
	result.Override(dp)
	if !result.HasLineWidth() {
		result.LineWidth = 0.353
	}
	if result.Join == "" {
		result.Join = "Miter"
	}
	if result.Cap == "" {
		result.Cap = "Butt"
	}
	if !result.HasMiterLimit() {
		result.MiterLimit = 3.528
	}
	return &result
}
func (p *Document) ParseSigns(file *models.StLoc) error {
	p.signs = make(map[string]*models.Signature)
	p.signedValues = make(map[string]*SignedValue)
	p.signedValueErrors = make(map[string]error)
	p.digestResults = make(map[string]*SignatureDigestResult)
	p.verificationResults = make(map[string]*SignatureVerificationResult)
	p.verificationErrors = make(map[string]error)
	p.seals = make(map[models.StID][]*SealInfo)
	if file == nil {
		return nil
	}
	var err error
	var signatures Signatures
	dir := file.Dir()
	if err = p.FileCache.ReadXML(file.String(), &signatures); err != nil {
		return err
	}

	for _, body := range signatures.Signatures {
		var sig models.Signature
		if err = p.FileCache.ReadXML(body.BaseLoc.Resolve(dir).String(), &sig); err != nil {
			return err
		}
		seDir := body.BaseLoc.Resolve(dir).Dir()
		p.signs[body.ID] = &sig
		var signedValue *SignedValue
		if sig.SignedValue != "" {
			signedValuePath := sig.SignedValue.Resolve(seDir).String()
			var signedValueBytes []byte
			if signedValueBytes, err = p.FileCache.Read(signedValuePath); err != nil {
				// 签名值属于签名扩展数据。文件缺失只影响当前签名，不能中止普通文档解析。
				signedValueErr := fmt.Errorf("%s: 读取签名值失败: %w", signedValuePath, err)
				p.signedValueErrors[body.ID] = signedValueErr
				slog.Warn("读取签名值失败", "signature", body.ID, "file", signedValuePath, "error", err)
			} else if signedValue, err = ParseSignedValue(signedValueBytes); err != nil {
				// SignedValue 是二进制扩展点。非 ASN.1 生产者数据继续保持历史兼容，
				// 同时将解析错误保存下来供调用方查看。
				signedValue = &SignedValue{
					Raw:    append([]byte(nil), signedValueBytes...),
					Format: "unknown",
				}
				p.signedValues[body.ID] = signedValue
				p.signedValueErrors[body.ID] = fmt.Errorf("%s: %w", signedValuePath, err)
				slog.Warn("解析签名值失败", "file", signedValuePath, "error", err)
			} else {
				p.signedValues[body.ID] = signedValue
			}
		}
		if digestResult, digestErr := VerifySignatureDigest(p.FileCache, body.BaseLoc.Resolve(dir).String(), &sig, signedValue); digestErr != nil {
			slog.Warn("校验签名摘要失败", "signature", body.ID, "error", digestErr)
		} else {
			p.digestResults[body.ID] = digestResult
		}
		if verification, verificationErr := VerifySESSignedValue(signedValue); verificationErr != nil {
			if signedValue != nil && signedValue.SES != nil {
				p.verificationErrors[body.ID] = verificationErr
				slog.Warn("验证签名失败", "signature", body.ID, "error", verificationErr)
			}
		} else if verification != nil {
			p.verificationResults[body.ID] = verification
		}
		var sealData *SealData
		var buf []byte
		if sig.SignedInfo.Seal != nil {
			seFile := sig.SignedInfo.Seal.BaseLoc.Resolve(seDir).String()
			if buf, err = p.FileCache.Read(seFile); err != nil {
				return err
			}

			if sealData, err = ExtractSealData(buf); err != nil {
				slog.Error(fmt.Sprintf("提取签章失败(%s): %v", seFile, err))
				continue
			}
			for _, annot := range sig.SignedInfo.StampAnnot {
				p.seals[models.StID(annot.PageRef)] = append(p.seals[models.StID(annot.PageRef)], &SealInfo{StampAnnot: annot, SealData: sealData})
			}
		} else {
			if len(sig.SignedInfo.StampAnnot) > 0 {
				if buf, err = p.FileCache.Read(sig.SignedValue.Resolve(seDir).String()); err != nil {
					slog.Warn("读取签名值失败", "signature", body.ID, "file", sig.SignedValue.Resolve(seDir).String(), "error", err)
					continue
				}
				if sealData, err = ExtractSealData(buf); err != nil {
					slog.Warn("提取签章失败", "file", sig.SignedValue.Resolve(seDir).String(), "error", err)
					continue
				}
				for _, annot := range sig.SignedInfo.StampAnnot {
					p.seals[models.StID(annot.PageRef)] = append(p.seals[models.StID(annot.PageRef)], &SealInfo{StampAnnot: annot, SealData: sealData})
				}
			}
		}
	}
	return nil
}

func (p *Document) parseAnnotations() error {
	p.annotations = make(map[models.StID]*models.PageAnnot)
	p.annotationLocations = make(map[models.StID]models.StLoc)
	p.annotationLoaded = make(map[models.StID]bool)
	if p.Document.Annotations == nil {
		return nil
	}
	var annot models.Annotations
	fileName := p.Document.Annotations.Resolve(p.BaseLoc)
	if err := p.FileCache.ReadXML(fileName.String(), &annot); err != nil {
		return err
	}
	dir := fileName.Dir()
	for _, page := range annot.Pages {
		location := page.FileLoc
		if !strings.HasPrefix(location.String(), "/") {
			location = models.StLoc.Join(dir, location.String())
		}
		p.annotationLocations[models.StID(page.PageID)] = location
	}
	return nil
}

// GetAnnotation 按需读取指定页面的注解。
func (p *Document) LoadAnnotation(pageID models.StID) (*models.PageAnnot, error) {
	p.annotationMu.Lock()
	defer p.annotationMu.Unlock()
	if p.annotations == nil {
		p.annotations = make(map[models.StID]*models.PageAnnot)
	}
	if p.annotationLoaded == nil {
		p.annotationLoaded = make(map[models.StID]bool)
	}
	if p.annotationErrors == nil {
		p.annotationErrors = make(map[models.StID]error)
	}
	if p.annotationLoaded[pageID] {
		return p.annotations[pageID], p.annotationErrors[pageID]
	}
	p.annotationLoaded[pageID] = true
	location, ok := p.annotationLocations[pageID]
	if !ok {
		return nil, nil
	}
	var annot models.PageAnnot
	if err := p.FileCache.ReadXML(location.String(), &annot); err != nil {
		p.annotationErrors[pageID] = err
		slog.Warn("读取页面注释失败", "file", location.String(), "page_id", pageID, "error", err)
		return nil, err
	}
	p.annotations[pageID] = &annot
	return &annot, nil
}

// GetAnnotation 按需读取指定页面的注解，读取失败时返回 nil。
func (p *Document) GetAnnotation(pageID models.StID) *models.PageAnnot {
	value, _ := p.LoadAnnotation(pageID)
	return value
}

type Signatures struct {
	XMLName    xml.Name    `xml:"Signatures"`
	Xmlns      string      `xml:"xmlns,attr"`
	MaxSignID  *string     `xml:"MaxSignId,omitempty"`
	Signatures []Signature `xml:"Signature,omitempty"`
}
type Signature struct {
	ID      string       `xml:"ID,attr"`
	BaseLoc models.StLoc `xml:"BaseLoc,attr"`
}
type SealInfo struct {
	StampAnnot *models.StampAnnot
	SealData   *SealData
}
