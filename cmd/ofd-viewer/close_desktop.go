//go:build !android

package main

import (
	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/layout"
)

type windowCloser struct{ window *app.Window }

func newWindowCloser(window *app.Window) *windowCloser { return &windowCloser{window: window} }
func (*windowCloser) listen(event.Event)               {}
func (c *windowCloser) close()                         { c.window.Perform(system.ActionClose) }

func isDetachedWindowEvent(event.Event) bool { return false }
func drainDetachedWindow(*app.Window)        {}

func (*viewer) handlePlatformBack(layout.Context, func(request), func()) {}
