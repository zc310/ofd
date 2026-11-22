package pdf

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

func TestPDFLossyImagesEncodePNGColorAndKeepExactAlpha(t *testing.T) {
	for _, transparent := range []bool{false, true} {
		t.Run(map[bool]string{false: "不透明PNG", true: "透明PNG"}[transparent], func(t *testing.T) {
			source := image.NewNRGBA(image.Rect(0, 0, 32, 24))
			for y := range 24 {
				for x := range 32 {
					alpha := uint8(255)
					if transparent {
						alpha = uint8(32 + x*6)
					}
					source.SetNRGBA(x, y, color.NRGBA{R: 240, G: 96, B: 32, A: alpha})
				}
			}
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, source); err != nil {
				t.Fatal(err)
			}
			img, err := cimage.NewPNGImage(bytes.NewReader(encoded.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			images := lossyImageStreams(t, img)
			var rgb, alpha *types.StreamDict
			for i := range images {
				if space := images[i].NameEntry("ColorSpace"); space != nil && *space == "DeviceRGB" {
					rgb = &images[i]
				} else {
					alpha = &images[i]
				}
			}
			if rgb == nil || len(rgb.FilterPipeline) != 1 || rgb.FilterPipeline[0].Name != "DCTDecode" {
				t.Fatal("显式有损模式下PNG颜色未转为JPEG")
			}
			decoded, err := jpeg.Decode(bytes.NewReader(rgb.Raw))
			if err != nil {
				t.Fatal(err)
			}
			// A uniform straight color must not darken with decreasing alpha.
			for x := range 32 {
				got := color.NRGBAModel.Convert(decoded.At(x, 12)).(color.NRGBA)
				if got.R < 230 || got.G < 86 || got.B < 22 {
					t.Fatalf("透明颜色被重复预乘：x=%d，颜色=%v", x, got)
				}
			}
			_, wantAlpha, _ := rgbStream(img, img.Bounds().Size())
			if transparent {
				if alpha == nil || !bytes.Equal(alpha.Content, wantAlpha) {
					t.Fatal("透明蒙版未无损保存")
				}
			} else if alpha != nil {
				t.Fatal("不透明图片不应产生SMask")
			}
		})
	}
}

func TestPDFLossyImagesKeepMaskedJPEGColorBytes(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 32, 24))
	mask := image.NewGray(source.Bounds())
	for y := range 24 {
		for x := range 32 {
			source.SetRGBA(x, y, color.RGBA{R: 220, G: 100, B: 40, A: 255})
			mask.SetGray(x, y, color.Gray{Y: uint8(x * 8)})
		}
	}
	var encoded, encodedMask bytes.Buffer
	if err := jpeg.Encode(&encoded, source, nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&encodedMask, mask); err != nil {
		t.Fatal(err)
	}
	maskImage, err := cimage.NewPNGImage(bytes.NewReader(encodedMask.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	img, err := cimage.NewJPEGMaskedImage(bytes.NewReader(encoded.Bytes()), maskImage)
	if err != nil {
		t.Fatal(err)
	}
	images := lossyImageStreams(t, img)
	_, wantAlpha, _ := rgbStream(img, img.Bounds().Size())
	if len(images) != 2 {
		t.Fatalf("颜色及蒙版流数量=%d，期望2", len(images))
	}
	for _, stream := range images {
		space := stream.NameEntry("ColorSpace")
		if space != nil && *space == "DeviceRGB" {
			if !bytes.Equal(stream.Raw, encoded.Bytes()) {
				t.Fatal("已有JPEG被重复压缩")
			}
		} else if !bytes.Equal(stream.Content, wantAlpha) {
			t.Fatal("JPEG的独立蒙版未无损保存")
		}
	}
}

func lossyImageStreams(t *testing.T, img image.Image) []types.StreamDict {
	t.Helper()
	var output bytes.Buffer
	w := newPDFWriter(&output)
	w.NewPage(50, 50).DrawImage(img, cimage.Lossy, canvas.Identity)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, err := api.ReadAndValidate(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	var images []types.StreamDict
	for _, entry := range ctx.Table {
		if entry == nil {
			continue
		}
		sd, _, err := ctx.DereferenceStreamDict(entry.Object)
		if err != nil || sd == nil {
			continue
		}
		if subtype := sd.NameEntry("Subtype"); subtype == nil || *subtype != "Image" {
			continue
		}
		if space := sd.NameEntry("ColorSpace"); space != nil && *space == "DeviceGray" {
			if err := ctx.DecodeStream(sd); err != nil {
				t.Fatal(err)
			}
		}
		images = append(images, *sd)
	}
	return images
}
