package canvas

import (
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func TestTextGradientDoesNotRestartAtScriptSpans(t *testing.T) {
	family := NewFontFamily(t.Name())
	if err := family.LoadFont(goregular.TTF, 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	defer family.Destroy()
	linear := NewLinearGradient(Point{}, Point{X: 40, Y: 8})
	linear.Add(0, Red)
	linear.Add(1, Blue)
	radial := NewRadialGradient(Point{X: 2, Y: 1}, 0, Point{X: 2, Y: 1}, 40)
	radial.Add(0, Red)
	radial.Add(1, Blue)
	for _, gradient := range []Gradient{linear, radial} {
		for _, style := range []FontStyle{FontRegular, FontBold | FontItalic} {
			face := family.Face(18, gradient, style)
			// Go Regular 自带希腊字形，可在不依赖系统字体的情况下触发 Latin/Greek/Latin 分段。
			line := NewTextLine(face, "ABΩCD", Left)
			var originals []string
			var expected []*Path
			matrix := Identity.Translate(7, 12).Rotate(12).Scale(1.3, .8)
			line.WalkSpans(func(x, y float64, span TextSpan) {
				path, _ := span.Face.toPath(span.Glyphs, 0)
				originals = append(originals, path.String())
				expected = append(expected, path.Copy().Transform(matrix.Translate(x, y).Rotate(float64(span.Rotation))))
			})
			if len(originals) < 3 {
				t.Fatal("混合脚本文字没有产生至少三个 span")
			}
			scene := New(100, 40)
			line.RenderTo(scene, matrix, 0)
			if len(scene.layers[0]) != len(expected) {
				t.Fatal("渐变修正改变了绘制分段数量")
			}
			for index, layer := range scene.layers[0] {
				if layer.path == nil || layer.path.Empty() {
					continue
				}
				if !layer.path.Copy().Transform(layer.m).Equals(expected[index]) {
					t.Fatal("渐变修正改变了字形位置或轮廓")
				}
				point := layer.path.Bounds().Center()
				world := layer.m.Dot(point)
				local := matrix.Inv().Dot(world)
				got := layer.style.Fill.At(point.X, point.Y)
				want := gradient.At(local.X, local.Y)
				if got != want {
					t.Fatalf("脚本分段后渐变重新起算：样式=%v，位置=%v，颜色=%v，期望=%v", style, local, got, want)
				}
			}
			index := 0
			line.WalkSpans(func(_, _ float64, span TextSpan) {
				path, _ := span.Face.toPath(span.Glyphs, 0)
				if path.String() != originals[index] || span.Face.Fill.Gradient != gradient {
					t.Fatal("渲染修改了共享字形轮廓或画笔")
				}
				index++
			})
		}
	}
}
