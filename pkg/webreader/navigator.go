package webreader

import (
	"errors"
	"sort"
	"sync"

	"github.com/zc310/ofd/internal/models"
)

// VersionInfo 描述文档体中的一个文档版本。
type VersionInfo struct {
	// Scope 是版本所属文档体的索引。
	Scope int
	// ID 是版本标识。
	ID string
	// Index 是版本序号。
	Index int
	// Current 表示该版本是否为当前版本。
	Current bool
	// Version 是该版本适用的 OFD 格式版本。
	Version string
	// Name 是版本名称。
	Name string
	// CreationDate 是版本创建日期。
	CreationDate string
	// DocRoot 是版本入口的包内绝对路径；空值表示版本描述未能加载。
	DocRoot string
	// Pages 是该版本文件清单里引用的全局页码，从 0 开始。
	Pages []int
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

// readerMetadata 缓存文档生命周期内不变的元数据，避免重复计算。
type readerMetadata struct {
	resourcesOnce sync.Once
	resourcesVal  resourceCatalog
	resourcesErr  error

	outlineOnce sync.Once
	outlineVal  OutlineTree
	outlineErr  error

	preferencesOnce sync.Once
	preferencesVal  ViewPreferences
	preferencesErr  error

	pageLinksOnce sync.Once
	pageLinksVal  []PageLink
	pageLinksErr  error

	signaturesOnce sync.Once
	signaturesVal  []SignatureInfo
	signaturesErr  error

	annotationsOnce sync.Once
	annotationsVal  []AnnotationInfo
	annotationsErr  error

	pageMediaActionsOnce sync.Once
	pageMediaActionsVal  []PageLink
	pageMediaActionsErr  error

	statsOnce sync.Once
	statsVal  DocumentStats
	statsErr  error

	fontListOnce sync.Once
	fontListVal  []FontInfo
	fontListErr  error

	fontsOnce sync.Once
	fontsVal  []FontResource
	fontsErr  error

	versionsOnce sync.Once
	versionsVal  []VersionInfo
	versionsErr  error

	attachmentsOnce sync.Once
	attachmentsVal  []AttachmentInfo
	attachmentsErr  error

	mediaOnce sync.Once
	mediaVal  []MediaInfo
	mediaErr  error

	customTagsOnce sync.Once
	customTagsVal  []CustomTagTree
	customTagsErr  error
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
// 返回缓存树及所有嵌套切片和指针只读。
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
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata.outlineOnce.Do(func() {
		r.metadata.outlineVal = r.computeOutline()
	})
	return r.metadata.outlineVal, r.metadata.outlineErr
}

// pageIndexesByScope 按文档体把页 ID 映射为阅读器的全局页索引。
// GB/T 33190—2016 表 2 规定 ST_ID 在文档内唯一，不同文档体可复用同一 ID。
func (r *Reader) pageIndexesByScope() map[int]map[models.StID]int {
	return r.navigationIndexes().pages
}

type pageNavigation struct {
	pages     map[int]map[models.StID]int
	first     map[int]int
	bookmarks map[int]map[string]models.CtDest
}

// 调用方持有 Reader 锁；仅版本重建时重置，隐藏图层不改变页面与书签索引。
func (r *Reader) navigationIndexes() *pageNavigation {
	r.navigationOnce.Do(func() {
		nav := &pageNavigation{
			pages: make(map[int]map[models.StID]int), first: make(map[int]int),
			bookmarks: make(map[int]map[string]models.CtDest),
		}
		for index, ref := range r.pages {
			if _, ok := nav.first[ref.fontScope]; !ok {
				nav.first[ref.fontScope] = index
				if ref.document != nil && ref.document.Bookmarks != nil {
					byName := make(map[string]models.CtDest, len(ref.document.Bookmarks.Bookmarks))
					for _, bookmark := range ref.document.Bookmarks.Bookmarks {
						byName[bookmark.Name] = bookmark.Dest
					}
					nav.bookmarks[ref.fontScope] = byName
				}
			}
			if ref.page == nil {
				continue
			}
			byPage := nav.pages[ref.fontScope]
			if byPage == nil {
				byPage = make(map[models.StID]int)
				nav.pages[ref.fontScope] = byPage
			}
			byPage[ref.page.ID] = index
		}
		r.navigation = nav
	})
	return r.navigation
}

func (r *Reader) firstPageOfScope(scope int) int {
	if index, ok := r.navigationIndexes().first[scope]; ok {
		return index
	}
	return -1
}

func (r *Reader) resetPageNavigationLocked() {
	r.navigation = nil
	r.navigationOnce = sync.Once{}
}

func (r *Reader) computeOutline() OutlineTree {
	var result OutlineTree
	indexes := r.pageIndexesByScope()
	for scope, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		pageIndexes := indexes[scope]
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
	}
	return result
}

