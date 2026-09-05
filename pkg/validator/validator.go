package validator

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/knroy/go-xml/xdm"
	"github.com/knroy/go-xml/xsd"
	"github.com/tjfoc/gmsm/sm3"
	"github.com/zc310/ofd/internal/schema"
)

const ofdNamespace = "http://www.ofdspec.org/2016"

// Mode 表示校验严格程度。
type Mode string

const (
	ModeStrict     Mode = "strict"
	ModeCompat     Mode = "compat"
	ModeStructural Mode = "structural"
)

// Options 定义校验器的资源限制和检查开关。
type Options struct {
	Mode          Mode
	MaxErrors     int
	MaxFileSize   int64
	MaxTotalSize  int64
	MaxEntries    int
	MaxXMLBytes   int64
	MaxXMLNodes   int
	MaxXMLDepth   int
	CheckXSD      bool
	ScanXML       bool
	CheckDigest   bool
	FailOnWarning bool
	Schemas       *schema.Set
}

// Option 修改校验器配置。
type Option func(*Options)

// WithMaxErrors 设置最多记录的错误数量；传入 0 表示不限制。
func WithMaxErrors(value int) Option { return func(o *Options) { o.MaxErrors = value } }

// WithMaxFileSize 设置单个 ZIP 条目解压后的大小上限。
func WithMaxFileSize(value int64) Option { return func(o *Options) { o.MaxFileSize = value } }

// WithMaxTotalSize 设置整个 OFD 包解压后的大小上限。
func WithMaxTotalSize(value int64) Option { return func(o *Options) { o.MaxTotalSize = value } }

// WithMaxEntries 设置 ZIP 条目数量上限。
func WithMaxEntries(value int) Option { return func(o *Options) { o.MaxEntries = value } }

// WithMaxXMLBytes 设置单个 XML 文件的大小上限。
func WithMaxXMLBytes(value int64) Option { return func(o *Options) { o.MaxXMLBytes = value } }

// WithMaxXMLNodes 设置单个 XML 文档的节点数量上限。
func WithMaxXMLNodes(value int) Option { return func(o *Options) { o.MaxXMLNodes = value } }

// WithMaxXMLDepth 设置单个 XML 文档的嵌套深度上限。
func WithMaxXMLDepth(value int) Option { return func(o *Options) { o.MaxXMLDepth = value } }

// WithSkipXSD 控制是否跳过 XSD 校验。
func WithSkipXSD(value bool) Option { return func(o *Options) { o.CheckXSD = !value } }

// WithScanXML 控制是否扫描未被主引用链到达的 XML 文件。
func WithScanXML(value bool) Option { return func(o *Options) { o.ScanXML = value } }

// WithCheckDigest 控制是否校验签名摘要。
func WithCheckDigest(value bool) Option { return func(o *Options) { o.CheckDigest = value } }

// WithFailOnWarning 控制是否将警告视为失败。
func WithFailOnWarning(value bool) Option { return func(o *Options) { o.FailOnWarning = value } }

// WithMode 设置校验模式。
func WithMode(value Mode) Option { return func(o *Options) { o.Mode = value } }

// WithSchemas 使用调用方提供的 XSD 模式集合。
func WithSchemas(value *schema.Set) Option {
	return func(o *Options) { o.Schemas = value }
}

// Validator 执行 OFD 容器、XML、XSD、引用和语义校验。
type Validator struct {
	opts Options
}

// New 创建一个使用内置 OFD XSD 的校验器。
func New(options ...Option) (*Validator, error) {
	opts := Options{
		Mode:         ModeStrict,
		MaxErrors:    100,
		MaxFileSize:  64 << 20,
		MaxTotalSize: 512 << 20,
		MaxEntries:   10000,
		MaxXMLBytes:  64 << 20,
		MaxXMLNodes:  2_000_000,
		MaxXMLDepth:  1000,
		CheckXSD:     true,
		ScanXML:      true,
		CheckDigest:  true,
	}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	if opts.MaxErrors < 0 || opts.MaxFileSize < 0 || opts.MaxTotalSize < 0 || opts.MaxEntries < 0 || opts.MaxXMLBytes < 0 || opts.MaxXMLNodes < 0 || opts.MaxXMLDepth < 0 {
		return nil, errors.New("大小、节点、深度和错误数量限制不能为负数")
	}
	if opts.Mode != ModeStrict && opts.Mode != ModeCompat && opts.Mode != ModeStructural {
		return nil, fmt.Errorf("不支持的校验模式 %q", opts.Mode)
	}
	if opts.Mode == ModeStructural {
		opts.CheckXSD = false
	}
	if opts.CheckXSD && opts.Schemas == nil {
		schemas, err := schema.Default()
		if err != nil {
			return nil, err
		}
		opts.Schemas = schemas
	}
	return &Validator{opts: opts}, nil
}

