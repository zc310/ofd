package watermark

import (
	"fmt"
	"math"
	"strconv"
	"unicode"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/pkg/creator"
)

// TextLayout 描述水印外观在页面区域中的布局方式。
type TextLayout int

const (
	// LayoutManual 使用显式位置（X/Y），保持默认的单条放置。
	LayoutManual TextLayout = iota
	// LayoutTile 把水印平铺铺满整个布局区域。
	LayoutTile
	// LayoutCenter 把水印居中放置。
	LayoutCenter
)

// TextOptions 描述 TextAppearance 生成的文本对象外观。
type TextOptions struct {
	// ID 是文本对象 ID；0 使用默认值 1。
	ID uint64
	// Font 是 Font 属性值（字体 ID 或名称）；空使用 "0"。
	Font string
	// Size 是字号；0 使用默认值 9。
	Size float64
	// Text 是 TextCode 内容，必填。
	Text string
	// X、Y 是布局偏移。LayoutManual 时就是文字起始位置；
	// 平铺/居中模式下作为相对计算位置的偏移。
	X, Y float64
	// Boundary 是布局区域（平铺/居中计算使用）；零尺寸时使用整页 A4 边界。
	Boundary creator.Box
	// CTM 是文本对象变换矩阵；零值表示不输出 CTM。
	CTM [6]float64
	// Color 是 FillColor；nil 时不输出 FillColor。
	Color *creator.Color
	// Fill 指定是否填充；nil 时不输出 Fill 属性。
	Fill *bool
	// Opacity 是整体不透明度（Alpha 属性），取值范围 0 到 255；nil 时不输出。
	Opacity *uint8
	// Layout 指定布局方式，默认 LayoutManual。
	Layout TextLayout
	// Rotation 是文字旋转角度（度），正值在屏幕上为顺时针（左边往上、右边向下），
	// 绕每个文本实例自身中心倾斜、位置不偏移。与 CTM 同时指定时以 CTM 为准。
	Rotation float64
	// LineGap 是平铺行间距（毫米）；0 使用默认值 size*0.6。
	LineGap float64
	// ColumnGap 是平铺列间距（毫米）；0 使用默认值 size*0.6。
	ColumnGap float64
}

// ImageOptions 描述 ImageAppearance 生成的图片对象外观。
type ImageOptions struct {
	// ID 是首个图片对象 ID；平铺时自增。0 使用默认值 1。
	ID uint64
	// ImageID 是被引用的媒体资源 ID，必填。
	ImageID uint64
	// Boundary 是布局区域（平铺/居中计算使用）；零尺寸时使用整页 A4 边界。
	Boundary creator.Box
	// Width 是单张图片显示宽度（毫米）；0 使用默认值 40。
	Width float64
	// Height 是单张图片显示高度（毫米）；0 时按 Aspect 推算。
	Height float64
	// Aspect 是图片宽高比（宽/高）；Height 为 0 且 Aspect 为 0 时按正方形处理。
	Aspect float64
	// X、Y 是布局偏移。LayoutManual 时是单张图片的起始位置；
	// 平铺/居中模式下作为相对计算位置的偏移。
	X, Y float64
	// Gap 是平铺行列间距（毫米）；0 使用默认值 5。
	Gap float64
	// Layout 指定布局方式，默认 LayoutManual。
	Layout TextLayout
}

