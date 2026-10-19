package canvas

import (
	"bytes"
	"image/color"
	"math"
	"os/exec"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/pdf"
	"github.com/zc310/ofd/internal/testutil"
	"golang.org/x/image/font/gofont/goregular"
)

func pdfTextTestFamily(t *testing.T) *canvas.FontFamily {
	t.Helper()
	family := canvas.NewFontFamily(t.Name())
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	return family
}

func renderPDFText(t *testing.T, width, height float64, draw func(pdfTextRenderer)) []byte {
	t.Helper()
	var output bytes.Buffer
	renderer := pdfTextRenderer{PDF: pdf.New(&output, width, height, &pdf.Options{SubsetFonts: true}), width: width, height: height}
	draw(renderer)
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

// 回读 PDF 对象，避免用序列化文本匹配来判断是否栅格化及图片大小。
func pdfTextImages(t *testing.T, data []byte) []types.StreamDict {
	t.Helper()
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	ctx, err := api.ReadContext(t.Context(), bytes.NewReader(data), conf)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.ValidateContext(t.Context(), ctx); err != nil {
		t.Fatal(err)
	}
	var images []types.StreamDict
	for _, entry := range ctx.Table {
		if entry == nil {
			continue
		}
		stream, _, err := ctx.DereferenceStreamDict(entry.Object)
		if err != nil || stream == nil {
			continue
		}
		if subtype := stream.NameEntry("Subtype"); subtype != nil && *subtype == "Image" {
			images = append(images, *stream)
		}
	}
	return images
}

func TestPDFTextRendererRasterizesOnlyTranslucentGradients(t *testing.T) {
	family := pdfTextTestFamily(t)
	for _, tc := range []struct {
		name   string
		radial bool
		alpha  uint8
		text   string
	}{
		{name: "solid", alpha: 255, text: "Vector"},
		{name: "opaque linear", alpha: 255, text: "Vector"},
		{name: "opaque radial", radial: true, alpha: 255, text: "Vector"},
		{name: "translucent linear", alpha: 128, text: "Alpha"},
		{name: "translucent radial", radial: true, alpha: 128, text: "Alpha"},
		{name: "mixed spans and inline path", alpha: 128, text: "AlphaSolid"},
		{name: "empty", alpha: 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fill any = canvas.Black
			if tc.name != "solid" {
				stops := canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: color.RGBA{B: tc.alpha, A: tc.alpha}}}
				if tc.radial {
					gradient := canvas.NewRadialGradient(canvas.Point{}, 0, canvas.Point{}, 20)
					gradient.Grad = stops
					fill = gradient
				} else {
					gradient := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20})
					gradient.Grad = stops
					fill = gradient
				}
			}
			text := canvas.NewTextLine(family.Face(24, fill), tc.text, canvas.Left)
			if tc.name == "mixed spans and inline path" {
				rich := canvas.NewRichText(family.Face(24, fill))
				rich.WriteString("Alpha")
				rich.WritePath(canvas.Rectangle(2, 2), canvas.Green, canvas.Baseline)
				rich.WriteFace(family.Face(24, canvas.Black), "Solid")
				text = rich.ToText(40, 0, canvas.Left, canvas.Top, nil)
			}
			matrix := canvas.Identity.Translate(5, 10)
			page := canvas.New(50, 25)
			page.RenderText(text, matrix)
			before := rasterize(page, canvas.DPI(144), canvas.DefaultColorSpace)
			data := renderPDFText(t, 50, 25, func(r pdfTextRenderer) { r.RenderText(text, matrix) })
			images := pdfTextImages(t, data)
			wantRaster := tc.alpha < 255 && tc.text != ""
			if (len(images) > 0) != wantRaster {
				t.Fatalf("image count = %d, want raster fallback = %v", len(images), wantRaster)
			}
			if wantRaster {
				masked := false
				for _, img := range images {
					_, hasMask := img.Find("SMask")
					masked = masked || hasMask
				}
				if !masked {
					t.Error("translucent text image lost its alpha mask")
				}
			}
			if after := rasterize(page, canvas.DPI(144), canvas.DefaultColorSpace); !bytes.Equal(before.Pix, after.Pix) {
				t.Error("PDF rendering modified shared text or gradient")
			}
			t.Run("text extraction", func(t *testing.T) {
				tool, err := exec.LookPath("pdftotext")
				if err != nil {
					t.Skip("缺少 pdftotext")
				}
				cmd := exec.CommandContext(t.Context(), tool, "-", "-")
				cmd.Stdin = bytes.NewReader(data)
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("pdftotext: %v: %s", err, output)
				}
				if got := strings.Join(strings.Fields(string(output)), ""); got != tc.text {
					t.Errorf("extracted text = %q, want %q", got, tc.text)
				}
			})
		})
	}
}

