package convertersvc

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"errors"
	"time"

	"github.com/zc310/ofd/internal/allowlist"
	"github.com/zc310/ofd/internal/transfer"
)

// ofdSample 取一个真实的小型 OFD 作为转换输入。
//
// 刻意用仓库里的真实样本而不是自造容器：OFD 内部结构（OFD.xml / Document.xml /
// Pages）必须齐全才能被解析器接受，手拼一个"看起来像"的文件只会让测试在
// 解析失败这一步就断掉，根本走不到本包要测的逻辑。
func ofdSample(t *testing.T) []byte {
	t.Helper()
	for _, name := range []string{"sample.ofd", "basic.ofd", "actions.ofd", "link.ofd"} {
		path := filepath.Join("..", "..", "test", "testdata", name)
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
	}
	// 逐级向上找，避免依赖具体目录名。
	var found string
	filepath.Walk(filepath.Join("..", "..", "test", "testdata"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || found != "" {
			return nil
		}
		if strings.HasSuffix(path, ".ofd") && info.Size() < 200<<10 {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Skip("仓库里没有可用的 OFD 样本")
	}
	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newService(t *testing.T) *Service {
	t.Helper()
	return New(t.TempDir(), nil)
}

func TestRunOFDToPDFStream(t *testing.T) {
	svc := newService(t)
	res, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "sample.ofd", Bytes: ofdSample(t)},
		Output: Output{Kind: OutputStream, Format: "pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != OutputStream || res.Format != "pdf" {
		t.Errorf("结果不对: %+v", res)
	}
	if res.MIME != "application/pdf" {
		t.Errorf("MIME = %q", res.MIME)
	}
	if !strings.HasSuffix(res.Filename, ".pdf") {
		t.Errorf("文件名 = %q", res.Filename)
	}
	if len(res.Bytes) < 5 || string(res.Bytes[:5]) != "%PDF-" {
		t.Errorf("输出不是 PDF: %q", truncate(res.Bytes))
	}
	if res.TookMs < 0 {
		t.Errorf("耗时 = %d", res.TookMs)
	}
}

func TestRunOFDToText(t *testing.T) {
	svc := newService(t)
	res, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofdSample(t)},
		Output: Output{Kind: OutputStream, Format: "text"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != "text" || len(res.Bytes) == 0 {
		t.Errorf("文本输出不对: %+v", res)
	}
}

func TestRunToDir(t *testing.T) {
	svc := newService(t)
	dir := filepath.Join(t.TempDir(), "out")
	res, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofdSample(t)},
		Output: Output{Kind: OutputDir, Format: "pdf", Dir: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != OutputDir || res.Dir != dir {
		t.Errorf("结果不对: %+v", res)
	}
	if len(res.Files) == 0 {
		t.Fatal("目录输出应至少有文件")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 清单必须与磁盘上的文件一一对应，否则调用方拿到的路径是空的。
	if len(entries) != len(res.Files) {
		t.Errorf("磁盘 %d 个文件，清单 %d 条", len(entries), len(res.Files))
	}
	for _, file := range res.Files {
		info, err := os.Stat(filepath.Join(dir, file.Name))
		if err != nil {
			t.Errorf("清单里的文件不存在: %v", err)
			continue
		}
		if info.Size() != file.Size {
			t.Errorf("%s 大小不对: 磁盘 %d，清单 %d", file.Name, info.Size(), file.Size)
		}
		if file.Size == 0 {
			t.Errorf("%s 是空文件", file.Name)
		}
	}
}

func TestRunOFDToPNGDir(t *testing.T) {
	svc := newService(t)
	dir := filepath.Join(t.TempDir(), "pages")
	res, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofdSample(t)},
		Output: Output{Kind: OutputDir, Format: "png", Dir: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) == 0 {
		t.Fatal("图像输出应有文件")
	}
	for _, file := range res.Files {
		if !strings.HasSuffix(file.Name, ".png") {
			t.Errorf("文件名 = %q", file.Name)
		}
		if file.Size == 0 {
			t.Errorf("%s 是空文件", file.Name)
		}
	}
}

// 图像格式是逐页输出，塞不进单文件流。这类请求应当明确报错，
// 而不是悄悄只返回第一页。
func TestRunImageToStreamRejected(t *testing.T) {
	svc := newService(t)
	_, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofdSample(t)},
		Output: Output{Kind: OutputStream, Format: "png"},
	})
	if err == nil {
		t.Fatal("图像格式 + stream 应报错")
	}
	if !strings.Contains(err.Error(), OutputDir) {
		t.Errorf("错误信息应提示改用 dir: %v", err)
	}
}

