package image

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

func TestImageMetadataDoesNotDecodePixels(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			var encoded bytes.Buffer
			var img *Image
			var err error
			if format == "jpeg" {
				err = jpeg.Encode(&encoded, source, nil)
				if err == nil {
					img, err = NewJPEGImage(&encoded)
				}
			} else {
				err = png.Encode(&encoded, source)
				if err == nil {
					img, err = NewPNGImage(&encoded)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds() != source.Bounds() || img.ColorModel() == nil {
				t.Fatal("图片配置不可用")
			}
			if img.image != nil {
				t.Fatal("构造和读取配置不应解码像素")
			}
			first, err := img.Image()
			if err != nil {
				t.Fatal(err)
			}
			second, err := img.Image()
			if err != nil || first != second || first.Bounds() != source.Bounds() {
				t.Fatalf("解码结果未复用：%T / %T，错误=%v", first, second, err)
			}
		})
	}
}

func TestImageConcurrentPixelReadsDecodeAndMaskExactlyOnce(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 240, G: 80, B: 40, A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{R: 40, G: 160, B: 240, A: 255})
	maskPixels := image.NewGray(source.Bounds())
	maskPixels.SetGray(0, 0, color.Gray{Y: 128})
	maskPixels.SetGray(1, 0, color.Gray{Y: 255})
	var sourceCalls, maskCalls atomic.Int32
	mask := &Image{Config: image.Config{ColorModel: color.GrayModel, Width: 2, Height: 1}, decode: func(io.Reader) (image.Image, error) {
		maskCalls.Add(1)
		return maskPixels, nil
	}}
	img := &Image{Config: image.Config{ColorModel: color.NRGBAModel, Width: 2, Height: 1}, Mask: mask, decode: func(io.Reader) (image.Image, error) {
		sourceCalls.Add(1)
		return source, nil
	}}
	const workers = 32
	results := make([]image.Image, workers)
	start := make(chan struct{})
	var group sync.WaitGroup
	for worker := range workers {
		group.Go(func() {
			<-start
			if worker%2 == 0 {
				if got, ok := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); !ok || got.A != 128 {
					t.Errorf("读到未应用掩码的像素：%v", got)
				}
			} else if img.Opaque() {
				t.Error("带掩码图片被误判为不透明")
			}
			var err error
			results[worker], err = img.Image()
			if err != nil {
				t.Error(err)
			}
			if _, err := mask.Image(); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	group.Wait()
	if sourceCalls.Load() != 1 || maskCalls.Load() != 1 {
		t.Fatalf("原图/掩码解码次数=%d/%d，期望1/1", sourceCalls.Load(), maskCalls.Load())
	}
	for worker, result := range results {
		if result == nil || result != results[0] {
			t.Fatalf("并发读取%d未复用完整解码结果", worker)
		}
		if got, ok := color.NRGBAModel.Convert(result.At(1, 0)).(color.NRGBA); !ok || got != source.NRGBAAt(1, 0) {
			t.Fatalf("不透明像素被改变：%v", got)
		}
	}
}

func TestImageCachesDecodeErrorAndBlackFallback(t *testing.T) {
	wantErr := errors.New("测试解码失败")
	calls := 0
	img := &Image{decode: func(io.Reader) (image.Image, error) {
		calls++
		return nil, wantErr
	}}
	for range 3 {
		pixels, err := img.Image()
		if !errors.Is(err, wantErr) {
			t.Fatalf("未保留解码错误：%v", err)
		}
		if pixels == nil || pixels.At(0, 0) != color.Black {
			t.Fatal("解码失败应保留黑色像素回退")
		}
	}
	if calls != 1 {
		t.Fatalf("失败后重复解码%d次", calls)
	}
}
