package transfer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// testSSHServer 是够用的最小 SSH 服务端，跑真实的 sftp 子系统。
//
// 之所以能测得这么实：x/crypto/ssh 可以在进程内起服务端，pkg/sftp 提供
// RequestServer 处理 sftp 协议请求，于是"主机密钥校验、认证、上传、目录创建、
// 覆盖拒绝"全都是真的走了一遍——不是打桩。
type testSSHServer struct {
	t *testing.T
	// root 是所有远端路径的落盘根目录。
	root string

	hostSigner   ssh.Signer
	clientSigner ssh.Signer
	// clientPrivate 是原始 ed25519 私钥，用来直接生成 PEM。
	// 走 ssh.MarshalPrivateKey 再 ParsePrivateKey 往返一次会得到
	// wrappedSigner，客户端发不出公钥（"unsupported key type"）。
	clientPrivate ed25519.PrivateKey

	listener net.Listener
	wg       sync.WaitGroup

	// mu 保护 sessions 与 liveSessions。
	mu sync.Mutex
	// sessions 是接受的 SSH 连接总数。连接池是否跨任务复用只能看它。
	sessions int
	// liveSessions 是当前存活的连接数。
	liveSessions int
	// mkdirs 统计 MKDIR 次数。目录记忆是否生效只能看它。
	mkdirs int

	// requireUser 是唯一接受的用户名。
	requireUser string
	// password 是接受的口令；为空表示只支持公钥认证。
	password string
	// knownHosts 是给客户端用的，内容为 authorized_keys 格式的一行。
	knownHosts string
}

func newTestSSHServer(t *testing.T) *testSSHServer {
	t.Helper()
	dir := t.TempDir()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	_, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPriv)
	if err != nil {
		t.Fatal(err)
	}
	server := &testSSHServer{
		t: t, root: dir,
		hostSigner: hostSigner, clientSigner: clientSigner, clientPrivate: clientPriv,
		requireUser: "uploader",
		password:    "secret",
	}
	server.knownHosts = fmt.Sprintf("[127.0.0.1]:%d %s", 0, "") // 占位，下面填真实端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.listener = ln
	server.knownHosts = fmt.Sprintf("[127.0.0.1]:%d %s", ln.Addr().(*net.TCPAddr).Port,
		strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostSigner.PublicKey()))))
	go server.acceptLoop()
	t.Cleanup(func() {
		_ = ln.Close()
		// 有限等待，不无限等。
		//
		// 连接池的连接按设计活过任务，所以"服务端所有连接都已关闭"不再是
		// 单个用例结束时成立的前提。无限等会在这里挂死——这不是被测代码
		// 的问题，而是测试的前提过时了。
		server.waitIdle(2 * time.Second)
	})
	return server
}

func (s *testSSHServer) addr() string { return s.listener.Addr().String() }

// sessionCount 返回接受的 SSH 连接总数。连接池是否跨任务复用只能看它。
func (s *testSSHServer) sessionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions
}

// mkdircount 返回 MKDIR 命令次数。
func (s *testSSHServer) mkdircount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mkdirs
}

// idleConnections 返回当前存活的连接数。
func (s *testSSHServer) idleConnections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.liveSessions
}

// waitIdle 等待所有连接处理结束，最多等 d。
func (s *testSSHServer) waitIdle(d time.Duration) {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		// 超时不算失败：连接池的连接本来就可能还开着。剩下的连接会随
		// 测试进程退出而消失。
	}
}

// hostKey 返回服务端主机公钥，供构造 known_hosts 或比对指纹。
func (s *testSSHServer) hostKey() ssh.PublicKey { return s.hostSigner.PublicKey() }

func (s *testSSHServer) knownHostsPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(s.knownHosts+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// clientKeyPEM 把客户端私钥写成 PEM，供"私钥文件"方式使用。
func (s *testSSHServer) clientKeyPEM(t *testing.T) string {
	t.Helper()
	der, err := ssh.MarshalPrivateKey(s.clientPrivate, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(der), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (s *testSSHServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.sessions++
		s.liveSessions++
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				s.liveSessions--
				s.mu.Unlock()
			}()
			s.serve(conn)
		}()
	}
}

