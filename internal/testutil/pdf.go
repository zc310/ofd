// Package testutil 提供跨包测试共享的辅助构造函数。
package testutil

import (
	"bytes"
	"fmt"
)

// MinimalPDF 返回一个最小单页 PDF，MediaBox 为 [0 0 width height]，内容流为 content。
func MinimalPDF(content []byte, width, height int) []byte {
	return MinimalPDFWithPage(content, fmt.Sprintf("[0 0 %d %d]", width, height), 1)
}

// MinimalPDFWithPage 返回一个最小单页 PDF，可指定 MediaBox 与 UserUnit。
func MinimalPDFWithPage(content []byte, mediaBox string, userUnit int) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox %s /UserUnit %d /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>", mediaBox, userUnit),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return output.Bytes()
}
