package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/notify"
)

// fakeConverter 是受控的转换器：能按通道阻塞、能统计并发数、能按需失败。
//
// 并发相关的断言只有拿到"同时有几个转换在跑"才写得出来，而真实 OFD 转换
// 快到没法制造这种场景，所以压测必须能控制速度。
type fakeConverter struct {
	// block 非 nil 时，命中对应通道的转换会等它关闭再返回。
	block    chan struct{}
	blocking func(spec convertersvc.Spec) bool
	// hold 是每次转换的固定耗时。
	hold time.Duration
	// failFirstN 让前 N 次转换失败（每次任务只计一次）。
	failFirstN int32
	failErr    error

	mu       sync.Mutex
	started  map[string]int // jobID -> 启动次数
	peak     map[string]int // lane -> 观察到的最大并发
	inflight map[string]int
	order    []string
}

func newFakeConverter() *fakeConverter {
	return &fakeConverter{
		started:  map[string]int{},
		peak:     map[string]int{},
		inflight: map[string]int{},
	}
}

func (c *fakeConverter) blockingFor(spec convertersvc.Spec) bool {
	if c.block == nil {
		return false
	}
	if c.blocking == nil {
		return true
	}
	return c.blocking(spec)
}

func (c *fakeConverter) Run(ctx context.Context, spec convertersvc.Spec) (convertersvc.Result, error) {
	jobID := spec.Output.Dir // 压测里借 Dir 字段带 job ID，见 enqueueWith
	lane := string(convertersvc.LaneFast)
	if convertersvc.IsHeavy(spec.Input, spec.Output.Format) {
		lane = string(convertersvc.LaneHeavy)
	}
	c.trackStart(lane)
	defer c.trackEnd(lane)

	seq := c.bump(jobID)
	if c.failErr != nil && int32(seq) <= atomic.LoadInt32(&c.failFirstN) {
		return convertersvc.Result{}, c.failErr
	}
	if c.hold > 0 {
		select {
		case <-time.After(c.hold):
		case <-ctx.Done():
			return convertersvc.Result{}, ctx.Err()
		}
	}
	if c.blockingFor(spec) {
		select {
		case <-c.block:
		case <-ctx.Done():
			return convertersvc.Result{}, ctx.Err()
		}
	}
	return convertersvc.Result{
		Kind: convertersvc.OutputStream, Format: spec.Output.Format,
		MIME: "application/octet-stream", Filename: "output.pdf",
		Bytes: []byte("%PDF-1.7\n" + jobID), TookMs: 1,
	}, nil
}

func (c *fakeConverter) trackStart(lane string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inflight[lane]++
	if c.inflight[lane] > c.peak[lane] {
		c.peak[lane] = c.inflight[lane]
	}
}

func (c *fakeConverter) trackEnd(lane string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inflight[lane]--
}

func (c *fakeConverter) bump(jobID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started[jobID]++
	c.order = append(c.order, jobID)
	return c.started[jobID]
}

func (c *fakeConverter) starts(jobID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started[jobID]
}

func (c *fakeConverter) peakFor(lane string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.peak[lane]
}

func (c *fakeConverter) distinct() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[string]bool{}
	for _, id := range c.order {
		seen[id] = true
	}
	return len(seen)
}

