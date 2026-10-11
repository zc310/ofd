package pdf2ofd

import (
	"bytes"
	"fmt"
	"image/color"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestConvertImagePreservesAffineCorners(t *testing.T) {
	// 预期坐标为页面左上角起算的点单位，依次对应源图左上、右上、左下、右下。
	for _, tc := range []struct {
		name      string
		matrix    string
		pageAttrs string
		corners   [4][2]float64
	}{
		{"normal", "60 0 0 40 20 30", "", [4][2]float64{{20, 130}, {80, 130}, {20, 170}, {80, 170}}},
		{"rotate 90", "0 60 -40 0 80 30", "", [4][2]float64{{40, 170}, {40, 110}, {80, 170}, {80, 110}}},
		{"rotate 45", "40 40 -20 20 60 30", "", [4][2]float64{{40, 150}, {80, 110}, {60, 170}, {100, 130}}},
		{"shear", "60 10 20 40 20 30", "", [4][2]float64{{40, 130}, {100, 120}, {20, 170}, {80, 160}}},
		{"mirror both axes", "-60 0 0 -40 80 70", "", [4][2]float64{{80, 170}, {20, 170}, {80, 130}, {20, 130}}},
		{"page rotate 90", "60 0 0 40 20 30", "/Rotate 90", [4][2]float64{{70, 20}, {70, 80}, {30, 20}, {30, 80}}},
		{"page rotate 180", "60 0 0 40 20 30", "/Rotate 180", [4][2]float64{{180, 70}, {120, 70}, {180, 30}, {120, 30}}},
		{"page rotate 270", "60 0 0 40 20 30", "/Rotate 270", [4][2]float64{{130, 180}, {130, 120}, {170, 180}, {170, 120}}},
		{"user unit", "60 0 0 40 20 30", "/UserUnit 2", [4][2]float64{{40, 260}, {160, 260}, {40, 340}, {160, 340}}},
		{"crop origin", "60 0 0 40 20 30", "/CropBox [10 20 190 180]", [4][2]float64{{10, 110}, {70, 110}, {10, 150}, {70, 150}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "q " + tc.matrix + " cm /Im Do Q"
			objects := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] " + tc.pageAttrs + " /Resources << /XObject << /Im 5 0 R >> >> /Contents 4 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
				"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 12 >>\nstream\n\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\x00\nendstream",
			}
			var output bytes.Buffer
			if err := Convert(t.Context(), assemblePDF(objects), &output, ""); err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(output.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ofd.Close() }()
			page := ofd.Documents[0].Pages[0]
			if err := page.EnsureLoaded(); err != nil {
				t.Fatal(err)
			}
			images := layerImages(page.Content().Layer[0])
			if len(images) != 1 || images[0].CTM == nil {
				t.Fatalf("expected one image with explicit CTM, got %+v", images)
			}
			object := images[0]
			for i, point := range [4][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				x, y := object.CTM.Transform(point[0], point[1])
				x, y = x+object.Boundary.X, y+object.Boundary.Y
				wantX, wantY := tc.corners[i][0]*pdfPointToMillimeter, tc.corners[i][1]*pdfPointToMillimeter
				if math.Abs(x-wantX) > 0.0002 || math.Abs(y-wantY) > 0.0002 {
					t.Fatalf("corner %d = (%g,%g), want (%g,%g); CTM=%v", i, x, y, wantX, wantY, *object.CTM)
				}
				if x < object.Boundary.X-0.0002 || y < object.Boundary.Y-0.0002 || x > object.Boundary.X+object.Boundary.Width+0.0002 || y > object.Boundary.Y+object.Boundary.Height+0.0002 {
					t.Fatalf("corner %d lies outside image Boundary", i)
				}
			}
			// 检查四个色块的实际像素朝向，避免只验证矩阵却在渲染时上下翻转。
			resolution := geom.DPMM(4)
			img, err := render.NewDocument(color.White, ofd.Documents[0]).RasterizePage(page, render.BackendCanvas, resolution)
			if err != nil {
				t.Fatal(err)
			}
			colors := [4]color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, G: 255, A: 255}}
			for i, uv := range [4][2]float64{{0.25, 0.25}, {0.75, 0.25}, {0.25, 0.75}, {0.75, 0.75}} {
				x := tc.corners[0][0] + uv[0]*(tc.corners[1][0]-tc.corners[0][0]) + uv[1]*(tc.corners[2][0]-tc.corners[0][0])
				y := tc.corners[0][1] + uv[0]*(tc.corners[1][1]-tc.corners[0][1]) + uv[1]*(tc.corners[2][1]-tc.corners[0][1])
				got := img.RGBAAt(int(x*pdfPointToMillimeter*float64(resolution)), int(y*pdfPointToMillimeter*float64(resolution)))
				want := colors[i]
				if math.Abs(float64(got.R)-float64(want.R)) > 40 || math.Abs(float64(got.G)-float64(want.G)) > 40 || math.Abs(float64(got.B)-float64(want.B)) > 40 || got.A != 255 {
					t.Fatalf("color block %d = %v, want %v", i, got, want)
				}
			}
		})
	}
}
