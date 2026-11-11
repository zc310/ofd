package session

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
)

type resourcePreviewDocument struct {
	blockingDocument

	data      []byte
	checks    []reader.PermissionAction
	deny      reader.PermissionAction
	reads     int
	limit     int64
	template  uint64
	afterRead func()
}

func (d *resourcePreviewDocument) CheckDocumentPermission(action reader.PermissionAction, _ int) error {
	d.checks = append(d.checks, action)
	if d.deny == action {
		return reader.ErrPermissionDenied
	}
	return nil
}
func (d *resourcePreviewDocument) MediaData(_ int, _ uint64, limit int64) ([]byte, error) {
	d.reads++
	d.limit = limit
	if d.afterRead != nil {
		d.afterRead()
	}
	return d.data, nil
}
func (d *resourcePreviewDocument) TemplateMediaData(scope int, template, id uint64, limit int64) ([]byte, error) {
	d.template = template
	return d.MediaData(scope, id, limit)
}
func (d *resourcePreviewDocument) AttachmentData(scope int, _ string, limit int64) ([]byte, error) {
	return d.MediaData(scope, 0, limit)
}
func previewService(t *testing.T, doc *resourcePreviewDocument) (*Service, string) {
	t.Helper()
	session := newSession(doc, 1)
	handler, err := NewPageHandler(session)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{session: session, pages: handler}
	t.Cleanup(func() { _ = service.Close() })
	return service, handler.Prefix()
}

func TestAttachmentImageChecksExportBeforeAndAfterReadAndRejectsStaleSession(t *testing.T) {
	doc := &resourcePreviewDocument{data: sealPNG(t), deny: reader.PermissionExport}
	service, prefix := previewService(t, doc)
	if _, err := service.AttachmentImagePreview(t.Context(), prefix, 0, "a"); !errors.Is(err, reader.ErrPermissionDenied) || doc.reads != 0 {
		t.Fatalf("读取前导出检查 = %v", err)
	}
	doc.deny = ""
	doc.afterRead = func() { doc.deny = reader.PermissionExport }
	if _, err := service.AttachmentImagePreview(t.Context(), prefix, 0, "a"); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("读取后导出检查 = %v", err)
	}
	doc.deny, doc.afterRead = "", nil
	if p, err := service.AttachmentImagePreview(t.Context(), prefix, 0, "a"); err != nil || p.MIME != "image/png" || doc.limit != 8<<20 {
		t.Fatalf("附件图片 = %s, %v", p.MIME, err)
	}
	if err := service.CloseCurrent(prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AttachmentImagePreview(t.Context(), prefix, 0, "a"); !errors.Is(err, ErrStaleDocument) {
		t.Fatal(err)
	}
}

func TestPlaybackUsesContentWhitelistTemplateScopeAndAccessNotExport(t *testing.T) {
	for name, data := range map[string][]byte{
		"MP3":         []byte("ID3\x04\x00\x00\x00\x00\x00\x00"),
		"WAV":         []byte("RIFF\x24\x00\x00\x00WAVEfmt "),
		"Ogg":         []byte("OggS\x00\x02\x00"),
		"WebM":        {0x1a, 0x45, 0xdf, 0xa3, 0x9f},
		"MP4":         []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isommp42"),
		"M4A":         []byte("\x00\x00\x00\x10ftypM4A \x00\x00\x00\x00"),
		"无 ID3 的 MP3": {0xff, 0xfb, 0x90, 0x64},
	} {
		t.Run(name, func(t *testing.T) {
			doc := &resourcePreviewDocument{data: data, deny: reader.PermissionExport}
			service, prefix := previewService(t, doc)
			preview, err := service.MediaPlayback(t.Context(), prefix, 2, 7, 3)
			if err != nil || !bytes.Equal(preview.Data, data) || doc.template != 3 || doc.limit != 32<<20 || len(doc.checks) != 2 {
				t.Fatalf("模板播放 = %s, %v", preview.MIME, err)
			}
			if _, err := service.AttachmentPlayback(t.Context(), prefix, 2, "a"); !errors.Is(err, reader.ErrPermissionDenied) || doc.reads != 1 {
				t.Fatalf("附件不能借播放绕过导出: %v", err)
			}
		})
	}
}

