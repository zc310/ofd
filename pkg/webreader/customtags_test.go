package webreader

import (
	"os"
	"path/filepath"
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
	invoiceCode := findCustomTag(tags[0].Root, "InvoiceCode")
	if invoiceCode == nil || len(invoiceCode.Children) != 1 || invoiceCode.Children[0].Target == nil {
		t.Fatalf("invoice code = %+v", invoiceCode)
	}
	target := invoiceCode.Children[0].Target
	if target.Page != 0 || target.PageID != 10 || target.ObjectID != 69 || target.ObjectText != "050001700111" {
		t.Fatalf("target = %+v", target)
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
