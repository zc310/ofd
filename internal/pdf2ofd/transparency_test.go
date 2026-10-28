package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

func transparencyPDF(content, resources string, extra ...string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Font << /F1 5 0 R >> " + resources + " >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	return assemblePDF(append(objects, extra...))
}

func transparencyForm(content, group, resources string) string {
	return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [10 10 90 90] /Group << /S /Transparency /CS /DeviceRGB %s >> /Resources << /Font << /F1 5 0 R >> %s >> /Length %d >>\nstream\n%s\nendstream", group, resources, len(content), content)
}

func transparencyConvert(t *testing.T, input []byte) (*parser.OFD, *parser.Page, []byte) {
	t.Helper()
	var output bytes.Buffer
	if err := Convert(t.Context(), input, &output, ""); err != nil {
		t.Fatal(err)
	}
	check, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	if report := check.ValidateReader(t.Context(), bytes.NewReader(output.Bytes()), "transparency"); report.Status != validator.StatusValid {
		t.Fatalf("strict OFD validation: %+v", report)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	return ofd, page, output.Bytes()
}

// Compare all color channels in flat source regions, including midtones. The
// inset discards antialiasing and resampling boundaries, not transparency.
func transparencyCompareSource(t *testing.T, pdf []byte, got image.Image, name string, readers ...string) {
	t.Helper()
	if len(readers) == 0 {
		readers = []string{"mutool", "pdftoppm"}
	}
	for _, reader := range readers {
		tool, err := exec.LookPath(reader)
		if err != nil {
			t.Log(reader + " unavailable: source comparison omitted")
			continue
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
		source, err := png.Decode(f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		dx, dy := source.Bounds().Dx()-got.Bounds().Dx(), source.Bounds().Dy()-got.Bounds().Dy()
		if dx < -1 || dx > 1 || dy < -1 || dy > 1 {
			t.Fatalf("raster dimensions %v != %v", source.Bounds(), got.Bounds())
		}
		bad, total := 0, 0
		var first string
		for y := 4; y < min(source.Bounds().Dy(), got.Bounds().Dy())-4; y++ {
			for x := 4; x < min(source.Bounds().Dx(), got.Bounds().Dx())-4; x++ {
				c := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
				flat := true
				for _, pt := range [][2]int{{-3, 0}, {3, 0}, {0, -3}, {0, 3}} {
					n := color.NRGBAModel.Convert(source.At(x+pt[0], y+pt[1])).(color.NRGBA)
					if absByteDifference(c.R, n.R) > 1 || absByteDifference(c.G, n.G) > 1 || absByteDifference(c.B, n.B) > 1 {
						flat = false
						break
					}
				}
				if !flat {
					continue
				}
				total++
				g := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
				if absByteDifference(c.R, g.R) > 5 || absByteDifference(c.G, g.G) > 5 || absByteDifference(c.B, g.B) > 5 {
					bad++
					if first == "" {
						first = fmt.Sprintf("(%d,%d) source=%v OFD=%v", x, y, c, g)
					}
				}
			}
		}
		if dir := os.Getenv("OFD_TRANSPARENCY_EVIDENCE_DIR"); dir != "" {
			base := filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "-")+"-"+name)
			if err := os.WriteFile(base+".pdf", pdf, 0600); err != nil {
				t.Fatal(err)
			}
			for suffix, img := range map[string]image.Image{"-ofd": got, "-" + reader: source} {
				f, err := os.Create(base + suffix + ".png")
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, img)
				closeErr := f.Close()
				if err != nil {
					t.Fatal(err)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
			}
		}
		if total < 1000 || bad > total/100 {
			t.Fatalf("%s %s flat color differences=%d/%d first=%s", name, reader, bad, total, first)
		}
	}
}

func TestTransparencyBlendModesUseActualBackdropAndOpacity(t *testing.T) {
	for _, mode := range []string{"Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn", "HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity"} {
		t.Run(mode, func(t *testing.T) {
			input := transparencyPDF(".2 .6 .8 rg 10 10 80 80 re f .8 .3 .1 rg 45 10 45 80 re f q /GS gs .7 .4 .2 rg 20 20 60 60 re f Q", "/ExtGState << /GS 6 0 R >>", "<< /BM /"+mode+" /ca .65 >>")
			ofd, page, _ := transparencyConvert(t, input)
			img := textFidelityRaster(t, ofd, page)
			transparencyCompareSource(t, input, img, "source")
			transparencyCompareSource(t, textFidelityPDFOutput(t, ofd, page), img, "roundtrip")
		})
	}
}

func TestTransparencyGroupsOverlapIsolationKnockoutAndText(t *testing.T) {
	for _, isolate := range []bool{false, true} {
		for _, knockout := range []bool{false, true} {
			t.Run(fmt.Sprintf("I%t-K%t", isolate, knockout), func(t *testing.T) {
				form := "/Inner gs .8 .2 .4 rg 10 10 55 60 re f .3 .7 .5 rg 35 35 55 55 re f"
				label := "BT /F1 9 Tf 15 95 Td (Label) Tj ET"
				if !knockout {
					form += " BT /F1 9 Tf 15 82 Td (Label) Tj ET"
					label = ""
				}
				input := transparencyPDF(".2 .6 .8 rg 10 10 80 80 re f q /Outer gs /Fm Do Q 1 0 0 rg 92 10 5 5 re f "+label, "/ExtGState << /Outer 6 0 R >> /XObject << /Fm 7 0 R >>",
					"<< /ca .6 /BM /Normal >>", transparencyForm(form, fmt.Sprintf("/I %t /K %t", isolate, knockout), "/ExtGState << /Inner 8 0 R >>"), "<< /ca .5 /BM /Multiply >>")
				ofd, page, _ := transparencyConvert(t, input)
				img := textFidelityRaster(t, ofd, page)
				if knockout {
					t.Log("Poppler 22.12 ignores K for overlapping group rectangles; compare knockout against MuPDF")
					transparencyCompareSource(t, input, img, "source", "mutool")
				} else {
					transparencyCompareSource(t, input, img, "source")
				}
				pdf := textFidelityPDFOutput(t, ofd, page)
				if text := textFidelityExtractPDF(t, pdf); !strings.Contains(text, "Label") {
					t.Fatalf("group text lost: %q", text)
				}
				transparencyCompareSource(t, pdf, img, "roundtrip")
			})
		}
	}
}

func TestTransparencyVectorMasksKeepCTMBackgroundTransferAndScope(t *testing.T) {
	for _, kind := range []string{"Alpha", "Luminosity"} {
		for _, transfer := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-TR%t", kind, transfer), func(t *testing.T) {
				tr := ""
				if transfer {
					tr = "/TR << /FunctionType 2 /Domain [0 1] /Range [.2 .8] /C0 [1] /C1 [0] /N 1 >>"
				}
				mask := "/Half gs 1 1 1 rg 20 20 30 60 re f 0 0 0 rg 50 20 30 60 re f"
				bc := "/BC [.2 .4 .6]"
				if kind == "Alpha" {
					bc = "/BC [0 0 0]"
				}
				input := transparencyPDF(".2 .6 .8 rg 0 0 100 100 re f q 1 0 0 1 5 0 cm /Mask gs 1 0 0 1 -5 0 cm .8 .2 .4 rg 10 10 80 80 re f Q 1 0 0 rg 92 10 5 5 re f", "/ExtGState << /Mask 6 0 R >>",
					"<< /SMask << /S /"+kind+" /G 7 0 R "+bc+" "+tr+" >> >>", transparencyForm(mask, "/I true", "/ExtGState << /Half 8 0 R >>"), "<< /ca .5 >>")
				ofd, page, _ := transparencyConvert(t, input)
				img := textFidelityRaster(t, ofd, page)
				if transfer {
					t.Log("MuPDF 1.21 ignores Luminosity TR and clips inverted Alpha outside G; compare TR against Poppler")
					transparencyCompareSource(t, input, img, "source", "pdftoppm")
				} else {
					transparencyCompareSource(t, input, img, "source")
				}
				transparencyCompareSource(t, textFidelityPDFOutput(t, ofd, page), img, "roundtrip")
			})
		}
	}
}

