// Command ofd-server 提供异步 OFD 转换的 HTTP 服务。
package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zc310/ofd/internal/allowlist"
	"github.com/zc310/ofd/internal/notify"
	"github.com/zc310/ofd/internal/transfer"
)

// Duration 是能从 JSON 里读出两种写法的时长。
//
// 标准库的 time.Duration 只认整数纳秒，配置里写 "retention": 720h 会被解析成
// 7.2e15 而非预期的时长，写成 "168h" 则直接报错。配置文件是人读的，这里两种都收：
// 字符串走 time.ParseDuration，数字按纳秒（与 time.Duration 本身一致）。
type Duration time.Duration

// UnmarshalJSON 实现 json.Unmarshaler。
func (d *Duration) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "null" || text == `""` {
		*d = 0
		return nil
	}
	if strings.HasPrefix(text, `"`) {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			*d = 0
			return nil
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("时长 %q 不合法（可用 30s、5m、2h30m）: %w", raw, err)
		}
		*d = Duration(parsed)
		return nil
	}
	// 裸数字按纳秒处理。
	var nanos int64
	if err := json.Unmarshal(data, &nanos); err != nil {
		return fmt.Errorf("时长应为 \"30s\" 这样的字符串或纳秒整数: %w", err)
	}
	*d = Duration(nanos)
	return nil
}

