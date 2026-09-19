package pdf2ofd

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/pkg/creator"
)

func TestConvertBasicPage(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("q 2 w 1 0 0 RG 72 72 m 144 72 l 144 144 l 72 144 l h S Q BT /F1 12 Tf 72 200 Td (Hello) Tj ET"), 144, 288)
	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(output.Bytes(), []byte("PK")) {
		t.Fatal("Convert did not write an OFD ZIP package")
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) < 4 {
		t.Fatalf("OFD entry count = %d, want at least 4", len(archive.File))
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if len(ofd.Documents) != 1 || len(ofd.Documents[0].Pages) != 1 {
		t.Fatalf("documents/pages = %d/%d, want 1/1", len(ofd.Documents), len(ofd.Documents[0].Pages))
	}
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	content := page.Content().Layer[0]
	if len(content.TextObject) != 1 || content.TextObject[0].TextCode[0].Value != "Hello" {
		t.Fatalf("text objects = %+v", content.TextObject)
	}
	if len(content.PathObject) != 1 {
		t.Fatalf("path objects = %d, want 1", len(content.PathObject))
	}
	if got := content.PathObject[0].Boundary.Width; got < 24.9 || got > 25.5 {
		t.Fatalf("path width = %gmm, want about 25.4mm", got)
	}
}

func TestConvertHonorsNonZeroMediaBoxAndUserUnit(t *testing.T) {
	pdf := testutil.MinimalPDFWithPage([]byte("0 0 1 rg 10 20 30 40 re f"), "[10 20 110 220]", 2)
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
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	if got := box.Width; got < 70.5 || got > 71.2 {
		t.Fatalf("page width = %gmm, want about 70.56mm", got)
	}
	if got := box.Height; got < 141.0 || got > 142.0 {
		t.Fatalf("page height = %gmm, want about 141.11mm", got)
	}
}

func TestConvertUsesTextMatrixScale(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("BT /F1 1 Tf 20 0 0 20 72 200 Tm (Hello) Tj ET"), 144, 288)
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
	text := page.Content().Layer[0].TextObject[0]
	if text.Boundary.Height < 6.5 || text.Boundary.Height > 7.2 {
		t.Fatalf("text height = %gmm, want about 7.06mm", text.Boundary.Height)
	}
}

func TestConvertTextLeadingAdvancesText(t *testing.T) {
	// TL 设置行距，T* 用它换行。忽略 TL 会让后续文字落在同一行造成公式错位。
	pdf := testutil.MinimalPDF([]byte("BT /F1 12 Tf 20 200 Td 14 TL (abc) Tj T* (def) Tj ET"), 144, 288)
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
	texts := page.Content().Layer[0].TextObject
	if len(texts) != 2 {
		t.Fatalf("text objects = %d, want 2", len(texts))
	}
	// 14pt ≈ 4.94mm，两行基线应相差一个行距。
	gap := math.Abs(texts[0].Boundary.Y - texts[1].Boundary.Y)
	if gap < 4.8 || gap > 5.0 {
		t.Fatalf("text baseline gap = %gmm, want about 4.94mm", gap)
	}
}

func TestConvertWhitespaceOnlyShowAdvancesText(t *testing.T) {
	// 纯空白 Tj 不产生文字对象，但仍要推进文本矩阵，否则后续文字会丢失
	// 前导空格的位移而整体左移（代码块 "    >>>" 的缩进会消失）。
	pdf := testutil.MinimalPDF([]byte("BT /F1 12 Tf 20 200 Td (A) Tj (   ) Tj (B) Tj ET"), 144, 288)
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
	texts := page.Content().Layer[0].TextObject
	if len(texts) != 2 {
		t.Fatalf("text objects = %d, want 2", len(texts))
	}
	// "A" 宽 6pt，"   " 宽 18pt，因此 "B" 相对 "A" 右移 24pt ≈ 8.47mm。
	gap := texts[1].Boundary.X - texts[0].Boundary.X
	want := 24 * pdfPointToMillimeter
	if math.Abs(gap-want) > 0.05 {
		t.Fatalf("text gap = %gmm, want about %gmm", gap, want)
	}
}

