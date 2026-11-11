package reader

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/zc310/ofd/internal/parser"
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
			r := &Reader{ofd: &parser.OFD{Documents: []*parser.Document{{}}}, pages: make([]pageRef, len(tc.runs)), text: newTextCache()}
			for index, runs := range tc.runs {
				r.text.Add(index, runs)
			}
			usage, err := r.FontUsage(t.Context(), FontRef{Scope: 0, ID: 1}, tc.options)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(usage.Pages, tc.wantPages) || usage.Scanned != tc.scanned || usage.Truncated != tc.truncated {
				t.Errorf("FontUsage=%+v，期望 pages=%v scanned=%d truncated=%v", usage, tc.wantPages, tc.scanned, tc.truncated)
			}
			report, err := r.FontUsageAll(t.Context(), tc.options)
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

type fontUsageCancelContext struct {
	context.Context //nolint:containedctx // 测试在指定检查次数触发取消。

	checks int
}

func (c *fontUsageCancelContext) Err() error {
	c.checks++
	if c.checks >= 3 {
		return context.Canceled
	}
	return nil
}

func TestFontUsageContextCancellationDiscardsPartialResults(t *testing.T) {
	r := &Reader{ofd: &parser.OFD{Documents: []*parser.Document{{}}}, pages: make([]pageRef, 3), text: newTextCache()}
	for index := range r.pages {
		r.text.Add(index, []TextRun{{Font: 1}})
	}
	usage, err := r.FontUsage(&fontUsageCancelContext{Context: context.Background()}, FontRef{ID: 1}, FontUsageOptions{})
	if !errors.Is(err, context.Canceled) || usage.Scanned != 0 || len(usage.Pages) != 0 {
		t.Fatalf("取消后的字体统计 = %+v, %v", usage, err)
	}
	report, err := r.FontUsageAll(&fontUsageCancelContext{Context: context.Background()}, FontUsageOptions{})
	if !errors.Is(err, context.Canceled) || report.Scanned != 0 || len(report.Fonts) != 0 {
		t.Fatalf("取消后的批量统计 = %+v, %v", report, err)
	}
	if _, err := r.FontUsage(nil, FontRef{}, FontUsageOptions{}); err == nil { //nolint:staticcheck // 验证空上下文错误。
		t.Fatal("空上下文必须返回错误")
	}
	if _, err := r.FontUsageAll(nil, FontUsageOptions{}); err == nil { //nolint:staticcheck // 验证空上下文错误。
		t.Fatal("空上下文必须返回错误")
	}
}
