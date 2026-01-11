package main

import (
	"fmt"
	"image"
	"log/slog"

	_ "image/gif"
	_ "image/jpeg"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nao1215/imaging"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/converter"
)

func main() {
	slog.Info(fmt.Sprintf("OFD thumbnailer started: %s\n", strings.Join(os.Args, " ")))

	// 设置超时
	timeout := time.After(15 * time.Second)
	done := make(chan error, 1)

	go func() {
		done <- realMain()
	}()

	select {
	case err := <-done:
		if err != nil {
			slog.Error(fmt.Sprintf("Error: %v\n", err))
			os.Exit(1)
		}
	case <-timeout:
		slog.Error(fmt.Sprintf("Error: timeout after 15 seconds\n"))
		os.Exit(1)
	}
}

func realMain() error {
	if len(os.Args) != 4 {
		return fmt.Errorf("usage: %s <input> <output> <size>", os.Args[0])
	}

	inputFile := os.Args[1]
	outputFile := os.Args[2]
	size, _ := strconv.Atoi(os.Args[3])

	// 验证输入文件
	if !strings.HasSuffix(strings.ToLower(inputFile), ".ofd") {
		return fmt.Errorf("not an OFD file")
	}

	return generateThumbnail(inputFile, outputFile, size)
}

// generateThumbnail 生成缩略图
func generateThumbnail(input, output string, size int) error {
	if size <= 0 {
		size = 128
	}
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
		converter.DPI(72))
}
