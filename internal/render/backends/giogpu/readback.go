package giogpu

import (
	"image"
	"math"
	"sync"
)

// GPU 在线性光空间预乘，再编码为 sRGB；Go RGBA 则要求 sRGB 通道值预乘。
// 例如半透明红色的 GPU 字节为 (188,0,0,128)，Go 应为 (128,0,0,128)。
// 固定 64 KiB 查表按需初始化，避免逐像素执行幂运算。
var readbackTable = sync.OnceValue(func() *[256][256]uint8 {
	table := new([256][256]uint8)
	for alpha := 1; alpha < 255; alpha++ {
		for value := range 256 {
			linear := float64(value) / 255
			if linear <= 0.04045 {
				linear /= 12.92
			} else {
				linear = math.Pow((linear+0.055)/1.055, 2.4)
			}
			straight := min(1, linear*255/float64(alpha))
			if straight <= 0.0031308 {
				straight *= 12.92
			} else {
				straight = 1.055*math.Pow(straight, 1/2.4) - 0.055
			}
			table[alpha][value] = uint8(math.Round(straight * float64(alpha)))
		}
	}
	return table
})

// normalizeReadback 只处理回读缓冲；不修改页面记录或共享图片。完全不透明的
// 像素保持原字节，因此不会改变白底文字边缘的线性光混合观感。
func normalizeReadback(img *image.RGBA) {
	var table *[256][256]uint8
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		start := img.PixOffset(img.Rect.Min.X, y)
		row := img.Pix[start : start+img.Rect.Dx()*4]
		for x := 0; x < len(row); x += 4 {
			alpha := row[x+3]
			switch alpha {
			case 255:
				continue
			case 0:
				row[x], row[x+1], row[x+2] = 0, 0, 0
			default:
				if table == nil {
					table = readbackTable()
				}
				row[x], row[x+1], row[x+2] = table[alpha][row[x]], table[alpha][row[x+1]], table[alpha][row[x+2]]
			}
		}
	}
}
