package creator

import (
	"bytes"
	"errors"
	"strings"

	"github.com/knroy/go-xml/v2/xdm"
)

// 原始资源继续使用与 XSD 校验相同的解析器。生成路径只使用 xmlcodec。
func parseXMLRoot(data []byte) (*xdm.Node, error) {
	tree, err := xdm.Parse(bytes.NewReader(data), xdm.ParseOptions{MaxBytes: -1, MaxNodes: -1, MaxDepth: -1})
	if err != nil {
		return nil, err
	}
	if tree.Root == nil {
		return nil, errors.New("XML 文档根元素为空")
	}
	roots := tree.Root.ChildElements()
	if len(roots) != 1 {
		return nil, errors.New("XML 文档必须且只能包含一个根元素")
	}
	return roots[0], nil
}

// xmlElements 只匹配直接子元素组成的固定路径，不执行 XPath 查询。
func xmlElements(root *xdm.Node, path string) []*xdm.Node {
	nodes := []*xdm.Node{root}
	for _, local := range strings.Split(path, "/") {
		var next []*xdm.Node
		for _, node := range nodes {
			for child := range node.Children() {
				if child.Kind() == xdm.KindElement && child.Name().Local == local {
					next = append(next, child)
				}
			}
		}
		nodes = next
	}
	return nodes
}

func xmlChild(node *xdm.Node, local string) *xdm.Node {
	for child := range node.Children() {
		if child.Kind() == xdm.KindElement && child.Name().Local == local {
			return child
		}
	}
	return nil
}
