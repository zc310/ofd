package pdf

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

func TestPDFImageTrimRectKeepsVisiblePixelsAndSource(t *testing.T) {
	for _, kind := range []string{"rgba", "nrgba"} {
		t.Run(kind, func(t *testing.T) {
			// 非零原点、父图步长与不对称可见范围，防止裁边时误用紧密排列或对称假设。
			source := pdfTrimSource(kind)
			before := pdfTrimPixels(source)
			want := image.Rect(49, 44, 91, 81)
			if got := pdfImageTrimRect(source); got != want {
				t.Fatalf("裁边范围=%v，期望 %v", got, want)
			}
			if !bytes.Equal(before, pdfTrimPixels(source)) {
				t.Fatal("裁边扫描修改了共享图片")
			}
			// Alpha=1 仍是有效采样，不能按视觉阈值删除。
			switch src := source.(type) {
			case *image.RGBA:
				src.SetRGBA(20, 30, color.RGBA{R: 1, A: 1})
			case *image.NRGBA:
				src.SetNRGBA(20, 30, color.NRGBA{R: 255, A: 1})
			}
			if got := pdfImageTrimRect(source); got != image.Rect(19, 29, 91, 81) {
				t.Fatalf("极低透明度像素被裁去：%v", got)
			}
		})
	}
}

func TestPDFImageTrimRectSkipsOpaqueSmallSavingsAndEncodedImages(t *testing.T) {
	for _, tc := range []struct {
		name string
		ink  image.Rectangle
	}{
		{"全透明", image.Rectangle{}},
		{"全图可见", image.Rect(0, 0, 100, 80)},
		{"只省少量像素", image.Rect(5, 5, 95, 75)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := image.NewNRGBA(image.Rect(0, 0, 100, 80))
			for y := tc.ink.Min.Y; y < tc.ink.Max.Y; y++ {
				for x := tc.ink.Min.X; x < tc.ink.Max.X; x++ {
					img.SetNRGBA(x, y, color.NRGBA{A: 255})
				}
			}
			if got := pdfImageTrimRect(img); got != img.Bounds() {
				t.Fatalf("不应裁边：%v", got)
			}
		})
	}
	empty := image.NewNRGBA(image.Rect(10, 20, 10, 20))
	corner := image.NewNRGBA(image.Rect(0, 0, 100, 80))
	corner.SetNRGBA(0, 0, color.NRGBA{A: 1})
	corner.SetNRGBA(99, 79, color.NRGBA{A: 1})
	// 编码包装没有像素解码函数：此处只能读取尺寸，不能触发解码。
	encoded := &cimage.Image{Mimetype: "image/png"}
	encoded.Width, encoded.Height = 100, 80
	for _, img := range []image.Image{empty, corner, encoded, struct{ image.Image }{pdfTrimSource("nrgba")}} {
		if got := pdfImageTrimRect(img); got != img.Bounds() {
			t.Fatalf("不应裁边的图片 %T 被改变：%v", img, got)
		}
	}
}

