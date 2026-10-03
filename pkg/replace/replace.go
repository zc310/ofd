// Package replace 提供 OFD 包内条目的替换、新增与删除能力。
//
// 替换按包内路径（如 "Doc_0/Pages/Page_0/Content.xml"）操作，不解析页面或
// 资源模型：命中的条目用新字节覆盖，其余条目原样搬运。由于任何字节改动都会
// 使已有签名摘要失效，Options.Signatures 默认丢弃签名目录；需要保留时可显式
// 选择 preserve 或 rewrite。
package replace

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/beevik/etree"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/entrywriter"
	"github.com/zc310/ofd/internal/spec"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

// OperationKind 描述对某个包内条目的操作类型。
type OperationKind string

const (
	// OpSet 替换已有条目，条目不存在时报错。
	OpSet OperationKind = "set"
	// OpAdd 新增条目，条目已存在时报错。
	OpAdd OperationKind = "add"
	// OpDelete 删除已有条目，条目不存在时报错。
	OpDelete OperationKind = "delete"
)

// Limits 限制输入与输出的规模，避免恶意或异常文档造成的解压放大。
type Limits = creator.Limits

// Operation 描述一次条目操作。
type Operation struct {
	// Kind 是操作类型。
	Kind OperationKind
	// Name 是包内路径，不带前导斜杠。
	Name string
	// Data 是 Set/Add 的新字节内容。
	Data []byte
}

// Options 控制替换输出方式。
type Options struct {
	// Compression 是输出 ZIP 的压缩策略，空值时按条目类型自动选择。
	Compression creator.CompressionMode
	// CompressionLevel 是 DEFLATE 压缩级别，0 使用默认级别 5，显式范围
	// 1（最快）到 9（最紧凑）。仅对实际使用 Deflate 的条目生效。
	CompressionLevel int
	// Deterministic 使用固定 ZIP 时间，生成可复现的结果。
	Deterministic bool
	// Signatures 是签名处理方式。零值等价于 creator.SignatureDrop，与包级说明
	// 一致：任何字节改动都会使已有摘要失效，保留一份摘要已失效的签名比直接移除
	// 更容易误导。需要保留签名条目（例如作为原始证据）时显式选 preserve。
	Signatures creator.SignatureMode
	// Limits 限制输入与输出的规模，零值使用默认限制。
	Limits Limits
	// Validate 在输出后执行严格 OFD 校验。
	Validate bool
	// SkipXMLCheck 关闭默认的 XML 良构检查；Set/Add 的新内容若命中 .xml 条目，
	// 默认会先解析校验，防止写入格式损坏的 XML。
	SkipXMLCheck bool
	// OnWarning 可选，接收非致命提示，例如签名被保留但摘要已失效。
	OnWarning func(string)
}

// Files 读取 input（文件路径、字节数据或 io.Reader），执行操作后写入 w。
func Files(input any, operations []Operation, w io.Writer, options Options) error {
	if w == nil {
		return errors.New("OFD 输出写入器为空")
	}
	options, err := normalizeOptions(options)
	if err != nil {
		return err
	}
	pkg, err := openAny(input)
	if err != nil {
		return err
	}
	defer func() { _ = pkg.Close() }()

	plan, err := buildPlan(pkg, operations, options)
	if err != nil {
		return err
	}
	var buffer bytes.Buffer
	if err := rebuild(pkg, plan, options, &buffer); err != nil {
		return err
	}
	if options.Validate {
		if err := validateBytes(buffer.Bytes()); err != nil {
			return err
		}
	}
	if _, err := w.Write(buffer.Bytes()); err != nil {
		return fmt.Errorf("写入输出失败: %w", err)
	}
	return nil
}

// Paths 是便捷入口：把所有 name->本地文件 映射作为 Set 操作执行。
func Paths(input any, files map[string]string, w io.Writer, options Options) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	operations := make([]Operation, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(files[name])
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", files[name], err)
		}
		operations = append(operations, Operation{Kind: OpSet, Name: name, Data: data})
	}
	return Files(input, operations, w, options)
}

// plan 保存校验后的操作分组与涉及的签名目录。
type plan struct {
	deletes  map[string]bool
	sets     map[string][]byte
	adds     map[string][]byte
	order    []string
	signDirs []string
}

