package export

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

// 同名字型的不同子集需生成不同的清单别名，同时保留相同的实际 FontName。
func TestExportDuplicateFontNamesPreservesNamesAndReferences(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "same-font-names", Fonts: []creator.Font{
		{Name: "AliasA", FontName: "Go Regular", FamilyName: "Go", Data: goregular.TTF},
		{Name: "AliasB", FontName: "Go Regular", FamilyName: "Go", Data: goregular.TTF},
	}, Pages: []creator.Page{{Items: []creator.Item{
		creator.Text{X: 10, Y: 10, Width: 30, Height: 8, Size: 4, Font: "AliasA", Value: "A"},
		creator.Text{X: 10, Y: 20, Width: 30, Height: 8, Size: 4, Font: "AliasB", Value: "B"},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "same-names.ofd")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			var output bytes.Buffer
			if err := WriteManifest(input, &output, Options{Format: format, AssetRoot: filepath.Join(root, "assets"), AssetPrefix: "assets"}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "document."+format)
			if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			m, base, err := manifest.Load(path, format)
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Resources.Fonts) != 2 || m.Resources.Fonts[0].Name == m.Resources.Fonts[1].Name {
				t.Fatal("字体资源别名没有去重")
			}
			for _, font := range m.Resources.Fonts {
				if font.Name != "Go Regular" && font.FontName != "Go Regular" {
					t.Fatal("别名追加后缀时丢失了实际字型名")
				}
			}
			document, err := m.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			rebuilt, err := creator.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(rebuilt)
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			doc := ofd.Documents[0]
			for _, font := range doc.Fonts() {
				if font.FontName != "Go Regular" || font.FamilyName != "Go" {
					t.Fatalf("导出重建改写了实际字型名: %+v", font)
				}
			}
			page := doc.Pages[0]
			if err := page.EnsureLoaded(); err != nil {
				t.Fatal(err)
			}
			ids := map[uint64]bool{}
			for _, layer := range page.Content().Layer {
				for _, item := range layer.Items {
					if item.Text != nil {
						ids[uint64(item.Text.Font)] = true
					}
				}
			}
			if len(ids) != 2 {
				t.Fatalf("导出重建合并了独立的字体引用: %v", ids)
			}
		})
	}
}
