package main

import (
	"bytes"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	textfont "github.com/go-text/typesetting/font"
	fontformat "github.com/zc310/font"
)

// Fyne's text painter parses SFNT resources. Normalize the WOFF2 alternate source
// in the background; IndexedDB keeps the original downloaded data for reuse.
func prepareFallbackUIFont(data []byte) (fyne.Resource, error) {
	sfnt, err := fontformat.ToSFNT(data)
	if err != nil {
		return nil, err
	}
	if _, err := textfont.ParseTTF(bytes.NewReader(sfnt)); err != nil {
		return nil, err
	}
	return fyne.NewStaticResource("ofd-viewer-NotoSansSC-Regular.ttf", sfnt), nil
}

type fallbackFontTheme struct {
	fyne.Theme
	font fyne.Resource
}

func (t *fallbackFontTheme) Font(style fyne.TextStyle) fyne.Resource {
	// The downloaded regular font covers proportional UI text. Keep specialized
	// monospace and symbol resources supplied by the underlying theme.
	if style.Monospace || style.Symbol {
		return t.Theme.Font(style)
	}
	return t.font
}

func (v *viewer) syncFallbackFontTheme(enabled bool) {
	a := fyne.CurrentApp()
	if a == nil {
		return
	}
	current := a.Settings().Theme()
	if !enabled {
		if previous := v.fallbackFont.uiTheme; previous != nil {
			if current == previous {
				a.Settings().SetTheme(previous.Theme)
			}
			v.fallbackFont.uiTheme = nil
		}
		return
	}
	if v.fallbackFont.uiFont == nil || current == v.fallbackFont.uiTheme {
		return
	}
	if current == nil {
		current = theme.DefaultTheme()
	}
	v.fallbackFont.uiTheme = &fallbackFontTheme{Theme: current, font: v.fallbackFont.uiFont}
	a.Settings().SetTheme(v.fallbackFont.uiTheme)
}
