package canvas

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/canvas/renderers/svg"
	"golang.org/x/image/font/gofont/goregular"
)

func svgPathsAndText(t *testing.T, data []byte) ([]string, string) {
	t.Helper()
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var paths []string
	var text strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
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
			if node.Name.Local == "path" {
				for _, attr := range node.Attr {
					if attr.Name.Local == "d" {
						paths = append(paths, attr.Value)
					}
				}
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
	return paths, text.String()
}

// 合成字重或斜体不能由浏览器二次合成，否则混排文字的可见字距会与光栅结果不同。
func TestSVGSyntheticTextUsesShapedOutlinesAndKeepsSemanticText(t *testing.T) {
	family := canvas.NewFontFamily("SVG synthetic regression")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	defer family.Destroy()
	for _, test := range []struct {
		name  string
		style canvas.FontStyle
	}{
		{"合成粗体", canvas.FontBold}, {"合成斜体", canvas.FontItalic},
		{"合成粗斜体", canvas.FontBold | canvas.FontItalic},
	} {
		t.Run(test.name, func(t *testing.T) {
			face := family.Face(24, canvas.Black, test.style)
			line := canvas.NewTextLine(face, "OFD AV fi", canvas.Left)
			matrix := canvas.Identity.Translate(18, 25).Scale(1.2, 0.9)
			var expected bytes.Buffer
			outline := svg.New(&expected, 100, 50, nil)
			line.RenderTo(outline, matrix, 0)
			if err := outline.Close(); err != nil {
				t.Fatal(err)
			}
			wantPaths, _ := svgPathsAndText(t, expected.Bytes())
			if len(wantPaths) == 0 {
				t.Fatal("测试字体未生成参考轮廓")
			}
			for _, clipped := range []bool{false, true} {
				var output bytes.Buffer
				writer := svg.New(&output, 100, 50, nil)
				renderer := svgTextRenderer{writer}
				if clipped {
					renderer.RenderClippedText(line, matrix, canvas.Rectangle(80, 40))
				} else {
					renderer.RenderText(line, matrix)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				paths, text := svgPathsAndText(t, output.Bytes())
				if clipped && len(paths) > 0 {
					paths = paths[1:] // clipPath 不是可见字形。
				}
				if !reflect.DeepEqual(paths, wantPaths) {
					t.Fatalf("SVG 未复用已整形轮廓，clipped=%v，实际路径数=%d，期望=%d", clipped, len(paths), len(wantPaths))
				}
				invisible := bytes.Contains(output.Bytes(), []byte("fill-opacity:0")) || bytes.Contains(output.Bytes(), []byte(`fill-opacity="0"`))
				if text != "OFD AV fi" || !invisible {
					t.Fatalf("SVG 未保留不可见语义文字，clipped=%v，text=%q", clipped, text)
				}
				if face.Fill.Color != canvas.Black || face.Style != test.style || len(face.Deco) != 0 {
					t.Fatal("SVG 导出修改了共享字体面")
				}
			}
		})
	}
}

type svgHeadingCapture struct {
	canvas.Renderer

	text   *canvas.Text
	matrix canvas.Matrix
}

func (r *svgHeadingCapture) RenderText(text *canvas.Text, matrix canvas.Matrix) {
	var value strings.Builder
	text.WalkSpans(func(_, _ float64, span canvas.TextSpan) { value.WriteString(span.Text) })
	if value.String() == "OFD 开源文档工具链" {
		r.text, r.matrix = text, matrix
	}
}

func (r *svgHeadingCapture) RenderClippedText(text *canvas.Text, matrix canvas.Matrix, _ *canvas.Path) {
	r.RenderText(text, matrix)
}

func TestCreatorThousandPagesSVGHeadingKeepsEmbeddedGlyphGeometry(t *testing.T) {
	const input = "../../../../testdata/ofd-creator/1000-pages.ofd"
	ofd, err := parser.NewOFD(input)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少字距回归样本 %s", input)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := render.NewDocument(canvas.White, ofd.Documents[0], 0)
	defer doc.Close()
	surface, err := doc.Page(doc.Pages[0])
	if err != nil {
		t.Fatal(err)
	}
	page, ok := surface.(canvasVectorSurface)
	if !ok {
		t.Fatalf("期望 canvas 矢量表面，实际为 %T", surface)
	}
	capture := &svgHeadingCapture{Renderer: canvas.New(page.Width(), page.Height())}
	page.RenderTo(capture)
	if capture.text == nil {
		t.Fatal("未找到模板标题 OFD 开源文档工具链")
	}
	var expected bytes.Buffer
	reference := svg.New(&expected, page.Width(), page.Height(), nil)
	capture.text.RenderTo(reference, capture.matrix, 0)
	if err := reference.Close(); err != nil {
		t.Fatal(err)
	}
	wantPaths, _ := svgPathsAndText(t, expected.Bytes())
	var output bytes.Buffer
	if err := page.Write(&output, "svg"); err != nil {
		t.Fatal(err)
	}
	paths, text := svgPathsAndText(t, output.Bytes())
	for _, want := range wantPaths {
		found := false
		for _, got := range paths {
			found = found || got == want
		}
		if !found {
			t.Fatal("SVG 标题没有保留已整形字形的精确轮廓及位置")
		}
	}
	if len(wantPaths) == 0 || !strings.Contains(text, "OFD 开源文档工具链") || len(svgFontPrograms(t, output.Bytes())) == 0 {
		t.Fatal("SVG 标题缺少轮廓、语义文字或内嵌字体")
	}
}
