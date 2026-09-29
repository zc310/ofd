package main

import (
	"strings"
	"testing"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
)

// withoutOutputKind 从一个合法请求体里去掉 output.kind，模拟"调用方只关心输出
// 格式、没写落点"。
func withoutOutputKind(t *testing.T, to string) string {
	t.Helper()
	return strings.Replace(submitBodyWith(t, to, "dir"), `"kind":"dir"`, `"format":"`+to+`"`, 1)
}

// TestSubmitOmittedOutputKindIsSynchronous 省略 output.kind 必须走同步路径。
//
// 这条守住一个真实缺陷：空 kind 在转换层被规范化为 stream，而 HTTP 层原先用
// 字面量比较 `kind == "stream"` 决定分流。省略字段的请求因此被送进异步队列，
// 随后按 stream 处理——产物只留在内存里随 Result 丢弃。任务记为 succeeded、
// output 字段全空，调用方拿着 202 和一个任务 ID 什么都取不到，而整次转换已经
// 白跑了。
func TestSubmitOmittedOutputKindIsSynchronous(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	got := do(t, client, fasthttp.MethodPost, "/v1/convert", withoutOutputKind(t, "pdf"))
	if got.status != fasthttp.StatusOK {
		t.Fatalf("状态码 = %d，期望 200（省略 kind 等同于 stream）", got.status)
	}
	if !strings.HasPrefix(string(got.body), "%PDF") {
		t.Errorf("响应不是 PDF：%.40q", got.body)
	}
	// 同步执行，不该占用队列。
	if depth, err := store.QueueDepth(); err != nil {
		t.Fatal(err)
	} else if total := depth[jobstore.StateQueued] + depth[jobstore.StateRunning]; total != 0 {
		t.Errorf("队列里有 %d 个任务，同步转换不该入队", total)
	}
}

// TestSubmitOmittedOutputKindImageFormatRejected 省略 kind 且输出图像格式时，
// 错误要落在提交阶段。
//
// 图像格式逐页输出塞不进单文件流，此前省略 kind 会入队、到 worker 才失败。
func TestSubmitOmittedOutputKindImageFormatRejected(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	got := do(t, client, fasthttp.MethodPost, "/v1/convert", withoutOutputKind(t, "png"))
	if got.status != fasthttp.StatusBadRequest {
		t.Fatalf("状态码 = %d，期望 400；实际 %s", got.status, got.body)
	}
	if !strings.Contains(got.body, "请使用 dir") {
		t.Errorf("错误信息应指出改用 dir，实际 %s", got.body)
	}
	if depth, err := store.QueueDepth(); err != nil {
		t.Fatal(err)
	} else if total := depth[jobstore.StateQueued] + depth[jobstore.StateRunning]; total != 0 {
		t.Errorf("队列里有 %d 个任务，提交阶段就该拒掉", total)
	}
}

// TestSubmitUnknownOutputKindRejected 未知的 output.kind 在提交阶段报 400。
//
// 此前 checkRemoteTarget 只在 kind 属于远端类型时才校验，拼错的 kind（如
// "dirr"）一路入队，到 worker 里才失败——那要等几秒，而且任务日志里看不出
// 是 kind 写错了。
func TestSubmitUnknownOutputKindRejected(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	for _, kind := range []string{"dirr", "streamm", "ftp2", "local"} {
		t.Run(kind, func(t *testing.T) {
			body := strings.Replace(submitBodyWith(t, "pdf", "dir"), `"kind":"dir"`, `"kind":"`+kind+`"`, 1)
			got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
			if got.status != fasthttp.StatusBadRequest {
				t.Fatalf("kind=%q 状态码 = %d，期望 400；实际 %s", kind, got.status, got.body)
			}
			if !strings.Contains(got.body, kind) {
				t.Errorf("错误信息应回显 %q，实际 %s", kind, got.body)
			}
		})
	}
	if depth, err := store.QueueDepth(); err != nil {
		t.Fatal(err)
	} else if total := depth[jobstore.StateQueued] + depth[jobstore.StateRunning]; total != 0 {
		t.Errorf("队列里有 %d 个任务，拼错的 kind 不该入队", total)
	}
}

// TestNormalizeOutputKindIsIdempotent 规范化必须幂等。
//
// 提交阶段已经规范化过一次，worker 还会再规范化一次；两次结论不一致就意味着
// 提交时看到的落点与实际落点不同。
func TestNormalizeOutputKindIsIdempotent(t *testing.T) {
	// 只测导出入口的语义，具体的合法值集合在 convertersvc 自己的测试里覆盖。
	for _, in := range []string{"", "stream", "dir", "STREAM", " dir "} {
		once, err := convertersvc.NormalizeOutputKind(in)
		if err != nil {
			t.Fatalf("NormalizeOutputKind(%q) 报错: %v", in, err)
		}
		twice, err := convertersvc.NormalizeOutputKind(once)
		if err != nil {
			t.Fatalf("二次规范化 %q 报错: %v", once, err)
		}
		if once != twice {
			t.Errorf("NormalizeOutputKind 不幂等: %q -> %q -> %q", in, once, twice)
		}
	}
}

// TestSubmitNotifyEventsUnknownRejected 未知事件名必须在提交阶段报 400。
//
// notify.events 接线之后，填一个拼错的事件名意味着这个任务一条通知都收不到。
// 调用方能观察到的只有"没收到"——那是最难查的失败形态，所以不能等到投递时
// 才静默过滤掉。
func TestSubmitNotifyEventsUnknownRejected(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	for _, events := range []string{`["typo"]`, `["succeeded ","succeeded"]`, `[""]`, `["SUCCEEDED"]`} {
		t.Run(events, func(t *testing.T) {
			body := strings.Replace(submitBodyWith(t, "pdf", "dir"),
				`"output":{`, `"notify":{"target":"erp","events":`+events+`},"output":{`, 1)
			got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
			if got.status != fasthttp.StatusBadRequest {
				t.Fatalf("events=%s 状态码 = %d，期望 400；实际 %s", events, got.status, got.body)
			}
		})
	}
	if depth, err := store.QueueDepth(); err != nil {
		t.Fatal(err)
	} else if total := depth[jobstore.StateQueued] + depth[jobstore.StateRunning]; total != 0 {
		t.Errorf("队列里有 %d 个任务，非法事件名不该入队", total)
	}
}

// TestSubmitNotifyEventsAccepted 合法事件名照常受理。
func TestSubmitNotifyEventsAccepted(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	for _, events := range []string{`["succeeded"]`, `["failed"]`, `["succeeded","failed"]`, `["*"]`} {
		t.Run(events, func(t *testing.T) {
			body := strings.Replace(submitBodyWith(t, "pdf", "dir"),
				`"output":{`, `"notify":{"target":"erp","events":`+events+`},"output":{`, 1)
			got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
			if got.status != fasthttp.StatusAccepted {
				t.Fatalf("events=%s 状态码 = %d，期望 202；实际 %s", events, got.status, got.body)
			}
		})
	}
}
