package preview

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func TestExtraRasterFormatsProduceStaticPNGAndBoundedThumbnails(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for name, encode := range map[string]func(*bytes.Buffer) error{
		"gif":  func(out *bytes.Buffer) error { return gif.Encode(out, source, nil) },
		"bmp":  func(out *bytes.Buffer) error { return bmp.Encode(out, source) },
		"tiff": func(out *bytes.Buffer) error { return tiff.Encode(out, source, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			var data bytes.Buffer
			if err := encode(&data); err != nil {
				t.Fatal(err)
			}
			preview, err := Raster(t.Context(), data.Bytes())
			if err != nil || preview.MIME != "image/png" {
				t.Fatalf("光栅预览 = %s, %v", preview.MIME, err)
			}
			config, err := png.DecodeConfig(bytes.NewReader(preview.Data))
			if err != nil || config.Width != 40 || config.Height != 20 {
				t.Fatalf("PNG 尺寸 = %+v, %v", config, err)
			}
			thumb, err := MakeThumbnail(t.Context(), data.Bytes())
			if err != nil || thumb.Width != 40 || thumb.Height != 20 {
				t.Fatalf("缩略图 = %+v, %v", thumb, err)
			}
		})
	}
	webp, err := base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAEAcQ/Y8CBiKi/wEA")
	if err != nil {
		t.Fatal(err)
	}
	if preview, err := Raster(t.Context(), webp); err != nil || preview.MIME != "image/png" {
		t.Fatalf("WebP 预览 = %s, %v", preview.MIME, err)
	}
}

func TestExtraRasterRejectsActiveContentSizeDimensionsDamageAndCancellation(t *testing.T) {
	for _, data := range [][]byte{[]byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), []byte("<html><script>alert(1)</script></html>")} {
		if _, err := Raster(t.Context(), data); !errors.Is(err, ErrRasterFormat) {
			t.Fatalf("活动内容未拒绝: %v", err)
		}
	}
	if _, err := Raster(t.Context(), make([]byte, MaxImageBytes+1)); !errors.Is(err, ErrImageSize) {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := gif.Encode(&data, image.NewGray(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	large := bytes.Clone(data.Bytes())
	binary.LittleEndian.PutUint16(large[6:8], 65535)
	binary.LittleEndian.PutUint16(large[8:10], 65535)
	if _, err := Raster(t.Context(), large); !errors.Is(err, ErrImageDimensions) {
		t.Fatal(err)
	}
	if _, err := Raster(t.Context(), data.Bytes()[:13]); err == nil {
		t.Fatal("损坏 GIF 未拒绝")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Raster(ctx, data.Bytes()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var output previewBuffer
	if _, err := output.Write(make([]byte, MaxImageBytes+1)); !errors.Is(err, ErrImageSize) || output.Len() != 0 {
		t.Fatal("PNG 输出预算未执行")
	}
}

func TestPNGJPEGRejectsExtraRasterFormatsEvenWithRegisteredDecoders(t *testing.T) {
	var data bytes.Buffer
	if err := gif.Encode(&data, image.NewGray(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Raster(t.Context(), data.Bytes()); err != nil {
		t.Fatalf("资源预览应接受 GIF: %v", err)
	}
	if _, err := PNGJPEG(data.Bytes()); !errors.Is(err, ErrImageFormat) {
		t.Fatalf("印章原图不能因解码器注册而接受 GIF: %v", err)
	}
	binary.LittleEndian.PutUint16(data.Bytes()[6:8], 65535)
	binary.LittleEndian.PutUint16(data.Bytes()[8:10], 65535)
	if _, err := PNGJPEG(data.Bytes()); !errors.Is(err, ErrImageFormat) {
		t.Fatalf("不允许的格式应先返回格式错误: %v", err)
	}
}

func TestRasterPreservesPNGJPEGBytesAndRejectsCancelledResults(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for format, encode := range map[string]func(*bytes.Buffer) error{
		"png":  func(out *bytes.Buffer) error { return png.Encode(out, source) },
		"jpeg": func(out *bytes.Buffer) error { return jpeg.Encode(out, source, nil) },
	} {
		t.Run(format, func(t *testing.T) {
			var data bytes.Buffer
			if err := encode(&data); err != nil {
				t.Fatal(err)
			}
			result, err := Raster(t.Context(), data.Bytes())
			if err != nil || result.MIME != "image/"+format || !bytes.Equal(result.Data, data.Bytes()) {
				t.Fatalf("预览不得重新编码原图: %s, %v", result.MIME, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if result, err := Raster(ctx, data.Bytes()); !errors.Is(err, context.Canceled) || result.Data != nil {
				t.Fatalf("取消后的预览 = %+v, %v", result, err)
			}
		})
	}
}
