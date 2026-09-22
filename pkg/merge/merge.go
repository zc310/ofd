// Package merge 提供 OFD 文档的合并能力。
//
// ZIP 级合并只重写 OFD.xml 与签名文件中的包内路径，把每个输入文档体
// （DocBody）的目录树原样搬运到新的 Doc_N 目录，不解析或重写页面、资源
// 等内部 XML，因此改动最小、资源 ID 不需要重映射。被引用文件的字节保持
// 不变，签名摘要仍然有效；但签名值（SignedValue）本身覆盖了签名清单，重写
// 路径后需要重新签名。签名处理方式由 Options.Signatures 控制。
//
// 模型级合并（Pages）把多个文档体的页面解析为 creator 模型后重新生成一个
// 单文档 OFD，支持跨文档拼页，但需要重新编号文档级资源；页面级资源冲突会由
// 创建器报错。
package merge

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/spec"
	"github.com/zc310/ofd/pkg/creator"
)

// SignatureMode 控制合并时如何处理签名文件。
type SignatureMode string

const (
	// SignaturePreserve 保持签名文件字节不变，仅重写 OFD.xml 中指向签名的路径。
	// 当文档被改名且签名使用包内绝对路径、必须重写签名文件才能保持引用有效时，
	// 返回错误，避免产出签名已失效的结果。空值等同于此模式。
	SignaturePreserve SignatureMode = "preserve"
	// SignatureRewrite 重写签名文件中的包内绝对路径，使其指向新目录。引用可解析、
	// 摘要仍有效，但签名值（SignedValue）覆盖签名清单，重写后原签名值失效。
	SignatureRewrite SignatureMode = "rewrite"
	// SignatureDrop 删除签名目录以及 OFD.xml 中的签名引用，产出无签名文档。
	SignatureDrop SignatureMode = "drop"
)

// OrphanMode 控制文档目录之外的条目如何处理。
type OrphanMode string

const (
	// OrphanError 在存在文档目录之外的条目时返回错误。空值等同于此模式。
	OrphanError OrphanMode = "error"
	// OrphanIgnore 跳过文档目录之外的条目。
	OrphanIgnore OrphanMode = "ignore"
	// OrphanPreserve 把文档目录之外的条目按原路径保留到输出包根目录。
	OrphanPreserve OrphanMode = "preserve"
)

// Limits 限制合并输入的规模，避免恶意或异常文档造成的解压放大。
type Limits struct {
	// MaxEntries 是允许搬运的最大条目数（含签名等所有非目录条目），
	// 0 表示默认 10000。
	MaxEntries int
	// MaxEntryBytes 是单个条目解压后的最大字节数，0 表示默认 64MB。
	MaxEntryBytes int64
	// MaxTotalBytes 是所有条目解压后的总字节上限，0 表示默认 512MB。
	MaxTotalBytes int64
}

const (
	defaultMaxEntries    = 10000
	defaultMaxEntryBytes = int64(64 << 20)
	defaultMaxTotalBytes = int64(512 << 20)
)

// Options 控制 ZIP 级合并的输出方式。
type Options struct {
	// Compression 是输出 ZIP 的压缩策略，空值时使用 creator.CompressionAuto。
	Compression creator.CompressionMode
	// Deterministic 使用固定 ZIP 时间，生成可复现的合并结果。
	Deterministic bool
	// Signatures 是签名处理方式，空值时使用 SignaturePreserve。
	Signatures SignatureMode
	// Orphans 是文档目录之外条目的处理方式，空值时使用 OrphanError。
	Orphans OrphanMode
	// Limits 限制合并输入的规模，零值使用默认限制。
	Limits Limits
	// OnWarning 可选，接收合并过程中的非致命提示，例如签名被重写后签名值失效。
	OnWarning func(string)
}

// bodyPlan 描述一个输入文档体从旧目录到新目录的搬运计划。
type bodyPlan struct {
	oldDir string
	newDir string
	// signatureDir 是旧的签名目录（包内相对路径），SignatureDrop 时整目录跳过。
	signatureDir string
}

