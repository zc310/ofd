package session

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"

	"github.com/zc310/ofd/internal/reader/preview"
	"github.com/zc310/ofd/pkg/reader"
)

var errPlaybackFormat = errors.New("仅支持 MP3、WAV、Ogg、MP4/M4A 和 WebM 媒体，不支持播放列表或外部资源")
var errPlaybackSize = errors.New("音视频超过 32 MiB 预览限制")
var errTemplatePlayback = errors.New("文档不支持模板媒体读取")

// PlaybackPreview 只包含受限的包内媒体，不含网络地址或播放列表。
type PlaybackPreview struct {
	MIME string `json:"mime"`
	Data []byte `json:"data"`
}

// CheckDocumentPermission 检查指定会话的文档体权限，不依赖页面列表或前端快照。
func (s *Service) CheckDocumentPermission(prefix string, scope int, action reader.PermissionAction) error {
	session, err := s.current(prefix)
	if err != nil {
		return err
	}
	if err := session.begin(); err != nil {
		return err
	}
	defer session.active.Done()
	return session.doc.CheckDocumentPermission(action, scope)
}

func (s *Session) attachmentImagePreview(ctx context.Context, scope int, id string) (preview.Image, error) {
	return limited(ctx, s, s.listMetadata, func(ctx context.Context) (preview.Image, error) {
		if err := s.doc.CheckDocumentPermission(reader.PermissionExport, scope); err != nil {
			return preview.Image{}, err
		}
		data, err := s.doc.AttachmentData(scope, id, preview.MaxImageBytes)
		if err != nil {
			return preview.Image{}, err
		}
		result, err := preview.Raster(ctx, data)
		if err != nil {
			return preview.Image{}, err
		}
		if err := s.doc.CheckDocumentPermission(reader.PermissionExport, scope); err != nil {
			return preview.Image{}, err
		}
		return result, nil
	})
}

type templateMediaDocument interface {
	TemplateMediaData(scope int, templateID, id uint64, maxBytes int64) ([]byte, error)
}

func playbackMIME(data []byte) string {
	// 标准 MIME 嗅探遗漏部分常见 M4A/isom 品牌；只补已知的媒体 ftyp 头。
	if len(data) >= 16 && string(data[4:8]) == "ftyp" {
		size := binary.BigEndian.Uint32(data[:4])
		if size >= 16 && size <= uint32(min(len(data), 512)) && size%4 == 0 {
			switch string(data[8:12]) {
			case "M4A ":
				return "audio/mp4"
			case "isom", "iso2", "avc1", "mp41", "mp42":
				return "video/mp4"
			}
		}
	}
	// 无 ID3 的 MP3 仍需合法的 MPEG 版本、Layer、比特率和采样率字段。
	if len(data) >= 4 && data[0] == 0xff && data[1]&0xe0 == 0xe0 && (data[1]>>3)&3 != 1 && (data[1]>>1)&3 != 0 && data[2]>>4 != 0 && data[2]>>4 != 15 && (data[2]>>2)&3 != 3 {
		return "audio/mpeg"
	}
	return http.DetectContentType(data)
}

func (s *Session) playbackPreview(ctx context.Context, scope int, attachment bool, read func() ([]byte, error)) (PlaybackPreview, error) {
	return limited(ctx, s, s.listMetadata, func(_ context.Context) (PlaybackPreview, error) {
		permission := reader.PermissionAccess
		if attachment {
			permission = reader.PermissionExport
		}
		if err := s.doc.CheckDocumentPermission(permission, scope); err != nil {
			return PlaybackPreview{}, err
		}
		data, err := read()
		if err != nil {
			return PlaybackPreview{}, err
		}
		if len(data) > 32<<20 {
			return PlaybackPreview{}, errPlaybackSize
		}
		mime := playbackMIME(data)
		switch mime {
		case "audio/mpeg", "audio/mp4", "audio/wave", "audio/wav", "application/ogg", "video/mp4", "video/webm":
		default:
			return PlaybackPreview{}, errPlaybackFormat
		}
		if err := s.doc.CheckDocumentPermission(permission, scope); err != nil {
			return PlaybackPreview{}, err
		}
		return PlaybackPreview{MIME: mime, Data: data}, nil
	})
}

// AttachmentImagePreview 按导出权限读取附件图像，额外格式以静态 PNG 返回。
func (s *Service) AttachmentImagePreview(ctx context.Context, prefix string, scope int, id string) (preview.Image, error) {
	session, err := s.current(prefix)
	if err != nil {
		return preview.Image{}, err
	}
	return session.attachmentImagePreview(ctx, scope, id)
}

// MediaPlayback 按访问权限读取受限的包内音视频，支持模板独立资源视图。
func (s *Service) MediaPlayback(ctx context.Context, prefix string, scope int, id, templateID uint64) (PlaybackPreview, error) {
	session, err := s.current(prefix)
	if err != nil {
		return PlaybackPreview{}, err
	}
	return session.playbackPreview(ctx, scope, false, func() ([]byte, error) {
		if templateID == 0 {
			return session.doc.MediaData(scope, id, 32<<20)
		}
		doc, ok := session.doc.(templateMediaDocument)
		if !ok {
			return nil, errTemplatePlayback
		}
		return doc.TemplateMediaData(scope, templateID, id, 32<<20)
	})
}

// AttachmentPlayback 按导出权限读取附件中的受限音视频。
func (s *Service) AttachmentPlayback(ctx context.Context, prefix string, scope int, id string) (PlaybackPreview, error) {
	session, err := s.current(prefix)
	if err != nil {
		return PlaybackPreview{}, err
	}
	return session.playbackPreview(ctx, scope, true, func() ([]byte, error) {
		return session.doc.AttachmentData(scope, id, 32<<20)
	})
}
