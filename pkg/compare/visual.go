package compare

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

func renderPage(ctx context.Context, s *snapshot, number int, options Options, remaining *int64) (*image.RGBA, error) {
	if number == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if img := s.cache.get(number); img != nil {
		return img, nil
	}
	p := s.input.Pages[number-1]
	w, h := math.Ceil(p.Width*options.DPI/25.4), math.Ceil(p.Height*options.DPI/25.4)
	pixels := w * h
	if w <= 0 || h <= 0 || math.IsNaN(pixels) || math.IsInf(pixels, 0) || pixels > float64(options.MaxPagePixels) || pixels > float64(*remaining) {
		return nil, fmt.Errorf("第 %d 页超过视觉对比像素预算，请降低 DPI 或调整限制", number)
	}
	*remaining -= int64(pixels)
	ref := s.pages[number-1]
	img, err := s.renderers[ref.doc].RasterizePage(ref.page, render.BackendCanvas, geom.DPI(options.DPI))
	if err != nil {
		return nil, fmt.Errorf("渲染第 %d 页: %w", number, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.cache.put(number, img)
	return img, nil
}

func compareVisual(ctx context.Context, a, b *snapshot, ai, bi int, options Options, remaining *int64) (*VisualChange, error) {
	before, err := renderPage(ctx, a, ai, options, remaining)
	if err != nil {
		return nil, err
	}
	after, err := renderPage(ctx, b, bi, options, remaining)
	if err != nil {
		return nil, err
	}
	return compareImages(ctx, before, after, options)
}

func compareImages(ctx context.Context, before, after *image.RGBA, options Options) (*VisualChange, error) {
	w, h := 0, 0
	for _, img := range []*image.RGBA{before, after} {
		if img != nil {
			w = max(w, img.Bounds().Dx())
			h = max(h, img.Bounds().Dy())
		}
	}
	if int64(w)*int64(h) > options.MaxPagePixels {
		return nil, fmt.Errorf("页面合并范围超过像素预算")
	}
	diff := image.NewNRGBA(image.Rect(0, 0, w, h))
	result := &VisualChange{TotalPixels: int64(w) * int64(h), Width: w, Height: h}
	if w == 0 || h == 0 {
		return result, nil
	}
	var grid *regionGrid
	bounds := image.Rect(w, h, 0, 0)
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < w; x++ {
			ac, aok := whitePixel(before, x, y)
			bc, bok := whitePixel(after, x, y)
			changed := aok != bok || abs(int(ac.R)-int(bc.R)) > options.PixelTolerance || abs(int(ac.G)-int(bc.G)) > options.PixelTolerance || abs(int(ac.B)-int(bc.B)) > options.PixelTolerance
			if changed {
				result.ChangedPixels++
				if grid == nil {
					grid = newRegionGrid(w, h, options.DPI)
				}
				grid.add(x, y)
				bounds.Min.X = min(bounds.Min.X, x)
				bounds.Min.Y = min(bounds.Min.Y, y)
				bounds.Max.X = max(bounds.Max.X, x+1)
				bounds.Max.Y = max(bounds.Max.Y, y+1)
				diff.SetNRGBA(x, y, color.NRGBA{R: 230, G: 40, B: 75, A: 255})
			} else {
				gray := uint8((int(ac.R) + int(ac.G) + int(ac.B)) / 3)
				gray = uint8(200 + int(gray)*55/255)
				diff.SetNRGBA(x, y, color.NRGBA{R: gray, G: gray, B: gray, A: 255})
			}
		}
	}
	if result.TotalPixels > 0 {
		result.Ratio = float64(result.ChangedPixels) / float64(result.TotalPixels)
	}
	if result.ChangedPixels > 0 {
		var err error
		result.Regions, result.RegionCount, result.IgnoredRegionPixels, err = grid.regions(ctx, options.MinRegionPixels)
		if err != nil {
			return nil, err
		}
		result.RegionsTruncated = result.RegionCount > len(result.Regions)
		result.Bounds = [4]int{bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Max.Y}
		if before != nil {
			result.BeforePNG, err = encodePreview(ctx, before, options)
			if err != nil {
				return nil, err
			}
		}
		if after != nil {
			result.AfterPNG, err = encodePreview(ctx, after, options)
			if err != nil {
				return nil, err
			}
		}
		result.DiffPNG, err = encodePreview(ctx, diff, options)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func whitePixel(img *image.RGBA, x, y int) (color.RGBA, bool) {
	if img == nil || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return color.RGBA{255, 255, 255, 255}, false
	}
	c := img.RGBAAt(x+img.Bounds().Min.X, y+img.Bounds().Min.Y)
	return color.RGBA{c.R + 255 - c.A, c.G + 255 - c.A, c.B + 255 - c.A, 255}, true
}

func resourcePreview(ctx context.Context, s *snapshot, f *File, options Options) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.Size > 2<<20 {
		return "", nil
	}
	data, err := s.ofd.Documents[0].FileCache.ReadLimit(f.Path, 2<<20)
	if err != nil {
		return "", nil
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 1_000_000/config.Height {
		return "", nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil
	}
	return encodePreview(ctx, img, options)
}
