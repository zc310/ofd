package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/render"
)

func type3TextPDF(content, matrix, glyphA, glyphB, resources string, extra ...string) []byte {
	if matrix == "" {
		matrix = ".001 0 0 .001 0 0"
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> /XObject << /Shape 8 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 600 700] /FontMatrix [" + matrix + "] /CharProcs << /A 6 0 R /B 7 0 R >> /Encoding << /Type /Encoding /Differences [65 /A /B] >> /FirstChar 65 /LastChar 66 /Widths [600 600] " + resources + " >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(glyphA), glyphA),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(glyphB), glyphB),
		"<< /Type /XObject /Subtype /Form /BBox [0 0 500 700] /Resources << >> /Length 16 >>\nstream\n0 0 500 700 re f\nendstream",
	}
	objects = append(objects, extra...)
	return assemblePDF(objects)
}

func TestType3CharProcsPaintPathsAndKeepUnicodeWithoutFallbackInk(t *testing.T) {
	for _, mode := range []int{0, 1, 2, 3, 4, 5, 6, 7} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			pdf := type3TextPDF(fmt.Sprintf("1 0 0 rg BT /F1 45 Tf %d Tr 30 110 Td (ABAB) Tj ET", mode), "", "600 0 0 0 500 700 d1 0 0 500 700 re f", "600 0 d0 0 0 1 rg 0 0 m 500 0 l 250 700 l h f", "")
			doc, page, _ := textFidelityConvert(t, pdf)
			paths := layerPaths(page.Content().Layer[0])
			texts := layerTexts(page.Content().Layer[0])
			wantPaths := 4
			if mode == 3 || mode == 7 {
				wantPaths = 0
			}
			wantTexts := 4
			if wantPaths == 0 {
				wantTexts = 1
			}
			if len(paths) != wantPaths || len(texts) != wantTexts {
				t.Fatalf("Type3 graphics/semantics missing: paths=%d texts=%d", len(paths), len(texts))
			}
			for i, text := range texts {
				x, y := textModelOrigin(text)
				if math.Abs(x-(30+27*float64(i))) > .002 || math.Abs(y-90) > .002 {
					t.Fatalf("Type3 origin %d=(%g,%g)", i, x, y)
				}
			}
			if wantPaths == 0 {
				code := texts[0].TextCode[0]
				if code.Value != "ABAB" || len(code.DeltaX) != 3 {
					t.Fatalf("invisible Type3 semantics lost: %+v", code)
				}
				for _, delta := range code.DeltaX {
					if math.Abs(delta-27*pdfPointToMillimeter) > .001 {
						t.Fatalf("invisible Type3 advance=%g", delta)
					}
				}
			}
			img := textFidelityRaster(t, doc, page)
			if mode%4 == 1 || mode%4 == 2 {
				textFidelityCompareSourceReader(t, pdf, img, "pdftoppm")
			} else {
				textFidelityCompareSource(t, pdf, img)
			}
		})
	}
}

func TestType3FontMatrixSpacingRiseAndAffineBasis(t *testing.T) {
	pdf := type3TextPDF("BT /F1 30 Tf 50 Tz 2 Tc 7 Ts 1 0 .2 1 40 100 Tm [(A) 200 (B)] TJ ET", ".002 .0003 .0005 .001 0 0", "600 0 0 0 500 700 d1 0 0 500 700 re f", "600 0 d0 0 0 1 rg 0 0 m 500 0 l 250 700 l h f", "")
	doc, page, _ := textFidelityConvert(t, pdf)
	texts := layerTexts(page.Content().Layer[0])
	if len(texts) != 2 {
		t.Fatal("Type3 texts missing")
	}
	// 第一字宽的位移为 (19,5.4)，TJ 的 -3 只沿文字横轴。Ts 不参与推进。
	x, y := textModelOrigin(texts[1])
	if math.Abs(x-58.48) > .003 || math.Abs(y-87.6) > .003 {
		t.Fatalf("Type3 general FontMatrix advance=(%g,%g)", x, y)
	}
	textFidelityCompareSource(t, pdf, textFidelityRaster(t, doc, page))
}

func TestType3ResourceLookupUsesFontResourcesOrPageFallback(t *testing.T) {
	for _, resources := range []string{"", "/Resources << /XObject << /Shape 8 0 R >> >>"} {
		t.Run(resources, func(t *testing.T) {
			pdf := type3TextPDF("BT /F1 45 Tf 30 110 Td (AA) Tj ET", "", "600 0 d0 /Shape Do", "600 0 d0", resources)
			doc, page, _ := textFidelityConvert(t, pdf)
			if len(layerPaths(page.Content().Layer[0])) != 2 {
				t.Fatal("Type3 Form resource lost")
			}
			textFidelityCompareSource(t, pdf, textFidelityRaster(t, doc, page))
		})
	}
}

