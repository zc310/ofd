package text

import (
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
)

// Run 是同一原点绘制的文字串。SemanticOnly 只保留映射字形对应的源文字，
// 不参与可见轮廓；PDF 输出可用它补充复制与搜索所需的不可见文字。
type Run struct {
	Value        string
	X, Y         float64
	SemanticOnly bool
}

// WalkRuns 按正文绘制规则定位并合并一个 TextCode 的字形，保持原来的分段顺序。
// object.CGTransform 应已展开对象内的位置；调用方负责字体锁，输入均视为只读。
func WalkRuns(face Face, object models.TextObject, code models.TextCode, codePosition int, visit func(Run)) {
	if face == nil || visit == nil {
		return
	}
	if len(object.CGTransform) == 0 && len(code.DeltaX) == 0 && len(code.DeltaY) == 0 && DirectionsZero(object) {
		if value := VisibleText(code.Value); value != "" {
			visit(Run{Value: value, X: code.X, Y: code.Y})
		}
		return
	}
	glyphs := CodeGlyphs(face, []rune(code.Value), object.CGTransform, codePosition)
	if len(glyphs) == 0 {
		return
	}
	glyphDeltas := UsesGlyphDeltas(code, glyphs)
	posX, posY := code.X, code.Y
	var run, semanticRun []rune
	runX, runY := posX, posY
	semanticX, semanticY := posX, posY
	flushRun := func() {
		if len(run) > 0 {
			visit(Run{Value: string(run), X: runX, Y: runY})
			run = run[:0]
		}
	}
	flushSemanticRun := func() {
		if len(semanticRun) > 0 {
			visit(Run{Value: string(semanticRun), X: semanticX, Y: semanticY, SemanticOnly: true})
			semanticRun = semanticRun[:0]
		}
	}
	for i, glyph := range glyphs {
		if i > 0 {
			prevWidth := face.TextWidth(glyphs[i-1].Value)
			dx, dy := PositionedAdvance(prevWidth, object, code, glyphs[i-1], glyph, glyphDeltas)
			posX, posY = posX+dx, posY+dy
			if len(run) > 0 && (CharDirection(object) != 0 || NeedsRunBreak(object, dx, dy, prevWidth*HScale(object))) {
				flushRun()
			}
		}
		// 不可绘制字符仍消费定位增量，避免后续字形向前漂移。
		if !Renderable(glyph.Value) {
			continue
		}
		if fontrepair.ContainsGlyphRune(glyph.Value) {
			flushRun()
			visit(Run{Value: glyph.Value, X: posX, Y: posY})
			if glyph.Semantic != "" {
				if len(semanticRun) == 0 {
					semanticX, semanticY = posX, posY
				}
				semanticRun = append(semanticRun, []rune(glyph.Semantic)...)
			}
			continue
		}
		flushSemanticRun()
		if len(run) == 0 {
			runX, runY = posX, posY
		}
		run = append(run, []rune(glyph.Value)...)
	}
	flushSemanticRun()
	flushRun()
}
