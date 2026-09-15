package render

import (
	"image"
	"testing"

	"github.com/tdewolff/canvas"
)

func TestShouldUseFastImageTransform(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 100, 100))
	cases := []struct {
		name string
		m    canvas.Matrix
		want bool
	}{
		{name: "large downscale", m: canvas.Identity.Scale(0.5, 0.5), want: true},
		{name: "slight downscale", m: canvas.Identity.Scale(0.9, 0.9), want: false},
		{name: "upscale", m: canvas.Identity.Scale(2, 2), want: false},
		{name: "mixed scale", m: canvas.Matrix{{0.5, 0, 0}, {0, 1, 0}}, want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := shouldUseFastImageTransform(source, testCase.m, canvas.DPMM(1)); got != testCase.want {
				t.Fatalf("shouldUseFastImageTransform() = %v, want %v", got, testCase.want)
			}
		})
	}
}
