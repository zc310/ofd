package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/zc310/ofd/internal/reader/preview"
	"github.com/zc310/ofd/pkg/reader"
)

var ErrStaleDocument = errors.New("文档已关闭或已切换")

// RenderPDF 为宿主导出选定页面的矢量 PDF。
func (s *Service) RenderPDF(ctx context.Context, prefix string, output io.Writer, indices []int, options reader.RenderOptions) error {
	session, err := s.current(prefix)
	if err != nil {
		return err
	}
	return session.RenderPDF(ctx, output, indices, options)
}

// RenderPage 为原生保存等宿主操作渲染指定会话的一页。
func (s *Service) RenderPage(ctx context.Context, prefix string, output io.Writer, index int, options reader.RenderOptions) error {
	session, err := s.current(prefix)
	if err != nil {
		return err
	}
	return session.RenderPage(ctx, output, index, options)
}

// MediaExportData 为原生保存操作读取指定会话的资源，并检查导出权限。
func (s *Service) MediaExportData(ctx context.Context, prefix string, scope int, id uint64) ([]byte, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.MediaExportData(ctx, scope, id)
}

// AttachmentData 为原生保存操作读取指定会话的受限附件。
func (s *Service) AttachmentData(ctx context.Context, prefix string, scope int, id string) ([]byte, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.AttachmentData(ctx, scope, id)
}

// AttachmentTextPreview 读取当前指定会话的受限文本附件。
func (s *Service) AttachmentTextPreview(ctx context.Context, prefix string, scope int, id string) (string, error) {
	session, err := s.current(prefix)
	if err != nil {
		return "", err
	}
	return session.AttachmentTextPreview(ctx, scope, id)
}

// MediaImagePreview 读取指定会话中的受限图片预览。
func (s *Service) MediaImagePreview(ctx context.Context, prefix string, scope int, id uint64) (preview.Image, error) {
	session, err := s.current(prefix)
	if err != nil {
		return preview.Image{}, err
	}
	return session.MediaImagePreview(ctx, scope, id)
}

// SignatureDetails 查询指定会话的签名验证明细。
func (s *Service) SignatureDetails(ctx context.Context, prefix string) ([]reader.SignatureInfo, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.SignatureDetails(ctx)
}

// OpenResult 是宿主打开文档后向前端返回的页面快照和预览路径。
type OpenResult struct {
	Pages       []reader.PageInfo            `json:"pages"`
	PagePrefix  string                       `json:"page_prefix"`
	Permissions []reader.DocumentPermissions `json:"permissions"`
}

// Service 管理一个当前文档；资源处理器可直接挂载到宿主的资源服务。
// Open 的输入由宿主提供，不应将任意文件路径直接暴露给不可信前端。
type Service struct {
	ops     sync.Mutex
	mu      sync.RWMutex
	session *Session
	pages   *PageHandler
	source  any
	options Options
}

// Open 打开普通文档或已解锁的只读视图；打开或读取页面失败时，保留原有文档。
// 成功替换时先撤销旧资源路径，取消旧渲染并等待释放，再发布新文档。
// 历史版本切换需要可重复打开的 input（字节、路径或仍由宿主持有的解密视图）。
func (s *Service) Open(input any, options Options) (OpenResult, error) {
	s.ops.Lock()
	defer s.ops.Unlock()
	newSession, err := Open(input, options)
	if err != nil {
		return OpenResult{}, err
	}
	result, err := s.publish(newSession)
	if err == nil {
		s.source, s.options = input, options
	}
	return result, err
}

// 调用者持有 ops；先取得全部快照，避免失败时替换仍可用的旧会话。
func (s *Service) publish(newSession *Session) (OpenResult, error) {
	pages, err := newSession.Pages()
	if err != nil {
		_ = newSession.Close()
		return OpenResult{}, err
	}
	permissions, err := newSession.Permissions()
	if err != nil {
		_ = newSession.Close()
		return OpenResult{}, err
	}
	handler, err := NewPageHandler(newSession)
	if err != nil {
		_ = newSession.Close()
		return OpenResult{}, err
	}
	s.mu.Lock()
	old := s.session
	s.session = nil
	s.pages = nil
	s.mu.Unlock()
	if old != nil {
		if err := old.Close(); err != nil {
			_ = newSession.Close()
			return OpenResult{}, fmt.Errorf("关闭旧文档: %w", err)
		}
	}
	s.mu.Lock()
	s.session, s.pages = newSession, handler
	s.mu.Unlock()
	return OpenResult{Pages: pages, PagePrefix: handler.Prefix(), Permissions: permissions}, nil
}

// Close 撤销当前文档的资源地址，取消并等待渲染，并释放文档。
// Close 后可以重新打开文档。
func (s *Service) Close() error {
	s.ops.Lock()
	defer s.ops.Unlock()
	s.source = nil
	s.mu.Lock()
	old := s.session
	s.session = nil
	s.pages = nil
	s.mu.Unlock()
	if old == nil {
		return nil
	}
	return old.Close()
}

// CloseCurrent 仅关闭指定地址对应的文档，避免过期请求关闭新文档。
func (s *Service) CloseCurrent(prefix string) error {
	s.ops.Lock()
	defer s.ops.Unlock()
	s.mu.Lock()
	if s.pages == nil || s.pages.Prefix() != prefix {
		s.mu.Unlock()
		return ErrStaleDocument
	}
	old := s.session
	s.source = nil
	s.session, s.pages = nil, nil
	s.mu.Unlock()
	return old.Close()
}

