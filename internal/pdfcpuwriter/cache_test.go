package pdfcpuwriter

import (
	"bytes"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestGradientCachesUseExactGeometryAndIndependentFunctions(t *testing.T) {
	r, err := New(t.Context(), 70, 30)
	if err != nil {
		t.Fatal(err)
	}
	makePaint := func(end float64) canvas.Paint {
		gradient := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: end})
		gradient.Add(0, canvas.Red)
		gradient.Add(1, canvas.Blue)
		return canvas.Paint{Gradient: gradient}
	}
	first := makePaint(20)
	r.paint(first, false, canvas.Identity)
	r.paint(makePaint(20), true, canvas.Identity)
	if len(r.patterns) != 1 || len(r.functionCache) != 1 {
		t.Fatal("相同渐变没有复用Pattern和函数")
	}
	r.paint(makePaint(math.Nextafter(20, math.Inf(1))), false, canvas.Identity)
	r.paint(first, false, canvas.Identity.Translate(math.SmallestNonzeroFloat64, 0))
	if len(r.patterns) != 3 || len(r.functionCache) != 1 {
		t.Fatal("单ULP坐标差被近似合并，或同色标函数被重复创建")
	}
	gradient := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20})
	gradient.Grad = canvas.Grad{{Offset: 0, Color: canvas.Red}, {Offset: .5, Color: canvas.Green}, {Offset: 1, Color: canvas.Blue}}
	r.paint(canvas.Paint{Gradient: gradient}, false, canvas.Identity)
	functions := len(r.functionCache)
	changed := *gradient
	changed.Grad = append(canvas.Grad(nil), gradient.Grad...)
	changed.Grad[1].Offset = math.Nextafter(.5, 1)
	r.paint(canvas.Paint{Gradient: &changed}, false, canvas.Identity)
	if len(r.functionCache) != functions+1 {
		t.Fatal("色标位置的单ULP差被合并，或未复用独立分段函数")
	}
	if r.err != nil {
		t.Fatal(r.err)
	}
}

func TestFontProgramCacheRequiresExactBytesAndStreamSemantics(t *testing.T) {
	r, err := New(t.Context(), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("font program")
	first := r.fontProgram(data, "Type1C", false)
	if second := r.fontProgram(bytes.Clone(data), "Type1C", false); first != second {
		t.Fatal("相同字体程序未复用")
	}
	for _, other := range []types.IndirectRef{
		r.fontProgram([]byte("font program!"), "Type1C", false),
		r.fontProgram(data, "CIDFontType0C", false),
		r.fontProgram(data, "", true),
	} {
		if other == first {
			t.Fatal("不同程序或字体流语义被错误合并")
		}
	}
}
