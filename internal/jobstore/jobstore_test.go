package jobstore

import (
	"fmt"
	"github.com/goccy/go-json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func newJob(id string, lane Lane, order Order) *Job {
	return &Job{
		ID: id, Lane: lane, Order: order, From: "docx", To: "ofd",
		CreatedAt: time.Now().UTC(),
	}
}

func TestEnqueueAndGet(t *testing.T) {
	store, _ := openStore(t)
	job := newJob("j1", LaneFast, OrderNormal)
	job.Request = json.RawMessage(`{"from":"docx"}`)
	if err := store.Enqueue(job); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("j1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateQueued || got.From != "docx" || string(got.Request) != `{"from":"docx"}` {
		t.Errorf("读回的任务不对: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt 不应为零")
	}
	// 缺 ID 必须被拒。
	if err := store.Enqueue(&Job{Lane: LaneFast}); err == nil {
		t.Error("空 ID 应被拒绝")
	}
	if err := store.Enqueue(nil); err == nil {
		t.Error("nil 任务应被拒绝")
	}
	// 未指定时间时补当前时间，指定时保留。
	stamped := &Job{ID: "j2", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	if err := store.Enqueue(stamped); err != nil {
		t.Fatal(err)
	}
	if back, _ := store.Get("j2"); !back.CreatedAt.Equal(stamped.CreatedAt) {
		t.Errorf("显式时间被覆盖: %v", back.CreatedAt)
	}
}

func TestClaimIsFIFOAndLaneSeparated(t *testing.T) {
	store, _ := openStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		job := newJob(fmt.Sprintf("fast-%d", i), LaneFast, OrderNormal)
		job.CreatedAt = base.Add(time.Duration(i) * time.Second)
		if err := store.Enqueue(job); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		job := newJob(fmt.Sprintf("heavy-%d", i), LaneHeavy, OrderNormal)
		job.CreatedAt = base.Add(time.Duration(i) * time.Second)
		if err := store.Enqueue(job); err != nil {
			t.Fatal(err)
		}
	}

	// 快路径按创建时间先进先出。
	for i := 0; i < 3; i++ {
		job, err := store.Claim(LaneFast)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			t.Fatalf("快路径第 %d 次取件为空", i)
		}
		if want := fmt.Sprintf("fast-%d", i); job.ID != want {
			t.Errorf("第 %d 次取到 %s，期望 %s（应先进先出）", i, job.ID, want)
		}
		if job.State != StateRunning || job.StartedAt.IsZero() || job.Attempt != 1 {
			t.Errorf("取件后状态不对: %+v", job)
		}
	}
	if job, err := store.Claim(LaneFast); err != nil || job != nil {
		t.Errorf("快路径取空时应返回 (nil, nil)，实际 %v / %v", job, err)
	}
	// 重路径不受影响。
	job, err := store.Claim(LaneHeavy)
	if err != nil || job == nil || job.ID != "heavy-0" {
		t.Errorf("重路径首件 = %v / %v", job, err)
	}
}