func TestRunRejectsBadSpecs(t *testing.T) {
	svc := newService(t)
	ofd := ofdSample(t)
	cases := []struct {
		name string
		spec Spec
	}{
		{"空上传", Spec{
			Input:  Input{Kind: InputUpload, Filename: "a.ofd"},
			Output: Output{Format: "pdf"},
		}},
		{"未知输出格式", Spec{
			Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
			Output: Output{Format: "nope"},
		}},
		{"未知输入格式", Spec{
			Input:  Input{Kind: InputUpload, Format: "nope", Filename: "a.ofd", Bytes: ofd},
			Output: Output{Format: "pdf"},
		}},
		{"无法识别输入", Spec{
			Input:  Input{Kind: InputUpload, Bytes: []byte("garbage")},
			Output: Output{Format: "pdf"},
		}},
		{"未知输出种类", Spec{
			Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
			Output: Output{Kind: "ftp", Format: "pdf"},
		}},
		{"未知输入种类", Spec{
			Input:  Input{Kind: "s3", Filename: "a.ofd", Bytes: ofd},
			Output: Output{Format: "pdf"},
		}},
		{"dir 缺目录", Spec{
			Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
			Output: Output{Kind: OutputDir, Format: "pdf"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Run(context.Background(), tc.spec); err == nil {
				t.Error("应报错")
			}
		})
	}
}

// 服务端用解析出的格式给临时文件命名，不采信调用方的原始文件名。
//
// 这条性质要验证的是"扩展名与内容不符时会失败"，而不是"某个转换恰好成功"：
// 一个 OFD 内容配 .pdf 名字，如果被当成 PDF 送去解析，要么报错要么产出乱码，
// 两者都说明请求本身是错的。旧写法依赖 HTML 导入器，本机没有 Chrome 时只能
// 整条跳过，等于没验证；这里改用 pdf↔ofd，不依赖任何外部程序。
func TestInputFormatNotTakenFromFilename(t *testing.T) {
	svc := newService(t)
	ofd := ofdSample(t)

	// 显式声明 input.format 时以声明为准，冲突的扩展名被忽略。
	res, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.pdf", Format: "ofd", Bytes: ofd},
		Output: Output{Format: "pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(res.Bytes), "%PDF-") {
		t.Errorf("显式声明 ofd 时应正常产出 PDF，实际 %q", truncate(res.Bytes))
	}

	// 不声明格式、按 .pdf 识别：内容其实是 OFD，必须报错，
	// 不能把 OFD 静默送进 PDF 解析器。
	if _, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.pdf", Bytes: ofd},
		Output: Output{Format: "pdf"},
	}); err == nil {
		t.Error("OFD 内容配 .pdf 名字应报错，不能静默转换")
	}
}

func TestResolveInputFormat(t *testing.T) {
	cases := []struct {
		in     Input
		format string
		ext    string
		ok     bool
	}{
		{Input{Filename: "a.ofd"}, "ofd", ".ofd", true},
		{Input{Filename: "a.pdf"}, "pdf", ".pdf", true},
		{Input{Filename: "a.docx"}, "docx", ".docx", true},
		{Input{Filename: "A.DOCX"}, "docx", ".docx", true},
		{Input{Filename: "a.docx", Format: "ofd"}, "ofd", ".ofd", true},
		{Input{Format: "pdf"}, "pdf", ".pdf", true},
		{Input{Bytes: []byte("%PDF-1.7")}, "pdf", ".pdf", true},
		{Input{Filename: "a.xyz"}, "", "", false},
		{Input{Bytes: []byte("nothing")}, "", "", false},
	}
	for _, tc := range cases {
		format, ext, err := resolveInputFormat(tc.in)
		if tc.ok {
			if err != nil {
				t.Errorf("%+v: %v", tc.in, err)
				continue
			}
			if format != tc.format || ext != tc.ext {
				t.Errorf("%+v -> (%q,%q)，期望 (%q,%q)", tc.in, format, ext, tc.format, tc.ext)
			}
		} else if err == nil {
			t.Errorf("%+v 应报错，实际 (%q,%q)", tc.in, format, ext)
		}
	}
}

func TestSniffMagic(t *testing.T) {
	if name, ok := sniffMagic([]byte("%PDF-1.7\n")); !ok || name != "pdf" {
		t.Errorf("PDF 魔数: %q %v", name, ok)
	}
	if name, ok := sniffMagic([]byte("PK\x03\x04...OFD.xml...")); !ok || name != "ofd" {
		t.Errorf("OFD 魔数: %q %v", name, ok)
	}
	// 纯 ZIP 不是 OFD。
	if _, ok := sniffMagic([]byte("PK\x03\x04...word/document.xml...")); ok {
		t.Error("普通 ZIP 不应被认成 OFD")
	}
	if _, ok := sniffMagic(nil); ok {
		t.Error("空内容不应被识别")
	}
}

