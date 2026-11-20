//go:build windows

package main

import (
	"context"

	"golang.org/x/sys/windows"
)

func openWebURL(ctx context.Context, uri string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(uri)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, nil, file, nil, nil, windows.SW_SHOWNORMAL)
}
