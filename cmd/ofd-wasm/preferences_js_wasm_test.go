//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
)

func TestWASMPreferencesPreservesTitleAndUIFlags(t *testing.T) {
	hide := true
	data, err := creator.Marshal(creator.Document{ID: "preferences", Title: "元数据标题", Pages: []creator.Page{{}},
		Preferences: &creator.ViewPreferences{TabDisplay: "DocTitle", HideToolbar: &hide, HideMenubar: &hide, HideWindowUI: &hide}})
	if err != nil {
		t.Fatal(err)
	}
	app := &wasmApp{}
	t.Cleanup(func() {
		if err := app.closeCurrentReader(); err != nil {
			t.Error(err)
		}
	})
	input := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(input, data)
	opened, ok := app.open(js.Undefined(), []js.Value{input}).(js.Value)
	if !ok {
		t.Fatal("打开接口没有返回 JS 对象")
	}
	if opened.Get("error").Type() == js.TypeString {
		t.Fatal(opened.Get("error").String())
	}
	got, ok := app.preferences(js.Undefined(), nil).(js.Value)
	if !ok {
		t.Fatal("偏好接口没有返回 JS 对象")
	}
	if got.Get("tab_display").String() != "DocTitle" || got.Get("document_title").String() != "元数据标题" ||
		!got.Get("hide_toolbar").Bool() || !got.Get("hide_menubar").Bool() || !got.Get("hide_window_ui").Bool() {
		t.Fatalf("偏好未完整传给浏览器：%v", got)
	}
}