func TestTransparencyStrictBudgetsCancellationAndMaskCycles(t *testing.T) {
	input := transparencyPDF("/Fm Do", "/XObject << /Fm 6 0 R >>", transparencyForm("1 0 0 rg 10 10 80 80 re f 0 0 1 rg 30 30 30 30 re f", "/I true", ""))
	for resource, limits := range map[string]Limits{"image_pixels": {MaxImagePixels: 100}, "total_image_pixels": {MaxTotalImagePixels: 100}, "operations": {MaxOperations: 1000}, "output_objects": {MaxOutputObjects: 1}} {
		requireBudgetError(t, input, limits, resource)
	}
	var output bytes.Buffer
	err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true})
	if !errors.Is(err, ErrUnsupported) || output.Len() != 0 {
		t.Fatalf("Strict: %v output=%d", err, output.Len())
	}
	ctx, cancel := context.WithCancel(t.Context())
	err = ConvertWithOptions(ctx, input, &output, "", Options{OnDiagnostic: func(Diagnostic) { cancel() }})
	if err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancel: %v output=%d", err, output.Len())
	}
	cycle := transparencyPDF("/GS gs 10 10 80 80 re f", "/ExtGState << /GS 6 0 R >>", "<< /SMask << /S /Alpha /G 7 0 R >> >>", transparencyForm("/GS gs 10 10 80 80 re f", "/I true", "/ExtGState << /GS 6 0 R >>"))
	requireBudgetError(t, cycle, Limits{MaxDepth: 4}, "form_depth")
}

