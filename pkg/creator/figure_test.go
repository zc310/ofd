package creator

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// figureTestPNG 造一张 w×h 的 PNG，用于尺寸与比例推算的测试。
func figureTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// figureDocWith 把图元包成文档并生成 OFD，返回各页 Content.xml。
func figureDocWith(t *testing.T, items ...Item) []string {
	t.Helper()
	doc := Document{
		ID:       "figure",
		PageSize: A4,
		Fonts:    []Font{{Name: "SimHei", FamilyName: "SimHei", Charset: "prc"}},
		Pages:    []Page{{Items: items}},
	}
	var buf bytes.Buffer
	if err := Create(doc, &buf); err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("读取生成的 OFD 失败: %v", err)
	}
	var pages []string
	for _, file := range zr.File {
		if !strings.HasSuffix(file.Name, "/Content.xml") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body := new(bytes.Buffer)
		if _, err := body.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		pages = append(pages, body.String())
	}
	return pages
}

// 只给宽度时高度按图片原始比例推出。原图 2:1，40mm 宽对应 20mm 高。
func TestFigureDerivesHeightFromAspectRatio(t *testing.T) {
	value := Figure{Width: 170, ImageWidth: 40}
	value.Image.Data = figureTestPNG(t, 400, 200)
	value.Image.Format = "PNG"
	geometry, err := resolveFigure(value, nil)
	if err != nil {
		t.Fatalf("解尺寸失败: %v", err)
	}
	if geometry.Width != 40 || geometry.Height != 20 {
		t.Errorf("图片尺寸 = %.2f x %.2f，期望 40 x 20（原始 2:1）", geometry.Width, geometry.Height)
	}
}

// 只给高度时宽度按比例推出。
func TestFigureDerivesWidthFromHeight(t *testing.T) {
	value := Figure{Width: 170, ImageHeight: 25}
	value.Image.Data = figureTestPNG(t, 400, 200)
	value.Image.Format = "PNG"
	geometry, err := resolveFigure(value, nil)
	if err != nil {
		t.Fatalf("解尺寸失败: %v", err)
	}
	if geometry.Width != 50 || geometry.Height != 25 {
		t.Errorf("图片尺寸 = %.2f x %.2f，期望 50 x 25", geometry.Width, geometry.Height)
	}
}

// 尺寸都不给时：给了图块宽度就填满它。
func TestFigureFillsFrameWidthWhenSizeUnset(t *testing.T) {
	value := Figure{Width: 120}
	value.Image.Data = figureTestPNG(t, 400, 200)
	value.Image.Format = "PNG"
	geometry, err := resolveFigure(value, nil)
	if err != nil {
		t.Fatalf("解尺寸失败: %v", err)
	}
	if geometry.Width != 120 || geometry.Height != 60 {
		t.Errorf("图片尺寸 = %.2f x %.2f，期望 120 x 60", geometry.Width, geometry.Height)
	}
}

// 尺寸与图块宽度都不给时按图片原始尺寸落位（96dpi）。
func TestFigureUsesNaturalSizeWhenNothingSet(t *testing.T) {
	value := Figure{}
	value.Image.Data = figureTestPNG(t, 192, 96)
	value.Image.Format = "PNG"
	geometry, err := resolveFigure(value, nil)
	if err != nil {
		t.Fatalf("解尺寸失败: %v", err)
	}
	// 192px / 96dpi = 2 英寸 = 50.8mm
	if geometry.Width < 50.7 || geometry.Width > 50.9 {
		t.Errorf("图片宽 %.2fmm，期望约 50.8mm（192px @96dpi）", geometry.Width)
	}
}

// align 共用于图片与说明文字，两者的水平位置都要跟着变。
func TestFigureAlignPositionsImageAndCaption(t *testing.T) {
	data := figureTestPNG(t, 400, 200)
	cases := []struct {
		align    string
		wantX    float64
		captionL string
	}{
		{"left", 0, "图"},
		{"center", 65, "图"},
		{"right", 130, "图"},
	}
	for _, c := range cases {
		value := Figure{X: 0, Y: 20, Width: 170, Align: c.align, ImageWidth: 40,
			Caption: "图 1 说明", CaptionStyle: &CaptionStyle{Font: "SimHei", Size: 4}}
		value.Image.Data = data
		value.Image.Format = "PNG"
		geometry, err := resolveFigure(value, nil)
		if err != nil {
			t.Fatalf("%s 解尺寸失败: %v", c.align, err)
		}
		if geometry.X != c.wantX {
			t.Errorf("align=%s 图片 X = %.2f，期望 %.2f", c.align, geometry.X, c.wantX)
		}
		caption := figureCaption(value, geometry)
		if caption == nil {
			t.Fatalf("align=%s 未生成说明文字", c.align)
		}
		if (c.align == "center" && caption.X <= geometry.X) ||
			(c.align == "right" && caption.X <= geometry.X) {
			t.Errorf("align=%s 说明文字 X=%.2f 未随图片对齐（图片 X=%.2f）", c.align, caption.X, geometry.X)
		}
	}
}

