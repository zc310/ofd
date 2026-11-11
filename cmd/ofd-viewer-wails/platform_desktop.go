//go:build !android

package main

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (d *Desktop) savePicker(_ context.Context, title string) func(string) (string, error) {
	return func(name string) (string, error) {
		dialog := d.app.Dialog.SaveFile()
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		options := &application.SaveFileDialogOptions{Title: d.tr(title), Filename: name, CanCreateDirectories: true}
		if ext != "" {
			options.Filters = []application.FileFilter{{DisplayName: strings.ToUpper(ext), Pattern: "*." + ext}}
		}
		dialog.SetOptions(options)
		return dialog.PromptForSingleSelection()
	}
}

func releaseSaveTarget(string) {}

func (d *Desktop) prunePlatformImports() {}

func releasePlatformCredential(string) {}

func writePlatformSave(context.Context, string, []byte) (bool, error) { return false, nil }

func (d *Desktop) checkSaveTarget(path string) error { return d.checkAttachmentTarget(path) }