// Source 描述一个可整体读取的 OFD 输入。Path、Data、Reader 必须且只能设置一个：
//   - Path 为文件路径，按需读取，内存占用最低；
//   - Data 为完整 OFD 字节数据；
//   - Reader 为随机访问的数据源，必须同时设置正数 Size，可避免整体复制到内存。
//
// Name 是可选的输入标识，用于错误信息。
type Source struct {
	Name   string
	Path   string
	Data   []byte
	Reader io.ReaderAt
	Size   int64
}

// Files 将 paths 指向的每个 OFD 文档体合并为一个多文档 OFD 包并写入 w。
// 输入文件按需读取、输出条目流式写入 w，适合较大的文档；即使只有一个输入
// 也会被重新打包为规范的多文档结构。
func Files(paths []string, w io.Writer, options Options) error {
	if len(paths) == 0 {
		return errors.New("至少需要一个 OFD 输入文件")
	}
	sources := make([]source, 0, len(paths))
	for _, path := range paths {
		sources = append(sources, fileSource(path))
	}
	return mergeSources(sources, w, options)
}

// Bytes 将 documents 中的每个 OFD 文档体合并为一个多文档 OFD 包并写入 w。
// documents 为完整的 OFD 字节数据，已驻留内存。
func Bytes(documents [][]byte, w io.Writer, options Options) error {
	if len(documents) == 0 {
		return errors.New("至少需要一个 OFD 输入文档")
	}
	sources := make([]source, 0, len(documents))
	for index, data := range documents {
		if len(data) == 0 {
			return fmt.Errorf("第 %d 个 OFD 输入文档为空", index)
		}
		sources = append(sources, bytesSource(index, data))
	}
	return mergeSources(sources, w, options)
}

// Sources 将每个 Source 的 OFD 文档体合并为一个多文档 OFD 包并写入 w。
// 每个 Source 的 Path、Data、Reader 必须且只能设置一个。
func Sources(inputs []Source, w io.Writer, options Options) error {
	if len(inputs) == 0 {
		return errors.New("至少需要一个 OFD 输入")
	}
	sources := make([]source, 0, len(inputs))
	for index, input := range inputs {
		converted, err := convertSource(index, input)
		if err != nil {
			return err
		}
		sources = append(sources, converted)
	}
	return mergeSources(sources, w, options)
}

// Marshal 合并 paths 指向的每个 OFD 文档体并返回完整 OFD 字节数据。
func Marshal(paths []string, options Options) ([]byte, error) {
	var buffer bytes.Buffer
	if err := Files(paths, &buffer, options); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// source 描述一个 OFD 输入，open 返回可读取的内部包，由调用方负责关闭。
type source struct {
	name string
	open func() (*core.Package, error)
}

func fileSource(path string) source {
	return source{name: path, open: func() (*core.Package, error) { return core.OpenFile(path) }}
}

func bytesSource(index int, data []byte) source {
	return source{name: fmt.Sprintf("第 %d 个输入文档", index), open: func() (*core.Package, error) { return core.OpenBytes(data) }}
}

// convertSource 校验并规范化一个公开的 Source。
func convertSource(index int, input Source) (source, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		if strings.TrimSpace(input.Path) != "" {
			name = input.Path
		} else {
			name = fmt.Sprintf("第 %d 个输入", index)
		}
	}
	configured := 0
	if strings.TrimSpace(input.Path) != "" {
		configured++
	}
	if len(input.Data) > 0 {
		configured++
	}
	if input.Reader != nil {
		configured++
	}
	if configured != 1 {
		return source{}, fmt.Errorf("输入 %s 必须且只能设置 Path、Data 或 Reader 之一", name)
	}
	switch {
	case strings.TrimSpace(input.Path) != "":
		return fileSource(input.Path), nil
	case len(input.Data) > 0:
		return source{name: name, open: func() (*core.Package, error) { return core.OpenBytes(input.Data) }}, nil
	default:
		if input.Size <= 0 {
			return source{}, fmt.Errorf("输入 %s 的 Reader 必须同时设置正数 Size", name)
		}
		reader, size := input.Reader, input.Size
		return source{name: name, open: func() (*core.Package, error) { return core.OpenReaderAt(reader, size) }}, nil
	}
}

