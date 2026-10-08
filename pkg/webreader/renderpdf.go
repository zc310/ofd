package webreader

import (
	"bytes"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// RenderPDF 将多个页面按传入顺序写入一个保留文字和矢量内容的 PDF 文档。
// 该接口会把完整 PDF 保存在内存中，需要导出大量页面时应使用 RenderPDFTo。
func (r *Reader) RenderPDF(indices []int, options RenderOptions) (outputBytes []byte, err error) {
	var output bytes.Buffer
	err = r.RenderPDFTo(&output, indices, options)
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// RenderPDFTo 将多个页面按传入顺序写入 output，保留文字和矢量内容。
// output 会在 PDF 生成过程中接收数据，适合流式保存大 PDF，因此不限制页数。
//
// 注意 options.DPI 在这里不是图像分辨率：PDF 不做栅格化，该值只用于把页面的
// 毫米尺寸换算成 PDF 点，因此 72 才能保持物理尺寸（A4 输出为 595.3 x 841.9 pt），
// 传入更高的值会把页面等比放大。导出 PDF 时不应把它暴露给用户。
func (r *Reader) RenderPDFTo(output io.Writer, indices []int, options RenderOptions) (err error) {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	if output == nil {
		return errors.New("PDF 输出为空")
	}
	if len(indices) == 0 {
		return errors.New("PDF 页面列表为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	background := options.Background
	if background == nil {
		background = color.Transparent
	}
	dpi := options.DPI
	if dpi == 0 {
		dpi = defaultDPI
	}
	if dpi < 1 || dpi > maxDPI || math.IsNaN(dpi) || math.IsInf(dpi, 0) {
		return fmt.Errorf("DPI 必须在 1 到 %d 之间", maxDPI)
	}
	// TrueType 字体应进行子集化，避免将完整中文字体写入每个 PDF。
	// CFF/TTC 回退字体暂时不能交给 canvas 的 CFF 子集器；对这类字体
	// 关闭子集化仍会保留 ToUnicode 和原生文字对象，避免 Close 时 panic。
	pdfDoc, err := render.NewPDFDocument(output, render.PDFOptions{Compress: true, SubsetFonts: !r.hasUnsafeFallbackFontSubset()})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := pdfDoc.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("关闭 PDF 文档失败: %w", closeErr)
		}
	}()
	pageLinks := r.pdfPageLinks(indices)
	for position, index := range indices {
		page, pageErr := r.pdfPage(index, background, geom.DPI(dpi))
		if pageErr != nil {
			return fmt.Errorf("处理 PDF 第 %d 页失败: %w", position+1, pageErr)
		}
		resolution := geom.DPI(dpi)
		width := page.Width() * resolution.DPMM()
		height := page.Height() * resolution.DPMM()
		if math.IsNaN(width) || math.IsInf(width, 0) || math.IsNaN(height) || math.IsInf(height, 0) || width*height > maxRenderPixels {
			return fmt.Errorf("第 %d 页 PDF 渲染尺寸过大", position+1)
		}
		if addErr := pdfDoc.AddPage(page, pageLinks[position]); addErr != nil {
			return fmt.Errorf("处理 PDF 第 %d 页失败: %w", position+1, addErr)
		}
	}
	return nil
}

// pdfPageLinks 返回导出页的链接热区，供 PDF 生成可点击区域。热区尺寸是页面物理
// 尺寸，与导出 DPI 无关，因此复用采集时的毫米值即可。
//
// resolve 把跳转目标解析到 indices 里的输出位置，因此只把本次导出的页登记进去：
// indices 之外的页不在输出里，指向它们的链接无法解析。
func (r *Reader) pdfPageLinks(indices []int) [][]drawing.PageLink {
	links := make([][]drawing.PageLink, len(indices))
	if len(indices) == 0 {
		return links
	}
	// 页 ID 只在文档体内唯一，因此解析器按文档体分别建立。
	targets := make(map[*render.Document]map[models.StID]render.LinkTargetRef)
	for index, pageIndex := range indices {
		if pageIndex < 0 || pageIndex >= len(r.pages) {
			continue
		}
		ref := r.pages[pageIndex]
		if ref.page == nil || ref.document == nil {
			continue
		}
		byPage, ok := targets[ref.document]
		if !ok {
			byPage = make(map[models.StID]render.LinkTargetRef)
			targets[ref.document] = byPage
		}
		if _, exists := byPage[ref.page.ID]; exists {
			continue
		}
		byPage[ref.page.ID] = render.LinkTargetRef{Index: index, Height: r.pdfPageHeight(ref)}
	}
	resolvers := make(map[*render.Document]render.PageLinkResolver, len(targets))
	for document, byPage := range targets {
		resolvers[document] = func(pageID models.StID) (render.LinkTargetRef, bool) {
			target, ok := byPage[pageID]
			return target, ok
		}
	}
	for index, pageIndex := range indices {
		if pageIndex < 0 || pageIndex >= len(r.pages) {
			continue
		}
		ref := r.pages[pageIndex]
		if ref.page == nil || ref.document == nil {
			continue
		}
		links[index] = ref.document.PageLinks(ref.page, resolvers[ref.document])
	}
	return links
}

// pdfPageHeight 读取页面物理高度；只读页面 Area，不加载页面内容或资源。
func (r *Reader) pdfPageHeight(ref pageRef) float64 {
	box, err := ref.page.PhysicalBoxMetadata()
	if err != nil || !box.IsFinite() || box.Height <= 0 {
		return 0
	}
	return box.Height
}

// pdfPage 获取 PDF 渲染所需的页面画布；调用方必须持有 Reader 读锁。
func (r *Reader) pdfPage(index int, background color.Color, dpi geom.Resolution) (render.VectorSurface, error) {
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	ref := r.pages[index]
	lease, err := ref.page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil {
		return nil, errors.New("页面内容为空")
	}
	document, err := r.pageDocument(ref, background, dpi)
	if err != nil {
		return nil, err
	}
	return document.Page(ref.page)
}
