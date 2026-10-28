package render

import (
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// 只生成有界几何与固定像素画布，验证有限/退化/非有限 CTM 和裁剪组合不会崩溃。
func FuzzRenderPathClipping(f *testing.F) {
	f.Add([]byte{16, 0, 0, 16, 0, 0, 1, 2})
	f.Add([]byte{0, 0, 0, 0, 255, 255, 255, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 {
			return
		}
		ctm := models.CTM{}
		for i := range 6 {
			ctm[i] = float64(int8(data[i])) / 16
		}
		if data[6] == 255 {
			ctm[0] = math.NaN()
		}
		object := models.PathObject{CtPath: models.CtPath{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 8, Height: 8}, CTM: &ctm, LineWidth: .2, Clips: rectangleClip(float64(data[7]%8+1), 8)},
			Fill:          true, Stroke: models.NewOptionalBool(data[6]&1 != 0), FillColor: &models.CTColor{Value: &models.Color{RGBA: geom.Black}},
			AbbreviatedData: models.SVGPath{
				{Type: models.MoveTo, Points: []models.StPos{{X: 1, Y: 1}}},
				{Type: models.LineTo, Points: []models.StPos{{X: 6, Y: 1}}},
				{Type: models.LineTo, Points: []models.StPos{{X: 4, Y: 6}}},
				{Type: models.Close},
			},
		}}
		doc := &Document{}
		ctx := newOffscreenSurface(8, 8, geom.DPMM(1))
		doc.Path(ctx, object, nil, models.StBox{Width: 8, Height: 8})
		if img := ctx.Raster(); img.Bounds().Dx() != 8 || img.Bounds().Dy() != 8 {
			t.Fatal("有界画布尺寸被输入改变")
		}
	})
}
