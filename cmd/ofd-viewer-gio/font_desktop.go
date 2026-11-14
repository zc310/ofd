//go:build !js

package main

import (
	"context"

	"gioui.org/font/gofont"
	"gioui.org/text"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/render"
)

func configureViewerFont(th *material.Theme) error {
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	return nil
}

func applyViewerFallback(_ *render.Document) error { return nil }

func startViewerFont(_ context.Context, _ func()) (<-chan viewerFontResult, <-chan struct{}) {
	return nil, nil
}