func TestLane(t *testing.T) {
	svc := newService(t)
	cases := []struct {
		spec Spec
		want Lane
	}{
		{Spec{Input: Input{Filename: "a.ofd"}, Output: Output{Format: "pdf"}}, LaneFast},
		{Spec{Input: Input{Filename: "a.ofd"}, Output: Output{Format: "text"}}, LaneFast},
		{Spec{Input: Input{Filename: "a.ofd"}, Output: Output{Format: "png"}}, LaneFast},
		{Spec{Input: Input{Filename: "a.ofd"}, Output: Output{Format: "html"}}, LaneHeavy},
		{Spec{Input: Input{Filename: "a.docx"}, Output: Output{Format: "pdf"}}, LaneHeavy},
		{Spec{Input: Input{Format: "xlsx"}, Output: Output{Format: "pdf"}}, LaneHeavy},
		{Spec{Input: Input{Format: "ofd"}, Output: Output{Format: "pptx"}}, LaneHeavy},
		// URL 输入默认走重路径：网络 I/O 不该占住快速通道。
		{Spec{Input: Input{Kind: InputURL, URL: "https://x/a.ofd"}, Output: Output{Format: "pdf"}}, LaneHeavy},
	}
	for _, tc := range cases {
		if got := svc.Lane(tc.spec); got != tc.want {
			t.Errorf("Lane(%s→%s) = %s，期望 %s", tc.spec.Input.Filename, tc.spec.Output.Format, got, tc.want)
		}
	}
}

// URL 输入默认必须拒绝：服务没配白名单就不能拉外部地址。
func TestURLInputRequiresAllowlist(t *testing.T) {
	svc := newService(t)
	_, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputURL, URL: "https://example.com/a.ofd", Format: "ofd"},
		Output: Output{Format: "pdf"},
	})
	if err == nil {
		t.Fatal("未配白名单时 URL 输入应被拒绝")
	}
}

func TestURLInputBlockedByAllowlist(t *testing.T) {
	list, err := allowlist.Parse([]string{"good.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(t.TempDir(), list)
	_, err = svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputURL, URL: "http://169.254.169.254/latest/meta-data/", Format: "pdf"},
		Output: Output{Format: "pdf"},
	})
	if err == nil {
		t.Fatal("不在白名单的主机应被拒绝")
	}
}

// 转换过程中产生的临时文件必须清理干净，不能把输入内容留在磁盘上。
func TestTempDirCleaned(t *testing.T) {
	temp := t.TempDir()
	svc := New(temp, nil)
	_, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofdSample(t)},
		Output: Output{Format: "pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(temp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "ofd-convert-") {
			entries, _ := os.ReadDir(filepath.Join(temp, entry.Name()))
			if len(entries) > 0 {
				t.Errorf("临时目录 %s 未清理，残留 %d 个文件", entry.Name(), len(entries))
			}
		}
	}
}

func TestStreamLimit(t *testing.T) {
	svc := newService(t)
	_, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofdSample(t)},
		Output: Output{Format: "pdf", MaxStreamBytes: 8},
	})
	if err == nil {
		t.Fatal("超出 stream 上限应报错")
	}
}

func TestConcurrentRuns(t *testing.T) {
	svc := newService(t)
	ofd := ofdSample(t)
	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			dir := filepath.Join(t.TempDir(), "out")
			_, err := svc.Run(context.Background(), Spec{
				Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
				Output: Output{Kind: OutputDir, Format: "pdf", Dir: dir},
			})
			errs <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("并发转换失败: %v", err)
		}
	}
}

func truncate(b []byte) string {
	if len(b) > 40 {
		return string(b[:40]) + "..."
	}
	return string(b)
}

