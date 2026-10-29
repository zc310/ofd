package text

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// OutlineFace 在定位度量之外提供只读字形轮廓。轮廓可以来自共享字体缓存。
type OutlineFace interface {
	Face
	DirectPath(value string) *geom.Path
	ToPath(value string) *geom.Path
}

// Outline 按与正文相同的定位和分段规则合并可见字形轮廓，不包含语义补偿文字。
// transform 将每个 run 的 Y 向上字形空间映射到目标空间，字体锁由调用方持有。
func Outline(face OutlineFace, object models.TextObject, transform func(x, y float64) geom.Matrix) *geom.Path {
	var result *geom.Path
	WalkOutlines(face, object, transform, func(path *geom.Path) {
		if result == nil {
			result = path
		} else {
			result = result.Or(path)
		}
	})
	return result
}

// WalkOutlines 按绘制顺序访问每个 run 的独立轮廓，供描边等必须在合并前处理
// 的操作使用。回调取得路径所有权，可以修改它；字体缓存中的原始轮廓保持只读。
func WalkOutlines(face OutlineFace, object models.TextObject, transform func(x, y float64) geom.Matrix, visit func(*geom.Path)) {
	if face == nil || transform == nil || visit == nil {
		return
	}
	object.CGTransform = models.ExpandCGTransformPositions(object.TextCode, object.CGTransform)
	position := 0
	for _, code := range object.TextCode {
		if finiteFloat(code.X) && finiteFloat(code.Y) && finiteDeltas(code.DeltaX) && finiteDeltas(code.DeltaY) {
			WalkRuns(face, object, code, position, func(run Run) {
				if run.SemanticOnly {
					return
				}
				matrix := transform(run.X, run.Y)
				if !finiteMatrix(matrix) {
					return
				}
				path := face.DirectPath(run.Value)
				if path == nil {
					path = face.ToPath(run.Value)
				}
				if path == nil || path.Empty() {
					return
				}
				path = path.Copy()
				path.Close()
				path.Transform(matrix)
				visit(path)
			})
		}
		// 无效 TextCode 仍占用源字符位置，不改变后续 CGTransform 的映射。
		position += len([]rune(code.Value))
	}
}

func finiteDeltas(values models.StArrayF) bool {
	for _, value := range values {
		if !finiteFloat(value) {
			return false
		}
	}
	return true
}

func finiteMatrix(matrix geom.Matrix) bool {
	for _, row := range matrix {
		for _, value := range row {
			if !finiteFloat(value) {
				return false
			}
		}
	}
	return true
}
