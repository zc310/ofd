package render

import (
	"strings"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
	"github.com/zc310/ofd/internal/render/paint"
)

func (p *Document) Text(ctx DrawContext, object models.TextObject, dp *models.DrawParam, pb models.StBox) {
	var budget renderBudget
	budget.reset()
	p.textWithBudget(ctx, object, dp, pb, nil, nil, &budget)
}

func (p *Document) textWithBudget(ctx DrawContext, object models.TextObject, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *geom.Path, budget *renderBudget) {
	if !drawableGraphicUnit(object.VisibleValue(), object.CTM, parentCTM, object.Boundary, pb) ||
		!finiteFloat(object.Size) {
		return
	}
	if parentCTM != nil && object.CTM != nil && !parentCTM.Multiply(object.CTM).IsFinite() {
		return
	}
	ctx.Push()
	defer ctx.Pop()
	// 文字裁剪区可能引用同一字体，必须在持有字体绘制锁之前构建。
	clip := p.graphicClip(object.CTGraphicUnit, pb.Height, parentCTM, parentClip)
	if clip != nil && clip.Empty() {
		return
	}
	// GB/T 33190—2016 §8.3.3：先绘制底纹填充，再绘制描边。
	// 底纹单元可能包含同一字体的文字，不能持字体锁展开。
	patternFill := false
	if source := textFillColor(object, dp); source != nil && source.Pattern != nil && !textFillDisabled(object) {
		if _, semanticOnly := ctx.(semanticOnlyContext); !semanticOnly {
			p.drawPatternText(ctx, source, object, pb, parentCTM, clip, budget)
			patternFill = true
			if !object.Stroke {
				return
			}
			object.Fill.Set(false)
		}
	}

	// 底纹描边先在字体锁内准备独立几何，填充结束并释放字体锁后再展开底纹。
	// §8.3.3 同时允许文字底纹填充和勾边；单元可能再次引用当前字体。
	if source := textStrokeColor(object, dp); object.Stroke && source != nil && source.Pattern != nil {
		if _, semanticOnly := ctx.(semanticOnlyContext); !semanticOnly {
			var outline *geom.Path
			ctx.Push()
			p.withTextOutlineFace(object, func(face FontFace) {
				outline = p.textStrokeOutline(ctx, face, object, dp, pb, parentCTM, clip)
			})
			ctx.Pop()
			defer p.drawPatternTextPath(ctx, source, object, outline, pb, parentCTM, budget, !patternFill && textFillDisabled(object))
			object.Stroke = false
		}
	}

	if p.fonts == nil {
		budget.diagnose("font_unavailable", "未注册字体引擎", models.StID(object.Font))
		return
	}
	fontFamily, err := p.fonts.LoadFont(object.Font)
	if err != nil || fontFamily == nil {
		message := "字体不可用"
		if err != nil {
			message = err.Error()
		}
		budget.diagnose("font_unavailable", message, models.StID(object.Font))
		return
	}
	if budget.Report != nil {
		if fallback := p.fonts.FallbackFontFamily(object.Font); fallback != "" {
			budget.diagnose("font_fallback", fallback, models.StID(object.Font))
		}
	}
	fontLock := p.fonts.RenderLock(fontFamily)
	fontLock.Lock()
	defer fontLock.Unlock()

	// OFD 中 Fill=false 表示文字不填充；当 Stroke=true 时仍需绘制描边。
	if textFillDisabled(object) && !object.Stroke {
		return
	}
	fill, _ := p.updateDrawParams(ctx, dp)
	if dp == nil {
		ctx.SetStrokeWidth(paint.DefaultLineWidth)
	}
	// 字号保持 OFD 的原始 Size（对象坐标毫米）；CTM 的线性部分（缩放、旋转、
	// 倾斜）统一由 textlayout.CTMLinearMatrix 作用到字形上，不能在这里再乘一次
	// CTM.YScale，否则缩放会被应用两次，文字明显变小。
	if !finiteFloat(object.Size) {
		return
	}
	// 空心字（仅描边，不填充）：将填充置空，保留描边颜色。
	if textFillDisabled(object) && object.Stroke {
		fill = nil
	} else {
		if paint.HasColorValue(object.FillColor) {
			fill = p.updateCtColor(object.FillColor)
		}
	}
	if object.Alpha != nil {
		if fill != nil && fill.HasValue {
			fill = &CTColor{Value: paint.ScaleAlpha(fill.Value, graphicOpacity(object.Alpha)), HasValue: true, Gradient: fill.Gradient}
		}
	}
	source := textFillColor(object, dp)
	// 矢量后端（PDF/SVG）能把原生 Linear/RadialGradient 序列化为文字着色图案。
	// 对可原生表达的轴向/径向渐变（含 Extend=0/1/2），改用带两端延伸位的原生
	// 渐变作为文字填充：文字保留真实文本 + 原生 Shading，PDF 中仍可复制。
	// Repeat/Reflect、椭圆、焦点径向、网格等仍走栅格化。
	if fill != nil {
		if bt, ok := ctx.(shadingTextBackend); ok && bt.ShadingText() && isAxialRadialShading(source) {
			if gradient := p.textShadingGradient(source, object.Alpha); gradient != nil {
				fill.Gradient = gradient
			}
		}
	}
	face := p.fonts.FaceObject(fontFamily, object, fill)
	if face == nil {
		return
	}
	if !face.Fill().Has() && !object.Stroke {
		// 颜色透明（Alpha=0）时文字不可见，且 PDF 渲染器会因此输出非法的 NaN 颜色值
		// 破坏内容流，直接跳过绘制。
		return
	}
	strokedText := object.Stroke
	if strokedText {
		// 描边在填充之后绘制，并在合并前分别扩展每个 run 的轮廓。此时仍持有
		// 字体锁；函数返回前先执行描边，再释放字体锁和绘制上下文状态。
		defer p.drawTextStroke(ctx, face, object, dp, pb, parentCTM, clip, !patternFill && textFillDisabled(object), budget)
		object.Stroke = false
		if textFillDisabled(object) {
			return
		}
	}
	nativeShadingText := false
	if bt, ok := ctx.(shadingTextBackend); ok && bt.ShadingText() && nativeShadingGradient(fill) {
		nativeShadingText = !strokedText && !textNeedsPathDrawing(face, object) &&
			paint.MapNativeGradient(fill.Gradient, textRunBoundaryMatrix(object, 0, 0)) != nil
	}
	if !textFillDisabled(object) && isShadingColor(source) && !nativeShadingText {
		if p.drawMeshText(ctx, face, source, object, pb, parentCTM, clip, budget) {
			p.drawRasterTextSemantic(ctx, face, object, pb.Height, parentCTM)
			return
		}
	}
	ctx.ClearStroke()
	faces := newTextFaces(p.fonts, fontFamily, object, fill, face, textlayout.HScale(object))
	faces.native = nativeShadingText
	faces.forcePath = strokedText
	if clip != nil {
		faces.clip = clip.Copy().Transform(ctx.CurrentMatrix())
	}
	codePosition := 0
	transforms := models.ExpandCGTransformPositions(object.TextCode, object.CGTransform)
	object.CGTransform = transforms
	for _, code := range object.TextCode {
		position := codePosition
		codePosition += len([]rune(code.Value))
		if !finiteFloat(code.X) || !finiteFloat(code.Y) || !finiteArray(code.DeltaX) || !finiteArray(code.DeltaY) {
			continue
		}
		p.drawTextCode(ctx, faces, object, code, pb.Height, parentCTM, position)
	}
}

