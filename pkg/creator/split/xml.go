package split

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/internal/xmlcodec"
	"github.com/zc310/ofd/pkg/creator"
)

func child(element *etree.Element, name string) *etree.Element {
	if element != nil {
		for _, candidate := range element.ChildElements() {
			if candidate.Tag == name && candidate.NamespaceURI() == xmlcodec.Namespace {
				return candidate
			}
		}
	}
	return nil
}

func children(element *etree.Element, name string) []*etree.Element {
	var result []*etree.Element
	if element != nil {
		for _, candidate := range element.ChildElements() {
			if candidate.Tag == name && candidate.NamespaceURI() == xmlcodec.Namespace {
				result = append(result, candidate)
			}
		}
	}
	return result
}

func text(element *etree.Element, name string) string {
	if value := child(element, name); value != nil {
		return strings.TrimSpace(value.Text())
	}
	return ""
}

func remove(element *etree.Element, name string) bool {
	removed := false
	for _, value := range children(element, name) {
		element.RemoveChild(value)
		removed = true
	}
	return removed
}

// §7.3 的 ST_Loc 以包含引用的 XML 为基准；绝对路径从包根开始。
func resolve(from, value string) (string, error) {
	name, err := securexml.Resolve(from, strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("%s: %w", from, err)
	}
	if err := container.ValidateEntryName(name); err != nil {
		return "", err
	}
	return name, nil
}

func numeric(value string) (uint64, error) {
	id, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(value), "+"), 10, 32)
	if err != nil || id == 0 {
		return 0, invalid("对象 ID 必须是非零 unsignedInt: %q", value)
	}
	return id, nil
}

func (s *Session) readXML(name, root string) (*etree.Document, error) {
	if cached := s.xml[name]; cached != nil {
		if cached.Root().Tag != root {
			return nil, invalid("%s 的 XML 类型冲突", name)
		}
		return cached, nil
	}
	data, err := s.pkg.ReadLimit(name, s.options.Limits.MaxEntryBytes)
	if err != nil {
		return nil, err
	}
	tree, err := xmlcodec.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("解析 %s: %w", name, err)
	}
	if tree.Root() == nil || tree.Root().Tag != root || tree.Root().NamespaceURI() != xmlcodec.Namespace {
		return nil, invalid("%s 的根元素或 OFD 命名空间无效", name)
	}
	s.xml[name] = tree
	return tree, nil
}