// buildPlan 校验操作集合：路径安全、跨操作不冲突、set/add/delete 语义严格。
func buildPlan(pkg *core.Package, operations []Operation, options Options) (*plan, error) {
	result := &plan{
		deletes: make(map[string]bool),
		sets:    make(map[string][]byte),
		adds:    make(map[string][]byte),
	}
	existing := make(map[string]bool)
	for _, entry := range pkg.Entries() {
		if !entry.IsDir {
			existing[entry.Path] = true
		}
	}
	signDirs, err := signatureDirs(pkg)
	if err != nil {
		return nil, err
	}
	result.signDirs = signDirs

	seen := make(map[string]OperationKind)
	for _, operation := range operations {
		name := strings.TrimSpace(operation.Name)
		if err := core.ValidateEntryName(name); err != nil {
			return nil, err
		}
		if creator.IsSignatureEntry(name) {
			return nil, fmt.Errorf("不能直接修改签名条目 %s，请通过 Signatures 选项处理", name)
		}
		if previous, ok := seen[name]; ok {
			return nil, fmt.Errorf("条目 %s 同时出现在 %s 和 %s 操作中", name, previous, operation.Kind)
		}
		switch operation.Kind {
		case OpSet:
			if !existing[name] {
				return nil, fmt.Errorf("set 目标条目不存在: %s", name)
			}
			if err := options.checkXMLWellFormed(name, operation.Data); err != nil {
				return nil, err
			}
			result.sets[name] = operation.Data
		case OpAdd:
			if existing[name] {
				return nil, fmt.Errorf("add 目标条目已存在: %s", name)
			}
			if err := options.checkXMLWellFormed(name, operation.Data); err != nil {
				return nil, err
			}
			result.adds[name] = operation.Data
		case OpDelete:
			if !existing[name] {
				return nil, fmt.Errorf("delete 目标条目不存在: %s", name)
			}
			if strings.EqualFold(name, spec.RootDocument) {
				return nil, fmt.Errorf("不能删除 %s：它是包的入口，删除后输出不是合法 OFD", spec.RootDocument)
			}
			result.deletes[name] = true
		default:
			return nil, fmt.Errorf("不支持的操作类型: %q", operation.Kind)
		}
		seen[name] = operation.Kind
		result.order = append(result.order, name)
	}
	if len(result.deletes)+len(result.sets)+len(result.adds) == 0 {
		return nil, errors.New("没有需要执行的条目操作")
	}
	return result, nil
}

// checkXMLWellFormed 校验 Set/Add 命中 .xml 条目的新内容是否良构（含根元素）；
// 可通过 Options.SkipXMLCheck 关闭。非 XML 条目直接返回 nil。
func (options Options) checkXMLWellFormed(name string, data []byte) error {
	if options.SkipXMLCheck || !strings.EqualFold(path.Ext(name), ".xml") {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	rooted := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if !rooted {
				return fmt.Errorf("条目 %s 的新内容缺少根元素，不是良构 XML", name)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("条目 %s 的新内容不是良构 XML: %w", name, err)
		}
		switch token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if rooted {
					return fmt.Errorf("条目 %s 的新内容存在多个根元素，不是良构 XML", name)
				}
				rooted = true
			}
			depth++
		case xml.EndElement:
			depth--
			if depth < 0 {
				return fmt.Errorf("条目 %s 的新内容存在多余的结束标签，不是良构 XML", name)
			}
		}
	}
}

func normalizeOptions(options Options) (Options, error) {
	if err := creator.ValidateLimits(options.Limits); err != nil {
		return Options{}, err
	}
	mode, err := creator.NormalizeCompression(options.Compression)
	if err != nil {
		return Options{}, err
	}
	options.Compression = mode
	level, err := creator.NormalizeCompressionLevel(options.CompressionLevel)
	if err != nil {
		return Options{}, err
	}
	options.CompressionLevel = level
	switch options.Signatures {
	case "":
		options.Signatures = creator.SignatureDrop
	case creator.SignatureDrop, creator.SignaturePreserve, creator.SignatureRewrite:
	default:
		return Options{}, fmt.Errorf("不支持的签名处理方式: %q", options.Signatures)
	}
	return options, nil
}

func openAny(input any) (*core.Package, error) {
	switch value := input.(type) {
	case string:
		return core.OpenFile(value)
	case []byte:
		return core.OpenBytes(value)
	case io.Reader:
		return core.OpenReader(value)
	case *core.Package:
		return value, nil
	default:
		return nil, fmt.Errorf("不支持的输入类型: %T", input)
	}
}

