// Package session 管理阅读器的文档生命周期和有界页面渲染。
package session

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/zc310/ofd/pkg/reader"
)

var ErrClosed = errors.New("阅读会话已经关闭")
var errInvalidRenderLimit = errors.New("并发渲染数必须在 0 到 32 之间")

// Options 配置单个文档会话。MaxConcurrentRenders 为零时使用默认值，最大为 32。
type Options struct {
	Reader               reader.OpenOptions
	MaxConcurrentRenders int
}

// Session 持有一个文档，关闭时取消渲染并等待所有操作退出。
type Session struct {
	mu           sync.Mutex
	closed       bool
	doc          document
	active       sync.WaitGroup
	done         chan struct{}
	closeErr     error
	ctx          context.Context //nolint:containedctx // 会话关闭信号独立于任何单次请求。
	cancel       context.CancelFunc
	renders      chan struct{}
	searches     chan struct{}
	pageMetadata chan struct{}
	listMetadata chan struct{}
}

// Open 打开普通文档或已解锁的只读视图。input 的支持类型和资源所有权与 reader.Open 相同。
func Open(input any, options Options) (*Session, error) {
	limit := options.MaxConcurrentRenders
	if limit < 0 || limit > 32 {
		return nil, errInvalidRenderLimit
	}
	if limit == 0 {
		limit = min(runtime.GOMAXPROCS(0), 4)
	}
	doc, err := reader.Open(input, options.Reader)
	if err != nil {
		return nil, fmt.Errorf("打开阅读会话: %w", err)
	}
	return newSession(doc, limit), nil
}

func newSession(doc document, limit int) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{doc: doc, done: make(chan struct{}), ctx: ctx, cancel: cancel,
		renders: make(chan struct{}, limit), searches: make(chan struct{}, 1), pageMetadata: make(chan struct{}, 2), listMetadata: make(chan struct{}, 2)}
}

func (s *Session) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.active.Add(1)
	return nil
}

// Close 取消进行中的渲染，等待操作退出并释放文档；可并发和重复调用。
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		<-s.done
		return s.closeErr
	}
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.active.Wait()
	s.closeErr = s.doc.Close()
	close(s.done)
	return s.closeErr
}
