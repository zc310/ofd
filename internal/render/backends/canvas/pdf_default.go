//go:build !pdfcpu

package canvas

// 默认构建继续使用 canvas PDF 写入器。
var newPDFDocument = newCanvasPDFDocument