// ValidatePath 校验指定路径的 OFD 文件并返回完整报告。
func (v *Validator) ValidatePath(ctx context.Context, filename string) Report {
	report := newReport(filename)
	start := report.StartedAt
	file, err := os.Open(filename)
	if err != nil {
		report.addIssue(Issue{
			Severity: SeverityError,
			Stage:    StageContainer,
			Code:     "input.open",
			Message:  fmt.Sprintf("无法打开输入文件：%v", err),
		}, v.opts.MaxErrors)
		report.finish(start, false, v.opts.FailOnWarning)
		return report
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr == nil {
		report.Input.Size = info.Size()
	}
	v.validateReader(ctx, file, &report)
	report.finish(start, v.opts.CheckXSD == false || v.opts.Mode == ModeCompat, v.opts.FailOnWarning)
	return report
}

// ValidateReader 校验读取器中的 OFD 数据；name 仅用于报告中的输入名称。
func (v *Validator) ValidateReader(ctx context.Context, reader io.Reader, name string) Report {
	report := newReport(name)
	start := report.StartedAt
	v.validateReader(ctx, reader, &report)
	report.finish(start, v.opts.CheckXSD == false || v.opts.Mode == ModeCompat, v.opts.FailOnWarning)
	return report
}

type packageFile struct {
	name string
	data []byte
}

type packageIndex struct {
	files map[string]packageFile
}

func (p *packageIndex) has(name string) bool {
	_, ok := p.files[name]
	return ok
}

func (p *packageIndex) get(name string) (packageFile, bool) {
	file, ok := p.files[name]
	return file, ok
}

type xmlDocument struct {
	file      packageFile
	tree      *xdm.Tree
	root      *xdm.Node
	rootName  string
	ofd       bool
	validated bool
	baseDir   string
}

type queuedXML struct {
	path     string
	expected string
	from     string
	fromNode *xdm.Node
	baseDir  string
}

type fileReference struct {
	from         string
	base         string
	fallbackBase string
	node         *xdm.Node
	value        string
	expected     string
	checkXML     bool
}

func (v *Validator) validateReader(ctx context.Context, reader io.Reader, report *Report) {
	if ctx == nil {
		ctx = context.Background()
	}
	if reader == nil {
		report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "input.reader_nil", Message: "输入读取器为空"}, v.opts.MaxErrors)
		report.setCheck("zip", "failed")
		return
	}
	data, err := readLimit(reader, v.opts.MaxTotalSize)
	if err != nil {
		report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.read", Message: fmt.Sprintf("读取 ZIP 数据失败：%v", err)}, v.opts.MaxErrors)
		report.setCheck("zip", "failed")
		return
	}
	archive, err := v.indexArchive(data, report)
	if err != nil {
		report.setCheck("zip", "failed")
		return
	}
	report.Input.Size = int64(len(data))
	report.Summary.Files = len(archive.files)
	report.setCheck("zip", "passed")

	_, ok := archive.get("OFD.xml")
	if !ok {
		report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.missing_ofd_xml", File: "OFD.xml", Message: "缺少必需的 OFD.xml 文件"}, v.opts.MaxErrors)
		report.setCheck("zip", "failed")
		return
	}

	documents := make(map[string]*xmlDocument)
	queue := []queuedXML{{path: "OFD.xml", expected: "OFD"}}
	queued := map[string]string{"OFD.xml": "OFD"}
	// 先校验文件引用，再将 XML 引用加入队列，确保扫描到的 XML 也会继续闭包解析。
	processReferences := func(candidates []fileReference) {
		for _, ref := range candidates {
			resolved, resolveErr := resolvePackagePath(ref.base, ref.value)
			if resolveErr == nil && !archive.has(resolved) {
				fallbackResolved := ""
				if ref.fallbackBase != "" {
					fallbackResolved, _ = resolvePackagePath(ref.fallbackBase, ref.value)
				}
				if fallbackResolved != "" && archive.has(fallbackResolved) {
					resolved = fallbackResolved
					resolveErr = nil
				}
			}
			if resolveErr != nil {
				report.addIssue(issueAt(ref.node, SeverityError, StageReference, "reference.path_escape", resolveErr.Error(), ref.from), v.opts.MaxErrors)
				continue
			}
			if !archive.has(resolved) {
				report.addIssue(issueAt(ref.node, SeverityError, StageReference, "reference.missing", fmt.Sprintf("引用的文件不存在：%s", resolved), ref.from), v.opts.MaxErrors)
				continue
			}
			if ref.checkXML && ref.expected != "" {
				if prior, exists := queued[resolved]; exists && prior != ref.expected {
					report.addIssue(issueAt(ref.node, SeverityError, StageReference, "reference.root_conflict", fmt.Sprintf("文件 %s 同时被引用为 %s 和 %s", resolved, prior, ref.expected), ref.from), v.opts.MaxErrors)
					continue
				}
				if _, exists := queued[resolved]; !exists {
					queued[resolved] = ref.expected
					queue = append(queue, queuedXML{
						path:     resolved,
						expected: ref.expected,
						from:     ref.from,
						fromNode: ref.node,
						baseDir:  path.Dir(ref.from),
					})
				}
			}
		}
	}

	processQueue := func() {
		for len(queue) > 0 {
			if err := ctx.Err(); err != nil {
				report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "validation.canceled", Message: fmt.Sprintf("校验已取消：%v", err)}, v.opts.MaxErrors)
				return
			}
			item := queue[0]
			queue = queue[1:]
			if _, done := documents[item.path]; done {
				continue
			}
			file, exists := archive.get(item.path)
			if !exists {
				report.addIssue(Issue{Severity: SeverityError, Stage: StageReference, Code: "reference.missing", File: item.from, Path: nodePath(item.fromNode), Message: fmt.Sprintf("引用的文件不存在：%s", item.path)}, v.opts.MaxErrors)
				continue
			}
			doc, parseRefs, valid := v.parseXML(file, item.expected, item.baseDir, report)
			if doc == nil {
				continue
			}
			documents[item.path] = doc
			if valid {
				processReferences(parseRefs)
			}
		}
	}
	processQueue()

	if v.opts.ScanXML {
		for _, name := range sortedPackageNames(archive.files) {
			file := archive.files[name]
			if path.Ext(name) != ".xml" || name == "OFD.xml" {
				continue
			}
			if _, exists := documents[name]; exists {
				continue
			}
			doc, parseRefs, _ := v.parseXML(file, "", "", report)
			if doc != nil {
				documents[name] = doc
				processReferences(parseRefs)
				processQueue()
			}
		}
	}

	v.semanticChecks(documents, report)
	if v.opts.CheckDigest {
		v.checkDigests(documents, archive, report)
		if report.hasStageErrors(StageDigest) {
			report.setCheck("digest", "failed")
		} else {
			report.setCheck("digest", "passed")
		}
	} else {
		report.setCheck("digest", "skipped")
	}
	if report.hasStageErrors(StageReference) {
		report.setCheck("references", "failed")
	} else {
		report.setCheck("references", "passed")
	}
	if report.hasStageErrors(StageSemantic) {
		report.setCheck("semantic", "failed")
	} else {
		report.setCheck("semantic", "passed")
	}
}

