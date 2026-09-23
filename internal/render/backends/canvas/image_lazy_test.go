package canvas

import (
	"bytes"
	"image"
	"image/jpeg"
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
