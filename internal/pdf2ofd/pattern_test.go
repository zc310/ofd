package pdf2ofd

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
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
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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

func TestConvertSupportsClipWrappedTilingPatternImage(t *testing.T) {
	// Ghostscript 导出的建筑图纸把图案单元写成「裁剪矩形包住一张图」：
	//   q <rect> re W n q <ctm> cm /Im1 Do Q Q
	// 前者只是裁剪、不绘制内容。此前 singlePatternImage 见到 re 就整类放弃，
	// 图案填充全部丢失，被填充的区域还退回上一次的实色（整页变黑）。
	patternContent := "q 0 -100 100 100 re W n q 100 0 0 100 0 0 cm /Im1 Do Q Q"
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
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
		t.Fatal("裁剪包住的图像图案没有输出图像对象")
	}
	if width := images[0].Boundary.Width; width < 35 || width > 35.6 {
		t.Fatalf("image width = %g, want ~35.28", width)
	}
	// 图案展开成功后填充改由图像承担，不能再给原路径按残留实色填充。
	for _, path := range layerPaths(page.Content().Layer[0]) {
		if color := path.FillColor; color != nil && color.Value != nil &&
			color.Value.R == 0 && color.Value.G == 0 && color.Value.B == 0 {
			t.Fatalf("图案区域仍按黑色填充: %+v", path)
		}
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
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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

// jpegGrayMask 编一张 16x16 灰度 JPEG：左半全白、右半全黑。分界线落在 JPEG
// 的 8 像素块边界上，两侧远离块效应，quality 100 下解码值与设定值基本一致。
func jpegGrayMask(t *testing.T) []byte {
	t.Helper()
	mask := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			value := uint8(0x00)
			if x < 8 {
				value = 0xff
			}
			mask.SetGray(x, y, color.Gray{Y: value})
		}
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, mask, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

// jpegMaskStream 把 JPEG 字节包装成 /Filter /DCTDecode 的掩码流。
func jpegMaskStream(data []byte) *types.StreamDict {
	return &types.StreamDict{
		Dict:           types.Dict{"Width": types.Integer(16), "Height": types.Integer(16)},
		Content:        data,
		Raw:            data,
		FilterPipeline: []types.PDFFilter{{Name: "DCTDecode"}},
	}
}

// assertJPEGMaskSamples 逐点比对解码出的掩码样本。左半应为白、右半应为黑。
//
// 必须全图比对：掩码数据是 JPEG 压缩字节，被按裸样解读时开头几个字节恰好是
// FF D8 FF … 的文件头，只抽查首行个别像素会与正确解码的结果碰巧相同。
func assertJPEGMaskSamples(t *testing.T, samples []byte) {
	t.Helper()
	if len(samples) != 16*16 {
		t.Fatalf("sample count = %d, want 256", len(samples))
	}
	mismatch := 0
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			want := uint8(0x00)
			if x < 8 {
				want = 0xff
			}
			got := int(samples[y*16+x]) - int(want)
			if got < 0 {
				got = -got
			}
			diff := got
			if diff > 32 {
				mismatch++
			}
		}
	}
	// JPEG 有损，允许少量边缘误差；按裸样解读则整片都是压缩字节的随机值，
	// 误差比例远高于这个界限。
	if mismatch > 16 {
		t.Fatalf("decoded mask has %d/256 samples far from the expected black/white split, want <= 16", mismatch)
	}
}

func TestPDFImageMaskSamplesDecodesDCTDecode(t *testing.T) {
	// DCTDecode（JPEG）软掩码必须按图像解码成灰度样本。此前只处理 JBIG2/JPX 与
	// 未压缩样本，JPEG 掩码落到 decodePDFImageSamples 把压缩字节当裸样解读，
	// 得到的 alpha 是 JPEG 文件头的字节值而非真正的掩码。
	stream := jpegMaskStream(jpegGrayMask(t))
	samples, err := pdfImageMaskSamples(nil, stream, 16, 16, 8)
	if err != nil {
		t.Fatalf("pdfImageMaskSamples: %v", err)
	}
	assertJPEGMaskSamples(t, samples)
}

func TestPDFImageMaskSamplesRejectsNonJPEGDCTStream(t *testing.T) {
	// DCTDecode 却不是 JPEG 数据时必须报错，不能把压缩字节当裸样静默解读。
	bad := []byte("not a jpeg at all")
	stream := jpegMaskStream(bad)
	if _, err := pdfImageMaskSamples(nil, stream, 16, 16, 8); err == nil {
		t.Fatal("pdfImageMaskSamples accepted non-JPEG DCTDecode data")
	}
}

func TestConvertAppliesJPEGEncodedImageSoftMask(t *testing.T) {
	// 建筑图纸常把「白线黑底的图案 + 灰度 JPEG 掩码」存成一对 DCTDecode 图像。
	// 掩码解码失败会被静默丢弃，图案整幅不透明并铺成整页黑块。
	data := jpegGrayMask(t)
	// 底图同为 16x16，使掩码的每个样本都被用到（2x2 底图只会取到掩码的首行
	// 两点，抽查不出按裸样解读的误码）。
	base := make([]byte, 16*16*3)
	content := []byte("q 16 0 0 16 0 0 cm /Im1 Do Q")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 16 16] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 16 /Height 16 /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 6 0 R /Length " + itoa(len(base)) + " >>\nstream\n" + string(base) + "\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 16 /Height 16 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode /Length " + itoa(len(data)) + " >>\nstream\n" + string(data) + "\nendstream",
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
		t.Fatal("JPEG soft-masked image produced no image object")
	}
	media := ofd.Documents[0].GetMedia(models.StID(images[0].ResourceID))
	if media == nil || !strings.EqualFold(media.Format, "PNG") {
		t.Fatalf("soft-masked image media = %+v, want PNG", media)
	}
	pngData, err := ofd.Documents[0].FileCache.Read(media.MediaFile.String())
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatal(err)
	}
	// 掩码左半为白（alpha 不透明）、右半为黑（alpha 全透明），逐行比对整幅。
	mismatch := 0
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			wantOpaque := x < 8
			if wantOpaque != (a > 0xf000) {
				mismatch++
			}
		}
	}
	if mismatch > 16 {
		t.Fatalf("%d/256 pixels have alpha on the wrong side of the mask split, want <= 16", mismatch)
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
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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

// TestConvertImageAlwaysEmitsCTM 验证普通（未镜像）图片也输出显式 CTM。
// OFD 规范允许省略并按边界尺寸取缺省值，但并非所有阅读器都实现该缺省：缺少
// CTM 时图片无处安放，整页满幅背景图直接不显示。现有 OFD（含官方阅读器样例
// intro.ofd）的图片对象全部显式带 CTM。
func TestConvertImageAlwaysEmitsCTM(t *testing.T) {
	content := []byte("q 100 0 0 100 0 0 cm /Im1 Do Q")
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
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
		t.Fatal("普通图片缺少 CTM，官方阅读器可能不显示（intro.ofd 第 12 页背景图消失）")
	}
	width, height := images[0].Boundary.Width, images[0].Boundary.Height
	if math.Abs(ctm[0]-width) > 0.01 || math.Abs(ctm[3]-height) > 0.01 {
		t.Fatalf("CTM scale = (%g, %g), want boundary (%g, %g)", ctm[0], ctm[3], width, height)
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
	if err := Convert(t.Context(), pdf, &output, ""); err != nil {
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
