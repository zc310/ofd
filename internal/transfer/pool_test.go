package transfer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeConn 是池测试用的假连接。
type fakeConn struct {
	id     int
	closed atomic.Int32
}

func (c *fakeConn) Close() {
	c.closed.Add(1)
}

func newTestPool(t *testing.T, maxIdle int, idleTTL time.Duration) (*connPool[*fakeConn], *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var dials, closes atomic.Int32
	p := &connPool[*fakeConn]{
		dial: func(context.Context) (*fakeConn, error) {
			dials.Add(1)
			return &fakeConn{id: int(dials.Load())}, nil
		},
		alive:   func(*fakeConn) error { return nil },
		release: func(c *fakeConn) { closes.Add(1); c.Close() },
		maxIdle: maxIdle,
		idleTTL: idleTTL,
	}
	return p, &dials, &closes
}

// TestPoolReusesReturnedConnection 归还的连接会被下一个人拿到。
func TestPoolReusesReturnedConnection(t *testing.T) {
	p, dials, _ := newTestPool(t, 2, time.Minute)
	defer p.close()

	c1, err := p.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.put(c1)
	c2, err := p.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c1 != c2 {
		t.Errorf("第二次取用应拿到同一条连接")
	}
	if dials.Load() != 1 {
		t.Errorf("拨号次数 = %d，期望 1（应复用）", dials.Load())
	}
}

// TestPoolDialsWhenEmpty 空池时新建。
func TestPoolDialsWhenEmpty(t *testing.T) {
	p, dials, _ := newTestPool(t, 2, time.Minute)
	defer p.close()
	if _, err := p.get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 2 {
		t.Errorf("拨号次数 = %d，期望 2（没有可复用的就新建）", dials.Load())
	}
}

// TestPoolDropsDeadConnectionOnGet 探活失败的连接不能交出去。
//
// 这是池最关键的安全性质：空闲久了的服务端会话已经断了，若照发，调用方会在
// 写到一半时才遇到坏连接——那时 io.Reader 已经被读掉一截，没法重放。
func TestPoolDropsDeadConnectionOnGet(t *testing.T) {
	p, dials, closes := newTestPool(t, 2, time.Minute)
	defer p.close()

	dead := &fakeConn{id: 99}
	p.alive = func(c *fakeConn) error {
		if c == dead {
			return errors.New("连接已失效")
		}
		return nil
	}
	p.put(dead)

	got, err := p.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == dead {
		t.Fatal("探活失败的连接被交出去了")
	}
	if dead.closed.Load() != 1 {
		t.Error("失效的连接应被关闭")
	}
	if dials.Load() != 1 {
		t.Errorf("拨号次数 = %d，期望 1（应重建）", dials.Load())
	}
	_ = closes
}

// TestPoolExpiryDropsIdle 超时未用的连接被丢弃。
func TestPoolExpiryDropsIdle(t *testing.T) {
	p, _, closes := newTestPool(t, 2, 30*time.Millisecond)
	defer p.close()

	p.put(&fakeConn{id: 1})
	if idle, _ := p.stats(); idle != 1 {
		t.Fatalf("归还后空闲数 = %d，期望 1", idle)
	}
	time.Sleep(60 * time.Millisecond)
	if n := p.sweep(); n != 1 {
		t.Errorf("清理条数 = %d，期望 1", n)
	}
	if closes.Load() != 1 {
		t.Errorf("关闭次数 = %d，期望 1", closes.Load())
	}
	if idle, _ := p.stats(); idle != 0 {
		t.Errorf("清理后空闲数 = %d，期望 0", idle)
	}
}

// TestPoolExpiryCheckedOnGet 也取用时判过期，不依赖清扫器跑到。
func TestPoolExpiryCheckedOnGet(t *testing.T) {
	p, dials, _ := newTestPool(t, 2, 30*time.Millisecond)
	defer p.close()

	p.put(&fakeConn{id: 1})
	time.Sleep(60 * time.Millisecond)
	if _, err := p.get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 1 {
		t.Errorf("拨号次数 = %d，期望 1（过期的应被丢弃重建）", dials.Load())
	}
}

// TestPoolMaxIdle 上限之外直接关掉，不进池。
func TestPoolMaxIdle(t *testing.T) {
	p, _, closes := newTestPool(t, 2, time.Minute)
	defer p.close()

	for i := 0; i < 5; i++ {
		p.put(&fakeConn{id: i})
	}
	if idle, max := p.stats(); idle != 2 || max != 2 {
		t.Errorf("空闲数 = %d，上限 = %d，期望 2/2", idle, max)
	}
	// 5 条归还、池里留 2 条，其余 3 条当场关掉。
	if closes.Load() != 3 {
		t.Errorf("关闭次数 = %d，期望 3（超出上限的直接关）", closes.Load())
	}
}

// TestPoolCloseDiscardsIdle close 之后所有空闲连接被断开。
func TestPoolCloseDiscardsIdle(t *testing.T) {
	p, _, closes := newTestPool(t, 4, time.Minute)
	p.put(&fakeConn{id: 1})
	p.put(&fakeConn{id: 2})
	p.close()
	if closes.Load() != 2 {
		t.Errorf("关闭次数 = %d，期望 2", closes.Load())
	}
	// 关闭后再取用要失败，而不是拿到一条已断的连接。
	if _, err := p.get(context.Background()); !errors.Is(err, errPoolClosed) {
		t.Errorf("关闭后取用返回 %v，期望 errPoolClosed", err)
	}
	// 重复关闭要幂等。
	p.close()
}

// TestPoolConcurrentGetPut 并发取还不会把同一条连接借给两个人。
func TestPoolConcurrentGetPut(t *testing.T) {
	p, _, _ := newTestPool(t, 8, time.Minute)
	defer p.close()

	var mu sync.Mutex
	inUse := map[*fakeConn]bool{}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := p.get(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if inUse[c] {
				t.Errorf("连接 %d 同时借给了两个 goroutine", c.id)
			}
			inUse[c] = true
			mu.Unlock()

			time.Sleep(time.Millisecond)

			mu.Lock()
			delete(inUse, c)
			mu.Unlock()
			p.put(c)
		}()
	}
	wg.Wait()
}

// TestPoolSweeperRuns 清扫器会自己跑，不依赖有人调用 get。
func TestPoolSweeperRuns(t *testing.T) {
	p, _, closes := newTestPool(t, 2, 20*time.Millisecond)
	p.sweepInterval = 10 * time.Millisecond
	defer p.close()
	p.startSweeper()
	p.put(&fakeConn{id: 1})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if closes.Load() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("清扫器没有在超时前清掉过期的空闲连接")
}
