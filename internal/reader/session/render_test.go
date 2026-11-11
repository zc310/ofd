package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/reader"
)

type lateRenderDocument struct {
	blockingDocument
}

// 模拟不能中断且不检查取消的底层绘制，返回成功并写入部分产物。
func (d *lateRenderDocument) RenderPage(_ context.Context, output io.Writer, _ int, _ reader.RenderOptions) error {
	d.started <- struct{}{}
	<-d.release
	_, err := io.WriteString(output, "partial")
	return err
}

func (d *lateRenderDocument) RenderPDF(ctx context.Context, output io.Writer, _ []int, options reader.RenderOptions) error {
	return d.RenderPage(ctx, output, 0, options)
}

func TestRenderRejectsLateSuccessAfterCancellationOrClose(t *testing.T) {
	for _, format := range []string{"page", "pdf"} {
		for _, closing := range []bool{false, true} {
			name := format + "/cancel"
			if closing {
				name = format + "/close"
			}
			t.Run(name, func(t *testing.T) {
				doc := &lateRenderDocument{blockingDocument: blockingDocument{started: make(chan struct{}, 1), release: make(chan struct{})}}
				s := newSession(doc, 1)
				var release sync.Once
				unblock := func() { release.Do(func() { close(doc.release) }) }
				t.Cleanup(func() { unblock(); _ = s.Close() })
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var output bytes.Buffer
				done := make(chan error, 1)
				go func() {
					if format == "pdf" {
						done <- s.RenderPDF(ctx, &output, []int{0}, reader.RenderOptions{})
						return
					}
					done <- s.RenderPage(ctx, &output, 0, reader.RenderOptions{})
				}()
				<-doc.started
				closed := make(chan error, 1)
				if closing {
					go func() { closed <- s.Close() }()
					<-s.ctx.Done()
					select {
					case err := <-closed:
						t.Fatalf("底层绘制未结束便关闭会话: %v", err)
					default:
					}
				} else {
					cancel()
				}
				unblock()
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatalf("迟到的渲染成功没有转为取消: %v", err)
				}
				if output.String() != "partial" {
					t.Fatalf("应允许调用方丢弃已经写出的部分产物: %q", output.String())
				}
				if closing {
					if err := <-closed; err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestPageAndPDFShareRenderLimit(t *testing.T) {
	for _, first := range []string{"page", "pdf"} {
		t.Run(first, func(t *testing.T) {
			doc := &blockingDocument{started: make(chan struct{}, 2), release: make(chan struct{})}
			s := newSession(doc, 1)
			var release sync.Once
			unblock := func() { release.Do(func() { close(doc.release) }) }
			t.Cleanup(func() { unblock(); _ = s.Close() })
			render := func(ctx context.Context, format string) error {
				if format == "pdf" {
					return s.RenderPDF(ctx, io.Discard, []int{0}, reader.RenderOptions{})
				}
				return s.RenderPage(ctx, io.Discard, 0, reader.RenderOptions{})
			}
			done := make(chan error, 1)
			go func() { done <- render(t.Context(), first) }()
			<-doc.started
			second := "pdf"
			if first == "pdf" {
				second = "page"
			}
			// 期限保证若两种格式错误地使用独立名额，第二个请求会进入底层绘制。
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			queued := make(chan error, 1)
			go func() { queued <- render(ctx, second) }()
			select {
			case <-doc.started:
				t.Fatal("第二种格式绕过了正在占用的渲染名额")
			case err := <-queued:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("排队超时 = %v", err)
				}
			}
			if len(s.renders) != 1 || doc.peak.Load() != 1 {
				t.Fatal("页面与 PDF 没有共用渲染名额")
			}
			unblock()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
