// Package physicalsig 读取由明文 OFD 入口定位的密文包签名，并隔离物理与逻辑引用域。
package physicalsig

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/beevik/etree"
	"github.com/knroy/go-xml/v2/xdm"
	"github.com/knroy/go-xml/v2/xsd"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/schema"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/pkg/spec"
)

type Record struct {
	DocumentIndex   int
	ID, List, Value string
	Signature       models.Signature
}

type Graph struct {
	Root           []byte
	Records        []Record
	Metadata       map[string]bool
	MetadataBytes  int64
	ProcessedBytes int64
	MetadataHashes map[string][32]byte
}

type Verification struct {
	Record Record
	Result *parser.SignatureVerificationResult
	Digest *parser.SignatureDigestResult
}

// ValidateXML 检查受限 XML 及附录 B/基础 OFD 模式，避免重写时遗漏未知结构。
func ValidateXML(ctx context.Context, data []byte, root, docType string, target any) error {
	if err := securexml.Decode(ctx, data, root, target); err != nil {
		return err
	}
	schemas, err := schema.Default()
	if err != nil {
		return err
	}
	definition, ok := schemas.SecuritySchema(root)
	if root == "OFD" {
		definition, ok = schemas.SchemaFor(root, docType)
	}
	if !ok {
		return fmt.Errorf("未知密码元数据模式 %s", root)
	}
	structure, err := xdm.Parse(bytes.NewReader(data), xdm.ParseOptions{MaxBytes: 4 << 20, MaxNodes: 100000, MaxDepth: 64})
	if err != nil {
		return err
	}
	if err := definition.Validate(structure.Root, xsd.ValidateOptions{MaxErrors: 1}); err != nil {
		return fmt.Errorf("%s 模式检查失败: %w", root, err)
	}
	return ctx.Err()
}