func (s *Service) current(prefix string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pages == nil || s.pages.Prefix() != prefix {
		return nil, ErrStaleDocument
	}
	return s.session, nil
}

// Page 查询指定会话的页面尺寸。旧地址不会访问新文档。
func (s *Service) Page(prefix string, index int) (reader.PageInfo, error) {
	session, err := s.current(prefix)
	if err != nil {
		return reader.PageInfo{}, err
	}
	return session.Page(index)
}

// Text 返回指定会话的文字；权限检查由 Reader 执行。
func (s *Service) Text(prefix string, index int) ([]reader.TextRun, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.Text(index)
}

// Permissions 返回当前会话的文档体权限快照。
func (s *Service) Permissions(prefix string) ([]reader.DocumentPermissions, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.Permissions()
}

// Outline 返回指定会话的大纲；旧会话地址不能读取新文档。
func (s *Service) Outline(prefix string) (reader.OutlineTree, error) {
	session, err := s.current(prefix)
	if err != nil {
		return reader.OutlineTree{}, err
	}
	return session.Outline()
}

// Preferences 查询指定会话的显示偏好。
func (s *Service) Preferences(prefix string) (reader.ViewPreferences, error) {
	session, err := s.current(prefix)
	if err != nil {
		return reader.ViewPreferences{}, err
	}
	return session.Preferences()
}

// Info 查询指定会话的元数据和权限快照。
func (s *Service) Info(prefix string) (reader.DocumentInfo, []reader.DocumentPermissions, error) {
	session, err := s.current(prefix)
	if err != nil {
		return reader.DocumentInfo{}, nil, err
	}
	return session.Info()
}

// FontList 查询指定会话的字体声明。
func (s *Service) FontList(prefix string) ([]reader.FontInfo, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.FontList()
}

// Stats 查询指定会话的资源声明统计。
func (s *Service) Stats(prefix string) (reader.DocumentStats, error) {
	session, err := s.current(prefix)
	if err != nil {
		return reader.DocumentStats{}, err
	}
	return session.Stats()
}

// Attachments 查询指定会话的附件声明。
func (s *Service) Attachments(ctx context.Context, prefix string) ([]reader.AttachmentInfo, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.Attachments(ctx)
}

// Media 查询指定会话的多媒体声明。
func (s *Service) Media(ctx context.Context, prefix string) ([]reader.MediaInfo, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.Media(ctx)
}

// AnnotationsForPage 查询指定会话单页的注解。
func (s *Service) AnnotationsForPage(ctx context.Context, prefix string, index int) ([]reader.AnnotationInfo, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.AnnotationsForPage(ctx, index)
}

// PageLinksForPage 查询指定会话单页的链接。
func (s *Service) PageLinksForPage(ctx context.Context, prefix string, index int) ([]reader.PageLink, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.PageLinksForPage(ctx, index)
}

// Layers 查询指定会话单页的图层。
func (s *Service) Layers(ctx context.Context, prefix string, index int) ([]reader.LayerInfo, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.Layers(ctx, index)
}

// SignatureSummaries 查询指定会话的签名声明。
func (s *Service) SignatureSummaries(ctx context.Context, prefix string) ([]SignatureSummary, error) {
	session, err := s.current(prefix)
	if err != nil {
		return nil, err
	}
	return session.SignatureSummaries(ctx)
}

// SetHiddenLayers 更新指定会话的图层，过期会话不能修改新文档。
func (s *Service) SetHiddenLayers(prefix string, index int, ids []string) error {
	session, err := s.current(prefix)
	if err != nil {
		return err
	}
	return session.SetHiddenLayers(index, ids)
}

// Annotations 有界扫描指定会话的注解。
func (s *Service) Annotations(ctx context.Context, prefix string) (AnnotationReport, error) {
	session, err := s.current(prefix)
	if err != nil {
		return AnnotationReport{}, err
	}
	return session.Annotations(ctx)
}

// FontUsage 查询指定会话中某字体的使用页。
func (s *Service) FontUsage(ctx context.Context, prefix string, ref reader.FontRef, options reader.FontUsageOptions) (reader.FontUsage, error) {
	session, err := s.current(prefix)
	if err != nil {
		return reader.FontUsage{}, err
	}
	return session.FontUsage(ctx, ref, options)
}

// FontUsageAll 查询指定会话的批量字体使用页。
func (s *Service) FontUsageAll(ctx context.Context, prefix string, options reader.FontUsageOptions) (reader.FontUsageReport, error) {
	session, err := s.current(prefix)
	if err != nil {
		return reader.FontUsageReport{}, err
	}
	return session.FontUsageAll(ctx, options)
}

// CheckPermission 在操作执行前重新检查当前会话的页面权限。
func (s *Service) CheckPermission(prefix string, action reader.PermissionAction, indices []int) error {
	session, err := s.current(prefix)
	if err != nil {
		return err
	}
	return session.CheckPermission(action, indices)
}

// Search 搜索指定会话；旧会话地址不能访问新文档。
func (s *Service) Search(ctx context.Context, prefix, query string, options reader.SearchOptions) (reader.SearchReport, error) {
	session, err := s.current(prefix)
	if err != nil {
		return reader.SearchReport{}, err
	}
	return session.Search(ctx, query, options)
}

// ServeHTTP 仅分发当前文档的预览地址，不提供其他文件访问。
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	handler := s.pages
	s.mu.RUnlock()
	if handler == nil {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	handler.ServeHTTP(w, r)
}
