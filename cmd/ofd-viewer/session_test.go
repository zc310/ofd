package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingCloser 记录关闭次数，用于验证会话的延迟释放。
type countingCloser struct {
	closes atomic.Int64
}

// waitClosed 等待底层文档被关闭。dispose 在后台执行，计数是原子的，
// 轮询它不会与关闭协程竞争。
func waitClosed(t *testing.T, closer *countingCloser, want int64, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if closer.closes.Load() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待超时: %s（关闭次数 = %d, want %d）", what, closer.closes.Load(), want)
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
	// 释放是异步的，但必须等到使用者退出之后才发生。
	time.Sleep(20 * time.Millisecond)
	if got := closer.closes.Load(); got != 0 {
		t.Fatalf("使用者未退出时的关闭次数 = %d, want 0", got)
	}
	if session.acquire() {
		t.Fatal("退休后的会话不应接受新的使用者")
	}

	session.release()
	waitClosed(t, closer, 1, "最后一个使用者退出后应关闭文档")
}

func TestDocumentSessionRetireClosesIdleDocument(t *testing.T) {
	closer := &countingCloser{}
	session := &documentSession{closer: closer}

	session.retire()
	waitClosed(t, closer, 1, "空闲会话退休后应关闭文档")

	session.retire()
	time.Sleep(20 * time.Millisecond)
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
	waitClosed(t, closer, 1, "并发退出后应只关闭一次")
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
	session.dispose()
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
		waitClosed(t, closer, 1, "登记与退休并发时应只关闭一次")
	}
}
