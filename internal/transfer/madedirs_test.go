package transfer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// TestMadeDirsMemo 记忆的基本行为。
func TestMadeDirsMemo(t *testing.T) {
	var m madeDirs
	if m.has("a") {
		t.Error("空记忆不该命中")
	}
	m.add("a")
	if !m.has("a") {
		t.Error("add 之后应命中")
	}
	if m.has("b") {
		t.Error("只应记住被 add 的那一项")
	}
	m.invalidate("a")
	if m.has("a") {
		t.Error("invalidate 之后不该命中")
	}
	m.add("x")
	m.add("y")
	m.forgetAll()
	if m.has("x") || m.has("y") {
		t.Error("forgetAll 应清空全部")
	}
	// 零值必须可用：Sink 的这个字段是值类型，不能是 nil 指针。
}

// TestMadeDirsConcurrent 记忆本身要并发安全——Put 可并发调用。
func TestMadeDirsConcurrent(t *testing.T) {
	var m madeDirs
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.add("shared")
			m.has("shared")
		}()
	}
	wg.Wait()
	if !m.has("shared") {
		t.Error("并发 add 之后应能命中")
	}
}

// TestFTPSinkSkipsRepeatedMkdir 逐页输出时不该每个文件都重跑建目录。
//
// 这是这次改动的全部意义：4 级目录乘 200 个文件，改动前是 800 次 MakeDir
// （已存在时还要再列一次目录区分 550），全是浪费的往返。测试服务器记命令
// 次数，所以能直接验证。
func TestFTPSinkSkipsRepeatedMkdir(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	sink := &FTPSink{
		Addr: server.addr(), User: "u", Password: "p",
		Insecure: true, Timeout: 10 * time.Second, Overwrite: true, OwnsPool: true,
	}
	defer sink.Close()
	derived, err := sink.WithBaseDir("a/b/c")
	if err != nil {
		t.Fatal(err)
	}
	const files = 20
	for i := 0; i < files; i++ {
		name := "page-" + strings.Repeat("0", 2) + string(rune('a'+i)) + ".pdf"
		if _, err := derived.Put(context.Background(), name, strings.NewReader("x")); err != nil {
			t.Fatalf("第 %d 个文件上传失败: %v", i+1, err)
		}
	}
	// 建目录只在第一次：3 次 MKDIR（a、a/b、a/b/c），不是 20×3。
	if got := server.mkdircount(); got > 6 {
		t.Errorf("MKDIR 次数 = %d，%d 个文件只该建 3 级目录（允许少量重试）", got, files)
	}
	if got := len(server.written()); got != files {
		t.Errorf("远端文件数 = %d，期望 %d", got, files)
	}
}

// TestFTPSinkRebuildsDirsAfterFailure 上传失败后要重建目录。
//
// 失败原因可能是"目录建好之后被第三方删了"，此时目录记忆已不可信。不清掉
// 的话，同一任务后续所有文件都会撞同一个错。
func TestFTPSinkRebuildsDirsAfterFailure(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	sink := &FTPSink{
		Addr: server.addr(), User: "u", Password: "p", BaseDir: "/base",
		Insecure: true, Timeout: 10 * time.Second, Overwrite: true, OwnsPool: true,
	}
	defer sink.Close()

	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if !sink.dirs.has("/base") {
		t.Fatal("第一次上传后应记住目录")
	}
	// 模拟"远端目录被删 + 记忆没清"这个状态。
	sink.dirs.forgetAll()
	if _, err := sink.Put(context.Background(), "b.pdf", strings.NewReader("y")); err != nil {
		t.Fatalf("记忆清空后应能重建目录: %v", err)
	}
	if !sink.dirs.has("/base") {
		t.Error("重建后应重新记住目录")
	}
}

// TestFTPSinkDirsMemoIsPerTask 记忆不能跨任务共享。
//
// 这是安全性的一部分：记忆里若混进别的任务建的目录，而那个目录后来被删了，
// 本次任务会一直失败。WithBaseDir 逐字段构造新实例，所以每个任务天然是空的。
func TestFTPSinkDirsMemoIsPerTask(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	sink := &FTPSink{
		Addr: server.addr(), User: "u", Password: "p", BaseDir: "/base",
		Insecure: true, Timeout: 10 * time.Second, Overwrite: true, OwnsPool: true,
	}
	defer sink.Close()

	first, err := sink.WithBaseDir("task-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Put(context.Background(), "x.pdf", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if !first.dirs.has("/base/task-a") {
		t.Fatal("第一个任务应记住自己的目录")
	}
	// 第二个任务是另一个实例，记忆应当是空的。
	second, err := sink.WithBaseDir("task-b")
	if err != nil {
		t.Fatal(err)
	}
	if second.dirs.has("/base/task-a") {
		t.Error("目录记忆跨任务共享了：第二个任务继承了第一个的目录")
	}
	if _, err := second.Put(context.Background(), "y.pdf", strings.NewReader("y")); err != nil {
		t.Fatal(err)
	}
}

// TestSFTPSinkSkipsRepeatedMkdir SFTP 侧同样。
func TestSFTPSinkSkipsRepeatedMkdir(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
		BaseDir: "/base", Timeout: 10 * time.Second, Overwrite: true, OwnsPool: true,
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
	}
	defer sink.Close()

	before := server.mkdircount()
	const files = 10
	for i := 0; i < files; i++ {
		if _, err := sink.Put(context.Background(), "p"+string(rune('a'+i))+".pdf", strings.NewReader("x")); err != nil {
			t.Fatalf("第 %d 个文件上传失败: %v", i+1, err)
		}
	}
	after := server.mkdircount()
	// 第一次会建 /base，之后 9 个文件不该再建。
	if after-before > 4 {
		t.Errorf("MKDIR 次数 = %d（%d 个文件），期望接近 1 —— 目录记忆没生效", after-before, files)
	}
}

// TestDirsMemoSurvivesPoolReuse 目录记忆与连接池互不干扰。
//
// 记忆的内容是"服务端存在这个目录"，与用哪条连接无关；所以换连接不该
// 让记忆失效，也不该因为记忆而跳过必要地建目录。
func TestDirsMemoSurvivesPoolReuse(t *testing.T) {
	server := newTestFTPServer(t, "u", "p")
	sink := &FTPSink{
		Addr: server.addr(), User: "u", Password: "p", BaseDir: "/base",
		Insecure: true, Timeout: 10 * time.Second, Overwrite: true, OwnsPool: true,
		MaxIdle: 1,
	}
	defer sink.Close()

	// 连续上传，连接会被复用；目录记忆让第二次起不再建目录。
	before := server.mkdircount()
	for i := 0; i < 5; i++ {
		if _, err := sink.Put(context.Background(), "f"+string(rune('a'+i))+".pdf", strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	if got := server.mkdircount() - before; got > 2 {
		t.Errorf("5 次上传建了 %d 次目录，期望 1（连接复用不应影响目录记忆）", got)
	}
	// 目录确实存在。
	if _, err := os.Stat(filepath.Join(server.root, "base")); err != nil {
		t.Errorf("目录未创建: %v", err)
	}
}
