// Package convertersvc 把 pkg/converter 的转换能力包成可被任务队列驱动的服务。
//
// 它解决的是 pkg/converter 留给调用方的三件事：输入从哪来、结果写到哪里、
// 以及"能不能安全地把调用方的输入变成转换库的输入"。
//
// 关键取舍是**不采信调用方给的文件名扩展名**。转换库按扩展名区分 docx/xlsx/pptx/
// html——这些格式的魔数都是 ZIP 或纯文本，靠嗅探分不开。如果直接拿调用方的
// "report.docx" 去转换，一个 .docx 内容配 .html 名字就会被送进 HTML 导入器，
// 既可能报错，也可能解析出完全错误的东西。因此这里先由服务端解析出可信的
// 输入格式，再用该格式的规范扩展名给临时文件命名，转换库的输入路径由我们生成。
package convertersvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zc310/ofd/internal/allowlist"
	"github.com/zc310/ofd/internal/transfer"
	"github.com/zc310/ofd/pkg/converter"
	// 服务需要支持全部已注册格式，导入器靠 init() 注册，必须显式链接：
	// 否则 ImporterByName 查不到 docx/pdf/html，请求会以"无法识别输入格式"被拒。
	_ "github.com/zc310/ofd/pkg/converter/import/html"     // HTML/MHTML→OFD
	_ "github.com/zc310/ofd/pkg/converter/import/image"    // PNG/JPEG/TIFF→OFD
	_ "github.com/zc310/ofd/pkg/converter/import/markdown" // Markdown→OFD
	_ "github.com/zc310/ofd/pkg/converter/import/office"   // Office→OFD 与 →PDF
	_ "github.com/zc310/ofd/pkg/converter/import/pdf"      // PDF→OFD
)

// Lane 是转换任务所属的执行通道，用于在任务队列里分配不同的并发额度。
type Lane string

const (
	// LaneFast 是纯 Go 的快速路径：OFD 解析与 PDF/文本/Markdown/图像输出。
	LaneFast Lane = "fast"
	// LaneHeavy 需要拉起外部进程（LibreOffice、Chrome），单次耗时与内存都高得多，
	// 必须与 LaneFast 分开限流，否则一批 Office 文档就能把服务打满。
	LaneHeavy Lane = "heavy"
)

// 输入来源种类。
const (
	InputUpload = "upload"
	InputURL    = "url"
)

// 输出目标种类。
const (
	OutputStream = "stream"
	OutputDir    = "dir"
	// OutputFTP 直接上传到 FTP/FTPS 服务器。结果不落本地磁盘。
	OutputFTP = "ftp"
	// OutputS3 直接写入 S3 兼容的对象存储。
	OutputS3 = "s3"
	// OutputWebDAV 直接写入 WebDAV 共享。
	OutputWebDAV = "webdav"
	// OutputSFTP 直接写入 SFTP 服务器。
	OutputSFTP = "sftp"
)

// DefaultStreamLimit 是 stream 输出的内存上限。
const DefaultStreamLimit int64 = 64 << 20

// Input 描述待转换文档的来源。
type Input struct {
	// Kind 为 InputUpload 或 InputURL。
	Kind string `json:"kind"`
	// Format 可选的显式输入格式。为空时按文件名扩展名与魔数识别。
	Format string `json:"format,omitempty"`
	// FileName 是上传的原始文件名，仅用于识别格式，不直接用于转换。
	FileName string `json:"file_name,omitempty"`
	// Bytes 是 Kind 为 InputUpload 时的内容。
	Bytes []byte `json:"bytes,omitempty"`
	// URL 是 Kind 为 InputURL 时的地址，必须通过服务端白名单。
	URL string `json:"url,omitempty"`
}