func textFillColor(object models.TextObject, dp *models.DrawParam) *models.CTColor {
	if paint.HasColorValue(object.FillColor) {
		return object.FillColor
	}
	if dp != nil {
		return dp.FillColor
	}
	return nil
}

func textStrokeColor(object models.TextObject, dp *models.DrawParam) *models.CTColor {
	if paint.HasColorValue(object.StrokeColor) {
		return object.StrokeColor
	}
	if dp != nil {
		return dp.StrokeColor
	}
	return nil
}

// nativeShadingGradient 判断填充渐变是否是可以直接写成 PDF 原生 Shading 的
// Linear/RadialGradient（Extend=3、非焦点径向）。自定义渐变类型返回 false。
func nativeShadingGradient(fill *CTColor) bool {
	if fill == nil || fill.Gradient == nil {
		return false
	}
	switch fill.Gradient.(type) {
	case *geom.LinearGradient, *geom.RadialGradient:
		return true
	default:
		return false
	}
}

// textNeedsPathDrawing 判断文字是否必须走轮廓路径绘制：描边、逐字旋转，或无法
// 按 Unicode 整形的嵌入子集/CFF 字体。这类文字无法用原生文字 + Shading 表达。
func textNeedsPathDrawing(face FontFace, object models.TextObject) bool {
	if face == nil {
		return true
	}
	if object.Stroke || textlayout.CharDirection(object) != 0 {
		return true
	}
	var text strings.Builder
	for _, code := range object.TextCode {
		if face.DirectPath(code.Value) != nil {
			return true
		}
		text.WriteString(code.Value)
	}
	if face.IsCFF() && !textlayout.CanRenderText(face, text.String()) {
		return true
	}
	return false
}

