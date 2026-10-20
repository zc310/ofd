package pdf2ofd

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/zc310/ofd/internal/parser"
)

// 对照源 PDF、OFD 光栅和往返 PDF；平坦区域比较所有颜色分量，忽略抗锯齿边界。
func assertPDFImportMatchesSource(t *testing.T, pdf []byte, doc *parser.OFD, page *parser.Page, readers ...string) {
	t.Helper()
	transparencyCompareSource(t, pdf, textFidelityRaster(t, doc, page), "content", readers...)
	tool, err := exec.LookPath("mutool")
	if err != nil {
		t.Log("mutool unavailable: roundtrip raster comparison omitted")
		return
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "roundtrip.pdf"), filepath.Join(dir, "roundtrip.png")
	if err := os.WriteFile(input, textFidelityPDFOutput(t, doc, page), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), tool, "draw", "-q", "-r", "144", "-o", output, input).CombinedOutput(); err != nil {
		t.Fatalf("mutool: %v %s", err, out)
	}
	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	transparencyCompareSource(t, pdf, img, "content-roundtrip", readers...)
}

func TestLiteralStringContinuationPreservesTextAndAdvance(t *testing.T) {
	for _, newline := range []string{"\n", "\r", "\r\n"} {
		t.Run(fmt.Sprintf("%q", newline), func(t *testing.T) {
			pdf := embeddedTextPDF("BT /F1 10 Tf 10 100 Td (HELLO\\" + newline + "WORLD) Tj 0 0 1 rg (X) Tj ET")
			doc, page, _ := textFidelityConvert(t, pdf)
			texts := layerTexts(page.Content().Layer[0])
			if len(texts) != 2 || texts[0].TextCode[0].Value != "HELLOWORLD" || texts[1].TextCode[0].Value != "X" {
				t.Fatalf("unexpected text: %+v", texts)
			}
			marker := texts[1]
			x, y := marker.TextCode[0].X, marker.TextCode[0].Y
			if marker.CTM != nil {
				x, y = marker.CTM.Transform(x, y)
			}
			if math.Abs(marker.Boundary.X+x-110*pdfPointToMillimeter) > .001 || math.Abs(marker.Boundary.Y+y-100*pdfPointToMillimeter) > .001 {
				t.Fatalf("following text origin=(%g,%g), want (110,100)pt", marker.Boundary.X+x, marker.Boundary.Y+y)
			}
			assertPDFImportMatchesSource(t, pdf, doc, page)
			for name, data := range map[string][]byte{"source": pdf, "roundtrip": textFidelityPDFOutput(t, doc, page)} {
				text := textFidelityExtractPDF(t, data)
				text = strings.Map(func(r rune) rune {
					if unicode.IsSpace(r) {
						return -1
					}
					return r
				}, text)
				if text != "HELLOWORLDX" {
					t.Fatalf("%s extracted text=%q", name, text)
				}
			}
		})
	}
}
