package render

import (
	"bytes"
	"encoding/xml"
	"image"
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

func clipTestPath(t *testing.T, data string) models.PathObject {
	t.Helper()
	var object models.PathObject
	if err := xml.Unmarshal([]byte(data), &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func clipTestSurface(t *testing.T, draw func(DrawContext)) VectorSurface {
	t.Helper()
	surface, err := NewVectorSurface(40, 40, draw)
	if err != nil {
		t.Fatal(err)
	}
	return surface
}

func clipTestDocument(t *testing.T) *Document {
	t.Helper()
	if err := RegisterFallbackFont(goregular.TTF, t.Name(), FontRegular); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UnregisterFallbackFont(t.Name()) })
	doc := NewDocument(nil, nil)
	t.Cleanup(doc.Close)
	if err := doc.UseFallbackFont(t.Name()); err != nil {
		t.Fatal(err)
	}
	return doc
}

func rectangleClip(width, height float64) *models.Clips {
	return &models.Clips{Clip: []models.CtClip{{Area: []models.ClipArea{{Path: &models.CtPath{
		AbbreviatedData: models.SVGPath{
			{Type: models.MoveTo, Points: []models.StPos{{}}},
			{Type: models.LineTo, Points: []models.StPos{{X: width}}},
			{Type: models.LineTo, Points: []models.StPos{{X: width, Y: height}}},
			{Type: models.LineTo, Points: []models.StPos{{Y: height}}},
			{Type: models.Close},
		},
	}}}}}}
}

func TestTextObjectAndParentClipHideDisjointGlyphs(t *testing.T) {
	doc := clipTestDocument(t)
	for _, kind := range []string{"object", "parent", "text area"} {
		t.Run(kind, func(t *testing.T) {
			object := models.TextObject{CtText: models.CtText{
				CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 5, Y: 5, Width: 30, Height: 12}},
				Size:          10, TextCode: []models.TextCode{{Y: 10, Value: "HELLO"}},
			}}
			var parent *geom.Path
			switch kind {
			case "object":
				object.Clips = rectangleClip(1, 1)
			case "parent":
				parent = geom.Rectangle(1, 1)
			case "text area":
				// 使用同一字体构建裁剪，不能与文字绘制锁自锁。
				object.Clips = &models.Clips{Clip: []models.CtClip{{Area: []models.ClipArea{{Text: &models.CtText{
					Size: 1, TextCode: []models.TextCode{{Y: 1, Value: "H"}},
				}}}}}}
			}
			surface := clipTestSurface(t, func(ctx DrawContext) {
				var budget renderBudget
				budget.reset()
				doc.textWithBudget(ctx, object, nil, models.StBox{Width: 40, Height: 40}, nil, parent, &budget)
			})
			img := surface.Rasterize(geom.DPMM(10), nil)
			for i := 3; i < len(img.Pix); i += 4 {
				if img.Pix[i] != 0 {
					t.Fatal("不相交裁剪区之外仍有文字像素")
				}
			}
		})
	}
}

func TestImageClipUsesSameTextRegionAsPathClip(t *testing.T) {
	doc := clipTestDocument(t)
	text := &models.CtText{Size: 10, TextCode: []models.TextCode{{Y: 10, Value: "H"}}}
	clips := &models.Clips{Clip: []models.CtClip{{Area: []models.ClipArea{{Text: text}}}}}
	want := doc.buildPathClip(clips, models.StBox{}, 40, nil, nil)
	got := doc.buildImageClip(clips, 40, 0, 0, models.IdentityMatrix)
	if want == nil || want.Empty() || got == nil || !want.Equals(got) {
		t.Fatal("图片和路径没有使用相同的文字裁剪轮廓")
	}
}

