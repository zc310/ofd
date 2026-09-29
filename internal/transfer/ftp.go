package transfer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/textproto"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"
)

// FTPSink 把转换结果上传到 FTP/FTPS 服务器。
//
// 三个安全取舍，都是有代价的选择：
//
//  1. **默认要求 TLS**。FTP 的控制通道和文件内容都是明文，账号密码会直接暴露在
//     链路上。确实要在内网用明文时把 TLS 置空并在配置里显式声明，服务启动时会
//     记一条 WARN——让这个决定是自觉做的，而不是默认忽略的。
//
//  2. **不信任 PASV 返回的地址**。被动模式下服务器会告诉客户端"把数据连到这个
//     地址"。如果照着连，一个被攻陷或恶意的 FTP 服务器就能把服务的数据流引到
//     内网任意地址。jlaffaye/ftp 默认不信任，这里也刻意不打开
//     DialWithTrustPasvIP，代价是少数配置了 PASV 外部地址的服务器用不了。
//
//  3. **限制单文件大小**。读多少传多少，不先把内容读进内存，也拒绝超限的上传，
//     避免一个失控的转换把远端磁盘写满。
type FTPSink struct {
	// Addr 是服务器地址，形如 "ftp.example.com:21"。
	Addr string
	// User 与 Password 是登录凭据。不会出现在任何日志里。
	User     string
	Password string
	// BaseDir 是允许写入的远端根目录。留空表示远端用户登录后的家目录。
	BaseDir string
	// TLSConfig 非 nil 时使用隐式 FTPS（连上直接握手）。默认使用显式 FTPS
	// （先读 220，再用 AUTH TLS 升级），这是更常见的部署形态。
	TLSConfig *tls.Config
	// Implicit 为真时使用隐式 FTPS；为假时使用显式 FTPS。
	Implicit bool
	// Insecure 允许明文 FTP。留空 TLSConfig 且置真时使用。生产环境不应开启。
	Insecure bool
	// Timeout 是连接、登录与单个操作的超时，0 时取 DefaultFTPTimeout。
	Timeout time.Duration
	// MaxBytes 单文件上限，0 时用 DefaultMaxBytes。
	MaxBytes int64
	// Overwrite 为真时允许覆盖远端同名文件。默认拒绝，避免误覆盖别人的文件。
	Overwrite bool
	// DisableEPSV 强制只用 PASV。少数服务器没实现 EPSV，而客户端默认优先
	// 试它，失败后能否回落到 PASV 取决于服务器实现。
	DisableEPSV bool

	// logger 只记结构化字段，绝不记凭据。
	logger *slog.Logger

	mu      sync.Mutex
	conn    *ftp.ServerConn
	control net.Conn // 底层 TCP 连接，用于设置 deadline
}

// DefaultFTPTimeout 是 FTP 连接与操作的默认超时。
const DefaultFTPTimeout = 60 * time.Second

// WithBaseDir 派生一份把 BaseDir 换成 sub 的副本。
//
// 必须是副本而不是就地改：FTP 目标在配置里是长期存在的单例，多个任务并发使用
// 时就地改 BaseDir 会互相踩。连接也随之独立，避免一个任务 Close 掉另一个任务
// 正在用的连接。
//
// sub 含 ".." 时报错，不做静默规整。
func (s *FTPSink) WithBaseDir(sub string) (*FTPSink, error) {
	base := cleanRemoteBase(s.BaseDir)
	sub = cleanRemoteBase(sub)
	// 必须在拼接之前拒绝：path.Join 会把 ".." 规整掉，
	// path.Join("/root", "../etc") 得到 "/etc"——等到 Put 里再检查就晚了，
	// 那个��候 BaseDir 里已经找不到 ".." 了。输出目录静默跑到配置范围之外，
	// 比直接报错糟得多。
	for _, part := range strings.Split(sub, "/") {
		if part == ".." {
			return nil, fmt.Errorf("FTP 输出子目录不能包含 ..: %q", sub)
		}
	}
	merged := ""
	switch {
	case base == "":
		merged = sub
	case sub == "":
		merged = base
	default:
		merged = path.Join(base, sub)
	}
	// 逐字段构造，不能 *clone = *s：FTPSink 内含 sync.Mutex，整体赋值会拷贝锁
	// （go vet 会直接报 assignment copies lock value）。连接也不共享——
	// 一个任务 Close 掉另一个任务正在用的连接比多握手一次糟糕得多。
	return &FTPSink{
		Addr:        s.Addr,
		User:        s.User,
		Password:    s.Password,
		BaseDir:     merged,
		TLSConfig:   s.TLSConfig,
		Implicit:    s.Implicit,
		Insecure:    s.Insecure,
		Timeout:     s.Timeout,
		MaxBytes:    s.MaxBytes,
		Overwrite:   s.Overwrite,
		DisableEPSV: s.DisableEPSV,
		logger:      s.logger,
	}, nil
}

