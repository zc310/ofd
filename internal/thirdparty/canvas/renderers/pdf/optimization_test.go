package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/tdewolff/minify/v2"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	ctext "github.com/zc310/ofd/internal/thirdparty/canvas/text"
)

func TestPDFDecimalPreservesLegacyRounding(t *testing.T) {
	values := make([]float64, 0, 2013)
	values = append(values,
		0, math.Copysign(0, -1), .5, -.5, 1.23456789,
		99999999.99999999, math.MaxInt32+1, math.MinInt32-1,
		math.MaxFloat64, math.SmallestNonzeroFloat64,
		math.Inf(1), math.Inf(-1), math.NaN(),
	)
	random := rand.New(rand.NewSource(1))
	for range 2000 {
		values = append(values, math.Float64frombits(random.Uint64()))
	}
	precision := canvas.Precision
	t.Cleanup(func() { canvas.Precision = precision })
	for _, p := range []int{0, 1, 8, 16} {
		canvas.Precision = p
		for _, value := range values {
			// 独立保留原算法，保护 minify 的有效位数与二次舍入语义。
			want := fmt.Sprintf("%.*f", p, value)
			want = string(minify.Decimal([]byte(want), p))
			if value > math.MaxInt32 || value < math.MinInt32 {
				if !strings.Contains(want, ".") {
					want += ".0"
				}
			}
			if got := dec(value).String(); got != want {
				t.Fatalf("precision=%d value=%v: got %q, want %q", p, value, got, want)
			}
		}
	}
}

func TestPDFHeaderRetainsFormatEscapes(t *testing.T) {
	var output bytes.Buffer
	writer := newPDFWriter(&output)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(output.Bytes(), []byte("%PDF-1.7\n%")) {
		t.Fatal("PDF 文件头的百分号不正确")
	}
}

func TestPDFPathScratchPreservesSourceAndBoundsRetention(t *testing.T) {
	renderer := New(io.Discard, 100, 100, nil)
	path := canvas.MustParseSVGPath("M1 2 L3 4 Q5 6 7 8 C9 10 11 12 13 14 A3 4 20 0 1 18 20 Z")
	original := path.Copy()
	for _, matrix := range []canvas.Matrix{
		canvas.Identity, canvas.Identity.Translate(3, 7), canvas.Identity.Scale(2, .5),
	} {
		want := path.Copy().Transform(matrix).ToPDF()
		if got := renderer.pathPDF(path, matrix); got != want {
			t.Fatalf("路径副本复用改变了输出：%q，预期 %q", got, want)
		}
		// 交错处理更短的裁剪路径，防止副本残留前一次的尾部数据。
		renderer.pathPDF(canvas.Rectangle(2, 3), canvas.Identity)
	}
	if !path.Equals(original) {
		t.Fatal("变换修改了调用方路径")
	}
	large := &canvas.Path{}
	for range 33_000 {
		large.MoveTo(1, 2)
		large.LineTo(3, 4)
	}
	renderer.pathPDF(large, canvas.Identity)
	if cap(renderer.pathScratch.Data())*8 > 1<<20 {
		t.Fatal("保留了超过预算的路径副本")
	}
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}
	if len(renderer.pathScratch.Data()) != 0 {
		t.Fatal("Close 未释放路径副本")
	}
}

