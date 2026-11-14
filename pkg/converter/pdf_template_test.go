//go:build !pdfcpu

package converter

import (
	"bytes"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// 千页样本同时保护模板复用及逐页加粗页码的紧凑输出。
func TestPDF1000PagesKeepsTemplateAndBoldPageNumbersCompact(t *testing.T) {
	input := filepath.Join("..", "..", "testdata", "ofd-creator", "1000-pages.ofd")
	if _, err := os.Stat(input); errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少样本：%s", input)
	} else if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := PDF(t.Context(), input, &output, BgColor(color.White)); err != nil {
		t.Fatal(err)
	}
	// 留出字体子集及元数据变化的余量；轮廓展开的回归会超过 7 MiB。
	if output.Len() > 2<<20 {
		t.Fatalf("千页 PDF 过大：%d 字节", output.Len())
	}
	ctx, err := api.ReadContext(t.Context(), bytes.NewReader(output.Bytes()), model.NewDefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if err := api.ValidateContext(t.Context(), ctx); err != nil {
		t.Fatal(err)
	}
	forms := 0
	for _, entry := range ctx.Table {
		if entry == nil {
			continue
		}
		stream, _, err := ctx.DereferenceStreamDict(entry.Object)
		if err == nil && stream != nil {
			if subtype := stream.NameEntry("Subtype"); subtype != nil && *subtype == "Form" {
				forms++
			}
		}
	}
	if ctx.PageCount != 1000 || forms != 1 {
		t.Fatalf("页数=%d，模板 Form 数=%d，期望 1000 页共用一个模板", ctx.PageCount, forms)
	}
}
