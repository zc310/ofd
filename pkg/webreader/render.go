package webreader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"strings"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/media"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	// RenderPage 依赖 canvas 后端注册的矢量表面与栅格后端。
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
)

// RenderFormat 是页面输出格式。
type RenderFormat string

// RenderOptions 控制页面输出。DPI 控制 PNG、JPG 以及 PDF 中复杂效果的内部栅格化分辨率；
// PNGCompressionLevel 为 1 到 9 时指定 PNG 压缩级别，0 使用默认级别 7。
// PDF 页面主体保留文字和矢量内容，SVG 复杂渐变等仍可能包含栅格回退。
type RenderOptions struct {
	DPI                 float64
	Background          color.Color
	Format              RenderFormat
	PNGCompressionLevel int
}

type pageRef struct {
	document  *render.Document
	page      *parser.Page
	fontScope int
}

type renderDocumentKey struct {
	base  *render.Document
	dpi   float64
	red   uint32
	green uint32
	blue  uint32
	alpha uint32
}

// PageCount 返回文档的总页数，页码从 0 开始。
func (r *Reader) PageCount() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return 0
	}
	return len(r.pages)
}

// Pages 返回所有页面的尺寸快照，尺寸单位为毫米。
// 优先读取页面 XML 中的 Area/PhysicalBox，不加载页面内容或资源。
func (r *Reader) Pages() ([]PageInfo, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}

	pages := make([]PageInfo, len(r.pages))
	for index, ref := range r.pages {
		if ref.document == nil || ref.document.Document == nil || ref.page == nil {
			return nil, fmt.Errorf("第 %d 页所属文档为空", index)
		}
		box := ref.document.CommonData.PageArea.PhysicalBox
		pageBox, err := ref.page.PhysicalBoxMetadata()
		if err == nil && finitePositive(pageBox.Width) && finitePositive(pageBox.Height) {
			box = pageBox
		}
		if !finitePositive(box.Width) || !finitePositive(box.Height) {
			box.Width = defaultPageWidth
			box.Height = defaultPageHeight
		}
		pages[index] = PageInfo{Index: index, Width: box.Width, Height: box.Height}
	}
	return pages, nil
}

