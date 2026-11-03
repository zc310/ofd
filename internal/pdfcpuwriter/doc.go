// Package pdfcpuwriter 提供实现 canvas.Renderer 的实验性多页 PDF 写入器。
//
// 本包不是完整 PDF 编码器。使用 pdfcpu 构建标签时，canvas 后端将其注册为 PDF 写入器。
// pdfcpu 负责对象、流和交叉引用写出；
// 几何、整形文字和字体子集仍使用 OFD 内置 canvas 及已有 font 依赖。
// 不支持的绘制能力通过 Err 或 Write 返回 ErrUnsupported，不静默降级。
// 不支持并发调用；文档和字体资源必须保持有效，直到 Write 返回。
package pdfcpuwriter