func (v *Validator) indexArchive(data []byte, report *Report) (*packageIndex, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.invalid", Message: fmt.Sprintf("ZIP 容器无效：%v", err)}, v.opts.MaxErrors)
		return nil, err
	}
	if v.opts.MaxEntries > 0 && len(reader.File) > v.opts.MaxEntries {
		err = fmt.Errorf("ZIP 包含 %d 个条目，不能超过 %d 个", len(reader.File), v.opts.MaxEntries)
		report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.too_many_entries", Message: err.Error()}, v.opts.MaxErrors)
		return nil, err
	}
	index := &packageIndex{files: make(map[string]packageFile, len(reader.File))}
	var total uint64
	hadError := false
	for _, entry := range reader.File {
		name, nameErr := cleanEntryName(entry.Name)
		if nameErr != nil {
			hadError = true
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.invalid_path", File: entry.Name, Message: nameErr.Error()}, v.opts.MaxErrors)
			continue
		}
		if _, exists := index.files[name]; exists {
			hadError = true
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.duplicate_entry", File: name, Message: "ZIP 中存在重复条目"}, v.opts.MaxErrors)
			continue
		}
		if entry.FileInfo().IsDir() {
			index.files[name] = packageFile{name: name}
			continue
		}
		if v.opts.MaxFileSize > 0 && entry.UncompressedSize64 > uint64(v.opts.MaxFileSize) {
			hadError = true
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.file_too_large", File: name, Message: fmt.Sprintf("解压后文件大小 %d 字节超过上限 %d 字节", entry.UncompressedSize64, v.opts.MaxFileSize)}, v.opts.MaxErrors)
			continue
		}
		if v.opts.MaxTotalSize > 0 && entry.UncompressedSize64 > uint64(v.opts.MaxTotalSize)-minUint64(total, uint64(v.opts.MaxTotalSize)) {
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.total_too_large", File: name, Message: fmt.Sprintf("ZIP 解压后总大小超过上限 %d 字节", v.opts.MaxTotalSize)}, v.opts.MaxErrors)
			return nil, fmt.Errorf("ZIP 解压后总大小超过上限")
		}
		entryLimit := v.opts.MaxFileSize
		if v.opts.MaxTotalSize > 0 {
			remaining := int64(uint64(v.opts.MaxTotalSize) - minUint64(total, uint64(v.opts.MaxTotalSize)))
			if entryLimit <= 0 || remaining < entryLimit {
				entryLimit = remaining
			}
		}
		fileReader, openErr := entry.Open()
		if openErr != nil {
			hadError = true
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.entry_open", File: name, Message: fmt.Sprintf("打开 ZIP 条目失败：%v", openErr)}, v.opts.MaxErrors)
			continue
		}
		content, readErr := readLimit(fileReader, entryLimit)
		closeErr := fileReader.Close()
		if readErr != nil {
			hadError = true
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.entry_read", File: name, Message: fmt.Sprintf("读取 ZIP 条目失败：%v", readErr)}, v.opts.MaxErrors)
			continue
		}
		if closeErr != nil {
			hadError = true
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.entry_close", File: name, Message: fmt.Sprintf("关闭 ZIP 条目失败：%v", closeErr)}, v.opts.MaxErrors)
			continue
		}
		total += uint64(len(content))
		if v.opts.MaxTotalSize > 0 && total > uint64(v.opts.MaxTotalSize) {
			report.addIssue(Issue{Severity: SeverityError, Stage: StageContainer, Code: "zip.total_too_large", File: name, Message: fmt.Sprintf("ZIP 实际解压后总大小超过上限 %d 字节", v.opts.MaxTotalSize)}, v.opts.MaxErrors)
			return nil, fmt.Errorf("ZIP 实际解压后总大小超过上限")
		}
		index.files[name] = packageFile{name: name, data: content}
	}
	if hadError {
		return nil, fmt.Errorf("ZIP 包含无效条目")
	}
	return index, nil
}

