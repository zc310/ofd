package converter

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

func conversionOutputFixture(t *testing.T, pages int) []byte {
	t.Helper()
	document := creator.Document{ID: "converter-cancel", PageSize: creator.PageSize{Width: 20, Height: 20},
		Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}}}
	for range pages {
		document.Pages = append(document.Pages, creator.Page{Items: []creator.Item{
			creator.Text{X: 1, Y: 1, Width: 18, Height: 6, Font: "Go", Size: 3, Value: "Cancel test"},
		}})
	}
	data, err := creator.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type cancelOutput struct {
	cancel         context.CancelFunc
	onClose        bool
	writes, closes int
	data           bytes.Buffer
}

func (w *cancelOutput) Write(data []byte) (int, error) {
	w.writes++
	if !w.onClose {
		w.cancel()
	}
	return w.data.Write(data)
}
func (w *cancelOutput) Close() error {
	w.closes++
	if w.onClose {
		w.cancel()
	}
	return nil
}

func TestConversionsReportCancellationDuringFinalOutput(t *testing.T) {
	input := conversionOutputFixture(t, 1)
	for _, format := range []string{"pdf", "png", "jpeg", "svg", "tiff", "text", "markdown", "docx", "html"} {
		for _, parallel := range []bool{false, true} {
			if parallel && format != "pdf" {
				continue
			}
			name := format + "/serial"
			if parallel {
				name = format + "/parallel"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				output := &cancelOutput{cancel: cancel}
				err := Encode(ctx, format, input, output, DPI(24), PDFParallel(parallel))
				if err != context.Canceled || output.writes == 0 {
					t.Fatalf("最后一页写出中取消 = %v writes=%d", err, output.writes)
				}
				if format == "pdf" {
					if !strings.HasSuffix(strings.TrimSpace(output.data.String()), "%%EOF") {
						t.Fatal("取消后未完成 PDF 收尾")
					}
				} else if output.writes != 1 {
					t.Fatalf("取消后仍调用底层写入器 %d 次", output.writes)
				}
			})
		}
	}
}

func TestImageOutputCallbacksHonorCancellationAndAlwaysClose(t *testing.T) {
	input := conversionOutputFixture(t, 1)
	for _, format := range []string{"png", "jpeg", "svg", "tiff"} {
		for _, onClose := range []bool{false, true} {
			ctx, cancel := context.WithCancel(t.Context())
			output := &cancelOutput{cancel: cancel, onClose: onClose}
			err := Image(ctx, input, WithFormat(format), DPI(24), Writer(func(int) (io.WriteCloser, error) { return output, nil }))
			cancel()
			if err != context.Canceled || output.closes != 1 || output.writes == 0 {
				t.Fatalf("format=%s onClose=%v: err=%v writes=%d closes=%d", format, onClose, err, output.writes, output.closes)
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	document := testRenderDocument(1)
	defer document.Close()
	err := ImageDocument(ctx, document, DPI(1), ImageWriter(func(int, image.Image) error { cancel(); return nil }))
	if err != context.Canceled {
		t.Fatalf("最后一个图像回调取消 = %v", err)
	}
}

type failedPDFOutput struct {
	failed atomic.Bool
	writes int
	short  bool
}

var pdfOutputFailure = errors.New("PDF 输出失败")

func (w *failedPDFOutput) Write([]byte) (int, error) {
	w.writes++
	w.failed.Store(true)
	if w.short {
		return 0, nil
	}
	return 0, pdfOutputFailure
}

type failureObservedBackground struct {
	writer *failedPDFOutput
	draws  atomic.Int32
}

func (c *failureObservedBackground) RGBA() (uint32, uint32, uint32, uint32) {
	if c.writer.failed.Load() {
		c.draws.Add(1)
	}
	return color.White.RGBA()
}

func TestPDFFailedOutputStopsFurtherRendering(t *testing.T) {
	input := conversionOutputFixture(t, 10)
	for _, parallel := range []bool{false, true} {
		for _, short := range []bool{false, true} {
			output := &failedPDFOutput{short: short}
			background := &failureObservedBackground{writer: output}
			err := PDF(t.Context(), input, output, PDFParallel(parallel), BgColor(background))
			want := pdfOutputFailure
			if short {
				want = io.ErrShortWrite
			}
			if !errors.Is(err, want) || output.writes != 1 || background.draws.Load() != 0 {
				t.Fatalf("parallel=%v short=%v: err=%v writes=%d drawsAfterFailure=%d", parallel, short, err, output.writes, background.draws.Load())
			}
		}
	}
}

func TestCanceledTIFFNeverCreatesOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	created := 0
	option := Writer(func(int) (io.WriteCloser, error) { created++; return discardWriteCloser{}, nil })
	if err := Image(ctx, []byte("unused"), WithFormat("tiff"), option); err != context.Canceled {
		t.Fatal(err)
	}
	document := testRenderDocument(1)
	defer document.Close()
	if err := ImageDocument(ctx, document, WithFormat("tiff"), option); err != context.Canceled {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("已取消的 TIFF 创建了 %d 个输出", created)
	}
}

func TestTIFFOutputPreservesWriteAndCloseFailures(t *testing.T) {
	input := conversionOutputFixture(t, 1)
	closeFailure := errors.New("TIFF 关闭失败")
	writeFailure := errors.New("TIFF 写入失败")
	for _, test := range []struct{ writeErr, closeErr, want error }{
		{closeErr: closeFailure, want: closeFailure},
		{writeErr: writeFailure, closeErr: closeFailure, want: writeFailure},
	} {
		writer := &failingImageWriter{writeErr: test.writeErr, closeErr: test.closeErr}
		err := Image(t.Context(), input, WithFormat("tiff"), DPI(24), Writer(func(int) (io.WriteCloser, error) { return writer, nil }))
		if !errors.Is(err, test.want) || writer.closes != 1 {
			t.Fatalf("TIFF 错误或资源关闭异常: err=%v closes=%d", err, writer.closes)
		}
	}
}

func TestTIFFCancellationDuringOutputCreationStillClosesWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := &failingImageWriter{}
	err := Image(ctx, []byte("unused"), WithFormat("tiff"), Writer(func(int) (io.WriteCloser, error) { cancel(); return writer, nil }))
	if err != context.Canceled || writer.writes != 0 || writer.closes != 1 {
		t.Fatalf("取消后的 TIFF 写入器未正确清理: err=%v writes=%d closes=%d", err, writer.writes, writer.closes)
	}
}
