package render

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
			g := newOFDRadialGradient(&models.CTRadialShd{
				StartPoint:  models.StPos{X: tc.c0x, Y: tc.c0y},
				StartRadius: tc.r0,
				EndPoint:    models.StPos{X: tc.c1x, Y: tc.c1y},
				EndRadius:   tc.r1,
				Extend:      tc.extend,
				Segment:     []models.Segment{{Color: red}, {Color: blue}},
			}, identityGradientTransform, nil)
			if g == nil {
				t.Fatal("newOFDRadialGradient 返回 nil")
			}
			_, isNative := g.(*geom.RadialGradient)
			if isNative != tc.wantNative {
				t.Errorf("走原生矢量路径 = %v，期望 %v（实际类型 %T）", isNative, tc.wantNative, g)
			}
		})
	}
}