func (s *Session) prepare(ctx context.Context, part Part) (Report, map[string][]byte, error) {
	report := Report{Document: part.Document, Pages: append([]int(nil), part.Pages...)}
	overrides := make(map[string][]byte)
	root := s.root.Copy()
	bodies := children(root.Root(), "DocBody")
	body := bodies[part.Document-1]
	for i, value := range bodies {
		if i != part.Document-1 {
			root.Root().RemoveChild(value)
		}
	}
	doc := s.documents[part.Document-1]
	report.SourceID = doc.info.ID
	report.DocID = doc.info.ID
	if s.pkg.Has("OFDEntries.xml") {
		report.Dropped = append(report.Dropped, "完整性清单及其签名")
	}
	if part.WholeDocument {
		for page := 1; page <= doc.info.Pages; page++ {
			report.Pages = append(report.Pages, page)
		}
		if s.options.Signatures == creator.SignatureDrop {
			files, err := s.signatureFiles(body)
			if err != nil {
				return Report{}, nil, err
			}
			report.DroppedSignatures = files
			if remove(body, "Signatures") {
				report.Dropped = append(report.Dropped, "数字签名及关联印章外观")
			}
		} else {
			files, err := s.signatureFiles(body)
			if err != nil {
				return Report{}, nil, err
			}
			report.Signatures = files
		}
	} else {
		if s.options.Signatures == creator.SignaturePreserve && child(body, "Signatures") != nil {
			return Report{}, nil, invalid("页面拆分不能保留原始签名，请使用 drop")
		}
		files, err := s.signatureFiles(body)
		if err != nil {
			return Report{}, nil, err
		}
		report.DroppedSignatures = files
		if remove(body, "Signatures") {
			report.Dropped = append(report.Dropped, "数字签名及关联印章外观")
		}
		if remove(body, "Versions") {
			report.Dropped = append(report.Dropped, "历史版本")
		}
		id, err := newDocID(doc.info.ID, part, s.options.Deterministic)
		if err != nil {
			return Report{}, nil, err
		}
		report.DocID = id
		info := child(body, "DocInfo")
		idNode := child(info, "DocID")
		if idNode == nil {
			return Report{}, nil, invalid("文档缺少 DocInfo/DocID")
		}
		idNode.SetText(id)
		setSourceData(info, "ofd.split.source_doc_id", doc.info.ID)
		setSourceData(info, "ofd.split.source_pages", fmt.Sprint(part.Pages))
		if err := s.selectPages(ctx, doc, part.Pages, overrides, &report); err != nil {
			return Report{}, nil, err
		}
	}
	if len(bodies) != 1 || !part.WholeDocument || s.options.Signatures == creator.SignatureDrop {
		data, err := xmlcodec.Marshal(root)
		if err != nil {
			return Report{}, nil, err
		}
		overrides["OFD.xml"] = data
	}
	if len(report.Signatures) > 0 {
		if err := s.checkSignatures(ctx, root, report.Signatures, overrides); err != nil {
			return Report{}, nil, err
		}
	}
	return report, overrides, nil
}

func setSourceData(info *etree.Element, name, value string) {
	list := child(info, "CustomDatas")
	qualified := func(tag string) string {
		if info.Space != "" {
			return info.Space + ":" + tag
		}
		return tag
	}
	if list == nil {
		list = info.CreateElement(qualified("CustomDatas"))
	}
	for _, node := range children(list, "CustomData") {
		if node.SelectAttrValue("Name", "") == name {
			node.SetText(value)
			return
		}
	}
	node := list.CreateElement(qualified("CustomData"))
	node.CreateAttr("Name", name)
	node.SetText(value)
}

