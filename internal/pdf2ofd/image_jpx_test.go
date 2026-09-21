package pdf2ofd

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
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
	if err := Convert(pdf, &output); err != nil {
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
	images := page.Content().Layer[0].ImageObject
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
