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
}

type pdfPathCommand struct {
	op     string
	values []float64
}

type pdfGraphicsState struct {
	ctm, textMatrix, lineMatrix      [6]float64
	fontName                         string
	fontSize                         float64
	fill, stroke                     pdfColor
	lineWidth                        float64
	renderMode                       int
	charSpacing, wordSpacing, hScale float64
	textLeading                      float64
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