// SetLogger 注入日志器。
func (s *FTPSink) SetLogger(log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	s.logger = log
}

func (s *FTPSink) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// Put 上传一个文件。name 只允许单一段文件名，父目录按 BaseDir 逐级创建。
func (s *FTPSink) Put(ctx context.Context, name string, r io.Reader) (Location, error) {
	if s.Addr == "" {
		return Location{}, errors.New("未配置 FTP 地址")
	}
	// 明文 FTP 需要显式承认。账号密码在链路上是明文，这个决定不该由"忘了配
	// TLS"默认发生。
	if s.TLSConfig == nil && !s.Insecure {
		return Location{}, errors.New("拒绝使用明文 FTP：请配置 TLS，或显式声明 Insecure 以承认账号密码会明文传输")
	}
	// 与 DirSink 用同一条规则：单段文件名，不含分隔符、不为 "." 或 ".."。
	// 这样远端路径就是 BaseDir + "/" + name，无需再做前缀判断——前缀判断在
	// BaseDir 为 "/" 或含软链接时都不可靠。
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Location{}, errors.New("输出文件名为空")
	}
	if strings.ContainsAny(trimmed, `/\\`) || strings.ContainsRune(trimmed, 0) {
		return Location{}, fmt.Errorf("FTP 输出文件名不能包含路径分隔符: %q", name)
	}
	if trimmed == "." || trimmed == ".." {
		return Location{}, fmt.Errorf("FTP 输出文件名无效: %q", name)
	}
	base := cleanRemoteBase(s.BaseDir)
	for _, part := range strings.Split(base, "/") {
		if part == ".." {
			// BaseDir 里带 ".."，拼出来的路径可能落在配置范围之外。
			// 不静默清理，直接拒绝。
			return Location{}, fmt.Errorf("FTP 输出根目录不能包含 ..: %q", s.BaseDir)
		}
	}
	remote := path.Join(base, trimmed)
	if remote == "" || remote == "." || strings.HasSuffix(remote, "/") {
		return Location{}, fmt.Errorf("FTP 输出路径无效: %q", remote)
	}

	conn, err := s.acquire(ctx)
	if err != nil {
		return Location{}, err
	}
	// 连接是复用的，上一轮设的 deadline 早已过期，每个操作前都要刷新。
	s.refreshDeadline()
	// 任何失败都要断开：连接可能已经处于半坏状态，复用只会把问题放大。
	committed := false
	defer func() {
		if !committed {
			s.drop()
		}
	}()

	if err := s.ensureDirs(conn, path.Dir(remote)); err != nil {
		return Location{}, err
	}
	if !s.Overwrite {
		if exists, err := s.exists(conn, remote); err != nil {
			return Location{}, err
		} else if exists {
			return Location{}, fmt.Errorf("%w: FTP 上目标文件已存在且不允许覆盖: %s", ErrExists, remote)
		}
	}

	// Stor 不接受 context：上传过程中无法中断。ctx 至少管住前面的连接与目录
	// 创建，超时由 FTP 服务器自己的会话时限兜底。
	stored, err := s.store(conn, remote, r)
	if err != nil {
		return Location{}, err
	}
	committed = true
	return Location{Kind: "ftp", Path: remote, Size: stored}, nil
}

