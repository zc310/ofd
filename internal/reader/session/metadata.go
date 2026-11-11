package session

import (
	"context"

	"github.com/zc310/ofd/pkg/reader"
)

// Pages 返回页面尺寸快照，单位为毫米。
func (s *Session) Pages() ([]reader.PageInfo, error) {
	if err := s.begin(); err != nil {
		return nil, err
	}
	defer s.active.Done()
	return s.doc.Pages()
}

// Page 查询指定页的完整尺寸信息。
func (s *Session) Page(index int) (reader.PageInfo, error) {
	if err := s.begin(); err != nil {
		return reader.PageInfo{}, err
	}
	defer s.active.Done()
	return s.doc.Page(index)
}

// Text 返回文字快照；底层 Reader 在读取前检查复制权限。
func (s *Session) Text(index int) ([]reader.TextRun, error) {
	if err := s.begin(); err != nil {
		return nil, err
	}
	defer s.active.Done()
	return s.doc.Text(index)
}

// Permissions 返回权限快照；有效期相关授权必须在执行操作时重新检查。
func (s *Session) Permissions() ([]reader.DocumentPermissions, error) {
	if err := s.begin(); err != nil {
		return nil, err
	}
	defer s.active.Done()
	return s.doc.Permissions()
}

// Outline 返回当前文档的大纲与书签快照。
func (s *Session) Outline() (reader.OutlineTree, error) {
	if err := s.begin(); err != nil {
		return reader.OutlineTree{}, err
	}
	defer s.active.Done()
	return s.doc.Outline()
}

// Preferences 返回文档声明的显示偏好。
func (s *Session) Preferences() (reader.ViewPreferences, error) {
	if err := s.begin(); err != nil {
		return reader.ViewPreferences{}, err
	}
	defer s.active.Done()
	return s.doc.Preferences()
}

// Info 返回文档元数据和权限快照。
func (s *Session) Info() (reader.DocumentInfo, []reader.DocumentPermissions, error) {
	if err := s.begin(); err != nil {
		return reader.DocumentInfo{}, nil, err
	}
	defer s.active.Done()
	info, err := s.doc.Info()
	if err != nil {
		return reader.DocumentInfo{}, nil, err
	}
	permissions, err := s.doc.Permissions()
	return info, permissions, err
}

// FontList 返回字体声明，不读取或传输字体文件。
func (s *Session) FontList() ([]reader.FontInfo, error) {
	if err := s.begin(); err != nil {
		return nil, err
	}
	defer s.active.Done()
	return s.doc.FontList()
}

// Stats 返回文档声明资源数量，并在会话关闭前保持读取有效。
func (s *Session) Stats() (reader.DocumentStats, error) {
	if err := s.begin(); err != nil {
		return reader.DocumentStats{}, err
	}
	defer s.active.Done()
	return s.doc.Stats()
}

// Attachments 返回附件清单快照，不读取附件内容；取消后丢弃正在读取的结果。
func (s *Session) Attachments(ctx context.Context) ([]reader.AttachmentInfo, error) {
	return limited(ctx, s, s.listMetadata, func(_ context.Context) ([]reader.AttachmentInfo, error) {
		attachments, err := s.doc.Attachments()
		if err != nil {
			return nil, err
		}
		result := make([]reader.AttachmentInfo, len(attachments))
		copy(result, attachments)
		return result, nil
	})
}

// Media 返回多媒体声明的独立快照，不读取资源内容。
func (s *Session) Media(ctx context.Context) ([]reader.MediaInfo, error) {
	return limited(ctx, s, s.listMetadata, func(_ context.Context) ([]reader.MediaInfo, error) {
		items, err := s.doc.Media()
		if err != nil {
			return nil, err
		}
		result := make([]reader.MediaInfo, len(items))
		copy(result, items)
		return result, nil
	})
}

// CheckPermission 按当前时间检查所选页面的操作权限。
func (s *Session) CheckPermission(action reader.PermissionAction, indices []int) error {
	if err := s.begin(); err != nil {
		return err
	}
	defer s.active.Done()
	return s.doc.CheckPermission(action, indices)
}
