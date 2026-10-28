package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	imagelayout "github.com/zc310/ofd/internal/render/layout/image"
)

// compositelayout.InkRect 收缩离屏画布后，离屏图只代表画布的子区域，必须映射回
// Boundary 上对应的子矩形。用几个页面参数核对收缩前后的映射是否一致。
func TestCompositeTightInkMappingMatchesFullCanvas(t *testing.T) {
	cases := []struct {
		name         string
		unitW, unitH float64
		box          models.StBox
		pageHeight   float64
		canvas       models.StBox
		dpmm         float64
	}{
		{
			name: "y.ofd 首行文字", unitW: 578.1247, unitH: 748.3031,
			box:        models.StBox{Width: 204.0008, Height: 264.0013},
			pageHeight: 264.0013,
			canvas:     models.StBox{X: 92.6, Y: 26.4, Width: 143.0, Height: 9.0},
			dpmm:       4.1958,
		},
		{
			name: "偏移边界", unitW: 300, unitH: 200,
			box:        models.StBox{X: 13.5, Y: 7.25, Width: 120, Height: 90},
			pageHeight: 297,
			canvas:     models.StBox{X: 40, Y: 0, Width: 260, Height: 180},
			dpmm:       5,
		},
		{
			name: "贴近画布下沿", unitW: 200, unitH: 400,
			box:        models.StBox{Width: 180, Height: 260},
			pageHeight: 260,
			canvas:     models.StBox{X: 0, Y: 0, Width: 200, Height: 6},
			dpmm:       6,
		},
		{
			name: "贴近画布上沿", unitW: 200, unitH: 400,
			box:        models.StBox{Width: 180, Height: 260},
			pageHeight: 260,
			canvas:     models.StBox{X: 0, Y: 380, Width: 60, Height: 20},
			dpmm:       6,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, h, box, canvasBox := tc.unitW, tc.unitH, tc.box, tc.canvas

			// 整幅画布路径：离屏图覆盖 w×h 毫米。
			fullM := imagelayout.MatrixWH(box, w*tc.dpmm, h*tc.dpmm,
				models.CTM{box.Width, 0, 0, box.Height, 0, 0}, tc.pageHeight)
			// 收缩路径：离屏图只覆盖 canvasBox 子区域，映射到 Boundary 上
			// 对应的子矩形。像素尺寸按实际栅格的整数取整。
			imgW := math.Round(canvasBox.Width * tc.dpmm)
			imgH := math.Round(canvasBox.Height * tc.dpmm)
			drawBox := models.StBox{
				X:      box.X + box.Width*canvasBox.X/w,
				Y:      box.Y + box.Height*(h-canvasBox.Y-canvasBox.Height)/h,
				Width:  box.Width * canvasBox.Width / w,
				Height: box.Height * canvasBox.Height / h,
			}
			tightM := imagelayout.MatrixWH(drawBox, imgW, imgH,
				models.CTM{drawBox.Width, 0, 0, drawBox.Height, 0, 0}, tc.pageHeight)

			// 缩放系数相同，差异只允许来自像素取整（相对误差 < 1/像素数）。
			tolScale := 1.0 / math.Min(imgW, imgH)
			if d := math.Abs(fullM[0][0] - tightM[0][0]); d > tolScale {
				t.Errorf("横向缩放偏差 %g 超过取整容差 %g: full=%v tight=%v", d, tolScale, fullM, tightM)
			}
			if d := math.Abs(fullM[1][1] - tightM[1][1]); d > tolScale {
				t.Errorf("纵向缩放偏差 %g 超过取整容差 %g: full=%v tight=%v", d, tolScale, fullM, tightM)
			}
			// 内容包围盒四角在两种路径下必须落在同一页面位置。
			for _, corner := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				u, v := corner[0], corner[1]
				want := fullM.Dot(geom.Point{
					X: (canvasBox.X + u*canvasBox.Width) * tc.dpmm,
					Y: (canvasBox.Y + v*canvasBox.Height) * tc.dpmm,
				})
				got := tightM.Dot(geom.Point{X: u * imgW, Y: v * imgH})
				if math.Abs(want.X-got.X) > 0.01 || math.Abs(want.Y-got.Y) > 0.01 {
					t.Errorf("包围盒角点 (u=%v, v=%v) 映射不一致: 期望 %v 实际 %v", u, v, want, got)
				}
			}
		})
	}
}

// 收缩画布后若整幅空白，说明包围盒推断与实际绘制范围不符，必须回退整幅
// 画布重新栅格化，不能因为包围盒算错就丢掉内容。
func TestCompositeRasterBlankDetectsEmptyRaster(t *testing.T) {
	if !compositeRasterBlank(nil) {
		t.Error("nil 栅格应视为空白")
	}
	if !compositeRasterBlank(image.NewRGBA(image.Rect(0, 0, 4, 4))) {
		t.Error("全透明栅格应视为空白")
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	if compositeRasterBlank(img) {
		t.Error("含不透明像素的栅格不应视为空白")
	}
}