// Output 描述转换结果的落点。
type Output struct {
	// Kind 为 OutputStream、OutputDir，或某个远端目标类型。
	Kind string `json:"kind"`
	// Format 输出格式，如 "pdf"、"markdown"、"png"。
	Format string `json:"format"`
	// FileName 是产物文件名的基名，不含扩展名——扩展名由 Format 决定。
	//
	// 为什么给基名而不是完整文件名：扩展名本来就是格式决定的，让调用方也能
	// 指定就多了一处可能自相矛盾的地方（声明 pdf 却起名 .txt）。基名已经能
	// 表达调用方的意图（发票号、客户编号），又不与之冲突。
	//
	// 为空时用默认值：单文件是 "output"，逐页输出是 "page"。
	//
	// 必须是单个路径组件，不含分隔符——否则就等于给了调用方一条穿越到
	// output_dir 之外的路径。完整路径仍由服务端决定（output_dir 之下、
	// 每个任务一个子目录），这一层隔离不受影响。
	FileName string `json:"file_name"`
	// Dir 是 Kind 为 OutputDir 时 output_dir 之下的相对子路径。
	Dir string `json:"dir"`
	// MaxStreamBytes 限制 stream 输出的内存占用，0 时取 DefaultStreamLimit。
	MaxStreamBytes int64 `json:"max_stream_bytes,omitempty"`
	// Remote 指向一个服务端预注册的远端目标，Kind 为远端类型时必填。
	Remote RemoteOutput `json:"remote,omitempty"`
}

// RemoteOutput 描述远端落点。
//
// 四个协议共用一组字段，而不是每种协议一组（ftp_target/ftp_dir、
// s3_target/s3_prefix、…）：kind 已经表明用哪种协议，再按协议分字段是冗余的，
// 而且调用方无法从字段名判断该填哪个、填完产物落在哪。
type RemoteOutput struct {
	// Target 是服务端配置里注册的目标名。调用方不能直接给地址与凭据——那等于
	// 让它往任意主机上传、往任意账号写数据。
	Target string `json:"target"`
	// Path 是该目标下的子路径，语义由目标自身的配置决定：FTP/SFTP/WebDAV 是
	// 远端 base_dir，S3 是桶内 prefix。调用方不必、也无法区分。
	Path string `json:"path,omitempty"`
}

// Spec 是一次转换的完整描述。
type Spec struct {
	Input  Input
	Output Output
	// Options 传给 pkg/converter 的可选项（DPI、纸张、外部程序路径等）。
	Options []converter.Option
}

// File 是目录输出里的一个文件。
type File struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// Result 是一次转换的结果。
type Result struct {
	// Kind 与 Spec.Output.Kind 一致。
	Kind string `json:"kind"`
	// InputFormat 实际判定的输入格式（已归一化），不是 Spec.Input.Format
	// 那个声明值。统计按类型分布必须用这个：用声明值会把"内容是 OFD、
	// 文件名是 .html"这类任务记成 ofd→pdf。
	InputFormat string `json:"input_format"`
	// InputBytes 实际读入的输入字节数，取落盘后文件的尺寸。
	// 输入一律先 materialize 成文件，所以这个值对上传、URL 与未来的
	// 其他来源口径一致。
	InputBytes int64 `json:"input_bytes"`
	// Format 实际使用的输出格式名（已归一化）。
	Format string `json:"format"`
	// MIME 输出的 MIME 类型。
	MIME string `json:"mime"`
	// FileName 输出文件名，stream 时有值。
	FileName string `json:"file_name,omitempty"`
	// Bytes stream 输出的内容。
	Bytes []byte `json:"-"`
	// Dir 目录输出的根路径。
	Dir string `json:"dir,omitempty"`
	// Files 目录输出里的文件清单。
	Files []File `json:"files,omitempty"`
	// TookMs 转换耗时（毫秒）。不用 time.Duration 直接序列化——那是纳秒。
	TookMs int64 `json:"took_ms"`
}

// 面向调用方的错误。
var (
	// ErrBadRequest 表示请求本身不合法，重试无意义。
	ErrBadRequest = errors.New("请求参数不合法")
	// ErrUnsupported 表示格式组合不支持，同样不该重试。
	ErrUnsupported = errors.New("不支持的格式组合")
	// ErrTooLarge 表示输入或输出超出限制。
	ErrTooLarge = errors.New("超出大小限制")
)

