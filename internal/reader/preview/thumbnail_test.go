package preview

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestImageThumbnailBoundsAspectRatioTransparencyAndOriginalSize(t *testing.T) {
	for _, test := range []struct {
		name                                 string
		width, height, wantWidth, wantHeight int
	}{
		{"横图", 1024, 512, 256, 128}, {"竖图", 100, 1000, 25, 256},
		{"小图不放大", 10, 5, 10, 5}, {"极窄图", 1, 1024, 1, 256},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := image.NewNRGBA(image.Rect(0, 0, test.width, test.height))
			source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 100})
			var data bytes.Buffer
			if err := png.Encode(&data, source); err != nil {
				t.Fatal(err)
			}
			thumbnail, err := MakeThumbnail(t.Context(), data.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if thumbnail.MIME != "image/png" || thumbnail.Width != test.width || thumbnail.Height != test.height || len(thumbnail.Data) > 1<<20 {
				t.Fatalf("缩略图元数据 = %+v", thumbnail)
			}
			decoded, err := png.Decode(bytes.NewReader(thumbnail.Data))
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Bounds().Dx() != test.wantWidth || decoded.Bounds().Dy() != test.wantHeight {
				t.Fatalf("缩略图尺寸 = %v", decoded.Bounds())
			}
			_, _, _, alpha := decoded.At(test.wantWidth-1, test.wantHeight-1).RGBA()
			if alpha != 0 {
				t.Fatal("透明背景变为不透明")
			}
		})
	}
}

func TestImageThumbnailDecodesJPEGAndRejectsDamagedImage(t *testing.T) {
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 512, 256)), nil); err != nil {
		t.Fatal(err)
	}
	thumbnail, err := MakeThumbnail(t.Context(), data.Bytes())
	if err != nil || thumbnail.MIME != "image/png" || thumbnail.Width != 512 {
		t.Fatalf("JPEG 缩略图 = %s, %v", thumbnail.MIME, err)
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	// DecodeConfig 能识别的头部不能代替完整解码。
	header := pngData.Bytes()[:33]
	if _, err := PNGJPEG(header); err != nil {
		t.Fatalf("测试头部不能识别: %v", err)
	}
	if _, err := MakeThumbnail(t.Context(), header); err == nil {
		t.Fatal("损坏图片没有拒绝")
	}
	if _, err := MakeThumbnail(t.Context(), make([]byte, MaxImageBytes+1)); !errors.Is(err, ErrImageSize) {
		t.Fatalf("图片大小 = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := MakeThumbnail(ctx, data.Bytes()); !errors.Is(err, context.Canceled) {
		t.Fatalf("缩略图取消 = %v", err)
	}
}
