package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	fontparser "github.com/zc310/font"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"golang.org/x/image/font/gofont/goregular"
)

func textFidelityPDF(content, font, extra string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content), font,
	}
	if extra != "" {
		objects = append(objects, extra)
	}
	return assemblePDF(objects)
}

func embeddedTextPDF(content string) []byte {
	font := "<< /Type /Font /Subtype /TrueType /BaseFont /GoRegular /Encoding /WinAnsiEncoding /FirstChar 0 /LastChar 255 /Widths [" + strings.Repeat("1000 ", 256) + "] /FontDescriptor 6 0 R >>"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content), font,
		"<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /Ascent 1000 /Descent -200 /FontBBox [0 -200 1100 1000] /CapHeight 800 /ItalicAngle 0 /StemV 80 /FontFile2 7 0 R >>",
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(goregular.TTF), len(goregular.TTF), goregular.TTF),
	}
	return assemblePDF(objects)
}

func textFidelityConvert(t *testing.T, pdf []byte) (*parser.OFD, *parser.Page, []byte) {
	t.Helper()
	var out bytes.Buffer
	if err := ConvertWithOptions(t.Context(), pdf, &out, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	doc, err := parser.NewOFD(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = doc.Close() })
	page := doc.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	return doc, page, out.Bytes()
}

func textFidelityRaster(t *testing.T, doc *parser.OFD, page *parser.Page) *image.RGBA {
	t.Helper()
	d := render.NewDocument(color.White, doc.Documents[0])
	t.Cleanup(func() { d.Close() })
	img, err := d.RasterizePage(page, render.BackendCanvas, geom.DPMM(144/25.4))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// 固定内嵌字体，直接与独立源阅读器比较，忽略抗锯齿边界但不忽略实心区域。
func textFidelityCompareSource(t *testing.T, pdf []byte, got *image.RGBA) {
	textFidelityCompareSourceReader(t, pdf, got, "mutool")
}

func textFidelityCompareSourceReader(t *testing.T, pdf []byte, got *image.RGBA, reader string) {
	t.Helper()
	tool, err := exec.LookPath(reader)
	if err != nil {
		t.Log(reader + " unavailable: source comparison omitted")
		return
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "source.pdf"), filepath.Join(dir, "source.png")
	if err := os.WriteFile(input, pdf, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"draw", "-q", "-r", "144", "-o", output, input}
	if reader == "pdftoppm" {
		args = []string{"-singlefile", "-r", "144", "-png", input, strings.TrimSuffix(output, ".png")}
	}
	if out, err := exec.CommandContext(t.Context(), tool, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v %s", reader, err, out)
	}
	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	source, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	bad, total := 0, 0
	for y := 2; y < min(source.Bounds().Dy(), got.Bounds().Dy())-2; y++ {
		for x := 2; x < min(source.Bounds().Dx(), got.Bounds().Dx())-2; x++ {
			c := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			solid := func(v uint8) bool { return v < 3 || v > 252 }
			if !solid(c.R) || !solid(c.G) || !solid(c.B) {
				continue
			}
			g := got.RGBAAt(x, y)
			total++
			if absByteDifference(c.R, g.R) > 30 || absByteDifference(c.G, g.G) > 30 || absByteDifference(c.B, g.B) > 30 {
				bad++
			}
		}
	}
	if bad > total/100 {
		t.Fatalf("source solid pixels differ: %d/%d", bad, total)
	}
}

func absByteDifference(a, b byte) int { return int(math.Abs(float64(a) - float64(b))) }

func TestTextClippingAccumulatesAtETAndRestoresScope(t *testing.T) {
	for _, mode := range []int{4, 5, 6, 7} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			pdf := embeddedTextPDF(fmt.Sprintf("q BT /F1 40 Tf %d Tr 30 100 Td (O) Tj 40 0 Td (O) Tj 0 Tr 40 0 Td (A) Tj ET 0 0 1 rg 0 0 200 200 re f Q 1 0 0 rg 10 10 5 5 re f", mode))
			doc, page, _ := textFidelityConvert(t, pdf)
			texts := layerTexts(page.Content().Layer[0])
			paths := layerPaths(page.Content().Layer[0])
			var values strings.Builder
			for _, text := range texts {
				if text.Clips != nil {
					t.Fatal("glyph clip became active before ET")
				}
				for _, code := range text.TextCode {
					values.WriteString(code.Value)
				}
			}
			if values.String() != "OOA" || len(paths) != 2 || paths[0].Clips == nil || paths[1].Clips != nil {
				t.Fatalf("ET/Q scope: texts=%d paths=%+v", len(texts), paths)
			}
			clip := paths[0].Clips
			if len(clip.Clip) != 1 || len(clip.Clip[0].Area) != 1 || clip.Clip[0].Area[0].Path == nil {
				t.Fatalf("glyph union missing: %+v", clip)
			}
			img := textFidelityRaster(t, doc, page)
			textFidelityCompareSource(t, pdf, img)
			// O 的洞和两个 O 之间的空隙必须保持白色，不能使用字框代替字形。
			for _, pt := range [][2]int{{88, 180}, {128, 180}} {
				c := img.RGBAAt(pt[0], pt[1])
				if c.R < 240 || c.G < 240 || c.B < 240 {
					t.Fatalf("glyph hole/gap painted at %v: %v", pt, c)
				}
			}
		})
	}
}

