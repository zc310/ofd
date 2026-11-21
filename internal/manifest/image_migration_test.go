package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v4"
)

func loadImageMigrationInput(t *testing.T, format string, input any) (Manifest, string) {
	t.Helper()
	var data []byte
	var err error
	if format == "json" {
		data, err = json.Marshal(input)
	} else {
		data, err = yaml.Marshal(input)
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image."+format)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	m, base, err := Load(path, "auto")
	if err != nil {
		t.Fatal(err)
	}
	return m, base
}

func TestFixedFigureRejectsWithFlowMigrationHintInBothFormats(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			m, base := loadImageMigrationInput(t, format, Manifest{
				Version: 1, Document: Document{ID: "removed-figure"},
				Pages: []Page{{Items: []Item{{Type: "figure"}}}},
			})
			if _, err := m.Build(base, ""); err == nil || !strings.Contains(err.Error(), "flow.blocks[].image") {
				t.Fatalf("缺少 Figure 迁移提示: %v", err)
			}
		})
	}
}

func TestFlowImageFitAndAlignmentLoadFromBothFormats(t *testing.T) {
	// 与已有 fixed 示例相同的 96×48 PNG，宽高比为 2:1。
	const pngData = "iVBORw0KGgoAAAANSUhEUgAAAGAAAAAwAgMAAACTeJdcAAAADFBMVEX///8vb7AaP2uaxOgQookGAAAAK0lEQVR42mKgC2ign8T///8HtQSMNcASoaGhw0wCwRqVoI7EKqyAYUABYADntv6/h3YNygAAAABJRU5ErkJggg=="
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			m, base := loadImageMigrationInput(t, format, Manifest{
				Version: 1, Document: Document{ID: "flow-image"},
				Flow: &Flow{Blocks: []FlowBlock{{Type: "image", Image: &FlowImage{
					DataBase64: pngData, Width: 40, Height: 40, Fit: "contain", Align: "left", Caption: "Caption",
				}}}},
			})
			doc, err := m.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, item := range doc.Pages[0].Items {
				if img, ok := item.(creator.Image); ok {
					found = true
					if img.X != 20 || img.Width != 40 || img.Height != 20 {
						t.Fatalf("fit/align 未传入 flow: %+v", img)
					}
				}
			}
			if !found {
				t.Fatal("缺少图片")
			}
		})
	}
}

// 删除的 Figure 专属字段必须报错，不能挂在普通图片图元上后被静默忽略。
func TestFixedImageRejectsRemovedFigureFieldsInBothFormats(t *testing.T) {
	for _, field := range []string{"caption", "caption_style", "image_width", "image_height", "image_fill_width", "align", "fit"} {
		for _, format := range []string{"json", "yaml"} {
			t.Run(format+"/"+field, func(t *testing.T) {
				var source string
				if format == "json" {
					source = fmt.Sprintf(`{"version":1,"pages":[{"items":[{"type":"image",%q:null}]}]}`, field)
				} else {
					source = fmt.Sprintf("version: 1\npages:\n  - items:\n      - type: image\n        %s: null\n", field)
				}
				path := filepath.Join(t.TempDir(), "image."+format)
				if err := os.WriteFile(path, []byte(source), 0600); err != nil {
					t.Fatal(err)
				}
				if _, _, err := Load(path, "auto"); err == nil {
					t.Fatalf("未拒绝已移除字段 %q", field)
				}
			})
		}
	}
}
