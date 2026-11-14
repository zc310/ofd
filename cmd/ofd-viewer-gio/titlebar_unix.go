//go:build (linux && !android) || freebsd || openbsd

package main

import "gioui.org/app"

func isWaylandView(event app.ViewEvent) bool {
	view, ok := event.(app.WaylandViewEvent)
	return ok && view.Valid()
}