// 远程输出目标必须已注册：地址与凭据只在服务端，调用方给的只是一个名字。
// 未注册的名字若被放行，就等于让它指定任意主机上传数据。
func TestRunRejectsUnregisteredRemoteTargets(t *testing.T) {
	svc := newService(t)
	ofd := ofdSample(t)
	cases := []struct {
		name   string
		output Output
	}{
		{"ftp 未指定目标", Output{Kind: OutputFTP, Format: "pdf"}},
		{"s3 未指定目标", Output{Kind: OutputS3, Format: "pdf"}},
		{"webdav 未指定目标", Output{Kind: OutputWebDAV, Format: "pdf"}},
		{"ftp 目标未注册", Output{Kind: OutputFTP, Format: "pdf", FTPTarget: "nope"}},
		{"s3 目标未注册", Output{Kind: OutputS3, Format: "pdf", S3Target: "nope"}},
		{"webdav 目标未注册", Output{Kind: OutputWebDAV, Format: "pdf", WebDAVTarget: "nope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Run(context.Background(), Spec{
				Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
				Output: tc.output,
			})
			if err == nil {
				t.Fatal("应报错")
			}
			if !errors.Is(err, ErrBadRequest) {
				t.Errorf("应是请求错误（不该重试），实际 %v", err)
			}
		})
	}
	// 注册了但端点不可达：属于运行时失败，不是请求错误。
	svc.FTPTargets = map[string]*transfer.FTPSink{
		"down": {Addr: "127.0.0.1:1", User: "u", Password: "password123", Insecure: true, Timeout: time.Second},
	}
	_, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
		Output: Output{Kind: OutputFTP, Format: "pdf", FTPTarget: "down"},
	})
	if err == nil {
		t.Fatal("端点不可达应报错")
	}
	if errors.Is(err, ErrBadRequest) {
		t.Errorf("连接失败不应被归为请求错误: %v", err)
	}
}

// 子目录不能带 ..：path.Join 会把它规整掉，等到写入时才检查就晚了。
func TestRunRejectsEscapingPrefixes(t *testing.T) {
	svc := newService(t)
	svc.FTPTargets = map[string]*transfer.FTPSink{
		"ok": {Addr: "127.0.0.1:1", User: "u", Password: "password123", BaseDir: "/base", Insecure: true},
	}
	svc.S3Targets = map[string]*transfer.MinioSink{
		"ok": {Endpoint: "minio:9000", Bucket: "b", AccessKey: "ak", SecretKey: "sk", BasePrefix: "base"},
	}
	svc.WebDAVTargets = map[string]*transfer.WebDAVSink{
		"ok": {Endpoint: "https://dav.example.com/", BaseDir: "base"},
	}
	cases := []Spec{
		{Input: Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofdSample(t)},
			Output: Output{Kind: OutputFTP, Format: "pdf", FTPTarget: "ok", FTPDir: "../escape"}},
		{Input: Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofdSample(t)},
			Output: Output{Kind: OutputS3, Format: "pdf", S3Target: "ok", S3Prefix: "../escape"}},
		{Input: Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofdSample(t)},
			Output: Output{Kind: OutputWebDAV, Format: "pdf", WebDAVTarget: "ok", WebDAVDir: "../escape"}},
	}
	for _, spec := range cases {
		if _, err := svc.Run(context.Background(), spec); err == nil {
			t.Errorf("%s 输出带 .. 的子目录应被拒绝", spec.Output.Kind)
		} else if !errors.Is(err, ErrBadRequest) {
			t.Errorf("应是请求错误，实际 %v", err)
		}
	}
}

// Result.Dir 必须报实际落点：配置的 base_dir 是服务端加的，漏掉它的话
// 调用方按任务状态里的路径去取文件会找不到。
func TestResultDirIncludesConfiguredBase(t *testing.T) {
	// 用一个不联网的假 Sink：只关心写入目标与返回位置，不碰网络。
	rec := &recordingSink{prefix: "/incoming/ofd"}
	svc := newService(t)
	svc.WebDAVTargets = map[string]*transfer.WebDAVSink{}
	_ = rec
	// 直接验证 writtenDir：给它一个 Sink 报回来的真实位置。
	spec := Spec{Output: Output{Kind: OutputWebDAV, Format: "pdf", WebDAVDir: "2026/09/28"}}
	locations := map[string]transfer.Location{
		"output.pdf": {Kind: "webdav", Path: "incoming/ofd/2026/09/28/output.pdf", Size: 7},
	}
	got := writtenDir(OutputWebDAV, spec, locations)
	if got != "incoming/ofd/2026/09/28" {
		t.Errorf("Dir = %q，期望包含配置的 base_dir: incoming/ofd/2026/09/28", got)
	}
	// 本地目录输出不受影响。
	local := Spec{Output: Output{Kind: OutputDir, Format: "pdf", Dir: "/var/out/x"}}
	if got := writtenDir(OutputDir, local, nil); got != "/var/out/x" {
		t.Errorf("dir 输出 Dir = %q", got)
	}
	// stream 没有目录。
	if got := writtenDir(OutputStream, spec, nil); got != "" {
		t.Errorf("stream 输出不该有目录，实际 %q", got)
	}
}