// textFillDisabled 判断文字对象是否明确禁止填充。
func textFillDisabled(object models.TextObject) bool {
	return !object.Fill.Value(true)
}

func (p *Document) drawTextCode(ctx DrawContext, faces *textFaces, object models.TextObject, code models.TextCode, pageHeight float64, parentCTM *models.CTM, codePosition int) {
	textlayout.WalkRuns(faces.base, object, code, codePosition, func(run textlayout.Run) {
		if run.SemanticOnly {
			p.drawSemanticText(ctx, faces.base, object, run.Value, run.X, run.Y, pageHeight, parentCTM)
			return
		}
		p.drawTextGlyph(ctx, faces, object, run.Value, run.X, run.Y, pageHeight, parentCTM)
	})
}

type invisibleTextShaper interface {
	InvisibleShapedRun(value string) TextRun
}

func (p *Document) drawSemanticText(ctx DrawContext, face FontFace, object models.TextObject, value string, x, y, pageHeight float64, parentCTM *models.CTM) {
	drawer, ok := ctx.(semanticTextDrawer)
	if !ok {
		return
	}
	shaper, ok := face.(invisibleTextShaper)
	if !ok {
		return
	}
	run := shaper.InvisibleShapedRun(value)
	if run == nil {
		return
	}
	transform := textPageTransform(object, pageHeight, parentCTM)
	if transform == nil {
		return
	}
	matrix := transform(x, y)
	if !finiteMatrix(matrix) {
		return
	}
	ctx.Push()
	ctx.Transform(matrix)
	drawer.DrawSemanticTextLine(run)
	ctx.Pop()
}

