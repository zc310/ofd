package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v3"
)

func writePaginationManifest(t *testing.T, format string, source map[string]any) (Manifest, string) {
	t.Helper()
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
	input := filepath.Join(t.TempDir(), "pagination."+format)
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	m, base, err := Load(input, format)
	if err != nil {
		t.Fatal(err)
	}
	return m, base
}

func TestPaginationManifestBalanceFalseOrOmittedOverridesPreviousSection(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/explicit%v", format, explicit), func(t *testing.T) {
				columns := map[string]any{"count": 2, "gap": 8}
				if explicit {
					columns["balance"] = false
				}
				lines := func(prefix string) string {
					var result []string
					for i := 0; i < 10; i++ {
						result = append(result, fmt.Sprintf("%s%02d", prefix, i))
					}
					return strings.Join(result, "\n")
				}
				source := map[string]any{
					"version": 1, "document": map[string]any{"id": "section-policy", "page_size": map[string]any{"width": 100, "height": 90}},
					"flow": map[string]any{
						"columns": map[string]any{"count": 2, "gap": 8, "balance": true},
						"margins": map[string]any{"top": 10, "right": 10, "bottom": 10, "left": 10}, "body_size": 10, "line_height": 1.2,
						"blocks": []any{
							map[string]any{"text": lines("A")},
							map[string]any{"type": "section", "section": map[string]any{"columns": columns}},
							map[string]any{"text": lines("B")},
						},
					},
				}
				m, base := writePaginationManifest(t, format, source)
				if !m.Flow.Columns.Balance || m.Flow.Blocks[1].Section.Columns.Balance {
					t.Fatal("分节的 false 或缺省平衡策略丢失")
				}
				doc, err := m.Build(base, "")
				if err != nil {
					t.Fatal(err)
				}
				counts := make([][2]int, len(doc.Pages))
				for p, page := range doc.Pages {
					for _, item := range page.Items {
						text, ok := item.(creator.Text)
						if !ok {
							continue
						}
						value := text.Value
						for _, code := range text.TextCodes {
							value += code.Value
						}
						if !strings.HasPrefix(value, "A") && !strings.HasPrefix(value, "B") {
							continue
						}
						col := 0
						if text.X >= 54 {
							col = 1
						}
						counts[p][col]++
					}
				}
				if !reflect.DeepEqual(counts, [][2]int{{5, 5}, {10, 0}}) {
					t.Fatalf("清单开关没有改变实际布局: %v", counts)
				}
			})
		}
	}
}

func TestPaginationManifestSplitRowsSwitchControlsTallCellsInBothFormats(t *testing.T) {
	for _, format := range []string{"yaml", "json"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/split%v", format, enabled), func(t *testing.T) {
				source := map[string]any{
					"version": 1, "document": map[string]any{"id": "tall-cell", "page_size": map[string]any{"width": 100, "height": 90}},
					"flow": map[string]any{
						"margins": map[string]any{"top": 10, "right": 10, "bottom": 10, "left": 10},
						"blocks":  []any{map[string]any{"type": "table", "table": map[string]any{"split_rows": enabled, "header": []string{"Header"}, "rows": [][]string{{strings.Repeat("Cell line\n", 30)}}}}},
					},
				}
				m, base := writePaginationManifest(t, format, source)
				doc, err := m.Build(base, "")
				if !enabled {
					if err == nil || !strings.Contains(err.Error(), "行组") {
						t.Fatalf("关闭拆分没有保留超高行诊断: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(doc.Pages) < 2 {
					t.Fatal("长单元格没有跨页")
				}
				count := 0
				for _, page := range doc.Pages {
					for _, item := range page.Items {
						if text, ok := item.(creator.Text); ok {
							for _, code := range text.TextCodes {
								count += strings.Count(code.Value, "Cell line")
							}
						}
					}
				}
				if count != 30 {
					t.Fatalf("清单拆分丢失或重复文字: %d", count)
				}
			})
		}
	}
}
