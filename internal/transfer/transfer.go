// Package transfer 为 ofd-server 定义输入与输出两侧的抽象：待转换文档从哪里来，
// 转换结果写到哪里去。
//
// 服务端接收两类输入——HTTP 上传的字节流，或由调用方给出的 URL 拉取。URL 拉取
// 必须经过 allowlist 校验，否则调用方能让服务去请求内网地址或云元数据端点。
// 输出侧同理，调用方指定的目录与文件名必须限制在配置允许的根目录内。
//
// 放在 internal 下是因为它只服务于 ofd-server：Source/Sink 的取舍都按服务端
// 场景定（例如输出默认只新建、不允许覆盖）。需要把转换库接到对象存储的调用方
// 可以直接用 pkg/converter 的 []byte 与路径入参，不必依赖本包。
package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrExists 表示目标已存在且该目标不允许覆盖。
//
// 单列一个哨兵错误而不是靠消息文本判断，是因为调用方需要据此决定要不要重试：
// 重试一个"文件已存在"永远还是已存在，白白耗掉 worker 与退避时间。而它恰恰是
// output.dir 指向共享目录、多个任务写同名文件时必然出现的错误。
var ErrExists = errors.New("目标已存在")

// DefaultMaxBytes 是单个文件的默认上限。输入与输出共用同一个上限常量，但可以
// 分别覆盖。
const DefaultMaxBytes int64 = 64 << 20

// Location 描述转换结果的存放位置，出现在任务状态与通知回调里。
type Location struct {
	// Kind 为 "stream"、"dir" 或后续实现的 "s3"、"ftp"。
	Kind string `json:"kind"`
	// Path 是本地绝对路径（Kind 为 "dir" 时）。
	Path string `json:"path,omitempty"`
	// Bucket 与 Key 用于对象存储。
	Bucket string `json:"bucket,omitempty"`
	Key    string `json:"key,omitempty"`
	// URL 是可直接访问的地址，由具体后端可选地填充。
	URL string `json:"url,omitempty"`
	// Size 是写入的字节数。
	Size int64 `json:"size"`
}

// Source 是待转换文档的来源。每次任务独占一个 Source，用完应当调用 Close 释放
// 其占用的临时文件。
type Source interface {
	// Name 返回带扩展名的文件名。转换库按扩展名识别输入格式（docx/xlsx/html 等
	// 无法靠魔数区分），因此这里的扩展名必须是可信的、由服务端按已解析的格式
	// 派生，而不是直接采信调用方的原始文件名。
	Name() string
	// Open 打开内容供读取。调用方负责关闭返回的 ReadCloser。
	Open(ctx context.Context) (io.ReadCloser, error)
	// Close 释放来源占用的资源，对内存来源是空操作。
	Close() error
}

// Sink 是转换结果的写入目标。
type Sink interface {
	// Put 把 r 的内容写入名为 name 的目标，返回可定位的结果。
	Put(ctx context.Context, name string, r io.Reader) (Location, error)
}

// BytesSource 是内存来源，用于测试与小文件。
type BytesSource struct {
	Data []byte
	File string
	// 关闭时是否释放 Data。
	release bool
}

// NewBytesSource 以给定文件名构造内存来源。
func NewBytesSource(data []byte, file string) *BytesSource {
	return &BytesSource{Data: data, File: file}
}

func (s *BytesSource) Name() string { return s.File }

func (s *BytesSource) Open(context.Context) (io.ReadCloser, error) {
	if s.Data == nil {
		return nil, fmt.Errorf("内存来源没有内容")
	}
	return io.NopCloser(bytes.NewReader(s.Data)), nil
}

func (s *BytesSource) Close() error {
	if s.release {
		s.Data = nil
	}
	return nil
}

// FileSource 是磁盘文件来源。Close 会删除该文件，因此路径必须由服务端创建、
// 而不是调用方指定。
type FileSource struct {
	Path string
	// Keep 为真时 Close 不删除文件，用于复用已存在的样本文件。
	Keep bool
}

// NewFileSource 构造磁盘来源，keep 为真表示不删除。
func NewFileSource(path string, keep bool) *FileSource {
	return &FileSource{Path: path, Keep: keep}
}

func (s *FileSource) Name() string { return filepath.Base(s.Path) }

func (s *FileSource) Open(context.Context) (io.ReadCloser, error) {
	if s.Path == "" {
		return nil, fmt.Errorf("文件来源没有路径")
	}
	file, err := os.Open(s.Path)
	if err != nil {
		return nil, fmt.Errorf("打开输入文件失败: %w", err)
	}
	return file, nil
}

func (s *FileSource) Close() error {
	if s.Keep || s.Path == "" {
		return nil
	}
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除临时输入文件失败: %w", err)
	}
	return nil
}

// BufferSink 把结果留在内存里，用于把转换结果直接回传响应体。
type BufferSink struct {
	Limit int64

	buf   []byte
	limit int64
}

