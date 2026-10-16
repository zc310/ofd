package canvas

import (
	"bytes"
	"testing"

	"github.com/zc310/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

func TestSVGTextKeepsSolidTextAndOutlinesGradients(t *testing.T) {
	family := canvas.NewFontFamily("SVG regression")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	linear := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20})
	linear.Grad = canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}
	radial := canvas.NewRadialGradient(canvas.Point{}, 0, canvas.Point{}, 20)
	radial.Grad = linear.Grad
	for _, test := range []struct {
		name   string
		fill   any
		marker string
	}{
		{"solid", canvas.Black, "<text"},
		{"axial", linear, "<linearGradient"},
		{"radial", radial, "<radialGradient"},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := canvas.New(80, 40)
			ctx := canvas.NewContext(page)
			ctx.Translate(10, 15)
			ctx.Scale(2, 2)
			ctx.DrawText(0, 0, canvas.NewTextLine(family.Face(12, test.fill), "Gradient", canvas.Left))
			var output bytes.Buffer
			if err := (canvasVectorSurface{c: page}).Write(&output, "svg"); err != nil {
				t.Fatal(err)
			}
			data := output.Bytes()
			if !bytes.Contains(data, []byte(test.marker)) || bytes.Contains(data, []byte("<image")) {
				t.Fatalf("SVG 缺少 %s 或文字被栅格化: %s", test.marker, data)
			}
			if test.name != "solid" && (!bytes.Contains(data, []byte("<path")) || bytes.Contains(data, []byte("<text"))) {
				t.Fatalf("SVG 渐变文字未输出为矢量轮廓: %s", data)
			}
		})
	}
}
