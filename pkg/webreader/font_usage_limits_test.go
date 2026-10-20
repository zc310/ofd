package webreader

import (
	"reflect"
	"slices"
	"testing"
)

func TestFontUsageTruncatesOnlyWhenPagesAreOmitted(t *testing.T) {
	font := TextRun{Scope: 0, Font: 1}
	otherScope := TextRun{Scope: 1, Font: 1}
	for _, tc := range []struct {
		name         string
		runs         [][]TextRun
		options      FontUsageOptions
		wantPages    []int
		wantFonts    []FontUsageSummary
		scanned      int
		truncated    bool
		allTruncated bool
	}{
		{
			name: "exact limit at end with duplicate runs", runs: [][]TextRun{{font}, {font, font}},
			options: FontUsageOptions{MaxPages: 2, MaxScan: 2}, wantPages: []int{0, 1},
			wantFonts: []FontUsageSummary{{Scope: 0, ID: 1, Pages: []int{0, 1}}}, scanned: 2,
		},
		{
			name: "exact limit with trailing nonmatching pages", runs: [][]TextRun{{font, font}, nil, nil},
			options: FontUsageOptions{MaxPages: 1}, wantPages: []int{0},
			wantFonts: []FontUsageSummary{{Scope: 0, ID: 1, Pages: []int{0}}}, scanned: 3,
		},
		{
			name: "another matching page", runs: [][]TextRun{{font, font}, nil, {font, font}},
			options: FontUsageOptions{MaxPages: 1}, wantPages: []int{0},
			wantFonts: []FontUsageSummary{{Scope: 0, ID: 1, Pages: []int{0}}}, scanned: 3, truncated: true,
		},
		{
			name: "scan limit leaves unscanned pages", runs: [][]TextRun{{font}, nil, {font}},
			options: FontUsageOptions{MaxPages: 2, MaxScan: 2}, wantPages: []int{0},
			wantFonts: []FontUsageSummary{{Scope: 0, ID: 1, Pages: []int{0}}}, scanned: 2, truncated: true,
		},
		{
			name: "same font ID in another scope", runs: [][]TextRun{{font}, {otherScope, otherScope}, nil},
			options: FontUsageOptions{MaxPages: 1}, wantPages: []int{0},
			wantFonts: []FontUsageSummary{{Scope: 0, ID: 1, Pages: []int{0}}, {Scope: 1, ID: 1, Pages: []int{1}}}, scanned: 3,
		},
		{
			name: "only another scope exceeds limit", runs: [][]TextRun{{font}, {otherScope}, {otherScope}},
			options: FontUsageOptions{MaxPages: 1}, wantPages: []int{0},
			wantFonts: []FontUsageSummary{{Scope: 0, ID: 1, Pages: []int{0}}, {Scope: 1, ID: 1, Pages: []int{1}}}, scanned: 3, allTruncated: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 统计直接消费已缓存的不可变文字快照，不涉及字体加载或页面解析。
			r := &Reader{pages: make([]pageRef, len(tc.runs)), text: newTextCache()}
			for index, runs := range tc.runs {
				r.text.Add(index, runs)
			}
			usage, err := r.FontUsage(FontRef{Scope: 0, ID: 1}, tc.options)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(usage.Pages, tc.wantPages) || usage.Scanned != tc.scanned || usage.Truncated != tc.truncated {
				t.Errorf("FontUsage=%+v，期望 pages=%v scanned=%d truncated=%v", usage, tc.wantPages, tc.scanned, tc.truncated)
			}
			report, err := r.FontUsageAll(tc.options)
			if err != nil {
				t.Fatal(err)
			}
			allTruncated := tc.truncated || tc.allTruncated
			if !reflect.DeepEqual(report.Fonts, tc.wantFonts) || report.Scanned != tc.scanned || report.Truncated != allTruncated {
				t.Errorf("FontUsageAll=%+v，期望 fonts=%+v scanned=%d truncated=%v", report, tc.wantFonts, tc.scanned, allTruncated)
			}
		})
	}
}
