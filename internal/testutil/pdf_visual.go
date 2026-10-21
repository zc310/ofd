package testutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// VisualComparison 保存两组图像的视觉比较结果。
type VisualComparison struct {
	PageCount           int
	Similarity          float64
	PixelMatchRate      float64
	MeanAbsoluteError   float64
	WorstPage           int
	WorstPageSimilarity float64
	WorstPageMatchRate  float64
}

// RasterizePDFPages 使用 pdftoppm 将 PDF 的所有页面栅格化为 RGBA 图像。
func RasterizePDFPages(ctx context.Context, pdftoppm string, pdf []byte, dpi int) ([]*image.RGBA, error) {
	if strings.TrimSpace(pdftoppm) == "" {
		return nil, errors.New("pdftoppm 路径为空")
	}
	if dpi <= 0 {
		return nil, fmt.Errorf("PDF 栅格化 DPI 必须大于 0: %d", dpi)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "ofd-pdf-visual-*")
	if err != nil {
		return nil, fmt.Errorf("创建 PDF 栅格化临时目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	input := filepath.Join(dir, "input.pdf")
	if err := os.WriteFile(input, pdf, 0600); err != nil {
		return nil, fmt.Errorf("写入待栅格化 PDF 失败: %w", err)
	}
	prefix := filepath.Join(dir, "page")
	command := exec.CommandContext(ctx, pdftoppm, "-png", "-r", strconv.Itoa(dpi), input, prefix)
	if output, err := command.CombinedOutput(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("pdftoppm 栅格化失败: %w: %s", err, output)
	}
	paths, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		return nil, fmt.Errorf("搜索栅格化页面失败: %w", err)
	}
	sort.Slice(paths, func(i, j int) bool {
		return pdfPageImageNumber(paths[i]) < pdfPageImageNumber(paths[j])
	})
	if len(paths) == 0 {
		return nil, errors.New("pdftoppm 没有生成页面图片")
	}
	images := make([]*image.RGBA, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取页面图片 %s 失败: %w", path, err)
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("解码页面图片 %s 失败: %w", path, err)
		}
		bounds := decoded.Bounds()
		converted := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		draw.Draw(converted, converted.Bounds(), decoded, bounds.Min, draw.Src)
		images = append(images, converted)
	}
	return images, nil
}

// ComparePageImages 按页逐像素比较两组栅格图像。
// pixelTolerance 是单个 RGB 通道视为匹配时允许的最大差值。
func ComparePageImages(reference, actual []*image.RGBA, pixelTolerance uint8) (VisualComparison, error) {
	if len(reference) != len(actual) {
		return VisualComparison{}, fmt.Errorf("页面数量不同: 原始 %d 页，待比较 %d 页", len(reference), len(actual))
	}
	if len(reference) == 0 {
		return VisualComparison{}, errors.New("没有可比较的页面")
	}
	result := VisualComparison{PageCount: len(reference), WorstPageSimilarity: 1, WorstPageMatchRate: 1}
	var absSum, matchingPixels, totalPixels uint64
	for index, first := range reference {
		second := actual[index]
		if first == nil || second == nil {
			return VisualComparison{}, fmt.Errorf("第 %d 页图像为空", index+1)
		}
		firstBounds, secondBounds := first.Bounds(), second.Bounds()
		if firstBounds.Size() != secondBounds.Size() {
			return VisualComparison{}, fmt.Errorf("第 %d 页尺寸不同: 原始 %dx%d，待比较 %dx%d",
				index+1, firstBounds.Dx(), firstBounds.Dy(), secondBounds.Dx(), secondBounds.Dy())
		}
		if firstBounds.Empty() {
			return VisualComparison{}, fmt.Errorf("第 %d 页图像尺寸为空", index+1)
		}
		pageAbsSum, pageMatching := uint64(0), uint64(0)
		pagePixels := uint64(firstBounds.Dx()) * uint64(firstBounds.Dy())
		for y := 0; y < firstBounds.Dy(); y++ {
			for x := 0; x < firstBounds.Dx(); x++ {
				firstPixel := first.RGBAAt(firstBounds.Min.X+x, firstBounds.Min.Y+y)
				secondPixel := second.RGBAAt(secondBounds.Min.X+x, secondBounds.Min.Y+y)
				red := absByteDifference(firstPixel.R, secondPixel.R)
				green := absByteDifference(firstPixel.G, secondPixel.G)
				blue := absByteDifference(firstPixel.B, secondPixel.B)
				// 通道差要先转成整数再相加：uint8 相加会溢出，
				// 让大差异像素的误差被低估。
				pageAbsSum += uint64(red) + uint64(green) + uint64(blue)
				if red <= pixelTolerance && green <= pixelTolerance && blue <= pixelTolerance {
					pageMatching++
				}
			}
		}
		pageSimilarity := 1 - float64(pageAbsSum)/(float64(pagePixels)*3*255)
		pageMatchRate := float64(pageMatching) / float64(pagePixels)
		if result.WorstPage == 0 || pageSimilarity < result.WorstPageSimilarity {
			result.WorstPage = index + 1
			result.WorstPageSimilarity = pageSimilarity
			result.WorstPageMatchRate = pageMatchRate
		}
		absSum += pageAbsSum
		matchingPixels += pageMatching
		totalPixels += pagePixels
	}
	result.Similarity = 1 - float64(absSum)/(float64(totalPixels)*3*255)
	result.PixelMatchRate = float64(matchingPixels) / float64(totalPixels)
	result.MeanAbsoluteError = float64(absSum) / (float64(totalPixels) * 3)
	return result, nil
}

func pdfPageImageNumber(path string) int {
	name := strings.TrimSuffix(filepath.Base(path), ".png")
	separator := strings.LastIndexByte(name, '-')
	if separator < 0 {
		return 0
	}
	page, _ := strconv.Atoi(name[separator+1:])
	return page
}

func absByteDifference(first, second uint8) uint8 {
	if first > second {
		return first - second
	}
	return second - first
}
