package transfer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/zc310/ofd/internal/allowlist"
)

// HTTPSource 按 URL 拉取待转换文档。拉取方向由调用方给出地址，因此必须经过
// allowlist 校验，否则服务会被当作跳板去请求内网地址或云元数据端点。
type HTTPSource struct {
	// URL 是要拉取的地址。
	URL string
	// Allowlist 限定允许访问的主机与网段。空白名单下 Open 恒返回错误。
	Allowlist *allowlist.List
	// MaxBytes 响应体上限，0 时用 DefaultMaxBytes。
	MaxBytes int64
	// Timeout 整体超时，0 时用 30s。
	Timeout time.Duration
	// AllowRedirect 为真时跟随重定向。每跳仍会经过拨号时的白名单校验，因此不会
	// 绕过限制；但重定向会把原始地址泄露给第三方，默认关闭。
	AllowRedirect bool
	// FileName 覆盖推导出的文件名。为空时从 Content-Disposition 与 URL 路径取。
	FileName string
	// UserAgent 供部分站点识别客户端。
	UserAgent string
	// Log 记录实际拉取的地址与解析结果，便于审计。
	Log *slog.Logger

	// audit 在 Open 中填入实际请求信息。
	audit *HTTPAudit
}

// HTTPAudit 记录一次 URL 拉取的实际去向，出问题时用来追溯。
type HTTPAudit struct {
	// Requested 是调用方给出的原始地址。
	Requested string
	// Fetched 是最终请求的地址（跟随重定向后会变）。
	Fetched string
	// Resolved 是拨号时校验通过的 IP 列表。
	Resolved []string
	// StatusCode 是响应状态。
	StatusCode int
	// Bytes 是读取到的字节数。
	Bytes int64
}

// Audit 返回最近一次 Open 的审计信息，Open 之前调用返回 nil。
func (s *HTTPSource) Audit() *HTTPAudit { return s.audit }

// Name 返回推导出的文件名。URL 无法推导时返回空，调用方需要显式指定格式。
func (s *HTTPSource) Name() string {
	if s.FileName != "" {
		return s.FileName
	}
	if base := path.Base(s.parsedPath()); base != "" && base != "/" && base != "." {
		return base
	}
	return ""
}

func (s *HTTPSource) parsedPath() string {
	parsed, err := url.Parse(s.URL)
	if err != nil {
		return ""
	}
	return parsed.Path
}

// Open 拉取内容。响应体被包装成受上限约束的 ReadCloser，由调用方关闭。
func (s *HTTPSource) Open(ctx context.Context) (io.ReadCloser, error) {
	if s.Allowlist == nil || s.Allowlist.Empty() {
		return nil, fmt.Errorf("URL 拉取未启用：白名单为空")
	}
	parsed, err := url.Parse(s.URL)
	if err != nil {
		return nil, fmt.Errorf("URL 无效: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("只允许 http/https，实际为 %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("URL 缺少主机名")
	}

	limit := s.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Transport: s.transport(), Timeout: timeout}
	if !s.AllowRedirect {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	audit := &HTTPAudit{Requested: s.URL}
	s.audit = audit

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	// 明确要求不走任何中间缓存。
	request.Header.Set("Cache-Control", "no-store")
	if s.UserAgent != "" {
		request.Header.Set("User-Agent", s.UserAgent)
	}

	addrs, err := s.Allowlist.ResolveAndCheck(parsed.Hostname())
	if err != nil {
		return nil, fmt.Errorf("拒绝拉取 %s: %w", parsed.Hostname(), err)
	}
	for _, addr := range addrs {
		audit.Resolved = append(audit.Resolved, addr.String())
	}
	s.log().Info("拉取远程文档", "url", s.URL, "host", parsed.Hostname())

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("拉取 %s 失败: %w", s.URL, err)
	}
	audit.Fetched = response.Request.URL.String()
	audit.StatusCode = response.StatusCode

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, fmt.Errorf("拉取 %s 返回 HTTP %d", s.URL, response.StatusCode)
	}
	if s.FileName == "" {
		if name := filenameFromResponse(response); name != "" {
			s.FileName = name
		}
	}
	return &limitedBody{rc: response.Body, remaining: limit, url: s.URL}, nil
}

func (s *HTTPSource) Close() error { return nil }

func (s *HTTPSource) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// transport 构造带白名单校验的 Transport。
func (s *HTTPSource) transport() *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		// 关键：校验发生在真正拨号的那一刻，并且直接连接已校验的 IP。
		//
		// 只在请求前解析一次再校验是不够的：检查与连接之间存在时间窗，攻击者
		// 可以用 DNS rebinding 在窗口内把域名改指到内网。这里解析一次、校验一次，
		// 然后拨号到那个已校验的地址，后续 DNS 再怎么变都不影响本次连接。
		// TLS 的 SNI 与 Host 头仍取自 URL，证书校验不受影响。
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			addrs, err := s.Allowlist.ResolveAndCheck(host)
			if err != nil {
				return nil, fmt.Errorf("拒绝连接 %s: %w", host, err)
			}
			var lastErr error
			for _, ip := range addrs {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("没有可用地址")
			}
			return nil, lastErr
		},
	}
}

// filenameFromResponse 从 Content-Disposition 优先取文件名，取不到时用 URL 路径。
func filenameFromResponse(response *http.Response) string {
	disposition := response.Header.Get("Content-Disposition")
	if disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			name := params["filename*"]
			if name == "" {
				name = params["filename"]
			}
			if name != "" {
				return SanitizeName(name)
			}
		}
	}
	if parsed, err := url.Parse(response.Request.URL.String()); err == nil {
		return SanitizeName(path.Base(parsed.Path))
	}
	return ""
}

// SanitizeName 把外部来源的文件名收敛成一个安全的单段文件名：去掉路径成分与控制
// 字符。结果为空时返回空串，由调用方补默认名。
func SanitizeName(name string) string {
	// 先按两种分隔符切，只保留最后一段，挡住 "../../etc/passwd" 与 Windows 路径。
	if index := strings.LastIndexAny(name, `/\`); index >= 0 {
		name = name[index+1:]
	}
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			// 控制字符直接丢弃
		case r == '/' || r == '\\' || r == 0:
			builder.WriteRune('_')
		default:
			builder.WriteRune(r)
		}
	}
	cleaned := strings.TrimSpace(builder.String())
	cleaned = strings.TrimLeft(cleaned, ".")
	if cleaned == "" {
		return ""
	}
	if len(cleaned) > 120 {
		// 保留扩展名，避免截断后格式无法识别。
		ext := filepath.Ext(cleaned)
		keep := 120 - len(ext)
		if keep < 1 {
			keep = 1
		}
		cleaned = cleaned[:keep] + ext
	}
	return cleaned
}

// limitedBody 在读取超过上限时返回错误，让"超大的响应"表现为一次明确的失败而不是
// 悄悄截断后交给转换器。
type limitedBody struct {
	rc        io.ReadCloser
	remaining int64
	url       string
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		// 还要再探一个字节，确认是恰好到上限还是超了。
		var probe [1]byte
		n, err := b.rc.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("%s 超过大小上限", b.url)
		}
		if err == nil {
			return 0, io.EOF
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.rc.Read(p)
	if int64(n) > b.remaining {
		return 0, fmt.Errorf("%s 超过大小上限", b.url)
	}
	b.remaining -= int64(n)
	return n, err
}

func (b *limitedBody) Close() error { return b.rc.Close() }