func (v *Validator) parseXML(file packageFile, expected, baseDir string, report *Report) (*xmlDocument, []fileReference, bool) {
	if v.opts.MaxXMLBytes > 0 && int64(len(file.data)) > v.opts.MaxXMLBytes {
		report.addIssue(Issue{Severity: SeverityError, Stage: StageXML, Code: "xml.too_large", File: file.name, Message: fmt.Sprintf("XML 文件大小 %d 字节超过上限 %d 字节", len(file.data), v.opts.MaxXMLBytes)}, v.opts.MaxErrors)
		report.setCheck("xml", "failed")
		return nil, nil, false
	}
	tree, err := xdm.Parse(bytes.NewReader(file.data), xdm.ParseOptions{
		TrackPositions: true,
		MaxBytes:       v.opts.MaxXMLBytes,
		MaxNodes:       v.opts.MaxXMLNodes,
		MaxDepth:       v.opts.MaxXMLDepth,
	})
	if err != nil {
		report.addIssue(Issue{Severity: SeverityError, Stage: StageXML, Code: "xml.malformed", File: file.name, Message: fmt.Sprintf("XML 解析失败：%v", err)}, v.opts.MaxErrors)
		report.setCheck("xml", "failed")
		return nil, nil, false
	}
	rootElements := tree.Root.ChildElements()
	if len(rootElements) != 1 {
		report.addIssue(Issue{Severity: SeverityError, Stage: StageXML, Code: "xml.root_count", File: file.name, Message: "XML 文档必须且只能包含一个根元素"}, v.opts.MaxErrors)
		report.setCheck("xml", "failed")
		return nil, nil, false
	}
	root := rootElements[0]
	rootIsOFD := root.Name.URI == ofdNamespace
	if !rootIsOFD && expected != "" {
		report.addIssue(issueAt(root, SeverityError, StageXML, "namespace.invalid", fmt.Sprintf("OFD XML 的根命名空间必须为 %s", ofdNamespace), file.name), v.opts.MaxErrors)
		report.setCheck("xml", "failed")
		return nil, nil, false
	}
	if expected != "" && root.Name.Local != expected {
		report.addIssue(issueAt(root, SeverityError, StageReference, "reference.root_mismatch", fmt.Sprintf("期望根元素 %s，实际为 %s", expected, root.Name.Local), file.name), v.opts.MaxErrors)
		report.setCheck("xml", "failed")
		return nil, nil, false
	}
	doc := &xmlDocument{file: file, tree: tree, root: root, rootName: root.Name.Local, ofd: rootIsOFD, baseDir: baseDir}
	var refs []fileReference
	if rootIsOFD {
		refs = collectReferences(doc)
	}
	if v.opts.CheckXSD && rootIsOFD {
		schema, ok := v.opts.Schemas.Schema(root.Name.Local)
		if !ok {
			report.addIssue(issueAt(root, v.xsdSeverity(), StageXSD, "xsd.schema_missing", "没有为此根元素注册内置 XSD 模式", file.name), v.opts.MaxErrors)
			if v.opts.Mode == ModeCompat {
				report.setCheck("xsd", "warning")
			} else {
				report.setCheck("xsd", "failed")
			}
		} else if err := schema.Validate(tree.Root, xsd.ValidateOptions{MaxErrors: xsdMaxErrors(v.opts.MaxErrors)}); err != nil {
			var validationErrors *xsd.ValidationErrors
			if errors.As(err, &validationErrors) {
				for _, validationErr := range validationErrors.Errors {
					report.addIssue(Issue{
						Severity:   v.xsdSeverity(),
						Stage:      StageXSD,
						Code:       "xsd.validation",
						EngineCode: validationErr.Code,
						Message:    localizeXSDMessage(validationErr.Message),
						File:       file.name,
						Path:       validationErr.Path,
						Line:       validationErr.Line,
						Column:     validationErr.Column,
					}, v.opts.MaxErrors)
				}
			} else {
				report.addIssue(Issue{Severity: v.xsdSeverity(), Stage: StageXSD, Code: "xsd.validation", File: file.name, Message: fmt.Sprintf("XSD 校验失败：%v", err)}, v.opts.MaxErrors)
			}
			if v.opts.Mode == ModeCompat {
				report.setCheck("xsd", "warning")
			} else {
				report.setCheck("xsd", "failed")
			}
		} else {
			doc.validated = true
			report.setCheck("xsd", "passed")
		}
	} else if !v.opts.CheckXSD || !rootIsOFD {
		report.setCheck("xsd", "skipped")
	}
	report.setCheck("xml", "passed")
	return doc, refs, true
}

func (v *Validator) xsdSeverity() Severity {
	if v.opts.Mode == ModeCompat {
		return SeverityWarning
	}
	return SeverityError
}

