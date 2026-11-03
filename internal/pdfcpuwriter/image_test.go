package pdfcpuwriter

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

func TestLossyImagesKeepExactAlphaAndReuseAcrossPages(t *testing.T) {
	src := image.NewNRGBA(image.Rect(3, 5, 19, 21))
	var alpha []byte
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			a := uint8((x + y) * 5)
			src.SetNRGBA(x, y, color.NRGBA{R: 180, G: 70, B: 30, A: a})
			alpha = append(alpha, a)
		}
	}
	doc, err := NewWithOptions(t.Context(), 40, 30, Options{LossyImages: true})
	if err != nil {
		t.Fatal(err)
	}
	doc.RenderImage(src, canvas.Identity)
	if err := doc.NextPage(40, 30); err != nil {
		t.Fatal(err)
	}
	doc.RenderImage(src, canvas.Identity)
	ctx := readWrittenPDF(t, doc)
	if doc.Stats().Images != 1 {
		t.Fatal("跨页图片未复用")
	}
	stream, _, err := ctx.DereferenceStreamDict(doc.imageResources["Im0"])
	if err != nil {
		t.Fatal(err)
	}
	if filter := stream.NameEntry("Filter"); filter == nil || *filter != "DCTDecode" {
		t.Fatal("颜色流没有使用 JPEG")
	}
	decoded, err := jpeg.Decode(bytes.NewReader(stream.Raw))
	if err != nil {
		t.Fatal(err)
	}
	rgb, _ := color.NRGBAModel.Convert(decoded.At(5, 5)).(color.NRGBA)
	if rgb.R < 170 || rgb.G < 60 || rgb.B < 20 {
		t.Fatalf("JPEG 颜色被透明度预乘而变暗：%v", rgb)
	}
	mask, _, err := ctx.DereferenceStreamDict(stream.Dict["SMask"])
	if err != nil {
		t.Fatal(err)
	}
	if mask.NameEntry("Filter") == nil || *mask.NameEntry("Filter") != "FlateDecode" {
		t.Fatal("透明蒙版不是无损编码")
	}
	if err := ctx.DecodeStream(mask); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mask.Content, alpha) || mask.Dict["Interpolate"] != types.Boolean(true) {
		t.Fatal("透明蒙版采样或插值设置改变")
	}
}

// 2×2 CMYK/YCCK JPEG，由 ImageMagick 从纯色生成，不依赖外部字体或图片样本。
const cmykJPEG = "/9j/7gAOQWRvYmUAZAAAAAAC/9sAQwADAgICAgIDAgICAwMDAwQGBAQEBAQIBgYFBgkICgoJCAkJCgwPDAoLDgsJCQ0RDQ4PEBAREAoMEhMSEBMPEBAQ/9sAQwEDAwMEAwQIBAQIEAsJCxAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ/8AAFAgAAgACBAERAAIRAQMRAQQRAP/EABUAAQEAAAAAAAAAAAAAAAAAAAUH/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/EABQBAQAAAAAAAAAAAAAAAAAAAAj/xAAUEQEAAAAAAAAAAAAAAAAAAAAA/9oADgQBAAIRAxEEAAA/AJYRojlX/9k="

func TestJPEGFastPathKeepsBytesAndColorSpace(t *testing.T) {
	rgb := image.NewRGBA(image.Rect(0, 0, 8, 8))
	gray := image.NewGray(rgb.Bounds())
	for y := range 8 {
		for x := range 8 {
			rgb.SetRGBA(x, y, color.RGBA{R: 180, G: 70, B: 30, A: 255})
			gray.SetGray(x, y, color.Gray{Y: 100})
		}
	}
	cmyk, err := base64.StdEncoding.DecodeString(cmykJPEG)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, space string
		source      image.Image
		data        []byte
	}{
		{name: "RGB", space: "DeviceRGB", source: rgb},
		{name: "灰度", space: "DeviceGray", source: gray},
		{name: "CMYK", space: "DeviceCMYK", data: cmyk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.data
			if data == nil {
				var encoded bytes.Buffer
				if err := jpeg.Encode(&encoded, tc.source, nil); err != nil {
					t.Fatal(err)
				}
				data = encoded.Bytes()
			}
			img, err := cimage.NewJPEGImage(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := New(t.Context(), 20, 20)
			if err != nil {
				t.Fatal(err)
			}
			doc.RenderImage(img, canvas.Identity)
			ctx := readWrittenPDF(t, doc)
			stream, _, err := ctx.DereferenceStreamDict(doc.imageResources["Im0"])
			if err != nil {
				t.Fatal(err)
			}
			if stream.Dict["ColorSpace"] != types.Name(tc.space) || !bytes.Equal(stream.Raw, data) {
				t.Fatal("JPEG 原字节或颜色空间改变")
			}
			if tc.space == "DeviceCMYK" && len(stream.ArrayEntry("Decode")) != 8 {
				t.Fatal("CMYK JPEG 缺少反相解码数组")
			}
			t.Run("PDF颜色", func(t *testing.T) { compareJPEGColor(t, img) })
		})
	}
}

