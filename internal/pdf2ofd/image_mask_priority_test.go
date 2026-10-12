package pdf2ofd

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

func TestPDFJPXEmbeddedOpacityIgnoresExplicitMaskBeforeDereferencing(t *testing.T) {
	for _, mode := range []int{1, 2} {
		for _, key := range []string{"Mask"} {
			for _, invalid := range []types.Object{types.Name("Invalid"), *types.NewIndirectRef(99, 0), &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1000000), "Height": types.Integer(1000000), "BitsPerComponent": types.Integer(8), "ColorSpace": types.Name("DeviceGray")}, Raw: []byte("invalid compressed mask"), FilterPipeline: []types.PDFFilter{{Name: "FlateDecode"}}}} {
				t.Run(fmt.Sprintf("mode%d_%s_%T", mode, key, invalid), func(t *testing.T) {
					pixel := color.NRGBA64{R: 65535, A: 32769}
					if mode == 2 {
						pixel.R = pixel.A
					}
					stream := jpxAlphaStream(t, 16, mode, pixel)
					stream.Dict[key] = invalid
					var diagnostics []Diagnostic
					options, _ := (Options{Strict: true, Limits: Limits{MaxImagePixels: 64, MaxTotalImagePixels: 64}, OnDiagnostic: func(d Diagnostic) { diagnostics = append(diagnostics, d) }}).Normalize()
					p := pdfInterpreter{ctx: &model.Context{XRefTable: &model.XRefTable{Table: map[int]*model.XRefTableEntry{}}}, control: &conversionControl{ctx: t.Context(), options: options}}
					img := jpxDecodedImage(t, &p, stream)
					if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{R: 65535, A: 32769}) {
						t.Fatalf("embedded opacity replaced: %#v", got)
					}
					if len(diagnostics) != 0 || p.control.pixels != 64 {
						t.Fatalf("ignored mask diagnostics=%v pixels=%d", diagnostics, p.control.pixels)
					}
				})
			}
		}
	}
}

func TestPDFJPXZeroSMaskInDataUsesExternalSoftMask(t *testing.T) {
	stream := jpxAlphaStream(t, 16, 0, color.NRGBA64{R: 65535, A: 32769})
	mask := jpegMatteStream("DeviceGray", bytes.Repeat([]byte{64}, 64), false)
	stream.Dict["SMask"] = mask
	img := jpxDecodedImage(t, &pdfInterpreter{}, stream)
	if got := color.NRGBA64Model.Convert(img.At(4, 4)).(color.NRGBA64); got != (color.NRGBA64{R: 65535, A: 64 * 257}) {
		t.Fatalf("external SMask ignored: %#v", got)
	}
}

func embeddedMaskPriorityPDF(t *testing.T, content string) []byte {
	t.Helper()
	data := jpxAlphaStream(t, 16, 1, color.NRGBA64{R: 65535, A: 32769}).Raw
	return budgetImagePDF(content,
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /SMaskInData 1 /Mask 7 0 R /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(data), data),
		budgetImage(1024, 1024, "/Filter /FlateDecode", []byte("corrupt ignored soft mask")),
		maskImage(1024, 1024, "/Filter /FlateDecode", []byte("corrupt ignored explicit mask")))
}