// Service 执行转换。
type Service struct {
	// Allowlist 限定 input.kind=url 可以拉取的主机。为 nil 时 URL 输入一律拒绝。
	Allowlist *allowlist.List
	// TempDir 是转换期间的临时目录。转换器需要真实文件路径（LibreOffice、
	// Chrome 都如此），所以输入总是落盘一次。
	TempDir string
	// URLTimeout 是拉取远程输入的整体超时。
	URLTimeout time.Duration
	// DefaultOptions 是每个任务都会附加的可选项。
	DefaultOptions []converter.Option
	// FTPTargets 是服务端预注册的 FTP 目标，按名字索引。
	FTPTargets map[string]*transfer.FTPSink
	// S3Targets 是预注册的对象存储目标。
	S3Targets map[string]*transfer.MinioSink
	// WebDAVTargets 是预注册的 WebDAV 目标。
	WebDAVTargets map[string]*transfer.WebDAVSink
	// SFTPTargets 是预注册的 SFTP 目标。
	SFTPTargets map[string]*transfer.SFTPSink
}

// New 构造服务。
func New(tempDir string, list *allowlist.List) *Service {
	return &Service{
		Allowlist:  list,
		TempDir:    tempDir,
		URLTimeout: 30 * time.Second,
	}
}

// Run 执行一次转换。
func (s *Service) Run(ctx context.Context, spec Spec) (Result, error) {
	started := time.Now()
	inputFormat, inExt, err := resolveInputFormat(spec.Input)
	if err != nil {
		return Result{}, err
	}
	outFormat, ok := converter.FormatByName(spec.Output.Format)
	if !ok {
		return Result{}, fmt.Errorf("%w: 未知输出格式 %s", ErrUnsupported, spec.Output.Format)
	}
	outputKind, err := NormalizeOutputKind(spec.Output.Kind)
	if err != nil {
		return Result{}, err
	}
	// 逐页图像格式会产生多个文件，塞不进单文件流里。与其在这里悄悄只给第一页，
	// 不如直接要求调用方改用 dir。
	if outputKind == OutputStream && outFormat.Kind == converter.KindImage {
		return Result{}, fmt.Errorf("%w: 图像格式 %s 逐页输出，请使用 %s", ErrBadRequest, outFormat.Name, OutputDir)
	}

	source, err := s.source(ctx, spec.Input)
	if err != nil {
		return Result{}, err
	}
	defer source.Close()

	// 输入一律落盘：转换库要路径，而且落盘后扩展名由我们决定。
	//
	// TempDir 先确保存在：容器里的挂载卷、K8s 的 emptyDir、刚部署的实例，
	// 目录可能压根还没建。少了这一步，每个任务都会以一句
	// "no such file or directory" 失败，而原因和转换毫无关系。
	if s.TempDir != "" {
		if err := os.MkdirAll(s.TempDir, 0o750); err != nil {
			return Result{}, err
		}
	}
	workDir, err := os.MkdirTemp(s.TempDir, "ofd-convert-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(workDir)
	inputPath := filepath.Join(workDir, "input"+inExt)
	if err := s.materialize(ctx, source, inputPath); err != nil {
		return Result{}, err
	}
	// 输入尺寸取落盘后的文件而不是源的大小：URL 输入到这一步才真正拿到
	// 字节数，而对上传输入它还能覆盖"落盘被截断"这类情况。
	inputSize, err := fileSize(inputPath)
	if err != nil {
		return Result{}, err
	}

	// 三种输出共用一套 writer 管线：区别只在"往哪儿写"。
	sink, err := s.sinkFor(outputKind, spec)
	if err != nil {
		return Result{}, err
	}
	if closer, ok := sink.(io.Closer); ok {
		// FTP 目标持有连接，用完必须断开。放在这里统一收尾，三种输出一视同仁。
		defer func() { _ = closer.Close() }()
	}

	extension := primaryExtension(outFormat.Extensions)
	// 文件名先校验：等到产出阶段才发现非法，任务已经占了队列和一次转换。
	naming, err := newOutputNaming(spec.Output.FileName, extension)
	if err != nil {
		return Result{}, err
	}
	opts := append(s.options(spec), converter.WithFormat(outFormat.Name))
	written := map[string]int64{}
	// locations 记录每个文件实际写到了哪里。远端目标的完整路径由 Sink 拼出
	// （含配置的 BasePrefix/BaseDir/base_dir），不能拿请求里的 prefix 顶替——
	// 那样报出来的位置和文件真正所在的地方对不上。
	locations := map[string]transfer.Location{}
	var writers []*sinkWriter
	var main io.Writer
	if outFormat.Kind == converter.KindImage {
		// 逐页格式：每页一个文件，靠 Writer 回调拿到输出位置。
		opts = append(opts, converter.Writer(func(page int) (io.WriteCloser, error) {
			// 转换器给的页号已经是从 1 开始的（pages.go 里 len(pages)+1），
			// 这里不能再 +1——那会让第一页被命名成 page-0002.png。
			name := naming.page(page)
			writer := newSinkWriter(sink, name, written, locations)
			writers = append(writers, writer)
			return writer, nil
		}))
	} else {
		// 文档格式：整体一个文件。必须给非 nil 的 writer，PDF 编码器会直接拒绝
		// nil 输出。
		writer := newSinkWriter(sink, naming.single(), written, locations)
		writers = append(writers, writer)
		main = writer
	}
	if err := converter.Convert(ctx, inputFormat, outFormat.Name, inputPath, main, opts...); err != nil {
		return Result{}, wrapConvertError(err)
	}
	// 转换器正常会 Close 每个 writer；这里兜底再 Flush 一次（幂等），
	// 免得某个编码器忘了 Close 就留下空文件。
	for _, writer := range writers {
		if err := writer.Flush(); err != nil {
			return Result{}, wrapConvertError(err)
		}
	}
	if len(written) == 0 {
		return Result{}, fmt.Errorf("%w: 转换没有产出任何文件", ErrUnsupported)
	}

	result := Result{Kind: outputKind, Format: outFormat.Name, MIME: outFormat.MIME,
		InputFormat: inputFormat, InputBytes: inputSize}
	switch outputKind {
	case OutputStream:
		buffer, ok := sink.(*transfer.BufferSink)
		if !ok {
			return Result{}, fmt.Errorf("%w: stream 输出应使用内存目标", ErrUnsupported)
		}
		result.FileName = naming.single()
		result.Bytes = buffer.Bytes()
	default:
		for _, name := range sortedNames(written) {
			result.Files = append(result.Files, File{Name: name, Size: written[name]})
		}
		result.FileName = result.Files[0].Name
		result.Dir = writtenDir(outputKind, spec, locations)
	}
	result.TookMs = time.Since(started).Milliseconds()
	return result, nil
}

// Lane 报告该任务应走哪条通道。
//
// 判据是"是否会拉起外部进程"：Office 文档走 LibreOffice，HTML 输出走 Chrome，
// 以及两者之间的直接转换器。它们比纯 Go 路径慢一个数量级以上。
func (s *Service) Lane(spec Spec) Lane {
	if IsHeavy(spec.Input, spec.Output.Format) {
		return LaneHeavy
	}
	return LaneFast
}

// IsHeavy 报告该输入输出组合是否属于重通道。
//
// 判据有两类：一是会不会拉起外部进程（Office 走 LibreOffice、HTML 走 Chrome），
// 二是输入是不是要从网络拉——URL 输入的耗时由对端决定，不该占住快速通道。
// 这两类都慢在"不可预期"上，而 fast 通道的用途正是保证可预期的请求不被拖住。
//
// 单独导出成包级函数，是为了让 HTTP 层在入队时就能定下通道：通道写在任务
// 记录里供队列分派，事后改会让已入队的任务和实际执行的资源池对不上。
func IsHeavy(in Input, outputFormat string) bool {
	if isHeavyFormat(outputFormat) {
		return true
	}
	if in.Kind == InputURL {
		return true
	}
	if in.Format != "" {
		return isHeavyFormat(in.Format)
	}
	return isHeavyFormat(filepath.Ext(in.FileName))
}

// heavyFormats 是走 LibreOffice 或 Chrome 的格式。
var heavyFormats = map[string]bool{
	"doc": true, "docx": true, "xls": true, "xlsx": true,
	"ppt": true, "pptx": true, "odt": true, "ods": true, "odp": true, "rtf": true,
	"html": true, "htm": true,
}

func isHeavyFormat(name string) bool {
	key := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "."))
	return heavyFormats[key]
}

