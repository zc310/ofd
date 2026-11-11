package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
)

func TestSaveMediaRechecksExportWithoutRestrictingImagePreview(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, allowed := range []bool{true, false} {
		t.Run(strconv.FormatBool(allowed), func(t *testing.T) {
			data, err := creator.Marshal(creator.Document{ID: "save-media", Pages: []creator.Page{{}},
				Permissions: &creator.Permissions{Export: &allowed},
				Media:       []creator.Media{{ID: 200, Type: "Image", Format: "png", Name: "picture.png", Data: picture.Bytes()}}})
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
			if _, err := desktop.MediaImagePreview(t.Context(), opened.PagePrefix, 0, 200); err != nil {
				t.Fatalf("图片预览不应要求导出权限: %v", err)
			}
			output := filepath.Join(t.TempDir(), "picture.png")
			saved, err := desktop.saveMediaWithPicker(t.Context(), opened.PagePrefix, 0, 200, func(name string) (string, error) {
				if !allowed {
					t.Error("禁止导出时不应打开对话框")
				}
				if name != "picture.png" {
					t.Errorf("资源文件名 = %q", name)
				}
				return output, nil
			})
			if !allowed {
				if saved || !errors.Is(err, reader.ErrPermissionDenied) {
					t.Fatalf("资源导出权限 = %v, %v", saved, err)
				}
				return
			}
			if err != nil || !saved {
				t.Fatalf("资源保存 = %v, %v", saved, err)
			}
			got, err := os.ReadFile(output)
			if err != nil || !bytes.Equal(got, picture.Bytes()) {
				t.Fatalf("资源保存未保留原始内容: %v", err)
			}
			if _, err := desktop.saveMediaWithPicker(t.Context(), opened.PagePrefix, 0, 200, func(string) (string, error) { return input, nil }); !errors.Is(err, errOverwriteInput) {
				t.Fatalf("输入文件保护 = %v", err)
			}
			if saved, err := desktop.saveMediaWithPicker(t.Context(), opened.PagePrefix, 0, 200, func(string) (string, error) { return "", nil }); err != nil || saved {
				t.Fatalf("取消保存 = %v, %v", saved, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if saved, err := desktop.saveMediaWithPicker(ctx, opened.PagePrefix, 0, 200, func(string) (string, error) { cancel(); return output, nil }); !errors.Is(err, context.Canceled) || saved {
				t.Fatalf("取消请求 = %v, %v", saved, err)
			}
			if saved, err := desktop.saveMediaWithPicker(t.Context(), opened.PagePrefix, 0, 200, func(string) (string, error) {
				_, err := service.Open(input, readersession.Options{})
				return output, err
			}); saved || !errors.Is(err, readersession.ErrStaleDocument) {
				t.Fatalf("保存前会话复核 = %v, %v", saved, err)
			}
		})
	}
}
