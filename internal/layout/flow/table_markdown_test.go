package flow

import (
	"strings"
	"testing"
)

func TestMarkdownCellRunsPreserveListsAndIsolateLinkTargets(t *testing.T) {
	runs, err := tableCellRuns(TableCell{Format: "markdown", Text: "- **first**\n- second\n\n[link](https://example.com) plain [anchor](#section)"})
	if err != nil {
		t.Fatal(err)
	}
	var value strings.Builder
	bold, link, anchor := false, false, false
	for _, run := range runs {
		value.WriteString(run.Text)
		bold = bold || run.Text == "first" && run.Bold
		link = link || run.Text == "link" && run.Href == "https://example.com"
		anchor = anchor || run.Text == "anchor" && run.Link && run.Href == ""
		if strings.Contains(run.Text, "plain") && (run.Link || run.Href != "") {
			t.Fatal("链接样式泄漏到后续文字")
		}
	}
	if !bold || !link || !anchor || !strings.Contains(value.String(), "• first\n• second") {
		t.Fatalf("Markdown 列表或样式丢失: %q %+v", value.String(), runs)
	}
}