func TestTextClippingEmptyGlyphSuppressesPaintUntilQ(t *testing.T) {
	pdf := embeddedTextPDF("q BT /F1 20 Tf 7 Tr ( ) Tj ET 0 0 200 200 re f Q 0 0 1 rg 10 10 10 10 re f")
	_, page, _ := textFidelityConvert(t, pdf)
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 1 || paths[0].Clips != nil {
		t.Fatalf("empty glyph did not clip all paint: %+v", paths)
	}
}

func TestEmptyTextClipKeepsFollowingTextSemantics(t *testing.T) {
	pdf := embeddedTextPDF("BT /F1 20 Tf 7 Tr ( ) Tj ET BT 0 Tr 30 100 Td (ABAB) Tj ET")
	doc, page, _ := textFidelityConvert(t, pdf)
	text := textFidelityExtractPDF(t, textFidelityPDFOutput(t, doc, page))
	if strings.TrimSpace(text) != "ABAB" {
		t.Fatalf("clipped text semantics lost: %q", text)
	}
	img := textFidelityRaster(t, doc, page)
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if c := img.RGBAAt(x, y); c.R != 255 || c.G != 255 || c.B != 255 {
				t.Fatalf("empty clip painted text: %v", c)
			}
		}
	}
}

func TestTextClippingPreservesAffineGlyphBasis(t *testing.T) {
	for _, matrix := range []string{"1 0 .35 1 30 100", "0 1 -1 0 120 30", "-1 0 0 1 120 100"} {
		t.Run(matrix, func(t *testing.T) {
			pdf := embeddedTextPDF("q BT /F1 45 Tf 65 Tz 5 Ts 7 Tr " + matrix + " Tm (Rg) Tj ET 0 0 1 rg 0 0 200 200 re f Q")
			doc, page, _ := textFidelityConvert(t, pdf)
			textFidelityCompareSource(t, pdf, textFidelityRaster(t, doc, page))
		})
	}
}

