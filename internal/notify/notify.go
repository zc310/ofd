// Package notify 负责把任务状态变化投递到预注册的 Webhook 目标。
//
// 设计要点：调用方只能提供目标名（Target），URL 与密钥都保存在服务端的注册表里。
// 这不是省事，而是安全边界——如果回调 URL 由调用方指定，它就能被当作探测内网、
// 盗取凭证的通道；配套的 SSRF 防护与出网白名单也随之省掉。
//
// 投递语义是 at-least-once：网络超时等服务端无法判定结果的场景下，接收方可能收到
// 重复请求。因此每条通知带稳定的 X-OFD-Delivery 头，接收方据此幂等去重。
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Target 是一个已注册的通知目标。
type Target struct {
	// URL 完整回调地址。仅服务端可见。
	URL string
	// Secret 用于 HMAC-SHA256 签名的密钥；为空表示不签名（仅建议本地调试用）。
	Secret string
	// Events 订阅的事件名白名单，空表示订阅全部。
	Events []string
	// Timeout 单次投递超时，0 时取 DefaultTimeout。
	Timeout time.Duration
}

// subscribes 判断目标是否订阅该事件。
func (t Target) subscribes(event string) bool { return Subscribes(t.Events, event) }

// DefaultTimeout 是单次投递的默认超时。
//
// 刻意比一般 HTTP 客户端短：通知是旁路，接收方卡住不应该拖住队列。超过这个时间
// 就当作失败退避重试，反正 at-least-once 允许重复。
const DefaultTimeout = 10 * time.Second

// 事件名。
const (
	EventSucceeded = "succeeded"
	EventFailed    = "failed"
	// EventWildcard 在订阅列表里表示"全部事件"，与配置侧同一套写法。
	EventWildcard = "*"
)

// KnownEvent 判断 name 是不是受支持的事件名。
//
// 供提交阶段校验调用方的 notify.events 用。接线之后一个拼错的事件名会让该
// 任务一条通知都收不到，而调用方唯一的线索是"没收到"——所以拼错必须在提交
// 阶段就报错，不能等到投递时静默过滤掉。
func KnownEvent(name string) bool {
	switch name {
	case EventSucceeded, EventFailed, EventWildcard:
		return true
	}
	return false
}

// Subscribes 判断订阅列表是否覆盖该事件。
//
// 列表为空表示不收窄——这正是 notify.events 与 Target.Events 共用的语义：
// 留空则沿用目标自己的订阅集合。
//
// 两个来源都过这一处：两个字段同名同义，各写一份判定迟早漂移。
func Subscribes(events []string, event string) bool {
	if len(events) == 0 {
		return true
	}
	for _, name := range events {
		if name == event || name == EventWildcard {
			return true
		}
	}
	return false
}

// 投递所用的请求头。
const (
	HeaderDelivery   = "X-OFD-Delivery"
	HeaderEvent      = "X-OFD-Event"
	HeaderJob        = "X-OFD-Job"
	HeaderTimestamp  = "X-OFD-Timestamp"
	HeaderSignature  = "X-OFD-Signature"
	SignatureVersion = "v1"
)

// Request 是一次投递所需的全部信息。
type Request struct {
	// Target 是已注册的目标名，不是 URL。
	Target string
	// ID 稳定唯一，作为去重标识。
	ID string
	// JobID 关联的转换任务。
	JobID string
	// Event 见 EventSucceeded / EventFailed。
	Event string
	// Payload 请求体，会原样发送。
	Payload []byte
	// Attempt 当前是第几次尝试，从 1 开始。
	Attempt int
}

// ErrTargetNotFound 表示目标未注册。调用方据此拒绝请求，而不是静默丢弃。
var ErrTargetNotFound = errors.New("通知目标未注册")

// ErrEventNotSubscribed 表示目标未订阅该事件。这不算错误：目标就是不要这个事件。
var ErrEventNotSubscribed = errors.New("通知目标未订阅该事件")

// Registry 持有已注册的目标。注册后只读，构造完成后可被多个投递协程共享。
type Registry struct {
	targets map[string]Target
}

// NewRegistry 注册目标。同名目标会覆盖，便于配置热更新。
func NewRegistry(targets map[string]Target) *Registry {
	copied := make(map[string]Target, len(targets))
	for name, target := range targets {
		copied[name] = target
	}
	return &Registry{targets: copied}
}

// Lookup 返回目标。
func (r *Registry) Lookup(name string) (Target, bool) {
	if r == nil {
		return Target{}, false
	}
	target, ok := r.targets[name]
	return target, ok
}

// Names 返回全部已注册的目标名，供管理接口列举。
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.targets))
	for name := range r.targets {
		names = append(names, name)
	}
	return names
}

