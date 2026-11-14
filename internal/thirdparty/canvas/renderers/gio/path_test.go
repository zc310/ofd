package gio

import (
	"bytes"
	"image"
	"image/color"
	"sync"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

// 对照原来的先复制、整体变换、再转换圆弧的流程，要求 GPU 像素完全一致。
// 覆盖孔洞、二次/三次曲线和圆弧，避免流式提交改变变换及圆弧近似顺序。
// 变换含义参照 GB/T 33190—2016 §8.1.5（PDF 第 30 页），不调整坐标约定。
func TestTransformedPathRecordingMatchesOriginalPixels(t *testing.T) {
	window, err := headless.NewWindow(160, 160)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	family := canvas.NewFontFamily("gio-path-test")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	text, _ := family.Face(12).ToPath("OFD 012")
	paths := []struct {
		name string
		path *canvas.Path
	}{
		{"字形轮廓", text},
		{"复合曲线", canvas.MustParseSVGPath("M0 0 Q10 30 20 0 C25 -10 35 20 40 0 L40 30 L0 30 Z M5 10 L5 20 L15 20 L15 10 Z")},
		{"椭圆", canvas.Ellipse(12, 8)},
		{"曲线和圆弧混合", canvas.MustParseSVGPath("M0 0 Q10 30 20 0 A12 8 30 0 1 35 20 C25 30 10 25 0 0 Z")},
		{"空路径", &canvas.Path{}},
	}
	matrices := []struct {
		name string
		m    canvas.Matrix
	}{
		{"平移", canvas.Identity.Translate(50, 60)},
		{"旋转非等比缩放", canvas.Identity.Translate(50, 60).Rotate(25).Scale(1.6, 0.8)},
		{"切变", canvas.Matrix{{1.3, 0.4, 40}, {-0.2, 1.6, 60}}},
		{"反射", canvas.Identity.Translate(100, 60).Scale(-1.5, 1.2)},
	}
	for _, path := range paths {
		for _, matrix := range matrices {
			t.Run(path.name+"/"+matrix.name, func(t *testing.T) {
				before := path.path.Copy()
				frame := func(original bool) *image.RGBA {
					var ops op.Ops
					r := New(layout.Context{Ops: &ops}, 160, 160)
					// 同时覆盖 NewContain/NewStretch 使用的内部缩放。
					r.xScale, r.yScale = 0.9, 1.1
					paint.Fill(&ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
					var shape clip.Op
					if original {
						shape = originalPathClip(r, path.path.Copy().Transform(matrix.m))
					} else {
						shape = r.pathClip(path.path, matrix.m)
					}
					stack := shape.Push(&ops)
					paint.Fill(&ops, color.NRGBA{R: 210, G: 30, B: 90, A: 180})
					stack.Pop()
					if err := window.Frame(&ops); err != nil {
						t.Fatal(err)
					}
					pixels := image.NewRGBA(image.Rect(0, 0, 160, 160))
					if err := window.Screenshot(pixels); err != nil {
						t.Fatal(err)
					}
					return pixels
				}
				got, want := frame(false), frame(true)
				if !bytes.Equal(got.Pix, want.Pix) {
					t.Fatal("路径提交优化改变了 GPU 像素")
				}
				if !path.path.Equals(before) {
					t.Fatal("路径提交修改了共享轮廓")
				}
			})
		}
	}
}

// 保留优化前的实现作为独立对照，不能复用待测的路径提交代码。
func originalPathClip(r *Gio, path *canvas.Path) clip.Op {
	path = path.Copy().ReplaceArcs()
	var p clip.Path
	p.Begin(r.ops)
	for scanner := path.Scanner(); scanner.Scan(); {
		switch scanner.Cmd() {
		case canvas.MoveToCmd:
			p.MoveTo(r.point(scanner.End()))
		case canvas.LineToCmd:
			p.LineTo(r.point(scanner.End()))
		case canvas.QuadToCmd:
			p.QuadTo(r.point(scanner.CP1()), r.point(scanner.End()))
		case canvas.CubeToCmd:
			p.CubeTo(r.point(scanner.CP1()), r.point(scanner.CP2()), r.point(scanner.End()))
		case canvas.CloseCmd:
			p.Close()
		}
	}
	return (clip.Outline{Path: p.End()}).Op()
}

func TestBezierFillRecordingDoesNotAllocateTemporaryPaths(t *testing.T) {
	var ops op.Ops
	r := New(layout.Context{Ops: &ops}, 200, 140)
	path := canvas.MustParseSVGPath("M0 0 Q10 30 20 0 C25 -10 35 20 40 0 L40 30 L0 30 Z")
	m := canvas.Identity.Translate(20, 30).Rotate(15).Scale(1.2, 0.8)
	allocations := testing.AllocsPerRun(100, func() {
		ops.Reset()
		r.RenderPath(path, canvas.DefaultStyle, m)
	})
	if allocations != 0 {
		t.Fatalf("贝塞尔填充产生临时分配: %g 次/次", allocations)
	}
}

func TestConcurrentPathRecordingPreservesSharedGeometry(t *testing.T) {
	path := canvas.MustParseSVGPath("M0 0 Q10 30 20 0 A12 8 30 0 1 35 20 C25 30 10 25 0 0 Z")
	before := path.Copy()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			var ops op.Ops
			r := New(layout.Context{Ops: &ops}, 200, 140)
			for range 10 {
				ops.Reset()
				r.RenderPath(path, canvas.DefaultStyle, canvas.Identity.Translate(20, 30))
			}
		})
	}
	wg.Wait()
	if !path.Equals(before) {
		t.Fatal("并发记录修改了共享路径")
	}
}

func TestConcreteColorConversionPreservesOriginalRounding(t *testing.T) {
	for alpha := range 256 {
		for value := range 256 {
			c := color.RGBA{R: uint8(value), G: uint8(value / 2), B: uint8(255 - value), A: uint8(alpha)}
			r, g, b, a := c.RGBA()
			var want color.NRGBA
			if a != 0 {
				want = color.NRGBA{R: uint8((r * 0xffff / a) >> 8), G: uint8((g * 0xffff / a) >> 8), B: uint8((b * 0xffff / a) >> 8), A: uint8(a >> 8)}
			}
			if got := toNRGBA(c); got != want {
				t.Fatalf("颜色 %v 的转换舍入改变: got=%v want=%v", c, got, want)
			}
		}
	}
}
