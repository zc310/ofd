package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/studio-b12/gowebdav"
)

// MinioSink 把转换结果写入 S3 兼容的对象存储（MinIO、AWS S3、Ceph RGW 等）。
//
// 三点与 FTP sink 的共同考量：
//
//  1. **凭据不上线**。地址、bucket、access key 都只存在于服务端配置里，调用方
//     只能给一个目标名。
//
//  2. **先探测再写**。对象存储没有"目录"概念，父目录是靠前缀隐含的，写入不会
//     失败于"目录不存在"。但重复覆盖会静默成功，因此默认仍拒绝覆盖同名对象。
//
//  3. **限制单对象大小**，与 FTP sink 同理。
type MinioSink struct {
	// Endpoint 是 S3 端点的主机名（不含协议），如 "minio.internal:9000"。
	Endpoint string
	// Secure 为真时用 https。为假时用 http——S3 的签名本身防篡改，但
	// 流量与凭据仍是明文，仅适用于内网。
	Secure bool
	// Region 是区域标识。多数自建部署可留空。
	Region string
	// AccessKey 与 SecretKey 是访问凭据。
	AccessKey string
	SecretKey string
	// SessionToken 是临时凭据的令牌，永久凭据留空。
	SessionToken string
	// Bucket 是目标桶。
	Bucket string
	// BasePrefix 是桶内的前缀，等价于 FTP 的 BaseDir。
	BasePrefix string
	// Timeout 是单次请求超时，0 时取 DefaultMinioTimeout。
	Timeout time.Duration
	// MaxBytes 单对象上限，0 时用 DefaultMaxBytes。
	MaxBytes int64
	// Overwrite 为真时允许覆盖同名对象。默认拒绝。
	Overwrite bool

	logger *slog.Logger

	mu   sync.Mutex
	conn *minio.Client
}

// DefaultMinioTimeout 是对象存储单次请求的默认超时。
const DefaultMinioTimeout = 2 * time.Minute

// SetLogger 注入日志器。
func (s *MinioSink) SetLogger(log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	s.logger = log
}

func (s *MinioSink) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// WithBasePrefix 派生一份换掉前缀的副本，并拒绝含 ".." 的前缀。
func (s *MinioSink) WithBasePrefix(prefix string) (*MinioSink, error) {
	cleaned := cleanObjectPrefix(prefix)
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			// 不能等到 Put 再查：那时前缀已经被 path.Join 规整掉了。
			return nil, fmt.Errorf("对象前缀不能包含 ..: %q", prefix)
		}
	}
	merged := cleanObjectPrefix(s.BasePrefix)
	if cleaned != "" {
		if merged == "" {
			merged = cleaned
		} else {
			merged = path.Join(merged, cleaned)
		}
	}
	// 逐字段构造：MinioSink 内含 sync.Mutex，整体赋值会拷贝锁。
	return &MinioSink{
		Endpoint:     s.Endpoint,
		Secure:       s.Secure,
		Region:       s.Region,
		AccessKey:    s.AccessKey,
		SecretKey:    s.SecretKey,
		SessionToken: s.SessionToken,
		Bucket:       s.Bucket,
		BasePrefix:   merged,
		Timeout:      s.Timeout,
		MaxBytes:     s.MaxBytes,
		Overwrite:    s.Overwrite,
		logger:       s.logger,
	}, nil
}