func TestPDFTextDeltasUsePDFWidthsWhenFontAdvanceMismatch(t *testing.T) {
	// ReportLab 子集可能写入与 unitsPerEm 不一致的 hmtx；此时必须按 PDF
	// /Widths 生成 DeltaX，否则阅读器按字体字宽排版会把文字挤在一起。
	font := pdfFontInfo{
		widths:      map[int]float64{65: 602.0508, 66: 602.0508, 67: 602.0508},
		glyphWidths: map[uint16]float64{65: 293.9, 66: 293.9, 67: 293.9},
	}
	deltas := pdfTextDeltas([]uint16{65, 66, 67}, "ABC", font, 10, 0, 0, 100, 1)
	if len(deltas) != 2 {
		t.Fatalf("deltas = %v, want length 2", deltas)
	}
	want := 602.0508 / 1000 * 10 * pdfPointToMillimeter
	for index, delta := range deltas {
		if math.Abs(delta-want) > 1e-9 {
			t.Fatalf("deltas[%d] = %g, want %g", index, delta, want)
		}
	}
}

func TestPDFTextDeltasSkipWhenFontAdvanceMatches(t *testing.T) {
	font := pdfFontInfo{
		widths:      map[int]float64{65: 602, 66: 602},
		glyphWidths: map[uint16]float64{65: 602, 66: 602},
	}
	if deltas := pdfTextDeltas([]uint16{65, 66}, "AB", font, 10, 0, 0, 100, 1); deltas != nil {
		t.Fatalf("deltas = %v, want nil when font advance matches /Widths", deltas)
	}
}

func TestPDFMatrixConcatenationKeepsLocalTranslation(t *testing.T) {
	current := [6]float64{2, 0, 0, 2, 0, 0}
	translation := [6]float64{1, 0, 0, 1, 10, 20}
	got := multiplyPDFMatrix(current, translation)
	if x, y := transformPDFPoint(0, 0, got); x != 20 || y != 40 {
		t.Fatalf("translated origin = (%g, %g), want (20, 40)", x, y)
	}
}

func TestPDFImageMaskPaintsZeroSamples(t *testing.T) {
	assertPixel := func(t *testing.T, decoded image.Image, x int, painted bool) {
		t.Helper()
		r, g, b, a := decoded.At(x, 0).RGBA()
		if painted {
			if r != uint32(12*257) || g != uint32(34*257) || b != uint32(56*257) || a != uint32(255*257) {
				t.Fatalf("painted mask pixel = (%d, %d, %d, %d)", r, g, b, a)
			}
			return
		}
		if r != 0 || g != 0 || b != 0 || a != 0 {
			t.Fatalf("transparent mask pixel = (%d, %d, %d, %d)", r, g, b, a)
		}
	}
	tests := []struct {
		name      string
		decode    types.Array
		paintLeft bool
	}{
		{name: "default decode paints zero samples"},
		{name: "reversed decode paints one samples", decode: types.Array{types.Integer(1), types.Integer(0)}, paintLeft: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dict := types.Dict{
				"Width":            types.Integer(2),
				"Height":           types.Integer(1),
				"BitsPerComponent": types.Integer(1),
				"ImageMask":        types.Boolean(true),
			}
			if test.decode != nil {
				dict["Decode"] = test.decode
			}
			stream := &types.StreamDict{Dict: dict, Content: []byte{0x80}}
			data, format, err := pdfImageData(nil, stream, pdfColor{r: 12, g: 34, b: 56})
			if err != nil {
				t.Fatal(err)
			}
			if format != "PNG" {
				t.Fatalf("image format = %q, want PNG", format)
			}
			decoded, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			// 0x80 的首位是 1。默认 Decode [0 1] 着色 0 位，所以左像素透明、
			// 右像素用当前填充色着色；[1 0] 反过来。
			assertPixel(t, decoded, 0, test.paintLeft)
			assertPixel(t, decoded, 1, !test.paintLeft)
		})
	}
}

func TestConvertInlineImageMaskEmitsImage(t *testing.T) {
	// dvips 等生产者用 1x1 内联图像掩码画表格线，转换必须保留它们。
	content := []byte("q 144 0 0 144 0 0 cm\nBI\n/IM true\n/W 1\n/H 1\n/BPC 1\nID \x00\nEI\nQ")
	pdf := testutil.MinimalPDF(content, 144, 288)
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
	if got := images[0].Boundary.Width; got < 50.7 || got > 50.9 {
		t.Fatalf("inline image width = %gmm, want about 50.8mm", got)
	}
}

func TestConvertInlineColorImageEmitsImage(t *testing.T) {
	// 无颜色空间时默认 DeviceGray，显式 /CS 使用 /RGB 等缩写，都必须能解析。
	content := []byte("q 144 0 0 144 0 0 cm\nBI\n/CS /RGB\n/W 1\n/H 1\n/BPC 8\nID \xff\x00\x00\nEI\nQ")
	pdf := testutil.MinimalPDF(content, 144, 288)
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
	if images := page.Content().Layer[0].ImageObject; len(images) != 1 {
		t.Fatalf("image objects = %d, want 1", len(images))
	}
}