func mergeSources(sources []source, w io.Writer, options Options) error {
	if w == nil {
		return errors.New("OFD 输出写入器为空")
	}
	options, err := normalizeOptions(options)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(w)
	state := &mergeState{
		writer:     newEntryWriter(archive, options),
		seenDocIDs: make(map[string]bool),
	}

	for _, src := range sources {
		if err := mergeSource(state, src); err != nil {
			_ = archive.Close()
			return err
		}
	}

	if len(state.bodies) == 0 {
		_ = archive.Close()
		return errors.New("没有可合并的文档体")
	}
	rootData, err := buildRootXML(state.bodies, state.attrs)
	if err != nil {
		_ = archive.Close()
		return err
	}
	if err := state.writer.write(spec.RootDocument, rootData); err != nil {
		_ = archive.Close()
		return err
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("关闭 OFD ZIP 包失败: %w", err)
	}
	return nil
}

// mergeState 汇总一次合并过程中的输出写入器与累计状态。
type mergeState struct {
	writer     *entryWriter
	bodies     []*etree.Element
	attrs      rootAttrs
	nextDir    int
	seenDocIDs map[string]bool
}

func normalizeOptions(options Options) (Options, error) {
	if options.Limits.MaxEntries < 0 || options.Limits.MaxEntryBytes < 0 || options.Limits.MaxTotalBytes < 0 {
		return Options{}, errors.New("合并规模限制不能为负数")
	}
	if options.Compression == "" {
		options.Compression = creator.CompressionAuto
	}
	switch options.Compression {
	case creator.CompressionAuto, creator.CompressionDeflate, creator.CompressionStore:
	default:
		return Options{}, fmt.Errorf("不支持的 ZIP 压缩策略: %q", options.Compression)
	}
	switch options.Signatures {
	case "":
		options.Signatures = SignaturePreserve
	case SignaturePreserve, SignatureRewrite, SignatureDrop:
	default:
		return Options{}, fmt.Errorf("不支持的签名处理方式: %q", options.Signatures)
	}
	switch options.Orphans {
	case "":
		options.Orphans = OrphanError
	case OrphanError, OrphanIgnore, OrphanPreserve:
	default:
		return Options{}, fmt.Errorf("不支持的目录外条目处理方式: %q", options.Orphans)
	}
	return options, nil
}

// rootAttrs 保存合并后 OFD.xml 根元素的通用属性。
type rootAttrs struct {
	version string
	docType string
	set     bool
}

// mergeSource 处理单个输入：解析 OFD.xml、规划目录改名、搬运目录树并
// 收集改写后的文档体元素。
func mergeSource(state *mergeState, src source) error {
	pkg, err := src.open()
	if err != nil {
		return fmt.Errorf("打开 OFD 输入 %s 失败: %w", src.name, err)
	}
	defer func() { _ = pkg.Close() }()

	rootData, err := pkg.ReadLimit(spec.RootDocument, state.writer.maxEntryBytes)
	if err != nil {
		return fmt.Errorf("读取 %s 的 OFD.xml 失败: %w", src.name, err)
	}
	rootDoc := etree.NewDocument()
	if err := rootDoc.ReadFromBytes(rootData); err != nil {
		return fmt.Errorf("解析 %s 的 OFD.xml 失败: %w", src.name, err)
	}
	root := rootDoc.Root()
	if root == nil || localName(root) != "OFD" {
		return fmt.Errorf("%s 的 OFD.xml 根元素无效", src.name)
	}
	if !state.attrs.set {
		state.attrs = readRootAttrs(root)
	}

	docBodies := directChildren(root, "DocBody")
	if len(docBodies) == 0 {
		return fmt.Errorf("%s 的 OFD.xml 中没有 DocBody", src.name)
	}

	plans := make([]bodyPlan, 0, len(docBodies))
	oldDirs := make(map[string]bool, len(docBodies))
	for _, body := range docBodies {
		docRoot := directChildText(body, "DocRoot")
		oldDir, err := normalizeDir(docRoot)
		if err != nil {
			return fmt.Errorf("%s 的 DocRoot %q 无效: %w", src.name, docRoot, err)
		}
		if oldDirs[oldDir] {
			return fmt.Errorf("%s 中多个 DocBody 共用文档目录 %s，无法合并", src.name, oldDir)
		}
		oldDirs[oldDir] = true

		newDir := fmt.Sprintf("Doc_%d", state.nextDir)
		state.nextDir++
		plan := bodyPlan{oldDir: oldDir, newDir: newDir}
		if state.writer.options.Signatures == SignatureDrop {
			plan.signatureDir = signatureDirOf(body)
		}
		plans = append(plans, plan)

		cloned := cloneElement(body)
		if state.writer.options.Signatures == SignatureDrop {
			removeBodySignatures(cloned)
		}
		rewriteBodyPaths(cloned, oldDir, newDir)
		ensureUniqueDocID(cloned, state.seenDocIDs)
		state.bodies = append(state.bodies, cloned)
	}

	if err := copyPackageEntries(state.writer, pkg, src.name, plans); err != nil {
		return err
	}
	return nil
}

