package render

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/renderstate"
)

func solidRegressionPattern(t *testing.T) *models.CtPattern {
	t.Helper()
	cell := clipTestPath(t, `<PathObject Boundary="0 0 4 4" Fill="true" Stroke="false"><FillColor Value="255 0 0"/><AbbreviatedData>M 0 0 L 4 0 L 4 4 L 0 4 C</AbbreviatedData></PathObject>`)
	return &models.CtPattern{Width: 4, Height: 4, CellContent: models.CellContent{CTPageBlock: models.CTPageBlock{
		// 完全重叠的两个子图元用于区分整组透明度和逐个子图元透明度。
		Items: []models.PageItem{{Kind: models.PageItemPath, Path: &cell}, {Kind: models.PageItemPath, Path: &cell}},
	}}}
}

func TestPathPatternCompositesObjectAndInheritedColorAlphaOnce(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		t.Run(map[bool]string{false: "对象颜色", true: "继承颜色"}[inherited], func(t *testing.T) {
			doc := clipTestDocument(t)
			object := clipTestPath(t, `<PathObject Boundary="2 2 20 20" Fill="true" Stroke="false" Alpha="128"><AbbreviatedData>M 0 0 L 20 0 L 20 20 L 0 20 C</AbbreviatedData></PathObject>`)
			source := &models.CTColor{Alpha: new(uint8(128)), Pattern: solidRegressionPattern(t)}
			object.FillColor = source
			var dp *models.DrawParam
			if inherited {
				dp, object.FillColor = &models.DrawParam{FillColor: source}, nil
			}
			surface := clipTestSurface(t, func(ctx DrawContext) { doc.Path(ctx, object, dp, models.StBox{Width: 40, Height: 40}) })
			img := surface.Rasterize(geom.DPI(254), nil)
			r, g, b, a := img.At(111, 111).RGBA()
			if a < 63*257 || a > 65*257 || r < 15000 || g != 0 || b != 0 {
				t.Fatalf("路径底纹未整组合成 Alpha：%d %d %d %d", r, g, b, a)
			}
			t.Run("PDF像素", func(t *testing.T) {
				img := clipPDFImage(t, surface, 254)
				r, g, b, _ := img.At(111, 111).RGBA()
				if r < 64000 || g < 48000 || g > 50000 || b != g {
					t.Fatalf("PDF 底纹透明度错误：%d %d %d", r, g, b)
				}
			})
			if source.Pattern == nil || *source.Alpha != 128 || *object.Alpha != 128 {
				t.Fatal("底纹合成修改了共享资源")
			}
		})
	}
}

func TestPatternTextFailureFallsBackAndKeepsPDFText(t *testing.T) {
	for _, failure := range []string{"离屏预算", "离屏工厂", "图块预算", "无效平铺"} {
		for _, stroke := range []bool{false, true} {
			t.Run(failure+map[bool]string{false: "填充", true: "描边"}[stroke], func(t *testing.T) {
				doc := clipTestDocument(t)
				object := regressionTextObject()
				object.Alpha = new(uint8(128))
				source := &models.CTColor{Alpha: new(uint8(128)), Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}, Pattern: solidRegressionPattern(t)}
				object.FillColor = source
				if stroke {
					object.Fill.Set(false)
					object.FillColor, object.StrokeColor, object.Stroke, object.LineWidth = nil, source, true, .5
				}
				if failure == "无效平铺" {
					source.Pattern.Width = 0
				}
				if failure == "离屏工厂" {
					setOffscreenSurfaceFactory(t, func(float64, float64, geom.Resolution) OffscreenSurface { return nil })
				}
				var budget renderBudget
				budget.reset()
				budget.Report = &renderstate.ReportBuilder{}
				switch failure {
				case "离屏预算":
					if !budget.AllowOffscreenPixels(renderstate.MaxOffscreenPixels, 1, 25.4) {
						t.Fatal("预消费预算失败")
					}
				case "图块预算":
					if !budget.AllowPatternTiles(renderstate.MaxPatternTiles) {
						t.Fatal("预消费预算失败")
					}
				}
				surface := clipTestSurface(t, func(ctx DrawContext) {
					doc.textWithBudget(ctx, object, nil, models.StBox{Width: 40, Height: 40}, nil, nil, &budget)
				})
				wantCode := "pattern_fill_failed"
				if stroke {
					wantCode = "pattern_stroke_failed"
				}
				found := false
				for _, diagnostic := range budget.Report.Diagnostics {
					if diagnostic.Code == wantCode {
						found = true
					}
				}
				if !found {
					t.Fatalf("缺少底纹失败诊断：%+v", budget.Report.Diagnostics)
				}
				fallback := patternFallbackColor(source)
				if stroke {
					object.StrokeColor = fallback
				} else {
					object.FillColor = fallback
				}
				expected := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40}) })
				actualPixels, expectedPixels := surface.Rasterize(geom.DPI(254), nil), expected.Rasterize(geom.DPI(254), nil)
				if !bytes.Equal(actualPixels.Pix, expectedPixels.Pix) {
					t.Fatal("失败回退与普通纯色文字不一致")
				}
				_, _, x1, _ := contentImageBounds(actualPixels)
				if x1 <= 0 {
					t.Fatal("回退文字不可见")
				}
				t.Run("PDF文字", func(t *testing.T) { checkPDFExtractedText(t, surface, "AB") })
				t.Run("PDF像素", func(t *testing.T) {
					actual, expected := clipPDFImage(t, surface, 254), clipPDFImage(t, expected, 254)
					if !equalRegressionPixels(actual, expected) {
						t.Fatal("PDF 回退与普通纯色文字不一致")
					}
				})
				if source.Pattern == nil {
					t.Fatal("回退修改了共享底纹")
				}
			})
		}
	}
}

func equalRegressionPixels(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				return false
			}
		}
	}
	return true
}
