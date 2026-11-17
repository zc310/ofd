package models

import (
	"math"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestEnsurePhysicalBoxKeepsOptionalRegionsWhileFallingBackToA4(t *testing.T) {
	EnsurePhysicalBox(nil)
	for _, box := range []spec.StBox{{Width: math.NaN(), Height: 297}, {Width: 210, Height: math.Inf(1)}, {X: 9, Y: 8, Width: -1, Height: 297}} {
		content := spec.StBox{X: 10, Y: 20, Width: 100, Height: 200}
		area := spec.PageArea{PhysicalBox: box, ContentBox: &content}
		EnsurePhysicalBox(&area)
		if area.PhysicalBox != (spec.StBox{Width: 210, Height: 297}) || area.ContentBox != &content || area.ApplicationBox != nil || area.BleedBox != nil {
			t.Fatalf("A4 兜底改变了其他区域声明: %+v", area)
		}
	}
}
