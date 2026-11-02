package converter

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestIntroPage9NativeBoldKeepsCompactPDFAndSearchableDates(t *testing.T) {
	const input = "../../testdata/ofdrw/intro.ofd"
	if _, err := os.Stat(input); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := PDF(t.Context(), input, &output, Page(9)); err != nil {
		t.Fatal(err)
	}
	ctx := readPDFContext(t, output.Bytes())
	reader, err := pdfcpu.ExtractPageContent(t.Context(), ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	// 旧实现把六个日期减细后逐字展开，内容流接近99KB；原生文字约31KB。
	if len(content) > 40_000 {
		t.Fatalf("第9页内容流膨胀：%d字节，日期应保留原生文字而非减细轮廓", len(content))
	}
	t.Run("文字提取", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("缺少pdftotext，跳过PDF文字提取检查")
		}
		cmd := exec.CommandContext(t.Context(), tool, "-", "-")
		cmd.Stdin = bytes.NewReader(output.Bytes())
		text, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("PDF文字提取失败：%v %s", err, text)
		}
		for _, date := range []string{"2016.07", "2019.01", "2019.04", "2019.03", "2018.09"} {
			want := 1
			if date == "2018.09" {
				want = 2
			}
			if got := strings.Count(string(text), date); got != want {
				t.Errorf("日期%s出现%d次，期望%d次", date, got, want)
			}
		}
	})
	t.Run("PNG与PDF字重", func(t *testing.T) {
		const dpi = 144
		tool, err := exec.LookPath("pdftoppm")
		if err != nil {
			t.Skip("缺少pdftoppm，跳过PDF像素检查")
		}
		var raster bytes.Buffer
		if err := Image(t.Context(), input, Page(9), DPI(dpi), PNG(), Writer(func(int) (io.WriteCloser, error) {
			return nopWriteCloser{&raster}, nil
		})); err != nil {
			t.Fatal(err)
		}
		pngImage, err := png.Decode(&raster)
		if err != nil {
			t.Fatal(err)
		}
		pdfImages, err := testutil.RasterizePDFPages(t.Context(), tool, output.Bytes(), dpi)
		if err != nil {
			t.Fatal(err)
		}
		// 日期区域是黑底白字，用亮度积分比较原生字形的用墨量，允许抗锯齿差异。
		for _, x := range []float64{20, 69, 118, 167, 216, 265} {
			box := image.Rect(int(x*dpi/25.4), int(math.Floor(107*dpi/25.4)), int(math.Ceil((x+22)*dpi/25.4)), int(math.Ceil(112*dpi/25.4)))
			pngInk, pdfInk := nativeDateInk(pngImage, box), nativeDateInk(pdfImages[0], box)
			ratio := float64(pdfInk) / float64(pngInk)
			if pngInk == 0 || ratio < 0.85 || ratio > 1.15 {
				t.Errorf("日期x=%g的PNG/PDF字重不一致：PNG=%d PDF=%d 比值=%g", x, pngInk, pdfInk, ratio)
			}
		}
	})
}

func nativeDateInk(img image.Image, box image.Rectangle) uint64 {
	var ink uint64
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			ink += uint64(r>>8) + uint64(g>>8) + uint64(b>>8)
		}
	}
	return ink
}

func TestIntroPage26ClippedWatermarkKeepsNativeCurves(t *testing.T) {
	if _, ok := any(canvas.New(1, 1)).(interface {
		RenderClippedPath(path *canvas.Path, style canvas.Style, matrix canvas.Matrix, clip *canvas.Path)
	}); !ok {
		t.Skip("当前canvas依赖尚未支持原生路径裁剪")
	}
	const input = "../../testdata/ofdrw/intro.ofd"
	if _, err := os.Stat(input); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := PDF(t.Context(), input, &output, Page(26)); err != nil {
		t.Fatal(err)
	}
	ctx := readPDFContext(t, output.Bytes())
	reader, err := pdfcpu.ExtractPageContent(t.Context(), ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	// 676号紫色水印确实越出页底，必须保留裁剪；展开后整页约67KB。
	if len(content) > 40_000 {
		t.Fatalf("第26页内容流膨胀：%d字节，越界水印应保留原生曲线及裁剪", len(content))
	}
	start := bytes.Index(content, []byte(".55686275 .3372549 .60784314 RG"))
	if start < 0 {
		t.Fatal("没有找到紫色水印的原生描边")
	}
	prefix := content[:start]
	q := bytes.LastIndex(prefix, []byte(" q"))
	if q < 0 || !bytes.Contains(prefix[q:], []byte(" W n ")) {
		t.Fatal("紫色水印的越界描边丢失原生裁剪")
	}
	end := bytes.Index(content[start:], []byte(" Q"))
	if end < 0 || bytes.Count(content[start:start+end], []byte(" c")) != 224 {
		t.Fatal("紫色水印的224段原始曲线没有完整保留")
	}
}

func TestIntroPage12CoveredDemoKeepsCompactNativeStroke(t *testing.T) {
	const input = "../../testdata/ofdrw/intro.ofd"
	if _, err := os.Stat(input); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := PDF(t.Context(), input, &output, Page(12)); err != nil {
		t.Fatal(err)
	}
	ctx := readPDFContext(t, output.Bytes())
	reader, err := pdfcpu.ExtractPageContent(t.Context(), ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	// DEMO被照片遮住，单看整页像素无法发现其描边被无谓展开。
	// 原生曲线约15KB，旧实现的展开轮廓使内容流超过35KB。
	if len(content) > 18_000 {
		t.Fatalf("第12页内容流膨胀：%d字节，完整覆盖的DEMO应保留原生描边", len(content))
	}
}

func TestIntroPage7SharesGradientSpaceWithoutPerGlyphPatterns(t *testing.T) {
	if _, ok := any(canvas.New(1, 1)).(interface {
		RenderTextWithPaint(text *canvas.Text, matrix canvas.Matrix, clip *canvas.Path, paint canvas.Paint, paintMatrix canvas.Matrix) bool
	}); !ok {
		t.Skip("当前canvas依赖尚未支持独立文字画笔坐标")
	}
	const input = "../../testdata/ofdrw/intro.ofd"
	if _, err := os.Stat(input); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := PDF(t.Context(), input, &output, Page(7)); err != nil {
		t.Fatal(err)
	}
	ctx := readPDFContext(t, output.Bytes())
	page, _, _, err := ctx.PageDict(t.Context(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := ctx.DereferenceDict(page["Resources"])
	if err != nil {
		t.Fatal(err)
	}
	patterns, err := ctx.DereferenceDict(resources["Pattern"])
	if err != nil {
		t.Fatal(err)
	}
	// 源页有16个渐变文字对象，不能因逐字定位膨胀为268个资源。
	if len(patterns) == 0 || len(patterns) > 16 {
		t.Fatalf("第7页渐变资源数=%d，期望不超过16个", len(patterns))
	}
}
