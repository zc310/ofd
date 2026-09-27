package render

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// compositeInkRect 收缩离屏画布后，离屏图只代表画布的子区域，必须映射回
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
			fullM := imageMatrixWH(box, w*tc.dpmm, h*tc.dpmm,
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
			tightM := imageMatrixWH(drawBox, imgW, imgH,
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

// y.ofd 的复合单元每个只画一行字，声明坐标系却是整页。包围盒收缩必须
// 让离屏像素降到原来的极小比例，否则离屏预算会在每页画满约 26 个单元后
// 耗尽，其余单元被静默丢弃。
func TestCompositeInkRectShrinksPageSizedUnit(t *testing.T) {
	const unitW, unitH = 578.1247, 748.3031
	var p *Document
	unit := &models.CompositeGraphicUnit{
		Content: models.CTPageBlock{
			Items: []models.PageItem{{
				Kind: models.PageItemText,
				Text: &models.TextObject{
					CtText: models.CtText{CTGraphicUnit: models.CTGraphicUnit{
						CTM:      &models.CTM{9, 0, 0, 9, 0, 0},
						Boundary: models.StBox{X: 92.6, Y: 712.4, Width: 143.0, Height: 7.98},
					}},
				},
			}},
		},
	}
	rect, ok := p.compositeInkRect(unit, unitW, unitH)
	if !ok {
		t.Fatal("单条无描边文字应能推断出内容包围盒")
	}
	full := unitW * unitH
	got := rect.Width * rect.Height
	if ratio := got / full; ratio > 0.01 {
		t.Errorf("包围盒面积占比 %.4f 过大，离屏像素未有效下降", ratio)
	}
	if rect.X < 0 || rect.Y < 0 || rect.X+rect.Width > unitW || rect.Y+rect.Height > unitH {
		t.Errorf("包围盒 %+v 越出画布 %vx%v", rect, unitW, unitH)
	}
	// 文字必须被包围盒完整包含（边距为正，故为严格包含）。
	text := unit.Content.Items[0].Text.Boundary
	if rect.X > text.X || rect.Y > unitH-text.Y-text.Height ||
		rect.X+rect.Width < text.X+text.Width || rect.Y+rect.Height < unitH-text.Y {
		t.Errorf("包围盒 %+v 未包含文字 %+v", rect, text)
	}
}

// 描边、旋转和嵌套内容的可见范围不能由 Boundary 推断，必须放弃收缩。
func TestCompositeInkRectRejectsUnreliableBounds(t *testing.T) {
	rotated := &models.CTM{0.8, 0.6, -0.6, 0.8, 0, 0}
	cases := []struct {
		name string
		item models.PageItem
	}{
		{"描边路径", models.PageItem{Kind: models.PageItemPath, Path: &models.PathObject{
			CtPath: models.CtPath{CTGraphicUnit: models.CTGraphicUnit{CTM: &models.CTM{2, 0, 0, 2, 0, 0}, Boundary: models.StBox{Width: 10, Height: 10}}},
		}}},
		{"描边文字", models.PageItem{Kind: models.PageItemText, Text: &models.TextObject{
			CtText: models.CtText{Stroke: true, CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 10, Height: 10}}},
		}}},
		{"旋转文字", models.PageItem{Kind: models.PageItemText, Text: &models.TextObject{
			CtText: models.CtText{CTGraphicUnit: models.CTGraphicUnit{CTM: rotated, Boundary: models.StBox{Width: 10, Height: 10}}},
		}}},
		{"嵌套图片", models.PageItem{Kind: models.PageItemImage, Image: &models.ImageObject{
			CtImage: models.CtImage{CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 10, Height: 10}}},
		}}},
		{"嵌套块", models.PageItem{Kind: models.PageItemBlock, Block: &models.PageBlock{}}},
	}
	var p *Document
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unit := &models.CompositeGraphicUnit{
				Content: models.CTPageBlock{Items: []models.PageItem{tc.item}},
			}
			if _, ok := p.compositeInkRect(unit, 500, 700); ok {
				t.Error("应放弃收缩并沿用整幅画布")
			}
		})
	}
}

// 内容占满画布时收缩没有收益，不应改变既有行为。
func TestCompositeInkRectKeepsFullCanvasWhenNoSaving(t *testing.T) {
	var p *Document
	unit := &models.CompositeGraphicUnit{
		Content: models.CTPageBlock{Items: []models.PageItem{{
			Kind: models.PageItemText,
			Text: &models.TextObject{
				CtText: models.CtText{CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 100, Height: 100}}},
			},
		}}},
	}
	if _, ok := p.compositeInkRect(unit, 100, 100); ok {
		t.Error("内容铺满画布时应沿用整幅画布")
	}
}

// 图片由 ctx.RenderImage 配合 imageMatrix 放置，矩阵在内部完成 y 轴翻转，
// 不经过离屏画布坐标系，因此收缩画布无法移动图片。必须放弃收缩，否则
// 图片会被推出画布而丢失。
func TestCompositeInkRectRejectsImageItems(t *testing.T) {
	var p *Document
	unit := &models.CompositeGraphicUnit{
		Content: models.CTPageBlock{Items: []models.PageItem{{
			Kind: models.PageItemImage,
			Image: &models.ImageObject{
				CtImage: models.CtImage{CTGraphicUnit: models.CTGraphicUnit{
					CTM:      &models.CTM{25.6804, 0, 0, 5.8262, 0, 0},
					Boundary: models.StBox{X: 48.5571, Y: 715.1494, Width: 25.6804, Height: 5.8262},
				}},
			},
		}}},
	}
	if rect, ok := p.compositeInkRect(unit, 578.2, 748.3); ok {
		t.Errorf("图片图元不应参与收缩，却返回了 %+v", rect)
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
