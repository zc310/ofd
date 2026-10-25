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
	"golang.org/x/image/font/sfnt"
)

// 注解在 flow 字体清理前参与引用收集，专用字体必须保留并写入实际嵌入字形。
func TestFlowManifestKeepsFontUsedOnlyByAnnotationsInBothFormats(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			const annotationText = "XYZ789"
			encoded := base64.StdEncoding.EncodeToString(goregular.TTF)
			m := Manifest{
				Version: 1,
				Resources: Resources{Fonts: []Font{
					{Name: "AnnotationOnly", FontName: "Annotation Face", DataBase64: encoded},
					{Name: "Unused", DataBase64: encoded},
				}},
				Flow: &Flow{Blocks: []FlowBlock{{Text: "Body"}}},
				Document: Document{Annotations: []AnnotationPage{{Page: 0, Items: []Annotation{{
					ID: 900, Type: "Stamp", Creator: "test", LastModDate: "2026-10-25",
					Boundary: &Box{X: 20, Y: 20, Width: 40, Height: 10},
					Items:    []Item{{Type: "text", Font: "AnnotationOnly", Value: annotationText, Size: 4, Width: 40, Height: 10}},
				}}}}},
			}
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
			input := filepath.Join(t.TempDir(), "annotation."+format)
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
			found := false
			for _, resource := range document.Fonts {
				if resource.Name == "Unused" {
					t.Fatal("未使用字体未被清理")
				}
				if resource.Name == "AnnotationOnly" {
					found = len(resource.Data) > 0 && resource.FontName == "Annotation Face"
				}
			}
			if !found {
				t.Fatal("仅由注解引用的嵌入字体丢失")
			}
			if len(document.Annotations) != 1 || len(document.Annotations[0].Items) != 1 {
				t.Fatalf("注解丢失或重复: %+v", document.Annotations)
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
			for _, resource := range ofd.Documents[0].Fonts() {
				if resource.FontName != "Annotation Face" {
					continue
				}
				if resource.FontFile.String() == "" {
					t.Fatal("注解字体被写成未嵌入的逻辑字体")
				}
				embedded, err := ofd.Documents[0].FileCache.Read(resource.FontFile.String())
				if err != nil {
					t.Fatal(err)
				}
				face, err := sfnt.Parse(embedded)
				if err != nil {
					t.Fatal(err)
				}
				var buffer sfnt.Buffer
				for _, r := range annotationText {
					glyph, err := face.GlyphIndex(&buffer, r)
					if err != nil || glyph == 0 {
						t.Fatalf("嵌入的注解字体缺少字符 %q: %v", r, err)
					}
				}
				return
			}
			t.Fatal("OFD 中未找到注解专用字体")
		})
	}
}