// Discover 只从 OFD.xml 的签名列表发现签名。References 指向 Encryptions.xml
// 的普通签名按物理域解释，并必须同时保护 OFD.xml 和所属列表。
// 暂不支持同一包混合逻辑签名、Relative 或加密的签名入口；无物理签名返回 nil。
func Discover(ctx context.Context, pkg *container.Package, limits crypt.Limits) (*Graph, error) {
	if !crypt.Present(pkg) {
		return nil, nil
	}
	opt, err := crypt.NormalizeOptions(crypt.InspectOptions{Limits: limits})
	if err != nil {
		return nil, err
	}
	remaining := opt.Limits.MaxTotalMetadataBytes
	hashes := map[string][32]byte{}
	read := func(name string) ([]byte, error) {
		entry, ok := pkg.Lookup(name)
		if !ok || entry.IsDir || entry.Path != name {
			return nil, fmt.Errorf("物理签名条目不存在或路径大小写不一致: %s", name)
		}
		maximum := min(int64(4<<20), opt.Limits.MaxMetadataBytes, remaining)
		if maximum <= 0 || entry.UncompressedSize > uint64(maximum) {
			return nil, crypt.ErrLimitExceeded
		}
		data, err := pkg.ReadLimit(name, maximum)
		remaining -= int64(len(data))
		if err == nil {
			hashes[name] = sha256.Sum256(data)
		}
		return data, err
	}
	if !pkg.Has(spec.RootDocument) {
		return nil, nil
	}
	data, err := read(spec.RootDocument)
	if err != nil {
		return nil, err
	}
	var root models.OFD
	// 整包加密可使用不可解析的占位主入口，此时没有可发现的外层签名。
	if err := securexml.Decode(ctx, data, "OFD", &root); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	g := &Graph{Root: data, Metadata: map[string]bool{}, MetadataHashes: hashes}
	physical, other := false, false
	for index, body := range root.DocBodies {
		if body.Signatures == nil {
			continue
		}
		listName, err := securexml.Resolve(spec.RootDocument, body.Signatures.String())
		if err != nil {
			return nil, err
		}
		// 隐藏的逻辑列表须等解密后验证，不能按物理路径猜测。
		if !pkg.Has(listName) {
			other = true
			continue
		}
		listData, err := read(listName)
		if err != nil {
			return nil, err
		}
		var list parser.Signatures
		if err := securexml.Decode(ctx, listData, "Signatures", &list); err != nil {
			return nil, err
		}
		for _, item := range list.Signatures {
			if item.Type != spec.SigTypeSign {
				other = true
				continue
			}
			name, err := securexml.Resolve(listName, item.BaseLoc.String())
			if err != nil {
				return nil, err
			}
			if !pkg.Has(name) {
				other = true
				continue
			}
			original, err := read(name)
			if err != nil {
				return nil, err
			}
			var signature models.Signature
			if err := securexml.Decode(ctx, original, "Signature", &signature); err != nil {
				return nil, err
			}
			targets := map[string]bool{}
			for _, ref := range signature.SignedInfo.References.Reference {
				target, err := securexml.Resolve(name, ref.FileRef.String())
				if err != nil {
					return nil, err
				}
				targets[target] = true
			}
			if !targets[crypt.EntryPoint] {
				other = true
				continue
			}
			physical = true
			if item.Relative != "" || !targets[spec.RootDocument] || !targets[listName] || signature.ID != "" && signature.ID != item.ID {
				return nil, fmt.Errorf("物理签名必须绑定入口和列表，且不支持 Relative")
			}
			if err := ValidateXML(ctx, original, "Signature", "", &signature); err != nil {
				return nil, err
			}
			value, err := securexml.Resolve(name, signature.SignedValue.String())
			if err != nil {
				return nil, err
			}
			valueEntry, exists := pkg.Lookup(value)
			if !exists || valueEntry.IsDir || valueEntry.Path != value {
				return nil, fmt.Errorf("物理签名值路径缺失或大小写不一致")
			}
			if g.Metadata[name] || g.Metadata[value] || name == value || value == listName || value == spec.RootDocument || value == crypt.EntryPoint {
				return nil, fmt.Errorf("物理签名元数据路径冲突")
			}
			signature.Type, signature.FilePath = spec.SigTypeSign, name
			g.Records = append(g.Records, Record{DocumentIndex: index, ID: item.ID, List: listName, Value: value, Signature: signature})
			g.Metadata[listName], g.Metadata[name], g.Metadata[value] = true, true, true
		}
		if physical {
			if err := ValidateXML(ctx, listData, "Signatures", "", &list); err != nil {
				return nil, err
			}
		}
	}
	if !physical {
		return nil, nil
	}
	if other {
		return nil, fmt.Errorf("%w: 物理与逻辑签名混合布局尚未支持", crypt.ErrUnsupported)
	}
	if err := ValidateXML(ctx, data, "OFD", root.DocType, &root); err != nil {
		return nil, err
	}
	g.MetadataBytes = opt.Limits.MaxTotalMetadataBytes - remaining
	return g, nil
}

// Verify 明确使用原始物理包，不回退到解密后的同名路径。
func Verify(ctx context.Context, pkg *container.Package, graph *Graph, options *parser.SignatureVerificationOptions, limits crypt.Limits) ([]Verification, error) {
	opt, err := crypt.NormalizeOptions(crypt.InspectOptions{Limits: limits})
	if err != nil {
		return nil, err
	}
	if err := CheckContainer(ctx, pkg, opt.Limits); err != nil {
		return nil, err
	}
	processed := opt.Limits.MaxProcessedBytes - graph.MetadataBytes
	budgetExceeded := false
	bounded, err := container.NewVirtual(pkg.Entries(), func(name string) (io.ReadCloser, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		maximum := min(opt.Limits.MaxEntryBytes, processed)
		if name == spec.RootDocument || graph.Metadata[name] {
			maximum = min(maximum, int64(4<<20), opt.Limits.MaxMetadataBytes)
		}
		if maximum <= 0 {
			budgetExceeded = true
			return nil, crypt.ErrLimitExceeded
		}
		data, err := pkg.ReadLimit(name, maximum)
		if errors.Is(err, container.ErrReadLimitExceeded) {
			budgetExceeded = true
		}
		processed -= int64(len(data))
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}, nil)
	if err != nil {
		return nil, err
	}
	defer bounded.Close()
	metadataRemaining := opt.Limits.MaxTotalMetadataBytes - graph.MetadataBytes
	var results []Verification
	for _, record := range graph.Records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		maximum := min(int64(4<<20), opt.Limits.MaxMetadataBytes, metadataRemaining)
		if maximum <= 0 {
			return nil, crypt.ErrLimitExceeded
		}
		value, err := bounded.ReadLimit(record.Value, maximum)
		if err != nil {
			return nil, err
		}
		metadataRemaining -= int64(len(value))
		graph.MetadataHashes[record.Value] = sha256.Sum256(value)
		result, err := parser.VerifyDocumentSignature(ctx, bounded, &record.Signature, &parser.SignedValue{Raw: value}, options)
		if budgetExceeded {
			return nil, crypt.ErrLimitExceeded
		}
		if err != nil {
			return nil, err
		}
		digest, err := parser.VerifySignatureDigest(bounded, record.Signature.FilePath, &record.Signature, nil)
		if budgetExceeded {
			return nil, crypt.ErrLimitExceeded
		}
		if err != nil {
			return nil, err
		}
		results = append(results, Verification{Record: record, Result: result, Digest: digest})
	}
	graph.MetadataBytes = opt.Limits.MaxTotalMetadataBytes - metadataRemaining
	graph.ProcessedBytes = opt.Limits.MaxProcessedBytes - processed
	return results, nil
}

