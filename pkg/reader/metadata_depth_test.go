package reader

import (
	"slices"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestNestedPageItemWalkPreservesOrderAndFirstLink(t *testing.T) {
	text := &models.TextObject{ID: 1}
	text.Boundary = models.StBox{Width: 10, Height: 10}
	text.Actions = &models.Actions{Action: []models.CtAction{{Event: models.ActionEventClick, URI: &models.ActionURI{URI: "https://example.test/first"}}}}
	leaf := models.PageItem{Kind: models.PageItemText, Text: text}
	items := []models.PageItem{leaf}
	for i := 0; i < 20000; i++ {
		items = []models.PageItem{{Kind: models.PageItemBlock, Block: &models.PageBlock{CTPageBlock: models.CTPageBlock{Items: items}}}}
	}
	last := *text
	last.ID = 2
	items = append(items, models.PageItem{Kind: models.PageItemText, Text: &last})
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
	var elems []models.CTOutlineElem
	for i := 0; i < 20000; i++ {
		elems = []models.CTOutlineElem{{Title: "nested", OutlineElem: elems}}
	}
	nodes := outlineNodes(elems, func(models.CtDest) int { return -1 }, nil)
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