// localizeXSDMessage 将常见的 XSD 引擎消息转换为中文，同时保留元素、属性和
// 期望值等技术细节，便于用户直接定位问题。
func localizeXSDMessage(message string) string {
	message = strings.TrimSpace(message)
	if parts := strings.SplitN(message, "; expected ", 2); len(parts) == 2 && strings.Contains(parts[0], "is not permitted here") {
		return localizeXSDMessage(parts[0]) + "；期望 " + localizeXSDExpected(parts[1])
	}
	translations := []struct{ from, to string }{
		{"no global declaration for element ", "没有全局元素声明："},
		{"no global declaration for attribute ", "没有全局属性声明："},
		{"no element declaration for ", "没有元素声明："},
		{"required attribute ", "缺少必需属性 "},
		{" is missing", ""},
		{"element ", "元素 "},
		{"attribute ", "属性 "},
		{" is not permitted here", " 不允许出现在当前位置"},
		{" is not permitted", " 不允许出现"},
		{" is not a valid ", " 不是有效的 "},
		{" is not a valid xs:ID", " 不是有效的 xs:ID"},
		{" does not match any member type of the union", " 不匹配联合类型的任何成员类型"},
		{" is not less than ", " 不小于 "},
		{" is not greater than ", " 不大于 "},
		{"enumeration facet of an anonymous type: ", "匿名类型的枚举值限制："},
		{"enumeration facet", "枚举值限制"},
		{" is not one of the permitted values", " 不在允许的枚举值中"},
		{"of an anonymous type: ", "匿名类型："},
		{"unsupported digest method", "不支持的摘要算法"},
	}
	for _, translation := range translations {
		message = strings.Replace(message, translation.from, translation.to, 1)
	}
	if strings.Contains(message, "cvc-") {
		return "XSD 数据类型或属性校验失败：" + message
	}
	if strings.Contains(message, "不允许") || strings.Contains(message, "缺少必需属性") || strings.Contains(message, "不是有效") || strings.Contains(message, "不支持") {
		return message
	}
	return "XSD 校验失败：" + message
}

func localizeXSDExpected(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "one of ")
	value = strings.ReplaceAll(value, "element ", "元素 ")
	value = strings.ReplaceAll(value, "attribute ", "属性 ")
	value = strings.ReplaceAll(value, " or ", " 或 ")
	return value
}

func collectReferences(doc *xmlDocument) []fileReference {
	var refs []fileReference
	addWithBase := func(node *xdm.Node, base, value, expected string, checkXML bool) {
		value = strings.TrimSpace(value)
		if value != "" {
			refs = append(refs, fileReference{from: doc.file.name, base: base, node: node, value: value, expected: expected, checkXML: checkXML})
		}
	}
	add := func(node *xdm.Node, value, expected string, checkXML bool) {
		addWithBase(node, doc.file.name, value, expected, checkXML)
	}
	walkOFDElements(doc.root, func(node *xdm.Node) {
		local := node.Name.Local
		parent := ""
		if node.Parent != nil && node.Parent.Kind == xdm.KindElement {
			parent = node.Parent.Name.Local
		}
		switch doc.rootName {
		case "OFD":
			switch local {
			case "DocRoot":
				add(node, node.StringValue(), "Document", true)
			case "Signatures":
				add(node, node.StringValue(), "Signatures", true)
			case "Cover":
				add(node, node.StringValue(), "", false)
			case "Version":
				if value := node.AttrValue("BaseLoc"); value != "" {
					addWithBase(node, doc.file.name, value, "DocVersion", true)
				}
			}
		case "Document":
			switch local {
			case "PublicRes", "DocumentRes":
				add(node, node.StringValue(), "Res", true)
			case "Annotations":
				add(node, node.StringValue(), "Annotations", true)
			case "CustomTags":
				add(node, node.StringValue(), "CustomTags", true)
			case "Attachments":
				add(node, node.StringValue(), "Attachments", true)
			case "Extensions":
				add(node, node.StringValue(), "Extensions", true)
			case "Page", "TemplatePage":
				if value := node.AttrValue("BaseLoc"); value != "" {
					addWithBase(node, doc.file.name, value, "Page", true)
				}
			}
		case "Page":
			if local == "PageRes" {
				add(node, node.StringValue(), "Res", true)
			}
		case "Annotations":
			if local == "FileLoc" {
				add(node, node.StringValue(), "PageAnnot", true)
			}
		case "Attachments":
			if local == "FileLoc" {
				value := strings.TrimSpace(node.StringValue())
				if value != "" {
					fallbackBase := ""
					if doc.baseDir != "" {
						// Some producers write FileLoc relative to the document
						// directory rather than the Attachments.xml directory.
						fallbackBase = path.Join(doc.baseDir, "_document")
					}
					refs = append(refs, fileReference{
						from:         doc.file.name,
						base:         doc.file.name,
						fallbackBase: fallbackBase,
						node:         node,
						value:        value,
					})
				}
			}
		case "CustomTags":
			if local == "SchemaLoc" || local == "FileLoc" {
				add(node, node.StringValue(), "", false)
			}
		case "Extensions":
			if local == "ExtendData" {
				add(node, node.StringValue(), "", false)
			}
		case "Res":
			switch local {
			case "FontFile", "MediaFile":
				base := doc.file.name
				if rootBase := strings.TrimSpace(doc.root.AttrValue("BaseLoc")); rootBase != "" {
					// resolvePackagePath treats base as a file path, so use a
					// sentinel file to resolve relative to the resource directory.
					base = path.Join(path.Dir(doc.file.name), rootBase, "_resource")
				}
				addWithBase(node, base, node.StringValue(), "", false)
			case "ColorSpace":
				base := doc.file.name
				if rootBase := strings.TrimSpace(doc.root.AttrValue("BaseLoc")); rootBase != "" {
					base = path.Join(path.Dir(doc.file.name), rootBase, "_resource")
				}
				addWithBase(node, base, node.AttrValue("Profile"), "", false)
			}
		case "Signatures":
			if local == "Signature" {
				if value := node.AttrValue("BaseLoc"); value != "" {
					addWithBase(node, doc.file.name, value, "Signature", true)
				}
			}
		case "Signature":
			switch local {
			case "SignedValue", "BaseLoc":
				if local == "SignedValue" || parent == "Seal" {
					add(node, node.StringValue(), "", false)
				}
			case "Reference":
				addWithBase(node, doc.file.name, node.AttrValue("FileRef"), "", false)
			}
		case "Version":
			switch local {
			case "File":
				add(node, node.StringValue(), "", false)
			case "DocRoot":
				add(node, node.StringValue(), "Document", true)
			}
		}
	})
	return refs
}

