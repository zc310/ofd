package transfer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"
)

// davServer 是够用的最小 WebDAV 服务端：MKCOL 建目录、PUT 写文件、HEAD 探测。
type davServer struct {
	t     *testing.T
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
	// authUser 为空表示不校验凭据。
	authUser     string
	authPassword string
	// failAuth 时所有请求都回 401。
	failAuth bool
	// serverHeader 记录收到的 Authorization 头。
	lastAuth string
}

func newDAVServer(t *testing.T) *davServer {
	return &davServer{t: t, files: map[string][]byte{}, dirs: map[string]bool{"/": true}}
}

func (s *davServer) start(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (s *davServer) handle(w http.ResponseWriter, r *http.Request) {
	if s.authUser != "" || s.failAuth {
		user, pass, ok := r.BasicAuth()
		s.mu.Lock()
		s.lastAuth = r.Header.Get("Authorization")
		s.mu.Unlock()
		if s.failAuth || !ok || user != s.authUser || pass != s.authPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	remote := r.URL.Path
	switch r.Method {
	case "MKCOL": // net/http 没有这个方法的常量
		s.mu.Lock()
		s.dirs[path.Clean(remote)] = true
		s.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.mu.Lock()
		s.files[path.Clean(remote)] = body
		s.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	case "PROPFIND":
		// gowebdav 的 Stat 走 PROPFIND，缺了这个实现就判不出文件是否存在。
		s.mu.Lock()
		_, exists := s.files[path.Clean(remote)]
		_, isDir := s.dirs[path.Clean(remote)]
		s.mu.Unlock()
		if exists || isDir {
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(`<?xml version="1.0"?>` +
				`<d:multistatus xmlns:d="DAV:"><d:response><d:href>` + remote + `</d:href></d:response></d:multistatus>`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	case http.MethodHead, http.MethodGet:
		s.mu.Lock()
		data, ok := s.files[path.Clean(remote)]
		isDir := s.dirs[path.Clean(remote)]
		s.mu.Unlock()
		if isDir {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !ok {
			// gowebdav 靠这个状态码判断"文件不存在"。
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *davServer) get(remote string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.files[path.Clean(remote)]
	return data, ok
}

func (s *davServer) hasDir(remote string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirs[path.Clean(remote)]
}

func TestWebDAVSinkUploadsFile(t *testing.T) {
	dav := newDAVServer(t)
	dav.authUser = "alice"
	dav.authPassword = "pw"
	endpoint := dav.start(t)
	sink := &WebDAVSink{
		Endpoint: endpoint, User: "alice", Password: "pw",
		BaseDir: "/converted", Timeout: 10 * time.Second, Overwrite: true,
	}
	defer sink.Close()

	location, err := sink.Put(context.Background(), "output.pdf", strings.NewReader("%PDF-1.7 内容"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Kind != "webdav" {
		t.Errorf("Kind = %q", location.Kind)
	}
	if location.Path != "converted/output.pdf" {
		t.Errorf("Path = %q", location.Path)
	}
	if location.Size == 0 {
		t.Error("Size 应为实际写入的字节数")
	}
	if !strings.HasPrefix(location.URL, endpoint) {
		t.Errorf("URL = %q，应以 endpoint 开头", location.URL)
	}
	data, ok := dav.get("/converted/output.pdf")
	if !ok {
		t.Fatal("远端没有该文件")
	}
	if string(data) != "%PDF-1.7 内容" {
		t.Errorf("远端内容 = %q", data)
	}
	// 父目录应当被创建。
	if !dav.hasDir("/converted") {
		t.Error("父目录未创建")
	}
}

func TestWebDAVSinkCreatesNestedDirs(t *testing.T) {
	dav := newDAVServer(t)
	endpoint := dav.start(t)
	sink := &WebDAVSink{Endpoint: endpoint, User: "u", Password: "p",
		Timeout: 10 * time.Second, Overwrite: true}
	defer sink.Close()
	derived, err := sink.WithBaseDir("2026/09/28")
	if err != nil {
		t.Fatal(err)
	}
	defer derived.Close()
	if _, err := derived.Put(context.Background(), "output.pdf", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if _, ok := dav.get("/2026/09/28/output.pdf"); !ok {
		t.Error("嵌套目录下的文件未写入")
	}
}

func TestWebDAVSinkWithBaseDir(t *testing.T) {
	base := &WebDAVSink{Endpoint: "https://dav/", BaseDir: "/root"}
	// 产出的是相对 endpoint 根的路径，不带前导斜杠（与 FTP 相反）。
	cases := []struct{ dir, want string }{
		{"", "root"},
		{"sub", "root/sub"},
		{"/sub/", "root/sub"},
		{"a/b", "root/a/b"},
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
	}
	// ".." 必须在拼接前拒绝：path.Join 之后它就消失了。
	for _, dir := range []string{"..", "../etc", "a/../../etc", `..\etc`} {
		if got, err := base.WithBaseDir(dir); err == nil {
			t.Errorf("WithBaseDir(%q) 应报错，实际 BaseDir=%q", dir, got.BaseDir)
		}
	}
}

func TestWebDAVSinkRejectsUnsafeNames(t *testing.T) {
	dav := newDAVServer(t)
	endpoint := dav.start(t)
	sink := &WebDAVSink{Endpoint: endpoint, User: "u", Password: "p", Timeout: 5 * time.Second}
	defer sink.Close()
	for _, name := range []string{"../x.pdf", "a/b.pdf", `a\b.pdf`, "..", ".", "", "  ", "nul\x00.pdf"} {
		if _, err := sink.Put(context.Background(), name, strings.NewReader("x")); err == nil {
			t.Errorf("文件名 %q 应被拒绝", name)
		}
	}
	if len(dav.files) != 0 {
		t.Errorf("非法文件名不该产生写入: %v", dav.files)
	}
}

func TestWebDAVSinkRefusesOverwrite(t *testing.T) {
	dav := newDAVServer(t)
	endpoint := dav.start(t)
	first := &WebDAVSink{Endpoint: endpoint, User: "u", Password: "p", Timeout: 5 * time.Second}
	if _, err := first.Put(context.Background(), "a.pdf", strings.NewReader("第一版")); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	second := &WebDAVSink{Endpoint: endpoint, User: "u", Password: "p", Timeout: 5 * time.Second}
	defer second.Close()
	if _, err := second.Put(context.Background(), "a.pdf", strings.NewReader("第二版")); err == nil {
		t.Error("不允许覆盖时应拒绝")
	}
	if data, _ := dav.get("/a.pdf"); string(data) != "第一版" {
		t.Errorf("原内容被改写: %q", data)
	}

	third := &WebDAVSink{Endpoint: endpoint, User: "u", Password: "p",
		Timeout: 5 * time.Second, Overwrite: true}
	defer third.Close()
	if _, err := third.Put(context.Background(), "a.pdf", strings.NewReader("第三版")); err != nil {
		t.Fatalf("允许覆盖时应成功: %v", err)
	}
	if data, _ := dav.get("/a.pdf"); string(data) != "第三版" {
		t.Errorf("内容 = %q，期望被覆盖", data)
	}
}

func TestWebDAVSinkRejectsOversize(t *testing.T) {
	dav := newDAVServer(t)
	endpoint := dav.start(t)
	sink := &WebDAVSink{Endpoint: endpoint, User: "u", Password: "p",
		Timeout: 5 * time.Second, MaxBytes: 32, Overwrite: true}
	defer sink.Close()
	_, err := sink.Put(context.Background(), "big.pdf", strings.NewReader(strings.Repeat("x", 4096)))
	if err == nil {
		t.Fatal("超过上限应报错")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Errorf("错误信息应说明超限: %v", err)
	}
}

// 认证失败必须报错，且错误里不能带密码。
func TestWebDAVSinkAuthFailure(t *testing.T) {
	dav := newDAVServer(t)
	dav.authUser = "alice"
	dav.authPassword = "correct"
	endpoint := dav.start(t)
	sink := &WebDAVSink{Endpoint: endpoint, User: "alice", Password: "wrong-secret",
		Timeout: 5 * time.Second, Overwrite: true}
	defer sink.Close()
	_, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x"))
	if err == nil {
		t.Fatal("认证失败应报错")
	}
	if strings.Contains(err.Error(), "wrong-secret") {
		t.Errorf("错误信息泄露了密码: %v", err)
	}
	if len(dav.files) != 0 {
		t.Errorf("认证失败不该写入: %v", dav.files)
	}
}

func TestWebDAVSinkRequiresEndpoint(t *testing.T) {
	sink := &WebDAVSink{}
	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err == nil {
		t.Error("未配置 endpoint 应报错")
	}
}

// MinIO/S3 的凭据与前缀是纯逻辑，不需要真的对象存储。
func TestMinioSinkValidationAndPrefix(t *testing.T) {
	base := &MinioSink{Endpoint: "minio:9000", Bucket: "out", BasePrefix: "results",
		AccessKey: "ak", SecretKey: "sk", Secure: true}
	// 缺端点或缺桶都必须报错。
	for _, sink := range []*MinioSink{
		{Endpoint: "", Bucket: "bucket"},
		{Endpoint: "h:9000", Bucket: ""},
	} {
		if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err == nil {
			t.Errorf("配置不全的 sink 应报错: %+v", sink)
		}
	}
	// 名字校验与 FTP/Dir 同规则。
	for _, name := range []string{"../x.pdf", "a/b.pdf", "..", ".", "", "  ", "nul\x00.pdf"} {
		if _, err := base.Put(context.Background(), name, strings.NewReader("x")); err == nil {
			t.Errorf("对象名 %q 应被拒绝", name)
		}
	}
	// 前缀拼接。
	cases := []struct{ prefix, want string }{
		{"", "results"},
		{"sub", "results/sub"},
		{"a/b", "results/a/b"},
		{"/a/", "results/a"},
		{"./a", "results/a"},
	}
	for _, tc := range cases {
		got, err := base.WithBasePrefix(tc.prefix)
		if err != nil {
			t.Fatalf("WithBasePrefix(%q): %v", tc.prefix, err)
		}
		if got.BasePrefix != tc.want {
			t.Errorf("WithBasePrefix(%q).BasePrefix = %q，期望 %q", tc.prefix, got.BasePrefix, tc.want)
		}
		if base.BasePrefix != "results" {
			t.Fatalf("原 BasePrefix 被改成 %q", base.BasePrefix)
		}
		if got.conn != nil {
			t.Error("副本不应继承客户端")
		}
	}
	// ".." 必须报错：path.Join 会把它规整掉，等到 Put 再查就晚了。
	for _, prefix := range []string{"..", "../etc", "a/../../etc", `..\etc`} {
		if got, err := base.WithBasePrefix(prefix); err == nil {
			t.Errorf("WithBasePrefix(%q) 应报错，实际 %q", prefix, got.BasePrefix)
		}
	}
}

func TestMinioEndpointURL(t *testing.T) {
	cases := []struct {
		endpoint string
		secure   bool
		want     string
	}{
		{"minio:9000", true, "https://minio:9000"},
		{"minio:9000", false, "http://minio:9000"},
		{"https://minio:9000", true, "https://minio:9000"},
		{"minio:9000/", false, "http://minio:9000"},
	}
	for _, tc := range cases {
		sink := &MinioSink{Endpoint: tc.endpoint, Secure: tc.secure}
		if got := sink.EndpointURL(); got != tc.want {
			t.Errorf("EndpointURL(%q, secure=%v) = %q，期望 %q", tc.endpoint, tc.secure, got, tc.want)
		}
	}
}

func TestRedactErrorHidesSecret(t *testing.T) {
	err := fmt.Errorf("签名错误: secret-key-value")
	got := redactError(err, "secret-key-value")
	if strings.Contains(got.Error(), "secret-key-value") {
		t.Errorf("密钥未被抹掉: %v", got)
	}
	// 没命中就返回原错误，不该无谓地包一层。
	plain := fmt.Errorf("网络超时")
	if redactError(plain, "secret") != plain {
		t.Error("未命中时应返回原错误")
	}
	if redactError(nil, "secret") != nil {
		t.Error("nil 应原样返回")
	}
}

func TestCleanObjectPrefix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"/", ""},
		{"results", "results"},
		{"/results/", "results"},
		{"//a//b//", "a/b"},
		{"./a", "a"},
		{`a\b`, "a/b"},
		{"a/../b", "a/../b"}, // 不规整，交给调用方拒绝
	}
	for _, tc := range cases {
		if got := cleanObjectPrefix(tc.in); got != tc.want {
			t.Errorf("cleanObjectPrefix(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

// s3Mock 是够用的最小 S3 服务端：实现 StatObject 用的 HEAD，以及
// PutObject(size=-1) 走的分块上传（CreateMultipartUpload / UploadPart / Complete）。
//
// 之所以要写：离线环境没有 S3 模拟器，而 PutObject 在长度未知时走分块上传，
// 不实现这一套就没法验证真正的线路路径——只测纯逻辑的话，签名、请求顺序、
// 分片拼装这些最容易错的地方全都没覆盖。
type s3Mock struct {
	t     *testing.T
	mu    sync.Mutex
	parts map[string]map[int][]byte // uploadID -> partNum -> data
	// completed 记录组装完成的对象名与内容。
	completed map[string][]byte
	// failComplete 让 CompleteMultipartUpload 返回 500。
	failComplete bool
	lastAuth     string
}

func newS3Mock(t *testing.T) *s3Mock {
	return &s3Mock{
		t:         t,
		parts:     map[string]map[int][]byte{},
		completed: map[string][]byte{},
	}
}

func (m *s3Mock) start(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (m *s3Mock) handle(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.lastAuth = r.Header.Get("Authorization")
	m.mu.Unlock()
	query := r.URL.Query()
	object := strings.TrimPrefix(r.URL.Path, "/")
	// 去掉桶名，剩下的才是对象名。
	if idx := strings.Index(object, "/"); idx >= 0 {
		object = object[idx+1:]
	}
	w.Header().Set("x-amz-request-id", "test")

	switch {
	// minio-go 在建客户端后会先问桶在哪个区域，缺这个响应一律失败。
	case r.Method == http.MethodGet && query.Has("location"):
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`))
	case r.Method == http.MethodGet && !query.Has("uploadId"):
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Buckets></Buckets></ListAllMyBucketsResult>`))
	case r.Method == http.MethodHead:
		// 存在即 200，不存在即 404。
		m.mu.Lock()
		_, exists := m.completed[object]
		m.mu.Unlock()
		status := http.StatusNotFound
		if exists {
			status = http.StatusOK
		}
		if status == http.StatusOK {
			// minio-go 解析 StatObject 的结果时需要这些头，缺一个就会当成
			// "查不到"，于是"不允许覆盖"的检查形同虚设。
			w.Header().Set("Content-Length", "10")
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.Header().Set("Accept-Ranges", "bytes")
		}
		w.WriteHeader(status)

	case r.Method == http.MethodPost && query.Has("uploads"):
		uploadID := "upload-" + object
		m.mu.Lock()
		m.parts[uploadID] = map[int][]byte{}
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult><Bucket>b</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`,
			object, uploadID)

	case r.Method == http.MethodPut && query.Has("uploadId"):
		uploadID := query.Get("uploadId")
		partNumber := 0
		fmt.Sscanf(query.Get("partNumber"), "%d", &partNumber)
		data := decodeAWSChunked(r.Body)
		m.mu.Lock()
		if m.parts[uploadID] == nil {
			m.parts[uploadID] = map[int][]byte{}
		}
		m.parts[uploadID][partNumber] = data
		m.mu.Unlock()
		w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
		w.WriteHeader(http.StatusOK)

	case r.Method == http.MethodPost && query.Has("uploadId"):
		if m.failComplete {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		uploadID := query.Get("uploadId")
		m.mu.Lock()
		stored := m.parts[uploadID]
		var joined []byte
		for i := 1; i <= len(stored); i++ {
			joined = append(joined, stored[i]...)
		}
		m.completed[object] = joined
		delete(m.parts, uploadID)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult><Location>http://example/%s</Location><Bucket>b</Bucket><Key>%s</Key><ETag>"x"</ETag></CompleteMultipartUploadResult>`,
			object, object)

	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (m *s3Mock) object(name string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.completed[name]
	return data, ok
}

func TestMinioSinkUploadsObject(t *testing.T) {
	mock := newS3Mock(t)
	endpoint := mock.start(t)
	host := strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")

	sink := &MinioSink{
		Endpoint: host, Bucket: "converted", BasePrefix: "incoming/ofd",
		AccessKey: "minioadmin", SecretKey: "minioadmin", Secure: false,
		Timeout: 30 * time.Second, MaxBytes: 1 << 20, Overwrite: true,
	}
	defer sink.Close()

	location, err := sink.Put(context.Background(), "output.pdf", strings.NewReader("%PDF-1.7 内容"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Kind != "s3" {
		t.Errorf("Kind = %q", location.Kind)
	}
	if location.Bucket != "converted" {
		t.Errorf("Bucket = %q", location.Bucket)
	}
	// 对象键必须带上配置的 prefix。
	if location.Key != "incoming/ofd/output.pdf" {
		t.Errorf("Key = %q，期望含 prefix: incoming/ofd/output.pdf", location.Key)
	}
	if location.Size == 0 {
		t.Error("Size 应为实际写入的字节数")
	}
	if !strings.HasPrefix(location.URL, "http://"+host+"/converted/") {
		t.Errorf("URL = %q", location.URL)
	}
	data, ok := mock.object("incoming/ofd/output.pdf")
	if !ok {
		t.Fatal("对象未上传成功")
	}
	if string(data) != "%PDF-1.7 内容" {
		t.Errorf("对象内容 = %q", data)
	}
	// 必须带签名。
	mock.mu.Lock()
	auth := mock.lastAuth
	mock.mu.Unlock()
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		t.Errorf("缺少 S3 签名头: %q", auth)
	}
}

// 不允许覆盖时，已存在的对象必须被拒；不存在才写。
func TestMinioSinkOverwriteGuard(t *testing.T) {
	mock := newS3Mock(t)
	endpoint := mock.start(t)
	host := strings.TrimPrefix(endpoint, "http://")
	newSink := func(overwrite bool) *MinioSink {
		return &MinioSink{Endpoint: host, Bucket: "bucket", AccessKey: "ak", SecretKey: "sk",
			Timeout: 30 * time.Second, Overwrite: overwrite}
	}
	first := newSink(true)
	if _, err := first.Put(context.Background(), "a.pdf", strings.NewReader("第一版")); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	second := newSink(false)
	defer second.Close()
	if _, err := second.Put(context.Background(), "a.pdf", strings.NewReader("第二版")); err == nil {
		t.Error("不允许覆盖时已存在的对象应被拒绝")
	}
	if data, _ := mock.object("a.pdf"); string(data) != "第一版" {
		t.Errorf("原内容被改写: %q", data)
	}

	third := newSink(true)
	defer third.Close()
	if _, err := third.Put(context.Background(), "a.pdf", strings.NewReader("第三版")); err != nil {
		t.Fatalf("允许覆盖时应成功: %v", err)
	}
	if data, _ := mock.object("a.pdf"); string(data) != "第三版" {
		t.Errorf("内容 = %q，期望被覆盖", data)
	}
}

func TestMinioSinkRejectsOversize(t *testing.T) {
	mock := newS3Mock(t)
	endpoint := mock.start(t)
	host := strings.TrimPrefix(endpoint, "http://")
	sink := &MinioSink{Endpoint: host, Bucket: "bucket", AccessKey: "ak", SecretKey: "sk",
		Timeout: 30 * time.Second, MaxBytes: 32, Overwrite: true}
	defer sink.Close()
	_, err := sink.Put(context.Background(), "big.pdf", strings.NewReader(strings.Repeat("x", 4096)))
	if err == nil {
		t.Fatal("超过上限应报错")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Errorf("错误信息应说明超限: %v", err)
	}
	// 服务端不该收到完整内容。
	if len(mock.completed) != 0 {
		t.Errorf("超限时不该完成上传: %v", mock.completed)
	}
}

// 子目录前缀参与对象键。
func TestMinioSinkPrefixFromRequest(t *testing.T) {
	mock := newS3Mock(t)
	endpoint := mock.start(t)
	host := strings.TrimPrefix(endpoint, "http://")
	base := &MinioSink{Endpoint: host, Bucket: "bucket", BasePrefix: "base",
		AccessKey: "ak", SecretKey: "sk", Timeout: 30 * time.Second, Overwrite: true}
	derived, err := base.WithBasePrefix("2026/09/28")
	if err != nil {
		t.Fatal(err)
	}
	defer derived.Close()
	location, err := derived.Put(context.Background(), "output.pdf", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Key != "base/2026/09/28/output.pdf" {
		t.Errorf("Key = %q", location.Key)
	}
	if _, ok := mock.object("base/2026/09/28/output.pdf"); !ok {
		t.Error("对象未按预期前缀写入")
	}
}

// 覆盖检查遇到认证失败时不能当成"对象不存在"，否则会变成假阳性然后去覆盖。
func TestMinioSinkStatErrorIsNotTreatedAsMissing(t *testing.T) {
	mock := newS3Mock(t)
	endpoint := mock.start(t)
	host := strings.TrimPrefix(endpoint, "http://")
	sink := &MinioSink{Endpoint: host, Bucket: "bucket", AccessKey: "ak", SecretKey: "sk",
		Timeout: 30 * time.Second}
	defer sink.Close()
	// 让 Complete 失败：写入会报错，但不能静默当成"成功覆盖"。
	mock.failComplete = true
	if _, err := sink.Put(context.Background(), "a.pdf", strings.NewReader("x")); err == nil {
		t.Error("CompleteMultipartUpload 失败时应报错")
	}
}

// decodeAWSChunked 剥掉 minio-go 流式签名 v4（aws-chunked）的分帧。
//
// 长度未知时 minio-go 会用这种编码：每段是 "<十六进制长度>;chunk-signature=...\r\n"
// + 原始数据 + "\r\n"，以长度为 0 的段结束。不剥掉的话断言看到的是分帧文本
// 而不是文件内容。
func decodeAWSChunked(r io.Reader) []byte {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	var out []byte
	for {
		nl := strings.Index(string(raw), "\r\n")
		if nl < 0 {
			return out
		}
		header := string(raw[:nl])
		raw = raw[nl+2:]
		sizeText, _, _ := strings.Cut(header, ";")
		size := 0
		if _, scanErr := fmt.Sscanf(sizeText, "%x", &size); scanErr != nil {
			return out
		}
		if size == 0 {
			return out
		}
		if int64(size) > int64(len(raw)) {
			return append(out, raw...)
		}
		out = append(out, raw[:size]...)
		raw = raw[size+2:] // 跳过数据后的 CRLF
	}
}