func TestPDFJPXEmbeddedMaskPriorityUsesOnlyBaseDecodeAndCache(t *testing.T) {
	var output bytes.Buffer
	err := ConvertWithOptions(t.Context(), embeddedMaskPriorityPDF(t, "/Im0 Do /Im0 Do"), &output, "", Options{Strict: true, Limits: Limits{MaxImagePixels: 64, MaxTotalImagePixels: 64, MaxOperations: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeFirstPageImage(t, output.Bytes()).NRGBAAt(4, 4); got.R != 255 || got.G != 0 || got.B != 0 || got.A < 127 || got.A > 128 {
		t.Fatalf("priority/cached alpha=%v", got)
	}
}

func TestPDFImageOwnSoftMaskOverridesGraphicsMaskForAllDrawingPaths(t *testing.T) {
	for _, kind := range []string{"embedded", "external", "external None", "no image mask"} {
		t.Run(kind, func(t *testing.T) {
			options, _ := (Options{Strict: true}).Normalize()
			page := creator.Page{}
			p := pdfInterpreter{page: &page, control: &conversionControl{ctx: t.Context(), options: options}, state: pdfGraphicsState{ctm: identityPDFMatrix(), fillAlpha: 1, strokeAlpha: 1, groupAlpha: 1}}
			// 故意不可解码的图形状态掩码：有图像软掩码时不能触发预算或诊断。
			p.state.lumMask = &types.StreamDict{Dict: types.Dict{"Width": types.Integer(1000000), "Height": types.Integer(1000000)}}
			stream := jpegMatteStream("DeviceRGB", bytes.Repeat([]byte{255, 0, 0}, 64), false)
			if kind == "embedded" {
				stream = jpxAlphaStream(t, 16, 1, color.NRGBA64{R: 65535, A: 32769})
			}
			if kind == "external" {
				stream.Dict["SMask"] = jpegMatteStream("DeviceGray", bytes.Repeat([]byte{128}, 64), false)
			}
			if kind == "external None" {
				stream.Dict["SMask"] = types.Name("None")
			}
			data, format, err := p.imageData(stream, pdfColor{})
			if err != nil {
				t.Fatal(err)
			}
			_ = p.appendImageWithGraphicsMask(stream, data, format)
			own := kind == "embedded" || kind == "external"
			if own {
				if err := p.control.check(); err != nil {
					t.Fatalf("own mask used graphics mask: %v", err)
				}
				if len(page.Items) != 1 {
					t.Fatalf("items=%d", len(page.Items))
				}
				img := page.Items[0].(creator.Image)
				decoded, _, err := image.Decode(bytes.NewReader(img.Data))
				if err != nil {
					t.Fatal(err)
				}
				_, _, _, a := decoded.At(4, 4).RGBA()
				if a < 32768 || a > 32896 {
					t.Fatalf("own alpha replaced: %d", a)
				}
				// 非缓存/内联入口也必须执行同一选择，而不能只修缓存 Do。
				if err := p.appendImage(stream); err != nil || p.control.check() != nil || len(page.Items) != 2 {
					t.Fatalf("uncached priority error=%v control=%v items=%d", err, p.control.check(), len(page.Items))
				}
			} else if p.control.check() == nil {
				t.Fatal("active graphics mask was ignored")
			}
		})
	}
}

func imageGraphicsPriorityPDF(t *testing.T, own string, trailingContent string) []byte {
	t.Helper()
	content := fmt.Sprintf("0 0 1 rg 0 0 200 200 re f q /GS1 gs 160 0 0 80 20 60 cm /Im0 Do Q %s", trailingContent)
	base := budgetImage(8, 8, "", bytes.Repeat([]byte{255, 0, 0}, 64))
	if own == "embedded" {
		data := jpxAlphaStream(t, 16, 1, color.NRGBA64{R: 65535, A: 32769}).Raw
		base = fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /SMaskInData 1 /Filter /JPXDecode /Length %d >>\nstream\n%s\nendstream", len(data), data)
	} else if own == "external" {
		base = budgetImage(8, 8, "/SMask 6 0 R", bytes.Repeat([]byte{255, 0, 0}, 64))
	}
	groupContent := "q 200 0 0 200 0 0 cm /GM Do Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Im0 5 0 R >> /ExtGState << /GS1 7 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content), base,
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 64 >>\nstream\n%s\nendstream", bytes.Repeat([]byte{128}, 64)),
		"<< /ca .5 /SMask << /S /Luminosity /G 8 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 200] /Group << /S /Transparency /CS /DeviceGray >> /Resources << /XObject << /GM 9 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(groupContent), groupContent),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n%s\nendstream", []byte{0}),
	}
	return assemblePDF(objects)
}

func TestPDFImageSoftMaskPriorityRendersWithGraphicsCaStillApplied(t *testing.T) {
	for _, kind := range []string{"embedded", "external", "no image mask"} {
		var output bytes.Buffer
		if err := ConvertWithOptions(t.Context(), imageGraphicsPriorityPDF(t, kind, ""), &output, "", Options{Strict: true}); err != nil {
			t.Fatal(err)
		}
		ofd, err := parser.NewOFD(output.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ofd.Close() }()
		doc := render.NewDocument(color.White, ofd.Documents[0])
		defer doc.Close()
		img, err := doc.RasterizePage(ofd.Documents[0].Pages[0], render.BackendCanvas, geom.DPMM(4))
		if err != nil {
			t.Fatal(err)
		}
		x, y := 100*pdfPointToMillimeter*4, 100*pdfPointToMillimeter*4
		got := img.RGBAAt(int(x), int(y))
		if kind == "no image mask" {
			if got.R != 0 || got.G != 0 || got.B != 255 {
				t.Fatalf("graphics mask ignored: %v", got)
			}
		} else if got.R < 62 || got.R > 65 || got.G != 0 || got.B < 190 || got.B > 193 {
			t.Fatalf("%s own mask or ca lost: %v", kind, got)
		}
	}
}

func TestPDFGraphicsSoftMaskSelectionDoesNotContaminateCachedImageOrState(t *testing.T) {
	for _, kind := range []string{"embedded", "external", "no image mask"} {
		input := imageGraphicsPriorityPDF(t, kind, "q 160 0 0 80 20 60 cm /Im0 Do Q q /GS1 gs 160 0 0 80 20 60 cm /Im0 Do Q")
		var output bytes.Buffer
		if err := ConvertWithOptions(t.Context(), input, &output, "", Options{Strict: true}); err != nil {
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
		images := layerImages(page.Content().Layer[0])
		if len(images) != 3 {
			t.Fatalf("cached images=%d", len(images))
		}
		for i, object := range images {
			data, err := doc.FileCache.Read(doc.GetMedia(models.StID(object.ResourceID)).MediaFile.String())
			if err != nil {
				t.Fatal(err)
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, alpha := img.At(4, 4).RGBA()
			if kind == "no image mask" {
				want := uint32(0)
				if i == 1 {
					want = 65535
				}
				if alpha != want {
					t.Fatalf("graphics state/cached alpha[%d]=%d want %d", i, alpha, want)
				}
			} else if alpha < 32768 || alpha > 32896 {
				t.Fatalf("%s cached own alpha[%d]=%d", kind, i, alpha)
			}
		}
	}
}
