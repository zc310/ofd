package session

import (
	"context"
	"errors"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

func TestSealThumbnailRechecksAccessAndDiscardsCancelledResults(t *testing.T) {
	doc := &sealPreviewDocument{data: sealPNG(t)}
	session := newSession(doc, 1)
	t.Cleanup(func() { _ = session.Close() })
	if thumbnail, err := session.SignatureSealThumbnail(t.Context(), 2, "s1", 3); err != nil || thumbnail.Width != 2 || doc.checks != 2 {
		t.Fatalf("印章缩略图 = %+v, %v", thumbnail, err)
	}
	doc.denyOn = doc.checks + 1
	reads := doc.reads
	if thumbnail, err := session.SignatureSealThumbnail(t.Context(), 2, "s1", 3); !errors.Is(err, reader.ErrPermissionDenied) || len(thumbnail.Data) != 0 || doc.reads != reads {
		t.Fatalf("读取前权限拒绝 = %v", err)
	}
	doc.denyOn = doc.checks + 2
	if thumbnail, err := session.SignatureSealThumbnail(t.Context(), 2, "s1", 3); !errors.Is(err, reader.ErrPermissionDenied) || len(thumbnail.Data) != 0 {
		t.Fatalf("缩放后权限过期 = %v", err)
	}
	doc.denyOn = 0
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	doc.afterRead = cancel
	if thumbnail, err := session.SignatureSealThumbnail(ctx, 2, "s1", 3); !errors.Is(err, context.Canceled) || len(thumbnail.Data) != 0 {
		t.Fatalf("取消后泄漏缩略图: %v", err)
	}
}

func TestMediaThumbnailKeepsResourceLimitAndOriginalDimensions(t *testing.T) {
	doc := &mediaPreviewDocument{data: sealPNG(t)}
	session := newSession(doc, 1)
	t.Cleanup(func() { _ = session.Close() })
	thumbnail, err := session.MediaThumbnail(t.Context(), 0, 1)
	if err != nil || thumbnail.Width != 2 || thumbnail.Height != 2 || doc.limit != 8<<20 {
		t.Fatalf("资源缩略图 = %+v, %v, 上限 %d", thumbnail, err, doc.limit)
	}
}

func TestThumbnailCloseWaitsForReadAndDiscardsOutput(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	doc := &sealPreviewDocument{data: sealPNG(t), afterRead: func() { close(started); <-release }}
	session := newSession(doc, 1)
	done := make(chan error, 1)
	go func() {
		thumbnail, err := session.SignatureSealThumbnail(t.Context(), 2, "s1", 3)
		if len(thumbnail.Data) != 0 {
			done <- errors.New("关闭后返回缩略图")
			return
		}
		done <- err
	}()
	<-started
	closed := make(chan error, 1)
	go func() { closed <- session.Close() }()
	<-session.ctx.Done()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("关闭期间缩略图 = %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}
