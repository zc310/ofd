//go:build js

package main

import (
	"context"
	"syscall/js"
)

func openWebURL(ctx context.Context, uri string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	window := js.Global().Call("open", "", "_blank")
	if window.IsNull() || window.IsUndefined() {
		return viewerError("浏览器阻止了新窗口，请允许此站点打开弹出窗口")
	}
	// 先在同源空白窗口移除 opener，再导航，不向外部站点暴露阅读器窗口。
	window.Set("opener", js.Null())
	window.Set("location", uri)
	return nil
}
