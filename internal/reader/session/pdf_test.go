package session

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

func TestPDFRenderPreservesCancellationDuringNativeWork(t *testing.T) {
	doc := &blockingDocument{started: make(chan struct{}, 1), release: make(chan struct{}), canceled: make(chan struct{})}
	session := newSession(doc, 1)
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- session.RenderPDF(ctx, io.Discard, []int{0}, reader.RenderOptions{}) }()
	<-doc.started
	cancel()
	<-doc.canceled
	close(doc.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("PDF 取消错误 = %v", err)
	}
}
