package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
)

// seedFinished 造一个已成功、产物已落盘的任务。
func seedFinished(t *testing.T, store *jobstore.Store, id, dir string, files map[string]string) *jobstore.Job {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	job := &jobstore.Job{ID: id, To: "pdf", CreatedAt: time.Now().UTC()}
	if err := store.Enqueue(job); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(jobstore.LaneFast); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	if err := store.RecordUsage(id, "ofd", 100); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(id, jobstore.StateSucceeded, jobstore.Output{
		Kind: convertersvc.OutputDir, Path: dir, Size: 10, Files: names,
	}, ""); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(id)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	return got
}

// TestContentServesSingleFile 只有一个产物时直接返回该文件。
func TestContentServesSingleFile(t *testing.T) {
	s, store, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	dir := filepath.Join(cfg.OutputDir, "out1")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	seedFinished(t, store, "c1", dir, map[string]string{"INV-1.pdf": "%PDF-1.7 fake"})

	got := do(t, client, fasthttp.MethodGet, "/v1/jobs/c1/content", "")
	if got.status != fasthttp.StatusOK {
		t.Fatalf("状态 = %d: %s", got.status, truncate(got.body))
	}
	if got.body != "%PDF-1.7 fake" {
		t.Errorf("响应体 = %q", got.body)
	}
	if got.contentType != "application/pdf" {
		t.Errorf("Content-Type = %q，期望 application/pdf", got.contentType)
	}
	if cd := got.header("Content-Disposition"); !strings.Contains(cd, "INV-1.pdf") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if cc := got.header("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q，产物不该被中间层缓存", cc)
	}
}

// TestContentPicksNamedFile 多个产物时用 name 取指定的。
func TestContentPicksNamedFile(t *testing.T) {
	s, store, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	dir := filepath.Join(cfg.OutputDir, "pages")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	seedFinished(t, store, "c2", dir, map[string]string{
		"page-0001.png": "PNG-1", "page-0002.png": "PNG-2", "page-0003.png": "PNG-3",
	})

	got := do(t, client, fasthttp.MethodGet, "/v1/jobs/c2/content?name=page-0002.png", "")
	if got.status != fasthttp.StatusOK {
		t.Fatalf("状态 = %d: %s", got.status, truncate(got.body))
	}
	if got.body != "PNG-2" {
		t.Errorf("响应体 = %q，期望 PNG-2", got.body)
	}
}

// TestContentZipsMultipleFiles 多个产物且不指定 name 时打包成 zip。
func TestContentZipsMultipleFiles(t *testing.T) {
	s, store, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	dir := filepath.Join(cfg.OutputDir, "pages2")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	seedFinished(t, store, "c3", dir, map[string]string{
		"page-0001.png": "one", "page-0002.png": "two",
	})

	got := do(t, client, fasthttp.MethodGet, "/v1/jobs/c3/content", "")
	if got.status != fasthttp.StatusOK {
		t.Fatalf("状态 = %d: %s", got.status, truncate(got.body))
	}
	if got.contentType != "application/zip" {
		t.Errorf("Content-Type = %q，期望 application/zip", got.contentType)
	}
	// 真的解一遍，确认是合法 zip 且内容齐全。
	zr, err := zip.NewReader(bytes.NewReader([]byte(got.body)), int64(len(got.body)))
	if err != nil {
		t.Fatalf("不是合法 zip: %v", err)
	}
	seen := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		seen[f.Name] = string(data)
	}
	if seen["page-0001.png"] != "one" || seen["page-0002.png"] != "two" {
		t.Errorf("zip 内容 = %v", seen)
	}
}

