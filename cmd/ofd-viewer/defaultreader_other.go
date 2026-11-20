//go:build !linux || android || flatpak

package main

import "context"

func defaultReaderSupported() bool { return false }

func detectDefaultReader(context.Context) defaultReaderState { return defaultReaderUnavailable }

func setDefaultReader(context.Context) error {
	return viewerError("当前平台不支持设置默认阅读器")
}
