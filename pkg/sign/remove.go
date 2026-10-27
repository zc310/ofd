package sign

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/beevik/etree"
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/internal/xmlcodec"
	"github.com/zc310/ofd/pkg/spec"
)

// RemoveOptions 控制签名移除；不提供 IDs 时移除所有文档体的全部签名。
type RemoveOptions struct {
	// IDs 在各文档体中选择签名；每个 ID 至少应在一个列表中存在。
	IDs []string
	// Cleanup 仅用于全部移除：额外删除 Signatures/、Signs/ 及惯用签名文件名的条目，
	// 并允许签名列表或描述缺失、损坏。自定义路径中无法解析发现的残留不会被猜测删除。
	Cleanup bool
	// Deterministic 使用固定 ZIP 时间。
	Deterministic bool
}

type removalSignature struct {
	id, relative      string
	files, references []string
	remove            bool
}

type removalList struct {
	document   *etree.Document
	signatures []removalSignature
}

// Remove 读取文件路径、[]byte 或 io.Reader，移除签名并写入 w，无需签名器。
// 按 GB/T 33190—2016 §7.4、§18.1、§18.2 更新入口/列表，只删除签名描述及负载，
// 不重命名文档或重写正文。保留的签名引用或 Relative 依赖受影响时返回 crypt.ErrProtectedContent。
// 加密包与完整性清单须先单独处理；预算同追加签署，任何错误都要求丢弃部分输出。
func Remove(ctx context.Context, input any, w io.Writer, options RemoveOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("签名清理输出写入器为空")
	}
	if options.Cleanup && len(options.IDs) != 0 {
		return fmt.Errorf("Cleanup 只能与全部签名移除组合使用")
	}
	wanted := map[string]bool{}
	for _, id := range options.IDs {
		if !securexml.ValidID(id) {
			return fmt.Errorf("签名 ID 无效: %q", id)
		}
		wanted[id] = false
	}
	pkg, err := openAppendPackage(input)
	if err != nil {
		return err
	}
	defer func() { _ = pkg.Close() }()
	if protocol.Present(pkg) || pkg.Has(spec.IntegrityEntryPoint) {
		return fmt.Errorf("%w: 清理签名前需单独处理加密包或完整性清单", protocol.ErrProtectedContent)
	}
	entries, err := readAppendEntries(ctx, pkg)
	if err != nil {
		return err
	}
	index := map[string][]byte{}
	for _, item := range entries {
		index[item.name] = item.data
	}
	document, err := removalXML(ctx, index, spec.RootDocument, "OFD")
	if err != nil {
		return err
	}
	bodies := removalChildren(document.Root(), "DocBody")
	if len(bodies) == 0 {
		return fmt.Errorf("OFD.xml 中没有 DocBody")
	}
	all := len(wanted) == 0
	deleted, changed := map[string]bool{}, map[string][]byte{}
	lists := map[string]*removalList{}
	content := map[string]bool{spec.RootDocument: true}
	rootChanged := false
	for _, body := range bodies {
		roots, locations := removalChildren(body, "DocRoot"), removalChildren(body, "Signatures")
		if len(roots) != 1 || len(locations) > 1 {
			return fmt.Errorf("DocBody 的 DocRoot 或 Signatures 缺失或重复")
		}
		docRoot := roots[0]
		name, err := securexml.Resolve(spec.RootDocument, strings.TrimSpace(docRoot.Text()))
		if err != nil {
			return err
		}
		content[name] = true
		if len(locations) == 0 {
			continue
		}
		location := locations[0]
		name, err = securexml.Resolve(spec.RootDocument, strings.TrimSpace(location.Text()))
		if err != nil {
			return err
		}
		list, exists := lists[name]
		if !exists {
			list, err = readRemovalList(ctx, index, name, options.Cleanup, !all)
			if err != nil {
				return err
			}
			lists[name] = list
		}
		remaining := 0
		selectedAny := false
		for i := range list.signatures {
			sig := &list.signatures[i]
			if _, selected := wanted[sig.id]; selected {
				wanted[sig.id] = true
				sig.remove = true
			}
			if all {
				sig.remove = true
			}
			if !sig.remove {
				remaining++
			} else {
				selectedAny = true
			}
		}
		if remaining == 0 && (all || selectedAny) {
			body.RemoveChild(location)
			rootChanged = true
			deleted[name] = true
		}
	}
	for id, found := range wanted {
		if !found {
			return fmt.Errorf("未找到签名 ID: %s", id)
		}
	}
	keptFiles := map[string]bool{}
	for name, list := range lists {
		listChanged := false
		for _, sig := range list.signatures {
			if sig.remove {
				listChanged = true
				for _, file := range sig.files {
					deleted[file] = true
				}
			} else {
				for _, file := range sig.files {
					keptFiles[file] = true
				}
			}
		}
		if !listChanged || deleted[name] {
			continue
		}
		for _, node := range directChildren(list.document.Root(), "Signature") {
			if _, selected := wanted[node.SelectAttrValue("ID", "")]; selected {
				list.document.Root().RemoveChild(node)
			}
		}
		changed[name], err = xmlcodec.Marshal(list.document)
		if err != nil {
			return err
		}
	}
	// 多个签名可共用印章或图片，仍被保留签名使用的负载不可删除。
	for name := range keptFiles {
		delete(deleted, name)
	}
	if options.Cleanup {
		for _, item := range entries {
			if isSignatureEntry(item.name) {
				deleted[item.name] = true
			}
		}
	}
	for name := range content {
		if deleted[name] {
			return fmt.Errorf("签名路径与文档入口冲突: %s", name)
		}
	}
	if rootChanged {
		changed[spec.RootDocument], err = xmlcodec.Marshal(document)
		if err != nil {
			return err
		}
	}
	for name, data := range changed {
		if len(data) > 4<<20 {
			return fmt.Errorf("%w: 输出 %s 超过 4 MiB", protocol.ErrLimitExceeded, name)
		}
	}
	if err := checkRemovalProtection(lists, deleted, changed, index); err != nil {
		return err
	}
	var kept []entry
	var total int
	for _, item := range entries {
		if deleted[item.name] {
			continue
		}
		if data, exists := changed[item.name]; exists {
			item.data = data
		}
		total += len(item.data)
		if len(item.data) > 64<<20 || total > 512<<20 {
			return protocol.ErrLimitExceeded
		}
		kept = append(kept, item)
	}
	rootData := index[spec.RootDocument]
	if rootChanged {
		rootData = changed[spec.RootDocument]
	}
	return writePackage(removalWriter{ctx, w}, kept, nil, rootData, options.Deterministic, true)
}