func TestPDFImageDataDecodesFiltersBeforeDCTDecode(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(jpeg); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	stream := &types.StreamDict{
		Dict: types.Dict{
			"Width":  types.Integer(1),
			"Height": types.Integer(1),
		},
		Raw: compressed.Bytes(),
		FilterPipeline: []types.PDFFilter{
			{Name: "FlateDecode"},
			{Name: "DCTDecode"},
		},
	}
	data, format, err := pdfImageData(nil, stream, pdfColor{})
	if err != nil {
		t.Fatal(err)
	}
	if format != "JPEG" {
		t.Fatalf("image format = %q, want JPEG", format)
	}
	if !bytes.Equal(data, jpeg) {
		t.Fatalf("image data = % x, want the Flate-decoded JPEG % x", data, jpeg)
	}
}

func TestConvertAdobeCMYKJPEGToRGB(t *testing.T) {
	pdf, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "pdf", "sample0.pdf"))
	if err != nil {
		t.Skipf("测试文件不存在，跳过: %v", err)
	}
	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	// sample0.pdf 第 2 页是一个 DeviceCMYK 的红色印章 JPEG，整幅图白底。
	page := ofd.Documents[0].Pages[1]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	images := page.Content().Layer[0].ImageObject
	if len(images) == 0 {
		t.Fatal("page 2 has no image objects")
	}
	found := false
	for _, image := range images {
		media := ofd.Documents[0].GetMedia(models.StID(image.ResourceID))
		if media == nil || !strings.EqualFold(media.Format, "PNG") {
			continue
		}
		data, err := ofd.Documents[0].FileCache.Read(media.MediaFile.String())
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() < 1000 {
			continue
		}
		if r, g, b, _ := img.At(0, 0).RGBA(); r>>8 < 128 || g>>8 < 128 || b>>8 < 128 {
			t.Fatalf("CMYK image background = (%d,%d,%d), want white", r>>8, g>>8, b>>8)
		}
		// 印章是纯品红油墨，应渲染为标准印刷品红 (~236,0,139)，
		// 而不是 color.CMYKToRGB 产生的纯品红 (255,0,255)。
		bounds := img.Bounds()
		var sumR, sumG, sumB, count int
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if g>>8 > 60 {
					continue
				}
				sumR += int(r >> 8)
				sumG += int(g >> 8)
				sumB += int(b >> 8)
				count++
			}
		}
		if count == 0 {
			t.Fatal("no seal pixels found in the CMYK image")
		}
		avgR, avgG, avgB := sumR/count, sumG/count, sumB/count
		if avgR < 200 || avgG > 60 || avgB < 90 || avgB > 190 {
			t.Fatalf("seal color = (%d,%d,%d), want process magenta near (236,0,139)", avgR, avgG, avgB)
		}
		found = true
	}
	if !found {
		t.Fatal("no PNG image converted from the CMYK JPEG")
	}
}

func TestPDFInterpreterHandlesSCNColorComponents(t *testing.T) {
	interpreter := &pdfInterpreter{}
	resources := types.Dict{}
	if err := interpreter.operator("scn", []any{float64(1), float64(1), float64(1)}, resources, 0); err != nil {
		t.Fatal(err)
	}
	if interpreter.state.fill != (pdfColor{r: 255, g: 255, b: 255}) {
		t.Fatalf("scn fill = %+v, want white", interpreter.state.fill)
	}
	if err := interpreter.operator("SCN", []any{float64(0.5)}, resources, 0); err != nil {
		t.Fatal(err)
	}
	if interpreter.state.stroke != (pdfColor{r: 127, g: 127, b: 127}) {
		t.Fatalf("SCN stroke = %+v, want 50%% gray", interpreter.state.stroke)
	}
	if err := interpreter.operator("scn", []any{pdfName("P1")}, resources, 0); err != nil {
		t.Fatal(err)
	}
	if interpreter.state.fill != (pdfColor{r: 255, g: 255, b: 255}) {
		t.Fatalf("pattern scn changed fill to %+v", interpreter.state.fill)
	}
}

