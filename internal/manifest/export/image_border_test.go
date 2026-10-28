package export

import (
	"bytes"
	"image"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

// YAML/JSON 的空值清理和重建均须保留显式零线宽，不能将其变回默认边框。
func TestImageBorderWidthSurvivesManifestRoundTrip(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		width *float64
	}{
		{"缺省", nil}, {"关闭", new(0.0)}, {"正线宽", new(0.5)},
	} {
		data, err := creator.Marshal(creator.Document{ID: "border-manifest", Pages: []creator.Page{{Items: []creator.Item{
			creator.Image{Width: 10, Height: 10, Data: encoded.Bytes(), Format: "PNG", Border: &creator.ImageBorder{LineWidth: tc.width}},
		}}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"json", "yaml"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				root := t.TempDir()
				var output bytes.Buffer
				if err := WriteManifest(data, &output, Options{AssetRoot: filepath.Join(root, "assets"), AssetPrefix: "assets", Format: format}); err != nil {
					t.Fatal(err)
				}
				value, err := manifest.Parse(output.Bytes(), format)
				if err != nil {
					t.Fatal(err)
				}
				document, err := value.Build(root, "")
				if err != nil {
					t.Fatal(err)
				}
				roundtrip, err := creator.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				ofd, err := parser.NewOFD(roundtrip)
				if err != nil {
					t.Fatal(err)
				}
				defer ofd.Close()
				var border *models.Border
				for _, layer := range ofd.Documents[0].Pages[0].Content().Layer {
					for _, item := range layer.Items {
						if item.Image != nil {
							border = item.Image.Border
						}
					}
				}
				if border == nil {
					t.Fatal("清单往返丢失图片边框")
				}
				width := border.LineWidth
				if (width == nil) != (tc.width == nil) || (width != nil && *width != *tc.width) {
					t.Fatalf("%s 往返改变了线宽语义: %s", format, output.String())
				}
			})
		}
	}
}
