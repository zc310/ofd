package split

import (
	"bytes"
	"context"
	"sort"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/xmlcodec"
)

type resourceDefinition struct {
	node *etree.Element
	kind string
}
type resourceReference struct {
	id   uint64
	kind string
}
type resources struct {
	session     *Session
	overrides   map[string][]byte
	clean       func(*etree.Element) error
	indexes     map[string]*etree.Document
	definitions map[uint64]resourceDefinition
	templates   map[uint64]bool
	pages       map[string]bool
	roots       []*etree.Element
}

func newResources(s *Session, overrides map[string][]byte, clean func(*etree.Element) error) *resources {
	return &resources{session: s, overrides: overrides, clean: clean, indexes: make(map[string]*etree.Document), definitions: make(map[uint64]resourceDefinition), templates: make(map[uint64]bool), pages: make(map[string]bool)}
}

// put 仅覆盖真正发生结构变化的 XML；未修改的页面直接复制原始字节。
func (r *resources) put(name string, tree *etree.Document) error {
	data, err := xmlcodec.Marshal(tree)
	if err != nil {
		return err
	}
	if original := r.session.xml[name]; original != nil {
		before, err := xmlcodec.Marshal(original)
		if err != nil {
			return err
		}
		if bytes.Equal(before, data) {
			return nil
		}
	}
	r.overrides[name] = data
	return nil
}

func (r *resources) addIndex(name string) error {
	if r.indexes[name] != nil {
		return nil
	}
	original, err := r.session.readXML(name, "Res")
	if err != nil {
		return err
	}
	tree := original.Copy()
	if err := r.clean(tree.Root()); err != nil {
		return err
	}
	r.indexes[name] = tree
	groups := map[string]string{"Fonts": "Font", "DrawParams": "DrawParam", "ColorSpaces": "ColorSpace", "MultiMedias": "MultiMedia", "CompositeGraphicUnits": "CompositeGraphicUnit"}
	for _, group := range tree.Root().ChildElements() {
		kind, known := groups[group.Tag]
		if !known || group.NamespaceURI() != xmlcodec.Namespace {
			return invalid("资源 %s 包含未知资源组 %s", name, group.Tag)
		}
		for _, node := range group.ChildElements() {
			if node.NamespaceURI() != xmlcodec.Namespace || node.Tag != kind {
				return invalid("资源 %s 包含未知命名空间", name)
			}
			id, err := numeric(node.SelectAttrValue("ID", ""))
			if err != nil {
				return err
			}
			if _, exists := r.definitions[id]; exists {
				return invalid("资源 ID %d 重复，无法确定拆分依赖", id)
			}
			r.definitions[id] = resourceDefinition{node: node, kind: kind}
		}
	}
	return nil
}

func (r *resources) addPage(ctx context.Context, name string, common *etree.Element, docPath string, stack map[string]bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if stack[name] {
		return invalid("模板页循环引用: %s", name)
	}
	if r.pages[name] {
		return nil
	}
	if len(stack) >= 256 {
		return invalid("模板引用深度超过上限 256")
	}
	if stack == nil {
		stack = make(map[string]bool)
	}
	stack[name] = true
	defer delete(stack, name)
	original, err := r.session.readXML(name, "Page")
	if err != nil {
		return err
	}
	tree := original.Copy()
	if err := r.clean(tree.Root()); err != nil {
		return err
	}
	for _, value := range children(tree.Root(), "PageRes") {
		file, err := resolve(name, value.Text())
		if err != nil {
			return err
		}
		if err := r.addIndex(file); err != nil {
			return err
		}
	}
	for _, value := range children(tree.Root(), "Template") {
		id, err := numeric(value.SelectAttrValue("TemplateID", ""))
		if err != nil {
			return err
		}
		var definition *etree.Element
		for _, candidate := range children(common, "TemplatePage") {
			candidateID, err := numeric(candidate.SelectAttrValue("ID", ""))
			if err != nil {
				return err
			}
			if candidateID == id {
				if definition != nil {
					return invalid("模板 ID %d 重复", id)
				}
				definition = candidate
			}
		}
		if definition == nil {
			return invalid("引用的模板 %d 不存在", id)
		}
		r.templates[id] = true
		file, err := resolve(docPath, definition.SelectAttrValue("BaseLoc", ""))
		if err != nil {
			return err
		}
		if err := r.addPage(ctx, file, common, docPath, stack); err != nil {
			return err
		}
	}
	r.pages[name] = true
	r.roots = append(r.roots, tree.Root())
	return r.put(name, tree)
}

