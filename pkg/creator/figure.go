package creator

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // 注册 GIF 解码器
	_ "image/jpeg" // 注册 JPEG 解码器
	_ "image/png"  // 注册 PNG 解码器
	"io"
)

// 图片自然尺寸按 96dpi 换算成毫米，与渲染层的默认假设一致。
const figureNaturalDPI = 96.0

// Figure 是带说明文字的图片块，生成时展开为一个 ImageObject 与一个 TextObject。
//
// 单独的 Image 图元对应 OFD 的 ImageObject，是个叶子图元，没有"图片带标题"的
// 概念；而带说明的图在公文、报告、论文里是极常见的排版单元，所以另立一个复合
// 图元，而不是往 Image 上加说明字段。
type Figure struct {
	// X、Y 是图块左上角坐标，单位毫米。
	X float64
	Y float64
	// Width 是图块宽度，也是图片与说明文字对齐的参照框，单位毫米。
	// 为 0 时图块宽度等于图片实际宽度，居中与居右都退化为无操作。
	Width float64
	// Align 是图片与说明文字共用的水平对齐：left / center / right。
	Align string

	// Image 承载图片数据与外观（边框、透明度、裁剪等）。其中的 X、Y、Width、
	// Height 由 Figure 统一决定，展开时会被覆盖。
	Image Image
	// ImageWidth 是图片宽度，单位毫米；为 0 表示不指定。
	// ImageFillWidth 为真时 ImageWidth 按图块宽度的比例解释（0.5 即一半）。
	ImageWidth float64
	// ImageFillWidth 为真时 ImageWidth 按图块宽度的比例解释。
	ImageFillWidth bool
	// ImageHeight 是图片高度，单位毫米；为 0 时按图片原始比例推出。
	ImageHeight float64
	// Fit 是图片在给定尺寸内的适配方式：
	// "contain"（默认）保持原始比例，两个尺寸都给时按小的比例缩放；
	// "stretch" 拉伸填满，此时两个尺寸都必须给。
	Fit string

	// Caption 是图片下方的说明文字，如 "图 3 营收走势"。留空不画说明。
	Caption string
	// CaptionStyle 是说明文字样式；Gap 指定说明与图片底边的间距，
	// 为 0 时用 defaultCaptionGap。
	CaptionStyle *CaptionStyle
}

func (Figure) isItem() {}

// figureGeometry 是一张图解析后的落位尺寸。
type figureGeometry struct {
	// FrameWidth 是图块宽度，即对齐参照框的宽度。
	FrameWidth float64
	// X、Y 是图片左上角坐标。
	X, Y float64
	// Width、Height 是图片绘制尺寸。
	Width, Height float64
	// NaturalWidth、NaturalHeight 是图片的原始像素尺寸，未能解析时为 0。
	NaturalWidth, NaturalHeight int
}

// TotalHeight 是图块总高：图片高度加说明文字所占的高度。
func (g figureGeometry) TotalHeight(caption string, style *CaptionStyle) float64 {
	total := g.Height
	if caption == "" {
		return total
	}
	size := captionSize(style)
	return g.Height + captionGap(style) + size
}

// captionSize 返回说明文字的字号，未设置时用默认值。
func captionSize(style *CaptionStyle) float64 {
	if style == nil || style.Size <= 0 {
		return defaultCaptionSize
	}
	return style.Size
}