func (s *Service) options(spec Spec) []converter.Option {
	opts := make([]converter.Option, 0, len(s.DefaultOptions)+len(spec.Options)+1)
	opts = append(opts, s.DefaultOptions...)
	opts = append(opts, spec.Options...)
	if s.TempDir != "" {
		opts = append(opts, converter.WithTempDir(s.TempDir))
	}
	return opts
}

// source 按输入种类构造取源方式。
func (s *Service) source(ctx context.Context, in Input) (transfer.Source, error) {
	switch in.Kind {
	case InputUpload, "":
		if len(in.Bytes) == 0 {
			return nil, fmt.Errorf("%w: 上传内容为空", ErrBadRequest)
		}
		return transfer.NewBytesSource(in.Bytes, filepath.Base(in.FileName)), nil
	case InputURL:
		if s.Allowlist == nil {
			return nil, fmt.Errorf("%w: 服务未启用 URL 输入", ErrBadRequest)
		}
		timeout := s.URLTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		return &transfer.HTTPSource{
			URL:       in.URL,
			Allowlist: s.Allowlist,
			Timeout:   timeout,
		}, nil
	default:
		return nil, fmt.Errorf("%w: 未知输入种类 %q", ErrBadRequest, in.Kind)
	}
}

// materialize 把取源内容写到 path。
func (s *Service) materialize(ctx context.Context, source transfer.Source, path string) error {
	reader, err := source.Open(ctx)
	if err != nil {
		return err
	}
	defer reader.Close()
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, reader); err != nil {
		file.Close()
		os.Remove(path)
		return err
	}
	return file.Close()
}

