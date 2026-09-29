package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/internal/runner"
	"github.com/zc310/ofd/internal/transfer"
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
	// output.dir 是 output_dir 之下的相对子路径，调用方不需要知道 output_dir
	// 配成了什么——那是服务端自己的布局。
	cases := []struct {
		name   string
		dir    string
		accept bool
	}{
		{"单层子目录", "sub", true},
		{"多层子目录", "a/b/c", true},
		{"就是配置目录", ".", true},
		{"点目录后带内容", "./sub", true},
		// 绝对路径要求调用方知道服务端布局，因此不接受。
		{"绝对路径", cfg.OutputDir, false},
		{"绝对路径指向别处", "/etc", false},
		{"上跳逃逸", "../../etc", false},
		{"上跳一段", "a/../../etc", false},
		// Windows 风格分隔符也要挡住：统一规范化后逐段判 ..
		{"反斜杠上跳", `a\..\..\etc`, false},
		{"反斜杠子目录", `a\b`, true},
		{"空字节", "a\x00b", false},
		{"换行", "a\nb", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + payload +
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
	return `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + ofdPayload(t) +
		`"},"output":{"kind":"dir","format":"` + format + `"}}`
}

// TestOutputDirOverridesJobIsolation 传了 dir 就不再套任务 ID。
//
// 调用方写了落点就按它来，不再追加 <job_id>：显式指定是主动放弃隔离。
// 未指定时仍然每个任务独占一个目录，否则所有 dir 输出的任务都往同一处写
// 同一个文件名，第二个必然因不允许覆盖而失败。
func TestOutputDirOverridesJobIsolation(t *testing.T) {
	s, _, cfg := newTestServer(t)
	cfg.OutputDir = filepath.Join(t.TempDir(), "out")
	client := newPipeServer(t, s.Handler())
	payload := ofdPayload(t)

	submit := func(dir string) string {
		body := `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + payload +
			`"},"output":{"format":"pdf","kind":"dir","file_name":"out.pdf","dir":` + mustJSON(t, dir) + `}}`
		got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
		if got.status != fasthttp.StatusAccepted {
			t.Fatalf("提交 dir=%q = %d: %s", dir, got.status, got.body)
		}
		return got.decode(t)["id"].(string)
	}

	// 断言存下来的请求里 dir 被解析成什么：newTestServer 不启动 runner，
	// 任务会停在 queued、Output 还是空的，而 buildJob 写进 Request 的
	// 才是这里要验的解析结果。
	resolvedDir := func(id string) string {
		t.Helper()
		job, err := s.store.Get(id)
		if err != nil || job == nil {
			t.Fatal(err)
		}
		var req struct {
			Output struct {
				Dir string `json:"dir"`
			} `json:"output"`
		}
		if err := json.Unmarshal(job.Request, &req); err != nil {
			t.Fatal(err)
		}
		return req.Output.Dir
	}

	// 指定了 dir：解析成 output_dir/a/b/c，中间不追加任务 ID。
	specified := submit("a/b/c")
	if want := filepath.Join(cfg.OutputDir, "a", "b", "c"); resolvedDir(specified) != want {
		t.Errorf("指定 dir 时落点 = %q，期望 %q（不应追加任务 ID）", resolvedDir(specified), want)
	}
	// 落点不含任务 ID —— 这是这条规则的全部要点。
	if strings.Contains(resolvedDir(specified), specified) {
		t.Errorf("落点 %q 里出现了任务 ID %q", resolvedDir(specified), specified)
	}

	// 未指定：仍然每个任务一个子目录，否则同目录同名文件会互相覆盖失败。
	defaulted := submit("")
	if want := filepath.Join(cfg.OutputDir, defaulted); resolvedDir(defaulted) != want {
		t.Errorf("未指定 dir 时落点 = %q，期望 %q", resolvedDir(defaulted), want)
	}
}

// TestRemotePathValidation remote.path 的校验规则。
//
// 逐段判 `..` 而不是整串 Contains：文件名里带两个点（v1.2、a..b）到处都
// 是，整串匹配会把它们全部误杀，而真正的穿越（.. 作为一段）反而只是它的一个
// 子集。
func TestRemotePathValidation(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		valid bool
	}{
		{"单段", "incoming", true},
		{"多段（bucket 前缀天然多段）", "incoming/2026/08", true},
		{"带点的目录名", "v1.2/ofd", true},
		{"名字里含两个点", "a..b/c", true},
		{"前导斜杠是绝对路径", "/incoming", false},
		{"上跳一段", "../escape", false},
		{"上跳嵌在中间", "a/../../escape", false},
		{"反斜杠上跳", `a\..\..\escape`, false},
		{"反斜杠多段合法", `a\b`, true},
		{"空字节", "a\x00b", false},
		{"换行", "a\nb", false},
		{"超长", strings.Repeat("x", maxRemotePathBytes+1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRemotePath(tc.path)
			if tc.valid && err != nil {
				t.Errorf("validateRemotePath(%q) 应通过: %v", tc.path, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("validateRemotePath(%q) 应被拒绝", tc.path)
			}
		})
	}
}

// TestRemoteOnlyValidForRemoteKinds 本地落点不该带 remote。
//
// 静默忽略会让调用方以为自己传了远程目标、产物其实落在本地——那是比报错
// 难查得多的故障。
func TestRemoteOnlyValidForRemoteKinds(t *testing.T) {
	s, _, _ := newTestServer(t)
	// 注册一个 SFTP 目标，让"缺 target"与"带 remote"两条路径能被区分开。
	s.sftp = map[string]*transfer.SFTPSink{
		"arch": {Addr: "127.0.0.1:1", User: "u"},
	}
	client := newPipeServer(t, s.Handler())
	payload := ofdPayload(t)

	// dir 输出带 remote：拒绝。
	body := `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + payload +
		`"},"output":{"kind":"dir","format":"pdf","remote":{"target":"arch"}}}`
	if got := do(t, client, fasthttp.MethodPost, "/v1/convert", body); got.status != fasthttp.StatusBadRequest {
		t.Errorf("dir 输出带 remote 应被拒，实际 %d: %s", got.status, truncate(got.body))
	}

	// 远端 kind 但没给 target：拒绝，且提示指向新字段名。
	body = `{"input":{"kind":"upload","file_name":"a.ofd","bytes":"` + payload +
		`"},"output":{"kind":"sftp","format":"pdf"}}`
	got := do(t, client, fasthttp.MethodPost, "/v1/convert", body)
	if got.status != fasthttp.StatusBadRequest {
		t.Fatalf("远端 kind 缺 target 应被拒，实际 %d: %s", got.status, got.body)
	}
	if !strings.Contains(got.body, "output.remote.target") {
		t.Errorf("错误信息应指向 output.remote.target，实际: %s", got.body)
	}
}