func (p *Document) drawTextGlyph(ctx DrawContext, faces *textFaces, object models.TextObject, value string, x, y, pageHeight float64, parentCTM *models.CTM) {
	face := faces.run(x, y)
	if _, semanticOnly := ctx.(semanticOnlyContext); semanticOnly {
		if !fontrepair.ContainsGlyphRune(value) {
			p.drawSemanticText(ctx, face, object, value, x, y, pageHeight, parentCTM)
		}
		return
	}
	hScale := textlayout.HScale(object)
	if !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(pageHeight) || !finiteFloat(hScale) ||
		!object.Boundary.IsFinite() || !object.CTM.IsFinite() || !parentCTM.IsFinite() {
		return
	}
	// 某些嵌入式 CFF 字体经过 OFD 子集修复后不能安全地交给 PDF
	// 子集器，因此对无法按调用文本成形的文字保留路径回退；已经注入文档
	// Unicode 映射、字体可以成形的文字走正常文字接口，以保留复制和搜索能力。
	isEmbeddedFont := p.fonts.FallbackFontFamily(object.Font) == ""
	if isEmbeddedFont && face.IsCFF() && !textlayout.CanRenderText(face, value) {
		p.drawCFFTextPath(ctx, faces, object, value, x, y, pageHeight, parentCTM, hScale)
		return
	}
	if path := face.DirectPath(value); path != nil {
		p.drawTextPath(ctx, faces, path, object, x, y, pageHeight, parentCTM, hScale)
		return
	}
	// CharDirection 需要逐字旋转，Canvas 的文字排版接口不能对单个字形
	// 应用该变换，因此退回到路径绘制。
	if textlayout.CharDirection(object) != 0 {
		if path := face.ToPath(value); path != nil && !path.Empty() {
			p.drawTextPath(ctx, faces, path, object, x, y, pageHeight, parentCTM, hScale)
			return
		}
	}
	// 只要需要描边就必须走路径绘制：Canvas 的原生文字接口用 DefaultStyle
	// 只复制 Fill（canvas/text.go 的 renderLineTo），FontFace 也没有描边字段，
	// 因此 DrawText 会静默丢弃描边。空心字与"填充+描边"文字都受此限制。
	if object.Stroke || faces.forcePath {
		if path := face.ToPath(value); path != nil && !path.Empty() {
			p.drawTextPath(ctx, faces, path, object, x, y, pageHeight, parentCTM, hScale)
			return
		}
	}
	transform := textPageTransform(object, pageHeight, parentCTM)
	if transform == nil {
		return
	}
	matrix := transform(x, y)
	if !finiteMatrix(matrix) {
		return
	}
	ctx.Push()
	ctx.Transform(matrix)
	p.drawTextInline(ctx, face, value, faces.clip)
	ctx.Pop()
}

// drawTextInline 优先使用后端原生文字绘制（语义与后端 DrawText 一致，
// 保留文字整形和 PDF 文字可复制性）。后端不支持时回退到字形轮廓路径。
func (p *Document) drawTextInline(ctx DrawContext, face FontFace, value string, clip *geom.Path) {
	if clip != nil {
		path := face.ToPath(value)
		local := localTextClip(ctx, clip)
		if path == nil || path.Empty() || local.Empty() {
			p.drawInvisibleSemanticText(ctx, face, value)
			return
		}
		if !clipContainsRect(local, path.Bounds()) {
			visible := path.Copy()
			visible.Close()
			visible = visible.And(local)
			if !visible.Empty() {
				ctx.ClearStroke()
				ctx.SetFillPaint(face.Fill())
				ctx.DrawPath(0, 0, visible)
			}
			p.drawInvisibleSemanticText(ctx, face, value)
			return
		}
	}
	if td, ok := ctx.(textDrawer); ok {
		if run := face.ShapedRun(value); run != nil {
			td.DrawTextLine(run)
		}
		return
	}
	if path := face.ToPath(value); path != nil && !path.Empty() {
		// 路径绘制使用当前填充样式：原生 DrawText 使用字体面内嵌的
		// 画笔且只填充、绝不描边（空心字由 drawTextPath 单独处理），
		// 这里同样显式套用画笔并清除残存的描边状态（例如上一条路径
		// 图元留下的描边），否则字形会被描边成更粗更黑的笔画。
		ctx.ClearStroke()
		ctx.SetFillPaint(face.Fill())
		ctx.DrawPath(0, 0, path)
	}
}

func (p *Document) drawInvisibleSemanticText(ctx DrawContext, face FontFace, value string) {
	shaper, ok := face.(invisibleTextShaper)
	if !ok {
		return
	}
	drawer, ok := ctx.(semanticTextDrawer)
	if ok {
		drawer.DrawSemanticTextLine(shaper.InvisibleShapedRun(value))
	}
}

