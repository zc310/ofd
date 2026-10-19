//go:build js && wasm

package main

import (
	"bytes"
	"errors"
	"io"
	"syscall/js"
	"testing"
)

// These tests can run with Go's Node WASM runner, without a graphical driver:
//
//	GOOS=js GOARCH=wasm go test -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
//	  filechooser.go filechooser_js.go translations.go filechooser_js_test.go
func mockBrowserFiles(t *testing.T, mode string) {
	t.Helper()
	js.Global().Set("viewerTestMode", mode)
	js.Global().Call("eval", `
		globalThis.viewerRemoved = 0;
		globalThis.document = {
			body: { appendChild() {} },
			createElement(tag) {
				return {
					style: {}, files: [{name: "示例.OFD"}],
					remove() { viewerRemoved++; },
					click() {
						if (tag === "a") {
							globalThis.viewerDownloadName = this.download;
							return;
						}
						setTimeout(() => {
							if (viewerTestMode === "cancel") this.oncancel();
							else this.onchange();
						}, 0);
					}
				};
			}
		};
		globalThis.FileReader = class {
			readAsArrayBuffer() {
				setTimeout(() => {
					if (viewerTestMode === "error") {
						this.error = {message: "read failed"};
						this.onerror();
					} else {
						this.result = new Uint8Array([0, 127, 128, 255]).buffer;
						this.onload();
					}
				}, 0);
			}
		};
		globalThis.Blob = class {
			constructor(parts) { globalThis.viewerDownloadBytes = parts[0]; }
		};
		globalThis.URL = {
			createObjectURL() { return "blob:viewer-test"; },
			revokeObjectURL() {}
		};
	`)
}

func TestBrowserOpenPreservesFilenameAndBytes(t *testing.T) {
	mockBrowserFiles(t, "open")
	selection, err := chooseOpenFile("", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, ok := selection.input.([]byte)
	if !ok || !bytes.Equal(data, []byte{0, 127, 128, 255}) || selection.name != "示例.OFD" {
		t.Fatalf("unexpected selection: %#v", selection)
	}
	if js.Global().Get("viewerRemoved").Int() != 1 {
		t.Fatal("file input was not removed")
	}
}

func TestBrowserOpenCancelAndReadError(t *testing.T) {
	for _, mode := range []string{"cancel", "error"} {
		t.Run(mode, func(t *testing.T) {
			mockBrowserFiles(t, mode)
			selection, err := chooseOpenFile("", nil)
			if (err != nil) != (mode == "error") || selection.input != nil {
				t.Fatalf("selection = %#v, err = %v", selection, err)
			}
			if js.Global().Get("viewerRemoved").Int() != 1 {
				t.Fatal("file input was not removed")
			}
		})
	}
}

func TestBrowserExportDownloadsBufferedBytesOnce(t *testing.T) {
	mockBrowserFiles(t, "save")
	selection, err := chooseSaveFile("", "示例.zip", "zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := selection.output
	want := []byte{0, 127, 128, 255}
	if _, err := w.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	js.CopyBytesToGo(got, js.Global().Get("viewerDownloadBytes"))
	if !bytes.Equal(got, want) || js.Global().Get("viewerDownloadName").String() != "示例.zip" {
		t.Fatalf("download bytes = %v", got)
	}
	if js.Global().Get("viewerRemoved").Int() != 1 {
		t.Fatal("download was repeated or link was not removed")
	}
	if _, err := w.Write(want); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write after close = %v", err)
	}
}
