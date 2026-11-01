package reader

import (
	"errors"
	"fmt"
	"sort"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/content"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/navigation"
)

// AttachmentInfo 描述文档中的一个附件，只包含清单元数据。
type AttachmentInfo struct {
	// Scope 是附件所属文档体的索引。
	Scope int
	// ID 是附件 ID。
	ID string
	// Name 是附件名称。
	Name string
	// Format 是附件格式，未声明时为空。
	Format string
	// Size 是附件声明的字节数，HasSize 为 false 时表示未声明。
	Size    int64
	HasSize bool
	// ActualSize 是附件在包中的实际字节数，0 表示未知或文件不存在。
	ActualSize int64
	// Usage 是附件用途，未声明时为空。
	Usage string
	// Visible 表示附件是否可见，未声明时为 true。
	Visible bool
	// Exists 表示附件文件是否存在于包中。
	Exists bool
}

// MediaInfo 描述文档中的一个多媒体资源（图片/音频/视频），只包含清单元数据。
type MediaInfo struct {
	// Scope 是资源所属文档体的索引。
	Scope int
	// ID 是多媒体资源标识。
	ID uint64
	// Name 是资源文件名，未解析时为空。
	Name string
	// Type 是多媒体类型，通常为 Image、Audio 或 Video。
	Type string
	// Format 是多媒体格式，未声明时为空。
	Format string
	// Size 是资源在包中的实际字节数，0 表示未知或文件不存在。
	Size int64
	// Exists 表示资源文件是否存在于包中。
	Exists bool
}

// AnnotationBoundary 是注解外观的边界框，单位为毫米。
type AnnotationBoundary struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// PageLink 描述页面正文图元上的可点击链接。与 AnnotationInfo 中的 Link 注解
// 不同，这类链接直接挂在页面内容（图层）的文字、路径、图像或复合图元上。
type PageLink struct {
	// Scope 是链接所属文档体的索引。
	Scope int
	// Page 是链接所在页面的全局索引。
	Page int
	// ID 是承载链接的图元标识，未声明时为空。
	ID string
	// Boundary 是图元边界（毫米）。
	Boundary AnnotationBoundary
	// Region 是相对 Boundary 归一化到 [0,1] 的 SVG 启动路径，空串表示矩形。
	Region string
	// AppearanceBoundary 是影片播放的图元外观区域，不随启动 Region 收缩。
	AppearanceBoundary *AnnotationBoundary
	// Actions 按原顺序保存同一图元的 CLICK 动作；为空时执行本条自身。
	Actions []PageLink
	// URI 是外部链接目标，非空时点击打开链接。
	URI string
	// TargetPage 是跳转目标页全局索引，-1 表示没有页面目标。
	TargetPage int
	// Dest 是跳转目标的位置与缩放，nil 表示没有位置信息。
	Dest *OutlineDest
	// AttachmentID 是 GotoA 动作引用的附件 ID，非空时点击打开该附件。
	AttachmentID string
	// AttachmentName 是附件名称，用于界面展示与文字匹配，未解析时为空。
	AttachmentName string
	// MediaID 是声音或影片动作引用的多媒体资源 ID，0 表示没有媒体动作。
	MediaID uint64
	// MediaKind 是媒体类型，sound 或 movie；空表示不是媒体动作。
	MediaKind string
	// Operator 是影片操作：Play、Stop、Pause、Resume。声音动作为空。
	Operator string
	// Volume 是声音音量，0 到 100；nil 表示未声明。
	Volume *int
	// Repeat 表示声音是否循环。
	Repeat bool
	// Synchronous 表示非循环声音结束后才继续执行后续动作。
	Synchronous bool
	// Event 是触发事件：CLICK、PO（进入页面）、DO（文档打开）。
	Event string
}

// AnnotationInfo 描述文档中的一个注解。
type AnnotationInfo struct {
	Scope int
	// Page 是注解所在页面的全局索引。
	Page int
	// ID 是注解标识。
	ID string
	// Type 是注解类型，如 Link、Highlight、Stamp。
	Type string
	// Subtype 是注解子类型，未声明时为空。
	Subtype string
	// Creator 是创建注解的软件或用户，未声明时为空。
	Creator string
	// LastModDate 是最后修改日期（YYYY-MM-DD），未声明时为空。
	LastModDate string
	// Visible 表示注解是否可见，未声明时为 true。
	Visible bool
	// Remark 是注解备注，未声明时为空。
	Remark string
	// Boundary 是注解外观边界，未声明时为 nil。
	Boundary *AnnotationBoundary
	// URI 是链接注解的外部链接目标，非空时点击打开链接。
	URI string
	// TargetPage 是链接注解的跳转目标页全局索引，-1 表示没有页面目标。
	TargetPage int
	// Dest 是链接注解跳转目标的位置与缩放，nil 表示没有位置信息。
	Dest *OutlineDest
}

