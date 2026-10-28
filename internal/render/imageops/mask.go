package imageops

import (
	"image"
	"image/color"
	"runtime"
	"sync"
)

// ApplyMaskAlpha 一次合成源 Alpha、覆盖率掩码和对象 Alpha。掩码左上角对应原图
// 左上角，尺寸须覆盖原图；返回独立 NRGBA，不修改共享图片或掩码。
func ApplyMaskAlpha(img image.Image, mask *image.Alpha, alpha uint8) image.Image {
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	src := decodedImage(img)
	switch source := src.(type) {
	case *image.YCbCr:
		applyMaskYCbCr(source, mask, out, bounds.Min, mask.Rect.Min, alpha)
	case *image.NRGBA:
		applyMaskNRGBA(source, mask, out, bounds.Min, mask.Rect.Min, alpha)
	case *image.RGBA:
		applyMaskRGBA(source, mask, out, bounds.Min, mask.Rect.Min, alpha)
	default:
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := nrgbaAt(src, x, y)
				ma := mask.AlphaAt(x-bounds.Min.X+mask.Rect.Min.X, y-bounds.Min.Y+mask.Rect.Min.Y).A
				c.A = alphaMul(alphaMul(c.A, ma), alpha)
				out.SetNRGBA(x, y, c)
			}
		}
	}
	return out
}

func applyMaskYCbCr(src *image.YCbCr, mask *image.Alpha, out *image.NRGBA, imgMin, maskMin image.Point, alpha uint8) {
	width, height := src.Rect.Dx(), src.Rect.Dy()
	if workers := maskRowWorkers(width, height); workers > 1 {
		runMaskRows(workers, height, func(lo, hi int) {
			maskYCbCrRows(src, mask, out, imgMin, maskMin, lo, hi, alpha)
		})
		return
	}
	maskYCbCrRows(src, mask, out, imgMin, maskMin, 0, height, alpha)
}

func maskYCbCrRows(src *image.YCbCr, mask *image.Alpha, out *image.NRGBA, imgMin, maskMin image.Point, lo, hi int, alpha uint8) {
	width := src.Rect.Dx()
	maskX := src.Rect.Min.X - imgMin.X + maskMin.X
	for row := lo; row < hi; row++ {
		y := src.Rect.Min.Y + row
		yi := row * src.YStride
		oi := out.PixOffset(src.Rect.Min.X, y)
		mi := mask.PixOffset(maskX, y-imgMin.Y+maskMin.Y)
		for col := range width {
			// 使用标准库的绝对色度下标换算，保留非零原点和子图的子采样位置。
			x := src.Rect.Min.X + col
			cidx := src.COffset(x, y)
			r, g, b := color.YCbCrToRGB(src.Y[yi+col], src.Cb[cidx], src.Cr[cidx])
			out.Pix[oi+0], out.Pix[oi+1], out.Pix[oi+2] = r, g, b
			out.Pix[oi+3] = alphaMul(mask.Pix[mi], alpha)
			oi += 4
			mi++
		}
	}
}

const (
	maskParallelThreshold = 1 << 18
	maskParallelWorkers   = 4
)

// 小图保持串行，大图最多四个分块，避免与页面级并行叠加后过度订阅 CPU。
func maskRowWorkers(width, height int) int {
	workers := maskParallelWorkers
	if v := runtime.GOMAXPROCS(0); v < workers {
		workers = v
	}
	if workers <= 1 || width*height < maskParallelThreshold {
		return 1
	}
	return workers
}

// 各分块只写自己的行，输入图片与掩码始终只读。
func runMaskRows(workers, height int, fn func(lo, hi int)) {
	if workers <= 1 {
		fn(0, height)
		return
	}
	rows := (height + workers - 1) / workers
	var wg sync.WaitGroup
	for lo := 0; lo < height; lo += rows {
		hi := min(lo+rows, height)
		wg.Go(func() {
			fn(lo, hi)
		})
	}
	wg.Wait()
}

