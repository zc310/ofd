package creator

import (
	"testing"

	"github.com/zc310/ofd/internal/models"
	"golang.org/x/image/font/gofont/goregular"
)

// 同一实际字型名允许对应多个独立资源，文字引用须按别名分配不同的 ID。
func TestFontNameIsIndependentOfResourceAlias(t *testing.T) {
	document := Document{ID: "font-names", Fonts: []Font{
		{Name: "BodyAlias", FontName: "Go Regular", FamilyName: "Go", Data: goregular.TTF},
		{Name: "OtherAlias", FontName: "Go Regular", FamilyName: "Go", Data: goregular.TTF},
	}, Pages: []Page{{Items: []Item{
		Text{X: 10, Y: 10, Width: 30, Height: 8, Size: 4, Font: "BodyAlias", Value: "A"},
		Text{X: 10, Y: 20, Width: 30, Height: 8, Size: 4, Font: "OtherAlias", Value: "B"},
	}}}}
	data, err := MarshalWithOptions(document, CreateOptions{Deterministic: true})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	defer ofd.Close()
	doc := ofd.Documents[0]
	if len(doc.Fonts()) != 2 {
		t.Fatalf("同名字型资源被合并: %+v", doc.Fonts())
	}
	for _, font := range doc.Fonts() {
		if font.FontName != "Go Regular" || font.FamilyName != "Go" {
			t.Fatalf("实际字型名未写出: %+v", font)
		}
	}
	seen := map[models.StRefID]bool{}
	for _, layer := range doc.Pages[0].Content().Layer {
		for _, item := range layer.Items {
			if item.Text != nil {
				seen[item.Text.Font] = true
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("文字没有引用独立字体资源: %v", seen)
	}
	if document.Fonts[0].Name != "BodyAlias" || document.Fonts[1].Name != "OtherAlias" {
		t.Fatal("输出修改了调用方的资源别名")
	}
	checkGeneratedPackage(t, data)
}
