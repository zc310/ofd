// Package image 将单页图片和多页 TIFF 导入为带图片与 OCR 文字层的 OFD。
package image

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"

	"github.com/hhrutter/tiff"
	"github.com/zc310/ofd/internal/ocr"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
)

// TextMode 控制 OCR 文字层的视觉呈现。
type TextMode string

const (
	// TextInvisible 让文字层透明，保留搜索和复制能力。
	TextInvisible TextMode = "invisible"
	// TextVisible 显示文字层，适合调试 OCR 坐标。
	TextVisible TextMode = "visible"
	// TextOff 不执行 OCR，也不生成文字层。
	TextOff TextMode = "off"
)

// Options 控制图片导入和 OCR。
type Options struct {
	Engine         ocr.Engine
	OCRLanguage    string
	DPI            float64
	TextMode       TextMode
	MinConfidence  float64
	MaxPages       int
	MaxPagePixels  int64
	MaxTotalPixels int64
	MaxInputBytes  int64
}

// Option 修改导入选项。
type Option func(*Options)

func WithOCREngine(engine ocr.Engine) Option { return func(o *Options) { o.Engine = engine } }
func WithDPI(dpi float64) Option             { return func(o *Options) { o.DPI = dpi } }
func WithTextMode(mode TextMode) Option      { return func(o *Options) { o.TextMode = mode } }
func WithOCR(enabled bool) Option {
	return func(o *Options) {
		if enabled {
			if o.Engine == nil {
				o.Engine = ocr.NewTesseract("", o.OCRLanguage)
			}
			if o.TextMode == TextOff {
				o.TextMode = TextInvisible
			}
			return
		}
		o.Engine = nil
		o.TextMode = TextOff
	}
}
func WithOCRLanguage(language string) Option {
	return func(o *Options) {
		o.OCRLanguage = language
		if engine, ok := o.Engine.(*ocr.Tesseract); ok {
			engine.Language = language
		}
	}
}
func WithMinConfidence(value float64) Option { return func(o *Options) { o.MinConfidence = value } }
func WithMaxPages(value int) Option          { return func(o *Options) { o.MaxPages = value } }
func WithMaxPagePixels(value int64) Option   { return func(o *Options) { o.MaxPagePixels = value } }
func WithMaxTotalPixels(value int64) Option  { return func(o *Options) { o.MaxTotalPixels = value } }
func WithMaxInputBytes(value int64) Option   { return func(o *Options) { o.MaxInputBytes = value } }

func defaultOptions() Options {
	return Options{
		Engine:         ocr.NewTesseract("", ""),
		OCRLanguage:    "chi_sim+eng",
		DPI:            300,
		TextMode:       TextInvisible,
		MaxPages:       1000,
		MaxPagePixels:  100_000_000,
		MaxTotalPixels: 250_000_000,
		MaxInputBytes:  512 << 20,
	}
}

// Convert 将图片输入写成 OFD。PNG/JPEG 产生单页 OFD；经典多页 TIFF 按 IFD 顺序产生多页 OFD。
func Convert(ctx context.Context, input any, output io.Writer, options ...Option) error {
	if output == nil {
		return fmt.Errorf("OFD 输出写入器为空")
	}
	opts := defaultOptions()
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	if err := opts.validate(); err != nil {
		return err
	}
	data, err := readInput(input, opts.MaxInputBytes)
	if err != nil {
		return err
	}
	pages, err := pageSources(data, opts)
	if err != nil {
		return err
	}
	models := make([]creator.Page, 0, len(pages))
	var pageSize creator.PageSize
	var totalPixels int64
	for index, source := range pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkConfigPixels(source.config, opts, &totalPixels); err != nil {
			return fmt.Errorf("第 %d 页: %w", index+1, err)
		}
		page, err := source.decode(data)
		if err != nil {
			return fmt.Errorf("解码第 %d 页失败: %w", index+1, err)
		}
		model, size, err := makePage(ctx, page, opts, index)
		if err != nil {
			return fmt.Errorf("处理第 %d 页失败: %w", index+1, err)
		}
		if index == 0 {
			pageSize = size
		}
		models = append(models, model)
	}
	return creator.Create(creator.Document{
		ID: "image-import", Title: "图片 OCR 文档", Creator: "ofd imageimport", CreatorVersion: "1",
		PageSize: pageSize, Pages: models,
	}, output)
}

