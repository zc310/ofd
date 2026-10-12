package pdf2ofd

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
)

func TestTransparencyMaskPreservesInheritedXObjectNamedMask(t *testing.T) {
	for _, kind := range []string{"Alpha", "Luminosity"} {
		for _, subtype := range []string{"Image", "Form"} {
			for _, indirect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-%s-indirect%t", kind, subtype, indirect), func(t *testing.T) {
					// The mask Form inherits resources from the page. Its /Mask Do
					// must resolve to object 8, not a temporary alias for itself.
					body := "/Half gs q 60 0 0 60 20 20 cm /Mask Do Q"
					mask := fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [10 10 90 90] /Group << /S /Transparency /CS /DeviceRGB /I true >> /Length %d >>\nstream\n%s\nendstream", len(body), body)
					original := "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray /Length 1 >>\nstream\n\x80\nendstream"
					if subtype == "Form" {
						paint := ".5 g 0 0 1 1 re f"
						original = fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Length %d >>\nstream\n%s\nendstream", len(paint), paint)
					}
					objects := []string{"<< /SMask << /S /" + kind + " /G 7 0 R >> >>", mask, original, "<< /ca .5 >>"}
					xobjects := "<< /Mask 8 0 R >>"
					if indirect {
						objects = append(objects, xobjects)
						xobjects = "10 0 R"
					}
					input := transparencyPDF(".2 .6 .8 rg 0 0 100 100 re f q /Apply gs .8 .2 .4 rg 10 10 40 80 re f 50 10 40 80 re f 0 g BT /F1 9 Tf 24 50 Td (MaskText) Tj ET Q q 8 0 0 8 90 2 cm /Mask Do Q", "/ExtGState << /Apply 6 0 R /Half 9 0 R >> /XObject "+xobjects, objects...)
					ofd, page, _ := transparencyConvert(t, input)
					img := textFidelityRaster(t, ofd, page)
					want := color.NRGBA{128, 102, 153, 255}
					if kind == "Luminosity" {
						want = color.NRGBA{89, 128, 178, 255}
					}
					for _, x := range []int{70, 130} {
						got := color.NRGBAModel.Convert(img.At(x, 70)).(color.NRGBA)
						if absByteDifference(got.R, want.R) > 3 || absByteDifference(got.G, want.G) > 3 || absByteDifference(got.B, want.B) > 3 {
							t.Fatalf("inherited mask at (%d,70): got=%v want=%v", x, got, want)
						}
					}
					got := color.NRGBAModel.Convert(img.At(188, 188)).(color.NRGBA)
					if absByteDifference(got.R, 128) > 2 || absByteDifference(got.G, 128) > 2 || absByteDifference(got.B, 128) > 2 {
						t.Fatalf("page XObject changed after mask use: %v", got)
					}
					transparencyCompareSource(t, input, img, "source")
					pdf := textFidelityPDFOutput(t, ofd, page)
					transparencyCompareSource(t, pdf, img, "roundtrip")
					if text := textFidelityExtractPDF(t, pdf); strings.TrimSpace(text) != "MaskText" {
						t.Fatalf("masked text lost: %q", text)
					}
				})
			}
		}
	}
}
