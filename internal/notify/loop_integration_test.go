package notify_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/notify"
)

// 接收方前两次返回 500，第三次成功，验证退避重试最终能收敛。
func TestNotifyLoopRetriesUntilSuccess(t *testing.T) {
	var hits atomic.Int32
	var deliveries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		deliveries = append(deliveries, r.Header.Get(notify.HeaderDelivery))
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store, err := jobstore.Open(t.TempDir() + "/jobs.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.RecoverInflight(); err != nil {
		t.Fatal(err)
	}

	deliverer := notify.NewDeliverer(notify.NewRegistry(map[string]notify.Target{
		"erp": {URL: srv.URL, Secret: "k"},
	}), &http.Client{Timeout: 2 * time.Second})

	if err := store.ScheduleDeliveries([]jobstore.Delivery{{
		ID: "d1", JobID: "j1", Target: "erp", Event: notify.EventSucceeded,
		Payload: []byte(`{"job_id":"j1"}`), DueAt: time.Now(), MaxAttempts: 5,
	}}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	for round := 0; round < 8; round++ {
		claimed, err := store.ClaimDueDeliveries(now, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(claimed) == 0 {
			// 没到期就把时钟推后，跳过退避等待。
			now = now.Add(time.Minute)
			continue
		}
		for _, item := range claimed {
			deliveryErr := deliverer.Deliver(context.Background(), notify.Request{
				Target: item.Target, ID: item.ID, JobID: item.JobID,
				Event: item.Event, Payload: item.Payload, Attempt: item.Attempts,
			})
			if deliveryErr == nil {
				store.CompleteDelivery(item.ID)
			} else if notify.Retryable(deliveryErr) {
				store.FailDelivery(item.ID, deliveryErr.Error(), time.Second)
			} else {
				store.FailDelivery(item.ID, deliveryErr.Error(), 0)
			}
		}
	}
	if hits.Load() != 3 {
		t.Errorf("接收方被打了 %d 次，期望 3 次", hits.Load())
	}
	for i, id := range deliveries {
		if id != "d1" {
			t.Errorf("第 %d 次的 X-OFD-Delivery = %q，应始终为 d1", i+1, id)
		}
	}
	done, err := store.ListDeliveries(jobstore.DeliveryDone, 0)
	if err != nil || len(done) != 1 {
		t.Errorf("最终应有一条 done，实际 %d 条 (err=%v)", len(done), err)
	}
}

// 目标未注册时不该空转重试：一次就判死。
func TestNotifyLoopUnknownTargetDiesFast(t *testing.T) {
	store, _ := jobstore.Open(t.TempDir() + "/jobs.db")
	defer store.Close()
	deliverer := notify.NewDeliverer(notify.NewRegistry(nil), nil)
	if err := store.ScheduleDeliveries([]jobstore.Delivery{{
		ID: "d1", Target: "missing", Event: notify.EventSucceeded, DueAt: time.Now(), MaxAttempts: 5,
	}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for round := 0; round < 8; round++ {
		claimed, _ := store.ClaimDueDeliveries(now, 0)
		if len(claimed) == 0 {
			now = now.Add(time.Minute)
			continue
		}
		for _, item := range claimed {
			err := deliverer.Deliver(context.Background(), notify.Request{
				Target: item.Target, ID: item.ID, Event: item.Event, Attempt: item.Attempts,
			})
			if err == nil {
				store.CompleteDelivery(item.ID)
			} else if notify.Retryable(err) {
				store.FailDelivery(item.ID, err.Error(), time.Second)
			} else {
				store.FailDelivery(item.ID, err.Error(), 0)
			}
		}
	}
	abandoned, _ := store.ListDeliveries(jobstore.DeliveryAbandoned, 0)
	if len(abandoned) != 1 {
		t.Errorf("未注册目标应判死为 abandoned，实际 %d 条", len(abandoned))
	}
	pending, _ := store.ListDeliveries(jobstore.DeliveryPending, 0)
	if len(pending) != 0 {
		t.Errorf("不应残留 pending，实际 %d 条", len(pending))
	}
}
