//go:build android

package main

import (
	"log/slog"
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"git.wow.st/gmp/jni"
)

type windowCloser struct {
	window *app.Window
	view   atomic.Uintptr
}

func newWindowCloser(window *app.Window) *windowCloser { return &windowCloser{window: window} }

func (c *windowCloser) listen(e event.Event) {
	if view, ok := e.(app.AndroidViewEvent); ok {
		c.view.Store(view.View)
	}
}

func isDetachedWindowEvent(e event.Event) bool {
	view, ok := e.(app.AndroidViewEvent)
	return ok && !view.Valid()
}

// Gio 要求继续读取事件以确认视图分离；否则 Android UI 线程会卡在事件刷新。
func drainDetachedWindow(window *app.Window) {
	go func() {
		for {
			switch e := window.Event().(type) {
			case app.FrameEvent:
				e.Frame(new(op.Ops))
			case app.DestroyEvent:
				return
			}
		}
	}()
}

func (c *windowCloser) close() {
	// Run 等待 UI 线程执行，不能在 Gio 的事件线程同步调用。
	go c.window.Run(func() {
		view := c.view.Load()
		if view == 0 {
			slog.Warn("关闭 Android 阅读器失败：窗口不可用")
			return
		}
		if err := finishAndroidActivity(view); err != nil {
			slog.Error("关闭 Android 阅读器失败", "error", err)
		}
	})
}

func (v *viewer) handlePlatformBack(gtx layout.Context, submit func(request), closeWindow func()) {
	if _, ok := gtx.Event(key.Filter{Name: key.NameBack}); ok {
		v.handleAndroidBack(gtx, submit, closeWindow)
	}
}

func finishAndroidActivity(view uintptr) error {
	return jni.Do(jni.JVMFor(app.JavaVM()), func(env jni.Env) error {
		object := jni.Object(view)
		viewClass := jni.GetObjectClass(env, object)
		defer jni.DeleteLocalRef(env, jni.Object(viewClass))
		method := jni.GetMethodID(env, viewClass, "getContext", "()Landroid/content/Context;")
		if method == nil {
			return viewerError("无法获取 Android 窗口上下文")
		}
		activity, err := jni.CallObjectMethod(env, object, method)
		if err != nil {
			return err
		}
		if activity == 0 {
			return viewerError("Android 窗口上下文为空")
		}
		defer jni.DeleteLocalRef(env, activity)
		class := jni.GetObjectClass(env, activity)
		defer jni.DeleteLocalRef(env, jni.Object(class))
		finish := jni.GetMethodID(env, class, "finish", "()V")
		if finish == nil {
			return viewerError("无法关闭 Android 窗口")
		}
		return jni.CallVoidMethod(env, activity, finish)
	})
}
