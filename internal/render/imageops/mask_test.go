package imageops

import (
	"bytes"
	"image"
	"image/color"
	"runtime"
	"testing"
)

func applyImageMaskReference(img image.Image, mask *image.Alpha) *image.NRGBA {
	bounds := img.Bounds()
	maskMin := mask.Rect.Min
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c, _ := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			ma := mask.AlphaAt(x-bounds.Min.X+maskMin.X, y-bounds.Min.Y+maskMin.Y).A
			c.A = uint8(int(c.A) * int(ma) / 255)
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

func requireNRGBA(t *testing.T, img image.Image) *image.NRGBA {
	t.Helper()
	result, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("合成结果类型=%T，期望 *image.NRGBA", img)
	}
	return result
}

func TestImageMaskAlphaMatchesSeparatePasses(t *testing.T) {
	for name, img := range alphaTestImages(t) {
		t.Run(name, func(t *testing.T) {
			bounds := img.Bounds()
			mask := image.NewAlpha(image.Rect(7, 9, 7+bounds.Dx(), 9+bounds.Dy()))
			for i := range mask.Pix {
				mask.Pix[i] = uint8(i * 37)
			}
			before := referenceImageAlpha(img, 255)
			for _, alpha := range []uint8{0, 1, 51, 128, 254, 255} {
				want := referenceImageAlpha(applyImageMaskReference(img, mask), alpha)
				got := requireNRGBA(t, ApplyMaskAlpha(img, mask, alpha))
				if got.Rect != want.Rect || !bytes.Equal(got.Pix, want.Pix) {
					t.Fatalf("合并掩码与Alpha=%d后与独立计算不一致", alpha)
				}
			}
			if !bytes.Equal(before.Pix, referenceImageAlpha(img, 255).Pix) {
				t.Fatal("修改了共享源图")
			}
		})
	}
}

// 同时验证真实并行入口和手工分块，避免仅测试串行的行处理函数。
func TestApplyImageMaskParallelMatchesReference(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("当前环境只有一个可用执行线程，无法验证并行分支")
	}
	width, height := 520, 510
	if width*height <= maskParallelThreshold || maskRowWorkers(width, height) <= 1 {
		t.Fatal("测试图片未触发并行掩码处理")
	}
	imgMin, maskMin := image.Point{X: 2, Y: 5}, image.Point{X: 1, Y: 1}
	src := image.NewNRGBA(image.Rect(imgMin.X, imgMin.Y, imgMin.X+width, imgMin.Y+height))
	for i := range src.Pix {
		src.Pix[i] = uint8(i*31 + 7)
	}
	mask := image.NewAlpha(image.Rect(maskMin.X, maskMin.Y, maskMin.X+width, maskMin.Y+height))
	for i := range mask.Pix {
		mask.Pix[i] = uint8(i*17 + 3)
	}
	want := applyImageMaskReference(src, mask)
	got := requireNRGBA(t, ApplyMaskAlpha(src, mask, 255))
	if !bytes.Equal(want.Pix, got.Pix) {
		t.Fatal("真实并行合成结果与参考实现不一致")
	}
	chunked := image.NewNRGBA(src.Rect)
	rows := src.Rect.Dy() / 3
	for lo := 0; lo < src.Rect.Dy(); lo += rows {
		hi := min(lo+rows, src.Rect.Dy())
		maskNRGBARowsAlpha(src, mask, chunked, imgMin, maskMin, lo, hi, 255)
	}
	if !bytes.Equal(want.Pix, chunked.Pix) {
		t.Fatal("分块合成结果与参考实现不一致")
	}
	// 预乘格式和色度子采样格式也必须经过真实并行入口验证。
	rgba := image.NewRGBA(src.Rect)
	ycc := image.NewYCbCr(src.Rect, image.YCbCrSubsampleRatio420)
	for i := range rgba.Pix {
		rgba.Pix[i] = uint8(i*13 + 1)
	}
	for i := range ycc.Y {
		ycc.Y[i] = uint8(i * 17)
	}
	for i := range ycc.Cb {
		ycc.Cb[i], ycc.Cr[i] = uint8(i*31), uint8(i*43)
	}
	for _, img := range []image.Image{rgba, ycc} {
		want := referenceImageAlpha(applyImageMaskReference(img, mask), 128)
		got := requireNRGBA(t, ApplyMaskAlpha(img, mask, 128))
		if !bytes.Equal(want.Pix, got.Pix) {
			t.Fatalf("%T 并行掩码不符合标准库参考结果", img)
		}
	}
}

func TestMaskRowWorkersFallsBackToSerial(t *testing.T) {
	if got := maskRowWorkers(16, 16); got != 1 {
		t.Errorf("小图应串行，得到 %d", got)
	}
	if got := maskRowWorkers(1024, 1024); got < 1 {
		t.Errorf("并发数至少为 1，得到 %d", got)
	}
	if got := maskRowWorkers(0, 0); got != 1 {
		t.Errorf("空图应串行，得到 %d", got)
	}
}
