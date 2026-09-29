package jobstore

import (
	"sync"
	"testing"
	"time"
)

func TestScheduleAndClaimDeliveries(t *testing.T) {
	store, _ := openStore(t)
	now := time.Now().UTC()
	items := []Delivery{
		{ID: "d-due", Target: "erp", Event: "succeeded", Payload: []byte(`{"a":1}`), DueAt: now.Add(-time.Minute)},
		{ID: "d-later", Target: "erp", Event: "failed", Payload: []byte(`{"a":2}`), DueAt: now.Add(time.Hour)},
	}
	if err := store.ScheduleDeliveries(items); err != nil {
		t.Fatal(err)
	}
	// 缺 ID 或 Target 必须被拒。
	for _, bad := range []Delivery{{Target: "erp"}, {ID: "d"}} {
		if err := store.ScheduleDeliveries([]Delivery{bad}); err == nil {
			t.Errorf("缺字段的投递记录应被拒绝: %+v", bad)
		}
	}
	if err := store.ScheduleDeliveries(nil); err != nil {
		t.Errorf("空批次应无操作: %v", err)
	}

	// 只有到期的会被取走。
	claimed, err := store.ClaimDueDeliveries(now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != "d-due" {
		t.Fatalf("取到 %v，期望 d-due", ids(claimed))
	}
	if claimed[0].State != DeliveryInflight || claimed[0].Attempts != 1 {
		t.Errorf("取件后状态不对: %+v", claimed[0])
	}
	if claimed[0].MaxAttempts != 3 {
		t.Errorf("MaxAttempts 应补默认值 3，实际 %d", claimed[0].MaxAttempts)
	}
	// 已取走的不会重复取。
	if again, err := store.ClaimDueDeliveries(now, 0); err != nil {
		t.Fatal(err)
	} else if len(again) != 0 {
		t.Errorf("重复取到 %v", ids(again))
	}
	// 未来到期的仍未到期。
	if claimed, err := store.ClaimDueDeliveries(now.Add(time.Hour), 0); err != nil {
		t.Fatal(err)
	} else if len(claimed) != 1 || claimed[0].ID != "d-later" {
		t.Errorf("推进时间后应取到 d-later，实际 %v", ids(claimed))
	}
	// 不存在的记录应报错。
	if err := store.CompleteDelivery("nope"); err == nil {
		t.Error("对不存在的记录应报错")
	}
}

func TestDeliveryRetryBackoffAndAbandon(t *testing.T) {
	store, _ := openStore(t)
	now := time.Now().UTC()
	if err := store.ScheduleDeliveries([]Delivery{
		{ID: "d1", Target: "erp", Event: "succeeded", DueAt: now, MaxAttempts: 2},
	}); err != nil {
		t.Fatal(err)
	}

	// 第一次投递失败后应回到 pending 并把到期时间推后。
	first, err := store.ClaimDueDeliveries(now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Attempts != 1 {
		t.Fatalf("首次取件: %+v", first)
	}
	if err := store.FailDelivery("d1", "连接超时", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	pending, err := store.ListDeliveries(DeliveryPending, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].LastError != "连接超时" {
		t.Fatalf("失败后应回到 pending 并记下原因: %+v", pending)
	}
	if !pending[0].DueAt.After(now) {
		t.Errorf("到期时间应推后，实际 %v", pending[0].DueAt)
	}
	if !pending[0].DueAt.After(now) || pending[0].DueAt.Before(now.Add(time.Second)) {
		t.Errorf("退避时间不在预期区间: %v", pending[0].DueAt)
	}

	// 第二次取件累加尝试次数。
	second, err := store.ClaimDueDeliveries(now.Add(time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Attempts != 2 {
		t.Fatalf("第二次取件: %+v", second)
	}

	// 达到上限后再失败即放弃。
	if err := store.FailDelivery("d1", "再次超时", 0); err != nil {
		t.Fatal(err)
	}
	abandoned, err := store.ListDeliveries(DeliveryAbandoned, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(abandoned) != 1 || abandoned[0].LastError != "再次超时" {
		t.Errorf("超过重试上限应转为 abandoned: %+v", abandoned)
	}
	// 放弃后不再被取到。
	if claimed, err := store.ClaimDueDeliveries(now.Add(24*time.Hour), 0); err != nil {
		t.Fatal(err)
	} else if len(claimed) != 0 {
		t.Errorf("已放弃的又被取到: %v", ids(claimed))
	}
	if err := store.FailDelivery("missing", "x", 0); err == nil {
		t.Error("对不存在的记录应报错")
	}
}

func TestCompleteDelivery(t *testing.T) {
	store, _ := openStore(t)
	now := time.Now().UTC()
	if err := store.ScheduleDeliveries([]Delivery{{ID: "d1", Target: "erp", DueAt: now}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimDueDeliveries(now, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteDelivery("d1"); err != nil {
		t.Fatal(err)
	}
	done, err := store.ListDeliveries(DeliveryDone, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0].ID != "d1" {
		t.Errorf("完成记录不对: %+v", done)
	}
	// 已完成的不会再被取到。
	if claimed, err := store.ClaimDueDeliveries(now.Add(time.Hour), 0); err != nil {
		t.Fatal(err)
	} else if len(claimed) != 0 {
		t.Errorf("已完成的又被取到: %v", ids(claimed))
	}
}

// 与任务同一道理：崩溃时停在 inflight 的通知外部无从察觉，必须重新排队。
func TestRecoverInflight(t *testing.T) {
	store, _ := openStore(t)
	now := time.Now().UTC()
	if err := store.ScheduleDeliveries([]Delivery{
		{ID: "inflight-1", Target: "erp", DueAt: now},
		{ID: "inflight-2", Target: "erp", DueAt: now},
		{ID: "pending-1", Target: "erp", DueAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimDueDeliveries(now, 2); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.RecoverInflight()
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 2 {
		t.Errorf("恢复数量 = %d，期望 2", recovered)
	}
	pending, _ := store.ListDeliveries(DeliveryPending, 0)
	if len(pending) != 3 {
		t.Errorf("恢复后全部应为 pending，实际 %d 条: %+v", len(pending), pending)
	}
	// 幂等。
	if again, err := store.RecoverInflight(); err != nil {
		t.Fatal(err)
	} else if again != 0 {
		t.Errorf("重复恢复数量 = %d，期望 0", again)
	}
	// 恢复的记录能再次取到。
	claimed, err := store.ClaimDueDeliveries(now.Add(time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 3 {
		t.Errorf("恢复后应取到 3 条，实际 %d", len(claimed))
	}
}

// 多个投递协程并发取件不得取到同一条：取出与标记 inflight 在同一写事务内。
func TestClaimDueDeliveriesConcurrentNeverDuplicate(t *testing.T) {
	store, _ := openStore(t)
	now := time.Now().UTC()
	const total = 150
	items := make([]Delivery, 0, total)
	for i := 0; i < total; i++ {
		items = append(items, Delivery{ID: idf(i), Target: "erp", DueAt: now})
	}
	if err := store.ScheduleDeliveries(items); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := make(map[string]int)
	var workers = 6
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				claimed, err := store.ClaimDueDeliveries(now, 7)
				if err != nil {
					t.Errorf("取件失败: %v", err)
					return
				}
				if len(claimed) == 0 {
					return
				}
				mu.Lock()
				for _, item := range claimed {
					seen[item.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != total {
		t.Errorf("共取到 %d 条，期望 %d", len(seen), total)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("%s 被取到 %d 次", id, count)
		}
	}
}

func TestPruneDeliveriesKeepsAbandoned(t *testing.T) {
	store, _ := openStore(t)
	now := time.Now().UTC()
	if err := store.ScheduleDeliveries([]Delivery{
		{ID: "done", Target: "erp", DueAt: now},
		{ID: "abandoned", Target: "erp", DueAt: now, MaxAttempts: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimDueDeliveries(now, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteDelivery("done"); err != nil {
		t.Fatal(err)
	}
	if err := store.FailDelivery("abandoned", "重试用尽", 0); err != nil {
		t.Fatal(err)
	}
	removed, err := store.PruneDeliveries(now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("清理数量 = %d，期望 1（abandoned 必须保留）", removed)
	}
	if left, _ := store.ListDeliveries(DeliveryAbandoned, 0); len(left) != 1 {
		t.Errorf("abandoned 记录应保留，实际 %d 条", len(left))
	}
	// 数据跨重启存活。
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func ids(items []Delivery) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

func idf(i int) string { return "d" + string(rune('a'+i%26)) + string(rune('a'+i/26)) }
