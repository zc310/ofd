package converter

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"strings"
	"testing"
)

// 两条渲染路径与各公开入口都应传递压缩级别，默认输出保持级别 7，解码像素无损。
func TestPNGCompressionLevelsPreservePixelsAcrossEntryPointsAndBackends(t *testing.T) {
	input := conversionOutputFixture(t, 1)
	documents, ofd, err := parseRenderDocuments(input, newConverter(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	defer closeOFD(ofd)
	defer func() {
		for _, doc := range documents {
			doc.Close()
		}
	}()
	for _, backend := range []string{"", "canvas"} {
		for _, entry := range []string{"Convert", "Encode", "Image", "ImageDocuments"} {
			t.Run(entry+"/"+backend, func(t *testing.T) {
				var baseline *image.NRGBA
				outputs := make(map[int][]byte)
				for level := -1; level <= 9; level++ {
					var output bytes.Buffer
					options := []Option{Page(1), RasterBackend(backend), Writer(func(int) (io.WriteCloser, error) {
						return &nopWriteCloser{&output}, nil
					})}
					if level >= 0 {
						options = append(options, PNGCompressionLevel(level))
					}
					var err error
					switch entry {
					case "Convert":
						err = Convert(t.Context(), "ofd", "png", input, &output, options...)
					case "Encode":
						err = Encode(t.Context(), "png", input, &output, options...)
					case "Image":
						err = Image(t.Context(), input, options...)
					case "ImageDocuments":
						err = ImageDocuments(t.Context(), documents, options...)
					}
					if err != nil {
						t.Fatalf("级别%d: %v", level, err)
					}
					decoded, err := png.Decode(bytes.NewReader(output.Bytes()))
					if err != nil {
						t.Fatal(err)
					}
					pixels := image.NewNRGBA(decoded.Bounds())
					draw.Draw(pixels, pixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
					if baseline == nil {
						baseline = pixels
					} else if pixels.Bounds() != baseline.Bounds() || !bytes.Equal(pixels.Pix, baseline.Pix) {
						t.Fatalf("压缩级别%d 改变了解码后的图像像素", level)
					}
					outputs[level] = bytes.Clone(output.Bytes())
				}
				if !bytes.Equal(outputs[-1], outputs[0]) || !bytes.Equal(outputs[0], outputs[7]) {
					t.Fatal("省略压缩级别或设置 0 时应使用级别 7")
				}
				if bytes.Equal(outputs[1], outputs[9]) {
					t.Fatal("级别 1 与 9 的输出完全相同，压缩选项可能未传递到编码器")
				}
			})
		}
	}
}

type pngUnreadInput struct{ t *testing.T }

func (input pngUnreadInput) Read([]byte) (int, error) {
	input.t.Fatal("无效 PNG 压缩级别不应读取输入")
	return 0, io.EOF
}

func TestPNGCompressionRejectsInvalidLevelsBeforeReadingOrWriting(t *testing.T) {
	for _, level := range []int{-2, -1, 10, 100} {
		for _, entry := range []string{"Convert", "Encode", "Image"} {
			t.Run(fmt.Sprintf("%s/%d", entry, level), func(t *testing.T) {
				var output bytes.Buffer
				options := []Option{PNGCompressionLevel(level), Writer(func(int) (io.WriteCloser, error) {
					t.Fatal("无效 PNG 压缩级别不应创建输出")
					return nil, io.ErrClosedPipe
				})}
				var err error
				switch entry {
				case "Convert":
					err = Convert(t.Context(), "ofd", "png", pngUnreadInput{t}, &output, options...)
				case "Encode":
					err = Encode(t.Context(), "png", pngUnreadInput{t}, &output, options...)
				case "Image":
					err = Image(t.Context(), pngUnreadInput{t}, options...)
				}
				if err == nil || !strings.Contains(err.Error(), "PNG 压缩级别") || output.Len() != 0 {
					t.Fatalf("无效级别应在输出前报错：err=%v bytes=%d", err, output.Len())
				}
			})
		}
	}
}
