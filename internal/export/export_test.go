package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v3"
)

func TestExportWritesCreatorManifest(t *testing.T) {
	assetRoot := t.TempDir()
	var output bytes.Buffer
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
	if err := WriteManifest(input, &output, Options{AssetRoot: assetRoot, AssetPrefix: "assets"}); err != nil {
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
			if err := WriteManifest(input, &output, Options{AssetRoot: filepath.Join(directory, "assets"), AssetPrefix: "assets", Format: format}); err != nil {
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
			if err := WriteManifest(input, &output, Options{AssetRoot: t.TempDir(), Format: "json", JSONIndent: indent}); err != nil {
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
			if err := WriteBundle(input, root, Options{Format: format}); err != nil {
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
	err := WriteManifest(filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), &output, Options{AssetRoot: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "多个文档体") {
		t.Fatalf("Export error = %v, want multiple-document error", err)
	}
}

func TestExportDocumentSelectsDocumentBody(t *testing.T) {
	var output bytes.Buffer
	input := filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd")
	if err := WriteDocumentManifest(input, 1, &output, Options{AssetRoot: t.TempDir()}); err != nil {
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
	err := WriteDocumentManifest(filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), 99, &output, Options{AssetRoot: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "索引超出范围") {
		t.Fatalf("ExportDocument error = %v, want index error", err)
	}
}

func TestExportAllWritesIndependentManifests(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bundle")
	input := filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd")
	if err := WriteBundle(input, root, Options{Format: "yaml"}); err != nil {
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
	err := WriteBundle(filepath.Join("..", "..", "test", "testdata", "multi_demo.ofd"), root, Options{Format: "yaml"})
	if err == nil || !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("WriteBundle error = %v, want existing-directory error", err)
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
	if err := WriteManifest(data, &output, Options{AssetRoot: t.TempDir()}); err != nil {
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
	if err := WriteManifest(data, &output, Options{AssetRoot: t.TempDir()}); err != nil {
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

func TestExportPreservesGradientColors(t *testing.T) {
	data, err := creator.Marshal(creator.Document{
		ID: "gradient-export-test",
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Path{X: 10, Y: 60, Width: 140, Height: 40, Data: "M 0 0 L 140 0 L 140 40 L 0 40 C", Fill: true, FillColor: &creator.Color{Axial: &creator.AxialShading{
				MapType: "Reflect", MapUnit: 25, StartPoint: "0 0", EndPoint: "25 0",
				Segments: []creator.ColorStop{{Position: 0, Color: creator.Color{R: 255, G: 255, B: 0}}, {Position: 1, Color: creator.Color{R: 0, G: 0, B: 255}}},
			}}},
			creator.Path{X: 10, Y: 90, Width: 60, Height: 45, Data: "M 0 0 L 60 0 L 60 45 L 0 45 C", Fill: true, FillColor: &creator.Color{Radial: &creator.RadialShading{
				StartPoint: "12 21", StartRadius: 3, EndPoint: "42 21", EndRadius: 15, Extend: 2,
				Segments: []creator.ColorStop{{Position: 0, Color: creator.Color{R: 255, G: 255, B: 0}}, {Position: 1, Color: creator.Color{R: 0, G: 0, B: 255}}},
			}}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := WriteManifest(data, &output, Options{AssetRoot: t.TempDir()}); err != nil {
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
	items := loaded.Pages[0].Layers[0].Items
	if len(items) != 2 {
		t.Fatalf("exported gradient items = %d, want 2", len(items))
	}
	axial := items[0].FillColor.Axial
	if axial == nil || axial.MapType != "Reflect" || axial.MapUnit != 25 || axial.StartPoint != "0 0" || axial.EndPoint != "25 0" || len(axial.Segments) != 2 {
		t.Fatalf("axial gradient was not preserved: %+v", items[0].FillColor)
	}
	if axial.Segments[0].Color.R != 255 || axial.Segments[1].Color.B != 255 {
		t.Fatalf("axial segment colors were not preserved: %+v", axial.Segments)
	}
	radial := items[1].FillColor.Radial
	if radial == nil || radial.StartPoint != "12 21" || radial.StartRadius != 3 || radial.EndPoint != "42 21" || radial.EndRadius != 15 || radial.Extend != 2 || len(radial.Segments) != 2 {
		t.Fatalf("radial gradient was not preserved: %+v", items[1].FillColor)
	}
	if _, err := loaded.Build(baseDir, ""); err != nil {
		t.Fatalf("build exported gradient manifest: %v", err)
	}
}

func TestExportPreservesGouraudLaGouraudAndPatternColors(t *testing.T) {
	input := filepath.Join("..", "..", "test", "testdata", "shading.ofd")
	var output bytes.Buffer
	if err := WriteManifest(input, &output, Options{AssetRoot: t.TempDir()}); err != nil {
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
	gouraud, laGouraud, pattern := 0, 0, 0
	for _, page := range loaded.Pages {
		for _, layer := range page.Layers {
			for _, item := range layer.Items {
				if item.FillColor == nil {
					continue
				}
				switch {
				case item.FillColor.Gouraud != nil:
					gouraud++
					if len(item.FillColor.Gouraud.Points) < 3 {
						t.Fatalf("gouraud points lost: %+v", item.FillColor.Gouraud)
					}
					for _, point := range item.FillColor.Gouraud.Points {
						if point.Color.R == 0 && point.Color.G == 0 && point.Color.B == 0 {
							t.Fatalf("gouraud point color lost: %+v", point)
						}
					}
				case item.FillColor.LaGouraud != nil:
					laGouraud++
					if item.FillColor.LaGouraud.VerticesPerRow < 2 || len(item.FillColor.LaGouraud.Points) == 0 {
						t.Fatalf("la_gouraud grid lost: %+v", item.FillColor.LaGouraud)
					}
				case item.FillColor.Pattern != nil:
					pattern++
					value := item.FillColor.Pattern
					if value.Width != 20 || value.Height != 20 || len(value.Items) == 0 {
						t.Fatalf("pattern cell content lost: %+v", value)
					}
					if len(value.Items[0].Data) == 0 {
						t.Fatalf("pattern item data lost: %+v", value.Items[0])
					}
				}
			}
		}
	}
	if gouraud != 3 {
		t.Fatalf("gouraud fills exported = %d, want 3", gouraud)
	}
	if laGouraud != 3 {
		t.Fatalf("la_gouraud fills exported = %d, want 3", laGouraud)
	}
	if pattern != 4 {
		t.Fatalf("pattern fills exported = %d, want 4", pattern)
	}
	if _, err := loaded.Build(baseDir, ""); err != nil {
		t.Fatalf("build exported shading manifest: %v", err)
	}
}

func TestExportPreservesItemClips(t *testing.T) {
	assetRoot := t.TempDir()
	var output bytes.Buffer
	input := filepath.Join("..", "..", "test", "testdata", "intro.ofd")
	if err := WriteManifest(input, &output, Options{AssetRoot: assetRoot}); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(assetRoot, "document.yaml")
	if err := os.WriteFile(manifestPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, baseDir, err := manifest.Load(manifestPath, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	clips, areas := 0, 0
	for _, page := range loaded.Pages {
		for _, layer := range page.Layers {
			for _, item := range layer.Items {
				clips += len(item.Clips)
				for _, clip := range item.Clips {
					areas += len(clip.Areas)
					for _, area := range clip.Areas {
						if area.Path != nil && (area.Path.Data == "" || area.Path.Boundary.Width == 0) {
							t.Fatalf("clip path content lost: %+v", area.Path)
						}
					}
				}
			}
		}
	}
	if clips == 0 || areas < clips {
		t.Fatalf("item clips were not preserved: clips=%d areas=%d", clips, areas)
	}
	if _, err := loaded.Build(baseDir, ""); err != nil {
		t.Fatalf("build exported clips manifest: %v", err)
	}
}

func TestExportPreservesAnnotationsAndVersions(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	visible := true
	notReadOnly := false
	source, err := creator.Marshal(creator.Document{
		ID: "annotation-version-export",
		Versions: []creator.DocumentVersion{{
			ID: "v1", Index: 1, Current: false, Version: "1.0", Name: "审查版本", CreationDate: created,
			Files: []creator.VersionFile{{ID: "F1", Path: "Document.xml"}},
		}},
		Annotations: []creator.AnnotationPage{{
			Page: 0,
			Items: []creator.Annotation{{
				ID: 9001, Type: "Highlight", Creator: "tester", LastModDate: created,
				Visible: &visible, Subtype: "manual", ReadOnlyValue: &notReadOnly,
				Remark:     "需要确认",
				Parameters: []creator.AnnotationParameter{{Name: "author", Value: "tester"}},
				Boundary:   &creator.Box{X: 10, Y: 10, Width: 80, Height: 20},
				Items:      []creator.Item{creator.Path{X: 10, Y: 10, Width: 80, Height: 20, Data: "M 0 0 L 80 0 L 80 20 C"}},
			}},
		}},
		Pages: []creator.Page{{
			Items: []creator.Item{creator.Path{X: 1, Y: 1, Width: 5, Height: 5, Data: "M 0 0 L 5 5"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assetRoot := t.TempDir()
	var output bytes.Buffer
	if err := WriteManifest(source, &output, Options{AssetRoot: assetRoot}); err != nil {
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
	if len(loaded.Document.Annotations) != 1 {
		t.Fatalf("annotations = %d", len(loaded.Document.Annotations))
	}
	page := loaded.Document.Annotations[0]
	if page.Page != 0 || len(page.Items) != 1 {
		t.Fatalf("annotation page = %+v", page)
	}
	annotation := page.Items[0]
	if annotation.ID != 9001 || annotation.Type != "Highlight" || annotation.Creator != "tester" || annotation.Remark != "需要确认" || annotation.Visible == nil || !*annotation.Visible || annotation.ReadOnly == nil || *annotation.ReadOnly || len(annotation.Parameters) != 1 || annotation.Boundary == nil || len(annotation.Items) != 1 {
		t.Fatalf("annotation was not preserved: %+v", annotation)
	}
	if len(loaded.Document.Versions) != 1 {
		t.Fatalf("versions = %d", len(loaded.Document.Versions))
	}
	version := loaded.Document.Versions[0]
	if version.ID != "v1" || version.Index != 1 || version.Current || version.Version != "1.0" || version.Name != "审查版本" || len(version.Files) != 1 || version.DocRoot == "" || version.DocRootName == "" {
		t.Fatalf("version was not preserved: %+v", version)
	}
	document, err := loaded.Build(baseDir, assetRoot)
	if err != nil {
		t.Fatalf("build exported annotation/version manifest: %v", err)
	}
	if len(document.Annotations) != 1 || len(document.Annotations[0].Items) != 1 || document.Annotations[0].Items[0].ID != 9001 || document.Annotations[0].Items[0].ReadOnlyValue == nil || *document.Annotations[0].Items[0].ReadOnlyValue {
		t.Fatalf("built annotations = %+v", document.Annotations)
	}
	if len(document.Versions) != 1 || document.Versions[0].ID != "v1" || len(document.Versions[0].Files) != 1 {
		t.Fatalf("built versions = %+v", document.Versions)
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
	if err := WriteManifest(source, &output, Options{AssetRoot: assetRoot}); err != nil {
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
