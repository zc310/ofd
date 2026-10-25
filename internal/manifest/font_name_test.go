package manifest

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v3"
	"golang.org/x/image/font/gofont/goregular"
)

func TestFontNameOverrideLoadsAndWritesInBothFormats(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			m := Manifest{Version: 1, Document: Document{ID: "font-name"}, Resources: Resources{Fonts: []Font{
				{Name: "Alias", FontName: "Explicit Face", FamilyName: "Explicit Family", DataBase64: base64.StdEncoding.EncodeToString(goregular.TTF)},
			}}, Flow: &Flow{Font: "Alias", Blocks: []FlowBlock{{Text: "Text"}}}}
			var data []byte
			var err error
			if format == "json" {
				data, err = json.Marshal(m)
			} else {
				data, err = yaml.Marshal(m)
			}
			if err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(t.TempDir(), "font-name."+format)
			if err := os.WriteFile(input, data, 0600); err != nil {
				t.Fatal(err)
			}
			loaded, base, err := Load(input, format)
			if err != nil {
				t.Fatal(err)
			}
			document, err := loaded.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			font := document.Fonts[len(document.Fonts)-1]
			text := document.Pages[0].Items[0].(creator.Text)
			if font.Name != "Alias" || font.FontName != "Explicit Face" || font.FamilyName != "Explicit Family" || text.Font != "Alias" {
				t.Fatalf("清单丢失实际字型名或内部引用: name=%q font_name=%q family=%q text.Font=%q", font.Name, font.FontName, font.FamilyName, text.Font)
			}
			data, err = creator.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(data)
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			found := false
			for _, resource := range ofd.Documents[0].Fonts() {
				found = found || resource.FontName == "Explicit Face" && resource.FamilyName == "Explicit Family"
			}
			if !found {
				t.Fatal("实际字型名未写入 OFD")
			}
		})
	}
}
