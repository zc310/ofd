package compare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image/color"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/goccy/go-json/v2"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
)

type snapshot struct {
	input     Input
	ofd       *parser.OFD
	files     map[string]*File
	pages     []pageRef
	renderers map[int]*render.Document
	cache     *pageCache
}

type pageRef struct {
	doc  int
	page *parser.Page
}

func (s *snapshot) close() {
	for _, doc := range s.renderers {
		doc.Close()
	}
	if s.ofd != nil {
		_ = s.ofd.Close()
	}
}

func openSnapshot(ctx context.Context, filename string, options Options) (_ *snapshot, err error) {
	ranges, err := ParsePageRanges(options.Pages)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("输入不是普通文件: %s", filename)
	}
	ofd, err := parser.NewOFD(filename)
	if err != nil {
		return nil, err
	}
	s := &snapshot{ofd: ofd, files: map[string]*File{}, renderers: map[int]*render.Document{}, input: Input{Name: filename, Size: info.Size(), Documents: len(ofd.Documents), Files: []File{}, Directories: []string{}, Pages: []Page{}}}
	defer func() {
		if err != nil {
			s.close()
		}
	}()
	if err := reserveData(options, filename); err != nil {
		return nil, err
	}
	if len(ofd.Documents) == 0 {
		return nil, fmt.Errorf("OFD 没有文档体")
	}
	pkg := ofd.Documents[0].FileCache
	dirs := map[string]bool{}
	entries := pkg.Entries()
	if len(entries) > 100000 {
		return nil, fmt.Errorf("包内条目超过 100000 个")
	}
	var totalBytes uint64
	for _, entry := range entries {
		if entry.UncompressedSize > 1<<30-totalBytes {
			return nil, fmt.Errorf("包内累计解压大小超过 1 GiB")
		}
		totalBytes += entry.UncompressedSize
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(entry.Path, "/")
		if entry.IsDir {
			dirs[name] = true
			continue
		}
		if _, exists := s.files[name]; exists {
			return nil, fmt.Errorf("包内文件路径重复: %s", name)
		}
		for dir := path.Dir(name); dir != "." && dir != "/"; dir = path.Dir(dir) {
			dirs[dir] = true
		}
		f, err := inspectEntry(ctx, pkg, entry, options)
		if err != nil {
			return nil, fmt.Errorf("读取 %s: %w", name, err)
		}
		if err := reserveData(options, f); err != nil {
			return nil, err
		}
		s.input.Files = append(s.input.Files, f)
		// 建表阶段只记路径，避免指针使切片扩容前的旧数组一直驻留。
		s.files[name] = nil
	}
	sort.Slice(s.input.Files, func(i, j int) bool { return s.input.Files[i].Path < s.input.Files[j].Path })
	// 排序和切片扩容后重新建立指针索引。
	for i := range s.input.Files {
		s.files[s.input.Files[i].Path] = &s.input.Files[i]
	}
	for dir := range dirs {
		if err := reserveData(options, dir); err != nil {
			return nil, err
		}
		s.input.Directories = append(s.input.Directories, dir)
	}
	sort.Strings(s.input.Directories)
	for di, doc := range ofd.Documents {
		meta := ofd.DocBodies[di].DocInfo
		value := func(s *string) string {
			if s == nil {
				return ""
			}
			return *s
		}
		s.input.Metadata = append(s.input.Metadata, DocumentInfo{Number: di + 1, ID: meta.DocID, Title: value(meta.Title), Author: value(meta.Author), Subject: value(meta.Subject), Creator: value(meta.Creator)})
		if err := reserveData(options, s.input.Metadata[len(s.input.Metadata)-1]); err != nil {
			return nil, err
		}
		rd := render.NewDocument(color.White, doc)
		s.renderers[di] = rd
		for pi, page := range doc.Pages {
			if len(s.pages) >= 10000 {
				return nil, fmt.Errorf("页面数量超过 10000 页")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			number := len(s.pages) + 1
			if !pageSelected(ranges, number) {
				s.input.Pages = append(s.input.Pages, Page{Number: number, Document: di + 1, PageInDocument: pi + 1})
				if err := reserveData(options, s.input.Pages[len(s.input.Pages)-1]); err != nil {
					return nil, err
				}
				s.pages = append(s.pages, pageRef{doc: di, page: page})
				continue
			}
			lease, err := page.AcquireLease()
			if err != nil {
				return nil, fmt.Errorf("第 %d 文档体第 %d 页: %w", di+1, pi+1, err)
			}
			content := lease.Content()
			encoded, marshalErr := json.Marshal(content, json.Deterministic(true))
			lease.Release()
			if marshalErr != nil {
				return nil, marshalErr
			}
			box, err := page.PhysicalBox()
			if err != nil {
				return nil, err
			}
			s.input.Pages = append(s.input.Pages, Page{Number: number, Document: di + 1, PageInDocument: pi + 1, Width: box.Width, Height: box.Height, Text: strings.Join(rd.TextValues(page), "\n"), ContentSHA256: digest(encoded), Selected: true})
			if err := reserveData(options, s.input.Pages[len(s.input.Pages)-1]); err != nil {
				return nil, err
			}
			s.pages = append(s.pages, pageRef{doc: di, page: page})
		}
	}
	if len(ranges) > 0 && len(selectedPages(s.input.Pages, options.Pages)) == 0 {
		return nil, fmt.Errorf("指定页范围 %q 在文件 %q 中未选中任何页面", options.Pages, filename)
	}
	return s, ctx.Err()
}

