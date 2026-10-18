package compare

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestInsertedPageDoesNotStealModifiedPageMatch(t *testing.T) {
	a := testOFD(t, "Cover", "Contract amount: 100; delivery: October 20", "Signature")
	b := testOFD(t, "Cover", "An entirely new appendix", "Contract amount: 120; delivery: October 25", "Signature")
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := r.Comparisons[0].Pages
	if len(p) != 4 || p[1].Before != 2 || p[1].After != 3 || !p[1].TextChanged || p[3].Before != 0 || p[3].After != 2 {
		t.Fatalf("插页导致正文错配: %+v", p)
	}
	if p[1].Match.Method != "text_similarity" || p[1].Match.TextSimilarity < 0.6 || p[3].Match.Method != "unmatched" {
		t.Fatalf("插页后配对依据错误：%+v", p)
	}
	r, err = CompareFiles(t.Context(), []string{b, a}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	p = r.Comparisons[0].Pages
	if p[1].After != 0 || p[2].After != 2 {
		t.Fatalf("删页导致正文错配: %+v", p)
	}
}

func TestPageMatchingRecordsActualMethodAndPriority(t *testing.T) {
	tests := []struct {
		name    string
		a, b    []Page
		options Options
		method  string
	}{
		{"content", []Page{{Document: 1, ContentSHA256: "same", Text: "same"}}, []Page{{Document: 1, ContentSHA256: "same", Text: "same"}}, Options{PageMatch: "auto"}, "content"},
		{"text", []Page{{Document: 1, ContentSHA256: "old", Text: "same"}}, []Page{{Document: 1, ContentSHA256: "new", Text: "same"}}, Options{PageMatch: "auto"}, "text"},
		{"similarity", []Page{{Document: 1, Text: "Contract amount: 100; delivery date: October 20"}}, []Page{{Document: 1, Text: "Contract amount: 120; delivery date: October 25"}}, Options{PageMatch: "auto"}, "text_similarity"},
		{"sequence", []Page{{Document: 1, Text: "old"}}, []Page{{Document: 1, Text: "new"}}, Options{PageMatch: "auto"}, "sequence"},
		{"number", []Page{{Document: 1, ContentSHA256: "same", Text: "same"}}, []Page{{Document: 1, ContentSHA256: "same", Text: "same"}}, Options{PageMatch: "number"}, "number"},
		{"manual", []Page{{Document: 1, ContentSHA256: "same", Text: "same"}}, []Page{{Document: 1, ContentSHA256: "same", Text: "same"}}, Options{PageMatch: "auto", PageMap: "1:1"}, "manual"},
		{"removed", []Page{{Document: 1, Text: "old"}}, nil, Options{PageMatch: "auto"}, "unmatched"},
		{"added", nil, []Page{{Document: 1, Text: "new"}}, Options{PageMatch: "auto"}, "unmatched"},
		{"duplicate text", []Page{{Document: 1, Text: "repeat"}, {Document: 1, Text: "repeat"}}, []Page{{Document: 1, Text: "repeat"}, {Document: 1, Text: "repeat"}}, Options{PageMatch: "auto"}, "sequence"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pairs, err := matchPages(t.Context(), tt.a, tt.b, tt.options)
			if err != nil || len(pairs) == 0 {
				t.Fatalf("%v %v", pairs, err)
			}
			for _, pair := range pairs {
				if pair.Info.Method != tt.method || pair.Info.SearchLimited {
					t.Fatalf("配对依据错误：%+v", pair)
				}
				if tt.method == "text_similarity" {
					want := fingerprintSimilarity(textFingerprint(tt.a[0].Text), textFingerprint(tt.b[0].Text))
					if pair.Info.TextSimilarity != want || want < 0.6 || want > 1 {
						t.Fatalf("未保存实际使用的相似度：%+v", pair)
					}
				} else if pair.Info.TextSimilarity != 0 {
					t.Fatalf("非相似度配对不应提供分数：%+v", pair)
				}
			}
		})
	}
}

