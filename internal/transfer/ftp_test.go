package transfer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testFTPServer 是够用的最小 FTP 服务端，只实现本包测试需要的命令：
// USER/PASS/TYPE/PASV/EPSV/STOR/MKD/SIZE/QUIT。
//
// 自己写而不是引第三方库，是因为没有可用的测试用 FTP 服务端模块；而只测纯逻辑
// 的话，连接、数据通道、STOR 这三段最容易出错的地方反而没覆盖到。
type testFTPServer struct {
	t        *testing.T
	root     string
	user     string
	password string

	mu       sync.Mutex
	listener net.Listener
	// files 记录成功写入的相对路径。
	files []string
	// pasvHostOverride 让 PASV 回报一个与控制连接不同的主机，用来验证
	// 客户端不会盲从（trustPasvIP 的意义）。
	pasvHostOverride string

	conns sync.WaitGroup
}

func newTestFTPServer(t *testing.T, user, password string) *testFTPServer {
	t.Helper()
	server := &testFTPServer{t: t, root: t.TempDir(), user: user, password: password}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.listener = ln
	go server.acceptLoop()
	t.Cleanup(func() {
		_ = ln.Close()
		server.conns.Wait()
	})
	return server
}

func (s *testFTPServer) addr() string { return s.listener.Addr().String() }

// path 解析命令里的路径，限制在测试根目录内。
func (s *testFTPServer) path(arg string) (string, error) {
	cleaned := filepath.Clean("/" + strings.TrimSpace(arg))
	full := filepath.Join(s.root, cleaned)
	if !strings.HasPrefix(full, s.root) {
		return "", fmt.Errorf("路径逃逸: %q", arg)
	}
	return full, nil
}

func (s *testFTPServer) record(remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files = append(s.files, remote)
}

func (s *testFTPServer) written() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.files...)
}

func (s *testFTPServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.conns.Add(1)
		go func() {
			defer s.conns.Done()
			s.serve(conn)
		}()
	}
}

func (s *testFTPServer) serve(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	write := func(format string, args ...any) {
		fmt.Fprintf(conn, format+"\r\n", args...)
	}
	write("220 test FTP")
	authenticated := false
	var dataListener net.Listener
	defer func() {
		if dataListener != nil {
			_ = dataListener.Close()
		}
	}()

	for {
		// 空闲超时：客户端异常退出时不会关闭连接，没有它的话处理协程会一直
		// 阻塞在读上，测试清理里的 conns.Wait() 就永远等不到。
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		line = strings.TrimRight(line, "\r\n")
		verb, arg, _ := strings.Cut(line, " ")
		verb = strings.ToUpper(verb)

		switch verb {
		case "USER":
			if arg == s.user {
				write("331 需要密码")
			} else {
				write("530 用户名错误")
			}
		case "PASS":
			if arg == s.password {
				authenticated = true
				write("230 已登录")
			} else {
				write("530 密码错误")
			}
		case "TYPE":
			write("200 类型已设为 %s", arg)
		case "SYST":
			write("215 UNIX Type: L8")
		case "PWD":
			write("257 \"/\"")
		case "EPSV":
			if !authenticated {
				write("530 未登录")
				continue
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				write("425 无法打开数据通道")
				continue
			}
			port := ln.Addr().(*net.TCPAddr).Port
			write("229 进入扩展被动模式 (|||%d|)", port)
			// 不在这里 Accept：等 STOR/LIST 命令来取用，避免双重 Accept
			// 把连接抢走一半。
			dataListener = ln
		case "PASV":
			if !authenticated {
				write("530 未登录")
				continue
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				write("425 无法打开数据通道")
				continue
			}
			port := ln.Addr().(*net.TCPAddr).Port
			host := "127,0,0,1"
			if s.pasvHostOverride != "" {
				// 让客户端拿到一个与控制连接不同的地址：如果它盲从，就会把
				// 数据发去别处而不是本机。
				parsed := strings.Split(s.pasvHostOverride, ".")
				if len(parsed) == 4 {
					host = strings.Join(parsed, ",")
				}
			}
			high, low := port/256, port%256
			write("227 进入被动模式 (%s,%d,%d)", host, high, low)
			dataListener = ln
		case "MKD":
			target, err := s.path(arg)
			if err != nil {
				write("550 %v", err)
				continue
			}
			if err := os.MkdirAll(target, 0o750); err != nil && !os.IsExist(err) {
				write("550 %v", err)
				continue
			}
			write("257 \"%s\" 已创建", arg)
		case "STOR":
			if !authenticated {
				write("530 未登录")
				continue
			}
			if dataListener == nil {
				write("425 请先使用 PASV 或 EPSV")
				continue
			}
			target, err := s.path(arg)
			if err != nil {
				write("550 %v", err)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				write("550 %v", err)
				continue
			}
			data, err := dataListener.Accept()
			if err != nil {
				write("425 无法接受数据连接")
				dataListener = nil
				continue
			}
			// 必须先发中间响应：客户端在 cmdDataConnFrom 里只接受 125 或 150，
			// 拿不到就既不传数据也不读结果，两边互等。
			write("150 准备接收数据")
			n, copyErr := io.Copy(mustCreate(s.t, target), data)
			_ = data.Close()
			_ = dataListener.Close()
			dataListener = nil
			if copyErr != nil {
				write("451 写入中断: %v", copyErr)
				continue
			}
			s.record(filepath.ToSlash(arg))
			write("226 传输完成（%d 字节）", n)
		case "SIZE":
			target, err := s.path(arg)
			if err != nil {
				write("550 %v", err)
				continue
			}
			info, err := os.Stat(target)
			if err != nil {
				// 不存在时必须回错误码，客户端据此判定"可以写"。
				write("550 请求的动作未采取：文件不存在")
				continue
			}
			write("213 %d", info.Size())
		case "LIST", "NLST":
			// 必须真的列出目录内容：FTPSink 用 List 判断目标文件是否已存在，
			// 空列表会让"不允许覆盖"失效。
			dir, dirErr := s.path(arg)
			var names []string
			if dirErr == nil {
				entries, readErr := os.ReadDir(dir)
				if readErr == nil {
					for _, entry := range entries {
						names = append(names, entry.Name())
					}
				}
			}
			write("150 文件列表")
			if dataListener != nil {
				if data, err := dataListener.Accept(); err == nil {
					for _, name := range names {
						fmt.Fprintf(data, "%s\r\n", name)
					}
					_ = data.Close()
					_ = dataListener.Close()
					dataListener = nil
				}
			}
			write("226 列表结束")
		case "QUIT":
			write("221 再见")
			return
		default:
			write("502 不支持的命令")
		}
	}
}

