package canvasfont

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func TestDefaultTextWeightPreservesNativeBoldGlyphs(t *testing.T) {
	for _, registeredStyle := range []canvas.FontStyle{canvas.FontRegular, canvas.FontBold} {
		t.Run(registeredStyle.String(), func(t *testing.T) {
			family := canvas.NewFontFamily(t.Name())
			if err := family.LoadFont(gobold.TTF, 0, registeredStyle); err != nil {
				t.Fatal(err)
			}
			native := family.Face(12, canvas.Black, registeredStyle)
			want, _ := native.ToPath("2016.07")
			for _, weight := range []int{0, 400} {
				t.Run(fmt.Sprintf("weight=%d", weight), func(t *testing.T) {
					face := buildTextFace(family, models.TextObject{CtText: models.CtText{Size: 12 / 2.83465, Weight: weight}}, nil)
					if face.FauxBold != 0 || face.Font != native.Font {
						t.Fatalf("默认字重改变了原生粗体：FauxBold=%g", face.FauxBold)
					}
					path, _ := face.ToPath("2016.07")
					if path == nil || want == nil || path.String() != want.String() {
						t.Fatal("默认字重的轮廓与原始粗体字形不一致")
					}
					if native.FauxBold != 0 || family.Face(12, canvas.Black, registeredStyle).FauxBold != 0 {
						t.Fatal("创建字体面修改了共享字体样式")
					}
				})
			}
		})
	}
}

func TestExplicitNonDefaultTextWeightStillUsesStyleSynthesis(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		style  canvas.FontStyle
		weight int
	}{
		{"常规体加粗", goregular.TTF, canvas.FontRegular, 700},
		{"粗体匹配粗体", gobold.TTF, canvas.FontBold, 700},
		{"粗体显式减细", gobold.TTF, canvas.FontBold, 300},
		{"粗体显式加粗", gobold.TTF, canvas.FontBold, 900},
	} {
		t.Run(tc.name, func(t *testing.T) {
			family := canvas.NewFontFamily(t.Name())
			if err := family.LoadFont(tc.data, 0, tc.style); err != nil {
				t.Fatal(err)
			}
			object := models.TextObject{CtText: models.CtText{Size: 12 / 2.83465, Weight: tc.weight}}
			face := buildTextFace(family, object, nil)
			want := family.Face(12, canvas.Black, canvasStyle(textFontStyle(tc.weight, false)))
			if face.FauxBold != want.FauxBold || face.Font != want.Font {
				t.Fatalf("显式字重%d的合成策略改变：FauxBold=%g，期望%g", tc.weight, face.FauxBold, want.FauxBold)
			}
		})
	}
}

func TestIntroPage9DatesKeepNativeEmbeddedBold(t *testing.T) {
	const input = "../../../../testdata/ofdrw/intro.ofd"
	ofd, err := parser.NewOFD(input)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	fonts := NewFonts(doc)
	defer fonts.Close()
	page := doc.Pages[8]
	count := 0
	if err := page.WithPageContent(func(content *models.PageContent) error {
		fonts.RegisterPageGlyphs(doc, page, content)
		for _, layer := range content.Content.Layer {
			for _, item := range layer.Items {
				if item.Text == nil || item.Text.Font != 83 {
					continue
				}
				object := *item.Text
				family, err := fonts.LoadFont(object.Font)
				if err != nil {
					return err
				}
				resolved, _ := fonts.FaceObject(family, object, nil)
				face, ok := resolved.(canvasFontFace)
				if !ok {
					t.Fatal("无法构造日期字体面")
				}
				cf, ok := family.(*canvas.FontFamily)
				if !ok {
					t.Fatal("日期字体不是canvas字体族")
				}
				native := cf.Face(object.Size*2.83465, canvas.Black, canvas.FontBold)
				value := object.TextCode[0].Value
				path, _ := face.face.ToPath(value)
				want, _ := native.ToPath(value)
				if face.face.FauxBold != 0 || face.face.Font != native.Font || path == nil || want == nil || path.String() != want.String() {
					t.Fatalf("日期%s未保留内嵌粗体原始轮廓：FauxBold=%g", value, face.face.FauxBold)
				}
				count++
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("日期标题数=%d，期望6", count)
	}
}