// Page 返回指定页面的真实尺寸信息。
// 与 Pages 不同，查询单页信息会按需加载该页完整内容。
func (r *Reader) Page(index int) (PageInfo, error) {
	if r == nil {
		return PageInfo{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return PageInfo{}, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return PageInfo{}, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return r.pageInfoLocked(index)
}

func (r *Reader) pageInfoLocked(index int) (PageInfo, error) {
	ref := r.pages[index]
	if ref.page == nil {
		return PageInfo{}, fmt.Errorf("第 %d 页为空", index)
	}
	box, err := ref.page.PhysicalBox()
	if err != nil {
		return PageInfo{}, fmt.Errorf("读取第 %d 页失败: %w", index, err)
	}
	if !finitePositive(box.Width) || !finitePositive(box.Height) {
		return PageInfo{}, fmt.Errorf("第 %d 页尺寸无效", index)
	}
	return PageInfo{Index: index, Width: box.Width, Height: box.Height}, nil
}

// RenderPage 将指定页面渲染为 PNG、JPG 或 SVG 数据。
// 在页面加载、绘制、栅格化及编码的阶段边界响应取消。
// 单次底层绘制或编码不会被强行中断；取消时返回 ctx.Err()。
func (r *Reader) RenderPage(ctx context.Context, index int, options RenderOptions) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	return r.renderPage(ctx, index, options)
}

// RenderPages 将多个页面按传入顺序渲染为 PNG、JPG 或 SVG 数据。
// 所有页面共享一次 Reader 锁和同一份文档状态；渲染本身仍按顺序执行。
// 取消时返回 ctx.Err()，不返回已完成的部分页面。
func (r *Reader) RenderPages(ctx context.Context, indices []int, options RenderOptions) ([][]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(indices) > maxRenderPages {
		return nil, fmt.Errorf("批量渲染页面数量超过限制 %d", maxRenderPages)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	results := make([][]byte, 0, len(indices))
	for _, index := range indices {
		data, err := r.renderPage(ctx, index, options)
		if err != nil {
			return nil, err
		}
		results = append(results, data)
	}
	return results, nil
}

// renderPage 将页面渲染为 PNG、JPG 或 SVG；调用方必须持有 Reader 读锁。
func (r *Reader) renderPage(ctx context.Context, index int, options RenderOptions) ([]byte, error) {
	if err := renderCheckpoint(ctx); err != nil {
		return nil, err
	}
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	if options.DPI == 0 {
		options.DPI = defaultDPI
	}
	if options.DPI < 1 || options.DPI > maxDPI || math.IsNaN(options.DPI) || math.IsInf(options.DPI, 0) {
		return nil, fmt.Errorf("DPI 必须在 1 到 %d 之间", maxDPI)
	}
	format := RenderFormat(strings.ToLower(strings.TrimSpace(string(options.Format))))
	if format == "" {
		format = RenderPNG
	}
	if format != RenderPNG && format != RenderSVG && format != RenderJPG {
		return nil, fmt.Errorf("不支持的页面输出格式: %q", format)
	}
	pngCompressionLevel := options.PNGCompressionLevel
	if pngCompressionLevel == 0 {
		pngCompressionLevel = 7
	}
	if format == RenderPNG && (pngCompressionLevel < 1 || pngCompressionLevel > 9) {
		return nil, fmt.Errorf("PNG 压缩级别必须在 1 到 9 之间")
	}

	ref := r.pages[index]
	lease, err := ref.page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	if err := renderCheckpoint(ctx); err != nil {
		return nil, err
	}
	content := lease.Content()
	if content == nil {
		return nil, fmt.Errorf("页面内容为空")
	}
	box := content.Area.PhysicalBox
	if !finitePositive(box.Width) || !finitePositive(box.Height) {
		return nil, fmt.Errorf("第 %d 页尺寸无效", index)
	}
	pixels := box.Width * options.DPI / 25.4 * box.Height * options.DPI / 25.4
	if math.IsNaN(pixels) || math.IsInf(pixels, 0) || pixels > maxRenderPixels {
		return nil, fmt.Errorf("第 %d 页渲染尺寸过大", index)
	}

	background := options.Background
	if background == nil {
		background = color.Transparent
	}
	// NewDocument 保证页面背景和渲染内容使用同一个文档级渲染上下文。
	document, err := r.pageDocument(ref, background, geom.DPI(options.DPI))
	if err != nil {
		return nil, err
	}
	defer r.releasePageDocument(document)
	page, err := document.PageWithVisibility(ref.page, r.hiddenLayers[index])
	if err != nil {
		return nil, fmt.Errorf("渲染第 %d 页失败: %w", index, err)
	}
	if err := renderCheckpoint(ctx); err != nil {
		return nil, err
	}

	estimatedSize := 1024
	if format != RenderSVG {
		// 编码数据通常远小于 RGBA 像素，不同时预留第二份整页像素大小的缓冲。
		estimatedSize = 64 << 10
	}
	// SVG 按实际内容增长，避免为矢量输出预留整页 RGBA 像素缓冲区。
	output := bytes.NewBuffer(make([]byte, 0, estimatedSize))
	writer := &renderWriter{ctx: ctx, out: output}
	if format == RenderSVG {
		if err := page.Write(writer, "svg"); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("编码第 %d 页 SVG 失败: %w", index, err)
		}
		if err := renderCheckpoint(ctx); err != nil {
			return nil, err
		}
		return output.Bytes(), nil
	}
	var rendered image.Image = page.Rasterize(geom.DPI(options.DPI))
	if err := renderCheckpoint(ctx); err != nil {
		return nil, err
	}
	if format == RenderJPG {
		rendered = opaqueImage(rendered, color.White)
		if err := jpeg.Encode(writer, rendered, &jpeg.Options{Quality: 90}); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("编码第 %d 页 JPG 失败: %w", index, err)
		}
		if err := renderCheckpoint(ctx); err != nil {
			return nil, err
		}
		return output.Bytes(), nil
	}
	if err := media.EncodePNGLevel(writer, rendered, pngCompressionLevel); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("编码第 %d 页 PNG 失败: %w", index, err)
	}
	if err := renderCheckpoint(ctx); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// opaqueImage 把可能带透明通道的页面渲染结果合到不透明背景上。
//
// 背景要垫在内容之下，因此 RGBA 快路径必须按 premultiplied 的
// 「C + B*(1-A)」逐像素原地混合；早先这里误用 draw.Src，把整页像素替换成
// 背景色又不绘制源图，页面内容被整体抹掉，JPG 输出是一张全白空页。
// image.RGBA 按定义是 alpha 预乘的，所以该式成立；非 RGBA 输入走通用分支，
// 由 draw.Over 正确合成。
func opaqueImage(source image.Image, background color.Color) image.Image {
	if rgba, ok := source.(*image.RGBA); ok {
		red, green, blue, _ := background.RGBA()
		// 背景是不透明的，预乘值与直通值一致，取高 8 位即为 8 位分量。
		backR, backG, backB := uint8(red>>8), uint8(green>>8), uint8(blue>>8)
		pix := rgba.Pix
		for i := 0; i+3 < len(pix); i += 4 {
			alpha := pix[i+3]
			if alpha == 0xff {
				continue
			}
			if alpha == 0 {
				pix[i], pix[i+1], pix[i+2] = backR, backG, backB
				pix[i+3] = 0xff
				continue
			}
			inv := 255 - int(alpha)
			pix[i] = addClamped(int(pix[i]), int(backR)*inv/255)
			pix[i+1] = addClamped(int(pix[i+1]), int(backG)*inv/255)
			pix[i+2] = addClamped(int(pix[i+2]), int(backB)*inv/255)
			pix[i+3] = 0xff
		}
		return rgba
	}
	bounds := source.Bounds()
	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, &image.Uniform{C: background}, image.Point{}, draw.Src)
	draw.Draw(result, bounds, source, bounds.Min, draw.Over)
	return result
}

