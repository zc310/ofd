package manifest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	manifestexport "github.com/zc310/ofd/internal/manifest/export"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
	"go.yaml.in/yaml/v4"
)

// 同一示例可由 YAML 和 JSON 创建，导出的固定页清单也应能重新加载并严格校验。
func TestCalloutManifestBothFormatsAndFixedExport(t *testing.T) {
	for _, example := range []string{"flow-callouts.yaml", "flow-callouts-layout.yaml"} {
		t.Run(example, func(t *testing.T) { testCalloutManifestExample(t, example) })
	}
}

func TestCalloutManifestThemeOverridesSelectedPresetAndLocalColorWins(t *testing.T) {
	source := manifest.Manifest{Version: 1, Flow: &manifest.Flow{
		Theme: "bootstrap5",
		Callouts: &manifest.FlowCalloutTheme{Warning: manifest.FlowCalloutPalette{
			AccentColor: &manifest.Color{R: 123}, Fill: &manifest.Color{G: 210},
		}},
		Blocks: []manifest.FlowBlock{
			{Type: "callout", Callout: &manifest.FlowCallout{Variant: "warning", Appearance: "subtle", Text: "Theme"}},
			{Type: "callout", Callout: &manifest.FlowCallout{Variant: "warning", Appearance: "subtle", Text: "Local", AccentColor: &manifest.Color{B: 98}}},
		},
	}}
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			var data []byte
			var err error
			if format == "json" {
				data, err = json.Marshal(source)
			} else {
				data, err = yaml.Marshal(source)
			}
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "input."+format)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			loaded, base, err := manifest.Load(path, format)
			if err != nil {
				t.Fatal(err)
			}
			model, err := loaded.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			var paths []creator.Path
			for _, item := range model.Pages[0].Items {
				if path, ok := item.(creator.Path); ok {
					paths = append(paths, path)
				}
			}
			for i, want := range map[int]*creator.Color{
				0: {G: 210}, 1: {R: 255, G: 230, B: 156}, 4: {R: 123}, 5: {G: 210}, 9: {B: 98},
			} {
				if !reflect.DeepEqual(paths[i].FillColor, want) {
					t.Fatalf("主题/局部覆盖未传递: path=%d got=%+v want=%+v", i, paths[i].FillColor, want)
				}
			}
		})
	}
}

func testCalloutManifestExample(t *testing.T, example string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../cmd/ofd-creator/examples", example))
	if err != nil {
		t.Fatal(err)
	}
	var source manifest.Manifest
	if err := yaml.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	for format, input := range map[string][]byte{"yaml": data, "json": encoded} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "input."+format)
			if err := os.WriteFile(path, input, 0600); err != nil {
				t.Fatal(err)
			}
			m, base, err := manifest.Load(path, format)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := m.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := creator.CreateWithOptions(doc, &output, creator.CreateOptions{Deterministic: true}); err != nil {
				t.Fatal(err)
			}
			check := func(data []byte) {
				t.Helper()
				report := v.ValidateReader(context.Background(), bytes.NewReader(data), "callouts.ofd")
				if report.HasErrors() || report.HasWarnings() {
					t.Fatalf("严格校验失败: %+v", report.Issues)
				}
			}
			check(output.Bytes())
			var exported bytes.Buffer
			if err := manifestexport.WriteManifest(output.Bytes(), &exported, manifestexport.Options{Format: format, AssetRoot: filepath.Join(root, "assets"), AssetPrefix: "assets"}); err != nil {
				t.Fatal(err)
			}
			path = filepath.Join(root, "fixed."+format)
			if err := os.WriteFile(path, exported.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			fixed, base, err := manifest.Load(path, format)
			if err != nil {
				t.Fatal(err)
			}
			model, err := fixed.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(model.Pages) != len(doc.Pages) {
				t.Fatal("导出丢失页面")
			}
			var rebuilt bytes.Buffer
			if err := creator.CreateWithOptions(model, &rebuilt, creator.CreateOptions{Deterministic: true}); err != nil {
				t.Fatal(err)
			}
			check(rebuilt.Bytes())
		})
	}
}

func TestCalloutManifestRejectsUnknownFieldsAndNestedContent(t *testing.T) {
	for _, input := range []string{
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"unknown":true,"blocks":[{"text":"Body"}]}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"blocks":[{"text":"Body","unknown":true}]}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"blocks":[{"text":"Body","page_break_before":true}]}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"blocks":[{"type":"callout","callout":{"blocks":[{"text":"Body"}]}}]}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"text":"Body","runs":[{"text":"Other"}]}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"text":"Body","blocks":[{"text":"Other"}]}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"runs":[{"text":"Body"}],"blocks":[{"text":"Other"}]}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"runs":[{"text":"Body","unknown":true}]}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"text":"Body","appearance":"unknown"}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"text":"Body","pagination":"unknown"}}]}}`,
		`{"version":1,"flow":{"blocks":[{"type":"callout","callout":{"text":"Body","repeat_title":true}}]}}`,
		`{"version":1,"flow":{"callouts":{"warning":{"unknown":true}},"blocks":[{"text":"Body"}]}}`,
	} {
		for _, format := range []string{"json", "yaml"} {
			var value any
			if err := json.Unmarshal([]byte(input), &value); err != nil {
				t.Fatal(err)
			}
			data := []byte(input)
			if format == "yaml" {
				var err error
				data, err = yaml.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(t.TempDir(), "input."+format)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			m, base, err := manifest.Load(path, format)
			if err == nil {
				_, err = m.Build(base, "")
			}
			if err == nil || !strings.Contains(err.Error(), "blocks") && !strings.Contains(err.Error(), "unknown") {
				t.Fatalf("未拒绝非法清单: %v", err)
			}
		}
	}
}
