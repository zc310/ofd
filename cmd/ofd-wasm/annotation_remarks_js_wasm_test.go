//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
)

func TestWASMAnnotationsExposeRemarkFlagsAndPageSelection(t *testing.T) {
	disabled := false
	data, err := creator.Marshal(creator.Document{ID: "remarks", Pages: []creator.Page{{}, {}}, Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{
		{ID: 10, Type: "Stamp", Creator: "test", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Remark: "缺省备注"},
		{ID: 11, Type: "Stamp", Creator: "test", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Remark: "显式备注", Print: &disabled, NoZoom: true, NoRotate: true, ReadOnlyValue: &disabled},
	}}}})
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
	_ = app.open(js.Undefined(), []js.Value{input})
	value, ok := app.annotations(js.Undefined(), []js.Value{js.ValueOf(0)}).(js.Value)
	if !ok || value.Type() != js.TypeObject || value.Length() != 2 {
		t.Fatalf("逐页注解返回错误：%v", value)
	}
	first, second := value.Index(0), value.Index(1)
	if !first.Get("print").Bool() || first.Get("no_zoom").Bool() || first.Get("no_rotate").Bool() || !first.Get("read_only").Bool() {
		t.Fatalf("缺省标志丢失：%v", first)
	}
	if second.Get("print").Bool() || !second.Get("no_zoom").Bool() || !second.Get("no_rotate").Bool() || second.Get("read_only").Bool() {
		t.Fatalf("显式标志丢失：%v", second)
	}
	empty, ok := app.annotations(js.Undefined(), []js.Value{js.ValueOf(1)}).(js.Value)
	if !ok || empty.Length() != 0 {
		t.Fatalf("目标页不应借用其他页注解：%v", empty)
	}
}