func (s *testSSHServer) serve(conn net.Conn) {
	defer conn.Close()
	// 空闲超时：客户端若不干净地断开（进程被杀、断言失败后直接返回），
	// 会话会一直挂着，清理里的 wg.Wait() 就永远等不到。
	defer func() {
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	}()
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if meta.User() != s.requireUser {
				return nil, fmt.Errorf("未知用户 %q", meta.User())
			}
			return s.checkKey(key)
		},
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if meta.User() != s.requireUser {
				return nil, fmt.Errorf("未知用户 %q", meta.User())
			}
			if s.password == "" || string(password) != s.password {
				return nil, fmt.Errorf("口令错误")
			}
			return &ssh.Permissions{}, nil
		},
		ServerVersion: "SSH-2.0-ofd-test",
	}
	config.AddHostKey(s.hostSigner)

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "只支持 session")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}
		go s.serveSession(channel, requests)
	}
}

// serveSession 处理会话内的 sftp 子系统请求。
func (s *testSSHServer) serveSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	for req := range requests {
		// 只认 sftp 子系统，其他（shell/exec）一律拒绝。
		if req.Type != "subsystem" || string(req.Payload[4:]) != "sftp" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		if req.WantReply {
			_ = req.Reply(true, nil)
		}
		// 包一层计数：目录记忆是否生效，只能看服务端收到了几次 MKDIR。
		inner := inMemoryHandlers{root: s.root}
		handlers := sftp.Handlers{
			FileGet:  inner,
			FilePut:  inner,
			FileCmd:  countingFileCmd{inner: inner, count: &s.mkdirs, mu: &s.mu},
			FileList: inner,
		}
		server := sftp.NewRequestServer(channel, handlers)
		_ = server.Serve()
		_ = server.Close()
		return
	}
	_ = channel.Close()
}

func (s *testSSHServer) checkKey(key ssh.PublicKey) (*ssh.Permissions, error) {
	if !bytesEqual(key.Marshal(), s.clientSigner.PublicKey().Marshal()) {
		return nil, fmt.Errorf("公钥不匹配")
	}
	return &ssh.Permissions{}, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// inMemoryHandlers 把 sftp 路径映射到本地目录。
//
// sftp 协议里的路径是绝对路径（/upload/a.pdf），这里统一去掉前导斜杠后
// 拼到测试根目录下——和真实 SFTP 服务端"家目录是某个目录"的行为一致。
type inMemoryHandlers struct {
	root string
}

func (h inMemoryHandlers) local(p string) string {
	cleaned := path.Clean("/" + strings.TrimSpace(p))
	return filepath.Join(h.root, filepath.FromSlash(cleaned))
}

func (h inMemoryHandlers) Fileread(*sftp.Request) (io.ReaderAt, error) {
	return nil, fmt.Errorf("未实现读取")
}

func (h inMemoryHandlers) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	target := h.local(r.Filepath)
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return nil, err
	}
	return os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0o640)
}

func (h inMemoryHandlers) Filecmd(r *sftp.Request) error {
	target := h.local(r.Filepath)
	switch r.Method {
	case "Setstat":
		return nil
	case "Rename":
		to := h.local(r.Target)
		if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
			return err
		}
		return os.Rename(target, to)
	case "Rmdir":
		return os.Remove(target)
	case "Remove":
		return os.Remove(target)
	case "Mkdir":
		return os.MkdirAll(target, 0o750)
	default:
		return fmt.Errorf("未实现的命令 %q", r.Method)
	}
}

func (h inMemoryHandlers) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	target := h.local(r.Filepath)
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	infos := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return sliceLister{infos: infos}, nil
}

// sliceLister 是 sftp.ListerAt 的最小实现（库没有导出构造函数）。
type sliceLister struct {
	infos []os.FileInfo
}

// ListAt 把 [offset, offset+len(f)) 区间的条目填进 f，返回填充条数。
// 返回 io.EOF 表示已到末尾。
func (l sliceLister) ListAt(f []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l.infos)) {
		return 0, io.EOF
	}
	n := copy(f, l.infos[offset:])
	return n, nil
}

