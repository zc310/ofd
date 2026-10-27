package canvas

import (
	"bytes"
	"encoding/xml"
	"image"
	"math"
	"testing"

	"github.com/zc310/canvas"
)

func TestSVGClippedImageFlipsAsymmetricClipIntoImageCoordinates(t *testing.T) {
	// 图片 20×12，裁剪仅覆盖局部左下角的三角形。SVG 中应位于图片底部，
	// 不能变成左上角的三角形；等高矩形或左右裁剪无法发现 Y 轴错误。
	img := image.NewNRGBA(image.Rect(0, 0, 20, 12))
	clip := &canvas.Path{}
	clip.MoveTo(2, 1)
	clip.LineTo(17, 1)
	clip.LineTo(2, 5)
	clip.Close()
	original := clip.ToSVG()
	for _, matrix := range []canvas.Matrix{
		canvas.Identity.Translate(3, 7),
		canvas.Identity.Translate(3, 7).Scale(2, .5),
		canvas.Identity.Translate(30, 8).Rotate(25).Shear(.2, .1),
	} {
		page := canvas.New(80, 60)
		page.RenderClippedImage(img, matrix, clip)
		surface := canvasVectorSurface{c: page}
		var output bytes.Buffer
		if err := surface.Write(&output, "svg"); err != nil {
			t.Fatal(err)
		}
		var svg struct {
			Defs []struct {
				Clip struct {
					Units string `xml:"clipPathUnits,attr"`
					Path  struct {
						Data string `xml:"d,attr"`
					} `xml:"path"`
				} `xml:"clipPath"`
			} `xml:"defs"`
		}
		if err := xml.Unmarshal(output.Bytes(), &svg); err != nil {
			t.Fatal(err)
		}
		if len(svg.Defs) != 1 || svg.Defs[0].Clip.Units != "userSpaceOnUse" {
			t.Fatalf("SVG 未保留图片局部裁剪：%s", output.String())
		}
		path, err := canvas.ParseSVGPath(svg.Defs[0].Clip.Path.Data)
		if err != nil {
			t.Fatal(err)
		}
		// SVG 局部 y=12-y，与 SVG 写入器的图片翻转抵消后，页面坐标必须
		// 与 PDF/光栅使用的 matrix·clip 完全相同。
		if got := path.Bounds(); got != (canvas.Rect{X0: 2, Y0: 7, X1: 17, Y1: 11}) {
			t.Fatalf("SVG 裁剪范围=%v，应位于图片下方", got)
		}
		flip := canvas.Identity.Translate(0, 12).ReflectY()
		got := path.Transform(matrix.Mul(flip)).Bounds()
		want := clip.Copy().Transform(matrix).Bounds()
		if math.Abs(got.X0-want.X0)+math.Abs(got.Y0-want.Y0)+math.Abs(got.X1-want.X1)+math.Abs(got.Y1-want.Y1) > 1e-6 {
			t.Fatalf("SVG 页面裁剪范围=%v，期望 %v", got, want)
		}
		if clip.ToSVG() != original {
			t.Fatal("SVG 输出改写了共享裁剪路径")
		}
		var repeated bytes.Buffer
		if err := surface.Write(&repeated, "svg"); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(output.Bytes(), repeated.Bytes()) {
			t.Fatal("重复 SVG 输出的裁剪发生变化")
		}
	}
}
