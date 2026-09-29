package transfer

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/allowlist"
)

func mustAllow(t *testing.T, entries ...string) *allowlist.List {
	t.Helper()
	list, err := allowlist.Parse(entries)
	if err != nil {
		t.Fatalf("allowlist.Parse 失败: %v", err)
	}
	return list
}

func TestResolveInRootRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	// 名字里带任何路径成分都必须被拒：这类输入是目录穿越最直接的载体。
	bad := []string{
		"../escape.txt",
		"../../etc/passwd",
		"a/b.txt",
		`a\b.txt`,
		"/etc/passwd",
		"",
		"   ",
		".",
		"..",
		"sub/dir/name.txt",
		`..\..\windows\system32`,
		"nul\x00.txt",
	}
	for _, name := range bad {
		if got, err := ResolveInRoot(root, name); err == nil {
			t.Errorf("ResolveInRoot(%q) 应当拒绝，却得到 %q", name, got)
		}
	}
	good := map[string]string{
		"a.txt":          "a.txt",
		"报告.ofd":         "报告.ofd",
		"a-b_c.1.ofd":    "a-b_c.1.ofd",
		".hidden.ofd":    ".hidden.ofd",
		"  spaced.txt  ": "spaced.txt",
	}
	for name, want := range good {
		got, err := ResolveInRoot(root, name)
		if err != nil {
			t.Errorf("ResolveInRoot(%q) 应当允许: %v", name, err)
			continue
		}
		if filepath.Base(got) != want {
			t.Errorf("ResolveInRoot(%q) = %q，期望文件名 %q", name, got, want)
		}
	}
}

// root 为 "/" 时基于前缀的判断会被绕过，所以实现改为只接受单段文件名。这里锁住
// 该行为，避免后续被"优化"回前缀比较。
func TestResolveInRootRootSlash(t *testing.T) {
	if _, err := ResolveInRoot(string(filepath.Separator), "../etc/passwd"); err == nil {
		t.Error("根目录为 / 时也不得放行穿越路径")
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":      "passwd",
		`..\..\windows\sys.dll`: "sys.dll",
		"plain.ofd":             "plain.ofd",
		"a/b/c.png":             "c.png",
		"带 空格.ofd":              "带 空格.ofd",
		"line\nbreak.txt":       "linebreak.txt",
		"\x00nul.txt":           "nul.txt",
		"...":                   "",
		"":                      "",
		"   ":                   "",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q，期望 %q", in, got, want)
		}
	}
	long := strings.Repeat("x", 200) + ".ofd"
	got := SanitizeName(long)
	if len(got) > 130 {
		t.Errorf("超长名未截断: %d 字节", len(got))
	}
	if !strings.HasSuffix(got, ".ofd") {
		t.Errorf("截断后应保留扩展名: %q", got)
	}
}

func TestCopyLimitedAndReadLimited(t *testing.T) {
	ctx := context.Background()
	data, err := ReadLimited(ctx, strings.NewReader("hello"), 0)
	if err != nil || string(data) != "hello" {
		t.Fatalf("ReadLimited = %q, %v", data, err)
	}
	// 恰好等于上限应当成功。
	if _, err := ReadLimited(ctx, strings.NewReader("12345"), 5); err != nil {
		t.Errorf("恰好到上限应成功: %v", err)
	}
	// 超过上限必须报错而不是截断。
	if _, err := ReadLimited(ctx, strings.NewReader("123456"), 5); err == nil {
		t.Error("超过上限应报错")
	}
	// 分块读取下也要正确累计。
	big := strings.Repeat("a", 300<<10)
	if _, err := ReadLimited(ctx, strings.NewReader(big), 1<<20); err != nil {
		t.Errorf("大内容读取失败: %v", err)
	}
	if _, err := ReadLimited(ctx, strings.NewReader(big), 100<<10); err == nil {
		t.Error("大内容超限应报错")
	}
	// 取消的 context 应当中断。
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ReadLimited(canceled, strings.NewReader("x"), 1<<20); err == nil {
		t.Error("已取消的 context 应报错")
	}
}

