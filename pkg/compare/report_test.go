package compare

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	comparelocalization "github.com/zc310/ofd/internal/localization/compare"
)

func TestReportShowsMultipleTextEditsAndRegionNavigation(t *testing.T) {
	a, b := testOFD(t, "Price 100; date 20; owner Alice"), testOFD(t, "Price 120; date 25; owner Bob")
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{Visual: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"html", "text", "markdown", "json"} {
		var out bytes.Buffer
		if err := WriteReport(&out, r, format); err != nil {
			t.Fatal(err)
		}
		if format == "html" {
			if strings.Count(out.String(), "<del>") < 3 || strings.Count(out.String(), "<ins>") < 3 {
				t.Fatal("多处文字变化仍被合成整段高亮")
			}
			for _, marker := range []string{`class="region-button"`, `data-crop="before"`, `data-crop="after"`, `data-crop="diff"`, `class="region-zoom"`, `class="region-map"`} {
				if !strings.Contains(out.String(), marker) {
					t.Errorf("报告缺少 %s", marker)
				}
			}
		}
	}
	r.Comparisons[0].Pages[0].TextDiff = []TextSpan{{Kind: "insert", Text: `<img src=x onerror="alert(1)">`}}
	var out bytes.Buffer
	if err := WriteReport(&out, r, "html"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `<img src=x`) {
		t.Fatal("文字差异中的 HTML 未转义")
	}
}

func TestAllReportFormatsExplainPageMatchingIncludingUnchangedPages(t *testing.T) {
	r := &Report{Baseline: 1, Inputs: []Input{{Name: "base.ofd"}, {Name: "target.ofd"}}, Comparisons: []Comparison{{Target: 2, Pages: []PageChange{
		{Before: 1, After: 1, Status: "unchanged", Match: PageMatchInfo{Method: "content"}},
		{Before: 2, After: 2, Status: "modified", Match: PageMatchInfo{Method: "text_similarity", TextSimilarity: 0.825}},
		{Before: 3, After: 3, Status: "modified", Match: PageMatchInfo{Method: "sequence", SearchLimited: true}},
		{After: 4, Status: "added", Match: PageMatchInfo{Method: "unmatched"}},
	}}}}
	for _, format := range []string{"html", "json", "text", "markdown"} {
		var out bytes.Buffer
		if err := WriteReport(&out, r, format); err != nil {
			t.Fatal(err)
		}
		if format == "json" {
			var decoded Report
			if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			for i, p := range decoded.Comparisons[0].Pages {
				if p.Match != r.Comparisons[0].Pages[i].Match {
					t.Fatal("配对依据 JSON 往返丢失")
				}
			}
			continue
		}
		for _, label := range []string{"页面内容摘要相同", "文字特征相似度 82.5%", "按顺序兜底配对", "相似度搜索达到上限", "未匹配（单侧页面）", "请核对页面对应关系"} {
			if !strings.Contains(out.String(), label) {
				t.Errorf("%s 未显示 %s", format, label)
			}
		}
	}
}

func TestPageMatchDescriptionsUseSelectedLocale(t *testing.T) {
	r := &Report{
		Baseline: 1,
		Options:  Options{Language: "en"},
		Inputs:   []Input{{Name: "base.ofd"}, {Name: "target.ofd"}},
		Comparisons: []Comparison{{Target: 2, Pages: []PageChange{{
			Before: 1, After: 1, Status: "modified",
			Match: PageMatchInfo{Method: "text_similarity", TextSimilarity: 0.825, SearchLimited: true},
		}}}},
	}
	for _, format := range []string{"html", "text", "markdown"} {
		var out bytes.Buffer
		err := WriteReport(&out, r, format)
		if err != nil {
			t.Fatal(err)
		}
		for _, phrase := range []string{"Text similarity match (feature similarity 82.5%)", "similarity search reached its limit"} {
			if !strings.Contains(out.String(), phrase) {
				t.Errorf("%s report missing localized phrase %q", format, phrase)
			}
		}
	}
	r.Options.Language = "fr"
	var out bytes.Buffer
	err := WriteReport(&out, r, "text")
	if err != nil {
		t.Fatal(err)
	}
	chineseLocalizer, err := comparelocalization.New("zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	chinese := chineseLocalizer.Message("compare.page_match.text_similarity", map[string]string{"Similarity": "82.5"})
	if !strings.Contains(out.String(), chinese) {
		t.Fatal("unsupported language did not fall back to Chinese")
	}
}

func TestCompleteFileListAlignsBothSidesAndMarksAllDifferences(t *testing.T) {
	base := Input{Name: "before.ofd", Files: []File{
		{Path: "same.xml", Size: 10, SHA256: "same"},
		{Path: "modified.xml", Size: 20, SHA256: "old"},
		{Path: "removed.xml", Size: 30, SHA256: "removed"},
		{Path: "old/image.png", Size: 40, SHA256: "image"},
		{Path: `<script>alert(1)</script>.xml`, Size: 1, SHA256: "safe"},
	}}
	target := Input{Name: "after.ofd", Files: []File{
		{Path: "added.xml", Size: 50, SHA256: "added"},
		{Path: "new/image.png", Size: 40, SHA256: "image"},
		{Path: "modified.xml", Size: 20, SHA256: "new"},
		base.Files[0], base.Files[4],
	}}
	c := Comparison{Target: 2, Files: []FileChange{{Status: "moved", Before: &base.Files[3], After: &target.Files[1]}}}
	r := &Report{Baseline: 1, Inputs: []Input{base, target}, Comparisons: []Comparison{c}}
	var out bytes.Buffer
	if err := WriteReport(&out, r, "html"); err != nil {
		t.Fatal(err)
	}
	_, table, ok := strings.Cut(out.String(), `<table class="file-comparison">`)
	if !ok {
		t.Fatal("缺少完整文件比较表")
	}
	table, _, _ = strings.Cut(table, "</table>")
	rows := regexp.MustCompile(`(?s)<tr class="file-row[^"]*" data-status="([^"]+)"[^>]*>(.*?)</tr>`).FindAllStringSubmatch(table, -1)
	if len(rows) != 6 {
		t.Fatalf("文件遗漏或重复：%d 行", len(rows))
	}
	counts := map[string]int{}
	for _, row := range rows {
		status, html := row[1], row[0]
		counts[status]++
		if strings.Contains(html, "file-different") != (status != "unchanged") {
			t.Fatalf("差异标记错误：%s", html)
		}
		switch status {
		case "unchanged":
			if strings.Contains(html, "same.xml") && strings.Count(html, "same.xml") != 2 {
				t.Fatal("相同文件未对齐")
			}
		case "modified":
			if strings.Count(html, "modified.xml") != 2 || strings.Count(html, "20 B") != 2 {
				t.Fatal("同大小修改文件未对齐")
			}
		case "moved":
			if !strings.Contains(html, "old/image.png") || !strings.Contains(html, "new/image.png") || !strings.Contains(html, "内容相同") {
				t.Fatal("移动文件未对齐或未说明内容相同")
			}
		case "added", "removed":
			if strings.Count(html, `aria-label="不存在"`) != 1 {
				t.Fatal("缺失文件一侧未留空")
			}
		}
	}
	if counts["unchanged"] != 2 || counts["modified"] != 1 || counts["added"] != 1 || counts["removed"] != 1 || counts["moved"] != 1 {
		t.Fatal(counts)
	}
	if strings.Contains(table, "<script>") || !strings.Contains(table, "&lt;script&gt;") {
		t.Fatal("清单文件名未转义")
	}
}
