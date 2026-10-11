package pdf2ofd

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/testutil"
)

func TestConvertTextStrokePreservesLineWidthAndReusesResources(t *testing.T) {
	for _, scale := range []float64{1, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			content := fmt.Sprintf("%g 0 0 %g 0 0 cm 0.302 w 1 J 2 j BT /F1 1 Tf 2 Tr 20 0 0 20 30 50 Tm (AA) Tj (BB) Tj ET", scale, scale)
			var output bytes.Buffer
			if err := Convert(t.Context(), testutil.MinimalPDF([]byte(content), 200, 200), &output, ""); err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(output.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ofd.Close() }()
			doc := ofd.Documents[0]
			page := doc.Pages[0]
			if err := page.EnsureLoaded(); err != nil {
				t.Fatal(err)
			}
			texts := layerTexts(page.Content().Layer[0])
			if len(texts) != 2 || !texts[0].Stroke || texts[0].DrawParam == 0 || texts[0].DrawParam != texts[1].DrawParam {
				t.Fatalf("stroke parameters not shared by text objects: %+v", texts)
			}
			param := doc.GetDrawParam(models.StID(texts[0].DrawParam))
			want := 0.302 * scale * pdfPointToMillimeter
			if param == nil || math.Abs(param.LineWidth-want) > 0.0002 || param.Cap != "Round" || param.Join != "Bevel" {
				t.Fatalf("text stroke parameter = %+v, want width %g, Round/Bevel", param, want)
			}
		})
	}
}

func TestConvertTextRisePreservesBaselineAndGraphicsState(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		origins [][2]float64
	}{
		{"rise and reset", "BT /F1 20 Tf 30 100 Td (AA) Tj 10 Ts (BB) Tj 0 Ts (CC) Tj ET", [][2]float64{{30, 100}, {56.68, 90}, {83.36, 100}}},
		{"saved state", "BT /F1 20 Tf 30 100 Td q 10 Ts (AA) Tj Q (BB) Tj ET", [][2]float64{{30, 90}, {56.68, 100}}},
		{"persists across BT", "BT /F1 20 Tf 10 Ts 30 100 Td (AA) Tj ET BT 30 100 Td (BB) Tj ET", [][2]float64{{30, 90}, {30, 90}}},
		{"rotated text matrix", "BT /F1 20 Tf 0 1 -1 0 100 80 Tm 10 Ts (AA) Tj ET", [][2]float64{{90, 120}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := parseContentPage(t, tc.content)
			texts := layerTexts(page.Content().Layer[0])
			if len(texts) != len(tc.origins) {
				t.Fatalf("text count = %d, want %d", len(texts), len(tc.origins))
			}
			for i, text := range texts {
				code := text.TextCode[0]
				x, y := code.X, code.Y
				if text.CTM != nil {
					x, y = text.CTM.Transform(x, y)
				}
				x, y = text.Boundary.X+x, text.Boundary.Y+y
				if math.Abs(x-tc.origins[i][0]*pdfPointToMillimeter) > 0.0002 || math.Abs(y-tc.origins[i][1]*pdfPointToMillimeter) > 0.0002 {
					t.Fatalf("text %d origin = (%g,%g), want (%g,%g)pt", i, x, y, tc.origins[i][0], tc.origins[i][1])
				}
			}
		})
	}
}

func TestConvertTJAdjustmentIncludesHorizontalScale(t *testing.T) {
	for _, scale := range []float64{25, 50, 100, 150} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			page := parseContentPage(t, fmt.Sprintf("BT /F1 20 Tf %g Tz 30 100 Td [(A) 100 (B)] TJ ET", scale))
			texts := layerTexts(page.Content().Layer[0])
			if len(texts) != 1 || len(texts[0].TextCode[0].DeltaX) != 1 {
				t.Fatalf("expected merged text with one advance, got %+v", texts)
			}
			want := (667 - 100) / 1000.0 * 20 * (scale / 100) * pdfPointToMillimeter
			if got := texts[0].TextCode[0].DeltaX[0]; math.Abs(got-want) > 0.0002 {
				t.Fatalf("advance = %g, want %g", got, want)
			}
		})
	}
}

func TestConvertTextSpacingIncludesTextMatrixScale(t *testing.T) {
	page := parseContentPage(t, "BT /F1 1 Tf 0.2 Tc 0.3 Tw 20 0 0 20 30 100 Tm (A B) Tj ET")
	texts := layerTexts(page.Content().Layer[0])
	deltas := texts[0].TextCode[0].DeltaX
	want := []float64{(0.667 + 0.2) * 20 * pdfPointToMillimeter, (0.278 + 0.2 + 0.3) * 20 * pdfPointToMillimeter}
	if len(deltas) != len(want) {
		t.Fatalf("DeltaX = %v, want %v", deltas, want)
	}
	for i, value := range want {
		if math.Abs(deltas[i]-value) > 0.0002 {
			t.Fatalf("DeltaX[%d] = %g, want %g", i, deltas[i], value)
		}
	}
}

func TestConvertAppliesPathClipToTextAndRestoresScope(t *testing.T) {
	page := parseContentPage(t, "q 0 0 40 40 re 10 10 20 20 re W* n BT /F1 12 Tf 30 30 Td (AA) Tj ET Q BT /F1 12 Tf 30 30 Td (BB) Tj ET")
	texts := layerTexts(page.Content().Layer[0])
	if len(texts) != 2 || texts[0].Clips == nil || texts[1].Clips != nil {
		t.Fatalf("text clipping or Q scope not preserved: %+v", texts)
	}
	clip := texts[0].Clips
	if len(clip.Clip) != 1 || len(clip.Clip[0].Area) != 1 || clip.Clip[0].Area[0].Path == nil {
		t.Fatalf("invalid text clip: %+v", clip)
	}
	if got := clip.Clip[0].Area[0].Path.Rule; got != "Even-Odd" {
		t.Fatalf("clip rule = %q, want Even-Odd", got)
	}
}

