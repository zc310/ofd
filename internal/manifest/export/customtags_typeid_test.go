package export

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

func TestExportCustomTagsPreservesTypeIDAcrossJSONAndYAMLRoundTrips(t *testing.T) {
	tags := []creator.CustomTag{
		{TypeID: "cert", DataName: "cert.xml", Data: []byte("<Certificate>证照数据</Certificate>")},
		{TypeID: "cert", NameSpace: "urn:cert", DataName: "both.xml", Data: []byte("<Certificate/>")},
		{NameSpace: "urn:other", DataName: "other.xml", Data: []byte("<Other/>")},
	}
	input, err := creator.Marshal(creator.Document{ID: "round-trip-cert", CustomTags: tags, Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			directory := t.TempDir()
			var output bytes.Buffer
			if err := WriteManifest(input, &output, Options{AssetRoot: filepath.Join(directory, "assets"), AssetPrefix: "assets", Format: format}); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(directory, "document."+format)
			if err := os.WriteFile(filename, output.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, baseDir, err := manifest.Load(filename, format)
			if err != nil {
				t.Fatal(err)
			}
			document, err := loaded.Build(baseDir, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(document.CustomTags) != len(tags) {
				t.Fatalf("标引数量改变: %+v", document.CustomTags)
			}
			for index, want := range tags {
				got := document.CustomTags[index]
				if got.TypeID != want.TypeID || got.NameSpace != want.NameSpace || !bytes.Equal(got.Data, want.Data) {
					t.Fatalf("标引属性或数据未保留: %+v", got)
				}
			}
			rebuilt, err := creator.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			instance, err := validator.New(validator.WithCustomTagTypeID(true))
			if err != nil {
				t.Fatal(err)
			}
			report := instance.ValidateReader(t.Context(), bytes.NewReader(rebuilt), "rebuilt.ofd")
			if report.HasErrors() || report.Summary.Warnings != 0 {
				t.Fatalf("重建产物未通过标引校验: %+v", report.Issues)
			}
			ofd, err := parser.NewOFD(rebuilt)
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			result, err := ofd.Documents[0].LoadCustomTags()
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || len(result.CustomTags) != len(tags) || result.CustomTags[0].TypeID != "cert" || result.CustomTags[0].NameSpace != "" {
				t.Fatalf("重建后 TypeID 被替换为命名空间: %+v", result)
			}
		})
	}
}
