package converter

import (
	"errors"
	"io"
	"testing"
)

type failingImageWriter struct {
	writeErr error
	closeErr error
	writes   int
	closes   int
}

func (w *failingImageWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(data), nil
}

func (w *failingImageWriter) Close() error {
	w.closes++
	return w.closeErr
}

func TestImageDocumentsPropagateWriterErrors(t *testing.T) {
	writeFailure := errors.New("写入失败")
	closeFailure := errors.New("关闭失败")
	for _, output := range []struct {
		name    string
		format  Option
		backend string
	}{
		{name: "canvas_png", format: PNG()},
		{name: "canvas_jpg", format: JPG()},
		{name: "canvas_svg", format: SVG()},
		{name: "gg_png", format: PNG(), backend: "gg"},
		{name: "gg_jpg", format: JPG(), backend: "gg"},
	} {
		t.Run(output.name, func(t *testing.T) {
			for _, failure := range []struct {
				name     string
				writeErr error
				closeErr error
				wantErr  error
			}{
				{name: "success"},
				{name: "close", closeErr: closeFailure, wantErr: closeFailure},
				{name: "write", writeErr: writeFailure, wantErr: writeFailure},
				{name: "write_and_close", writeErr: writeFailure, closeErr: closeFailure, wantErr: writeFailure},
			} {
				t.Run(failure.name, func(t *testing.T) {
					document := testRenderDocument(2)
					defer document.Close()
					var writers []*failingImageWriter
					err := ImageDocument(ctxTODO, document, output.format, DPI(1), RasterBackend(output.backend), Writer(func(page int) (io.WriteCloser, error) {
						if page != len(writers)+1 {
							t.Fatalf("输出页码 = %d，期望 %d", page, len(writers)+1)
						}
						writer := &failingImageWriter{writeErr: failure.writeErr, closeErr: failure.closeErr}
						writers = append(writers, writer)
						return writer, nil
					}))
					if !errors.Is(err, failure.wantErr) {
						t.Fatalf("转换错误 = %v，期望 %v", err, failure.wantErr)
					}
					if failure.writeErr != nil && errors.Is(err, closeFailure) {
						t.Fatalf("关闭错误覆盖了写入错误: %v", err)
					}
					wantPages := 2
					if failure.wantErr != nil {
						wantPages = 1
					}
					if len(writers) != wantPages {
						t.Fatalf("输出了 %d 页，期望 %d 页", len(writers), wantPages)
					}
					for index, writer := range writers {
						if writer.writes == 0 || writer.closes != 1 {
							t.Errorf("第 %d 页写入 %d 次、关闭 %d 次，期望写入后恰好关闭一次", index+1, writer.writes, writer.closes)
						}
					}
				})
			}
		})
	}
}
