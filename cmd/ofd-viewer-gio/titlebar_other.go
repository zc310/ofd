//go:build (!linux || android) && !freebsd && !openbsd

package main

import "gioui.org/app"

func isWaylandView(_ app.ViewEvent) bool { return false }
