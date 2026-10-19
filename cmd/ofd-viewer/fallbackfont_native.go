//go:build !js || !wasm

package main

const webFallbackFontSupported = false

func newFallbackFontCache() fallbackFontCache { return nil }
