package canvasfont

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	fontfile "github.com/zc310/ofd/internal/thirdparty/font"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// 原生粗体仅覆盖部分字符时，每个对象必须独立选择，不能缓存首段文字的覆盖决策。
func TestSystemObjectStyleSelectionDoesNotDependOnTextOrder(t *testing.T) {
	if !systemFontLookupUsable() {
		t.Skip("该平台不枚举系统字体")
	}
	bold, err := fontfile.ParseSFNT(bytes.Clone(gobold.TTF), 0)
	if err != nil {
		t.Fatal(err)
	}
	subset, err := bold.Subset([]uint16{0, bold.GlyphIndex('A')}, fontfile.SubsetOptions{Tables: fontfile.KeepMinTables})
	if err != nil {
		t.Fatal(err)
	}
	// 子集化默认移除名称；模拟仍属于 Go 字体族、但覆盖范围较小的系统粗体。
	subset.Tables["name"] = bold.Tables["name"]
	boldData := subset.Write()
	dir := t.TempDir()
	for name, data := range map[string][]byte{"regular.ttf": goregular.TTF, "bold.ttf": boldData} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := canvas.CacheSystemFonts(filepath.Join(dir, "index.gob"), []string{dir}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := canvas.CacheSystemFonts(filepath.Join(t.TempDir(), "restore.gob"), nil); err != nil {
			t.Error(err)
		}
	})
	model, err := creator.Marshal(creator.Document{
		ID:    "system-style-text-order",
		Fonts: []creator.Font{{Name: "Logical", FontName: "Go-Regular", FamilyName: "Go"}},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{Font: "Logical", Size: 4, Width: 30, Height: 6, Value: "AΩ"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(model)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	for _, values := range [][]string{{"A", "Ω", "A"}, {"Ω", "A", "Ω"}} {
		t.Run(values[0], func(t *testing.T) {
			fonts := NewFonts(ofd.Documents[0])
			defer fonts.Close()
			var id spec.StRefID
			fonts.ForEachFont(func(key spec.StID, _ *spec.Font) bool {
				id = spec.StRefID(key)
				return false
			})
			base, err := fonts.LoadFont(id)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range values {
				object := textObjectWith(value)
				object.Font, object.Weight = id, 700
				face, actual := fonts.FaceObject(base, object, nil)
				resolved, ok := face.(canvasFontFace)
				if !ok {
					t.Fatal("没有返回 canvas 字体面")
				}
				want := canvasFamily(base).Face(12).Font.Tables["glyf"]
				if value == "A" {
					want = subset.Tables["glyf"]
					if resolved.face.FauxBold != 0 {
						t.Fatal("先前缺字对象阻止了原生粗体选择")
					}
				}
				if !bytes.Equal(resolved.face.Font.Tables["glyf"], want) || resolved.GlyphIndex([]rune(value)[0]) == 0 {
					t.Fatalf("文字 %q 沿用了其他对象的字体选择", value)
				}
				rebuilt, _ := fonts.FaceObject(actual, object, nil)
				if face.TextWidth(value) != rebuilt.TextWidth(value) ||
					face.ToPath(value).String() != rebuilt.ToPath(value).String() {
					t.Fatalf("文字 %q 重建后度量或轮廓不一致", value)
				}
			}
		})
	}
}
