package pdf2ofd

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"testing"
)

func TestPatchGridStepsRefinesCurvatureAndBilinearColour(t *testing.T) {
	flat := func(u, v float64) (float64, float64, pdfColor) {
		return u * 100, v * 100, pdfColor{r: 100}
	}
	if got := patchGridSteps(flat, nil); got != minPatchTessellationSteps {
		t.Fatalf("平坦补丁不应过度细分：steps=%d", got)
	}
	for _, sample := range []patchSample{
		func(u, v float64) (float64, float64, pdfColor) {
			// 中点恰好在弦上的 S 形边界也必须加密。
			return u * 100, v*100 + 100*u*(1-u)*(u-.5), pdfColor{r: 100}
		},
		func(u, v float64) (float64, float64, pdfColor) {
			return u * 100, v * 100, patchCornerColor([4]pdfColor{{}, {r: 255}, {}, {r: 255}}, u, v)
		},
	} {
		if got := patchGridSteps(sample, nil); got <= minPatchTessellationSteps {
			t.Fatalf("曲率或颜色变化大的补丁没有加密：steps=%d", got)
		}
	}
}

func TestMeshRasterUsesSubpixelCoverageWithoutFillingRealHoles(t *testing.T) {
	var triangles []meshTriangleMM
	red := pdfColor{r: 180, g: 20, b: 30}
	addRect := func(x0, x1 float64) {
		triangles = append(triangles,
			meshTriangleMM{x: [3]float64{x0, x1, x1}, y: [3]float64{0, 0, 2}, c: [3]pdfColor{red, red, red}},
			meshTriangleMM{x: [3]float64{x0, x1, x0}, y: [3]float64{0, 2, 2}, c: [3]pdfColor{red, red, red}},
		)
	}
	// 第一处窄缝穿过输出像素中心，模拟两个曲面分别近似后的亚像素裂缝。
	// 第二处保留一个足够大的真实空洞，不能通过“补透明像素”掩盖。
	seam := 6.5 / meshRasterPixelsPerMM
	addRect(0, seam-.001)
	addRect(seam+.001, 2)
	addRect(3, 4)
	data := rasterizeMeshTriangles(triangles, 0, 0, 4, 2)
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(6, 6).RGBA()
	if a != 65535 || r != 180*257 || g != 20*257 || b != 30*257 {
		t.Fatalf("亚像素裂缝成为白点或降采样污染颜色：%d,%d,%d,%d", r, g, b, a)
	}
	if _, _, _, a := img.At(15, 6).RGBA(); a != 0 {
		t.Fatalf("真实网格空洞被错误填充：alpha=%d", a)
	}
}

func TestPatchGridStepsPreservesCancellationAndOperationBudget(t *testing.T) {
	opts, err := (Options{Limits: Limits{MaxOperations: 8}}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	sample := func(u, v float64) (float64, float64, pdfColor) {
		return u, v, pdfColor{}
	}
	for i, testContext := range []context.Context{t.Context(), ctx} {
		want := []error{ErrLimitExceeded, context.Canceled}[i]
		control := &conversionControl{ctx: testContext, options: opts}
		if steps := patchGridSteps(sample, control); steps != 0 || !errors.Is(control.err, want) {
			t.Fatalf("steps=%d err=%v，期望 %v", steps, control.err, want)
		}
	}
}

func TestMeshRasterCountsSubpixelBufferAndPreservesCancellation(t *testing.T) {
	triangles := []meshTriangleMM{{
		x: [3]float64{0, 1, 0},
		y: [3]float64{0, 0, 1},
		c: [3]pdfColor{{r: 180}, {r: 180}, {r: 180}},
	}}
	// 输出为 6×6，4×4 子像素缓冲为 24×24；后者也必须计入图片预算。
	for _, limits := range []Limits{
		{MaxImagePixels: 100},
		{MaxTotalImagePixels: 100},
		{MaxOperations: 1},
	} {
		opts, err := (Options{Limits: limits}).Normalize()
		if err != nil {
			t.Fatal(err)
		}
		control := &conversionControl{ctx: t.Context(), options: opts}
		if got := rasterizeMeshTriangles(triangles, 0, 0, 1, 1, control); got != nil || !errors.Is(control.err, ErrLimitExceeded) {
			t.Fatalf("子像素栅格未遵守预算 %+v：bytes=%d err=%v", limits, len(got), control.err)
		}
	}
	opts, err := (Options{}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	control := &conversionControl{ctx: ctx, options: opts}
	if got := rasterizeMeshTriangles(triangles, 0, 0, 1, 1, control); got != nil || !errors.Is(control.err, context.Canceled) {
		t.Fatalf("取消未原样保留：bytes=%d err=%v", len(got), control.err)
	}
}

func TestPatchMeshRejectsTriangleOverflowInsteadOfDroppingTail(t *testing.T) {
	patch := buildAdobeTensorPatchStream([4][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}})
	count := maxMeshTriangles/(2*minPatchTessellationSteps*minPatchTessellationSteps) + 1
	data := bytes.Repeat(patch, count)
	shading := &pdfShading{
		colorSpace: deviceRGBSpace,
		mesh: &pdfMeshData{
			kind: 7, bitsPerCoordinate: 32, bitsPerComponent: 8, bitsPerFlag: 8,
			decode: []float64{0, 100, 0, 100, 0, 1, 0, 1, 0, 1}, data: data,
		},
	}
	opts, err := (Options{}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	control := &conversionControl{ctx: t.Context(), options: opts}
	got := decodePatchMesh(shading.mesh, &meshBitReader{data: data, control: control}, shading)
	var limit *LimitError
	if got != nil || !errors.As(control.err, &limit) || limit.Resource != "mesh_triangles" {
		t.Fatalf("超限时不能保留残缺网格：triangles=%d err=%v", len(got), control.err)
	}
}
