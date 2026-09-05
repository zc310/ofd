package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nao1215/imaging"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/converter"
)

const (
	timeoutDuration = 60 * time.Second
	defaultSize     = 128
	dpi             = 72
)

var (
	ErrInvalidArgs    = errors.New("invalid arguments")
	ErrNoSupportedImg = errors.New("no supported image files found")
	ErrTimeout        = errors.New("timeout after 60 seconds")
)

func main() {
	if err := runWithTimeout(); err != nil {
		slog.Error("Error:", "err", err)
		os.Exit(1)
	}
}

func runWithTimeout() error {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- realMain()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ErrTimeout
	}
}

func realMain() error {
	if len(os.Args) != 4 {
		return fmt.Errorf("%w: usage: %s <input> <output> <size>",
			ErrInvalidArgs, filepath.Base(os.Args[0]))
	}

	inputFile := os.Args[1]
	outputFile := os.Args[2]

	size, err := parseSize(os.Args[3])
	if err != nil {
		return fmt.Errorf("invalid size: %w", err)
	}

	if isOFDFile(inputFile) {
		return generateOFDThumbnail(inputFile, outputFile, size)
	}

	return generateImageThumbnail(inputFile, outputFile, size)
}

func parseSize(sizeStr string) (int, error) {
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size <= 0 {
		return defaultSize, err
	}
	return size, nil
}

func isOFDFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".ofd"
}

func generateImageThumbnail(inputFile, outputFile string, size int) error {
	firstImage, err := utils.ExtractFirstImage(inputFile)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoSupportedImg, err)
	}

	resizedImg := resizeImage(firstImage, size)
	return imaging.Save(resizedImg, outputFile)
}

func resizeImage(img image.Image, size int) image.Image {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	if width > height {
		return imaging.Resize(img, size, 0, imaging.Lanczos)
	}
	return imaging.Resize(img, 0, size, imaging.Lanczos)
}

func generateOFDThumbnail(input, output string, size int) error {
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return err
	}
	defer ofd.Close()

	return converter.Image(input,
		converter.Thumbnail(size),
		converter.ImageWriter(func(page int, img image.Image) error {
			return imaging.Save(img, output)
		}),
		converter.Page(1),
		converter.PNG(),
		converter.DPI(dpi))
}
