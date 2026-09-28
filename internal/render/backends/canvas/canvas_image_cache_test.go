package canvas

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zc310/ofd/internal/render"
)

// EncodedImage 的 canvas 缓存过去是一个裸字段，构造与读取分两步：先读缓存、
// 未命中再构造、最后写回。多个页面并发渲染同一文档时，两步之间没有任何同步，
// 读取方可能看到写了一半的接口值（any 是「类型指针 + 数据指针」两个 word，
// 并发读写可以观察到类型来自新值、数据来自旧值的组合）。
//
// 竞态检测器下本用例在旧实现上必然报 DATA RACE；同时断言构造只发生一次，
// 这条断言不依赖竞态检测器，普通 go test 也能拦住重复构造。
func TestCanvasImageConcurrentAccessIsRaceFree(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"jpeg", encodeJPEG(t)},
		{"png", encodePNG(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lazy := render.NewEncodedImage(tc.name, tc.data)

			// 直接对 EncodedImage 施加并发压力：所有 goroutine 拿到同一个实例。
			const goroutines = 16
			var wg sync.WaitGroup
			start := make(chan struct{})
			results := make([]image.Image, goroutines)
			for i := 0; i < goroutines; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					results[i] = canvasImage(lazy)
				}(i)
			}
			close(start)
			wg.Wait()

			first := results[0]
			if first == nil {
				t.Fatal("canvasImage 返回 nil")
			}
			for i, got := range results {
				if got != first {
					t.Fatalf("goroutine %d 拿到不同对象 %T，期望复用同一 %T", i, got, first)
				}
			}
			// 复用必须来自缓存：连续两次调用返回同一实例。
			if again := canvasImage(lazy); again != first {
				t.Error("第二次调用没有命中 canvas 缓存")
			}
		})
	}
}

// CanvasImageOnce 必须把构造放进 sync.Once：并发调用只构造一次。
func TestCanvasImageOnceBuildsExactlyOnce(t *testing.T) {
	lazy := render.NewEncodedImage("png", encodePNG(t))

	var calls atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	got := make([]image.Image, 32)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i] = lazy.CanvasImageOnce(func() image.Image {
				calls.Add(1)
				// 故意做点工作，增大并发窗口
				img, err := png.Decode(bytes.NewReader(lazy.Data))
				if err != nil {
					t.Error(err)
					return nil
				}
				return img
			})
		}(i)
	}
	close(start)
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Errorf("build 被调用 %d 次，期望恰好 1 次", n)
	}
	for i, v := range got {
		if v == nil {
			t.Fatalf("goroutine %d 得到 nil", i)
		}
		if v != got[0] {
			t.Fatalf("goroutine %d 拿到不同实例", i)
		}
	}
}

// build 返回 nil 表示不走原字节内嵌快路径，该结果也必须被缓存，
// 否则每次访问都会重新解码。
func TestCanvasImageOnceCachesNegativeResult(t *testing.T) {
	lazy := render.NewEncodedImage("png", encodePNG(t))
	var calls atomic.Int64
	build := func() image.Image {
		calls.Add(1)
		return nil
	}
	for i := 0; i < 5; i++ {
		if got := lazy.CanvasImageOnce(build); got != nil {
			t.Fatalf("第 %d 次得到 %T，期望 nil", i, got)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("build 被调用 %d 次，期望 1 次（nil 结果也应缓存）", n)
	}
}

// 灰度 PNG 必须继续跳过原字节内嵌快路径：canvas 的 PDF 写入器固定按
// DeviceRGB 声明图像，灰度图走内嵌会错位。这条行为在改造后必须保持。
func TestCanvasImageSkipsGrayFastPath(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range gray.Pix {
		gray.Pix[i] = byte(i * 7)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, gray); err != nil {
		t.Fatal(err)
	}
	lazy := render.NewEncodedImage("png", buf.Bytes())
	if lazy.ColorModel() != color.GrayModel {
		t.Fatalf("样本颜色模型 = %v，期望灰度", lazy.ColorModel())
	}
	if got := canvasImage(lazy); got != image.Image(lazy) {
		t.Errorf("灰度图应回退到原始 EncodedImage，得到 %T", got)
	}
}

func encodeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 16, 12))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 5)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 16, 12))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 3)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