// TestContentRejectsTraversal name 不能穿越，也不能读到别的任务的文件。
//
// 这是本端点唯一的安全要害：output.dir 允许指向共享目录，同一个目录里可能
// 躺着别的任务的产物。name 必须在该任务自己的记录清单里精确匹配。
func TestContentRejectsTraversal(t *testing.T) {
	s, store, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	dir := filepath.Join(cfg.OutputDir, "shared")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	// 同一个目录里放两个任务的产物。
	seedFinished(t, store, "own", dir, map[string]string{"mine.pdf": "MINE"})
	if err := os.WriteFile(filepath.Join(dir, "secret.env"), []byte("TOKEN=abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 另一个任务，它记录了 secret.env。
	job := &jobstore.Job{ID: "other", To: "pdf", CreatedAt: time.Now().UTC()}
	if err := store.Enqueue(job); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(jobstore.LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("other", jobstore.StateSucceeded, jobstore.Output{
		Kind: convertersvc.OutputDir, Path: dir, Size: 6, Files: []string{"secret.env"},
	}, ""); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"../../etc/passwd",
		"../secret.env",
		"secret.env", // 在磁盘上，但不在 own 的清单里
		"/etc/passwd",
		"sub/../../x",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			got := do(t, client, fasthttp.MethodGet, "/v1/jobs/own/content?name="+name, "")
			if got.status != fasthttp.StatusNotFound {
				t.Errorf("name=%q 状态 = %d，期望 404（不能读到清单外的内容）", name, got.status)
			}
			if strings.Contains(got.body, "TOKEN=abc") {
				t.Errorf("name=%q 泄露了其他任务的内容", name)
			}
		})
	}
	// 另一个任务自己能取到它自己的文件。
	got := do(t, client, fasthttp.MethodGet, "/v1/jobs/other/content?name=secret.env", "")
	if got.status != fasthttp.StatusOK || got.body != "TOKEN=abc" {
		t.Errorf("own 清单外的文件对自己的任务应可取，实际 %d: %q", got.status, got.body)
	}
}

// TestContentRejectsNonDirKinds stream 与远端产物不通过本端点提供。
func TestContentRejectsNonDirKinds(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())

	cases := []struct {
		name string
		out  jobstore.Output
	}{
		{"stream", jobstore.Output{Kind: convertersvc.OutputStream, Path: "output.pdf"}},
		{"s3", jobstore.Output{Kind: convertersvc.OutputS3, Bucket: "b", Key: "k"}},
		{"ftp", jobstore.Output{Kind: convertersvc.OutputFTP, Path: "/remote/x.pdf"}},
		{"sftp", jobstore.Output{Kind: convertersvc.OutputSFTP, Path: "/remote/x.pdf"}},
		{"webdav", jobstore.Output{Kind: convertersvc.OutputWebDAV, Path: "/remote/x.pdf"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := "nk-" + tc.name
			job := &jobstore.Job{ID: id, To: "pdf", CreatedAt: time.Now().UTC()}
			if err := store.Enqueue(job); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Claim(jobstore.LaneFast); err != nil {
				t.Fatal(err)
			}
			if err := store.Finish(id, jobstore.StateSucceeded, tc.out, ""); err != nil {
				t.Fatal(err)
			}
			got := do(t, client, fasthttp.MethodGet, "/v1/jobs/"+id+"/content", "")
			if got.status == fasthttp.StatusOK {
				t.Fatalf("%s 产物不应由本端点提供，实际 200", tc.name)
			}
			// 错误信息要点明为什么，让调用方知道该去哪儿取。
			if !strings.Contains(got.body, tc.name) && !strings.Contains(got.body, "产物不通过本端点") {
				t.Errorf("错误信息应说明原因，实际: %s", truncate(got.body))
			}
		})
	}
}

// TestContentRequiresSucceeded 非成功终态没有产物。
func TestContentRequiresSucceeded(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	for _, state := range []jobstore.State{jobstore.StateFailed, jobstore.StateCancelled} {
		id := "st-" + string(state)
		job := &jobstore.Job{ID: id, To: "pdf", CreatedAt: time.Now().UTC()}
		if err := store.Enqueue(job); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Claim(jobstore.LaneFast); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(id, state, jobstore.Output{}, "原因"); err != nil {
			t.Fatal(err)
		}
		got := do(t, client, fasthttp.MethodGet, "/v1/jobs/"+id+"/content", "")
		if got.status != fasthttp.StatusConflict {
			t.Errorf("%s 状态 = %d，期望 409", state, got.status)
		}
	}
}

// TestContentRejectsUnfinished 未到终态不该给产物。
func TestContentRejectsUnfinished(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	if err := store.Enqueue(&jobstore.Job{ID: "pending", To: "pdf"}); err != nil {
		t.Fatal(err)
	}
	got := do(t, client, fasthttp.MethodGet, "/v1/jobs/pending/content", "")
	if got.status != fasthttp.StatusConflict {
		t.Errorf("状态 = %d，期望 409（任务还在排队）", got.status)
	}
}

