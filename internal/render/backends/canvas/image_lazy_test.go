package canvas

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/zc310/ofd/internal/render"
)

func TestCanvasImageWrapsEncodedImage(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatal(err)
	}
	lazy := render.NewEncodedImage("jpeg", buf.Bytes())
	converted := canvasImage(lazy)
	if converted == image.Image(lazy) {
		t.Fatal("EncodedImage 应被转换为 canvas/image.Image")
	}
	if again := canvasImage(lazy); again != converted {
		t.Fatal("canvas 图片转换结果应被缓存复用")
	}
}

func TestCanvasImageKeepsGrayscaleImageUnwrapped(t *testing.T) {
	// 灰度 PNG 若走 canvas 快路径会被解码成 *image.Gray，随后被 Go 的 JPEG
	// 编码器写成单通道灰度 JPEG，而 PDF 字典固定声明 DeviceRGB，解码错位会
	// 让整幅图重复或花屏；灰度图必须保留为 EncodedImage 走通用 RGB 编码。
	var gray bytes.Buffer
	if err := png.Encode(&gray, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	lazyGray := render.NewEncodedImage("png", gray.Bytes())
	if converted := canvasImage(lazyGray); converted != image.Image(lazyGray) {
		t.Fatal("灰度图片不应被转换为 canvas/image.Image")
	}

	var rgb bytes.Buffer
	if err := png.Encode(&rgb, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	lazyRGB := render.NewEncodedImage("png", rgb.Bytes())
	if converted := canvasImage(lazyRGB); converted == image.Image(lazyRGB) {
		t.Fatal("彩色图片应被转换为 canvas/image.Image")
	}
}
