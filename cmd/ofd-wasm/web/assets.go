// Package web 提供桌面阅读器使用的共享网页资源，不包含 WASM 产物。
package web

import "embed"

// Assets 只包含原生桌面界面实际使用的静态资源。
//
//go:embed index.html viewer.js material-symbols-outlined-subset.woff2
var Assets embed.FS
