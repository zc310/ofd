package converter

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

// Kind 表示输出格式的类别，用于调用方按类别分发转换行为。
type Kind int

const (
	// KindDocument 输出单个文档，例如 PDF、HTML、文本和 Markdown。
	KindDocument Kind = iota
	// KindImage 逐页输出图像或矢量，例如 PNG、JPEG、SVG、EPS 和 TeX。
	KindImage
)

// Encoder 是所有输出格式编码器必须实现的接口。
// 各格式文件在 init() 中调用 Register 注册。
type Encoder interface {
	// Name 返回格式名称，如 "pdf"、"png"、"html"。
	Name() string

	// Kind 返回格式类别，调用方据此区分文档输出和逐页图像输出。
	Kind() Kind

	// Extensions 返回支持的文件扩展名，如 [".pdf"]、[".png", ".jpg"]。
	Extensions() []string

	// MIME 返回 MIME 类型，如 "application/pdf"。
	MIME() string

	// Encode 把 input 写入 output。input 可以是原始 OFD 输入（路径、[]byte、
	// io.Reader），也可以是已解析的 []*render.Document。
	// 需要解析 OFD 的编码器应通过 encodeOFD 复用统一的解析逻辑。
	Encode(input any, output io.Writer, conv *Converter) error
}

// Format 是已注册格式的元信息。
type Format struct {
	Name       string
	Kind       Kind
	Extensions []string
	MIME       string
	encoder    Encoder
}

var (
	registryMu sync.RWMutex
	formatList = map[string]*Format{} // name → Format
	extMap     = map[string]*Format{} // ".png" → Format
)

// formatAliases 把常见的简写映射到注册名，方便 CLI 等调用方按扩展名或别名分发。
var formatAliases = map[string]string{
	"txt":      "text",
	"md":       "markdown",
	"markdown": "markdown",
	"jpg":      "jpeg",
	"jpeg":     "jpeg",
}

// Register 注册一个编码器。通常在 init() 中调用。
func Register(enc Encoder) {
	registryMu.Lock()
	defer registryMu.Unlock()
	f := &Format{
		Name:       enc.Name(),
		Kind:       enc.Kind(),
		Extensions: enc.Extensions(),
		MIME:       enc.MIME(),
		encoder:    enc,
	}
	formatList[f.Name] = f
	for _, ext := range f.Extensions {
		extMap[normalizeExtension(ext)] = f
	}
}

// Encode 解析 input 中的 OFD 文档，并按 format 选择注册的编码器写入 output。
// format 可以是注册名（"pdf"、"text"、"markdown"、"jpeg"）、别名（"txt"、"md"、
// "jpg"）或带点/不带点的文件扩展名（".png"、"svg"）。
func Encode(format string, input any, output io.Writer, opts ...Option) error {
	conv := newConverter(opts...)
	f, err := lookupFormat(format)
	if err != nil {
		return err
	}
	if f.Kind == KindImage && output == nil && conv.fileWriter == nil && conv.imageWriter == nil {
		return errors.New("未设置图像输出参数")
	}
	return f.encoder.Encode(input, output, conv)
}

func encodeWithConverterFormat(format string, input any, output io.Writer, conv *Converter) error {
	f, err := lookupFormat(format)
	if err != nil {
		return err
	}
	return f.encoder.Encode(input, output, conv)
}

// EncodeDocuments 按 format 选择注册的编码器，直接写入已解析的文档。
// 当 output 为 nil 时，逐页图像格式使用 Option 中通过 Writer 或 ImageWriter 提供的写入器。
func EncodeDocuments(format string, documents []*render.Document, output io.Writer, opts ...Option) error {
	f, err := lookupFormat(format)
	if err != nil {
		return err
	}
	return f.encoder.Encode(documents, output, newConverter(opts...))
}

// encodeOFD 为处理 OFD 输入的编码器复用统一的解析流程：input 已是文档切片时直接使用，
// 否则解析 OFD 并设置文档标题，最后交给 encode 编码。
func encodeOFD(input any, output io.Writer, conv *Converter, encode func([]*render.Document, io.Writer, *Converter) error) error {
	documents, ofd, err := documentsFromInput(input, conv)
	if ofd != nil {
		defer closeOFD(ofd)
	}
	if err != nil {
		return err
	}
	if ofd != nil {
		conv.docTitle = htmlDocumentTitle(ofd)
	}
	return encode(documents, output, conv)
}

// documentsFromInput 在 input 已是已解析文档时直接返回，否则解析 OFD 输入。
func documentsFromInput(input any, conv *Converter) ([]*render.Document, *parser.OFD, error) {
	if documents, ok := input.([]*render.Document); ok {
		return documents, nil, nil
	}
	return parseRenderDocuments(input, conv)
}

func parseRenderDocuments(input any, conv *Converter) ([]*render.Document, *parser.OFD, error) {
	ofd, err := parser.NewOFDWithOptions(input, parser.Options{
		PageCacheCapacity: conv.pageCacheCapacity,
		PageCacheBytes:    conv.pageCacheBytes,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("解析OFD失败: %w", err)
	}
	if len(ofd.Documents) == 0 {
		return nil, ofd, errors.New("没有文档")
	}
	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocumentWithDPI(conv.bgColor, document, conv.dpi))
	}
	if len(collectDocumentPages(documents)) == 0 {
		return nil, ofd, errors.New("文档没有页面")
	}
	return documents, ofd, nil
}

func closeOFD(ofd *parser.OFD) {
	if ofd == nil {
		return
	}
	if err := ofd.Close(); err != nil {
		slog.Error("关闭OFD文档失败", "error", err)
	}
}

// lookupFormat 按注册名、别名或文件扩展名解析格式。
func lookupFormat(format string) (*Format, error) {
	key := strings.ToLower(strings.TrimSpace(format))
	if key == "" {
		return nil, errors.New("未指定输出格式")
	}
	if alias, ok := formatAliases[key]; ok {
		key = alias
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	if f, ok := formatList[key]; ok {
		return f, nil
	}
	if f, ok := extMap[normalizeExtension(key)]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("不支持的输出格式: %s", format)
}

// FormatByExtension 根据文件扩展名查找格式，如 ".pdf" → Format。
func FormatByExtension(ext string) (*Format, bool) {
	key := normalizeExtension(ext)
	if key == "" {
		return nil, false
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := extMap[key]
	return f, ok
}

// FormatByName 根据格式名（或别名）查找格式。
func FormatByName(name string) (*Format, bool) {
	f, err := lookupFormat(name)
	return f, err == nil
}

// IsImageFormat 判断给定格式名、别名或扩展名是否逐页输出图像。
func IsImageFormat(format string) bool {
	f, err := lookupFormat(format)
	return err == nil && f.Kind == KindImage
}

// Formats 返回所有已注册格式的快照，按名称排序。
func Formats() []*Format {
	registryMu.RLock()
	defer registryMu.RUnlock()
	result := make([]*Format, 0, len(formatList))
	for _, f := range formatList {
		result = append(result, f)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// normalizeExtension 把扩展名统一为小写的 ".ext" 形式；已有前导点时保留。
func normalizeExtension(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}