// Preferences 返回文档声明的阅读器显示偏好（取第一个声明了偏好的文档体）。
// 没有声明时返回零值。
// 返回值中的指针只读。
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
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata.preferencesOnce.Do(func() {
		r.metadata.preferencesVal = r.computePreferences()
	})
	return r.metadata.preferencesVal, r.metadata.preferencesErr
}

func (r *Reader) computePreferences() ViewPreferences {
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
		return result
	}
	return ViewPreferences{}
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

// outlineNodes 迭代转换大纲项；目标优先取 Goto.Dest，其次按书签名称解析，
// 没有页面目标时回退到 URI 链接。
func outlineNodes(elems []models.CTOutlineElem, resolvePage func(models.CtDest) int, bookmarks map[string]models.CtDest) []OutlineNode {
	if len(elems) == 0 {
		return nil
	}
	nodes := make([]OutlineNode, len(elems))
	type frame struct {
		elems []models.CTOutlineElem
		nodes []OutlineNode
	}
	stack := []frame{{elems, nodes}}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for index, elem := range current.elems {
			node := &current.nodes[index]
			node.Title, node.Page = elem.Title, -1
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
			if len(elem.OutlineElem) != 0 {
				node.Children = make([]OutlineNode, len(elem.OutlineElem))
				stack = append(stack, frame{elem.OutlineElem, node.Children})
			}
		}
	}
	return nodes
}

// Versions 返回所有文档体的版本清单。DocRoot 可用于 SelectVersion 加载独立历史文档。
// Pages 只包含文件清单里能对应到当前视图页的页码。
// 返回缓存切片及嵌套 Pages 切片只读。
func (r *Reader) Versions() ([]VersionInfo, error) {
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
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata.versionsOnce.Do(func() {
		r.metadata.versionsVal, r.metadata.versionsErr = r.computeVersions()
	})
	return r.metadata.versionsVal, r.metadata.versionsErr
}

func (r *Reader) computeVersions() ([]VersionInfo, error) {
	infos := make([]VersionInfo, 0)
	for scope, document := range r.ofd.Documents {
		if document == nil || scope >= len(r.ofd.DocBodies) {
			continue
		}
		body := r.ofd.DocBodies[scope]
		if body.Versions == nil {
			continue
		}
		pageIndexes := r.pageIndexesByScope()[scope]
		pagePaths := make(map[string][]int)
		for _, page := range document.Document.Pages.Pages {
			if index, ok := pageIndexes[page.ID]; ok {
				location := page.BaseLoc.Resolve(document.BaseLoc.Resolve("/")).String()
				pagePaths[location] = append(pagePaths[location], index)
			}
		}
		for _, version := range body.Versions.VersionList {
			info := VersionInfo{Scope: scope, ID: version.ID, Index: version.Index, Current: version.Current}
			if loaded := document.GetVersion(version.ID); loaded != nil {
				if loaded.DocRoot != "" {
					info.DocRoot = loaded.DocRoot.Resolve(version.BaseLoc.Resolve("/").Dir()).String()
				}
				if loaded.Version != nil {
					info.Version = *loaded.Version
				}
				if loaded.Name != nil {
					info.Name = *loaded.Name
				}
				if loaded.CreationDate != nil && !loaded.CreationDate.IsZero() {
					info.CreationDate = loaded.CreationDate.Format("2006-01-02")
				}
				info.Pages = versionPages(pagePaths, version.BaseLoc.Resolve("/").Dir(), loaded.FileList.Files)
			}
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// GB/T 33190—2016 §7.3、§19.2：解析 File 的实际路径，不能用目录名推断页码。
func versionPages(pagePaths map[string][]int, base models.StLoc, files []models.VersionFile) []int {
	seen := map[int]bool{}
	result := make([]int, 0)
	for _, file := range files {
		location := file.Path.Resolve(base).String()
		for _, index := range pagePaths[location] {
			if !seen[index] {
				seen[index] = true
				result = append(result, index)
			}
		}
	}
	sort.Ints(result)
	return result
}
