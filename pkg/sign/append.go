package sign

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/beevik/etree"
	"github.com/knroy/go-xml/v2/xdm"
	"github.com/knroy/go-xml/v2/xsd"
	"github.com/zc310/ofd/internal/container"
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/schema"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/pkg/spec"
)

type appendBody struct {
	docDir, signatureDir, listName string
	listData                       []byte
	document                       *etree.Document
	list                           parser.Signatures
}

type appendPlan struct {
	id         string
	root       []byte
	bodies     []appendBody
	priorFiles []string
	catalogs   map[string]bool
	remaining  int64
}

func openAppendPackage(input any) (*container.Package, error) {
	if reader, ok := input.(io.Reader); ok {
		data, err := io.ReadAll(io.LimitReader(reader, (512<<20)+1))
		if err != nil {
			return nil, err
		}
		if len(data) > 512<<20 {
			return nil, protocol.ErrLimitExceeded
		}
		input = data
	}
	return openPackage(input)
}

func validateAppendOptions(options Options) error {
	if options.Physical && (options.Type != spec.SigTypeSign || options.Append || options.Relative != "" || options.LockSignatures || options.References != nil || options.Stamp != nil || options.StampSeam != nil || options.Seal != "" || options.SealImage != "") {
		return fmt.Errorf("物理签署仅支持普通签名，不能组合追加、引用筛选或印模选项")
	}
	if !options.Append && (options.Relative != "" || options.LockSignatures) {
		return fmt.Errorf("Relative 与 LockSignatures 需要 Append")
	}
	if options.Append && options.Type != spec.SigTypeSign {
		return fmt.Errorf("追加签署仅支持 Type=Sign")
	}
	if options.Relative != "" && !securexml.ValidID(options.Relative) {
		return fmt.Errorf("Relative 必须为合法 xs:IDREF")
	}
	return nil
}

