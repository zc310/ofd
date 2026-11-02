package imageasset

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

func smallPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestDecodeRejectsOversizedHeaderBeforeAllocatingPixels(t *testing.T) {
	data := smallPNG(t)
	binary.BigEndian.PutUint32(data[16:20], 1000000)
	binary.BigEndian.PutUint32(data[20:24], 1000000)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err := Decode(data); err == nil {
		t.Fatal("未拒绝超大像素尺寸")
	}
	lazy := NewEncodedImage("png", data)
	if _, err := lazy.Image(); err == nil || lazy.img != nil {
		t.Fatal("直接访问延迟解码包装绕过了尺寸上限")
	}
	for _, size := range [][2]int{{0, 1}, {-1, 1}, {int(^uint(0) >> 1), 2}} {
		if CheckSize(size[0], size[1]) == nil {
			t.Fatalf("未拒绝无效尺寸 %v", size)
		}
	}
}

func TestImageDecodeBudgetChargesOnlyCacheMissAndDoesNotPoisonCache(t *testing.T) {
	data := smallPNG(t)
	var store Store
	defer store.Close()
	reader := resourceReader(func(string) ([]byte, error) { return data, nil })
	loads, pixels := 0, 0
	options := LoadOptions{AllowPixels: func(w, h int) bool { loads++; pixels += w * h; return false }}
	if _, err := store.LoadWithOptions("a.png", "PNG", reader, nil, options); !errors.Is(err, ErrPixelBudget) {
		t.Fatalf("预算拒绝=%v", err)
	}
	options.AllowPixels = func(w, h int) bool { loads++; pixels += w * h; return true }
	for range 2 {
		if _, err := store.LoadWithOptions("a.png", "PNG", reader, nil, options); err != nil {
			t.Fatal(err)
		}
	}
	if loads != 2 || pixels != 96 {
		t.Fatalf("命中缓存重复扣费或预算失败污染缓存：loads=%d pixels=%d", loads, pixels)
	}
}

func TestSVGRasterUsesTargetPixelsAndResolutionSpecificCache(t *testing.T) {
	var store Store
	defer store.Close()
	rasterized, charged := 0, 0
	parse := func([]byte) (SVGScene, error) {
		return fakeScene{raster: func(r geom.Resolution) image.Image {
			rasterized++
			side := int(10*r.DPMM() + .5)
			return image.NewRGBA(image.Rect(0, 0, side, side))
		}}, nil
	}
	reader := resourceReader(func(string) ([]byte, error) { return []byte("svg"), nil })
	for _, side := range []int{100, 200, 100} {
		img, err := store.LoadWithOptions("a.svg", "SVG", reader, parse, LoadOptions{
			TargetWidth: float64(side), TargetHeight: float64(side),
			AllowPixels: func(w, h int) bool { charged += w * h; return true },
		})
		if err != nil || img.Bounds().Dx() != side {
			t.Fatalf("目标像素=%d，实际=%v err=%v", side, img, err)
		}
	}
	if rasterized != 2 || charged != 50000 {
		t.Fatalf("SVG 缓存或预算未区分目标尺寸：raster=%d pixels=%d", rasterized, charged)
	}
}

func TestSVGBudgetRejectsBeforeRasterization(t *testing.T) {
	var store Store
	defer store.Close()
	parse := func([]byte) (SVGScene, error) {
		return fakeScene{raster: func(geom.Resolution) image.Image { t.Fatal("拒绝预算后仍然分配 SVG 像素"); return nil }}, nil
	}
	_, err := store.LoadWithOptions("a.svg", "SVG", resourceReader(func(string) ([]byte, error) { return nil, nil }), parse, LoadOptions{AllowPixels: func(int, int) bool { return false }})
	if !errors.Is(err, ErrPixelBudget) {
		t.Fatalf("SVG 预算拒绝=%v", err)
	}
}

// 不分配真实大图，用尺寸模型验证旁路缓存有界且不冲掉普通 LRU。
type sizedImage struct{ side int }

func (s sizedImage) Bounds() image.Rectangle { return image.Rect(0, 0, s.side, s.side) }
func (s sizedImage) ColorModel() color.Model { return color.NRGBAModel }
func (s sizedImage) At(int, int) color.Color { return color.NRGBA{} }

func TestOversizedImageReusedWithoutEvictingSmallImages(t *testing.T) {
	var store Store
	defer store.Close()
	load := func(side int) func() (image.Image, error) {
		return func() (image.Image, error) { return sizedImage{side}, nil }
	}
	if _, err := store.Image("small", load(8)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := store.Image("large", load(5000)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Image("large", func() (image.Image, error) { t.Error("大图重复加载"); return sizedImage{5000}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.images.Get("small"); !ok {
		t.Fatal("大图冲掉了普通缓存")
	}
	if _, err := store.Image("next", load(6000)); err != nil {
		t.Fatal(err)
	}
	if store.largeKey != "next" || store.images.Len() != 1 {
		t.Fatal("大图旁路没有限制为一个条目")
	}
	if _, err := store.Image("too-large", load(10000)); err != nil {
		t.Fatal(err)
	}
	if store.largeKey != "next" {
		t.Fatal("超过旁路上限的条目替换了可复用大图")
	}
	store.Close()
	if store.largeImage != nil {
		t.Fatal("关闭后仍持有大图")
	}
}

func TestBadImageNegativeCacheIsBoundedAndCoalescesConcurrentLoads(t *testing.T) {
	var store Store
	defer store.Close()
	var reads atomic.Int32
	reader := resourceReader(func(string) ([]byte, error) { reads.Add(1); return []byte("broken"), nil })
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if _, err := store.Load("bad.png", "PNG", reader, nil); err == nil {
				t.Error("损坏资源未返回错误")
			}
		})
	}
	wg.Wait()
	if reads.Load() != 1 {
		t.Fatalf("损坏资源读取 %d 次", reads.Load())
	}
	for i := range imageCacheCapacity + 10 {
		if _, err := store.Load(spec.StLoc(fmt.Sprintf("bad-%d", i)), "PNG", reader, nil); err == nil {
			t.Fatal("损坏资源未返回错误")
		}
	}
	if store.failures.Len() > imageCacheCapacity {
		t.Fatal("负缓存无界增长")
	}
	store.Close()
	if store.failures != nil {
		t.Fatal("关闭后负缓存未释放")
	}
}

func TestTransientImageErrorsAreNotNegativeCached(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, ErrPixelBudget, errors.New("临时读取失败")} {
		var store Store
		loads := 0
		for range 2 {
			_, err := store.Image("key", func() (image.Image, error) { loads++; return nil, failure })
			if !errors.Is(err, failure) {
				t.Fatalf("错误丢失：%v", err)
			}
		}
		if loads != 2 {
			t.Fatalf("临时失败被缓存：%v", failure)
		}
		store.Close()
	}
}
