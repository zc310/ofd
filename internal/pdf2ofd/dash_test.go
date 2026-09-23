package pdf2ofd

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

func TestConvertStrokeDashPattern(t *testing.T) {
	// PDF 的 d 是绝对长度（pt）；OFD 的 DashPattern 以线宽为单位，转换时按生效
	// 线宽换算。`[3] 1 d` + `1 w` 应输出 [3 3]，偏移 1。
	content := []byte("q 1 w [3] 1 d 0 10 m 100 10 l S Q")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 1 {
		t.Fatalf("path objects = %d, want 1", len(paths))
	}
	pattern := paths[0].DashPattern
	if pattern == nil || len(*pattern) != 2 {
		t.Fatalf("DashPattern = %v, want two elements", pattern)
	}
	for _, value := range *pattern {
		if value < 2.9 || value > 3.1 {
			t.Fatalf("DashPattern element = %g, want ~3", value)
		}
	}
	if offset := paths[0].DashOffset; offset < 0.9 || offset > 1.1 {
		t.Fatalf("DashOffset = %g, want ~1", offset)
	}
}

func TestConvertEmptyDashPatternClearsDashes(t *testing.T) {
	// `[] 0 d` 恢复实线：不输出 DashPattern。
	content := []byte("q 1 w [3] 0 d 0 10 m 100 10 l S [] 0 d 0 20 m 100 20 l S Q")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + string(content) + "\nendstream",
	}
	pdf := assemblePDF(objects)

	var output bytes.Buffer
	if err := Convert(pdf, &output); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	paths := layerPaths(page.Content().Layer[0])
	if len(paths) != 2 {
		t.Fatalf("path objects = %d, want 2", len(paths))
	}
	if paths[0].DashPattern == nil {
		t.Fatal("first stroke should keep its dash pattern")
	}
	if paths[1].DashPattern != nil {
		t.Fatalf("second stroke after [] 0 d should be solid, got %v", paths[1].DashPattern)
	}
}
