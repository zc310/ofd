package render

import (
	"bytes"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/testutil"
	"golang.org/x/image/font/gofont/goregular"
)

type gradientRunEngine struct {
	fakeFontEngine

	fill *CTColor
}

func (e *gradientRunEngine) FaceObject(_ FontFamily, _ models.TextObject, fill *CTColor) FontFace {
	e.fill = fill
	return metricsFontFace{}
}

func TestNativeTextGradientUsesBoundaryCoordinatesAcrossRuns(t *testing.T) {
	stops := geom.Grad{{Offset: 0, Color: color.RGBA{R: 240, A: 255}}, {Offset: 1, Color: color.RGBA{B: 240, A: 255}}}
	linear := geom.NewLinearGradient(geom.Point{X: -17, Y: 58}, geom.Point{X: 162, Y: -14})
	linear.Grad, linear.Extend = stops, [2]bool{false, true}
	radial := geom.NewRadialGradient(geom.Point{X: 15, Y: 10}, 3, geom.Point{X: 20, Y: 10}, 20)
	radial.Grad, radial.Extend = stops, [2]bool{true, false}
	for _, test := range []struct {
		name     string
		ctm      models.CTM
		hScale   float64
		gradient geom.Gradient
	}{
		{"identity_axial", models.CTM{1, 0, 0, 1, 0, 0}, 1, linear},
		{"intro_title_axial", models.CTM{9.8778, 0, 0, 9.8778, 0, 0}, 1, linear},
		{"rotated_axial", models.CTM{0, 2, -2, 0, 3, 4}, 1, linear},
		{"sheared_axial", models.CTM{2, 0.5, 1, 3, 4, -2}, 0.7, linear},
		{"scaled_radial", models.CTM{2, 0, 0, 2, 3, 4}, 1, radial},
		{"rotated_radial", models.CTM{0, 2, -2, 0, 3, 4}, 1, radial},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := &gradientRunEngine{}
			object := models.TextObject{CtText: models.CtText{CTGraphicUnit: models.CTGraphicUnit{CTM: &test.ctm}, HScale: test.hScale}}
			fill := &CTColor{Gradient: test.gradient}
			faces := newTextFaces(engine, metricsFontFamily{}, object, fill, metricsFontFace{}, test.hScale)
			faces.native = true
			for _, origin := range []geom.Point{{X: 0, Y: 0.836}, {X: 7, Y: 3}} {
				engine.fill = nil
				faces.run(origin.X, origin.Y)
				if engine.fill == nil {
					t.Fatal("原生渐变未按 run 重建字体面")
				}
				mapped := engine.fill.Gradient
				switch gradient := mapped.(type) {
				case *geom.LinearGradient:
					if gradient.Extend != linear.Extend {
						t.Fatal("轴向渐变丢失 Extend")
					}
				case *geom.RadialGradient:
					if gradient.Extend != radial.Extend {
						t.Fatal("径向渐变丢失 Extend")
					}
				default:
					t.Fatalf("渐变未保留原生类型: %T", mapped)
				}
				for _, point := range []geom.Point{{}, {X: 2, Y: 0.5}, {X: 9, Y: -2}, {X: -8, Y: 13}} {
					x, y := test.ctm.Transform(origin.X+point.X*test.hScale, origin.Y-point.Y)
					want := test.gradient.At(x, y)
					got := mapped.At(point.X, point.Y)
					if colorDistance(got, want) > 1 {
						t.Errorf("run %v 点 %v 的颜色 = %v，Boundary 坐标 (%g,%g) 应为 %v", origin, point, got, x, y, want)
					}
				}
			}
			if fill.Gradient != test.gradient || linear.Start != (geom.Point{X: -17, Y: 58}) || radial.R0 != 3 {
				t.Fatal("原始渐变被修改")
			}
		})
	}
}

