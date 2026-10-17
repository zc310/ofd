package canvas

import (
	"bytes"
	"encoding/xml"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func svgStrokeStyles(t *testing.T, data []byte) []map[string]string {
	t.Helper()
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var styles []map[string]string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		node, ok := token.(xml.StartElement)
		if !ok || node.Name.Local != "path" {
			continue
		}
		style := make(map[string]string)
		for _, attr := range node.Attr {
			if attr.Name.Local != "style" {
				continue
			}
			for _, property := range strings.Split(attr.Value, ";") {
				key, value, ok := strings.Cut(property, ":")
				if ok {
					style[strings.TrimSpace(key)] = strings.TrimSpace(value)
				}
			}
		}
		if style["stroke"] != "" && style["stroke"] != "none" {
			styles = append(styles, style)
		}
	}
	return styles
}

func svgStrokeNumber(t *testing.T, style map[string]string, key string, fallback float64) float64 {
	t.Helper()
	if style[key] == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(style[key], 64)
	if err != nil {
		t.Fatalf("无法解析SVG样式 %s=%q：%v", key, style[key], err)
	}
	return value
}

func TestSVGMinimumStrokeKeepsGeometryAndDashLengths(t *testing.T) {
	for _, test := range []struct {
		name               string
		width, scale, want float64
	}{
		{"缩小的字形轮廓", 0.002, 0.3528, 2 * 25.4 / 300},
		{"放大的极细描边", 0.002, 9, 2 * 25.4 / 300},
		{"普通线宽", 0.5, 2, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := canvas.New(30, 10)
			path := &canvas.Path{}
			path.MoveTo(2/test.scale, 5/test.scale)
			path.LineTo(28/test.scale, 5/test.scale)
			style := canvas.DefaultStyle
			style.Fill = canvas.Paint{}
			style.Stroke = canvas.Paint{Color: canvas.Black}
			style.StrokeWidth = test.width
			style.Dashes = []float64{2 / (test.width * test.scale), 1 / (test.width * test.scale)}
			style.DashOffset = 0.5 / (test.width * test.scale)
			originalDash := style.Dashes[0]
			page.RenderPath(path, style, canvas.Identity.Scale(test.scale, test.scale))
			surface := canvasVectorSurface{c: page}
			var output bytes.Buffer
			if err := surface.Write(&output, "svg"); err != nil {
				t.Fatal(err)
			}
			styles := svgStrokeStyles(t, output.Bytes())
			if len(styles) != 1 {
				t.Fatalf("SVG描边数量=%d，期望1", len(styles))
			}
			got := styles[0]
			if width := svgStrokeNumber(t, got, "stroke-width", 1); math.Abs(width-test.want) > 1e-6 {
				t.Errorf("SVG线宽=%g，期望%g", width, test.want)
			}
			if phase := svgStrokeNumber(t, got, "stroke-dashoffset", 0); math.Abs(phase-0.5) > 1e-6 {
				t.Errorf("SVG虚线相位=%g，期望0.5", phase)
			}
			if got["stroke-dasharray"] != "2 1" {
				t.Errorf("SVG虚线长度=%q，期望2 1", got["stroke-dasharray"])
			}
			if bytes.Contains(output.Bytes(), []byte("<image")) {
				t.Fatal("SVG描边被栅格化")
			}
			if style.StrokeWidth != test.width || style.Dashes[0] != originalDash {
				t.Fatal("SVG输出改写了共享样式")
			}
			var repeated bytes.Buffer
			if err := surface.Write(&repeated, "svg"); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(output.Bytes(), repeated.Bytes()) {
				t.Fatal("重复SVG输出发生变化")
			}
		})
	}
}

// 首页标签的加粗来自轮廓描边；SVG也必须应用与图片预览相同的细线显示下限。
func TestZsbkFirstPageSVGKeepsLabelOutlinesVisible(t *testing.T) {
	ofd, err := parser.NewOFD("../../../../testdata/ofdrw/zsbk.ofd")
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := render.NewDocument(canvas.White, ofd.Documents[0])
	defer doc.Close()
	surface, err := doc.Page(doc.Pages[0])
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := surface.Write(&output, "svg"); err != nil {
		t.Fatal(err)
	}
	styles := svgStrokeStyles(t, output.Bytes())
	if len(styles) == 0 {
		t.Fatal("SVG缺少首页文字轮廓描边")
	}
	for _, style := range styles {
		if width := svgStrokeNumber(t, style, "stroke-width", 1); width > 0 && width < 2*25.4/300-1e-6 {
			t.Errorf("首页SVG仍存在几乎不可见的描边：%g", width)
		}
	}
	if !bytes.Contains(output.Bytes(), []byte("<text")) {
		t.Fatal("首页SVG丢失了原生文字")
	}
}