func TestTransparencyNestedGroupsPreserveParentBackdropAndChildShape(t *testing.T) {
	for _, knockout := range []bool{false, true} {
		t.Run(fmt.Sprint(knockout), func(t *testing.T) {
			input := transparencyPDF(".2 .6 .8 rg 10 10 80 80 re f q /Outer gs /Parent Do Q", "/ExtGState << /Outer 6 0 R >> /XObject << /Parent 7 0 R >>",
				"<< /ca .8 /BM /Overlay >>",
				transparencyForm(".8 .2 .4 rg 10 10 55 60 re f /Child gs /Nested Do", fmt.Sprintf("/I false /K %t", knockout), "/ExtGState << /Child 8 0 R >> /XObject << /Nested 9 0 R >>"),
				"<< /ca .6 /BM /Screen >>",
				transparencyForm("/Paint gs .3 .7 .5 rg 35 35 55 55 re f .6 .2 .9 rg 55 15 30 45 re f", "/I true", "/ExtGState << /Paint 10 0 R >>"),
				"<< /ca .5 /BM /Normal >>")
			ofd, page, _ := transparencyConvert(t, input)
			img := textFidelityRaster(t, ofd, page)
			if knockout {
				transparencyCompareSource(t, input, img, "source", "mutool")
			} else {
				transparencyCompareSource(t, input, img, "source")
			}
			transparencyCompareSource(t, textFidelityPDFOutput(t, ofd, page), img, "roundtrip")
		})
	}
}