// signatureDirOf 返回 DocBody 签名文件的所在目录（包内相对路径）。
func signatureDirOf(body *etree.Element) string {
	value := strings.TrimSpace(directChildText(body, "Signatures"))
	if value == "" {
		return ""
	}
	return path.Dir(path.Clean(strings.TrimPrefix(value, "/")))
}

// removeBodySignatures 从文档体克隆中删除 Signatures 路径元素。
func removeBodySignatures(body *etree.Element) {
	if child := directChild(body, "Signatures"); child != nil {
		body.RemoveChild(child)
	}
}

// copyPackageEntries 把输入包中除 OFD.xml 外的条目按目录计划搬运到输出包。
func copyPackageEntries(writer *entryWriter, pkg *core.Package, input string, plans []bodyPlan) error {
	signatures := writer.options.Signatures
	for _, entry := range pkg.Entries() {
		if entry.IsDir {
			continue
		}
		name := entry.Path
		if strings.EqualFold(name, spec.RootDocument) {
			continue
		}

		plan, ok := matchPlan(name, plans)
		if !ok {
			switch writer.options.Orphans {
			case OrphanIgnore:
				continue
			case OrphanPreserve:
				if err := writer.writeSource(pkg, entry, name); err != nil {
					return fmt.Errorf("写入 %s 的 %s 失败: %w", input, name, err)
				}
				continue
			default:
				return fmt.Errorf("%s 的条目 %s 不在任何文档目录内；可用 OrphanIgnore 跳过或 OrphanPreserve 保留", input, name)
			}
		}
		if signatures == SignatureDrop && inSignatureDir(name, plan.signatureDir) {
			continue
		}
		newName := plan.newDir + strings.TrimPrefix(name, plan.oldDir)

		if isSignatureXML(name) {
			if entry.UncompressedSize > uint64(writer.maxEntryBytes) {
				return fmt.Errorf("签名文件 %s 声明解压后 %d 字节，超过单条上限 %d 字节", name, entry.UncompressedSize, writer.maxEntryBytes)
			}
			data, err := pkg.ReadLimit(name, writer.maxEntryBytes)
			if err != nil {
				return fmt.Errorf("读取 %s 的 %s 失败: %w", input, name, err)
			}
			rewritten, changed := rewriteSignatureXML(data, plan.oldDir, plan.newDir)
			if changed {
				if signatures == SignaturePreserve {
					return fmt.Errorf("%s 的 %s 使用包内绝对路径，文档改名后必须重写路径才能保持引用有效；preserve 模式无法保留原始签名，请改用 rewrite 或 drop", input, name)
				}
				writer.warn(fmt.Sprintf("签名文件 %s 的包内绝对路径已重写为 %s，原签名值失效", name, newName))
				data = rewritten
			}
			if err := writer.write(newName, data); err != nil {
				return err
			}
			continue
		}

		if err := writer.writeSource(pkg, entry, newName); err != nil {
			return fmt.Errorf("写入 %s 的 %s 失败: %w", input, name, err)
		}
	}
	return nil
}

// matchPlan 找到条目所属的文档目录计划。oldDir 之间不会嵌套，因此按
// 目录前缀精确匹配即可。
func matchPlan(name string, plans []bodyPlan) (bodyPlan, bool) {
	for _, plan := range plans {
		if name == plan.oldDir || strings.HasPrefix(name, plan.oldDir+"/") {
			return plan, true
		}
	}
	return bodyPlan{}, false
}

