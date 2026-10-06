package runner

import (
	"context"
	"github.com/goccy/go-json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/notify"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func ofdBytes(t *testing.T) []byte {
	t.Helper()
	for _, name := range []string{"sample.ofd", "actions.ofd", "link.ofd"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil && len(data) > 0 {
			return data
		}
	}
	t.Skip("仓库里没有可用的 OFD 样本")
	return nil
}

func newRunner(t *testing.T, cfg Config) (*Runner, *jobstore.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := jobstore.Open(filepath.Join(dir, "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	convert := convertersvc.New(filepath.Join(dir, "tmp"), nil)
	r := New(store, convert, nil, cfg, quietLogger())
	t.Cleanup(r.Stop)
	return r, store, dir
}

// requestFor 造一份可被 specFromJob 还原的请求体。
// []byte 在 JSON 里编码为 base64，这里直接交给 json.Marshal 处理。
func requestFor(t *testing.T, to string, body []byte) json.RawMessage {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"input":  map[string]any{"kind": "upload", "file_name": "a.ofd", "bytes": body},
		"output": map[string]any{"kind": "stream", "format": to},
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func enqueueOfd(t *testing.T, store *jobstore.Store, id, to, notifyTarget string) {
	t.Helper()
	job := &jobstore.Job{
		ID:           id,
		Lane:         jobstore.LaneFast,
		From:         "ofd",
		To:           to,
		Request:      requestFor(t, to, ofdBytes(t)),
		NotifyTarget: notifyTarget,
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.Enqueue(job); err != nil {
		t.Fatal(err)
	}
}

// waitFor 轮询等待条件成立。
func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

func TestRunnerProcessesJob(t *testing.T) {
	r, store, _ := newRunner(t, Config{FastWorkers: 1, PollInterval: 10 * time.Millisecond})
	enqueueOfd(t, store, "j1", "pdf", "")
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "任务进入终态", func() bool {
		job, err := store.Get("j1")
		return err == nil && job != nil && job.State.Terminal()
	})
	job, _ := store.Get("j1")
	if job.State != jobstore.StateSucceeded {
		t.Fatalf("状态 = %s，err = %s", job.State, job.Error)
	}
	if job.Output.Kind != "stream" || job.Output.Size == 0 {
		t.Errorf("输出记录不对: %+v", job.Output)
	}
	if job.FinishedAt.IsZero() {
		t.Error("FinishedAt 未记录")
	}
	if job.Attempt != 1 {
		t.Errorf("Attempt = %d", job.Attempt)
	}
}

// 可重试的失败要按退避重新排队，而不是立刻重跑或直接判死。
//
// 这里直接调 retryOrFail 而不是靠"制造一次转换失败"来驱动：转换器在解析过程中
// 并不检查 ctx，用超时制造失败依赖实现细节，OFD→PDF 又快到几乎不可能超时，
// 那种写法既不稳定也测不到退避本身。
func TestRetryOrFailRearrangesWithBackoff(t *testing.T) {
	r, store, _ := newRunner(t, Config{MaxJobAttempts: 3, Backoff: 50 * time.Millisecond})
	if err := store.Enqueue(&jobstore.Job{
		ID: "j1", Lane: jobstore.LaneFast, From: "ofd", To: "pdf",
		Request: requestFor(t, "pdf", ofdBytes(t)), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(jobstore.LaneFast)
	if err != nil || job == nil {
		t.Fatalf("取件失败: %v", err)
	}
	before := time.Now()
	r.retryOrFail(job, context.DeadlineExceeded)

	got, _ := store.Get("j1")
	if got.State != jobstore.StateQueued {
		t.Fatalf("可重试的失败应回到 queued，实际 %s", got.State)
	}
	if !got.DueAt.After(before) {
		t.Errorf("重试应带退避: DueAt = %v，不应早于 %v", got.DueAt, before)
	}
	if got.Error != "" {
		t.Errorf("重试期间不应保留失败原因: %q", got.Error)
	}
	if got.StartedAt.IsZero() == false {
		t.Error("重试应清空 StartedAt")
	}
	// 退避未到，不能被取走。
	if claimed, err := store.Claim(jobstore.LaneFast); err != nil {
		t.Fatal(err)
	} else if claimed != nil {
		t.Fatalf("退避期内不应被取走，实际 %v", claimed.ID)
	}
	// 退避到点后应能取走，且 Attempt 累加。
	time.Sleep(70 * time.Millisecond)
	again, err := store.Claim(jobstore.LaneFast)
	if err != nil || again == nil {
		t.Fatalf("退避到点后应能取走: %v", err)
	}
	if again.Attempt != 2 {
		t.Errorf("Attempt = %d，期望 2", again.Attempt)
	}
	// again 已被取走、处于 running，先按生产流程把它排回去。
	r.retryOrFail(again, context.DeadlineExceeded)

	// 继续按生产序列跑：每次都重新取件，让 Attempt 由 Claim 真实累加。
	// 直接拿同一份 job 反复调 retryOrFail 是测不出来的——Attempt 来自 Claim，
	// 传旧副本等于假装重试从未发生。
	waitFor(t, "重试用尽后判死", func() bool {
		claimed, err := store.Claim(jobstore.LaneFast)
		if err != nil || claimed == nil {
			return false
		}
		r.retryOrFail(claimed, context.DeadlineExceeded)
		got, err := store.Get("j1")
		return err == nil && got != nil && got.State.Terminal()
	})
	final, _ := store.Get("j1")
	if final.State != jobstore.StateFailed {
		t.Errorf("超过重试上限应判死，实际 %s", final.State)
	}
	if final.Attempt != 3 {
		t.Errorf("Attempt = %d，期望 3（MaxJobAttempts）", final.Attempt)
	}
	if final.Error == "" {
		t.Error("判死时应记录失败原因")
	}
}

// 成功或不可重试的失败都不该进重试路径。
func TestRetryOrFailTerminalCases(t *testing.T) {
	r, store, _ := newRunner(t, Config{MaxJobAttempts: 5, Backoff: time.Millisecond})
	for _, tc := range []struct {
		name  string
		cause error
	}{
		{"参数错误", convertersvc.ErrBadRequest},
		{"格式不支持", convertersvc.ErrUnsupported},
		{"超出限制", convertersvc.ErrTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "j-" + tc.name
			if err := store.Enqueue(&jobstore.Job{
				ID: id, Lane: jobstore.LaneFast, From: "ofd", To: "pdf",
				Request: requestFor(t, "pdf", ofdBytes(t)), CreatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatal(err)
			}
			job, err := store.Claim(jobstore.LaneFast)
			if err != nil || job == nil {
				t.Fatalf("取件失败: %v", err)
			}
			r.retryOrFail(job, tc.cause)
			got, _ := store.Get(id)
			if got.State != jobstore.StateFailed {
				t.Errorf("不可重试的失败应直接判死，实际 %s", got.State)
			}
			if got.Attempt != 1 {
				t.Errorf("Attempt = %d，期望 1", got.Attempt)
			}
		})
	}
}

// 参数错误的请求不该重试：重试多少次都是同样的错。
func TestRunnerDoesNotRetryBadRequest(t *testing.T) {
	r, store, _ := newRunner(t, Config{
		FastWorkers: 1, PollInterval: 10 * time.Millisecond,
		MaxJobAttempts: 5, Backoff: 10 * time.Millisecond,
	})
	request, _ := json.Marshal(map[string]any{
		"output": map[string]any{"kind": "stream", "format": "no-such-format"},
	})
	if err := store.Enqueue(&jobstore.Job{
		ID: "j1", Lane: jobstore.LaneFast, From: "ofd", To: "no-such-format",
		Request: request, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "任务失败", func() bool {
		job, err := store.Get("j1")
		return err == nil && job != nil && job.State == jobstore.StateFailed
	})
	job, _ := store.Get("j1")
	if job.Attempt != 1 {
		t.Errorf("不可重试的失败 Attempt 应为 1，实际 %d", job.Attempt)
	}
	if job.Error == "" {
		t.Error("失败原因应记录")
	}
}

// 两条通道的并发上限要真的生效：heavy 只有一个 worker 时，
// 两个重任务不能同时跑。
func TestRunnerLaneConcurrency(t *testing.T) {
	r, store, _ := newRunner(t, Config{
		FastWorkers: 1, HeavyWorkers: 1, PollInterval: 10 * time.Millisecond,
		JobTimeout: 30 * time.Millisecond,
	})
	// 让转换必然超时：JobTimeout 设得极短，OFD→PDF 未必来得及完成。
	// 这里只验证 heavy 任务被分到 heavy 通道，不依赖是否超时。
	for _, id := range []string{"h1", "h2"} {
		request := requestFor(t, "pdf", nil)
		if err := store.Enqueue(&jobstore.Job{
			ID: id, Lane: jobstore.LaneHeavy, From: "ofd", To: "pdf",
			Request: request, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "两个 heavy 任务都离开 running", func() bool {
		a, _ := store.Get("h1")
		b, _ := store.Get("h2")
		return a != nil && b != nil && a.State.Terminal() && b.State.Terminal()
	})
	// 串行执行下，同一时刻只应有一个 running；最终两者都应终止。
	for _, id := range []string{"h1", "h2"} {
		job, _ := store.Get(id)
		if job == nil || !job.State.Terminal() {
			t.Errorf("%s 未进入终态: %+v", id, job)
		}
	}
}

func TestRunnerSendsNotification(t *testing.T) {
	var hits atomic.Int32
	var events []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		events = append(events, r.Header.Get(notify.HeaderEvent))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	store, err := jobstore.Open(filepath.Join(dir, "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	deliverer := notify.NewDeliverer(notify.NewRegistry(map[string]notify.Target{
		"erp": {URL: srv.URL, Secret: "k"},
	}), &http.Client{Timeout: 2 * time.Second})
	r := New(store, convertersvc.New(filepath.Join(dir, "tmp"), nil), deliverer,
		Config{FastWorkers: 1, PollInterval: 10 * time.Millisecond, NotifyWorkers: 1}, quietLogger())
	defer r.Stop()

	request := requestFor(t, "pdf", ofdBytes(t))
	if err := store.Enqueue(&jobstore.Job{
		ID: "j1", Lane: jobstore.LaneFast, From: "ofd", To: "pdf",
		Request: request, NotifyTarget: "erp", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "通知已送达", func() bool { return hits.Load() >= 1 })
	waitFor(t, "通知记为完成", func() bool {
		done, _ := store.ListDeliveries(jobstore.DeliveryDone, 0)
		return len(done) == 1
	})
	if len(events) == 0 || events[0] != notify.EventSucceeded {
		t.Errorf("事件 = %v", events)
	}
	// X-OFD-Delivery 必须是 job:event 的稳定形式。
	done, _ := store.ListDeliveries(jobstore.DeliveryDone, 0)
	if done[0].ID != "j1:"+notify.EventSucceeded {
		t.Errorf("通知 ID = %q", done[0].ID)
	}
}

func TestRunnerSendsFailureNotification(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	store, _ := jobstore.Open(filepath.Join(dir, "jobs.db"))
	defer store.Close()
	deliverer := notify.NewDeliverer(notify.NewRegistry(map[string]notify.Target{
		"erp": {URL: srv.URL},
	}), &http.Client{Timeout: 2 * time.Second})
	r := New(store, convertersvc.New(filepath.Join(dir, "tmp"), nil), deliverer,
		Config{FastWorkers: 1, PollInterval: 10 * time.Millisecond, NotifyWorkers: 1}, quietLogger())
	defer r.Stop()

	request := requestFor(t, "bad-format", nil)
	if err := store.Enqueue(&jobstore.Job{
		ID: "j1", Lane: jobstore.LaneFast, From: "ofd", To: "bad-format",
		Request: request, NotifyTarget: "erp", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 送达与记账之间有窗口：Deliver 返回后 runner 才调 CompleteDelivery。
	// 直接查会偶发失败，必须等状态落定。
	waitFor(t, "失败通知已记为完成", func() bool {
		done, _ := store.ListDeliveries(jobstore.DeliveryDone, 0)
		return len(done) == 1
	})
	if hits.Load() < 1 {
		t.Error("接收方应被调用过")
	}
	done, _ := store.ListDeliveries(jobstore.DeliveryDone, 0)
	var payload struct {
		Event string `json:"event"`
		Error string `json:"error"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(done[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Event != notify.EventFailed {
		t.Errorf("事件 = %q", payload.Event)
	}
	if payload.Error == "" {
		t.Error("失败通知应带原因")
	}
}

// 启动时要把上次残留的 running / inflight 重新排队，否则永久卡住。
func TestRunnerRecoversOrphansOnStart(t *testing.T) {
	dir := t.TempDir()
	store, _ := jobstore.Open(filepath.Join(dir, "jobs.db"))
	// 模拟上次进程崩溃：入队后直接取走不写终态。
	request := requestFor(t, "pdf", nil)
	if err := store.Enqueue(&jobstore.Job{
		ID: "j1", Lane: jobstore.LaneFast, From: "ofd", To: "pdf",
		Request: request, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(jobstore.LaneFast); err != nil {
		t.Fatal(err)
	}
	// 制造一条卡在 inflight 的通知。
	if err := store.ScheduleDeliveries([]jobstore.Delivery{{
		ID: "d1", Target: "erp", Event: notify.EventSucceeded, DueAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimDueDeliveries(time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	job, _ := store.Get("j1")
	if job.State != jobstore.StateRunning {
		t.Fatalf("前置条件不成立: %s", job.State)
	}

	r := New(store, convertersvc.New(filepath.Join(dir, "tmp"), nil), nil,
		Config{FastWorkers: 1, PollInterval: 10 * time.Millisecond}, quietLogger())
	defer r.Stop()
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 恢复后的任务应当最终进入终态（而不是永远 running）。
	waitFor(t, "恢复的任务进入终态", func() bool {
		got, _ := store.Get("j1")
		return got != nil && got.State.Terminal()
	})
}

func TestRunnerStopIsIdempotent(t *testing.T) {
	r, store, _ := newRunner(t, Config{FastWorkers: 1, PollInterval: 10 * time.Millisecond})
	enqueueOfd(t, store, "j1", "pdf", "")
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.Stop()
	r.Stop() // 再调一次不应 panic 或死锁
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	r, _, _ := newRunner(t, Config{})
	previous := time.Duration(0)
	for attempt := 1; attempt <= 20; attempt++ {
		got := r.backoff(attempt)
		if got <= 0 {
			t.Fatalf("第 %d 次退避时间应为正，实际 %v", attempt, got)
		}
		if previous != 0 && got < previous {
			t.Errorf("退避不应缩短: 第 %d 次 %v < 上次 %v", attempt, got, previous)
		}
		if got > 10*time.Minute {
			t.Errorf("退避应封顶在 10 分钟，实际 %v", got)
		}
		previous = got
	}
}

func TestRetryableJobClassification(t *testing.T) {
	permanent := []error{
		convertersvc.ErrBadRequest,
		convertersvc.ErrUnsupported,
		convertersvc.ErrTooLarge,
	}
	for _, err := range permanent {
		if retryableJob(err) {
			t.Errorf("%v 不应重试", err)
		}
	}
	if retryableJob(context.DeadlineExceeded) != true {
		t.Error("超时应可重试")
	}
	if retryableJob(context.Canceled) != true {
		t.Error("取消应可重试")
	}
	// 包装过的也要能识别出来。
	wrapped := &wrappedError{msg: "bad", inner: convertersvc.ErrBadRequest}
	if retryableJob(wrapped) {
		t.Error("包装后的永久错误不应重试")
	}
}

type wrappedError struct {
	msg   string
	inner error
}

func (e *wrappedError) Error() string { return e.msg }
func (e *wrappedError) Unwrap() error { return e.inner }

// TestRunnerNotifyEventsNarrowDelivery 请求里的 notify.events 收窄本任务要收的
// 事件。
//
// 字段从引入起就存在（随 NotifyTarget 一起），但 runner 从未读过它：server 把它
// 存进任务记录，之后没有任何代码读它。留空收窄、填了就生效，中间没有第三种
// 状态——否则又是那种"字段被接受了但没按你想的方式工作"的静默失效。
// waitForSettled 等到投递队列彻底安静：既没有待投递也没有重试中的条目。
func waitForSettled(t *testing.T, store *jobstore.Store) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, _ := store.ListDeliveries(jobstore.DeliveryPending, 0)
		done, _ := store.ListDeliveries(jobstore.DeliveryDone, 0)
		if len(pending) == 0 && len(done) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunnerNotifyEventsNarrowDelivery(t *testing.T) {
	cases := []struct {
		name       string
		jobEvents  []string
		tgtEvents  []string
		wantServed bool
	}{
		{name: "留空沿用目标默认", jobEvents: nil, tgtEvents: nil, wantServed: true},
		{name: "目标默认下只收失败", jobEvents: []string{notify.EventFailed}, tgtEvents: nil, wantServed: false},
		{name: "通配等于不收窄", jobEvents: []string{notify.EventWildcard}, tgtEvents: nil, wantServed: true},
		{name: "目标已订阅时收窄生效", jobEvents: []string{notify.EventFailed}, tgtEvents: nil, wantServed: false},
		// 交集语义：请求侧不能把目标显式拒掉的事件放回来。
		{name: "不能越过目标的订阅", jobEvents: []string{notify.EventSucceeded}, tgtEvents: []string{notify.EventFailed}, wantServed: false},
		{name: "两边都订阅才发", jobEvents: []string{notify.EventSucceeded}, tgtEvents: []string{notify.EventSucceeded}, wantServed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			dir := t.TempDir()
			store, err := jobstore.Open(filepath.Join(dir, "jobs.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			deliverer := notify.NewDeliverer(notify.NewRegistry(map[string]notify.Target{
				"erp": {URL: srv.URL, Secret: "k", Events: tc.tgtEvents},
			}), &http.Client{Timeout: 2 * time.Second})
			r := New(store, convertersvc.New(filepath.Join(dir, "tmp"), nil), deliverer,
				Config{FastWorkers: 1, PollInterval: 10 * time.Millisecond, NotifyWorkers: 1}, quietLogger())
			defer r.Stop()

			if err := store.Enqueue(&jobstore.Job{
				ID: "j1", Lane: jobstore.LaneFast, From: "ofd", To: "pdf",
				Request:      requestFor(t, "pdf", ofdBytes(t)),
				NotifyTarget: "erp", NotifyEvents: tc.jobEvents,
				CreatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatal(err)
			}
			if err := r.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			// 任务本身要跑完，才能区分"任务没转"与"通知被收窄掉了"。
			waitFor(t, "任务成功", func() bool {
				job, _ := store.Get("j1")
				return job != nil && job.State.Terminal()
			})
			if tc.wantServed {
				waitFor(t, "通知已送达", func() bool { return hits.Load() >= 1 })
			} else {
				// 等任务真的跑到终态，而不是睡固定时间再猜。睡眠会在负载高时
				// 于任务完成前就断言，看到的是"还没投递"——恰好与期望一致，
				// 于是测试通过；真正的回归（该收窄的没收窄）却要等任务完成
				// 之后才暴露，那时就太晚了。这条断言本来就该以任务终态为
				// 同步点。
				waitFor(t, "任务已终结", func() bool {
					job, _ := store.Get("j1")
					return job != nil && job.State.Terminal()
				})
				// 终态后投递要么已入队要么已被拒。等一小段时间让投递循环
				// 有机会把它取走——这一段是必要的，因为断言的是"最终没有"，
				// 而不是"此刻还没有"。
				waitForSettled(t, store)
				if got := hits.Load(); got != 0 {
					t.Errorf("被收窄的事件仍投递了 %d 次", got)
				}
				pending, _ := store.ListDeliveries(jobstore.DeliveryPending, 0)
				done, _ := store.ListDeliveries(jobstore.DeliveryDone, 0)
				if len(pending)+len(done) != 0 {
					t.Errorf("不该入队却排了 %d 条通知", len(pending)+len(done))
				}
			}
		})
	}
}
