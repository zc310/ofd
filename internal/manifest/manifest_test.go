package manifest

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/creator"
)

func TestBuildRejectsAssetPathEscape(t *testing.T) {
	directory := t.TempDir()
	value := Manifest{
		Version:   1,
		Document:  Document{ID: "path-test"},
		Resources: Resources{Images: []Image{{ID: 10, File: "../outside.png"}}},
		Pages:     []Page{{Items: []Item{{Type: "image", ResourceID: 10}}}},
	}
	if _, err := value.Build(directory, directory); err == nil {
		t.Fatal("Build accepted an asset path outside asset root")
	}
}

func TestLoadJSONAndBuildAssets(t *testing.T) {
	directory := t.TempDir()
	asset := filepath.Join(directory, "image.bin")
	if err := os.WriteFile(asset, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(directory, "document.json")
	if err := os.WriteFile(input, []byte(`{"version":1,"document":{"id":"json-test"},"resources":{"images":[{"id":10,"file":"image.bin"}]},"pages":[{"items":[{"type":"image","resourceId":10}]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	value, baseDir, err := Load(input, "auto")
	if err != nil {
		t.Fatal(err)
	}
	document, err := value.Build(baseDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Media) != 1 || len(document.Pages) != 1 {
		t.Fatalf("document resources/pages = %d/%d", len(document.Media), len(document.Pages))
	}
}

func TestLoadTOMLAndBuild(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.toml")
	toml := `version = 1

[document]
id = "toml-test"
title = "TOML document"

[[pages]]

[[pages.items]]
type = "text"
value = "hello"
width = 20
height = 5
`
	if err := os.WriteFile(input, []byte(toml), 0600); err != nil {
		t.Fatal(err)
	}
	value, baseDir, err := Load(input, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if value.Version != 1 || value.Document.ID != "toml-test" || len(value.Pages) != 1 || len(value.Pages[0].Items) != 1 {
		t.Fatalf("TOML manifest was not decoded: %+v", value)
	}
	if _, err := value.Build(baseDir, ""); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTOMLRejectsUnknownField(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "document.toml")
	if err := os.WriteFile(input, []byte("version = 1\nunknown = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(input, "auto"); err == nil {
		t.Fatal("TOML parser accepted an unknown field")
	}
}

func TestPathFillRulesExampleUsesValidPathCommands(t *testing.T) {
	input := filepath.Join("..", "..", "cmd", "ofd-creator", "examples", "path-fill-rules.json")
	value, _, err := Load(input, "auto")
	if err != nil {
		t.Fatal(err)
	}
	var checkItems func([]Item)
	checkItems = func(items []Item) {
		for index, item := range items {
			if item.Type == "path" {
				if _, err := models.ParsePathData(item.Data); err != nil {
					t.Fatalf("pages path %d has invalid data: %v", index, err)
				}
			}
			checkItems(item.Items)
		}
	}
	for _, page := range value.Pages {
		checkItems(page.Items)
		for _, layer := range page.Layers {
			checkItems(layer.Items)
		}
	}
}

func TestBuildDocumentFeatures(t *testing.T) {
	value := Manifest{
		Version: 1,
		Document: Document{
			ID:        "features",
			Actions:   []Action{{Event: "DO", URI: &URIAction{URI: "https://example.test"}, Region: &ActionRegion{Areas: []ActionArea{{Start: Point{X: 1, Y: 1}, Commands: []RegionCommand{{Type: "line", X: 10, Y: 10}, {Type: "close"}}}}}}},
			Bookmarks: []Bookmark{{Name: "首页", Goto: GotoAction{Page: 0}}},
		},
		Resources: Resources{DrawParams: []DrawParam{{Name: "line", LineWidth: 0.5}}, ColorSpaces: []ColorSpace{{ID: 7, Type: "RGB"}}},
		Templates: []Template{{ID: 3, Name: "header", Items: []Item{{Type: "text", X: 1, Y: 1, Width: 20, Height: 5, Value: "template"}}}},
		Pages:     []Page{{Templates: []TemplateRef{{ID: 3}}, Layers: []Layer{{Type: "Body", Items: []Item{{Type: "path", Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", DrawParam: "line", CTM: []float64{1, 0, 0, 1, 2, 3}}}}}}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Actions) != 1 || len(document.Bookmarks) != 1 || len(document.Templates) != 1 || len(document.Pages[0].Layers) != 1 {
		t.Fatalf("document features were not converted: %+v", document)
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPreservesImageCTM(t *testing.T) {
	value := Manifest{
		Version:   1,
		Document:  Document{ID: "image-ctm"},
		Resources: Resources{Images: []Image{{ID: 10, Format: "PNG", DataBase64: base64.StdEncoding.EncodeToString([]byte("image"))}}},
		Pages:     []Page{{Items: []Item{{Type: "image", ResourceID: 10, CTM: []float64{1, 0, 0, 1, 20, 30}}}}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	image, ok := document.Pages[0].Items[0].(creator.Image)
	if !ok || image.CTM == nil || *image.CTM != (creator.CTM{1, 0, 0, 1, 20, 30}) {
		t.Fatalf("image CTM was lost: %#v", document.Pages[0].Items[0])
	}
}

func TestBuildGotoTargetParameters(t *testing.T) {
	left, top, right, bottom, zoom := 1.0, 2.0, 30.0, 40.0, 1.5
	value := Manifest{
		Version: 1,
		Document: Document{
			ID:      "goto-target",
			Actions: []Action{{Goto: &GotoAction{Page: 2, Type: "XYZ", Left: &left, Top: &top, Right: &right, Bottom: &bottom, Zoom: &zoom}}},
		},
		Pages: []Page{{}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	gotoAction := document.Actions[0].Goto
	if gotoAction == nil || gotoAction.Left == nil || *gotoAction.Left != left || gotoAction.Top == nil || *gotoAction.Top != top || gotoAction.Right == nil || *gotoAction.Right != right || gotoAction.Bottom == nil || *gotoAction.Bottom != bottom || gotoAction.Zoom == nil || *gotoAction.Zoom != zoom {
		t.Fatalf("goto target parameters were lost: %#v", gotoAction)
	}
}

func TestBuildPageSizeNameDoesNotOverrideExplicitDimensions(t *testing.T) {
	value := Manifest{Version: 1, Document: Document{ID: "page-size", PageSize: PageSize{Name: "A4", Width: 100, Height: 120}}, Pages: []Page{{}}}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if document.PageSize.Width != 100 || document.PageSize.Height != 120 {
		t.Fatalf("explicit page dimensions were overridden: %+v", document.PageSize)
	}
}

func TestBuildTextFillPreservesUnsetAndFalse(t *testing.T) {
	falseValue := false
	value := Manifest{
		Version:  1,
		Document: Document{ID: "text-fill"},
		Pages:    []Page{{Items: []Item{{Type: "text", Value: "unset"}, {Type: "text", Value: "false", Fill: &falseValue}}}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	unset, ok := document.Pages[0].Items[0].(creator.Text)
	if !ok || unset.Fill != nil {
		t.Fatalf("unset text fill was not preserved: %#v", document.Pages[0].Items[0])
	}
	explicitFalse, ok := document.Pages[0].Items[1].(creator.Text)
	if !ok || explicitFalse.Fill == nil || *explicitFalse.Fill {
		t.Fatalf("explicit false text fill was not preserved: %#v", document.Pages[0].Items[1])
	}
}

func TestBuildRejectsUnsupportedNamedPageSize(t *testing.T) {
	value := Manifest{Version: 1, Document: Document{ID: "named-page-size", PageSize: PageSize{Name: "Letter"}}, Pages: []Page{{}}}
	if _, err := value.Build(t.TempDir(), ""); err == nil {
		t.Fatal("Build accepted an unsupported named page size")
	}
}

func TestBuildGradientPatternAndAnnotation(t *testing.T) {
	value := Manifest{
		Version: 1,
		Document: Document{
			ID: "graphics",
			Annotations: []AnnotationPage{
				{
					Page: 0,
					Items: []Annotation{
						{
							ID:          8,
							Type:        "Highlight",
							Creator:     "tester",
							LastModDate: "2026-01-02",
							ReadOnly:    boolPointer(false),
							Boundary:    &Box{X: 1, Y: 1, Width: 20, Height: 10},
							Items: []Item{{
								Type:      "path",
								Width:     20,
								Height:    10,
								Fill:      boolPointer(true),
								Data:      "M 0 0 L 20 0 L 20 10 C",
								FillColor: &Color{R: 255, G: 200, B: 0},
							}},
						},
					},
				},
			},
		},
		Pages: []Page{{Items: []Item{
			{
				Type:   "path",
				Width:  30,
				Height: 20,
				Fill:   boolPointer(true),
				Data:   "M 0 0 L 30 0 L 30 20 C",
				FillColor: &Color{Axial: &AxialShading{
					StartPoint: "0 0",
					EndPoint:   "1 1",
					Segments: []ColorStop{
						{Position: 0, Color: Color{R: 0, G: 0, B: 0}},
						{Position: 1, Color: Color{R: 255, G: 255, B: 255}},
					},
				}},
			},
			{
				Type:   "path",
				Width:  20,
				Height: 20,
				Fill:   boolPointer(true),
				Data:   "M 0 0 L 20 0 L 20 20 C",
				FillColor: &Color{Pattern: &Pattern{
					Width: 5, Height: 5, XStep: 5, YStep: 5,
					Items: []Item{{Type: "path", Width: 5, Height: 5, Fill: boolPointer(true), Data: "M 0 0 L 5 0 L 5 5 C"}},
				}},
			},
		}}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Annotations) != 1 || len(document.Annotations[0].Items) != 1 {
		t.Fatalf("annotations were not converted: %+v", document.Annotations)
	}
	if document.Annotations[0].Items[0].ReadOnlyValue == nil || *document.Annotations[0].Items[0].ReadOnlyValue {
		t.Fatal("annotation readOnly=false was not preserved")
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildDocumentMetadataAndResources(t *testing.T) {
	directory := t.TempDir()
	files := map[string][]byte{
		"cover.png":      []byte("cover"),
		"shared.xml":     []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."/>`),
		"shared.bin":     []byte("shared"),
		"page.xml":       []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."/>`),
		"page-image.png": []byte("page-image"),
		"schema.xsd":     []byte("schema"),
		"tag.xml":        []byte("tag"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	value := Manifest{
		Version: 1,
		Document: Document{
			ID:           "metadata",
			Title:        "Metadata",
			CreationDate: "2026-01-02",
			ModDate:      "2026-01-03",
			Cover:        "cover.png",
			CoverName:    "cover.png",
			DefaultCS:    7,
			CustomData:   []CustomData{{Name: "department", Value: "test"}},
			Permissions:  &Permissions{Edit: boolPointer(false), Print: &PrintSettings{Printable: true}},
		},
		Resources: Resources{
			ColorSpaces: []ColorSpace{{ID: 7, Type: "RGB"}},
			Public:      []PublicResource{{Name: "SharedRes.xml", File: "shared.xml", Files: []ResourceFile{{Path: "Data/shared.bin", File: "shared.bin"}}}},
			CustomTags:  []CustomTag{{NameSpace: "urn:test", Schema: "schema.xsd", SchemaName: "schema.xsd", Data: "tag.xml", DataName: "tag.xml"}},
		},
		Pages: []Page{{Resources: []PageResource{{File: "page.xml"}, {Images: []PageImage{{ID: 20, Format: "PNG", Name: "page.png", File: "page-image.png"}}}}}},
	}
	document, err := value.Build(directory, "")
	if err != nil {
		t.Fatal(err)
	}
	if document.CreationDate.IsZero() || document.ModDate.IsZero() || len(document.PublicRes) != 1 || len(document.CustomTags) != 1 || len(document.Pages[0].Resources) != 2 {
		t.Fatalf("metadata or resources were not converted: %+v", document)
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildDocumentOutlineSignatureAndVersionDetails(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "attachment.txt"), []byte("attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	value := Manifest{
		Version: 1,
		Document: Document{
			ID:          "document-details",
			Bookmarks:   []Bookmark{{Name: "home", Goto: GotoAction{Page: 0}}},
			Outlines:    []Outline{{Title: "首页", Expanded: boolPointer(true), Actions: []Action{{Event: "CLICK", Goto: &GotoAction{Page: 0}}}, Children: []Outline{{Title: "章节"}}}},
			Attachments: []Attachment{{ID: "att-1", Name: "附件", Format: "txt", File: "attachment.txt", CreationDate: "2026-01-01", ModDate: "2026-01-02", Visible: boolPointer(true)}},
			Extensions:  []Extension{{AppName: "test", Date: "2026-01-03", RefID: 1, Properties: []ExtensionProperty{{Name: "enabled", Type: "boolean", Value: "true"}}, Data: "<Data/>"}},
			Versions:    []Version{{ID: "v1", Index: 1, Current: true, Version: "1.0", Files: []VersionFile{{ID: "document", Path: "Document.xml"}}}},
			Signatures:  []Signature{{ID: "sig-1", Type: "Sign", ProviderName: "tester", References: []SignatureReference{{FileRef: "../Document.xml"}}, SignedValue: "dmFsdWU=", SignedValueName: "value.bin"}},
		},
		Pages: []Page{{}},
	}
	document, err := value.Build(directory, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Outlines) != 1 || len(document.Attachments) != 1 || len(document.Extensions) != 1 || len(document.Versions) != 1 || len(document.Signatures) != 1 {
		t.Fatalf("document details were not converted: %+v", document)
	}
	if document.Attachments[0].CreationDate.IsZero() || len(document.Extensions[0].Properties) != 1 || len(document.Versions[0].Files) != 1 || string(document.Signatures[0].SignedValue) != "value" {
		t.Fatalf("document detail fields were lost: %+v", document)
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildBase64BinaryResources(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	encoded := base64.StdEncoding.EncodeToString(png)
	mediaPNG := append(append([]byte(nil), png...), 4)
	value := Manifest{
		Version:  1,
		Document: Document{ID: "base64-resources", Attachments: []Attachment{{ID: "att", Name: "data", DataBase64: base64.StdEncoding.EncodeToString([]byte("attachment"))}}},
		Resources: Resources{
			Fonts:  []Font{{Name: "TestFont", Format: "ttf", DataBase64: base64.StdEncoding.EncodeToString([]byte("font"))}},
			Images: []Image{{ID: 10, Format: "PNG", DataBase64: encoded}},
			Media:  []Media{{ID: 11, Type: "Image", Format: "PNG", DataBase64: base64.StdEncoding.EncodeToString(mediaPNG)}},
		},
		Pages: []Page{{Items: []Item{{Type: "image", ResourceID: 10, X: 1, Y: 1, Width: 10, Height: 10}}}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Fonts) != 1 || len(document.Media) != 2 || len(document.Attachments) != 1 {
		t.Fatalf("base64 resources were not converted: %+v", document)
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildBase64DocumentBinaryFields(t *testing.T) {
	documentRoot := `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="../../Pages/Page_0/Content.xml"/></Pages></Document>`
	value := Manifest{
		Version: 1,
		Document: Document{
			ID:          "base64-document-fields",
			CoverBase64: base64.StdEncoding.EncodeToString([]byte("cover")),
			CoverName:   "cover.bin",
			Extensions:  []Extension{{AppName: "test", RefID: 1, DataFileBase64: base64.StdEncoding.EncodeToString([]byte("extension")), DataName: "extension.bin"}},
			Versions:    []Version{{ID: "v1", Index: 1, Files: []VersionFile{{ID: "document", Path: "Document.xml"}}, DocRootBase64: base64.StdEncoding.EncodeToString([]byte(documentRoot)), DocRootName: "version.xml"}},
		},
		Pages: []Page{{}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.CoverData) == 0 || len(document.Extensions[0].DataFile) == 0 || len(document.Versions[0].DocRoot) == 0 {
		t.Fatal("base64 document binary fields were not converted")
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildColorSpaceProfileBase64(t *testing.T) {
	profile := base64.StdEncoding.EncodeToString([]byte("icc-profile"))
	value := Manifest{
		Version:   1,
		Document:  Document{ID: "profile-base64"},
		Resources: Resources{ColorSpaces: []ColorSpace{{ID: 20, Type: "RGB", ProfileBase64: profile, ProfileName: "custom.icc"}}},
		Pages:     []Page{{}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if document.ColorSpaces[0].Profile != "Profiles/custom.icc" || string(document.ColorSpaces[0].ProfileData) != "icc-profile" {
		t.Fatalf("color profile was not converted: %+v", document.ColorSpaces[0])
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRejectsUnsafeColorProfileName(t *testing.T) {
	value := Manifest{
		Version:   1,
		Document:  Document{ID: "unsafe-profile-name"},
		Resources: Resources{ColorSpaces: []ColorSpace{{ID: 20, Type: "RGB", ProfileBase64: base64.StdEncoding.EncodeToString([]byte("profile")), ProfileName: "../profile.icc"}}},
	}
	if _, err := value.Build(t.TempDir(), ""); err == nil {
		t.Fatal("Build accepted an unsafe color profile name")
	}
}

func TestBuildBase64RawResourceFiles(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."/>`))
	value := Manifest{
		Version:   1,
		Document:  Document{ID: "base64-raw-resources"},
		Resources: Resources{Public: []PublicResource{{Name: "SharedRes.xml", DataBase64: raw, Files: []ResourceFile{{Path: "Data/value.bin", DataBase64: base64.StdEncoding.EncodeToString([]byte("value"))}}}}},
		Pages:     []Page{{Resources: []PageResource{{DataBase64: raw, Files: []ResourceFile{{Path: "Data/page.bin", DataBase64: base64.StdEncoding.EncodeToString([]byte("page"))}}}}}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.PublicRes) != 1 || len(document.PublicRes[0].Files) != 1 || len(document.Pages[0].Resources[0].Files) != 1 {
		t.Fatal("Base64 raw resource files were not converted")
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCompositeClips(t *testing.T) {
	value := Manifest{
		Version:   1,
		Document:  Document{ID: "composite-clips"},
		Resources: Resources{Composites: []Composite{{ID: 30, Width: 10, Height: 10, Items: []Item{{Type: "path", Width: 5, Height: 5, Data: "M 0 0 L 5 5 C"}}}}},
		Pages: []Page{{Items: []Item{{
			Type: "composite", X: 1, Y: 1, Width: 10, Height: 10, ResourceID: 30,
			Clips: []Clip{{Areas: []ClipArea{{Path: &ClipPath{Boundary: Box{Width: 5, Height: 5}, Data: "M 0 0 L 5 5 C"}}}}},
		}}}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	composite, ok := document.Pages[0].Items[0].(creator.Composite)
	if !ok || composite.Clips == nil {
		t.Fatalf("composite clips were not converted: %#v", document.Pages[0].Items[0])
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRejectsUnknownActionRegionCommand(t *testing.T) {
	value := Manifest{
		Version:  1,
		Document: Document{ID: "bad-action-region", Actions: []Action{{Region: &ActionRegion{Areas: []ActionArea{{Commands: []RegionCommand{{Type: "unknown"}}}}}}}},
	}
	if _, err := value.Build(t.TempDir(), ""); err == nil {
		t.Fatal("Build accepted an unknown action region command")
	}
}

func TestBuildRejectsConflictingPageContent(t *testing.T) {
	value := Manifest{
		Version:  1,
		Document: Document{ID: "conflicting-page-content"},
		Pages:    []Page{{Layers: []Layer{{Type: "Body"}}, Items: []Item{{Type: "path", Width: 1, Height: 1, Data: "M 0 0 C"}}}},
	}
	if _, err := value.Build(t.TempDir(), ""); err == nil {
		t.Fatal("Build accepted page layers and items together")
	}
}

func TestBuildRejectsConflictingPageResourceContent(t *testing.T) {
	value := Manifest{
		Version:  1,
		Document: Document{ID: "conflicting-page-resource"},
		Pages:    []Page{{Resources: []PageResource{{DataBase64: base64.StdEncoding.EncodeToString([]byte("<Res/>")), Images: []PageImage{{ID: 1, Format: "PNG", DataBase64: base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})}}}}}},
	}
	if _, err := value.Build(t.TempDir(), ""); err == nil {
		t.Fatal("Build accepted page resource data and images together")
	}
}

func TestBuildSignatureSealBase64(t *testing.T) {
	value := Manifest{
		Version:  1,
		Document: Document{ID: "seal-base64", Signatures: []Signature{{ID: "seal-1", Type: "Seal", ProviderName: "tester", References: []SignatureReference{{FileRef: "../Document.xml"}}, SealFileBase64: base64.StdEncoding.EncodeToString([]byte("seal")), SealName: "seal.bin"}}},
		Pages:    []Page{{}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if string(document.Signatures[0].SealFile) != "seal" {
		t.Fatalf("signature seal was not decoded: %q", document.Signatures[0].SealFile)
	}
	if _, err := creator.Marshal(document); err != nil {
		t.Fatal(err)
	}
}

func TestBuildClipTextRFC3339AndFields(t *testing.T) {
	value := Manifest{
		Version:  1,
		Document: Document{ID: "clip-text"},
		Pages:    []Page{{Items: []Item{{Type: "path", X: 1, Y: 1, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", Clips: []Clip{{Areas: []ClipArea{{Text: &ClipText{Boundary: Box{Width: 5, Height: 5}, Value: "裁剪文字", Size: 4, HScale: 0.8, ReadDirection: 1, CharDirection: 2, Weight: 700, Italic: true}}}}}}}}},
	}
	document, err := value.Build(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	path, ok := document.Pages[0].Items[0].(creator.Path)
	if !ok || path.Clips == nil || path.Clips.Items[0].Areas[0].Text == nil {
		t.Fatal("clip text was not converted")
	}
	text := path.Clips.Items[0].Areas[0].Text
	if text.HScale != 0.8 || text.Weight != 700 || !text.Italic {
		t.Fatalf("clip text fields were lost: %+v", text)
	}
}

func TestBuildRejectsInvalidBase64Resource(t *testing.T) {
	value := Manifest{Version: 1, Resources: Resources{Images: []Image{{ID: 1, Format: "PNG", DataBase64: "not-base64"}}}}
	if _, err := value.Build(t.TempDir(), ""); err == nil {
		t.Fatal("Build accepted invalid Base64 resource")
	}
}

func boolPointer(value bool) *bool {
	return &value
}