func TestType3CharProcPaintsImageAndKeepsGlyphStateLocal(t *testing.T) {
	imageObject := "<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceRGB /Length 12 >>\nstream\n\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\x00\nendstream"
	pdf := type3TextPDF("1 0 0 rg BT /F1 50 Tf 30 100 Td (AB) Tj ET", "", "600 0 d0 q 500 0 0 700 0 0 cm /Im Do Q 0 1 0 rg", "600 0 0 0 500 700 d1 0 0 500 700 re f", "/Resources << /XObject << /Im 9 0 R >> >>", imageObject)
	doc, page, _ := textFidelityConvert(t, pdf)
	if len(layerImages(page.Content().Layer[0])) != 1 || len(layerPaths(page.Content().Layer[0])) != 1 {
		t.Fatal("Type3 image or path lost")
	}
	img := textFidelityRaster(t, doc, page)
	// 低分辨率位图放大仍受既有 Interpolate 缺口影响；验证四角原色与字形位置，
	// 不把源阅读器的最近邻和 OFD 的平滑交界误记为整图一致。
	for i, point := range [][2]int{{65, 135}, {105, 135}, {65, 195}, {105, 195}} {
		want := []color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, G: 255, A: 255}}[i]
		got := img.RGBAAt(point[0], point[1])
		if absByteDifference(got.R, want.R) > 10 || absByteDifference(got.G, want.G) > 10 || absByteDifference(got.B, want.B) > 10 {
			t.Fatalf("Type3 bitmap corner %d=%v, want %v", i, got, want)
		}
	}
	if c := img.RGBAAt(140, 170); c.R < 240 || c.G > 20 || c.B > 20 {
		t.Fatalf("CharProc color leaked to next glyph: %v", c)
	}
}

func TestType3DoesNotAddTextClipForHighRenderingModes(t *testing.T) {
	for _, mode := range []int{4, 5, 6, 7} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			pdf := type3TextPDF(fmt.Sprintf("BT /F1 20 Tf %d Tr 30 100 Td (A) Tj ET 0 0 1 rg 0 0 200 200 re f", mode), "", "600 0 d0 0 0 500 700 re f", "600 0 d0", "")
			_, page, _ := textFidelityConvert(t, pdf)
			paths := layerPaths(page.Content().Layer[0])
			if len(paths) == 0 || paths[len(paths)-1].Clips != nil {
				t.Fatal("Type3 incorrectly changed clipping path")
			}
		})
	}
}

func TestType3RecursionUsesDepthBudget(t *testing.T) {
	pdf := type3TextPDF("BT /F1 20 Tf (A) Tj ET", "", "600 0 d0 BT /F1 20 Tf (A) Tj ET", "600 0 d0", "")
	var out bytes.Buffer
	err := ConvertWithOptions(t.Context(), pdf, &out, "", Options{Limits: Limits{MaxDepth: 4}})
	if !errors.Is(err, ErrLimitExceeded) || out.Len() != 0 {
		t.Fatalf("recursive Type3 not stopped: %v output=%d", err, out.Len())
	}
}

func TestType3BrokenCharProcReportsGlyphResourceAndStrictRejects(t *testing.T) {
	pdf := type3TextPDF("BT /F1 20 Tf (A) Tj ET", "", "0 0 500 700 re f", "600 0 d0", "")
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprint(strict), func(t *testing.T) {
			var out bytes.Buffer
			var ds []Diagnostic
			err := ConvertWithOptions(t.Context(), pdf, &out, "", Options{Strict: strict, OnDiagnostic: func(d Diagnostic) { ds = append(ds, d) }})
			if strict {
				if !errors.Is(err, ErrUnsupported) || out.Len() != 0 {
					t.Fatalf("strict=%v output=%d", err, out.Len())
				}
			} else if err != nil || out.Len() == 0 {
				t.Fatalf("ordinary=%v", err)
			}
			if len(ds) != 1 || ds[0].Code != "font.type3" || ds[0].Resource != "F1/A" || ds[0].Object != 6 {
				t.Fatalf("Type3 diagnostics=%+v", ds)
			}
		})
	}
}

func TestType3DiagnosticCancellationReturnsContextError(t *testing.T) {
	pdf := type3TextPDF("BT /F1 20 Tf (A) Tj ET", "", "600 0 0 0 500 700 d1 0 1 0 rg 0 0 500 700 re f", "600 0 d0", "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out bytes.Buffer
	err := ConvertWithOptions(ctx, pdf, &out, "", Options{OnDiagnostic: func(d Diagnostic) {
		if d.Code != "font.type3_color" {
			t.Fatalf("unexpected diagnostic: %+v", d)
		}
		cancel()
	}})
	if err != context.Canceled || out.Len() != 0 {
		t.Fatalf("Type3 callback cancellation=%v output=%d", err, out.Len())
	}
}

func TestType3AndClipOnlyTextSurviveRoundtripPDFExtraction(t *testing.T) {
	toolText := type3TextPDF("BT /F1 20 Tf 30 100 Td (ABAB) Tj ET", "", "600 0 d0 0 0 500 700 re f", "600 0 d0 0 0 m 500 0 l 250 700 l h f", "")
	for name, pdf := range map[string][]byte{"Type3": toolText, "clip only": embeddedTextPDF("BT /F1 20 Tf 7 Tr 30 100 Td (ABAB) Tj ET 0 0 200 200 re f")} {
		t.Run(name, func(t *testing.T) {
			var roundtrip bytes.Buffer
			doc, page, _ := textFidelityConvert(t, pdf)
			d := render.NewDocument(color.White, doc.Documents[0])
			defer d.Close()
			surface, err := d.Page(page)
			if err != nil {
				t.Fatal(err)
			}
			writer, err := render.NewPDFDocument(&roundtrip, render.PDFOptions{Compress: true, SubsetFonts: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := writer.AddPage(surface, nil); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			// 独立工具读取输出文字，验证可见图形之外仍保留原生文字层。
			if roundtrip.Len() == 0 {
				t.Fatal("no roundtrip PDF")
			}
			text := textFidelityExtractPDF(t, roundtrip.Bytes())
			if strings.ReplaceAll(strings.TrimSpace(text), " ", "") != "ABAB" {
				t.Fatalf("roundtrip extracted text=%q", text)
			}
		})
	}
}