type idDeclaration struct {
	value string
	scope string
	file  string
}

type idIndex struct {
	all     map[string][]idDeclaration
	byScope map[string]map[string][]idDeclaration
}

func newIDIndex() *idIndex {
	return &idIndex{
		all:     make(map[string][]idDeclaration),
		byScope: make(map[string]map[string][]idDeclaration),
	}
}

func (index *idIndex) add(declaration idDeclaration) {
	index.all[declaration.value] = append(index.all[declaration.value], declaration)
	values := index.byScope[declaration.scope]
	if values == nil {
		values = make(map[string][]idDeclaration)
		index.byScope[declaration.scope] = values
	}
	values[declaration.value] = append(values[declaration.value], declaration)
}

func (index *idIndex) has(scope, value string) bool {
	if scope == "" {
		return len(index.all[value]) > 0
	}
	if values := index.byScope[scope]; values != nil {
		return len(values[value]) > 0
	}
	return false
}

func (index *idIndex) hasAny(scopes []string, value string) bool {
	for _, scope := range scopes {
		if index.has(scope, value) {
			return true
		}
	}
	return false
}

func (v *Validator) semanticChecks(documents map[string]*xmlDocument, report *Report) {
	knownIDs := newIDIndex()
	docIDs := make(map[string]string)
	for _, name := range sortedDocumentNames(documents) {
		doc := documents[name]
		if !doc.ofd {
			continue
		}
		localIDs := make(map[string]map[string]struct{})
		walkOFDElements(doc.root, func(node *xdm.Node) {
			if scope, declared := declaredIDScope(doc, node); declared {
				value := strings.TrimSpace(node.AttrValue("ID"))
				if value != "" {
					values := localIDs[scope]
					if values == nil {
						values = make(map[string]struct{})
						localIDs[scope] = values
					}
					if _, duplicate := values[value]; duplicate {
						report.addIssue(issueAt(node, SeverityError, StageSemantic, "semantic.duplicate_id", fmt.Sprintf("ID %s 在同一 %s 范围内重复", value, idScopeLabel(scope)), name), v.opts.MaxErrors)
					}
					values[value] = struct{}{}
					knownIDs.add(idDeclaration{value: value, scope: scope, file: name})
				}
			}
			if node.Name.Local == "DocID" && doc.rootName == "OFD" {
				value := strings.TrimSpace(node.StringValue())
				if previous, exists := docIDs[value]; exists && value != "" {
					report.addIssue(issueAt(node, SeverityError, StageSemantic, "semantic.duplicate_doc_id", fmt.Sprintf("DocID 与 %s 重复", previous), name), v.opts.MaxErrors)
				} else if value != "" {
					docIDs[value] = name
				}
			}
		})
	}

	for _, name := range sortedDocumentNames(documents) {
		doc := documents[name]
		if !doc.ofd {
			continue
		}
		walkOFDElements(doc.root, func(node *xdm.Node) {
			for _, reference := range semanticIDReferences(node) {
				if !knownIDs.hasAny(reference.scopes, reference.value) {
					report.addIssue(issueAt(node, SeverityError, StageSemantic, "semantic.unresolved_id", fmt.Sprintf("%s %s 未在 OFD 包中找到对应的 ID", reference.name, reference.value), doc.file.name), v.opts.MaxErrors)
				}
			}
		})
	}
}

type semanticIDReference struct {
	name   string
	value  string
	scopes []string
}