// Attachments 返回所有文档体的附件清单（只读元数据，不读取附件内容）。
// 返回缓存切片只读。
func (r *Reader) Attachments() ([]AttachmentInfo, error) {
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
	r.metadata.attachmentsOnce.Do(func() {
		r.metadata.attachmentsVal, r.metadata.attachmentsErr = r.computeAttachments()
	})
	return r.metadata.attachmentsVal, r.metadata.attachmentsErr
}

func (r *Reader) computeAttachments() ([]AttachmentInfo, error) {
	infos := make([]AttachmentInfo, 0)
	for scope, document := range r.ofd.Documents {
		if document == nil || document.Attachments == nil {
			continue
		}
		list := document.GetAttachments()
		if list == nil {
			continue
		}
		attachmentsPath := document.Attachments.Resolve(document.BaseLoc).String()
		for _, attachment := range list.Attachments {
			info := AttachmentInfo{
				Scope:   scope,
				ID:      attachment.ID,
				Name:    attachment.Name,
				Usage:   attachment.Usage,
				Visible: attachment.Visible.Value(true),
			}
			if attachment.Format != nil {
				info.Format = *attachment.Format
			}
			if attachment.Size != nil {
				info.Size = int64(*attachment.Size)
				info.HasSize = true
			}
			path := resolveAttachmentAsset(document, attachmentsPath, attachment.FileLoc)
			if path != "" && document.FileCache != nil {
				if entry, ok := document.FileCache.Lookup(path); ok && !entry.IsDir {
					info.Exists = true
					info.ActualSize = int64(entry.UncompressedSize)
				}
			}
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// AttachmentData 读取指定附件的二进制内容。maxBytes 为 0 时使用默认上限，
// 超过 maxAttachmentBytesHard 时按硬上限处理。
func (r *Reader) AttachmentData(scope int, id string, maxBytes int64) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	if maxBytes <= 0 {
		maxBytes = maxAttachmentBytes
	} else if maxBytes > maxAttachmentBytesHard {
		maxBytes = maxAttachmentBytesHard
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	document, path, ok := r.attachmentPath(scope, id)
	if !ok || path == "" {
		return nil, fmt.Errorf("附件不存在: %d/%s", scope, id)
	}
	if err := r.checkDocumentPermissionLocked(PermissionExport, scope); err != nil {
		return nil, err
	}
	entry, exists := document.FileCache.Lookup(path)
	if !exists || entry.IsDir {
		return nil, fmt.Errorf("附件文件不存在: %s", path)
	}
	if entry.UncompressedSize > uint64(maxBytes) {
		return nil, fmt.Errorf("附件超过大小限制 %d MB", maxBytes>>20)
	}
	data, err := document.FileCache.ReadLimit(path, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("读取附件失败: %w", err)
	}
	return data, nil
}

func (r *Reader) attachmentPath(scope int, id string) (*parser.Document, string, bool) {
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, "", false
	}
	document := r.ofd.Documents[scope]
	if document == nil || document.Attachments == nil {
		return nil, "", false
	}
	list := document.GetAttachments()
	if list == nil {
		return nil, "", false
	}
	attachmentsPath := document.Attachments.Resolve(document.BaseLoc).String()
	for _, attachment := range list.Attachments {
		if attachment.ID != id {
			continue
		}
		return document, resolveAttachmentAsset(document, attachmentsPath, attachment.FileLoc), true
	}
	return nil, "", false
}

// resolveAttachmentAsset 解析附件文件在包内的路径：优先相对附件清单所在目录，
// 其次相对文档 BaseLoc，与 analyzer 的解析保持一致。
func resolveAttachmentAsset(document *parser.Document, attachmentsPath string, value models.StLoc) string {
	if value.IsEmpty() {
		return ""
	}
	if value.IsAbsolute() {
		return models.StLoc(value.String()).Clean().String()
	}
	candidates := []models.StLoc{
		models.StLoc(attachmentsPath).Dir().Join(value.String()).Clean(),
	}
	if document != nil {
		candidates = append(candidates, document.BaseLoc.Join(value.String()).Clean())
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if document != nil && document.FileCache != nil && document.FileCache.Has(candidate.String()) {
			return candidate.String()
		}
	}
	if candidates[0] == "" {
		return ""
	}
	return candidates[0].String()
}

// Media 返回所有文档体登记的多媒体资源（只读元数据，不读取资源内容）。
// 返回缓存切片只读。
func (r *Reader) Media() ([]MediaInfo, error) {
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
	r.metadata.mediaOnce.Do(func() {
		r.metadata.mediaVal, r.metadata.mediaErr = r.computeMedia()
	})
	return r.metadata.mediaVal, r.metadata.mediaErr
}

func (r *Reader) computeMedia() ([]MediaInfo, error) {
	catalog, err := r.resourceCatalogLocked()
	if err != nil {
		return nil, err
	}
	infos := make([]MediaInfo, 0)
	for _, item := range catalog.media {
		media := item.media
		info := MediaInfo{Scope: item.scope, ID: uint64(item.id), Type: media.Type, Format: media.Format}
		path := media.MediaFile.String()
		if path != "" {
			info.Name = models.StLoc(path).Base()
		}
		if path != "" && item.document.FileCache != nil {
			if entry, ok := item.document.FileCache.Lookup(path); ok && !entry.IsDir {
				info.Exists = true
				info.Size = int64(entry.UncompressedSize)
			}
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Scope != infos[j].Scope {
			return infos[i].Scope < infos[j].Scope
		}
		if infos[i].Type != infos[j].Type {
			return infos[i].Type < infos[j].Type
		}
		return infos[i].ID < infos[j].ID
	})
	return infos, nil
}

// MediaData 读取指定多媒体资源的二进制内容。maxBytes 为 0 时使用默认上限，
// 超过 maxAttachmentBytesHard 时按硬上限处理。
func (r *Reader) MediaData(scope int, mediaID uint64, maxBytes int64) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	if maxBytes <= 0 {
		maxBytes = maxAttachmentBytes
	} else if maxBytes > maxAttachmentBytesHard {
		maxBytes = maxAttachmentBytesHard
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, fmt.Errorf("多媒体作用域超出范围: %d", scope)
	}
	document := r.ofd.Documents[scope]
	if document == nil {
		return nil, errors.New("多媒体所属文档为空")
	}
	r.metadataMu.Lock()
	catalog, catalogErr := r.resourceCatalogLocked()
	r.metadataMu.Unlock()
	if catalogErr != nil {
		return nil, catalogErr
	}
	var media *models.MultiMedia
	for _, item := range catalog.media {
		if item.scope == scope && uint64(item.id) == mediaID {
			media = item.media
			break
		}
	}
	if media == nil {
		return nil, fmt.Errorf("多媒体资源不存在: %d/%d", scope, mediaID)
	}
	path := media.MediaFile.String()
	if path == "" {
		return nil, fmt.Errorf("多媒体资源缺少文件路径: %d/%d", scope, mediaID)
	}
	entry, ok := document.FileCache.Lookup(path)
	if !ok || entry.IsDir {
		return nil, fmt.Errorf("多媒体文件不存在: %s", path)
	}
	if entry.UncompressedSize > uint64(maxBytes) {
		return nil, fmt.Errorf("多媒体文件超过大小限制 %d MB", maxBytes>>20)
	}
	data, err := document.FileCache.ReadLimit(path, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("读取多媒体失败: %w", err)
	}
	return data, nil
}

// Annotations 返回所有页面中的注解，按页面顺序排列。
// 返回缓存切片及嵌套指针只读。
func (r *Reader) Annotations() ([]AnnotationInfo, error) {
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
	r.metadata.annotationsOnce.Do(func() {
		r.metadata.annotationsVal, r.metadata.annotationsErr = r.computeAnnotations()
	})
	return r.metadata.annotationsVal, r.metadata.annotationsErr
}

func (r *Reader) computeAnnotations() ([]AnnotationInfo, error) {
	infos := make([]AnnotationInfo, 0)
	pageIndexes := r.pageIndexesByScope()
	for index, ref := range r.pages {
		if ref.page == nil || ref.document == nil {
			continue
		}
		annot := ref.document.GetAnnotation(ref.page.ID)
		if annot == nil {
			continue
		}
		for _, item := range annot.Annots {
			if item == nil {
				continue
			}
			info := AnnotationInfo{
				Scope:      ref.fontScope,
				Page:       index,
				ID:         item.ID,
				Type:       string(item.Type),
				Subtype:    item.Subtype,
				Creator:    item.Creator,
				Visible:    item.Visible.Value(true),
				TargetPage: -1,
			}
			if !item.LastModDate.IsZero() {
				info.LastModDate = item.LastModDate.Format("2006-01-02")
			}
			if item.Remark != nil {
				info.Remark = *item.Remark
			}
			if item.Appearance != nil && item.Appearance.Boundary != nil {
				boundary := item.Appearance.Boundary
				info.Boundary = &AnnotationBoundary{
					X:      boundary.X,
					Y:      boundary.Y,
					Width:  boundary.Width,
					Height: boundary.Height,
				}
			}
			info.URI, info.TargetPage, info.Dest = annotLinkAction(item, pageIndexes[ref.fontScope], r.navigationIndexes().bookmarks[ref.fontScope])
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// annotLinkAction 在注解外观的页面对象中查找 CLICK 动作。链接目标既可能在注解
// 外观的图形对象上（Link 注解的常见形态），也可能通过嵌套 PageBlock 组织，因此
// 这里展开嵌套块；命中外部链接或页面跳转后立即停止。
func annotLinkAction(annot *models.Annot, pageIndex map[models.StID]int, bookmarks map[string]models.CtDest) (string, int, *OutlineDest) {
	if annot == nil || annot.Appearance == nil {
		return "", -1, nil
	}
	uri, page, dest, found := pageBlockLinkAction(annot.Appearance.Items, pageIndex, bookmarks)
	if !found {
		return "", -1, nil
	}
	return uri, page, dest
}

// pageBlockLinkAction 按文档顺序遍历页面对象，返回首个 CLICK 动作的链接。
func pageBlockLinkAction(items []models.PageItem, pageIndex map[models.StID]int, bookmarks map[string]models.CtDest) (uri string, page int, dest *OutlineDest, found bool) {
	page = -1
	walkPageItems(items, func(item models.PageItem) bool {
		unit := pageItemGraphicUnit(item)
		if unit == nil || unit.Actions == nil {
			return true
		}
		uri, page, dest, found = actionLink(*unit.Actions, pageIndex, bookmarks)
		return !found
	})
	return
}

// actionLink 返回动作集合中首个 CLICK 链接动作；found 为 false 表示没有链接动作。
func actionLink(actions models.Actions, pageIndex map[models.StID]int, bookmarks map[string]models.CtDest) (string, int, *OutlineDest, bool) {
	for _, action := range actions.Action {
		if action.Event != models.ActionEventClick {
			continue
		}
		if action.URI != nil && action.URI.URI != "" {
			return navigation.URI(action.URI), -1, nil, true
		}
		if action.Goto != nil {
			var target models.CtDest
			if action.Goto.Dest != nil {
				target = *action.Goto.Dest
			} else if action.Goto.Bookmark != nil {
				var ok bool
				target, ok = bookmarks[action.Goto.Bookmark.Name]
				if !ok {
					continue
				}
			} else {
				continue
			}
			dest := convertOutlineDest(target)
			page := -1
			if resolved, ok := pageIndex[models.StID(target.PageID)]; ok {
				page = resolved
			}
			return "", page, dest, true
		}
	}
	return "", -1, nil, false
}

// mediaAction 返回动作集合中首个声音或影片动作。event 为空时接受任意事件。
func mediaAction(actions []models.CtAction, event models.ActionEvent) (PageLink, bool) {
	for _, action := range actions {
		if event != "" && action.Event != event {
			continue
		}
		if action.Sound != nil && action.Sound.ResourceID != 0 {
			link := PageLink{
				MediaID:    uint64(action.Sound.ResourceID),
				MediaKind:  "sound",
				Event:      string(action.Event),
				TargetPage: -1,
			}
			if action.Sound.Volume != nil {
				volume := *action.Sound.Volume
				link.Volume = &volume
			}
			if action.Sound.Repeat != nil {
				link.Repeat = *action.Sound.Repeat
			}
			link.Synchronous = action.Sound.Synchronous != nil && *action.Sound.Synchronous
			return link, true
		}
		if action.Movie != nil && action.Movie.ResourceID != 0 {
			operator := string(action.Movie.Operator)
			if operator == "" {
				operator = string(models.MovieOperatorPlay)
			}
			return PageLink{
				MediaID:    uint64(action.Movie.ResourceID),
				MediaKind:  "movie",
				Operator:   operator,
				Event:      string(action.Event),
				TargetPage: -1,
			}, true
		}
	}
	return PageLink{}, false
}

// PageLinks 返回页面正文图元（图层）上的可点击链接，按页面顺序排列。
// 文档级 CLICK 没有区域时，按第一页文字内容匹配 URI 或书签名称，挂到对应文字边界。
// 返回缓存切片及嵌套指针只读。
func (r *Reader) PageLinks() ([]PageLink, error) {
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
	r.metadata.pageLinksOnce.Do(func() {
		r.metadata.pageLinksVal, r.metadata.pageLinksErr = r.computePageLinks()
	})
	return r.metadata.pageLinksVal, r.metadata.pageLinksErr
}

// PageLinksForPage 返回指定页面正文、模板和注解中的可点击链接。
// 与 PageLinks 不同，此方法只读取目标页面，适合阅读器按需加载。
// 返回链接中的跳转目标指针只读。
func (r *Reader) PageLinksForPage(index int) ([]PageLink, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return r.computePageLinksForPage(index)
}

// PageMediaActionsForPage 返回指定页面进入时以及文档打开时的媒体动作。
// 页面级动作只读取目标页面；文档级 DO/PO 动作只在文档首页返回。
func (r *Reader) PageMediaActionsForPage(index int) ([]PageLink, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	ref := r.pages[index]
	if ref.page == nil || ref.document == nil || ref.document.Document == nil {
		return nil, nil
	}
	actions := make([]PageLink, 0, 3)
	snapshot, err := loadPageContentSnapshot(ref.page)
	if err != nil {
		return nil, fmt.Errorf("读取第 %d 页动作失败: %w", index, err)
	}
	if pageActions := snapshot.actions; pageActions != nil {
		// GB/T 33190—2016 表 12：页面动作的事件类型应为 PO。
		if media, ok := mediaAction(pageActions.Action, models.ActionEventPO); ok {
			media.Scope, media.Page = ref.fontScope, index
			actions = append(actions, media)
		}
	}
	if index == r.firstPageOfScope(ref.fontScope) && ref.document.Actions != nil {
		for _, event := range []models.ActionEvent{models.ActionEventDO, models.ActionEventPO} {
			if media, ok := mediaAction(ref.document.Actions.Actions, event); ok {
				media.Scope, media.Page = ref.fontScope, index
				actions = append(actions, media)
			}
		}
	}
	return actions, nil
}

// PageEntryActionsForPage 返回指定页面进入时以及文档打开时的 URI 和媒体动作。
// 页面级动作只读取目标页面；文档级 DO/PO 动作只在文档首页返回。
func (r *Reader) PageEntryActionsForPage(index int) ([]PageLink, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	ref := r.pages[index]
	if ref.page == nil || ref.document == nil || ref.document.Document == nil {
		return nil, nil
	}
	var actions []PageLink
	pageIndex := r.pageIndexesByScope()[ref.fontScope]
	bookmarks := r.navigationIndexes().bookmarks[ref.fontScope]
	appendEvents := func(source []models.CtAction, events ...models.ActionEvent) {
		for _, action := range source {
			for _, event := range events {
				if action.Event != event {
					continue
				}
				if link, ok := resolvedAction(action, pageIndex, bookmarks); ok {
					link.Scope, link.Page = ref.fontScope, index
					actions = append(actions, link)
					break
				}
			}
		}
	}
	snapshot, err := loadPageContentSnapshot(ref.page)
	if err != nil {
		return nil, fmt.Errorf("读取第 %d 页动作失败: %w", index, err)
	}
	if pageActions := snapshot.actions; pageActions != nil {
		appendEvents(pageActions.Action, models.ActionEventPO)
	}
	if index == r.firstPageOfScope(ref.fontScope) && ref.document.Actions != nil {
		appendEvents(ref.document.Actions.Actions, models.ActionEventDO, models.ActionEventPO)
	}
	return actions, nil
}

func (r *Reader) computePageLinks() ([]PageLink, error) {
	pageIndexes := r.pageIndexesByScope()
	links := make([]PageLink, 0)
	for index, ref := range r.pages {
		pageLinks, err := r.computePageLinksForPageWithIndex(index, pageIndexes[ref.fontScope])
		if err != nil {
			return nil, err
		}
		links = append(links, pageLinks...)
	}
	return links, nil
}

func (r *Reader) computePageLinksForPage(index int) ([]PageLink, error) {
	scope := r.pages[index].fontScope
	pageIndex := r.pageIndexesByScope()[scope]
	return r.computePageLinksForPageWithIndex(index, pageIndex)
}

func (r *Reader) computePageLinksForPageWithIndex(index int, pageIndex map[models.StID]int) ([]PageLink, error) {
	ref := r.pages[index]
	if ref.page == nil || ref.document == nil || ref.document.Document == nil {
		return nil, nil
	}
	snapshot, err := loadPageContentSnapshot(ref.page)
	if err != nil {
		return nil, fmt.Errorf("读取第 %d 页链接失败: %w", index, err)
	}
	links := make([]PageLink, 0)
	bookmarks := r.navigationIndexes().bookmarks[ref.fontScope]
	visibleLayers := r.visibleLayers(index, snapshot.layers)
	appendLayerLinks(visibleLayers, ref.fontScope, index, pageIndex, bookmarks, &links)
	if index == r.firstPageOfScope(ref.fontScope) {
		appendDocumentClickLinksForTexts(ref, index, pageIndex, &links, pageTextObjectsFromLayers(visibleLayers))
	}
	// 链接也可能定义在页面使用的模板页上，模板按 ZOrder 叠加到页面坐标。
	for _, tpl := range snapshot.templates {
		if r.hiddenLayers[index].HiddenTemplates[models.StID(tpl.TemplateID)] {
			continue
		}
		template, loadErr := ref.document.LoadTemplate(models.StID(tpl.TemplateID))
		if loadErr != nil {
			return nil, fmt.Errorf("读取第 %d 页模板 %d 失败: %w", index, tpl.TemplateID, loadErr)
		}
		if template == nil || template.Content == nil {
			continue
		}
		appendLayerLinks(template.Content.Layer, ref.fontScope, index, pageIndex, bookmarks, &links)
	}
	// 绘制层只负责复合单元映射，动作与正文共用解析，保留书签、附件、媒体和完整序列。
	err = ref.document.WalkCompositeActions(ref.page, r.hiddenLayers[index], func(id models.StID, unit models.CTGraphicUnit, matrix geom.Matrix, clip *geom.Path) {
		link, ok := graphicActionLink(unit, pageIndex, bookmarks)
		if !ok || !placePageLink(&link, ref.fontScope, index, matrix, clip) {
			return
		}
		link.ID = formatStID(id)
		links = append(links, link)
	})
	if err != nil {
		return nil, err
	}
	annot, err := ref.document.LoadAnnotation(ref.page.ID)
	if err != nil {
		return nil, fmt.Errorf("读取第 %d 页注解失败: %w", index, err)
	}
	if annot != nil {
		for _, item := range annot.Annots {
			if item == nil || item.Type != models.AnnotTypeLink || item.Appearance == nil || item.Appearance.Boundary == nil {
				continue
			}
			boundary := item.Appearance.Boundary
			collectPageBlockLinks(item.Appearance.Items, pageIndex, bookmarks, func(link PageLink) {
				if link.URI == "" && link.TargetPage < 0 && link.AttachmentID == "" && link.MediaID == 0 {
					return
				}
				if placePageLink(&link, ref.fontScope, index, geom.Identity.Translate(boundary.X, boundary.Y), nil) {
					link.ID = item.ID
					links = append(links, link)
				}
			})
		}
	}
	clip := content.ApplicationClip(snapshot.area)
	visible := links[:0]
	for _, link := range links {
		if placePageLink(&link, ref.fontScope, index, geom.Identity, clip) {
			visible = append(visible, link)
		}
	}
	return visible, nil
}

// placePageLink 同步变换根热区和动作序列，并限制到页面显示区域。
func placePageLink(link *PageLink, scope, page int, matrix geom.Matrix, clip *geom.Path) bool {
	path := geom.Rectangle(link.Boundary.Width, link.Boundary.Height)
	if link.Region != "" {
		path = geom.MustParseSVGPath(link.Region).Scale(link.Boundary.Width, link.Boundary.Height)
	}
	path = path.Translate(link.Boundary.X, link.Boundary.Y).Transform(matrix)
	if clip != nil {
		path = path.And(clip)
	}
	if path.Empty() {
		return false
	}
	box := path.Bounds()
	link.Boundary = AnnotationBoundary{X: box.X0, Y: box.Y0, Width: box.W(), Height: box.H()}
	link.Region = navigation.NormalizedRegion(path)
	link.Scope, link.Page = scope, page
	if link.AppearanceBoundary != nil {
		appearance := link.AppearanceBoundary
		box := geom.RectFromSize(appearance.X, appearance.Y, appearance.Width, appearance.Height).Transform(matrix)
		link.AppearanceBoundary = &AnnotationBoundary{X: box.X0, Y: box.Y0, Width: box.W(), Height: box.H()}
	}
	if len(link.Actions) > 0 {
		visible := make([]PageLink, 0, len(link.Actions))
		for _, action := range link.Actions {
			if placePageLink(&action, scope, page, matrix, clip) {
				visible = append(visible, action)
			}
		}
		link.Actions = visible
		if len(visible) == 0 {
			return false
		}
	}
	return true
}

// appendDocumentClickLinksForTexts 把没有区域的文档级 CLICK 动作挂到第一页匹配的文字上。
func appendDocumentClickLinksForTexts(ref pageRef, page int, pageIndex map[models.StID]int, links *[]PageLink, texts []*models.TextObject) {
	if ref.document == nil || ref.document.Document == nil || ref.document.Actions == nil || ref.page == nil {
		return
	}
	for _, action := range ref.document.Actions.Actions {
		if action.Event != models.ActionEventClick {
			continue
		}
		var text *models.TextObject
		link := PageLink{Scope: ref.fontScope, Page: page, Event: string(action.Event), TargetPage: -1}
		switch {
		case action.URI != nil && action.URI.URI != "":
			text = textMatching(texts, action.URI.URI)
			link.URI = navigation.URI(action.URI)
		case action.Goto != nil && action.Goto.Bookmark != nil && action.Goto.Bookmark.Name != "":
			name := action.Goto.Bookmark.Name
			text = textMatching(texts, name)
			if ref.document.Bookmarks != nil {
				for _, bookmark := range ref.document.Bookmarks.Bookmarks {
					if bookmark.Name != name {
						continue
					}
					dest := convertOutlineDest(bookmark.Dest)
					link.Dest = dest
					if resolved, ok := pageIndex[models.StID(bookmark.Dest.PageID)]; ok {
						link.TargetPage = resolved
					}
					break
				}
			}
		case action.GotoA != nil && action.GotoA.AttachID != "":
			link.AttachmentID = action.GotoA.AttachID
			link.AttachmentName = attachmentName(ref.document, action.GotoA.AttachID)
			// 附件动作优先按附件名称匹配文字（示例中文字为 “打开附件 说明.txt”），
			// 名称缺失时退回附件 ID。
			if link.AttachmentName != "" {
				text = textMatching(texts, link.AttachmentName)
			}
			if text == nil {
				text = textMatching(texts, action.GotoA.AttachID)
			}
		case action.Movie != nil && action.Movie.ResourceID != 0:
			text = firstUnusedText(texts, *links, page)
			operator := string(action.Movie.Operator)
			if operator == "" {
				operator = string(models.MovieOperatorPlay)
			}
			link.MediaID = uint64(action.Movie.ResourceID)
			link.MediaKind = "movie"
			link.Operator = operator
		default:
			continue
		}
		if text == nil || !text.Boundary.IsFinite() || text.Boundary.Width <= 0 || text.Boundary.Height <= 0 {
			continue
		}
		link.ID = formatStID(text.ID)
		link.Boundary = AnnotationBoundary{X: text.Boundary.X, Y: text.Boundary.Y, Width: text.Boundary.Width, Height: text.Boundary.Height}
		*links = append(*links, link)
	}
}

// attachmentName 按附件 ID 解析附件名称；文档或缺省时返回空串。
func attachmentName(document *render.Document, id string) string {
	if document == nil || id == "" {
		return ""
	}
	list := document.GetAttachments()
	if list == nil {
		return ""
	}
	for _, attachment := range list.Attachments {
		if attachment.ID == id {
			return attachment.Name
		}
	}
	return ""
}

// appendLayerLinks 把一组图层中的链接图元回调到 links，并补齐作用域与页码。
func appendLayerLinks(layers []*models.Layer, scope, page int, pageIndex map[models.StID]int, bookmarks map[string]models.CtDest, links *[]PageLink) {
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		collectPageBlockLinks(layer.Items, pageIndex, bookmarks, func(link PageLink) {
			link.Scope = scope
			link.Page = page
			*links = append(*links, link)
		})
	}
}

// collectPageBlockLinks 遍历页面对象，把带边界的链接动作回调给 emit。
func collectPageBlockLinks(items []models.PageItem, pageIndex map[models.StID]int, bookmarks map[string]models.CtDest, emit func(PageLink)) {
	walkPageItems(items, func(item models.PageItem) bool {
		unit := pageItemGraphicUnit(item)
		if unit == nil || unit.Actions == nil || !unit.Boundary.IsFinite() {
			return true
		}
		if unit.Boundary.Width <= 0 || unit.Boundary.Height <= 0 {
			return true
		}
		if link, ok := graphicActionLink(*unit, pageIndex, bookmarks); ok {
			link.ID = itemID(item)
			emit(link)
		}
		return true
	})
}

func resolvedAction(action models.CtAction, pages map[models.StID]int, bookmarks map[string]models.CtDest) (PageLink, bool) {
	click := action
	click.Event = models.ActionEventClick
	uri, page, dest, found := actionLink(models.Actions{Action: []models.CtAction{click}}, pages, bookmarks)
	if found {
		if uri == "" && dest == nil {
			return PageLink{}, false
		}
		return PageLink{URI: uri, TargetPage: page, Dest: dest, Event: string(action.Event)}, true
	}
	if action.GotoA != nil && action.GotoA.AttachID != "" {
		return PageLink{AttachmentID: action.GotoA.AttachID, TargetPage: -1, Event: string(action.Event)}, true
	}
	return mediaAction([]models.CtAction{action}, action.Event)
}

func graphicActionLink(unit models.CTGraphicUnit, pages map[models.StID]int, bookmarks map[string]models.CtDest) (PageLink, bool) {
	var actions []PageLink
	var region *geom.Path
	for _, action := range unit.Actions.Action {
		if action.Event != models.ActionEventClick {
			continue
		}
		link, ok := resolvedAction(action, pages, bookmarks)
		if !ok {
			continue
		}
		path := navigation.ActionRegion(action, unit)
		if path.Empty() {
			continue
		}
		box := path.Bounds()
		link.Boundary = AnnotationBoundary{X: box.X0, Y: box.Y0, Width: box.W(), Height: box.H()}
		link.Region = navigation.NormalizedRegion(path)
		if link.MediaKind == "movie" {
			box := unit.Boundary
			link.AppearanceBoundary = &AnnotationBoundary{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height}
		}
		actions = append(actions, link)
		if region == nil {
			region = path
		} else {
			region = region.Or(path)
		}
	}
	if len(actions) == 0 {
		return PageLink{}, false
	}
	root := actions[0]
	if len(actions) > 1 {
		root.Actions = actions
		box := region.Bounds()
		root.Boundary = AnnotationBoundary{X: box.X0, Y: box.Y0, Width: box.W(), Height: box.H()}
		root.Region = navigation.NormalizedRegion(region)
	}
	return root, true
}

func (r *Reader) computePageMediaActions() ([]PageLink, error) {
	actions := make([]PageLink, 0)
	seenDocument := make(map[int]bool)
	for index, ref := range r.pages {
		if ref.page == nil || ref.document == nil {
			continue
		}
		snapshot, err := loadPageContentSnapshot(ref.page)
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 页动作失败: %w", index, err)
		}
		if pageActions := snapshot.actions; pageActions != nil {
			if media, ok := mediaAction(pageActions.Action, models.ActionEventPO); ok {
				media.Scope = ref.fontScope
				media.Page = index
				actions = append(actions, media)
			}
		}
		if seenDocument[ref.fontScope] || ref.document.Document == nil || ref.document.Actions == nil {
			continue
		}
		seenDocument[ref.fontScope] = true
		for _, event := range []models.ActionEvent{models.ActionEventDO, models.ActionEventPO} {
			if media, ok := mediaAction(ref.document.Actions.Actions, event); ok {
				media.Scope = ref.fontScope
				media.Page = index
				actions = append(actions, media)
			}
		}
	}
	return actions, nil
}
