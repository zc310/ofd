//go:build js

package main

import (
	"context"
	"errors"
	"io"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/x/explorer"
)

const maxBrowserDocumentSize = 128 << 20

type viewerPicker struct{ explorer *explorer.Explorer }

func newViewerPicker(window *app.Window) *viewerPicker {
	return &viewerPicker{explorer: explorer.NewExplorer(window)}
}

func (p *viewerPicker) listen(e event.Event) { p.explorer.ListenEvents(e) }

func (p *viewerPicker) choose() (selectedFile, error) {
	file, err := p.explorer.ChooseFile(".ofd")
	if errors.Is(err, explorer.ErrUserDecline) {
		return selectedFile{}, nil
	}
	if err != nil {
		return selectedFile{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBrowserDocumentSize+1))
	if err != nil {
		return selectedFile{}, err
	}
	if len(data) > maxBrowserDocumentSize {
		return selectedFile{}, viewerError("浏览器文档超过 128 MiB 限制")
	}
	return selectedFile{name: "浏览器文档.ofd", data: data}, nil
}

func chooseExportFile(_, _ string, _ int) (string, error) {
	return "", viewerError("浏览器导出尚未适配")
}

func exportFile(ctx context.Context, _, _ string, _ exportOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return viewerError("浏览器导出尚未适配")
}

func exportFileInput(ctx context.Context, source, target string, opts exportOptions, _ any) error {
	return exportFile(ctx, source, target, opts)
}
