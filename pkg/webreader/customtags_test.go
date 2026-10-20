package webreader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomTagsResolvesObjectRefs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofdrw", "999.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tags, err := reader.CustomTags()
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Root == nil || tags[0].Root.Name != "eInvoice" {
		t.Fatalf("tags = %+v", tags)
	}
	again, err := reader.CustomTags()
	if err != nil || len(again) != 1 || again[0].Root != tags[0].Root {
		t.Fatal("重复读取自定义标签没有复用只读解析树")
	}
	invoiceCode := findCustomTag(tags[0].Root, "InvoiceCode")
	if invoiceCode == nil || len(invoiceCode.Children) != 1 || invoiceCode.Children[0].Target == nil {
		t.Fatalf("invoice code = %+v", invoiceCode)
	}
	target := invoiceCode.Children[0].Target
	if target.Page != 0 || target.PageID != 10 || target.ObjectID != 69 || target.ObjectText != "050001700111" {
		t.Fatalf("target = %+v", target)
	}
}

func TestCustomTagFragmentedTextPreservesOnlyDirectText(t *testing.T) {
	const fragments = 20000
	data := []byte(`<root>  ` + strings.Repeat(`abc<!-- split -->`, fragments) + `<child>child text</child><![CDATA[tail]]>  </root>`)
	root, err := parseCustomTag(data)
	if err != nil {
		t.Fatal(err)
	}
	if root.Text != strings.Repeat("abc", fragments)+"tail" || len(root.Children) != 1 || root.Children[0].Text != "child text" {
		t.Fatal("碎片文本或子节点文本的拼接语义改变")
	}
}

func findCustomTag(node *CustomTagNode, name string) *CustomTagNode {
	if node == nil {
		return nil
	}
	if node.Name == name {
		return node
	}
	for _, child := range node.Children {
		if result := findCustomTag(child, name); result != nil {
			return result
		}
	}
	return nil
}