func TestRGBStreamPreservesPixelsWithLateAlphaAndSubimages(t *testing.T) {
	for _, kind := range []string{"rgba", "nrgba"} {
		for _, firstAlpha := range []int{-1, 0, 17, 34} {
			t.Run(fmt.Sprintf("%s/firstAlpha=%d", kind, firstAlpha), func(t *testing.T) {
				rect := image.Rect(10, 20, 20, 30)
				var source interface {
					image.Image
					Set(x, y int, c color.Color)
					SubImage(r image.Rectangle) image.Image
				}
				if kind == "rgba" {
					source = image.NewRGBA(rect)
				} else {
					source = image.NewNRGBA(rect)
				}
				crop := image.Rect(12, 23, 19, 28)
				for y := crop.Min.Y; y < crop.Max.Y; y++ {
					for x := crop.Min.X; x < crop.Max.X; x++ {
						index := (y-crop.Min.Y)*crop.Dx() + x - crop.Min.X
						alpha := uint8(255)
						if firstAlpha >= 0 && index >= firstAlpha {
							alpha = uint8(index * 7)
						}
						source.Set(x, y, color.NRGBA{R: uint8(index * 13), G: 67, B: 241, A: alpha})
					}
				}
				img := source.SubImage(crop)
				// 隐藏具体类型，使用原始 At/RGBA 运算独立核对每个像素。
				generic := struct{ image.Image }{img}
				wantRGB, wantAlpha, wantHasAlpha := rgbStream(generic, crop.Size())
				rgb, alpha, hasAlpha := rgbStream(img, crop.Size())
				if !bytes.Equal(rgb, wantRGB) || !bytes.Equal(alpha, wantAlpha) || hasAlpha != wantHasAlpha {
					t.Fatal("RGB/alpha 输出与逐像素算法不一致")
				}
				mask, maskHasAlpha := alphaGray(img, crop.Size())
				wantMask, wantMaskHasAlpha := alphaGray(generic, crop.Size())
				if !bytes.Equal(mask.Pix, wantMask.Pix) || maskHasAlpha != wantMaskHasAlpha {
					t.Fatal("alphaGray 改变了透明通道")
				}
				if firstAlpha == -1 {
					allocs := testing.AllocsPerRun(5, func() { _, _, _ = rgbStream(img, crop.Size()) })
					if allocs > 1 {
						t.Fatalf("不透明图片分配了 %g 次，预期仅分配 RGB", allocs)
					}
				}
			})
		}
	}
}

func TestPDFTextByteEscapingPreservesEveryByte(t *testing.T) {
	source := make([]byte, 0, 256)
	var encoded []byte
	for i := range 256 {
		source = append(source, byte(i))
		encoded = appendPDFTextByte(encoded, byte(i))
	}
	want := string(source)
	for _, pair := range [][2]string{
		{`\`, `\\`}, {"(", `\(`}, {")", `\)`},
		{"\n", `\n`}, {"\r", `\r`}, {"\t", `\t`}, {"\b", `\b`}, {"\f", `\f`},
	} {
		want = strings.ReplaceAll(want, pair[0], pair[1])
	}
	if string(encoded) != want {
		t.Fatal("文字编码改变了控制字符、定界符或普通字节")
	}
}

func TestPDFWriteTextPreservesCodesAndLimitsScratchRetention(t *testing.T) {
	family := canvas.NewFontFamily("encoding")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	font := family.Face(10, canvas.Black).Font
	renderer := New(io.Discard, 100, 100, nil)
	page := renderer.w
	page.StartTextObject()
	page.SetFont(font, 10, ctext.LeftToRight)
	subset := page.pdf.fontSubset[font]
	glyphID := font.GlyphIndex('A')
	for i := 1; i <= 256; i++ {
		subset.Get(glyphID, strconv.Itoa(i))
	}
	glyphs := make([]ctext.Glyph, 0, 11)
	for _, code := range []int{1, 8, 9, 10, 12, 13, 40, 41, 92, 255, 256} {
		glyphs = append(glyphs, ctext.Glyph{
			ID: glyphID, Text: strconv.Itoa(code), XAdvance: int32(font.GlyphAdvance(glyphID)),
		})
	}
	start := page.Len()
	page.WriteText(canvas.HorizontalTB, glyphs, 1)
	want := "[(\x00\x01\x00\\b\x00\\t\x00\\n\x00\\f\x00\\r\x00\\(\x00\\)\x00\\\\\x00\xff\x01\x00) -100]TJ"
	if got := string(page.Bytes()[start:]); got != want {
		t.Fatalf("文字操作符=%q，预期 %q", got, want)
	}
	// 超长段落不能让整个文档持续持有同等大小的编码缓冲区。
	long := make([]ctext.Glyph, 40_000)
	for i := range long {
		long[i] = glyphs[0]
	}
	page.WriteText(canvas.HorizontalTB, long)
	if cap(page.pdf.textBuf) > 64<<10 {
		t.Fatal("保留了超过预算的文字缓冲区")
	}
	page.EndTextObject()
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}
	if page.pdf.textBuf != nil {
		t.Fatal("Close 未释放文字缓冲区")
	}
}