// fileSize 返回文件字节数。
func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// resolveInputFormat 确定输入格式，并返回该格式的规范扩展名。
//
// 优先级：显式声明 > 文件名扩展名 > 魔数。三者都拿不到时只能报错——宁可让调用方
// 明确指定，也不能猜错格式后把内容送进错误的导入器。
func resolveInputFormat(in Input) (string, string, error) {
	if in.Format != "" {
		if ext, ok := canonicalExtension(in.Format); ok {
			return strings.ToLower(in.Format), ext, nil
		}
		return "", "", fmt.Errorf("%w: 未知输入格式 %q", ErrUnsupported, in.Format)
	}
	if ext := filepath.Ext(in.FileName); ext != "" {
		if name, canon, ok := formatForExtension(ext); ok {
			return name, canon, nil
		}
	}
	// 魔数只能分出 OFD 与 PDF，docx/xlsx/pptx 同为 ZIP 容器分不开。
	if name, ok := sniffMagic(in.Bytes); ok {
		if ext, ok := canonicalExtension(name); ok {
			return name, ext, nil
		}
	}
	if in.Kind == InputURL {
		return "", "", fmt.Errorf("%w: 无法识别 URL 输入的格式，请在请求中声明 input.format", ErrBadRequest)
	}
	return "", "", fmt.Errorf("%w: 无法识别输入格式，请在请求中声明 input.format", ErrBadRequest)
}

