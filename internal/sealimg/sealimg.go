// Package sealimg 生成仅供演示与测试的印章图片。
//
// 印章上的文字是固定文案：顶部「OFD 测试专用章」、底部「非正式印章」，五角星下方以小字
// 「zc310/ofd」标识来源。
// 用途。这样做不是省事，而是必要的——印章图片会被单独复制传播，一旦长得像真章
// 就可能脱离本仓库流入真实业务。图片自己带着免责声明，比文档里写多少遍
// 「仅供测试」都管用。
//
// 因此本包不提供自定义文案、不做旧做质感、也不试图逼近真实印章的观感：它要
// 的是一张能替换占位图、让演示与测试截图像模像样，同时一眼看出是测试件的图。
//
// 弧形文字沿椭圆弧排布，字宽按字体度量逐字累加到弧长上，而不是把角度均分——
// 均分会让字在弧顶挤成一团、在弧两端拉散。椭圆弧长没有初等表达式，用数值积分
// 求，再二分反解出给定弧长对应的参数。
package sealimg

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	"golang.org/x/image/draw"
)

// Shape 是印章外框形状。
type Shape int

const (
	// ShapeCircle 是圆形印章，正圆。
	ShapeCircle Shape = iota
	// ShapeEllipse 是椭圆印章，允许宽高不等。
	ShapeEllipse
)

// 固定文案。改动会让演示产物和回归基线一起变化，确有必要时再改。
const (
	TopText    = "OFD 测试专用章"
	BottomText = "非正式印章"
	CenterText = "zc310/ofd"
)

const (
	// defaultSize 是缺省输出像素。
	defaultSize = 512
	// supersample 是超采样倍数。弧形文字和斜边的锯齿靠降采样压掉。
	supersample = 2

	// 以下比例都相对画布尺寸，改输出尺寸不会走形。
	borderAxisRatio         = 0.94  // 外框半轴占该方向半宽的比例
	borderWidthRatio        = 0.022 // 外框线宽相对短边
	innerBorderRatio        = 0.90  // 内框半径相对外框
	innerWidthRatio         = 0.008 // 内框线宽相对短边
	textRadiusRatio         = 0.78  // 文字弧半径相对外框
	topTextRadiusRatio      = 0.72  // 圆形顶部文字弧半径，保持既有圆章视觉距离
	ellipseTopTextGapRatio  = 0.075 // 椭圆顶部文字距内框的统一内缩距离
	textSizeRatio           = 0.14
	ellipseTopTextSizeRatio = 0.13  // 椭圆顶部弧长较短，顶部文案略缩小以避免拥挤
	ellipseTopSpacingRatio  = 0.005 // 椭圆顶部文案较长，收紧字间距以避免过于松散
	spacingRatio            = 0.02  // 字间距相对短边
	starRadiusRatio         = 0.30  // 五角星外接圆半径相对外框
	centerTextSizeRatio     = 0.07  // 五角星下方来源标识使用小字
	centerTextGapRatio      = 0.055 // 小字基线与五角星底部的间距
)

// Options 控制印章图片的输出。
type Options struct {
	// Width、Height 是输出像素；任一为 0 时用 512。椭圆印章可以宽高不等。
	Width, Height int
	// FontName 是字体家族名。留空时按内置候选顺序取第一个覆盖中文的系统字体。
	FontName string
	// Shape 是外框形状，默认圆形。
	Shape Shape
}

func (o Options) size() (width, height int) {
	width, height = o.Width, o.Height
	if width <= 0 {
		width = defaultSize
	}
	if height <= 0 {
		height = defaultSize
	}
	if o.Shape != ShapeEllipse {
		return width, width
	}
	return width, height
}

// Render 渲染印章图片，输出透明底的位图。
//
// 需要系统安装覆盖中文的字体；找不到时返回错误并提示用 Options.FontName 指定，
// 不静默退回缺字形的字体——底字是中文，缺字形出来的图 unusable。
func Render(opts Options) (*image.RGBA, error) {
	width, height := opts.size()
	face, err := loadFace(opts.FontName, float64(min(width, height))*textSizeRatio)
	if err != nil {
		return nil, err
	}
	topFace := face
	if opts.Shape == ShapeEllipse {
		topFace, err = loadFace(opts.FontName, float64(min(width, height))*ellipseTopTextSizeRatio)
		if err != nil {
			return nil, err
		}
	}
	centerFace, err := loadFace(opts.FontName, float64(min(width, height))*centerTextSizeRatio)
	if err != nil {
		return nil, err
	}
	canvasWidth, canvasHeight := float64(width*supersample), float64(height*supersample)
	img := image.NewRGBA(image.Rect(0, 0, width*supersample, height*supersample))
	pen := newPen(img)
	defer pen.close()

	layout := newSealLayout(canvasWidth, canvasHeight)
	pen.drawBorders(layout)
	pen.drawStar(layout)
	pen.drawCenterText(centerFace, layout, CenterText)
	// 顶字从左往右排（dir=-1）；底字保持左往右排并使用正常字形。
	topLayout := topTextLayout(layout, opts.Shape)
	topSpacingRatio := spacingRatio
	if opts.Shape == ShapeEllipse {
		topSpacingRatio = ellipseTopSpacingRatio
	}
	pen.drawArcText(topFace, topLayout, TopText, arcTop, -1, false, topSpacingRatio)
	pen.drawArcText(face, layout, BottomText, arcBottom, 1, false, spacingRatio)
	// 超采样后缩回目标尺寸。CatmullRom 是双三次卷积，比双线性更锐利，
	// 弧形文字的斜边受益最明显。
	small := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(small, small.Bounds(), img, img.Bounds(), draw.Over, nil)
	return small, nil
}

func topTextLayout(layout sealLayout, shape Shape) sealLayout {
	top := layout
	top.textRX = top.borderRX * topTextRadiusRatio
	top.textRY = top.borderRY * topTextRadiusRatio
	if shape == ShapeEllipse {
		gap := top.shortSide * ellipseTopTextGapRatio
		top.textRX = top.borderRX*innerBorderRatio - gap
		top.textRY = top.borderRY*innerBorderRatio - gap
	}
	return top
}

// RenderPNG 渲染印章图片并编码为 PNG，供 ofd-seal --generate 与测试 fixture 使用。
func RenderPNG(opts Options) ([]byte, error) {
	img, err := Render(opts)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("编码印章 PNG 失败: %w", err)
	}
	return out.Bytes(), nil
}