func removalXML(ctx context.Context, index map[string][]byte, name, root string) (*etree.Document, error) {
	data, exists := index[name]
	if !exists {
		return nil, fmt.Errorf("签名清理所需文件不存在: %s", name)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, fmt.Errorf("%w: %s 超过 4 MiB", protocol.ErrLimitExceeded, name)
	}
	// 主入口还可含重复的 CustomData 等元数据，不能套用密码 XML 的单值白名单。
	if root != "OFD" {
		var ignored struct{}
		if err := securexml.Decode(ctx, data, root, &ignored); err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", name, err)
		}
	}
	document, err := xmlcodec.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", name, err)
	}
	if element := document.Root(); element == nil || localName(element) != root || element.NamespaceURI() != spec.Namespace {
		return nil, fmt.Errorf("%s 的根元素或命名空间无效", name)
	}
	return document, nil
}

func removalChildren(parent *etree.Element, name string) []*etree.Element {
	var children []*etree.Element
	for _, child := range directChildren(parent, name) {
		if child.NamespaceURI() == spec.Namespace {
			children = append(children, child)
		}
	}
	return children
}

func readRemovalList(ctx context.Context, index map[string][]byte, name string, cleanup, validateIDs bool) (*removalList, error) {
	list := &removalList{}
	document, err := removalXML(ctx, index, name, "Signatures")
	if err != nil {
		if cleanup && removableMetadataError(ctx, err) {
			return list, nil
		}
		return nil, err
	}
	list.document = document
	ids := map[string]bool{}
	for _, node := range directChildren(document.Root(), "Signature") {
		id := node.SelectAttrValue("ID", "")
		// 全部移除无需解释 ID，允许清除真实旧样例使用的非 xs:ID 数字标识。
		if validateIDs && (!securexml.ValidID(id) || ids[id]) {
			return nil, fmt.Errorf("签名列表 %s 的 ID 无效或重复: %s", name, id)
		}
		ids[id] = true
		target, err := securexml.Resolve(name, node.SelectAttrValue("BaseLoc", ""))
		if err != nil {
			return nil, err
		}
		sig := removalSignature{id: id, relative: node.SelectAttrValue("Relative", ""), files: []string{target}}
		description, err := removalXML(ctx, index, target, "Signature")
		if err != nil {
			if cleanup && removableMetadataError(ctx, err) {
				list.signatures = append(list.signatures, sig)
				continue
			}
			return nil, err
		}
		info := directChild(description.Root(), "SignedInfo")
		if info == nil {
			if !cleanup {
				return nil, fmt.Errorf("签名 %s 缺少 SignedInfo", target)
			}
		} else {
			if seal := directChild(info, "Seal"); seal != nil {
				for _, child := range seal.ChildElements() {
					if localName(child) == "BaseLoc" || localName(child) == "ImageLoc" {
						file, err := securexml.Resolve(target, strings.TrimSpace(child.Text()))
						if err != nil {
							return nil, err
						}
						sig.files = append(sig.files, file)
					}
				}
			}
			if refs := directChild(info, "References"); refs != nil && !cleanup {
				for _, ref := range directChildren(refs, "Reference") {
					file, err := securexml.Resolve(target, ref.SelectAttrValue("FileRef", ""))
					if err != nil {
						return nil, err
					}
					sig.references = append(sig.references, file)
				}
			}
		}
		if value := directChild(description.Root(), "SignedValue"); value != nil {
			file, err := securexml.Resolve(target, strings.TrimSpace(value.Text()))
			if err != nil {
				return nil, err
			}
			sig.files = append(sig.files, file)
		} else if !cleanup {
			return nil, fmt.Errorf("签名 %s 缺少 SignedValue", target)
		}
		list.signatures = append(list.signatures, sig)
	}
	return list, nil
}