func TestPageMatchingReportsSearchLimitOnlyForFallbackAndUnmatchedPages(t *testing.T) {
	a := []Page{{Document: 1, ContentSHA256: "anchor", Text: "anchor"}}
	b := []Page{a[0]}
	for i := 0; i < 1100; i++ {
		a = append(a, Page{Document: 1, Text: fmt.Sprintf("old paragraph %d", i)})
		b = append(b, Page{Document: 1, Text: fmt.Sprintf("new paragraph %d", i)})
	}
	b = append(b, Page{Document: 1, Text: "additional page"})
	pairs, err := matchPages(t.Context(), a, b, Options{PageMatch: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if pairs[0].Info.Method != "content" || pairs[0].Info.SearchLimited {
		t.Fatal("相同内容匹配不应标记搜索回退")
	}
	for _, pair := range pairs[1:] {
		if !pair.Info.SearchLimited || (pair.Info.Method != "sequence" && pair.Info.Method != "unmatched") {
			t.Fatalf("搜索上限回退未正确记录：%+v", pair)
		}
	}
	if pairs[len(pairs)-1].Info.Method != "unmatched" {
		t.Fatal("新增页未标记未匹配")
	}
}

func TestAnchorWindowsKeepUnrelatedReplacementBetweenItsNeighbors(t *testing.T) {
	a := []Page{{Document: 1, Text: "start"}, {Document: 1, Text: "old"}, {Document: 1, Text: "middle"}, {Document: 1, Text: "end"}}
	b := []Page{{Document: 1, Text: "start"}, {Document: 1, Text: "middle"}, {Document: 1, Text: "new"}, {Document: 1, Text: "end"}}
	got, err := matchPages(t.Context(), a, b, Options{PageMatch: "auto"})
	want := [][2]int{{1, 1}, {2, 0}, {3, 2}, {4, 4}, {0, 3}}
	if err != nil || !reflect.DeepEqual(pagePairsForTest(got), want) {
		t.Fatalf("%v %v", got, err)
	}
}

func TestManualPageMapOverridesAutomaticMatchingAndRequiresSelectedPages(t *testing.T) {
	a, b := testOFD(t, "one", "two"), testOFD(t, "one", "two")
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{PageMap: "1:2,2:1"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Comparisons[0].Pages[0].After != 2 || r.Comparisons[0].Pages[1].After != 1 {
		t.Fatal(r.Comparisons[0].Pages)
	}
	for _, options := range []Options{{PageMap: "1:2", Pages: "2"}, {PageMap: "1:3"}, {PageMap: "1:1", PageMatch: "number"}, {PageMap: "1:1,1:2"}, {PageMap: "0:1"}, {PageMap: "1:1:2"}} {
		if _, err := CompareFiles(t.Context(), []string{a, b}, options); err == nil {
			t.Fatalf("未拒绝 %+v", options)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := matchPages(ctx, []Page{{Text: "one"}}, []Page{{Text: "two"}}, Options{PageMatch: "auto"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNumberMatchingKeepsGlobalOrderAcrossDocumentBodies(t *testing.T) {
	a := []Page{{Number: 1, Document: 1}, {Number: 2, Document: 2}}
	b := []Page{{Number: 1, Document: 1}, {Number: 2, Document: 1}}
	got, err := matchPages(t.Context(), a, b, Options{PageMatch: "number"})
	if err != nil || !reflect.DeepEqual(pagePairsForTest(got), [][2]int{{1, 1}, {2, 2}}) {
		t.Fatalf("%v %v", got, err)
	}
}

func pagePairsForTest(pairs []pagePairMatch) [][2]int {
	out := make([][2]int, len(pairs))
	for i, p := range pairs {
		out[i] = [2]int{p.Before, p.After}
	}
	return out
}