// TestConvertClosesImplicitFillSubpaths 验证填充路径会补齐闭合命令。PDF 填充
// 隐式闭合子路径，而 OFD 阅读器只填充显式闭合的路径；不补 C 时，用
// m/l…/f* 绘制的细长矩形（如 sample2.pdf 第 8 页的条形图）不会渲染。
func TestConvertClosesImplicitFillSubpaths(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("0.5 0.5 0.5 rg 10 10 m 100 10 l 100 20 l 10 20 l f*"), 144, 288)
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
	paths := page.Content().Layer[0].PathObject
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	commands := paths[0].AbbreviatedData
	if len(commands) == 0 || commands[len(commands)-1].Type != models.Close {
		t.Fatalf("last path command = %+v, want Close", commands)
	}
}

func TestConvertExplicitlyDisablesStrokeForFillOnlyPaths(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("0 0 1 rg 10 20 30 40 re f"), 144, 288)
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
	paths := page.Content().Layer[0].PathObject
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	if paths[0].Stroke != "false" {
		t.Fatalf("fill-only path Stroke = %q, want \"false\"", paths[0].Stroke)
	}
}

func TestConvertScalesLineWidthByCTM(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("2 w 0.5 0 0 0.5 0 0 cm 0 0 m 100 0 l S"), 144, 288)
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
	paths := page.Content().Layer[0].PathObject
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	// 2pt 线宽经 0.5 倍 CTM 缩放后为 1pt，即 0.3528mm。
	// XML 序列化保留 4 位小数，容差需覆盖该精度（≤0.0001mm）。
	if want := 2 * 0.5 * pdfPointToMillimeter; math.Abs(paths[0].LineWidth-want) > 1e-4 {
		t.Fatalf("line width = %gmm, want %gmm", paths[0].LineWidth, want)
	}
}