// formatForExtension 由扩展名反查输入格式与规范扩展名。
//
// 只查 Importer 注册表，不查 Format 注册表。Format 记的是"OFD→X"的输出编码器，
// 拿它来认输入会把 .pdf/.png 这类格式误判成"输入是 ofd、扩展名是 .pdf"，
// 然后把一个 PDF 文件送进 OFD 解析器。
func formatForExtension(ext string) (string, string, bool) {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return "", "", false
	}
	// ofd 不是导入器，但它是主输入，必须认。
	if ext == ".ofd" {
		return "ofd", ".ofd", true
	}
	if imp, ok := converter.ImporterByName(ext); ok {
		return normalize(imp.Name()), primaryExtension(imp.Extensions()), true
	}
	return "", "", false
}

// canonicalExtension 取某格式的规范扩展名。
func canonicalExtension(name string) (string, bool) {
	name = normalize(name)
	if name == "ofd" {
		return ".ofd", true
	}
	if imp, ok := converter.ImporterByName(name); ok {
		return primaryExtension(imp.Extensions()), true
	}
	if f, ok := converter.FormatByName(name); ok {
		if f.Kind == converter.KindImage {
			return "", false
		}
		return primaryExtension(f.Extensions), true
	}
	return "", false
}

// sniffMagic 只能识别 PDF 与 OFD。
//
// docx/xlsx/pptx 同为 ZIP 容器，xlsx 与 docx 的成员名不同但要解包才能确认，
// 这里的取舍是分不出来就要求调用方显式声明 input.format，而不是猜。
func sniffMagic(data []byte) (string, bool) {
	if len(data) >= 5 && string(data[:5]) == "%PDF-" {
		return "pdf", true
	}
	// OFD 也是 ZIP，特征是包内有 OFD.xml。
	if len(data) >= 4 && string(data[:4]) == "PK\x03\x04" && bytes.Contains(data, []byte("OFD.xml")) {
		return "ofd", true
	}
	return "", false
}

// sinkWriter 把转换器的 io.Writer 输出适配成 transfer.Sink 的一次 Put。
//
// 转换器要的是一个可写的流，而 Sink.Put 要的是一个完整的 reader，两者对不上，
// 只能先在内存里攒完再交出去。代价是单个输出文件要在内存里待到结束：stream
// 输出受 MaxStreamBytes 约束，dir 输出受页缓存上限约束，都不会无限增长。
type sinkWriter struct {
	sink transfer.Sink
	name string
	size map[string]int64
	// locations 非 nil 时记录 Sink 返回的位置，供 Result.Dir 使用。
	locations map[string]transfer.Location
	buf       bytes.Buffer
	flushed   bool
}

func newSinkWriter(sink transfer.Sink, name string, size map[string]int64, locations map[string]transfer.Location) *sinkWriter {
	return &sinkWriter{sink: sink, name: name, size: size, locations: locations}
}