func TestSFTPSinkUploadsFile(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr:          server.addr(),
		User:          "uploader",
		Auth:          SFTPAuth{Password: "secret"},
		BaseDir:       "/converted",
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
		Timeout:       15 * time.Second,
		MaxBytes:      1 << 20,
		Overwrite:     true,
	}
	defer sink.Close()

	location, err := sink.Put(context.Background(), "output.pdf", strings.NewReader("%PDF-1.7 内容"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Kind != "sftp" {
		t.Errorf("Kind = %q", location.Kind)
	}
	if location.Path != "/converted/output.pdf" {
		t.Errorf("Path = %q", location.Path)
	}
	if location.Size == 0 {
		t.Error("Size 应为实际写入的字节数")
	}
	got, err := os.ReadFile(filepath.Join(server.root, "converted", "output.pdf"))
	if err != nil {
		t.Fatalf("远端没有该文件: %v", err)
	}
	if string(got) != "%PDF-1.7 内容" {
		t.Errorf("远端内容 = %q", got)
	}
}

// 私钥文件方式：不需要把密钥材料放进配置。
func TestSFTPSinkWithPrivateKey(t *testing.T) {
	server := newTestSSHServer(t)
	// 关闭口令认证，只留公钥。
	server.password = ""
	keyPath := server.clientKeyPEM(t)
	sink := &SFTPSink{
		Addr:          server.addr(),
		User:          "uploader",
		Auth:          SFTPAuth{PrivateKeyFile: keyPath},
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
		Timeout:       15 * time.Second,
		Overwrite:     true,
	}
	defer sink.Close()
	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err != nil {
		t.Fatalf("私钥认证应成功: %v", err)
	}
	if _, err := os.Stat(filepath.Join(server.root, "a.pdf")); err != nil {
		t.Errorf("文件未写入: %v", err)
	}
	// 私钥路径写错时错误信息要指向路径，而不是含糊的"解析失败"。
	bad := &SFTPSink{
		Addr: server.addr(), User: "uploader",
		Auth:          SFTPAuth{PrivateKeyFile: "/不存在/id_ed25519"},
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
		Timeout:       5 * time.Second,
	}
	defer bad.Close()
	_, err := bad.Put(context.Background(), "a.pdf", strings.NewReader("x"))
	if err == nil {
		t.Fatal("私钥不存在应报错")
	}
	if !strings.Contains(err.Error(), "读取私钥文件") {
		t.Errorf("错误信息应说明是读文件失败: %v", err)
	}
}

// known_hosts 方式。
func TestSFTPSinkWithKnownHosts(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader",
		Auth:        SFTPAuth{Password: "secret"},
		HostKeyFile: server.knownHostsPath(t),
		Timeout:     15 * time.Second, Overwrite: true,
	}
	defer sink.Close()
	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err != nil {
		t.Fatalf("known_hosts 校验通过时应能连接: %v", err)
	}
}

// 主机密钥不匹配必须拒绝连接——这是 SFTP 相对明文 FTP 的核心安全保证。
func TestSFTPSinkRejectsWrongHostKey(t *testing.T) {
	server := newTestSSHServer(t)
	// 换一个（正确格式但错误的）指纹。
	other := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader",
		Auth:          SFTPAuth{Password: "secret"},
		HostKeySHA256: ssh.FingerprintSHA256(other.hostKey()),
		Timeout:       5 * time.Second, Overwrite: true,
	}
	defer sink.Close()
	_, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x"))
	if err == nil {
		t.Fatal("主机密钥不匹配必须拒绝")
	}
	if !strings.Contains(err.Error(), "主机密钥不匹配") {
		t.Errorf("错误信息应说明原因: %v", err)
	}
	// 指纹写法差异（小写、缺前缀）不该被当成不匹配。
	loose := &SFTPSink{
		Addr: server.addr(), User: "uploader",
		Auth: SFTPAuth{Password: "secret"},
		// 去掉 "SHA256:" 前缀
		HostKeySHA256: strings.TrimPrefix(ssh.FingerprintSHA256(server.hostKey()), "SHA256:"),
		Timeout:       5 * time.Second, Overwrite: true,
	}
	defer loose.Close()
	if _, err := loose.Put(context.Background(), "a.pdf", strings.NewReader("x")); err != nil {
		t.Errorf("省略 SHA256: 前缀的指纹应被接受: %v", err)
	}
}

