package testutil

import (
	"context"
	"errors"
	"image"
	"image/color"
	"os"
	"testing"
)

func TestComparePageImages(t *testing.T) {
	first := image.NewRGBA(image.Rect(0, 0, 2, 1))
	second := image.NewRGBA(image.Rect(0, 0, 2, 1))
	second.SetRGBA(1, 0, color.RGBA{R: 20, G: 10, A: 255})

	comparison, err := ComparePageImages([]*image.RGBA{first}, []*image.RGBA{second}, 16)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.PageCount != 1 || comparison.PixelMatchRate != 0.5 {
		t.Fatalf("comparison = %+v, want one page and half matching pixels", comparison)
	}
	if comparison.MeanAbsoluteError <= 0 || comparison.Similarity >= 1 {
		t.Fatalf("comparison = %+v, want non-zero error", comparison)
	}
}

func TestComparePageImagesRejectsDifferentSizes(t *testing.T) {
	first := image.NewRGBA(image.Rect(0, 0, 1, 1))
	second := image.NewRGBA(image.Rect(0, 0, 2, 1))
	if _, err := ComparePageImages([]*image.RGBA{first}, []*image.RGBA{second}, 0); err == nil {
		t.Fatal("不同尺寸图片应返回错误")
	}
}

// TestComparePageImagesCountsLargeChannelDifferences 守住通道差累加不溢出：
// 黑对白的三个通道差都是 255，相加必须按 765 计入。
func TestComparePageImagesCountsLargeChannelDifferences(t *testing.T) {
	first := image.NewRGBA(image.Rect(0, 0, 1, 1))
	second := image.NewRGBA(image.Rect(0, 0, 1, 1))
	second.SetRGBA(0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})

	comparison, err := ComparePageImages([]*image.RGBA{first}, []*image.RGBA{second}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.MeanAbsoluteError != 255 {
		t.Fatalf("MeanAbsoluteError = %v, want 255", comparison.MeanAbsoluteError)
	}
	if comparison.Similarity != 0 {
		t.Fatalf("Similarity = %v, want 0", comparison.Similarity)
	}
	if comparison.PixelMatchRate != 0 {
		t.Fatalf("PixelMatchRate = %v, want 0", comparison.PixelMatchRate)
	}
}

func TestRasterizePDFPagesRejectsInvalidArguments(t *testing.T) {
	if _, err := RasterizePDFPages(context.Background(), "", []byte("pdf"), 36); err == nil {
		t.Fatal("空 pdftoppm 路径应返回错误")
	}
	if _, err := RasterizePDFPages(context.Background(), "pdftoppm", []byte("pdf"), 0); err == nil {
		t.Fatal("无效 DPI 应返回错误")
	}
}

func TestRasterizePDFPagesPreservesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RasterizePDFPages(ctx, os.Args[0], []byte("pdf"), 36); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
