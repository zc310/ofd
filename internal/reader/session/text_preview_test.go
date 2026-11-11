package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

type textPreviewDocument struct {
	blockingDocument

	data  []byte
	err   error
	limit int64
}

func (d *textPreviewDocument) AttachmentData(_ int, _ string, limit int64) ([]byte, error) {
	d.limit = limit
	return d.data, d.err
}

func TestAttachmentTextPreviewEncodingAndLimits(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
		want string
		err  error
	}{
		{"空文本", nil, "", nil},
		{"中文及标记源码", []byte("说明\n<script>alert(1)</script>\t"), "说明\n<script>alert(1)</script>\t", nil},
		{"UTF8签名", []byte("\xef\xbb\xbf中文"), "中文", nil},
		{"无效编码", []byte{0xff}, "", errTextPreviewEncoding},
		{"二进制", []byte{'a', 0}, "", errTextPreviewEncoding},
		{"控制字符", []byte("\u0085"), "", errTextPreviewEncoding},
		{"超限", []byte(strings.Repeat("a", (1<<20)+1)), "", errTextPreviewSize},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := &textPreviewDocument{data: test.data}
			session := newSession(doc, 1)
			t.Cleanup(func() { _ = session.Close() })
			got, err := session.AttachmentTextPreview(t.Context(), 0, "a")
			if !errors.Is(err, test.err) || got != test.want || doc.limit != 1<<20 {
				t.Fatalf("文本预览 = %q, %v，读取上限 %d", got, err, doc.limit)
			}
		})
	}
}

func TestAttachmentTextPreviewPermissionAndCancellation(t *testing.T) {
	doc := &textPreviewDocument{err: reader.ErrPermissionDenied}
	session := newSession(doc, 1)
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.AttachmentTextPreview(t.Context(), 0, "a"); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("权限错误 = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := session.AttachmentTextPreview(ctx, 0, "a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
}

type pendingTextPreviewDocument struct {
	blockingDocument

	started chan struct{}
	release chan struct{}
}

func (d *pendingTextPreviewDocument) AttachmentData(int, string, int64) ([]byte, error) {
	close(d.started)
	<-d.release
	return []byte("已取消的文本"), nil
}

func TestAttachmentTextPreviewDiscardsResultAfterCancellation(t *testing.T) {
	doc := &pendingTextPreviewDocument{started: make(chan struct{}), release: make(chan struct{})}
	session := newSession(doc, 1)
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		text, err := session.AttachmentTextPreview(ctx, 0, "a")
		if text != "" {
			t.Errorf("取消后仍返回文本: %q", text)
		}
		done <- err
	}()
	<-doc.started
	cancel()
	close(doc.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("读取期间取消 = %v", err)
	}
}
