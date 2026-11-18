package render

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/content"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

func TestSimpleCompositeSizeMismatchPreservesScaleInRasterAndPDF(t *testing.T) {
	path := writeCompositeOFD(t, 600, 600, compositeRect20, false)
	x0, y0, x1, y1 := compositeInkBox(t, path)
	if abs(x0-25) > 1.2 || abs(y0-25) > 1.2 || abs(x1-x0-10) > 1.2 || abs(y1-y0-10) > 1.2 {
		t.Fatalf("无裁剪单路径复合图元被拉伸：(%g,%g)-(%g,%g)", x0, y0, x1, y1)
	}
	ofd, err := parser.NewOFD(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := NewDocument(color.White, ofd.Documents[0], 0)
	defer doc.Close()
	surface, err := doc.Page(doc.Pages[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Run("PDF pixels", func(t *testing.T) {
		img := clipPDFImage(t, surface, 254)
		if r, _, _, _ := img.At(300, 300).RGBA(); r > 1000 {
			t.Fatal("PDF 中方块应位于 (25,25)-(35,35)mm")
		}
		for _, point := range [][2]int{{10, 10}, {50, 50}, {90, 90}} {
			if r, _, _, _ := img.At(point[0]*10, point[1]*10).RGBA(); r < 64000 {
				t.Fatal("PDF 中小方块被拉伸铺满 Boundary")
			}
		}
	})
}

func TestIncompletePageItemsAreSkippedByRenderingAndTextCollection(t *testing.T) {
	items := []spec.PageItem{ //nolint:prealloc // 分两步构造缺失载荷及其嵌套副本，便于核对用例。
		{Kind: spec.PageItemPath}, {Kind: spec.PageItemImage}, {Kind: spec.PageItemText},
		{Kind: spec.PageItemComposite}, {Kind: spec.PageItemBlock},
	}
	items = append(items, spec.PageItem{Kind: spec.PageItemBlock, Block: &spec.PageBlock{CTPageBlock: spec.CTPageBlock{Items: append([]spec.PageItem(nil), items...)}}})
	doc := &Document{}
	var budget renderBudget
	budget.reset()
	doc.drawItems(nil, items, nil, spec.StBox{}, &budget)
	for _, item := range items {
		doc.annotItem(nil, item, spec.StBox{}, spec.StBox{}, false, &budget)
	}
	visited := 0
	frame := contentFrame{Scope: &contentSource{Document: doc, budget: &budget}}
	content.WalkItems(frame, items, func(_ contentFrame, _ spec.PageItem) { visited++ })
	if visited != 0 {
		t.Fatal("缺失载荷的图元不应生成文字或占用 run 索引")
	}
}

func TestClosedDocumentRejectsPageDrawing(t *testing.T) {
	doc := NewDocument(color.White, nil, 0)
	doc.Close()
	for _, document := range []*Document{doc, nil} {
		if err := document.Draw(nil, nil); err == nil {
			t.Fatal("已关闭文档接受 Draw")
		}
		if surface, err := document.Page(nil); err == nil || surface != nil {
			t.Fatal("已关闭文档接受 Page")
		}
		if img, err := document.RasterizePage(nil, BackendCanvas, geom.DPI(96)); err == nil || img != nil {
			t.Fatal("已关闭文档接受 RasterizePage")
		}
	}
}

func regressionTextObject() spec.TextObject {
	return spec.TextObject{CtText: spec.CtText{
		CTGraphicUnit: spec.CTGraphicUnit{Boundary: spec.StBox{X: 2, Y: 2, Width: 35, Height: 30}},
		Size:          8, TextCode: []spec.TextCode{{X: 2, Y: 12, Value: "AB"}},
	}}
}

func TestVerticalDeltaWithoutHorizontalDeltaRendersAllGlyphs(t *testing.T) {
	doc := clipTestDocument(t)
	object := spec.TextObject{CtText: spec.CtText{ //nolint:modernize // 当前 Go 编译器仍要求显式嵌入字段类型。
		CTGraphicUnit: spec.CTGraphicUnit{Boundary: spec.StBox{X: 5, Y: 5, Width: 7, Height: 30}}, //nolint:modernize // 同上。
		Size:          6, TextCode: []spec.TextCode{{X: 1, Y: 6, Value: "AAA", DeltaY: spec.StArray[float64]{9, 9}}},
	}}
	render := func() VectorSurface {
		return clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, spec.StBox{Width: 40, Height: 40}) })
	}
	implicit := render()
	object.TextCode[0].DeltaX = spec.StArray[float64]{0, 0}
	explicit := render()
	check := func(t *testing.T, got, want image.Image) {
		t.Helper()
		count := [3]int{}
		for y := range 400 {
			for x := range 400 {
				gr, _, _, ga := got.At(x, y).RGBA()
				wr, _, _, wa := want.At(x, y).RGBA()
				if gr != wr || ga != wa {
					t.Fatalf("缺省横向增量与显式零增量的像素不一致：(%d,%d)", x, y)
				}
				if x >= 50 && x < 120 && y >= 50 && y < 320 && ga > 0 && gr < 30000 {
					count[(y-50)/90]++
				}
			}
		}
		for i, n := range count {
			if n == 0 {
				t.Errorf("第 %d 个字没有可见像素", i+1)
			}
		}
	}
	t.Run("光栅", func(t *testing.T) {
		check(t, implicit.Rasterize(geom.DPMM(10), nil), explicit.Rasterize(geom.DPMM(10), nil))
	})
	t.Run("PDF", func(t *testing.T) {
		check(t, clipPDFImage(t, implicit, 254), clipPDFImage(t, explicit, 254))
		if text := extractPDFText(t, implicit); strings.Count(text, "A") != 3 {
			t.Fatalf("PDF 提取文字不完整：%q", text)
		}
	})
}

