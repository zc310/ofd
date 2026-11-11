package models

import (
	"math"
	"testing"
)

func TestEnsurePhysicalBoxKeepsOptionalRegionsWhileFallingBackToA4(t *testing.T) {
	EnsurePhysicalBox(nil)
	for _, box := range []StBox{{Width: math.NaN(), Height: 297}, {Width: 210, Height: math.Inf(1)}, {X: 9, Y: 8, Width: -1, Height: 297}} {
		content := StBox{X: 10, Y: 20, Width: 100, Height: 200}
		area := CtPageArea{PhysicalBox: box, ContentBox: &content}
		EnsurePhysicalBox(&area)
		if area.PhysicalBox != (StBox{Width: 210, Height: 297}) || area.ContentBox != &content || area.ApplicationBox != nil || area.BleedBox != nil {
			t.Fatalf("A4 兜底改变了其他区域声明: %+v", area)
		}
	}
}
