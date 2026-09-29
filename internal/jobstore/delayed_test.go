package jobstore

import (
	"testing"
	"time"
)

// 未到 DueAt 的任务不能被取走，到点后必须自动出现。
func TestDelayedJobNotClaimedBeforeDue(t *testing.T) {
	store, _ := openStore(t)
	now := time.Now()
	job := newJob("j1", LaneFast, OrderNormal)
	job.DueAt = now.Add(time.Hour)
	if err := store.Enqueue(job); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil {
		t.Fatalf("未到期任务不应被取走，实际 %v", claimed.ID)
	}
	// 记录本身应仍处于 queued。
	got, _ := store.Get("j1")
	if got == nil || got.State != StateQueued {
		t.Errorf("未到期任务应保持 queued: %+v", got)
	}
	// 把 DueAt 改到过去并重新入队，这次应能取到。
	got.DueAt = now.Add(-time.Second)
	if err := store.Enqueue(got); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != "j1" {
		t.Fatalf("到期后应取到 j1，实际 %v", claimed)
	}
	if claimed.Attempt != 1 {
		t.Errorf("Attempt = %d", claimed.Attempt)
	}
}

// 延迟队列里的任务到期后应按创建时间先进先出进入通道队列，
// 不能因为它曾经"延迟过"就插到队尾之外。
func TestDelayedJobPromotedFIFO(t *testing.T) {
	store, _ := openStore(t)
	base := time.Now().Add(-2 * time.Hour)
	late := newJob("late", LaneFast, OrderNormal)
	late.CreatedAt = base.Add(time.Minute)
	late.DueAt = time.Now().Add(-time.Minute)
	early := newJob("early", LaneFast, OrderNormal)
	early.CreatedAt = base
	early.DueAt = time.Now().Add(-time.Minute)

	if err := store.Enqueue(late); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(early); err != nil {
		t.Fatal(err)
	}
	first, err := store.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.ID != "early" {
		t.Fatalf("先到期队列中应按创建时间取出 early，实际 %v", first)
	}
	second, err := store.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.ID != "late" {
		t.Fatalf("第二个应是 late，实际 %v", second)
	}
}

// 取消一个还在延迟等待中的任务，之后它不能因为到期而被取走。
func TestCancelDelayedJob(t *testing.T) {
	store, _ := openStore(t)
	job := newJob("j1", LaneFast, OrderNormal)
	job.DueAt = time.Now().Add(50 * time.Millisecond)
	if err := store.Enqueue(job); err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel("j1"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get("j1")
	if got == nil || got.State != StateCancelled {
		t.Fatalf("取消后状态不对: %+v", got)
	}
	time.Sleep(80 * time.Millisecond)
	claimed, err := store.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil {
		t.Errorf("已取消的延迟任务到期后仍被取走: %v", claimed.ID)
	}
}

// 延迟任务到期后仍走各自的通道，不能串道。
func TestDelayedJobRespectsLane(t *testing.T) {
	store, _ := openStore(t)
	heavy := newJob("h1", LaneHeavy, OrderNormal)
	heavy.DueAt = time.Now().Add(-time.Second)
	fast := newJob("f1", LaneFast, OrderNormal)
	fast.DueAt = time.Now().Add(-time.Second)
	if err := store.Enqueue(heavy); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(fast); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != "f1" {
		t.Fatalf("快通道应取到 f1，实际 %v", claimed)
	}
	claimed, err = store.Claim(LaneHeavy)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != "h1" {
		t.Fatalf("重通道应取到 h1，实际 %v", claimed)
	}
}

// 立即可取的任务（DueAt 为零）不受延迟队列影响。
func TestJobWithoutDueAtIsImmediate(t *testing.T) {
	store, _ := openStore(t)
	if err := store.Enqueue(newJob("j1", LaneFast, OrderNormal)); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != "j1" {
		t.Fatalf("无 DueAt 的任务应立即可取，实际 %v", claimed)
	}
}

// 延迟队列的记录必须能跨重启存活，否则重启会丢掉所有等待中的重试。
func TestDelayedJobSurvivesReopen(t *testing.T) {
	store, path := openStore(t)
	job := newJob("j1", LaneFast, OrderNormal)
	job.DueAt = time.Now().Add(-time.Second)
	if err := store.Enqueue(job); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	claimed, err := reopened.Claim(LaneFast)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != "j1" {
		t.Fatalf("重启后应取到 j1，实际 %v", claimed)
	}
}
