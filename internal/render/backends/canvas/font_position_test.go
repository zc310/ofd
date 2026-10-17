package canvas

import (
	"bytes"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/font"
	"github.com/zc310/ofd/internal/models"
	"golang.org/x/image/font/gofont/goregular"
)

// 小字号经CTM放大后，文字和预先定位的轮廓必须落在同一位置。
// 否则单独对文字基线做像素吸附，会在低DPI时放大两层图形的错位。
func TestTransformedTextMatchesPositionedOutline(t *testing.T) {
	family := canvas.NewFontFamily("positioned-outline")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	face := buildTextFace(family, models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &models.CTM{9, 0, 0, 9, 0, 0}},
		Size:          0.3528,
	}}, nil)
	path, _ := face.ToPath("H")
	if path == nil || path.Empty() {
		t.Fatal("缺少参考轮廓")
	}
	for _, dpi := range []float64{72, 96, 150, 300} {
		for _, y := range []float64{3.01, 3.09, 3.17, 3.31} {
			matrix := canvas.Identity.Translate(2.13, y).Scale(9, 9)
			native := canvas.New(12, 10)
			native.RenderText(canvas.NewTextLine(face, "H", canvas.Left), matrix)
			outline := canvas.New(12, 10)
			outline.RenderPath(path, canvas.DefaultStyle, matrix)
			a := rasterize(native, canvas.DPI(dpi), canvas.DefaultColorSpace)
			b := rasterize(outline, canvas.DPI(dpi), canvas.DefaultColorSpace)
			if !bytes.Equal(a.Pix, b.Pix) {
				t.Errorf("DPI=%g 基线y=%g：文字与原位轮廓发生错位", dpi, y)
			}
		}
	}
}

func TestTextLineCacheSeparatesPixelHinting(t *testing.T) {
	family := canvas.NewFontFamily("hinting-cache")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	fonts := NewFonts(nil)
	defer fonts.Close()
	regular := buildTextFace(family, models.TextObject{CtText: models.CtText{Size: 0.3528}}, nil)
	positioned := buildTextFace(family, models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{CTM: &models.CTM{9, 0, 0, 9, 0, 0}}, Size: 0.3528,
	}}, nil)
	if regular.Hinting != font.VerticalHinting || positioned.Hinting != font.NoHinting {
		t.Fatal("未按对象定位方式选择提示模式")
	}
	line := fonts.shapedTextLine(regular, "H")
	positionedLine := fonts.shapedTextLine(positioned, "H")
	if line == positionedLine {
		t.Fatal("精确定位文字借用了启用像素吸附的缓存")
	}
	if fonts.shapedTextLine(positioned, "H") != positionedLine {
		t.Fatal("相同提示模式未命中缓存")
	}
}