// MarshalJSON 反向输出为可读字符串。
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// Duration 取出标准库类型。
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Config 是服务配置。
//
// 配置来源是单个 JSON 文件加少量环境变量覆盖。之所以不用环境变量承载全部配置：
// 通知目标的密钥不适合出现在进程环境里（/proc/<pid>/environ 对同用户可读），
// 而零散的 flag 又没法表达嵌套结构。
type Config struct {
	// Listen 是监听地址，host 为空表示监听所有网卡，如 ":9705"。
	Listen string `json:"listen"`
	// APIKey 是访问 /v1/* 接口的 Bearer 令牌。必填。
	//
	// 服务会把调用方上传的文件交给 LibreOffice 与 Chrome 解析，两者都是解析
	// 不可信输入的经典目标；而 listen 的默认值 ":9705" 监听所有网卡。所以
	// "内网、前面有网关"只是部署假设，不是访问控制——真的要有网关也该由
	// 服务自己再校验一次，否则换一种部署方式就没有任何保护。
	//
	// 留空即拒绝启动，不提供"关闭认证"的开关：一个能被静默关掉的认证
	// 等于没有认证。探针端点 /healthz 与 /readyz 不需要这个令牌。
	APIKey string `json:"api_key"`
	// DBPath 是 bbolt 任务库路径。单节点，同一文件只能由一个进程打开。
	DBPath string `json:"db_path"`
	// TempDir 是转换期间的临时目录。
	TempDir string `json:"temp_dir"`
	// OutputDir 是 dir 输出的根目录，所有结果都落在它下面。
	OutputDir string `json:"output_dir"`
	// LogLevel 是 slog 的级别名。
	LogLevel string `json:"log_level"`
	// LogDir 是日志目录。为空表示只写标准输出，不落文件——容器部署下
	// 由 runtime 收集 stdout 往往比写文件更合适。
	LogDir string `json:"log_dir"`
	// LogFile 是 LogDir 下的文件名。为空时取 "ofd-server.log"。
	LogFile string `json:"log_file"`
	// LogMaxSizeMB 是单个日志文件的大小上限，超过即轮转。
	LogMaxSizeMB int `json:"log_max_size_mb"`
	// LogMaxBackups 是保留的历史文件个数，0 表示不限制数量。
	LogMaxBackups int `json:"log_max_backups"`
	// LogMaxAgeDays 是历史日志的保留天数，0 表示不限。
	LogMaxAgeDays int `json:"log_max_age_days"`
	// LogCompress 表示是否 gzip 压缩轮转出去的历史日志。
	LogCompress bool `json:"log_compress"`
	// LogToStdout 控制是否同时写标准输出。关掉后只有文件里有日志，
	// 排查线上问题时要先知道去哪儿看。
	LogToStdout *bool `json:"log_to_stdout"`

	// MaxUploadBytes 限制请求体大小，0 时取 64 MiB。
	MaxUploadBytes int64 `json:"max_upload_bytes"`
	// MaxStreamBytes 限制单次 stream 输出的内存占用，0 时取 64 MiB。
	MaxStreamBytes int64 `json:"max_stream_bytes"`

	// FastWorkers 是快速通道的并发数。
	FastWorkers int `json:"fast_workers"`
	// HeavyWorkers 是重通道的并发数。
	HeavyWorkers int `json:"heavy_workers"`
	// NotifyWorkers 是通知投递的并发数。
	NotifyWorkers int `json:"notify_workers"`
	// JobTimeout 是单次转换的整体超时，0 表示不限。
	JobTimeout Duration `json:"job_timeout"`
	// MaxJobAttempts 是任务自动重试次数，0 表示只试一次。
	MaxJobAttempts int `json:"max_job_attempts"`
	// JobRetryBackoff 是首次重试的等待时长，之后按次数指数翻倍，上限 10 分钟。
	//
	// 这一项过去没有对应的配置，runner 拿到的值一直是 0，于是"按退避重新排队"
	// 实际是零延迟立即重试——一批任务同时失败时会形成惊群，把 worker 反复
	// 占满去跑注定失败的转换。
	JobRetryBackoff Duration `json:"job_retry_backoff"`
	// Retention 是终态任务的保留时长。
	Retention Duration `json:"retention"`

	// AllowInputURLHosts 是 input.kind=url 允许访问的主机与网段。为空时
	// URL 输入一律拒绝——这是默认值，不是不安全时的降级。
	AllowInputURLHosts []string `json:"allow_input_url_hosts"`
	// URLTimeout 是拉取远程输入的整体超时。
	URLTimeout Duration `json:"url_timeout"`

	// NotifyTargets 是预注册的通知目标。请求方只能引用名字。
	NotifyTargets map[string]NotifyTarget `json:"notify_targets"`
	// FTPTargets 是预注册的 FTP 目标。请求方只能引用名字，地址与凭据不上线。
	FTPTargets map[string]FTPTargetConfig `json:"ftp_targets"`
	// S3Targets 是预注册的对象存储目标。
	S3Targets map[string]S3TargetConfig `json:"s3_targets"`
	// WebDAVTargets 是预注册的 WebDAV 目标。
	WebDAVTargets map[string]WebDAVTargetConfig `json:"webdav_targets"`
	// SFTPTargets 是预注册的 SFTP 目标。
	SFTPTargets map[string]SFTPTargetConfig `json:"sftp_targets"`
	// OfficePath 与 ChromePath 是外部转换程序路径，为空时由转换库自行探测。
	OfficePath string `json:"soffice_path"`
	ChromePath string `json:"chrome_path"`
	// ChromeNoSandbox 给 Chrome 加 --no-sandbox。
	//
	// 容器里通常需要：Chrome 沙箱依赖 user namespace，而容器默认不提供，
	// 不开这个开关 HTML 转换会起不来。chromedp 只在以 root 运行时自动补该
	// 参数，因此非 root 的容器必须显式打开。代价是 Chrome 失去沙箱隔离，
	// 只应在容器边界内的可信部署上使用。
	ChromeNoSandbox bool `json:"chrome_no_sandbox"`
	// AllowInsecureFTP 允许配置明文 FTP 目标。默认 false：明文下账号密码在
	// 链路上没有任何保护，这个开关必须由部署方主动打开。
	AllowInsecureFTP bool `json:"allow_insecure_ftp"`
	// AllowInsecureS3 允许配置 http 的对象存储目标。默认 false，理由同 FTP。
	AllowInsecureS3 bool `json:"allow_insecure_s3"`

	// allowInsecureFTP 是 AllowInsecureFTP 的内部副本，validate 需要读它
	// 而不能改动调用方的配置。
	allowInsecureFTP bool
	allowInsecureS3  bool
}