func compareJPEGColor(t *testing.T, encoded *cimage.Image) {
	t.Helper()
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少 pdftoppm")
	}
	samples := make([]color.NRGBA, 0, 2)
	for _, original := range []bool{false, true} {
		doc, err := New(t.Context(), 20, 20)
		if err != nil {
			t.Fatal(err)
		}
		if original {
			// 以优化前的 pdfcpu 通用路径为对照；CMYK 不能先转 RGB，否则会改变阅读器的色彩管理。
			sd, width, height, err := model.CreateImageStreamDict(doc.pdf.XRefTable, bytes.NewReader(encoded.Bytes))
			if err != nil {
				t.Fatal(err)
			}
			sd.Dict["Interpolate"] = types.Boolean(true)
			doc.imageResources["Im0"] = doc.object(*sd)
			doc.imageCache[sha256.Sum256(encoded.Bytes)] = prototypeImage{name: "Im0", width: width, height: height}
		}
		doc.RenderImage(encoded, canvas.Identity.Translate(5, 5).Scale(10/float64(encoded.Width), 10/float64(encoded.Height)))
		var output bytes.Buffer
		if err := doc.Write(&output); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "image.pdf")
		if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		data, err := exec.CommandContext(t.Context(), tool, "-singlefile", "-png", "-r", "144", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		page, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		c, _ := color.NRGBAModel.Convert(page.At(page.Bounds().Dx()/2, page.Bounds().Dy()/2)).(color.NRGBA)
		samples = append(samples, c)
	}
	for _, delta := range []int{int(samples[0].R) - int(samples[1].R), int(samples[0].G) - int(samples[1].G), int(samples[0].B) - int(samples[1].B)} {
		if delta < -8 || delta > 8 {
			t.Fatalf("JPEG 快路径与原通用路径的 PDF 颜色不同：%v", samples)
		}
	}
}

func TestRasterFastPathKeepsSubimageStraightColorsAndAlpha(t *testing.T) {
	nrgba := image.NewNRGBA(image.Rect(2, 3, 8, 9))
	rgba := image.NewRGBA(nrgba.Bounds())
	for y := 3; y < 9; y++ {
		for x := 2; x < 8; x++ {
			c := color.NRGBA{R: 180, G: 80, B: 40, A: uint8((x + y) * 15)}
			nrgba.SetNRGBA(x, y, c)
			rgba.Set(x, y, c)
		}
	}
	rect := image.Rect(3, 4, 7, 8)
	for _, img := range []image.Image{nrgba.SubImage(rect), rgba.SubImage(rect)} {
		doc, err := New(t.Context(), 20, 20)
		if err != nil {
			t.Fatal(err)
		}
		data, err := doc.prepareImage(img)
		if err != nil {
			t.Fatal(err)
		}
		if data.png || data.jpeg || data.space != "DeviceRGB" {
			t.Fatal("常用光栅类型仍走编码中转")
		}
		for y := range rect.Dy() {
			for x := range rect.Dx() {
				i := y*rect.Dx() + x
				want, _ := color.NRGBAModel.Convert(img.At(x+rect.Min.X, y+rect.Min.Y)).(color.NRGBA)
				got := color.NRGBA{R: data.pixels[3*i], G: data.pixels[3*i+1], B: data.pixels[3*i+2], A: data.alpha[i]}
				if got != want {
					t.Fatalf("子图像反预乘或透明度改变：%T (%d,%d)=%v，期望%v", img, x, y, got, want)
				}
			}
		}
	}
}

func TestRasterImageCacheDistinguishesDimensionsAndAlpha(t *testing.T) {
	doc, err := New(t.Context(), 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{2, 4}, {4, 2}, {2, 4}} {
		img := image.NewNRGBA(image.Rect(0, 0, size[0], size[1]))
		for i := range len(img.Pix) / 4 {
			copy(img.Pix[4*i:4*i+4], []byte{100, 100, 100, 128})
		}
		doc.RenderImage(img, canvas.Identity)
		if doc.Err() != nil {
			t.Fatal(doc.Err())
		}
	}
	if doc.Stats().Images != 2 {
		t.Fatal("相同采样不同尺寸被合并，或相同图片没有复用")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 4))
	for i := range len(img.Pix) / 4 {
		copy(img.Pix[4*i:4*i+4], []byte{100, 100, 100, 129})
	}
	doc.RenderImage(img, canvas.Identity)
	if doc.Stats().Images != 3 {
		t.Fatal("不同透明度被错误合并")
	}
}

