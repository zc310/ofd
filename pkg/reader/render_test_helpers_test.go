package reader

import (
	"bytes"
	"context"
)

func renderPageBytes(ctx context.Context, r *Reader, index int, options RenderOptions) ([]byte, error) {
	var output bytes.Buffer
	if err := r.RenderPage(ctx, &output, index, options); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func renderPageWithReportBytes(ctx context.Context, r *Reader, index int, options RenderOptions) ([]byte, RenderReport, error) {
	var output bytes.Buffer
	report, err := r.RenderPageWithReport(ctx, &output, index, options)
	if err != nil {
		return nil, report, err
	}
	return output.Bytes(), report, nil
}

func renderPDFBytes(ctx context.Context, r *Reader, indices []int, options RenderOptions) ([]byte, error) {
	var output bytes.Buffer
	if err := r.RenderPDF(ctx, &output, indices, options); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func renderPagesBytes(ctx context.Context, r *Reader, indices []int, options RenderOptions) ([][]byte, error) {
	pages := make([][]byte, 0, len(indices))
	err := r.RenderPages(ctx, indices, options, func(_ context.Context, _, _ int, data []byte) error {
		pages = append(pages, bytes.Clone(data))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pages, nil
}
