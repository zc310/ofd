package reader

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

func pdfSizeTestReader(t *testing.T, width, height float64) *Reader {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "pdf-page-size", Pages: []creator.Page{{
		Area: &creator.PageArea{PhysicalBox: &creator.Box{Width: width, Height: height}},
		Items: []creator.Item{creator.Path{
			X: 10, Y: 10, Width: 10, Height: 10, Data: "M 0 0 L 10 10",
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestRenderPDFKeepsPhysicalPageSizeAcrossDPI(t *testing.T) {
	mediaBox := regexp.MustCompile(`/MediaBox\s*\[\s*0\s+0\s+([0-9.]+)\s+([0-9.]+)\s*\]`)
	for _, size := range []struct {
		name          string
		width, height float64
	}{
		{"A4", 210, 297},
		{"A3", 297, 420},
	} {
		t.Run(size.name, func(t *testing.T) {
			r := pdfSizeTestReader(t, size.width, size.height)
			for _, dpi := range []float64{0, 72, 96, 150, 600} {
				t.Run(fmt.Sprintf("DPI=%g", dpi), func(t *testing.T) {
					data, err := renderPDFBytes(t.Context(), r, []int{0}, RenderOptions{DPI: dpi})
					if err != nil {
						t.Fatal(err)
					}
					matches := mediaBox.FindAllSubmatch(data, -1)
					if len(matches) != 1 {
						t.Fatalf("PDF 中应有一个 MediaBox，得到 %d 个", len(matches))
					}
					for i, mm := range []float64{size.width, size.height} {
						got, err := strconv.ParseFloat(string(matches[0][i+1]), 64)
						want := mm * 72 / 25.4
						if err != nil || math.Abs(got-want) > 0.001 {
							t.Errorf("MediaBox 维度 %d=%g pt (%v)，期望 %g pt", i, got, err, want)
						}
					}
				})
			}
			if size.name == "A3" {
				if _, err := renderPageBytes(t.Context(), r, 0, RenderOptions{DPI: 600}); err == nil {
					t.Error("A3 的 600 DPI 位图输出仍应受像素上限限制")
				}
			}
		})
	}
}

func TestRenderPDFPageAreaLimitDoesNotDependOnDPI(t *testing.T) {
	r := pdfSizeTestReader(t, 4000, 4000)
	for _, dpi := range []float64{1, 72, 600} {
		t.Run(fmt.Sprintf("DPI=%g", dpi), func(t *testing.T) {
			if _, err := renderPDFBytes(t.Context(), r, []int{0}, RenderOptions{DPI: dpi}); err == nil {
				t.Fatal("超大物理页面应始终拒绝，不能通过降低 DPI 绕过限制")
			}
		})
	}
}

func TestRenderPageRejectsExtremeAspectRatioBeforeRasterization(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height float64
	}{
		{name: "极宽页面", width: 1e100, height: 1e-100},
		{name: "不足一个像素", width: 1e-10, height: 100},
	} {
		t.Run(size.name, func(t *testing.T) {
			if validRasterDimensions(size.width, size.height, 96) {
				t.Fatal("接受了无法安全栅格化的页面")
			}
		})
	}
}

func TestRenderPDFRejectsExtremeSingleDimension(t *testing.T) {
	if validPDFDimensions(1e100, 1e-100) {
		t.Fatal("PDF 接受了单边尺寸超出限制的页面")
	}
}
