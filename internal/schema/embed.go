// Package schema 提供内置 OFD XSD 模式的加载和查询能力。
package schema

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sync"

	"github.com/knroy/go-xml/xsd"
	"github.com/zc310/ofd/pkg/spec"
)

// Files 保存校验器使用的 OFD XSD 集合。文件名和相对路径保持不变，
// 因为这些模式文件通过 xs:include 相互引用。
//
//go:embed xsd/*.xsd
var Files embed.FS

const namespace = spec.Namespace

// ofdRoot 是 OFD 根元素在官方模式中的文件名。
const ofdRoot = "OFD.xsd"

// ofdProfileRoot 是 OFD 根元素的 profile overlay 文件名。它只放宽 DocType
// 取值，其余与 ofdRoot 一致。
const ofdProfileRoot = "OFDProfile.xsd"

var roots = map[string]string{
	"OFD":         ofdRoot,
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
//
// byRoot 是 GB/T 33190—2016 发布的官方模式，原样保留；profileByRoot 是
// profile overlay，只在文档声明了非基础 DocType 时使用。两者按需选择，
// 因此基础 profile 的一致性检查不受 overlay 影响。
type Set struct {
	byRoot        map[string]*xsd.Schema
	profileByRoot map[string]*xsd.Schema
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

// Schema 返回指定 OFD XML 根元素对应的官方模式，等价于以基础 DocType 调用
// SchemaFor。
func (s *Set) Schema(root string) (*xsd.Schema, bool) {
	return s.SchemaFor(root, spec.DocTypeOFD)
}

// SchemaFor 返回指定根元素对应的模式。docType 为空或为基础 profile 时用官方
// GB/T 33190 模式，其余已知 profile 改用 overlay——GB/T 42133 与电子病历标准
// 把 DocType 收紧为 "OFD-A"/"OFD-H"，直接用官方模式校验会把合规文件判为错误。
//
// overlay 只负责不产生假阳性，profile 的具体要求由校验器的 profile 规则负责。
func (s *Set) SchemaFor(root string, docType string) (*xsd.Schema, bool) {
	if s == nil {
		return nil, false
	}
	// 未知取值不切换 overlay，仍按官方模式校验，由 DocType 的枚举约束报出
	// “不在允许的枚举值中”。返回“未注册”会把问题误报成缺少根元素模式。
	if docType == "" || docType == spec.DocTypeOFD || !spec.IsDocType(docType) {
		schema, ok := s.byRoot[root]
		return schema, ok
	}
	schema, ok := s.profileByRoot[root]
	if !ok {
		schema, ok = s.byRoot[root]
	}
	return schema, ok
}

func load() (*Set, error) {
	resolver := xsd.NewCatalogResolver()
	filenames := make(map[string]struct{}, len(roots)+2)
	filenames["Definitions.xsd"] = struct{}{}
	filenames[ofdProfileRoot] = struct{}{}
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

	set := &Set{
		byRoot:        make(map[string]*xsd.Schema, len(roots)),
		profileByRoot: make(map[string]*xsd.Schema, len(roots)),
	}
	for root, filename := range roots {
		schema, err := xsd.LoadFile(filename, xsd.Options{
			Resolver: resolver,
		})
		if err != nil {
			return nil, fmt.Errorf("编译内置 XSD 模式 %q 失败：%w", filename, err)
		}
		set.byRoot[root] = schema
		if filename == ofdRoot {
			overlay, err := xsd.LoadFile(ofdProfileRoot, xsd.Options{
				Resolver: resolver,
			})
			if err != nil {
				return nil, fmt.Errorf("编译 profile overlay 模式 %q 失败：%w", ofdProfileRoot, err)
			}
			set.profileByRoot[root] = overlay
		}
	}
	// overlay 只在 OFD 根元素上生效，其余根元素与官方模式共用。
	for root, filename := range roots {
		if filename != ofdRoot {
			set.profileByRoot[root] = set.byRoot[root]
		}
	}
	return set, nil
}
