package imageasset

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func awaitLoad(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("图片资源操作被阻塞")
	}
}

type fakeScene struct {
	raster func(geom.Resolution) image.Image
}

func (fakeScene) Width() float64                     { return 10 }
func (fakeScene) Height() float64                    { return 10 }
func (fakeScene) RenderVector(any, geom.Matrix) bool { return true }
func (s fakeScene) Rasterize(dpi geom.Resolution) image.Image {
	if s.raster != nil {
		return s.raster(dpi)
	}
	return nil
}

func TestImageCacheWeightCountsLazyImageBytesAndPixels(t *testing.T) {
	var encoded bytes.Buffer
	source := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 8), B: uint8((x + y) * 3), A: 255})
		}
	}
	if err := jpeg.Encode(&encoded, source, nil); err != nil {
		t.Fatal(err)
	}
	lazy := NewEncodedImage("jpeg", encoded.Bytes())
	weight := imageCacheWeight(lazy)
	if weight <= int64(len(encoded.Bytes())) {
		t.Fatalf("imageCacheWeight = %d, 期望大于压缩字节 %d", weight, len(encoded.Bytes()))
	}
}

func TestImageCacheEvictsByByteWeight(t *testing.T) {
	const maxWeight = int64(1 << 20)
	store := &Store{images: cache.NewWeightedLRU[string, image.Image](imageCacheCapacity, maxWeight, nil)}
	defer store.Close()
	for i := range 4 {
		// 每张 512x512 RGBA 约 1MiB，必须通过真正的加载入口按字节淘汰。
		if _, err := store.Image(fmt.Sprintf("img-%d", i), func() (image.Image, error) {
			return image.NewRGBA(image.Rect(0, 0, 512, 512)), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if weight := store.images.Weight(); weight > maxWeight {
		t.Fatalf("图片缓存权重 = %d, 期望 <= %d", weight, maxWeight)
	}
	if length := store.images.Len(); length > 1 {
		t.Fatalf("图片缓存条目 = %d, 期望 <= 1", length)
	}
	if len(store.keyLocks) != 0 {
		t.Fatal("加载结束后保留了无用的键锁")
	}
}

func TestPreparedImagesShareResourceByteBudget(t *testing.T) {
	store := &Store{images: cache.NewWeightedLRU[string, image.Image](imageCacheCapacity, 64, nil)}
	defer store.Close()
	var loads int
	prepared := func() (image.Image, error) {
		loads++
		return image.NewNRGBA(image.Rect(0, 0, 4, 4)), nil
	}
	first, err := store.Image("\x00seal-image:hash", prepared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Image("resource.png", prepared); err != nil {
		t.Fatal(err)
	}
	again, err := store.Image("\x00seal-image:hash", prepared)
	if err != nil || again == first || loads != 3 || store.images.Len() != 1 || store.images.Weight() != 64 {
		t.Fatalf("预处理图片未与普通资源共用字节预算：loads=%d err=%v", loads, err)
	}
}

func TestSVGCanvasWeightUsesSourceBytes(t *testing.T) {
	if got := svgCanvasWeight(nil); got != 1 {
		t.Fatalf("svgCanvasWeight(nil) = %d, 期望 1", got)
	}
	data := []byte("<svg/>")
	if got := svgCanvasWeight(data); got != int64(len(data)) {
		t.Fatalf("svgCanvasWeight = %d, 期望 %d", got, len(data))
	}
}

func TestSlowImageLoadDoesNotBlockOtherImagesOrRefillAfterClose(t *testing.T) {
	store := &Store{}
	defer store.Close()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if _, err := store.Image("hot", func() (image.Image, error) { return img, nil }); err != nil {
		t.Fatal(err)
	}
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	go func() {
		defer close(done)
		if _, err := store.Image("slow", func() (image.Image, error) {
			close(entered)
			<-resume
			return img, nil
		}); err != nil {
			t.Error(err)
		}
	}()
	defer func() { release(); <-done }()
	awaitLoad(t, entered)
	available := make(chan struct{})
	go func() {
		defer close(available)
		if got, err := store.Image("hot", func() (image.Image, error) {
			t.Error("缓存命中重复加载")
			return img, nil
		}); err != nil || got != img {
			t.Error("慢加载阻塞或破坏了其他缓存命中")
		}
		store.Close()
	}()
	awaitLoad(t, available)
	release()
	awaitLoad(t, done)
	if store.images != nil || len(store.keyLocks) != 0 {
		t.Fatal("关闭后在途图片解码重新填回缓存或保留加载锁")
	}
	if _, err := store.Image("hot", func() (image.Image, error) {
		t.Error("关闭后仍调用图片加载函数")
		return img, nil
	}); err == nil {
		t.Fatal("关闭后仍返回缓存图片")
	}
	if len(store.keyLocks) != 0 {
		t.Fatal("关闭后的访问保留了键锁")
	}
}

func TestCloseDuringSVGParseDoesNotRefillSceneCache(t *testing.T) {
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	parse := func([]byte) (SVGScene, error) {
		close(entered)
		<-resume
		return fakeScene{}, nil
	}
	store := &Store{}
	defer store.Close()
	go func() {
		defer close(done)
		if _, err := store.SVG("slow.svg", func() ([]byte, error) { return nil, nil }, parse); err != nil {
			t.Error(err)
		}
	}()
	defer func() { release(); <-done }()
	awaitLoad(t, entered)
	store.Close()
	release()
	awaitLoad(t, done)
	if store.svgCanvases != nil || len(store.keyLocks) != 0 {
		t.Fatal("关闭后在途 SVG 解析重新填充了场景缓存")
	}
}

type resourceReader func(string) ([]byte, error)

func (r resourceReader) Read(name string) ([]byte, error) { return r(name) }

func TestLoadNormalizesPathsAndCoalescesConcurrentDecoding(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int32
	source := resourceReader(func(name string) ([]byte, error) {
		if name != "Res/picture.png" {
			t.Errorf("资源路径未规范化：%q", name)
		}
		reads.Add(1)
		return encoded.Bytes(), nil
	})
	var store Store
	defer store.Close()
	results := make([]image.Image, 16)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			file := models.StLoc("Res/picture.png")
			if i%2 == 0 {
				file = "Res/unused/../picture.png"
			}
			var err error
			results[i], err = store.Load(file, "PNG", source, nil)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, result := range results {
		if result == nil || result != results[0] || result.Bounds() != image.Rect(0, 0, 4, 3) {
			t.Fatal("同一资源路径的并发加载未复用图片")
		}
	}
	if reads.Load() != 1 || len(store.keyLocks) != 0 {
		t.Fatalf("重复读资源或保留键锁：reads=%d locks=%d", reads.Load(), len(store.keyLocks))
	}
}

func TestSVGSceneAndRasterShareParsingWithoutLockCycle(t *testing.T) {
	var reads, parses, rasters atomic.Int32
	source := resourceReader(func(string) ([]byte, error) { reads.Add(1); return []byte("<svg/>"), nil })
	parse := func([]byte) (SVGScene, error) {
		parses.Add(1)
		return fakeScene{raster: func(dpi geom.Resolution) image.Image {
			if dpi != geom.DPI(96) {
				t.Errorf("SVG 资源位图回退 DPI=%v", dpi)
			}
			rasters.Add(1)
			return image.NewRGBA(image.Rect(0, 0, 4, 4))
		}}, nil
	}
	var store Store
	defer store.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(2)
			go func() {
				defer wg.Done()
				if _, err := store.Load("picture.SVG", "", source, parse); err != nil {
					t.Error(err)
				}
			}()
			go func() {
				defer wg.Done()
				if _, err := store.LoadSVG("picture.SVG", "", source, parse); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
	}()
	awaitLoad(t, done)
	if reads.Load() != 1 || parses.Load() != 1 || rasters.Load() != 1 || len(store.keyLocks) != 0 {
		t.Fatalf("场景和位图未共享加载：read=%d parse=%d raster=%d", reads.Load(), parses.Load(), rasters.Load())
	}
}

func TestLoadFailureCanRetryAndDoesNotRetainKeyLocks(t *testing.T) {
	var store Store
	defer store.Close()
	want := errors.New("read failed")
	if _, err := store.Load("image.png", "PNG", resourceReader(func(string) ([]byte, error) { return nil, want }), nil); !errors.Is(err, want) {
		t.Fatalf("未保留资源读取错误：%v", err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("image.png", "PNG", resourceReader(func(string) ([]byte, error) { return data.Bytes(), nil }), nil); err != nil {
		t.Fatalf("读取失败后不能重试：%v", err)
	}
	if len(store.keyLocks) != 0 {
		t.Fatal("失败或重试后保留了键锁")
	}
}