func TestPDFStandardGlyphName(t *testing.T) {
	tests := map[rune]string{
		' ': "space",
		'0': "zero",
		'2': "two",
		'9': "nine",
		'A': "A",
		'z': "z",
		'.': "period",
		'(': "parenleft",
		'~': "asciitilde",
	}
	for value, want := range tests {
		if got := pdfStandardGlyphName(value); got != want {
			t.Errorf("pdfStandardGlyphName(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestPDFDecodeGBKEucText(t *testing.T) {
	font := pdfFontInfo{
		encoding:  "GBK-EUC-H",
		codeBytes: 2,
		widths:    map[int]float64{},
		defaultW:  1000,
	}
	// "国" = 0xB9FA, "2015" 为 ASCII 单字节，"闽" = 0xC3F6。
	data := []byte{0xB9, 0xFA, '2', '0', '1', '5', 0xC3, 0xF6}
	text, codes := decodePDFText(data, font)
	if text != "国2015闽" {
		t.Fatalf("decodePDFText = %q, want %q", text, "国2015闽")
	}
	if len(codes) != 6 {
		t.Fatalf("codes = %d entries, want 6", len(codes))
	}
	// 国/闽 各一个双字节码，数字四个单字节码。
	if codes[0] != 0xB9FA || codes[1] != '2' || codes[4] != '5' || codes[5] != 0xC3F6 {
		t.Fatalf("codes = %v, want [0xb9fa 0x32 0x30 0x31 0x35 0xc3f6]", codes)
	}
	// 半角单字节码按 defaultW 的一半计宽，全角码使用 defaultW。
	codes = []uint16{0xB9FA, '2', '0', 0xC3F6}
	if got := pdfTextWidth(codes, font, 1000, 0, 0, 100); got != 3000 {
		t.Fatalf("pdfTextWidth = %g, want 3000", got)
	}
}

func TestPDFDecodeFontWithoutEncodingMap(t *testing.T) {
	font := pdfFontInfo{
		encoding:  "Identity-H",
		codeBytes: 2,
		widths:    map[int]float64{},
		defaultW:  1000,
	}
	// 无预定义 CJK CMap，2 字节码落到原有回退逻辑。
	data := []byte{'A', 0, 'B', 0}
	text, _ := decodePDFText(data, font)
	if text != "AB" {
		t.Fatalf("decodePDFText = %q, want %q", text, "AB")
	}
}

func TestParsePDFToUnicodeHandlesEntryCountPrefix(t *testing.T) {
	// CMap 规范允许 beginbfchar/beginbfrange 前带条目数量，例如
	// "136 beginbfchar"。部分 PDF（如 ReportLab 输出）依赖该写法。
	data := []byte("/CIDInit /ProcSet findresource begin\n" +
		"1 begincodespacerange\n<00> <FF>\nendcodespacerange\n" +
		"2 beginbfchar\n<41> <0041>\n<85> <00C4>\nendbfchar\n" +
		"endcmap\n")
	result := parsePDFToUnicode(data)
	if result[0x41] != "A" {
		t.Fatalf("0x41 = %q, want %q", result[0x41], "A")
	}
	if result[0x85] != "\u00C4" {
		t.Fatalf("0x85 = %q, want %q", result[0x85], "\u00C4")
	}
	// 不带数量前缀的写法仍需兼容。
	noCount := []byte("1 beginbfchar\n<42> <0042>\nendbfchar\n")
	if got := parsePDFToUnicode(noCount)[0x42]; got != "B" {
		t.Fatalf("0x42 = %q, want %q", got, "B")
	}
}

func TestPDFTextGlyphTransformsKeepsMappedGlyphsWhenSomeCodesUnmapped(t *testing.T) {
	font := pdfFontInfo{
		data:      fakeSFNTWithGlyphCount(200),
		codeBytes: 1,
		codeToGID: map[uint16]uint16{0x41: 34, 0x42: 35},
		toUnicode: map[uint16]string{},
		cidToGID:  map[uint16]uint16{},
	}
	// 第二个字符编码没有任何字形映射，不能因此丢弃整段 CGTransform。
	codes := []uint16{0x41, 0x99, 0x42}
	transforms := pdfTextGlyphTransforms(codes, "ABC", font)
	if len(transforms) != 2 {
		t.Fatalf("transforms = %d, want 2", len(transforms))
	}
	if transforms[0].CodePosition != 0 || transforms[0].Glyphs[0] != 34 {
		t.Fatalf("first transform = %+v", transforms[0])
	}
	if transforms[1].CodePosition != 2 || transforms[1].Glyphs[0] != 35 {
		t.Fatalf("second transform = %+v", transforms[1])
	}
}

// fakeSFNTWithGlyphCount 构造只包含 SFNT 头和 maxp 表的最小字体，供
// embeddedFontGlyphCount 使用，避免测试依赖真实字体文件。
func fakeSFNTWithGlyphCount(numGlyphs uint16) []byte {
	const offset = 12 + 16
	data := make([]byte, offset+6)
	binary.BigEndian.PutUint32(data[0:4], 0x00010000)
	binary.BigEndian.PutUint16(data[4:6], 1)
	entry := data[12:28]
	copy(entry[0:4], "maxp")
	binary.BigEndian.PutUint32(entry[8:12], offset)
	binary.BigEndian.PutUint32(entry[12:16], 6)
	binary.BigEndian.PutUint16(data[offset+4:offset+6], numGlyphs)
	return data
}

func TestConvertTestdataPDFs(t *testing.T) {
	paths, err := filepath.Glob("../../test/testdata/pdf/*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Skip("测试文件不存在，跳过")
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("测试文件不存在，跳过: %v", err)
			}
			var output bytes.Buffer
			if err := Convert(data, &output); err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(output.Bytes(), []byte("PK")) {
				t.Fatal("conversion did not produce an OFD ZIP package")
			}
			ofd, err := parser.NewOFD(output.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			if len(ofd.Documents) != 1 || len(ofd.Documents[0].Pages) == 0 {
				t.Fatalf("documents/pages = %d/%d", len(ofd.Documents), len(ofd.Documents[0].Pages))
			}
			if strings.HasSuffix(path, "sample2.pdf") || strings.HasSuffix(path, "fontforge_cn.pdf") {
				fontCount := 0
				for _, document := range ofd.Documents {
					document.ForEachFont(func(_ models.StID, font *models.Font) bool {
						if font != nil && font.FontFile != "" {
							fontCount++
						}
						return true
					})
				}
				if fontCount == 0 {
					t.Fatal("expected embedded fonts in converted fixture")
				}
			}
			// ReportLab 子集字体的字形名是 uF00XX，无法按标准字形名解析。
			// 必须回退到字体 cmap 生成 CGTransform，否则阅读器会退回 Unicode
			// cmap 查询并渲染成错误的字形宽度。
			if strings.HasSuffix(path, "byte_of_python_v192.pdf") {
				transformCount := 0
				urlTransform := -1
				for _, document := range ofd.Documents {
					for _, page := range document.Pages {
						if err := page.EnsureLoaded(); err != nil {
							t.Fatal(err)
						}
						for _, layer := range page.Content().Layer {
							for _, text := range layer.TextObject {
								transformCount += len(text.CGTransform)
								for _, code := range text.TextCode {
									if strings.Contains(code.Value, "Pythona)") {
										urlTransform = len(text.CGTransform)
									}
								}
							}
						}
					}
				}
				if transformCount == 0 {
					t.Fatal("expected CGTransform for embedded subset fonts")
				}
				// 第 9 页底部 URL 中个别字符编码没有字形，不能因此丢弃
				// 整个 TextObject 的 CGTransform，否则相邻字形渲染成方块。
				if urlTransform == 0 {
					t.Fatal("expected CGTransform on URL text object")
				}
			}
			// CID 字体的子集字形序不连续（cid 值可能大于 numGlyphs），
			// 不能用 glyph < numGlyphs 判定字形缺失，否则整段
			// CGTransform 被丢弃（斜体/装饰字退化成方块），且因内嵌
			// 字体 hmtx 为占位值而没有 DeltaX，导致字宽错位。
			if strings.HasSuffix(path, "bibble.pdf") {
				noCGT := 0
				deltaCount := 0
				for _, document := range ofd.Documents {
					for _, page := range document.Pages {
						if err := page.EnsureLoaded(); err != nil {
							t.Fatal(err)
						}
						for _, layer := range page.Content().Layer {
							for _, text := range layer.TextObject {
								for _, code := range text.TextCode {
									if len(code.DeltaX) > 0 {
										deltaCount++
									}
								}
								if len(text.CGTransform) == 0 {
									noCGT++
								}
							}
						}
					}
				}
				if noCGT > 0 {
					t.Fatalf("bibble: %d text objects lack CGTransform (CID subset glyph mapping)", noCGT)
				}
				if deltaCount == 0 {
					t.Fatal("bibble: expected DeltaX to correct placeholder font advances")
				}
			}
			// Type3 与部分子集字体没有 /ToUnicode，只能靠 /Encoding 的
			// /Differences 字形名恢复 Unicode；缺失时会输出 U+FFFD 乱码。
			if strings.HasSuffix(path, "magazine.pdf") {
				replacement := 0
				mergedRuns := 0
				for _, document := range ofd.Documents {
					for _, page := range document.Pages {
						if err := page.EnsureLoaded(); err != nil {
							t.Fatal(err)
						}
						for _, layer := range page.Content().Layer {
							for _, text := range layer.TextObject {
								for _, code := range text.TextCode {
									replacement += strings.Count(code.Value, "\uFFFD")
									if len(code.DeltaX) > 1 {
										mergedRuns++
									}
								}
							}
						}
					}
				}
				if replacement > 0 {
					t.Fatalf("magazine: %d 个替换字符，Type3 编码未恢复为 Unicode", replacement)
				}
				// 生产者逐字输出 Tj，转换后应把相邻单字合并为带合成 DeltaX 的文字段。
				if mergedRuns == 0 {
					t.Fatal("magazine: 期望单字文字对象合并为带 DeltaX 的文字段")
				}
			}
		})
	}
}

// TestPDFTextGlyphTransformsCIDFontDirect 检验 CID 字体的字形覆盖
// 判断：code(CID) 数值可以大于内嵌字体的 numGlyphs，只要 fontfix
// 的私有区映射 F0000+code 存在，渲染端就能找到该字形。
func TestPDFTextGlyphTransformsCIDFontDirect(t *testing.T) {
	font := pdfFontInfo{
		data:      fakeSFNTWithGlyphCount(200),
		codeBytes: 2,
		toUnicode: map[uint16]string{},
		cidToGID:  map[uint16]uint16{},
		sfnt:      nil,
	}
	// code 小于 numGlyphs：保守路径直接命中
	if !pdfFontCoversGlyph(font, 150) {
		t.Fatal("glyph 150 should be covered when below numGlyphs")
	}
	// code 大于 numGlyphs 且无 sfnt：无法确认字形存在
	if pdfFontCoversGlyph(font, 250) {
		t.Fatal("glyph 250 should not be covered without sfnt")
	}
	// codeBytes == 1（非 CID 路径）：不进入此函数覆盖逻辑
	font.codeBytes = 1
	font.codeToGID = map[uint16]uint16{0x41: 34, 0x42: 35}
	codes := []uint16{0x41, 0x99, 0x42}
	transforms := pdfTextGlyphTransforms(codes, "ABC", font)
	if len(transforms) != 2 {
		t.Fatalf("transforms = %d, want 2", len(transforms))
	}
}

// TestPDFCIDFontGlyphWidthsNilWhenNoSFNT 在没有 sfnt 解析结果时
// 返回空表（旧有行为：不会因 panic 崩溃）。
func TestPDFCIDFontGlyphWidthsNilWhenNoSFNT(t *testing.T) {
	if got := pdfCIDFontGlyphWidths(map[int]float64{65: 602}, nil); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

// TestConvertAppliesPathClip 验证 PDF 的 W/W* 裁剪会写入后续图元的 Clips。
// 没有该处理时，被裁剪的图片/路径会按原始尺寸溢出（如 magazine.pdf 第 5 页
// 底部图片未裁剪而变大）。
func TestConvertAppliesPathClip(t *testing.T) {
	content := []byte("q 20 20 60 40 re W n 10 10 200 200 re f Q")
	pdf := testutil.MinimalPDF(content, 300, 300)
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
	clipped := 0
	for _, layer := range page.Content().Layer {
		for _, path := range layer.PathObject {
			if path.Clips != nil && len(path.Clips.Clip) > 0 {
				clipped++
				if len(path.Clips.Clip[0].Area) == 0 || path.Clips.Clip[0].Area[0].Path == nil {
					t.Fatalf("裁剪区缺少路径: %+v", path.Clips.Clip[0])
				}
			}
		}
	}
	if clipped == 0 {
		t.Fatal("expected W/W* 裁剪写入图元 Clips")
	}
}

// TestCommitPendingClipOnlyAffectsCurrentScope 验证 q/Q 会正确恢复裁剪区。
func TestCommitPendingClipOnlyAffectsCurrentScope(t *testing.T) {
	content := []byte("q 20 20 60 40 re W n Q 10 10 200 200 re f")
	pdf := testutil.MinimalPDF(content, 300, 300)
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
	for _, layer := range page.Content().Layer {
		for _, path := range layer.PathObject {
			if path.Clips != nil && len(path.Clips.Clip) > 0 {
				t.Fatal("Q 之后的路径不应继承已恢复的裁剪区")
			}
		}
	}
}

// TestMergeAdjacentTextItems 验证相邻单字 TextObject 会合并为一个对象，
// 并用合成的 DeltaX 记录逐字步进（位置不变）。
func TestMergeAdjacentTextItems(t *testing.T) {
	fill := true
	item := func(x float64, value, font string) creator.Text {
		return creator.Text{X: x, Y: 10, Width: 2, Height: 8, Value: value, Font: font, Size: 8, Fill: &fill}
	}
	merged := mergeAdjacentTextItems([]creator.Item{
		item(0, "a", "F"), item(2, "b", "F"), item(4, "c", "F"),
	})
	if len(merged) != 1 {
		t.Fatalf("merged objects = %d, want 1", len(merged))
	}
	text, ok := merged[0].(creator.Text)
	if !ok {
		t.Fatalf("merged item type = %T", merged[0])
	}
	if text.Value != "abc" {
		t.Fatalf("merged value = %q, want %q", text.Value, "abc")
	}
	if text.X != 0 || math.Abs(text.Width-6) > 1e-9 {
		t.Fatalf("merged boundary = X:%g Width:%g, want X:0 Width:6", text.X, text.Width)
	}
	if len(text.TextCodes) != 1 || len(text.TextCodes[0].DeltaX) != 2 {
		t.Fatalf("merged TextCodes = %+v, want 1 code with 2 deltas", text.TextCodes)
	}
	if d := text.TextCodes[0].DeltaX; math.Abs(d[0]-2) > 1e-9 || math.Abs(d[1]-2) > 1e-9 {
		t.Fatalf("merged DeltaX = %v, want [2 2]", d)
	}
}

// TestMergeAdjacentTextItemsBreaksAndStyle 验证大间距与样式差异会断开合并。
func TestMergeAdjacentTextItemsBreaksAndStyle(t *testing.T) {
	fill := true
	item := func(x float64, font string) creator.Text {
		return creator.Text{X: x, Y: 10, Width: 2, Height: 8, Value: "a", Font: font, Size: 8, Fill: &fill}
	}
	// 大间距：字号 8，间距阈值 6.4mm，20-2=18 应断开。
	bigGap := mergeAdjacentTextItems([]creator.Item{item(0, "F"), item(20, "F")})
	if len(bigGap) != 2 {
		t.Fatalf("big gap merged objects = %d, want 2", len(bigGap))
	}
	// 字体不同应断开。
	diffFont := mergeAdjacentTextItems([]creator.Item{item(0, "F"), item(2, "G")})
	if len(diffFont) != 2 {
		t.Fatalf("different font merged objects = %d, want 2", len(diffFont))
	}
}

// TestMergeAdjacentTextItemsPreservesCGTransform 验证合并后 CGTransform 的
// CodePosition 会顺延，保证字形映射仍然有效。
func TestMergeAdjacentTextItemsPreservesCGTransform(t *testing.T) {
	fill := true
	glyph := creator.CGTransform{CodeCount: 1, GlyphCount: 1, Glyphs: []int{42}}
	item := func(x float64) creator.Text {
		return creator.Text{X: x, Y: 10, Width: 2, Height: 8, Value: "a", Font: "F", Size: 8, Fill: &fill, CGTransforms: []creator.CGTransform{glyph}}
	}
	merged := mergeAdjacentTextItems([]creator.Item{item(0), item(2), item(4)})
	if len(merged) != 1 {
		t.Fatalf("merged objects = %d, want 1", len(merged))
	}
	text := merged[0].(creator.Text)
	if len(text.CGTransforms) != 3 {
		t.Fatalf("CGTransforms = %d, want 3", len(text.CGTransforms))
	}
	for index, transform := range text.CGTransforms {
		if transform.CodePosition != index {
			t.Fatalf("CGTransform[%d].CodePosition = %d, want %d", index, transform.CodePosition, index)
		}
	}
}

// TestConvertWritesConverterMetadata 验证转换后的 OFD 标识了转换工具本身，
// 并把源格式写入自定义元数据。
func TestConvertWritesConverterMetadata(t *testing.T) {
	pdf := testutil.MinimalPDF([]byte("BT /F1 12 Tf 20 200 Td (Hello) Tj ET"), 144, 288)
	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
		t.Fatal(err)
	}
	ofdXML := readOFDXML(t, output.Bytes())
	for _, want := range []string{
		"<Creator>zc310/ofd</Creator>",
		"<CreatorVersion>0.0.1</CreatorVersion>",
		`<CustomData Name="SourceFormat">PDF</CustomData>`,
	} {
		if !bytes.Contains(ofdXML, []byte(want)) {
			t.Fatalf("OFD.xml 缺少 %q:\n%s", want, ofdXML)
		}
	}
}

// TestConvertPreservesSourceProducer 验证源 PDF 的生产者保留在自定义元数据中，
// 不会被转换工具标识覆盖而丢失。
func TestConvertPreservesSourceProducer(t *testing.T) {
	data, err := os.ReadFile("../../test/testdata/pdf/sample0.pdf")
	if err != nil {
		t.Skipf("测试文件不存在，跳过: %v", err)
	}
	var output bytes.Buffer
	if err := Convert(data, &output); err != nil {
		t.Fatal(err)
	}
	ofdXML := readOFDXML(t, output.Bytes())
	needle := []byte(`<CustomData Name="SourceProducer">`)
	index := bytes.Index(ofdXML, needle)
	if index < 0 {
		t.Fatalf("OFD.xml 缺少 SourceProducer:\n%s", ofdXML)
	}
	if bytes.HasPrefix(ofdXML[index+len(needle):], []byte("</CustomData>")) {
		t.Fatal("SourceProducer 为空")
	}
}

// readOFDXML 从 OFD 包中读取 OFD.xml 内容。
func readOFDXML(t *testing.T, data []byte) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if file.Name != "OFD.xml" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return content
	}
	t.Fatal("OFD 包缺少 OFD.xml")
	return nil
}

