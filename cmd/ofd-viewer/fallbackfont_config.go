package main

import "context"

const (
	defaultFallbackFontFamily = "Noto Sans SC"
	fallbackFontPreferenceKey = "web-default-fallback-font"
	maxFallbackFontBytes      = 32 << 20
)

var defaultFallbackFontURLs = []string{
	"https://cdn.jsdelivr.net/npm/@reogrid/font-sc@2.0.0/NotoSansSC-Regular.ttf",
	"https://cdn.jsdelivr.net/npm/@fontsource/noto-sans-sc/files/noto-sans-sc-chinese-simplified-400-normal.woff2",
}

type fallbackFontCache interface {
	Load(context.Context) ([]byte, error)
	Store(context.Context, []byte) error
}