func TestImageFastPathRejectsResourceLimitsAndCorruptJPEG(t *testing.T) {
	for _, limits := range []struct{ pixels, bytes int64 }{{1, 1024}, {1024, 1}} {
		doc, err := New(t.Context(), 20, 20)
		if err != nil {
			t.Fatal(err)
		}
		doc.pdf.Conf.Limits.MaxImagePixels = limits.pixels
		doc.pdf.Conf.Limits.MaxImageBytes = limits.bytes
		doc.RenderImage(image.NewNRGBA(image.Rect(0, 0, 2, 2)), canvas.Identity)
		if !errors.Is(doc.Err(), errInvalidImage) {
			t.Fatalf("快路径绕过图片预算：%v", doc.Err())
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	img, err := cimage.NewJPEGImage(bytes.NewReader(encoded.Bytes()[:encoded.Len()-2]))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(t.Context(), 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	doc.RenderImage(img, canvas.Identity)
	var output bytes.Buffer
	if err := doc.Write(&output); err == nil || output.Len() != 0 {
		t.Fatal("截断 JPEG 被当作原字节直接嵌入")
	}
}

func TestJPEGFastPathOwnsEncodedSnapshot(t *testing.T) {
	var original bytes.Buffer
	if err := jpeg.Encode(&original, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	img, err := cimage.NewJPEGImage(bytes.NewReader(original.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := New(t.Context(), 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	doc.RenderImage(img, canvas.Identity)
	img.Bytes[0] = 0 // 已绘制的资源不能别名调用方的可变字节。
	ctx := readWrittenPDF(t, doc)
	stream, _, err := ctx.DereferenceStreamDict(doc.imageResources["Im0"])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stream.Raw, original.Bytes()) {
		t.Fatal("修改输入字节改变了已绘制的 JPEG")
	}
}

func TestImageFallbackKeeps16BitColorAndAlpha(t *testing.T) {
	src := image.NewNRGBA64(image.Rect(0, 0, 2, 2))
	colors := make([]byte, 0, 24)
	alpha := make([]byte, 0, 8)
	for y := range 2 {
		for x := range 2 {
			c := color.NRGBA64{R: 0x1234, G: 0x5678, B: 0x9abc, A: uint16(0x3456 + x + y)}
			src.SetNRGBA64(x, y, c)
			colors = append(colors, byte(c.R>>8), byte(c.R), byte(c.G>>8), byte(c.G), byte(c.B>>8), byte(c.B))
			alpha = append(alpha, byte(c.A>>8), byte(c.A))
		}
	}
	doc, err := New(t.Context(), 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	doc.RenderImage(src, canvas.Identity)
	ctx := readWrittenPDF(t, doc)
	stream, _, err := ctx.DereferenceStreamDict(doc.imageResources["Im0"])
	if err != nil {
		t.Fatal(err)
	}
	if stream.Dict["BitsPerComponent"] != types.Integer(16) {
		t.Fatal("16 位图片被降精度")
	}
	if err := ctx.DecodeStream(stream); err != nil {
		t.Fatal(err)
	}
	mask, _, err := ctx.DereferenceStreamDict(stream.Dict["SMask"])
	if err != nil {
		t.Fatal(err)
	}
	if err := ctx.DecodeStream(mask); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stream.Content, colors) || !bytes.Equal(mask.Content, alpha) {
		t.Fatal("16 位颜色或透明蒙版采样改变")
	}
}

func TestLossyImagesKeepOriginalMaskedJPEGBytes(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	mask := image.NewGray(src.Bounds())
	for y := range 8 {
		for x := range 8 {
			src.SetRGBA(x, y, color.RGBA{R: 180, G: 70, B: 30, A: 255})
			mask.SetGray(x, y, color.Gray{Y: uint8(x * 30)})
		}
	}
	var encoded, maskPNG bytes.Buffer
	if err := jpeg.Encode(&encoded, src, nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&maskPNG, mask); err != nil {
		t.Fatal(err)
	}
	alpha, err := cimage.NewPNGImage(&maskPNG)
	if err != nil {
		t.Fatal(err)
	}
	img, err := cimage.NewJPEGMaskedImage(bytes.NewReader(encoded.Bytes()), alpha)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := NewWithOptions(t.Context(), 40, 30, Options{LossyImages: true})
	if err != nil {
		t.Fatal(err)
	}
	doc.RenderImage(img, canvas.Identity)
	ctx := readWrittenPDF(t, doc)
	stream, _, err := ctx.DereferenceStreamDict(doc.imageResources["Im0"])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stream.Raw, encoded.Bytes()) || stream.Dict["SMask"] == nil {
		t.Fatal("带蒙版的原 JPEG 被重复编码或丢失蒙版")
	}
}