// Put 写入一个对象。
func (s *MinioSink) Put(ctx context.Context, name string, r io.Reader) (Location, error) {
	if s.Endpoint == "" {
		return Location{}, errors.New("未配置对象存储端点")
	}
	if s.Bucket == "" {
		return Location{}, errors.New("未配置对象存储桶")
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Location{}, errors.New("输出文件名为空")
	}
	// 键里带分隔符就能写到前缀之外，与 FTP/Dir 用同一条规则。
	if strings.ContainsAny(trimmed, `/\`) || strings.ContainsRune(trimmed, 0) {
		return Location{}, fmt.Errorf("对象名不能包含路径分隔符: %q", name)
	}
	if trimmed == "." || trimmed == ".." {
		return Location{}, fmt.Errorf("对象名无效: %q", name)
	}
	object := trimmed
	if prefix := strings.Trim(s.BasePrefix, "/"); prefix != "" {
		object = prefix + "/" + trimmed
	}

	conn, err := s.acquire()
	if err != nil {
		return Location{}, err
	}
	if !s.Overwrite {
		if _, statErr := conn.StatObject(ctx, s.Bucket, object, minio.StatObjectOptions{}); statErr == nil {
			return Location{}, fmt.Errorf("%w: 目标对象已存在且不允许覆盖: %s/%s", ErrExists, s.Bucket, object)
		} else if !isObjectNotFound(statErr) {
			// 认证失败、网络问题之类不能当成"不存在"，否则会把覆盖检查
			// 变成一个假阳性，然后让写入去覆盖别人的数据。
			return Location{}, fmt.Errorf("查询对象失败: %w", statErr)
		}
	}

	limit := s.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	counter := &countingReader{r: r, limit: limit}
	info, putErr := conn.PutObject(ctx, s.Bucket, object, counter, -1, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if putErr != nil {
		if counter.exceeded {
			return Location{}, fmt.Errorf("内容超过上限 %d 字节", limit)
		}
		return Location{}, fmt.Errorf("上传对象失败: %w", redactError(putErr, s.SecretKey))
	}
	if info.Size == 0 && counter.n == 0 {
		// 空对象是合法的（空 PDF 之类），不算错误，这里只保证返回值可解释。
		s.log().Debug("上传了空对象", "bucket", s.Bucket, "object", object)
	}
	url := s.EndpointURL() + "/" + s.Bucket + "/" + object
	return Location{Kind: "s3", Bucket: s.Bucket, Key: object, URL: url, Size: info.Size}, nil
}

// EndpointURL 拼出对象的可访问地址，用于任务状态展示。
func (s *MinioSink) EndpointURL() string {
	scheme := "http"
	if s.Secure {
		scheme = "https"
	}
	host := s.Endpoint
	if !strings.Contains(host, "://") {
		host = scheme + "://" + host
	}
	return strings.TrimSuffix(host, "/")
}

// acquire 复用客户端，必要时新建。
//
// minio.Client 自带连接池且并发安全，因此这里不需要像 FTP 那样加互斥保护
// 复用逻辑；只保护"只创建一次"这件事。
func (s *MinioSink) acquire() (*minio.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return s.conn, nil
	}
	creds := credentials.NewStaticV4(s.AccessKey, s.SecretKey, s.SessionToken)
	conn, err := minio.New(s.Endpoint, &minio.Options{
		Creds:  creds,
		Secure: s.Secure,
		Region: s.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化对象存储客户端失败: %w", redactError(err, s.SecretKey))
	}
	s.conn = conn
	s.log().Info("已连接对象存储", "endpoint", s.Endpoint, "bucket", s.Bucket, "secure", s.Secure)
	return conn, nil
}

// Close 释放客户端持有的连接。
func (s *MinioSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = nil
	return nil
}

// isObjectNotFound 判断错误是否是"对象不存在"。
func isObjectNotFound(err error) bool {
	return minio.ToErrorResponse(err).Code == "NoSuchKey" ||
		minio.ToErrorResponse(err).Code == "NoSuchBucket" ||
		minio.ToErrorResponse(err).StatusCode == http.StatusNotFound
}

// cleanObjectPrefix 规整桶内前缀：去首尾斜杠与 "." 段，不动 ".."（交给调用方拒绝）。
func cleanObjectPrefix(prefix string) string {
	prefix = strings.TrimSpace(strings.ReplaceAll(prefix, "\\", "/"))
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(prefix, "/") {
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

// redactError 从错误里抹掉密钥。签名错误里可能带上参与签名的材料。
//
// 短密钥同样不替换，理由见 redact。
func redactError(err error, secret string) error {
	if err == nil || len(secret) < minRedactableSecret {
		return err
	}
	message := strings.ReplaceAll(err.Error(), secret, "***")
	if message == err.Error() {
		return err
	}
	return errors.New(message)
}

// WebDAVSink 把转换结果写入 WebDAV 共享。
//
// 与 FTP 的差别：WebDAV 跑在 HTTP 上，天然有 TLS、有状态码、有中间件生态，
// 因此不需要像 FTP 那样处理明文与 PASV 地址。但它同样需要凭据，同样只接受
// 已注册的目标名，同样限制单文件大小。
type WebDAVSink struct {
	// Endpoint 是服务地址，如 "https://dav.example.com/remote.php/dav/files/user"。
	Endpoint string
	// User 与 Password 是凭据。不会出现在任何日志里。
	User     string
	Password string
	// BaseDir 是允许写入的远端目录（相对 endpoint 根）。
	BaseDir string
	// dirs 记住已建过的远端目录，避免逐页输出时每个文件都重跑一遍 MkdirAll。
	dirs madeDirs
	// Timeout 是单次请求超时，0 时取 DefaultWebDAVTimeout。
	Timeout time.Duration
	// MaxBytes 单文件上限，0 时用 DefaultMaxBytes。
	MaxBytes int64
	// Overwrite 为真时允许覆盖同名文件。默认拒绝。
	Overwrite bool

	logger *slog.Logger

	mu   sync.Mutex
	sink *gowebdav.Client
}

// DefaultWebDAVTimeout 是 WebDAV 单次请求的默认超时。
const DefaultWebDAVTimeout = 2 * time.Minute

// SetLogger 注入日志器。
func (s *WebDAVSink) SetLogger(log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	s.logger = log
}

func (s *WebDAVSink) log() *slog.Logger {
	if s.logger == nil {
		return slog.Default()
	}
	return s.logger
}

// WithBaseDir 派生一份换掉远端目录的副本，并拒绝含 ".." 的目录。
func (s *WebDAVSink) WithBaseDir(dir string) (*WebDAVSink, error) {
	cleaned := strings.Trim(strings.ReplaceAll(dir, "\\", "/"), "/")
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return nil, fmt.Errorf("WebDAV 输出目录不能包含 ..: %q", dir)
		}
	}
	// 一律产出相对 endpoint 根的路径，不保留前导斜杠：gowebdav 会把路径
	// 拼到 endpoint 上，"/root" 会被当成 "endpoint//root"（多一个斜杠），
	// 而 "root" 才是它要的 "endpoint/root"。这一点与 FTPSink 相反——
	// 远端 FTP 的 "/results" 与 "results" 确实不是一回事。
	base := strings.Trim(s.BaseDir, "/")
	switch {
	case cleaned == "":
	case base == "":
		base = cleaned
	default:
		base = path.Join(base, cleaned)
	}
	return &WebDAVSink{
		Endpoint:  s.Endpoint,
		User:      s.User,
		Password:  s.Password,
		BaseDir:   base,
		Timeout:   s.Timeout,
		MaxBytes:  s.MaxBytes,
		Overwrite: s.Overwrite,
		logger:    s.logger,
	}, nil
}

// Put 写入一个文件，父目录按需创建。
func (s *WebDAVSink) Put(ctx context.Context, name string, r io.Reader) (Location, error) {
	if s.Endpoint == "" {
		return Location{}, errors.New("未配置 WebDAV 地址")
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Location{}, errors.New("输出文件名为空")
	}
	if strings.ContainsAny(trimmed, `/\`) || strings.ContainsRune(trimmed, 0) {
		return Location{}, fmt.Errorf("WebDAV 输出文件名不能包含路径分隔符: %q", name)
	}
	if trimmed == "." || trimmed == ".." {
		return Location{}, fmt.Errorf("WebDAV 输出文件名无效: %q", name)
	}
	if err := ctx.Err(); err != nil {
		return Location{}, err
	}

	client, err := s.acquire()
	if err != nil {
		return Location{}, err
	}
	base := strings.Trim(s.BaseDir, "/")
	remote := trimmed
	if base != "" {
		remote = base + "/" + trimmed
	}
	// 目录记忆：逐页输出常是同一个目录几百个文件，而 MkdirAll 每个文件都要
	// 跑一遍（逐级 MKCOL），全是浪费的往返。
	//
	// WebDAV 的 MKCOL 建目录是否幂等由实现决定，所以首次仍然逐级试建，
	// 只是不重复建。
	if base != "" && !s.dirs.has(base) {
		if err := client.MkdirAll(base, 0o750); err != nil {
			return Location{}, fmt.Errorf("创建 WebDAV 目录 %s 失败: %w", base, redactError(err, s.Password))
		}
		s.dirs.add(base)
	}
	if !s.Overwrite {
		if _, statErr := client.Stat(remote); statErr == nil {
			return Location{}, fmt.Errorf("%w: 目标文件已存在且不允许覆盖: %s", ErrExists, remote)
		} else if !isNotFound(statErr) {
			return Location{}, fmt.Errorf("查询 WebDAV 文件失败: %w", redactError(statErr, s.Password))
		}
	}

	limit := s.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	counter := &countingReader{r: r, limit: limit}
	// 长度未知时 gowebdav 会用分块传输，正合适——我们本来就不知道确切大小，
	// 而且限制由 countingReader 兜着。
	if err := client.WriteStream(remote, counter, 0o640); err != nil {
		if counter.exceeded {
			return Location{}, fmt.Errorf("内容超过上限 %d 字节", limit)
		}
		// 失败可能是"目录在我们建完之后又被删了"，目录记忆已不可信，清掉它。
		s.dirs.forgetAll()
		return Location{}, fmt.Errorf("写入 WebDAV 失败: %w", redactError(err, s.Password))
	}
	return Location{Kind: "webdav", Path: remote, Size: counter.n,
		URL: strings.TrimSuffix(s.Endpoint, "/") + "/" + remote}, nil
}

// acquire 复用客户端。
func (s *WebDAVSink) acquire() (*gowebdav.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sink != nil {
		return s.sink, nil
	}
	client := gowebdav.NewClient(s.Endpoint, s.User, s.Password)
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultWebDAVTimeout
	}
	client.SetTimeout(timeout)
	// 限制底层传输：响应头不能无限等、连接要能回收。gowebdav 只暴露
	// SetTransport，没有 SetHTTPClient，因此超时由 SetTimeout 统一管。
	client.SetTransport(&http.Transport{
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: timeout,
	})
	s.sink = client
	s.log().Info("已连接 WebDAV", "endpoint", s.Endpoint, "user", s.User)
	return client, nil
}

// Close 释放客户端。
func (s *WebDAVSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sink = nil
	return nil
}

// isNotFound 判断错误是否是 404。
//
// 用库自带的 IsErrCode：StatusError 是值类型且包在 *os.PathError 里，
// 自己用 errors.As 剥要照顾好几层。
func isNotFound(err error) bool {
	return gowebdav.IsErrCode(err, http.StatusNotFound)
}