// imagePixelSize 读取图片头的像素尺寸。只读文件头，不解码整张图。
//
// 返回 false 表示该格式拿不到尺寸：Go 标准库只带 PNG、JPEG、GIF 三个解码器，
// 而 imageFormatFromPrefix 还认得 BMP、WEBP、TIFF、JBIG2。
func imagePixelSize(value Image) (int, int, bool) {
	reader := bytesReaderFor(value)
	if reader == nil {
		return 0, 0, false
	}
	config, _, err := image.DecodeConfig(reader)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

// figureMediaLookup 按资源 ID 取回图片数据，用于读取像素尺寸。
//
// 图片也可以只给 resource_id 引用文档里已声明的资源，此时 Figure 自身没有
// 字节，按比例推尺寸就得回文档里找。
type figureMediaLookup func(id uint64) (Image, bool)

// resolveFigure 计算图片的最终落位与尺寸。
//
// lookup 为 nil 时只能按自带数据推尺寸，引用 resource_id 的图片会因读不到
// 像素尺寸而受限。
func resolveFigure(value Figure, lookup figureMediaLookup) (figureGeometry, error) {
	if value.Width < 0 {
		return figureGeometry{}, fmt.Errorf("图块宽度无效: %.6g", value.Width)
	}
	if value.ImageWidth < 0 || value.ImageHeight < 0 {
		return figureGeometry{}, errors.New("图片尺寸不能为负数")
	}
	if value.ImageFillWidth && value.ImageWidth > 1 {
		return figureGeometry{}, fmt.Errorf("image_fill_width 为真时 image_width 应为 0 到 1 的比例，实际 %.6g", value.ImageWidth)
	}
	switch value.Align {
	case "", "left", "center", "right":
	default:
		return figureGeometry{}, fmt.Errorf("图块对齐方式无效: %q", value.Align)
	}
	switch value.Fit {
	case "", "contain", "stretch":
	default:
		return figureGeometry{}, fmt.Errorf("图片适配方式无效: %q", value.Fit)
	}

	frameWidth := value.Width
	targetWidth := value.ImageWidth
	if value.ImageFillWidth {
		if frameWidth <= 0 {
			return figureGeometry{}, errors.New("image_fill_width 需要同时设置图块宽度")
		}
		targetWidth = frameWidth * value.ImageWidth
	}
	targetHeight := value.ImageHeight

	// 两个尺寸都没给时：给了图块宽度就填满它，否则按图片原始尺寸落位。
	// 于是"给个图块宽度"或"丢张图进来"两种写法都能用。
	if targetWidth <= 0 && targetHeight <= 0 {
		if frameWidth > 0 {
			targetWidth = frameWidth
		} else {
			naturalW, naturalH, ok := imagePixelSize(figureSourceImage(value, lookup))
			if !ok {
				return figureGeometry{}, errors.New("未指定图片尺寸，且无法读取该格式的图片尺寸；请设置 image_width 或 image_height")
			}
			targetWidth = float64(naturalW) * 25.4 / figureNaturalDPI
			targetHeight = float64(naturalH) * 25.4 / figureNaturalDPI
		}
	}

	// 还缺的一边按原始比例补上。
	if (targetWidth <= 0) != (targetHeight <= 0) {
		naturalW, naturalH, ok := imagePixelSize(figureSourceImage(value, lookup))
		if !ok {
			return figureGeometry{}, errors.New("只指定了 image_width 与 image_height 其中一个，无法按比例补出另一边；请把另一个也设上")
		}
		if targetWidth <= 0 {
			targetWidth = targetHeight * float64(naturalW) / float64(naturalH)
		} else {
			targetHeight = targetWidth * float64(naturalH) / float64(naturalW)
		}
	}

	// contain 的两个尺寸是"装得下的框"，不是图片的目标尺寸：图片按原始比例缩放
	// 装进这个框。直接按给定值缩放会把 2:1 的图压成正方形。
	if value.Fit != "stretch" {
		if naturalW, naturalH, ok := imagePixelSize(figureSourceImage(value, lookup)); ok {
			scale := targetWidth / float64(naturalW)
			if fit := targetHeight / float64(naturalH); fit < scale {
				scale = fit
			}
			targetWidth = float64(naturalW) * scale
			targetHeight = float64(naturalH) * scale
		}
		if frameWidth > 0 && targetWidth > frameWidth {
			scale := frameWidth / targetWidth
			targetWidth *= scale
			targetHeight *= scale
		}
	}

	if frameWidth <= 0 {
		frameWidth = targetWidth
	}
	geometry := figureGeometry{
		FrameWidth: frameWidth,
		Y:          value.Y,
		Width:      targetWidth,
		Height:     targetHeight,
	}
	geometry.X = value.X + alignOffset(value.Align, frameWidth, targetWidth)
	return geometry, nil
}

// alignOffset 按对齐方式算出图片相对图块左边界的偏移。
func alignOffset(align string, frameWidth, contentWidth float64) float64 {
	switch align {
	case "center":
		return (frameWidth - contentWidth) / 2
	case "right":
		return frameWidth - contentWidth
	default:
		return 0
	}
}

// figureCaption 构造说明文字的落位。说明在图片下方，与图片共用同一套对齐方式。
func figureCaption(value Figure, geometry figureGeometry) *Text {
	if value.Caption == "" {
		return nil
	}
	style := value.CaptionStyle
	size := captionSize(style)
	textWidth := estimateTextWidth(value.Caption, size)
	x := value.X + alignOffset(value.Align, geometry.FrameWidth, textWidth)

	font := "SimHei"
	weight := 700
	if style != nil {
		if style.Font != "" {
			font = style.Font
		}
		if style.Weight > 0 {
			weight = style.Weight
		}
	}
	offset := size
	return &Text{
		X:         x,
		Y:         geometry.Y + geometry.Height + captionGap(style),
		Width:     textWidth,
		Height:    size,
		Value:     value.Caption,
		Size:      size,
		Font:      font,
		Weight:    weight,
		Italic:    style != nil && style.Italic,
		TextCodes: []TextCode{{Value: value.Caption, Y: &offset}},
	}
}

// figureSourceImage 返回带图片数据的 Image：自带数据优先，否则按资源 ID 回查。
func figureSourceImage(value Figure, lookup figureMediaLookup) Image {
	if len(value.Image.Data) > 0 || value.Image.Source != nil {
		return value.Image
	}
	if value.Image.ResourceID != 0 && lookup != nil {
		if media, ok := lookup(value.Image.ResourceID); ok {
			return media
		}
	}
	return value.Image
}

// bytesReaderFor 返回图片数据的读取器，优先惰性来源。
func bytesReaderFor(value Image) *bytes.Reader {
	if value.Source != nil {
		reader, err := value.Source.Open()
		if err != nil {
			return nil
		}
		defer func() { _ = reader.Close() }()
		data, err := io.ReadAll(reader)
		if err != nil {
			return nil
		}
		return bytes.NewReader(data)
	}
	if len(value.Data) == 0 {
		return nil
	}
	return bytes.NewReader(value.Data)
}

// TableHeight 返回表格内容区从顶边到最后一行的总高，单位毫米。
//
// 用于把多张表依次排在同一页上：摆下一张表要知道上一张有多高，而这个高度由行高
// 与单元格换行决定，调用方按字号估算必然对不上。
//
// 不含表题：表题在内容上方，计入会让调用方重复预留间距。页高不参与计算，
// 所以这个值与表格最终落在第几页无关。
func TableHeight(table Table, pageWidth float64) (float64, error) {
	layout, err := layoutTable(table, pageWidth, 0)
	if err != nil {
		return 0, err
	}
	return layout.rowY[len(layout.rowY)-1] - layout.rowY[0], nil
}
