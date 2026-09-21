package pdf2ofd

import (
	"regexp"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	fontparser "github.com/tdewolff/font"
	"github.com/zc310/ofd/pkg/creator"
)

const pdfPointToMillimeter = 25.4 / 72

var pdfObjectHeader = regexp.MustCompile(`(?m)(\d+)\s+(\d+)\s+obj(?:[ \t\r\n])`)

var pdfRootReference = regexp.MustCompile(`/Root\s+(\d+)\s+(\d+)\s+R`)

type pdfIndirectObject struct {
	number     int
	generation int
	data       []byte
}

type pdfPageInfo struct {
	minX, minY, maxX, maxY float64
	userUnit               float64
	rotate                 int
}

type pdfFontInfo struct {
	widths      map[int]float64
	defaultW    float64
	toUnicode   map[uint16]string
	cidToGID    map[uint16]uint16
	codeBytes   int
	familyName  string
	encoding    string
	format      string
	data        []byte
	codeToGID   map[uint16]uint16
	glyphWidths map[uint16]float64
	sfnt        *fontparser.SFNT
	// bold、italic 是依据字体名称推断的样式，写入文字对象后阅读器才能
	// 选中与嵌入字体匹配的样式，避免对已是斜体/粗体的字体再叠加合成样式。
	bold, italic bool
}

type pdfPathCommand struct {
	op     string
	values []float64
}

type pdfGraphicsState struct {
	ctm, textMatrix, lineMatrix [6]float64
	fontName                    string
	fontSize                    float64
	fill, stroke                pdfColor
	// fillPaint、strokePaint 是 Pattern 颜色空间下 scn/SCN 选中的图案填充；
	// 非 nil 时优先于 fill/stroke 的纯色，用于保留 PDF 渐变与平铺图案。
	fillPaint, strokePaint *pdfPatternPaint
	// fillSpace、strokeSpace 是当前非描边/描边颜色空间，用于正确解释
	// sc/scn 与 SC/SCN 的分量（例如 Separation 的 tint 值）。
	fillSpace, strokeSpace           *pdfColorSpace
	lineWidth                        float64
	renderMode                       int
	charSpacing, wordSpacing, hScale float64
	textLeading                      float64
	// dashPattern、dashOffset 是 `d` 设置的虚线数组与起始偏移（PDF 用户空间
	// 单位）。空数组表示实线。
	dashPattern []float64
	dashOffset  float64
	// fillAlpha、strokeAlpha 是 ExtGState 的 ca/CA 不透明度（0-1）。
	fillAlpha, strokeAlpha float64
	// groupAlpha 是外层 Form XObject 在 Do 时的组透明度。OFD 没有混合模式，
	// 这里忽略 PDF 的 BM，但保留组透明度：Form 内容里的 gs 重置 ca/CA 时，
	// 组透明度仍然生效，避免外层不透明被内层 ca=1 覆盖。
	groupAlpha float64
	// blendMode 是 ExtGState 的 BM 名称。OFD 无法表达混合模式，只在 BM 为
	// Normal/Compatible 时才按纯透明度近似；其它混合模式单独套用 ca/CA 会引入
	// 更大偏差，因此保持原样。
	blendMode string
	// clips 是当前生效的裁剪区域（设备坐标），按 PDF 的 W/W* 逐个求交。
	clips []pdfClipRegion
}

type pdfColor struct{ r, g, b uint8 }

// pdfClipRegion 是一个 PDF 裁剪区域，commands 中的坐标已应用 CTM。
type pdfClipRegion struct {
	commands []pdfPathCommand
}

type pdfInterpreter struct {
	ctx                            *model.Context
	page                           *creator.Page
	document                       *creator.Document
	info                           pdfPageInfo
	state                          pdfGraphicsState
	stack                          []pdfGraphicsState
	path                           []pdfPathCommand
	pointX, pointY, startX, startY float64
	fonts                          map[string]pdfFontInfo
	fontAliases                    map[string]string
	// pendingClip 保存 W/W* 指定的裁剪路径，待下一个路径绘制操作生效。
	pendingClip    []pdfPathCommand
	hasPendingClip bool
}