// NotifyTarget 是一个预注册的通知目标。
type NotifyTarget struct {
	// URL 是完整回调地址。仅服务端可见，调用方碰不到。
	URL string `json:"url"`
	// Secret 是 HMAC 密钥。正式环境务必配置：没有它接收方无法验证来源，
	// 任何人都能伪造回调。
	Secret string `json:"secret"`
	// Events 限定订阅的事件，空表示全部。
	Events []string `json:"events"`
	// TimeoutSeconds 是单次投递超时。
	TimeoutSeconds int `json:"timeout_seconds"`
}

// FTPTargetConfig 是一个预注册的 FTP 输出目标。
type FTPTargetConfig struct {
	// Addr 是服务器地址，如 "ftp.example.com:21"。
	Addr string `json:"addr"`
	// User 与 Password 是登录凭据。
	User     string `json:"user"`
	Password string `json:"password"`
	// BaseDir 是允许写入的远端根目录，空表示用户家目录。
	BaseDir string `json:"base_dir"`
	// Insecure 允许明文 FTP。明文下账号密码在链路上没有任何保护，
	// 生产环境不应开启；开启后启动时会记一条 WARN。
	Insecure bool `json:"insecure"`
	// Implicit 为真时用隐式 FTPS（连上直接握手），否则用显式 FTPS
	// （先读 220，再用 AUTH TLS 升级）。显式是更常见的部署形态。
	Implicit bool `json:"implicit"`
	// InsecureSkipVerify 跳过 TLS 证书校验。仅用于自签名证书的内网服务器。
	InsecureSkipVerify bool `json:"insecure_skip_verify"`
	// DisableEPSV 强制只用 PASV。少数服务器没实现 EPSV，而客户端默认优先试它。
	DisableEPSV bool `json:"disable_epsv"`
	// TimeoutSeconds 是连接与单次操作的超时。
	TimeoutSeconds int `json:"timeout_seconds"`
	// MaxBytes 是单文件上限，0 时取 64 MiB。
	MaxBytes int64 `json:"max_bytes"`
	// Overwrite 允许覆盖远端同名文件。默认拒绝。
	Overwrite bool `json:"overwrite"`
}

// S3TargetConfig 是一个预注册的对象存储目标。
type S3TargetConfig struct {
	// Endpoint 是主机名（不含协议），如 "minio.internal:9000"。
	Endpoint string `json:"endpoint"`
	// Secure 为真时用 https。http 下签名仍有效，但流量与凭据是明文。
	Secure bool `json:"secure"`
	// Region 是区域标识，自建部署通常留空。
	Region string `json:"region"`
	// AccessKey 与 SecretKey 是访问凭据。
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token"`
	// Bucket 是目标桶，必须已存在（本服务不负责建桶）。
	Bucket string `json:"bucket"`
	// Prefix 是桶内前缀，等价于 FTP 的 base_dir。
	Prefix string `json:"prefix"`
	// TimeoutSeconds 是单次请求超时。
	TimeoutSeconds int `json:"timeout_seconds"`
	// MaxBytes 是单对象上限，0 时取 64 MiB。
	MaxBytes int64 `json:"max_bytes"`
	// Overwrite 允许覆盖同名对象。默认拒绝。
	Overwrite bool `json:"overwrite"`
}

// WebDAVTargetConfig 是一个预注册的 WebDAV 目标。
type WebDAVTargetConfig struct {
	// Endpoint 是完整地址，如 "https://dav.example.com/remote.php/dav/files/user"。
	Endpoint string `json:"endpoint"`
	// User 与 Password 是凭据。
	User     string `json:"user"`
	Password string `json:"password"`
	// BaseDir 是允许写入的远端目录。
	BaseDir string `json:"base_dir"`
	// TimeoutSeconds 是单次请求超时。
	TimeoutSeconds int `json:"timeout_seconds"`
	// MaxBytes 是单文件上限，0 时取 64 MiB。
	MaxBytes int64 `json:"max_bytes"`
	// Overwrite 允许覆盖同名文件。默认拒绝。
	Overwrite bool `json:"overwrite"`
}

