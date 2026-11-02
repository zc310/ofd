package pdfcpuwriter

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"io"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

var _ canvas.Renderer = (*Document)(nil)

func TestDocumentRejectsInvalidPageSizeBeforeSealingPage(t *testing.T) {
	for _, size := range [][2]float64{{0, 10}, {-1, 10}, {10, math.NaN()}, {math.Inf(1), 10}, {math.MaxFloat64, 10}} {
		if _, err := New(t.Context(), size[0], size[1]); !errors.Is(err, errInvalidPageSize) {
			t.Fatalf("无效尺寸%v未拒绝：%v", size, err)
		}
	}
	r, err := New(t.Context(), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.NextPage(0, 30); !errors.Is(err, errInvalidPageSize) || r.Stats().Pages != 0 {
		t.Fatalf("参数错误不应封存当前页：%v，%+v", err, r.Stats())
	}
	if width, height := r.Size(); width != 40 || height != 30 {
		t.Fatal("参数错误改变了页面尺寸")
	}
}

func TestDocumentCannotWriteOrDrawAfterWrite(t *testing.T) {
	r, err := New(t.Context(), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	r.RenderPath(canvas.Rectangle(10, 10), canvas.DefaultStyle, canvas.Identity)
	var output bytes.Buffer
	if err := r.Write(&output); err != nil {
		t.Fatal(err)
	}
	if err := api.Validate(t.Context(), bytes.NewReader(output.Bytes()), model.NewDefaultConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
	before := r.content.String()
	var repeated bytes.Buffer
	if err := r.Write(&repeated); !errors.Is(err, ErrClosed) || repeated.Len() != 0 {
		t.Fatalf("重复写出未拒绝：%v", err)
	}
	if err := r.NextPage(50, 40); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	r.RenderPath(canvas.Rectangle(20, 20), canvas.DefaultStyle, canvas.Identity)
	if r.content.String() != before || !errors.Is(r.Err(), ErrClosed) {
		t.Fatal("写出后仍可修改内容")
	}
}

func TestDocumentCancellationAndUnsupportedPaintDoNotWriteOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r, err := New(ctx, 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	var output bytes.Buffer
	if err := r.Write(&output); err != ctx.Err() || output.Len() != 0 { //nolint:errorlint // 取消契约要求原样返回 ctx.Err()。
		t.Fatalf("取消未原样返回：%v", err)
	}
	r, err = New(t.Context(), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	gradient := canvas.NewLinearGradient(canvas.Point{}, canvas.Point{X: 10})
	gradient.Add(0, color.RGBA{R: 128, A: 128})
	gradient.Add(1, canvas.Blue)
	style := canvas.DefaultStyle
	style.Fill = canvas.Paint{Gradient: gradient}
	r.RenderPath(canvas.Rectangle(10, 10), style, canvas.Identity)
	if !errors.Is(r.Err(), ErrUnsupported) {
		t.Fatalf("未报告不支持的绘制：%v", r.Err())
	}
	if err := r.Write(&output); !errors.Is(err, ErrUnsupported) || output.Len() != 0 {
		t.Fatalf("绘制失败不应写出：%v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDocumentReturnsWriteFailureAndCannotBeRetried(t *testing.T) {
	r, err := New(t.Context(), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Write(failingWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("丢失底层写入错误：%v", err)
	}
	if err := r.Write(io.Discard); !errors.Is(err, ErrClosed) {
		t.Fatalf("写入失败后不应重试：%v", err)
	}
}
