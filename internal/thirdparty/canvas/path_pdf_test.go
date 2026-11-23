package canvas

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/tdewolff/minify/v2"
)

func TestDecimalPreservesLegacyPrecisionAndRounding(t *testing.T) {
	precision := Precision
	t.Cleanup(func() { Precision = precision })
	for _, p := range []int{0, 1, 8, 16} {
		Precision = p
		for _, value := range []float64{
			0, math.Copysign(0, -1), .5, -.5, 1.23456789, 99999999.99999999,
			math.MaxInt32 + 1, math.MinInt32 - 1, math.MaxFloat64,
			math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1), math.NaN(),
		} {
			want := fmt.Sprintf("%.*f", p, value)
			want = string(minify.Decimal([]byte(want), p))
			if value > math.MaxInt32 || value < math.MinInt32 {
				if !strings.Contains(want, ".") {
					want += ".0"
				}
			}
			if got := dec(value).String(); got != want {
				t.Fatalf("precision=%d value=%v: got %q, want %q", p, value, got, want)
			}
		}
	}
}

func TestPathToPDFPreservesOperatorsCoordinatesAndSource(t *testing.T) {
	path := &Path{}
	path.MoveTo(.5, -.5)
	path.LineTo(1, 2)
	path.QuadTo(4, 8, 7, 8)
	path.CubeTo(1.23456789, -1.23456789, 9, 10, 11, 12)
	path.Close()
	original := path.Copy()
	want := ".5 -.5 m 1 2 l 3 6 5 8 7 8 c 1.2345679 -1.2345679 9 10 11 12 c h"
	if got := path.ToPDF(); got != want {
		t.Fatalf("路径输出=%q，预期 %q", got, want)
	}
	if !path.Equals(original) {
		t.Fatal("序列化修改了调用方路径")
	}
}
