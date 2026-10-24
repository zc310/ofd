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

	"github.com/beevik/etree"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/entrywriter"
	"github.com/zc310/ofd/internal/xmlcodec"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
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
type Limits = creator.Limits

// SignatureAction 描述合并过程对某个签名采取的动作。
type SignatureAction string

const (
	// SignaturePreserved 表示签名文件字节原样保留。
	SignaturePreserved SignatureAction = "preserved"
	// SignatureRewritten 表示签名文件中的包内路径被重写，签名值会失效。
	SignatureRewritten SignatureAction = "rewritten"
	// SignatureDropped 表示签名被丢弃。
	SignatureDropped SignatureAction = "dropped"
)

// SignatureEvent 描述某个输入签名的处理结果。
type SignatureEvent struct {
	// Input 是来源名称，文件输入时为路径。
	Input string
	// DocumentIndex 是文档体索引，从 0 开始。
	DocumentIndex int
	// ID 是签名标识或签名文件路径。
	ID string
	// Action 是对该签名的处理动作。
	Action SignatureAction
}

// Options 控制 ZIP 级合并的输出方式。
type Options struct {
	// Compression 是输出 ZIP 的压缩策略，空值时使用 creator.CompressionAuto。
	Compression creator.CompressionMode
	// CompressionLevel 是 DEFLATE 压缩级别，0 使用默认级别 5，显式范围
	// 1（最快）到 9（最紧凑）。仅对实际使用 Deflate 的条目生效。
	CompressionLevel int
	// Deterministic 使用固定 ZIP 时间，生成可复现的合并结果。
	Deterministic bool
	// Signatures 是签名处理方式，空值时使用 creator.SignaturePreserve。
	// preserve 无法保持签名路径或受保护文件字节不变时返回错误。
	Signatures creator.SignatureMode
	// Orphans 是文档目录之外条目的处理方式，空值时使用 OrphanError。
	Orphans OrphanMode
	// Limits 限制合并输入的规模，零值使用默认限制。
	Limits Limits
	// OnWarning 可选，接收合并过程中的非致命提示，例如签名被重写后签名值失效。
	OnWarning func(string)
	// OnSignature 可选，接收每个输入签名的处理结果。
	OnSignature func(SignatureEvent)
}

