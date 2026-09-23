package pdf2ofd

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// jbig2SampleMask 是 gobig2 测试夹具 testdata/pdf-embedded/sample.jb2 的原始字节：
// PDF 内嵌形态（无文件头），解码为 3562x851 的全黑位图。
const jbig2SampleMask = "\x00\x00\x00\x00\x30\x00\x01\x00\x00\x00\x13\x00\x00\x0d\xea" +
	"\x00\x00\x03\x53\x00\x00\x17\x11\x00\x00\x17\x11\x51\x00\x00\x00\x00\x00\x01\x26" +
	"\x00\x01\x00\x00\x00\x35\x00\x00\x0d\xea\x00\x00\x03\x53\x00\x00\x00\x00\x00\x00" +
	"\x00\x00\x02\x00\x03\xff\xfd\xff\x02\xfe\xfe\xfe\xff\x7f\x86\x53\x0f\xb6\xc9\x22" +
	"\xcf\xff\x7f\xff\x7f\xff\x7f\xff\x7f\xff\x7f\xff\x7f\xff\x7f\xff\x7f\xff\xac"

func TestConvertJBIG2ImageMask(t *testing.T) {
	content := []byte("q 100 0 0 100 0 0 cm /Im1 Do Q")
	image := "<< /Type /XObject /Subtype /Image /Width 3562 /Height 851 /ImageMask true" +
		" /BitsPerComponent 1 /Filter /JBIG2Decode /Length " + itoa(len(jbig2SampleMask)) +
		" >>\nstream\n" + jbig2SampleMask + "\nendstream"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		image,
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
	images := layerImages(page.Content().Layer[0])
	if len(images) != 1 {
		t.Fatalf("image objects = %d, want 1", len(images))
	}
	media := ofd.Documents[0].GetMedia(models.StID(images[0].ResourceID))
	if media == nil || media.Format != "PNG" {
		t.Fatalf("JBIG2 mask media = %v, want PNG", media)
	}
	data, err := ofd.Documents[0].FileCache.Read(media.MediaFile.String())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 3562 || bounds.Dy() != 851 {
		t.Fatalf("decoded JBIG2 size = %dx%d, want 3562x851", bounds.Dx(), bounds.Dy())
	}
	// 全黑位图作为 ImageMask 时以填充色（默认黑）着色，应处处不透明。
	for _, point := range [][2]int{{0, 0}, {bounds.Dx() / 2, bounds.Dy() / 2}, {bounds.Dx() - 1, bounds.Dy() - 1}} {
		r, g, b, a := img.At(point[0], point[1]).RGBA()
		if a>>8 != 255 || r>>8 > 40 || g>>8 > 40 || b>>8 > 40 {
			t.Fatalf("pixel %v = (%d,%d,%d,%d), want opaque black", point, r>>8, g>>8, b>>8, a>>8)
		}
	}
}

func TestConvertSkipsUnsupportedImageFilter(t *testing.T) {
	// 不支持的图像过滤器（如 JPXDecode）只跳过该图像，不能中断整页转换。
	content := []byte("q 10 0 0 10 0 0 cm /Im1 Do Q 0 0 1 rg 5 5 20 20 re f")
	image := "<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB" +
		" /BitsPerComponent 8 /Filter /JPXDecode /Length 4 >>\nstream\n\x00\x00\x00\x00\nendstream"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		image,
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
		t.Fatalf("unsupported image filter aborted conversion: %v", err)
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
	block := page.Content().Layer[0]
	if len(layerImages(block)) != 0 {
		t.Fatalf("image objects = %d, want 0", len(layerImages(block)))
	}
	if len(layerPaths(block)) == 0 {
		t.Fatal("page content after unsupported image was dropped")
	}
}

func TestConvertJBIG2GrayscaleImage(t *testing.T) {
	// 非 ImageMask 的 JBIG2 位图按 1 位 DeviceGray 输出为不透明灰度 PNG。
	content := []byte("q 100 0 0 100 0 0 cm /Im1 Do Q")
	image := "<< /Type /XObject /Subtype /Image /Width 3562 /Height 851 /ColorSpace /DeviceGray" +
		" /BitsPerComponent 1 /Filter /JBIG2Decode /Length " + itoa(len(jbig2SampleMask)) +
		" >>\nstream\n" + jbig2SampleMask + "\nendstream"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		image,
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
	images := layerImages(page.Content().Layer[0])
	if len(images) != 1 {
		t.Fatalf("image objects = %d, want 1", len(images))
	}
	media := ofd.Documents[0].GetMedia(models.StID(images[0].ResourceID))
	if media == nil {
		t.Fatal("JBIG2 image has no media")
	}
	data, err := ofd.Documents[0].FileCache.Read(media.MediaFile.String())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 3562 || img.Bounds().Dy() != 851 {
		t.Fatalf("decoded JBIG2 size = %dx%d, want 3562x851", img.Bounds().Dx(), img.Bounds().Dy())
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if a>>8 != 255 || r>>8 > 40 || g>>8 > 40 || b>>8 > 40 {
		t.Fatalf("pixel = (%d,%d,%d,%d), want opaque black", r>>8, g>>8, b>>8, a>>8)
	}
}
