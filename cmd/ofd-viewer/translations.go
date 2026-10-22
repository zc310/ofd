package main

import (
	"sync/atomic"

	viewerlocalization "github.com/zc310/ofd/internal/localization/viewer"
)

// File pickers and export workers also translate messages.
//
//nolint:gochecknoglobals // Viewer has one app-wide language preference shared by UI and workers.
var activeLocalizer atomic.Pointer[viewerlocalization.Localizer]

func setViewerLanguage(language string) {
	localizer, err := viewerlocalization.New(language)
	if err == nil {
		activeLocalizer.Store(localizer)
	}
}

func viewerUsesEnglish() bool {
	localizer := activeLocalizer.Load()
	return localizer != nil && localizer.Language() == "en"
}

func tr(message string) string {
	localizer := activeLocalizer.Load()
	if localizer == nil {
		return message
	}
	return localizer.Message(message, nil)
}
