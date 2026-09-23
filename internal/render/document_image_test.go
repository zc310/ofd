package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/utils"
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

func (r *recordingRenderer) RenderPath(_ *canvas.Path, _ canvas.Style, _ canvas.Matrix) {
	r.paths++
}
func (r *recordingRenderer) RenderText(_ *canvas.Text, _ canvas.Matrix) {
	r.texts++
}
func (r *recordingRenderer) RenderImage(_ image.Image, _ canvas.Matrix) {
	r.images++
}

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
	clip := geom.Rectangle(100, 50)
	out := imageWithClip(img, clip, geom.Identity)
	if out != image.Image(img) {
		t.Fatal("expected a clip covering the full image to be skipped")
	}
}

func TestClipCoversImageSkipsScaledFullRectangleClip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	clip := geom.Rectangle(100, 50)
	out := imageWithClip(img, clip, geom.Matrix{{0.5, 0, 0}, {0, 0.5, 0}})
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
	clip := geom.Rectangle(50, 50)
	out := imageWithClip(img, clip, geom.Identity)
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
	img, ok := decoded.(*EncodedImage)
	if !ok {
		t.Fatalf("expected lazy EncodedImage for JPEG, got %T", decoded)
	}
	if img.Format != "jpeg" || !bytes.Equal(img.Data, buf.Bytes()) {
		t.Fatalf("expected raw JPEG bytes preserved, format=%q", img.Format)
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
	img, ok := decoded.(*EncodedImage)
	if !ok {
		t.Fatalf("expected lazy EncodedImage for PNG, got %T", decoded)
	}
	if img.Format != "png" || !bytes.Equal(img.Data, buf.Bytes()) {
		t.Fatalf("expected raw PNG bytes preserved, format=%q", img.Format)
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
	ofd.Documents[0].ForEachMedia(func(_ models.StID, media *models.MultiMedia) bool {
		img1, err := doc.decodeImage(media.MediaFile.Clean(), media.Format)
		if err != nil {
			return true
		}
		img2, err := doc.decodeImage(media.MediaFile.Clean(), media.Format)
		if err != nil {
			t.Fatal(err)
		}
		if img1 != img2 {
			t.Fatalf("expected cached decode for %s, got different instances", media.MediaFile)
		}
		t.Logf("cached image %s: %dx%d %T", media.MediaFile, img1.Bounds().Dx(), img1.Bounds().Dy(), img1)
		return true
	})
}

func TestDocumentDecodeImageConcurrentByKey(t *testing.T) {
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
	var file models.StLoc
	var format string
	ofd.Documents[0].ForEachMedia(func(_ models.StID, media *models.MultiMedia) bool {
		file = media.MediaFile.Clean()
		format = media.Format
		return false
	})
	if file == "" {
		t.Skip("测试文档没有图片资源")
	}

	const workers = 16
	images := make(chan image.Image, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decoded, decodeErr := doc.decodeImage(file, format)
			if decodeErr != nil {
				errs <- decodeErr
				return
			}
			images <- decoded
		}()
	}
	wg.Wait()
	close(images)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var first image.Image
	for decoded := range images {
		if first == nil {
			first = decoded
			continue
		}
		if decoded != first {
			t.Fatal("同一图片 key 的并发解码创建了多个图片实例")
		}
	}
}

type benchmarkImageMedia struct {
	file   models.StLoc
	format string
}

func benchmarkImageDocument(b *testing.B) (*Document, []benchmarkImageMedia) {
	b.Helper()
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := ofd.Close(); err != nil {
			b.Errorf("关闭 OFD 失败: %v", err)
		}
	})

	medias := make([]benchmarkImageMedia, 0)
	ofd.Documents[0].ForEachMedia(func(_ models.StID, media *models.MultiMedia) bool {
		medias = append(medias, benchmarkImageMedia{file: media.MediaFile.Clean(), format: media.Format})
		return true
	})
	if len(medias) == 0 {
		b.Fatal("测试文档没有图片资源")
	}
	return NewDocument(color.Transparent, ofd.Documents[0]), medias
}