type contextReader struct {
	io.Reader
	err func() error
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func inspectEntry(ctx context.Context, pkg *container.Package, entry container.Entry, options Options) (File, error) {
	f := File{Path: entry.Path, Size: entry.UncompressedSize, Kind: fileKind(entry.Path)}
	r, err := pkg.OpenEntry(entry)
	if err != nil {
		return f, err
	}
	defer r.Close()
	h := sha256.New()
	isXML := strings.EqualFold(path.Ext(f.Path), ".xml")
	if _, err := io.Copy(h, contextReader{Reader: r, err: ctx.Err}); err != nil {
		return f, err
	}
	f.SHA256 = hex.EncodeToString(h.Sum(nil))
	if !isXML {
		return f, nil
	}
	if entry.UncompressedSize > uint64(options.MaxXMLBytes) {
		f.XMLNote = "XML 超过结构分析大小限制，仅比较文件摘要"
		return f, nil
	}
	return f, nil
}

// 只在文件字节发生变化时分析 XML；字段表由当前文件比较持有，不跨文件缓存。
func (s *snapshot) xmlFields(ctx context.Context, f *File, options Options) (map[string]string, []byte, error) {
	if f.Kind != "xml" || f.Size > uint64(options.MaxXMLBytes) {
		return nil, nil, nil
	}
	data, err := s.ofd.Documents[0].FileCache.ReadLimit(f.Path, options.MaxXMLBytes)
	if err != nil {
		return nil, nil, err
	}
	fields, err := xmlFieldsLimit(ctx, data, options.MaxXMLStructureBytes)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		f.XMLNote = "XML 结构分析失败: " + err.Error()
		return nil, nil, nil
	}
	encoded, _ := json.Marshal(fields, json.Deterministic(true))
	f.XMLSHA256 = digest(encoded)
	if len(data) > maxXMLViewSourceBytes {
		data = nil
	}
	return fields, data, nil
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func fileKind(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".bmp", ".tif", ".tiff", ".gif", ".webp", ".jp2", ".jbig2", ".svg":
		return "image"
	case ".ttf", ".otf", ".ttc", ".cff", ".woff", ".woff2":
		return "font"
	case ".xml":
		return "xml"
	default:
		return "binary"
	}
}
