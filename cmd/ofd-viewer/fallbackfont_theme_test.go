package main

import (
	"bytes"
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	textfont "github.com/go-text/typesetting/font"
	fontformat "github.com/zc310/font"
	"golang.org/x/image/font/gofont/goregular"
)

func TestFallbackUIFontAcceptsTTFAndNormalizesWOFF2(t *testing.T) {
	parsed, err := fontformat.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	woff2, err := parsed.WriteWOFF2()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{{"ttf", goregular.TTF}, {"woff2", woff2}} {
		t.Run(tc.name, func(t *testing.T) {
			resource, err := prepareFallbackUIFont(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.HasPrefix(resource.Content(), []byte("wOF2")) {
				t.Fatal("compressed WOFF2 was passed directly to Fyne")
			}
			// Use the same parser as Fyne's painter, rather than only checking
			// that the document renderer accepts this font.
			face, err := textfont.ParseTTF(bytes.NewReader(resource.Content()))
			if err != nil {
				t.Fatal(err)
			}
			if glyph, ok := face.NominalGlyph('A'); !ok || glyph == 0 {
				t.Fatal("normalized UI font lost its character map")
			}
		})
	}
	if _, err := prepareFallbackUIFont([]byte("invalid font")); err == nil {
		t.Fatal("invalid font accepted for UI rendering")
	}
}

func TestFallbackFontThemeChangesTextAndPreservesThemeProperties(t *testing.T) {
	resource, err := prepareFallbackUIFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	base := theme.DefaultTheme()
	wrapped := &fallbackFontTheme{Theme: base, font: resource}
	for _, style := range []fyne.TextStyle{{}, {Bold: true}, {Italic: true}, {Bold: true, Italic: true}} {
		if wrapped.Font(style) != resource {
			t.Fatalf("UI style %+v does not use the downloaded font", style)
		}
	}
	for _, style := range []fyne.TextStyle{{Monospace: true}, {Symbol: true}} {
		if wrapped.Font(style) != base.Font(style) {
			t.Fatalf("specialized font changed for %+v", style)
		}
	}
	if !sameBackground(wrapped.Color(theme.ColorNameBackground, theme.VariantDark), base.Color(theme.ColorNameBackground, theme.VariantDark)) ||
		wrapped.Size(theme.SizeNameText) != base.Size(theme.SizeNameText) ||
		wrapped.Icon(theme.IconNameInfo) != base.Icon(theme.IconNameInfo) {
		t.Fatal("font wrapper changed colors, sizes or icons")
	}
}

func TestFallbackFontOptionRestoresOriginalUITheme(t *testing.T) {
	registerViewerTestFont(t)
	v := newLoadedTestViewer(t)
	a := fyne.CurrentApp()
	base := a.Settings().Theme()
	resource, err := prepareFallbackUIFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	v.fallbackFont = fallbackFontState{enabled: true, ready: true, uiFont: resource}
	v.loading = true
	v.syncDefaultFallbackFont()
	if a.Settings().Theme() != base {
		t.Fatal("UI theme changed before the pending file operation finished")
	}
	v.finishLoad()
	if a.Settings().Theme() != v.fallbackFont.uiTheme || a.Settings().Theme().Font(fyne.TextStyle{}) != resource {
		t.Fatal("loaded fallback font was not applied to the UI")
	}
	v.setDefaultFallbackFont(false)
	if a.Settings().Theme() != base {
		t.Fatal("original UI theme was not restored")
	}
	v.setDefaultFallbackFont(true)
	if v.fallbackFont.downloading || v.fallbackFont.uiTheme.Theme != base {
		t.Fatal("re-enabling the cached font downloaded again or nested theme wrappers")
	}
	// If another component changes the theme, disabling this option should keep
	// that newer choice instead of replacing it with an outdated theme.
	external := theme.LightTheme()
	a.Settings().SetTheme(external)
	v.setDefaultFallbackFont(false)
	if a.Settings().Theme() != external {
		t.Fatal("disabling fallback replaced a newer theme")
	}
}

func TestDownloadedFallbackUIFontHasChineseGlyphs(t *testing.T) {
	path := os.Getenv("OFD_VIEWER_TEST_FALLBACK_FONT")
	if path == "" {
		t.Skip("set OFD_VIEWER_TEST_FALLBACK_FONT to validate the downloaded CJK font")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := prepareFallbackUIFont(data)
	if err != nil {
		t.Fatal(err)
	}
	face, err := textfont.ParseTTF(bytes.NewReader(resource.Content()))
	if err != nil {
		t.Fatal(err)
	}
	for _, character := range "中文界面字体回退菜单导出" {
		if glyph, ok := face.NominalGlyph(character); !ok || glyph == 0 {
			t.Errorf("UI font has no glyph for %q", character)
		}
	}
}
