package render

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"path/filepath"
	"testing"

	"github.com/tdewolff/canvas"
	cimage "github.com/tdewolff/canvas/image"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

type recordingRenderer struct {
	width, height float64
	paths         int
	texts         int
	images        int
}

func (r *recordingRenderer) Size() (float64, float64) {
	return r.width, r.height
}

func (r *recordingRenderer) RenderPath(_ *canvas.Path, _ canvas.Style, _ canvas.Matrix) { r.paths++ }
func (r *recordingRenderer) RenderText(_ *canvas.Text, _ canvas.Matrix)                 { r.texts++ }
func (r *recordingRenderer) RenderImage(_ image.Image, _ canvas.Matrix)                 { r.images++ }

func TestApplyImageMaskFastPathsMatchReference(t *testing.T) {
	mask := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for i := 0; i < len(mask.Pix); i++ {
		mask.Pix[i] = uint8(i*37 + 11)
	}
	cases := map[string]image.Image{
		"nrgba": func() image.Image {
			img := image.NewNRGBA(image.Rect(0, 0, 8, 6))
			for i := 0; i < len(img.Pix); i++ {
				img.Pix[i] = uint8(i*29 + 5)
			}
			return img
		}(),
		"rgba": func() image.Image {
			img := image.NewRGBA(image.Rect(0, 0, 8, 6))
			for i := 0; i < len(img.Pix); i++ {
				img.Pix[i] = uint8(i*23 + 3)
			}
			return img
		}(),
		"ycbcr444": func() image.Image {
			img := image.NewYCbCr(image.Rect(0, 0, 8, 6), image.YCbCrSubsampleRatio444)
			for i := 0; i < len(img.Y); i++ {
				img.Y[i] = uint8(i*17 + 7)
			}
			for i := 0; i < len(img.Cb); i++ {
				img.Cb[i] = uint8(i*13 + 1)
			}
			for i := 0; i < len(img.Cr); i++ {
				img.Cr[i] = uint8(i*11 + 9)
			}
			return img
		}(),
		"ycbcr420": func() image.Image {
			img := image.NewYCbCr(image.Rect(0, 0, 8, 6), image.YCbCrSubsampleRatio420)
			for i := 0; i < len(img.Y); i++ {
				img.Y[i] = uint8(i*17 + 7)
			}
			for i := 0; i < len(img.Cb); i++ {
				img.Cb[i] = uint8(i*13 + 1)
			}
			for i := 0; i < len(img.Cr); i++ {
				img.Cr[i] = uint8(i*11 + 9)
			}
			return img
		}(),
	}
	for name, img := range cases {
		t.Run(name, func(t *testing.T) {
			want := applyImageMaskReference(img, mask)
			got := applyImageMask(img, mask)
			if !bytes.Equal(want.(*image.NRGBA).Pix, got.(*image.NRGBA).Pix) {
				t.Fatal("fast path output differs from reference")
			}
		})
	}
}

func applyImageMaskReference(img image.Image, mask *image.RGBA) image.Image {
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			ma := mask.RGBAAt(x-bounds.Min.X, y-bounds.Min.Y).A
			c.A = uint8(int(c.A) * int(ma) / 255)
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

func TestClipCoversImageSkipsFullRectangleClip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	clip := canvas.Rectangle(100, 50)
	out := imageWithClip(img, clip, canvas.Identity)
	if out != image.Image(img) {
		t.Fatal("expected a clip covering the full image to be skipped")
	}
}

func TestClipCoversImageSkipsScaledFullRectangleClip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	clip := canvas.Rectangle(100, 50)
	out := imageWithClip(img, clip, canvas.Matrix{{0.5, 0, 0}, {0, 0.5, 0}})
	if out != image.Image(img) {
		t.Fatal("expected a scaled clip covering the full image to be skipped")
	}
}

func TestClipCoversImageAppliesPartialRectangleClip(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 100, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 100; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	clip := canvas.Rectangle(50, 50)
	out := imageWithClip(img, clip, canvas.Identity)
	if out == image.Image(img) {
		t.Fatal("expected a partial clip to produce a masked image")
	}
	var fullyOpaque int
	for y := 0; y < 50; y++ {
		for x := 0; x < 100; x++ {
			if _, _, _, a := out.At(x, y).RGBA(); a == 0xffff {
				fullyOpaque++
			}
		}
	}
	if fullyOpaque != 50*50 {
		t.Fatalf("opaque pixel count = %d, want %d", fullyOpaque, 50*50)
	}
}

