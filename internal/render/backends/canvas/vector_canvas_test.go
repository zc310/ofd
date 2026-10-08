package canvas

import (
	"testing"

	"github.com/zc310/ofd/internal/render/drawing"
)

// destBranch 复现 canvas 依据矩形分量组合反推目的地类型的规则，用来断言
// anchorRect 产出的是哪一类目的地。
func destBranch(rect [4]float64) string {
	x0, y0, x1, y1 := rect[0], rect[1], rect[2], rect[3]
	switch {
	case x0 == 0 && x1 == 0 && y0 == 0 && y1 == 0:
		return "Fit"
	case x0 == 0 && x1 == 0 && y0 == y1:
		return "FitH"
	case y0 == 0 && y1 == 0 && x0 == x1:
		return "FitV"
	case x0 == x1 || y0 == y1:
		return "XYZ"
	default:
		return "FitR"
	}
}

func branchOf(target drawing.LinkTarget) string {
	rect := anchorRect(target)
	return destBranch([4]float64{rect.X0, rect.Y0, rect.X1, rect.Y1})
}

// TestAnchorRectProducesMatchingDestType 守住每种 OFD 跳转类型都能产出对应的 PDF
// 目的地类型。canvas 不接受显式类型，只按矩形分量反推，因此矩形形状本身就是契约：
// 喂错形状会让跳转落到别的地方，而不是报错。
func TestAnchorRectProducesMatchingDestType(t *testing.T) {
	const height = 297.0
	cases := []struct {
		name   string
		target drawing.LinkTarget
		want   string
	}{
		{"整页适配", drawing.LinkTarget{Type: drawing.DestFit}, "Fit"},
		{"高度适配顶端", drawing.LinkTarget{Type: drawing.DestFitH, Top: 0}, "FitH"},
		{"高度适配中部", drawing.LinkTarget{Type: drawing.DestFitH, Top: 100}, "FitH"},
		{"宽度适配", drawing.LinkTarget{Type: drawing.DestFitV, Left: 50}, "FitV"},
		{"定位到点", drawing.LinkTarget{Type: drawing.DestXYZ, Left: 50, Top: 100}, "XYZ"},
		{"缩放到矩形", drawing.LinkTarget{Type: drawing.DestFitR, Left: 10, Top: 20, Right: 110, Bottom: 60}, "FitR"},
	}
	for _, c := range cases {
		c.target.PageHeight = height
		if got := branchOf(c.target); got != c.want {
			t.Errorf("%s: 目的地类型 = %s，期望 %s", c.name, got, c.want)
		}
	}
}

// TestAnchorRectFlipsVerticalAxis 守住纵向坐标按目标页高翻转。OFD 原点在左上角且
// y 向下，PDF 在左下角且 y 向上，不翻转会让跳转落到镜像位置。
func TestAnchorRectFlipsVerticalAxis(t *testing.T) {
	// 距页顶 100mm 处，在 297mm 高的页面上距页底 197mm。
	rect := anchorRect(drawing.LinkTarget{Type: drawing.DestXYZ, Left: 50, Top: 100, PageHeight: 297})
	if rect.X0 != 50 || rect.Y0 != 197 || rect.X1 != 50 || rect.Y1 != 197 {
		t.Fatalf("翻转后的矩形 = %+v，期望 x=50 y=197", rect)
	}
	// 缩放到矩形时四条边都要翻转。
	rect = anchorRect(drawing.LinkTarget{
		Type: drawing.DestFitR, Left: 10, Top: 20, Right: 110, Bottom: 60, PageHeight: 297,
	})
	if rect.X0 != 10 || rect.Y0 != 297-60 || rect.X1 != 110 || rect.Y1 != 297-20 {
		t.Fatalf("FitR 矩形 = %+v", rect)
	}
}

// TestAnchorRectKnownPrecisionLosses 锁定两处已知的精度损失，防止无意间把行为
// 改掉却没人察觉：canvas 用矩形反推目的地类型，x 为 0 的 XYZ 无法与 FitH 区分，
// x 为 0 的 FitV 无法与 Fit 区分。这两处的落点仍正确，只是窗口缩放行为不同。
func TestAnchorRectKnownPrecisionLosses(t *testing.T) {
	if got := branchOf(drawing.LinkTarget{Type: drawing.DestXYZ, Left: 0, Top: 100, PageHeight: 297}); got != "FitH" {
		t.Errorf("Left 为 0 的 XYZ 现在产出 %s，若已修复请更新本用例与文档", got)
	}
	if got := branchOf(drawing.LinkTarget{Type: drawing.DestFitV, Left: 0, PageHeight: 297}); got != "Fit" {
		t.Errorf("Left 为 0 的 FitV 现在产出 %s，若已修复请更新本用例与文档", got)
	}
}
