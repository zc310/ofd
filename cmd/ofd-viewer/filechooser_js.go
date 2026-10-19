//go:build js && wasm

package main

import (
	"bytes"
	"fmt"
	"io"
	"syscall/js"
	"time"

	"fyne.io/fyne/v2"
)

// chooseOpenFile runs in the loading goroutine; callbacks only publish results
// so the browser event loop stays available while the file is being read.
func chooseOpenFile(_ string, _ fyne.Window) (fileSelection, error) {
	document := js.Global().Get("document")
	input := document.Call("createElement", "input")
	input.Set("type", "file")
	input.Set("accept", ".ofd,.OFD")
	input.Get("style").Set("display", "none")
	document.Get("body").Call("appendChild", input)
	defer input.Call("remove")

	selected := make(chan js.Value, 1)
	onChange := js.FuncOf(func(js.Value, []js.Value) any {
		files := input.Get("files")
		if files.Length() == 0 {
			selected <- js.Undefined()
		} else {
			selected <- files.Index(0)
		}
		return nil
	})
	onCancel := js.FuncOf(func(js.Value, []js.Value) any {
		selected <- js.Undefined()
		return nil
	})
	input.Set("onchange", onChange)
	input.Set("oncancel", onCancel)
	defer func() {
		input.Set("onchange", js.Null())
		input.Set("oncancel", js.Null())
		onChange.Release()
		onCancel.Release()
	}()
	input.Call("click")
	file := <-selected
	if file.IsUndefined() {
		return fileSelection{}, nil
	}
	data, err := readBrowserFile(file)
	if err != nil {
		return fileSelection{}, err
	}
	return fileSelection{name: file.Get("name").String(), input: data}, nil
}

func readBrowserFile(file js.Value) ([]byte, error) {
	reader := js.Global().Get("FileReader").New()
	done := make(chan error, 1)
	onLoad := js.FuncOf(func(js.Value, []js.Value) any {
		done <- nil
		return nil
	})
	onError := js.FuncOf(func(js.Value, []js.Value) any {
		done <- fmt.Errorf(tr("读取文件失败: %s"), reader.Get("error").Get("message").String())
		return nil
	})
	reader.Set("onload", onLoad)
	reader.Set("onerror", onError)
	defer func() {
		reader.Set("onload", js.Null())
		reader.Set("onerror", js.Null())
		onLoad.Release()
		onError.Release()
	}()
	reader.Call("readAsArrayBuffer", file)
	if err := <-done; err != nil {
		return nil, err
	}
	array := js.Global().Get("Uint8Array").New(reader.Get("result"))
	data := make([]byte, array.Get("byteLength").Int())
	js.CopyBytesToGo(data, array)
	return data, nil
}

func chooseSaveFile(_ string, fileName, _ string, _ fyne.Window) (fileSelection, error) {
	return fileSelection{name: fileName, output: &browserDownload{name: fileName}}, nil
}

// browserDownload buffers exports because the browser downloads a complete Blob.
type browserDownload struct {
	buffer bytes.Buffer
	name   string
	closed bool
}

func (w *browserDownload) Write(data []byte) (int, error) {
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	return w.buffer.Write(data)
}

func (w *browserDownload) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	array := js.Global().Get("Uint8Array").New(w.buffer.Len())
	js.CopyBytesToJS(array, w.buffer.Bytes())
	w.buffer = bytes.Buffer{}
	blob := js.Global().Get("Blob").New([]any{array}, map[string]any{"type": "application/octet-stream"})
	urlAPI := js.Global().Get("URL")
	url := urlAPI.Call("createObjectURL", blob).String()
	link := js.Global().Get("document").Call("createElement", "a")
	link.Set("href", url)
	link.Set("download", w.name)
	js.Global().Get("document").Get("body").Call("appendChild", link)
	link.Call("click")
	link.Call("remove")
	// Let the browser start consuming the Blob before releasing its URL.
	time.AfterFunc(time.Second, func() { urlAPI.Call("revokeObjectURL", url) })
	return nil
}
