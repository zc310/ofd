package canvas

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"os/exec"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

// 模板内部记录为 Y 向上，父级上下文为 Y 向下；页高不等及外层旋转/错切时
// 必须只在重放边界翻转一次，不能把模板移到相反一侧或镜像其图片。
func TestTemplateReplayPreservesTopLeftPlacementUnderOuterTransforms(t *testing.T) {
	family := pdfTextTestFamily(t)
	line := canvas.NewTextLine(family.Face(9, canvas.Black), "Axis", canvas.Left)
	draw := func(ctx drawing.DrawContext) {
		ctx.SetFillColor(color.RGBA{R: 255, A: 255})
		ctx.DrawPath(.7, 1.3, geom.Rectangle(6.2, 3.1))
		ctx.SetFillColor(color.RGBA{B: 128, A: 128})
		ctx.DrawPath(3.1, 13.4, geom.Circle(2.3))
		source := image.NewRGBA(image.Rect(0, 0, 2, 3))
		for y := range 3 {
			for x := range 2 {
				source.SetRGBA(x, y, color.RGBA{R: uint8(x * 100), G: uint8(40 + y*80), A: 255})
			}
		}
		ctx.DrawImage(source, 9.2, 2.1, .5)
		gradient := geom.Grad{{Offset: 0, Color: canvas.Red}, {Offset: 1, Color: canvas.Blue}}
		ctx.SetFillGradient(gradient.ToLinear(geom.Point{X: 7, Y: 10}, geom.Point{X: 14, Y: 16}))
		drawer, ok := ctx.(*canvasBackend)
		if !ok || !drawer.DrawClippedPath(geom.Rectangle(7, 6).Translate(7, 10), geom.Rectangle(5, 3).Translate(8, 11)) {
			t.Fatal("未记录模板渐变裁剪路径")
		}
		ctx.ClearFill()
		ctx.SetStrokeColor(canvas.Green)
		ctx.SetStrokeWidth(.3)
		ctx.SetDashes(.4, 1.1, .7)
		path := &geom.Path{}
		path.MoveTo(1, 6)
		path.LineTo(6, 8)
		path.LineTo(2, 11)
		ctx.DrawPath(0, 0, path)
		ctx.Push()
		ctx.Translate(5, 21)
		ctx.Scale(1, -1)
		drawer.DrawTextLine(line)
		ctx.Pop()
	}
	source := newCanvasVectorSurface(17.13, 23.27, draw)
	var scenes [3][]drawing.VectorSurface
	for _, transform := range []geom.Matrix{
		geom.Identity.Translate(10.3, 12.7),
		geom.Identity.Translate(30, 9).Rotate(30).Scale(.8, 1.2),
		geom.Identity.Translate(5, 7).Shear(.2, .1).Scale(1.1, .7),
		geom.Identity.Translate(31, 4).Scale(-1, 1),
	} {
		surfaces := make([]drawing.VectorSurface, 0, 3)
		for mode := range 3 {
			surfaces = append(surfaces, newCanvasVectorSurface(60.17, 70.19, func(ctx drawing.DrawContext) {
				ctx.Transform(transform)
				if mode != 0 {
					drawer, ok := ctx.(*canvasBackend)
					if !ok {
						t.Fatal("不是 canvas 绘制后端")
					}
					if mode == 2 {
						// 隐藏 RenderCanvas，覆盖直接展开记录的重放分支。
						drawer.ctx.Renderer = struct{ canvas.Renderer }{drawer.ctx.Renderer}
					}
					if !drawer.DrawTemplateSurface(source) {
						t.Fatal("未重放模板")
					}
				} else {
					draw(ctx)
				}
			}))
		}
		for mode, surface := range surfaces {
			scenes[mode] = append(scenes[mode], surface)
		}
		for _, dpi := range []float64{72, 96, 200, 300} {
			direct := surfaces[0].Rasterize(geom.DPI(dpi), nil)
			for mode := 1; mode < len(surfaces); mode++ {
				replay := surfaces[mode].Rasterize(geom.DPI(dpi), nil)
				if direct.Bounds() != replay.Bounds() {
					t.Fatal("模板重放改变了像素尺寸")
				}
				for i, value := range direct.Pix {
					if math.Abs(float64(value)-float64(replay.Pix[i])) > 1 {
						t.Fatalf("分支=%d，DPI=%g，变换=%v，模板重放像素字节%d发生偏移或镜像", mode, dpi, transform, i)
					}
				}
			}
		}
	}
	export := func(surfaces []drawing.VectorSurface) []byte {
		var output bytes.Buffer
		doc, err := newCanvasPDFDocument(&output, drawing.PDFOptions{Compress: true, SubsetFonts: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, surface := range surfaces {
			if err := doc.AddPage(surface, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := doc.Close(); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	var pdfs [3][]byte
	for mode, surfaces := range scenes {
		pdfs[mode] = export(surfaces)
	}
	t.Run("PDF像素", func(t *testing.T) {
		tool, err := exec.LookPath("pdftoppm")
		if err != nil {
			t.Skip("缺少 pdftoppm")
		}
		direct, err := testutil.RasterizePDFPages(t.Context(), tool, pdfs[0], 144)
		if err != nil {
			t.Fatal(err)
		}
		for mode := 1; mode < len(pdfs); mode++ {
			replay, err := testutil.RasterizePDFPages(t.Context(), tool, pdfs[mode], 144)
			if err != nil {
				t.Fatal(err)
			}
			comparison, err := testutil.ComparePageImages(direct, replay, 1)
			// Form 与直接绘制的渐变、描边存在少量 PDF 栅格差异，坐标迁移前
			// 的基线同样如此；结合前面的严格光栅对比，防止位置或轴向变化。
			if err != nil || comparison.PixelMatchRate < .995 || comparison.MeanAbsoluteError > .2 {
				t.Fatalf("分支=%d，模板重放改变 PDF 像素：%+v，%v", mode, comparison, err)
			}
		}
	})
	t.Run("PDF文字", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("缺少 pdftotext")
		}
		var expected string
		for mode, data := range pdfs {
			cmd := exec.CommandContext(t.Context(), tool, "-raw", "-", "-")
			cmd.Stdin = bytes.NewReader(data)
			text, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("pdftotext：%v：%s", err, text)
			}
			got := strings.Join(strings.Fields(string(text)), "")
			if mode == 0 {
				expected = got
				if !strings.Contains(got, "Axis") {
					t.Fatalf("参照 PDF 缺少文字：%q", got)
				}
			} else if got != expected {
				t.Fatalf("分支=%d，模板文字=%q，期望%q", mode, got, expected)
			}
		}
	})
}

// 保留文字与渐变绘制能力，隐藏模板复用接口，
// 使参考结果沿用原来的逐页模板绘制流程。
type directTemplateContext struct{ drawing.DrawContext }

func (c directTemplateContext) ShadingText() bool { return true }
func (c directTemplateContext) DrawTextLine(run drawing.TextRun) {
	c.DrawContext.(interface{ DrawTextLine(drawing.TextRun) }).DrawTextLine(run)
}
func (c directTemplateContext) DrawSemanticTextLine(run drawing.TextRun) {
	c.DrawContext.(interface{ DrawSemanticTextLine(drawing.TextRun) }).DrawSemanticTextLine(run)
}

func (c directTemplateContext) DrawClippedTextLine(run drawing.TextRun, clip *geom.Path) bool {
	drawer, ok := c.DrawContext.(interface {
		DrawClippedTextLine(run drawing.TextRun, clip *geom.Path) bool
	})
	return ok && drawer.DrawClippedTextLine(run, clip)
}

func TestTemplateFormsPreservePixelsTextLinksAndPageSizes(t *testing.T) {
	box := func(x float64, fillColor creator.Color) creator.Path {
		return creator.Path{X: x, Width: 10, Height: 10, Fill: true, FillColor: &fillColor, Data: "M 0 0 L 10 0 L 10 10 L 0 10 C"}
	}
	text := creator.Text{X: 1, Y: 11, Width: 25, Height: 5, Size: 3, Font: "Go", Value: "Template", Actions: []creator.Action{{
		Event: creator.ActionEventClick, URI: &creator.URIAction{URI: "https://example.com/template"},
	}}}
	data, err := creator.Marshal(creator.Document{
		ID: "template-forms", PageSize: creator.PageSize{Width: 30, Height: 20},
		Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}},
		Templates: []creator.TemplatePage{
			{ID: 1000, ZOrder: "Background", Items: []creator.Item{box(0, creator.Color{R: 255}), text}},
			{ID: 1001, ZOrder: "Foreground", Items: []creator.Item{box(5, creator.Color{B: 255})}},
		},
		Pages: []creator.Page{
			{Templates: []creator.TemplateRef{{ID: 1000}, {ID: 1001}}, Items: []creator.Item{box(0, creator.Color{G: 255})}},
			{Templates: []creator.TemplateRef{{ID: 1000}, {ID: 1001, ZOrder: "Background"}}, Items: []creator.Item{box(0, creator.Color{G: 255})}},
			{Area: &creator.PageArea{PhysicalBox: &creator.Box{Width: 40, Height: 25}}, Templates: []creator.TemplateRef{{ID: 1000}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	doc := render.NewDocument(color.White, ofd.Documents[0], 0)
	t.Cleanup(doc.Close)
	export := func(reuse bool) ([]byte, []*image.RGBA) {
		var output bytes.Buffer
		pdf, err := newCanvasPDFDocument(&output, drawing.PDFOptions{Compress: true, SubsetFonts: true})
		if err != nil {
			t.Fatal(err)
		}
		var rasters []*image.RGBA
		for _, page := range doc.Pages {
			var surface drawing.VectorSurface
			if reuse {
				surface, err = doc.Page(page)
			} else {
				box, boxErr := page.PhysicalBox()
				if boxErr != nil {
					t.Fatal(boxErr)
				}
				surface = newCanvasVectorSurface(box.Width, box.Height, func(ctx drawing.DrawContext) {
					if err := doc.Draw(directTemplateContext{ctx}, page); err != nil {
						t.Fatal(err)
					}
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			rasters = append(rasters, surface.Rasterize(geom.DPI(96), nil))
			if err := pdf.AddPage(surface, doc.PageExternalLinks(page)); err != nil {
				t.Fatal(err)
			}
		}
		if err := pdf.Close(); err != nil {
			t.Fatal(err)
		}
		return output.Bytes(), rasters
	}
	reference, expected := export(false)
	actual, rasters := export(true)
	for i := range expected {
		if expected[i].Bounds() != rasters[i].Bounds() || !bytes.Equal(expected[i].Pix, rasters[i].Pix) {
			t.Fatalf("page %d: cached template changed raster output", i+1)
		}
	}
	conf := model.NewStatelessConfiguration()
	ctx, err := api.ReadContext(t.Context(), bytes.NewReader(actual), conf)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.ValidateContext(t.Context(), ctx); err != nil {
		t.Fatal(err)
	}
	forms := 0
	for _, entry := range ctx.Table {
		if entry == nil {
			continue
		}
		stream, _, err := ctx.DereferenceStreamDict(entry.Object)
		if err == nil && stream != nil {
			if name := stream.NameEntry("Subtype"); name != nil && *name == "Form" {
				forms++
			}
		}
	}
	// 两个模板用于 30×20 页面，第一个模板还用于 40×25 页面，共生成三个 Form。
	if forms != 3 || ctx.PageCount != 3 {
		t.Fatalf("forms=%d pages=%d, want 3 each", forms, ctx.PageCount)
	}
	for i := 1; i <= 3; i++ {
		page, _, _, err := ctx.PageDict(t.Context(), i, false)
		if err != nil {
			t.Fatal(err)
		}
		annots := page.ArrayEntry("Annots")
		if len(annots) != 1 {
			t.Fatalf("page %d lost template link: %v", i, annots)
		}
	}
	t.Run("PDFPixels", func(t *testing.T) {
		tool, err := exec.LookPath("pdftoppm")
		if err != nil {
			t.Skip("缺少 pdftoppm")
		}
		before, err := testutil.RasterizePDFPages(t.Context(), tool, reference, 96)
		if err != nil {
			t.Fatal(err)
		}
		after, err := testutil.RasterizePDFPages(t.Context(), tool, actual, 96)
		if err != nil {
			t.Fatal(err)
		}
		comparison, err := testutil.ComparePageImages(before, after, 1)
		if err != nil || comparison.PixelMatchRate < 0.999 {
			t.Fatalf("PDF pixels differ: %+v, %v", comparison, err)
		}
	})
	t.Run("PDFText", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("缺少 pdftotext")
		}
		extract := func(data []byte) string {
			cmd := exec.CommandContext(t.Context(), tool, "-", "-")
			cmd.Stdin = bytes.NewReader(data)
			text, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("pdftotext: %v: %s", err, text)
			}
			return string(text)
		}
		if got, want := extract(actual), extract(reference); got != want || strings.Count(got, "Template") != 3 {
			t.Fatalf("template text changed: got %q, want %q", got, want)
		}
	})
}

func TestPDFFormKeepsTranslucentGradientText(t *testing.T) {
	family := pdfTextTestFamily(t)
	gradient := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 20})
	gradient.Grad = canvas.Grad{{Offset: 0, Color: color.RGBA{R: 128, A: 128}}, {Offset: 1, Color: color.RGBA{B: 128, A: 128}}}
	scene := canvas.New(40, 20)
	scene.RenderText(canvas.NewTextLine(family.Face(24, gradient), "Alpha", canvas.Left), canvas.Identity.Translate(2, 5))
	data := renderPDFText(t, 40, 20, func(r pdfTextRenderer) {
		r.RenderCanvas(scene, canvas.Identity)
	})
	if len(pdfTextImages(t, data)) == 0 {
		t.Fatal("Form bypassed transparent-gradient PDF text adapter")
	}
}

func TestPDFFormPreservesFractionalPageEdgesAndGraphicsState(t *testing.T) {
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少 pdftoppm")
	}
	const width, height = 23.0111, 30.1528
	scene := canvas.New(width, height)
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Color: color.RGBA{R: 40, G: 60, B: 50, A: 255}}
	scene.RenderPath(canvas.Rectangle(width, height), style, canvas.Identity)
	var results [][]*image.RGBA
	for _, reuse := range []bool{false, true} {
		data := renderPDFText(t, width, height, func(r pdfTextRenderer) {
			style := canvas.DefaultStyle
			style.Fill = canvas.Paint{Color: color.RGBA{R: 64, A: 64}}
			r.RenderPath(canvas.Rectangle(4, 4), style, canvas.Identity)
			if reuse {
				r.RenderCanvas(scene, canvas.Identity)
			} else {
				scene.RenderTo(r)
			}
			// 复用前面的画笔，同时检查 Q 是否恢复了页面写入器的
			// 透明度和填充缓存所对应的图形状态。
			r.RenderPath(canvas.Rectangle(4, 4), style, canvas.Identity.Translate(5, 5))
		})
		pages, err := testutil.RasterizePDFPages(t.Context(), tool, data, 144)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, pages)
	}
	if len(results[0]) != 1 || len(results[1]) != 1 || !bytes.Equal(results[0][0].Pix, results[1][0].Pix) {
		t.Fatal("Form clipping or inherited graphics state changed PDF pixels")
	}
}