// drawCFFTextPath 避免将修复后的裸 CFF 字体交给 PDF 字体子集器，
// 因为子集器无法安全地序列化某些嵌入式 CFF 程序。
func (p *Document) drawCFFTextPath(ctx DrawContext, faces *textFaces, object models.TextObject, value string, x, y, pageHeight float64, parentCTM *models.CTM, hScale float64) {
	face := faces.run(x, y)
	path := face.DirectPath(value)
	if path == nil {
		path = face.ToPath(value)
	}
	if path == nil || path.Empty() {
		return
	}
	p.drawTextPath(ctx, faces, path, object, x, y, pageHeight, parentCTM, hScale)
}

func (p *Document) drawTextPath(ctx DrawContext, faces *textFaces, path *geom.Path, object models.TextObject, x, y, pageHeight float64, parentCTM *models.CTM, hScale float64) {
	// runX/runY 是 run 起点在图元 Boundary 内的坐标，渐变以此还原取样空间。
	runX, runY := x, y
	face := faces.run(runX, runY)
	if path == nil || !finiteFloat(x) || !finiteFloat(y) || !finiteFloat(pageHeight) || !finiteFloat(hScale) ||
		!object.Boundary.IsFinite() || !object.CTM.IsFinite() || !parentCTM.IsFinite() {
		return
	}
	transform := textPageTransform(object, pageHeight, parentCTM)
	if transform == nil {
		return
	}
	matrix := transform(x, y)
	if !finiteMatrix(matrix) {
		return
	}
	if parentCTM != nil {
		// 该分支用上下文变换绘制路径，后端在 run 排版空间中取样渐变，
		// 与原生文字分支一致。
		p.applyTextPathFill(ctx, faces.runPaint(runX, runY, face.Fill()), object)
		ctx.Push()
		ctx.Transform(matrix)
		p.drawTextOutline(ctx, path, faces.clip, object)
		ctx.Pop()
		return
	}
	// 排版矩阵已烘焙进轮廓，后端按设备坐标取样渐变，因此画笔要用矩阵逆
	// 映回 Boundary 空间；矩阵退化时退回 run 空间画笔。
	fillPaint, ok := faces.devicePaint(matrix, faces.runPaint(runX, runY, face.Fill()))
	if !ok {
		fillPaint = faces.runPaint(runX, runY, face.Fill())
	}
	p.applyTextPathFill(ctx, fillPaint, object)
	// path 可能来自字体轮廓缓存（同一字形/字体跨对象、跨页面复用），而
	// Path.Transform 是原地修改。必须先复制，否则平移会累积到缓存对象上，
	// 后续渲染同一字形时坐标持续偏移，最终移出页面。
	p.drawTextOutline(ctx, path.Copy().Transform(matrix), faces.clip, object)
}

// applyTextPathFill 设置走路径文字的填充与描边状态。
func (p *Document) applyTextPathFill(ctx DrawContext, fillPaint geom.Paint, object models.TextObject) {
	// ToPath 只返回几何路径；与 DrawText 不同，它不会应用 FontFace 的画笔，
	// 因此需要将文字填充样式复制到路径绘制状态。
	if textFillDisabled(object) && object.Stroke {
		// 空心字：仅描边，不填充。
		ctx.ClearFill()
		return
	}
	ctx.SetFillPaint(fillPaint)
	if !object.Stroke {
		// 只有不描边的文字才清除描边。textWithBudget 已按 StrokeColor
		// 设置好描边画笔与线宽，若在此清除，"填充+描边" 文字的描边会被
		// 静默丢弃（ofd-creator/text-directions.ofd 第 3 页 ID=51 即是此情况）。
		ctx.ClearStroke()
	}
}

func finiteArray(values models.StArrayF) bool {
	for _, value := range values {
		if !finiteFloat(value) {
			return false
		}
	}
	return true
}
