package giogpu

import (
	"image"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// 严格 GPU 入口不能由 CPU 回退掩盖尺寸差异。
func TestRasterizeCoversFractionalPage(t *testing.T) {
	requireGPU(t)
	img, err := Rasterize(canvas.New(210, 140), geom.DPI(200))
	if err != nil {
		t.Fatal(err)
	}
	if want := image.Rect(0, 0, 1654, 1103); img.Bounds() != want {
		t.Fatalf("图片尺寸 %v，期望 %v", img.Bounds(), want)
	}
}
