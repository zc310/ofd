package imageops

import (
	"image"
	"image/color"
)

// ApplyAlpha 将整张图片的不透明度乘以 alpha，不修改原图，255 时直接返回原图。
func ApplyAlpha(img image.Image, alpha uint8) image.Image {
	if img == nil || alpha == 255 {
		return img
	}
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	img = decodedImage(img)
	switch src := img.(type) {
	case *image.NRGBA:
		for row := range bounds.Dy() {
			y := bounds.Min.Y + row
			si, oi := src.PixOffset(bounds.Min.X, y), out.PixOffset(bounds.Min.X, y)
			source := src.Pix[si : si+4*bounds.Dx()]
			target := out.Pix[oi : oi+4*bounds.Dx()]
			copy(target, source)
			for i := 3; i < len(target); i += 4 {
				target[i] = alphaMul(source[i], alpha)
			}
		}
		return out
	case *image.RGBA:
		for row := range bounds.Dy() {
			y := bounds.Min.Y + row
			si, oi := src.PixOffset(bounds.Min.X, y), out.PixOffset(bounds.Min.X, y)
			source := src.Pix[si : si+4*bounds.Dx()]
			target := out.Pix[oi : oi+4*bounds.Dx()]
			for i := 0; i < len(target); i += 4 {
				a := source[i+3]
				switch a {
				case 255:
					copy(target[i:i+3], source[i:i+3])
				case 0:
					// 与 NRGBAModel 相同：全透明的预乘像素没有 RGB 可还原。
				default:
					for c := range 3 {
						target[i+c] = uint8((uint32(source[i+c]) * 0xffff / uint32(a)) >> 8)
					}
				}
				target[i+3] = alphaMul(a, alpha)
			}
		}
		return out
	case *image.YCbCr:
		for row := range bounds.Dy() {
			y := bounds.Min.Y + row
			yi, oi := src.YOffset(bounds.Min.X, y), out.PixOffset(bounds.Min.X, y)
			for col := range bounds.Dx() {
				ci := src.COffset(bounds.Min.X+col, y)
				r, g, b := color.YCbCrToRGB(src.Y[yi+col], src.Cb[ci], src.Cr[ci])
				out.Pix[oi], out.Pix[oi+1], out.Pix[oi+2], out.Pix[oi+3] = r, g, b, alpha
				oi += 4
			}
		}
		return out
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := nrgbaAt(img, x, y)
			c.A = uint8(int(c.A) * int(alpha) / 255)
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

func decodedImage(img image.Image) image.Image {
	type lazyImage interface {
		Image() (image.Image, error)
	}
	for range 4 {
		lazy, ok := img.(lazyImage)
		if !ok {
			return img
		}
		decoded, err := lazy.Image()
		if err != nil || decoded == nil || decoded == img {
			return img
		}
		img = decoded
	}
	return img
}

func alphaMul(a, b uint8) uint8 { return uint8(int(a) * int(b) / 255) }