func (w *sinkWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

// Close 交给调用方把内容落进 Sink。转换器一般不调 Close，所以 dir 场景由
// Run 在 Convert 返回后统一 Flush。
func (w *sinkWriter) Close() error { return w.Flush() }

func (w *sinkWriter) Flush() error {
	// 幂等：转换器会 Close 一次，Run 收尾时再兜底调一次。
	if w.flushed {
		return nil
	}
	data := w.buf.Bytes()
	location, err := w.sink.Put(context.Background(), w.name, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if w.size != nil {
		w.size[w.name] = int64(len(data))
	}
	if w.locations != nil {
		w.locations[w.name] = location
	}
	w.buf.Reset()
	w.flushed = true
	return nil
}

// wrapConvertError 把超限错误翻译成明确类型，便于调用方判断该不该重试。
//
// 上限数字不在这里编造：大小限制由各 Sink 自己执行，消息里带着真实数字
// （"内容超过上限 67108864 字节"）。早先这里接收一个 limit 参数，在输出
// 路径统一之后传进来的都是 0，于是超限错误会显示成"输出超过 0 字节"。
func wrapConvertError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	msg := err.Error()
	if strings.Contains(msg, "too large") || strings.Contains(msg, "超过上限") {
		return fmt.Errorf("%w: %v", ErrTooLarge, err)
	}
	return err
}

// NormalizeOutputKind 把 output.kind 规范化为内部使用的标准值：空值按 stream
// 处理，忽略大小写与首尾空白，未知值报 ErrBadRequest。
//
// 导出是因为 HTTP 层必须在决定同步还是异步之前先做同一套判定。空值意味着
// stream——若 HTTP 层用字面比较判断 `kind == "stream"`，省略字段的请求会被
// 送进异步队列，随后按 stream 处理、产物只留在内存里随 Result 丢弃：任务报
// succeeded 而调用方拿不到任何东西。规范化放在这里而不是各写一份，是为了让
// 提交阶段与 worker 阶段永远得出同一个结论。
//
// 幂等：规范化后的值再规范化仍得到自身。
func NormalizeOutputKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", OutputStream:
		return OutputStream, nil
	case OutputDir:
		return OutputDir, nil
	case OutputFTP:
		return OutputFTP, nil
	case OutputS3:
		return OutputS3, nil
	case OutputWebDAV:
		return OutputWebDAV, nil
	case OutputSFTP:
		return OutputSFTP, nil
	default:
		return "", fmt.Errorf("%w: 未知输出种类 %q", ErrBadRequest, kind)
	}
}