func (s *Session) signatureFiles(body *etree.Element) ([]string, error) {
	location := text(body, "Signatures")
	if location == "" {
		return nil, nil
	}
	name, err := resolve("OFD.xml", location)
	if err != nil {
		return nil, err
	}
	list, err := s.readXML(name, "Signatures")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, signature := range children(list.Root(), "Signature") {
		file, err := resolve(name, signature.SelectAttrValue("BaseLoc", ""))
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// 完整文档体的签名只允许继续引用分卷自身的文件；保护主入口时还必须逐字节相等。
func (s *Session) checkSignatures(ctx context.Context, root *etree.Document, files []string, overrides map[string][]byte) error {
	unsigned := root.Copy()
	for _, body := range children(unsigned.Root(), "DocBody") {
		remove(body, "Signatures")
	}
	data, err := xmlcodec.Marshal(unsigned)
	if err != nil {
		return err
	}
	plain := make(map[string][]byte, len(overrides)+1)
	maps.Copy(plain, overrides)
	plain["OFD.xml"] = data
	index, err := s.resolver.Resolve(ctx, plain)
	if err != nil {
		return err
	}
	if !index.Complete() {
		return invalid("无法确定签名保护范围: %s", index.IncompleteReason())
	}
	allowed := index.Reachable
	for _, file := range files {
		allowed[file] = true
		tree, err := s.readXML(file, "Signature")
		if err != nil {
			return err
		}
		for _, value := range tree.Root().FindElements(".//*") {
			if value.NamespaceURI() == xmlcodec.Namespace && (value.Tag == "SignedValue" || (value.Tag == "BaseLoc" || value.Tag == "ImageLoc") && value.Parent().Tag == "Seal") {
				name, err := resolve(file, value.Text())
				if err != nil {
					return err
				}
				allowed[name] = true
			}
		}
	}
	if body := child(root.Root(), "DocBody"); body != nil && text(body, "Signatures") != "" {
		name, err := resolve("OFD.xml", text(body, "Signatures"))
		if err != nil {
			return err
		}
		allowed[name] = true
	}
	for _, file := range files {
		tree, err := s.readXML(file, "Signature")
		if err != nil {
			return err
		}
		for _, ref := range tree.Root().FindElements(".//Reference") {
			if ref.NamespaceURI() != xmlcodec.Namespace {
				continue
			}
			target, err := resolve(file, ref.SelectAttrValue("FileRef", ""))
			if err != nil {
				return err
			}
			if !allowed[target] {
				return invalid("签名 %s 引用分卷外文件 %s，无法保留；请使用 drop", file, target)
			}
			if changed, ok := overrides[target]; ok {
				original, err := s.pkg.ReadLimit(target, s.options.Limits.MaxEntryBytes)
				if err != nil {
					return err
				}
				if !bytes.Equal(original, changed) {
					return invalid("签名 %s 保护的 %s 将变化，无法保留；请使用 drop", file, target)
				}
			}
		}
	}
	return nil
}

func (s *Session) selectPages(ctx context.Context, doc document, selected []int, overrides map[string][]byte, report *Report) error {
	tree := doc.tree.Copy()
	root := tree.Root()
	pageList := child(root, "Pages")
	all := children(pageList, "Page")
	ids := make(map[uint64]bool)
	seen := make(map[uint64]bool)
	for _, page := range all {
		id, err := numeric(page.SelectAttrValue("ID", ""))
		if err != nil {
			return err
		}
		if seen[id] {
			return invalid("重复的页面 ID %d", id)
		}
		seen[id] = true
		pageList.RemoveChild(page)
	}
	for _, page := range selected {
		value := all[page-1]
		id, _ := numeric(value.SelectAttrValue("ID", ""))
		ids[id] = true
		pageList.AddChild(value)
	}
	for _, tag := range []string{"CustomTags", "Extensions"} {
		if remove(root, tag) {
			report.Dropped = append(report.Dropped, tag)
		}
	}
	if !s.options.KeepAttachments && remove(root, "Attachments") {
		report.Dropped = append(report.Dropped, "文档附件")
	}
	bookmarks := make(map[string]bool)
	if list := child(root, "Bookmarks"); list != nil {
		for _, bookmark := range children(list, "Bookmark") {
			dest := child(bookmark, "Dest")
			id, err := numeric(destAttr(dest, "PageID"))
			if err != nil {
				return err
			}
			if !ids[id] {
				list.RemoveChild(bookmark)
			} else {
				bookmarks[bookmark.SelectAttrValue("Name", "")] = true
			}
		}
	}
	clean := func(value *etree.Element) error {
		return cleanNavigation(value, ids, bookmarks, s.options.KeepAttachments)
	}
	if err := clean(root); err != nil {
		return err
	}
	work := newResources(s, overrides, clean)
	common := child(root, "CommonData")
	for _, kind := range []string{"PublicRes", "DocumentRes"} {
		for _, value := range children(common, kind) {
			name, err := resolve(doc.path, value.Text())
			if err != nil {
				return err
			}
			if err := work.addIndex(name); err != nil {
				return err
			}
		}
	}
	for _, value := range children(pageList, "Page") {
		name, err := resolve(doc.path, value.SelectAttrValue("BaseLoc", ""))
		if err != nil {
			return err
		}
		if err := work.addPage(ctx, name, common, doc.path, nil); err != nil {
			return err
		}
	}
	if location := text(root, "Annotations"); location != "" {
		name, err := resolve(doc.path, location)
		if err != nil {
			return err
		}
		original, err := s.readXML(name, "Annotations")
		if err != nil {
			return err
		}
		annotations := original.Copy()
		for _, value := range children(annotations.Root(), "Page") {
			id, err := numeric(value.SelectAttrValue("PageID", ""))
			if err != nil {
				return err
			}
			if !ids[id] {
				annotations.Root().RemoveChild(value)
				continue
			}
			file, err := resolve(name, text(value, "FileLoc"))
			if err != nil {
				return err
			}
			original, err := s.readXML(file, "PageAnnot")
			if err != nil {
				return err
			}
			annot := original.Copy()
			if err := clean(annot.Root()); err != nil {
				return err
			}
			work.roots = append(work.roots, annot.Root())
			if err := work.put(file, annot); err != nil {
				return err
			}
		}
		if len(children(annotations.Root(), "Page")) == 0 {
			remove(root, "Annotations")
		} else if err := work.put(name, annotations); err != nil {
			return err
		}
	}
	for _, template := range children(common, "TemplatePage") {
		id, err := numeric(template.SelectAttrValue("ID", ""))
		if err != nil {
			return err
		}
		if !work.templates[id] {
			common.RemoveChild(template)
		}
	}
	work.roots = append(work.roots, root)
	if err := work.prune(ctx); err != nil {
		return err
	}
	return work.put(doc.path, tree)
}

func destAttr(element *etree.Element, name string) string {
	if element == nil {
		return ""
	}
	return element.SelectAttrValue(name, "")
}

// 保持页面 ID，因此跳转只需清理分卷外目标；坐标、缩放和对象 CTM 均无需重写。
func cleanNavigation(element *etree.Element, pages map[uint64]bool, bookmarks map[string]bool, attachments bool) error {
	for _, value := range element.ChildElements() {
		if value.NamespaceURI() != xmlcodec.Namespace {
			continue
		}
		if value.Tag == "Action" {
			invalid := false
			if gotoNode := child(value, "Goto"); gotoNode != nil {
				if dest := child(gotoNode, "Dest"); dest != nil {
					id, err := numeric(dest.SelectAttrValue("PageID", ""))
					if err != nil {
						return err
					}
					invalid = !pages[id]
				} else if bookmark := child(gotoNode, "Bookmark"); bookmark != nil {
					invalid = !bookmarks[bookmark.SelectAttrValue("Name", "")]
				}
			}
			if !attachments && child(value, "GotoA") != nil {
				invalid = true
			}
			if invalid {
				element.RemoveChild(value)
				continue
			}
		}
		if err := cleanNavigation(value, pages, bookmarks, attachments); err != nil {
			return err
		}
		if value.Tag == "OutlineElem" {
			value.RemoveAttr("Count")
			if child(value, "Actions") == nil && len(children(value, "OutlineElem")) == 0 {
				element.RemoveChild(value)
			}
		}
		if (value.Tag == "Actions" || value.Tag == "Outlines" || value.Tag == "Bookmarks") && len(value.ChildElements()) == 0 {
			element.RemoveChild(value)
		}
	}
	return nil
}

func parseGroup(spec string, maxPages int) ([]int, error) {
	var pages []int
	seen := make(map[int]bool)
	for token := range strings.SplitSeq(spec, ",") {
		bounds := strings.Split(strings.TrimSpace(token), "-")
		if len(bounds) < 1 || len(bounds) > 2 {
			return nil, invalid("页码范围无效: %q", token)
		}
		start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		if err != nil || start < 1 {
			return nil, invalid("页码无效: %q", token)
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil || end < start {
				return nil, invalid("页码范围无效: %q", token)
			}
		}
		if end-start >= maxPages-len(pages) {
			return nil, invalid("分卷页数超过上限")
		}
		for page := start; ; page++ {
			if seen[page] {
				return nil, invalid("分卷内页码重复: %d", page)
			}
			seen[page] = true
			pages = append(pages, page)
			if page == end {
				break
			}
		}
	}
	return pages, nil
}
