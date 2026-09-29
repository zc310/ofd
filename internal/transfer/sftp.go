package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SFTPSink 把转换结果通过 SFTP 写入远端。
//
// SFTP 跑在 SSH 上，因此天然加密，这点和明文 FTP 有本质区别。但也带来一个
// FTP/WebDAV/MinIO 都没有的开关：**主机密钥校验**。
//
// SSH 靠 host key 识别对方。只连不管的话，攻击者可以在中间人位置冒充服务器，
// 拿到私钥或口令、读到全部文件内容——而连接照样"成功"。所以这里默认必须校验，
// 三种方式按严格程度排：
//
//  1. HostKeySHA256：配置里写死目标主机的指纹（ssh-keygen 打印的那个）。
//  2. HostKeyFile：读 OpenSSH 的 known_hosts 文件。
//  3. InsecureIgnoreHostKey：跳过校验。必须显式声明，配置校验也会拦一次。
//
// 三种都不配时报错，而不是退回"跳过校验"——那正是最容易出事的情况。
type SFTPSink struct {
	// Addr 是服务器地址，形如 "sftp.example.com:22"。
	Addr string
	// User 是登录用户名。
	User string
	// Auth 是认证方式。三种可叠加，客户端会按顺序尝试。
	Auth SFTPAuth
	// BaseDir 是允许写入的远端目录，空表示登录用户的家目录。
	BaseDir string
	// HostKeySHA256 是预期的主机密钥指纹（SHA256:...），与 OpenSSH 一致。
	HostKeySHA256 string
	// HostKeyFile 是 known_hosts 文件路径。
	HostKeyFile string
	// InsecureIgnoreHostKey 跳过主机密钥校验。仅供一次性排查。
	InsecureIgnoreHostKey bool
	// Timeout 是连接、认证与单次操作的超时，0 时取 DefaultSFTPTimeout。
	Timeout time.Duration
	// MaxBytes 单文件上限，0 时用 DefaultMaxBytes。
	MaxBytes int64
	// Overwrite 为真时允许覆盖远端同名文件。默认拒绝。
	Overwrite bool

	logger *slog.Logger

	mu   sync.Mutex
	conn *ssh.Client
	cl   *sftp.Client
}

// DefaultSFTPTimeout 是 SFTP 连接与操作的默认超时。
const DefaultSFTPTimeout = 60 * time.Second

// SFTPAuth 描述 SFTP 的登录方式。
//
// 私钥文件与 agent 都不需要把密钥材料放进服务配置；口令是最后手段，配置文件
// 里的口令应当等同于明文存储。
type SFTPAuth struct {
	// PrivateKeyFile 是私钥文件路径（PEM 或 OpenSSH 新格式）。
	PrivateKeyFile string
	// PrivateKeyPassphrase 是私钥口令。留空表示私钥未加密。
	// 加密私钥且这里留空时连接会失败——这是有意的：不想把口令写进配置文件，
	// 就该走 AgentSocket。
	PrivateKeyPassphrase string
	// AgentSocket 是 ssh-agent 的 socket 路径。配置里只有这个路径，
	// 私钥与已解密的口令都由 agent 持有，是最干净的一种。
	AgentSocket string
	// Password 是口令认证的密码。
	Password string
}

// configured 报告是否配置了任何一种认证方式。
func (a SFTPAuth) configured() bool {
	return a.PrivateKeyFile != "" || a.AgentSocket != "" || a.Password != ""
}

// SetLogger 注入日志器。
func (s *SFTPSink) SetLogger(log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	s.logger = log
}

func (s *SFTPSink) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// WithBaseDir 派生一份换掉远端目录的副本，并拒绝含 ".." 的目录。
func (s *SFTPSink) WithBaseDir(dir string) (*SFTPSink, error) {
	cleaned := cleanAbsoluteRemote(dir)
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			// 必须在 path.Join 之前拒绝：它会把 ".." 规整掉，那时再查就晚了。
			return nil, fmt.Errorf("SFTP 输出目录不能包含 ..: %q", dir)
		}
	}
	base := cleanAbsoluteRemote(s.BaseDir)
	switch {
	case cleaned == "":
	case base == "":
		base = cleaned
	default:
		base = path.Join(base, cleaned)
	}
	// 逐字段构造：SFTPSink 内含 sync.Mutex，整体赋值会拷贝锁。
	return &SFTPSink{
		Addr:                  s.Addr,
		User:                  s.User,
		Auth:                  s.Auth,
		BaseDir:               base,
		HostKeySHA256:         s.HostKeySHA256,
		HostKeyFile:           s.HostKeyFile,
		InsecureIgnoreHostKey: s.InsecureIgnoreHostKey,
		Timeout:               s.Timeout,
		MaxBytes:              s.MaxBytes,
		Overwrite:             s.Overwrite,
		logger:                s.logger,
	}, nil
}

