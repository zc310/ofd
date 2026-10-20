// Command ofd-thumbnailer 从 OFD 首页或 ZIP 包中的图片生成 PNG 缩略图。
package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kovidgoyal/imaging"
	"github.com/zc310/ofd/internal/media"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/converter"
)

const (
	timeoutDuration  = 60 * time.Second
	maxThumbnailSize = 4096
	maxRasterPixels  = maxThumbnailSize * maxThumbnailSize
)

var (
	ErrInvalidArgs    = errors.New("invalid arguments")
	ErrNoSupportedImg = errors.New("no supported image files found")
	ErrTimeout        = errors.New("timeout after 60 seconds")
)

func main() {
	if err := runWithTimeout(); err != nil {
		slog.Error("failed to generate thumbnail", "error", err)
		os.Exit(1)
	}
}

func runWithTimeout() error {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- realMain(ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ErrTimeout
	}
}

func realMain(ctx context.Context) error {
	return run(ctx, os.Args[1:])
}

func run(ctx context.Context, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) != 3 {
		return fmt.Errorf("%w: usage: %s <input> <output> <size>",
			ErrInvalidArgs, filepath.Base(os.Args[0]))
	}

	inputFile := args[0]
	outputFile := args[1]

	size, err := parseSize(args[2])
	if err != nil {
		return fmt.Errorf("%w: invalid size: %w", ErrInvalidArgs, err)
	}
	if utils.SamePath(inputFile, outputFile) {
		return fmt.Errorf("%w: output must not overwrite input", ErrInvalidArgs)
	}

	if isOFDFile(inputFile) {
		return generateOFDThumbnail(ctx, inputFile, outputFile, size)
	}

	return generateZIPThumbnail(ctx, inputFile, outputFile, size)
}

func parseSize(sizeStr string) (int, error) {
	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		return 0, err
	}
	if size <= 0 || size > maxThumbnailSize {
		return 0, fmt.Errorf("size must be between 1 and %d pixels", maxThumbnailSize)
	}
	return size, nil
}

func isOFDFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".ofd"
}

func generateZIPThumbnail(ctx context.Context, inputFile, outputFile string, size int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	firstImage, err := media.ExtractFirstImage(inputFile)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoSupportedImg, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	resizedImg := resizeImage(firstImage, size)
	return saveThumbnail(ctx, inputFile, outputFile, resizedImg)
}

func resizeImage(img image.Image, size int) image.Image {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	if width > height {
		return imaging.Resize(img, size, 0, imaging.Lanczos)
	}
	return imaging.Resize(img, 0, size, imaging.Lanczos)
}

func generateOFDThumbnail(ctx context.Context, input, output string, size int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ofd, err := parser.NewOFD(input)
	if ofd != nil {
		defer ofd.Close()
	}
	if err != nil {
		return fmt.Errorf("open OFD: %w", err)
	}
	for _, document := range ofd.Documents {
		if len(document.Pages) == 0 {
			continue
		}
		box, err := document.Pages[0].PhysicalBox()
		if err != nil {
			return fmt.Errorf("read first page dimensions: %w", err)
		}
		resolution, err := thumbnailResolution(box.Width, box.Height, size)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		view := render.NewDocumentWithDPI(color.Transparent, document, resolution)
		defer view.Close()
		return converter.ImageDocument(ctx, view,
			converter.Thumbnail(size),
			converter.ImageWriter(func(page int, img image.Image) error {
				return saveThumbnail(ctx, input, output, img)
			}),
			converter.Page(1),
			converter.DPI(resolution.DPI()))
	}
	return errors.New("OFD has no pages")
}

// thumbnailResolution 在栅格化之前限定长边和总像素，避免先分配整页大图。
func thumbnailResolution(width, height float64, size int) (geom.Resolution, error) {
	if size <= 0 || size > maxThumbnailSize {
		return 0, fmt.Errorf("size must be between 1 and %d pixels", maxThumbnailSize)
	}
	if width <= 0 || height <= 0 || math.IsNaN(width) || math.IsNaN(height) || math.IsInf(width, 0) || math.IsInf(height, 0) {
		return 0, errors.New("invalid OFD page dimensions")
	}
	dpmm := float64(size) / math.Max(width, height)
	if dpmm <= 0 || math.IsInf(dpmm, 0) {
		return 0, errors.New("OFD page dimensions cannot be rasterized safely")
	}
	pixelWidth := math.Floor(width*dpmm + 0.5)
	pixelHeight := math.Floor(height*dpmm + 0.5)
	if pixelWidth < 1 || pixelHeight < 1 || pixelWidth > float64(size) || pixelHeight > float64(size) || pixelWidth*pixelHeight > maxRasterPixels {
		return 0, errors.New("OFD page dimensions cannot be rasterized within the thumbnail pixel budget")
	}
	return geom.DPMM(dpmm), nil
}

// saveThumbnail 不依赖输出扩展名，并在编码成功且未取消时发布完整 PNG。
func saveThumbnail(ctx context.Context, input, output string, img image.Image) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if utils.SamePath(input, output) {
		return fmt.Errorf("%w: output must not overwrite input", ErrInvalidArgs)
	}
	file, err := os.CreateTemp(filepath.Dir(output), ".ofd-thumbnail-*.png")
	if err != nil {
		return fmt.Errorf("create thumbnail: %w", err)
	}
	defer os.Remove(file.Name())
	encodeErr := png.Encode(contextWriter{ctx: ctx, writer: file}, img)
	closeErr := file.Close()
	if encodeErr != nil {
		return fmt.Errorf("encode PNG: %w", encodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close thumbnail: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if utils.SamePath(input, output) {
		return fmt.Errorf("%w: output must not overwrite input", ErrInvalidArgs)
	}
	if err := os.Rename(file.Name(), output); err != nil {
		return fmt.Errorf("publish thumbnail: %w", err)
	}
	return nil
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w contextWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(data)
}
