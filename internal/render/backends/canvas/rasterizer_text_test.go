package canvas

import (
	"bytes"
	"fmt"
	"image/color"
	"testing"

	"github.com/zc310/canvas"
)

// 裁剪不能改变文字的设备像素对齐；同时核对跨字形裁剪、裁剪孔洞和共享轮廓。
func TestClippedTextRasterKeepsPixelAlignmentAndExactClip(t *testing.T) {
	family := pdfTextTestFamily(t)
	for _, dpi := range []float64{72, 96, 150, 300} {
		t.Run(fmt.Sprintf("DPI=%g", dpi), func(t *testing.T) {
			resolution := canvas.DPI(dpi)
			dpmm := resolution.DPMM()
			const width, height = 200, 100
			line := canvas.NewTextLine(family.Face(18*72/dpi, canvas.Black), "OOOO", canvas.Left)
			matrix := canvas.Identity.Translate(5.37/dpmm, 35.13/dpmm)
			plain := canvas.New(width/dpmm, height/dpmm)
			plain.RenderText(line, matrix)
			want := rasterize(plain, resolution, canvas.DefaultColorSpace)
			for _, kind := range []string{"完整覆盖", "部分裁剪", "裁剪孔洞"} {
				t.Run(kind, func(t *testing.T) {
					clip := canvas.Rectangle(width/dpmm, height/dpmm)
					switch kind {
					case "部分裁剪":
						clip = canvas.Rectangle(40/dpmm, height/dpmm)
					case "裁剪孔洞":
						clip = clip.Append(canvas.Rectangle(10/dpmm, 25/dpmm).Translate(10/dpmm, 35/dpmm)).Settle(canvas.EvenOdd)
					}
					before := clip.ToSVG()
					page := canvas.New(width/dpmm, height/dpmm)
					page.RenderClippedText(line, matrix, clip.Copy().Transform(matrix.Inv()))
					got := rasterize(page, resolution, canvas.DefaultColorSpace)
					ink, removed := 0, 0
					for y := range height {
						for x := range width {
							expected := want.RGBAAt(x, y)
							outside := kind == "部分裁剪" && x >= 40 || kind == "裁剪孔洞" && x >= 10 && x < 20 && height-1-y >= 35 && height-1-y < 60
							if outside {
								removed += int(expected.A)
								expected = color.RGBA{}
							}
							ink += int(expected.A)
							pixel := got.RGBAAt(x, y)
							if (kind == "完整覆盖" || outside) && pixel != expected {
								t.Fatalf("裁剪改变了像素对齐或边界：(%d,%d)=%v，期望%v", x, y, pixel, expected)
							}
							// 曲线求交会重新细分抗锯齿边缘，只对一致的 3×3 实心/空白区域
							// 核对覆盖率，仍可发现基线移动、丢字和字形孔洞被填实。
							if !outside && (expected.A == 0 || expected.A == 255) && x > 0 && x+1 < width && y > 0 && y+1 < height {
								solid := true
								for yy := y - 1; yy <= y+1; yy++ {
									for xx := x - 1; xx <= x+1; xx++ {
										solid = solid && want.RGBAAt(xx, yy).A == expected.A
									}
								}
								if solid && pixel != expected {
									t.Fatalf("裁剪改变了字形实心区域：(%d,%d)=%v，期望%v", x, y, pixel, expected)
								}
							}
						}
					}
					if ink < 255*50 || kind != "完整覆盖" && removed < 255*10 {
						t.Fatalf("样例未覆盖可见文字与裁剪：ink=%d removed=%d", ink, removed)
					}
					if clip.ToSVG() != before {
						t.Fatal("栅格化改写了共享裁剪路径")
					}
				})
			}
			if after := rasterize(plain, resolution, canvas.DefaultColorSpace); !bytes.Equal(after.Pix, want.Pix) {
				t.Fatal("裁剪改写了共享字形轮廓")
			}
		})
	}
}
