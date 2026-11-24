// Package image 计算图片放置矩阵、边框和局部裁剪，不解码资源或执行绘制。
// 输入模型与共享路径均视为只读。
package image

import (
	"image"
	"math"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

// CTM 返回图片放置使用的矩阵；缺省时以 Boundary 宽高缩放单位图像。
// 此合成矩阵仅用于图片放置，不能作为对象显式 CTM 应用于毫米裁剪路径。
func CTM(object spec.ImageObject) spec.CTM {
	if object.CTM != nil {
		return *object.CTM
	}
	return spec.CTM{object.Boundary.Width, 0, 0, object.Boundary.Height, 0, 0}
}

// Matrix 将后端图片的局部坐标（左下原点、Y 向上）映射到 OFD 页面，不读取像素。
func Matrix(box spec.StBox, img image.Image, ctm spec.CTM) geom.Matrix {
	return MatrixWH(box, float64(img.Bounds().Dx()), float64(img.Bounds().Dy()), ctm)
}

// MatrixWH 将宽高为 w、h 的后端图片局部空间映射到 Y 向下的页面画布。
// 用于位图、SVG 和离屏结果的相同放置规则，调用方负责检查计算结果的有限性。
func MatrixWH(box spec.StBox, w, h float64, ctm spec.CTM) geom.Matrix {
	return geom.Matrix{
		{ctm[0] / w, -ctm[2] / h, box.X + ctm[2] + ctm[4]},
		{ctm[1] / w, -ctm[3] / h, box.Y + ctm[3] + ctm[5]},
	}
}

// CanvasBox 将 OFD 页面画布包围盒换成 Boundary，无需翻转坐标。
func CanvasBox(bounds geom.Rect) spec.StBox {
	return spec.StBox{X: bounds.X0, Y: bounds.Y0, Width: bounds.W(), Height: bounds.H()}
}

// BoundaryMatrix 把整个源画布放入 Boundary；只叠加容器 CTM，不再次施加
// 源内容自身的 CTM。沿用 §8.1.5、§13 表50 的内部变换与外部放置区分。
func BoundaryMatrix(box spec.StBox, w, h float64, parent *spec.CTM) geom.Matrix {
	ctm := spec.CTM{box.Width, 0, 0, box.Height, 0, 0}
	if parent != nil {
		ctm = *parent.Multiply(&ctm)
	}
	return MatrixWH(box, w, h, ctm)
}

// OffscreenMatrix 将离屏图片按实际像素与分辨率放回画布的左上角。
// 底层光栅器从左下原点扫描，像素高度向上取整会在顶部产生额外边缘；
// 放置时扣除这段边缘，既不压缩图像，也不让内容向下偏移。
// 调用方负责检查矩阵有限性。
// 图像放置遵循 GB/T 33190—2016 第 10 章。
func OffscreenMatrix(bounds geom.Rect, img image.Image, resolution geom.Resolution) geom.Matrix {
	w, h := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
	bounds.X1 = bounds.X0 + w/resolution.DPMM()
	bounds.Y0 = bounds.Y1 - h/resolution.DPMM()
	return BoundaryMatrix(CanvasBox(bounds), w, h, nil)
}

// Border 生成局部 Y 向上的图片边框，从左上角顺时针绕行。
// 对应 GB/T 33190—2016 §10 表43 的边框半径及虚线起点约定。
func Border(width, height, rx, ry float64) *geom.Path {
	if width <= 0 || height <= 0 || !finiteFloat(width) || !finiteFloat(height) || !finiteFloat(rx) || !finiteFloat(ry) {
		return &geom.Path{}
	}
	rx, ry = max(rx, 0), max(ry, 0)
	if rx > width/2 {
		rx = width / 2
	}
	if ry > height/2 {
		ry = height / 2
	}
	if rx == 0 && ry == 0 {
		return geom.Rectangle(width, height).Transform(geom.Matrix{{1, 0, 0}, {0, -1, height}})
	}
	if rx == 0 {
		rx = ry
	}
	if ry == 0 {
		ry = rx
	}
	// 顺时针绕行时 sweep=false 才得到凸圆角。
	path := &geom.Path{}
	path.MoveTo(rx, height)
	path.LineTo(width-rx, height)
	path.ArcTo(rx, ry, 0, false, false, width, height-ry)
	path.LineTo(width, ry)
	path.ArcTo(rx, ry, 0, false, false, width-rx, 0)
	path.LineTo(rx, 0)
	path.ArcTo(rx, ry, 0, false, false, 0, ry)
	path.LineTo(0, height-ry)
	path.ArcTo(rx, ry, 0, false, false, rx, height)
	path.Close()
	return path
}

// BorderPath 将 Boundary 中的边框映射到页面画布，并应用容器的父级变换。
// 图像 CTM 用于单位图像放置；边框半径与线宽是 Boundary 空间中的毫米值，
// 不应再套用该单位图像缩放。参见 GB/T 33190—2016 §8.4 表34、§10 表43。
func BorderPath(object spec.ImageObject, parent *spec.CTM) *geom.Path {
	if object.Border == nil || !object.Boundary.IsFinite() || !parent.IsFinite() {
		return &geom.Path{}
	}
	box := object.Boundary
	path := Border(box.Width, box.Height, object.Border.HorizonalCornerRadius, object.Border.VerticalCornerRadius)
	matrix := geom.Identity.Translate(box.X, box.Y)
	if parent != nil {
		matrix = matrix.Mul(geom.Matrix{{parent[0], parent[2], parent[4]}, {parent[1], parent[3], parent[5]}})
	}
	// Border 的局部 Y 向上，父级 CTM 则在 OFD 的 Y 向下坐标中生效。
	matrix = matrix.Mul(geom.Matrix{{1, 0, 0}, {0, -1, box.Height}})
	if !finiteMatrix(matrix) {
		return &geom.Path{}
	}
	return path.Transform(matrix)
}

// Clip 将页面裁剪转换到图片像素空间，供原生路径裁剪及位图掩码共同使用。
// nil 表示无需裁剪；保留既有非有限或退化放置矩阵的跳过行为。逆矩阵溢出时
// 返回空路径以隐藏图片，避免泄露裁剪外内容。返回的非空路径为独立副本。
func Clip(clip *geom.Path, matrix geom.Matrix, width, height int) *geom.Path {
	if clip == nil || width <= 0 || height <= 0 || !finiteMatrix(matrix) || geom.Equal(matrix.Det(), 0) {
		return nil
	}
	inverse := matrix.Inv()
	if !finiteMatrix(inverse) {
		return &geom.Path{}
	}
	local := clip.Copy().Transform(inverse)
	// GB/T 33190—2016 §8.4（印刷页48）：仅完整覆盖图片时才可省略裁剪。
	// 亚毫米或不足一个像素的内缩仍是真实裁剪；PDF 可直接保留路径和原始图片，
	// 不必通过忽略细边来规避烘焙透明蒙版造成的插值灰边。
	if geom.ClipContainsRect(local, geom.RectFromSize(0, 0, float64(width), float64(height))) {
		return nil
	}
	return local
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

func finiteFloat(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
