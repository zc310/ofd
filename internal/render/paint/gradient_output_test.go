package paint

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// TestNewOFDRadialGradientNativeSafety 锁定哪些几何可以交给原生矢量路径。
//
// PDF 与 SVG 的径向着色由阅读器按自己的规则求值，绕开 geom.RadialParameter。
// 两圆外离或相交时 a = |C1-C0|²-(r1-r0)² > 0，插值族向外发散：既会在两圆之间
// 留下判别式为负的空隙（原生只能夹取端点色、无法留空），又会让 t>1 的延伸根
// 压过 t<=1 的扫描根。这两点都必须在采样路径上求值，因此这些几何不能走原生。
// 内含与同心几何 a<=0 时原生等价，保持矢量输出。
func TestNewOFDRadialGradientNativeSafety(t *testing.T) {
	red := models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 230, G: 50, A: 255}}}
	blue := models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 230, A: 255}}}
	tests := []struct {
		name       string
		c0x, c0y   float64
		r0         float64
		c1x, c1y   float64
		r1         float64
		extend     int
		wantNative bool
	}{
		{"两圆外离 Extend=3 需采样", 18, 35, 8, 52, 35, 18, 3, false},
		{"两圆相交 Extend=3 需采样", 12, 35, 6, 24, 35, 12, 3, false},
		{"焦点族 Extend=3 需采样", 0, 0, 0, 30, 0, 10, 3, false},
		{"两圆内含 Extend=3 原生", 18, 35, 6, 25, 35, 14, 3, true},
		{"同心 Extend=3 增长 原生", 0, 0, 10, 0, 0, 20, 3, true},
		{"同心 Extend=3 收缩 原生", 0, 0, 20, 0, 0, 10, 3, true},
		{"两圆外离 Extend=0 走采样", 18, 35, 8, 52, 35, 18, 0, false},
		{"两圆内含 Extend=1 走采样", 18, 35, 6, 25, 35, 14, 1, false},
		{"两圆内含 Extend=2 走采样", 18, 35, 6, 25, 35, 14, 2, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewRadialGradient(&models.CTRadialShd{
				StartPoint:  models.StPos{X: tc.c0x, Y: tc.c0y},
				StartRadius: tc.r0,
				EndPoint:    models.StPos{X: tc.c1x, Y: tc.c1y},
				EndRadius:   tc.r1,
				Extend:      tc.extend,
				Segment:     []models.Segment{{Color: red}, {Color: blue}},
			}, identityGradientTransform, nil)
			if g == nil {
				t.Fatal("NewRadialGradient 返回 nil")
			}
			_, isNative := g.(*geom.RadialGradient)
			if isNative != tc.wantNative {
				t.Errorf("走原生矢量路径 = %v，期望 %v（实际类型 %T）", isNative, tc.wantNative, g)
			}
		})
	}
}

// TestNeedsRasterGradientHandlesTransparency 锁定半透明渐变必须走栅格化：
// canvas 的 PDF 写入器把渐变写成 Pattern/Shading 时只写 DeviceRGB，丢弃色标
// Alpha，只有栅格化后再按图片嵌入才能保住透明度。
func TestNeedsRasterGradientHandlesTransparency(t *testing.T) {
	opaque := geom.Grad{
		{Offset: 0, Color: color.RGBA{R: 255, A: 255}},
		{Offset: 1, Color: color.RGBA{B: 255, A: 255}},
	}
	transparentStop := geom.Grad{
		{Offset: 0, Color: color.RGBA{R: 255, A: 255}},
		{Offset: 1, Color: color.RGBA{B: 255, A: 128}},
	}
	half := uint8(128)
	linear := func(stops geom.Grad) *geom.LinearGradient {
		g := geom.NewLinearGradient(geom.Point{}, geom.Point{X: 100})
		g.Grad = stops
		return g
	}

	tests := []struct {
		name   string
		fill   geom.Gradient
		alpha  *uint8
		expect bool
	}{
		{"不透明渐变无对象 Alpha 走原生", linear(opaque), nil, false},
		{"不透明渐变带对象 Alpha 需栅格化", linear(opaque), &half, true},
		{"半透明色标需栅格化", linear(transparentStop), nil, true},
		{"OFD 采样渐变始终栅格化", &ofdLinearGradient{}, nil, true},
		{"nil 渐变不处理", nil, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsRaster(tc.fill, tc.alpha); got != tc.expect {
				t.Fatalf("NeedsRaster = %v，期望 %v", got, tc.expect)
			}
		})
	}
}

func TestGradientHasTransparency(t *testing.T) {
	opaque := geom.Grad{{Offset: 0, Color: color.RGBA{A: 255}}, {Offset: 1, Color: color.RGBA{A: 255}}}
	if gradientHasTransparency(&ofdLinearGradient{stops: opaque}) {
		t.Fatal("全不透明色标被判定为透明")
	}
	translucent := geom.Grad{{Offset: 0, Color: color.RGBA{A: 255}}, {Offset: 1, Color: color.RGBA{A: 0}}}
	if !gradientHasTransparency(&ofdLinearGradient{stops: translucent}) {
		t.Fatal("含 A=0 色标的渐变未被判定为透明")
	}
}