// LogicalPackage 从普通文档遍历中移除外层物理签名入口及其元数据。
// 原始字节仍在物理包上验证；外签 References 不能作为文档可达性的依据。
// 返回包借用 logical，不取得其所有权。
func LogicalPackage(ctx context.Context, logical *container.Package, graph *Graph) (*container.Package, error) {
	data, err := logical.ReadLimit(spec.RootDocument, 4<<20)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, graph.Root) {
		return nil, fmt.Errorf("%w: 解密覆盖了物理签名入口", crypt.ErrProtectedContent)
	}
	for name := range graph.Metadata {
		data, err := logical.ReadLimit(name, 4<<20)
		if err != nil {
			return nil, err
		}
		if sha256.Sum256(data) != graph.MetadataHashes[name] {
			return nil, fmt.Errorf("%w: 解密覆盖了物理签名元数据 %s", crypt.ErrProtectedContent, name)
		}
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, err
	}
	for _, body := range doc.Root().ChildElements() {
		if body.Tag != "DocBody" {
			continue
		}
		for _, child := range body.ChildElements() {
			if child.Tag == "Signatures" {
				body.RemoveChild(child)
			}
		}
	}
	projected, err := doc.WriteToBytes()
	if err != nil {
		return nil, err
	}
	var entries []container.Entry
	for _, entry := range logical.Entries() {
		if graph.Metadata[entry.Path] {
			continue
		}
		if entry.Path == spec.RootDocument {
			entry.UncompressedSize = uint64(len(projected))
			entry.Virtual = true
		}
		entries = append(entries, entry)
	}
	return container.NewVirtual(entries, func(name string) (io.ReadCloser, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if name == spec.RootDocument {
			return io.NopCloser(bytes.NewReader(projected)), nil
		}
		return logical.Open(name)
	}, nil)
}

// CheckContainer 在摘要读取前限制条目大小和总量，并拒绝歧义路径。
func CheckContainer(ctx context.Context, pkg *container.Package, limits crypt.Limits) error {
	opt, err := crypt.NormalizeOptions(crypt.InspectOptions{Limits: limits})
	if err != nil {
		return err
	}
	if pkg.InputSize() > opt.Limits.MaxInputBytes {
		return crypt.ErrLimitExceeded
	}
	seen, files := map[string]bool{}, map[string]bool{}
	remaining := opt.Limits.MaxTotalBytes
	for _, entry := range pkg.Entries() {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSuffix(entry.Name, "/")
		key := strings.ToLower(name)
		if container.ValidateEntryName(name) != nil || strings.Contains(name, ":") || seen[key] {
			return fmt.Errorf("物理签名容器路径重复或无效")
		}
		seen[key] = true
		if entry.IsDir && entry.UncompressedSize != 0 {
			return fmt.Errorf("物理签名容器目录含有数据")
		}
		if len(seen) > opt.Limits.MaxEntries || entry.UncompressedSize > uint64(min(remaining, opt.Limits.MaxEntryBytes)) {
			return crypt.ErrLimitExceeded
		}
		remaining -= int64(entry.UncompressedSize)
		if !entry.IsDir {
			files[key] = true
		}
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if files[parent] {
				return fmt.Errorf("物理签名容器文件与目录冲突")
			}
		}
	}
	return nil
}
