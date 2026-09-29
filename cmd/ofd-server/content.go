package main

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/valyala/fasthttp"

	"github.com/zc310/ofd/internal/convertersvc"
	"github.com/zc310/ofd/internal/jobstore"
	"github.com/zc310/ofd/pkg/converter"
)

// maxZipEntryBytes 限制单个文件进入 zip 的大小。
//
// 逐页 PNG 的产物目录可能有几百个文件、几百 MB。整个打进内存会随并发线性
// 放大；流式写 zip 就不会，但一个失控的大文件仍可能把磁盘写满，所以单条
// 仍设上限。
const maxZipEntryBytes = 1 << 30

// errNoLocalContent 表示该任务的产物不在本服务能取回的地方。
var errNoLocalContent = errors.New("产物不通过本端点提供")

var errUnsafeArtifact = errors.New("产物不是安全的普通文件")

// handleContent 取回任务产物。
//
// 两种用法：
//
//	GET /v1/jobs/{id}/content            恰好一个产物时直接返回该文件；
//	                                   多个产物时打包成 zip
//	GET /v1/jobs/{id}/content?name=xxx   取回指定的某个产物
//
// 只服务本地 dir 输出的产物。stream 输出的内容在响应里当场给完了、不落盘，
// 没有可取的东西；远端目标的产物在 FTP/S3/WebDAV/SFTP 上，由那边的存储
// 负责——本服务不该替别人回源。
func (s *Server) handleContent(ctx *fasthttp.RequestCtx) {
	id, ok := pathParam(ctx, "id")
	if !ok {
		writeError(ctx, fasthttp.StatusNotFound, "not_found", "任务 ID 不合法")
		return
	}
	job, err := s.store.Get(id)
	if err != nil {
		s.log.Error("查询任务失败", "job", id, "err", err)
		writeError(ctx, fasthttp.StatusInternalServerError, "internal_error", "查询失败")
		return
	}
	if job == nil {
		writeError(ctx, fasthttp.StatusNotFound, "job_not_found", "任务不存在")
		return
	}
	if !job.State.Terminal() {
		writeError(ctx, fasthttp.StatusConflict, "job_not_finished",
			fmt.Sprintf("任务处于 %s，还没到终态", job.State))
		return
	}
	if job.State != jobstore.StateSucceeded {
		writeError(ctx, fasthttp.StatusConflict, "job_not_succeeded",
			fmt.Sprintf("任务终态为 %s，没有可取回的产物", job.State))
		return
	}
	if err := checkServableOutput(job.Output); err != nil {
		writeError(ctx, fasthttp.StatusConflict, "not_downloadable", err.Error())
		return
	}

	// 目录本身要兜住：产物路径写进任务记录时是服务端拼的，但这里读的是
	// 持久化数据，配置也可能在这期间被改过。两条检查都做——纵深防御在
	// 一个"按路径读文件"的端点上不是多余的。
	dir, err := s.resolveOutputDir(job)
	if err != nil {
		s.log.Error("产物目录不可用", "job", id, "err", err)
		writeError(ctx, fasthttp.StatusConflict, "not_downloadable", err.Error())
		return
	}

	requested := string(ctx.QueryArgs().Peek("name"))
	if requested != "" {
		s.serveOneFile(ctx, dir, job, requested)
		return
	}
	if len(job.Output.Files) == 1 {
		s.serveOneFile(ctx, dir, job, job.Output.Files[0])
		return
	}
	s.serveArchive(ctx, dir, job)
}