func TestBytesSourceAndBufferSink(t *testing.T) {
	src := NewBytesSource([]byte("content"), "a.txt")
	reader, err := src.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := readAll(reader)
	_ = reader.Close()
	if string(body) != "content" {
		t.Errorf("读取内容 = %q", body)
	}
	if src.Name() != "a.txt" {
		t.Errorf("Name = %q", src.Name())
	}
	_ = src.Close()

	sink := NewBufferSink(0)
	loc, err := sink.Put(context.Background(), "a.ofd", strings.NewReader("result"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Kind != "stream" || loc.Size != 6 || string(sink.Bytes()) != "result" {
		t.Errorf("Location = %+v, 内容 = %q", loc, sink.Bytes())
	}
	if _, err := NewBufferSink(4).Put(context.Background(), "a", strings.NewReader("too long")); err == nil {
		t.Error("超过 sink 上限应报错")
	}
}

func TestFileSourceRemovesFileOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := NewFileSource(path, false)
	reader, err := src.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	if src.Name() != "in.txt" {
		t.Errorf("Name = %q", src.Name())
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Close 后临时文件应被删除")
	}
	// 重复 Close 不应报错。
	if err := src.Close(); err != nil {
		t.Errorf("重复 Close 应幂等: %v", err)
	}
	// Keep 模式不得删除。
	keep := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(keep, []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewFileSource(keep, true).Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("Keep 模式下文件不应被删除")
	}
}

func TestDirSinkWritesAndRefuses(t *testing.T) {
	root := t.TempDir()
	sink := NewDirSink(root)

	loc, err := sink.Put(context.Background(), "out.ofd", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Kind != "dir" || filepath.Base(loc.Path) != "out.ofd" || loc.Size != 7 {
		t.Errorf("Location = %+v", loc)
	}
	body, err := os.ReadFile(loc.Path)
	if err != nil || string(body) != "payload" {
		t.Errorf("落盘内容 = %q, %v", body, err)
	}
	// 默认只新建：重复写入同一名字必须失败，不得静默覆盖既有文件。
	if _, err := sink.Put(context.Background(), "out.ofd", strings.NewReader("other")); err == nil {
		t.Error("默认应拒绝覆盖已存在文件")
	}
	body, _ = os.ReadFile(loc.Path)
	if string(body) != "payload" {
		t.Error("失败的写入不应改动原文件")
	}
	// 显式允许覆盖时可以覆盖。
	sink.Overwrite = true
	if _, err := sink.Put(context.Background(), "out.ofd", strings.NewReader("other")); err != nil {
		t.Errorf("允许覆盖时不应失败: %v", err)
	}
	body, _ = os.ReadFile(loc.Path)
	if string(body) != "other" {
		t.Error("覆盖未生效")
	}
	// 穿越路径一律拒绝。
	if _, err := sink.Put(context.Background(), "../x.ofd", strings.NewReader("x")); err == nil {
		t.Error("穿越路径应被拒绝")
	}
	// 未配置根目录时拒绝。
	if _, err := NewDirSink("").Put(context.Background(), "a", strings.NewReader("a")); err == nil {
		t.Error("未配置根目录应拒绝")
	}
	// 超过上限拒绝，且不留下半截文件。
	limited := NewDirSink(t.TempDir())
	if _, err := limited.Put(context.Background(), "big", strings.NewReader(strings.Repeat("a", 100))); err != nil {
		t.Fatal(err)
	}
	limited.MaxBytes = 10
	if _, err := limited.Put(context.Background(), "big2", strings.NewReader(strings.Repeat("a", 100))); err == nil {
		t.Error("超过上限应拒绝")
	}
	entries, _ := os.ReadDir(limited.Root)
	for _, entry := range entries {
		if entry.Name() == "big2" {
			t.Error("失败的写入不应留下目标文件")
		}
		if strings.HasPrefix(entry.Name(), ".ofd-out-") {
			t.Error("临时文件未被清理: " + entry.Name())
		}
	}
}

// 覆盖模式下若目标是符号链接，必须拒绝，避免借链接把内容写到根目录之外。
func TestDirSinkRefusesSymlinkOverwrite(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.ofd")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	sink := NewDirSink(root)
	sink.Overwrite = true
	if _, err := sink.Put(context.Background(), "link.ofd", strings.NewReader("pwned")); err == nil {
		t.Error("覆盖符号链接应被拒绝")
	}
	body, _ := os.ReadFile(outside)
	if string(body) != "original" {
		t.Errorf("链接目标被改写: %q", body)
	}
}

func TestHTTPSourceRefusesWhenNotConfigured(t *testing.T) {
	src := &HTTPSource{URL: "http://127.0.0.1:1/a.ofd"}
	if _, err := src.Open(context.Background()); err == nil {
		t.Error("白名单为空时应拒绝 URL 拉取")
	} else if !strings.Contains(err.Error(), "白名单") {
		t.Errorf("错误信息应说明是白名单问题: %v", err)
	}
	src = &HTTPSource{URL: "http://127.0.0.1:1/a.ofd", Allowlist: mustAllow(t, "127.0.0.1")}
	if err := src.Close(); err != nil {
		t.Errorf("Close 应为空操作: %v", err)
	}
}

func TestHTTPSourceRejectsSchemeAndHost(t *testing.T) {
	list := mustAllow(t, "cdn.example.com")
	cases := []struct{ url, want string }{
		{"file:///etc/passwd", "http/https"},
		{"ftp://cdn.example.com/a.ofd", "http/https"},
		{"gopher://cdn.example.com/a", "http/https"},
		{"http://evil.com/a.ofd", "白名单"},
		{"https://not-allowed.example/a.ofd", "白名单"},
		{"http:///no-host", "主机名"},
	}
	for _, c := range cases {
		src := &HTTPSource{URL: c.url, Allowlist: list}
		_, err := src.Open(context.Background())
		if err == nil {
			t.Errorf("Open(%q) 应当拒绝", c.url)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("Open(%q) 错误信息 = %v，期望提到 %q", c.url, err, c.want)
		}
	}
}

// 走通完整链路：本地测试服务器地址是回环，需要在白名单里显式列出。
func TestHTTPSourceFetchesAllowedHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("请求应带 no-store，实际 %q", r.Header.Get("Cache-Control"))
		}
		switch r.URL.Path {
		case "/a.ofd":
			w.Header().Set("Content-Disposition", `attachment; filename="真实名称.ofd"`)
			_, _ = w.Write([]byte("body"))
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", 1000)))
		case "/redirect":
			http.Redirect(w, r, "http://blocked.invalid/x", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	host, _, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	src := &HTTPSource{URL: server.URL + "/a.ofd", Allowlist: mustAllow(t, host)}

	reader, err := src.Open(context.Background())
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	body, _ := readAll(reader)
	_ = reader.Close()
	if string(body) != "body" {
		t.Errorf("响应体 = %q", body)
	}
	// Content-Disposition 优先于 URL 路径。
	if src.Name() != "真实名称.ofd" {
		t.Errorf("Name = %q，期望取 Content-Disposition", src.Name())
	}
	audit := src.Audit()
	if audit == nil || audit.StatusCode != 200 || len(audit.Resolved) == 0 {
		t.Errorf("审计信息不完整: %+v", audit)
	}
	if len(audit.Resolved) == 0 || audit.Resolved[0] != host {
		t.Errorf("审计应记录解析结果 %q，实际 %v", host, audit.Resolved)
	}
}

func TestHTTPSourceEnforcesSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
	}))
	defer server.Close()
	host, _, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	src := &HTTPSource{URL: server.URL + "/big", Allowlist: mustAllow(t, host), MaxBytes: 100}
	reader, err := src.Open(context.Background())
	if err != nil {
		t.Fatalf("Open 应成功（超限在读取时暴露）: %v", err)
	}
	defer func() { _ = reader.Close() }()
	if _, err := readAll(reader); err == nil {
		t.Error("读取超过上限应报错")
	}
	// 恰好等于上限不应报错。
	exact := &HTTPSource{URL: server.URL + "/big", Allowlist: mustAllow(t, host), MaxBytes: 5000}
	reader2, err := exact.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader2.Close() }()
	if body, err := readAll(reader2); err != nil || len(body) != 5000 {
		t.Errorf("恰好到上限应完整读出: %d 字节, %v", len(body), err)
	}
}

func TestHTTPSourceHTTPStatusAndRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/redirect":
			http.Redirect(w, r, "http://blocked.invalid/x", http.StatusFound)
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer server.Close()
	host, _, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	list := mustAllow(t, host)

	missing := &HTTPSource{URL: server.URL + "/missing", Allowlist: list}
	if _, err := missing.Open(context.Background()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404 应报错并带上状态码: %v", err)
	}

	// 默认不跟随重定向：302 原样返回，落在非 2xx 分支上。
	noFollow := &HTTPSource{URL: server.URL + "/redirect", Allowlist: list}
	if _, err := noFollow.Open(context.Background()); err == nil {
		t.Error("默认应拒绝跟随重定向")
	}

	// 允许跟随时，每一跳仍要过白名单：跳向未列入的主机应被拒。
	follow := &HTTPSource{URL: server.URL + "/redirect", Allowlist: list, AllowRedirect: true}
	_, err = follow.Open(context.Background())
	if err == nil {
		t.Fatal("跳向白名单外的主机应被拒绝")
	}
	if !strings.Contains(err.Error(), "白名单") {
		t.Errorf("错误信息 = %v，期望提到白名单", err)
	}
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				return out, nil
			}
			return out, err
		}
	}
}