func TestDecodeRasterImageKeepsRawJPEGBytes(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRasterImage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	img, ok := decoded.(*cimage.Image)
	if !ok {
		t.Fatalf("expected lazy cimage.Image for JPEG, got %T", decoded)
	}
	if img.Mimetype != "image/jpeg" {
		t.Fatalf("unexpected mimetype %q", img.Mimetype)
	}
}

func TestDecodeRasterImageKeepsRawPNGBytes(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRasterImage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	img, ok := decoded.(*cimage.Image)
	if !ok {
		t.Fatalf("expected lazy cimage.Image for PNG, got %T", decoded)
	}
	if img.Mimetype != "image/png" {
		t.Fatalf("unexpected mimetype %q", img.Mimetype)
	}
}

func TestDocumentDecodeImageCacheReusesInstance(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ofd.Close(); err != nil {
			t.Errorf("关闭 OFD 失败: %v", err)
		}
	}()

	doc := NewDocument(color.Transparent, ofd.Documents[0])
	for _, media := range doc.Res {
		img1, err := doc.decodeImage(media.MediaFile.Clean(), media.Format)
		if err != nil {
			continue
		}
		img2, err := doc.decodeImage(media.MediaFile.Clean(), media.Format)
		if err != nil {
			t.Fatal(err)
		}
		if img1 != img2 {
			t.Fatalf("expected cached decode for %s, got different instances", media.MediaFile)
		}
		t.Logf("cached image %s: %dx%d %T", media.MediaFile, img1.Bounds().Dx(), img1.Bounds().Dy(), img1)
	}
}

func TestSVGImageRendersAsVector(t *testing.T) {
	svgData := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="red"/></svg>`)
	data, err := creator.Marshal(creator.Document{
		ID:    "svg-vector-test",
		Media: []creator.Media{{ID: 90, Type: "Image", Format: "SVG", Data: svgData}},
		Pages: []creator.Page{{Items: []creator.Item{creator.Image{
			X: 10, Y: 10, Width: 50, Height: 50, ResourceID: 90,
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ofd.Close(); err != nil {
			t.Errorf("关闭 OFD 失败: %v", err)
		}
	}()
	if len(ofd.Documents[0].Pages) == 0 {
		t.Fatal("expected at least one page")
	}
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	pb := page.Area.PhysicalBox
	rec := &recordingRenderer{width: pb.Width, height: pb.Height}
	doc := NewDocument(color.Transparent, ofd.Documents[0])
	if err := doc.Draw(canvas.NewContext(rec), page); err != nil {
		t.Fatal(err)
	}
	if rec.images != 0 {
		t.Fatalf("expected SVG vector embedding (0 raster images), got %d raster images", rec.images)
	}
	if rec.paths == 0 {
		t.Fatal("expected SVG vector content to produce at least one path record")
	}
}

func TestImageMatrixWHVectorEmbedMatchesRasterPlacement(t *testing.T) {
	box := models.StBox{X: 20, Y: 30, Width: 100, Height: 80}
	ctm := models.CTM{2, 0.1, -0.05, 1.5, 5, -3}
	pageHeight := 297.0
	svgW := 100.0
	svgH := 75.0
	dpmm := canvas.DPI(96).DPMM()
	imgW := svgW * dpmm
	imgH := svgH * dpmm
	flip := canvas.Matrix{{1, 0, 0}, {0, -1, svgH}}

	mRaster := imageMatrixWH(box, imgW, imgH, ctm, pageHeight)
	mVector := imageMatrixWH(box, svgW, svgH, ctm, pageHeight).Mul(flip)

	corners := [][2]float64{
		{0, 0},
		{svgW, 0},
		{0, svgH},
		{svgW, svgH},
	}
	for _, corner := range corners {
		canvasX, canvasY := corner[0], corner[1]
		pixelX := canvasX * dpmm
		pixelY := imgH - canvasY*dpmm

		pageFromRasterX := mRaster[0][0]*pixelX + mRaster[0][1]*pixelY + mRaster[0][2]
		pageFromRasterY := mRaster[1][0]*pixelX + mRaster[1][1]*pixelY + mRaster[1][2]

		pageFromVectorX := mVector[0][0]*canvasX + mVector[0][1]*canvasY + mVector[0][2]
		pageFromVectorY := mVector[1][0]*canvasX + mVector[1][1]*canvasY + mVector[1][2]

		if math.Abs(pageFromRasterX-pageFromVectorX) > 1e-9 || math.Abs(pageFromRasterY-pageFromVectorY) > 1e-9 {
			t.Fatalf("canvas (%.2f,%.2f): raster=(%.6f,%.6f) vector=(%.6f,%.6f) differ",
				canvasX, canvasY, pageFromRasterX, pageFromRasterY, pageFromVectorX, pageFromVectorY)
		}
	}
}
