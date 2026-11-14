package gio

import (
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

// 单独测量矢量路径的操作记录，不包含 GPU 帧提交、回读和字体加载。
func BenchmarkVectorPathRecording(b *testing.B) {
	family := canvas.NewFontFamily("gio-path-bench")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		b.Fatal(err)
	}
	text, _ := family.Face(12).ToPath("OFD vector rendering 0123456789")
	curve := canvas.MustParseSVGPath("M0 0 Q10 30 20 0 C25 -10 35 20 40 0 L40 30 L0 30 Z")
	for _, tc := range []struct {
		name string
		path *canvas.Path
	}{
		{"TextOutline", text},
		{"BezierFill", curve},
		{"ArcFill", canvas.Ellipse(10, 20)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var ops op.Ops
			r := New(layout.Context{Ops: &ops}, 200, 140)
			m := canvas.Identity.Translate(20, 30).Rotate(15).Scale(1.2, 0.8)
			b.ReportAllocs()
			for b.Loop() {
				ops.Reset()
				r.RenderPath(tc.path, canvas.DefaultStyle, m)
			}
		})
	}
}