// TestPDFFontStyleFlags 验证依据 PDF 字体名推断的粗体/斜体/衬线/等宽标志。
// 非嵌入字体没有字形数据，这些标志用于让阅读器回退到风格一致的系统字体。
func TestPDFFontStyleFlags(t *testing.T) {
	cases := []struct {
		name                            string
		bold, italic, serif, fixedWidth bool
	}{
		{"NimbusRomNo9L-Regu", false, false, true, false},
		{"NimbusRomNo9L-Medi", true, false, true, false},
		{"NimbusRomNo9L-ReguItal", false, true, true, false},
		{"NimbusRomNo9L-MediItal", true, true, true, false},
		{"NimbusRomNo9L-Regu-Slant_167", false, true, true, false},
		{"CMSY8", false, false, true, false},
		{"CMMI9", false, false, true, false},
		{"CMTT9", false, false, false, true},
		{"Courier-Bold", true, false, false, true},
		{"Helvetica", false, false, false, false},
	}
	for _, test := range cases {
		bold, italic, serif, fixedWidth := pdfFontStyleFlags(test.name)
		if bold != test.bold || italic != test.italic || serif != test.serif || fixedWidth != test.fixedWidth {
			t.Fatalf("%s = bold:%v italic:%v serif:%v fixed:%v, want bold:%v italic:%v serif:%v fixed:%v",
				test.name, bold, italic, serif, fixedWidth, test.bold, test.italic, test.serif, test.fixedWidth)
		}
	}
}
