package markdown_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
	markdown "github.com/zc310/ofd/pkg/converter/import/markdown"
)

type cancelReader struct {
	io.Reader
	cancel context.CancelFunc
}

func (r cancelReader) Read(p []byte) (int, error) { r.cancel(); return r.Reader.Read(p) }

type cancelWriter struct {
	cancel context.CancelFunc
	writes int
}

func (w *cancelWriter) Write(p []byte) (int, error) { w.writes++; w.cancel(); return len(p), nil }

func TestMarkdownImportHonorsCancellationDuringReadAndWrite(t *testing.T) {
	for _, direct := range []bool{false, true} {
		convert := func(ctx context.Context, input any, output io.Writer) error {
			if direct {
				return markdown.Convert(ctx, input, output)
			}
			return converter.Convert(ctx, "markdown", "ofd", input, output)
		}
		ctx, cancel := context.WithCancel(t.Context())
		var output bytes.Buffer
		err := convert(ctx, cancelReader{Reader: strings.NewReader("# 取消测试\n\n正文"), cancel: cancel}, &output)
		cancel()
		if err != context.Canceled || output.Len() != 0 {
			t.Fatalf("读取中取消 direct=%v: err=%v output=%d", direct, err, output.Len())
		}
		ctx, cancel = context.WithCancel(t.Context())
		writer := &cancelWriter{cancel: cancel}
		err = convert(ctx, []byte("# 取消测试\n\n正文"), writer)
		cancel()
		if err != context.Canceled || writer.writes != 1 {
			t.Fatalf("写出中取消 direct=%v: err=%v writes=%d", direct, err, writer.writes)
		}
	}
}

func TestMarkdownConvertFileCanceledBeforeEntryPreservesExistingOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.ofd")
	want := []byte("original")
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := markdown.ConvertFile(ctx, "missing.md", path); err != context.Canceled {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("取消后原输出被修改: %q %v", got, err)
	}
}