func BenchmarkDecodeRasterImage(b *testing.B) {
	doc, medias := benchmarkImageDocument(b)
	media := medias[0]
	data, err := doc.Document.FileCache.Read(media.file.String())
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := decodeRasterImage(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDocumentDecodeImageCached(b *testing.B) {
	doc, medias := benchmarkImageDocument(b)
	media := medias[0]
	if _, err := doc.decodeImage(media.file, media.format); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := doc.decodeImage(media.file, media.format); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDocumentDecodeImageCold(b *testing.B) {
	doc, medias := benchmarkImageDocument(b)
	media := medias[0]
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		b.StopTimer()
		doc.images = utils.NewLRU[string, image.Image](imageCacheCapacity, nil)
		b.StartTimer()
		if _, err := doc.decodeImage(media.file, media.format); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDocumentDecodeImageSameKeyParallel(b *testing.B) {
	doc, medias := benchmarkImageDocument(b)
	media := medias[0]
	if _, err := doc.decodeImage(media.file, media.format); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := doc.decodeImage(media.file, media.format); err != nil {
				b.Errorf("图片解码失败: %v", err)
			}
		}
	})
}

func BenchmarkDocumentDecodeImageDifferentKeysParallel(b *testing.B) {
	doc, medias := benchmarkImageDocument(b)
	for _, media := range medias {
		if _, err := doc.decodeImage(media.file, media.format); err != nil {
			b.Fatal(err)
		}
	}
	var next atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			media := medias[(next.Add(1)-1)%uint64(len(medias))]
			if _, err := doc.decodeImage(media.file, media.format); err != nil {
				b.Errorf("图片解码失败: %v", err)
			}
		}
	})
}

func BenchmarkDocumentDecodeImageColdSameKeyParallel(b *testing.B) {
	doc, medias := benchmarkImageDocument(b)
	benchmarkColdImageDecodeParallel(b, doc, medias, true)
}

func BenchmarkDocumentDecodeImageColdDifferentKeysParallel(b *testing.B) {
	doc, medias := benchmarkImageDocument(b)
	benchmarkColdImageDecodeParallel(b, doc, medias, false)
}

func benchmarkColdImageDecodeParallel(b *testing.B, doc *Document, medias []benchmarkImageMedia, sameKey bool) {
	b.Helper()
	workers := len(medias)
	if sameKey {
		workers = 8
	}
	if workers == 0 {
		b.Fatal("测试文档没有图片资源")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for round := 0; round < b.N; round++ {
		b.StopTimer()
		doc.images = utils.NewLRU[string, image.Image](imageCacheCapacity, nil)
		start := make(chan struct{})
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for index := 0; index < workers; index++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				<-start
				media := medias[index%len(medias)]
				if sameKey {
					media = medias[0]
				}
				if _, err := doc.decodeImage(media.file, media.format); err != nil {
					errs <- err
				}
			}(index)
		}
		b.StartTimer()
		close(start)
		wg.Wait()
		b.StopTimer()
		close(errs)
		for err := range errs {
			b.Fatal(err)
		}
	}
}

type benchmarkSVGMedia struct {
	file   models.StLoc
	format string
}

