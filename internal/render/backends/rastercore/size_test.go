package rastercore

import (
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
)

func TestNewRoundsPageDimensionsUp(t *testing.T) {
	var width, height int
	_, err := New(210, 140, geom.DPI(200), func(w, h int, _, _ float64) Hooks {
		width, height = w, h
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if width != 1654 || height != 1103 {
		t.Fatalf("栅格尺寸 %d×%d，期望 1654×1103", width, height)
	}
}
