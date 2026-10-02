package render

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

// TestTextLayoutSizeFollowsCTMScale 锁定 TextLayout.Size 是实际绘制字号而非对象
// 上声明的原始 Size。
//
// OFD 允许用很小的 Size 配一个放大倍数的 CTM 来排大标题：test/testdata/intro.ofd
// 第 14 页就是 Size="1" 配 CTM="9.8778 0 0 9.8778 0 0"，画出 9.88mm 的字。
// buildTextLayout 早先把修正只用在 Height 上，Size 原样返回，消费方（HTML 文字层
// 与 pkg/webreader）按它换算字号就会小近 10 倍——文字层缩成一条 1mm 的细线。
func TestTextLayoutSizeFollowsCTMScale(t *testing.T) {
	const declaredSize = 1.0
	const scale = 9.8778

	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{
			Boundary: models.StBox{X: 20, Y: 20.6161, Width: 40.0445, Height: 9.1666},
			CTM:      &models.CTM{9.8778, 0, 0, 9.8778, 0, 0},
		},
		Size: declaredSize,
		TextCode: []models.TextCode{{
			DeltaX: models.StArrayF{0.995, 0.995, 0.995},
			Value:  "人脸检测",
		}},
	}}

	layout := buildTextLayout(nil, object, object.TextCode[0], 0)

	want := declaredSize * scale
	if math.Abs(layout.Size-want) > 1e-6 {
		t.Errorf("Size = %v，期望 %v（Size %v 乘 CTM.YScale %v）",
			layout.Size, want, declaredSize, scale)
	}
	// Height 早已应用同一系数，两者应当一致，否则字号与行高会脱节。
	if math.Abs(layout.Height-layout.Size) > 1e-6 {
		t.Errorf("Height = %v 与 Size = %v 不一致，CSS 行高会与字号脱节",
			layout.Height, layout.Size)
	}
}

// TestTextLayoutSizeWithoutCTM 确认没有 CTM 时 Size 就是声明值。
func TestTextLayoutSizeWithoutCTM(t *testing.T) {
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{
			Boundary: models.StBox{X: 10, Y: 10, Width: 30, Height: 6},
		},
		Size:     4.5,
		TextCode: []models.TextCode{{Value: "无 CTM"}},
	}}
	layout := buildTextLayout(nil, object, object.TextCode[0], 0)
	if math.Abs(layout.Size-4.5) > 1e-9 {
		t.Errorf("Size = %v，期望 4.5", layout.Size)
	}
}