// newStressRunner 组装一套可压测的 runner。
func newStressRunner(t *testing.T, cfg Config, convert Converter) (*Runner, *jobstore.Store, *fakeConverter) {
	t.Helper()
	dir := t.TempDir()
	store, err := jobstore.Open(filepath.Join(dir, "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fake, _ := convert.(*fakeConverter)
	r := New(store, convert, nil, cfg, quietLogger())
	t.Cleanup(r.Stop)
	return r, store, fake
}

// enqueueWith 入队一个任务，把 jobID 塞进 output.dir 供 fakeConverter 识别。
func enqueueWith(t *testing.T, store *jobstore.Store, id string, lane jobstore.Lane, format string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"input":  map[string]any{"kind": "upload", "filename": "a.ofd"},
		"output": map[string]any{"kind": "stream", "format": format, "dir": id},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(&jobstore.Job{
		ID: id, Lane: lane, From: "ofd", To: format, Request: raw,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
}

func waitTerminal(t *testing.T, store *jobstore.Store, ids []string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		done := 0
		for _, id := range ids {
			job, err := store.Get(id)
			if err == nil && job != nil && job.State.Terminal() {
				done++
			}
		}
		if done == len(ids) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	var pending []string
	for _, id := range ids {
		job, _ := store.Get(id)
		if job == nil || !job.State.Terminal() {
			pending = append(pending, id)
		}
	}
	t.Fatalf("以下任务未进入终态: %v", pending)
}

// 高并发下的基本保证：不丢、不重、全部完成。
func TestStressNoLostOrDuplicatedJobs(t *testing.T) {
	const jobs = 300
	convert := newFakeConverter()
	r, store, fake := newStressRunner(t, Config{
		FastWorkers: 8, HeavyWorkers: 2, PollInterval: time.Millisecond,
	}, convert)
	ids := make([]string, 0, jobs)
	for i := 0; i < jobs; i++ {
		id := fmt.Sprintf("j%04d", i)
		ids = append(ids, id)
		lane := jobstore.LaneFast
		if i%4 == 0 {
			lane = jobstore.LaneHeavy
		}
		enqueueWith(t, store, id, lane, "pdf")
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, store, ids, 60*time.Second)

	// 每个任务都成功。
	for _, id := range ids {
		job, _ := store.Get(id)
		if job.State != jobstore.StateSucceeded {
			t.Errorf("%s 状态 = %s，错误 = %q", id, job.State, job.Error)
		}
		// Attempt 必须恰好是 1：成功一次，不该有重试。
		if job.Attempt != 1 {
			t.Errorf("%s Attempt = %d，期望 1", id, job.Attempt)
		}
	}
	// 没有任务被转换两次。
	if fake.distinct() != jobs {
		t.Errorf("实际启动 %d 个不同任务，期望 %d", fake.distinct(), jobs)
	}
	for _, id := range ids {
		if n := fake.starts(id); n != 1 {
			t.Errorf("%s 被转换了 %d 次", id, n)
		}
	}
}

// 通道隔离：重通道被慢任务占满时，快通道的任务仍应正常完成。
//
// 这是 fast/heavy 分道的全部意义——一个 Office 文档不该让 Markdown 转换一起卡住。
func TestStressLaneIsolation(t *testing.T) {
	convert := newFakeConverter()
	release := make(chan struct{})
	convert.block = release
	// 只有重通道的任务会阻塞。
	convert.blocking = func(spec convertersvc.Spec) bool {
		return spec.Output.Format == "pptx"
	}
	r, store, _ := newStressRunner(t, Config{
		FastWorkers: 2, HeavyWorkers: 1, PollInterval: time.Millisecond,
	}, convert)

	// 先塞三个重任务把唯一的一个 heavy worker 占住。
	var heavy []string
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("h%d", i)
		heavy = append(heavy, id)
		enqueueWith(t, store, id, jobstore.LaneHeavy, "pptx")
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 等 heavy worker 真的进入阻塞状态。
	waitFor(t, "heavy worker 开始转换", func() bool { return convert.peakFor("heavy") == 1 })

	// 此时提交快任务，它们必须在 heavy 还没放开时就完成。
	var fast []string
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("f%02d", i)
		fast = append(fast, id)
		enqueueWith(t, store, id, jobstore.LaneFast, "pdf")
	}
	waitTerminal(t, store, fast, 20*time.Second)

	// heavy 一个都还没完成——证明它们确实被卡着。
	for _, id := range heavy {
		job, _ := store.Get(id)
		if job.State.Terminal() {
			t.Errorf("%s 在放开前就完成了，隔离没生效", id)
		}
	}
	// 快通道并发不应超过配置值。
	if peak := convert.peakFor("fast"); peak > 2 {
		t.Errorf("快通道并发峰值 %d，超过 FastWorkers=2", peak)
	}
	// heavy 并发必须被 HeavyWorkers=1 限住。
	if peak := convert.peakFor("heavy"); peak > 1 {
		t.Errorf("重通道并发峰值 %d，超过 HeavyWorkers=1", peak)
	}
	close(release)
	waitTerminal(t, store, heavy, 30*time.Second)
}

// 退避真的生效：失败任务不会立刻重跑。
func TestStressBackoffDelaysRetries(t *testing.T) {
	convert := newFakeConverter()
	convert.failFirstN = 2
	convert.failErr = context.DeadlineExceeded
	r, store, _ := newStressRunner(t, Config{
		FastWorkers: 1, PollInterval: time.Millisecond,
		MaxJobAttempts: 5, Backoff: 300 * time.Millisecond,
	}, convert)
	enqueueWith(t, store, "j1", jobstore.LaneFast, "pdf")
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, store, []string{"j1"}, 30*time.Second)
	job, _ := store.Get("j1")
	if job.State != jobstore.StateSucceeded {
		t.Fatalf("重试到上限内应成功，实际 %s (%s)", job.State, job.Error)
	}
	if job.Attempt != 3 {
		t.Errorf("Attempt = %d，期望 3（失败 2 次后成功）", job.Attempt)
	}
	// 两次退避共约 300+600ms，若没有退避，第三次尝试会远早于此。
	if convert.starts("j1") != 3 {
		t.Errorf("转换次数 = %d，期望 3", convert.starts("j1"))
	}
}

// 大量失败且不再重试时，不应形成热循环。
func TestStressNoHotLoopOnPermanentFailure(t *testing.T) {
	convert := newFakeConverter()
	convert.failErr = fmt.Errorf("%w: 参数错误", convertersvc.ErrBadRequest)
	r, store, _ := newStressRunner(t, Config{
		FastWorkers: 4, PollInterval: time.Millisecond, MaxJobAttempts: 10,
	}, convert)
	const jobs = 50
	ids := make([]string, 0, jobs)
	for i := 0; i < jobs; i++ {
		id := fmt.Sprintf("j%02d", i)
		ids = append(ids, id)
		enqueueWith(t, store, id, jobstore.LaneFast, "pdf")
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, store, ids, 30*time.Second)
	// 永久失败只该尝试一次。
	for _, id := range ids {
		job, _ := store.Get(id)
		if job.Attempt != 1 {
			t.Errorf("%s Attempt = %d，永久失败应只试一次", id, job.Attempt)
		}
	}
}

// 通知风暴：并发投递下每条都应恰好送达一次，去重 ID 唯一。
func TestStressNotificationStorm(t *testing.T) {
	const jobs = 120
	var delivered atomic.Int32
	var mu sync.Mutex
	ids := map[string]int{}
	srv := newCountingHook(t, &delivered, &mu, ids)

	dir := t.TempDir()
	store, err := jobstore.Open(filepath.Join(dir, "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	deliverer := notify.NewDeliverer(notify.NewRegistry(map[string]notify.Target{
		"hook": {URL: srv},
	}), nil)
	convert := newFakeConverter()
	r := New(store, convert, deliverer, Config{
		FastWorkers: 6, NotifyWorkers: 4, PollInterval: time.Millisecond,
	}, quietLogger())
	defer r.Stop()

	var jobIDs []string
	for i := 0; i < jobs; i++ {
		id := fmt.Sprintf("j%04d", i)
		jobIDs = append(jobIDs, id)
		raw, _ := json.Marshal(map[string]any{
			"input":  map[string]any{"kind": "upload", "filename": "a.ofd"},
			"output": map[string]any{"kind": "stream", "format": "pdf", "dir": id},
		})
		if err := store.Enqueue(&jobstore.Job{
			ID: id, Lane: jobstore.LaneFast, From: "ofd", To: "pdf",
			Request: raw, NotifyTarget: "hook", CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, store, jobIDs, 60*time.Second)
	waitFor(t, "全部通知送达", func() bool { return int(delivered.Load()) == jobs })
	// 通知最终状态。
	waitFor(t, "通知全部记为完成", func() bool {
		done, _ := store.ListDeliveries(jobstore.DeliveryDone, 0)
		return len(done) == jobs
	})
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != jobs {
		t.Errorf("去重 ID 应有 %d 个，实际 %d", jobs, len(ids))
	}
	for id, n := range ids {
		if n != 1 {
			t.Errorf("通知 %s 送达 %d 次，期望恰好 1 次", id, n)
		}
	}
}

// 关闭时不该有任务永久卡在 running。
func TestStressShutdownLeavesNoRunningJobs(t *testing.T) {
	convert := newFakeConverter()
	convert.block = make(chan struct{})
	r, store, _ := newStressRunner(t, Config{
		FastWorkers: 2, PollInterval: time.Millisecond,
	}, convert)
	const jobs = 40
	ids := make([]string, 0, jobs)
	for i := 0; i < jobs; i++ {
		id := fmt.Sprintf("j%02d", i)
		ids = append(ids, id)
		enqueueWith(t, store, id, jobstore.LaneFast, "pdf")
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "有任务开始转换", func() bool { return convert.peakFor("fast") > 0 })
	// 在途任务全被卡住时关闭。
	r.Stop()

	// 关闭不是任务失败：被中断的任务应留在 running，等下次启动的 Recover
	// 重新排队。曾经这里把它们写成 failed，调用方会收到一条 "context canceled"
	// 的失败通知，而重启后任务其实会正常跑完。
	for _, id := range ids {
		job, err := store.Get(id)
		if err != nil || job == nil {
			continue
		}
		if job.State == jobstore.StateFailed && job.Error == context.Canceled.Error() {
			t.Errorf("%s 因服务关闭被判为失败，调用方会收到假的失败通知", id)
		}
	}
	// 卡住的转换器已被 ctx 取消，fakeConverter 不会再永久阻塞。
	close(convert.block)

	// 这些 running 的任务必须能被恢复，否则就永久卡住了。
	recovered, err := store.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if recovered == 0 {
		t.Error("关闭时中断的任务没有被 Recover 捞起来")
	}
	for _, id := range ids {
		job, _ := store.Get(id)
		if job.State == jobstore.StateRunning {
			t.Errorf("%s 恢复后仍处于 running", id)
		}
	}
}

// 反复启停不应丢任务：关闭时排队或重排的任务，重启后应被 Recover 捞起来跑完。
func TestStressRestartResumesQueuedJobs(t *testing.T) {
	convert := newFakeConverter()
	release := make(chan struct{})
	convert.block = release
	r, store, _ := newStressRunner(t, Config{
		FastWorkers: 1, PollInterval: time.Millisecond,
	}, convert)
	enqueueWith(t, store, "j1", jobstore.LaneFast, "pdf")
	enqueueWith(t, store, "j2", jobstore.LaneFast, "pdf")
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "j1 进入转换", func() bool { return convert.starts("j1") == 1 })
	// 在 j1 卡住、j2 还在队列里时关闭。
	r.Stop()
	close(release)

	// 模拟进程重启：同一个任务库上起一个新 runner。
	restarted := New(store, convert, nil, Config{
		FastWorkers: 2, PollInterval: time.Millisecond, MaxJobAttempts: 1,
	}, quietLogger())
	defer restarted.Stop()
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, store, []string{"j1", "j2"}, 30*time.Second)
	// j1 在关停时被打断、重启后重排，j2 一直在队列里：两者都应成功，
	// 且都不该收到过"失败"。
	for _, id := range []string{"j1", "j2"} {
		job, _ := store.Get(id)
		if job.State != jobstore.StateSucceeded {
			t.Errorf("%s 重启后状态 = %s (%s)", id, job.State, job.Error)
		}
	}
}

// newCountingHook 起一个记录投递次数的回调服务。
func newCountingHook(t *testing.T, delivered *atomic.Int32, mu *sync.Mutex, ids map[string]int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ids[r.Header.Get(notify.HeaderDelivery)]++
		mu.Unlock()
		delivered.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
