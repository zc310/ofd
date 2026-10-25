package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/pkg/validator"
	"go.yaml.in/yaml/v3"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// YAML 示例及其临时转换的 JSON 用构造的映射字体核对结构，不依赖测试机安装中文字体。
func TestRecipientExamplesCreateExportAndRecreateWithFinalDestinations(t *testing.T) {
	var first []byte
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			m, _, err := manifest.Load("examples/flow-gbt9704-recipients.yaml", "yaml")
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := yaml.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			face, err := sfnt.Parse(goregular.TTF)
			if err != nil {
				t.Fatal(err)
			}
			var buffer sfnt.Buffer
			g, err := face.GlyphIndex(&buffer, 'H')
			if err != nil {
				t.Fatal(err)
			}
			seen := map[rune]bool{'H': true}
			mappings := []fontrepair.GlyphMapping{{Rune: 'H', Glyph: uint16(g)}}
			for _, r := range string(encoded) + "主送抄送：。，印发" {
				if r > 127 && !seen[r] {
					seen[r] = true
					mappings = append(mappings, fontrepair.GlyphMapping{Rune: r, Glyph: uint16(g)})
				}
			}
			data, err := fontrepair.RepairWithGlyphs(goregular.TTF, mappings)
			if err != nil {
				t.Fatal(err)
			}
			m.Resources.Fonts = []manifest.Font{{Name: "TestOfficial", DataBase64: base64.StdEncoding.EncodeToString(data)}}
			m.Flow.Official.Fonts = manifest.FlowOfficialFonts{Body: "TestOfficial", Title: "TestOfficial", Hei: "TestOfficial", Kai: "TestOfficial", Song: "TestOfficial"}
			if format == "json" {
				encoded, err = json.Marshal(m)
			} else {
				encoded, err = yaml.Marshal(m)
			}
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			input := filepath.Join(root, "input."+format)
			if err := os.WriteFile(input, encoded, 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			output := filepath.Join(root, "recipients.ofd")
			for _, args := range [][]string{{"-i", input, "--check"}, {"-i", input, "-o", output, "--validate", "--deterministic"}} {
				if code := run(args, &stdout, &stderr); code != 0 {
					t.Fatalf("creator=%d: %s", code, stderr.String())
				}
			}
			ofd, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if first == nil {
				first = ofd
			} else if !bytes.Equal(first, ofd) {
				t.Fatal("JSON/YAML 迁移示例输出不一致")
			}
			v, err := validator.New(validator.WithMode(validator.ModeStrict))
			if err != nil {
				t.Fatal(err)
			}
			report := v.ValidateReader(context.Background(), bytes.NewReader(ofd), "recipient.ofd")
			if report.HasErrors() || report.HasWarnings() {
				t.Fatalf("严格校验失败: %+v", report.Issues)
			}
			exported := filepath.Join(root, "fixed."+format)
			if code := run([]string{"export", "-i", output, "-o", exported, "--format", format}, &stdout, &stderr); code != 0 {
				t.Fatalf("export=%d: %s", code, stderr.String())
			}
			loaded, base, err := manifest.Load(exported, format)
			if err != nil {
				t.Fatal(err)
			}
			fixed, err := loaded.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			pages := map[string]int{}
			for _, bookmark := range fixed.Bookmarks {
				pages[bookmark.Name] = bookmark.Goto.Page
			}
			if len(fixed.Pages) < 2 || pages["body"] != 0 || pages["notice"] != 0 || pages["recipients"] != len(fixed.Pages)-1 {
				t.Fatalf("导出丢失迁移目的地: %+v", pages)
			}
			if code := run([]string{"-i", exported, "--check"}, &stdout, &stderr); code != 0 {
				t.Fatalf("export check=%d: %s", code, stderr.String())
			}
			if code := run([]string{"-i", exported, "-o", filepath.Join(root, "recreated.ofd"), "--validate"}, &stdout, &stderr); code != 0 {
				t.Fatalf("recreate=%d: %s", code, stderr.String())
			}
		})
	}
}
