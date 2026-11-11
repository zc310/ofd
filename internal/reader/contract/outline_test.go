package contract

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

func TestOutlineContractPreservesOptionalValuesAndEmptyLists(t *testing.T) {
	zero, top := 0.0, 4.0
	expanded := false
	tree := reader.OutlineTree{PageMode: "UseOutlines", Nodes: []reader.OutlineNode{{
		Title: "章节", Page: -1, URI: "https://example.com", Expanded: &expanded,
		Dest:     &reader.OutlineDest{Type: "XYZ", Left: &zero, Top: &top},
		Children: []reader.OutlineNode{{Title: "子项", Page: 1}},
	}}, Bookmarks: []reader.Bookmark{{Name: "书签", Page: 1}}}
	data, err := json.Marshal(Outline(tree))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"bookmarks":[{"dest":null,"name":"书签","page":1}],"nodes":[{"children":[{"children":[],"dest":null,"expanded":null,"page":1,"title":"子项","uri":""}],"dest":{"bottom":null,"left":0,"right":null,"top":4,"type":"XYZ","zoom":null},"expanded":false,"page":-1,"title":"章节","uri":"https://example.com"}],"page_mode":"UseOutlines"}`
	var actual, expected any
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("大纲契约 = %s", data)
	}
	empty, err := json.Marshal(Outline(reader.OutlineTree{}))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(empty, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"bookmarks":[],"nodes":[],"page_mode":""}`), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("空大纲 = %s, %v", empty, err)
	}
	if tree.Nodes[0].Children[0].Children != nil {
		t.Fatal("不能修改原大纲的子列表")
	}
}
