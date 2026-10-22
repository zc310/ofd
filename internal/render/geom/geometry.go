package geom

import "github.com/zc310/canvas"

// 几何类型直接复用canvas。字体轮廓、OFD变换及各后端消费同一条路径，
// 不再逐段转换或持有另一份路径缓存。共享轮廓仍须先Copy再修改。
type (
	Path               = canvas.Path
	Paths              = canvas.Paths
	PathScanner        = canvas.PathScanner
	PathReverseScanner = canvas.PathReverseScanner
	Point              = canvas.Point
	Matrix             = canvas.Matrix
	Rect               = canvas.Rect
	Resolution         = canvas.Resolution
	FillRule           = canvas.FillRule
	Capper             = canvas.Capper
	ButtCapper         = canvas.ButtCapper
	RoundCapper        = canvas.RoundCapper
	SquareCapper       = canvas.SquareCapper
	Joiner             = canvas.Joiner
	BevelJoiner        = canvas.BevelJoiner
	RoundJoiner        = canvas.RoundJoiner
	MiterJoiner        = canvas.MiterJoiner
)

const (
	MoveToCmd = canvas.MoveToCmd
	LineToCmd = canvas.LineToCmd
	QuadToCmd = canvas.QuadToCmd
	CubeToCmd = canvas.CubeToCmd
	ArcToCmd  = canvas.ArcToCmd
	CloseCmd  = canvas.CloseCmd

	NonZero  = canvas.NonZero
	EvenOdd  = canvas.EvenOdd
	Positive = canvas.Positive
	Negative = canvas.Negative

	// Tolerance 是OFD描边展开显式传入的毫米容差，不修改canvas的全局配置。
	Tolerance         = 0.01
	DefaultResolution = canvas.DefaultResolution
)

var (
	Identity  = canvas.Identity
	ButtCap   = canvas.ButtCap
	RoundCap  = canvas.RoundCap
	SquareCap = canvas.SquareCap
	BevelJoin = canvas.BevelJoin
	RoundJoin = canvas.RoundJoin
	MiterJoin = canvas.MiterJoin
)

func Equal(a, b float64) bool { return canvas.Equal(a, b) }

func Rectangle(width, height float64) *Path { return canvas.Rectangle(width, height) }
func RoundedRectangle(width, height, radius float64) *Path {
	return canvas.RoundedRectangle(width, height, radius)
}
func Circle(radius float64) *Path { return canvas.Circle(radius) }

func RectFromSize(x, y, width, height float64) Rect {
	return canvas.RectFromSize(x, y, width, height)
}

func DPMM(dpmm float64) Resolution { return canvas.DPMM(dpmm) }
func DPI(dpi float64) Resolution   { return canvas.DPI(dpi) }

func MustParseSVGPath(data string) *Path { return canvas.MustParseSVGPath(data) }

func SolveQuadraticFormula(a, b, c float64) (float64, float64) {
	return canvas.SolveQuadraticFormula(a, b, c)
}
