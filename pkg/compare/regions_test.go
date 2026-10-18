package compare

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
)

func TestVisualRegionsSeparateDistantChangesAndKeepPixelCounts(t *testing.T) {
	a, b := image.NewRGBA(image.Rect(0, 0, 180, 120)), image.NewRGBA(image.Rect(0, 0, 180, 120))
	for _, box := range []image.Rectangle{image.Rect(10, 10, 16, 16), image.Rect(130, 80, 139, 89)} {
		for y := box.Min.Y; y < box.Max.Y; y++ {
			for x := box.Min.X; x < box.Max.X; x++ {
				b.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
			}
		}
	}
	v, err := compareImages(t.Context(), a, b, Options{DPI: 72, MaxPagePixels: 100000})
	if err != nil {
		t.Fatal(err)
	}
	if v.RegionCount != 2 || len(v.Regions) != 2 || v.ChangedPixels != 36+81 {
		t.Fatalf("错误的区域或像素统计: %+v", v)
	}
	for i, want := range [][4]int{{9, 9, 18, 18}, {129, 78, 141, 90}} {
		if v.Regions[i].Bounds != want {
			t.Fatalf("区域 %d: %v，期望 %v", i, v.Regions[i].Bounds, want)
		}
	}
}

func TestRegionNoiseFilterDoesNotChangeRawVisualResult(t *testing.T) {
	a, b := image.NewRGBA(image.Rect(0, 0, 60, 60)), image.NewRGBA(image.Rect(0, 0, 60, 60))
	b.SetRGBA(5, 5, color.RGBA{A: 255})
	for y := 30; y < 32; y++ {
		for x := 30; x < 32; x++ {
			b.SetRGBA(x, y, color.RGBA{A: 255})
		}
	}
	v, err := compareImages(t.Context(), a, b, Options{DPI: 72, MaxPagePixels: 10000, MinRegionPixels: 2})
	if err != nil {
		t.Fatal(err)
	}
	if v.ChangedPixels != 5 || v.IgnoredRegionPixels != 1 || v.RegionCount != 1 || v.Regions[0].ChangedPixels != 4 {
		t.Fatalf("过滤改变了原始差异或漏掉了有效区域: %+v", v)
	}
}

func TestVisualRegionDetailsAreBoundedAndDeterministic(t *testing.T) {
	g := newRegionGrid(240, 240, 72)
	for y := 0; y < 240; y += 6 {
		for x := 0; x < 240; x += 6 {
			g.add(x, y)
		}
	}
	regions, count, ignored, err := g.regions(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1600 || len(regions) != maxVisualRegions || ignored != 0 {
		t.Fatalf("%d %d %d", count, len(regions), ignored)
	}
	if regions[0].Bounds != [4]int{0, 0, 3, 3} || regions[199].Bounds != [4]int{234, 24, 237, 27} {
		t.Fatal("同大小区域的保留顺序不稳定")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, _, err := g.regions(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestAddedPageAndDifferentPageSizesHaveRegions(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 10, 10))
	b := image.NewRGBA(image.Rect(0, 0, 20, 10))
	v, err := compareImages(t.Context(), a, b, Options{DPI: 72, MaxPagePixels: 1000})
	if err != nil || v.ChangedPixels != 100 || v.RegionCount != 1 {
		t.Fatalf("%+v %v", v, err)
	}
	v, err = compareImages(t.Context(), nil, b, Options{DPI: 72, MaxPagePixels: 1000})
	if err != nil || v.ChangedPixels != 200 || v.RegionCount != 1 || v.BeforePNG != "" {
		t.Fatalf("%+v %v", v, err)
	}
}