// checkServableOutput 判断该任务的产物能否由本端点提供。
func checkServableOutput(out jobstore.Output) error {
	switch out.Kind {
	case convertersvc.OutputDir:
		return nil
	case convertersvc.OutputStream:
		// stream 的内容随响应当场返回，没有落盘，也就无从取回。
		return fmt.Errorf("%w：stream 输出的内容在提交响应里已给出，"+
			"需要事后取回请改用 output.kind 为 dir", errNoLocalContent)
	case convertersvc.OutputFTP, convertersvc.OutputS3,
		convertersvc.OutputWebDAV, convertersvc.OutputSFTP:
		return fmt.Errorf("%w：产物在远端目标上，请从 %s 位置取（目标 %s）",
			errNoLocalContent, out.Kind, out.Bucket+out.Key)
	default:
		return fmt.Errorf("%w：未知的输出类型 %q", errNoLocalContent, out.Kind)
	}
}

// resolveOutputDir 校验并返回产物目录。
//
// 先用词法路径检查任务目录必须落在配置的 output_dir 之下，再用 os.Root
// 固定目录句柄读取产物。这样既能防止配置修改导致历史任务越界，也能防止
// 输出目录或产物文件中的符号链接把读取重定向到 output_dir 外。
func (s *Server) resolveOutputDir(job *jobstore.Job) (*os.Root, error) {
	dir := job.Output.Path
	if dir == "" {
		return nil, fmt.Errorf("%w：任务记录里没有产物路径", errNoLocalContent)
	}
	cleanRoot, err := filepath.Abs(filepath.Clean(s.cfg.OutputDir))
	if err != nil {
		return nil, fmt.Errorf("%w：无法解析配置的 output_dir: %v", errNoLocalContent, err)
	}
	cleanDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return nil, fmt.Errorf("%w：无法解析产物目录: %v", errNoLocalContent, err)
	}
	rel, err := filepath.Rel(cleanRoot, cleanDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// 落点不在 output_dir 之下，直接拒绝而不是尝试读取。
		return nil, fmt.Errorf("%w：产物路径 %q 不在配置的 output_dir 之下", errNoLocalContent, dir)
	}
	root, err := os.OpenRoot(cleanRoot)
	if err != nil {
		return nil, fmt.Errorf("无法打开配置的 output_dir: %w", err)
	}
	if err := rejectSymlinkPath(root, rel); err != nil {
		_ = root.Close()
		return nil, err
	}
	dirRoot, err := root.OpenRoot(rel)
	_ = root.Close()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("产物目录已不存在（可能已被外部清理）：%s", cleanDir)
		}
		return nil, fmt.Errorf("产物目录不可用：%w", err)
	}
	return dirRoot, nil
}

// rejectSymlinkPath 在打开目录前拒绝已有的符号链接路径段。后续的 os.Root
// 操作也会把并发路径变化限制在输出目录内，因此此检查用于明确拒绝符号链接，
// 真正的路径安全边界由根目录句柄保证。
func rejectSymlinkPath(root *os.Root, path string) error {
	path = filepath.Clean(path)
	if path == "." {
		return nil
	}
	current := ""
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if current == "" {
			current = component
		} else {
			current = filepath.Join(current, component)
		}
		info, err := root.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("产物目录不可用：%w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w：产物目录不能包含符号链接", errNoLocalContent)
		}
	}
	return nil
}

// serveOneFile 返回单个产物文件。
//
// name 必须在任务记录的 output.files 里。**不能把它直接拼到目录上**：
// output.dir 允许指向共享目录（显式指定时不追加任务 ID），同一个目录里可能
// 躺着别的任务的产物。按记录清单做精确匹配既挡了 ../ 穿越，也挡了跨任务
// 读取——清单是转换结束时由服务端写下的，调用方改不动。
func (s *Server) serveOneFile(ctx *fasthttp.RequestCtx, dir *os.Root, job *jobstore.Job, name string) {
	defer dir.Close()
	target, ok := recordedFile(job.Output.Files, name)
	if !ok {
		writeError(ctx, fasthttp.StatusNotFound, "file_not_found",
			fmt.Sprintf("任务 %s 没有名为 %q 的产物", job.ID, name))
		return
	}
	file, err := openRecordedFile(dir, target)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, errUnsafeArtifact) {
			writeError(ctx, fasthttp.StatusNotFound, "file_not_found",
				fmt.Sprintf("产物文件已不存在：%s", target))
			return
		}
		s.log.Error("打开产物失败", "job", job.ID, "file", target, "err", err)
		writeError(ctx, fasthttp.StatusInternalServerError, "internal_error", "读取产物失败")
		return
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		writeError(ctx, fasthttp.StatusInternalServerError, "internal_error", "读取产物失败")
		return
	}
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetContentType(contentTypeFor(target))
	ctx.Response.Header.Set("Content-Disposition",
		`attachment; filename="`+headerSafe(target)+`"`)
	// 产物不该被中间层缓存住：任务同名重跑后内容会变，而 URL 不变。
	ctx.Response.Header.Set("Cache-Control", "no-store")
	ctx.SetBodyStream(file, int(info.Size()))
}

