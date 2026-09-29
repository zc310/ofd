package transfer

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 连接池。FTP 与 SFTP 共用：两者要解决的问题一样——服务端会在空闲一段时间后
// 关闭会话，而"每个任务重新登录"在逐页输出几百个文件时是几十秒的纯开销。
//
// 为什么是池而不是"复用同一个连接"：一个任务结束时如果直接关连接，会把同时
// 在用同一连接的其他任务一起关掉。池把"借用"与"归还"分开，任务只管借还，
// 真正地断开由池决定。
//
// 空闲连接会被服务端单方面关闭，所以取用时必须探活（见 ftpAlive/sftpAlive）。
// 探活放在**操作之前**是有原因的：一旦开始上传才发现连接已死，调用方给的
// io.Reader 已经被读掉一截，没法重放——那会导致静默的数据损坏，而不是干净的
// 错误。
type connPool[T any] struct {
	mu     sync.Mutex
	idle   []*pooledConn[T]
	closed bool

	// dial 建一条新连接。由各协议的 Sink 提供。
	dial func(context.Context) (T, error)
	// alive 探活。返回错误即认为连接已不可用，会被丢弃。
	alive func(T) error
	// release 关闭一条连接。
	//
	// 必须是**硬关**（直接关底层传输），不能走协议的优雅关闭：被丢弃的
	// 连接对端可能早已不可达，优雅关闭要等它回应，会一直挂到 deadline。
	// 实测里这会让连接池的清扫 goroutine 卡死。
	release func(T)

	// maxIdle 是空闲连接上限。超出时新归还的连接直接关掉——池是给突发用的，
	// 不是常驻连接数。0 时取 2。
	maxIdle int
	// idleTTL 是连接最多空闲多久。超过就主动关掉，不等服务端来断。
	// 0 时取 2 分钟。
	idleTTL time.Duration

	now     func() time.Time
	stopCh  chan struct{}
	stopOne sync.Once
	// sweepInterval 是清理周期。0 时取 30s。
	sweepInterval time.Duration
}

type pooledConn[T any] struct {
	conn   T
	idleAt time.Time
}

// 池的默认参数。
const (
	defaultMaxIdle       = 2
	defaultIdleTTL       = 2 * time.Minute
	defaultSweepInterval = 30 * time.Second
)

// configLocked 填默认值，必须持有 p.mu。
//
// 之前这个函数在无锁状态下改写池字段（stopCh、now 以及各项默认值），并发
// 首次取用时就是一次数据竞争——`go test -race` 直接报出来。调用方都要先
// 拿锁再调它。
func (p *connPool[T]) configLocked() {
	if p.maxIdle <= 0 {
		p.maxIdle = defaultMaxIdle
	}
	if p.idleTTL <= 0 {
		p.idleTTL = defaultIdleTTL
	}
	if p.sweepInterval <= 0 {
		p.sweepInterval = defaultSweepInterval
	}
	if p.now == nil {
		p.now = time.Now
	}
	if p.stopCh == nil {
		p.stopCh = make(chan struct{})
	}
}

// get 借一条连接：优先取空闲且仍存活的，都不行就新建。
func (p *connPool[T]) get(ctx context.Context) (T, error) {
	var zero T
	p.mu.Lock()
	p.configLocked()
	if p.closed {
		p.mu.Unlock()
		return zero, errPoolClosed
	}
	now := p.now()
	// 从后往前取：最近的入池者最可能还活着。
	var stale []T
	var picked T
	found := false
	for i := len(p.idle) - 1; i >= 0; i-- {
		item := p.idle[i]
		if now.Sub(item.idleAt) > p.idleTTL {
			stale = append(stale, item.conn)
			p.idle = append(p.idle[:i], p.idle[i+1:]...)
			continue
		}
		picked = item.conn
		found = true
		p.idle = append(p.idle[:i], p.idle[i+1:]...)
		break
	}
	p.mu.Unlock()

	// 过期的直接关掉，不在锁内做网络 IO。
	for _, c := range stale {
		p.releaseConn(c)
	}
	if found {
		if err := p.alive(picked); err == nil {
			return picked, nil
		}
		// 探活失败说明空闲期间被服务端关了。丢掉重建——这正是探活存在的理由，
		// 否则这里会是一个把数据传到一半才暴露的坏连接。
		p.releaseConn(picked)
	}
	return p.dial(ctx)
}

// put 归还一条连接。池满或已关闭时直接关掉。
func (p *connPool[T]) put(conn T) {
	p.mu.Lock()
	p.configLocked()
	if p.closed || len(p.idle) >= p.maxIdle {
		p.mu.Unlock()
		p.releaseConn(conn)
		return
	}
	p.idle = append(p.idle, &pooledConn[T]{conn: conn, idleAt: p.now()})
	p.mu.Unlock()
}

// discard 丢弃一条连接：调用方已经知道它坏了（操作失败）。
func (p *connPool[T]) discard(conn T) { p.releaseConn(conn) }

func (p *connPool[T]) releaseConn(conn T) {
	if p.release != nil {
		p.release(conn)
	}
}

// sweep 清理过期连接。返回关掉的条数。
func (p *connPool[T]) sweep() int {
	p.mu.Lock()
	p.configLocked()
	now := p.now()
	cut := 0
	for cut < len(p.idle) && now.Sub(p.idle[cut].idleAt) > p.idleTTL {
		cut++
	}
	expired := make([]T, 0, cut)
	for i := 0; i < cut; i++ {
		expired = append(expired, p.idle[i].conn)
	}
	p.idle = p.idle[cut:]
	p.mu.Unlock()
	for _, c := range expired {
		p.releaseConn(c)
	}
	return cut
}

// startSweeper 启动定时清理。幂等。
//
// 存在的理由是"空闲连接不占着进程的资源不占着服务端的槽位"：没有它，池里的
// 连接会一直挂到下次 get 才被判断过期，服务端那边的会话表也会一直占着。
func (p *connPool[T]) startSweeper() {
	p.mu.Lock()
	p.configLocked()
	interval := p.sweepInterval
	stop := p.stopCh
	p.mu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				p.sweep()
			}
		}
	}()
}

// close 关闭池并断开所有空闲连接。
func (p *connPool[T]) close() {
	p.mu.Lock()
	p.configLocked()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	idle := p.idle
	p.idle = nil
	p.mu.Unlock()
	for _, item := range idle {
		p.releaseConn(item.conn)
	}
	stop := p.stopCh
	p.stopOne.Do(func() { close(stop) })
}

// stats 返回池的当前状态，供测试与 /metrics 使用。
func (p *connPool[T]) stats() (idle int, maxIdle int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.idle), p.maxIdle
}

// errPoolClosed 表示池已关闭，不再借出连接。
var errPoolClosed = errors.New("连接池已关闭")