func TestPDFTextRendererRestoresAlphaForFollowingGradient(t *testing.T) {
	family := pdfTextTestFamily(t)
	gradient := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20})
	gradient.Grad = canvas.Grad{{Offset: 0, Color: color.RGBA{R: 128, A: 128}}, {Offset: 1, Color: color.RGBA{B: 128, A: 128}}}
	opaque := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20})
	opaque.Grad = canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}
	data := renderPDFText(t, 60, 20, func(r pdfTextRenderer) {
		r.RenderText(canvas.NewTextLine(family.Face(36, gradient), "Alpha", canvas.Left), canvas.Identity.Translate(2, 5))
		r.RenderText(canvas.NewTextLine(family.Face(36, opaque), "Solid", canvas.Left), canvas.Identity.Translate(32, 5))
	})
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少 pdftoppm，跳过 PDF 像素检查")
	}
	pages, err := testutil.RasterizePDFPages(t.Context(), tool, data, 144)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 {
		t.Fatalf("page count = %d, want 1", len(pages))
	}
	img := pages[0]
	leftGreen, rightGreen := uint8(255), uint8(255)
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			green := img.RGBAAt(x, y).G
			if x < img.Bounds().Dx()/2 {
				leftGreen = min(leftGreen, green)
			} else {
				rightGreen = min(rightGreen, green)
			}
		}
	}
	// 红蓝文字不含绿色，白底剩余绿色反映实际透明度。
	if leftGreen < 125 || leftGreen > 129 {
		t.Errorf("translucent text green = %d, want approximately 127", leftGreen)
	}
	if rightGreen > 3 {
		t.Errorf("following opaque gradient green = %d, want 0", rightGreen)
	}
}

func TestPDFTextRendererClipsAndLimitsRasterFallback(t *testing.T) {
	family := pdfTextTestFamily(t)
	gradient := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20})
	gradient.Grad = canvas.Grad{{Offset: 0, Color: color.RGBA{R: 128, A: 128}}, {Offset: 1, Color: canvas.Blue}}
	text := canvas.NewTextLine(family.Face(24, gradient), "Mask", canvas.Left)
	for _, tc := range []struct {
		name          string
		width, height float64
		matrix        canvas.Matrix
		wantImage     bool
	}{
		{"outside left", 20, 20, canvas.Identity.Translate(-100, 10), false},
		{"outside right", 20, 20, canvas.Identity.Translate(100, 10), false},
		{"outside above", 20, 20, canvas.Identity.Translate(5, 100), false},
		{"outside below", 20, 20, canvas.Identity.Translate(5, -100), false},
		{"partially clipped", 5, 5, canvas.Identity.Translate(-2, -1), true},
		{"pixel budget", 10000, 10000, canvas.Identity.Translate(0, 100).Scale(1000, 1000), true},
		{"width limit", 1000000, 20, canvas.Identity.Translate(0, 5).Scale(10000, 1), true},
		{"height limit", 20, 1000000, canvas.Identity.Translate(0, 5).Scale(1, 10000), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := renderPDFText(t, tc.width, tc.height, func(r pdfTextRenderer) { r.RenderText(text, tc.matrix) })
			images := pdfTextImages(t, data)
			if (len(images) > 0) != tc.wantImage {
				t.Fatalf("image count = %d, want image = %v", len(images), tc.wantImage)
			}
			for _, img := range images {
				width, height := img.IntEntry("Width"), img.IntEntry("Height")
				if width == nil || height == nil {
					t.Fatal("image dimensions missing")
				}
				if *width <= 0 || *height <= 0 || *width > 32768 || *height > 32768 {
					t.Errorf("invalid raster dimensions: %dx%d", *width, *height)
				}
				// 栅格尺寸向上取整，允许最后一行/列的像素误差。
				if (*width-1)*(*height-1) > 4_000_000 {
					t.Errorf("raster exceeds pixel budget: %dx%d", *width, *height)
				}
				if *width > int(math.Ceil(tc.width*300/25.4)) || *height > int(math.Ceil(tc.height*300/25.4)) {
					t.Errorf("raster exceeds page bounds at 300 DPI: %dx%d", *width, *height)
				}
			}
		})
	}
}
