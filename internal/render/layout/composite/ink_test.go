package composite

import (
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

// y.ofd 的复合单元每个只画一行字，声明坐标系却是整页。包围盒收缩必须
// 让离屏像素降到原来的极小比例，否则离屏预算会在每页画满约 26 个单元后
// 耗尽，其余单元被静默丢弃。
func TestCompositeInkRectShrinksPageSizedUnit(t *testing.T) {
	const unitW, unitH = 578.1247, 748.3031
	unit := &spec.CompositeGraphicUnit{
		Content: spec.CTPageBlock{
			Items: []spec.PageItem{{
				Kind: spec.PageItemText,
				Text: &spec.TextObject{
					CtText: spec.CtText{CTGraphicUnit: spec.CTGraphicUnit{
						CTM:      &spec.CTM{9, 0, 0, 9, 0, 0},
						Boundary: spec.StBox{X: 92.6, Y: 712.4, Width: 143.0, Height: 7.98},
					}},
				},
			}},
		},
	}
	rect, ok := InkRect(unit, unitW, unitH)
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
	rotated := &spec.CTM{0.8, 0.6, -0.6, 0.8, 0, 0}
	cases := []struct {
		name string
		item spec.PageItem
	}{
		{"描边路径", spec.PageItem{Kind: spec.PageItemPath, Path: &spec.PathObject{
			CtPath: spec.CtPath{CTGraphicUnit: spec.CTGraphicUnit{CTM: &spec.CTM{2, 0, 0, 2, 0, 0}, Boundary: spec.StBox{Width: 10, Height: 10}}},
		}}},
		{"描边文字", spec.PageItem{Kind: spec.PageItemText, Text: &spec.TextObject{
			CtText: spec.CtText{Stroke: true, CTGraphicUnit: spec.CTGraphicUnit{Boundary: spec.StBox{Width: 10, Height: 10}}},
		}}},
		{"旋转文字", spec.PageItem{Kind: spec.PageItemText, Text: &spec.TextObject{
			CtText: spec.CtText{CTGraphicUnit: spec.CTGraphicUnit{CTM: rotated, Boundary: spec.StBox{Width: 10, Height: 10}}},
		}}},
		{"嵌套图片", spec.PageItem{Kind: spec.PageItemImage, Image: &spec.ImageObject{
			CtImage: spec.CtImage{CTGraphicUnit: spec.CTGraphicUnit{Boundary: spec.StBox{Width: 10, Height: 10}}},
		}}},
		{"嵌套块", spec.PageItem{Kind: spec.PageItemBlock, Block: &spec.PageBlock{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unit := &spec.CompositeGraphicUnit{
				Content: spec.CTPageBlock{Items: []spec.PageItem{tc.item}},
			}
			if _, ok := InkRect(unit, 500, 700); ok {
				t.Error("应放弃收缩并沿用整幅画布")
			}
		})
	}
}

// 内容占满画布时收缩没有收益，不应改变既有行为。
func TestCompositeInkRectKeepsFullCanvasWhenNoSaving(t *testing.T) {
	unit := &spec.CompositeGraphicUnit{
		Content: spec.CTPageBlock{Items: []spec.PageItem{{
			Kind: spec.PageItemText,
			Text: &spec.TextObject{
				CtText: spec.CtText{CTGraphicUnit: spec.CTGraphicUnit{Boundary: spec.StBox{Width: 100, Height: 100}}},
			},
		}}},
	}
	if _, ok := InkRect(unit, 100, 100); ok {
		t.Error("内容铺满画布时应沿用整幅画布")
	}
}

// 图片由 ctx.RenderImage 配合 imageMatrix 放置，矩阵在内部完成 y 轴翻转，
// 不经过离屏画布坐标系，因此收缩画布无法移动图片。必须放弃收缩，否则
// 图片会被推出画布而丢失。
func TestCompositeInkRectRejectsImageItems(t *testing.T) {
	unit := &spec.CompositeGraphicUnit{
		Content: spec.CTPageBlock{Items: []spec.PageItem{{
			Kind: spec.PageItemImage,
			Image: &spec.ImageObject{
				CtImage: spec.CtImage{CTGraphicUnit: spec.CTGraphicUnit{
					CTM:      &spec.CTM{25.6804, 0, 0, 5.8262, 0, 0},
					Boundary: spec.StBox{X: 48.5571, Y: 715.1494, Width: 25.6804, Height: 5.8262},
				}},
			},
		}}},
	}
	if rect, ok := InkRect(unit, 578.2, 748.3); ok {
		t.Errorf("图片图元不应参与收缩，却返回了 %+v", rect)
	}
}