// 什么都不配时必须报错，而不是退回"跳过校验"。
func TestSFTPSinkRequiresHostKeyVerification(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader",
		Auth: SFTPAuth{Password: "secret"}, Timeout: 5 * time.Second,
	}
	defer sink.Close()
	_, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x"))
	if err == nil {
		t.Fatal("未配置主机密钥校验应报错")
	}
	if !strings.Contains(err.Error(), "主机密钥校验") {
		t.Errorf("错误信息应说明缺什么: %v", err)
	}
	// 显式声明跳过才放行。
	loose := &SFTPSink{
		Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
		InsecureIgnoreHostKey: true, Timeout: 5 * time.Second, Overwrite: true,
	}
	defer loose.Close()
	if _, err := loose.Put(context.Background(), "a.pdf", strings.NewReader("x")); err != nil {
		t.Errorf("显式跳过校验后应能连接: %v", err)
	}
}

func TestSFTPSinkRequiresAuth(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader",
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
		Timeout:       5 * time.Second,
	}
	defer sink.Close()
	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err == nil {
		t.Error("未配置认证方式应报错")
	}
}

func TestSFTPSinkRejectsUnsafeNames(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
		Timeout:       5 * time.Second,
	}
	defer sink.Close()
	for _, name := range []string{"../x.pdf", "a/b.pdf", `a\b.pdf`, "..", ".", "", "  ", "nul\x00.pdf"} {
		if _, err := sink.Put(context.Background(), name, strings.NewReader("x")); err == nil {
			t.Errorf("文件名 %q 应被拒绝", name)
		}
	}
	// 都应在连上之前就被拒，因此不该产生任何连接副作用。
	if _, err := os.Stat(filepath.Join(server.root, "x.pdf")); err == nil {
		t.Error("非法文件名不该产生文件")
	}
}

func TestSFTPSinkRefusesOverwrite(t *testing.T) {
	server := newTestSSHServer(t)
	newSink := func(overwrite bool) *SFTPSink {
		return &SFTPSink{
			Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
			HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
			Timeout:       10 * time.Second, Overwrite: overwrite,
		}
	}
	first := newSink(false)
	if _, err := first.Put(context.Background(), "a.pdf", strings.NewReader("第一版")); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	second := newSink(false)
	defer second.Close()
	if _, err := second.Put(context.Background(), "a.pdf", strings.NewReader("第二版")); err == nil {
		t.Error("不允许覆盖时同名文件应被拒绝")
	}
	if data, _ := os.ReadFile(filepath.Join(server.root, "a.pdf")); string(data) != "第一版" {
		t.Errorf("原内容被改写: %q", data)
	}
	third := newSink(true)
	defer third.Close()
	if _, err := third.Put(context.Background(), "a.pdf", strings.NewReader("第三版")); err != nil {
		t.Fatalf("允许覆盖时应成功: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(server.root, "a.pdf")); string(data) != "第三版" {
		t.Errorf("内容 = %q，期望被覆盖", data)
	}
}

func TestSFTPSinkRejectsOversize(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
		Timeout:       10 * time.Second, MaxBytes: 32, Overwrite: true,
	}
	defer sink.Close()
	_, err := sink.Put(context.Background(), "big.pdf", strings.NewReader(strings.Repeat("x", 4096)))
	if err == nil {
		t.Fatal("超过上限应报错")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Errorf("错误信息应说明超限: %v", err)
	}
	// 超限时远端不该留下截断的文件。
	if _, statErr := os.Stat(filepath.Join(server.root, "big.pdf")); statErr == nil {
		t.Error("超限时不该留下半截文件")
	}
}

