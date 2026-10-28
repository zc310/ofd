package canvas

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
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

func svgFontPrograms(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	pattern := regexp.MustCompile(`@font-face\{font-family:'([^']+)'[^}]*base64,([^']+)`)
	for _, match := range pattern.FindAllSubmatch(data, -1) {
		name := string(match[1])
		fontData, err := base64.StdEncoding.DecodeString(string(match[2]))
		if err != nil {
			t.Fatal(err)
		}
		if previous, exists := result[name]; exists && !bytes.Equal(previous, fontData) {
			t.Errorf("SVG中的同名字体 %q 指向不同子集数据", name)
		}
		result[name] = fontData
	}
	return result
}

func TestZsbkSecondPageSVGKeepsSameNameFontSubsetsSeparate(t *testing.T) {
	ofd, err := parser.NewOFD("../../../../testdata/ofdrw/zsbk.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少 fixture: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := render.NewDocument(canvas.White, ofd.Documents[0])
	defer doc.Close()
	surface, err := doc.Page(doc.Pages[1])
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := surface.Write(&output, "svg"); err != nil {
		t.Fatal(err)
	}
	programs := svgFontPrograms(t, output.Bytes())
	if len(programs) < 2 {
		t.Fatal("SVG没有保留内嵌字体")
	}
	// 原生标签字应仍在文本节点中，不能仅靠有轮廓掩盖字体互相覆盖。
	decoder := xml.NewDecoder(bytes.NewReader(output.Bytes()))
	var text strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch node := token.(type) {
		case xml.StartElement:
			if node.Name.Local == "text" {
				inText = true
			}
		case xml.EndElement:
			if node.Name.Local == "text" {
				inText = false
			}
		case xml.CharData:
			if inText {
				text.Write(node)
			}
		}
	}
	if !strings.Contains(text.String(), "建设单位") {
		t.Fatal("SVG缺少建设单位标签的原生文字")
	}
}