func TestPathBoundaryClipsOverflowInRasterAndPDF(t *testing.T) {
	object := clipTestPath(t, `<PathObject Boundary="5 5 5 5" Fill="true" Stroke="false"><FillColor Value="0 0 0"/><AbbreviatedData>M 0 0 L 20 0 L 20 20 L 0 20 C</AbbreviatedData></PathObject>`)
	surface := clipTestSurface(t, func(ctx DrawContext) {
		(&Document{}).Path(ctx, object, nil, models.StBox{Width: 40, Height: 40})
	})
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		inside := color.NRGBAModel.Convert(img.At(70, 70)).(color.NRGBA)
		outside := color.NRGBAModel.Convert(img.At(150, 150)).(color.NRGBA)
		if inside.A == 0 || inside.R > 10 || (outside.A != 0 && outside.R < 245) {
			t.Fatalf("Boundary内外像素错误：inside=%v outside=%v", inside, outside)
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10), nil))
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func clipPDFBytes(t *testing.T, surface VectorSurface) []byte {
	t.Helper()
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
	return output.Bytes()
}

func clipPDFImage(t *testing.T, surface VectorSurface, dpi int) image.Image {
	t.Helper()
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少pdftoppm，跳过PDF像素检查")
	}
	pages, err := testutil.RasterizePDFPages(t.Context(), tool, clipPDFBytes(t, surface), dpi)
	if err != nil {
		t.Fatal(err)
	}
	return pages[0]
}

func TestPartialTextClipKeepsVisiblePixelsAndPDFText(t *testing.T) {
	doc := clipTestDocument(t)
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 5, Y: 5, Width: 30, Height: 12}, Clips: rectangleClip(4, 12)},
		Size:          10, TextCode: []models.TextCode{{Y: 10, Value: "HELLO"}},
	}}
	surface := clipTestSurface(t, func(ctx DrawContext) {
		doc.Text(ctx, object, nil, models.StBox{Width: 40, Height: 40})
	})
	check := func(t *testing.T, img image.Image) {
		t.Helper()
		ink := 0
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if c.A > 100 && c.R < 100 {
					ink++
					if x > 90 {
						t.Fatalf("裁剪右侧仍有文字像素：(%d,%d)", x, y)
					}
				}
			}
		}
		if ink < 100 {
			t.Fatalf("裁剪区内文字丢失：%d像素", ink)
		}
	}
	check(t, surface.Rasterize(geom.DPMM(10), nil))
	t.Run("PDF pixels", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
	t.Run("PDF text", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("缺少pdftotext")
		}
		path := filepath.Join(t.TempDir(), "clipped.pdf")
		if err := os.WriteFile(path, clipPDFBytes(t, surface), 0o600); err != nil {
			t.Fatal(err)
		}
		text, err := exec.CommandContext(t.Context(), tool, path, "-").CombinedOutput()
		if err != nil || !strings.Contains(string(text), "HELLO") {
			t.Fatalf("裁剪文字不能提取：%v %s", err, text)
		}
	})
}

func TestSimpleCompositeHonorsOwnClipWithoutRasterizing(t *testing.T) {
	doc := &Document{}
	path := clipTestPath(t, `<PathObject Boundary="0 0 20 20" Fill="true" Stroke="false"><FillColor Value="0 0 0"/><AbbreviatedData>M 0 0 L 20 0 L 20 20 L 0 20 C</AbbreviatedData></PathObject>`)
	unit := &models.CompositeGraphicUnit{Width: 20, Height: 20, Content: models.CTPageBlock{Items: []models.PageItem{{Kind: models.PageItemPath, Path: &path}}}}
	object := models.CompositeObject{CtComposite: models.CtComposite{CTGraphicUnit: models.CTGraphicUnit{
		Boundary: models.StBox{Width: 20, Height: 20}, Clips: rectangleClip(5, 20),
	}}}
	surface := clipTestSurface(t, func(ctx DrawContext) {
		if !doc.renderSimpleCompositeVector(ctx, object, unit, nil, models.StBox{Width: 40, Height: 40}, nil, nil) {
			t.Fatal("单纯填充路径应保留矢量")
		}
	})
	img := surface.Rasterize(geom.DPMM(10), nil)
	if img.RGBAAt(20, 100).A != 255 || img.RGBAAt(100, 100).A != 0 {
		t.Fatal("复合对象自身的裁剪未生效")
	}
}