func TestSFTPSinkWithBaseDir(t *testing.T) {
	server := newTestSSHServer(t)
	base := &SFTPSink{Addr: server.addr(), User: "uploader", BaseDir: "/root",
		Auth: SFTPAuth{Password: "secret"}, HostKeySHA256: ssh.FingerprintSHA256(server.hostKey())}
	// 远端路径是绝对路径，前导斜杠要保留。
	cases := []struct{ dir, want string }{
		{"", "/root"},
		{"sub", "/root/sub"},
		{"/sub/", "/root/sub"},
		{"a/b", "/root/a/b"},
	}
	for _, tc := range cases {
		got, err := base.WithBaseDir(tc.dir)
		if err != nil {
			t.Fatalf("WithBaseDir(%q): %v", tc.dir, err)
		}
		if got.BaseDir != tc.want {
			t.Errorf("WithBaseDir(%q).BaseDir = %q，期望 %q", tc.dir, got.BaseDir, tc.want)
		}
		if base.BaseDir != "/root" {
			t.Fatalf("原 BaseDir 被改成 %q", base.BaseDir)
		}
		// 副本共享连接池但不拥有它：不拥有是刻意的——副本在任务收尾时会
		// 调 Close()，若它有权关池，就会掐掉并发任务的连接。
		if got.OwnsPool {
			t.Error("副本不应拥有连接池")
		}
		if err := got.Close(); err != nil {
			t.Errorf("副本 Close(): %v", err)
		}
	}
	// ".." 必须在拼接前拒绝：path.Join 之后它就消失了。
	for _, dir := range []string{"..", "../etc", "a/../../etc", `..\etc`} {
		if got, err := base.WithBaseDir(dir); err == nil {
			t.Errorf("WithBaseDir(%q) 应报错，实际 %q", dir, got.BaseDir)
		}
	}
	// 子目录会真的体现在远端路径上。
	derived, err := base.WithBaseDir("2026/09/28")
	if err != nil {
		t.Fatal(err)
	}
	derived.Overwrite = true
	defer derived.Close()
	location, err := derived.Put(context.Background(), "output.pdf", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Path != "/root/2026/09/28/output.pdf" {
		t.Errorf("远端路径 = %q", location.Path)
	}
	if _, err := os.Stat(filepath.Join(server.root, "root", "2026", "09", "28", "output.pdf")); err != nil {
		t.Errorf("文件未落在预期位置: %v", err)
	}
}

func TestSFTPSinkCloseIsIdempotent(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
		Timeout:       10 * time.Second, Overwrite: true,
	}
	// 最后一次 Put 会新建连接，用例结束前必须关掉，否则服务端会话不会退出、
	// 清理里的 wg.Wait() 会一直等。
	defer sink.Close()
	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := sink.Close(); err != nil {
			t.Fatalf("第 %d 次关闭应成功: %v", i+1, err)
		}
	}
	// 关闭后可重连。
	if _, err := sink.Put(context.Background(), "b.pdf", strings.NewReader("x")); err != nil {
		t.Errorf("关闭后应能重连: %v", err)
	}
}