func TestPDFTrimmedImageKeepsSamplesPlacementAndOriginalCacheKey(t *testing.T) {
	source := pdfTrimSource("nrgba")
	before := pdfTrimPixels(source)
	crop := pdfImageTrimRect(source)
	var output bytes.Buffer
	writer := newPDFWriter(&output)
	writer.SetCompression(false)
	page := writer.NewPage(100, 100)
	// 先直接内嵌再绘制，二者必须使用同一裁边范围，不能复用错误尺寸的图像。
	first := page.embedImage(source, cimage.Lossless)
	for i, matrix := range []canvas.Matrix{
		canvas.Identity.Translate(10, 20).Scale(.2, .3),
		canvas.Identity.Translate(70, 30).Rotate(30).Shear(.2, -.1).Scale(.2, .3),
		canvas.Identity.Translate(60, 70).Scale(-.2, .3),
	} {
		start := page.Len()
		page.DrawImage(source, cimage.Lossless, matrix)
		checkTrimmedImageMatrix(t, page.Bytes()[start:], matrix, source.Bounds(), crop)
		if writer.images[source] != first || len(writer.images) != 1 {
			t.Fatalf("第%d次绘制没有复用原图键", i+1)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, pdfTrimPixels(source)) {
		t.Fatal("PDF 嵌入修改了原图采样")
	}
	ctx, err := api.ReadAndValidate(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	cropped, _ := source.(interface {
		SubImage(bounds image.Rectangle) image.Image
	})
	wantRGB, wantAlpha, _ := rgbStream(cropped.SubImage(crop), crop.Size())
	count := 0
	for _, entry := range ctx.Table {
		if entry == nil {
			continue
		}
		stream, _, err := ctx.DereferenceStreamDict(entry.Object)
		if err != nil || stream == nil || stream.NameEntry("Subtype") == nil || *stream.NameEntry("Subtype") != "Image" {
			continue
		}
		count++
		if width, height := stream.IntEntry("Width"), stream.IntEntry("Height"); width == nil || height == nil ||
			*width != crop.Dx() || *height != crop.Dy() {
			t.Fatal("PDF 中颜色和蒙版没有采用裁边尺寸")
		}
		if err := ctx.DecodeStream(stream); err != nil {
			t.Fatal(err)
		}
		want := wantRGB
		if *stream.NameEntry("ColorSpace") == "DeviceGray" {
			want = wantAlpha
		}
		if !bytes.Equal(stream.Content, want) {
			t.Fatal("裁边改变了保留区域的颜色或透明度采样")
		}
	}
	if count != 2 {
		t.Fatalf("图片流数量=%d，期望共用一张 RGB 图片及其蒙版", count)
	}
}

func checkTrimmedImageMatrix(t *testing.T, content []byte, original canvas.Matrix, bounds, crop image.Rectangle) {
	t.Helper()
	tokens := strings.Fields(string(content))
	var matrix canvas.Matrix
	for i, token := range tokens {
		if token != "cm" || i < 6 {
			continue
		}
		var values [6]float64
		for j := range values {
			v, err := strconv.ParseFloat(tokens[i-6+j], 64)
			if err != nil {
				t.Fatal(err)
			}
			values[j] = v
		}
		matrix = canvas.Matrix{{values[0], values[2], values[4]}, {values[1], values[3], values[5]}}
	}
	for _, point := range []image.Point{crop.Min, crop.Max.Sub(image.Pt(1, 1))} {
		x, y := float64(point.X)+.5, float64(point.Y)+.5
		want := original.Dot(canvas.Point{X: x - float64(bounds.Min.X), Y: float64(bounds.Max.Y) - y})
		got := matrix.Dot(canvas.Point{X: (x - float64(crop.Min.X)) / float64(crop.Dx()), Y: (float64(crop.Max.Y) - y) / float64(crop.Dy())})
		if math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 {
			t.Fatalf("裁边改变了像素位置：%v，期望 %v", got, want)
		}
	}
}

func TestPDFTrimmedImageMatchesUntrimmedNativeClipAndTransform(t *testing.T) {
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少 pdftoppm")
	}
	for _, kind := range []string{"rgba", "nrgba"} {
		t.Run(kind, func(t *testing.T) {
			source := pdfTrimSource(kind)
			pages := make([]image.Image, 0, 2)
			for _, trim := range []bool{false, true} {
				img := source
				if !trim {
					// 隐藏直接像素访问能力，以原尺寸嵌入作为独立参照。
					img = struct{ image.Image }{source}
				}
				var output bytes.Buffer
				r := New(&output, 100, 80, nil)
				style := canvas.DefaultStyle
				style.Fill = canvas.Paint{Color: color.RGBA{R: 220, G: 235, B: 250, A: 255}}
				r.RenderPath(canvas.Rectangle(100, 80), style, canvas.Identity)
				for _, matrix := range []canvas.Matrix{
					canvas.Identity.Translate(5, 5).Scale(.3, .3),
					canvas.Identity.Translate(55, 5).Shear(.3, .2).Scale(.3, .3),
					canvas.Identity.Translate(40, 50).Rotate(30).Scale(-.3, .3),
				} {
					r.RenderImage(img, matrix)
				}
				r.RenderClippedImage(img, canvas.Identity.Translate(55, 40).Scale(.3, .3), canvas.Circle(20).Translate(60, 50))
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				root := t.TempDir()
				input, prefix := filepath.Join(root, "trim.pdf"), filepath.Join(root, "page")
				if err := os.WriteFile(input, output.Bytes(), 0o600); err != nil {
					t.Fatal(err)
				}
				if log, err := exec.CommandContext(t.Context(), tool, "-r", "144", "-png", "-singlefile", input, prefix).CombinedOutput(); err != nil {
					t.Fatalf("pdftoppm: %v: %s", err, log)
				}
				data, err := os.ReadFile(prefix + ".png")
				if err != nil {
					t.Fatal(err)
				}
				page, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				pages = append(pages, page)
			}
			edgeDifferences := 0
			for y := range pages[0].Bounds().Dy() {
				for x := range pages[0].Bounds().Dx() {
					first, _ := color.RGBAModel.Convert(pages[0].At(x, y)).(color.RGBA)
					second, _ := color.RGBAModel.Convert(pages[1].At(x, y)).(color.RGBA)
					for i, a := range []byte{first.R, first.G, first.B} {
						b := []byte{second.R, second.G, second.B}[i]
						if delta := int(a) - int(b); delta < -2 || delta > 2 {
							// Poppler 对旋转图像按设备包围盒重采样，裁边会改变边缘插值相位。
							// 几何位置和全部采样已独立精确验证；这里不允许改变平坦区域。
							if !pdfTrimPixelNearEdge(pages[0], x, y) {
								t.Fatalf("裁边改变了平坦区域：(%d,%d) %v != %v", x, y, first, second)
							}
							edgeDifferences++
							break
						}
					}
				}
			}
			if edgeDifferences > pages[0].Bounds().Dx()*pages[0].Bounds().Dy()/100 {
				t.Fatalf("裁边差异范围过大：%d 个边缘像素", edgeDifferences)
			}
		})
	}
}

func pdfTrimPixelNearEdge(img image.Image, x, y int) bool {
	center, _ := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			point := image.Pt(x+dx, y+dy)
			if !point.In(img.Bounds()) {
				continue
			}
			value, _ := color.RGBAModel.Convert(img.At(point.X, point.Y)).(color.RGBA)
			for i, a := range []byte{center.R, center.G, center.B} {
				b := []byte{value.R, value.G, value.B}[i]
				if d := int(a) - int(b); d < -2 || d > 2 {
					return true
				}
			}
		}
	}
	return false
}

