package converter

import (
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"

	"github.com/nao1215/imaging"
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers"
	wpng "github.com/woozymasta/png"
	"github.com/zc310/ofd/internal/render"
)

// renderPage 渲染单个页面
func (c *Converter) renderPage(pageNumber int, page *canvas.Canvas) error {
	// 文件写入器处理
	if c.fileWriter != nil {
		w, err := c.fileWriter(pageNumber)
		if err != nil {
			return fmt.Errorf("创建文件写入器失败: %w", err)
		}
		defer func() {
			if err := w.Close(); err != nil {
				slog.Error("关闭文件写入器失败", "error", err)
			}
		}()

		renderer := c.renderer
		if renderer == nil {
			renderer = renderers.PNG(c.dpi)
		}

		// PNG 走本地快速光栅化路径 + woozymasta/png 编码器（klauspost zlib）：
		// 光栅化与 RasterizePage / ImageWriter 共用同一 fast 路径，像素一致；
		// level 7 压缩下输出体积与标准库默认压缩相当，编码更快。
		if c.format == "png" {
			if err := c.writePNG(w, page); err != nil {
				return fmt.Errorf("写入第%d页失败: %w", pageNumber, err)
			}
		} else if err := page.Write(w, renderer); err != nil {
			return fmt.Errorf("写入第%d页失败: %w", pageNumber, err)
		}
	}

	// 图像写入器处理
	if c.imageWriter != nil {
		var img image.Image
		img = render.Rasterize(page, c.dpi, canvas.DefaultColorSpace)

		// 缩略图处理
		if c.thumbnail > 0 {
			img = c.resizeThumbnail(img)
		}

		if err := c.imageWriter(pageNumber, img); err != nil {
			return fmt.Errorf("写入第%d页图像失败: %w", pageNumber, err)
		}
	}

	return nil
}

// writePNG 光栅化页面并写出 PNG。光栅化复用 render.Rasterize（与
// RasterizePage / ImageWriter 同一路径，输出像素一致）；编码使用
// woozymasta/png（klauspost zlib 实现），level 7 下输出体积与标准库
// 默认压缩相当但速度更快。
func (c *Converter) writePNG(w io.Writer, cPage *canvas.Canvas) error {
	img := render.Rasterize(cPage, c.dpi, canvas.DefaultColorSpace)
	encoder := &wpng.Encoder{CompressionLevel: wpng.CompressionLevel(7)}
	return encoder.Encode(w, img)
}

// resizeThumbnail 生成缩略图
func (c *Converter) resizeThumbnail(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width > height {
		return imaging.Resize(img, c.thumbnail, 0, imaging.Lanczos)
	}
	return imaging.Resize(img, 0, c.thumbnail, imaging.Lanczos)
}

// validateConfig 验证配置
func (c *Converter) validateConfig() error {
	if c.fileWriter == nil && c.imageWriter == nil {
		return errors.New("未设置图像输出参数")
	}
	return nil
}

func (c *Converter) renderDocument(doc *render.Document) error {
	return c.renderDocuments([]*render.Document{doc})
}

func (c *Converter) renderDocuments(documents []*render.Document) error {
	pages := collectDocumentPages(documents)
	if len(pages) == 0 {
		return errors.New("文档没有页面")
	}
	pageStart, pageEnd, err := pageRange(len(pages), c.page)
	if err != nil {
		return err
	}
	pages = pages[pageStart:pageEnd]

	for _, page := range pages {
		canvasPage, err := page.document.Page(page.document.Pages[page.pageIndex])
		if err != nil {
			return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
		}

		if err := c.renderPage(page.pageNumber, canvasPage); err != nil {
			return err
		}
	}
	return nil
}