// TestContentRejectsPathOutsideOutputDir 记录里的路径若不在 output_dir 之下就拒绝。
//
// 这一层防的是"配置被改过"：把 output_dir 指到别处之后，历史任务记录的路径
// 仍然有效，但已不在新配置范围内，不该继续可读。
func TestContentRejectsPathOutsideOutputDir(t *testing.T) {
	s, store, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "x.pdf"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedFinished(t, store, "esc", outside, nil)

	got := do(t, client, fasthttp.MethodGet, "/v1/jobs/esc/content?name=x.pdf", "")
	if got.status == fasthttp.StatusOK {
		t.Fatalf("output_dir 之外的产物不该可读，实际 200: %q", got.body)
	}
	if strings.Contains(got.body, "SECRET") {
		t.Errorf("泄露了 output_dir 之外的文件内容")
	}
}

// TestContentMissingFileOnDisk 记录里有、磁盘上没有了（被外部清理）。
func TestContentMissingFileOnDisk(t *testing.T) {
	s, store, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	dir := filepath.Join(cfg.OutputDir, "gone")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	// 只写记录，不落盘。
	job := &jobstore.Job{ID: "gone1", To: "pdf", CreatedAt: time.Now().UTC()}
	if err := store.Enqueue(job); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(jobstore.LaneFast); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish("gone1", jobstore.StateSucceeded, jobstore.Output{
		Kind: convertersvc.OutputDir, Path: dir, Files: []string{"absent.pdf"},
	}, ""); err != nil {
		t.Fatal(err)
	}
	got := do(t, client, fasthttp.MethodGet, "/v1/jobs/gone1/content", "")
	if got.status != fasthttp.StatusNotFound {
		t.Errorf("状态 = %d，期望 404（文件已被外部清理）", got.status)
	}
}

// TestContentRequiresAPIKey 产物属于业务数据，要令牌。
func TestContentRequiresAPIKey(t *testing.T) {
	s, store, cfg := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	dir := filepath.Join(cfg.OutputDir, "k")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	seedFinished(t, store, "k1", dir, map[string]string{"a.pdf": "A"})
	if got := doAuth(t, client, fasthttp.MethodGet, "/v1/jobs/k1/content", "", ""); got.status != fasthttp.StatusUnauthorized {
		t.Errorf("无令牌时状态 = %d，期望 401", got.status)
	}
}

// TestContentJobNotFound 不存在的任务。
func TestContentJobNotFound(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	got := do(t, client, fasthttp.MethodGet, "/v1/jobs/nope/content", "")
	if got.status != fasthttp.StatusNotFound {
		t.Errorf("状态 = %d，期望 404", got.status)
	}
}

// TestRecordedFilesFiltering 清单里含路径分隔符的条目要被剔除。
func TestRecordedFilesFiltering(t *testing.T) {
	in := []string{"ok.pdf", "../escape.pdf", `a\b.pdf`, "..", ".", "", "sub/dir.pdf"}
	got := recordedFiles(in)
	want := []string{"ok.pdf"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("recordedFiles(%v) = %v，期望 %v", in, got, want)
	}
}

// TestEnsureInside 目录包含性检查。
func TestEnsureInside(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	cases := []struct {
		path  string
		valid bool
	}{
		{filepath.Join(dir, "a.pdf"), true},
		{filepath.Join(dir, "sub", "a.pdf"), true},
		{filepath.Join(dir, "..", "etc", "passwd"), false},
		{dir, true},
		{"/etc/passwd", false},
	}
	for _, tc := range cases {
		err := ensureInside(dir, tc.path)
		if tc.valid && err != nil {
			t.Errorf("ensureInside(%q) 应通过: %v", tc.path, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("ensureInside(%q) 应被拒", tc.path)
		}
	}
}

// TestContentTypeFor 扩展名到 Content-Type。
func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"a.pdf": "application/pdf",
		"a.png": "image/png",
		"a.svg": "image/svg+xml",
		"a.zzz": "application/octet-stream",
		"noext": "application/octet-stream",
	}
	for name, want := range cases {
		if got := contentTypeFor(name); got != want {
			t.Errorf("contentTypeFor(%q) = %q，期望 %q", name, got, want)
		}
	}
}

// TestContentJSONOnError 错误响应仍是 JSON，便于客户端解析。
func TestContentJSONOnError(t *testing.T) {
	s, _, _ := newTestServer(t)
	client := newPipeServer(t, s.Handler())
	got := do(t, client, fasthttp.MethodGet, "/v1/jobs/nope/content", "")
	var payload map[string]any
	if err := json.Unmarshal([]byte(got.body), &payload); err != nil {
		t.Fatalf("错误响应不是 JSON: %v\n%s", err, got.body)
	}
	if payload["code"] == nil {
		t.Errorf("错误响应缺少 code: %s", got.body)
	}
}
