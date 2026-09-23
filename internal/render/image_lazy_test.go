package render

import (
	"bytes"
	"image"
	"image/jpeg"
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

	// 真正需要像素时才解码，并缓存结果。
	if lazy.Bounds().Empty() {
		t.Fatal("Bounds() 解码失败")
	}
	if lazy.img == nil {
		t.Fatal("Bounds() 应触发解码并缓存")
	}
}

// TestCanvasImageWrapsEncodedImage 回归：canvas 适配层能把保留原始字节的
// 懒解码图片转换为 canvas/image.Image（供 PDF 原字节内嵌），且结果被缓存。