// Importer 是 converter 注册表使用的图片导入器。
type Importer struct {
	NameValue       string
	ExtensionsValue []string
}

func (i *Importer) Name() string         { return i.NameValue }
func (i *Importer) Extensions() []string { return i.ExtensionsValue }
func (i *Importer) MIME() string {
	switch i.NameValue {
	case "jpeg":
		return "image/jpeg"
	case "tiff":
		return "image/tiff"
	default:
		return "image/png"
	}
}

func (i *Importer) Import(input any, output io.Writer, conv *converter.Converter) error {
	return Convert(conv.Context(), input, output,
		WithOCR(conv.ImageOCREnabled()),
		WithOCRLanguage(conv.ImageOCRLanguage()),
	)
}

func init() {
	converter.RegisterImporter(&Importer{NameValue: "png", ExtensionsValue: []string{".png"}})
	converter.RegisterImporter(&Importer{NameValue: "jpeg", ExtensionsValue: []string{".jpg", ".jpeg"}})
	converter.RegisterImporter(&Importer{NameValue: "tiff", ExtensionsValue: []string{".tif", ".tiff"}})
}

type pageSource struct {
	offset int64
	tiff   bool
	config image.Config
}

func (p pageSource) decode(data []byte) (image.Image, error) {
	if p.tiff {
		return tiff.DecodeAt(bytes.NewReader(data), p.offset)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	return decoded, err
}

func (o Options) validate() error {
	if o.DPI <= 0 {
		return fmt.Errorf("DPI 必须大于 0")
	}
	if o.TextMode != TextInvisible && o.TextMode != TextVisible && o.TextMode != TextOff {
		return fmt.Errorf("不支持的文字层模式 %q", o.TextMode)
	}
	if o.MinConfidence < 0 || o.MinConfidence > 100 {
		return fmt.Errorf("OCR 最低置信度必须在 0 到 100 之间")
	}
	if o.MaxPages <= 0 || o.MaxPagePixels <= 0 || o.MaxTotalPixels <= 0 || o.MaxInputBytes <= 0 {
		return fmt.Errorf("图片资源上限必须大于 0")
	}
	return nil
}

func readInput(input any, limit int64) ([]byte, error) {
	switch value := input.(type) {
	case string:
		file, err := os.Open(value)
		if err != nil {
			return nil, fmt.Errorf("读取图片失败: %w", err)
		}
		defer func() { _ = file.Close() }()
		return readLimited(file, limit)
	case []byte:
		if int64(len(value)) > limit {
			return nil, fmt.Errorf("图片输入超过大小上限 %d 字节", limit)
		}
		return append([]byte(nil), value...), nil
	case io.Reader:
		return readLimited(value, limit)
	default:
		return nil, fmt.Errorf("不支持的图片输入类型 %T", input)
	}
}

func readLimited(input io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取图片输入失败: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("图片输入超过大小上限 %d 字节", limit)
	}
	return data, nil
}

func pageSources(data []byte, opts Options) ([]pageSource, error) {
	if isTIFF(data) {
		offsets, err := tiffOffsets(data, opts.MaxPages)
		if err != nil {
			return nil, err
		}
		pages := make([]pageSource, 0, len(offsets))
		for _, offset := range offsets {
			config, err := tiff.DecodeConfigAt(bytes.NewReader(data), offset)
			if err != nil {
				return nil, fmt.Errorf("读取 TIFF 页面信息失败: %w", err)
			}
			pages = append(pages, pageSource{offset: offset, tiff: true, config: config})
		}
		return pages, nil
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("读取图片信息失败: %w", err)
	}
	return []pageSource{{config: config}}, nil
}

func checkConfigPixels(config image.Config, opts Options, total *int64) error {
	pixels := int64(config.Width) * int64(config.Height)
	if opts.MaxPagePixels > 0 && pixels > opts.MaxPagePixels {
		return fmt.Errorf("图片像素数 %d 超过上限 %d", pixels, opts.MaxPagePixels)
	}
	if total != nil {
		*total += pixels
		if opts.MaxTotalPixels > 0 && *total > opts.MaxTotalPixels {
			return fmt.Errorf("图片总像素数 %d 超过上限 %d", *total, opts.MaxTotalPixels)
		}
	}
	return nil
}

func isTIFF(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return (data[0] == 'I' && data[1] == 'I' && data[2] == 42 && data[3] == 0) ||
		(data[0] == 'M' && data[1] == 'M' && data[2] == 0 && data[3] == 42)
}

