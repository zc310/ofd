package schema

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sync"

	"github.com/knroy/go-xml/xsd"
)

// Files 保存校验器使用的 OFD XSD 集合。文件名和相对路径保持不变，
// 因为这些模式文件通过 xs:include 相互引用。
//
//go:embed xsd/*.xsd
var Files embed.FS

const namespace = "http://www.ofdspec.org/2016"

var roots = map[string]string{
	"OFD":         "OFD.xsd",
	"Document":    "Document.xsd",
	"Page":        "Page.xsd",
	"Res":         "Res.xsd",
	"Signatures":  "Signatures.xsd",
	"Signature":   "Signature.xsd",
	"Annotations": "Annotations.xsd",
	"PageAnnot":   "Annotation.xsd",
	"Attachments": "Attachments.xsd",
	"CustomTags":  "CustomTags.xsd",
	"Extensions":  "Extensions.xsd",
	"DocVersion":  "Version.xsd",
}

// Set 是按根元素索引的已编译模式集合。
type Set struct {
	byRoot map[string]*xsd.Schema
}

var defaultSet struct {
	once sync.Once
	set  *Set
	err  error
}

// Default 返回内置且已编译的 OFD 模式。由于 xsd.Schema 加载后不可变且可安全共享，
// 因此只编译一次。
func Default() (*Set, error) {
	defaultSet.once.Do(func() {
		defaultSet.set, defaultSet.err = load()
	})
	return defaultSet.set, defaultSet.err
}

// Schema 返回指定 OFD XML 根元素对应的模式。
func (s *Set) Schema(root string) (*xsd.Schema, bool) {
	if s == nil {
		return nil, false
	}
	schema, ok := s.byRoot[root]
	return schema, ok
}

func load() (*Set, error) {
	resolver := xsd.NewCatalogResolver()
	filenames := make(map[string]struct{}, len(roots)+1)
	filenames["Definitions.xsd"] = struct{}{}
	for _, filename := range roots {
		filenames[filename] = struct{}{}
	}
	for filename := range filenames {
		source, err := fs.ReadFile(Files, path.Join("xsd", filename))
		if err != nil {
			return nil, fmt.Errorf("读取内置 XSD 模式 %q 失败：%w", filename, err)
		}
		resolver.Add(namespace, source,
			filename,
			"ofd://schema/"+filename,
		)
	}

	set := &Set{byRoot: make(map[string]*xsd.Schema, len(roots))}
	for root, filename := range roots {
		schema, err := xsd.LoadFile(filename, xsd.Options{
			Resolver: resolver,
		})
		if err != nil {
			return nil, fmt.Errorf("编译内置 XSD 模式 %q 失败：%w", filename, err)
		}
		set.byRoot[root] = schema
	}
	return set, nil
}
