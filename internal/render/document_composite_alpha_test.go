package render

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"golang.org/x/image/font/gofont/goregular"
)

func TestCompositeAlphaAppliesOnceToOverlappingChildren(t *testing.T) {
	doc, _, object := budgetComposite(t)
	if err := RegisterFallbackFont(goregular.TTF, t.Name(), FontRegular); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UnregisterFallbackFont(t.Name()) })
	if err := doc.UseFallbackFont(t.Name()); err != nil {
		t.Fatal(err)
	}
	path := clipTestPath(t, `<PathObject Boundary="0 0 10 20" Fill="true" Stroke="false"><FillColor Value="0 0 0"/><AbbreviatedData>M 0 0 L 10 0 L 10 20 L 0 20 C</AbbreviatedData></PathObject>`)
	overlap := path
	overlap.Boundary.X = 5
	unit := doc.GetCompositeUnit(5)
	unit.Width, unit.Height = 60, 60
	unit.Content.Items = []models.PageItem{
		{Kind: models.PageItemPath, Path: &path},
		{Kind: models.PageItemPath, Path: &overlap},
		{Kind: models.PageItemText, Text: &models.TextObject{CtText: models.CtText{
			CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 1, Y: 13, Width: 18, Height: 6}},
			Size:          3, TextCode: []models.TextCode{{Y: 4, Value: "GROUP"}},
		}}},
	}
	alpha := uint8(128)
	object.Boundary, object.CTM, object.Alpha = models.StBox{Width: 20, Height: 20}, nil, &alpha
	surface := clipTestSurface(t, func(ctx DrawContext) {
		doc.Composite(ctx, object, nil, models.StBox{Width: 40, Height: 40})
	})
	img := surface.Rasterize(geom.DPMM(10))
	for _, x := range []int{20, 70, 120} {
		if a := img.RGBAAt(x, 100).A; a != 128 {
			t.Errorf("x=%d处组Alpha=%d，期望128", x, a)
		}
	}
	t.Run("PDF pixels", func(t *testing.T) {
		var img image.Image = clipPDFImage(t, surface, 254)
		for _, x := range []int{20, 70, 120} {
			c := color.NRGBAModel.Convert(img.At(x, 100)).(color.NRGBA)
			if c.R < 125 || c.R > 129 {
				t.Errorf("x=%d处PDF白底灰度=%d，期望127", x, c.R)
			}
		}
	})
	t.Run("PDF text", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("缺少pdftotext")
		}
		path := filepath.Join(t.TempDir(), "group.pdf")
		if err := os.WriteFile(path, clipPDFBytes(t, surface), 0o600); err != nil {
			t.Fatal(err)
		}
		text, err := exec.CommandContext(t.Context(), tool, path, "-").CombinedOutput()
		if err != nil || !strings.Contains(string(text), "GROUP") {
			t.Fatalf("半透明组文字不可提取：%v %s", err, text)
		}
	})
}