// SFTPTargetConfig 是一个预注册的 SFTP 目标。
type SFTPTargetConfig struct {
	// Addr 是服务器地址，如 "sftp.example.com:22"。
	Addr string `json:"addr"`
	// User 是登录用户名。
	User string `json:"user"`
	// Auth 是认证方式。三种可叠加，客户端按 agent、私钥、口令的顺序尝试。
	Auth SFTPAuthConfig `json:"auth"`
	// BaseDir 是允许写入的远端目录，空表示登录用户的家目录。
	BaseDir string `json:"base_dir"`
	// HostKeySHA256 是预期的主机密钥指纹（ssh-keygen 打印的那个）。
	HostKeySHA256 string `json:"host_key_sha256"`
	// HostKeyFile 是 known_hosts 文件路径。
	HostKeyFile string `json:"host_key_file"`
	// InsecureIgnoreHostKey 跳过主机密钥校验。
	// 连接被劫持时凭据与内容都会泄露，只有一次性排查才该用。
	InsecureIgnoreHostKey bool `json:"insecure_ignore_host_key"`
	// TimeoutSeconds 是连接、认证与单次操作的超时。
	TimeoutSeconds int `json:"timeout_seconds"`
	// MaxBytes 是单文件上限，0 时取 64 MiB。
	MaxBytes int64 `json:"max_bytes"`
	// Overwrite 允许覆盖远端同名文件。默认拒绝。
	Overwrite bool `json:"overwrite"`
}

// SFTPAuthConfig 描述 SFTP 的登录方式。
type SFTPAuthConfig struct {
	// PrivateKeyFile 是私钥文件路径。
	PrivateKeyFile string `json:"private_key_file"`
	// PrivateKeyPassphrase 是私钥口令。留空表示私钥未加密。
	// 不想把口令写进配置文件，就该用 AgentSocket。
	PrivateKeyPassphrase string `json:"private_key_passphrase"`
	// AgentSocket 是 ssh-agent 的 socket 路径。最干净的一种：
	// 配置里只有路径，私钥与已解密的口令都由 agent 持有。
	AgentSocket string `json:"agent_socket"`
	// Password 是口令认证的密码。配置文件里的口令等同于明文存储。
	Password string `json:"password"`
}

