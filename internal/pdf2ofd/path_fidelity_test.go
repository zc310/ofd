package pdf2ofd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func TestPathClipTakesEffectAfterPaintingCurrentPath(t *testing.T) {
	for _, clip := range []string{"W", "W*"} {
		for _, paint := range []string{"S", "s", "f", "F", "f*", "B", "B*", "b", "b*", "n"} {
			t.Run(clip+"_"+paint, func(t *testing.T) {
				content := fmt.Sprintf("q 0 0 170 170 re W n 20 w 1 0 0 rg 30 30 100 100 re %s %s 0 0 1 rg 80 80 100 100 re f Q 10 10 5 5 re f", clip, paint)
				pdf := embeddedTextPDF(content)
				doc, page, _ := textFidelityConvert(t, pdf)
				paths := layerPaths(page.Content().Layer[0])
				if paint == "n" {
					if len(paths) != 2 {
						t.Fatalf("paths = %d, want 2", len(paths))
					}
				} else {
					if len(paths) != 3 || paths[0].Clips == nil || len(paths[0].Clips.Clip) != 1 {
						t.Fatalf("current path should use only the old clip: %+v", paths)
					}
				}
				following := paths[len(paths)-2]
				if following.Clips == nil || len(following.Clips.Clip) != 2 || paths[len(paths)-1].Clips != nil {
					t.Fatal("new clip did not apply to following path, or Q did not restore scope")
				}
				assertPDFImportMatchesSource(t, pdf, doc, page)
			})
		}
	}
}

func TestFillAndStrokePreservesOpenAndClosedSubpaths(t *testing.T) {
	for _, tc := range []struct {
		name, path, paint string
		objects           int
	}{
		{"open_nonzero", "30 30 m 130 30 l 130 130 l", "B", 2},
		{"open_evenodd", "30 30 m 130 30 l 130 130 l", "B*", 2},
		{"closed_nonzero", "30 30 m 130 30 l 130 130 l h", "B", 1},
		{"closed_evenodd", "30 30 m 130 30 l 130 130 l h", "B*", 1},
		{"close_operator", "30 30 m 130 30 l 130 130 l", "b", 1},
		{"close_operator_evenodd", "30 30 m 130 30 l 130 130 l", "b*", 1},
		{"mixed_subpaths", "30 30 m 130 30 l 130 130 l 60 50 10 10 re", "B", 2},
		{"earlier_open_subpath", "30 30 m 130 30 l 130 130 l 60 50 m 70 50 l 70 60 l", "b*", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pdf := embeddedTextPDF("20 w 1 0 0 rg " + tc.path + " " + tc.paint)
			doc, page, _ := textFidelityConvert(t, pdf)
			paths := layerPaths(page.Content().Layer[0])
			if len(paths) != tc.objects {
				t.Fatalf("paths = %d, want %d", len(paths), tc.objects)
			}
			if tc.objects == 2 && (!paths[0].Fill || paths[0].Stroke.Value(true) || paths[1].Fill || !paths[1].Stroke.Value(true)) {
				t.Fatalf("expected fill followed by original stroke: %+v", paths)
			}
			assertPDFImportMatchesSource(t, pdf, doc, page)
		})
	}
}

func TestOpenFillAndStrokePreservesIndependentOpacityAndClip(t *testing.T) {
	for _, paint := range []string{"B", "B*"} {
		t.Run(paint, func(t *testing.T) {
			pdf := transparencyPDF("/GS gs 5 5 90 90 re W n 12 w 1 0 0 rg 10 10 m 80 10 l 80 80 l "+paint,
				"/ExtGState << /GS << /ca 0.4 /CA 0.7 >> >>")
			doc, page, _ := transparencyConvert(t, pdf)
			// MuPDF 1.21 将填充/描边作为单个透明对象，描边形状挖空填充；
			// Poppler 22.12 在该样例的重叠区保留填充贡献。按 PDF 的单对象语义
			// 对照 MuPDF，不能据此宣称两种阅读器对透明 B 的显示一致。
			assertPDFImportMatchesSource(t, pdf, doc, page, "mutool")
		})
	}
}

func TestOpenFillAndStrokeRasterFallbackReportsAndHonorsLimits(t *testing.T) {
	pdf := transparencyPDF("/GS gs 12 w 1 0 0 rg 10 10 m 80 10 l 80 80 l B",
		"/ExtGState << /GS << /ca 0.4 /CA 0.7 >> >>")
	for _, tc := range []struct {
		name    string
		options Options
		want    error
	}{
		{"strict", Options{Strict: true}, ErrUnsupported},
		{"pixels", Options{Limits: Limits{MaxImagePixels: 1}}, ErrLimitExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var codes []string
			tc.options.OnDiagnostic = func(d Diagnostic) { codes = append(codes, d.Code) }
			var out bytes.Buffer
			err := ConvertWithOptions(t.Context(), pdf, &out, "", tc.options)
			if !errors.Is(err, tc.want) || out.Len() != 0 || len(codes) != 1 || codes[0] != "graphics.path_fill_stroke" {
				t.Fatalf("err=%v output=%d diagnostics=%v", err, out.Len(), codes)
			}
		})
	}
}

func TestOpenFillAndStrokeBlendsOnceAgainstBackdrop(t *testing.T) {
	pdf := transparencyPDF("0 0 1 rg 0 0 100 100 re f /GS gs 12 w 1 0 0 rg 0.5 G 10 10 m 80 10 l 80 80 l B",
		"/ExtGState << /GS << /ca 0.4 /CA 0.7 /BM /Screen >> >>")
	doc, page, _ := transparencyConvert(t, pdf)
	assertPDFImportMatchesSource(t, pdf, doc, page, "mutool")
}