// TextAppearance 生成水印外观中的文本对象 XML 片段，结果可直接用作
// Watermark.Appearance。
func TextAppearance(options TextOptions) ([]byte, error) {
	if options.Text == "" {
		return nil, fmt.Errorf("水印文本内容为空")
	}
	id := options.ID
	if id == 0 {
		id = 1
	}
	font := options.Font
	if font == "" {
		font = "0"
	}
	size := options.Size
	if size == 0 {
		size = 9
	}
	boundary := options.Boundary
	if boundary.Width == 0 && boundary.Height == 0 {
		boundary = creator.Box{Width: 210, Height: 297}
	}

	lineGap := options.LineGap
	if lineGap == 0 {
		lineGap = size * 0.6
	}
	positions := textPositions(options.Text, size, boundary, options.Layout, options.X, options.Y, lineGap, options.ColumnGap)
	ctm := options.CTM
	if options.CTM == [6]float64{} && options.Rotation != 0 {
		rad := options.Rotation * math.Pi / 180
		cosR, sinR := math.Cos(rad), math.Sin(rad)
		width, height := advance(options.Text, size), size
		// 渲染器会先用 CTM 把 TextCode 绕原点旋转。先做逆变换，平铺网格才会留在页面上。
		// 附加项只让每个字绕自身中心转。正角度 CTM={cos, sin, -sin, cos}，屏幕上为顺时针。
		for i := range positions {
			x, y := positions[i].x, positions[i].y
			positions[i].x = x*cosR + y*sinR + width*0.5*(cosR-1) - height*0.4*sinR
			positions[i].y = -x*sinR + y*cosR - width*0.5*sinR + height*0.4*(1-cosR)
		}
		ctm = [6]float64{cosR, sinR, -sinR, cosR, 0, 0}
	}

	element := etree.NewElement("TextObject")
	element.CreateAttr("ID", strconv.FormatUint(id, 10))
	element.CreateAttr("Boundary", fmtBox(0, 0, boundary.Width, boundary.Height))
	if ctm != [6]float64{} {
		element.CreateAttr("CTM",
			fmtNumber(ctm[0])+" "+fmtNumber(ctm[1])+" "+
				fmtNumber(ctm[2])+" "+fmtNumber(ctm[3])+" "+
				fmtNumber(ctm[4])+" "+fmtNumber(ctm[5]))
	}
	element.CreateAttr("Font", font)
	element.CreateAttr("Size", fmtNumber(size))
	if options.Fill != nil {
		element.CreateAttr("Fill", strconv.FormatBool(*options.Fill))
	}
	if options.Opacity != nil {
		element.CreateAttr("Alpha", strconv.Itoa(int(*options.Opacity)))
	}
	if options.Color != nil {
		fillColor := element.CreateElement("FillColor")
		fillColor.CreateAttr("Value", buildFillColorValue(options.Color))
		if options.Color.ColorSpace != 0 {
			fillColor.CreateAttr("ColorSpace", strconv.FormatUint(options.Color.ColorSpace, 10))
		}
	}

	for _, position := range positions {
		code := element.CreateElement("TextCode")
		code.CreateAttr("X", fmtNumber(position.x))
		code.CreateAttr("Y", fmtNumber(position.y))
		code.SetText(options.Text)
	}

	doc := etree.NewDocument()
	doc.AddChild(element)
	return doc.WriteToBytes()
}

// ImageAppearance 生成水印外观中的图片对象 XML 片段，结果可直接用作
// Watermark.Appearance。
func ImageAppearance(options ImageOptions) ([]byte, error) {
	if options.ImageID == 0 {
		return nil, fmt.Errorf("缺少图片资源 ID")
	}
	id := options.ID
	if id == 0 {
		id = 1
	}
	boundary := options.Boundary
	if boundary.Width == 0 && boundary.Height == 0 {
		boundary = creator.Box{Width: 210, Height: 297}
	}
	width := options.Width
	if width == 0 {
		width = 40
	}
	height := options.Height
	if height == 0 {
		height = width
		if options.Aspect > 0 {
			height = width / options.Aspect
		}
	}
	gap := options.Gap
	if gap == 0 {
		gap = 5
	}
	positions := boxPositions(width, height, boundary, options.Layout, options.X, options.Y, gap)

	doc := etree.NewDocument()
	for index, position := range positions {
		element := etree.NewElement("ImageObject")
		element.CreateAttr("ID", strconv.FormatUint(id+uint64(index), 10))
		element.CreateAttr("Boundary", fmtBox(position.x, position.y, position.w, position.h))
		element.CreateAttr("ResourceID", strconv.FormatUint(options.ImageID, 10))
		doc.AddChild(element)
	}
	return doc.WriteToBytes()
}

// layoutPosition 是平铺/居中计算出的单个放置位置。
type layoutPosition struct {
	x float64
	y float64
}

