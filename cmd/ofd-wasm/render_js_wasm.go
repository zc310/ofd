//go:build js && wasm

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"syscall/js"
)

type wasmRenderRequest struct {
	id     uint64
	cancel context.CancelFunc
}

// renderAsync 将渲染放进 Promise，使 Worker 能在阶段边界处理取消消息。
// 用请求对象登记生命周期，未指定 ID 的直接调用也能随文档关闭一起取消。
func (a *wasmApp) renderAsync(id uint64, render func(context.Context) (js.Value, error)) js.Value {
	ctx, cancel := context.WithCancel(context.Background())
	request := &wasmRenderRequest{id: id, cancel: cancel}
	a.requestsMu.Lock()
	if a.requests == nil {
		a.requests = make(map[*wasmRenderRequest]struct{})
	}
	a.requests[request] = struct{}{}
	a.requestsMu.Unlock()

	executor := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolve, reject := args[0], args[1]
		go func() {
			result := js.Undefined()
			var err error
			defer func() {
				if recovered := recover(); recovered != nil {
					err = fmt.Errorf("渲染失败: %v", recovered)
				}
				if ctx.Err() != nil {
					err = ctx.Err()
				}
				cancel()
				a.requestsMu.Lock()
				delete(a.requests, request)
				a.requestsMu.Unlock()
				if err != nil {
					_ = invokeJSCallback(reject, js.Global().Get("Error").New(err.Error()))
					return
				}
				_ = invokeJSCallback(resolve, result)
			}()
			result, err = render(ctx)
		}()
		return nil
	})
	promise := js.Global().Get("Promise").New(executor)
	executor.Release()
	return promise
}

func renderRequestID(args []js.Value) (uint64, error) {
	if len(args) == 0 || args[0].IsUndefined() || args[0].IsNull() {
		return 0, nil
	}
	value := args[0]
	if value.Type() != js.TypeNumber || value.IsNaN() || math.IsInf(value.Float(), 0) ||
		value.Float() != math.Trunc(value.Float()) || value.Float() <= 0 || value.Float() > 1<<53-1 {
		return 0, errors.New("渲染请求 ID 必须为正的安全整数")
	}
	return uint64(value.Float()), nil
}

func (a *wasmApp) cancelRender(_ js.Value, args []js.Value) any {
	if len(args) != 1 {
		return errorValue(errors.New("ofd.cancelRender 需要请求 ID"))
	}
	id, err := renderRequestID(args)
	if err != nil || id == 0 {
		return errorValue(errors.New("ofd.cancelRender 的请求 ID 无效"))
	}
	a.requestsMu.Lock()
	for request := range a.requests {
		if request.id == id {
			request.cancel()
		}
	}
	a.requestsMu.Unlock()
	return nil
}
