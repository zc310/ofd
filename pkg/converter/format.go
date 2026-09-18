package converter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
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

	importerList   = map[string]Importer{} // name → Importer
	importerExtMap = map[string]Importer{} // ".pdf" → Importer

	transformerList = map[string]Transformer{} // "from>to" → Transformer
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

// Importer 是非 OFD 输入（如 PDF、DOCX、PPTX）到 OFD 的导入器。
// 各输入格式包在 init() 中调用 RegisterImporter 注册，并按需空白导入，
// 使不使用的导入依赖不进最终二进制。
type Importer interface {
	// Name 返回输入格式名，如 "pdf"、"docx"、"pptx"。
	Name() string
	// Extensions 返回支持的文件扩展名，如 [".pdf"]。
	Extensions() []string
	// MIME 返回 MIME 类型，如 "application/pdf"。
	MIME() string
	// Import 把 input 读取为 OFD 并写入 output。
	Import(input any, output io.Writer, conv *Converter) error
}

// RegisterImporter 注册一个导入器。通常在 init() 中调用。
func RegisterImporter(imp Importer) {
	name := strings.ToLower(strings.TrimSpace(imp.Name()))
	if name == "" {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	importerList[name] = imp
	for _, ext := range imp.Extensions() {
		importerExtMap[normalizeExtension(ext)] = imp
	}
}

// Transformer 是某类输入到某个输出格式的直接转换器，用于避免"先导入 OFD
// 再导出"的中间损失（例如 docx→pdf 直接由 LibreOffice 完成）。各格式包在
// init() 中调用 RegisterTransformer 注册。
type Transformer interface {
	// From 返回支持的输入格式名，如 "docx"、"doc"。
	From() []string
	// To 返回输出格式名，如 "pdf"。
	To() string
	// Transform 把 input 直接转换为 output。
	Transform(input any, output io.Writer, conv *Converter) error
}

// RegisterTransformer 注册一个直接转换器。通常在 init() 中调用。
func RegisterTransformer(transformer Transformer) {
	to := normalizeFormatName(transformer.To())
	if to == "" {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, from := range transformer.From() {
		key := normalizeFormatName(from)
		if key == "" {
			continue
		}
		transformerList[key+">"+to] = transformer
	}
}

// TransformerFor 查找 from→to 的直接转换器。
func TransformerFor(from, to string) (Transformer, bool) {
	key := normalizeFormatName(from) + ">" + normalizeFormatName(to)
	registryMu.RLock()
	defer registryMu.RUnlock()
	transformer, ok := transformerList[key]
	return transformer, ok
}

// ImporterByName 按输入格式名（含别名或扩展名）查找导入器。
func ImporterByName(name string) (Importer, bool) {
	key := normalizeFormatName(name)
	registryMu.RLock()
	defer registryMu.RUnlock()
	if imp, ok := importerList[key]; ok {
		return imp, true
	}
	if imp, ok := importerExtMap[normalizeExtension(key)]; ok {
		return imp, true
	}
	return nil, false
}

// ImporterByExtension 按文件扩展名查找导入器。
func ImporterByExtension(ext string) (Importer, bool) {
	key := normalizeExtension(ext)
	if key == "" {
		return nil, false
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	imp, ok := importerExtMap[key]
	return imp, ok
}

// ImportFormats 返回所有已注册输入格式的名称快照，按名称排序。
func ImportFormats() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(importerList))
	for name := range importerList {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Convert 在输入格式 from 与输出格式 to 之间转换。from 为空时按文件扩展名或
// 内容识别。to 为 "ofd" 时使用导入器（X→OFD）；from 为 "ofd" 时使用导出器
// （OFD→X）；两者都不是时先导入为 OFD 再导出。
func Convert(from, to string, input any, output io.Writer, opts ...Option) error {
	conv := newConverter(opts...)
	to = normalizeFormatName(to)
	if to == "" {
		return errors.New("未指定输出格式")
	}
	from = normalizeFormatName(from)
	if from == "" {
		detected, err := detectInputFormat(input)
		if err != nil {
			return err
		}
		from = detected
	}
	switch {
	case from == "ofd" && to == "ofd":
		return errors.New("输入和输出不能都是 OFD")
	case from != "ofd" && to != "ofd":
		// 优先使用直接转换器（如 docx→pdf 交给 LibreOffice），避免经过 OFD
		// 中间格式造成的二次版式损失。
		if transformer, ok := TransformerFor(from, to); ok {
			return transformer.Transform(input, output, conv)
		}
		imp, ok := ImporterByName(from)
		if !ok {
			return fmt.Errorf("不支持导入格式: %s", from)
		}
		var intermediate bytes.Buffer
		if err := imp.Import(input, &intermediate, conv); err != nil {
			return err
		}
		return encodeWithConverterFormat(to, intermediate.Bytes(), output, conv)
	case to == "ofd":
		imp, ok := ImporterByName(from)
		if !ok {
			return fmt.Errorf("不支持从 %s 转换为 OFD", from)
		}
		if output == nil {
			return errors.New("未设置 OFD 输出参数")
		}
		return imp.Import(input, output, conv)
	default:
		return encodeWithConverterFormat(to, input, output, conv)
	}
}

// normalizeFormatName 统一格式名的大小写、空格与别名。
func normalizeFormatName(format string) string {
	key := strings.ToLower(strings.TrimSpace(format))
	if alias, ok := formatAliases[key]; ok {
		key = alias
	}
	return key
}

// detectInputFormat 根据输入的类型、扩展名或魔数识别格式。无法可靠识别时
// 返回错误，要求调用方显式指定 from。
func detectInputFormat(input any) (string, error) {
	switch value := input.(type) {
	case []*render.Document:
		return "ofd", nil
	case string:
		if ext := filepath.Ext(value); ext != "" {
			if imp, ok := ImporterByExtension(ext); ok {
				return imp.Name(), nil
			}
			if _, ok := FormatByExtension(ext); ok {
				return "ofd", nil
			}
		}
		return "", fmt.Errorf("无法从文件名识别输入格式: %s", value)
	case []byte:
		return sniffInputFormat(value)
	default:
		return "", errors.New("无法识别输入格式，请显式指定 from")
	}
}

// sniffInputFormat 通过魔数识别 PDF 与 OFD。OFD 与 DOCX/PPTX 都是 ZIP 容器，
// 这里只区分 OFD（含 OFD.xml）；其它 ZIP 输入需显式指定 from。
func sniffInputFormat(data []byte) (string, error) {
	if len(data) >= 5 && string(data[:5]) == "%PDF-" {
		return "pdf", nil
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte("PK\x03\x04")) && bytes.Contains(data, []byte("OFD.xml")) {
		return "ofd", nil
	}
	return "", errors.New("无法识别输入格式，请显式指定 from")
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
