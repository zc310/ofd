//go:build js && wasm

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"syscall/js"
	"testing"
	"time"

	ofdreader "github.com/zc310/ofd/pkg/reader"
)

func awaitRenderPromise(t *testing.T, value any) (js.Value, bool) {
	t.Helper()
	promise, ok := value.(js.Value)
	if !ok || promise.Get("then").Type() != js.TypeFunction {
		t.Fatalf("渲染未返回 Promise: %v", value)
	}
	type result struct {
		value    js.Value
		rejected bool
	}
	done := make(chan result, 1)
	resolve := js.FuncOf(func(_ js.Value, args []js.Value) any { done <- result{value: args[0]}; return nil })
	reject := js.FuncOf(func(_ js.Value, args []js.Value) any { done <- result{value: args[0], rejected: true}; return nil })
	defer resolve.Release()
	defer reject.Release()
	promise.Call("then", resolve, reject)
	select {
	case result := <-done:
		return result.value, result.rejected
	case <-time.After(10 * time.Second):
		t.Fatal("流式渲染未退出")
		return js.Undefined(), false
	}
}

func TestJSChunkWriterCanceledFlushDoesNotEmit(t *testing.T) {
	calls := 0
	callback := js.FuncOf(func(js.Value, []js.Value) any { calls++; return nil })
	defer callback.Release()
	for _, streamID := range []uint64{0, 1} {
		writer := newJSChunkWriter(t.Context(), callback.Value, streamID)
		if _, err := writer.Write([]byte("pending")); err != nil {
			t.Fatal(err)
		}
		writer.cancel()
		writer.cancel()
		if err := writer.flush(); !errors.Is(err, context.Canceled) {
			t.Fatalf("取消后 flush = %v", err)
		}
		if _, err := writer.Write([]byte("late")); !errors.Is(err, context.Canceled) {
			t.Fatalf("取消后 Write = %v", err)
		}
	}
	if calls != 0 {
		t.Fatalf("取消后仍发送 %d 个块", calls)
	}
}

func TestJSChunkWriterCancellationReleasesPendingACK(t *testing.T) {
	var writer *jsChunkWriter
	callback := js.FuncOf(func(js.Value, []js.Value) any {
		writer.cancel()
		return nil
	})
	defer callback.Release()
	writer = newJSChunkWriter(t.Context(), callback.Value, 1)
	defer writer.cancel()
	_, err := writer.Write(make([]byte, wasmOutputChunkSize))
	if !errors.Is(err, context.Canceled) || len(writer.acks) != 0 {
		t.Fatalf("取消未释放 ACK: err=%v acks=%d", err, len(writer.acks))
	}
}

func TestRenderStreamProcessesJSCancellationBeforeFirstChunk(t *testing.T) {
	for _, format := range []string{"png", "jpg", "pdf"} {
		for _, action := range []string{"cancel", "close"} {
			t.Run(format+"/"+action, func(t *testing.T) {
				reader, err := ofdreader.Open(wasmDocumentFixture(t))
				if err != nil {
					t.Fatal(err)
				}
				app := &wasmApp{reader: reader}
				defer app.closeCurrentReader()
				chunks := 0
				callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
					chunks++
					app.streamAck(js.Undefined(), []js.Value{js.ValueOf(1), args[1]})
					return nil
				})
				defer callback.Release()
				var closeErr error
				cancel := js.FuncOf(func(js.Value, []js.Value) any {
					if action == "close" {
						closeErr = app.closeCurrentReader()
					} else {
						app.cancelRender(js.Undefined(), []js.Value{js.ValueOf(1)})
					}
					return nil
				})
				defer cancel.Release()
				// JS 定时器必须等 WASM 让出事件循环才能执行，不能只依靠 Go 调度。
				timer := js.Global().Call("setTimeout", cancel, 0)
				defer js.Global().Call("clearTimeout", timer)
				value := app.renderStream(js.Undefined(), []js.Value{
					js.ValueOf([]any{0, 0, 0}),
					js.ValueOf(map[string]any{"format": format, "dpi": 12}),
					callback.Value, js.ValueOf(1),
				})
				_, rejected := awaitRenderPromise(t, value)
				if !rejected || chunks != 0 || len(app.streams) != 0 || len(app.requests) != 0 || closeErr != nil {
					t.Fatalf("取消未及时结束: rejected=%v chunks=%d streams=%d closeErr=%v", rejected, chunks, len(app.streams), closeErr)
				}
				_, err = reader.RenderPage(t.Context(), 0, ofdreader.RenderOptions{DPI: 12})
				if action == "cancel" && err != nil {
					t.Fatalf("取消后 Reader 不可复用: %v", err)
				}
				if action == "close" && err == nil {
					t.Fatal("取消并关闭后 Reader 仍可读取")
				}
			})
		}
	}
}

func TestRenderStreamCompletesWithACKs(t *testing.T) {
	for _, format := range []string{"png", "jpg", "pdf"} {
		t.Run(format, func(t *testing.T) {
			reader, err := ofdreader.Open(wasmDocumentFixture(t))
			if err != nil {
				t.Fatal(err)
			}
			app := &wasmApp{reader: reader}
			defer app.closeCurrentReader()
			var output bytes.Buffer
			callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
				data := make([]byte, args[0].Get("byteLength").Int())
				js.CopyBytesToGo(data, args[0])
				output.Write(data)
				app.streamAck(js.Undefined(), []js.Value{js.ValueOf(1), args[1]})
				return nil
			})
			defer callback.Release()
			value := app.renderStream(js.Undefined(), []js.Value{
				js.ValueOf([]any{0, 0}),
				js.ValueOf(map[string]any{"format": format, "dpi": 12}),
				callback.Value, js.ValueOf(1),
			})
			if _, rejected := awaitRenderPromise(t, value); rejected || len(app.streams) != 0 || len(app.requests) != 0 {
				t.Fatal("正常流式输出未完成或未清理")
			}
			if format == "pdf" {
				if !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) || !bytes.Contains(output.Bytes(), []byte("%%EOF")) {
					t.Fatal("PDF 输出不完整")
				}
			} else {
				archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
				if err != nil || len(archive.File) != 2 {
					t.Fatalf("图片 ZIP 输出不完整: %v", err)
				}
			}
		})
	}
}
