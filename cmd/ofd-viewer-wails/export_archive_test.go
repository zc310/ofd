package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
)

func desktopWithExportPages(t *testing.T, pages []creator.Page) (*Desktop, *openedDocument, string) {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "archive-export", Pages: pages})
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
	return desktop, opened, input
}

func TestMultiPageImagesSaveZIPInSelectedOrder(t *testing.T) {
	desktop, opened, _ := desktopWithExportPages(t, []creator.Page{
		{Area: &creator.PageArea{PhysicalBox: &creator.Box{Width: 25.4, Height: 25.4}}},
		{Area: &creator.PageArea{PhysicalBox: &creator.Box{Width: 50.8, Height: 25.4}}},
	})
	for _, format := range []string{"png", "jpg", "svg"} {
		t.Run(format, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "selected.zip")
			saved, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, []int{1, 0}, format, 30, "#00000000", func(name string) (string, error) {
				if name != "input-selected-2-pages.zip" {
					t.Errorf("ZIP 默认名称 = %q", name)
				}
				return output, nil
			})
			if err != nil || !saved {
				t.Fatalf("多页 %s = %v, %v", format, saved, err)
			}
			archive, err := zip.OpenReader(output)
			if err != nil {
				t.Fatal(err)
			}
			defer archive.Close()
			if len(archive.File) != 2 {
				t.Fatalf("条目数 = %d", len(archive.File))
			}
			for position, index := range []int{1, 0} {
				entry := archive.File[position]
				if entry.Name != fmt.Sprintf("%03d-page-%d.%s", position+1, index+1, format) || entry.Method != zip.Store {
					t.Fatalf("条目名称或存储方式 = %+v", entry.FileHeader)
				}
				file, err := entry.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(file)
				_ = file.Close()
				if err != nil {
					t.Fatal(err)
				}
				if format == "svg" {
					if !bytes.Contains(data, []byte("<svg")) {
						t.Fatal("ZIP 内不是 SVG")
					}
					continue
				}
				decoded, kind, err := image.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				wantKind := format
				if format == "jpg" {
					wantKind = "jpeg"
				}
				if kind != wantKind || decoded.Bounds().Dx() != (index+1)*30 || decoded.Bounds().Dy() != 30 {
					t.Fatalf("图片格式或页序/尺寸 = %s, %v", kind, decoded.Bounds())
				}
				red, green, blue, alpha := decoded.At(0, 0).RGBA()
				if format == "jpg" && (red != 65535 || green != 65535 || blue != 65535 || alpha != 65535) {
					t.Fatal("JPG 透明背景没有转白色")
				}
				if format == "png" && alpha != 0 {
					t.Fatal("PNG 丢失透明背景")
				}
			}
		})
	}
}

func TestMultiPageRasterChecksLaterPageSizeBeforePicker(t *testing.T) {
	desktop, opened, _ := desktopWithExportPages(t, []creator.Page{
		{Area: &creator.PageArea{PhysicalBox: &creator.Box{Width: 25.4, Height: 25.4}}}, {},
	})
	for _, format := range []string{"png", "jpg"} {
		if _, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, []int{0, 1}, format, 600, "#ffffffff", func(string) (string, error) {
			t.Error("后续页面过大时不应显示保存对话框")
			return "", nil
		}); !errors.Is(err, errPageExportLimit) {
			t.Fatalf("%s 后续页面大小检查 = %v", format, err)
		}
	}
}