func applyMaskNRGBA(src *image.NRGBA, mask *image.Alpha, out *image.NRGBA, imgMin, maskMin image.Point, alpha uint8) {
	width, height := src.Rect.Dx(), src.Rect.Dy()
	if workers := maskRowWorkers(width, height); workers > 1 {
		runMaskRows(workers, height, func(lo, hi int) {
			maskNRGBARowsAlpha(src, mask, out, imgMin, maskMin, lo, hi, alpha)
		})
		return
	}
	maskNRGBARowsAlpha(src, mask, out, imgMin, maskMin, 0, height, alpha)
}

func maskNRGBARowsAlpha(src *image.NRGBA, mask *image.Alpha, out *image.NRGBA, imgMin, maskMin image.Point, lo, hi int, alpha uint8) {
	width := src.Rect.Dx()
	maskX := src.Rect.Min.X - imgMin.X + maskMin.X
	for row := lo; row < hi; row++ {
		y := src.Rect.Min.Y + row
		si := src.PixOffset(src.Rect.Min.X, y)
		oi := out.PixOffset(src.Rect.Min.X, y)
		mi := mask.PixOffset(maskX, y-imgMin.Y+maskMin.Y)
		for range width {
			out.Pix[oi+0] = src.Pix[si+0]
			out.Pix[oi+1] = src.Pix[si+1]
			out.Pix[oi+2] = src.Pix[si+2]
			out.Pix[oi+3] = alphaMul(alphaMul(src.Pix[si+3], mask.Pix[mi]), alpha)
			si += 4
			oi += 4
			mi++
		}
	}
}

func applyMaskRGBA(src *image.RGBA, mask *image.Alpha, out *image.NRGBA, imgMin, maskMin image.Point, alpha uint8) {
	width, height := src.Rect.Dx(), src.Rect.Dy()
	if workers := maskRowWorkers(width, height); workers > 1 {
		runMaskRows(workers, height, func(lo, hi int) {
			maskRGBARows(src, mask, out, imgMin, maskMin, lo, hi, alpha)
		})
		return
	}
	maskRGBARows(src, mask, out, imgMin, maskMin, 0, height, alpha)
}

func maskRGBARows(src *image.RGBA, mask *image.Alpha, out *image.NRGBA, imgMin, maskMin image.Point, lo, hi int, alpha uint8) {
	width := src.Rect.Dx()
	maskX := src.Rect.Min.X - imgMin.X + maskMin.X
	for row := lo; row < hi; row++ {
		y := src.Rect.Min.Y + row
		si := src.PixOffset(src.Rect.Min.X, y)
		oi := out.PixOffset(src.Rect.Min.X, y)
		mi := mask.PixOffset(maskX, y-imgMin.Y+maskMin.Y)
		for range width {
			ma := mask.Pix[mi]
			r, g, b, a := src.Pix[si+0], src.Pix[si+1], src.Pix[si+2], src.Pix[si+3]
			switch a {
			case 0xff:
				out.Pix[oi+0], out.Pix[oi+1], out.Pix[oi+2] = r, g, b
				out.Pix[oi+3] = alphaMul(ma, alpha)
			case 0:
				out.Pix[oi+0], out.Pix[oi+1], out.Pix[oi+2], out.Pix[oi+3] = 0, 0, 0, 0
			default:
				// 与 NRGBAModel.Convert 一致，先去预乘再叠加掩码。
				out.Pix[oi+3] = alphaMul(alphaMul(a, ma), alpha)
				r16 := uint32(r) * 0xffff / uint32(a)
				g16 := uint32(g) * 0xffff / uint32(a)
				b16 := uint32(b) * 0xffff / uint32(a)
				out.Pix[oi+0], out.Pix[oi+1], out.Pix[oi+2] = uint8(r16>>8), uint8(g16>>8), uint8(b16>>8)
			}
			si += 4
			oi += 4
			mi++
		}
	}
}
