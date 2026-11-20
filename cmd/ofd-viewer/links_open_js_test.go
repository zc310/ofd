//go:build js

package main

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
)

func TestBrowserWebLinkRemovesOpenerBeforeNavigationAndReportsPopupBlock(t *testing.T) {
	previous := js.Global().Get("open")
	defer js.Global().Set("open", previous)
	window := js.Global().Get("Object").New()
	window.Set("opener", js.Global().Get("Object").New())
	blocked := false
	open := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 2 || args[0].String() != "" || args[1].String() != "_blank" {
			t.Error("应先创建空白窗口，移除 opener 后再导航")
		}
		if blocked {
			return js.Null()
		}
		return window
	})
	defer open.Release()
	js.Global().Set("open", open)
	if err := openWebURL(t.Context(), "https://example.test"); err != nil {
		t.Fatal(err)
	}
	if !window.Get("opener").IsNull() || window.Get("location").String() != "https://example.test" {
		t.Fatal("外部网址导航前未移除 opener")
	}
	blocked = true
	if err := openWebURL(t.Context(), "https://example.test"); err == nil {
		t.Fatal("弹窗被拦截时应提示用户")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := openWebURL(ctx, "https://example.test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应保留 context.Canceled: %v", err)
	}
}
