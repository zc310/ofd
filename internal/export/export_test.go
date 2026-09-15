package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v3"
)

func TestExportWritesCreatorManifest(t *testing.T) {
	assetRoot := t.TempDir()
	var output bytes.Buffer
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	if err := Export(input, &output, Options{AssetRoot: assetRoot, AssetPrefix: "assets"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "version: 1") || !strings.Contains(output.String(), "pages:") {
		t.Fatalf("exported manifest is missing required fields: %s", output.String())
	}
	manifestPath := filepath.Join(t.TempDir(), "document.yaml")
	if err := os.WriteFile(manifestPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := manifest.Load(manifestPath, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != 1 || len(loaded.Pages) == 0 {
		t.Fatalf("loaded exported manifest = version %d, pages %d", loaded.Version, len(loaded.Pages))
	}
}

func TestExportWritesJSONAndTOMLManifests(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	for _, format := range []string{"json", "toml"} {
		t.Run(format, func(t *testing.T) {
			var output bytes.Buffer
			directory := t.TempDir()
			if err := Export(input, &output, Options{AssetRoot: filepath.Join(directory, "assets"), AssetPrefix: "assets", Format: format}); err != nil {
				t.Fatal(err)
			}
			inputPath := filepath.Join(directory, "document."+format)
			if err := os.WriteFile(inputPath, output.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			loaded, baseDir, err := manifest.Load(inputPath, format)
			if err != nil {
				t.Fatalf("load exported %s manifest: %v\n%s", format, err, output.String())
			}
			if loaded.Version != 1 || len(loaded.Pages) == 0 {
				t.Fatalf("loaded exported manifest = version %d, pages %d", loaded.Version, len(loaded.Pages))
			}
			if _, err := loaded.Build(baseDir, ""); err != nil {
				t.Fatalf("build exported %s manifest: %v", format, err)
			}
		})
	}
}

func TestExportJSONIndentOption(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	for _, indent := range []bool{false, true} {
		t.Run(fmt.Sprintf("indent-%t", indent), func(t *testing.T) {
			var output bytes.Buffer
			if err := Export(input, &output, Options{AssetRoot: t.TempDir(), Format: "json", JSONIndent: indent}); err != nil {
				t.Fatal(err)
			}
			if indent && !bytes.Contains(output.Bytes(), []byte("\n  \"")) {
				t.Fatal("indented JSON does not contain object indentation")
			}
			if !indent && bytes.Count(output.Bytes(), []byte("\n")) != 1 {
				t.Fatalf("compact JSON contains unexpected newlines: %q", output.Bytes())
			}
		})
	}
}

func TestExportAllWritesJSONAndTOMLIndexes(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd")
	for _, format := range []string{"json", "toml"} {
		t.Run(format, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "bundle")
			if err := ExportAllWithFormat(input, root, format); err != nil {
				t.Fatal(err)
			}
			indexData, err := os.ReadFile(filepath.Join(root, "index."+format))
			if err != nil {
				t.Fatal(err)
			}
			var index BundleIndex
			if format == "json" {
				if err := json.Unmarshal(indexData, &index); err != nil {
					t.Fatal(err)
				}
			} else if _, err := toml.Decode(string(indexData), &index); err != nil {
				t.Fatal(err)
			}
			if index.Version != 1 || len(index.Documents) < 2 || index.Documents[0].AssetRoot == "" {
				t.Fatalf("bundle index = %+v", index)
			}
		})
	}
}

func TestExportRejectsMultipleDocumentBodies(t *testing.T) {
	var output bytes.Buffer
	err := Export(filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), &output, Options{AssetRoot: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "多个文档体") {
		t.Fatalf("Export error = %v, want multiple-document error", err)
	}
}

func TestExportDocumentSelectsDocumentBody(t *testing.T) {
	var output bytes.Buffer
	input := filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd")
	if err := ExportDocument(input, 1, &output, Options{AssetRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("selected document export is empty")
	}
	manifestPath := filepath.Join(t.TempDir(), "document.yaml")
	if err := os.WriteFile(manifestPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := manifest.Load(manifestPath, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != 1 || len(loaded.Pages) == 0 {
		t.Fatalf("selected document manifest = version %d, pages %d", loaded.Version, len(loaded.Pages))
	}
}

func TestExportDocumentRejectsInvalidIndex(t *testing.T) {
	var output bytes.Buffer
	err := ExportDocument(filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), 99, &output, Options{AssetRoot: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "索引超出范围") {
		t.Fatalf("ExportDocument error = %v, want index error", err)
	}
}

func TestExportAllWritesIndependentManifests(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bundle")
	input := filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd")
	if err := ExportAll(input, root); err != nil {
		t.Fatal(err)
	}
	indexData, err := os.ReadFile(filepath.Join(root, "index.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var index BundleIndex
	if err := yaml.Unmarshal(indexData, &index); err != nil {
		t.Fatal(err)
	}
	if index.Version != 1 || len(index.Documents) < 2 {
		t.Fatalf("bundle index = version %d, documents %d", index.Version, len(index.Documents))
	}
	for _, entry := range index.Documents {
		manifestPath := filepath.Join(root, filepath.FromSlash(entry.Manifest))
		loaded, baseDir, err := manifest.Load(manifestPath, "yaml")
		if err != nil {
			t.Fatalf("load document %d: %v", entry.Index, err)
		}
		if _, err := loaded.Build(baseDir, ""); err != nil {
			t.Fatalf("build document %d: %v", entry.Index, err)
		}
		if entry.Pages == 0 || entry.AssetRoot == "" {
			t.Fatalf("bundle entry %d is incomplete: %+v", entry.Index, entry)
		}
	}
}

func TestExportAllRejectsExistingOutputDirectory(t *testing.T) {
	root := t.TempDir()
	err := ExportAll(filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), root)
	if err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("ExportAll error = %v, want existing-directory error", err)
	}
}

func TestUniqueFontName(t *testing.T) {
	exporter := &documentExporter{fontNames: map[string]bool{"HelveticaNeue": true}}
	name := exporter.uniqueFontName("HelveticaNeue", 131)
	if name == "HelveticaNeue" || !strings.HasPrefix(name, "HelveticaNeue-") {
		t.Fatalf("unique font name = %q", name)
	}
}

func TestExportPreservesTemplatesAndActions(t *testing.T) {
	top := 12.5
	data, err := creator.Marshal(creator.Document{
		ID:      "template-action-test",
		Actions: []creator.Action{{Event: creator.ActionEventDO, URI: &creator.URIAction{URI: "https://example.com/document"}}},
		Templates: []creator.TemplatePage{{
			ID: 10, Name: "Header", ZOrder: "Background",
			Items: []creator.Item{creator.Path{X: 1, Y: 1, Width: 20, Height: 4, Data: "M 0 0 L 20 0 L 20 4 C", Fill: true}},
		}},
		Pages: []creator.Page{
			{Templates: []creator.TemplateRef{{ID: 10, ZOrder: "Background"}}, Actions: []creator.Action{{Event: creator.ActionEventPO, Goto: &creator.GotoAction{Page: 1, Type: "FitH", Top: &top}}}, Items: []creator.Item{creator.Path{X: 5, Y: 5, Width: 10, Height: 10, Data: "M 0 0 L 10 10", Actions: []creator.Action{{URI: &creator.URIAction{URI: "https://example.com/page"}}}}}},
			{Items: []creator.Item{creator.Path{X: 1, Y: 1, Width: 5, Height: 5, Data: "M 0 0 L 5 5"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Export(data, &output, Options{AssetRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "document.yaml")
	if err := os.WriteFile(manifestPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, baseDir, err := manifest.Load(manifestPath, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Templates) != 1 || len(loaded.Pages) != 2 || len(loaded.Pages[0].Templates) != 1 {
		t.Fatalf("template export = templates %d, pages %d, refs %d", len(loaded.Templates), len(loaded.Pages), len(loaded.Pages[0].Templates))
	}
	if len(loaded.Document.Actions) != 1 || len(loaded.Pages[0].Actions) != 1 || len(loaded.Pages[0].Layers) != 1 || len(loaded.Pages[0].Layers[0].Items) != 1 || len(loaded.Pages[0].Layers[0].Items[0].Actions) != 1 {
		t.Fatal("document, page, or item actions were not exported")
	}
	if _, err := loaded.Build(baseDir, ""); err != nil {
		t.Fatalf("build exported template/action manifest: %v", err)
	}
}

func TestExportPreservesDocumentMetadata(t *testing.T) {
	hideToolbar := true
	pageMode := "UseOutlines"
	pageLayout := "OneColumn"
	zoomMode := "FitWidth"
	edit := false
	data, err := creator.Marshal(creator.Document{
		ID: "metadata-test", Title: "Metadata", Author: "Author", Subject: "Subject", Abstract: "Abstract",
		DocUsage: "electronic", Keywords: []string{"one", "two"},
		CustomDatas: []creator.CustomData{{Name: "department", Value: "engineering"}},
		Preferences: &creator.ViewPreferences{PageMode: pageMode, PageLayout: pageLayout, HideToolbar: &hideToolbar, ZoomMode: zoomMode},
		Permissions: &creator.Permissions{Edit: &edit},
		Pages:       []creator.Page{{Items: []creator.Item{creator.Path{X: 1, Y: 1, Width: 5, Height: 5, Data: "M 0 0 L 5 5"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Export(data, &output, Options{AssetRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "document.yaml")
	if err := os.WriteFile(manifestPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, baseDir, err := manifest.Load(manifestPath, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Document.Title != "Metadata" || loaded.Document.Author != "Author" || len(loaded.Document.Keywords) != 2 || len(loaded.Document.CustomData) != 1 {
		t.Fatalf("document metadata was not preserved: %+v", loaded.Document)
	}
	if loaded.Document.Preferences == nil || loaded.Document.Preferences.PageMode != pageMode || loaded.Document.Preferences.PageLayout != pageLayout || loaded.Document.Preferences.HideToolbar == nil || !*loaded.Document.Preferences.HideToolbar || loaded.Document.Preferences.ZoomMode != zoomMode {
		t.Fatalf("document preferences were not preserved: %+v", loaded.Document.Preferences)
	}
	if loaded.Document.Permissions == nil || loaded.Document.Permissions.Edit == nil || *loaded.Document.Permissions.Edit {
		t.Fatalf("document permissions were not preserved: %+v", loaded.Document.Permissions)
	}
	if _, err := loaded.Build(baseDir, ""); err != nil {
		t.Fatalf("build exported metadata manifest: %v", err)
	}
}

func TestExportPreservesPageResourceXMLAndFiles(t *testing.T) {
	rawResource := []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><Fonts><Font ID="9001" FontName="PageFont"><FontFile>font.bin</FontFile></Font></Fonts></Res>`)
	source, err := creator.Marshal(creator.Document{
		ID: "page-resource-export",
		Pages: []creator.Page{
			{
				Resources: []creator.PageResource{
					{
						Data:  rawResource,
						Files: []creator.PageResourceFile{{Path: "font.bin", Data: []byte("font")}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assetRoot := t.TempDir()
	var output bytes.Buffer
	if err := Export(source, &output, Options{AssetRoot: assetRoot}); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "document.yaml")
	if err := os.WriteFile(manifestPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	value, baseDir, err := manifest.Load(manifestPath, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Pages) != 1 || len(value.Pages[0].Resources) != 1 {
		t.Fatalf("page resources = %+v", value.Pages)
	}
	resource := value.Pages[0].Resources[0]
	if resource.File == "" || len(resource.Files) != 1 || resource.Files[0].Path != "font.bin" {
		t.Fatalf("exported page resource = %+v", resource)
	}
	document, err := value.Build(baseDir, assetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Pages[0].Resources) != 1 || string(document.Pages[0].Resources[0].Data) == "" || len(document.Pages[0].Resources[0].Files) != 1 {
		t.Fatalf("built page resource was not preserved: %+v", document.Pages[0].Resources)
	}
}