func removableMetadataError(ctx context.Context, err error) bool {
	return ctx.Err() == nil && !errors.Is(err, protocol.ErrLimitExceeded) && !errors.Is(err, securexml.ErrLimitExceeded)
}

func checkRemovalProtection(lists map[string]*removalList, deleted map[string]bool, changed, index map[string][]byte) error {
	for listName, list := range lists {
		remaining := map[string]bool{}
		for _, sig := range list.signatures {
			if !sig.remove {
				remaining[sig.id] = true
			}
		}
		for _, sig := range list.signatures {
			if sig.remove {
				continue
			}
			if deleted[listName] {
				return fmt.Errorf("%w: 保留签名 %s 所属列表将被删除: %s", protocol.ErrProtectedContent, sig.id, listName)
			}
			if sig.relative != "" && !remaining[sig.relative] {
				return fmt.Errorf("%w: 保留签名 %s 依赖签名 %s", protocol.ErrProtectedContent, sig.id, sig.relative)
			}
			for _, name := range append(append([]string{}, sig.references...), sig.files...) {
				data, updated := changed[name]
				if deleted[name] || updated && !bytes.Equal(data, index[name]) {
					return fmt.Errorf("%w: 保留签名 %s 保护了需删除或修改的文件 %s", protocol.ErrProtectedContent, sig.id, name)
				}
			}
		}
	}
	return nil
}

type removalWriter struct {
	ctx context.Context
	w   io.Writer
}

func (w removalWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.w.Write(data)
}
