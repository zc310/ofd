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
	// （先读 220，再用 AUTH TLS 升级），这是更常见地部署形态。
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

	// pool 是连接池，由注册目标持有、派生的子目录实例共享。
	//
	// 共享而不是各自持有，是因为登录一次（TLS 握手 + 认证往返）在远端可能
	// 几百毫秒；逐页输出几百个文件时，逐任务重连的代价不可接受。
	pool   *connPool[*ftpConn]
	poolMu sync.Mutex
	// OwnsPool 区分注册目标与 WithBaseDir 派生的实例。只有前者关池。
	//
	// 外部（cmd/ofd-server）从配置构造注册目标时置 true；派生实例不带它。
	OwnsPool bool
	// dirs 记住已建过的远端目录，避免逐页输出时每个文件都重跑一遍建目录。
	// WithBaseDir 逐字段构造新实例，所以派生实例天然是空记忆。
	dirs madeDirs

	// MaxIdle 与 IdleTTL 覆盖池的默认参数，0 表示用默认值。
	//
	// 这个结构体不参与序列化（配置类型在 cmd/ofd-server），所以不带 json 标签。
	MaxIdle int
	IdleTTL time.Duration
}

func (s *FTPSink) MaxBytesLimit() int64 {
	if s.MaxBytes <= 0 {
		return DefaultMaxBytes
	}
	return s.MaxBytes
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
	// 那个时候 BaseDir 里已经找不到 ".." 了。输出目录静默跑到配置范围之外，
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
	// 必须先让父对象的池建好再复制指针：父的池是惰性初始化的，若此刻还是
	// nil，派生实例会拿到 nil 并各自建一个自己的池——共享就消失了，而
	// "每个任务一条新连接"正是连接池要解决的问题。
	s.ensurePool()
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
		pool:        s.pool,
		MaxIdle:     s.MaxIdle,
		IdleTTL:     s.IdleTTL,
		// ownsPool 保持 false：派生实例不负责关闭共享池。
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
	// 任何失败都丢弃连接：它可能已处于半坏状态，放回池里只会让下一个任务在
	// 写到一半时才暴露问题。
	committed := false
	defer func() { s.release(conn, committed) }()

	if err := s.ensureDirs(conn.server, path.Dir(remote)); err != nil {
		return Location{}, err
	}
	if !s.Overwrite {
		if exists, err := s.exists(conn.server, remote); err != nil {
			return Location{}, err
		} else if exists {
			return Location{}, fmt.Errorf("%w: FTP 上目标文件已存在且不允许覆盖: %s", ErrExists, remote)
		}
	}

	// Stor 不接受 context：上传过程中无法中断。ctx 至少管住前面的连接与目录
	// 创建，超时由 FTP 服务器自己的会话时限兜底。
	stored, err := s.store(conn.server, remote, r)
	if err != nil {
		// 上传失败可能是"目录在我们建完之后又被删了"，此时目录记忆已经不可信。
		// 清掉它，下次会重新走一遍建目录流程——这比让同一个任务里后续所有
		// 文件都撞同一个错要好。
		s.dirs.forgetAll()
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
		// 逐级记忆而不是记整条路径：多页输出常是 4 级目录、200 个文件，
		// 每次都跑一遍就是 800 次 MakeDir（已存在时还要再列一次目录区分 550），
		// 全是浪费的往返。
		if s.dirs.has(current) {
			continue
		}
		if err := conn.MakeDir(current); err != nil {
			// 550 通常是"已存在"，换一种说法询问一次以区分。
			if exists, queryErr := s.dirExists(conn, current); queryErr == nil && exists {
				s.dirs.add(current)
				continue
			}
			return fmt.Errorf("创建 FTP 目录 %s 失败: %w", current, err)
		}
		s.dirs.add(current)
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
// acquire 从池里取一条可用连接。
func (s *FTPSink) acquire(ctx context.Context) (*ftpConn, error) {
	pool := s.ensurePool()
	conn, err := pool.get(ctx)
	if err != nil {
		return nil, err
	}
	s.refreshDeadline(conn)
	return conn, nil
}

// ensurePool 惰性建池。
func (s *FTPSink) ensurePool() *connPool[*ftpConn] {
	if s.pool != nil {
		return s.pool
	}
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	if s.pool == nil {
		s.pool = &connPool[*ftpConn]{
			dial:    s.dial,
			alive:   ftpAlive,
			release: ftpConnClose,
			maxIdle: s.MaxIdle,
			idleTTL: s.IdleTTL,
		}
		s.pool.startSweeper()
	}
	return s.pool
}

// dial 建立一条新连接：自己拨号而不是 ftp.Dial，原因见 dialer 那段注释。
func (s *FTPSink) dial(ctx context.Context) (*ftpConn, error) {
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
	s.log().Info("已连接 FTP 服务器", "addr", s.Addr, "user", s.User, "tls", s.TLSConfig != nil)
	return &ftpConn{server: conn, control: control}, nil
}

// ftpConn 是一条 FTP 连接及其底层 TCP 连接。
//
// 两者必须成对：ServerConn 不导出底层连接，设不了 deadline。
type ftpConn struct {
	server  *ftp.ServerConn
	control net.Conn
}

// ftpAlive 探活。NOOP 只走控制连接，一次往返。
//
// 池里的连接空闲久了会被服务端单方面关闭，而这个检查必须发生在上传**之前**：
// 一旦开始写数据才发现连接已死，调用方给的 io.Reader 已经被读掉一截，没法重放
// ——那会变成静默的数据损坏，而不是一个干净的错误。
func ftpAlive(c *ftpConn) error {
	return c.server.NoOp()
}

// ftpConnClose 硬关连接。
//
// 不发 QUIT：QUIT 要等服务端的 221 回应，而可能被……丢弃早已不可达，
// 那一等就是整个清扫 goroutine 卡住。直接关控制连接即可——连接本来就要丢，
// 没有谁需要一次礼貌的道别。
func ftpConnClose(c *ftpConn) {
	if c == nil {
		return
	}
	if c.control != nil {
		_ = c.control.Close()
	}
}

// refreshDeadline 把控制连接的读写 deadline 推到未来。
//
// 必须在每个操作前调用：连接是复用的，上一轮设的 deadline 早就过了。
func (s *FTPSink) refreshDeadline(c *ftpConn) {
	if c == nil || c.control == nil {
		return
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultFTPTimeout
	}
	_ = c.control.SetDeadline(time.Now().Add(timeout))
}

// release 归还或丢弃连接。
//
// 失败时 discard 而不是 put：连接可能已经半坏，放回池里会让下一个任务在
// 写到一半时才暴露问题。
func (s *FTPSink) release(c *ftpConn, reusable bool) {
	if c == nil {
		return
	}
	if reusable {
		s.ensurePool().put(c)
		return
	}
	s.ensurePool().discard(c)
}

// Close 关闭连接。
//
// Sink 接口没有 Close，调用方用可选的 io.Closer 断言来收尾；不实现也可以，
// 只是每次 PUT 都要重新握手。
func (s *FTPSink) Close() error {
	// 派生实例不关池：连接是共享的，一个任务收尾时把池关掉，会连带掐掉
	// 正在上传的其它任务。只有注册目标（ownsPool）才有这个权力。
	if !s.OwnsPool {
		return nil
	}
	if s.pool != nil {
		s.pool.close()
	}
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