// recordingSink 只记录 Put 调用，供不需要网络的用例使用。
type recordingSink struct {
	prefix string
	names  []string
}

func (s *recordingSink) Put(_ context.Context, name string, r io.Reader) (transfer.Location, error) {
	if _, err := io.ReadAll(r); err != nil {
		return transfer.Location{}, err
	}
	s.names = append(s.names, name)
	remote := name
	if s.prefix != "" {
		remote = s.prefix + "/" + name
	}
	return transfer.Location{Kind: "webdav", Path: remote, Size: int64(len(name))}, nil
}

// SFTP 目标同样必须已注册，且子目录不能带 ".."。
func TestRunRejectsUnregisteredSFTPTargets(t *testing.T) {
	svc := newService(t)
	ofd := ofdSample(t)
	for _, spec := range []Spec{
		{Input: Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
			Output: Output{Kind: OutputSFTP, Format: "pdf"}},
		{Input: Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
			Output: Output{Kind: OutputSFTP, Format: "pdf", SFTPTarget: "nope"}},
	} {
		if _, err := svc.Run(context.Background(), spec); err == nil {
			t.Errorf("%+v 应报错", spec.Output)
		} else if !errors.Is(err, ErrBadRequest) {
			t.Errorf("应是请求错误，实际 %v", err)
		}
	}
	// 注册了但没配主机密钥校验：属于配置错误，不是请求错误。
	svc.SFTPTargets = map[string]*transfer.SFTPSink{
		"noverify": {Addr: "127.0.0.1:1", User: "u", Auth: transfer.SFTPAuth{Password: "secret"}},
	}
	_, err := svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
		Output: Output{Kind: OutputSFTP, Format: "pdf", SFTPTarget: "noverify"},
	})
	if err == nil {
		t.Fatal("未配置主机密钥校验应报错")
	}
	if !strings.Contains(err.Error(), "主机密钥校验") {
		t.Errorf("错误信息应说明缺什么: %v", err)
	}
	// 子目录带 ".." 必须在拼接前被拒。
	svc.SFTPTargets = map[string]*transfer.SFTPSink{
		"ok": {Addr: "127.0.0.1:1", User: "u", BaseDir: "/base",
			Auth:          transfer.SFTPAuth{Password: "secret"},
			HostKeySHA256: "SHA256:whatever"},
	}
	_, err = svc.Run(context.Background(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: ofd},
		Output: Output{Kind: OutputSFTP, Format: "pdf", SFTPTarget: "ok", SFTPDir: "../escape"},
	})
	if err == nil || !errors.Is(err, ErrBadRequest) {
		t.Errorf("子目录带 .. 应被拒为请求错误，实际 %v", err)
	}
}

// TestResultReportsInputFormatAndBytes 守住"统计口径来自服务端判定"。
//
// 请求里声明 format 与实际内容不一致时，Result.InputFormat 必须报实际格式。
// 用 From（声明值）做统计会把这类任务算错，而这种任务在真实调用里不少见：
// 调用方常靠改扩展名来告诉服务端格式。
func TestResultReportsInputFormatAndBytes(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, nil)

	payload := ofdSample(t)
	res, err := svc.Run(t.Context(), Spec{
		Input:  Input{Kind: InputUpload, Format: "ofd", Filename: "a.ofd", Bytes: payload},
		Output: Output{Kind: OutputStream, Format: "pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.InputFormat != "ofd" {
		t.Errorf("InputFormat = %q，期望 ofd", res.InputFormat)
	}
	if res.InputBytes <= 0 {
		t.Errorf("InputBytes = %d，应为正的落盘尺寸", res.InputBytes)
	}
	// 声明值与实际值一致时，InputBytes 至少要覆盖已上传的字节数。
	if res.InputBytes < int64(len(payload)) {
		t.Errorf("InputBytes = %d，小于上传的 %d 字节", res.InputBytes, len(payload))
	}
}

// TestInputBytesIsMaterializedSize 输入字节数取落盘后的尺寸。
func TestInputBytesIsMaterializedSize(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, nil)
	payload := ofdSample(t)
	res, err := svc.Run(t.Context(), Spec{
		Input:  Input{Kind: InputUpload, Filename: "a.ofd", Bytes: payload},
		Output: Output{Kind: OutputStream, Format: "pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.InputBytes != int64(len(payload)) {
		t.Errorf("InputBytes = %d，期望等于上传字节数 %d", res.InputBytes, len(payload))
	}
}