func TestInvoice999VerticalBuyerTextLayout(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "ofdrw", "999.ofd")
	ofd, err := parser.NewOFD(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少样本 %s：%v", path, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := NewDocument(color.White, ofd.Documents[0], 0)
	defer doc.Close()
	layouts, err := doc.QueryTextLayouts(doc.Pages[0], TextQueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, layout := range layouts {
		if layout.Text != "购买方" {
			continue
		}
		if len(layout.Glyphs) != 3 {
			t.Fatalf("购买方文字层字符数错误：%+v", layout.Glyphs)
		}
		for i, glyph := range layout.Glyphs {
			if math.Abs(glyph.X-layout.Glyphs[0].X) > 1e-6 ||
				math.Abs(glyph.Y-layout.Glyphs[0].Y-float64(i)*6.35) > 1e-6 {
				t.Fatalf("购买方字符 %d 定位错误：%+v", i, layout.Glyphs)
			}
		}
		return
	}
	t.Fatal("首页模板缺少购买方文字")
}

func TestMixedControlCharactersDoNotDrawNotdef(t *testing.T) {
	doc := clipTestDocument(t)
	for _, shading := range []bool{false, true} {
		for _, positioned := range []bool{false, true} {
			object := regressionTextObject()
			if shading {
				object.FillColor = &spec.CTColor{AxialShd: &spec.CTAxialShd{
					EndPoint: spec.StPos{X: 20}, Extend: 3, Segment: []spec.Segment{
						{Color: spec.CTColor{Value: &spec.ColorValue{RGBA: color.RGBA{R: 255, A: 255}}}},
						{Color: spec.CTColor{Value: &spec.ColorValue{RGBA: color.RGBA{B: 255, A: 255}}}},
					},
				}}
			}
			if positioned {
				object.TextCode[0].DeltaX = spec.StArray[float64]{6}
			}
			render := func() VectorSurface {
				return clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, spec.StBox{Width: 40, Height: 40}) })
			}
			clean := render().Rasterize(geom.DPI(254), nil)
			object.TextCode[0].Value = "A\x7f\u009b\uFFFDB"
			if positioned {
				object.TextCode[0].DeltaX = spec.StArray[float64]{6, 0, 0, 0}
			}
			surface := render()
			if mixed := surface.Rasterize(geom.DPI(254), nil); !bytes.Equal(clean.Pix, mixed.Pix) {
				t.Fatalf("控制字符改变了可见文字：shading=%v positioned=%v", shading, positioned)
			}
			if !shading && !positioned {
				checkPDFExtractedText(t, surface, "AB")
			}
		}
	}
}

