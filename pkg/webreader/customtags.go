package webreader

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

const (
	customTagMaxBytes = 8 << 20
	customTagMaxDepth = 64
	customTagMaxNodes = 100000
)

// CustomTagTree 是一个自定义标签数据文件的树形快照。
type CustomTagTree struct {
	Namespace string
	Path      string
	Root      *CustomTagNode
}

// CustomTagNode 描述自定义 XML 元素。Target 只在识别到 ObjectRef 时存在。
type CustomTagNode struct {
	Name       string
	Namespace  string
	Attributes map[string]string
	Text       string
	Children   []*CustomTagNode
	Target     *CustomTagTarget
}

// CustomTagTarget 是 ObjectRef 对应的页面图元位置。
type CustomTagTarget struct {
	Page       int
	PageID     uint64
	ObjectID   uint64
	X          float64
	Y          float64
	Width      float64
	Height     float64
	ObjectText string
}

// CustomTags 返回文档中的自定义标签树及其可定位的页面对象。
func (r *Reader) CustomTags() ([]CustomTagTree, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil {
		return nil, nil
	}
	result := make([]CustomTagTree, 0)
	for scope, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		value, err := document.LoadCustomTags()
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 个文档体自定义标签失败: %w", scope, err)
		}
		if value == nil || document.Document.CustomTags == nil {
			continue
		}
		listPath := document.Document.CustomTags.Resolve(document.BaseLoc)
		for _, tag := range value.CustomTags {
			path := tag.FileLoc.Resolve(listPath.Dir())
			data, err := document.FileCache.ReadLimit(path.String(), customTagMaxBytes)
			if err != nil {
				return nil, fmt.Errorf("读取自定义标签数据 %s 失败: %w", path, err)
			}
			root, err := parseCustomTag(data)
			if err != nil {
				return nil, fmt.Errorf("解析自定义标签数据 %s 失败: %w", path, err)
			}
			resolveCustomTagTargets(root, r.pages, scope)
			result = append(result, CustomTagTree{Namespace: tag.NameSpace, Path: path.String(), Root: root})
		}
	}
	return result, nil
}

func parseCustomTag(data []byte) (*CustomTagNode, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	var nodes int
	var parse func(xml.StartElement, int) (*CustomTagNode, error)
	parse = func(start xml.StartElement, depth int) (*CustomTagNode, error) {
		if depth > customTagMaxDepth {
			return nil, errors.New("自定义标签嵌套层级过深")
		}
		nodes++
		if nodes > customTagMaxNodes {
			return nil, errors.New("自定义标签元素数量过多")
		}
		node := &CustomTagNode{Name: start.Name.Local, Namespace: start.Name.Space, Attributes: map[string]string{}}
		for _, attr := range start.Attr {
			name := attr.Name.Local
			if attr.Name.Space != "" {
				name = attr.Name.Space + ":" + name
			}
			node.Attributes[name] = attr.Value
		}
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			switch value := token.(type) {
			case xml.StartElement:
				child, err := parse(value, depth+1)
				if err != nil {
					return nil, err
				}
				node.Children = append(node.Children, child)
			case xml.CharData:
				node.Text += string(value)
			case xml.EndElement:
				if value.Name == start.Name {
					node.Text = strings.TrimSpace(node.Text)
					return node, nil
				}
			}
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return nil, errors.New("自定义标签缺少根元素")
			}
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok {
			return parse(start, 1)
		}
	}
}

func resolveCustomTagTargets(root *CustomTagNode, pages []pageRef, scope int) {
	if root == nil {
		return
	}
	var walk func(*CustomTagNode)
	walk = func(node *CustomTagNode) {
		if node.Name == "ObjectRef" {
			pageID, pageOK := parseUint(node.Attributes["PageRef"])
			objectID, objectOK := parseUint(node.Text)
			if pageOK && objectOK {
				for index, ref := range pages {
					if ref.fontScope != scope || uint64(ref.page.ID) != pageID {
						continue
					}
					if target := pageObjectTarget(ref.page, objectID); target != nil {
						target.Page, target.PageID, target.ObjectID = index, pageID, objectID
						node.Target = target
					}
					break
				}
			}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(root)
}

func parseUint(value string) (uint64, bool) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return parsed, err == nil
}

func pageObjectTarget(page *parser.Page, objectID uint64) *CustomTagTarget {
	var result *CustomTagTarget
	var walk func([]models.PageItem)
	walk = func(items []models.PageItem) {
		for _, item := range items {
			if result != nil {
				return
			}
			if item.Kind == models.PageItemBlock && item.Block != nil {
				walk(item.Block.Items)
				continue
			}
			var id uint64
			var box models.StBox
			var text string
			switch item.Kind {
			case models.PageItemText:
				if item.Text == nil {
					continue
				}
				id, box, text = uint64(item.Text.ID), item.Text.Boundary, textValue(item.Text)
			case models.PageItemPath:
				if item.Path == nil {
					continue
				}
				id, box = uint64(item.Path.ID), item.Path.Boundary
			case models.PageItemImage:
				if item.Image == nil {
					continue
				}
				id, box = uint64(item.Image.ID), item.Image.Boundary
			case models.PageItemComposite:
				if item.Composite == nil {
					continue
				}
				id, box = uint64(item.Composite.ID), item.Composite.Boundary
			}
			if id == objectID {
				result = &CustomTagTarget{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height, ObjectText: text}
			}
		}
	}
	_ = page.WithPageContent(func(content *models.PageContent) error {
		if content != nil && content.Content != nil {
			for _, layer := range content.Content.Layer {
				if layer != nil {
					walk(layer.Items)
				}
			}
		}
		return nil
	})
	return result
}