// textPositions 计算文字水印的 TextCode 位置列表。LayoutManual 时只返回
// 单个起点 (offsetX, offsetY)；LayoutTile 平铺网格；LayoutCenter 居中单行。
func textPositions(text string, size float64, boundary creator.Box,
	layout TextLayout, offsetX, offsetY, lineGap, columnGap float64) []layoutPosition {
	textWidth := advance(text, size)
	switch layout {
	case LayoutCenter:
		// 对象坐标相对 Appearance.Boundary，渲染时会再叠加外观原点。
		return []layoutPosition{{
			x: (boundary.Width-textWidth)/2 + offsetX,
			y: (boundary.Height-size)/2 + offsetY,
		}}
	case LayoutTile:
		if columnGap == 0 {
			columnGap = lineGap
		}
		columnStep := textWidth + columnGap
		rowStep := size + lineGap
		columns := cellCount(boundary.Width, textWidth, columnStep)
		rows := cellCount(boundary.Height, size, rowStep)
		gridWidth := float64(columns-1)*columnStep + textWidth
		gridHeight := float64(rows-1)*rowStep + size
		startX := (boundary.Width-gridWidth)/2 + offsetX
		startY := (boundary.Height-gridHeight)/2 + offsetY
		positions := make([]layoutPosition, 0, rows*columns)
		for row := 0; row < rows; row++ {
			for column := 0; column < columns; column++ {
				positions = append(positions, layoutPosition{
					x: startX + float64(column)*columnStep,
					y: startY + float64(row)*rowStep,
				})
			}
		}
		return positions
	default:
		if offsetX == 0 && offsetY == 0 {
			offsetX, offsetY = 8, 10
		}
		return []layoutPosition{{x: offsetX, y: offsetY}}
	}
}

// boxPositions 计算图片水印的放置矩形列表，规则与 textPositions 一致。
type boxPosition struct {
	x, y, w, h float64
}

func boxPositions(width, height float64, boundary creator.Box,
	layout TextLayout, offsetX, offsetY, gap float64) []boxPosition {
	place := func(x, y float64) boxPosition {
		return boxPosition{x: x, y: y, w: width, h: height}
	}
	switch layout {
	case LayoutCenter:
		return []boxPosition{place(
			(boundary.Width-width)/2+offsetX,
			(boundary.Height-height)/2+offsetY)}
	case LayoutTile:
		columnStep := width + gap
		rowStep := height + gap
		columns := cellCount(boundary.Width, width, columnStep)
		rows := cellCount(boundary.Height, height, rowStep)
		gridWidth := float64(columns-1)*columnStep + width
		gridHeight := float64(rows-1)*rowStep + height
		startX := (boundary.Width-gridWidth)/2 + offsetX
		startY := (boundary.Height-gridHeight)/2 + offsetY
		positions := make([]boxPosition, 0, rows*columns)
		for row := 0; row < rows; row++ {
			for column := 0; column < columns; column++ {
				positions = append(positions, place(
					startX+float64(column)*columnStep,
					startY+float64(row)*rowStep))
			}
		}
		return positions
	default:
		return []boxPosition{place(offsetX, offsetY)}
	}
}

// cellCount 计算区域内能容纳的格子数量，至少为 1。
func cellCount(area, cell, step float64) int {
	if step <= 0 || cell <= 0 || area <= 0 {
		return 1
	}
	count := int((area-cell)/step) + 1
	if count < 1 {
		count = 1
	}
	return count
}

// advance 估算整行文本在指定字号下的宽度（毫米）。全角字符按一个字号
// 计算，半角字符按半个字号估算；不是精确排版，仅用于布局定位。
func advance(text string, size float64) float64 {
	total := 0.0
	for _, r := range text {
		total += runeAdvance(r, size)
	}
	return total
}

func runeAdvance(r rune, size float64) float64 {
	if r <= 0 || r == '\n' || r == '\r' {
		return 0
	}
	if isCJK(r) {
		return size
	}
	return size * 0.5
}

func isCJK(r rune) bool {
	if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
		return true
	}
	return (r >= 0x1100 && r <= 0x11FF) || // Hangul Jamo
		(r >= 0x2E80 && r <= 0xA4CF) || // CJK Radicals .. Yi
		(r >= 0xAC00 && r <= 0xD7A3) || // Hangul Syllables
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compat Ideographs
		(r >= 0xFE30 && r <= 0xFE4F) || // CJK Compat Forms
		(r >= 0xFF00 && r <= 0xFF60) // Fullwidth Forms
}

func buildFillColorValue(value *creator.Color) string {
	if len(value.Components) > 0 {
		return fmtIntList(value.Components)
	}
	return fmt.Sprintf("%d %d %d", value.R, value.G, value.B)
}

func fmtIntList(values []int) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += " "
		}
		result += strconv.Itoa(value)
	}
	return result
}
