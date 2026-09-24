package converter

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"

	"github.com/kovidgoyal/imaging"
	wpng "github.com/woozymasta/png"
	"github.com/zc310/ofd/internal/render"
)

// useRasterBackend 判断当前配置是否应把位图输出交给可替换的栅格后端：
// 仅当显式设置了 RasterBackend 且输出为 PNG/JPEG 时启用；矢量格式
// （SVG/EPS/TeX）仍走 VectorSurface。
func (c *Converter) useRasterBackend() bool {
	if c.rasterBackend == "" {
		return false
	}
	switch c.format {
	case "png", "jpeg", "jpg":
		return true
	default:
		return false
	}
}

// renderPage 渲染单个页面（默认路径：canvas 矢量表面光栅化）。
func (c *Converter) renderPage(pageNumber int, page render.VectorSurface) error {
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

		switch c.format {
		case "png":
			if err := encodePNG(w, page.Rasterize(c.dpi)); err != nil {
				return fmt.Errorf("写入第%d页失败: %w", pageNumber, err)
			}
		case "jpeg", "jpg":
			img := opaqueImage(page.Rasterize(c.dpi), color.White)
			if err := jpeg.Encode(w, img, &jpeg.Options{Quality: 90}); err != nil {
				return fmt.Errorf("写入第%d页失败: %w", pageNumber, err)
			}
		default:
			if err := page.Write(w, c.format); err != nil {
				return fmt.Errorf("写入第%d页失败: %w", pageNumber, err)
			}
		}
	}

	if c.imageWriter != nil {
		var img image.Image = page.Rasterize(c.dpi)
		if c.thumbnail > 0 {
			img = c.resizeThumbnail(img)
		}
		if err := c.imageWriter(pageNumber, img); err != nil {
			return fmt.Errorf("写入第%d页图像失败: %w", pageNumber, err)
		}
	}

	return nil
}

// renderRasterPage 用已由栅格后端光栅化的页面图像完成写出。
func (c *Converter) renderRasterPage(pageNumber int, img image.Image) error {
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

		switch c.format {
		case "png":
			if err := encodePNG(w, img); err != nil {
				return fmt.Errorf("写入第%d页失败: %w", pageNumber, err)
			}
		default: // jpeg / jpg
			opaque := opaqueImage(img, color.White)
			if err := jpeg.Encode(w, opaque, &jpeg.Options{Quality: 90}); err != nil {
				return fmt.Errorf("写入第%d页失败: %w", pageNumber, err)
			}
		}
	}

	if c.imageWriter != nil {
		out := img
		if c.thumbnail > 0 {
			out = c.resizeThumbnail(out)
		}
		if err := c.imageWriter(pageNumber, out); err != nil {
			return fmt.Errorf("写入第%d页图像失败: %w", pageNumber, err)
		}
	}

	return nil
}

// encodePNG 使用 woozymasta/png（klauspost zlib 实现）写出 PNG；level 7
// 压缩下输出体积与标准库默认压缩相当但速度更快。
func encodePNG(w io.Writer, img image.Image) error {
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
		// PNG/JPEG 指定了栅格后端时直接经 RasterizePage 取图，跳过 canvas
		// 矢量表面的创建与光栅化。
		if c.useRasterBackend() {
			img, err := page.document.RasterizePage(page.document.Pages[page.pageIndex], c.rasterBackend, c.dpi)
			if err != nil {
				return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
			}
			if err := c.renderRasterPage(page.pageNumber, img); err != nil {
				return err
			}
			continue
		}

		surface, err := page.document.Page(page.document.Pages[page.pageIndex])
		if err != nil {
			return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
		}
		if err := c.renderPage(page.pageNumber, surface); err != nil {
			return err
		}
	}
	return nil
}