func TestClaimRespectsPriority(t *testing.T) {
	store, _ := openStore(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	normal := newJob("normal", LaneFast, OrderNormal)
	normal.CreatedAt = base
	high := newJob("high", LaneFast, OrderHigh)
	high.CreatedAt = base.Add(time.Hour) // 高优先级任务反而更晚入队
	for _, job := range []*Job{normal, high} {
		if err := store.Enqueue(job); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.ID != "high" {
		t.Errorf("高优先级任务应先出队，实际 %v", first)
	}
}

// 多个 worker 并发取件不得取到同一个任务：出队与状态变更在同一个写事务内，
// bbolt 串行化写事务保证这一点。
func TestClaimConcurrentWorkersNeverDuplicate(t *testing.T) {
	store, _ := openStore(t)
	const total = 200
	for i := 0; i < total; i++ {
		if err := store.Enqueue(newJob(fmt.Sprintf("j%d", i), LaneFast, OrderNormal)); err != nil {
			t.Fatal(err)
		}
	}
	var claimed sync.Map
	var duplicates atomic.Int64
	var workers = 8
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				job, err := store.Claim(LaneFast)
				if err != nil {
					t.Errorf("Claim 失败: %v", err)
					return
				}
				if job == nil {
					return
				}
				if _, loaded := claimed.LoadOrStore(job.ID, true); loaded {
					duplicates.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if duplicates.Load() != 0 {
		t.Errorf("出现 %d 次重复取件", duplicates.Load())
	}
	count := 0
	claimed.Range(func(_, _ any) bool { count++; return true })
	if count != total {
		t.Errorf("共取到 %d 件，期望 %d 件", count, total)
	}
}

func TestFinishRejectsNonTerminalAndAlreadyFinished(t *testing.T) {
	store, _ := openStore(t)
	if err := store.Enqueue(newJob("j1", LaneFast, OrderNormal)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("j1", StateQueued, Output{}, ""); err == nil {
		t.Error("非终态应被拒绝")
	}
	if err := store.Finish("missing", StateFailed, Output{}, "x"); err == nil {
		t.Error("不存在的任务应报错")
	}
	if err := store.Finish("j1", StateSucceeded, Output{Kind: "dir", Path: "/out/a.ofd", Size: 7}, ""); err != nil {
		t.Fatal(err)
	}
	job, _ := store.Get("j1")
	if job.State != StateSucceeded || job.Output.Path != "/out/a.ofd" || job.FinishedAt.IsZero() {
		t.Errorf("终态记录不对: %+v", job)
	}
	// 终态不可再改。
	if err := store.Finish("j1", StateFailed, Output{}, "再次失败"); err == nil {
		t.Error("已终结的任务不应被改写")
	}
	if job, _ := store.Get("j1"); job.State != StateSucceeded {
		t.Errorf("状态被改写为 %s", job.State)
	}
	if err := store.Finish("j1", StateFailed, Output{}, ""); err == nil {
		t.Error("重复 Finish 应报错")
	}
}

func TestCancelOnlyQueued(t *testing.T) {
	store, _ := openStore(t)
	// 先入队 "taken"，Claim 按 FIFO 取走的正是它，"queued" 因此仍在排队。
	if err := store.Enqueue(newJob("taken", LaneFast, OrderNormal)); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(newJob("queued", LaneFast, OrderNormal)); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	} else if claimed == nil || claimed.ID != "taken" {
		t.Fatalf("首次取件 = %v，期望 taken", claimed)
	}
	if err := store.Cancel("queued"); err != nil {
		t.Fatalf("取消排队任务失败: %v", err)
	}
	job, _ := store.Get("queued")
	if job.State != StateCancelled {
		t.Errorf("取消后状态 = %s", job.State)
	}
	// 取消的任务必须已从队列摘除：快路径此时应取空（"taken" 已被取走处于运行中，
	// "queued" 已取消）。
	if job, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	} else if job != nil {
		t.Errorf("取到 %v，期望空队列（取消的任务不应再被取到）", job)
	}
	// 运行中不可取消。
	if err := store.Cancel("taken"); err == nil {
		t.Error("运行中的任务不应被取消")
	}
	// 不存在的任务。
	if err := store.Cancel("missing"); err == nil {
		t.Error("取消不存在的任务应报错")
	}
	// 重复取消。
	if err := store.Cancel("queued"); err == nil {
		t.Error("重复取消应报错")
	}
}

// 崩溃恢复：上次进程退出时停在 running 的任务，其转换进程已随之消失，
// 启动时必须重新入队，否则永久卡住。
func TestRecoverRequeuesOrphanedRunning(t *testing.T) {
	store, _ := openStore(t)
	for i := 0; i < 3; i++ {
		if err := store.Enqueue(newJob(fmt.Sprintf("j%d", i), LaneFast, OrderNormal)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := store.Claim(LaneFast); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Enqueue(newJob("still-queued", LaneHeavy, OrderNormal)); err != nil {
		t.Fatal(err)
	}
	counts, err := store.Count()
	if err != nil {
		t.Fatal(err)
	}
	// 入队 3 取走 2，另有 1 个重路径任务仍在排队，因此 queued 应为 2。
	if counts[StateRunning] != 2 || counts[StateQueued] != 2 {
		t.Fatalf("恢复前状态分布 = %v", counts)
	}

	recovered, err := store.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 2 {
		t.Errorf("恢复数量 = %d，期望 2", recovered)
	}
	counts, _ = store.Count()
	if counts[StateRunning] != 0 || counts[StateQueued] != 4 {
		t.Errorf("恢复后状态分布 = %v，期望 running=0 queued=4", counts)
	}
	// 幂等：紧接着再恢复一次，此时没有任务处于 running，不应有动作。必须放在下面
	// 的取件循环之前——一旦取走就又变成 running，再恢复当然会重新入队。
	if again, err := store.Recover(); err != nil {
		t.Fatal(err)
	} else if again != 0 {
		t.Errorf("连续第二次恢复数量 = %d，期望 0", again)
	}
	// 恢复的任务能重新取到，且重路径的排队任务不受影响。
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		job, err := store.Claim(LaneFast)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			t.Fatalf("快路径第 %d 次取件为空", i)
		}
		seen[job.ID] = true
	}
	if len(seen) != 3 {
		t.Errorf("恢复后取到的任务有重复: %v", seen)
	}
	// Attempt 保留历史，重排后再次取件应累加。
	job, _ := store.Get("j0")
	if job.Attempt < 1 {
		t.Errorf("取件次数应记录，实际 %d", job.Attempt)
	}
}

func TestListByStateAndCount(t *testing.T) {
	store, _ := openStore(t)
	for i := 0; i < 3; i++ {
		if err := store.Enqueue(newJob(fmt.Sprintf("ok%d", i), LaneFast, OrderNormal)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := store.Enqueue(newJob(fmt.Sprintf("bad%d", i), LaneFast, OrderNormal)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := store.Claim(LaneFast); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Finish("ok0", StateSucceeded, Output{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("ok1", StateFailed, Output{}, "坏文件"); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("bad0", StateFailed, Output{}, "坏文件"); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("bad1", StateFailed, Output{}, "坏文件"); err != nil {
		t.Fatal(err)
	}

	counts, err := store.Count()
	if err != nil {
		t.Fatal(err)
	}
	want := map[State]int{StateQueued: 1, StateRunning: 0, StateSucceeded: 1, StateFailed: 3, StateCancelled: 0}
	for state, expected := range want {
		if counts[state] != expected {
			t.Errorf("%s 计数 = %d，期望 %d（全部 %v）", state, counts[state], expected, counts)
		}
	}
	// limit 生效。
	failed, err := store.ListByState(StateFailed, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 2 {
		t.Errorf("limit=2 时返回 %d 条", len(failed))
	}
	all, _ := store.ListByState(StateFailed, 0)
	if len(all) != 3 {
		t.Errorf("limit=0 应返回全部 3 条，实际 %d", len(all))
	}
}

func TestPruneRemovesOnlyOldTerminal(t *testing.T) {
	store, _ := openStore(t)
	old := time.Now().UTC().Add(-48 * time.Hour)
	recent := time.Now().UTC()

	mk := func(id string, at time.Time, lane Lane) {
		job := newJob(id, lane, OrderNormal)
		job.CreatedAt = at
		if err := store.Enqueue(job); err != nil {
			t.Fatal(err)
		}
	}
	mk("old-ok", old, LaneFast)
	mk("old-bad", old, LaneFast)
	mk("new-ok", recent, LaneFast)
	mk("old-queued", old, LaneFast) // 仍在排队，不该被清

	if _, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("old-ok", StateSucceeded, Output{}, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("old-bad", StateFailed, Output{}, "x"); err != nil {
		t.Fatal(err)
	}

	removed, err := store.Prune(time.Now().UTC().Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("清理数量 = %d，期望 2", removed)
	}
	for _, id := range []string{"old-ok", "old-bad"} {
		if job, _ := store.Get(id); job != nil {
			t.Errorf("%s 应已清理", id)
		}
	}
	for _, id := range []string{"new-ok", "old-queued"} {
		if job, _ := store.Get(id); job == nil {
			t.Errorf("%s 不应被清理", id)
		}
	}
	// 清理后队列仍可用：old-queued 仍可取到。
	job, err := store.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.ID != "old-queued" {
		t.Errorf("清理后队列损坏，取到 %v", job)
	}
}

// 数据必须跨进程重启存活，这是 bbolt 相对"每任务一个 JSON 文件"的根本优势。
func TestPersistenceAcrossReopen(t *testing.T) {
	store, path := openStore(t)
	if err := store.Enqueue(newJob("keep", LaneHeavy, OrderNormal)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(LaneHeavy); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// 重复 Close 应当无害。
	if err := store.Close(); err != nil {
		t.Errorf("重复 Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	job, err := reopened.Get("keep")
	if err != nil || job == nil {
		t.Fatalf("重开后读不到任务: %v", err)
	}
	if job.State != StateRunning {
		t.Errorf("重开后状态 = %s", job.State)
	}
	// 状态索引也在。
	counts, _ := reopened.Count()
	if counts[StateRunning] != 1 {
		t.Errorf("重开后状态索引 = %v", counts)
	}
}

func TestCompactShrinksAndKeepsData(t *testing.T) {
	store, path := openStore(t)
	// 制造足够多的删除，使文件有空闲页。
	for i := 0; i < 300; i++ {
		if err := store.Enqueue(newJob(fmt.Sprintf("j%d", i), LaneFast, OrderNormal)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 300; i++ {
		if _, err := store.Claim(LaneFast); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(fmt.Sprintf("j%d", i), StateSucceeded, Output{}, ""); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := store.Prune(time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 300 {
		t.Fatalf("清理 = %d，期望 300", removed)
	}
	before := fileSize(t, path)

	if err := store.Compact(); err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}
	after := fileSize(t, path)
	if after >= before {
		t.Errorf("压缩后未变小: %d → %d", before, after)
	}
	// 压缩后仍可读写。
	if err := store.Enqueue(newJob("after", LaneFast, OrderNormal)); err != nil {
		t.Fatalf("压缩后写入失败: %v", err)
	}
	job, err := store.Claim(LaneFast)
	if err != nil || job == nil || job.ID != "after" {
		t.Errorf("压缩后队列不可用: %v / %v", job, err)
	}
	counts, _ := store.Count()
	if counts[StateRunning] != 1 {
		t.Errorf("压缩后状态索引异常: %v", counts)
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Error("空路径应被拒绝")
	}
	// 不存在的父目录应被创建。
	nested := filepath.Join(t.TempDir(), "a", "b", "jobs.db")
	store, err := Open(nested)
	if err != nil {
		t.Fatalf("应自动创建父目录: %v", err)
	}
	_ = store.Close()
}

// 版本号不匹配时必须拒绝打开，否则会用错误的 bucket 布局读写已有数据。
func TestOpenRejectsVersionMismatch(t *testing.T) {
	store, path := openStore(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("meta")).Put([]byte("schema_version"), []byte{99})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("版本不匹配应拒绝打开")
	} else if !strings.Contains(err.Error(), "版本") {
		t.Errorf("错误信息 = %v，期望提到版本", err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取文件大小失败: %v", err)
	}
	return info.Size()
}

func TestStateTerminal(t *testing.T) {
	terminal := map[State]bool{
		StateSucceeded: true, StateFailed: true, StateCancelled: true,
		StateQueued: false, StateRunning: false,
	}
	for state, want := range terminal {
		if state.Terminal() != want {
			t.Errorf("%s.Terminal() = %v，期望 %v", state, state.Terminal(), want)
		}
	}
	if got := Lanes(); len(got) != 2 {
		t.Errorf("Lanes() = %v", got)
	}
}

// TestRecordUsage 守住用量记账与状态迁移互不干扰。
//
// RecordUsage 单独于 Finish 是为了"记账失败不改变任务结局"。这条断言把它
// 固定下来：记账后任务必须仍是 running，没有被顺手置成终态。
func TestRecordUsage(t *testing.T) {
	store, _ := openStore(t)

	// 走 Enqueue + Claim 而不是直接塞一个 running 任务：Enqueue 会把状态
	// 归一成 queued，真实流程本来也是先入队再被 worker 领走。
	if err := store.Enqueue(&Job{ID: "usage-1", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordUsage("usage-1", "docx", 4096); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("usage-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.FromActual != "docx" {
		t.Errorf("FromActual = %q，期望 docx", got.FromActual)
	}
	if got.InputBytes != 4096 {
		t.Errorf("InputBytes = %d，期望 4096", got.InputBytes)
	}
	if got.State != StateRunning {
		t.Errorf("State = %s，记账不应改变任务状态（期望 %s）", got.State, StateRunning)
	}
}

// TestRecordUsageOverwritesDeclaredFormat 确认回写覆盖的是"实际格式"，
// 而不是把声明值补上——声明值在 From 里已经有了，两者的区别正是统计口径。
func TestRecordUsageOverwritesDeclaredFormat(t *testing.T) {
	store, _ := openStore(t)

	job := &Job{ID: "usage-2", State: StateRunning, From: "ofd", To: "pdf", CreatedAt: time.Now().UTC()}
	if err := store.Enqueue(job); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordUsage(job.ID, "html", 100); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.From != "ofd" {
		t.Errorf("From = %q，声明值不应被改写", got.From)
	}
	if got.FromActual != "html" {
		t.Errorf("FromActual = %q，期望实际格式 html", got.FromActual)
	}
}

// TestStatsSurvivePrune 守住 Prometheus counter 的单调性。
//
// 这是整个统计设计里最关键的一条：如果 /metrics 的数值随任务清理而下降，
// Prometheus 的 rate() 与 increase() 会产出错值，而且不报错——图看着有数据
// 实际全错。所以计数必须独立于 jobs 表存在。
func TestStatsSurvivePrune(t *testing.T) {
	store, _ := openStore(t)

	if err := store.Enqueue(&Job{ID: "s1", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordUsage("s1", "ofd", 1024); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("s1", StateSucceeded, Output{Size: 2048}, ""); err != nil {
		t.Fatal(err)
	}

	before, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := before.Jobs[StatsTriple{"ofd", "pdf", "succeeded"}]; got != 1 {
		t.Fatalf("转换计数 = %d，期望 1", got)
	}

	// 清理掉任务记录。
	if n, err := store.Prune(time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("Prune 删除 %d 个，期望 1", n)
	}
	if job, err := store.Get("s1"); err != nil || job != nil {
		t.Fatalf("任务记录应已删除，Get 返回 %v, %v", job, err)
	}

	after, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.Jobs[StatsTriple{"ofd", "pdf", "succeeded"}] != 1 {
		t.Errorf("清理后转换计数下降为 %d，期望仍为 1", after.Jobs[StatsTriple{"ofd", "pdf", "succeeded"}])
	}
	if after.InputBytes["ofd"] != 1024 {
		t.Errorf("清理后输入字节 = %d，期望仍为 1024", after.InputBytes["ofd"])
	}
	if after.OutputBytes[StatsPair{"ofd", "pdf"}] != 2048 {
		t.Errorf("清理后输出字节 = %d，期望仍为 2048", after.OutputBytes[StatsPair{"ofd", "pdf"}])
	}
}

// TestStatsUseActualFormat 统计必须按服务端判定的格式归类。
//
// 用声明值（From）会把这个任务算成 ofd→pdf，而实际输入是 HTML。
func TestStatsUseActualFormat(t *testing.T) {
	store, _ := openStore(t)

	if err := store.Enqueue(&Job{ID: "s2", From: "ofd", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordUsage("s2", "html", 10); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("s2", StateSucceeded, Output{Size: 20}, ""); err != nil {
		t.Fatal(err)
	}

	snap, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Jobs[StatsTriple{"html", "pdf", "succeeded"}]; got != 1 {
		t.Errorf("实际格式计数 = %d，期望 html→pdf 记 1", got)
	}
	if got := snap.Jobs[StatsTriple{"ofd", "pdf", "succeeded"}]; got != 0 {
		t.Errorf("声明格式不应被计入，ofd→pdf = %d，期望 0", got)
	}
}

// TestStatsFailedJobsGroupedUnknown 失败任务归到 unknown。
//
// 失败时没有 FromActual（转换没跑到判定格式那步）。这时用声明值补是错的：
// 它会把"没判出来"和"判成这个格式"混成一类，让按类型分布失真。
func TestStatsFailedJobsGroupedUnknown(t *testing.T) {
	store, _ := openStore(t)

	if err := store.Enqueue(&Job{ID: "s3", From: "docx", To: "ofd"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("s3", StateFailed, Output{}, "转换失败"); err != nil {
		t.Fatal(err)
	}

	snap, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Jobs[StatsTriple{"unknown", "ofd", "failed"}]; got != 1 {
		t.Errorf("失败任务计数 = %d，期望 unknown→ofd 记 1", got)
	}
	if got := snap.Jobs[StatsTriple{"docx", "ofd", "failed"}]; got != 0 {
		t.Errorf("不应按声明值 docx 归类，实际 %d", got)
	}
	// 失败没有产物，不该建输出字节序列。
	if len(snap.OutputBytes) != 0 {
		t.Errorf("失败任务不应有输出字节序列: %+v", snap.OutputBytes)
	}
}

// TestStatsAccumulateAcrossJobs 同一格式组合的多次转换要累加。
func TestStatsAccumulateAcrossJobs(t *testing.T) {
	store, _ := openStore(t)
	for i, size := range []int64{100, 200, 300} {
		id := fmt.Sprintf("s4-%d", i)
		if err := store.Enqueue(&Job{ID: id, To: "pdf"}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Claim(LaneFast); err != nil {
			t.Fatal(err)
		}
		if err := store.RecordUsage(id, "ofd", size); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(id, StateSucceeded, Output{Size: size * 2}, ""); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Jobs[StatsTriple{"ofd", "pdf", "succeeded"}]; got != 3 {
		t.Errorf("转换计数 = %d，期望 3", got)
	}
	if got := snap.InputBytes["ofd"]; got != 600 {
		t.Errorf("输入字节 = %d，期望 600", got)
	}
	if got := snap.OutputBytes[StatsPair{"ofd", "pdf"}]; got != 1200 {
		t.Errorf("输出字节 = %d，期望 1200", got)
	}
}

// TestQueueDepthIncludesBackoffJobs 退避中的任务必须算进 queued。
//
// 这是最容易漏的一条：重试任务在 backoff 期间躺在 delayed 队列里，
// 状态是 queued 但不在通道队列中。只数通道队列的话，一次大规模失败重试
// 期间队列深度会显示为 0——而实际有几千个任务在等重试，是最需要告警的时候。
func TestQueueDepthIncludesBackoffJobs(t *testing.T) {
	store, _ := openStore(t)

	// 一个正常排队的任务。
	if err := store.Enqueue(&Job{ID: "ready-1", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	// 一个在退避中的任务：DueAt 在未来，落 delayed 队列。
	if err := store.Enqueue(&Job{ID: "backoff-1", To: "pdf",
		DueAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	depth, err := store.QueueDepth()
	if err != nil {
		t.Fatal(err)
	}
	if depth[StateQueued] != 2 {
		t.Errorf("queued = %d，期望 2（一个就绪、一个退避中）", depth[StateQueued])
	}
	if depth[StateRunning] != 0 {
		t.Errorf("running = %d，期望 0", depth[StateRunning])
	}
}

// TestQueueDepthSeparatesRunning 取出任务后从 queued 移到 running。
func TestQueueDepthSeparatesRunning(t *testing.T) {
	store, _ := openStore(t)
	for i := 0; i < 3; i++ {
		if err := store.Enqueue(&Job{ID: fmt.Sprintf("q-%d", i), To: "pdf"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(LaneFast); err != nil {
		t.Fatal(err)
	}
	depth, err := store.QueueDepth()
	if err != nil {
		t.Fatal(err)
	}
	if depth[StateQueued] != 1 || depth[StateRunning] != 2 {
		t.Errorf("queued=%d running=%d，期望 1/2", depth[StateQueued], depth[StateRunning])
	}
}

// TestQueueDepthIgnoresTerminalStates 终态任务不进队列深度。
//
// 终态任务在 retention 期内会累积到很多，只看"未终结总数"的实现会
// 把它们算进来，深度就永远只涨不跌。
func TestQueueDepthIgnoresTerminalStates(t *testing.T) {
	store, _ := openStore(t)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("done-%d", i)
		if err := store.Enqueue(&Job{ID: id, To: "pdf"}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Claim(LaneFast); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(id, StateSucceeded, Output{Size: 1}, ""); err != nil {
			t.Fatal(err)
		}
	}
	depth, err := store.QueueDepth()
	if err != nil {
		t.Fatal(err)
	}
	if depth[StateQueued] != 0 || depth[StateRunning] != 0 {
		t.Errorf("终态任务应被排除，实际 queued=%d running=%d",
			depth[StateQueued], depth[StateRunning])
	}
}

// TestQueueDepthAcrossLanes 两条通道的任务都要算进总数。
func TestQueueDepthAcrossLanes(t *testing.T) {
	store, _ := openStore(t)
	if err := store.Enqueue(&Job{ID: "f1", Lane: LaneFast, To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(&Job{ID: "h1", Lane: LaneHeavy, To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	depth, err := store.QueueDepth()
	if err != nil {
		t.Fatal(err)
	}
	if depth[StateQueued] != 2 {
		t.Errorf("queued = %d，期望 2（fast 与 heavy 各一）", depth[StateQueued])
	}
}

// TestQueueDepthCancelledNotCounted 已取消的任务不该继续占着深度。
func TestQueueDepthCancelledNotCounted(t *testing.T) {
	store, _ := openStore(t)
	if err := store.Enqueue(&Job{ID: "c1", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel("c1"); err != nil {
		t.Fatal(err)
	}
	depth, err := store.QueueDepth()
	if err != nil {
		t.Fatal(err)
	}
	if depth[StateQueued] != 0 {
		t.Errorf("取消后 queued = %d，期望 0", depth[StateQueued])
	}
}