func TestTransparencyMaskRotationNoneAndOwnImageMask(t *testing.T) {
	input := transparencyPDF(".2 .6 .8 rg 0 0 100 100 re f q /Mask gs .8 .2 .4 rg 10 10 80 80 re f /Clear gs 1 0 0 rg 10 10 8 8 re f Q", "/ExtGState << /Mask 6 0 R /Clear 9 0 R >>",
		"<< /SMask << /S /Alpha /G 7 0 R >> >>",
		transparencyForm("q .8 .2 -.2 .8 20 0 cm /Half gs 20 20 30 60 re f Q", "/I true", "/ExtGState << /Half 8 0 R >>"),
		"<< /ca .5 >>", "<< /SMask /None >>")
	input = bytes.Replace(input, []byte("/MediaBox [0 0 100 100]"), []byte("/MediaBox [0 0 100 100] /Rotate 90"), 1)
	ofd, page, _ := transparencyConvert(t, input)
	img := textFidelityRaster(t, ofd, page)
	transparencyCompareSource(t, input, img, "source")
	transparencyCompareSource(t, textFidelityPDFOutput(t, ofd, page), img, "roundtrip")
	// A real vector mask is overridden by the image-associated SMask. Strict
	// must not rasterize or diagnose an unused graphics-state mask.
	own := transparencyPDF("q /Mask gs 60 0 0 60 20 20 cm /Image Do Q", "/ExtGState << /Mask 6 0 R >> /XObject << /Image 9 0 R >>",
		"<< /SMask << /S /Alpha /G 7 0 R >> >>", transparencyForm("/Half gs 20 20 30 60 re f", "/I true", "/ExtGState << /Half 8 0 R >>"), "<< /ca .5 >>",
		budgetImage(1, 1, "/SMask 10 0 R", []byte{255, 0, 0}),
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray /Length 1 >>\nstream\n\x80\nendstream")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), own, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	if c := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(0, 0); c.A != 128 {
		t.Fatalf("own mask alpha=%d", c.A)
	}
}

func TestTransparencyKnockoutDistinguishesOpacityFromShape(t *testing.T) {
	for _, ais := range []bool{false, true} {
		t.Run(fmt.Sprint(ais), func(t *testing.T) {
			input := transparencyPDF(".2 .6 .8 rg 10 10 80 80 re f /Fm Do", "/XObject << /Fm 6 0 R >>",
				transparencyForm(".8 .2 .4 rg 10 10 55 60 re f /Zero gs .3 .7 .5 rg 35 35 55 55 re f", "/I true /K true", "/ExtGState << /Zero 7 0 R >>"), fmt.Sprintf("<< /ca .5 /AIS %t >>", ais))
			ofd, page, _ := transparencyConvert(t, input)
			img := textFidelityRaster(t, ofd, page)
			if !ais {
				transparencyCompareSource(t, input, img, "source", "mutool")
			} else {
				// MuPDF 1.21 ignores AIS. With alpha interpreted as shape, half
				// coverage retains half the earlier red member in the overlap.
				c := img.RGBAAt(100, 100)
				if absByteDifference(c.R, 140) > 3 || absByteDifference(c.G, 115) > 3 || absByteDifference(c.B, 115) > 3 {
					t.Fatalf("AIS shape overlap=%v", c)
				}
			}
		})
	}
}

func TestTransparencySeparateFillAndStrokeAlpha(t *testing.T) {
	input := transparencyPDF(".2 .6 .8 rg 0 0 100 100 re f /GS gs .8 .2 .4 rg 0 0 0 RG 8 w 25 25 50 50 re B", "/ExtGState << /GS 6 0 R >>", "<< /ca .4 /CA 1 >>")
	ofd, page, _ := transparencyConvert(t, input)
	img := textFidelityRaster(t, ofd, page)
	transparencyCompareSource(t, input, img, "source")
	transparencyCompareSource(t, textFidelityPDFOutput(t, ofd, page), img, "roundtrip")
}

