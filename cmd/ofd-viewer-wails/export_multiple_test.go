package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
	"golang.org/x/image/font/gofont/goregular"
)

func TestMultiPageExportKeepsSelectionOrderAndEmptyPageBoundary(t *testing.T) {
	pages := make([]creator.Page, 0, 3)
	for _, text := range []string{"First", "Second", ""} {
		if text == "" {
			pages = append(pages, creator.Page{})
			continue
		}
		pages = append(pages, creator.Page{Items: []creator.Item{creator.Text{X: 10, Y: 10, Width: 100, Height: 10, Font: "Go", Size: 4, Value: text}}})
	}
	data, err := creator.Marshal(creator.Document{ID: "multiple-export", Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}}, Pages: pages})
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
		output := filepath.Join(t.TempDir(), "selected."+format)
		saved, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, []int{1, 0, 2}, format, 150, "#ffffffff", func(string) (string, error) { return output, nil })
		if err != nil || !saved {
			t.Fatalf("%s 多页导出 = %v, %v", format, saved, err)
		}
		if format == "pdf" {
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			count, err := api.PageCount(t.Context(), bytes.NewReader(data), model.NewStatelessConfiguration())
			if err != nil || count != 3 {
				t.Fatalf("PDF 页数 = %d, %v", count, err)
			}
		} else {
			data, err := os.ReadFile(output)
			if err != nil || string(data) != "Second\n\f\nFirst\n\f\n" {
				t.Fatalf("TXT 页序或空页边界 = %q, %v", data, err)
			}
		}
	}
	for _, indices := range [][]int{nil, {0, 0}, {-1}, make([]int, 201)} {
		if _, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, indices, "pdf", 150, "#ffffffff", func(string) (string, error) { t.Error("非法页码不应显示对话框"); return "", nil }); !errors.Is(err, errPageExportSelection) {
			t.Fatalf("非法选择 %v = %v", indices, err)
		}
	}
}

func TestMultiPageExportRejectsAnyForbiddenScopeBeforePicker(t *testing.T) {
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, content := range map[string]string{
		"OFD.xml":        `<OFD Version="1.0"><DocBody><DocRoot>A/Document.xml</DocRoot></DocBody><DocBody><DocRoot>B/Document.xml</DocRoot></DocBody></OFD>`,
		"A/Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"B/Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages><Permissions><Export>false</Export></Permissions></Document>`,
		"A/Page.xml":     `<Page><Content><Layer ID="2"/></Content></Page>`,
		"B/Page.xml":     `<Page><Content><Layer ID="2"/></Content></Page>`,
	} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "mixed.ofd")
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
	for _, format := range []string{"pdf", "txt", "png", "jpg", "svg"} {
		if _, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, []int{0, 1}, format, 150, "#ffffffff", func(string) (string, error) { t.Error("混合权限页面不应显示对话框"); return "", nil }); !errors.Is(err, reader.ErrPermissionDenied) {
			t.Fatalf("混合权限 %s = %v", format, err)
		}
		if _, err := desktop.exportPagesWithPicker(t.Context(), opened.PagePrefix, []int{0}, format, 150, "#ffffffff", func(string) (string, error) { return "", nil }); err != nil {
			t.Fatalf("单独选择允许页应可进入保存: %v", err)
		}
	}
}
