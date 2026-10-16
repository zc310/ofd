package pdf2ofd

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/pdf2ofd/config"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

// encodeJPX 用纯 Go 编码器生成一张无损 JPEG 2000，作为转换回归测试的输入。
func encodeJPX(t *testing.T, src image.Image, format jpeg2000.Format) []byte {
	t.Helper()
	options := jpeg2000.DefaultOptions()
	options.Lossless = true
	options.Format = format
	var buffer bytes.Buffer
	if err := jpeg2000.Encode(&buffer, src, options); err != nil {
		t.Fatalf("encode JPX: %v", err)
	}
	return buffer.Bytes()
}

func jpxTestSource() image.Image {
	src := image.NewGray(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			value := uint8(0)
			if x >= 2 {
				value = 255
			}
			src.SetGray(x, y, color.Gray{Y: value})
		}
	}
	return src
}

// convertJPXImage 把 JPX 码流包成 PDF 图像并转换，返回解码后的 PNG。
func convertJPXImage(t *testing.T, jp2 []byte) image.Image {
	return convertJPXImageWithOptions(t, jp2, config.Options{})
}

func convertJPXImageWithOptions(t *testing.T, jp2 []byte, options config.Options) image.Image {
	t.Helper()
	content := []byte("q 100 0 0 100 0 0 cm /Im1 Do Q")
	imageObject := "<< /Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray" +
		" /BitsPerComponent 8 /Filter /JPXDecode /Length " + itoa(len(jp2)) +
		" >>\nstream\n" + string(jp2) + "\nendstream"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		imageObject,
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := ConvertWithOptions(t.Context(), pdf, &output, "", options); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	images := layerImages(page.Content().Layer[0])
	if len(images) != 1 {
		t.Fatalf("image objects = %d, want 1", len(images))
	}
	media := ofd.Documents[0].GetMedia(models.StID(images[0].ResourceID))
	if media == nil || media.Format != "PNG" {
		t.Fatalf("JPX media = %v, want PNG", media)
	}
	data, err := ofd.Documents[0].FileCache.Read(media.MediaFile.String())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func assertJPXGrayscale(t *testing.T, img image.Image) {
	t.Helper()
	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 4 {
		t.Fatalf("decoded JPX size = %dx%d, want 4x4", img.Bounds().Dx(), img.Bounds().Dy())
	}
	if left := color.GrayModel.Convert(img.At(0, 0)).(color.Gray).Y; left > 30 {
		t.Fatalf("left pixel = %d, want black", left)
	}
	if right := color.GrayModel.Convert(img.At(3, 0)).(color.Gray).Y; right < 225 {
		t.Fatalf("right pixel = %d, want white", right)
	}
}

func TestConvertJPXJP2Image(t *testing.T) {
	assertJPXGrayscale(t, convertJPXImage(t, encodeJPX(t, jpxTestSource(), jpeg2000.FormatJP2)))
}

func TestConvertJPXRawCodestreamImage(t *testing.T) {
	// PDF /JPXDecode 也常见裸 JPEG 2000 码流（SOC 开头，无 JP2 盒子）。
	assertJPXGrayscale(t, convertJPXImage(t, encodeJPX(t, jpxTestSource(), jpeg2000.FormatJ2K)))
}

func TestPDFJPXImagesWithICCBasedColorSpaceDecode(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "pdf", "2025-Microsoft-Environmental-Sustainability-Report-PDF.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("PDF fixture unavailable: %v", err)
		}
		t.Fatal(err)
	}
	configuration, err := api.LoadConfiguration(api.ConfigurationOptions{Mode: api.ConfigurationModeStateless})
	if err != nil {
		t.Fatal(err)
	}
	configuration.ValidationMode = model.ValidationRelaxed
	ctx, err, panicked := readPDFContext(t.Context(), data, configuration)
	if panicked || err != nil {
		t.Fatalf("read PDF context: panicked=%v err=%v", panicked, err)
	}
	if err := api.ValidateContext(t.Context(), ctx); err != nil {
		t.Fatalf("validate PDF context: %v", err)
	}
	document := &creator.Document{}
	page, _, err := convertPDFPage(t.Context(), ctx, 2, document, map[types.Object]pdfFontInfo{}, map[types.IndirectRef]pdfImageDataCache{}, pdfPageGeometries(t.Context(), ctx), new(uint64))
	if err != nil {
		t.Fatalf("convert PDF page 2: %v", err)
	}
	images := 0
	for _, item := range page.Items {
		if image, ok := item.(creator.Image); ok {
			images++
			decoded, err := png.Decode(bytes.NewReader(image.Data))
			if err != nil || decoded.Bounds().Empty() {
				t.Fatalf("converted page image %d is invalid: bounds=%v err=%v", images, decoded.Bounds(), err)
			}
			if images == 1 {
				if edges := highContrastEdges(decoded, 48); edges < 1000 {
					t.Fatalf("first ICCBased image has only %d high-contrast edges; likely lost image detail", edges)
				}
			}
		}
	}
	if images != 2 {
		t.Fatalf("page 2 image objects=%d, want both ICCBased images", images)
	}
	document.Pages = []creator.Page{page}
	document.ID = "pdf-jpx-icc"
	document.PageSize = creator.PageSize{Width: page.Area.PhysicalBox.Width, Height: page.Area.PhysicalBox.Height}
	var ofdBytes bytes.Buffer
	if err := creator.CreateWithOptions(*document, &ofdBytes, creator.CreateOptions{PreserveEmbeddedFonts: true}); err != nil {
		t.Fatalf("write converted page: %v", err)
	}
	ofd, err := parser.NewOFD(ofdBytes.Bytes())
	if err != nil {
		t.Fatalf("parse converted page: %v", err)
	}
	defer ofd.Close()
	renderedDocument := render.NewDocument(color.White, ofd.Documents[0])
	defer renderedDocument.Close()
	raster, err := renderedDocument.RasterizePage(ofd.Documents[0].Pages[0], render.BackendCanvas, geom.DPMM(4))
	if err != nil {
		t.Fatalf("rasterize converted page: %v", err)
	}
	for _, item := range page.Items {
		imageObject, ok := item.(creator.Image)
		if !ok {
			continue
		}
		minX, minY := int(imageObject.X*4), int(imageObject.Y*4)
		maxX, maxY := int((imageObject.X+imageObject.Width)*4), int((imageObject.Y+imageObject.Height)*4)
		visible := false
		for y := max(0, minY); y < min(raster.Bounds().Max.Y, maxY) && !visible; y++ {
			for x := max(0, minX); x < min(raster.Bounds().Max.X, maxX); x++ {
				pixel := raster.RGBAAt(x, y)
				if pixel.R < 248 || pixel.G < 248 || pixel.B < 248 {
					visible = true
					break
				}
			}
		}
		if !visible {
			t.Fatalf("converted image at (%g,%g) size %gx%g is not visible in the raster", imageObject.X, imageObject.Y, imageObject.Width, imageObject.Height)
		}
	}
}

func highContrastEdges(img image.Image, threshold uint8) int {
	gray := image.NewGray(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			gray.Set(x, y, color.GrayModel.Convert(img.At(x, y)))
		}
	}
	edges := 0
	for y := gray.Bounds().Min.Y; y < gray.Bounds().Max.Y; y++ {
		for x := gray.Bounds().Min.X; x < gray.Bounds().Max.X; x++ {
			value := gray.GrayAt(x, y).Y
			if x+1 < gray.Bounds().Max.X && absByteDifference(value, gray.GrayAt(x+1, y).Y) >= int(threshold) {
				edges++
			}
			if y+1 < gray.Bounds().Max.Y && absByteDifference(value, gray.GrayAt(x, y+1).Y) >= int(threshold) {
				edges++
			}
		}
	}
	return edges
}
