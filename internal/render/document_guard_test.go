package render

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

// drawableGraphicUnit 是文字、路径、图片和复合图元四条绘制入口共用的入参校验。
// 任一项非法都必须判定为不可绘制，否则 NaN 会进入变换矩阵污染整个内容流。
func TestDrawableGraphicUnit(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)
	okCTM := &models.CTM{2, 0, 0, 2, 1, 1}
	badCTM := &models.CTM{nan, 0, 0, 2, 0, 0}
	okBox := models.StBox{X: 1, Y: 2, Width: 3, Height: 4}
	badBox := models.StBox{Width: 3, Height: inf}
	okPage := models.StBox{Width: 210, Height: 297}
	badPage := models.StBox{Width: 210, Height: nan}

	cases := []struct {
		name           string
		visible        bool
		ctm, parentCTM *models.CTM
		boundary, page models.StBox
		want           bool
	}{
		{name: "全部合法", visible: true, ctm: okCTM, parentCTM: nil, boundary: okBox, page: okPage, want: true},
		{name: "父级 CTM 为 nil 视为单位阵", visible: true, ctm: okCTM, parentCTM: nil, boundary: okBox, page: okPage, want: true},
		{name: "对象 CTM 为 nil", visible: true, ctm: nil, parentCTM: okCTM, boundary: okBox, page: okPage, want: true},
		{name: "不可见", visible: false, ctm: okCTM, parentCTM: nil, boundary: okBox, page: okPage, want: false},
		{name: "对象 CTM 含 NaN", visible: true, ctm: badCTM, parentCTM: nil, boundary: okBox, page: okPage, want: false},
		{name: "父级 CTM 含 NaN", visible: true, ctm: okCTM, parentCTM: badCTM, boundary: okBox, page: okPage, want: false},
		{name: "Boundary 含 Inf", visible: true, ctm: okCTM, parentCTM: nil, boundary: badBox, page: okPage, want: false},
		{name: "页面高度含 NaN", visible: true, ctm: okCTM, parentCTM: nil, boundary: okBox, page: badPage, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := drawableGraphicUnit(tc.visible, tc.ctm, tc.parentCTM, tc.boundary, tc.page); got != tc.want {
				t.Errorf("drawableGraphicUnit() = %v, 期望 %v", got, tc.want)
			}
		})
	}
}
