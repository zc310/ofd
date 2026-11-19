package canvas

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/rasterizer"
	"golang.org/x/image/draw"
)

// 整页不透明背景的快路径必须与原扫描器逐像素一致，其他路径保持原绘制行为。
func TestOpaqueCanvasFillMatchesScanner(t *testing.T) {
	resolution := canvas.DPI(200)
	margin := 1 / resolution.DPMM()
	rectangle := canvas.Rectangle(210+2*margin, 140+2*margin)
	matrix := canvas.Identity.Translate(-margin, -margin)
	hole := rectangle.Copy().Append(canvas.Rectangle(20, 20).Translate(10, 10))
	tests := []struct {
		name   string
		path   *canvas.Path
		matrix canvas.Matrix
		fill   color.Color
		stroke bool
		linear bool
		fast   bool
	}{
		{"白色背景", rectangle, matrix, color.White, false, true, true},
		{"彩色背景", rectangle, matrix, color.RGBA{R: 31, G: 113, B: 220, A: 255}, false, true, true},
		{"透明背景", rectangle, matrix, color.NRGBA{R: 200, G: 30, B: 20, A: 128}, false, true, false},
		{"覆盖不足", canvas.Rectangle(210, 140), canvas.Identity, color.White, false, true, false},
		{"包含孔洞", hole, matrix, color.White, false, true, false},
		{"旋转矩形", rectangle, matrix.Rotate(3), color.White, false, true, false},
		{"包含描边", rectangle, matrix, color.White, true, true, false},
		{"非线性色彩空间", rectangle, matrix, color.White, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bounds := image.Rect(0, 0, 1654, 1103)
			got, want := image.NewRGBA(bounds), image.NewRGBA(bounds)
			initial := image.NewUniform(color.RGBA{R: 9, G: 70, B: 120, A: 170})
			draw.Draw(got, bounds, initial, image.Point{}, draw.Src)
			draw.Draw(want, bounds, initial, image.Point{}, draw.Src)
			ctx := canvas.NewContext(canvas.New(210, 140))
			ctx.SetFillColor(tt.fill)
			if tt.stroke {
				ctx.SetStrokeColor(color.Black)
				ctx.SetStrokeWidth(0.5)
			}
			base := rasterizer.FromImage(got, resolution, canvas.DefaultColorSpace)
			adaptive := &adaptiveRasterizer{Rasterizer: base, resolution: resolution, fastImages: tt.linear}
			used := adaptive.fillOpaqueCanvas(tt.path, ctx.Style, tt.matrix)
			if used != tt.fast {
				t.Fatalf("快路径命中=%t，期望 %t", used, tt.fast)
			}
			if !used {
				adaptive.RenderPath(tt.path, ctx.Style, tt.matrix)
			}
			reference := rasterizer.FromImage(want, resolution, canvas.DefaultColorSpace)
			reference.RenderPath(tt.path, ctx.Style, tt.matrix)
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Fatal("输出与原扫描器不一致")
			}
		})
	}
}
