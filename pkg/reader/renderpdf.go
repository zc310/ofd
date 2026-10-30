package reader

import (
	"bytes"
	"context"
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

// PDF 页面物理面积的上限，单位为平方点（1 点 = 1/72 英寸），不随输出 DPI 改变。
const maxPDFPageAreaPoints = 50_000_000

// RenderPDF 将多个页面按传入顺序写入一个保留文字和矢量内容的 PDF 文档。
// 该接口会把完整 PDF 保存在内存中，需要导出大量页面时应使用 RenderPDFTo。
func (r *Reader) RenderPDF(ctx context.Context, indices []int, options RenderOptions) (outputBytes []byte, err error) {
	var output bytes.Buffer
	err = r.RenderPDFTo(ctx, &output, indices, options)
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// RenderPDFTo 将多个页面按传入顺序写入 output，保留文字和矢量内容。
// output 会在 PDF 生成过程中接收数据，适合流式保存大 PDF，因此不限制页数。
//
// options.DPI 用于复杂效果的内部离屏栅格化，默认 96；文字和路径保留矢量。
// PDF 页面始终按物理尺寸输出，毫米到 PDF 点的换算固定使用 72 点/英寸，
// 不受 options.DPI 影响（A4 为 595.3 x 841.9 pt）。
//
// 在链接收集、页面绘制及写出之间响应取消。
// 取消时返回 ctx.Err()；output 可能已包含不完整的 PDF，调用方应丢弃。
func (r *Reader) RenderPDFTo(ctx context.Context, output io.Writer, indices []int, options RenderOptions) (err error) {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	if err := ctx.Err(); err != nil {
		return err
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
	// 导出前检查全部页面，避免混合权限文档已经写出部分内容才发现禁止导出。
	if err := r.checkPermissionLocked(PermissionExport, indices); err != nil {
		return err
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
	writer := &renderWriter{ctx: ctx, out: output}
	pdfDoc, err := render.NewPDFDocument(writer, render.PDFOptions{Compress: true, SubsetFonts: true})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := pdfDoc.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("关闭 PDF 文档失败: %w", closeErr)
		}
		if cancelErr := renderCheckpoint(ctx); cancelErr != nil {
			err = cancelErr
		} else if err == nil {
			err = writer.err
		}
	}()
	pageLinks, err := r.pdfPageLinks(ctx, indices)
	if err != nil {
		return err
	}
	for position, index := range indices {
		if pageErr := r.addPDFPage(ctx, pdfDoc, index, background, geom.DPI(dpi), pageLinks[position]); pageErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("处理 PDF 第 %d 页失败: %w", position+1, pageErr)
		}
		if writer.err != nil {
			return writer.err
		}
	}
	if err := renderCheckpoint(ctx); err != nil {
		return err
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
func (r *Reader) pdfPageLinks(ctx context.Context, indices []int) ([][]drawing.PageLink, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	links := make([][]drawing.PageLink, len(indices))
	if len(indices) == 0 {
		return links, nil
	}
	// 页 ID 只在文档体内唯一，因此解析器按文档体分别建立。
	targets := make(map[*render.Document]map[models.StID]render.LinkTargetRef)
	for index, pageIndex := range indices {
		if index%32 == 0 {
			if err := renderCheckpoint(ctx); err != nil {
				return nil, err
			}
		}
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
		if err := renderCheckpoint(ctx); err != nil {
			return nil, err
		}
		if pageIndex < 0 || pageIndex >= len(r.pages) {
			continue
		}
		ref := r.pages[pageIndex]
		if ref.page == nil || ref.document == nil {
			continue
		}
		pageLinks, err := ref.document.PageLinksWithVisibilityE(ref.page, resolvers[ref.document], r.hiddenLayers[pageIndex])
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 页链接失败: %w", pageIndex, err)
		}
		links[index] = pageLinks
	}
	return links, ctx.Err()
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

// addPDFPage 在页面加入 PDF 前持续持有渲染视图；调用方必须持有 Reader 读锁。
func (r *Reader) addPDFPage(ctx context.Context, pdfDoc render.PDFDocument, index int, background color.Color, dpi geom.Resolution, links []drawing.PageLink) error {
	if err := renderCheckpoint(ctx); err != nil {
		return err
	}
	if index < 0 || index >= len(r.pages) {
		return fmt.Errorf("页面索引超出范围: %d", index)
	}
	ref := r.pages[index]
	lease, err := ref.page.AcquireLease()
	if err != nil {
		return err
	}
	defer lease.Release()
	if err := renderCheckpoint(ctx); err != nil {
		return err
	}
	content := lease.Content()
	if content == nil {
		return errors.New("页面内容为空")
	}
	// 在绘制前按物理面积检查页面；实际离屏栅格化由渲染层单独限制像素预算。
	box := content.Area.PhysicalBox
	pointsPerMM := geom.DPI(72).DPMM()
	width, height := box.Width*pointsPerMM, box.Height*pointsPerMM
	if !validPDFDimensions(width, height) {
		return errors.New("PDF 渲染尺寸过大")
	}
	document, err := r.pageDocument(ref, background, dpi)
	if err != nil {
		return err
	}
	defer r.releasePageDocument(document)
	page, err := document.PageWithVisibility(ref.page, r.hiddenLayers[index])
	if err != nil {
		return err
	}
	if err := renderCheckpoint(ctx); err != nil {
		return err
	}
	return pdfDoc.AddPage(page, links)
}

func validPDFDimensions(width, height float64) bool {
	return finitePositive(width) && finitePositive(height) &&
		width <= maxPDFPageAreaPoints && height <= maxPDFPageAreaPoints &&
		width <= maxPDFPageAreaPoints/height
}
