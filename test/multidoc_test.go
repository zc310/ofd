package test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
)

func TestMultiDocumentConvertersUseGlobalPages(t *testing.T) {
	input := "testdata/multi_demo.ofd"

	var text bytes.Buffer
	if err := converter.Text(input, &text); err != nil {
		t.Fatal(err)
	}
	markers := []string{
		"XX 信息化建设项目立项报告",
		"附表：项目经费预算明细表",
		"增值税电子普通发票",
	}
	positions := make([]int, len(markers))
	for i, marker := range markers {
		positions[i] = strings.Index(text.String(), marker)
		if positions[i] < 0 {
			t.Fatalf("text output does not contain %q", marker)
		}
	}
	if positions[1] < positions[0] || positions[2] < positions[1] {
		t.Fatalf("document bodies are not ordered in text output: %q", text.String())
	}

	var selectedText bytes.Buffer
	if err := converter.Text(input, &selectedText, converter.Page(3)); err != nil {
		t.Fatal(err)
	}
	if got := selectedText.String(); !strings.Contains(got, markers[1]) || strings.Contains(got, markers[0]) || strings.Contains(got, markers[2]) {
		t.Fatalf("global page 3 text = %q", got)
	}

	var pdf bytes.Buffer
	if err := converter.PDF(input, &pdf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pdf.String(), "/Count 4") {
		t.Fatalf("PDF does not contain four pages")
	}
	var selectedPDF bytes.Buffer
	if err := converter.PDF(input, &selectedPDF, converter.Page(3)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(selectedPDF.String(), "/Count 1") {
		t.Fatalf("selected PDF does not contain one page")
	}

	var pages []int
	if err := converter.Image(input,
		converter.DPI(10),
		converter.PNG(),
		converter.Writer(func(page int) (io.WriteCloser, error) {
			pages = append(pages, page)
			return discardWriteCloser{}, nil
		}),
	); err != nil {
		t.Fatal(err)
	}
	if got, want := pages, []int{1, 2, 3, 4}; !equalPageNumbers(got, want) {
		t.Fatalf("image page numbers = %v, want %v", got, want)
	}

	pages = nil
	if err := converter.Image(input,
		converter.DPI(10),
		converter.PNG(),
		converter.Page(3),
		converter.Writer(func(page int) (io.WriteCloser, error) {
			pages = append(pages, page)
			return discardWriteCloser{}, nil
		}),
	); err != nil {
		t.Fatal(err)
	}
	if got, want := pages, []int{3}; !equalPageNumbers(got, want) {
		t.Fatalf("selected image page numbers = %v, want %v", got, want)
	}
}

type discardWriteCloser struct{}

func (discardWriteCloser) Write(data []byte) (int, error) {
	return len(data), nil
}

func (discardWriteCloser) Close() error { return nil }

func equalPageNumbers(left, right []int) bool {
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