// Signer 生成 Webhook 签名头。
//
// 签名覆盖 时间戳 + "." + 原始请求体，接收方用同一密钥重算并常量时间比较。
// 时间戳让同一份请求体无法被无限期重放。
func Signer(secret string, body []byte, timestamp time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s.%d.", SignatureVersion, timestamp.Unix())
	mac.Write(body)
	return SignatureVersion + "=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify 校验签名，供接收方或测试使用。
func Verify(secret string, header string, body []byte, timestamp time.Time) bool {
	want := Signer(secret, body, timestamp)
	return hmac.Equal([]byte(want), []byte(header))
}

// Deliverer 按注册表投递通知。
type Deliverer struct {
	registry *Registry
	client   *http.Client
	now      func() time.Time
}

// NewDeliverer 构造投递器。client 为 nil 时按目标超时创建客户端。
func NewDeliverer(registry *Registry, client *http.Client) *Deliverer {
	if client == nil {
		client = &http.Client{}
	}
	return &Deliverer{registry: registry, client: client, now: time.Now}
}

// SetClock 替换时钟，仅用于测试。
func (d *Deliverer) SetClock(now func() time.Time) { d.now = now }

// Deliver 投递一条通知。
//
// 重试由调用方（jobstore）负责：这里只区分可重试与不可重试的失败，
// 不可重试的 4xx 会终止重试，把消息直接判死。
func (d *Deliverer) Deliver(ctx context.Context, req Request) error {
	if req.ID == "" {
		return errors.New("投递缺少通知 ID")
	}
	if req.Event == "" {
		return errors.New("投递缺少事件名")
	}
	target, ok := d.registry.Lookup(req.Target)
	if !ok {
		return fmt.Errorf("%w: %s", ErrTargetNotFound, req.Target)
	}
	if !target.subscribes(req.Event) {
		return fmt.Errorf("%w: %s", ErrEventNotSubscribed, req.Event)
	}
	body := req.Payload
	if len(body) == 0 {
		body = []byte("{}")
	}
	now := d.now().UTC()
	// Attempt 从 1 开始，缺失时补 1，接收方据此判断重试。
	attempt := req.Attempt
	if attempt < 1 {
		attempt = 1
	}
	// 超时按目标算，不共用 client 的那个：不同回调方该有不同的耐心。
	// 用 context 而不是每个目标一个 http.Client，是为了复用连接池——
	// client 数量会随目标数增长，而连接复用只在同一个 client 内生效。
	timeout := target.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, target.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set(HeaderDelivery, req.ID)
	httpReq.Header.Set(HeaderEvent, req.Event)
	httpReq.Header.Set(HeaderJob, req.JobID)
	httpReq.Header.Set(HeaderTimestamp, strconv.FormatInt(now.Unix(), 10))
	httpReq.Header.Set("User-Agent", "ofd-server-webhook/1")
	if target.Secret != "" {
		httpReq.Header.Set(HeaderSignature, Signer(target.Secret, body, now))
	}
	resp, err := d.client.Do(httpReq)
	if err != nil {
		// 网络错误一律可重试。
		return &Error{Delivery: req.ID, Retryable: true, Err: err}
	}
	defer resp.Body.Close()
	// 必须读到并关闭，否则连接无法复用；读取量有上限，防止接收方回 giant body。
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return &Error{
		Delivery:  req.ID,
		Retryable: retryableStatus(resp.StatusCode),
		Status:    resp.StatusCode,
	}
}

// 4xx（除 408/429）表示请求本身有问题，重试同样的请求不会有不同结果。
// 5xx、408、429 可能是接收方的暂时状态，值得重试。
func retryableStatus(status int) bool {
	if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests {
		return true
	}
	return status >= 500
}

// Error 是投递失败。
type Error struct {
	Delivery  string
	Status    int
	Retryable bool
	Err       error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("通知投递失败")
	// 带上通知 ID，让错误自描述：这个值经常被直接写进日志，
	// 少了它就得回调用处交叉比对 delivery 记录才能知道是哪条通知。
	if e.Delivery != "" {
		b.WriteString(" [")
		b.WriteString(e.Delivery)
		b.WriteString("]")
	}
	if e.Status != 0 {
		fmt.Fprintf(&b, " HTTP %d", e.Status)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

// Retryable 报告该失败是否值得重试。
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	// 目标未注册/未订阅是配置问题，重试多少次结果都一样，必须立刻判死，
	// 否则这条通知会一直占用重试配额直到 abandoned。
	if errors.Is(err, ErrTargetNotFound) || errors.Is(err, ErrEventNotSubscribed) {
		return false
	}
	var deliveryErr *Error
	if errors.As(err, &deliveryErr) {
		return deliveryErr.Retryable
	}
	// 未分类的错误保守重试：通知本身允许重复，漏发比多发更糟。
	return true
}