func TestTransparencyAnnotationGroupUsesAppearancePlacement(t *testing.T) {
	input := transparencyPDF(".2 .6 .8 rg 0 0 100 100 re f", "",
		transparencyForm(".8 .2 .4 rg 10 10 55 60 re f /Half gs .3 .7 .5 rg 35 35 55 55 re f", "/I true /K true", "/ExtGState << /Half 7 0 R >>"),
		"<< /ca .5 >>", "<< /Type /Annot /Subtype /Stamp /Rect [20 20 80 80] /AP << /N 6 0 R >> >>")
	input = bytes.Replace(input, []byte("/MediaBox [0 0 100 100]"), []byte("/MediaBox [0 0 100 100] /Annots [8 0 R]"), 1)
	ofd, page, _ := transparencyConvert(t, input)
	img := textFidelityRaster(t, ofd, page)
	transparencyCompareSource(t, input, img, "source", "mutool")
	transparencyCompareSource(t, textFidelityPDFOutput(t, ofd, page), img, "roundtrip")
}

func TestTransparencyConstantBlackMaskMakesImagesTransparent(t *testing.T) {
	data, format, err := encodePNGImage(image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	if err != nil {
		t.Fatal(err)
	}
	p := pdfInterpreter{
		page:  &creator.Page{},
		state: pdfGraphicsState{ctm: identityPDFMatrix(), fillAlpha: 1, strokeAlpha: 1, groupAlpha: 1, constantMaskBlack: true},
	}
	if err := p.appendImageData(data, format); err != nil {
		t.Fatal(err)
	}
	if len(p.page.Items) != 1 {
		t.Fatalf("items=%d", len(p.page.Items))
	}
	img, ok := p.page.Items[0].(creator.Image)
	if !ok || img.Alpha == nil || *img.Alpha != 0 {
		t.Fatalf("image alpha=%v", img.Alpha)
	}
}

func TestTransparencyConstantBlackMaskKeepsVectorPaintTransparent(t *testing.T) {
	input := imageGraphicsPriorityPDF(t, "no image mask", "q /GS1 gs 1 0 0 rg 20 20 60 60 re f Q")
	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ofd.Close() }()
	page := ofd.Documents[0].Pages[0]
	img := textFidelityRaster(t, ofd, page)
	transparencyCompareSource(t, input, img, "source")
}

func TestTransparencyConstantBlackMaskFastPathAcceptsExplicitBlackBackgroundAndIdentityTransfer(t *testing.T) {
	base := imageGraphicsPriorityPDF(t, "no image mask", "")
	original := []byte("<< /ca .5 /SMask << /S /Luminosity /G 8 0 R >> >>")
	for _, tc := range []struct {
		name  string
		extra string
		want  bool
	}{
		{"bc_black", "/BC [0]", true},
		{"tr_identity", "/TR /Identity", true},
		{"bc_black_tr_identity", "/BC [0] /TR /Identity", true},
		{"bc_white", "/BC [1]", false},
		{"tr_nonzero", "/TR << /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replacement := []byte("<< /ca .5 /SMask << /S /Luminosity /G 8 0 R " + tc.extra + " >> >>")
			input := bytes.Replace(base, original, replacement, 1)
			ctx, err, panicked := readPDFContext(t.Context(), input, model.NewDefaultConfiguration())
			if panicked || err != nil {
				t.Fatalf("read context: panicked=%v err=%v", panicked, err)
			}
			gs, err := ctx.XRefTable.DereferenceDict(ctx.XRefTable.Table[7].Object)
			if err != nil {
				t.Fatal(err)
			}
			options, _ := (Options{}).Normalize()
			p := pdfInterpreter{ctx: ctx, control: &conversionControl{ctx: t.Context(), options: options}, state: pdfGraphicsState{ctm: identityPDFMatrix(), fillAlpha: 1, strokeAlpha: 1, groupAlpha: 1}}
			p.parseTransparencyMask(gs["SMask"], nil)
			if p.state.constantMaskBlack != tc.want {
				t.Fatalf("constantMaskBlack=%v, want %v", p.state.constantMaskBlack, tc.want)
			}
		})
	}
}