func primaryExtension(exts []string) string {
	if len(exts) == 0 {
		return ""
	}
	ext := exts[0]
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func normalize(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func sortedNames(m map[string]int64) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// sinkFor 按输出种类构造写入目标。
func (s *Service) sinkFor(kind string, spec Spec) (transfer.Sink, error) {
	switch kind {
	case OutputStream:
		limit := spec.Output.MaxStreamBytes
		if limit <= 0 {
			limit = DefaultStreamLimit
		}
		return transfer.NewBufferSink(limit), nil
	case OutputDir:
		if spec.Output.Dir == "" {
			return nil, fmt.Errorf("%w: dir 输出必须指定目录", ErrBadRequest)
		}
		if err := os.MkdirAll(spec.Output.Dir, 0o750); err != nil {
			return nil, err
		}
		return transfer.NewDirSink(spec.Output.Dir), nil
	case OutputFTP:
		return s.ftpSink(spec)
	case OutputS3:
		return s.s3Sink(spec)
	case OutputWebDAV:
		return s.webdavSink(spec)
	case OutputSFTP:
		return s.sftpSink(spec)
	default:
		return nil, fmt.Errorf("%w: 未知输出种类 %q", ErrBadRequest, kind)
	}
}

// ftpSink 取出已注册的 FTP 目标。
//
// 目标必须已注册：地址与凭据只存在于服务端配置里，调用方能给的只是一个名字。
// 与通知目标同一套逻辑——如果让请求带地址，它就能被当作往任意主机上传数据的
// 通道，SSRF 防护也随之全废。
func (s *Service) ftpSink(spec Spec) (transfer.Sink, error) {
	registered, err := s.lookupFTPTarget(spec)
	if err != nil {
		return nil, err
	}
	// 每个任务派生一份：连接与"已创建目录"状态不能跨任务共享。
	// WithBaseDir 会拒绝含 ".." 的子目录，不静默把输出放到配置范围之外。
	derived, err := registered.WithBaseDir(spec.Output.Remote.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	return derived, nil
}

// s3Sink 取出已注册的对象存储目标。
func (s *Service) s3Sink(spec Spec) (transfer.Sink, error) {
	registered, err := s.lookupS3Target(spec)
	if err != nil {
		return nil, err
	}
	// S3 用 prefix 而不是目录名，其余协议用 base_dir——差异由 Sink 自己承担，
	// 调用方只给一个 path。
	derived, err := registered.WithBasePrefix(spec.Output.Remote.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	return derived, nil
}

// webdavSink 取出已注册的 WebDAV 目标。
func (s *Service) webdavSink(spec Spec) (transfer.Sink, error) {
	registered, err := s.lookupWebDAVTarget(spec)
	if err != nil {
		return nil, err
	}
	derived, err := registered.WithBaseDir(spec.Output.Remote.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	return derived, nil
}

// 四个 lookup 薄助手：把"必须已注册"这条规则和错误文案统一起来。
//
// 目标必须已注册：地址与凭据只存在于服务端配置里，调用方能给的只是一个名字。
// 与通知目标同一套逻辑——如果让请求带地址，它就能被当作往任意主机上传数据的
// 通道，SSRF 防护也随之全废。

func (s *Service) lookupFTPTarget(spec Spec) (*transfer.FTPSink, error) {
	name := spec.Output.Remote.Target
	if name == "" {
		return nil, fmt.Errorf("%w: ftp 输出必须指定已注册的 output.remote.target", ErrBadRequest)
	}
	sink, ok := s.FTPTargets[name]
	if !ok || sink == nil {
		return nil, fmt.Errorf("%w: 未注册的 FTP 目标 %q", ErrBadRequest, name)
	}
	return sink, nil
}

func (s *Service) lookupS3Target(spec Spec) (*transfer.MinioSink, error) {
	name := spec.Output.Remote.Target
	if name == "" {
		return nil, fmt.Errorf("%w: s3 输出必须指定已注册的 output.remote.target", ErrBadRequest)
	}
	sink, ok := s.S3Targets[name]
	if !ok || sink == nil {
		return nil, fmt.Errorf("%w: 未注册的 S3 目标 %q", ErrBadRequest, name)
	}
	return sink, nil
}

func (s *Service) lookupWebDAVTarget(spec Spec) (*transfer.WebDAVSink, error) {
	name := spec.Output.Remote.Target
	if name == "" {
		return nil, fmt.Errorf("%w: webdav 输出必须指定已注册的 output.remote.target", ErrBadRequest)
	}
	sink, ok := s.WebDAVTargets[name]
	if !ok || sink == nil {
		return nil, fmt.Errorf("%w: 未注册的 WebDAV 目标 %q", ErrBadRequest, name)
	}
	return sink, nil
}

func (s *Service) lookupSFTPTarget(spec Spec) (*transfer.SFTPSink, error) {
	name := spec.Output.Remote.Target
	if name == "" {
		return nil, fmt.Errorf("%w: sftp 输出必须指定已注册的 output.remote.target", ErrBadRequest)
	}
	sink, ok := s.SFTPTargets[name]
	if !ok || sink == nil {
		return nil, fmt.Errorf("%w: 未注册的 SFTP 目标 %q", ErrBadRequest, name)
	}
	return sink, nil
}

// writtenDir 报告结果实际落在哪个目录。
//
// 远端目标必须用 Sink 返回的路径：配置的 base_dir/base_dir 前缀是服务端加的，
// 请求里的 ftp_dir/s3_prefix 只是它下面的一层。拿请求值顶替的话，任务状态里
// 报的路径会少一段，调用方按它去取文件就会找不到。
func writtenDir(kind string, spec Spec, locations map[string]transfer.Location) string {
	switch kind {
	case OutputDir:
		return spec.Output.Dir
	case OutputFTP, OutputS3, OutputWebDAV:
		// 任取一个已写入文件的位置，取其目录部分即可：同一任务的所有文件
		// 都在同一个目录下。
		for _, location := range locations {
			dir := path.Dir(location.Path)
			if dir == "." {
				return ""
			}
			return dir
		}
		return ""
	default:
		return ""
	}
}

// sftpSink 取出已注册的 SFTP 目标。
func (s *Service) sftpSink(spec Spec) (transfer.Sink, error) {
	registered, err := s.lookupSFTPTarget(spec)
	if err != nil {
		return nil, err
	}
	derived, err := registered.WithBaseDir(spec.Output.Remote.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadRequest, err)
	}
	return derived, nil
}