func TestNativeTextGradientAppliesColorAndObjectAlpha(t *testing.T) {
	colorAlpha, objectAlpha := uint8(128), uint8(128)
	source := &models.CTColor{
		Alpha: &colorAlpha,
		AxialShd: &models.CTAxialShd{
			StartPoint: models.StPos{},
			EndPoint:   models.StPos{X: 10},
			Extend:     3,
			Segment: []models.Segment{
				{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
				{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
			},
		},
	}
	gradient := (&Document{}).textShadingGradient(source, &objectAlpha)
	if gradient == nil {
		t.Fatal("原生文字渐变为空")
	}
	for _, x := range []float64{0, 5, 10} {
		if got := gradient.At(x, 0).A; got < 63 || got > 65 {
			t.Errorf("x=%g 的渐变 Alpha=%d，期望颜色 Alpha 与对象 Alpha 叠加后约为 64", x, got)
		}
	}
}

func TestTranslucentGradientTextKeepsAlphaInRasterAndPDF(t *testing.T) {
	if err := RegisterFallbackFont(goregular.TTF, t.Name(), FontRegular); err != nil {
		t.Fatal(err)
	}
	defer UnregisterFallbackFont(t.Name())
	doc := NewDocument(nil, nil)
	defer doc.Close()
	if err := doc.UseFallbackFont(t.Name()); err != nil {
		t.Fatal(err)
	}
	alpha := uint8(128)
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 5, Y: 5, Width: 50, Height: 15}, Alpha: &alpha},
		Size:          10,
		TextCode:      []models.TextCode{{X: 0, Y: 10, Value: "Alpha"}},
		FillColor: &models.CTColor{Alpha: &alpha, AxialShd: &models.CTAxialShd{
			StartPoint: models.StPos{}, EndPoint: models.StPos{X: 40}, Extend: 3,
			Segment: []models.Segment{
				{Position: 0, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}},
				{Position: 1, PositionSet: true, Color: models.CTColor{Value: &models.Color{RGBA: color.RGBA{B: 255, A: 255}}}},
			},
		}},
	}}
	surface, err := NewVectorSurface(120, 25, func(ctx DrawContext) {
		doc.Text(ctx, object, nil, models.StBox{Width: 120, Height: 25})
		// 紧接着绘制不透明渐变，检查不可见文字层不会污染后续图元的 Alpha。
		opaque := object
		opaque.Boundary.X = 65
		opaque.Alpha = nil
		fill := *object.FillColor
		fill.Alpha = nil
		opaque.FillColor = &fill
		doc.Text(ctx, opaque, nil, models.StBox{Width: 120, Height: 25})
	})
	if err != nil {
		t.Fatal(err)
	}
	img := surface.Rasterize(geom.DPI(144))
	var maxAlpha uint8
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx()/2; x++ {
			maxAlpha = max(maxAlpha, img.RGBAAt(x, y).A)
		}
	}
	if maxAlpha < 63 || maxAlpha > 65 {
		t.Fatalf("文字栅格 Alpha=%d，期望两层透明度叠乘后约64", maxAlpha)
	}
	t.Run("PDF", func(t *testing.T) {
		var output bytes.Buffer
		pdf, err := NewPDFDocument(&output, PDFOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := pdf.AddPage(surface, nil); err != nil {
			t.Fatal(err)
		}
		if err := pdf.Close(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(img.Pix, surface.Rasterize(geom.DPI(144)).Pix) {
			t.Fatal("PDF 输出改写了共享文字或渐变，导致后续栅格化变化")
		}
		var svg bytes.Buffer
		if err := surface.Write(&svg, "svg"); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(svg.Bytes(), []byte("<linearGradient")) || bytes.Contains(svg.Bytes(), []byte("<image")) {
			t.Fatal("PDF 的透明渐变回退影响了 SVG 矢量输出")
		}
		if tool, err := exec.LookPath("pdftotext"); err == nil {
			path := filepath.Join(t.TempDir(), "alpha.pdf")
			if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			text, err := exec.CommandContext(t.Context(), tool, path, "-").CombinedOutput()
			if err != nil || !strings.Contains(string(text), "Alpha") {
				t.Fatalf("透明渐变文字不可提取：%v %s", err, text)
			}
		}
		tool, err := exec.LookPath("pdftoppm")
		if err != nil {
			t.Skip("缺少 pdftoppm，跳过 PDF 像素检查")
		}
		pages, err := testutil.RasterizePDFPages(t.Context(), tool, output.Bytes(), 144)
		if err != nil {
			t.Fatal(err)
		}
		minGreen, opaqueGreen := uint8(255), uint8(255)
		for y := 0; y < pages[0].Bounds().Dy(); y++ {
			for x := 0; x < pages[0].Bounds().Dx(); x++ {
				pixel := color.NRGBAModel.Convert(pages[0].At(x, y)).(color.NRGBA)
				if x < pages[0].Bounds().Dx()/2 {
					minGreen = min(minGreen, pixel.G)
				} else {
					opaqueGreen = min(opaqueGreen, pixel.G)
				}
			}
		}
		// 红蓝渐变本身不含绿色；白底的剩余绿色直接反映文字不透明度。
		if minGreen < 188 || minGreen > 194 {
			t.Fatalf("PDF 文字的白底绿色分量=%d，期望约191", minGreen)
		}
		if opaqueGreen > 3 {
			t.Fatalf("后续不透明渐变文字受到 Alpha 污染：绿色分量=%d，期望0", opaqueGreen)
		}
	})
}

func colorDistance(a, b color.RGBA) int {
	distance := 0
	for index, value := range []uint8{a.R, a.G, a.B, a.A} {
		delta := int(value) - int([]uint8{b.R, b.G, b.B, b.A}[index])
		if delta < 0 {
			delta = -delta
		}
		distance = max(distance, delta)
	}
	return distance
}