// contain 在两个尺寸都超出框宽时按小的比例缩放，不变形；stretch 则拉伸填满。
func TestFigureFitContainVersusStretch(t *testing.T) {
	data := figureTestPNG(t, 400, 200) // 2:1

	contained := Figure{Width: 60, ImageWidth: 100, ImageHeight: 100, Fit: "contain"}
	contained.Image.Data = data
	contained.Image.Format = "PNG"
	geometry, err := resolveFigure(contained, nil)
	if err != nil {
		t.Fatalf("contain 解尺寸失败: %v", err)
	}
	if geometry.Width != 60 {
		t.Errorf("contain 图片宽 %.2f，期望缩到框宽 60", geometry.Width)
	}
	if ratio := geometry.Width / geometry.Height; ratio < 1.99 || ratio > 2.01 {
		t.Errorf("contain 变形了，宽高比 %.3f，期望 2.000", ratio)
	}

	stretched := Figure{Width: 60, ImageWidth: 50, ImageHeight: 30, Fit: "stretch"}
	stretched.Image.Data = data
	stretched.Image.Format = "PNG"
	geometry, err = resolveFigure(stretched, nil)
	if err != nil {
		t.Fatalf("stretch 解尺寸失败: %v", err)
	}
	if geometry.Width != 50 || geometry.Height != 30 {
		t.Errorf("stretch 图片尺寸 = %.2f x %.2f，期望按给定值 50 x 30", geometry.Width, geometry.Height)
	}
}

// image_fill_width 把 image_width 当成图块宽度的比例。
func TestFigureFillWidthIsRatio(t *testing.T) {
	value := Figure{Width: 160, ImageWidth: 0.25, ImageFillWidth: true}
	value.Image.Data = figureTestPNG(t, 400, 200)
	value.Image.Format = "PNG"
	geometry, err := resolveFigure(value, nil)
	if err != nil {
		t.Fatalf("解尺寸失败: %v", err)
	}
	if geometry.Width != 40 {
		t.Errorf("图片宽 %.2f，期望图块宽的 25%% 即 40", geometry.Width)
	}
	if _, err := resolveFigure(Figure{Width: 160, ImageWidth: 2, ImageFillWidth: true}, nil); err == nil {
		t.Error("image_fill_width 为真时比例大于 1 应当报错")
	}
}

// 说明文字在图片下方，间距由 Gap 控制。
func TestFigureCaptionBelowImage(t *testing.T) {
	value := Figure{X: 20, Y: 30, Width: 170, ImageWidth: 40,
		Caption: "图 1 说明", CaptionStyle: &CaptionStyle{Font: "SimHei", Size: 4, Gap: 3}}
	value.Image.Data = figureTestPNG(t, 400, 200)
	value.Image.Format = "PNG"
	geometry, err := resolveFigure(value, nil)
	if err != nil {
		t.Fatalf("解尺寸失败: %v", err)
	}
	caption := figureCaption(value, geometry)
	if caption == nil {
		t.Fatal("未生成说明文字")
	}
	wantTop := geometry.Y + geometry.Height + 3
	if caption.Y != wantTop {
		t.Errorf("说明文字 Y = %.2f，期望图片底边下方 3mm 即 %.2f", caption.Y, wantTop)
	}
	if caption.Height != 4 {
		t.Errorf("说明文字高 %.2f，期望字号 4", caption.Height)
	}
}

// 说明默认间距是 6mm。
func TestFigureCaptionDefaultGap(t *testing.T) {
	value := Figure{X: 0, Y: 10, Width: 100, ImageWidth: 50,
		Caption: "图 1", CaptionStyle: &CaptionStyle{Font: "SimHei", Size: 4}}
	value.Image.Data = figureTestPNG(t, 400, 200)
	value.Image.Format = "PNG"
	geometry, err := resolveFigure(value, nil)
	if err != nil {
		t.Fatalf("解尺寸失败: %v", err)
	}
	caption := figureCaption(value, geometry)
	if caption == nil {
		t.Fatal("未生成说明文字")
	}
	if got := caption.Y - (geometry.Y + geometry.Height); got != defaultCaptionGap {
		t.Errorf("默认间距 %.2fmm，期望 %v", got, defaultCaptionGap)
	}
}

// 说明留空时不画任何文字——自动生成"图 N"会让用户没法关掉。
func TestFigureCaptionOptional(t *testing.T) {
	value := Figure{X: 0, Y: 10, Width: 100, ImageWidth: 50}
	value.Image.Data = figureTestPNG(t, 400, 200)
	value.Image.Format = "PNG"
	geometry, err := resolveFigure(value, nil)
	if err != nil {
		t.Fatalf("解尺寸失败: %v", err)
	}
	if caption := figureCaption(value, geometry); caption != nil {
		t.Errorf("说明留空时不应生成文字，实际 %+v", caption)
	}
	// 包内也不该出现多余的 TextObject。
	pages := figureDocWith(t, value)
	if strings.Contains(pages[0], "TextObject") {
		t.Error("说明留空时页面里出现了文字对象")
	}
	if !strings.Contains(pages[0], "ImageObject") {
		t.Error("页面里没有图片对象")
	}
}