// normalizeDir 规范化 DocRoot 的目录部分，返回不带首尾斜杠的安全目录名。
func normalizeDir(docRoot string) (string, error) {
	trimmed := strings.TrimSpace(docRoot)
	if trimmed == "" {
		return "", errors.New("DocRoot 为空")
	}
	dir := path.Dir(path.Clean(strings.TrimPrefix(trimmed, "/")))
	dir = strings.Trim(dir, "/")
	if err := core.ValidateEntryName(dir); err != nil {
		return "", fmt.Errorf("DocRoot 没有安全且独立的文档目录: %w", err)
	}
	return dir, nil
}

// rewriteBodyPaths 在文档体克隆上重写 DocRoot、Signatures 和版本 BaseLoc。
func rewriteBodyPaths(element *etree.Element, oldDir, newDir string) {
	switch localName(element) {
	case "DocRoot":
		element.SetText(rewriteRef(element.Text(), oldDir, newDir))
	case "Signatures":
		element.SetText(rewriteRef(element.Text(), oldDir, newDir))
	case "Version":
		if attr := element.SelectAttr("BaseLoc"); attr != nil {
			attr.Value = rewriteRef(attr.Value, oldDir, newDir)
		}
	}
	for _, child := range element.ChildElements() {
		rewriteBodyPaths(child, oldDir, newDir)
	}
}

// ensureUniqueDocID 保证合并结果中的 DocID 唯一；重复时重新生成。
func ensureUniqueDocID(body *etree.Element, seen map[string]bool) {
	info := directChild(body, "DocInfo")
	if info == nil {
		return
	}
	id := directChild(info, "DocID")
	if id == nil {
		return
	}
	value := strings.TrimSpace(id.Text())
	if value == "" {
		return
	}
	if !seen[value] {
		seen[value] = true
		id.SetText(value)
		return
	}
	for suffix := 1; ; suffix++ {
		generated := fmt.Sprintf("%s-%d", value, suffix)
		if !seen[generated] {
			seen[generated] = true
			id.SetText(generated)
			return
		}
	}
}

// rewriteSignatureXML 重写签名文件中指向旧文档目录的包内绝对路径。
// 未发生改动时返回原始字节，保证签名文件保持逐字节不变。
func rewriteSignatureXML(data []byte, oldDir, newDir string) ([]byte, bool) {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return data, false
	}
	root := doc.Root()
	if root == nil {
		return data, false
	}
	changed := false
	switch localName(root) {
	case "Signatures":
		walkElements(root, func(element *etree.Element) {
			if localName(element) != "Signature" {
				return
			}
			if attr := element.SelectAttr("BaseLoc"); attr != nil {
				if value := rewriteRef(attr.Value, oldDir, newDir); value != attr.Value {
					attr.Value = value
					changed = true
				}
			}
		})
	case "Signature":
		walkElements(root, func(element *etree.Element) {
			switch localName(element) {
			case "Reference":
				if attr := element.SelectAttr("FileRef"); attr != nil {
					if value := rewriteRef(attr.Value, oldDir, newDir); value != attr.Value {
						attr.Value = value
						changed = true
					}
				}
			case "SignedValue":
				if value := rewriteRef(element.Text(), oldDir, newDir); value != element.Text() {
					element.SetText(value)
					changed = true
				}
			case "BaseLoc":
				if parent := element.Parent(); parent != nil && localName(parent) == "Seal" {
					if value := rewriteRef(element.Text(), oldDir, newDir); value != element.Text() {
						element.SetText(value)
						changed = true
					}
				}
			}
		})
	}
	if !changed {
		return data, false
	}
	out, err := doc.WriteToBytes()
	if err != nil {
		return data, false
	}
	return out, true
}

// rewriteRef 把包内路径中的旧文档目录替换为新目录，保留前导斜杠与相对形式。
func rewriteRef(value, oldDir, newDir string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return value
	}
	absolute := strings.HasPrefix(trimmed, "/")
	clean := strings.TrimPrefix(trimmed, "/")
	switch {
	case clean == oldDir:
		clean = newDir
	case strings.HasPrefix(clean, oldDir+"/"):
		clean = newDir + strings.TrimPrefix(clean, oldDir)
	default:
		return value
	}
	if absolute {
		return "/" + clean
	}
	return clean
}