// rootDocumentBytes 产出输出包中 OFD.xml 的最终字节。
//
// 单独处理是因为主循环跳过了它，而它恰恰可能被显式 Set 覆盖：此前无论用户
// 是否要求替换 OFD.xml，这里都写出原始字节，导致 Set 被静默忽略。
//
// 另外 SignatureDrop 只删签名条目、不改 OFD.xml，会留下指向已删除条目的
// DocBody/Signatures 引用，产出不再是合法 OFD。这里同步摘掉该引用。
func rootDocumentBytes(pkg *core.Package, plan *plan, options Options) ([]byte, error) {
	data, err := pkg.Read(spec.RootDocument)
	if err != nil {
		return nil, fmt.Errorf("读取 OFD.xml 失败: %w", err)
	}
	if custom, ok := plan.sets[spec.RootDocument]; ok {
		data = custom
	}
	if options.Signatures != creator.SignatureDrop {
		return data, nil
	}
	stripped, changed, err := stripSignaturesRef(data)
	if err != nil {
		return nil, fmt.Errorf("清理 OFD.xml 的签名引用失败: %w", err)
	}
	if changed && options.OnWarning != nil {
		options.OnWarning("已移除 OFD.xml 中的 DocBody/Signatures 引用，否则输出会指向已删除的签名条目")
	}
	return stripped, nil
}

// stripSignaturesRef 摘掉 OFD.xml 里 DocBody/Signatures 的签名列表引用，返回新
// 字节与是否发生了改动。没有该引用时原样返回。
func stripSignaturesRef(data []byte) ([]byte, bool, error) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, false, err
	}
	root := doc.Root()
	if root == nil {
		return nil, false, errors.New("OFD.xml 没有根元素")
	}
	var removed []*etree.Element
	for _, el := range root.FindElements("./DocBody/Signatures") {
		removed = append(removed, el)
	}
	if len(removed) == 0 {
		return data, false, nil
	}
	for _, el := range removed {
		el.Parent().RemoveChild(el)
	}
	out, err := doc.WriteToBytes()
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// signatureDirs 收集包内所有签名目录（含历史 Signs 布局）。
func signatureDirs(pkg *core.Package) ([]string, error) {
	dirs := make(map[string]bool)
	for _, entry := range pkg.Entries() {
		name := entry.Path
		switch path.Base(name) {
		case "Signatures.xml", "Signs.xml":
			dirs[path.Dir(name)] = true
		case "Signature.xml":
			dirs[path.Dir(path.Dir(name))] = true
		}
		if index := strings.Index(name, "/Signatures/"); index >= 0 {
			dirs[name[:index]+"/Signatures"] = true
		}
		if index := strings.Index(name, "/Signs/"); index >= 0 {
			dirs[name[:index]+"/Signs"] = true
		}
	}
	result := make([]string, 0, len(dirs))
	for dir := range dirs {
		result = append(result, dir)
	}
	sort.Strings(result)
	return result, nil
}

// rebuild 重建 ZIP 包：先按原顺序搬运（命中 set 写新内容、跳过 delete、
// 按签名模式处理签名目录），再追加 add 条目与 OFD.xml。
func rebuild(pkg *core.Package, plan *plan, options Options, w io.Writer) error {
	writer := zip.NewWriter(w)
	state := entrywriter.New(writer, entrywriter.Config{Compression: options.Compression, CompressionLevel: options.CompressionLevel, Deterministic: options.Deterministic, Limits: options.Limits, OnWarning: options.OnWarning})
	rootData, err := rootDocumentBytes(pkg, plan, options)
	if err != nil {
		_ = writer.Close()
		return err
	}

	for _, entry := range pkg.Entries() {
		if entry.IsDir || strings.EqualFold(entry.Path, spec.RootDocument) {
			continue
		}
		name := entry.Path
		if plan.deletes[name] {
			continue
		}
		if creator.InSignatureDir(name, plan.signDirs...) {
			if options.Signatures == creator.SignatureDrop {
				state.Warn(fmt.Sprintf("签名条目 %s 已移除：字节改动已使其摘要失效", name))
				continue
			}
			state.Warn(fmt.Sprintf("签名条目 %s 已保留，改动后的条目摘要可能失效", name))
		}
		if data, ok := plan.sets[name]; ok {
			if err := state.Write(name, data); err != nil {
				_ = writer.Close()
				return err
			}
			continue
		}
		if err := state.WriteSource(pkg, entry, name); err != nil {
			_ = writer.Close()
			return fmt.Errorf("写入 %s 失败: %w", name, err)
		}
	}
	for _, name := range plan.order {
		data, ok := plan.adds[name]
		if !ok {
			continue
		}
		if err := state.Write(name, data); err != nil {
			_ = writer.Close()
			return err
		}
	}
	if err := state.Write(spec.RootDocument, rootData); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("关闭 OFD ZIP 包失败: %w", err)
	}
	return nil
}

func validateBytes(data []byte) error {
	instance, err := validator.New()
	if err != nil {
		return err
	}
	report := instance.ValidateReader(context.Background(), bytes.NewReader(data), "replaced.ofd")
	if report.HasErrors() {
		var builder strings.Builder
		_ = validator.RenderText(&builder, report)
		return fmt.Errorf("替换结果未通过校验:\n%s", builder.String())
	}
	return nil
}