// 图块总高要算上说明文字，将来图片进表格单元格时要用。
func TestFigureTotalHeightIncludesCaption(t *testing.T) {
	value := Figure{X: 0, Y: 0, Width: 170, ImageWidth: 40,
		Caption: "图 1 说明", CaptionStyle: &CaptionStyle{Font: "SimHei", Size: 4, Gap: 5}}
	value.Image.Data = figureTestPNG(t, 400, 200)
	value.Image.Format = "PNG"
	geometry, err := resolveFigure(value, nil)
	if err != nil {
		t.Fatalf("解尺寸失败: %v", err)
	}
	want := 20.0 + 5 + 4
	if got := geometry.TotalHeight(value.Caption, value.CaptionStyle); got != want {
		t.Errorf("图块总高 %.2fmm，期望 %.2fmm（图片 20 + 间距 5 + 字号 4）", got, want)
	}
	if got := geometry.TotalHeight("", value.CaptionStyle); got != 20 {
		t.Errorf("无说明时总高 %.2fmm，期望等于图片高 20", got)
	}
}

// 拿不到像素尺寸的格式要报错并说清怎么办，而不是默默拉伸变形。
func TestFigureRequiresSizeForUndecodableFormat(t *testing.T) {
	value := Figure{Width: 170, Image: Image{Format: "WEBP", Data: []byte("RIFF....WEBPVP8 ")}}
	_, err := resolveFigure(value, nil)
	if err == nil {
		t.Fatal("WEBP 未指定尺寸时应报错")
	}
	if !strings.Contains(err.Error(), "image_width") {
		t.Errorf("错误信息应提示设置 image_width，实际: %v", err)
	}
	// 只给一个尺寸同样补不出另一边。
	half := Figure{Width: 170, ImageWidth: 40, Image: Image{Format: "WEBP", Data: []byte("RIFF....WEBPVP8 ")}}
	if _, err := resolveFigure(half, nil); err == nil {
		t.Error("只给一个尺寸且格式不可解析时应报错")
	}
}

// PNG / JPEG / GIF 都能读出尺寸；同尺寸下三者推算结果一致。
func TestFigureReadsPixelSizeAcrossFormats(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 400, 200))
	var pngBuf, jpegBuf, gifBuf bytes.Buffer
	if err := png.Encode(&pngBuf, source); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegBuf, source, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&gifBuf, source, nil); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"PNG": pngBuf.Bytes(), "JPEG": jpegBuf.Bytes(), "GIF": gifBuf.Bytes()} {
		value := Figure{Width: 170, ImageWidth: 40, Image: Image{Format: name, Data: data}}
		geometry, err := resolveFigure(value, nil)
		if err != nil {
			t.Errorf("%s 解尺寸失败: %v", name, err)
			continue
		}
		if geometry.Height != 20 {
			t.Errorf("%s 图片高 %.2f，期望 20（400x200 的 2:1）", name, geometry.Height)
		}
	}
}

// 参数非法时报错要具体，别静默按默认值走。
func TestFigureRejectsInvalidFields(t *testing.T) {
	data := figureTestPNG(t, 400, 200)
	base := func() Figure {
		return Figure{Width: 170, ImageWidth: 40, Image: Image{Format: "PNG", Data: data}}
	}
	if err := validateFigure(base(), nil); err != nil {
		t.Fatalf("合法参数应通过: %v", err)
	}
	for name, mutate := range map[string]func(*Figure){
		"对齐方式非法": func(f *Figure) { f.Align = "middle" },
		"适配方式非法": func(f *Figure) { f.Fit = "fill" },
		"尺寸为负":   func(f *Figure) { f.ImageWidth = -1 },
		"间距为负": func(f *Figure) {
			f.Caption = "图 1"
			f.CaptionStyle = &CaptionStyle{Gap: -1}
		},
		"无图片数据": func(f *Figure) { f.Image.Data = nil },
	} {
		value := base()
		mutate(&value)
		if err := validateFigure(value, nil); err == nil {
			t.Errorf("%s 应当报错", name)
		}
	}
}

// 生成 OFD 后说明文字与图片对象都在，且顺序是先图后文。
func TestFigureExpandsToImageAndText(t *testing.T) {
	value := Figure{X: 20, Y: 30, Width: 170, Align: "center", ImageWidth: 60,
		Caption: "图 1 营收走势", CaptionStyle: &CaptionStyle{Font: "SimHei", Size: 4}}
	value.Image.Data = figureTestPNG(t, 400, 200)
	value.Image.Format = "PNG"
	page := figureDocWith(t, value)[0]
	imageAt := strings.Index(page, "ImageObject")
	textAt := strings.Index(page, "TextObject")
	if imageAt < 0 {
		t.Fatal("缺少图片对象")
	}
	if textAt < 0 {
		t.Fatal("缺少说明文字对象")
	}
	if imageAt > textAt {
		t.Error("说明文字应排在图片之后")
	}
	if !strings.Contains(page, "图 1 营收走势") {
		t.Error("说明文字内容缺失")
	}
}