func TestMultiPageImageCancellationStaleSessionAndInputProtection(t *testing.T) {
	for _, format := range []string{"png", "jpg", "svg"} {
		t.Run(format, func(t *testing.T) {
			desktop, opened, input := desktopWithExportPages(t, []creator.Page{{}, {}})
			original, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range []struct {
				name string
				link func(string, string) error
			}{
				{"input", nil}, {"symbolic", os.Symlink}, {"hard", os.Link},
			} {
				path := input
				if target.link != nil {
					path = filepath.Join(t.TempDir(), target.name+".zip")
					if err := target.link(input, path); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, []int{1, 0}, format, 30, "#ffffffff", func(string) (string, error) { return path, nil }); !errors.Is(err, errOverwriteInput) {
					t.Fatalf("%s 输入保护 = %v", target.name, err)
				}
			}
			if saved, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, []int{1, 0}, format, 30, "#ffffffff", func(string) (string, error) { return "", nil }); err != nil || saved {
				t.Fatalf("取消对话框 = %v, %v", saved, err)
			}
			output := filepath.Join(t.TempDir(), "existing.zip")
			if err := os.WriteFile(output, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if _, err := desktop.exportPagesWithPicker(ctx, opened.PagePrefix, []int{1, 0}, format, 30, "#ffffffff", func(string) (string, error) { cancel(); return output, nil }); !errors.Is(err, context.Canceled) {
				t.Fatalf("取消请求 = %v", err)
			}
			if _, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, []int{1, 0}, format, 30, "#ffffffff", func(string) (string, error) {
				_, err := desktop.document.Open(input, readersession.Options{})
				return output, err
			}); !errors.Is(err, readersession.ErrStaleDocument) {
				t.Fatalf("旧会话 = %v", err)
			}
			if data, err := os.ReadFile(output); err != nil || string(data) != "keep" {
				t.Fatalf("失败操作覆盖了现有输出: %q, %v", data, err)
			}
			if data, err := os.ReadFile(input); err != nil || !bytes.Equal(data, original) {
				t.Fatalf("输入被修改: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(output))
			if err != nil || len(entries) != 1 {
				t.Fatalf("失败后残留临时文件: %v, %v", entries, err)
			}
		})
	}
}

func TestPageArchiveStopsOnCancellationAndRenderFailure(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("页面渲染失败")} {
		var output pageExportBuffer
		calls := 0
		err := writePageArchive(t.Context(), &output, []int{1, 0}, "png", func(writer io.Writer, _ int) error {
			calls++
			_, _ = io.WriteString(writer, "partial")
			return failure
		})
		if !errors.Is(err, failure) || calls != 1 {
			t.Fatalf("归档应停止生成后续页: %v, %d", err, calls)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	err := writePageArchive(ctx, io.Discard, []int{1, 0}, "svg", func(io.Writer, int) error { calls++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("页间取消 = %v, %d", err, calls)
	}
}

func TestLaterPageFailureDoesNotCommitPartialZIP(t *testing.T) {
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, content := range map[string]string{
		"OFD.xml":          `<OFD Version="1.0"><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc/Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 20 20</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="First.xml"/><Page ID="2" BaseLoc="Broken.xml"/></Pages></Document>`,
		"Doc/First.xml":    `<Page><Content><Layer ID="1"/></Content></Page>`,
		"Doc/Broken.xml":   `<Page><Content><Layer ID="2">`,
	} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "broken.ofd")
	if err := os.WriteFile(input, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &readersession.Service{}
	t.Cleanup(func() { _ = service.Close() })
	desktop := &Desktop{document: service}
	opened, err := desktop.openPath(input)
	if err != nil {
		t.Fatal(err)
	}
	// 先确认第一页可生成内容，错误确实发生在后续页，而非输入打开阶段。
	var first pageExportBuffer
	if err := desktop.document.RenderPage(t.Context(), opened.PagePrefix, &first, 0, reader.RenderOptions{Format: reader.RenderSVG, Purpose: reader.PermissionExport}); err != nil || first.data.Len() == 0 {
		t.Fatalf("第一页无法生成: %v", err)
	}
	output := filepath.Join(t.TempDir(), "existing.zip")
	if err := os.WriteFile(output, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if saved, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, []int{0, 1}, "svg", 30, "#ffffffff", func(string) (string, error) { return output, nil }); err == nil || saved {
		t.Fatalf("损坏的后续页不应提交 ZIP: %v, %v", saved, err)
	}
	if data, err := os.ReadFile(output); err != nil || string(data) != "keep" {
		t.Fatalf("部分 ZIP 覆盖了现有文件: %q, %v", data, err)
	}
}

func TestPageArchiveLimitIncludesZIPDirectory(t *testing.T) {
	// 仅剩 64 字节：页面内容本身可容纳，ZIP 条目和目录必须使操作失败。
	output := pageExportBuffer{data: *bytes.NewBuffer(make([]byte, (64<<20)-64))}
	err := writePageArchive(t.Context(), &output, []int{0, 1}, "svg", func(writer io.Writer, _ int) error {
		_, err := io.WriteString(writer, "<svg/>")
		return err
	})
	if !errors.Is(err, errPageExportLimit) || output.data.Len() > 64<<20 {
		t.Fatalf("ZIP 总输出限制 = %v, %d", err, output.data.Len())
	}
	if _, err := output.Write([]byte(strings.Repeat("x", 65))); !errors.Is(err, errPageExportLimit) {
		t.Fatalf("缓冲超限 = %v", err)
	}
}
