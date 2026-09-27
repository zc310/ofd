package main

import (
	"sync"
	"sync/atomic"
	"testing"
)

// countingCloser 记录关闭次数，用于验证会话的延迟释放。
type countingCloser struct {
	closes atomic.Int64
}

func (c *countingCloser) Close() error {
	c.closes.Add(1)
	return nil
}

func TestDocumentSessionRetireDefersCloseWhileInUse(t *testing.T) {
	closer := &countingCloser{}
	session := &documentSession{closer: closer}

	if !session.acquire() {
		t.Fatal("未退休的会话应当允许登记使用者")
	}
	session.retire()
	if got := closer.closes.Load(); got != 0 {
		t.Fatalf("使用者未退出时的关闭次数 = %d, want 0", got)
	}
	if session.acquire() {
		t.Fatal("退休后的会话不应接受新的使用者")
	}

	session.release()
	if got := closer.closes.Load(); got != 1 {
		t.Fatalf("最后一个使用者退出后的关闭次数 = %d, want 1", got)
	}
}

func TestDocumentSessionRetireClosesIdleDocument(t *testing.T) {
	closer := &countingCloser{}
	session := &documentSession{closer: closer}

	session.retire()
	if got := closer.closes.Load(); got != 1 {
		t.Fatalf("空闲会话退休后的关闭次数 = %d, want 1", got)
	}

	session.retire()
	if got := closer.closes.Load(); got != 1 {
		t.Fatalf("重复退休的关闭次数 = %d, want 1", got)
	}
}

func TestDocumentSessionReleaseClosesOnceAfterRetire(t *testing.T) {
	closer := &countingCloser{}
	session := &documentSession{closer: closer}

	if !session.acquire() {
		t.Fatal("未退休的会话应当允许登记使用者")
	}
	if !session.acquire() {
		t.Fatal("未退休的会话应当允许登记第二个使用者")
	}
	session.retire()

	var group sync.WaitGroup
	group.Add(2)
	for range 2 {
		go func() {
			defer group.Done()
			session.release()
		}()
	}
	group.Wait()

	if got := closer.closes.Load(); got != 1 {
		t.Fatalf("并发退出后的关闭次数 = %d, want 1", got)
	}
}

func TestNewDocumentSessionRejectsNilDocument(t *testing.T) {
	session := newDocumentSession(nil)
	if session != nil {
		t.Fatal("空文档不应创建会话")
	}
	// 空会话的所有方法都必须可安全调用。
	if session.acquire() {
		t.Fatal("空会话不应允许登记使用者")
	}
	session.release()
	session.retire()
	session.close()
}

func TestDocumentSessionAcquireRetireRace(t *testing.T) {
	for range 50 {
		closer := &countingCloser{}
		session := &documentSession{closer: closer}
		acquired := make(chan bool, 1)
		go func() {
			acquired <- session.acquire()
		}()
		session.retire()
		// 登记成功意味着本次调用持有一个引用，必须归还；失败时 acquire 内部已归还。
		if <-acquired {
			session.release()
		}
		session.retire()
		if got := closer.closes.Load(); got != 1 {
			t.Fatalf("登记与退休并发时的关闭次数 = %d, want 1", got)
		}
	}
}