// Put 上传一个文件，父目录按需创建。
func (s *SFTPSink) Put(ctx context.Context, name string, r io.Reader) (Location, error) {
	if s.Addr == "" {
		return Location{}, errors.New("未配置 SFTP 地址")
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Location{}, errors.New("输出文件名为空")
	}
	// 与其他 Sink 同一套名字规则：单段文件名，远程路径无法借此逃出 BaseDir。
	if strings.ContainsAny(trimmed, `/\`) || strings.ContainsRune(trimmed, 0) {
		return Location{}, fmt.Errorf("SFTP 输出文件名不能包含路径分隔符: %q", name)
	}
	if trimmed == "." || trimmed == ".." {
		return Location{}, fmt.Errorf("SFTP 输出文件名无效: %q", name)
	}
	// 保留前导斜杠：SFTP 的远端路径是绝对路径（家目录是 /upload 这种），
	// "/converted" 与 "converted" 不是一回事。这里与 WebDAV 相反——
	// gowebdav 要相对路径，sftp 要绝对路径。
	base := cleanAbsoluteRemote(s.BaseDir)
	remote := trimmed
	if base != "" {
		remote = base + "/" + trimmed
	}
	if err := ctx.Err(); err != nil {
		return Location{}, err
	}

	client, err := s.acquire(ctx)
	if err != nil {
		return Location{}, err
	}
	if base != "" {
		if err := client.MkdirAll(base); err != nil {
			return Location{}, fmt.Errorf("创建 SFTP 目录 %s 失败: %w", base, redactError(err, s.Auth.Password))
		}
	}
	if !s.Overwrite {
		if _, statErr := client.Stat(remote); statErr == nil {
			return Location{}, fmt.Errorf("%w: 目标文件已存在且不允许覆盖: %s", ErrExists, remote)
		} else if !isSFTPNotFound(statErr) {
			// 认证失败、权限不足之类不能当成"文件不存在"，否则覆盖检查就
			// 变成假阳性，然后去覆盖别人的文件。
			return Location{}, fmt.Errorf("查询 SFTP 文件失败: %w", redactError(statErr, s.Auth.Password))
		}
	}

	limit := s.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	file, err := client.Create(remote)
	if err != nil {
		return Location{}, fmt.Errorf("创建 SFTP 文件 %s 失败: %w", remote, redactError(err, s.Auth.Password))
	}
	counter := &countingReader{r: r, limit: limit}
	written, copyErr := io.Copy(file, counter)
	closeErr := file.Close()
	if counter.exceeded {
		// 超限时远端已经写进去一半。留着截断的文件比删掉更糟：调用方
		// 看到文件存在就会去取，取到的是残缺内容。删除失败只能记日志——
		// 清理不该掩盖真正的失败原因。
		if err := client.Remove(remote); err != nil {
			s.log().Warn("删除超限的残留文件失败", "path", remote, "written", written, "err", err)
		}
		return Location{}, fmt.Errorf("内容超过上限 %d 字节（已写入 %d 字节后中止）", limit, written)
	}
	if copyErr != nil {
		return Location{}, fmt.Errorf("写入 SFTP 失败: %w", redactError(copyErr, s.Auth.Password))
	}
	if closeErr != nil {
		return Location{}, fmt.Errorf("关闭 SFTP 文件失败: %w", redactError(closeErr, s.Auth.Password))
	}
	return Location{Kind: "sftp", Path: remote, Size: counter.n}, nil
}

// acquire 复用连接，必要时新建。
func (s *SFTPSink) acquire(ctx context.Context) (*sftp.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cl != nil {
		return s.cl, nil
	}
	if !s.Auth.configured() {
		return nil, errors.New("未配置任何 SFTP 认证方式（私钥文件、agent 或口令）")
	}
	hostKeyCallback, err := s.hostKeyCallback()
	if err != nil {
		return nil, err
	}
	methods, err := s.authMethods()
	if err != nil {
		return nil, err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultSFTPTimeout
	}
	config := &ssh.ClientConfig{
		User:            s.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}
	// 拨号走 ctx：Put 可能在队列里等了很久，连接时不该再阻塞。
	dialer := &net.Dialer{Timeout: timeout}
	netConn, err := dialer.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return nil, fmt.Errorf("连接 SFTP 服务器失败: %w", err)
	}
	// 握手也要有 deadline，否则认证阶段可能无限等。
	if err := netConn.SetDeadline(time.Now().Add(timeout)); err == nil {
		defer func() { _ = netConn.SetDeadline(time.Time{}) }()
	}
	sshConn, channels, requests, err := ssh.NewClientConn(netConn, s.Addr, config)
	if err != nil {
		_ = netConn.Close()
		return nil, fmt.Errorf("SFTP 握手失败: %w", redactError(err, s.Auth.Password))
	}
	client := ssh.NewClient(sshConn, channels, requests)
	fileClient, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("启动 SFTP 会话失败: %w", redactError(err, s.Auth.Password))
	}
	s.conn = client
	s.cl = fileClient
	s.log().Info("已连接 SFTP 服务器", "addr", s.Addr, "user", s.User)
	return fileClient, nil
}

// hostKeyCallback 构造主机密钥校验回调。
func (s *SFTPSink) hostKeyCallback() (ssh.HostKeyCallback, error) {
	switch {
	case s.InsecureIgnoreHostKey:
		// 连接被劫持时这里什么也拦不住，是所有分支里最弱的。
		return ssh.InsecureIgnoreHostKey(), nil
	case s.HostKeySHA256 != "":
		expected := normalizeFingerprint(s.HostKeySHA256)
		return func(_ string, _ net.Addr, key ssh.PublicKey) error {
			actual := normalizeFingerprint(ssh.FingerprintSHA256(key))
			if actual != expected {
				return fmt.Errorf("主机密钥不匹配：期望 %s，实际 %s", expected, actual)
			}
			return nil
		}, nil
	case s.HostKeyFile != "":
		callback, err := knownhosts.New(s.HostKeyFile)
		if err != nil {
			return nil, fmt.Errorf("读取 known_hosts 失败: %w", err)
		}
		return callback, nil
	default:
		// 落到这里说明三种都没配。退回"跳过校验"是最糟的选择——服务看起来
		// 正常工作，但所有凭据都可被中间人获取。
		return nil, errors.New("未配置主机密钥校验：请设置 host_key_sha256 或 host_key_file" +
			"（确需跳过请显式声明 insecure_ignore_host_key）")
	}
}

// authMethods 组装认证方式，顺序与常见客户端一致：agent、私钥、口令。
func (s *SFTPSink) authMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if s.Auth.AgentSocket != "" {
		conn, err := net.Dial("unix", s.Auth.AgentSocket)
		if err != nil {
			return nil, fmt.Errorf("连接 ssh-agent 失败: %w", err)
		}
		methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
	}
	if s.Auth.PrivateKeyFile != "" {
		var signer ssh.Signer
		var err error
		// 先单独读文件：路径写错与私钥被加密是两个完全不同的问题，
		// 混在一起报"解析失败"会让人往错的方向查。
		keyBytes, readErr := os.ReadFile(s.Auth.PrivateKeyFile)
		if readErr != nil {
			return nil, fmt.Errorf("读取私钥文件 %s 失败: %w", s.Auth.PrivateKeyFile, readErr)
		}
		if s.Auth.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(s.Auth.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyBytes)
		}
		if err != nil {
			return nil, fmt.Errorf("解析私钥 %s 失败（若私钥已加密，请配置 passphrase 或改用 agent）: %w",
				s.Auth.PrivateKeyFile, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if s.Auth.Password != "" {
		methods = append(methods, ssh.Password(s.Auth.Password))
	}
	return methods, nil
}

// Close 关闭连接。
func (s *SFTPSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	if s.cl != nil {
		if err := s.cl.Close(); err != nil {
			firstErr = err
		}
		s.cl = nil
	}
	if s.conn != nil {
		if err := s.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		s.conn = nil
	}
	return firstErr
}

// cleanAbsoluteRemote 规整远端绝对路径：去掉重复与尾随斜杠，保留前导斜杠。
//
// 与 FTPSink 的 cleanRemoteBase 是同一套语义，单独写一份是因为 WebDAV 那边
// 需要相反的行为（相对路径），共用一个函数迟早会有人在某处用错。
func cleanAbsoluteRemote(dir string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(dir), "\\", "/")
	absolute := strings.HasPrefix(normalized, "/")
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." {
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

// normalizeFingerprint 统一指纹写法：去掉 "SHA256:" 前缀的大小写差异与空白。
//
// OpenSSH 打印的是 "SHA256:xxxx"，配置里常写成小写或漏掉前缀，
// 不该因为大小写就报"主机密钥不匹配"。
func normalizeFingerprint(fingerprint string) string {
	trimmed := strings.TrimSpace(fingerprint)
	trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "SHA256:"), "sha256:")
	return "SHA256:" + trimmed
}

// isSFTPNotFound 判断错误是否是"文件不存在"。
func isSFTPNotFound(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, fs.ErrNotExist) ||
		strings.Contains(err.Error(), "no such file")
}