func mustCreate(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func TestFTPSinkUploadsFile(t *testing.T) {
	server := newTestFTPServer(t, "uploader", "s3cret")
	sink := &FTPSink{
		Addr:      server.addr(),
		User:      "uploader",
		Password:  "s3cret",
		BaseDir:   "/results",
		Insecure:  true,
		Timeout:   10 * time.Second,
		MaxBytes:  1 << 20,
		Overwrite: true,
	}
	defer sink.Close()

	location, err := sink.Put(context.Background(), "output.pdf", strings.NewReader("%PDF-1.7\n内容"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Kind != "ftp" {
		t.Errorf("Kind = %q", location.Kind)
	}
	if location.Path != "/results/output.pdf" {
		t.Errorf("Path = %q，期望 /results/output.pdf", location.Path)
	}
	if location.Size == 0 {
		t.Error("Size 应为实际写入的字节数")
	}
	got, err := os.ReadFile(filepath.Join(server.root, "results", "output.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "%PDF-1.7\n内容" {
		t.Errorf("远端内容 = %q", got)
	}
}

// 同一 Sink 连传多个文件应复用连接，不重复握手。
func TestFTPSinkReusesConnection(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	sink := &FTPSink{
		Addr: server.addr(), User: "u", Password: "p", BaseDir: "/out",
		Insecure: true, Timeout: 10 * time.Second, Overwrite: true,
	}
	defer sink.Close()
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("page-%04d.png", i+1)
		if _, err := sink.Put(context.Background(), name, strings.NewReader("x")); err != nil {
			t.Fatalf("第 %d 次上传失败: %v", i+1, err)
		}
	}
	if got := len(server.written()); got != 4 {
		t.Errorf("远端应有 4 个文件，实际 %d", got)
	}
	// 连接只建立一次。
	if sink.conn == nil {
		t.Error("连接应仍保持打开")
	}
}

func TestFTPSinkCreatesRemoteDirectories(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	sink := &FTPSink{
		Addr: server.addr(), User: "u", Password: "p", BaseDir: "/base",
		Insecure: true, Timeout: 10 * time.Second, Overwrite: true,
	}
	// 带子目录的目标，父目录应当被逐级创建。
	derived, err := sink.WithBaseDir("2026/09/28")
	if err != nil {
		t.Fatal(err)
	}
	defer derived.Close()
	if _, err := derived.Put(context.Background(), "output.pdf", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(server.root, "base", "2026", "09", "28", "output.pdf")); err != nil {
		t.Errorf("父目录未创建: %v", err)
	}
}

func TestFTPSinkWithBaseDir(t *testing.T) {
	base := &FTPSink{Addr: "h:21", BaseDir: "/root", Insecure: true}
	cases := []struct {
		sub  string
		want string
	}{
		{"", "/root"},
		{"a", "/root/a"},
		{"a/b", "/root/a/b"},
		{"/a/", "/root/a"},
		{"./a", "/root/a"},
	}
	for _, tc := range cases {
		got, err := base.WithBaseDir(tc.sub)
		if err != nil {
			t.Fatalf("WithBaseDir(%q): %v", tc.sub, err)
		}
		if got.BaseDir != tc.want {
			t.Errorf("WithBaseDir(%q).BaseDir = %q，期望 %q", tc.sub, got.BaseDir, tc.want)
		}
		// 原对象不能被改：它是配置里的长期单例。
		if base.BaseDir != "/root" {
			t.Fatalf("原 BaseDir 被改成 %q", base.BaseDir)
		}
		// 副本不能共享连接。
		if got.conn != nil {
			t.Error("副本不应继承连接")
		}
	}
	// 未配 BaseDir 时子目录就是 BaseDir。
	empty, err := (&FTPSink{Addr: "h:21", Insecure: true}).WithBaseDir("sub")
	if err != nil {
		t.Fatal(err)
	}
	if empty.BaseDir != "sub" {
		t.Errorf("空 BaseDir + 子目录 = %q", empty.BaseDir)
	}
	// 含 ".." 必须报错，而不是被 path.Join 静默规整到配置目录之外：
	// path.Join("/root", "../etc") 会得到 "/etc"，等到 Put 里检查就晚了。
	for _, sub := range []string{"..", "../etc", "a/../../etc", "a/..", "..\\etc"} {
		if got, err := base.WithBaseDir(sub); err == nil {
			t.Errorf("WithBaseDir(%q) 应报错，实际得到 BaseDir=%q", sub, got.BaseDir)
		}
	}
}

func TestFTPSinkRejectsUnsafeNames(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	sink := &FTPSink{Addr: server.addr(), User: "u", Password: "p", Insecure: true, Timeout: 5 * time.Second}
	defer sink.Close()
	// 名字里带分隔符就能写到配置目录之外，必须拒绝。
	for _, name := range []string{
		"../escape.pdf",
		"a/b.pdf",
		"a\\b.pdf",
		"..",
		".",
		"",
		"   ",
		"nul\x00.pdf",
	} {
		if _, err := sink.Put(context.Background(), name, strings.NewReader("x")); err == nil {
			t.Errorf("文件名 %q 应被拒绝", name)
		}
	}
	// 上面的 Put 都应在连接之前就被拒掉，因此一个文件都不该产生。
	if got := server.written(); len(got) != 0 {
		t.Errorf("非法文件名不该产生上传: %v", got)
	}
	// BaseDir 里带 .. 同样拒绝。
	bad := &FTPSink{Addr: server.addr(), User: "u", Password: "p", BaseDir: "/a/../../etc", Insecure: true}
	defer bad.Close()
	if _, err := bad.Put(context.Background(), "x.pdf", strings.NewReader("x")); err == nil {
		t.Error("BaseDir 含 .. 应被拒绝")
	}
}

// 明文 FTP 必须显式承认，这是有代价的默认。
func TestFTPSinkRefusesPlaintextUnlessAllowed(t *testing.T) {
	sink := &FTPSink{Addr: "127.0.0.1:1", User: "u", Password: "p"}
	_, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x"))
	if err == nil {
		t.Fatal("未声明 Insecure 时应拒绝明文 FTP")
	}
	if !strings.Contains(err.Error(), "Insecure") {
		t.Errorf("错误信息应说明怎么开: %v", err)
	}
	// 显式声明后照常走（地址不可达，但错误应来自连接而不是明文检查）。
	sink.Insecure = true
	_, err = sink.Put(context.Background(), "a.pdf", strings.NewReader("x"))
	if err != nil && strings.Contains(err.Error(), "明文") {
		t.Error("已声明 Insecure 后不该再报明文问题")
	}
}

func TestFTPSinkRejectsOversize(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	sink := &FTPSink{
		Addr: server.addr(), User: "u", Password: "p", Insecure: true,
		Timeout: 10 * time.Second, MaxBytes: 16, Overwrite: true,
	}
	defer sink.Close()
	_, err := sink.Put(context.Background(), "big.pdf", strings.NewReader(strings.Repeat("x", 4096)))
	if err == nil {
		t.Fatal("超过上限应报错")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Errorf("错误信息应说明超限: %v", err)
	}
}

func TestFTPSinkRefusesOverwrite(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	newSink := func(overwrite bool) *FTPSink {
		return &FTPSink{
			Addr: server.addr(), User: "u", Password: "p", Insecure: true,
			Timeout: 10 * time.Second, Overwrite: overwrite,
		}
	}
	first := newSink(false)
	if _, err := first.Put(context.Background(), "a.pdf", strings.NewReader("第一版")); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	// 不允许覆盖时，同名文件必须被拒。
	second := newSink(false)
	defer second.Close()
	if _, err := second.Put(context.Background(), "a.pdf", strings.NewReader("第二版")); err == nil {
		t.Error("不允许覆盖时同名文件应被拒绝")
	}
	// 允许覆盖时应当成功，且内容是新的。
	third := newSink(true)
	defer third.Close()
	if _, err := third.Put(context.Background(), "a.pdf", strings.NewReader("第三版")); err != nil {
		t.Fatalf("允许覆盖时应成功: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(server.root, "a.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "第三版" {
		t.Errorf("内容 = %q，期望被覆盖为第三版", got)
	}
}

func TestFTPSinkLoginFailure(t *testing.T) {
	server := newTestFTPServer(t, "right", "right")
	sink := &FTPSink{Addr: server.addr(), User: "right", Password: "wrong", Insecure: true, Timeout: 5 * time.Second}
	defer sink.Close()
	_, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x"))
	if err == nil {
		t.Fatal("密码错误应报错")
	}
	// 错误信息里带用户名的同时不该带密码。
	if strings.Contains(err.Error(), "wrong") {
		t.Errorf("错误信息泄露了密码: %v", err)
	}
}

// 客户端不该盲从 PASV 回报的地址：否则一个恶意服务器能把数据流引到内网别处。
// 这里让服务端回报 10.255.255.1，数据仍必须落到本机。
func TestFTPSinkIgnoresForeignPASVHost(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	server.pasvHostOverride = "10.255.255.1"
	sink := &FTPSink{
		Addr: server.addr(), User: "u", Password: "p", Insecure: true,
		Timeout: 5 * time.Second, Overwrite: true,
	}
	defer sink.Close()
	// 禁用 EPSV 以强制走 PASV 这条路径。
	sink.DisableEPSV = true
	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err != nil {
		// 连不上 10.255.255.1 也是"没盲从"的一种表现，不算失败。
		return
	}
	// 关键断言：数据必须落到本机。客户端若照着 PASV 回报的 10.255.255.1 去连，
	// 文件就不会出现在这里——那正是一个恶意 FTP 服务器能把服务的数据流引到
	// 内网任意地址的路径。
	if _, statErr := os.Stat(filepath.Join(server.root, "a.pdf")); statErr != nil {
		t.Errorf("数据应落到控制连接所在主机，实际没找到: %v", statErr)
	}
}

func TestFTPSinkCloseIsIdempotent(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	sink := &FTPSink{Addr: server.addr(), User: "u", Password: "p", Insecure: true, Timeout: 5 * time.Second, Overwrite: true}
	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := sink.Close(); err != nil {
			t.Fatalf("第 %d 次关闭应成功: %v", i+1, err)
		}
	}
	// 关闭后可以重新建立连接继续用。
	if _, err := sink.Put(context.Background(), "b.pdf", strings.NewReader("x")); err != nil {
		t.Errorf("关闭后应能重连: %v", err)
	}
}

func TestRedactHidesPassword(t *testing.T) {
	if got := redact("login failed for secret-token", "secret-token"); strings.Contains(got, "secret-token") {
		t.Errorf("密码未被抹掉: %q", got)
	}
	if got := redact("nothing to hide", ""); got != "nothing to hide" {
		t.Errorf("空密码时不应改动: %q", got)
	}
}

func TestCleanRemoteBase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		// 根目录就是 FTP 服务器的根，前导斜杠保留。
		{"/", "/"},
		{"results", "results"},
		// 绝对路径的前导斜杠要保留：远端 "/results" 与 "results" 不是一回事。
		{"/results/", "/results"},
		{"//results//sub//", "/results/sub"},
		{"./a", "a"},
		{`a\b`, "a/b"},
		{"a/../b", "a/../b"}, // cleanRemoteBase 不清理，由调用方拒绝
	}
	for _, tc := range cases {
		if got := cleanRemoteBase(tc.in); got != tc.want {
			t.Errorf("cleanRemoteBase(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestCountingReaderLimits(t *testing.T) {
	r := &countingReader{r: strings.NewReader("0123456789"), limit: 4}
	buf := make([]byte, 8)
	var lastErr error
	for {
		_, err := r.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			lastErr = err
			break
		}
	}
	if lastErr == nil {
		t.Fatal("超过上限应报错")
	}
	if !r.exceeded {
		t.Error("exceeded 标志未置位")
	}
	// 超限之后继续读必须立刻报错，不能再放行数据。
	if _, err := r.Read(buf); err == nil {
		t.Error("超限后应持续报错")
	}
	// 未超限时正常读完。
	ok := &countingReader{r: strings.NewReader("abc"), limit: 10}
	n, err := io.ReadAll(ok)
	if err != nil || string(n) != "abc" || ok.n != 3 {
		t.Errorf("正常读取失败: %q %v %d", n, err, ok.n)
	}
}