// bodyPlan 描述一个输入文档体从旧目录到新目录的搬运计划。
type bodyPlan struct {
	oldDir string
	newDir string
	// signatureFiles 只记录签名清单及其声明的签名文件、签名值和印章。
	signatureFiles map[string]signatureEntryKind
	// documentIndex 是文档体索引，用于签名事件。
	documentIndex int
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
	for _, p := range paths {
		sources = append(sources, fileSource(p))
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
	open func() (*container.Package, error)
}

func fileSource(path string) source {
	return source{name: path, open: func() (*container.Package, error) { return container.OpenFile(path) }}
}

func bytesSource(index int, data []byte) source {
	return source{name: fmt.Sprintf("第 %d 个输入文档", index), open: func() (*container.Package, error) { return container.OpenBytes(data) }}
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
		return source{name: name, open: func() (*container.Package, error) { return container.OpenBytes(input.Data) }}, nil
	default:
		if input.Size <= 0 {
			return source{}, fmt.Errorf("输入 %s 的 Reader 必须同时设置正数 Size", name)
		}
		reader, size := input.Reader, input.Size
		return source{name: name, open: func() (*container.Package, error) { return container.OpenReaderAt(reader, size) }}, nil
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
		writer:     entrywriter.New(archive, entrywriter.Config{Compression: options.Compression, CompressionLevel: options.CompressionLevel, Deterministic: options.Deterministic, Limits: options.Limits, OnWarning: options.OnWarning}),
		options:    options,
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
	for _, signature := range state.protectedRoots {
		if !bytes.Equal(rootData, signature.original) {
			_ = archive.Close()
			return fmt.Errorf("%s 的签名 %s 保护了 OFD.xml，合并将修改该文件；preserve 模式无法保留原始签名，请改用 rewrite 或 drop", signature.input, signature.name)
		}
	}
	if err := state.writer.Write(spec.RootDocument, rootData); err != nil {
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
	writer         *entrywriter.Writer
	options        Options
	bodies         []*etree.Element
	attrs          rootAttrs
	nextDir        int
	seenDocIDs     map[string]bool
	protectedRoots []protectedRoot
}

// emit 通过 Options.OnSignature 上报签名处理结果。
func (state *mergeState) emit(event SignatureEvent) {
	if state.options.OnSignature != nil {
		state.options.OnSignature(event)
	}
}

func normalizeOptions(options Options) (Options, error) {
	compression, level, err := entrywriter.NormalizeOptions(options.Compression, options.CompressionLevel, options.Limits)
	if err != nil {
		return Options{}, err
	}
	options.Compression, options.CompressionLevel = compression, level
	switch options.Signatures {
	case "":
		options.Signatures = creator.SignaturePreserve
	case creator.SignaturePreserve, creator.SignatureRewrite, creator.SignatureDrop:
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

	rootData, err := pkg.ReadLimit(spec.RootDocument, state.writer.MaxEntryBytes())
	if err != nil {
		return fmt.Errorf("读取 %s 的 OFD.xml 失败: %w", src.name, err)
	}
	rootDoc, err := xmlcodec.Parse(rootData)
	if err != nil {
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
	for documentIndex, body := range docBodies {
		docRoot := directChildText(body, "DocRoot")
		oldDir, err := normalizeDir(docRoot)
		if err != nil {
			return fmt.Errorf("%s 的 DocRoot %q 无效: %w", src.name, docRoot, err)
		}
		if oldDirs[oldDir] {
			return fmt.Errorf("%s 中多个 DocBody 共用文档目录 %s，无法合并", src.name, oldDir)
		}
		for _, previous := range plans {
			if strings.HasPrefix(oldDir, previous.oldDir+"/") || strings.HasPrefix(previous.oldDir, oldDir+"/") {
				return fmt.Errorf("%s 的文档目录 %s 与 %s 重叠，无法合并", src.name, previous.oldDir, oldDir)
			}
		}
		oldDirs[oldDir] = true

		newDir := fmt.Sprintf("Doc_%d", state.nextDir)
		state.nextDir++
		plan := bodyPlan{oldDir: oldDir, newDir: newDir, documentIndex: documentIndex}
		plan.signatureFiles, err = readSignatureEntries(pkg, body, state.writer.MaxEntryBytes())
		if err != nil {
			return fmt.Errorf("读取 %s 的签名失败: %w", src.name, err)
		}
		plans = append(plans, plan)

		cloned := cloneElement(body)
		if state.options.Signatures == creator.SignatureDrop {
			removeBodySignatures(cloned)
		}
		rewriteBodyPaths(cloned, oldDir, newDir)
		ensureUniqueDocID(cloned, state.seenDocIDs)
		state.bodies = append(state.bodies, cloned)
	}

	if state.options.Signatures == creator.SignaturePreserve {
		if err := validatePreservedSignatures(state, pkg, src.name, plans, rootData); err != nil {
			return err
		}
	}
	if err := copyPackageEntries(state, pkg, src.name, plans); err != nil {
		return err
	}
	return nil
}

// removeBodySignatures 从文档体克隆中删除 Signatures 路径元素。
func removeBodySignatures(body *etree.Element) {
	if child := directChild(body, "Signatures"); child != nil {
		body.RemoveChild(child)
	}
}

// copyPackageEntries 把输入包中除 OFD.xml 外的条目按目录计划搬运到输出包。
func copyPackageEntries(state *mergeState, pkg *container.Package, input string, plans []bodyPlan) error {
	writer := state.writer
	signatures := state.options.Signatures
	for _, entry := range pkg.Entries() {
		if entry.IsDir {
			continue
		}
		name := entry.Path
		if strings.EqualFold(name, spec.RootDocument) {
			continue
		}
		signaturePlan, kind := signaturePlanFor(name, plans)
		if signatures == creator.SignatureDrop {
			if kind != 0 {
				if kind == signatureDocument {
					state.emit(SignatureEvent{Input: input, DocumentIndex: signaturePlan.documentIndex, ID: name, Action: SignatureDropped})
				}
				continue
			}
		}

		plan, ok := matchPlan(name, plans)
		newName := name
		if !ok {
			switch state.options.Orphans {
			case OrphanIgnore:
				if kind != 0 {
					return fmt.Errorf("%s 的签名条目 %s 位于文档目录外，ignore 会使签名引用失效；请改用 OrphanPreserve 或丢弃签名", input, name)
				}
				continue
			case OrphanPreserve:
			default:
				return fmt.Errorf("%s 的条目 %s 不在任何文档目录内；可用 OrphanIgnore 跳过或 OrphanPreserve 保留", input, name)
			}
		} else {
			newName = plan.newDir + strings.TrimPrefix(name, plan.oldDir)
		}

		if kind == signatureList || kind == signatureDocument {
			if entry.UncompressedSize > uint64(writer.MaxEntryBytes()) {
				return fmt.Errorf("签名文件 %s 声明解压后 %d 字节，超过单条上限 %d 字节", name, entry.UncompressedSize, writer.MaxEntryBytes())
			}
			data, err := pkg.ReadLimit(name, writer.MaxEntryBytes())
			if err != nil {
				return fmt.Errorf("读取 %s 的 %s 失败: %w", input, name, err)
			}
			rewritten, changed, err := rewriteSignatureXML(data, signaturePlan.oldDir, signaturePlan.newDir)
			if err != nil {
				return fmt.Errorf("重写 %s 的签名文件 %s 失败: %w", input, name, err)
			}
			action := SignaturePreserved
			if changed {
				if signatures == creator.SignaturePreserve {
					return fmt.Errorf("%s 的 %s 使用包内绝对路径，文档改名后必须重写路径才能保持引用有效；preserve 模式无法保留原始签名，请改用 rewrite 或 drop", input, name)
				}
				writer.Warn(fmt.Sprintf("签名文件 %s 的包内绝对路径已重写为 %s，原签名值失效", name, newName))
				data = rewritten
				action = SignatureRewritten
			}
			if kind == signatureDocument {
				state.emit(SignatureEvent{Input: input, DocumentIndex: signaturePlan.documentIndex, ID: name, Action: action})
			}
			if err := writer.Write(newName, data); err != nil {
				return err
			}
			continue
		}

		if err := writer.WriteSource(pkg, entry, newName); err != nil {
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
	if err := container.ValidateEntryName(dir); err != nil {
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
func rewriteSignatureXML(data []byte, oldDir, newDir string) ([]byte, bool, error) {
	doc, err := xmlcodec.Parse(data)
	if err != nil {
		return nil, false, err
	}
	root := doc.Root()
	if root == nil {
		return nil, false, fmt.Errorf("签名 XML 缺少根元素")
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
		return data, false, nil
	}
	out, err := marshalOFDXML(doc)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
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

// cloneElement 保留扩展命名空间和混合内容，并为 OFD 元素使用统一前缀。
func cloneElement(src *etree.Element) *etree.Element {
	dst := src.Copy()
	for parent := src.Parent(); parent != nil; parent = parent.Parent() {
		for _, attr := range parent.Attr {
			if (attr.Space == "xmlns" || attr.Key == "xmlns") && dst.SelectAttr(attr.FullKey()) == nil {
				dst.CreateAttr(attr.FullKey(), attr.Value)
			}
		}
	}
	prefixOFDXML(dst)
	return dst
}

func prefixOFDXML(root *etree.Element) {
	var elements []*etree.Element
	conflict := false
	legacy := root.Space == "" && root.NamespaceURI() == ""
	walkElements(root, func(element *etree.Element) {
		for _, attr := range element.Attr {
			if attr.Space == "xmlns" && attr.Key == "ofd" && attr.Value != spec.Namespace {
				conflict = true
			}
		}
		if uri := element.NamespaceURI(); uri == spec.Namespace || legacy && uri == "" && element.Space == "" && !hasDefaultNamespaceDeclaration(element) {
			elements = append(elements, element)
		}
	})
	if conflict {
		// 非规范输入的外部 ofd 绑定不能覆盖；由 xmlcodec 按作用域安全归一化。
		return
	}
	root.CreateAttr("xmlns:ofd", spec.Namespace)
	for _, element := range elements {
		element.Space = "ofd"
	}
}

// 仅将完全省略命名空间的旧式 OFD 输入补为 OFD 元素；显式 xmlns="" 是
// 有意义的命名空间重置，不能把其中的扩展元素改为 OFD 元素。
func hasDefaultNamespaceDeclaration(element *etree.Element) bool {
	for parent := element; parent != nil; parent = parent.Parent() {
		for _, attr := range parent.Attr {
			if attr.Space == "" && attr.Key == "xmlns" {
				return true
			}
		}
	}
	return false
}

func buildRootXML(bodies []*etree.Element, attrs rootAttrs) ([]byte, error) {
	w := xmlcodec.New()
	defer xmlcodec.Put(w)
	w.Start("OFD")
	version := attrs.version
	if version == "" {
		version = "1.0"
	}
	docType := attrs.docType
	if docType == "" {
		docType = "OFD"
	}
	w.Attr("Version", version)
	w.Attr("DocType", docType)
	for _, body := range bodies {
		if err := xmlcodec.WriteElement(w, body); err != nil {
			return nil, fmt.Errorf("生成 DocBody XML 失败: %w", err)
		}
	}
	return bytes.Clone(w.Bytes()), nil
}

func readRootAttrs(root *etree.Element) rootAttrs {
	return rootAttrs{
		version: root.SelectAttrValue("Version", ""),
		docType: root.SelectAttrValue("DocType", ""),
		set:     true,
	}
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
