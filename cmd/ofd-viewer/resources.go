package main

import (
	"embed"

	"fyne.io/fyne/v2"
)

//go:embed Icon.png
var iconFile embed.FS

var viewerIcon = func() fyne.Resource {
	data, err := iconFile.ReadFile("Icon.png")
	if err != nil {
		return nil
	}
	return fyne.NewStaticResource("Icon.png", data)
}()
