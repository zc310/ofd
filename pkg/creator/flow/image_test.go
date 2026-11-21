package flow_test

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func imageTestData(t *testing.T, format string) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, 192, 96))
	for y := range 96 {
		for x := range 192 {
			source.Set(x, y, color.RGBA{R: 20, G: 100, B: 200, A: 255})
		}
	}
	var data bytes.Buffer
	var err error
	switch format {
	case "JPEG":
		err = jpeg.Encode(&data, source, nil)
	case "GIF":
		err = gif.Encode(&data, source, nil)
	default:
		err = png.Encode(&data, source)
	}
	if err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func placedImage(t *testing.T, model *creator.Document) creator.Image {
	t.Helper()
	for _, page := range model.Pages {
		for _, item := range page.Items {
			if img, ok := item.(creator.Image); ok {
				return img
			}
		}
	}
	t.Fatal("缺少图片图元")
	return creator.Image{}
}

// 替代旧 Figure 的尺寸推算测试，保持 flow 双边尺寸的既有拉伸默认值。
func TestImageSizingPreservesAspectRatioAndExplicitFit(t *testing.T) {
	for _, format := range []string{"PNG", "JPEG", "GIF"} {
		data := imageTestData(t, format)
		for _, test := range []struct {
			name         string
			image        flow.Image
			wantW, wantH float64
		}{
			{"natural", flow.Image{}, 50.8, 25.4},
			{"width", flow.Image{Width: 40}, 40, 20},
			{"height", flow.Image{Height: 25}, 50, 25},
			{"contain", flow.Image{Width: 40, Height: 10, Fit: "contain"}, 20, 10},
			{"contain page limit", flow.Image{Width: 100, Height: 100, Fit: "contain"}, 80, 40},
			{"stretch", flow.Image{Width: 50, Height: 30, Fit: "stretch"}, 50, 30},
			{"existing default", flow.Image{Width: 50, Height: 30}, 50, 30},
			{"explicit fraction", flow.Image{Width: 80 * 0.25}, 20, 10},
		} {
			t.Run(format+"/"+test.name, func(t *testing.T) {
				d := flow.New(smallOptions())
				img := test.image
				img.Data = data
				d.AddImage(img)
				model, err := d.Build(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				got := placedImage(t, model)
				if math.Abs(got.Width-test.wantW) > 0.001 || math.Abs(got.Height-test.wantH) > 0.001 {
					t.Fatalf("图片尺寸 = %.3f×%.3f，期望 %.3f×%.3f", got.Width, got.Height, test.wantW, test.wantH)
				}
				if !reflect.DeepEqual(*d.Blocks[0].Image, img) {
					t.Fatal("尺寸推算修改了输入")
				}
				if strings.Contains(pageText(model.Pages[0]), "Figure") {
					t.Fatal("无图注时生成了多余说明")
				}
			})
		}
	}
}

func TestImageAlignmentUsesIndentedColumnWidth(t *testing.T) {
	data := imageTestData(t, "PNG")
	for _, test := range []struct {
		align string
		wantX float64
	}{
		{"left", 16}, {"center", 33}, {"", 33}, {"right", 50},
	} {
		t.Run(test.align, func(t *testing.T) {
			d := flow.New(smallOptions())
			d.Blocks = []flow.Block{{Kind: "image", Indent: 6, Image: &flow.Image{Data: data, Width: 40, Align: test.align}}}
			model, err := d.Build(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if got := placedImage(t, model); math.Abs(got.X-test.wantX) > 0.001 {
				t.Fatalf("图片对齐位置 = %.3f，期望 %.3f", got.X, test.wantX)
			}
		})
	}
	opts := smallOptions()
	opts.Columns = flow.Columns{Count: 2, Gap: 6}
	d := flow.New(opts)
	d.AddImage(flow.Image{Data: data, Width: 100, Align: "right"})
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := placedImage(t, model); math.Abs(got.Width-37) > 0.001 || math.Abs(got.X-10) > 0.001 {
		t.Fatalf("图片未适配当前栏宽: %+v", got)
	}
}

// 图片与多行图注整块换页，说明文字在图片之后且可被标准文字提取。
func TestImageCaptionPaginatesTogetherAndWritesStandardObjects(t *testing.T) {
	opts := smallOptions()
	opts.Theme.Caption.SpaceBefore = 3
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 7))
	d.AddImage(flow.Image{Data: imageTestData(t, "PNG"), Width: 60, Height: 30, Fit: "contain", Caption: "First caption line\nSecond caption line"})
	d.AddParagraph("Following")
	model, err := d.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) < 2 {
		t.Fatal("图片与图注未整体换页")
	}
	img := placedImage(t, model)
	caption := false
	imageIndex := -1
	for index, item := range model.Pages[1].Items {
		switch value := item.(type) {
		case creator.Image:
			imageIndex = index
		case creator.Text:
			if strings.Contains(textValue(value), "caption line") {
				if imageIndex < 0 || value.Y < img.Y+img.Height+3-0.001 {
					t.Fatalf("图注顺序或间距错误: %+v", value)
				}
				caption = true
			}
		}
	}
	if !caption {
		t.Fatal("图注未与图片同页")
	}
	var first, second bytes.Buffer
	for _, output := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(t.Context(), output, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("图片图注输出不确定")
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(t.Context(), bytes.NewReader(first.Bytes()), "image-caption.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("图片图注严格校验失败: %+v", report.Issues)
	}
}

func TestImageRejectsInvalidFitAlignmentAndDimensions(t *testing.T) {
	for _, img := range []flow.Image{
		{Align: "middle"}, {Fit: "fill"}, {Fit: "stretch", Width: 40},
		{Width: -1}, {Height: math.NaN()}, {Width: math.Inf(1)}, {Height: math.MaxFloat64},
		{Width: 10, Height: 1000}, {Data: []byte("invalid")},
	} {
		if img.Data == nil {
			img.Data = imageTestData(t, "PNG")
		}
		d := flow.New(smallOptions())
		d.AddImage(img)
		if _, err := d.Build(t.Context()); err == nil || !strings.Contains(err.Error(), "blocks[0]") {
			t.Fatalf("未拒绝错误图片或错误缺少位置: %v", err)
		}
	}
}
