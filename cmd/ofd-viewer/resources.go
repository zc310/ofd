package main

import (
	"embed"

	"fyne.io/fyne/v2"
)

//go:embed Icon.png
var iconFile embed.FS

// viewerIcon 是内嵌图标。Icon.png 来自仓库，读取失败说明构建产物损坏，
// 直接终止比让关于对话框拿到空资源再崩溃更容易排查。
var viewerIcon = mustLoadViewerIcon()

func mustLoadViewerIcon() fyne.Resource {
	data, err := iconFile.ReadFile("Icon.png")
	if err != nil {
		panic("读取内嵌图标失败: " + err.Error())
	}
	return fyne.NewStaticResource("Icon.png", data)
}
