package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
	"golang.org/x/image/font/gofont/goregular"
)

func TestCurrentPagePDFHasSelectableTextAndTXTIsUTF8(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "page-text-export", Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}},
		Pages: []creator.Page{{Items: []creator.Item{creator.Text{X: 10, Y: 10, Width: 100, Height: 10, Font: "Go", Size: 4, Value: "Selectable text"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "input.ofd")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	desktop := &Desktop{document: service}
	opened, err := desktop.openPath(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"pdf", "txt"} {
		output := filepath.Join(t.TempDir(), "page."+format)
		saved, err := desktop.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, format, 600, "#ffffffff", func(string) (string, error) { return output, nil })
		if err != nil || !saved {
			t.Fatalf("%s 导出 = %v, %v", format, saved, err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if format == "pdf" {
			if !bytes.Contains(data, []byte("/ToUnicode")) || !bytes.HasSuffix(bytes.TrimSpace(data), []byte("%%EOF")) {
				t.Fatal("PDF 不完整或没有可搜索文字映射")
			}
		} else if string(data) != "Selectable text" {
			t.Fatalf("TXT = %q", data)
		}
	}
}

func TestWritePageTextOrdersGeometryWithoutMutatingRuns(t *testing.T) {
	runs := []reader.TextRun{{Text: "下一行", X: 1, Y: 10, Height: 4}, {Text: "乙", X: 8, Y: 1, Height: 4}, {Text: "甲", X: 1, Y: 1, Height: 4}}
	var output pageExportBuffer
	if err := writePageText(t.Context(), &output, runs); err != nil {
		t.Fatal(err)
	}
	if output.data.String() != "甲乙\n下一行" || runs[0].Text != "下一行" {
		t.Fatalf("文字顺序或输入快照被改动: %q, %+v", output.data.String(), runs)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := writePageText(ctx, &output, runs); !errors.Is(err, context.Canceled) {
		t.Fatalf("文字导出取消 = %v", err)
	}
}

func TestPDFAndTXTExportRejectPermissionAndCancelledSave(t *testing.T) {
	for _, format := range []string{"pdf", "txt"} {
		t.Run(format, func(t *testing.T) {
			denied, opened, _ := desktopWithAttachment(t, false)
			if _, err := denied.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, format, 150, "#ffffffff", func(string) (string, error) {
				t.Error("禁止导出时不应显示对话框")
				return "", nil
			}); !errors.Is(err, reader.ErrPermissionDenied) {
				t.Fatalf("权限拒绝 = %v", err)
			}
			desktop, opened, _ := desktopWithAttachment(t, true)
			output := filepath.Join(t.TempDir(), "cancelled."+format)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if _, err := desktop.exportPageWithPicker(ctx, opened.PagePrefix, 0, format, 150, "#ffffffff", func(string) (string, error) {
				cancel()
				return output, nil
			}); !errors.Is(err, context.Canceled) {
				t.Fatalf("取消 = %v", err)
			}
			if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("取消后产生输出: %v", err)
			}
		})
	}
}
