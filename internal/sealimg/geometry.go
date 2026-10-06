package sealimg

import "math"

// 弧形文字的基准角。
//
// canvas 的坐标系是 y 向上（实测：画布 y=20 的矩形落在图像第 360 行），参数角
// 沿逆时针增大，因此正上方是 +π/2、正下方是 -π/2。写反的话章会上下颠倒而且
// 字全是反的，肉眼很容易漏过——因为单看一个字符仍然是个合法的字。
const (
	arcTop    = math.Pi / 2
	arcBottom = -math.Pi / 2
)

// sealLayout 是一次渲染用到的几何量，全部以画布坐标（像素）表示。
type sealLayout struct {
	width, height float64
	cx, cy        float64
	// borderRX/borderRY 是外框半轴；圆形时两者相等。
	borderRX, borderRY float64
	// textRX/textRY 是文字弧半轴，取内外框之间。
	textRX, textRY float64
	shortSide      float64
}

// newSealLayout 由画布尺寸推出全部几何量。
//
// 外框半轴取「该方向半宽 × borderAxisRatio」，而不是统一按短边算：椭圆印章
// 的长轴也要按同样比例留白，否则 W 远大于 H 时外框会偏向一侧。
func newSealLayout(width, height float64) sealLayout {
	short := math.Min(width, height)
	borderRX := width / 2 * borderAxisRatio
	borderRY := height / 2 * borderAxisRatio
	return sealLayout{
		width:     width,
		height:    height,
		cx:        width / 2,
		cy:        height / 2,
		borderRX:  borderRX,
		borderRY:  borderRY,
		textRX:    borderRX * textRadiusRatio,
		textRY:    borderRY * textRadiusRatio,
		shortSide: short,
	}
}

// arcPoint 返回椭圆弧上参数 t 处的坐标。
func arcPoint(cx, cy, a, b, t float64) (x, y float64) {
	sin, cos := math.Sincos(t)
	return cx + a*cos, cy + b*sin
}

// arcTangent 返回沿参数增大方向（dir=+1）或相反方向（dir=-1）前进时的切线角（度）。
//
// 切线取参数导数 (−a·sin t, b·cos t) 的方向角；圆上它恰好等于 t+90°。椭圆必须
// 走导数：长轴方向的切线变化更快，直接用 t 会让两端的字歪得不对。
//
// dir 决定字的字基朝向。沿 +t 方向走，字基指向该方向的切线；顶部沿 +t 走是朝
// 左的，字会整个倒过来，所以顶部必须用 dir=-1 反向排布。
func arcTangent(a, b, t, dir float64) float64 {
	sin, cos := math.Sincos(t)
	return math.Atan2(b*cos*dir, -a*sin*dir) * 180 / math.Pi
}

// speed 是弧长对参数 t 的导数：ds/dt = √((a·sin t)² + (b·cos t)²)。
func speed(a, b, t float64) float64 {
	sin, cos := math.Sincos(t)
	return math.Hypot(a*sin, b*cos)
}

// arcSteps 是弧长积分的分段数，必须保持为偶数（复合 Simpson 公式要求）。
// 固定 64 段对 180° 以内的弧足够精确（误差在 1e-6 量级，远小于一个像素），
// 且保证同一输入永远得到同一结果——印章图片要能进 golden 测试，随机或不稳定
// 的结果都会让基线失效。
const arcSteps = 64

// arcLength 返回从 t0 到 t1 的弧长。
//
// 圆上用解析解 a·(t1−t0)；椭圆没有初等表达式，用复合 Simpson 积分。字的位置
// 完全由弧长决定，这里是整套弧形排版的基础。
func arcLength(a, b, t0, t1 float64) float64 {
	if t0 == t1 {
		return 0
	}
	if a == b {
		return a * (t1 - t0)
	}
	sign := 1.0
	lo, hi := t0, t1
	if hi < lo {
		lo, hi = hi, lo
		sign = -1
	}
	h := (hi - lo) / float64(arcSteps)
	sum := speed(a, b, lo) + speed(a, b, hi)
	for i := 1; i < arcSteps; i++ {
		w := 2.0
		if i%2 == 1 {
			w = 1
		}
		sum += w * speed(a, b, lo+float64(i)*h)
	}
	return sign * sum * h / 3
}

// paramAtArcLength 反解：从 t0 出发沿参数增大方向走 s 的弧长，对应的参数是多少。
//
// 弧长关于参数单调递增，用二分即可，不需要解析反函数，也不存在多解。圆走解析
// 反解；椭圆先按短半轴估一个够大的上界（扁椭圆上速度变化大，估小了要翻倍
// 重来），再二分 60 次把区间收敛到远小于一个像素的尺度。
func paramAtArcLength(a, b, t0, s float64) float64 {
	if a == b {
		return t0 + s/a
	}
	if s == 0 {
		return t0
	}
	// arcLength 关于 t0 单调递增，因此对任意符号的 s 都可以用同一条二分。
	// 区间端点先按最小可能速度（min(a,b)）估计，再倍增到覆盖目标弧长为止。
	step := math.Abs(s) / math.Min(a, b)
	lo, hi := t0, t0
	for range 24 {
		if s > 0 {
			hi = t0 + step
			if arcLength(a, b, t0, hi) >= s {
				break
			}
		} else {
			lo = t0 - step
			if arcLength(a, b, t0, lo) <= s {
				break
			}
		}
		step *= 2
	}
	const iterations = 60
	for range iterations {
		mid := (lo + hi) / 2
		if arcLength(a, b, t0, mid) < s {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}
