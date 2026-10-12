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
	// PDF 写出器逐字体选择子集化；多 FD CFF 保留该字体面的完整程序。
	pdfDoc, err := render.NewPDFDocument(output, render.PDFOptions{Compress: true, SubsetFonts: true})
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
	return r.addPDFOutlines(pdfDoc, indices)
}

// addPDFOutlines 把文档大纲写入导出的 PDF。
//
// 大纲是文档级的，与页面的选取顺序无关，但仍需按本次导出的页建立页序映射：
// indices 之外的页不在输出里，指向它们的条目会被丢弃。indices 可以乱序也可以
// 重复，因此页序取该页在导出序列中的位置。
func (r *Reader) addPDFOutlines(pdfDoc render.PDFDocument, indices []int) error {
	outlines := make([]render.PageOutline, 0, 16)
	for _, document := range r.distinctDocuments(indices) {
		resolver := r.pdfOutlineResolver(indices, document)
		outlines = append(outlines, document.Outlines(resolver)...)
	}
	if len(outlines) == 0 {
		return nil
	}
	return pdfDoc.AddOutlines(outlines)
}

// pdfOutlineResolver 为单个文档体建立页序解析器。
func (r *Reader) pdfOutlineResolver(indices []int, document *render.Document) render.PageLinkResolver {
	targets := make(map[models.StID]render.LinkTargetRef)
	for position, pageIndex := range indices {
		if pageIndex < 0 || pageIndex >= len(r.pages) {
			continue
		}
		ref := r.pages[pageIndex]
		if ref.document != document || ref.page == nil {
			continue
		}
		if _, exists := targets[ref.page.ID]; exists {
			continue
		}
		targets[ref.page.ID] = render.LinkTargetRef{Index: position, Height: r.pdfPageHeight(ref)}
	}
	return func(pageID models.StID) (render.LinkTargetRef, bool) {
		target, ok := targets[pageID]
		return target, ok
	}
}

// distinctDocuments 按首次出现顺序返回 indices 涉及的文档体。
func (r *Reader) distinctDocuments(indices []int) []*render.Document {
	documents := make([]*render.Document, 0, 4)
	seen := make(map[*render.Document]bool, 4)
	for _, pageIndex := range indices {
		if pageIndex < 0 || pageIndex >= len(r.pages) {
			continue
		}
		document := r.pages[pageIndex].document
		if document == nil || seen[document] {
			continue
		}
		seen[document] = true
		documents = append(documents, document)
	}
	return documents
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
	if err == nil && box.Width > 0 && box.Height > 0 {
		return box.Height
	}
	// 页面自身没有定义有效尺寸时回退到文档公共 PageArea，再回退 A4。少了这一步，
	// 跳转坐标会以 0 参与从页顶原点到页底原点的换算，落点会上下颠倒。
	area := models.CtPageArea{}
	if ref.document != nil && ref.document.Document != nil {
		area = ref.document.CommonData.PageArea
	}
	area.EnsurePhysicalBox()
	return area.PhysicalBox.Height
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