// cloneElement 递归复制元素，去掉命名空间前缀，使其落入输出的默认命名空间。
func cloneElement(src *etree.Element) *etree.Element {
	dst := etree.NewElement(src.Tag)
	copyAttrs(src, dst)
	if len(src.ChildElements()) == 0 {
		dst.SetText(src.Text())
		return dst
	}
	for _, child := range src.ChildElements() {
		dst.AddChild(cloneElement(child))
	}
	return dst
}

func copyAttrs(src, dst *etree.Element) {
	for _, attr := range src.Attr {
		if attr.Space == "xmlns" || attr.Key == "xmlns" {
			continue
		}
		dst.CreateAttr(attr.FullKey(), attr.Value)
	}
}

func buildRootXML(bodies []*etree.Element, attrs rootAttrs) ([]byte, error) {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	root := doc.CreateElement("OFD")
	root.CreateAttr("xmlns", spec.Namespace)
	version := attrs.version
	if version == "" {
		version = "1.0"
	}
	docType := attrs.docType
	if docType == "" {
		docType = "OFD"
	}
	root.CreateAttr("Version", version)
	root.CreateAttr("DocType", docType)
	for _, body := range bodies {
		root.AddChild(body)
	}
	out, err := doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("生成 OFD.xml 失败: %w", err)
	}
	return out, nil
}

func readRootAttrs(root *etree.Element) rootAttrs {
	return rootAttrs{
		version: root.SelectAttrValue("Version", ""),
		docType: root.SelectAttrValue("DocType", ""),
		set:     true,
	}
}

// entryWriter 校验并写入输出 ZIP 条目，同时拒绝重复路径。
type entryWriter struct {
	archive *zip.Writer
	options Options
	seen    map[string]bool

	entryCount    int
	remaining     int64
	maxEntries    int
	maxEntryBytes int64
	maxTotalBytes int64
}

func newEntryWriter(archive *zip.Writer, options Options) *entryWriter {
	limits := options.Limits
	if limits.MaxEntries == 0 {
		limits.MaxEntries = defaultMaxEntries
	}
	if limits.MaxEntryBytes == 0 {
		limits.MaxEntryBytes = defaultMaxEntryBytes
	}
	if limits.MaxTotalBytes == 0 {
		limits.MaxTotalBytes = defaultMaxTotalBytes
	}
	return &entryWriter{
		archive:       archive,
		options:       options,
		seen:          make(map[string]bool),
		remaining:     limits.MaxTotalBytes,
		maxEntries:    limits.MaxEntries,
		maxEntryBytes: limits.MaxEntryBytes,
		maxTotalBytes: limits.MaxTotalBytes,
	}
}

// reserve 校验条目标路径并预留条目配额。
func (w *entryWriter) reserve(name string) error {
	if err := validateEntryName(name, w.seen); err != nil {
		return err
	}
	if w.entryCount >= w.maxEntries {
		return fmt.Errorf("合并条目数超过上限 %d", w.maxEntries)
	}
	w.entryCount++
	return nil
}

// checkSize 校验单个条目解压后的字节数是否在单条与总预算内，并扣减总预算。
func (w *entryWriter) checkSize(name string, size int64) error {
	if size > w.maxEntryBytes {
		return fmt.Errorf("条目 %s 解压后 %d 字节，超过单条上限 %d 字节", name, size, w.maxEntryBytes)
	}
	if size > w.remaining {
		return fmt.Errorf("解压总大小超过上限 %d 字节", w.maxTotalBytes)
	}
	w.remaining -= size
	return nil
}

// warn 通过 Options.OnWarning 上报非致命提示。
func (w *entryWriter) warn(message string) {
	if w.options.OnWarning != nil {
		w.options.OnWarning(message)
	}
}

// validateEntryName 校验包内条目路径安全且未重复出现。
func validateEntryName(name string, seen map[string]bool) error {
	if err := core.ValidateEntryName(name); err != nil {
		return err
	}
	if seen[name] {
		return fmt.Errorf("OFD 包条目路径重复: %s", name)
	}
	seen[name] = true
	return nil
}

// write 按压缩策略与确定性选项写入一个内存条目。
func (w *entryWriter) write(name string, data []byte) error {
	if err := w.reserve(name); err != nil {
		return err
	}
	if err := w.checkSize(name, int64(len(data))); err != nil {
		return err
	}
	header := &zip.FileHeader{Name: name, Method: zipEntryMethod(name, w.options.Compression)}
	if w.options.Deterministic {
		header.Modified = time.Unix(0, 0).UTC()
	}
	file, err := w.archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("创建 ZIP 条目 %q 失败: %w", name, err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("写入 ZIP 条目 %q 失败: %w", name, err)
	}
	return nil
}