func TestSFTPSinkRequiresAddr(t *testing.T) {
	sink := &SFTPSink{}
	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err == nil {
		t.Error("未配置地址应报错")
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SHA256:abc", "SHA256:abc"},
		{"sha256:abc", "SHA256:abc"},
		{"abc", "SHA256:abc"},
		{"  SHA256:abc  ", "SHA256:abc"},
		{"", "SHA256:"},
	}
	for _, tc := range cases {
		if got := normalizeFingerprint(tc.in); got != tc.want {
			t.Errorf("normalizeFingerprint(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

// TestSFTPSinkReusesConnectionAcrossTasks 跨任务复用连接。
//
// 池挂在注册目标上、WithBaseDir 派生的实例共享它，所以连续几个任务之间不该
// 重新握手。改造前每个任务一条新连接，几十毫秒的 SSH 握手乘以任务数就是
// 可观的固定开销；逐页输出时更明显。
func TestSFTPSinkReusesConnectionAcrossTasks(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
		BaseDir: "/base", Timeout: 10 * time.Second, Overwrite: true, OwnsPool: true,
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
	}
	defer sink.Close()

	// 三个"任务"：每个都是一次 WithBaseDir + Put + Close。
	//
	// 子目录各不相同：测试服务器的 Stat 对已存在目录返回"不存在"（见
	// TestSSHServerStatReportsMissingDirectories），重复对同一目录建目录
	// 会失败。真实 SFTP 服务器没有这个毛病，但也没必要让这个用例踩它。
	for i := 0; i < 3; i++ {
		derived, err := sink.WithBaseDir(fmt.Sprintf("task-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := derived.Put(context.Background(), "output.pdf", strings.NewReader("x")); err != nil {
			t.Fatalf("第 %d 次上传失败: %v", i+1, err)
		}
		if err := derived.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if got := server.sessionCount(); got != 1 {
		t.Errorf("SSH 会话数 = %d，期望 1（跨任务应复用连接）", got)
	}
}

// TestSFTPSinkDerivedCloseKeepsPoolOpen 派生实例收尾不能关掉共享池。
//
// 这是所有权边界：一个任务结束时若把池关掉，会连带掐掉并发任务的连接。
func TestSFTPSinkDerivedCloseKeepsPoolOpen(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
		BaseDir: "/base", Timeout: 10 * time.Second, Overwrite: true, OwnsPool: true,
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
	}
	defer sink.Close()

	first, err := sink.WithBaseDir("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Put(context.Background(), "one.pdf", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	// 第一个任务收尾。
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	// 第二个任务还能用上池里那条连接。
	second, err := sink.WithBaseDir("b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Put(context.Background(), "two.pdf", strings.NewReader("y")); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if got := server.sessionCount(); got != 1 {
		t.Errorf("SSH 会话数 = %d，期望 1（派生 Close 不该影响池）", got)
	}
}

// TestSFTPSinkClosesPoolOnClose 注册目标关闭时池里的连接要断。
func TestSFTPSinkClosesPoolOnClose(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
		BaseDir: "/base", Timeout: 10 * time.Second, Overwrite: true, OwnsPool: true,
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
	}
	derived, err := sink.WithBaseDir("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := derived.Put(context.Background(), "one.pdf", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if err := derived.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if server.idleConnections() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("注册目标关闭后池里的连接仍未断开，剩余 %d 条", server.idleConnections())
}

// TestSFTPSinkDerivedSharesPool 派生实例必须与原对象共享同一个池。
//
// 这条守住一个曾经真实存在的缺陷：池是惰性建的，若 WithBaseDir 在父的池还是
// nil 时就复制指针，派生实例会各自建一个空池——于是每个任务一条新连接，
// 连接池形同虚设，而所有上传都成功、没有任何错误信号。
func TestSFTPSinkDerivedSharesPool(t *testing.T) {
	server := newTestSSHServer(t)
	sink := &SFTPSink{
		Addr: server.addr(), User: "uploader", Auth: SFTPAuth{Password: "secret"},
		BaseDir: "/base", Timeout: 10 * time.Second, Overwrite: true, OwnsPool: true,
		HostKeySHA256: ssh.FingerprintSHA256(server.hostKey()),
	}
	defer sink.Close()

	derived, err := sink.WithBaseDir("sub")
	if err != nil {
		t.Fatal(err)
	}
	if derived.pool == nil {
		t.Fatal("派生实例的池为 nil")
	}
	if derived.pool != sink.pool {
		t.Errorf("派生实例建了自己的池（%p != %p），连接不会跨任务复用", derived.pool, sink.pool)
	}
}

// countingFileCmd 包一层统计 MKDIR 次数，其余透传。
type countingFileCmd struct {
	inner inMemoryHandlers
	count *int
	mu    *sync.Mutex
}

func (c countingFileCmd) Filecmd(r *sftp.Request) error {
	if r.Method == "Mkdir" {
		c.mu.Lock()
		*c.count++
		c.mu.Unlock()
	}
	return c.inner.Filecmd(r)
}