func checkPDFExtractedText(t *testing.T, surface VectorSurface, want string) {
	t.Helper()
	text := extractPDFText(t, surface)
	if !strings.Contains(text, want) {
		t.Fatalf("PDF 文字提取=%q，期望 %q", text, want)
	}
}

func extractPDFText(t *testing.T, surface VectorSurface) string {
	t.Helper()
	tool, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("缺少 pdftotext")
	}
	path := filepath.Join(t.TempDir(), "text.pdf")
	if err := os.WriteFile(path, clipPDFBytes(t, surface), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := exec.CommandContext(t.Context(), tool, "-raw", path, "-").CombinedOutput()
	if err != nil {
		t.Fatalf("PDF 文字提取失败：%v，输出=%q", err, text)
	}
	return string(text)
}

func TestTextPatternKeepsColorsOpacityClipAndPDFText(t *testing.T) {
	doc := clipTestDocument(t)
	object := regressionTextObject()
	red := clipTestPath(t, `<PathObject Boundary="0 0 1 2" Fill="true" Stroke="false"><FillColor Value="255 0 0"/><AbbreviatedData>M 0 0 L 1 0 L 1 2 L 0 2 C</AbbreviatedData></PathObject>`)
	blue := clipTestPath(t, `<PathObject Boundary="1 0 1 2" Fill="true" Stroke="false"><FillColor Value="0 0 255"/><AbbreviatedData>M 0 0 L 1 0 L 1 2 L 0 2 C</AbbreviatedData></PathObject>`)
	alpha := uint8(128)
	object.Alpha = &alpha
	object.FillColor = &spec.CTColor{Alpha: &alpha, Pattern: &spec.CtPattern{
		Width: 2, Height: 2, CellContent: spec.CellContent{CTPageBlock: spec.CTPageBlock{Items: []spec.PageItem{
			{Kind: spec.PageItemPath, Path: &red}, {Kind: spec.PageItemPath, Path: &blue},
		}}},
	}}
	surface := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, spec.StBox{Width: 40, Height: 40}) })
	checkColors := func(t *testing.T, img image.Image, pdf bool) {
		t.Helper()
		redCount, blueCount := 0, 0
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if pdf {
					if r > 60000 && g > 45000 && g < 52000 && b < 52000 {
						redCount++
					}
					if b > 60000 && g > 45000 && g < 52000 && r < 52000 {
						blueCount++
					}
				} else {
					if a > 65*257 {
						t.Fatal("底纹透明度未按整组相乘")
					}
					if r > 12000 && g == 0 && b == 0 {
						redCount++
					}
					if b > 12000 && g == 0 && r == 0 {
						blueCount++
					}
				}
			}
		}
		if redCount < 20 || blueCount < 20 {
			t.Fatalf("底纹退化为纯色：red=%d blue=%d", redCount, blueCount)
		}
	}
	t.Run("Raster", func(t *testing.T) { checkColors(t, surface.Rasterize(geom.DPI(254), nil), false) })
	t.Run("PDF pixels", func(t *testing.T) { checkColors(t, clipPDFImage(t, surface, 254), true) })
	t.Run("PDF text", func(t *testing.T) { checkPDFExtractedText(t, surface, "AB") })
	object.Clips = rectangleClip(0.5, 30)
	clipped := clipTestSurface(t, func(ctx DrawContext) { doc.Text(ctx, object, nil, spec.StBox{Width: 40, Height: 40}) })
	for _, value := range clipped.Rasterize(geom.DPI(254), nil).Pix {
		if value != 0 {
			t.Fatal("与字形不相交的裁剪未清除底纹")
		}
	}
}

