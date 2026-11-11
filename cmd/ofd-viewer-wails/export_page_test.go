package main

import (
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/reader"
)

// 单页用例统一使用单元素选择集，避免重复初始化页码切片。
func (d *Desktop) exportPageWithPicker(ctx context.Context, prefix string, index int, format string, dpi float64, background string, choose func(string) (string, error)) (bool, error) {
	return d.exportPagesWithPicker(ctx, prefix, []int{index}, format, dpi, background, choose)
}

func TestExportPageFormatsAndInputProtection(t *testing.T) {
	desktop, opened, input := desktopWithAttachment(t, true)
	for _, format := range []string{"png", "jpg", "svg", "pdf", "txt"} {
		t.Run(format, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "page."+format)
			saved, err := desktop.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, format, 30, "#00000000", func(name string) (string, error) {
				if !strings.HasSuffix(name, "-page-1."+format) {
					t.Errorf("默认名称 = %q", name)
				}
				return output, nil
			})
			if err != nil || !saved {
				t.Fatalf("导出 = %v, %v", saved, err)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			switch format {
			case "svg":
				if !strings.Contains(string(data), "<svg") {
					t.Fatal("没有 SVG 输出")
				}
			case "pdf":
				if !strings.HasPrefix(string(data), "%PDF-") {
					t.Fatal("没有 PDF 输出")
				}
			case "txt":
				if len(data) != 0 {
					t.Fatalf("空页不应输出占位文字: %q", data)
				}
			default:
				file, err := os.Open(output)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				_, decoded, err := image.DecodeConfig(file)
				want := format
				if want == "jpg" {
					want = "jpeg"
				}
				if err != nil || decoded != want {
					t.Fatalf("图片格式 = %s, %v", decoded, err)
				}
			}
		})
	}
	if _, err := desktop.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, "png", 30, "#ffffffff", func(string) (string, error) { return input, nil }); !errors.Is(err, errOverwriteInput) {
		t.Fatalf("输入保护 = %v", err)
	}
}

func TestExportPagePermissionCancellationAndStaleSession(t *testing.T) {
	denied, opened, _ := desktopWithAttachment(t, false)
	if _, err := denied.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, "png", 30, "#ffffffff", func(string) (string, error) { t.Error("禁止导出时显示了对话框"); return "", nil }); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("权限拒绝 = %v", err)
	}
	desktop, opened, input := desktopWithAttachment(t, true)
	if saved, err := desktop.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, "png", 30, "#ffffffff", func(string) (string, error) { return "", nil }); err != nil || saved {
		t.Fatalf("取消 = %v, %v", saved, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	output := filepath.Join(t.TempDir(), "no-output.png")
	if _, err := desktop.exportPageWithPicker(ctx, opened.PagePrefix, 0, "png", 30, "#ffffffff", func(string) (string, error) { cancel(); return output, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消请求 = %v", err)
	}
	if _, err := desktop.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, "png", 30, "#ffffffff", func(string) (string, error) {
		_, err := desktop.document.Open(input, readersession.Options{})
		return output, err
	}); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatalf("过期会话 = %v", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("取消或旧会话产生了输出: %v", err)
	}
}

func TestExportPageRejectsUnsupportedOptionsAndOversizeRaster(t *testing.T) {
	desktop, opened, _ := desktopWithAttachment(t, true)
	choose := func(string) (string, error) { t.Error("非法参数打开了对话框"); return "", nil }
	for _, format := range []string{"exe", "zip", ""} {
		if _, err := desktop.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, format, 30, "#ffffffff", choose); !errors.Is(err, errPageExportOptions) {
			t.Fatalf("格式参数 = %v", err)
		}
	}
	if _, err := desktop.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, "png", 601, "#ffffffff", choose); !errors.Is(err, errPageExportOptions) {
		t.Fatalf("DPI 参数 = %v", err)
	}
	if _, err := desktop.exportPageWithPicker(t.Context(), opened.PagePrefix, 0, "png", 600, "#ffffffff", choose); !errors.Is(err, errPageExportLimit) {
		t.Fatalf("像素上限 = %v", err)
	}
}
