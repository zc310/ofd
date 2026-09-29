package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/runner"
)

// dir 输出的任务必须互不干扰。
//
// 曾经所有任务都往同一个 output_dir 写 "output.pdf"，而 DirSink 默认不覆盖，
// 于是第二个 dir 任务（哪怕顺序执行）必然以"输出文件已存在"失败。
// 现在按任务 ID 隔离到子目录。
func TestConcurrentDirOutputIsolated(t *testing.T) {
	s, store, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	dir := t.TempDir()
	cfg.TempDir = filepath.Join(dir, "tmp")
	cfg.OutputDir = filepath.Join(dir, "out")
	for _, d := range []string{cfg.TempDir, cfg.OutputDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	jobRunner := runner.New(store, convertersvc.New(cfg.TempDir, nil), nil,
		runner.Config{FastWorkers: 4, HeavyWorkers: 1, PollInterval: 10 * time.Millisecond}, testLogger())
	if err := jobRunner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer jobRunner.Stop()

	const count = 4
	body := dirSubmitBody(t, "pdf")
	ids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
		ids = append(ids, got.decode(t)["id"].(string))
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		done := 0
		for _, id := range ids {
			job, _ := store.Get(id)
			if job.State.Terminal() {
				done++
			}
		}
		if done == count {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	// 全部成功，且各自落在自己的子目录里。
	for _, id := range ids {
		job, _ := store.Get(id)
		if job.State != jobstore.StateSucceeded {
			t.Errorf("任务 %s 状态 = %s，错误 = %q", id, job.State, job.Error)
			continue
		}
		want := filepath.Join(cfg.OutputDir, id)
		if job.Output.Path != want {
			t.Errorf("任务 %s 输出目录 = %q，期望 %q", id, job.Output.Path, want)
		}
		if _, err := os.Stat(filepath.Join(want, "output.pdf")); err != nil {
			t.Errorf("任务 %s 的产物不存在: %v", id, err)
		}
	}
	// 子目录互不相同。
	seen := map[string]bool{}
	for _, id := range ids {
		sub := filepath.Join(cfg.OutputDir, id)
		if seen[sub] {
			t.Errorf("输出目录重复: %s", sub)
		}
		seen[sub] = true
	}
}

// output.dir 的越界检查不能被路径技巧绕过。
func TestOutputDirContainment(t *testing.T) {
	s, _, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	cfg.OutputDir = filepath.Join(t.TempDir(), "out")

	payload := ofdPayload(t)
	cases := []struct {
		name   string
		dir    string
		accept bool
	}{
		{"配置目录之下", filepath.Join(cfg.OutputDir, "sub"), true},
		{"就是配置目录", cfg.OutputDir, true},
		{"别处", "/etc", false},
		{"前缀相似", cfg.OutputDir + "-other", false},
		// 前缀比较会被这种输入绕过：Clean 之后落到 /etc。
		{"上跳逃逸", filepath.Join(cfg.OutputDir, "..", "..", "etc"), false},
		{"上跳回配置目录", filepath.Join(cfg.OutputDir, "..", filepath.Base(cfg.OutputDir)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"input":{"kind":"upload","filename":"a.ofd","bytes":"` + payload +
				`"},"output":{"format":"pdf","kind":"dir","dir":` + mustJSON(t, tc.dir) + `}}`
			got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
			if tc.accept && got.status != fasthttp.StatusAccepted {
				t.Errorf("状态 = %d，期望 202: %s", got.status, got.body)
			}
			if !tc.accept && got.status != fasthttp.StatusBadRequest {
				t.Errorf("状态 = %d，期望 400: %s", got.status, got.body)
			}
		})
	}
}

func mustJSON(t *testing.T, v string) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func dirSubmitBody(t *testing.T, format string) string {
	t.Helper()
	return `{"input":{"kind":"upload","filename":"a.ofd","bytes":"` + ofdPayload(t) +
		`"},"output":{"kind":"dir","format":"` + format + `"}}`
}