func TestPlaybackRejectsPlaylistsOversizeExpiredAndCancelledResults(t *testing.T) {
	doc := &resourcePreviewDocument{}
	service, prefix := previewService(t, doc)
	for _, data := range [][]byte{[]byte("#EXTM3U\nhttps://example.test/media.mp3"), []byte("<html>mp3</html>"), []byte("https://example.test"), make([]byte, (32<<20)+1)} {
		doc.data = data
		if preview, err := service.MediaPlayback(t.Context(), prefix, 0, 1, 0); err == nil || len(preview.Data) != 0 {
			t.Fatal("无效或超限媒体未拒绝")
		}
	}
	doc.data = []byte("ID3\x04\x00\x00\x00\x00\x00\x00")
	doc.afterRead = func() { doc.deny = reader.PermissionAccess }
	if _, err := service.MediaPlayback(t.Context(), prefix, 0, 1, 0); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("读取后权限失效: %v", err)
	}
	doc.deny = ""
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	doc.afterRead = cancel
	if _, err := service.MediaPlayback(ctx, prefix, 0, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消不保真: %v", err)
	}
	if err := service.CloseCurrent(prefix); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MediaPlayback(t.Context(), prefix, 0, 1, 0); !errors.Is(err, ErrStaleDocument) {
		t.Fatal(err)
	}
	if _, err := service.AttachmentPlayback(t.Context(), prefix, 0, "a"); !errors.Is(err, ErrStaleDocument) {
		t.Fatal(err)
	}
}

type blockedResourcePreviewDocument struct {
	blockingDocument

	started chan struct{}
	release chan struct{}
	reads   atomic.Int32
	picture []byte
}

func (d *blockedResourcePreviewDocument) MediaData(int, uint64, int64) ([]byte, error) {
	d.reads.Add(1)
	d.started <- struct{}{}
	<-d.release
	return []byte("ID3\x04\x00\x00\x00\x00\x00\x00"), nil
}

func (d *blockedResourcePreviewDocument) AttachmentData(int, string, int64) ([]byte, error) {
	d.reads.Add(1)
	d.started <- struct{}{}
	<-d.release
	return d.picture, nil
}

func TestReplacingSessionCancelsQueuedPreviewsDiscardsActiveResultsAndWaitsForRelease(t *testing.T) {
	doc := &blockedResourcePreviewDocument{started: make(chan struct{}, 16), release: make(chan struct{}), picture: sealPNG(t)}
	var release sync.Once
	unblock := func() { release.Do(func() { close(doc.release) }) }
	t.Cleanup(unblock)
	session := newSession(doc, 1)
	handler, err := NewPageHandler(session)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{session: session, pages: handler}
	t.Cleanup(func() { unblock(); _ = service.Close() })
	prefix := handler.Prefix()
	results := make(chan error, 10)
	playback := func() {
		preview, err := service.MediaPlayback(t.Context(), prefix, 0, 1, 0)
		if len(preview.Data) != 0 {
			results <- errors.New("会话替换后仍发布音视频字节")
			return
		}
		results <- err
	}
	picture := func() {
		preview, err := service.AttachmentImagePreview(t.Context(), prefix, 0, "image")
		if len(preview.Data) != 0 {
			results <- errors.New("会话替换后仍发布附件图片")
			return
		}
		results <- err
	}
	go playback()
	go picture()
	<-doc.started
	<-doc.started
	for range 4 {
		go playback()
		go picture()
	}
	data, err := creator.Marshal(creator.Document{ID: "replacement-preview", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan error, 1)
	go func() { _, err := service.Open(data, Options{}); opened <- err }()
	<-session.ctx.Done()
	if _, err := service.MediaPlayback(t.Context(), prefix, 0, 1, 0); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("等待旧读取期间没有撤销旧地址: %v", err)
	}
	select {
	case err := <-opened:
		t.Fatalf("旧读取尚未退出时已发布新会话: %v", err)
	default:
	}
	if doc.closed.Load() != 0 {
		t.Fatal("读取尚未完成就释放底层文档")
	}
	unblock()
	if err := <-opened; err != nil {
		t.Fatal(err)
	}
	for range 10 {
		err := <-results
		if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClosed) && !errors.Is(err, ErrStaleDocument) {
			t.Errorf("旧预览未丢弃结果或取消错误不保真: %v", err)
		}
	}
	if doc.reads.Load() != 2 || doc.closed.Load() != 1 {
		t.Fatalf("排队预览不应启动，底层文档必须只释放一次: reads=%d closed=%d", doc.reads.Load(), doc.closed.Load())
	}
}
