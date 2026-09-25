package render

import (
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

func TestResolveColorSpaceColor(t *testing.T) {
	alpha := uint8(128)
	cases := []struct {
		name  string
		space models.ColorSpace
		color models.CTColor
		want  color.RGBA
	}{
		{
			name:  "gray",
			space: models.ColorSpace{Type: "GRAY"},
			color: models.CTColor{ColorSpace: 1, Value: &models.Color{RGBA: color.RGBA{R: 128, A: 255}}},
			want:  color.RGBA{R: 128, G: 128, B: 128, A: 255},
		},
		{
			name:  "rgb",
			space: models.ColorSpace{Type: "RGB"},
			color: models.CTColor{ColorSpace: 1, Value: &models.Color{RGBA: color.RGBA{R: 10, G: 20, B: 30, A: 255}}},
			want:  color.RGBA{R: 10, G: 20, B: 30, A: 255},
		},
		{
			name:  "cmyk white",
			space: models.ColorSpace{Type: "CMYK"},
			color: models.CTColor{ColorSpace: 1, Value: &models.Color{RGBA: color.RGBA{0, 0, 0, 0}}},
			want:  color.RGBA{R: 255, G: 255, B: 255, A: 255},
		},
		{
			// Adobe/poppler 矩阵模型下纯黑油墨并非完全吸光，结果为接近黑的 (35,31,32)。
			name:  "cmyk black",
			space: models.ColorSpace{Type: "CMYK"},
			color: models.CTColor{ColorSpace: 1, Value: &models.Color{RGBA: color.RGBA{0, 0, 0, 255}}},
			want:  color.RGBA{R: 35, G: 31, B: 32, A: 255},
		},
		{
			name: "palette index",
			space: models.ColorSpace{Type: "RGB", Palette: &models.Palette{CV: []models.StArray{
				{"255", "0", "0"},
				{"0", "255", "0"},
			}}},
			color: models.CTColor{ColorSpace: 1, Index: 1},
			want:  color.RGBA{R: 0, G: 255, B: 0, A: 255},
		},
		{
			name:  "alpha attribute",
			space: models.ColorSpace{Type: "RGB"},
			color: models.CTColor{ColorSpace: 1, Value: &models.Color{RGBA: color.RGBA{R: 1, G: 2, B: 3, A: 255}}, Alpha: &alpha},
			// color.RGBA 为预乘格式，需按 alpha 预乘 RGB。
			want: premultiplied(1, 2, 3, 128),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveColorSpaceColor(&tc.space, tc.color); got != tc.want {
				t.Fatalf("resolveColorSpaceColor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGradientStopsUseColorSpaceColors(t *testing.T) {
	// 渐变分段颜色引用 CMYK 颜色空间时也应按油墨合成转换，而不是当作 RGBA。
	space := &models.ColorSpace{Type: "CMYK"}
	shd := &models.CTAxialShd{
		StartPoint: models.StPos{X: 0, Y: 0},
		EndPoint:   models.StPos{X: 10, Y: 0},
		Segment: []models.Segment{
			{Position: 0, PositionSet: true, Color: models.CTColor{ColorSpace: 1, Value: &models.Color{RGBA: color.RGBA{0, 0, 0, 0}}}},
			{Position: 1, PositionSet: true, Color: models.CTColor{ColorSpace: 1, Value: &models.Color{RGBA: color.RGBA{0, 0, 0, 255}}}},
		},
	}
	resolve := func(source models.CTColor) color.RGBA {
		return resolveColorSpaceColor(space, source)
	}
	var gradient geom.Grad
	addOFDGradientStops(&gradient, shd.Segment, resolve)
	if len(gradient) != 2 {
		t.Fatalf("stops = %d, want 2", len(gradient))
	}
	if gradient[0].Color != (color.RGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("first stop = %v, want white", gradient[0].Color)
	}
	if gradient[1].Color != (color.RGBA{R: 35, G: 31, B: 32, A: 255}) {
		t.Fatalf("second stop = %v, want near black (35,31,32)", gradient[1].Color)
	}
}