func benchmarkSVGDocument(b *testing.B) (*Document, []benchmarkSVGMedia) {
	b.Helper()
	media := make([]creator.Media, 4)
	for index, fill := range []string{"red", "green", "blue", "gold"} {
		media[index] = creator.Media{
			ID:     uint64(90 + index),
			Type:   "Image",
			Format: "SVG",
			Data:   []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="320" height="240"><rect width="320" height="240" fill="%s"/><circle cx="160" cy="120" r="80" fill="white"/></svg>`, fill)),
		}
	}
	data, err := creator.Marshal(creator.Document{
		ID:    "svg-benchmark",
		Media: media,
		Pages: []creator.Page{{Items: []creator.Item{creator.Image{X: 0, Y: 0, Width: 320, Height: 240, ResourceID: 90}}}},
	})
	if err != nil {
		b.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := ofd.Close(); err != nil {
			b.Errorf("关闭 OFD 失败: %v", err)
		}
	})

	medias := make([]benchmarkSVGMedia, 0, len(media))
	ofd.Documents[0].ForEachMedia(func(_ models.StID, media *models.MultiMedia) bool {
		medias = append(medias, benchmarkSVGMedia{file: media.MediaFile.Clean(), format: media.Format})
		return true
	})
	if len(medias) == 0 {
		b.Fatal("SVG 基准文档没有图片资源")
	}
	return NewDocument(color.Transparent, ofd.Documents[0]), medias
}

func BenchmarkDocumentDecodeSVGCanvasCold(b *testing.B) {
	doc, medias := benchmarkSVGDocument(b)
	media := medias[0]
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		b.StopTimer()
		doc.svgCanvases = utils.NewLRU[string, SVGScene](svgCacheCapacity, nil)
		b.StartTimer()
		if _, err := doc.decodeSVGScene(media.file, media.format); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDocumentDecodeSVGCanvasCached(b *testing.B) {
	doc, medias := benchmarkSVGDocument(b)
	media := medias[0]
	if _, err := doc.decodeSVGScene(media.file, media.format); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := doc.decodeSVGScene(media.file, media.format); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDocumentDecodeSVGCanvasSameKeyParallel(b *testing.B) {
	doc, medias := benchmarkSVGDocument(b)
	media := medias[0]
	if _, err := doc.decodeSVGScene(media.file, media.format); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := doc.decodeSVGScene(media.file, media.format); err != nil {
				b.Errorf("SVG 解码失败: %v", err)
			}
		}
	})
}

func BenchmarkDocumentDecodeSVGCanvasDifferentKeysParallel(b *testing.B) {
	doc, medias := benchmarkSVGDocument(b)
	for _, media := range medias {
		if _, err := doc.decodeSVGScene(media.file, media.format); err != nil {
			b.Fatal(err)
		}
	}
	var next atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			media := medias[(next.Add(1)-1)%uint64(len(medias))]
			if _, err := doc.decodeSVGScene(media.file, media.format); err != nil {
				b.Errorf("SVG 解码失败: %v", err)
			}
		}
	})
}

func BenchmarkDocumentDecodeSVGCanvasColdSameKeyParallel(b *testing.B) {
	doc, medias := benchmarkSVGDocument(b)
	benchmarkColdSVGCanvasDecodeParallel(b, doc, medias, true)
}

func BenchmarkDocumentDecodeSVGCanvasColdDifferentKeysParallel(b *testing.B) {
	doc, medias := benchmarkSVGDocument(b)
	benchmarkColdSVGCanvasDecodeParallel(b, doc, medias, false)
}

func benchmarkColdSVGCanvasDecodeParallel(b *testing.B, doc *Document, medias []benchmarkSVGMedia, sameKey bool) {
	b.Helper()
	workers := len(medias)
	if sameKey {
		workers = 8
	}
	b.ReportAllocs()
	b.ResetTimer()
	for round := 0; round < b.N; round++ {
		b.StopTimer()
		doc.svgCanvases = utils.NewLRU[string, SVGScene](svgCacheCapacity, nil)
		start := make(chan struct{})
		errs := make(chan error, workers)
		var wg sync.WaitGroup
		for index := 0; index < workers; index++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				<-start
				media := medias[index%len(medias)]
				if sameKey {
					media = medias[0]
				}
				if _, err := doc.decodeSVGScene(media.file, media.format); err != nil {
					errs <- err
				}
			}(index)
		}
		b.StartTimer()
		close(start)
		wg.Wait()
		b.StopTimer()
		close(errs)
		for err := range errs {
			b.Fatal(err)
		}
	}
}

func TestImageNegativeYCTMRendersFlipped(t *testing.T) {
	// 图片对象带负 Y 缩放 CTM 时必须垂直镜像，用于还原 PDF 扫描件的朝向。
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{R: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})
	img.Set(1, 1, color.RGBA{B: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}

	ctm := creator.CTM{20, 0, 0, -20, 0, 20}
	document := creator.Document{
		ID:       "flip",
		PageSize: creator.PageSize{Width: 20, Height: 20},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Image{X: 0, Y: 0, Width: 20, Height: 20, Data: encoded.Bytes(), Format: "PNG", CTM: &ctm},
		}}},
	}
	var ofdBytes bytes.Buffer
	if err := creator.Create(document, &ofdBytes); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(ofdBytes.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := NewDocumentWithDPI(canvas.Transparent, ofd.Documents[0], geom.DPI(96))
	raster, err := doc.RasterizePage(doc.Pages[0], BackendCanvas, geom.DPI(96))
	if err != nil {
		t.Fatal(err)
	}
	bounds := raster.Bounds()
	topR, _, topB, _ := raster.At(bounds.Dx()/2, 1).RGBA()
	bottomR, _, bottomB, _ := raster.At(bounds.Dx()/2, bounds.Dy()-2).RGBA()
	if topB <= topR {
		t.Fatalf("top pixel r=%d b=%d, want blue (flipped)", topR>>8, topB>>8)
	}
	if bottomR <= bottomB {
		t.Fatalf("bottom pixel r=%d b=%d, want red", bottomR>>8, bottomB>>8)
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
	pb, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	rec := &recordingRenderer{width: pb.Width, height: pb.Height}
	doc := NewDocument(color.Transparent, ofd.Documents[0])
	if err := doc.Draw(newCanvasBackend(canvas.NewContext(rec)), page); err != nil {
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
	dpmm := geom.DPI(96).DPMM()
	imgW := svgW * dpmm
	imgH := svgH * dpmm
	flip := geom.Matrix{{1, 0, 0}, {0, -1, svgH}}

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

func TestImageCacheWeightCountsLazyImageBytesAndPixels(t *testing.T) {
	var encoded bytes.Buffer
	source := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 8), B: uint8((x + y) * 3), A: 255})
		}
	}
	if err := jpeg.Encode(&encoded, source, nil); err != nil {
		t.Fatal(err)
	}
	lazy := newEncodedImage("jpeg", encoded.Bytes())
	weight := imageCacheWeight(lazy)
	// 懒加载图片在缓存中同时保留压缩字节和按需解码后的像素，
	// 权重必须大于压缩字节数，否则大图文档的驻留内存无法被限制。
	if weight <= int64(len(encoded.Bytes())) {
		t.Fatalf("imageCacheWeight = %d, 期望大于压缩字节 %d", weight, len(encoded.Bytes()))
	}
}

func TestImageCacheEvictsByByteWeight(t *testing.T) {
	const maxWeight = int64(1 << 20)
	doc := &Document{images: utils.NewWeightedLRU[string, image.Image](imageCacheCapacity, maxWeight, nil)}
	for index := 0; index < 4; index++ {
		// 每张 512x512 RGBA 约 1MiB，超过缓存上限后必须按字节淘汰。
		img := image.NewRGBA(image.Rect(0, 0, 512, 512))
		doc.images.AddWeighted(fmt.Sprintf("img-%d", index), img, imageCacheWeight(img))
	}
	if weight := doc.images.Weight(); weight > maxWeight {
		t.Fatalf("图片缓存权重 = %d, 期望 <= %d", weight, maxWeight)
	}
	if length := doc.images.Len(); length > 1 {
		t.Fatalf("图片缓存条目 = %d, 期望 <= 1", length)
	}
}

func TestSVGCanvasWeightUsesSourceBytes(t *testing.T) {
	if got := svgCanvasWeight(nil); got != 1 {
		t.Fatalf("svgCanvasWeight(nil) = %d, 期望 1", got)
	}
	data := []byte("<svg/>")
	if got := svgCanvasWeight(data); got != int64(len(data)) {
		t.Fatalf("svgCanvasWeight = %d, 期望 %d", got, len(data))
	}
}
