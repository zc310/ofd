// Package split 按页面或完整文档体拆分 OFD，保留原包内路径和页面对象 ID。
package split

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/beevik/etree"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/container/entrywriter"
	"github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

// Limits 限制输入、单份结果和批量拆分规模；零值采用有限默认值。
type Limits struct {
	MaxInputBytes  int64 // 压缩输入，默认 512 MiB。
	MaxEntries     int   // 输入/每份输出的条目数，默认 10000。
	MaxEntryBytes  int64 // 单条解压大小，默认 64 MiB。
	MaxTotalBytes  int64 // 输入/每份输出的解压总大小，默认 512 MiB。
	MaxParts       int   // 分卷数，默认 10000。
	MaxPages       int   // 每份输出页数，默认 100000。
	MaxOutputBytes int64 // 本会话成功及失败写入的累计压缩字节数，默认 2 GiB。
}

// Options 控制拆分输出。页面拆分移除签名、版本、自定义标引和扩展并报告。
type Options struct {
	Compression      creator.CompressionMode
	CompressionLevel int
	Deterministic    bool
	KeepAttachments  bool                  // 页面拆分是否复制文档附件；默认移除并清理附件动作。
	Signatures       creator.SignatureMode // 文档体拆分支持 preserve（默认）和 drop。
	DropIntegrity    bool                  // 显式移除完整性清单；否则发现清单时拒绝拆分。
	Limits           Limits
}

// Source 提供随机访问输入；Path、Data、Reader 必须且只能设置一个。
type Source struct {
	Path   string
	Data   []byte
	Reader io.ReaderAt
	Size   int64 // Reader 的完整字节长度。
}

// DocumentInfo 描述输入文档体，Index 从 1 开始。
type DocumentInfo struct {
	Index int
	ID    string
	Title string
	Pages int
}

// Selection 指定拆分分组；Every、Groups、ByDocument 必须且只能设置一个。
// 页面模式下 Document 从 1 开始，多文档输入必须显式指定。
type Selection struct {
	Document   int
	Every      int
	Groups     [][]int // 每组页码从 1 开始；组内不允许重复，组间可重叠。
	ByDocument bool
}

// Part 是一个分卷计划。Document 和 Pages 均从 1 开始。
type Part struct {
	Document      int
	Pages         []int
	WholeDocument bool
}

// Report 在写出成功后返回，明确列出发生的内容移除和签名处理。
type Report struct {
	Document          int
	Pages             []int
	SourceID          string // 原始文档体 DocID。
	DocID             string
	Dropped           []string
	DroppedSignatures []string // 被移除的签名 XML 路径。
	Signatures        []string // 保留的签名 XML 路径；只表示原始字节保留，不表示验签通过。
	Bytes             int64
}

// Session 持有已打开输入、XML 缓存和批量引用索引。使用后调用 Close。
// 方法可并发调用，内部串行处理；调用方持有的 Reader 不会被关闭。
type Session struct {
	mu        sync.Mutex
	pkg       *container.Package
	resolver  *validator.ReferenceResolver
	options   Options
	root      *etree.Document
	documents []document
	xml       map[string]*etree.Document
	entries   map[string]container.Entry
	written   int64
	parts     int
}

type document struct {
	info DocumentInfo
	path string
	tree *etree.Document
}