// readAppendEntries 在摘要和路径预检之前拒绝歧义容器，限制输入、条目数及解压总量。
func readAppendEntries(ctx context.Context, pkg *container.Package) ([]entry, error) {
	if pkg.InputSize() > 512<<20 {
		return nil, protocol.ErrLimitExceeded
	}
	seen, files := map[string]bool{}, map[string]bool{}
	var infos []container.Entry
	var total uint64
	var invalid error
	err := pkg.WalkEntries(func(info container.Entry) bool {
		name := strings.TrimSuffix(info.Name, "/")
		key := strings.ToLower(name)
		if container.ValidateEntryName(name) != nil || strings.Contains(name, ":") || seen[key] {
			invalid = fmt.Errorf("签名容器路径重复或无效: %s", info.Name)
			return false
		}
		seen[key] = true
		if !info.IsDir {
			files[key] = true
		}
		if len(seen) > 10000 || info.UncompressedSize > 64<<20 || info.UncompressedSize > 512<<20-total {
			invalid = protocol.ErrLimitExceeded
			return false
		}
		total += info.UncompressedSize
		infos = append(infos, info)
		return ctx.Err() == nil
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if invalid != nil {
		return nil, invalid
	}
	for key := range seen {
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			if files[parent] {
				return nil, fmt.Errorf("签名容器文件与目录冲突: %s", parent)
			}
		}
	}
	var entries []entry
	for _, info := range infos {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if info.IsDir {
			continue
		}
		data, err := pkg.ReadLimit(info.Path, int64(info.UncompressedSize)+1)
		if err != nil {
			return nil, err
		}
		if uint64(len(data)) != info.UncompressedSize {
			return nil, fmt.Errorf("签名容器条目长度不匹配: %s", info.Path)
		}
		entries = append(entries, entry{name: info.Path, data: data, method: info.Method})
	}
	return entries, nil
}

// prepareAppend 按 GB/T 33190 §18.1 和 GM/T 0099 §7.2、表 B.1 增加列表记录。
// 所有旧引用和将被修改的路径均在调用签名器之前检查，不迁移旧签名的原文路径。
func prepareAppend(ctx context.Context, pkg *container.Package, entries []entry, index map[string][]byte, document *etree.Document, options Options) (*appendPlan, error) {
	root := document.Root()
	if err := validateAppendXML(ctx, index[spec.RootDocument], "OFD", root.SelectAttrValue("DocType", "")); err != nil {
		return nil, err
	}
	plan := &appendPlan{}
	ids, catalogs, directories := map[string]bool{}, map[string]bool{}, map[string]bool{}
	plan.catalogs = catalogs
	var previous []models.Signature
	rootChanged := false
	for _, body := range directChildren(root, "DocBody") {
		docRoot, err := securexml.Resolve(spec.RootDocument, directChild(body, "DocRoot").Text())
		if err != nil {
			return nil, err
		}
		if _, exists := index[docRoot]; !exists {
			return nil, fmt.Errorf("DocRoot 不存在或路径大小写不一致: %s", docRoot)
		}
		docDir, err := docDirOf(body)
		if err != nil {
			return nil, err
		}
		if directories[docDir] {
			return nil, fmt.Errorf("追加签署不接受文档体目录重复: %s", docDir)
		}
		directories[docDir] = true
		item := appendBody{docDir: docDir, listName: path.Join(docDir, "Signatures.xml"), document: etree.NewDocument()}
		if node := directChild(body, "Signatures"); node != nil {
			item.listName, err = securexml.Resolve(spec.RootDocument, node.Text())
			if err != nil {
				return nil, err
			}
			data, exists := index[item.listName]
			if !exists {
				return nil, fmt.Errorf("签名列表不存在或路径大小写不一致: %s", item.listName)
			}
			if err := validateAppendXML(ctx, data, "Signatures", ""); err != nil {
				return nil, err
			}
			if err := securexml.Decode(ctx, data, "Signatures", &item.list); err != nil {
				return nil, err
			}
			if err := item.document.ReadFromBytes(data); err != nil {
				return nil, err
			}
		} else {
			if err := unusedAppendPath(pkg, item.listName); err != nil {
				return nil, err
			}
			item.document.CreateElement("Signatures").CreateAttr("xmlns", spec.Namespace)
			setBodySignatures(body, item.listName)
			rootChanged = true
		}
		if catalogs[item.listName] {
			return nil, fmt.Errorf("文档体共用了同一签名列表")
		}
		catalogs[item.listName] = true
		parents := map[string]string{}
		for _, old := range item.list.Signatures {
			if old.Type != spec.SigTypeSign {
				return nil, fmt.Errorf("追加签署仅支持已有普通数字签名")
			}
			ids[old.ID], parents[old.ID] = true, old.Relative
			name, err := securexml.Resolve(item.listName, old.BaseLoc.String())
			if err != nil {
				return nil, err
			}
			data, exists := index[name]
			if !exists {
				return nil, fmt.Errorf("旧签名文件不存在或路径大小写不一致: %s", name)
			}
			if err := validateAppendXML(ctx, data, "Signature", ""); err != nil {
				return nil, err
			}
			var signature models.Signature
			if err := securexml.Decode(ctx, data, "Signature", &signature); err != nil {
				return nil, err
			}
			if signature.ID != "" && signature.ID != old.ID {
				return nil, fmt.Errorf("旧签名 ID 与列表不一致")
			}
			signature.FilePath, signature.Type, signature.Relative = name, old.Type, old.Relative
			previous = append(previous, signature)
			plan.priorFiles = append(plan.priorFiles, name)
			locations := []models.StLoc{signature.SignedValue}
			if seal := signature.SignedInfo.Seal; seal != nil {
				locations = append(locations, seal.BaseLoc, seal.ImageLoc)
			}
			for _, loc := range locations {
				if loc == "" {
					continue
				}
				artifact, err := securexml.Resolve(name, loc.String())
				if err != nil {
					return nil, err
				}
				if _, ok := index[artifact]; !ok {
					return nil, fmt.Errorf("旧签名附属文件不存在: %s", artifact)
				}
				plan.priorFiles = append(plan.priorFiles, artifact)
			}
		}
		if item.list.MaxSignID != nil {
			ids[*item.list.MaxSignID] = true
		}
		for id := range parents {
			if err := checkAppendRelative(id, parents, 32); err != nil {
				return nil, err
			}
		}
		if options.Relative != "" {
			if err := checkAppendRelative(options.Relative, parents, 31); err != nil {
				return nil, err
			}
		}
		plan.bodies = append(plan.bodies, item)
	}
	plan.id = strings.TrimSpace(options.ID)
	if plan.id == "" {
		for n := 1; ; n++ {
			candidate := fmt.Sprintf("sign-%d", n)
			if !ids[candidate] {
				plan.id = candidate
				break
			}
		}
	}
	if !securexml.ValidID(plan.id) || ids[plan.id] {
		return nil, fmt.Errorf("追加签名 ID 非法或已使用: %s", plan.id)
	}
	plan.root = index[spec.RootDocument]
	if rootChanged {
		var err error
		plan.root, err = document.WriteToBytes()
		if err != nil {
			return nil, err
		}
	}
	modified := map[string]bool{}
	for name := range catalogs {
		modified[name] = true
	}
	if !bytes.Equal(plan.root, index[spec.RootDocument]) {
		modified[spec.RootDocument] = true
	}
	for _, signature := range previous {
		for _, reference := range signature.SignedInfo.References.Reference {
			name, err := securexml.Resolve(signature.FilePath, reference.FileRef.String())
			if err != nil {
				return nil, err
			}
			if modified[name] {
				return nil, fmt.Errorf("%w: 已有签名保护了需要更新的 %s", protocol.ErrProtectedContent, name)
			}
		}
		valueName, err := securexml.Resolve(signature.FilePath, signature.SignedValue.String())
		if err != nil {
			return nil, err
		}
		result, err := parser.VerifyDocumentSignature(ctx, pkg, &signature, &parser.SignedValue{Raw: index[valueName]}, &VerificationOptions{UID: options.UID})
		if err != nil {
			return nil, fmt.Errorf("旧签名无法验证: %w", err)
		}
		if !result.Valid || result.Validation.Status == "invalid" {
			return nil, fmt.Errorf("旧签名或引用摘要无效: %s", signature.FilePath)
		}
	}
	for i := range plan.bodies {
		item := &plan.bodies[i]
		item.signatureDir = path.Join(item.docDir, "Signatures", plan.id)
		if err := unusedAppendPath(pkg, item.signatureDir); err != nil {
			return nil, err
		}
		listRoot := item.document.Root()
		prefix := ""
		if listRoot.Space != "" {
			prefix = listRoot.Space + ":"
		}
		if maximum := directChild(listRoot, "MaxSignId"); maximum != nil {
			listRoot.RemoveChild(maximum)
		}
		maxID := "max-" + plan.id
		for ids[maxID] || maxID == plan.id {
			maxID = "max-" + maxID
		}
		maximum := etree.NewElement(prefix + "MaxSignId")
		maximum.SetText(maxID)
		listRoot.InsertChildAt(0, maximum)
		node := listRoot.CreateElement(prefix + "Signature")
		node.CreateAttr("ID", plan.id)
		node.CreateAttr("Type", spec.SigTypeSign)
		node.CreateAttr("BaseLoc", "/"+path.Join(item.signatureDir, "Signature_"+plan.id+".xml"))
		if options.Relative != "" {
			node.CreateAttr("Relative", options.Relative)
		}
		data, err := item.document.WriteToBytes()
		if err != nil {
			return nil, err
		}
		item.listData = data
		if err := validateAppendXML(ctx, item.listData, "Signatures", ""); err != nil {
			return nil, err
		}
	}
	plan.remaining = 512 << 20
	count := 1 // OFD.xml 由最终原字节或新增入口替换。
	plan.remaining -= int64(len(plan.root))
	for _, entry := range entries {
		if entry.name != spec.RootDocument && !catalogs[entry.name] {
			plan.remaining -= int64(len(entry.data))
			count++
		}
	}
	perBody := 3 // 列表、签名 XML、签名值。
	if options.Seal != "" {
		perBody++
	}
	if options.SealImage != "" {
		perBody++
	}
	if count+perBody*len(plan.bodies) > 10000 || plan.remaining < 0 {
		return nil, protocol.ErrLimitExceeded
	}
	return plan, nil
}

func (p *appendPlan) consume(entries []entry) error {
	for _, item := range entries {
		if len(item.data) > 64<<20 {
			return protocol.ErrLimitExceeded
		}
		p.remaining -= int64(len(item.data))
		if p.remaining < 0 {
			return protocol.ErrLimitExceeded
		}
	}
	return nil
}

func validateAppendXML(ctx context.Context, data []byte, root, docType string) error {
	var ignored struct{}
	if err := securexml.Decode(ctx, data, root, &ignored); err != nil {
		return err
	}
	schemas, err := schema.Default()
	if err != nil {
		return err
	}
	definition, _ := schemas.SecuritySchema(root)
	if root == "OFD" {
		definition, _ = schemas.SchemaFor(root, docType)
	}
	structure, err := xdm.Parse(bytes.NewReader(data), xdm.ParseOptions{MaxBytes: 4 << 20, MaxNodes: 100000, MaxDepth: 64})
	if err != nil {
		return err
	}
	if err := definition.Validate(structure.Root, xsd.ValidateOptions{MaxErrors: 1}); err != nil {
		return fmt.Errorf("追加签署的 %s 模式检查失败: %w", root, err)
	}
	return ctx.Err()
}

func checkAppendRelative(id string, parents map[string]string, maximum int) error {
	seen := map[string]bool{}
	for id != "" {
		parent, ok := parents[id]
		if !ok || seen[id] || len(seen) >= maximum {
			return fmt.Errorf("签名 Relative 悬空、循环或超过 32 层")
		}
		seen[id] = true
		id = parent
	}
	return nil
}

func unusedAppendPath(pkg *container.Package, name string) error {
	key := strings.ToLower(name)
	for _, info := range pkg.Entries() {
		existing := strings.ToLower(strings.TrimSuffix(info.Name, "/"))
		if existing == key || strings.HasPrefix(existing, key+"/") || !info.IsDir && strings.HasPrefix(key, existing+"/") {
			return fmt.Errorf("%w: 新签名路径与已有条目冲突: %s", protocol.ErrProtectedContent, name)
		}
	}
	return nil
}

func appendReferences(docDir string, entries []entry, options *ReferenceOptions, previous []string, catalogs map[string]bool) []string {
	seen := map[string]bool{}
	for _, item := range entries {
		if !isSignatureEntry(item.name) && !catalogs[item.name] && strings.HasPrefix(item.name, docDir+"/") && matchReference(strings.TrimPrefix(item.name, docDir+"/"), options) {
			seen["/"+item.name] = true
		}
	}
	if options != nil && options.RootDocument {
		seen["/"+spec.RootDocument] = true
	}
	for _, name := range previous {
		seen["/"+name] = true
	}
	var result []string
	for reference := range seen {
		result = append(result, reference)
	}
	sort.Strings(result)
	return result
}

func readAppendAsset(name string, limit int64) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, protocol.ErrLimitExceeded
	}
	return data, nil
}
