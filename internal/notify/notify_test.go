package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSecret = "s3cret"

func newTestDeliverer(t *testing.T, targets map[string]Target) *Deliverer {
	t.Helper()
	return NewDeliverer(NewRegistry(targets), &http.Client{Timeout: 3 * time.Second})
}

func TestDeliverSuccess(t *testing.T) {
	var got struct {
		body      []byte
		delivery  string
		event     string
		job       string
		timestamp string
		signature string
		ctype     string
		agent     string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		got.body = buf[:n]
		got.delivery = r.Header.Get(HeaderDelivery)
		got.event = r.Header.Get(HeaderEvent)
		got.job = r.Header.Get(HeaderJob)
		got.timestamp = r.Header.Get(HeaderTimestamp)
		got.signature = r.Header.Get(HeaderSignature)
		got.ctype = r.Header.Get("Content-Type")
		got.agent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := newTestDeliverer(t, map[string]Target{
		"erp": {URL: srv.URL, Secret: testSecret},
	})
	body := []byte(`{"job_id":"j1","status":"succeeded"}`)
	if err := d.Deliver(context.Background(), Request{
		Target: "erp", ID: "d1", JobID: "j1", Event: EventSucceeded, Payload: body, Attempt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if string(got.body) != string(body) {
		t.Errorf("请求体不对: %s", got.body)
	}
	if got.delivery != "d1" || got.event != EventSucceeded || got.job != "j1" {
		t.Errorf("头部不对: delivery=%q event=%q job=%q", got.delivery, got.event, got.job)
	}
	if got.ctype != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got.ctype)
	}
	if !strings.HasPrefix(got.agent, "ofd-server-webhook/") {
		t.Errorf("User-Agent = %q", got.agent)
	}
	// 签名必须能用同一密钥验过，且换密钥/换请求体都不通过。
	ts, err := strconv.ParseInt(got.timestamp, 10, 64)
	if err != nil {
		t.Fatalf("时间戳不合法: %q", got.timestamp)
	}
	when := time.Unix(ts, 0)
	if !Verify(testSecret, got.signature, body, when) {
		t.Error("签名校验不通过")
	}
	if Verify("other-secret", got.signature, body, when) {
		t.Error("换密钥后不应通过")
	}
	if Verify(testSecret, got.signature, []byte("tampered"), when) {
		t.Error("请求体被篡改后不应通过")
	}
}

// 签名带时间戳，同样的请求体换个时间戳签名就不同，防止长期重放。
func TestSignatureBindsTimestamp(t *testing.T) {
	body := []byte(`{"a":1}`)
	base := time.Unix(1750000000, 0)
	sig := Signer(testSecret, body, base)
	if !Verify(testSecret, sig, body, base) {
		t.Error("同时间戳应通过")
	}
	if Verify(testSecret, sig, body, base.Add(time.Hour)) {
		t.Error("不同时间戳不应通过")
	}
}

func TestDeliverUnknownTarget(t *testing.T) {
	d := newTestDeliverer(t, map[string]Target{"erp": {URL: "http://127.0.0.1:1/"}})
	err := d.Deliver(context.Background(), Request{Target: "nope", ID: "d1", Event: EventSucceeded})
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("未注册目标应报 ErrTargetNotFound，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("错误信息应带目标名: %v", err)
	}
	// 未注册目标绝不能被当成可重试错误，否则会一直重试到天荒地老。
	if Retryable(err) {
		t.Error("未注册目标不应重试")
	}
}

func TestDeliverEventFilter(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := newTestDeliverer(t, map[string]Target{
		"only-fail": {URL: srv.URL, Events: []string{EventFailed}},
	})
	if err := d.Deliver(context.Background(), Request{Target: "only-fail", ID: "d1", Event: EventSucceeded}); !errors.Is(err, ErrEventNotSubscribed) {
		t.Errorf("未订阅事件应报 ErrEventNotSubscribed，实际 %v", err)
	}
	if err := d.Deliver(context.Background(), Request{Target: "only-fail", ID: "d2", Event: EventFailed}); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("命中次数 = %d，期望 1", hits)
	}
	// "*" 通配与空列表都表示订阅全部。
	for _, events := range [][]string{nil, {"*"}} {
		d := newTestDeliverer(t, map[string]Target{"all": {URL: srv.URL, Events: events}})
		if err := d.Deliver(context.Background(), Request{Target: "all", ID: "d3", Event: EventSucceeded}); err != nil {
			t.Errorf("events=%v 应订阅全部: %v", events, err)
		}
	}
}

