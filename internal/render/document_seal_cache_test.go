package render

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/renderstate"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/spec"
	"golang.org/x/image/font/gofont/goregular"
)

func TestClippedOFDSealCachesFullRasterAndChargesEveryPage(t *testing.T) {
	doc := &Document{}
	t.Cleanup(doc.Close)
	data := concurrentSealData(t)
	entry, err := doc.sealDocument(data)
	if err != nil || entry == nil {
		t.Fatalf("读取印章：%v", err)
	}
	page, err := entry.ofd.Documents[0].GetPage(0)
	if err != nil {
		t.Fatal(err)
	}
	// 先登记字形，避免把首次字体映射变更混入缓存命中计数。
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	entry.doc.fonts.RegisterPageGlyphs(entry.doc.Document, page, lease.Content())
	box := spec.StBox{Width: 10, Height: 10}
	var allocations atomic.Int32
	original := drawing.RegisterOffscreenSurfaceFactory(nil)
	drawing.RegisterOffscreenSurfaceFactory(original)
	setOffscreenSurfaceFactory(t, func(w, h float64, r geom.Resolution) OffscreenSurface {
		allocations.Add(1)
		return original(w, h, r)
	})
	var workers sync.WaitGroup
	for i := range 6 {
		workers.Go(func() {
			ctx := newCanvasBackend(canvas.NewContext(canvas.New(30, 30)))
			info := &parser.SealInfo{SealData: &parser.SealData{FileType: "ofd", Data: data}, StampAnnot: &spec.StampAnnot{
				Boundary: spec.StBox{X: float64(i), Y: 1, Width: 10, Height: 10},
				Clip:     spec.StBox{X: float64(i%2) * 5, Width: 5, Height: 10},
			}}
			var budget renderBudget
			budget.reset()
			if err := doc.sealWithBudget(ctx, info, spec.StBox{Width: 30, Height: 30}, &budget); err != nil {
				t.Error(err)
			}
			if budget.Usage().OffscreenPixels == 0 {
				t.Error("印章缓存命中没有计入页面离屏用量")
			}
		})
	}
	workers.Wait()
	if allocations.Load() != 1 || entry.rasters.Len() != 1 {
		t.Fatalf("位置和 Clip 不同的同一印章重复栅格化：%d 次", allocations.Load())
	}
	var exhausted renderBudget
	exhausted.reset()
	exhausted.AllowOffscreenPixels(renderstate.MaxOffscreenPixels, 1, 25.4)
	if entry.rasterPage(page, box, box, &exhausted) != nil || allocations.Load() != 1 {
		t.Fatal("印章缓存绕过已耗尽的页面预算")
	}
	// 不同实际分辨率必须分别绘制，不能复用像素不足的位图。
	entry.rasterPage(page, box, spec.StBox{Width: 20, Height: 20}, nil)
	if allocations.Load() != 2 || entry.rasters.Weight() > sealRasterCacheBytes {
		t.Fatal("印章缓存没有区分分辨率或超过字节上限")
	}
	if err := RegisterFallbackFont(goregular.TTF, t.Name(), FontRegular); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterFallbackFont(t.Name()); err != nil {
			t.Error(err)
		}
	})
	if err := entry.doc.UseFallbackFont(t.Name()); err != nil {
		t.Fatal(err)
	}
	entry.rasterPage(page, box, box, nil)
	if allocations.Load() != 3 || entry.rasters.Len() != 1 {
		t.Fatal("字体版本变化后复用了旧印章位图，或仍保留旧版本缓存")
	}
}