// LoadConfig 读取配置文件并用环境变量覆盖若干项。
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取配置失败: %w", err)
		}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(cfg); err != nil {
			// 未知字段直接报错：配置里的拼写错误如果被静默忽略，表现出来是
			// "服务起来了但用了默认值"，排查起来非常费时间。
			return nil, fmt.Errorf("解析配置失败: %w", err)
		}
	}
	applyEnvOverrides(cfg)
	cfg.allowInsecureFTP = cfg.AllowInsecureFTP
	cfg.allowInsecureS3 = cfg.AllowInsecureS3
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyEnvOverrides 用环境变量覆盖。容器部署时把路径与监听地址留在环境里更方便。
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("OFD_SERVER_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("OFD_SERVER_DB"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("OFD_SERVER_TEMP_DIR"); v != "" {
		cfg.TempDir = v
	}
	if v := os.Getenv("OFD_SERVER_OUTPUT_DIR"); v != "" {
		cfg.OutputDir = v
	}
	if v := os.Getenv("OFD_SERVER_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	// 令牌走环境变量，不进配置文件：/proc/<pid>/environ 对同用户可读，
	// 而配置文件常常是 0644 跟着镜像或备份走一圈。
	if v := os.Getenv("OFD_SERVER_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	// 密钥单独走环境变量，不进配置文件。
	if v := os.Getenv("OFD_SERVER_MAX_UPLOAD_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.MaxUploadBytes = n
		}
	}
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":9705"
	}
	if c.TempDir == "" {
		c.TempDir = filepath.Join(os.TempDir(), "ofd-server")
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	if c.JobRetryBackoff <= 0 {
		// 0 意味着"零延迟重试"，那是惊群而不是退避。5 秒是让远端与文件系统
		// 有时间恢复的量级；已经失败的转换立刻重跑不会有不同结果。
		c.JobRetryBackoff = Duration(5 * time.Second)
	}
	if c.LogFile == "" {
		c.LogFile = "ofd-server.log"
	}
	if c.LogMaxSizeMB <= 0 {
		c.LogMaxSizeMB = 100
	}
	if c.LogMaxBackups <= 0 {
		c.LogMaxBackups = 10
	}
	if c.LogMaxAgeDays <= 0 {
		c.LogMaxAgeDays = 30
	}
	// LogToStdout 零值是 false，但"只写文件不写 stdout"几乎总是误配：
	// 容器里看不到日志、K8s 里也收不到。显式用指针才能表达"我要关掉"。
	if c.LogToStdout == nil {
		// 默认值跟着 log_dir 走：配了日志目录就只写文件，不配就只写 stdout。
		// 两处都写等于每行日志序列化一次、写两次，既没有额外信息也白花 I/O。
		// 用指针而不是 bool，是为了让"未设"和"显式关掉"能区分开——
		// 零值 false 配上"配了目录"的默认就正好反了。
		enabled := c.LogDir == ""
		c.LogToStdout = &enabled
	}
	if c.MaxUploadBytes <= 0 {
		c.MaxUploadBytes = 64 << 20
	}
	if c.MaxStreamBytes <= 0 {
		c.MaxStreamBytes = 64 << 20
	}
	if c.FastWorkers <= 0 {
		c.FastWorkers = 4
	}
	if c.HeavyWorkers <= 0 {
		// 重通道默认串行：每个 heavy 任务都会拉起一个外部进程，
		// 并发拉高会直接吃光内存。
		c.HeavyWorkers = 1
	}
	if c.NotifyWorkers <= 0 {
		c.NotifyWorkers = 4
	}
	if c.Retention <= 0 {
		c.Retention = Duration(7 * 24 * time.Hour)
	}
	if c.URLTimeout <= 0 {
		c.URLTimeout = Duration(30 * time.Second)
	}
}

