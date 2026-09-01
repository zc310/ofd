package render

import (
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/models"
)

const patternDPI = 300.0
const maxPatternTiles = 100000

// drawPatternPath 将 OFD Pattern 单元栅格化后平铺到页面，再用路径作为蒙版。
// canvas 当前没有可直接承载任意 OFD CellContent 的图案类型，因此在渲染边界
// 内使用图片平铺，仍然保留路径的填充边界和对象坐标。
func (p *Document) drawPatternPath(ctx *canvas.Context, path *canvas.Path, pattern *models.CtPattern, objectBox models.StBox, pb models.StBox, alpha *uint8) bool {
	if pattern == nil || pattern.Width <= 0 || pattern.Height <= 0 || path == nil {
		return false
	}

	cell := canvas.New(pattern.Width, pattern.Height)
	cellCtx := canvas.NewContext(cell)
	if matrix, ok := patternMatrix(pattern); ok {
		cellCtx.SetView(matrix)
	}
	p.drawItems(cellCtx, pattern.CellContent.Items, nil, models.StBox{Width: pattern.Width, Height: pattern.Height})

	cellImage := rasterizer.Draw(cell, canvas.DPI(patternDPI), canvas.DefaultColorSpace)
	if cellImage == nil || cellImage.Bounds().Empty() {
		return false
	}

	page := canvas.New(pb.Width, pb.Height)
	pageCtx := canvas.NewContext(page)
	xStep, yStep := pattern.XStep, pattern.YStep
	if xStep <= 0 {
		xStep = pattern.Width
	}
	if yStep <= 0 {
		yStep = pattern.Height
	}

	originX, originY := 0.0, 0.0
	if strings.EqualFold(pattern.RelativeTo, "Object") {
		originX, originY = objectBox.X, objectBox.Y
	}
	startX := int(math.Floor((0-originX)/xStep)) - 1
	endX := int(math.Ceil((pb.Width-originX)/xStep)) + 1
	startY := int(math.Floor((0-originY)/yStep)) - 1
	endY := int(math.Ceil((pb.Height-originY)/yStep)) + 1
	if endX-startX+1 <= 0 || endY-startY+1 <= 0 ||
		(endX-startX+1) > maxPatternTiles || (endY-startY+1) > maxPatternTiles ||
		(endX-startX+1)*(endY-startY+1) > maxPatternTiles {
		return false
	}

	for row, iy := 0, startY; iy <= endY; iy, row = iy+1, row+1 {
		for col, ix := 0, startX; ix <= endX; ix, col = ix+1, col+1 {
			tile := patternTile(cellImage, pattern.ReflectMethod, row, col)
			x := originX + float64(ix)*xStep
			y := originY + float64(iy)*yStep
			m := imageMatrix(models.StBox{X: x, Y: y, Width: pattern.Width, Height: pattern.Height}, tile, models.CTM{pattern.Width, 0, 0, pattern.Height, 0, 0}, pb.Height)
			pageCtx.RenderImage(tile, m)
		}
	}

	patternImage := rasterizer.Draw(page, canvas.DPI(patternDPI), canvas.DefaultColorSpace)
	if patternImage == nil || patternImage.Bounds().Empty() {
		return false
	}
	pageMatrix := imageMatrix(models.StBox{Width: pb.Width, Height: pb.Height}, patternImage, models.CTM{pb.Width, 0, 0, pb.Height, 0, 0}, pb.Height)
	var output image.Image = imageWithClip(patternImage, path, pageMatrix)
	if alpha != nil {
		// OFD Alpha 表示透明度，0 为不透明，255 为完全透明。
		output = applyImageAlpha(output, 255-*alpha)
	}
	ctx.RenderImage(output, ctx.CoordSystemView().Mul(ctx.View()).Mul(pageMatrix))
	return true
}

// patternTile 返回反射模式下当前行列使用的单元图像。
func patternTile(src image.Image, method string, row, col int) image.Image {
	flipX, flipY := false, false
	switch strings.ToLower(method) {
	case "row":
		flipX = row&1 == 1
	case "column":
		flipY = col&1 == 1
	case "rowandcolumn":
		flipX = row&1 == 1
		flipY = col&1 == 1
	}
	if !flipX && !flipY {
		return src
	}

	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			sx, sy := x, y
			if flipX {
				sx = b.Dx() - 1 - sx
			}
			if flipY {
				sy = b.Dy() - 1 - sy
			}
			out.Set(x, y, src.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return out
}

func patternMatrix(pattern *models.CtPattern) (canvas.Matrix, bool) {
	if pattern == nil || len(pattern.CTM) == 0 {
		return canvas.Identity, true
	}
	values := make([]float64, 0, len(pattern.CTM))
	for _, value := range pattern.CTM {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return canvas.Identity, false
		}
		values = append(values, v)
	}
	if len(values) != 6 || values[0]*values[3]-values[1]*values[2] == 0 {
		return canvas.Identity, false
	}
	return canvas.Matrix{{values[0], values[2], values[4]}, {values[1], values[3], values[5]}}, true
}
