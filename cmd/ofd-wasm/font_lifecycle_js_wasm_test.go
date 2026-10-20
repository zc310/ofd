//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/zc310/ofd/pkg/webreader"
	"golang.org/x/image/font/gofont/goregular"
)

func TestRemoveFallbackFontReleasesGlobalSourceAndAppBuffer(t *testing.T) {
	source := webreader.FontSource{Family: t.Name(), Data: goregular.TTF}
	if err := webreader.RegisterFallbackFont(source); err != nil {
		t.Fatal(err)
	}
	defer webreader.UnregisterFallbackFont(source.Family)
	app := &wasmApp{fallbackFonts: []webreader.FontSource{source}}
	backing := app.fallbackFonts
	if result := app.removeFallbackFont(js.Undefined(), []js.Value{js.ValueOf(source.Family)}); result != nil {
		t.Fatalf("移除回退字体失败：%v", result)
	}
	if _, ok := webreader.FallbackFontData(source.Family); ok {
		t.Fatal("移除后仍保留全局来源，注册额度未释放")
	}
	if len(app.fallbackFonts) != 0 || backing[0].Data != nil {
		t.Fatal("WASM app 的切片仍保留被移除的字体字节")
	}
	if result := app.removeFallbackFont(js.Undefined(), []js.Value{js.ValueOf(source.Family)}); result != nil {
		t.Fatal("重复移除应保持幂等")
	}
}
