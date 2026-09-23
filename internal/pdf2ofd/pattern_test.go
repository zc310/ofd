package pdf2ofd

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func TestConvertEmitsImageForTilingPatternFill(t *testing.T) {
	// canvas 之外的 PDF（例如导出的演示文稿）常用 PatternType 1 平铺图案承载
	// 整页背景图。转换后必须输出图像对象，否则页面会丢失背景。
	patternContent := "q 100 0 0 100 0 0 cm /Im1 Do Q"
	content := []byte("/Pattern cs /P1 scn 0 0 100 100 re f")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Pattern << /P1 6 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x80\nendstream",
		"<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 100 100] /XStep 100 /YStep 100 /Matrix [1 0 0 1 0 0] /Resources << /XObject << /Im1 5 0 R >> >> /Length " + itoa(len(patternContent)) + " >>\nstream\n" + patternContent + "\nendstream",
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
	if len(images) == 0 {
		t.Fatal("tiling pattern fill produced no image object")
	}
	// 页面为 100x100pt，整页背景图宽度应约为 100 * 25.4/72 = 35.28mm。
	width := images[0].Boundary.Width
	if width < 35 || width > 35.6 {
		t.Fatalf("background image width = %g, want ~35.28", width)
	}
}

func TestConvertComposesDenseTilingPatternIntoImage(t *testing.T) {
	// 高密度平铺图案（图块数超过上限，例如整页的 5x5pt 纹理背景）应合成为
	// 单张覆盖填充范围的图片，而不是回退成纯色（此前会变成黑色背景）。
	patternContent := "q 1 0 0 1 0 0 cm /Im1 Do Q"
	content := []byte("/Pattern cs /P1 scn 0 0 100 100 re f")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /Pattern << /P1 6 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 3 >>\nstream\n\xf8\xe6\xbd\nendstream",
		"<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 1 1] /XStep 1 /YStep 1 /Matrix [1 0 0 1 0 0] /Resources << /XObject << /Im1 5 0 R >> >> /Length " + itoa(len(patternContent)) + " >>\nstream\n" + patternContent + "\nendstream",
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
		t.Fatalf("dense tiling pattern produced %d images, want 1 composed image", len(images))
	}
	if width := images[0].Boundary.Width; width < 35 || width > 35.6 {
		t.Fatalf("composed image width = %g, want ~35.28", width)
	}
	media := ofd.Documents[0].GetMedia(models.StID(images[0].ResourceID))
	if media == nil || !strings.EqualFold(media.Format, "PNG") {
		t.Fatal("composed tiling image is not a PNG")
	}
	data, err := ofd.Documents[0].FileCache.Read(media.MediaFile.String())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() < 50 || img.Bounds().Dy() < 50 {
		t.Fatalf("composed image too small: %v", img.Bounds())
	}
	if r, g, b, _ := img.At(0, 0).RGBA(); r>>8 < 200 || g>>8 < 180 || b>>8 > 210 {
		t.Fatalf("composed pixel = (%d,%d,%d), want tan", r>>8, g>>8, b>>8)
	}
}

func TestConvertAppliesImageSoftMask(t *testing.T) {
	// 图像带 SMask 时必须把掩码作为 alpha 应用，否则透明区域会被绘制成
	// 不透明色块（例如整片黑色遮住背景）。
	content := []byte("q 10 0 0 10 0 0 cm /Im1 Do Q")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 6 0 R /Length 12 >>\nstream\n\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 4 >>\nstream\n\xff\x00\x00\xff\nendstream",
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
	if len(images) == 0 {
		t.Fatal("soft-masked image produced no image object")
	}
	media := ofd.Documents[0].GetMedia(models.StID(images[0].ResourceID))
	if media == nil || !strings.EqualFold(media.Format, "PNG") {
		t.Fatal("soft-masked image is not a PNG")
	}
	data, err := ofd.Documents[0].FileCache.Read(media.MediaFile.String())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0xffff {
		t.Fatalf("opaque corner alpha = %d, want 65535", a)
	}
	if _, _, _, a := img.At(1, 0).RGBA(); a != 0 {
		t.Fatalf("transparent corner alpha = %d, want 0", a)
	}
}

func TestConvertImageNegativeYMatrixEmitsFlipCTM(t *testing.T) {
	// 扫描件常用负 Y 缩放 CTM（如 595 0 0 -842 ... cm）翻转图像。OFD 图片
	// 缺省按边界正放，这里必须输出负 Y 缩放的 CTM，否则上下颠倒。
	content := []byte("q 100 0 0 -100 0 100 cm /Im1 Do Q")
	base := "\xff\x00\x00\xff\x00\x00\x00\x00\xff\x00\x00\xff"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 12 >>\nstream\n" + base + "\nendstream",
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
	ctm := images[0].CTM
	if ctm == nil {
		t.Fatal("image with negative Y matrix has no CTM; scan would render upside down")
	}
	width := images[0].Boundary.Width
	height := images[0].Boundary.Height
	if ctm[0] <= 0 || ctm[3] >= 0 {
		t.Fatalf("CTM = %v, want positive X and negative Y scale", *ctm)
	}
	if ctm[5] < height*0.99 || ctm[5] > height*1.01 {
		t.Fatalf("CTM Y offset = %g, want ~%g (height)", ctm[5], height)
	}
	if ctm[0] < width*0.99 || ctm[0] > width*1.01 {
		t.Fatalf("CTM X scale = %g, want ~%g (width)", ctm[0], width)
	}
}

func TestConvertSoftMaskKeepsBaseColorAtLowAlpha(t *testing.T) {
	// 软掩码会把接近透明的像素写入 alpha。低 alpha 像素的 RGB 必须保持原色；
	// 若按 RGBA（预乘）编码，PNG 的反预乘会把颜色放大成红/品红边缘。
	content := []byte("q 10 0 0 10 0 0 cm /Im1 Do Q")
	base := "\xc8\x64\x32\x28\x50\x78\x0a\x14\x1e\x3c\x46\x50"
	mask := "\xff\x08\x00\x80"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 6 0 R /Length 12 >>\nstream\n" + base + "\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 4 >>\nstream\n" + mask + "\nendstream",
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
	if len(images) == 0 {
		t.Fatal("soft-masked image produced no image object")
	}
	media := ofd.Documents[0].GetMedia(models.StID(images[0].ResourceID))
	if media == nil {
		t.Fatal("soft-masked image has no media")
	}
	data, err := ofd.Documents[0].FileCache.Read(media.MediaFile.String())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded image type = %T, want *image.NRGBA", img)
	}
	// 像素 (1,0) 的 alpha 为 8，颜色应保持基础图的 (40,80,120)。
	offset := nrgba.PixOffset(1, 0)
	r, g, b, a := nrgba.Pix[offset], nrgba.Pix[offset+1], nrgba.Pix[offset+2], nrgba.Pix[offset+3]
	if a != 8 {
		t.Fatalf("low-alpha pixel alpha = %d, want 8", a)
	}
	if r != 40 || g != 80 || b != 120 {
		t.Fatalf("low-alpha pixel color = (%d,%d,%d), want (40,80,120)", r, g, b)
	}
}