// Open 打开路径、完整 []byte 或 Source。索引在有限预算内建立一次。
func Open(ctx context.Context, input any, options Options) (*Session, error) {
	if ctx == nil {
		return nil, invalid("上下文为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	options, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	pkg, err := openSource(input, options.Limits.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	s := &Session{pkg: pkg, options: options, xml: make(map[string]*etree.Document), entries: make(map[string]container.Entry)}
	ok := false
	defer func() {
		if !ok {
			_ = pkg.Close()
		}
	}()
	if crypt.Present(pkg) {
		info, inspectErr := crypt.InspectPackage(ctx, pkg, crypt.InspectOptions{Mode: crypt.ModeCompat})
		return nil, &crypt.LockedError{Info: info, Cause: inspectErr}
	}
	if pkg.Has("OFDEntries.xml") && !options.DropIntegrity {
		return nil, invalid("拆分将改变完整性清单，请显式设置 DropIntegrity 后在输出上重新生成清单")
	}
	seen := make(map[string]bool)
	var total uint64
	entries := pkg.Entries()
	if len(entries) > options.Limits.MaxEntries {
		return nil, invalid("输入条目数超过上限")
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := container.ValidateEntryName(entry.Path); err != nil {
			return nil, err
		}
		lower := strings.ToLower(entry.Path)
		if seen[lower] {
			return nil, invalid("输入路径重复或大小写歧义: %s", entry.Path)
		}
		seen[lower] = true
		if entry.UncompressedSize > uint64(options.Limits.MaxEntryBytes) || entry.UncompressedSize > uint64(options.Limits.MaxTotalBytes)-total {
			return nil, invalid("输入解压大小超过上限")
		}
		total += entry.UncompressedSize
		s.entries[entry.Path] = entry
	}
	s.root, err = s.readXML("OFD.xml", "OFD")
	if err != nil {
		return nil, err
	}
	for index, body := range children(s.root.Root(), "DocBody") {
		name, err := resolve("OFD.xml", text(body, "DocRoot"))
		if err != nil {
			return nil, err
		}
		tree, err := s.readXML(name, "Document")
		if err != nil {
			return nil, err
		}
		pages := children(child(tree.Root(), "Pages"), "Page")
		if len(pages) == 0 || len(pages) > options.Limits.MaxPages {
			return nil, invalid("文档体 %d 的页数无效或超过上限", index+1)
		}
		info := child(body, "DocInfo")
		s.documents = append(s.documents, document{info: DocumentInfo{Index: index + 1, ID: text(info, "DocID"), Title: text(info, "Title"), Pages: len(pages)}, path: name, tree: tree})
	}
	if len(s.documents) == 0 {
		return nil, invalid("输入没有文档体")
	}
	s.resolver, err = validator.NewReferenceResolver(ctx, pkg, validator.WithMaxEntries(options.Limits.MaxEntries), validator.WithMaxFileSize(options.Limits.MaxEntryBytes), validator.WithMaxTotalSize(options.Limits.MaxTotalBytes), validator.WithGMT0099(true))
	if err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}

// Close 释放输入句柄和缓存；可重复调用。
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pkg == nil {
		return nil
	}
	err := s.pkg.Close()
	s.pkg, s.resolver, s.root, s.xml, s.documents, s.entries = nil, nil, nil, nil, nil, nil
	return err
}

// Documents 返回输入文档体信息的独立副本。
func (s *Session) Documents() []DocumentInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]DocumentInfo, len(s.documents))
	for i := range s.documents {
		result[i] = s.documents[i].info
	}
	return result
}

// Plan 校验全部分组后返回计划，不写入文件。
func (s *Session) Plan(selection Selection) ([]Part, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pkg == nil {
		return nil, container.ErrPackageClosed
	}
	modes := 0
	if selection.Every != 0 {
		modes++
	}
	if len(selection.Groups) != 0 {
		modes++
	}
	if selection.ByDocument {
		modes++
	}
	if modes != 1 || selection.Every < 0 {
		return nil, invalid("Every、Groups、ByDocument 必须且只能设置一个，Every 必须为正数")
	}
	if selection.ByDocument {
		if selection.Document != 0 {
			return nil, invalid("按文档体拆分不能同时指定 Document")
		}
		if len(s.documents) > s.options.Limits.MaxParts {
			return nil, invalid("分卷数超过上限")
		}
		parts := make([]Part, len(s.documents))
		for i := range parts {
			parts[i] = Part{Document: i + 1, WholeDocument: true}
		}
		return parts, nil
	}
	doc := selection.Document
	if doc == 0 && len(s.documents) == 1 {
		doc = 1
	}
	if doc < 1 || doc > len(s.documents) {
		return nil, invalid("多文档输入必须指定有效的 Document（从 1 开始）")
	}
	count := s.documents[doc-1].info.Pages
	groups := selection.Groups
	if selection.Every > 0 {
		partCount := 1 + (count-1)/selection.Every
		if partCount > s.options.Limits.MaxParts {
			return nil, invalid("分卷数超过上限")
		}
		groups = make([][]int, 0, partCount)
		for start := 1; start <= count; {
			end := start + min(selection.Every, count-start+1)
			group := make([]int, end-start)
			for i := range group {
				group[i] = start + i
			}
			groups = append(groups, group)
			start = end
		}
	}
	if len(groups) > s.options.Limits.MaxParts {
		return nil, invalid("分卷数超过上限")
	}
	parts := make([]Part, len(groups))
	for i, pages := range groups {
		if err := s.validatePages(doc, pages); err != nil {
			return nil, fmt.Errorf("分卷 %d: %w", i+1, err)
		}
		parts[i] = Part{Document: doc, Pages: append([]int(nil), pages...)}
	}
	return parts, nil
}