func TestTextPatternAllowsSameFontInsideCell(t *testing.T) {
	for _, stroke := range []bool{false, true} {
		t.Run(map[bool]string{false: "填充", true: "描边"}[stroke], func(t *testing.T) {
			testTextPatternAllowsSameFontInsideCell(t, stroke)
		})
	}
}

func testTextPatternAllowsSameFontInsideCell(t *testing.T, stroke bool) {
	t.Helper()
	doc := clipTestDocument(t)
	object := regressionTextObject()
	cellText := object
	cellText.Boundary = spec.StBox{Width: 4, Height: 4}
	cellText.Size = 3
	cellText.TextCode = []spec.TextCode{{Y: 3, Value: "x"}}
	object.FillColor = &spec.CTColor{Pattern: &spec.CtPattern{
		Width: 4, Height: 4, CellContent: spec.CellContent{CTPageBlock: spec.CTPageBlock{
			Items: []spec.PageItem{{Kind: spec.PageItemText, Text: &cellText}},
		}},
	}}
	if stroke {
		object.StrokeColor, object.Stroke, object.LineWidth = object.FillColor, true, 1
		object.FillColor = nil
		object.Fill.Set(false)
	}
	type result struct {
		surface VectorSurface
		err     error
	}
	done := make(chan result, 1)
	go func() {
		surface, err := NewVectorSurface(40, 40, func(ctx DrawContext) {
			doc.Text(ctx, object, nil, spec.StBox{Width: 40, Height: 40})
		})
		done <- result{surface, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		_, _, x1, y1 := contentImageBounds(got.surface.Rasterize(geom.DPI(254), nil))
		if x1 <= 0 || y1 <= 0 {
			t.Fatal("同字体底纹文字未绘制")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("展开底纹时仍持有字体锁，发生重复加锁")
	}
}

func TestTextPatternOutlineFollowsTextAndContainerTransforms(t *testing.T) {
	doc := clipTestDocument(t)
	cell := clipTestPath(t, `<PathObject Boundary="0 0 4 4" Fill="true" Stroke="false"><FillColor Value="255 0 0"/><AbbreviatedData>M 0 0 L 4 0 L 4 4 L 0 4 C</AbbreviatedData></PathObject>`)
	pattern := &spec.CTColor{Pattern: &spec.CtPattern{
		Width: 4, Height: 4, CellContent: spec.CellContent{CTPageBlock: spec.CTPageBlock{
			Items: []spec.PageItem{{Kind: spec.PageItemPath, Path: &cell}},
		}},
	}}
	for _, ctm := range []*spec.CTM{nil, {1, 0, .25, 1, 0, 0}} {
		object := regressionTextObject()
		object.CTM, object.HScale = ctm, .8
		object.TextCode[0].DeltaX = spec.StArray[float64]{6}
		render := func(fill *spec.CTColor) *image.RGBA {
			object.FillColor = fill
			surface := clipTestSurface(t, func(ctx DrawContext) {
				ctx.Translate(2, 3)
				ctx.Scale(.8, .8)
				var budget renderBudget
				budget.reset()
				doc.textWithBudget(ctx, object, nil, spec.StBox{Width: 40, Height: 40}, &spec.CTM{1, 0, 0, 1, 2, 1}, nil, &budget)
			})
			return surface.Rasterize(geom.DPI(254), nil)
		}
		solid, patterned := render(nil), render(pattern)
		sx0, sy0, sx1, sy1 := contentImageBounds(solid)
		px0, py0, px1, py1 := contentImageBounds(patterned)
		if px1 <= px0 || py1 <= py0 || abs(sx0-px0) > 3 || abs(sy0-py0) > 3 || abs(sx1-px1) > 3 || abs(sy1-py1) > 3 {
			t.Fatalf("底纹未跟随文字变换：solid=(%g,%g)-(%g,%g), pattern=(%g,%g)-(%g,%g)", sx0, sy0, sx1, sy1, px0, py0, px1, py1)
		}
	}
}
