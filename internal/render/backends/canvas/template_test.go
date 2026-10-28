package canvas

import (
	"bytes"
	"image"
	"image/color"
	"os/exec"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

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
	doc := render.NewDocument(color.White, ofd.Documents[0])
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
			rasters = append(rasters, surface.Rasterize(geom.DPI(96)))
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
	conf := model.NewDefaultConfiguration()
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
