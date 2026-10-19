package render

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"sync"
	"testing"
)

// TestEncodedImageWeightUsesHeaderOnly 回归：计算缓存权重只读取图片头，
// 不应触发完整解码，否则大图会在入缓存时就全部解码，失去懒加载意义。
func TestEncodedImageWeightUsesHeaderOnly(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 6)), nil); err != nil {
		t.Fatal(err)
	}
	lazy := newEncodedImage("jpeg", buf.Bytes())
	weight := lazy.Weight()
	if weight <= int64(len(buf.Bytes())) {
		t.Fatalf("weight = %d, 期望大于编码字节 %d", weight, len(buf.Bytes()))
	}
	if lazy.img != nil {
		t.Fatal("weight() 不应触发完整解码")
	}

	// Bounds/ColorModel 只读图片头，不应触发完整解码。
	if lazy.Bounds().Empty() {
		t.Fatal("Bounds() 返回空矩形")
	}
	if lazy.img != nil {
		t.Fatal("Bounds() 不应触发完整解码")
	}
	if lazy.ColorModel() == nil {
		t.Fatal("ColorModel() 为空")
	}
	if lazy.img != nil {
		t.Fatal("ColorModel() 不应触发完整解码")
	}

	// 真正需要像素时才解码，并缓存结果。
	if lazy.At(0, 0) == nil {
		t.Fatal("At() 失败")
	}
	if lazy.img == nil {
		t.Fatal("At() 应触发解码并缓存")
	}
}

func TestEncodedImageInvalidDataReturnsSafeFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"unknown format", []byte("not an image")},
		{"truncated PNG", []byte("\x89PNG\r\n\x1a\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lazy := NewEncodedImage("png", tc.data)
			if got := lazy.Bounds(); !got.Empty() {
				t.Errorf("Bounds = %v, want empty", got)
			}
			if lazy.ColorModel() != color.RGBAModel {
				t.Error("invalid image should fall back to RGBA model")
			}
			wantWeight := max(int64(len(tc.data)), 1)
			if got := lazy.Weight(); got != wantWeight {
				t.Errorf("Weight = %d, want %d", got, wantWeight)
			}
			img, err := lazy.Image()
			if img != nil || err == nil {
				t.Fatalf("Image = %v, %v, want nil image and decode error", img, err)
			}
			if again, againErr := lazy.Image(); again != nil || againErr != err {
				t.Error("decode failure should be cached")
			}
			if got := color.RGBAModel.Convert(lazy.At(0, 0)); got != (color.RGBA{}) {
				t.Errorf("At = %v, want transparent", got)
			}
		})
	}
}

// 图片头可读而像素数据缺失时，布局和缓存预算仍可使用头信息；
// 解码失败不能使后续 Bounds/ColorModel/Weight 退化。
func TestEncodedImageKeepsHeaderAfterPixelDecodeFailure(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatal(err)
	}
	// PNG 签名及完整 IHDR 块，不包含 IDAT 像素数据。
	header := data.Bytes()[:33]
	lazy := NewEncodedImage("png", header)
	for range 2 {
		if got := lazy.Bounds(); got != image.Rect(0, 0, 8, 6) {
			t.Errorf("Bounds = %v, want 8x6", got)
		}
		if got := lazy.ColorModel(); got != color.NRGBAModel {
			t.Errorf("ColorModel = %v, want NRGBA", got)
		}
		if got, want := lazy.Weight(), int64(len(header)+8*6*4); got != want {
			t.Errorf("Weight = %d, want %d", got, want)
		}
		if img, err := lazy.Image(); img != nil || err == nil {
			t.Fatalf("Image = %v, %v, want decode failure", img, err)
		}
		if got := color.RGBAModel.Convert(lazy.At(3, 2)); got != (color.RGBA{}) {
			t.Errorf("At = %v, want transparent", got)
		}
	}
}

func TestEncodedImageConcurrentHeaderAndPixelAccess(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	wantColor := color.NRGBA{R: 200, G: 100, B: 50, A: 128}
	source.SetNRGBA(3, 2, wantColor)
	var data bytes.Buffer
	if err := png.Encode(&data, source); err != nil {
		t.Fatal(err)
	}
	lazy := NewEncodedImage("png", data.Bytes())
	const workers = 16
	results := make([]image.Image, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if got := lazy.Bounds(); got != source.Bounds() {
				t.Errorf("Bounds = %v, want %v", got, source.Bounds())
			}
			if got := lazy.ColorModel(); got != color.NRGBAModel {
				t.Errorf("ColorModel = %v, want NRGBA", got)
			}
			if got, want := lazy.Weight(), int64(data.Len()+8*6*4); got != want {
				t.Errorf("Weight = %d, want %d", got, want)
			}
			if got := color.NRGBAModel.Convert(lazy.At(3, 2)); got != wantColor {
				t.Errorf("At = %v, want %v", got, wantColor)
			}
			img, err := lazy.Image()
			if err != nil {
				t.Error(err)
			}
			results[i] = img
		}()
	}
	close(start)
	wg.Wait()
	for i, img := range results {
		if img == nil || img != results[0] {
			t.Errorf("worker %d did not reuse decoded image", i)
		}
	}
}