func pdfTrimSource(kind string) image.Image {
	parent := image.Rect(-5, 10, 150, 130)
	bounds := image.Rect(10, 20, 138, 116)
	ink := image.Rect(50, 45, 90, 80)
	var result image.Image
	if kind == "rgba" {
		source := image.NewRGBA(parent)
		for y := ink.Min.Y; y < ink.Max.Y; y++ {
			for x := ink.Min.X; x < ink.Max.X; x++ {
				source.SetRGBA(x, y, color.RGBA{R: 100, G: 40, B: 10, A: 128})
			}
		}
		result = source.SubImage(bounds)
	} else {
		source := image.NewNRGBA(parent)
		for y := ink.Min.Y; y < ink.Max.Y; y++ {
			for x := ink.Min.X; x < ink.Max.X; x++ {
				source.SetNRGBA(x, y, color.NRGBA{R: 200, G: 80, B: 20, A: 128})
			}
		}
		result = source.SubImage(bounds)
	}
	return result
}

func pdfTrimPixels(img image.Image) []byte {
	switch source := img.(type) {
	case *image.NRGBA:
		return bytes.Clone(source.Pix)
	case *image.RGBA:
		return bytes.Clone(source.Pix)
	default:
		return nil
	}
}

func BenchmarkPDFEmbedSparseRasterImage(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, 1245, 946))
	for y := 20; y < 929; y++ {
		img.SetNRGBA(1233, y, color.NRGBA{R: 128, G: 64, A: 12})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		var output bytes.Buffer
		writer := newPDFWriter(&output)
		writer.NewPage(320, 240).DrawImage(img, cimage.Lossless, canvas.Identity.Scale(.25, .25))
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(output.Len()), "PDF-bytes/op")
	}
}