func (s *Session) validatePages(doc int, pages []int) error {
	if doc < 1 || doc > len(s.documents) {
		return invalid("文档体索引越界")
	}
	if len(pages) == 0 || len(pages) > s.options.Limits.MaxPages {
		return invalid("分卷页码为空或超过上限")
	}
	seen := make(map[int]bool, len(pages))
	for _, page := range pages {
		if page < 1 || page > s.documents[doc-1].info.Pages || seen[page] {
			return invalid("页码 %d 越界或重复", page)
		}
		seen[page] = true
	}
	return nil
}

// Write 把一个分卷写入 w。任何错误均须丢弃部分输出；成功时才返回完整 Report。
// 保留输入页面 ID 和包内路径；WholeDocument 模式保留原 DocID。
func (s *Session) Write(ctx context.Context, part Part, w io.Writer) (Report, error) {
	if ctx == nil {
		return Report{}, invalid("上下文为空")
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pkg == nil {
		return Report{}, container.ErrPackageClosed
	}
	if w == nil {
		return Report{}, invalid("输出写入器为空")
	}
	if s.parts >= s.options.Limits.MaxParts {
		return Report{}, invalid("累计分卷数超过上限")
	}
	if part.Document < 1 || part.Document > len(s.documents) {
		return Report{}, invalid("文档体索引越界")
	}
	if part.WholeDocument && len(part.Pages) != 0 {
		return Report{}, invalid("完整文档体模式不能指定 Pages")
	}
	if !part.WholeDocument {
		if err := s.validatePages(part.Document, part.Pages); err != nil {
			return Report{}, err
		}
	}
	report, overrides, err := s.prepare(ctx, part)
	if err != nil {
		return Report{}, err
	}
	index, err := s.resolver.Resolve(ctx, overrides)
	if err != nil {
		return Report{}, err
	}
	if !index.Complete() {
		return Report{}, invalid("无法完整确认分卷的文件引用: %s", index.IncompleteReason())
	}
	names := make([]string, 0, len(index.Reachable))
	for name := range index.Reachable {
		names = append(names, name)
	}
	sort.Strings(names)
	budget := &outputWriter{check: ctx.Err, writer: w, remaining: s.options.Limits.MaxOutputBytes - s.written}
	defer func() { s.written += budget.written }()
	archive := zip.NewWriter(budget)
	writer := entrywriter.New(archive, entrywriter.Config{Compression: s.options.Compression, CompressionLevel: s.options.CompressionLevel, Deterministic: s.options.Deterministic, Limits: container.Limits{MaxEntries: s.options.Limits.MaxEntries, MaxEntryBytes: s.options.Limits.MaxEntryBytes, MaxTotalBytes: s.options.Limits.MaxTotalBytes}})
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			_ = archive.Close()
			return Report{}, err
		}
		if data, changed := overrides[name]; changed {
			err = writer.Write(name, data)
		} else {
			err = writer.WriteSource(s.pkg, s.entries[name], name)
		}
		if err != nil {
			_ = archive.Close()
			return Report{}, err
		}
	}
	if err := archive.Close(); err != nil {
		return Report{}, err
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	s.parts++
	report.Bytes = budget.written
	return report, nil
}

type outputWriter struct {
	check              func() error
	writer             io.Writer
	remaining, written int64
}

