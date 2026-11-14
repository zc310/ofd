package main

import (
	"sync"

	"github.com/zc310/ofd/pkg/crypt"
)

// continuousSession 将文档关闭延迟到所有页面重放租约归还之后。
// 退役不等待关闭，避免在事件线程阻塞；文档访问仍须由调用者串行化。
type continuousSession struct {
	mu      sync.Mutex
	refs    int
	retired bool
	close   func()
	view    *crypt.View
	input   []byte
}

type continuousLease struct {
	once    sync.Once
	session *continuousSession
}

func (s *continuousSession) acquire() *continuousLease {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired {
		return nil
	}
	s.refs++
	return &continuousLease{session: s}
}

func (s *continuousSession) retire() {
	s.mu.Lock()
	if s.retired {
		s.mu.Unlock()
		return
	}
	s.retired = true
	closeNow := s.refs == 0
	closeFn := s.close
	s.mu.Unlock()
	if closeNow && closeFn != nil {
		go closeFn()
	}
}

func (l *continuousLease) release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		s := l.session
		s.mu.Lock()
		s.refs--
		closeNow := s.retired && s.refs == 0
		closeFn := s.close
		s.mu.Unlock()
		if closeNow && closeFn != nil {
			go closeFn()
		}
	})
}
