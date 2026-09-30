package webreader

import (
	"errors"
	"fmt"
	"sort"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
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
		if document == nil || document.Document.Attachments == nil {
			continue
		}
		list := document.GetAttachments()
		if list == nil {
			continue
		}
		attachmentsPath := document.Document.Attachments.Resolve(document.BaseLoc).String()
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
	if document == nil || document.Document.Attachments == nil {
		return nil, "", false
	}
	list := document.GetAttachments()
	if list == nil {
		return nil, "", false
	}
	attachmentsPath := document.Document.Attachments.Resolve(document.BaseLoc).String()
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
	infos := make([]MediaInfo, 0)
	for scope, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		document.ForEachMedia(func(id models.StID, media *models.MultiMedia) bool {
			if media == nil {
				return true
			}
			info := MediaInfo{Scope: scope, ID: uint64(id), Type: media.Type, Format: media.Format}
			path := media.MediaFile.String()
			if path != "" {
				info.Name = models.StLoc(path).Base()
			}
			if path != "" && document.FileCache != nil {
				if entry, ok := document.FileCache.Lookup(path); ok && !entry.IsDir {
					info.Exists = true
					info.Size = int64(entry.UncompressedSize)
				}
			}
			infos = append(infos, info)
			return true
		})
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
	media := document.GetMedia(models.StID(mediaID))
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
	pageIndex := make(map[models.StID]int, len(r.pages))
	for index, ref := range r.pages {
		if ref.page != nil {
			pageIndex[ref.page.ID] = index
		}
	}
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
				info.LastModDate = item.LastModDate.Time.Format("2006-01-02")
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
			info.URI, info.TargetPage, info.Dest = annotLinkAction(item, pageIndex)
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// annotLinkAction 在注解外观的页面对象中查找 CLICK 动作。链接目标既可能在注解
// 外观的图形对象上（Link 注解的常见形态），也可能通过嵌套 PageBlock 组织，因此
// 这里递归遍历；命中外部链接或页面跳转后立即停止。
func annotLinkAction(annot *models.Annot, pageIndex map[models.StID]int) (string, int, *OutlineDest) {
	if annot == nil || annot.Appearance == nil {
		return "", -1, nil
	}
	uri, page, dest, found := pageBlockLinkAction(annot.Appearance.Items, pageIndex)
	if !found {
		return "", -1, nil
	}
	return uri, page, dest
}

// pageBlockLinkAction 递归遍历页面对象，返回首个 CLICK 动作的外部链接或页面跳转。
func pageBlockLinkAction(items []models.PageItem, pageIndex map[models.StID]int) (string, int, *OutlineDest, bool) {
	for _, item := range items {
		if item.Kind == models.PageItemBlock {
			if item.Block != nil {
				if uri, page, dest, found := pageBlockLinkAction(item.Block.Items, pageIndex); found {
					return uri, page, dest, true
				}
			}
			continue
		}
		unit := pageItemGraphicUnit(item)
		if unit == nil || unit.Actions == nil {
			continue
		}
		uri, page, dest, found := actionLink(*unit.Actions, pageIndex)
		if found {
			return uri, page, dest, true
		}
	}
	return "", -1, nil, false
}

// actionLink 返回动作集合中首个 CLICK 链接动作；found 为 false 表示没有链接动作。
func actionLink(actions models.Actions, pageIndex map[models.StID]int) (string, int, *OutlineDest, bool) {
	for _, action := range actions.Action {
		if action.Event != models.ActionEventClick {
			continue
		}
		if action.URI != nil && action.URI.URI != "" {
			return action.URI.URI, -1, nil, true
		}
		if action.Goto != nil && action.Goto.Dest != nil {
			dest := convertOutlineDest(*action.Goto.Dest)
			page := -1
			if resolved, ok := pageIndex[models.StID(action.Goto.Dest.PageID)]; ok {
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

func (r *Reader) computePageLinks() ([]PageLink, error) {
	pageIndex := make(map[models.StID]int, len(r.pages))
	for index, ref := range r.pages {
		if ref.page != nil {
			pageIndex[ref.page.ID] = index
		}
	}
	links := make([]PageLink, 0)
	for index, ref := range r.pages {
		if ref.page == nil || ref.document == nil || ref.document.Document == nil {
			continue
		}
		appendLayerLinks(contentLayers(ref.page), ref.fontScope, index, pageIndex, &links)
		if index == firstPageOfScope(r.pages, ref.fontScope) {
			appendDocumentClickLinks(ref, index, pageIndex, &links)
		}
		// 链接也可能定义在页面使用的模板页上，模板按 ZOrder 叠加到页面坐标。
		for _, tpl := range ref.page.Template() {
			template := ref.document.Document.GetTemplate(models.StID(tpl.TemplateID))
			if template == nil || template.Content == nil {
				continue
			}
			appendLayerLinks(template.Content.Layer, ref.fontScope, index, pageIndex, &links)
		}
	}
	return links, nil
}

func firstPageOfScope(pages []pageRef, scope int) int {
	for index, ref := range pages {
		if ref.fontScope == scope {
			return index
		}
	}
	return -1
}

// appendDocumentClickLinks 把没有区域的文档级 CLICK 动作挂到第一页匹配的文字上。
func appendDocumentClickLinks(ref pageRef, page int, pageIndex map[models.StID]int, links *[]PageLink) {
	if ref.document == nil || ref.document.Document == nil || ref.document.Document.Actions == nil || ref.page == nil {
		return
	}
	texts := pageTextObjects(ref.page)
	for _, action := range ref.document.Document.Actions.Actions {
		if action.Event != models.ActionEventClick {
			continue
		}
		var text *models.TextObject
		link := PageLink{Scope: ref.fontScope, Page: page, Event: string(action.Event), TargetPage: -1}
		switch {
		case action.URI != nil && action.URI.URI != "":
			text = textMatching(texts, action.URI.URI)
			link.URI = action.URI.URI
		case action.Goto != nil && action.Goto.Bookmark != nil && action.Goto.Bookmark.Name != "":
			name := action.Goto.Bookmark.Name
			text = textMatching(texts, name)
			if ref.document.Document.Bookmarks != nil {
				for _, bookmark := range ref.document.Document.Bookmarks.Bookmarks {
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

// attachmentAction 返回动作集合中首个附件动作（GotoA）的附件 ID。
func attachmentAction(actions []models.CtAction, event models.ActionEvent) (string, bool) {
	for _, action := range actions {
		if event != "" && action.Event != event {
			continue
		}
		if action.GotoA != nil && action.GotoA.AttachID != "" {
			return action.GotoA.AttachID, true
		}
	}
	return "", false
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
func appendLayerLinks(layers []*models.Layer, scope, page int, pageIndex map[models.StID]int, links *[]PageLink) {
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		collectPageBlockLinks(layer.Items, pageIndex, func(link PageLink) {
			link.Scope = scope
			link.Page = page
			*links = append(*links, link)
		})
	}
}

// collectPageBlockLinks 遍历页面对象，把带边界的链接动作回调给 emit。
func collectPageBlockLinks(items []models.PageItem, pageIndex map[models.StID]int, emit func(PageLink)) {
	for _, item := range items {
		if item.Kind == models.PageItemBlock {
			if item.Block != nil {
				collectPageBlockLinks(item.Block.Items, pageIndex, emit)
			}
			continue
		}
		unit := pageItemGraphicUnit(item)
		if unit == nil || unit.Actions == nil || !unit.Boundary.IsFinite() {
			continue
		}
		if unit.Boundary.Width <= 0 || unit.Boundary.Height <= 0 {
			continue
		}
		id := itemID(item)
		boundary := AnnotationBoundary{X: unit.Boundary.X, Y: unit.Boundary.Y, Width: unit.Boundary.Width, Height: unit.Boundary.Height}
		uri, target, dest, found := actionLink(*unit.Actions, pageIndex)
		if found {
			emit(PageLink{
				ID:         id,
				Boundary:   boundary,
				URI:        uri,
				TargetPage: target,
				Dest:       dest,
				Event:      string(models.ActionEventClick),
			})
			continue
		}
		if attachID, ok := attachmentAction(unit.Actions.Action, models.ActionEventClick); ok {
			emit(PageLink{
				ID:           id,
				Boundary:     boundary,
				AttachmentID: attachID,
				Event:        string(models.ActionEventClick),
			})
			continue
		}
		media, ok := mediaAction(unit.Actions.Action, models.ActionEventClick)
		if !ok {
			continue
		}
		media.ID = id
		media.Boundary = boundary
		emit(media)
	}
}

func (r *Reader) computePageMediaActions() ([]PageLink, error) {
	actions := make([]PageLink, 0)
	seenDocument := make(map[int]bool)
	for index, ref := range r.pages {
		if ref.page == nil || ref.document == nil {
			continue
		}
		if pageActions := ref.page.Actions(); pageActions != nil {
			if media, ok := mediaAction(pageActions.Action, ""); ok {
				media.Scope = ref.fontScope
				media.Page = index
				actions = append(actions, media)
			}
		}
		if seenDocument[ref.fontScope] || ref.document.Document == nil || ref.document.Document.Actions == nil {
			continue
		}
		seenDocument[ref.fontScope] = true
		for _, event := range []models.ActionEvent{models.ActionEventDO, models.ActionEventPO} {
			if media, ok := mediaAction(ref.document.Document.Actions.Actions, event); ok {
				media.Scope = ref.fontScope
				media.Page = index
				actions = append(actions, media)
			}
		}
	}
	return actions, nil
}
