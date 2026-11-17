package reader

import (
	"slices"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestNestedPageItemWalkPreservesOrderAndFirstLink(t *testing.T) {
	text := &spec.TextObject{ID: 1}
	text.Boundary = spec.StBox{Width: 10, Height: 10}
	text.Actions = &spec.Actions{Action: []spec.Action{{Event: spec.ActionEventClick, URI: &spec.ActionURI{URI: "https://example.test/first"}}}}
	leaf := spec.PageItem{Kind: spec.PageItemText, Text: text}
	items := []spec.PageItem{leaf} //nolint:prealloc // 每次加深嵌套都会替换切片，只在最终层追加兄弟图元。
	for i := 0; i < 20000; i++ {
		items = []spec.PageItem{{Kind: spec.PageItemBlock, Block: &spec.PageBlock{CTPageBlock: spec.CTPageBlock{Items: items}}}}
	}
	last := *text
	last.ID = 2
	items = append(items, spec.PageItem{Kind: spec.PageItemText, Text: &last})
	var ids []string
	collectPageBlockLinks(items, nil, nil, func(link PageLink) { ids = append(ids, link.ID) })
	if !slices.Equal(ids, []string{"1", "2"}) {
		t.Fatalf("嵌套图元顺序错误：%v", ids)
	}
	uri, _, _, found := pageBlockLinkAction(items, nil, nil)
	if !found || uri != "https://example.test/first" {
		t.Fatalf("没有找到首个嵌套链接：uri=%q found=%v", uri, found)
	}
}

func TestOutlineConversionHandlesDeepTreesWithoutRecursion(t *testing.T) {
	var elems []spec.OutlineElem
	for i := 0; i < 20000; i++ {
		elems = []spec.OutlineElem{{Title: "nested", OutlineElem: elems}}
	}
	nodes := outlineNodes(elems, func(spec.Dest) int { return -1 }, nil)
	depth := 0
	for len(nodes) != 0 {
		if len(nodes) != 1 || nodes[0].Title != "nested" {
			t.Fatal("大纲内容被改写")
		}
		depth++
		nodes = nodes[0].Children
	}
	if depth != 20000 {
		t.Fatalf("大纲被截断：深度=%d", depth)
	}
}
