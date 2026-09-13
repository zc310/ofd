package converter

import (
	"bytes"
	"image/color"
	"io"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func TestCollectDocumentPagesUsesGlobalPageNumbers(t *testing.T) {
	documents := []*render.Document{
		render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}, {}}}),
		render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}}}),
	}

	pages := collectDocumentPages(documents)
	if len(pages) != 3 {
		t.Fatalf("page count = %d, want 3", len(pages))
	}
	if pages[0].pageNumber != 1 || pages[1].pageNumber != 2 || pages[2].pageNumber != 3 {
		t.Fatalf("page numbers = %d, %d, %d; want 1, 2, 3", pages[0].pageNumber, pages[1].pageNumber, pages[2].pageNumber)
	}
	if pages[2].document != documents[1] || pages[2].pageIndex != 0 {
		t.Fatalf("global page 3 does not map to document 2 page 1")
	}
}

func TestWalkDocumentPagesUsesGlobalPageNumbersAndRange(t *testing.T) {
	documents := []*render.Document{
		testRenderDocument(2),
		testRenderDocument(3),
	}
	var pages []documentPage
	if err := walkDocumentPages(documents, 1, 4, func(page documentPage) error {
		pages = append(pages, page)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(pages) != 3 {
		t.Fatalf("walked page count = %d, want 3", len(pages))
	}
	if pages[0].pageNumber != 2 || pages[1].pageNumber != 3 || pages[2].pageNumber != 4 {
		t.Fatalf("walked page numbers = %d, %d, %d; want 2, 3, 4", pages[0].pageNumber, pages[1].pageNumber, pages[2].pageNumber)
	}
	if pages[0].document != documents[0] || pages[0].pageIndex != 1 || pages[2].document != documents[1] || pages[2].pageIndex != 1 {
		t.Fatal("walked pages do not preserve document and page indexes")
	}
}

func TestCountDocumentPagesSkipsNilPages(t *testing.T) {
	documents := []*render.Document{
		render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}, nil, {}}}),
		nil,
	}
	if got, want := countDocumentPages(documents), 2; got != want {
		t.Fatalf("countDocumentPages = %d, want %d", got, want)
	}
}

func TestPageRange(t *testing.T) {
	tests := []struct {
		total, page int
		start, end  int
		wantErr     bool
	}{
		{total: 3, page: 0, start: 0, end: 3},
		{total: 3, page: 2, start: 1, end: 2},
		{total: 3, page: -1, wantErr: true},
		{total: 3, page: 4, wantErr: true},
	}
	for _, test := range tests {
		start, end, err := pageRange(test.total, test.page)
		if test.wantErr {
			if err == nil {
				t.Fatalf("pageRange(%d, %d) returned nil error", test.total, test.page)
			}
			continue
		}
		if err != nil || start != test.start || end != test.end {
			t.Fatalf("pageRange(%d, %d) = (%d, %d, %v), want (%d, %d)", test.total, test.page, start, end, err, test.start, test.end)
		}
	}
}

func TestPDFParallelOption(t *testing.T) {
	if newConverter().pdfParallel {
		t.Fatal("PDF 默认应关闭并行渲染")
	}
	if newConverter(PDFParallel(false)).pdfParallel {
		t.Fatal("PDFParallel(false) 未关闭并行渲染")
	}
	if !newConverter(PDFParallel(false), PDFParallel(true)).pdfParallel {
		t.Fatal("PDFParallel(true) 未重新启用并行渲染")
	}
}

func TestImageDocumentsUseGlobalPageNumbers(t *testing.T) {
	documents := []*render.Document{
		testRenderDocument(2),
		testRenderDocument(1),
	}
	var pages []int
	err := ImageDocuments(documents, PNG(), DPI(1), Writer(func(page int) (io.WriteCloser, error) {
		pages = append(pages, page)
		return testWriteCloser{Writer: io.Discard}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pages, []int{1, 2, 3}; !equalInts(got, want) {
		t.Fatalf("writer page numbers = %v, want %v", got, want)
	}

	pages = nil
	err = ImageDocuments(documents, PNG(), DPI(1), Page(3), Writer(func(page int) (io.WriteCloser, error) {
		pages = append(pages, page)
		return testWriteCloser{Writer: io.Discard}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pages, []int{3}; !equalInts(got, want) {
		t.Fatalf("selected writer page numbers = %v, want %v", got, want)
	}
}

func TestPDFDocumentsUseGlobalPages(t *testing.T) {
	documents := []*render.Document{
		testRenderDocument(2),
		testRenderDocument(1),
	}
	var output bytes.Buffer
	if err := PDFDocuments(documents, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "/Count 3") {
		t.Fatalf("PDF does not contain three pages: %q", output.String())
	}

	output.Reset()
	if err := PDFDocuments(documents, &output, Page(3)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "/Count 1") {
		t.Fatalf("selected PDF does not contain one page: %q", output.String())
	}
}

func TestImageDocumentsRejectInvalidGlobalPage(t *testing.T) {
	documents := []*render.Document{testRenderDocument(1), testRenderDocument(1)}
	for _, page := range []int{-1, 3} {
		err := ImageDocuments(documents, PNG(), Page(page), Writer(func(int) (io.WriteCloser, error) {
			return testWriteCloser{Writer: io.Discard}, nil
		}))
		if err == nil {
			t.Fatalf("ImageDocuments Page(%d) returned nil error", page)
		}
	}
}

func testRenderDocument(pageCount int) *render.Document {
	pages := make([]*parser.Page, pageCount)
	for i := range pages {
		pages[i] = &parser.Page{}
	}
	return render.NewDocument(color.Transparent, &parser.Document{Pages: pages})
}

type testWriteCloser struct {
	io.Writer
}

func (testWriteCloser) Close() error { return nil }

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
