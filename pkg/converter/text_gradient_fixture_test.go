package converter

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/backends/canvasfont"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/testutil"
)

func TestIntroPage5TextGradientsMatchSourceCoordinates(t *testing.T) {
	const input = "../../testdata/ofdrw/intro.ofd"
	const dpi = 144
	ofd, err := parser.NewOFD(input)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("缺少样例: %v", err)
		}
		t.Fatal(err)
	}
	defer ofd.Close()
	fonts := canvasfont.NewFonts(ofd.Documents[0])
	defer fonts.Close()
	var titles []models.TextObject
	if err := ofd.Documents[0].Pages[4].WithPageContent(func(content *models.PageContent) error {
		fonts.RegisterPageGlyphs(ofd.Documents[0], ofd.Documents[0].Pages[4], content)
		for _, layer := range content.Content.Layer {
			for _, item := range layer.Items {
				if item.Text != nil && item.Text.ID >= 187 && item.Text.ID <= 189 {
					titles = append(titles, *item.Text)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(titles) != 3 {
		t.Fatalf("渐变标题数量 = %d，期望 3", len(titles))
	}
	period := introTitlePeriodBounds(t, fonts, titles[2])
	t.Run("PNG", func(t *testing.T) {
		var output bytes.Buffer
		if err := Image(t.Context(), input, Page(5), DPI(dpi), PNG(), Writer(func(int) (io.WriteCloser, error) {
			return nopWriteCloser{&output}, nil
		})); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(&output)
		if err != nil {
			t.Fatal(err)
		}
		assertIntroTitleGradientColors(t, img, titles, dpi)
		assertIntroTitlePeriodComplete(t, img, period, dpi)
	})
	t.Run("PDF", func(t *testing.T) {
		var output bytes.Buffer
		if err := PDF(t.Context(), input, &output, Page(5)); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "page.pdf")
		if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		if tool, err := exec.LookPath("pdftotext"); err == nil {
			text, err := exec.CommandContext(t.Context(), tool, path, "-").CombinedOutput()
			if err != nil {
				t.Fatalf("PDF 文字提取失败: %v %s", err, text)
			}
			for _, title := range titles {
				if !strings.Contains(string(text), title.TextCode[0].Value) {
					t.Errorf("PDF 丢失可复制的标题 %d", title.ID)
				}
			}
		}
		tool, err := exec.LookPath("pdftoppm")
		if err != nil {
			t.Skip("缺少 pdftoppm，跳过 PDF 像素检查")
		}
		images, err := testutil.RasterizePDFPages(t.Context(), tool, output.Bytes(), dpi)
		if err != nil {
			t.Fatal(err)
		}
		assertIntroTitleGradientColors(t, images[0], titles, dpi)
		assertIntroTitlePeriodComplete(t, images[0], period, dpi)
	})
	t.Run("SVG", func(t *testing.T) {
		var output bytes.Buffer
		if err := Image(t.Context(), input, Page(5), SVG(), Writer(func(int) (io.WriteCloser, error) {
			return nopWriteCloser{&output}, nil
		})); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(output.Bytes(), []byte("<linearGradient")) || bytes.Contains(output.Bytes(), []byte("<image")) {
			t.Fatal("SVG 未保留矢量文字渐变")
		}
		var chrome string
		for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
			if path, err := exec.LookPath(name); err == nil {
				chrome = path
				break
			}
		}
		if chrome == "" {
			t.Skip("缺少 Chrome/Chromium，跳过 SVG 浏览器像素检查")
		}
		dir := t.TempDir()
		path, screenshot := filepath.Join(dir, "page.svg"), filepath.Join(dir, "page.png")
		if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, chrome, "--headless", "--no-sandbox", "--disable-gpu", "--disable-background-networking", "--hide-scrollbars", "--user-data-dir="+filepath.Join(dir, "chrome"), "--force-device-scale-factor=1.5", "--window-size=1210,908", "--screenshot="+screenshot, "file://"+filepath.ToSlash(path))
		if log, err := command.CombinedOutput(); err != nil {
			t.Fatalf("SVG 浏览器渲染失败: %v %s", err, log)
		}
		data, err := os.ReadFile(screenshot)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		assertIntroTitleGradientColors(t, img, titles, dpi)
		assertIntroTitlePeriodComplete(t, img, period, dpi)
	})
}

// 预期句号位置直接由源 DeltaX 累加与内嵌字形轮廓计算，独立于绘制 run 的分段结果。
// §11.2 表46：定位数组给出相邻绘制点偏移，不能换成字体自然字宽。
func introTitlePeriodBounds(t *testing.T, fonts *canvasfont.Fonts, title models.TextObject) geom.Rect {
	t.Helper()
	if title.ID != 189 || len(title.TextCode) != 1 || !strings.HasSuffix(title.TextCode[0].Value, "。") || title.CTM == nil {
		t.Fatal("样例末行不再是预期的渐变句号对象")
	}
	family, err := fonts.LoadFont(title.Font)
	if err != nil {
		t.Fatal(err)
	}
	face := fonts.FaceObject(family, title, nil)
	if face == nil {
		t.Fatal("无法加载样例句号字体面")
	}
	path := face.ToPath("。")
	if path == nil || path.Empty() {
		t.Fatal("内嵌字体中没有句号轮廓")
	}
	code := title.TextCode[0]
	x, y := code.X, code.Y
	for _, delta := range code.DeltaX {
		x += delta
	}
	for _, delta := range code.DeltaY {
		y += delta
	}
	ctm := title.CTM
	matrix := geom.Matrix{{ctm[0], ctm[2], title.Boundary.X + ctm[4]}, {ctm[1], ctm[3], title.Boundary.Y + ctm[5]}}
	return path.Bounds().Transform(matrix.Translate(x, y).Scale(1, -1))
}

func assertIntroTitlePeriodComplete(t *testing.T, img image.Image, period geom.Rect, dpi float64) {
	t.Helper()
	scale := dpi / 25.4
	ink := geom.Rect{X0: math.Inf(1), Y0: math.Inf(1), X1: math.Inf(-1), Y1: math.Inf(-1)}
	count := 0
	for y := int(math.Floor(period.Y0*scale)) - 1; y <= int(math.Ceil(period.Y1*scale))+1; y++ {
		for x := int(math.Floor(period.X0*scale)) - 1; x <= int(math.Ceil(period.X1*scale))+1; x++ {
			pixel, ok := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if !ok {
				t.Fatal("无法读取句号像素")
			}
			if pixel.R > 110 || pixel.G < 140 || pixel.B < 170 {
				continue
			}
			ink = ink.AddPoint(geom.Point{X: (float64(x) + .5) / scale, Y: (float64(y) + .5) / scale})
			count++
		}
	}
	// 允许两像素抗锯齿与字体 hinting 差异，四侧均须覆盖到预期字形外沿附近。
	tolerance := 2 / scale
	if count < 30 || ink.X0 > period.X0+tolerance || ink.X1 < period.X1-tolerance ||
		ink.Y0 > period.Y0+tolerance || ink.Y1 < period.Y1-tolerance {
		t.Errorf("渐变句号不完整或偏离源定位：有色像素=%d，边界=%v，期望=%v", count, ink, period)
	}
	center := period.Center()
	pixel, ok := color.NRGBAModel.Convert(img.At(int(center.X*scale), int(center.Y*scale))).(color.NRGBA)
	if !ok {
		t.Fatal("无法读取句号中心像素")
	}
	if pixel.R > 30 || pixel.G > 30 || pixel.B > 30 {
		t.Errorf("句号中心应保留黑底镂空，实际=%v", pixel)
	}
}

func assertIntroTitleGradientColors(t *testing.T, img image.Image, titles []models.TextObject, dpi float64) {
	t.Helper()
	scale := dpi / 25.4
	for _, title := range titles {
		shading := title.FillColor.AxialShd
		gradient := geom.NewLinearGradient(geom.Point(shading.StartPoint), geom.Point(shading.EndPoint))
		for _, segment := range shading.Segment {
			gradient.Grad.Add(segment.Position, segment.Color.Value.RGBA)
		}
		box := title.Boundary
		count, matched := 0, 0
		for y := int(box.Y * scale); y < int((box.Y+box.Height)*scale); y++ {
			for x := int(box.X * scale); x < int((box.X+box.Width)*scale); x++ {
				pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				// 排除黑底、白色正文和低覆盖率的抗锯齿边缘。
				if pixel.R > 110 || pixel.G < 140 || pixel.B < 170 {
					continue
				}
				want := gradient.At((float64(x)+0.5)/scale-box.X, (float64(y)+0.5)/scale-box.Y)
				count++
				if math.Abs(float64(pixel.R)-float64(want.R)) <= 12 && math.Abs(float64(pixel.G)-float64(want.G)) <= 12 && math.Abs(float64(pixel.B)-float64(want.B)) <= 12 {
					matched++
				}
			}
		}
		if count < 500 || float64(matched)/float64(count) < 0.85 {
			t.Errorf("标题 %d 颜色符合源渐变的像素 = %d/%d，期望至少 500 个采样且匹配率 >=85%%", title.ID, matched, count)
		}
	}
}
