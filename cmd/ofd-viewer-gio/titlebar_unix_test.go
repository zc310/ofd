//go:build (linux && !android) || freebsd || openbsd

package main

import (
	"testing"
	"unsafe"

	"gioui.org/app"
)

func TestDetectsValidWaylandViewWithoutChangingX11(t *testing.T) {
	var marker byte
	if !isWaylandView(app.WaylandViewEvent{Display: unsafe.Pointer(&marker)}) {
		t.Fatal("未识别有效的 Wayland 视图")
	}
	if isWaylandView(app.WaylandViewEvent{}) || isWaylandView(app.X11ViewEvent{Display: unsafe.Pointer(&marker)}) {
		t.Fatal("无效 Wayland 视图或 X11 视图不应启用 Wayland 标题栏")
	}
}