func semanticIDReferences(node *xdm.Node) []semanticIDReference {
	var references []semanticIDReference
	for _, name := range []string{"TemplateID", "ResourceID", "Substitution", "ImageMask", "ColorSpace", "DrawParam", "Relative", "Thumbnail", "PageRef", "PageID", "AttachID", "Font", "RefId", "RefID"} {
		value := strings.TrimSpace(node.AttrValue(name))
		if value == "" {
			continue
		}
		scopes := semanticIDScopesForNode(node, name)
		if len(scopes) > 0 {
			references = append(references, semanticIDReference{name: name, value: value, scopes: scopes})
		}
	}
	switch node.Name.Local {
	case "DefaultCS":
		if value := strings.TrimSpace(node.StringValue()); value != "" {
			references = append(references, semanticIDReference{name: "DefaultCS", value: value, scopes: []string{"color-space"}})
		}
	case "Thumbnail", "Substitution":
		if value := strings.TrimSpace(node.StringValue()); value != "" {
			references = append(references, semanticIDReference{name: node.Name.Local, value: value, scopes: []string{"media", "composite"}})
		}
	}
	return references
}

func semanticIDScopes(name string) []string {
	switch name {
	case "TemplateID":
		return []string{"template"}
	case "Substitution", "ImageMask", "Thumbnail":
		return []string{"media", "composite"}
	case "ColorSpace":
		return []string{"color-space"}
	case "DrawParam", "Relative":
		return []string{"draw-param"}
	case "PageRef", "PageID":
		return []string{"page"}
	case "AttachID":
		return []string{"attachment"}
	case "Font":
		return []string{"font"}
	case "RefId", "RefID":
		// Extensions can target any OFD object, so use the package-wide ID scope.
		return []string{""}
	default:
		return nil
	}
}

func semanticIDScopesForNode(node *xdm.Node, name string) []string {
	if name != "ResourceID" {
		return semanticIDScopes(name)
	}
	if node == nil {
		return []string{"media", "composite"}
	}
	switch node.Name.Local {
	case "CompositeObject":
		return []string{"composite"}
	case "ImageObject":
		return []string{"media"}
	default:
		return []string{"media", "composite"}
	}
}

func declaredIDScope(doc *xmlDocument, node *xdm.Node) (string, bool) {
	if node == nil || node.AttrValue("ID") == "" {
		return "", false
	}
	parent := elementParentName(node)
	switch doc.rootName {
	case "OFD":
		if node.Name.Local == "Version" && parent == "Versions" {
			return "version", true
		}
	case "Document":
		switch {
		case node.Name.Local == "Page" && parent == "Pages":
			return "page", true
		case node.Name.Local == "TemplatePage" && parent == "CommonData":
			return "template", true
		}
	case "Page":
		switch node.Name.Local {
		case "Layer", "TextObject", "PathObject", "ImageObject", "CompositeObject", "PageBlock":
			return "page-unit", true
		}
	case "Res":
		switch node.Name.Local {
		case "ColorSpace":
			return "color-space", true
		case "DrawParam":
			return "draw-param", true
		case "Font":
			return "font", true
		case "MultiMedia":
			return "media", true
		case "CompositeGraphicUnit":
			return "composite", true
		}
	case "Signatures":
		if node.Name.Local == "Signature" {
			return "signature", true
		}
	case "Signature":
		if node.Name.Local == "StampAnnot" {
			return "stamp", true
		}
	case "PageAnnot":
		if node.Name.Local == "Annot" {
			return "annotation", true
		}
	case "Attachments":
		if node.Name.Local == "Attachment" {
			return "attachment", true
		}
	case "DocVersion":
		if node.Name.Local == "DocVersion" || node.Name.Local == "File" {
			return "version", true
		}
	}
	return "", false
}

func elementParentName(node *xdm.Node) string {
	if node != nil && node.Parent != nil && node.Parent.Kind == xdm.KindElement {
		return node.Parent.Name.Local
	}
	return ""
}

func idScopeLabel(scope string) string {
	switch scope {
	case "page", "page-unit":
		return "页面"
	case "template":
		return "模板页"
	case "resource", "color-space", "draw-param", "font", "media", "composite":
		return "资源"
	case "attachment":
		return "附件"
	case "annotation":
		return "注释"
	case "signature", "stamp":
		return "签名"
	case "version":
		return "版本"
	default:
		return scope
	}
}

func (v *Validator) checkDigests(documents map[string]*xmlDocument, archive *packageIndex, report *Report) {
	for _, name := range sortedDocumentNames(documents) {
		doc := documents[name]
		if !doc.ofd || doc.rootName != "Signature" {
			continue
		}
		method := "MD5"
		for _, node := range descendants(doc.root, "References") {
			if value := node.AttrValue("CheckMethod"); value != "" {
				method = strings.ToUpper(strings.TrimSpace(value))
			}
		}
		for _, reference := range descendants(doc.root, "Reference") {
			fileRef := reference.AttrValue("FileRef")
			checkValueNode := firstChild(reference, "CheckValue")
			if checkValueNode == nil {
				continue
			}
			resolved, err := resolvePackagePath(doc.file.name, fileRef)
			if err != nil {
				report.addIssue(issueAt(reference, SeverityError, StageDigest, "digest.path_invalid", err.Error(), doc.file.name), v.opts.MaxErrors)
				continue
			}
			target, ok := archive.get(resolved)
			if !ok {
				continue
			}
			hashFunc, ok := digestHash(method)
			if !ok {
				report.addIssue(issueAt(reference, SeverityError, StageDigest, "digest.method_unsupported", fmt.Sprintf("不支持的摘要算法 %s", method), doc.file.name), v.opts.MaxErrors)
				continue
			}
			digest := hashFunc()
			_, _ = digest.Write(target.data)
			actual := digest.Sum(nil)
			encoded := strings.Join(strings.Fields(checkValueNode.StringValue()), "")
			expected, decodeErr := base64.StdEncoding.DecodeString(encoded)
			if decodeErr != nil || !bytes.Equal(actual, expected) {
				report.addIssue(issueAt(reference, SeverityError, StageDigest, "digest.mismatch", fmt.Sprintf("文件 %s 的摘要不匹配", resolved), doc.file.name), v.opts.MaxErrors)
			}
		}
	}
}

