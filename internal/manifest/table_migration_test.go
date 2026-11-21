package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v4"
)

// YAML/JSON 同时接通新的单元格格式字段，不能在清单转换时退化为纯文本。
func TestFlowTableMarkdownCellLoadsFromBothFormats(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			input := Manifest{Version: 1, Document: Document{ID: "markdown-table"},
				Flow: &Flow{Blocks: []FlowBlock{{Type: "table", Table: &FlowTable{
					Grid: [][]FlowTableCell{{{Text: "**Bold** [Link](https://example.com)", Format: "markdown"}}},
				}}}}}
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
			path := filepath.Join(t.TempDir(), "table."+format)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			m, base, err := Load(path, "auto")
			if err != nil {
				t.Fatal(err)
			}
			doc, err := m.Build(base, "")
			if err != nil {
				t.Fatal(err)
			}
			bold, link := false, false
			for _, item := range doc.Pages[0].Items {
				if text, ok := item.(creator.Text); ok {
					bold = bold || text.Weight == 700
					link = link || len(text.Actions) == 1 && text.Actions[0].URI != nil
				}
			}
			if !bold || !link {
				t.Fatalf("Markdown 格式字段未传递: bold=%v link=%v", bold, link)
			}
		})
	}
}