func (w *outputWriter) Write(data []byte) (int, error) {
	if err := w.check(); err != nil {
		return 0, err
	}
	if int64(len(data)) > w.remaining {
		return 0, invalid("累计输出字节数超过上限")
	}
	n, err := w.writer.Write(data)
	w.remaining -= int64(n)
	w.written += int64(n)
	if err == nil {
		err = w.check()
	}
	return n, err
}

func normalizeOptions(options Options) (Options, error) {
	l := &options.Limits
	if l.MaxInputBytes < 0 || l.MaxEntries < 0 || l.MaxEntryBytes < 0 || l.MaxTotalBytes < 0 || l.MaxParts < 0 || l.MaxPages < 0 || l.MaxOutputBytes < 0 {
		return options, invalid("拆分规模限制不能为负数")
	}
	if l.MaxInputBytes == 0 {
		l.MaxInputBytes = 512 << 20
	}
	if l.MaxEntries == 0 {
		l.MaxEntries = 10000
	}
	if l.MaxEntryBytes == 0 {
		l.MaxEntryBytes = 64 << 20
	}
	if l.MaxTotalBytes == 0 {
		l.MaxTotalBytes = 512 << 20
	}
	if l.MaxParts == 0 {
		l.MaxParts = 10000
	}
	if l.MaxPages == 0 {
		l.MaxPages = 100000
	}
	if l.MaxOutputBytes == 0 {
		l.MaxOutputBytes = 2 << 30
	}
	var err error
	options.Compression, options.CompressionLevel, err = entrywriter.NormalizeOptions(options.Compression, options.CompressionLevel, container.Limits{})
	if err != nil {
		return options, err
	}
	if options.Signatures != "" && options.Signatures != creator.SignaturePreserve && options.Signatures != creator.SignatureDrop {
		return options, invalid("拆分签名策略仅支持 preserve 或 drop")
	}
	return options, nil
}

func openSource(input any, limit int64) (*container.Package, error) {
	source := Source{}
	switch value := input.(type) {
	case string:
		source.Path = value
	case []byte:
		source.Data = value
	case Source:
		source = value
	default:
		return nil, invalid("不支持的拆分输入类型 %T", input)
	}
	configured := 0
	if source.Path != "" {
		configured++
	}
	if len(source.Data) > 0 {
		configured++
	}
	if source.Reader != nil {
		configured++
	}
	if configured != 1 {
		return nil, invalid("Path、Data、Reader 必须且只能设置一个")
	}
	if source.Path != "" {
		info, err := os.Stat(source.Path)
		if err != nil {
			return nil, err
		}
		if info.Size() > limit {
			return nil, invalid("输入字节数超过上限")
		}
		return container.OpenFile(source.Path)
	}
	if len(source.Data) > 0 {
		if int64(len(source.Data)) > limit {
			return nil, invalid("输入字节数超过上限")
		}
		return container.OpenBytes(source.Data)
	}
	if source.Size <= 0 || source.Size > limit {
		return nil, invalid("Reader 大小无效或超过输入上限")
	}
	return container.OpenReaderAt(source.Reader, source.Size)
}

func newDocID(source string, part Part, deterministic bool) (string, error) {
	var id [16]byte
	if deterministic {
		sum := sha256.Sum256([]byte(fmt.Sprintf("ofd-split\x00%s\x00%d\x00%v", source, part.Document, part.Pages)))
		copy(id[:], sum[:16])
	} else if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return hex.EncodeToString(id[:]), nil
}

// ParseGroups 解析 "1-3;4-8;10"，以 maxPages 限制单组展开大小，maxParts 限制分卷数。
func ParseGroups(spec string, maxPages, maxParts int) ([][]int, error) {
	if maxPages <= 0 || maxParts <= 0 {
		return nil, invalid("页数与分卷上限必须为正数")
	}
	if strings.TrimSpace(spec) == "" {
		return nil, invalid("分组表达式为空")
	}
	if strings.Count(spec, ";") >= maxParts {
		return nil, invalid("分卷数超过上限")
	}
	var groups [][]int
	for group := range strings.SplitSeq(spec, ";") {
		pages, err := parseGroup(group, maxPages)
		if err != nil {
			return nil, err
		}
		groups = append(groups, pages)
	}
	return groups, nil
}
