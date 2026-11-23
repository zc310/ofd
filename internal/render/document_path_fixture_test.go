package render

import (
	"bytes"
	"errors"
	"image/color"
	"os"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/paint"
	"github.com/zc310/ofd/internal/render/pathbuild"
	"github.com/zc310/ofd/pkg/spec"
)

func TestIntroPage4WatermarkKeepsNativeStrokeAfterBoundaryRounding(t *testing.T) {
	assertIntroPathKeepsNativeStroke(t, 4, 125)
}

func TestIntroPage12DemoKeepsNativeStrokeAfterBoundaryRounding(t *testing.T) {
	assertIntroPathKeepsNativeStroke(t, 12, 2125)
}

func assertIntroPathKeepsNativeStroke(t *testing.T, pageNumber int, pathID spec.StID) {
	t.Helper()
	const input = "../../testdata/ofdrw/intro.ofd"
	ofd, err := parser.NewOFD(input)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[pageNumber-1]
	var object spec.PathObject
	if err := page.WithPageContent(func(content *models.PageContent) error {
		for _, layer := range content.Content.Layer {
			for _, item := range layer.Items {
				if item.Path != nil && item.Path.ID == pathID {
					object = *item.Path
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if object.ID != pathID {
		t.Fatalf("样例第%d页缺少路径%d", pageNumber, pathID)
	}
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	doc := NewDocument(color.Transparent, ofd.Documents[0], 0)
	defer doc.Close()
	want := pathbuild.Object(object, nil)
	before := want.ToSVG()
	actual, err := NewVectorSurface(box.Width, box.Height, func(ctx DrawContext) {
		capture := &clippedPathCaptureContext{DrawContext: ctx}
		doc.Path(capture, object, nil, box)
		if len(capture.paths) != 1 || !capture.paths[0].Equals(want) {
			t.Fatalf("完全覆盖路径%d的Boundary因舍入误差触发描边展开", pathID)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// 独立参照直接提交源曲线及描边样式，不经过裁剪判断。
	reference, err := NewVectorSurface(box.Width, box.Height, func(ctx DrawContext) {
		doc.updateCtPathStyle(ctx, &object.CtPath, nil, paint.StrokeScale(object.CTM, nil))
		ctx.DrawPath(0, 0, want)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("光栅", func(t *testing.T) {
		first := actual.Rasterize(geom.DPI(144), nil)
		second := reference.Rasterize(geom.DPI(144), nil)
		if first.Bounds() != second.Bounds() || !bytes.Equal(first.Pix, second.Pix) {
			t.Fatal("像素与源曲线直接描边不一致")
		}
	})
	t.Run("PDF", func(t *testing.T) {
		data := clipPDFBytes(t, actual)
		if len(data) > 20_000 {
			t.Fatalf("路径PDF膨胀：%d字节，期望保留原生描边而非展开轮廓", len(data))
		}
		first, second := clipPDFImage(t, actual, 144), clipPDFImage(t, reference, 144)
		if first.Bounds() != second.Bounds() {
			t.Fatal("PDF页面尺寸发生变化")
		}
		for y := first.Bounds().Min.Y; y < first.Bounds().Max.Y; y++ {
			for x := first.Bounds().Min.X; x < first.Bounds().Max.X; x++ {
				if first.At(x, y) != second.At(x, y) {
					t.Fatalf("路径PDF像素(%d,%d)与源曲线描边不一致", x, y)
				}
			}
		}
	})
	if want.ToSVG() != before {
		t.Fatal("绘制改写了共享路径")
	}
}