// tiffOffsets 读取经典 TIFF 的 IFD 链。DecodeAt 仍负责校验和解压每一页。
func tiffOffsets(data []byte, maxPages int) ([]int64, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("TIFF 文件头不完整")
	}
	little := data[0] == 'I'
	read16 := func(pos int) (uint16, error) {
		if pos < 0 || pos+2 > len(data) {
			return 0, fmt.Errorf("TIFF 偏移越界")
		}
		if little {
			return uint16(data[pos]) | uint16(data[pos+1])<<8, nil
		}
		return uint16(data[pos])<<8 | uint16(data[pos+1]), nil
	}
	read32 := func(pos int) (uint32, error) {
		if pos < 0 || pos+4 > len(data) {
			return 0, fmt.Errorf("TIFF 偏移越界")
		}
		if little {
			return uint32(data[pos]) | uint32(data[pos+1])<<8 | uint32(data[pos+2])<<16 | uint32(data[pos+3])<<24, nil
		}
		return uint32(data[pos])<<24 | uint32(data[pos+1])<<16 | uint32(data[pos+2])<<8 | uint32(data[pos+3]), nil
	}
	magic, err := read16(2)
	if err != nil || magic != 42 {
		return nil, fmt.Errorf("不支持的 TIFF 版本")
	}
	next, err := read32(4)
	if err != nil {
		return nil, err
	}
	seen := map[uint32]bool{}
	result := []int64{}
	for next != 0 {
		if len(result) >= maxPages {
			return nil, fmt.Errorf("TIFF 页数超过上限 %d", maxPages)
		}
		if seen[next] {
			return nil, fmt.Errorf("TIFF IFD 链存在循环")
		}
		seen[next] = true
		pos := int(next)
		n, err := read16(pos)
		if err != nil {
			return nil, err
		}
		end := pos + 2 + int(n)*12
		if end < pos || end+4 > len(data) {
			return nil, fmt.Errorf("TIFF IFD 越界")
		}
		result = append(result, int64(next))
		next, err = read32(end)
		if err != nil {
			return nil, err
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("TIFF 不包含页面")
	}
	return result, nil
}

func makePage(ctx context.Context, page image.Image, opts Options, index int) (creator.Page, creator.PageSize, error) {
	if err := ctx.Err(); err != nil {
		return creator.Page{}, creator.PageSize{}, err
	}
	bounds := page.Bounds()
	width := float64(bounds.Dx()) / opts.DPI * 25.4
	height := float64(bounds.Dy()) / opts.DPI * 25.4
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, page); err != nil {
		return creator.Page{}, creator.PageSize{}, fmt.Errorf("编码页面图片失败: %w", err)
	}
	resourceID := uint64(index + 1)
	items := []creator.Item{
		creator.Image{X: 0, Y: 0, Width: width, Height: height, ResourceID: resourceID},
	}
	if opts.Engine != nil && opts.TextMode != TextOff {
		blocks, err := opts.Engine.Recognize(ctx, page)
		if err != nil {
			return creator.Page{}, creator.PageSize{}, err
		}
		blocks = ocr.Normalize(blocks, bounds)
		for _, block := range blocks {
			if block.Confidence < opts.MinConfidence {
				continue
			}
			box := block.Bounds
			x := float64(box.Min.X) / opts.DPI * 25.4
			w := float64(box.Dx()) / opts.DPI * 25.4
			h := float64(box.Dy()) / opts.DPI * 25.4
			y := height - float64(box.Max.Y)/opts.DPI*25.4
			fill := true
			alpha := uint8(0)
			if opts.TextMode == TextVisible {
				alpha = 255
			}
			items = append(items, creator.Text{
				X: x, Y: y, Width: w, Height: h, Value: block.Text,
				Font: "SimSun", Size: h, Fill: &fill,
				FillColor: &creator.Color{R: 0, G: 0, B: 0, Alpha: &alpha},
			})
		}
	}
	return creator.Page{
		Area:      &creator.PageArea{PhysicalBox: &creator.Box{Width: width, Height: height}},
		Resources: []creator.PageResource{{Images: []creator.PageImage{{ID: resourceID, Format: "PNG", Name: fmt.Sprintf("page-%04d.png", index+1), Data: encoded.Bytes()}}}},
		Items:     items,
	}, creator.PageSize{Width: width, Height: height}, nil
}
