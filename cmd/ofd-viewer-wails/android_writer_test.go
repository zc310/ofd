package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

type androidTestWriter struct {
	write func([]byte) (int, error)
}

func (w androidTestWriter) Write(data []byte) (int, error) { return w.write(data) }

func TestAndroidDocumentWriterBoundsAndCancellation(t *testing.T) {
	data := bytes.Repeat([]byte("a"), 200000)
	var output bytes.Buffer
	writer := androidTestWriter{write: func(chunk []byte) (int, error) {
		if len(chunk) > 64<<10 {
			t.Fatalf("单次写入未受限: %d", len(chunk))
		}
		return output.Write(chunk)
	}}
	if err := writeAndroidDocument(t.Context(), writer, data); err != nil || !bytes.Equal(output.Bytes(), data) {
		t.Fatalf("保存内容不一致: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	count := 0
	err := writeAndroidDocument(ctx, androidTestWriter{write: func(chunk []byte) (int, error) {
		count++
		cancel()
		return len(chunk), nil
	}}, data)
	if !errors.Is(err, context.Canceled) || count != 1 {
		t.Fatalf("取消后仍继续写入: %v, 次数=%d", err, count)
	}
	err = writeAndroidDocument(t.Context(), androidTestWriter{write: func(chunk []byte) (int, error) {
		return len(chunk) - 1, nil
	}}, data)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("短写未报错: %v", err)
	}
}