func TestTextClippingNonEmbeddedFontUsesPositionedTextAreas(t *testing.T) {
	pdf := textFidelityPDF("BT /F1 20 Tf 7 Tr 30 100 Td (AA) Tj ET 0 0 200 200 re f", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", "")
	_, page, _ := textFidelityConvert(t, pdf)
	paths := layerPaths(page.Content().Layer[0])
	areas := paths[0].Clips.Clip[0].Area
	if len(areas) != 2 || areas[0].Text == nil || areas[1].Text == nil || areas[0].CTM == nil || areas[1].CTM == nil {
		t.Fatalf("missing per-code text areas: %+v", areas)
	}
	x0, _ := areas[0].CTM.Transform(0, 0)
	x1, _ := areas[1].CTM.Transform(0, 0)
	if math.Abs((x1-x0)-13.34*pdfPointToMillimeter) > .001 {
		t.Fatalf("PDF width not applied: %+v", areas)
	}
}

func TestSemanticFontHasDistinctUnicodeAndEmptyContours(t *testing.T) {
	data, err := pdfSemanticFont(map[uint32]string{1: "A中𠀀"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := fontparser.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uint16]bool{}
	for _, r := range "A中𠀀" {
		g := f.GlyphIndex(r)
		if g == 0 || seen[g] {
			t.Fatalf("Unicode %U has shared/missing glyph %d", r, g)
		}
		seen[g] = true
		p := pdfGlyphPather{matrix: identityPDFMatrix()}
		if err := f.GlyphPath(&p, g, 0, 0, 0, 1, fontparser.NoHinting); err != nil || len(p.commands) != 0 {
			t.Fatalf("semantic glyph has ink: %v %+v", err, p.commands)
		}
	}
}

func TestTextClippingHonorsOperationBudgetAndCancellation(t *testing.T) {
	pdf := embeddedTextPDF("BT /F1 20 Tf 7 Tr 30 100 Td (OOOO) Tj ET 0 0 200 200 re f")
	var out bytes.Buffer
	err := ConvertWithOptions(t.Context(), pdf, &out, "", Options{Limits: Limits{MaxOperations: 30}})
	if !errors.Is(err, ErrLimitExceeded) || out.Len() != 0 {
		t.Fatalf("budget=%v output=%d", err, out.Len())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = Convert(ctx, pdf, &out, "")
	if err != context.Canceled {
		t.Fatalf("cancellation=%v", err)
	}
}

func textModelOrigin(text models.TextObject) (float64, float64) {
	code := text.TextCode[0]
	x, y := code.X, code.Y
	if text.CTM != nil {
		x, y = text.CTM.Transform(x, y)
	}
	return (x + text.Boundary.X) / pdfPointToMillimeter, (y + text.Boundary.Y) / pdfPointToMillimeter
}

func textFidelityExtractPDF(t *testing.T, data []byte) string {
	t.Helper()
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext unavailable")
	}
	input := filepath.Join(t.TempDir(), "roundtrip.pdf")
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), tool, "-layout", input, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func textFidelityPDFOutput(t *testing.T, doc *parser.OFD, page *parser.Page) []byte {
	t.Helper()
	var out bytes.Buffer
	d := render.NewDocument(color.White, doc.Documents[0])
	defer d.Close()
	surface, err := d.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := render.NewPDFDocument(&out, render.PDFOptions{Compress: true, SubsetFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.AddPage(surface, nil); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// 可选导出独立视觉证据；普通测试只在临时目录验证，不写仓库样本。
func TestTextFidelityEvidenceSources(t *testing.T) {
	dir := os.Getenv("OFD_TEXT_EVIDENCE_DIR")
	if dir == "" {
		t.Skip("optional visual artifacts")
	}
	imageObject := "<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /BitsPerComponent 8 /ColorSpace /DeviceRGB /Length 12 >>\nstream\n\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\x00\nendstream"
	sources := map[string][]byte{
		"clip":        embeddedTextPDF("q BT /F1 60 Tf 7 Tr 30 100 Td (RgO) Tj ET 0 0 1 rg 0 0 200 200 re f Q"),
		"vertical":    verticalTextPDF(t, "BT /F1 35 Tf 80 Tz 3 Tc 100 175 Td [<00010002> 200 <00030004>] TJ ET", "/DW2 [950 -1300] /W2 [1 [-1200 300 700 -900 500 800] 3 3 -1100 400 900]"),
		"type3":       type3TextPDF("1 0 0 rg BT /F1 50 Tf 30 100 Td (ABAB) Tj ET", "", "600 0 0 0 500 700 d1 0 0 500 700 re f", "600 0 d0 0 0 1 rg 0 0 m 500 0 l 250 700 l h f", ""),
		"type3-image": type3TextPDF("1 0 0 rg BT /F1 50 Tf 30 100 Td (AB) Tj ET", "", "600 0 d0 q 500 0 0 700 0 0 cm /Im Do Q 0 1 0 rg", "600 0 0 0 500 700 d1 0 0 500 700 re f", "/Resources << /XObject << /Im 9 0 R >> >>", imageObject),
	}
	for name, pdf := range sources {
		t.Run(name, func(t *testing.T) {
			doc, page, ofd := textFidelityConvert(t, pdf)
			img := textFidelityRaster(t, doc, page)
			roundtrip := textFidelityPDFOutput(t, doc, page)
			var pngOut bytes.Buffer
			if err := png.Encode(&pngOut, img); err != nil {
				t.Fatal(err)
			}
			for extension, data := range map[string][]byte{"-source.pdf": pdf, ".ofd": ofd, ".png": pngOut.Bytes(), "-roundtrip.pdf": roundtrip, "-text.txt": []byte(textFidelityExtractPDF(t, roundtrip))} {
				if err := os.WriteFile(filepath.Join(dir, name+extension), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