func (c *Config) validate() error {
	if c.DBPath == "" {
		return fmt.Errorf("必须配置 db_path（或设置 OFD_SERVER_DB）")
	}
	if c.OutputDir == "" {
		return fmt.Errorf("必须配置 output_dir（或设置 OFD_SERVER_OUTPUT_DIR）")
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("必须配置 api_key（或设置 OFD_SERVER_API_KEY）：" +
			"服务会把上传文件交给 LibreOffice 与 Chrome 解析，接口不能匿名开放。" +
			"可用 openssl rand -hex 32 生成")
	}
	for name, target := range c.FTPTargets {
		if target.Addr == "" {
			return fmt.Errorf("FTP 目标 %s 缺少 addr", name)
		}
		if target.User == "" {
			return fmt.Errorf("FTP 目标 %s 缺少 user", name)
		}
		if target.Insecure && !c.allowInsecureFTP {
			// 明文 FTP 是有意识的选择，必须在配置里显式认领。
			return fmt.Errorf("FTP 目标 %s 使用明文 FTP：请改用 FTPS（insecure=false），"+
				"或在配置中设置 \"allow_insecure_ftp\": true 明确承担这一风险", name)
		}
	}
	for name, target := range c.S3Targets {
		if target.Endpoint == "" {
			return fmt.Errorf("S3 目标 %s 缺少 endpoint", name)
		}
		if target.Bucket == "" {
			return fmt.Errorf("S3 目标 %s 缺少 bucket", name)
		}
		if target.AccessKey == "" || target.SecretKey == "" {
			return fmt.Errorf("S3 目标 %s 缺少 access_key 或 secret_key", name)
		}
		if !target.Secure && !c.allowInsecureS3 {
			return fmt.Errorf("S3 目标 %s 使用 http：凭据与流量都是明文，"+
				"请改用 https（secure=true），或在配置中设置 \"allow_insecure_s3\": true 明确承担这一风险", name)
		}
	}
	for name, target := range c.WebDAVTargets {
		if target.Endpoint == "" {
			return fmt.Errorf("WebDAV 目标 %s 缺少 endpoint", name)
		}
		if !strings.HasPrefix(target.Endpoint, "https://") && !isLoopbackURL(target.Endpoint) {
			return fmt.Errorf("WebDAV 目标 %s 的 endpoint 必须是 https（本地联调可用 http://127.0.0.1）", name)
		}
	}
	for name, target := range c.SFTPTargets {
		if target.Addr == "" {
			return fmt.Errorf("SFTP 目标 %s 缺少 addr", name)
		}
		if target.User == "" {
			return fmt.Errorf("SFTP 目标 %s 缺少 user", name)
		}
		if target.Auth.PrivateKeyFile == "" && target.Auth.AgentSocket == "" && target.Auth.Password == "" {
			return fmt.Errorf("SFTP 目标 %s 没有配置任何认证方式（private_key_file、agent_socket 或 password）", name)
		}
		// 主机密钥校验是 SFTP 相对明文 FTP 的核心安全保证，三种都不配时
		// 必须报错而不是退回"跳过校验"。
		if target.HostKeySHA256 == "" && target.HostKeyFile == "" && !target.InsecureIgnoreHostKey {
			return fmt.Errorf("SFTP 目标 %s 没有配置主机密钥校验：请设置 host_key_sha256 或 host_key_file；"+
				"确需跳过请显式声明 insecure_ignore_host_key: true", name)
		}
	}
	for name, target := range c.NotifyTargets {
		if target.URL == "" {
			return fmt.Errorf("通知目标 %s 缺少 url", name)
		}
		if !strings.HasPrefix(target.URL, "https://") && !isLoopbackURL(target.URL) {
			// 回调会带上任务 ID 与失败原因等业务信息，明文 HTTP 等于把它
			// 交给链路上的任何人。本地联调用回环地址豁免。
			return fmt.Errorf("通知目标 %s 的 url 必须是 https（本地联调可用 http://127.0.0.1）", name)
		}
	}
	return nil
}

func isLoopbackURL(url string) bool {
	return strings.HasPrefix(url, "http://127.0.0.1") ||
		strings.HasPrefix(url, "http://[::1]") ||
		strings.HasPrefix(url, "http://localhost")
}

// Allowlist 构造 URL 输入的访问白名单。
//
// 列表为空时返回一个"拒绝一切"的 List，而不是 nil：convertersvc 用 nil 表示
// "服务未启用 URL 输入"，两者语义一致但更明确的是直接给一个空列表。
func (c *Config) Allowlist() (*allowlist.List, error) {
	return allowlist.Parse(c.AllowInputURLHosts)
}

// NotifyRegistry 把配置里的目标转成 notify 注册表。
func (c *Config) NotifyRegistry() *notify.Registry {
	targets := make(map[string]notify.Target, len(c.NotifyTargets))
	for name, target := range c.NotifyTargets {
		timeout := notify.DefaultTimeout
		if target.TimeoutSeconds > 0 {
			timeout = time.Duration(target.TimeoutSeconds) * time.Second
		}
		targets[name] = notify.Target{
			URL:     target.URL,
			Secret:  target.Secret,
			Events:  target.Events,
			Timeout: timeout,
		}
	}
	return notify.NewRegistry(targets)
}

