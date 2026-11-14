//go:build !js

package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/pkg/converter"
)

const exportPageBudget = 128 << 20

type exportWriter struct{ io.Writer }

func (exportWriter) Close() error { return nil }

// 导出会话独立打开源文件，不与阅读区并发访问解析器。
func exportFile(ctx context.Context, source, target string, opts exportOptions) error {
	return exportFileInput(ctx, source, target, opts, source)
}

func exportFileInput(ctx context.Context, source, target string, opts exportOptions, input any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	valid := false
	for _, f := range exportFormats {
		if f == opts.format {
			valid = true
			break
		}
	}
	if !valid {
		return viewerError("不支持的导出格式")
	}
	if opts.dpi < 1 || opts.dpi > 1200 {
		return viewerError("DPI 必须在 1 到 1200 之间")
	}
	src, err := os.Stat(source)
	if err != nil {
		return err
	}
	if dst, statErr := os.Stat(target); statErr == nil {
		if os.SameFile(src, dst) {
			return viewerError("不能覆盖源 OFD 文件")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	m, err := openDocumentInput(source, input) //nolint:contextcheck // 现有文档打开接口不接收 context。
	if err != nil {
		return err
	}
	defer m.close()
	if opts.format == "png" || opts.format == "jpg" {
		for _, page := range m.pages {
			if err = ctx.Err(); err != nil {
				return err
			}
			meta := readPageMetadata(page)
			if meta.err != nil {
				return meta.err
			}
			w, h := meta.size.width*float64(opts.dpi)/25.4, meta.size.height*float64(opts.dpi)/25.4
			if math.Ceil(w)*math.Ceil(h)*4 > exportPageBudget {
				return viewerError("页面尺寸超过所选 DPI 的导出内存限制，请降低 DPI")
			}
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".ofd-export-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	err = exportDocuments(ctx, m, tmp, opts)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

func exportDocuments(ctx context.Context, m *documentModel, output io.Writer, opts exportOptions) error {
	documents := m.documents
	if opts.white && opts.format != "txt" {
		documents = make([]*render.Document, 0, len(m.documents))
		for _, doc := range m.documents {
			documents = append(documents, render.NewDocument(color.White, doc.Document, 0))
		}
		defer func() {
			for _, doc := range documents {
				doc.Close()
			}
		}()
	}
	switch opts.format {
	case "pdf":
		return converter.PDF(ctx, documents, output)
	case "txt":
		docs := make([]*parser.Document, 0, len(m.documents))
		for _, doc := range m.documents {
			docs = append(docs, doc.Document)
		}
		return converter.Text(ctx, docs, output)
	}
	var archive *zip.Writer
	if len(m.pages) > 1 {
		archive = zip.NewWriter(output)
	}
	err := converter.Image(ctx, documents, converter.WithFormat(opts.format), converter.DPI(float64(opts.dpi)), converter.Writer(func(page int) (io.WriteCloser, error) {
		if archive == nil {
			return exportWriter{output}, nil
		}
		entry, err := archive.Create(fmt.Sprintf("page-%04d.%s", page, opts.format))
		if err != nil {
			return nil, err
		}
		return exportWriter{entry}, nil
	}))
	if archive != nil {
		if closeErr := archive.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}