func TestConvertStrokeStylesAndMiterUnits(t *testing.T) {
	page := parseContentPage(t, "4 w 10 10 m 30 30 l S q 2 0 0 2 0 0 cm 2 J 2 j 5 M 10 20 m 30 40 l S Q 1 J 1 j 10 30 m 30 50 l S")
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 3 {
		t.Fatalf("path count = %d, want 3", len(paths))
	}
	for i, want := range []struct {
		cap, join string
		limit     float64
	}{{"Butt", "Miter", 20}, {"Square", "Bevel", 20}, {"Round", "Round", 20}} {
		path := paths[i]
		if path.Cap != want.cap || path.Join != want.join || math.Abs(path.MiterLimit-want.limit*pdfPointToMillimeter) > 0.0002 {
			t.Fatalf("path %d style = %q/%q/%g, want %q/%q/%g", i, path.Cap, path.Join, path.MiterLimit, want.cap, want.join, want.limit*pdfPointToMillimeter)
		}
	}
}

func TestConvertExtGStateStrokeStyles(t *testing.T) {
	content := "/GS gs 10 10 m 30 30 l S"
	page := parseConvertedPage(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /ExtGState << /GS 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /ExtGState /LW 4 /LC 1 /LJ 2 /ML 5 >>",
	})
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 1 {
		t.Fatalf("path count = %d, want 1", len(paths))
	}
	path := paths[0]
	if path.Cap != "Round" || path.Join != "Bevel" || math.Abs(path.LineWidth-4*pdfPointToMillimeter) > 0.0002 || math.Abs(path.MiterLimit-10*pdfPointToMillimeter) > 0.0002 {
		t.Fatalf("ExtGState stroke style not preserved: %+v", path)
	}
}

func TestConvertTextPreservesAffineBasis(t *testing.T) {
	// 基线位置和一 em 的两条轴向量都按源 PDF 的几何给出，避免只检查字号和角度。
	for _, tc := range []struct {
		name, state  string
		origin       [2]float64
		xAxis, yAxis [2]float64
		pageAttrs    string
	}{
		{"text matrix scales", "BT /F1 10 Tf 2 0 0 3 30 100 Tm", [2]float64{30, 100}, [2]float64{20, 0}, [2]float64{0, 30}, ""},
		{"ctm scales", "3 0 0 2 0 0 cm BT /F1 10 Tf 30 50 Td", [2]float64{90, 100}, [2]float64{30, 0}, [2]float64{0, 20}, ""},
		{"shear", "BT /F1 10 Tf 1 0 0.4 1 30 100 Tm", [2]float64{30, 100}, [2]float64{10, 0}, [2]float64{-4, 10}, ""},
		{"rotation", "BT /F1 10 Tf 0 1 -1 0 100 80 Tm", [2]float64{100, 120}, [2]float64{0, -10}, [2]float64{10, 0}, ""},
		{"mirror", "BT /F1 10 Tf -1 0 0 1 100 100 Tm", [2]float64{100, 100}, [2]float64{-10, 0}, [2]float64{0, 10}, ""},
		{"page rotate 90", "BT /F1 10 Tf 30 100 Td", [2]float64{100, 30}, [2]float64{0, 10}, [2]float64{-10, 0}, "/Rotate 90"},
		{"page rotate 180", "BT /F1 10 Tf 30 100 Td", [2]float64{170, 100}, [2]float64{-10, 0}, [2]float64{0, -10}, "/Rotate 180"},
		{"page rotate 270", "BT /F1 10 Tf 30 100 Td", [2]float64{100, 170}, [2]float64{0, -10}, [2]float64{10, 0}, "/Rotate 270"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := parseContentPageAttrs(t, tc.state+" (AA) Tj ET", tc.pageAttrs)
			texts := layerTexts(page.Content().Layer[0])
			if len(texts) != 1 {
				t.Fatalf("text count = %d, want 1", len(texts))
			}
			text := texts[0]
			transform := func(x, y float64) (float64, float64) {
				if text.CTM != nil {
					x, y = text.CTM.Transform(x, y)
				}
				return x, y
			}
			code := text.TextCode[0]
			x, y := transform(code.X, code.Y)
			if math.Abs(text.Boundary.X+x-tc.origin[0]*pdfPointToMillimeter) > 0.0003 || math.Abs(text.Boundary.Y+y-tc.origin[1]*pdfPointToMillimeter) > 0.0003 {
				t.Fatalf("baseline not preserved: %+v", text)
			}
			ox, oy := transform(0, 0)
			for i, want := range [][2]float64{tc.xAxis, tc.yAxis} {
				vx, vy := 0.0, text.Size
				if i == 0 {
					vx, vy = text.Size, 0
				}
				vx, vy = transform(vx, vy)
				// CTM 与 Size 各保留四位小数，组合后的误差按毫米容许到 0.001。
				if math.Abs(vx-ox-want[0]*pdfPointToMillimeter) > 0.001 || math.Abs(vy-oy-want[1]*pdfPointToMillimeter) > 0.001 {
					t.Fatalf("em axis %d = (%g,%g), want (%g,%g)pt", i, vx-ox, vy-oy, want[0], want[1])
				}
			}
		})
	}
}
