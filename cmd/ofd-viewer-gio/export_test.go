package main

import (
	"archive/zip"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/zc310/ofd/pkg/creator"
)

func TestExportFormatsAndSourceProtection(t *testing.T) {
	dir := t.TempDir()
	data, err := creator.Marshal(creator.Document{ID: "export", PageSize: creator.A4, Pages: []creator.Page{{}, {}}})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source.ofd")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range exportFormats {
		t.Run(format, func(t *testing.T) {
			target := filepath.Join(dir, "out."+exportExtension(format, 2))
			if err := exportFile(context.Background(), source, target, exportOptions{format: format, dpi: 150}); err != nil {
				t.Fatal(err)
			}
			if format == "png" || format == "jpg" || format == "svg" {
				z, err := zip.OpenReader(target)
				if err != nil {
					t.Fatal(err)
				}
				defer z.Close()
				if len(z.File) != 2 {
					t.Fatalf("导出页数 = %d", len(z.File))
				}
			}
		})
	}
	if err := exportFile(context.Background(), source, source, exportOptions{format: "pdf", dpi: 150}); err == nil {
		t.Fatal("不应覆盖源文件")
	}
	link := filepath.Join(dir, "hardlink.ofd")
	if err := os.Link(source, link); err != nil {
		t.Fatal(err)
	}
	if err := exportFile(context.Background(), source, link, exportOptions{format: "pdf", dpi: 150}); err == nil {
		t.Fatal("不应覆盖源文件的硬链接")
	}
	target := filepath.Join(dir, "existing.pdf")
	if err := os.WriteFile(target, []byte("保留"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := exportFile(context.Background(), source, target, exportOptions{format: "invalid", dpi: 150}); err == nil {
		t.Fatal("不应接受未知格式")
	}
	if existing, err := os.ReadFile(target); err != nil || string(existing) != "保留" {
		t.Fatal("失败时不应破坏目标文件")
	}
	got, err := os.ReadFile(source)
	if err != nil || string(got) != string(data) {
		t.Fatal("源文件被修改")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := exportFile(ctx, source, filepath.Join(dir, "cancel.pdf"), exportOptions{format: "pdf", dpi: 150}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误: %v", err)
	}
}

func TestExportDialogLayoutAtDifferentSizes(t *testing.T) {
	window, err := headless.NewWindow(500, 600)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var v viewer
	v.menu.open, v.menu.exportOpen = true, true
	v.menu.exportFormat.Value, v.menu.exportBackground.Value = "png", "white"
	v.menu.exportDPIInput.SetText("150")
	var router input.Router
	var ops op.Ops
	for _, size := range []image.Point{image.Pt(500, 600), image.Pt(300, 180), image.Pt(180, 120)} {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(size), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
		v.layoutMenuOverlay(gtx, continuousTestTheme())
		router.Frame(&ops)
		if err := window.Frame(&ops); err != nil {
			t.Fatalf("窗口 %v 导出对话框绘制失败: %v", size, err)
		}
	}
}

func TestExportDialogOptionsAndPNGBackground(t *testing.T) {
	var menu viewerMenu
	menu.exportFormat.Value = "png"
	menu.exportBackground.Value = "white"
	menu.exportDPIInput.SetText("72")
	opts, err := menu.exportOptions()
	if err != nil || opts.dpi != 72 || !opts.white {
		t.Fatalf("导出选项: %+v %v", opts, err)
	}
	menu.exportDPIInput.SetText("0")
	if _, err := menu.exportOptions(); err == nil {
		t.Fatal("无效 DPI 应被拒绝")
	}
	menu.exportFormat.Value = "pdf"
	if _, err := menu.exportOptions(); err != nil {
		t.Fatalf("PDF 不应受图片 DPI 输入影响: %v", err)
	}

	dir := t.TempDir()
	data, err := creator.Marshal(creator.Document{ID: "background", PageSize: creator.A4, Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source.ofd")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, white := range []bool{false, true} {
		path := filepath.Join(dir, "page.png")
		if err := exportFile(context.Background(), source, path, exportOptions{format: "png", dpi: 72, white: white}); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(file)
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if w := img.Bounds().Dx(); w < 590 || w > 600 {
			t.Fatalf("72 DPI 图片宽度 = %d", w)
		}
		_, _, _, alpha := img.At(0, 0).RGBA()
		if (alpha == 0xffff) != white {
			t.Fatalf("white=%v 时 alpha=%d", white, alpha)
		}
	}
}