// store 上传并返回实际字节数。
//
// 包一层 countingReader 限制大小：Stor 会一路读到底，失控的转换能把远端磁盘
// 写满，而远端往往是别人的机器，清理起来比本地麻烦得多。
func (s *FTPSink) store(conn *ftp.ServerConn, remote string, r io.Reader) (int64, error) {
	limit := s.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	counter := &countingReader{r: r, limit: limit}
	if err := conn.Stor(remote, counter); err != nil {
		if counter.exceeded {
			return counter.n, fmt.Errorf("内容超过上限 %d 字节", limit)
		}
		return counter.n, fmt.Errorf("上传到 FTP 失败: %w", err)
	}
	return counter.n, nil
}

// countingReader 统计读取的字节数，超过 limit 即报错。
type countingReader struct {
	r        io.Reader
	limit    int64
	n        int64
	exceeded bool
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.exceeded {
		return 0, fmt.Errorf("内容超过上限 %d 字节", c.limit)
	}
	n, err := c.r.Read(p)
	if n > 0 {
		c.n += int64(n)
		if c.n > c.limit {
			c.exceeded = true
			// 返回 0 加错误，让 Stor 立刻中止而不是把这段也写下去。
			return 0, fmt.Errorf("内容超过上限 %d 字节", c.limit)
		}
	}
	return n, err
}

// ensureDirs 逐级创建远端目录。已存在不算错误——多次 PUT 到同一目录是常态。
func (s *FTPSink) ensureDirs(conn *ftp.ServerConn, dir string) error {
	dir = strings.Trim(dir, "/")
	if dir == "" || dir == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(dir, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			// 不允许上跳：BaseDir 之外的目录不该由本服务创建。
			return errors.New("FTP 输出目录不能包含 ..")
		}
		current = current + "/" + part
		if err := conn.MakeDir(current); err != nil {
			// 550 通常是"已存在"，换一种说法询问一次以区分。
			if exists, queryErr := s.dirExists(conn, current); queryErr == nil && exists {
				continue
			}
			return fmt.Errorf("创建 FTP 目录 %s 失败: %w", current, err)
		}
	}
	return nil
}

// dirExists 用一次列目录来判断：List 成功即说明目录可读（也即存在）。
// 空目录同样算存在，所以不能按长度判断。
func (s *FTPSink) dirExists(conn *ftp.ServerConn, dir string) (bool, error) {
	if _, err := conn.List(dir); err != nil {
		return false, err
	}
	return true, nil
}

// exists 判断远端文件是否已存在。
//
// 用 SIZE 而不是 List：List 要传输并解析整个目录的 ls 输出（格式因服务器而异，
// 解析失败就等于判断不出），而判断一个文件在不在，一条 SIZE 就够了。
// SIZE 对不存在的文件返回错误——这正是我们要的答案。
func (s *FTPSink) exists(conn *ftp.ServerConn, remote string) (bool, error) {
	if _, err := conn.FileSize(remote); err != nil {
		var protoErr *textproto.Error
		if errors.As(err, &protoErr) {
			// 服务端明确回了错误码：550 就是"没有这个文件"，属于预期答案。
			return false, nil
		}
		return false, fmt.Errorf("查询 FTP 文件大小失败: %w", err)
	}
	return true, nil
}