// serveArchive 把多个产物打成 zip 流式返回。
//
// 用流式而不是先读进内存：逐页 PNG 的产物动辄几百 MB，buffer 起来会随并发
// 线性放大，而这里没有任何理由把它整份驻留。
func (s *Server) serveArchive(ctx *fasthttp.RequestCtx, dir *os.Root, job *jobstore.Job) {
	files := recordedFiles(job.Output.Files)
	if len(files) == 0 {
		_ = dir.Close()
		writeError(ctx, fasthttp.StatusNotFound, "file_not_found", "任务没有记录任何产物文件")
		return
	}
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetContentType("application/zip")
	ctx.Response.Header.Set("Content-Disposition",
		`attachment; filename="`+headerSafe(job.ID)+`.zip"`)
	ctx.Response.Header.Set("Cache-Control", "no-store")
	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		defer dir.Close()
		zw := zip.NewWriter(w)
		defer func() { _ = zw.Close() }()
		for _, name := range files {
			src, err := openRecordedFile(dir, name)
			if err != nil {
				continue
			}
			entry, err := zw.Create(name)
			if err != nil {
				_ = src.Close()
				continue
			}
			// 限制单条大小：包一层 LimitReader，EOF 时不当作错误。
			_, err = io.Copy(entry, io.LimitReader(src, maxZipEntryBytes))
			_ = src.Close()
			if err != nil && !errors.Is(err, io.EOF) {
				continue
			}
		}
	})
}

func openRecordedFile(dir *os.Root, name string) (*os.File, error) {
	info, err := dir.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w：产物文件是符号链接", errUnsafeArtifact)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w：产物文件不是普通文件", errUnsafeArtifact)
	}
	// 即使 Lstat 与 Open 之间文件被并发替换为符号链接，os.Root.Open 也会将
	// 解析结果限制在输出目录内，避免借此访问目录外的文件。
	file, err := dir.Open(name)
	if err != nil {
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("%w：产物文件打开后不是普通文件", errUnsafeArtifact)
	}
	return file, nil
}

// recordedFile 在任务记录的文件清单里找精确匹配。
func recordedFile(files []string, name string) (string, bool) {
	for _, f := range files {
		if f == name && isRecordedFileName(f) {
			return f, true
		}
	}
	return "", false
}

// recordedFiles 取出清单里的合法条目。
//
// 过滤掉含路径分隔符的条目：清单理论上只会有基名（转换器生成的），但这是
// 一个"按名字读文件"的路径，值不值得再挡一道由成本决定——挡一道很便宜。
func recordedFiles(files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if !isRecordedFileName(f) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func isRecordedFileName(name string) bool {
	return name != "" && !strings.ContainsAny(name, `/\`) && name != "." && name != ".."
}

// contentTypeFor 由扩展名推断 Content-Type。
//
// 先查转换库的格式注册表：它知道 png/jpeg/svg 这类格式的规范 MIME，
// 比通用表的条目准；查不到再退回通用推断。
func contentTypeFor(name string) string {
	if format, ok := converter.FormatByExtension(filepath.Ext(name)); ok && format.MIME != "" {
		return format.MIME
	}
	return "application/octet-stream"
}
