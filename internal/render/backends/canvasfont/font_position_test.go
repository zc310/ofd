package canvasfont

import (
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/font"
	"github.com/zc310/ofd/internal/models"
	"golang.org/x/image/font/gofont/goregular"
)

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