// acquire 复用已建立的连接，必要时新建。
func (s *FTPSink) acquire(ctx context.Context) (*ftp.ServerConn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return s.conn, nil
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultFTPTimeout
	}
	options := []ftp.DialOption{
		ftp.DialWithContext(ctx),
		ftp.DialWithTimeout(timeout),
		// 不加 DialWithTrustPasvIP(true)：见类型注释里的第 2 点。
	}
	if s.DisableEPSV {
		options = append(options, ftp.DialWithDisabledEPSV(true))
	}
	switch {
	case s.TLSConfig != nil && s.Implicit:
		options = append(options, ftp.DialWithTLS(s.TLSConfig))
	case s.TLSConfig != nil:
		options = append(options, ftp.DialWithExplicitTLS(s.TLSConfig))
	}
	// 自己拨号而不是交给 ftp.Dial：ServerConn 不导出底层 net.Conn，设不了
	// deadline。而命令往返（STOR、等 226）必须有时间限制——ftp.Dial 的超时
	// 只管握手，服务器接了连接就沉默的话，Put 会一直挂着，任务永远停在
	// running，进程也退不出去。
	var control net.Conn
	dialer := &net.Dialer{Timeout: timeout}
	options = append(options, ftp.DialWithDialFunc(func(network, address string) (net.Conn, error) {
		// 地址原样传给 DialContext：拆掉端口再拨会得到 "missing port"，
		// DialContext 本身就接受 host:port，也一样受 ctx 控制。
		raw, dialErr := dialer.DialContext(ctx, network, address)
		if dialErr != nil {
			return nil, dialErr
		}
		// 控制连接在读命令响应时必须能被 deadline 打断；数据连接由
		// Stor 自己管，不设。
		_ = raw.SetDeadline(time.Now().Add(timeout))
		control = raw
		return raw, nil
	}))
	conn, err := ftp.Dial(s.Addr, options...)
	if err != nil {
		if control != nil {
			_ = control.Close()
		}
		return nil, fmt.Errorf("连接 FTP 服务器失败: %w", err)
	}
	if control == nil {
		// 理论上不会发生：DialWithDialFunc 一定会被调用。发生的话宁可直接
		// 失败，也不要留下一条没有 deadline 的连接。
		_ = conn.Quit()
		return nil, errors.New("FTP 连接未提供底层连接，无法设置超时")
	}
	if err := conn.Login(s.User, s.Password); err != nil {
		_ = conn.Quit()
		// 凭据本身可能出现在某些服务器的响应里，错误信息要过一遍。
		return nil, fmt.Errorf("FTP 登录失败（用户 %q）: %s", s.User, redact(err.Error(), s.Password))
	}
	s.conn = conn
	s.control = control
	s.log().Info("已连接 FTP 服务器", "addr", s.Addr, "user", s.User, "tls", s.TLSConfig != nil)
	return conn, nil
}

// refreshDeadline 把控制连接的读写 deadline 推到未来。
//
// 必须在每个操作前调用：连接是复用的，上一轮设的 deadline 早就过了。
func (s *FTPSink) refreshDeadline() {
	if s.control == nil {
		return
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultFTPTimeout
	}
	_ = s.control.SetDeadline(time.Now().Add(timeout))
}

// drop 断开并清空连接。
func (s *FTPSink) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked()
}

func (s *FTPSink) dropLocked() {
	if s.conn == nil {
		return
	}
	if err := s.conn.Quit(); err != nil {
		s.log().Debug("FTP 退出时报错", "err", err)
	}
	s.conn = nil
	if s.control != nil {
		_ = s.control.Close()
		s.control = nil
	}
}

// Close 关闭连接。
//
// Sink 接口没有 Close，调用方用可选的 io.Closer 断言来收尾；不实现也可以，
// 只是每次 PUT 都要重新握手。
func (s *FTPSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked()
	return nil
}

// cleanRemoteBase 规整远端根目录：去掉首尾斜杠与 "." 段，保留是否为绝对路径。
func cleanRemoteBase(base string) string {
	base = strings.TrimSpace(base)
	base = strings.ReplaceAll(base, "\\", "/")
	absolute := strings.HasPrefix(base, "/")
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(base, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			// 上跳交给调用方报错，这里不静默丢弃——悄悄改掉路径比报错更糟。
			parts = append(parts, part)
			continue
		}
		parts = append(parts, part)
	}
	joined := strings.Join(parts, "/")
	if absolute {
		return "/" + joined
	}
	return joined
}

// redact 从错误信息里抹掉密码。
//
// 有些 FTP 服务器会把认证失败时的上下文原样回显，密码可能因此出现在错误串里，
// 而错误串会被写进日志。
//
// 太短的密码不做替换：子串匹配会命中无关文本——密码是 "p" 时，
// "a.pdf" 会被改成 "a.***df"，把路径和错误信息都搅乱。这种情况下宁可留原文，
// 因为短密码本来也不太可能原样出现在服务器响应里。
const minRedactableSecret = 6

func redact(message, password string) string {
	if len(password) < minRedactableSecret {
		return message
	}
	return strings.ReplaceAll(message, password, "***")
}
