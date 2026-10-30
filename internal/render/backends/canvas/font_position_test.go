package canvas

import (
	"bytes"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/backends/canvasfont"
	"golang.org/x/image/font/gofont/goregular"
)

// 小字号经 CTM 放大后，文字和预先定位的轮廓必须落在同一位置。
// 否则单独对文字基线做像素吸附，会在低 DPI 时放大两层图形的错位。
func TestTransformedTextMatchesPositionedOutline(t *testing.T) {
	family := canvas.NewFontFamily("positioned-outline")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	fonts := canvasfont.NewFonts(nil)
	defer fonts.Close()
	face := fonts.FaceObject(family, models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &models.CTM{9, 0, 0, 9, 0, 0}},
		Size:          0.3528,
	}}, nil)
	path := face.ToPath("H")
	if path == nil || path.Empty() {
		t.Fatal("缺少参考轮廓")
	}
	line, ok := face.ShapedRun("H").(*canvas.Text)
	if !ok || line == nil {
		t.Fatal("缺少原生文字行")
	}
	for _, dpi := range []float64{72, 96, 150, 300} {
		for _, y := range []float64{3.01, 3.09, 3.17, 3.31} {
			matrix := canvas.Identity.Translate(2.13, y).Scale(9, 9)
			native := canvas.New(12, 10)
			native.RenderText(line, matrix)
			outline := canvas.New(12, 10)
			outline.RenderPath(path, canvas.DefaultStyle, matrix)
			a := rasterize(native, canvas.DPI(dpi), canvas.DefaultColorSpace, nil)
			b := rasterize(outline, canvas.DPI(dpi), canvas.DefaultColorSpace, nil)
			if !bytes.Equal(a.Pix, b.Pix) {
				t.Errorf("DPI=%g 基线 y=%g：文字与原位轮廓发生错位", dpi, y)
			}
		}
	}
}
