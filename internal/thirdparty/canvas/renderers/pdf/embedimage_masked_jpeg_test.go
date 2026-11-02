package pdf

import (
	"bytes"
	"compress/zlib"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

// A JPEG image with a separate non-JPEG mask used to take a branch in
// embedImage that wrote unpremultiplied RGB samples over the raw JPEG payload
// while the filter still claimed DCTDecode. The result was a corrupt image, or
// an index-out-of-range panic whenever the JPEG was smaller than width*height*3.
// Because Lossless is the default encoding, that was the common path for such an
// image, not an exotic one.
func TestPDFJPEGMaskedImageLossless(t *testing.T) {
	const w, h = 24, 16

	// Source image that will be JPEG compressed.
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 15), B: 0x80, A: 0xff})
		}
	}
	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}

	// A grayscale mask, deliberately not JPEG so the branch is taken.
	maskImg := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			maskImg.SetGray(x, y, color.Gray{Y: uint8(x * 8)})
		}
	}
	var maskBuf bytes.Buffer
	if err := png.Encode(&maskBuf, maskImg); err != nil {
		t.Fatal(err)
	}
	mask, err := cimage.NewPNGImage(bytes.NewReader(maskBuf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	cimg, err := cimage.NewJPEGMaskedImage(bytes.NewReader(jpegBuf.Bytes()), mask)
	if err != nil {
		t.Fatal(err)
	}

	buf := &bytes.Buffer{}
	pdf := newPDFWriter(buf)
	// Lossless is the default encoding and is what triggered the broken branch.
	pdf.NewPage(50, 50).DrawImage(cimg, cimage.Lossless, canvas.Identity)
	if err := pdf.Close(); err != nil {
		t.Fatal(err)
	}
	out := buf.Bytes()

	if strings.Contains(string(out), "/DCTDecode") {
		t.Errorf("无损编码不应使用 DCTDecode 过滤器")
	}

	rgbStreamData, alphaStreamData := extractImageStreams(t, out)
	if len(rgbStreamData) != w*h*3 {
		t.Errorf("颜色流长度 = %d, 期望 %d（不得复用 JPEG 负载）", len(rgbStreamData), w*h*3)
	}
	if len(alphaStreamData) != w*h {
		t.Errorf("掩码流长度 = %d, 期望 %d", len(alphaStreamData), w*h)
	}

	// The embedded samples must reproduce the mask-composited image, i.e. the
	// same thing the two other lossless branches embed.
	composited, err := cimg.Image()
	if err != nil {
		t.Fatal(err)
	}
	wantRGB, wantAlpha, _ := rgbStream(composited, image.Pt(w, h))
	if !bytes.Equal(rgbStreamData, wantRGB) {
		t.Errorf("嵌入的颜色流与 rgbStream 不一致")
	}
	if !bytes.Equal(alphaStreamData, wantAlpha) {
		t.Errorf("嵌入的掩码流与 rgbStream 不一致")
	}
	if wantAlpha == nil {
		t.Error("测试样本应产生非空掩码流")
	}
}

// extractImageStreams returns the decompressed DeviceRGB image stream and the
// DeviceGray SMask stream from a rendered PDF, keyed off the declared colour
// space rather than the payload length.
func extractImageStreams(t *testing.T, out []byte) (rgb, alpha []byte) {
	t.Helper()
	rest := out
	for {
		at := bytes.Index(rest, []byte("/Type/XObject/Subtype/Image"))
		if at < 0 {
			break
		}
		rest = rest[at:]
		dictEnd := bytes.Index(rest, []byte("stream\n"))
		if dictEnd < 0 {
			break
		}
		dict := string(rest[:dictEnd])
		body := rest[dictEnd+len("stream\n"):]
		rawEnd := bytes.Index(body, []byte("\nendstream"))
		if rawEnd < 0 {
			break
		}
		raw := body[:rawEnd]
		rest = body[rawEnd:]

		r, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		data, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			continue
		}
		switch {
		case strings.Contains(dict, "/ColorSpace/DeviceRGB") && rgb == nil:
			rgb = data
		case strings.Contains(dict, "/ColorSpace/DeviceGray") && alpha == nil:
			alpha = data
		}
	}
	return rgb, alpha
}
