package geom

import "testing"

func TestRasterPixelsRoundsUpFractionalDimensions(t *testing.T) {
	for _, tc := range []struct {
		length float64
		res    Resolution
		want   int
	}{
		{210, DPI(200), 1654},
		{140, DPI(200), 1103},
		{10, DPMM(1), 10},
		{10.1, DPMM(1), 11},
	} {
		if got := RasterPixels(tc.length, tc.res); got != tc.want {
			t.Errorf("RasterPixels(%g, %v) = %d，期望 %d", tc.length, tc.res, got, tc.want)
		}
	}
}
