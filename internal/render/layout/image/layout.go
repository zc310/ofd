// Package image 计算图片放置矩阵、边框和局部裁剪，不解码资源或执行绘制。
// 输入模型与共享路径均视为只读。
package image

import (
	"image"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// CTM 返回图片放置使用的矩阵；缺省时以 Boundary 宽高缩放单位图像。
// 此合成矩阵仅用于图片放置，不能作为对象显式 CTM 应用于毫米裁剪路径。
func CTM(object models.ImageObject) models.CTM {
	if object.CTM != nil {
		return *object.CTM
	}
	return models.CTM{object.Boundary.Width, 0, 0, object.Boundary.Height, 0, 0}
}

// Matrix 将图片的像素坐标映射到 Y 向上的页面画布，不读取像素。
func Matrix(box models.StBox, img image.Image, ctm models.CTM, pageHeight float64) geom.Matrix {
	return MatrixWH(box, float64(img.Bounds().Dx()), float64(img.Bounds().Dy()), ctm, pageHeight)
}

// MatrixWH 将宽高为 w、h 的 Y 向下坐标空间映射到页面画布。
// 用于位图、SVG 和离屏结果的相同放置规则，调用方负责检查计算结果的有限性。
func MatrixWH(box models.StBox, w, h float64, ctm models.CTM, pageHeight float64) geom.Matrix {
	return geom.Matrix{
		{ctm[0] / w, -ctm[2] / h, box.X + ctm[2] + ctm[4]},
		{-ctm[1] / w, ctm[3] / h, pageHeight - box.Y - ctm[3] - ctm[5]},
	}
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
func BorderPath(object models.ImageObject, pageHeight float64, parent *models.CTM) *geom.Path {
	if object.Border == nil || !object.Boundary.IsFinite() || !finiteFloat(pageHeight) || !parent.IsFinite() {
		return &geom.Path{}
	}
	box := object.Boundary
	path := Border(box.Width, box.Height, object.Border.HorizonalCornerRadius, object.Border.VerticalCornerRadius)
	matrix := geom.Matrix{{1, 0, box.X}, {0, -1, pageHeight - box.Y}}
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
	if coversImage(local, matrix, width, height) {
		return nil
	}
	return local
}

// coversImage 仅跳过覆盖图片的轴对齐矩形。面积近似不能证明完整覆盖：小洞、
// 圆角或缺口即使只占不到 1% 面积，也必须保留裁剪。
func coversImage(clip *geom.Path, matrix geom.Matrix, width, height int) bool {
	// 保留出血兼容容差：短边的 1.5%、0.5mm 折算像素数及 2px 中取最大值。
	// 跳过近乎整图的矩形裁剪可保留源编码，并避免 PDF 的 SMask 插值产生灰边。
	eps := math.Max(2, 0.015*float64(min(width, height)))
	if perPx := millimeterPerPixel(matrix); perPx > 0 {
		eps = math.Max(eps, 0.5/perPx)
	}
	bounds := clip.Bounds()
	return geom.ClipContainsRect(clip, bounds) &&
		bounds.X0 <= eps && bounds.Y0 <= eps &&
		bounds.X1 >= float64(width)-eps && bounds.Y1 >= float64(height)-eps
}

// millimeterPerPixel 取两轴像素尺度的较小值，保持既有毫米出血容差。
func millimeterPerPixel(matrix geom.Matrix) float64 {
	sx := math.Hypot(matrix[0][0], matrix[1][0])
	sy := math.Hypot(matrix[0][1], matrix[1][1])
	scale := math.Min(sx, sy)
	if math.IsNaN(scale) || math.IsInf(scale, 0) || scale <= 0 {
		return 0
	}
	return scale
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
