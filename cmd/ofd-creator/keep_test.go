package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	"go.yaml.in/yaml/v3"
)

// 从两种创作清单生成的标题、主送机关和正文必须一起换页，导出重建保留最终目的地。
func TestOfficialKeepChainSurvivesManifestCreationAndExport(t *testing.T) {
	m := manifest.Manifest{Version: 1, Document: manifest.Document{ID: "keep-chain"}, Flow: &manifest.Flow{
		Profile: "gbt9704", GenerateOutlines: true,
		Official: &manifest.FlowOfficial{Fonts: manifest.FlowOfficialFonts{Body: "Flow-Regular", Title: "Flow-Regular", Hei: "Flow-Bold", Kai: "Flow-Italic", Song: "Flow-Regular"}},
		Blocks: []manifest.FlowBlock{
			{Text: strings.Repeat("Prefix\n", 15)},
			{ID: "title", Type: "heading", Level: 1, Text: "Notice"},
			{ID: "recipient", Type: "recipient", Text: "All departments:"},
			{ID: "body", Text: "Body line one\nBody line two"},
		},
	}}
	var first []byte
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			var source []byte
			var err error
			if format == "json" {
				source, err = json.Marshal(m)
			} else {
				source, err = yaml.Marshal(m)
			}
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			input := filepath.Join(root, "source."+format)
			if err := os.WriteFile(input, source, 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			output := filepath.Join(root, "keep.ofd")
			for _, args := range [][]string{{"-i", input, "--check"}, {"-i", input, "-o", output, "--validate", "--deterministic"}} {
				if code := run(args, &stdout, &stderr); code != 0 {
					t.Fatalf("creator=%d: %s", code, stderr.String())
				}
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if first == nil {
				first = data
			} else if !bytes.Equal(first, data) {
				t.Fatal("两种清单的跟随链输出不一致")
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
			if len(fixed.Pages) != 2 || len(fixed.Bookmarks) != 3 {
				t.Fatal("导出丢失跟随链的页面或定位")
			}
			for _, bookmark := range fixed.Bookmarks {
				if bookmark.Goto.Page != 1 {
					t.Fatalf("导出保留了错误的跟随链页码: %+v", bookmark)
				}
			}
			if len(fixed.Outlines) != 1 || len(fixed.Outlines[0].Actions) != 1 || fixed.Outlines[0].Actions[0].Goto == nil || fixed.Outlines[0].Actions[0].Goto.Page != 1 {
				t.Fatal("导出大纲没有指向移动后的标题页")
			}
			if code := run([]string{"-i", exported, "-o", filepath.Join(root, "recreated.ofd"), "--validate"}, &stdout, &stderr); code != 0 {
				t.Fatalf("recreate=%d: %s", code, stderr.String())
			}
		})
	}
}
