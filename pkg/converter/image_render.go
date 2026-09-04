package converter

import (
	"errors"
	"fmt"
	"image"
	"log/slog"

	"github.com/nao1215/imaging"
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers"
	"github.com/tdewolff/canvas/renderers/rasterizer"
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

		var renderer canvas.Writer
		switch c.format {
		case "jpeg":
			renderer = renderers.JPEG(c.dpi)
		case "svg":
			renderer = renderers.SVG()
		case "eps":
			renderer = renderers.EPS()
		case "tex":
			renderer = renderers.TeX()
		default:
			renderer = renderers.PNG(c.dpi)
		}

		if err := page.Write(w, renderer); err != nil {
			return fmt.Errorf("写入第%d页失败: %w", pageNumber, err)
		}
	}

	// 图像写入器处理
	if c.imageWriter != nil {
		var img image.Image
		img = rasterizer.Draw(page, c.dpi, canvas.DefaultColorSpace)

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