// FTPSinks 把配置里的 FTP 目标转成可用的 Sink 集合。
func (c *Config) FTPSinks() (map[string]*transfer.FTPSink, error) {
	sinks := make(map[string]*transfer.FTPSink, len(c.FTPTargets))
	for name, target := range c.FTPTargets {
		sink := &transfer.FTPSink{
			Addr:      target.Addr,
			User:      target.User,
			Password:  target.Password,
			BaseDir:   target.BaseDir,
			Insecure:  target.Insecure,
			Implicit:  target.Implicit,
			MaxBytes:  target.MaxBytes,
			Overwrite: target.Overwrite,
		}
		if target.TimeoutSeconds > 0 {
			sink.Timeout = time.Duration(target.TimeoutSeconds) * time.Second
		}
		// 明文 FTP：既不设 TLSConfig 也不设 Implicit，sink 侧会拒绝，
		// 这里靠 allow_insecure_ftp 已经放行过一次。
		if !target.Insecure {
			tlsConfig := &tls.Config{
				ServerName:         hostOf(target.Addr),
				InsecureSkipVerify: target.InsecureSkipVerify, //nolint:gosec // 配置显式声明
				MinVersion:         tls.VersionTLS12,
			}
			sink.TLSConfig = tlsConfig
		}
		sinks[name] = sink
	}
	return sinks, nil
}

// hostOf 从 host:port 里取出主机名，用于 TLS SNI 与证书校验。
func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// S3Sinks 把配置里的对象存储目标转成可用的 Sink 集合。
func (c *Config) S3Sinks() map[string]*transfer.MinioSink {
	sinks := make(map[string]*transfer.MinioSink, len(c.S3Targets))
	for name, target := range c.S3Targets {
		sink := &transfer.MinioSink{
			Endpoint:     target.Endpoint,
			Secure:       target.Secure,
			Region:       target.Region,
			AccessKey:    target.AccessKey,
			SecretKey:    target.SecretKey,
			SessionToken: target.SessionToken,
			Bucket:       target.Bucket,
			BasePrefix:   target.Prefix,
			MaxBytes:     target.MaxBytes,
			Overwrite:    target.Overwrite,
		}
		if target.TimeoutSeconds > 0 {
			sink.Timeout = time.Duration(target.TimeoutSeconds) * time.Second
		}
		sinks[name] = sink
	}
	return sinks
}

// WebDAVSinks 把配置里的 WebDAV 目标转成可用的 Sink 集合。
func (c *Config) WebDAVSinks() map[string]*transfer.WebDAVSink {
	sinks := make(map[string]*transfer.WebDAVSink, len(c.WebDAVTargets))
	for name, target := range c.WebDAVTargets {
		sink := &transfer.WebDAVSink{
			Endpoint:  target.Endpoint,
			User:      target.User,
			Password:  target.Password,
			BaseDir:   target.BaseDir,
			MaxBytes:  target.MaxBytes,
			Overwrite: target.Overwrite,
		}
		if target.TimeoutSeconds > 0 {
			sink.Timeout = time.Duration(target.TimeoutSeconds) * time.Second
		}
		sinks[name] = sink
	}
	return sinks
}

// SFTPSinks 把配置里的 SFTP 目标转成可用的 Sink 集合。
func (c *Config) SFTPSinks() map[string]*transfer.SFTPSink {
	sinks := make(map[string]*transfer.SFTPSink, len(c.SFTPTargets))
	for name, target := range c.SFTPTargets {
		sink := &transfer.SFTPSink{
			Addr:    target.Addr,
			User:    target.User,
			BaseDir: target.BaseDir,
			Auth: transfer.SFTPAuth{
				PrivateKeyFile:       target.Auth.PrivateKeyFile,
				PrivateKeyPassphrase: target.Auth.PrivateKeyPassphrase,
				AgentSocket:          target.Auth.AgentSocket,
				Password:             target.Auth.Password,
			},
			HostKeySHA256:         target.HostKeySHA256,
			HostKeyFile:           target.HostKeyFile,
			InsecureIgnoreHostKey: target.InsecureIgnoreHostKey,
			MaxBytes:              target.MaxBytes,
			Overwrite:             target.Overwrite,
		}
		if target.TimeoutSeconds > 0 {
			sink.Timeout = time.Duration(target.TimeoutSeconds) * time.Second
		}
		sinks[name] = sink
	}
	return sinks
}