// addClamped 返回 a+b，溢出 255 时收敛到 255。
func addClamped(a, b int) uint8 {
	if sum := a + b; sum > 255 {
		return 255
	} else {
		return uint8(sum)
	}
}

// pageDocument 获取并持有页面对应的渲染视图；调用方必须持有 Reader 读锁，
// 并在绘制和输出完成后调用 releasePageDocument。
func (r *Reader) pageDocument(ref pageRef, background color.Color, dpi geom.Resolution) (*render.Document, error) {
	document := ref.document
	if document == nil || document.Document == nil {
		return nil, errors.New("页面渲染上下文为空")
	}
	red, green, blue, alpha := background.RGBA()
	key := renderDocumentKey{base: document, dpi: dpi.DPI(), red: red, green: green, blue: blue, alpha: alpha}
	r.renderDocsMu.Lock()
	defer r.renderDocsMu.Unlock()
	if r.renderDocUsers == nil {
		r.renderDocUsers = make(map[*render.Document]int)
	}
	if r.renderDocs != nil {
		if cached, ok := r.renderDocs.Get(key); ok && cached != nil {
			r.renderDocUsers[cached]++
			return cached, nil
		}
	}
	var err error
	document, err = ref.document.NewView(background, dpi)
	if err != nil {
		return nil, err
	}
	if r.renderDocs == nil {
		// 所有 Get/Add/Trim 均持 renderDocsMu，检查使用计数与获取视图不会交错。
		// 并发使用的视图暂时允许超容量，最后一次使用完成后重新执行淘汰。
		r.renderDocs = cache.NewWeightedLRU[renderDocumentKey, *render.Document](maxRenderDocs, 0,
			func(_ renderDocumentKey, evicted *render.Document) { evicted.Close() },
			func(_ renderDocumentKey, candidate *render.Document) bool { return r.renderDocUsers[candidate] == 0 },
		)
	}
	r.renderDocUsers[document]++
	r.renderDocs.Add(key, document)
	return document, nil
}

// releasePageDocument 结束一次视图使用；调用方仍须持有 Reader 读锁。
func (r *Reader) releasePageDocument(document *render.Document) {
	if r == nil || document == nil {
		return
	}
	r.renderDocsMu.Lock()
	defer r.renderDocsMu.Unlock()
	users := r.renderDocUsers[document]
	if users <= 0 {
		delete(r.renderDocUsers, document)
		return
	}
	if users == 1 {
		delete(r.renderDocUsers, document)
	} else {
		r.renderDocUsers[document] = users - 1
	}
	if r.renderDocs != nil {
		r.renderDocs.Trim()
	}
}

// clearRenderViewsLocked 释放各 DPI/背景视图，保留基础文档和共享字体。
// 调用方持有 Reader 写锁，此时没有正在使用这些视图的渲染任务。
func (r *Reader) clearRenderViewsLocked() {
	r.renderDocsMu.Lock()
	cached := r.renderDocs
	r.renderDocs = nil
	r.renderDocsMu.Unlock()
	if cached != nil {
		for _, document := range cached.Values() {
			document.Close()
		}
	}
}

// Close 释放文档资源。Close 可以安全地重复调用。
func (r *Reader) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.closeRenderDocumentsLocked()
	r.text = nil
	r.search = nil
	r.metadata = nil
	r.hiddenLayers = nil
	r.fallbackFamily = ""
	r.fallbackFamilies = nil
	ofd := r.ofd
	r.ofd = nil
	if ofd == nil {
		return nil
	}
	return ofd.Close()
}

// closeRenderDocumentsLocked 释放当前视图的渲染资源；调用方持有 Reader 写锁。
func (r *Reader) closeRenderDocumentsLocked() {
	// 基础文档与 DPI/背景视图共同持有共享字体，全部关闭后释放最后一个引用。
	pageDocs := make([]*render.Document, 0, len(r.pages))
	seen := make(map[*render.Document]bool, len(r.pages))
	for _, ref := range r.pages {
		if ref.document != nil && !seen[ref.document] {
			seen[ref.document] = true
			pageDocs = append(pageDocs, ref.document)
		}
	}
	r.pages = nil
	r.resetPageNavigationLocked()
	r.renderDocsMu.Lock()
	cached := r.renderDocs
	r.renderDocs = nil
	r.renderDocsMu.Unlock()
	// 先关闭视图，再关闭各文档体的基础上下文。
	if cached != nil {
		for _, doc := range cached.Values() {
			if !seen[doc] {
				doc.Close()
			}
		}
	}
	for _, doc := range pageDocs {
		doc.Close()
	}
}
