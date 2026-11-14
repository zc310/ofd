package main

import (
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/text"
	"gioui.org/widget/material"
)

type viewerFontResult struct {
	faces []font.FontFace
	err   error
}

func applyViewerFont(th *material.Theme, faces []font.FontFace) {
	th.Shaper = text.NewShaper(text.WithCollection(append(gofont.Collection(), faces...)))
}
