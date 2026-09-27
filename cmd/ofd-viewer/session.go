package main

import (
	"io"
	"sync/atomic"

	"github.com/zc310/ofd/internal/parser"
)

// documentSession 持有已解析的 OFD 文档，并跟踪正在使用它的导出任务。
//
// 页面按需渲染不需要会话保护：internal/parser 的页面租约会让文档关闭等待
// 在途渲染结束，新的渲染在关闭后直接返回错误。导出不同，页面内容在租约之外
// 通过 render.VectorSurface 被访问，因此导出必须显式持有会话引用。
//
// 关闭文档只标记为待释放，等最后一个使用者退出后再真正关闭底层文档，
// 这样打开或关闭文档不会阻塞 Fyne 事件循环。
type documentSession struct {
	closer  io.Closer
	users   atomic.Int64
	retired atomic.Bool
	closed  atomic.Bool
}

func newDocumentSession(ofd *parser.OFD) *documentSession {
	if ofd == nil {
		return nil
	}
	return &documentSession{closer: ofd}
}

// acquire 登记一个使用者；文档已标记关闭时返回 false。
func (s *documentSession) acquire() bool {
	if s == nil || s.retired.Load() {
		return false
	}
	s.users.Add(1)
	// retire 可能刚好在两次检查之间发生，此时本次使用不再安全。
	if s.retired.Load() {
		s.release()
		return false
	}
	return true
}

// release 注销一个使用者；最后一个使用者退出时释放已标记关闭的文档。
func (s *documentSession) release() {
	if s == nil {
		return
	}
	if s.users.Add(-1) == 0 && s.retired.Load() {
		s.close()
	}
}

// retire 标记文档不再接受新的使用者，并在没有使用者时立即释放。
func (s *documentSession) retire() {
	if s == nil {
		return
	}
	if s.retired.CompareAndSwap(false, true) && s.users.Load() == 0 {
		s.close()
	}
}

func (s *documentSession) close() {
	if s == nil || !s.closed.CompareAndSwap(false, true) {
		return
	}
	if s.closer != nil {
		_ = s.closer.Close()
	}
}
