package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

func TestPageLinksPreserveNestedActionsAndOptionalFields(t *testing.T) {
	links := []reader.PageLink{{Boundary: reader.AnnotationBoundary{X: 1, Width: 2, Height: 3}, TargetPage: 2,
		Actions: []reader.PageLink{{URI: "https://example.org", TargetPage: -1}}}}
	data, err := json.Marshal(PageLinks(links))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{`"boundary":{"x":1`, `"target_page":2`, `"actions":[`, `"uri":"https://example.org"`, `"boundary":null`, `"volume":null`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("缺少 %s: %s", expected, text)
		}
	}
	if strings.Contains(text, `"appearance_boundary"`) {
		t.Fatalf("未声明的外观边界不应出现: %s", text)
	}
	empty, err := json.Marshal(PageLinks(nil))
	if err != nil || string(empty) != "[]" {
		t.Fatalf("空链接 = %s, %v", empty, err)
	}
}
