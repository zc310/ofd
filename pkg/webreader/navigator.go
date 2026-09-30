package webreader

import (
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"
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
	// Version 是版本号。
	Version string
	// Name 是版本名称。
	Name string
	// CreationDate 是版本创建日期。
	CreationDate string
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
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata.outlineOnce.Do(func() {
		r.metadata.outlineVal = r.computeOutline()
	})
	return r.metadata.outlineVal, r.metadata.outlineErr
}

func (r *Reader) computeOutline() OutlineTree {
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
	return result
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

// Versions 返回所有文档体的版本清单。Pages 只包含文件清单里能对应到当前文档页的页码。
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
		for _, version := range body.Versions.VersionList {
			info := VersionInfo{Scope: scope, ID: version.ID, Index: version.Index, Current: version.Current}
			if loaded := document.GetVersion(version.ID); loaded != nil {
				if loaded.Version != nil {
					info.Version = *loaded.Version
				}
				if loaded.Name != nil {
					info.Name = *loaded.Name
				}
				if loaded.CreationDate != nil && !loaded.CreationDate.IsZero() {
					info.CreationDate = loaded.CreationDate.Format("2006-01-02")
				}
				info.Pages = versionPages(r.pages, scope, loaded.FileList.Files)
			}
			infos = append(infos, info)
		}
	}
	return infos, nil
}

func versionPages(pages []pageRef, scope int, files []models.VersionFile) []int {
	start := 0
	count := 0
	for _, ref := range pages {
		if ref.fontScope < scope {
			start++
			continue
		}
		if ref.fontScope == scope {
			count++
		}
	}
	seen := map[int]bool{}
	result := make([]int, 0)
	for _, file := range files {
		match := versionPagePattern.FindStringSubmatch(strings.ReplaceAll(file.Path.Clean().String(), "\\", "/"))
		if match == nil {
			base := path.Base(file.Path.Clean().String())
			match = versionPagePattern.FindStringSubmatch(base)
		}
		if match == nil {
			continue
		}
		local, err := strconv.Atoi(match[1])
		if err != nil || local < 0 || local >= count || seen[local] {
			continue
		}
		seen[local] = true
		result = append(result, start+local)
	}
	sort.Ints(result)
	return result
}
