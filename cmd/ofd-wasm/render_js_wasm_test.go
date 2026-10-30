//go:build js && wasm

package main

import (
	"context"
	"math"
	"strings"
	"syscall/js"
	"testing"

	ofdreader "github.com/zc310/ofd/pkg/reader"
)

func wasmRenderTestApp(t *testing.T) *wasmApp {
	t.Helper()
	reader, err := ofdreader.Open(wasmDocumentFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	app := &wasmApp{reader: reader}
	t.Cleanup(func() { _ = app.closeCurrentReader() })
	return app
}

func invokeTestRender(app *wasmApp, command string, id uint64) any {
	options := js.ValueOf(map[string]any{"dpi": 12})
	requestID := js.Undefined()
	if id != 0 {
		requestID = js.ValueOf(float64(id))
	}
	switch command {
	case "page":
		return app.renderPage(js.Undefined(), []js.Value{js.ValueOf(0), options, requestID})
	case "pages":
		return app.renderPages(js.Undefined(), []js.Value{js.ValueOf([]any{0, 0}), options, requestID})
	default:
		return app.renderStream(js.Undefined(), []js.Value{js.ValueOf([]any{0}), options, js.Null(), requestID})
	}
}

func TestPageRenderRequestsProcessJSCancelAndClose(t *testing.T) {
	for _, command := range []string{"page", "pages", "single-stream"} {
		for _, action := range []string{"cancel", "close", "close-without-id"} {
			t.Run(command+"/"+action, func(t *testing.T) {
				app := wasmRenderTestApp(t)
				var closeErr error
				cancel := js.FuncOf(func(js.Value, []js.Value) any {
					if action == "cancel" {
						app.cancelRender(js.Undefined(), []js.Value{js.ValueOf(123)})
					} else {
						closeErr = app.closeCurrentReader()
					}
					return nil
				})
				defer cancel.Release()
				timer := js.Global().Call("setTimeout", cancel, 0)
				defer js.Global().Call("clearTimeout", timer)
				id := uint64(123)
				if action == "close-without-id" {
					id = 0
				}
				value, rejected := awaitRenderPromise(t, invokeTestRender(app, command, id))
				if !rejected || !strings.Contains(value.Get("message").String(), "context canceled") || len(app.requests) != 0 || closeErr != nil {
					t.Fatalf("取消未完成: rejected=%v value=%v requests=%d closeErr=%v", rejected, value, len(app.requests), closeErr)
				}
			})
		}
	}
}

func TestPageRenderCancellationDoesNotCancelOtherRequests(t *testing.T) {
	for _, command := range []string{"page", "pages", "single-stream"} {
		t.Run(command, func(t *testing.T) {
			app := wasmRenderTestApp(t)
			first := invokeTestRender(app, command, 1)
			second := invokeTestRender(app, command, 2)
			app.cancelRender(js.Undefined(), []js.Value{js.ValueOf(1)})
			if _, rejected := awaitRenderPromise(t, first); !rejected {
				t.Fatal("被取消的请求仍成功")
			}
			value, rejected := awaitRenderPromise(t, second)
			if rejected || value.Length() == 0 || len(app.requests) != 0 {
				t.Fatalf("另一个请求未正常完成: rejected=%v value=%v requests=%d", rejected, value, len(app.requests))
			}
			if command == "pages" && value.Length() != 2 {
				t.Fatal("批量渲染未返回两页")
			}
		})
	}
}

func TestRenderAsyncFailureReleasesRequest(t *testing.T) {
	app := wasmRenderTestApp(t)
	value, rejected := awaitRenderPromise(t, app.renderAsync(1, func(context.Context) (js.Value, error) {
		panic("test-render-panic")
	}))
	if !rejected || !strings.Contains(value.Get("message").String(), "test-render-panic") || len(app.requests) != 0 {
		t.Fatalf("渲染异常未清理请求: rejected=%v value=%v requests=%d", rejected, value, len(app.requests))
	}
}

func TestRenderRequestIDsRejectInvalidValues(t *testing.T) {
	for _, value := range []js.Value{js.ValueOf(-1), js.ValueOf(0), js.ValueOf(0.5), js.ValueOf(math.NaN()), js.ValueOf(math.Inf(1)), js.ValueOf(float64(1 << 53)), js.ValueOf("1")} {
		if _, err := renderRequestID([]js.Value{value}); err == nil {
			t.Fatalf("接受了无效请求 ID: %v", value)
		}
	}
}

func TestSinglePageRenderStreamAcceptsOmittedOptions(t *testing.T) {
	app := wasmRenderTestApp(t)
	value, rejected := awaitRenderPromise(t, app.renderStream(js.Undefined(), []js.Value{js.ValueOf([]any{0})}))
	if rejected || value.Get("byteLength").Int() == 0 {
		t.Fatalf("省略配置的单页流式渲染失败: rejected=%v value=%v", rejected, value)
	}
}
