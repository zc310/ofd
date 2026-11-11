package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strings"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/reader"
)

var errPageExportOptions = errors.New("页面导出参数无效，仅支持 PNG/JPG/SVG/PDF/TXT、1-600 DPI 和 RGBA 背景色")
var errPageExportLimit = errors.New("页面导出超过 2500 万像素或 64 MiB 限制")
var errPageExportSelection = errors.New("请选择 1-200 个不重复的页面")

type pageExportBuffer struct{ data bytes.Buffer }

func (b *pageExportBuffer) Write(data []byte) (int, error) {
	if len(data) > (64<<20)-b.data.Len() {
		return 0, errPageExportLimit
	}
	return b.data.Write(data)
}

// ExportPages 按指定页序导出页面，保存路径由原生对话框取得。
func (d *Desktop) ExportPages(ctx context.Context, prefix string, indices []int, format string, dpi float64, background string) (bool, error) {
	return d.exportPagesWithPicker(ctx, prefix, indices, format, dpi, background, d.savePicker(ctx, "导出所选页面"))
}

func (d *Desktop) exportPagesWithPicker(ctx context.Context, prefix string, indices []int, format string, dpi float64, background string, choose func(string) (string, error)) (bool, error) {
	if len(indices) == 0 || len(indices) > 200 {
		return false, errPageExportSelection
	}
	seen := make(map[int]bool, len(indices))
	for _, index := range indices {
		if index < 0 || seen[index] {
			return false, errPageExportSelection
		}
		seen[index] = true
	}
	indices = append([]int(nil), indices...)
	index := indices[0]
	if (format != "png" && format != "jpg" && format != "svg" && format != "pdf" && format != "txt") || math.IsNaN(dpi) || math.IsInf(dpi, 0) || dpi < 1 || dpi > 600 || len(background) != 9 || background[0] != '#' {
		return false, errPageExportOptions
	}
	rgba, err := hex.DecodeString(background[1:])
	if err != nil {
		return false, errPageExportOptions
	}
	d.openMu.Lock()
	defer d.openMu.Unlock()
	if prefix != d.inputPrefix {
		return false, readersession.ErrStaleDocument
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := d.document.CheckPermission(prefix, reader.PermissionExport, indices); err != nil {
		return false, err
	}
	if format == "txt" {
		if err := d.document.CheckPermission(prefix, reader.PermissionCopy, indices); err != nil {
			return false, err
		}
	}
	// 保存对话框前检查每一页，不能只按首张图片推断后续页面大小。
	if format == "png" || format == "jpg" {
		for _, pageIndex := range indices {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			page, err := d.document.Page(prefix, pageIndex)
			if err != nil {
				return false, err
			}
			pixels := math.Ceil(page.Width*dpi/25.4) * math.Ceil(page.Height*dpi/25.4)
			if math.IsNaN(pixels) || math.IsInf(pixels, 0) || pixels <= 0 || pixels > 25000000 {
				return false, errPageExportLimit
			}
		}
	}
	base := strings.TrimSuffix(filepath.Base(d.inputPath), filepath.Ext(d.inputPath))
	name := fmt.Sprintf("%s-page-%d.%s", safeAttachmentFilename(base), index+1, format)
	if len(indices) > 1 {
		ext := format
		if format != "pdf" && format != "txt" {
			ext = "zip"
		}
		name = fmt.Sprintf("%s-selected-%d-pages.%s", safeAttachmentFilename(base), len(indices), ext)
	}
	path, err := choose(name)
	defer releaseSaveTarget(path)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if path == "" {
		return false, nil
	}
	if err := d.checkSaveTarget(path); err != nil {
		return false, err
	}
	// 对话框可能停留很久，开始生成前再次检查全部所选页。
	if err := d.document.CheckPermission(prefix, reader.PermissionExport, indices); err != nil {
		return false, err
	}
	var output pageExportBuffer
	options := reader.RenderOptions{Format: reader.RenderFormat(format), DPI: dpi, Purpose: reader.PermissionExport,
		Background: color.RGBA{R: rgba[0], G: rgba[1], B: rgba[2], A: rgba[3]}, PNGCompressionLevel: 1}
	if format == "jpg" && rgba[3] == 0 {
		options.Background = color.White
	}
	if err := d.renderExportPages(ctx, prefix, &output, indices, options); err != nil {
		return false, err
	}
	if err := d.document.CheckPermission(prefix, reader.PermissionExport, indices); err != nil {
		return false, err
	}
	if format == "txt" {
		if err := d.document.CheckPermission(prefix, reader.PermissionCopy, indices); err != nil {
			return false, err
		}
	}
	data := output.data.Bytes()
	if err := d.writeAttachment(ctx, path, data); err != nil {
		return false, err
	}
	return true, nil
}

// 只生成内容，不操作文件；失败或取消的部分输出始终由保存流程丢弃。
func (d *Desktop) renderExportPages(ctx context.Context, prefix string, output *pageExportBuffer, indices []int, options reader.RenderOptions) error {
	switch options.Format {
	case "pdf":
		return d.document.RenderPDF(ctx, prefix, output, indices, options)
	case "txt":
		for position, pageIndex := range indices {
			if err := ctx.Err(); err != nil {
				return err
			}
			runs, err := d.document.Text(prefix, pageIndex)
			if err != nil {
				return err
			}
			var text pageExportBuffer
			if err := writePageText(ctx, &text, runs); err != nil {
				return err
			}
			if position > 0 {
				if _, err := output.Write([]byte("\n\f\n")); err != nil {
					return err
				}
			}
			if _, err := output.Write(bytes.TrimSpace(text.data.Bytes())); err != nil {
				return err
			}
		}
		return nil
	case reader.RenderPNG, reader.RenderJPG, reader.RenderSVG:
		if len(indices) == 1 {
			return d.document.RenderPage(ctx, prefix, output, indices[0], options)
		}
		return writePageArchive(ctx, output, indices, string(options.Format), func(writer io.Writer, pageIndex int) error {
			return d.document.RenderPage(ctx, prefix, writer, pageIndex, options)
		})
	default:
		return errPageExportOptions
	}
}

// 页面直接写入有界 ZIP，不为每张图片再分配完整缓冲。Store 使 64 MiB
// 归档上限同时约束图片内容总量，避免高压缩 SVG 绕过内存与解包大小限制。
func writePageArchive(ctx context.Context, output io.Writer, indices []int, format string, render func(io.Writer, int) error) error {
	archive := zip.NewWriter(output)
	for position, index := range indices {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := archive.CreateHeader(&zip.FileHeader{
			Name: fmt.Sprintf("%03d-page-%d.%s", position+1, index+1, format), Method: zip.Store,
		})
		if err != nil {
			return err
		}
		if err := render(entry, index); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return archive.Close()
}

// 按几何位置合并同一行，与共享界面的当前页文字顺序一致；不推断表格或多栏结构。
func writePageText(ctx context.Context, output *pageExportBuffer, runs []reader.TextRun) error {
	ordered := append([]reader.TextRun(nil), runs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Y != ordered[j].Y {
			return ordered[i].Y < ordered[j].Y
		}
		return ordered[i].X < ordered[j].X
	})
	var y, height float64
	hasText := false
	for _, run := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		if run.Text == "" {
			continue
		}
		runHeight := run.Height
		if runHeight == 0 {
			runHeight = 1
		}
		previousHeight := height
		if previousHeight == 0 {
			previousHeight = runHeight
		}
		tolerance := math.Max(1, math.Min(runHeight, previousHeight)*0.5)
		if hasText && math.Abs(run.Y-y) <= tolerance {
			y = (y + run.Y) / 2
			height = math.Max(height, run.Height)
		} else {
			if hasText {
				if _, err := output.Write([]byte("\n")); err != nil {
					return err
				}
			}
			y, height = run.Y, run.Height
		}
		if _, err := output.Write([]byte(run.Text)); err != nil {
			return err
		}
		hasText = true
	}
	return nil
}