func digestHash(method string) (func() hash.Hash, bool) {
	switch strings.ToUpper(method) {
	case "MD5":
		return md5.New, true
	case "SHA1":
		return sha1.New, true
	case "SM3", "1.2.156.10197.1.401":
		return sm3.New, true
	default:
		return nil, false
	}
}

func xsdMaxErrors(maxErrors int) int {
	if maxErrors > 0 {
		return maxErrors
	}
	return int(^uint(0) >> 1)
}

func sortedPackageNames(files map[string]packageFile) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedDocumentNames(documents map[string]*xmlDocument) []string {
	names := make([]string, 0, len(documents))
	for name := range documents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func minUint64(left, right uint64) uint64 {
	if left < right {
		return left
	}
	return right
}

func readLimit(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(reader)
	}
	readSize := limit
	if limit < int64(^uint64(0)>>1) {
		readSize++
	}
	data, err := io.ReadAll(io.LimitReader(reader, readSize))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("输入数据超过大小上限 %d 字节", limit)
	}
	return data, nil
}

func cleanEntryName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("ZIP 路径为空或包含 NUL 字符")
	}
	if strings.Contains(name, "\\") {
		return "", fmt.Errorf("ZIP 路径必须使用 '/' 分隔符：%s", name)
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." {
			return "", fmt.Errorf("ZIP 路径越过包根目录：%s", name)
		}
	}
	cleaned := path.Clean(name)
	if strings.HasPrefix(name, "/") || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("ZIP 路径越过包根目录：%s", name)
	}
	return strings.TrimSuffix(cleaned, "/"), nil
}

func resolvePackagePath(from, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("包内路径为空")
	}
	if strings.ContainsRune(value, '\x00') || strings.Contains(value, "\\") {
		return "", fmt.Errorf("包内路径无效：%q", value)
	}
	if strings.Contains(value, "://") {
		return "", fmt.Errorf("OFD 包内路径不允许使用外部 URI：%q", value)
	}
	absolute := strings.HasPrefix(value, "/")
	value = strings.TrimPrefix(value, "/")
	resolved := path.Clean(path.Join(path.Dir(from), value))
	if absolute {
		resolved = path.Clean(value)
	}
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") || strings.HasPrefix(resolved, "/") {
		return "", fmt.Errorf("包内路径越过包根目录：%q", value)
	}
	return resolved, nil
}

func walkElements(node *xdm.Node, visit func(*xdm.Node)) {
	if node == nil {
		return
	}
	if node.Kind == xdm.KindElement {
		visit(node)
	}
	for _, child := range node.Children {
		walkElements(child, visit)
	}
}

func walkOFDElements(node *xdm.Node, visit func(*xdm.Node)) {
	if node == nil {
		return
	}
	if node.Kind == xdm.KindElement {
		if node.Name.URI != ofdNamespace {
			return
		}
		visit(node)
	}
	for _, child := range node.Children {
		walkOFDElements(child, visit)
	}
}

func descendants(node *xdm.Node, local string) []*xdm.Node {
	var nodes []*xdm.Node
	walkOFDElements(node, func(candidate *xdm.Node) {
		if candidate.Name.Local == local {
			nodes = append(nodes, candidate)
		}
	})
	return nodes
}

func firstChild(node *xdm.Node, local string) *xdm.Node {
	for _, child := range node.ChildElements() {
		if child.Name.URI == ofdNamespace && child.Name.Local == local {
			return child
		}
	}
	return nil
}

func nodePath(node *xdm.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == xdm.KindAttribute {
		return nodePath(node.Parent) + "/@" + node.Name.Local
	}
	var parts []string
	for current := node; current != nil && current.Kind != xdm.KindDocument; current = current.Parent {
		index := 1
		if current.Parent != nil {
			for _, sibling := range current.Parent.ChildElements() {
				if sibling == current {
					break
				}
				if sibling.Name.Local == current.Name.Local {
					index++
				}
			}
		}
		parts = append(parts, fmt.Sprintf("%s[%d]", current.Name.Local, index))
	}
	var builder strings.Builder
	for i := len(parts) - 1; i >= 0; i-- {
		builder.WriteByte('/')
		builder.WriteString(parts[i])
	}
	return builder.String()
}

func issueAt(node *xdm.Node, severity Severity, stage Stage, code, message, file string) Issue {
	issue := Issue{Severity: severity, Stage: stage, Code: code, Message: message, File: file, Path: nodePath(node)}
	if node != nil {
		issue.Line, issue.Column, _ = node.Position()
	}
	return issue
}