// §7.9、§8 的资源引用按对象 ID 求传递闭包；随后由 validator 计算文件路径闭包。
// 递归遍历覆盖图案、裁剪、渐变颜色、复合图元和声音/影片动作。
func resourceReferences(root *etree.Element) ([]resourceReference, error) {
	var result []resourceReference
	var visit func(*etree.Element) error
	add := func(value, kind string) error {
		id, err := numeric(value)
		if err != nil {
			return err
		}
		result = append(result, resourceReference{id: id, kind: kind})
		return nil
	}
	visit = func(node *etree.Element) error {
		if node.NamespaceURI() != xmlcodec.Namespace {
			return nil
		}
		for _, attr := range node.Attr {
			if attr.Space != "" {
				continue
			}
			kind := ""
			switch attr.Key {
			case "Font":
				kind = "Font"
			case "DrawParam":
				kind = "DrawParam"
			case "ColorSpace":
				kind = "ColorSpace"
			case "Relative":
				if node.Tag == "DrawParam" {
					kind = "DrawParam"
				}
			case "Substitution", "ImageMask", "Thumbnail":
				kind = "MultiMedia"
			case "ResourceID":
				kind = "MultiMedia"
				if node.Tag == "CompositeObject" {
					kind = "CompositeGraphicUnit"
				}
			}
			if kind != "" {
				if err := add(attr.Value, kind); err != nil {
					return err
				}
			}
		}
		switch node.Tag {
		case "DefaultCS":
			if err := add(node.Text(), "ColorSpace"); err != nil {
				return err
			}
		case "Thumbnail", "Substitution":
			if err := add(node.Text(), "MultiMedia"); err != nil {
				return err
			}
		}
		for _, value := range node.ChildElements() {
			if err := visit(value); err != nil {
				return err
			}
		}
		return nil
	}
	err := visit(root)
	return result, err
}

func (r *resources) prune(ctx context.Context) error {
	state := make(map[uint64]uint8)
	var include func(resourceReference, int) error
	include = func(ref resourceReference, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		definition, exists := r.definitions[ref.id]
		if !exists || definition.kind != ref.kind {
			return invalid("资源 %d 缺失或类型不匹配（需要 %s）", ref.id, ref.kind)
		}
		if state[ref.id] == 1 {
			return invalid("资源 %d 存在循环引用", ref.id)
		}
		if state[ref.id] == 2 {
			return nil
		}
		if depth >= 256 {
			return invalid("资源引用深度超过上限 256")
		}
		state[ref.id] = 1
		refs, err := resourceReferences(definition.node)
		if err != nil {
			return err
		}
		for _, next := range refs {
			if err := include(next, depth+1); err != nil {
				return err
			}
		}
		state[ref.id] = 2
		return nil
	}
	for _, root := range r.roots {
		refs, err := resourceReferences(root)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			if err := include(ref, 0); err != nil {
				return err
			}
		}
	}
	names := make([]string, 0, len(r.indexes))
	for name := range r.indexes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tree := r.indexes[name]
		for _, group := range tree.Root().ChildElements() {
			for _, value := range group.ChildElements() {
				id, err := numeric(value.SelectAttrValue("ID", ""))
				if err != nil {
					return err
				}
				if state[id] != 2 {
					group.RemoveChild(value)
				}
			}
			if len(group.ChildElements()) == 0 {
				tree.Root().RemoveChild(group)
			}
		}
		if err := r.put(name, tree); err != nil {
			return err
		}
	}
	return nil
}