// writeSource 流式写入输入包中的条目，避免资源整体驻留内存。
func (w *entryWriter) writeSource(pkg *core.Package, entry core.Entry, name string) error {
	if err := w.reserve(name); err != nil {
		return err
	}
	if entry.UncompressedSize > uint64(w.maxEntryBytes) {
		return fmt.Errorf("条目 %s 声明解压后 %d 字节，超过单条上限 %d 字节", name, entry.UncompressedSize, w.maxEntryBytes)
	}
	if int64(entry.UncompressedSize) > w.remaining {
		return fmt.Errorf("解压总大小超过上限 %d 字节", w.maxTotalBytes)
	}
	reader, err := pkg.OpenEntry(entry)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	header := &zip.FileHeader{Name: name, Method: zipEntryMethod(name, w.options.Compression)}
	if w.options.Deterministic {
		header.Modified = time.Unix(0, 0).UTC()
	}
	file, err := w.archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("创建 ZIP 条目 %q 失败: %w", name, err)
	}
	counter := &limitWriter{writer: file, entryRemaining: w.maxEntryBytes, totalRemaining: &w.remaining, name: name, maxTotal: w.maxTotalBytes}
	if _, err := io.Copy(counter, reader); err != nil {
		return err
	}
	return nil
}

// limitWriter 在解压复制过程中同时限制单条和总字节数，防止声明大小与实际不符。
type limitWriter struct {
	writer         io.Writer
	entryRemaining int64
	totalRemaining *int64
	name           string
	maxTotal       int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.entryRemaining {
		return 0, fmt.Errorf("条目 %s 解压后超过单条大小上限", l.name)
	}
	if int64(len(p)) > *l.totalRemaining {
		return 0, fmt.Errorf("解压总大小超过上限 %d 字节", l.maxTotal)
	}
	n, err := l.writer.Write(p)
	l.entryRemaining -= int64(n)
	*l.totalRemaining -= int64(n)
	return n, err
}

// zipEntryMethod 与 creator 的压缩策略保持一致：已压缩格式使用 Store。
func zipEntryMethod(name string, mode creator.CompressionMode) uint16 {
	if mode == creator.CompressionStore {
		return zip.Store
	}
	if mode == creator.CompressionDeflate {
		return zip.Deflate
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".jxl",
		".mp3", ".mp4", ".m4a", ".aac", ".ogg", ".oga", ".opus", ".wav",
		".flac", ".mpg", ".mpeg", ".avi", ".mkv", ".mov", ".webm",
		".m4v", ".wma", ".pdf", ".zip", ".gz", ".bz2", ".xz", ".7z", ".rar":
		return zip.Store
	default:
		return zip.Deflate
	}
}

func isSignatureXML(name string) bool {
	switch path.Base(name) {
	case "Signatures.xml", "Signature.xml":
		return true
	default:
		return false
	}
}

// inSignatureDir 判断条目是否位于指定的签名目录内。
func inSignatureDir(name, dir string) bool {
	if dir == "" {
		return false
	}
	return name == dir || strings.HasPrefix(name, dir+"/")
}

func localName(element *etree.Element) string {
	if element == nil {
		return ""
	}
	if index := strings.Index(element.Tag, ":"); index >= 0 {
		return element.Tag[index+1:]
	}
	return element.Tag
}

func directChildren(element *etree.Element, local string) []*etree.Element {
	var result []*etree.Element
	for _, child := range element.ChildElements() {
		if localName(child) == local {
			result = append(result, child)
		}
	}
	return result
}

func directChild(element *etree.Element, local string) *etree.Element {
	for _, child := range element.ChildElements() {
		if localName(child) == local {
			return child
		}
	}
	return nil
}

func directChildText(element *etree.Element, local string) string {
	child := directChild(element, local)
	if child == nil {
		return ""
	}
	return child.Text()
}

func walkElements(element *etree.Element, visit func(*etree.Element)) {
	visit(element)
	for _, child := range element.ChildElements() {
		walkElements(child, visit)
	}
}
