package converter

import (
	"fmt"
	"image"
	"image/color"
	"io"

	"github.com/hhrutter/tiff"
	"github.com/zc310/ofd/internal/render"
)

func init() { Register(&tiffEncoder{}) }

type tiffEncoder struct{}

func (e *tiffEncoder) Name() string         { return "tiff" }
func (e *tiffEncoder) Kind() Kind           { return KindImage }
func (e *tiffEncoder) Extensions() []string { return []string{".tif", ".tiff"} }
func (e *tiffEncoder) MIME() string         { return "image/tiff" }

func (e *tiffEncoder) Encode(input any, output io.Writer, conv *Converter) (err error) {
	if output == nil {
		if conv.fileWriter == nil {
			return fmt.Errorf("未设置 TIFF 输出参数")
		}
		writer, err := conv.fileWriter(1)
		if err != nil {
			return fmt.Errorf("创建 TIFF 输出失败: %w", err)
		}
		defer func() {
			if closeErr := writer.Close(); err == nil {
				err = closeErr
			}
		}()
		output = writer
	}
	conv.format = "tiff"
	return encodeOFD(input, output, conv, encodeTIFFDocuments)
}

func encodeTIFFDocuments(documents []*render.Document, output io.Writer, conv *Converter) error {
	pages := collectDocumentPages(documents)
	if len(pages) == 0 {
		return fmt.Errorf("文档没有页面")
	}
	start, end, err := pageRange(len(pages), conv.page)
	if err != nil {
		return err
	}
	images := make([]image.Image, 0, end-start)
	for _, page := range pages[start:end] {
		if err := conv.checkCancelled(); err != nil {
			return err
		}
		if conv.useRasterBackend() {
			img, err := page.document.RasterizePage(page.document.Pages[page.pageIndex], conv.rasterBackend, conv.dpi)
			if err != nil {
				return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
			}
			background := conv.bgColor
			_, _, _, alpha := background.RGBA()
			if alpha == 0 {
				background = color.White
			}
			images = append(images, opaqueImage(img, background))
			continue
		}
		surface, err := page.document.Page(page.document.Pages[page.pageIndex])
		if err != nil {
			return fmt.Errorf("处理第%d页失败: %w", page.pageNumber, err)
		}
		background := conv.bgColor
		_, _, _, alpha := background.RGBA()
		if alpha == 0 {
			background = color.White
		}
		images = append(images, opaqueImage(surface.Rasterize(conv.dpi), background))
	}
	if err := conv.checkCancelled(); err != nil {
		return err
	}
	if err := tiff.EncodeAll(output, images, &tiff.Options{Compression: tiff.Deflate, Predictor: true}); err != nil {
		return fmt.Errorf("编码多页 TIFF 失败: %w", err)
	}
	return nil
}