func TestDeliverStatusClassification(t *testing.T) {
	var code int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
	}))
	defer srv.Close()
	d := newTestDeliverer(t, map[string]Target{"erp": {URL: srv.URL, Secret: testSecret}})

	// 2xx 是成功，不产生错误。
	for _, status := range []int{200, 202, 204} {
		code = status
		if err := d.Deliver(context.Background(), Request{Target: "erp", ID: "d1", Event: EventSucceeded}); err != nil {
			t.Errorf("状态 %d 应视为成功，实际 %v", status, err)
		}
	}
	cases := []struct {
		status    int
		retryable bool
	}{
		{301, false}, {400, false}, {401, false}, {403, false}, {404, false}, {410, false},
		{408, true}, {429, true},
		{500, true}, {502, true}, {503, true}, {504, true},
	}
	for _, tc := range cases {
		code = tc.status
		err := d.Deliver(context.Background(), Request{Target: "erp", ID: "d1", Event: EventSucceeded})
		if err == nil {
			t.Errorf("状态 %d 应视为失败", tc.status)
			continue
		}
		if got := Retryable(err); got != tc.retryable {
			t.Errorf("状态 %d 可重试 = %v，期望 %v", tc.status, got, tc.retryable)
		}
		if !strings.Contains(err.Error(), fmt.Sprint(tc.status)) && tc.status >= 400 {
			t.Errorf("状态 %d 的错误信息应带状态码: %v", tc.status, err)
		}
	}
}

func TestDeliverNetworkErrorIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // 立刻关掉，制造连接被拒
	d := newTestDeliverer(t, map[string]Target{"erp": {URL: url}})
	err := d.Deliver(context.Background(), Request{Target: "erp", ID: "d1", Event: EventSucceeded})
	if err == nil {
		t.Fatal("连接失败应有错误")
	}
	if !Retryable(err) {
		t.Error("网络错误应可重试")
	}
}

func TestDeliverContextCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	d := newTestDeliverer(t, map[string]Target{"erp": {URL: srv.URL}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := d.Deliver(ctx, Request{Target: "erp", ID: "d1", Event: EventSucceeded}); err == nil {
		t.Fatal("超时应报错")
	}
}

// 没有配置密钥时仍可投递，只是不带签名头——便于本地联调，但正式环境应始终配密钥。
func TestDeliverWithoutSecret(t *testing.T) {
	var sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig = r.Header.Get(HeaderSignature)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := newTestDeliverer(t, map[string]Target{"erp": {URL: srv.URL}})
	if err := d.Deliver(context.Background(), Request{Target: "erp", ID: "d1", Event: EventSucceeded}); err != nil {
		t.Fatal(err)
	}
	if sig != "" {
		t.Errorf("未配密钥时不应有签名头，实际 %q", sig)
	}
}

func TestDeliverValidatesInput(t *testing.T) {
	d := newTestDeliverer(t, map[string]Target{"erp": {URL: "http://127.0.0.1:1/"}})
	if err := d.Deliver(context.Background(), Request{ID: "d1", Event: EventSucceeded}); err == nil {
		t.Error("缺目标名应报错")
	}
	if err := d.Deliver(context.Background(), Request{Target: "erp", Event: EventSucceeded}); err == nil {
		t.Error("缺 ID 应报错")
	}
	if err := d.Deliver(context.Background(), Request{Target: "erp", ID: "d1"}); err == nil {
		t.Error("缺事件名应报错")
	}
}

// 空负载退化为 {}，避免接收方拿到 0 字节请求体后解析失败。
func TestDeliverEmptyPayload(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		body = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := newTestDeliverer(t, map[string]Target{"erp": {URL: srv.URL}})
	if err := d.Deliver(context.Background(), Request{Target: "erp", ID: "d1", Event: EventSucceeded}); err != nil {
		t.Fatal(err)
	}
	if body != "{}" {
		t.Errorf("空负载应发 {}，实际 %q", body)
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry(map[string]Target{"a": {URL: "http://a"}, "b": {URL: "http://b"}})
	if _, ok := r.Lookup("a"); !ok {
		t.Error("a 应存在")
	}
	if _, ok := r.Lookup("zzz"); ok {
		t.Error("zzz 不应存在")
	}
	if len(r.Names()) != 2 {
		t.Errorf("Names = %v", r.Names())
	}
	// nil 注册表不应 panic。
	var nilReg *Registry
	if _, ok := nilReg.Lookup("a"); ok {
		t.Error("nil 注册表不应命中")
	}
	if nilReg.Names() != nil {
		t.Error("nil 注册表 Names 应为 nil")
	}
}

// 接收方回一个巨大的响应体也不应把内存吃光。
func TestDeliverIgnoresGiantResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		chunk := strings.Repeat("x", 64<<10)
		for i := 0; i < 64; i++ {
			fmt.Fprint(w, chunk)
		}
	}))
	defer srv.Close()
	d := newTestDeliverer(t, map[string]Target{"erp": {URL: srv.URL}})
	if err := d.Deliver(context.Background(), Request{Target: "erp", ID: "d1", Event: EventSucceeded}); err != nil {
		t.Fatal(err)
	}
}

func TestDeliverConcurrent(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get(HeaderDelivery)]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := newTestDeliverer(t, map[string]Target{"erp": {URL: srv.URL, Secret: testSecret}})

	const n = 60
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- d.Deliver(context.Background(), Request{
				Target: "erp", ID: fmt.Sprintf("d%d", i), Event: EventSucceeded, Payload: []byte(`{}`),
			})
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("并发投递失败: %v", err)
		}
	}
	if len(seen) != n {
		t.Errorf("收到 %d 个去重 ID，期望 %d", len(seen), n)
	}
}

// 每个目标的投递超时必须真的生效。曾经的 bug：Target.Timeout 只有注释和配置项，
// 从来没被读过，于是配置里写的 timeout_seconds 完全不起作用。
//
// 这里只测"目标自带的超时优先于 client 的超时"；DefaultTimeout 那条路径要等
// 10 秒，不适合放进单元测试。
func TestPerTargetTimeoutOverridesClient(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // 一直不返回，逼客户端超时
	}))
	// defer 是 LIFO：先注册 Close 再注册 close(release)，退出时才会先放开
	// handler 再关服务，否则 srv.Close() 会一直等这些连接。
	defer srv.Close()
	defer close(release)

	// 目标的 50ms 应当先于 client 的 3s 生效。
	d := newTestDeliverer(t, map[string]Target{
		"慢": {URL: srv.URL, Timeout: 50 * time.Millisecond},
	})
	started := time.Now()
	err := d.Deliver(context.Background(), Request{Target: "慢", ID: "d1", Event: EventSucceeded})
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("应因超时失败")
	}
	if elapsed > 2*time.Second {
		t.Errorf("目标超时未生效，耗时 %v（client 超时是 3s，说明退化成了 client 的）", elapsed)
	}
	if !Retryable(err) {
		t.Error("超时属于可重试错误")
	}
	// 超时错误要带上目标信息，便于排查是谁的回调有问题。
	if !strings.Contains(err.Error(), "d1") {
		t.Errorf("错误应带通知 ID: %v", err)
	}
}
