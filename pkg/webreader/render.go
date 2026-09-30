package webreader

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"strings"

	"github.com/zc310/ofd/internal/media"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/utils"
	// 注册 canvas 栅格后端与离屏表面工厂，以及系统回退字体工厂。
	// RenderPage 依赖这些注册，不能因为拆分就丢掉这个空导入。
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
)

// RenderFormat 是页面输出格式。
type RenderFormat string

// RenderOptions 控制页面输出。DPI 控制 PNG、JPG 以及 PDF 中复杂效果的内部栅格化分辨率；
// PDF 页面主体保留文字和矢量内容，SVG 复杂渐变等仍可能包含栅格回退。
type RenderOptions struct {
	DPI        float64
	Background color.Color
	Format     RenderFormat
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
func (r *Reader) RenderPage(index int, options RenderOptions) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	return r.renderPage(index, options)
}

// RenderPages 将多个页面按传入顺序渲染为 PNG、JPG 或 SVG 数据。
// 所有页面共享一次 Reader 锁和同一份文档状态；渲染本身仍按顺序执行。
func (r *Reader) RenderPages(indices []int, options RenderOptions) ([][]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
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
		data, err := r.renderPage(index, options)
		if err != nil {
			return nil, err
		}
		results = append(results, data)
	}
	return results, nil
}

func (r *Reader) hasUnsafeFallbackFontSubset() bool {
	for _, family := range r.fallbackFamilies {
		data, ok := render.FallbackFontData(family)
		if !ok || len(data) < 4 {
			continue
		}
		signature := string(data[:4])
		if signature == "OTTO" || signature == "ttcf" {
			return true
		}
	}
	return false
}

// renderPage 将页面渲染为 PNG、JPG 或 SVG；调用方必须持有 Reader 读锁。
func (r *Reader) renderPage(index int, options RenderOptions) ([]byte, error) {
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

	ref := r.pages[index]
	lease, err := ref.page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
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
	page, err := document.Page(ref.page)
	if err != nil {
		return nil, fmt.Errorf("渲染第 %d 页失败: %w", index, err)
	}

	estimatedSize := int(pixels * 4)
	if estimatedSize < 1024 {
		estimatedSize = 1024
	}
	output := bytes.NewBuffer(make([]byte, 0, estimatedSize))
	if format == RenderSVG {
		if err := page.Write(output, "svg"); err != nil {
			return nil, fmt.Errorf("编码第 %d 页 SVG 失败: %w", index, err)
		}
		return output.Bytes(), nil
	}
	var rendered image.Image = page.Rasterize(geom.DPI(options.DPI))
	if format == RenderJPG {
		rendered = opaqueImage(rendered, color.White)
		if err := jpeg.Encode(output, rendered, &jpeg.Options{Quality: 90}); err != nil {
			return nil, fmt.Errorf("编码第 %d 页 JPG 失败: %w", index, err)
		}
		return output.Bytes(), nil
	}
	if err := media.EncodePNGLevel(output, rendered, 7); err != nil {
		return nil, fmt.Errorf("编码第 %d 页 PNG 失败: %w", index, err)
	}
	return output.Bytes(), nil
}

func opaqueImage(source image.Image, background color.Color) image.Image {
	if rgba, ok := source.(*image.RGBA); ok {
		draw.Draw(rgba, rgba.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
		return rgba
	}
	bounds := source.Bounds()
	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, &image.Uniform{C: background}, image.Point{}, draw.Src)
	draw.Draw(result, bounds, source, bounds.Min, draw.Over)
	return result
}

// pageDocument 获取页面对应的渲染文档；调用方必须持有 Reader 读锁。
func (r *Reader) pageDocument(ref pageRef, background color.Color, dpi geom.Resolution) (*render.Document, error) {
	document := ref.document
	if document == nil || document.Document == nil {
		return nil, errors.New("页面渲染上下文为空")
	}
	red, green, blue, alpha := background.RGBA()
	key := renderDocumentKey{base: document, dpi: dpi.DPI(), red: red, green: green, blue: blue, alpha: alpha}
	r.renderDocsMu.Lock()
	defer r.renderDocsMu.Unlock()
	if r.renderDocs != nil {
		if cached, ok := r.renderDocs.Get(key); ok && cached != nil {
			return cached, nil
		}
	}
	document = render.NewDocumentWithDPI(background, ref.document.Document, dpi)
	for _, family := range r.fallbackFamilies {
		// 字体已在打开/注册时全局登记，这里只把已锁定字体族应用到该文档。
		_ = document.UseFallbackFont(family)
	}
	if r.renderDocs == nil {
		r.renderDocs = utils.NewLRU[renderDocumentKey, *render.Document](maxRenderDocs, nil)
	}
	r.renderDocs.Add(key, document)
	return document, nil
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
	r.pages = nil
	r.text = nil
	r.search = nil
	r.fallbackFamily = ""
	r.fallbackFamilies = nil
	r.renderDocsMu.Lock()
	r.renderDocs = nil
	r.renderDocsMu.Unlock()
	ofd := r.ofd
	r.ofd = nil
	if ofd == nil {
		return nil
	}
	return ofd.Close()
}