// NewBufferSink 构造内存目标，limit 为 0 时用 DefaultMaxBytes。
func NewBufferSink(limit int64) *BufferSink {
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	return &BufferSink{limit: limit}
}

func (s *BufferSink) Put(ctx context.Context, name string, r io.Reader) (Location, error) {
	if err := ctx.Err(); err != nil {
		return Location{}, err
	}
	data, err := ReadLimited(ctx, r, s.limit)
	if err != nil {
		return Location{}, err
	}
	s.buf = data
	return Location{Kind: "stream", Path: name, Size: int64(len(data))}, nil
}

// Bytes 返回已写入的内容。
func (s *BufferSink) Bytes() []byte { return s.buf }

// DirSink 把结果写入本地目录。
type DirSink struct {
	// Root 是允许写入的根目录。Put 会拒绝任何逃出该目录的 name。
	Root string
	// MaxBytes 单文件上限，0 时用 DefaultMaxBytes。
	MaxBytes int64
	// Overwrite 为真时允许覆盖已存在的普通文件。默认只新建：既避免误覆盖，也
	// 顺带杜绝了"目标是指向目录外的符号链接"这一类写入劫持。
	Overwrite bool
}

// NewDirSink 构造目录目标。
func NewDirSink(root string) *DirSink { return &DirSink{Root: root} }

func (s *DirSink) Put(ctx context.Context, name string, r io.Reader) (Location, error) {
	if s.Root == "" {
		return Location{}, fmt.Errorf("未配置输出目录")
	}
	target, err := ResolveInRoot(s.Root, name)
	if err != nil {
		return Location{}, err
	}
	limit := s.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}

	// 先在同目录内写临时文件再改名，避免中途失败留下半截文件被当成有效结果。
	tmp, err := os.CreateTemp(filepath.Dir(target), ".ofd-out-*")
	if err != nil {
		return Location{}, fmt.Errorf("创建输出临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	// 改名成功后目标已存在，这里删除的是旧名字，是空操作。
	defer func() { _ = os.Remove(tmpName) }()

	written, err := CopyLimited(ctx, tmp, r, limit)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Location{}, fmt.Errorf("写入输出失败: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return Location{}, fmt.Errorf("设置输出权限失败: %w", err)
	}
	if s.Overwrite {
		// 目标是符号链接时拒绝：跟随链接会把内容写到根目录之外。
		if info, statErr := os.Lstat(target); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return Location{}, fmt.Errorf("拒绝覆盖符号链接: %s", target)
		}
		if err := os.Rename(tmpName, target); err != nil {
			return Location{}, fmt.Errorf("落盘输出失败: %w", err)
		}
		return Location{Kind: "dir", Path: target, Size: written}, nil
	}
	// 只新建：用 link 而不是 rename。rename 会无条件替换已存在的文件，而 link 在
	// 目标已存在时失败，从而把"不允许覆盖"变成原子的判断，不受检查与改名之间的
	// 时间窗影响。临时文件与目标同目录，link 不会跨文件系统。
	if err := os.Link(tmpName, target); err != nil {
		if os.IsExist(err) {
			return Location{}, fmt.Errorf("%w: 输出文件已存在且不允许覆盖: %s", ErrExists, target)
		}
		return Location{}, fmt.Errorf("落盘输出失败: %w", err)
	}
	return Location{Kind: "dir", Path: target, Size: written}, nil
}

// ResolveInRoot 把 name 解析到 root 之内，并拒绝任何路径逃逸。
//
// 只允许单一段文件名：含分隔符、绝对路径、"." 或 ".." 的名字一律拒绝。这样
// name 就无法表达 "../../etc/passwd" 这类意图，也不需要在拼好路径后再做前缀
// 判断（前缀判断会被 root 为 "/" 之类的边界情况骗到）。
func ResolveInRoot(root, name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("输出文件名为空")
	}
	if strings.ContainsAny(trimmed, `/\`) || strings.ContainsRune(trimmed, 0) {
		return "", fmt.Errorf("输出文件名不能包含路径分隔符: %q", name)
	}
	if trimmed == "." || trimmed == ".." {
		return "", fmt.Errorf("输出文件名无效: %q", name)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("解析根目录失败: %w", err)
	}
	target := filepath.Join(absRoot, trimmed)
	rel, err := filepath.Rel(absRoot, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("输出路径逃出根目录: %q", name)
	}
	return target, nil
}

// ReadLimited 读取 r 的全部内容，超过 limit 字节即报错。
func ReadLimited(ctx context.Context, r io.Reader, limit int64) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := CopyLimited(ctx, &buf, r, limit); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CopyLimited 把 r 拷贝到 w，累计超过 limit 字节时中止并报错。分块读取，内存占用
// 与内容大小无关。
func CopyLimited(ctx context.Context, w io.Writer, r io.Reader, limit int64) (int64, error) {
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	buf := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := r.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > limit {
				return total, fmt.Errorf("内容超过上限 %d 字节", limit)
			}
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return total, writeErr
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
		if n == 0 {
			continue
		}
	}
}
