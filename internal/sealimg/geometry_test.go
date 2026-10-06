package sealimg

import (
	"math"
	"testing"
)

// TestArcLengthCircleIsAnalytic 圆上的弧长必须等于解析解 r·Δt。
//
// 这是整套弧形排版的基准：圆弧走的是解析分支，椭圆走数值积分，两者的一致性靠
// 这个等价关系保证。解析解一旦被破坏，字会沿弧线均匀铺开——而均匀铺开正是
// "按角度均分"的错误做法在圆上的伪装，两者看起来一样。
func TestArcLengthCircleIsAnalytic(t *testing.T) {
	const r = 100.0
	cases := []struct{ t0, t1 float64 }{
		{0, math.Pi / 2},
		{-math.Pi / 2, math.Pi / 2},
		{0, -math.Pi / 3},
		{1.2, -0.4},
	}
	for _, tc := range cases {
		got := arcLength(r, r, tc.t0, tc.t1)
		want := r * (tc.t1 - tc.t0)
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("arcLength(圆, %v, %v) = %v, 期望 %v", tc.t0, tc.t1, got, want)
		}
	}
	if got := arcLength(r, r, 0.5, 0.5); got != 0 {
		t.Errorf("零长度弧应为 0，实际 %v", got)
	}
}

// TestArcLengthEllipseIsMonotonic 椭圆弧长必须随参数单调递增，且不小于短半轴乘角度。
//
// 单调性是二分反解的前提；下界用来确认积分没有少算——扁平椭圆上按短半轴估
// 的上界不够会直接导致二分区间不覆盖目标值。
func TestArcLengthEllipseIsMonotonic(t *testing.T) {
	const a, b = 200.0, 60.0
	previous := arcLength(a, b, 0, 0)
	for step := 1; step <= 40; step++ {
		current := arcLength(a, b, 0, float64(step)*0.1)
		if current <= previous {
			t.Fatalf("弧长应单调递增：step=%d 时 %v <= %v", step, current, previous)
		}
		previous = current
	}
	full := arcLength(a, b, 0, 2*math.Pi)
	// 椭圆周长介于 2·π·b 与 2·π·a 之间（严格大于内接圆周长）。
	if full <= 2*math.Pi*b || full >= 2*math.Pi*a {
		t.Errorf("椭圆周长 %v 应落在 (%.1f, %.1f) 之间", full, 2*math.Pi*b, 2*math.Pi*a)
	}
}

// TestParamAtArcLengthInverse 弧长与参数必须互为反函数。
//
// 字的位置完全由"给定弧长求参数"决定；反函数不闭合，字就会沿弧线漂移，而且
// 漂移量随字号放大，肉眼很难看出是这里错了。
func TestParamAtArcLengthInverse(t *testing.T) {
	cases := []struct{ a, b float64 }{{100, 100}, {200, 60}, {60, 200}, {150, 90}}
	for _, c := range cases {
		const t0 = -1.1
		for _, want := range []float64{0, 1, 12.5, 60, 200, -30, -150} {
			param := paramAtArcLength(c.a, c.b, t0, want)
			if got := arcLength(c.a, c.b, t0, param); math.Abs(got-want) > 1e-6*math.Max(1, math.Abs(want)) {
				t.Errorf("椭圆(%.0f,%.0f): 弧长 %v 反解参数得 %.6f，正算得 %v",
					c.a, c.b, want, param, got)
			}
		}
	}
}

// TestArcTangentDirection 切线角必须随排布方向取反。
//
// 顶部文字若沿参数增大方向排布，字基朝左，整行字会左右镜像——单个字仍然是合法
// 的汉字，只看一个字发现不了，必须成行比对。
func TestArcTangentDirection(t *testing.T) {
	const a, b = 150.0, 90.0
	for _, base := range []float64{arcTop, arcBottom} {
		forward := arcTangent(a, b, base, 1)
		backward := arcTangent(a, b, base, -1)
		diff := math.Mod(math.Abs(forward-backward), 360)
		if math.Abs(diff-180) > 1e-9 {
			t.Errorf("base=%.4f: 正反切线角应相差 180°，实际 %.2f° / %.2f°", base, forward, backward)
		}
	}
	// 顶部沿 -t 排布时字基水平（0°），字才是正的。
	if deg := arcTangent(150, 150, arcTop, -1); math.Abs(deg) > 1e-9 {
		t.Errorf("顶部反向排布的切线角应为 0°，实际 %.4f°", deg)
	}
}

// TestArcPointOnAxes 弧上基准点必须落在上下两端。
func TestArcPointOnAxes(t *testing.T) {
	const cx, cy, r = 200.0, 150.0, 80.0
	x, y := arcPoint(cx, cy, r, r, arcTop)
	if math.Abs(x-cx) > 1e-9 || math.Abs(y-(cy+r)) > 1e-9 {
		t.Errorf("arcTop 应在 (%.1f, %.1f)，实际 (%.1f, %.1f)", cx, cy+r, x, y)
	}
	x, y = arcPoint(cx, cy, r, r, arcBottom)
	if math.Abs(x-cx) > 1e-9 || math.Abs(y-(cy-r)) > 1e-9 {
		t.Errorf("arcBottom 应在 (%.1f, %.1f)，实际 (%.1f, %.1f)", cx, cy-r, x, y)
	}
}

// TestSealLayoutProportions 外框与文字弧的比例必须与形状无关。
func TestSealLayoutProportions(t *testing.T) {
	circle := newSealLayout(512, 512)
	if circle.borderRX != circle.borderRY {
		t.Errorf("正圆的横纵半轴应相等，实际 %.2f / %.2f", circle.borderRX, circle.borderRY)
	}
	ellipse := newSealLayout(660, 420)
	if ellipse.borderRX <= ellipse.borderRY {
		t.Errorf("横向椭圆应有更大的横轴半轴，实际 %.2f / %.2f", ellipse.borderRX, ellipse.borderRY)
	}
	if ellipse.borderRX != 660/2*borderAxisRatio {
		t.Errorf("横轴半轴 %.2f 与比例不符", ellipse.borderRX)
	}
	for _, layout := range []sealLayout{circle, ellipse} {
		if layout.textRX >= layout.borderRX || layout.textRY >= layout.borderRY {
			t.Error("文字弧应在外框之内")
		}
	}
}

func TestTopTextLayoutKeepsShapeSpecificRadii(t *testing.T) {
	circle := topTextLayout(newSealLayout(512, 512), ShapeCircle)
	if circle.textRX != circle.borderRX*topTextRadiusRatio || circle.textRY != circle.borderRY*topTextRadiusRatio {
		t.Errorf("圆形顶部文字半径未保持圆章比例：text=(%.2f,%.2f)", circle.textRX, circle.textRY)
	}

	ellipse := topTextLayout(newSealLayout(660, 440), ShapeEllipse)
	gap := ellipse.shortSide * ellipseTopTextGapRatio
	if ellipse.textRX != ellipse.borderRX*innerBorderRatio-gap || ellipse.textRY != ellipse.borderRY*innerBorderRatio-gap {
		t.Errorf("椭圆顶部文字未保持统一内缩距离：text=(%.2f,%.2f)", ellipse.textRX, ellipse.textRY)
	}
}

func TestArcTextSpanIncludesSpacing(t *testing.T) {
	if got := arcTextSpan([]float64{10, 20, 30}, 4); got != 68 {
		t.Errorf("弧形文字总宽度 = %.2f，期望 68", got)
	}
	if got := arcTextSpan([]float64{10}, 4); got != 10 {
		t.Errorf("单字弧形文字总宽度 = %.2f，期望 10", got)
	}
}
