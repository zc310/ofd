package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

// TextSource 标记文字的来源，不把底纹中的装饰文字作为正文收集。
type TextSource string

const (
	TextSourcePage       TextSource = "page"
	TextSourceTemplate   TextSource = "template"
	TextSourceAnnotation TextSource = "annotation"
	TextSourceWatermark  TextSource = "watermark"
	TextSourceSeal       TextSource = "seal"
)

// TextOptions 控制附加文字来源。正文、模板及其复合对象始终收集。
// 印章只收集 OFD 子文档里的真实文字，不对图片或轮廓执行 OCR。
type TextOptions struct {
	IncludeAnnotations bool
	IncludeWatermarks  bool
	IncludeSeals       bool
}

type contentFrame struct {
	document    *Document
	box         models.StBox
	matrix      geom.Matrix // 当前 OFD 左上原点坐标到目标页面的 OFD 坐标。
	clip        *geom.Path  // 目标页面的 OFD 坐标。
	source      TextSource
	dp          *models.DrawParam
	depth       int
	inComposite bool
	budget      *renderBudget
}

type contentVisitor func(contentFrame, models.PageItem)

func (p *Document) walkPageText(page *parser.Page, visibility PageVisibility, options TextOptions, visit contentVisitor) error {
	if p == nil || p.Document == nil || page == nil {
		return fmt.Errorf("文字文档或页面为空")
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return err
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil {
		return fmt.Errorf("页面内容为空")
	}
	if p.fonts != nil {
		p.fonts.RegisterPageGlyphs(p.Document, page, content)
	}
	var budget renderBudget
	budget.reset()
	frame := contentFrame{document: p, box: content.Area.PhysicalBox, matrix: geom.Identity, source: TextSourcePage, budget: &budget}
	var annotations *models.PageAnnot
	if options.IncludeAnnotations || options.IncludeWatermarks {
		annotations, err = p.LoadAnnotation(page.ID)
		if err != nil {
			return fmt.Errorf("加载页面 %d 注解失败: %w", page.ID, err)
		}
	}
	for _, foreground := range []bool{false, true} {
		if foreground {
			if options.IncludeWatermarks {
				p.walkAnnotationText(frame, annotations, true, visit)
			}
			if content.Content != nil {
				p.walkContentLayers(frame, visiblePageLayers(content.Content.Layer, visibility.HiddenLayers), visit)
			}
		}
		for _, ref := range content.Template {
			if visibility.HiddenTemplates[models.StID(ref.TemplateID)] || p.TemplateForeground(ref) != foreground {
				continue
			}
			template, err := p.LoadTemplate(models.StID(ref.TemplateID))
			if err != nil {
				return fmt.Errorf("加载模板 %d 失败: %w", ref.TemplateID, err)
			}
			if template != nil && template.Content != nil {
				templateFrame := frame
				templateFrame.source = TextSourceTemplate
				p.walkContentLayers(templateFrame, template.Content.Layer, visit)
			}
		}
	}
	if options.IncludeSeals {
		for _, seal := range p.Document.GetSeals(page.ID) {
			if seal == nil || seal.SealData == nil || seal.StampAnnot == nil || !strings.EqualFold(seal.SealData.FileType, "ofd") {
				continue
			}
			if err := p.walkSealText(frame, seal, visit); err != nil {
				return err
			}
		}
	}
	if options.IncludeAnnotations {
		p.walkAnnotationText(frame, annotations, false, visit)
	}
	return nil
}

func (p *Document) walkContentLayers(frame contentFrame, layers []*models.Layer, visit contentVisitor) {
	for _, background := range []bool{true, false} {
		for _, layer := range layers {
			if layer == nil || (layer.Type == spec.LayerBackground) != background {
				continue
			}
			child := frame
			if layer.DrawParam > 0 {
				child.dp = p.GetDrawParam(models.StID(layer.DrawParam))
			}
			p.walkContentItems(child, layer.Items, visit)
		}
	}
}

func (p *Document) walkContentItems(frame contentFrame, items []models.PageItem, visit contentVisitor) {
	if frame.depth >= maxCompositeDepth || (frame.clip != nil && frame.clip.Empty()) {
		return
	}
	for _, item := range items {
		if !pageItemPresent(item) {
			continue
		}
		if item.Kind == models.PageItemBlock {
			child := frame
			child.depth++
			p.walkContentItems(child, item.Block.Items, visit)
			continue
		}
		itemFrame := frame
		if item.Kind == models.PageItemText && item.Text.Clips != nil {
			clip := p.graphicClip(item.Text.CTGraphicUnit, frame.box.Height, nil, nil)
			itemFrame.clip = intersectClips(frame.clip, frame.ofdClip(clip))
			if itemFrame.clip != nil && itemFrame.clip.Empty() {
				continue
			}
		}
		visit(itemFrame, item)
		if item.Kind != models.PageItemComposite || !item.Composite.VisibleValue() || graphicOpacity(item.Composite.Alpha) == 0 {
			continue
		}
		object := *item.Composite
		unit := p.GetCompositeUnit(models.StID(object.ResourceID))
		if unit == nil || !frame.budget.allowComposite(models.StID(object.ResourceID)) {
			continue
		}
		dp := p.objectDrawParam(object.DrawParam, frame.dp)
		source, w, h, ok := p.compositeContentSource(object, unit, dp, frame.depth, frame.budget)
		if !ok {
			continue
		}
		sx, sy := object.Boundary.Width/source.Width, object.Boundary.Height/source.Height
		mapping := geom.Matrix{{sx, 0, object.Boundary.X - source.X*sx}, {0, sy, object.Boundary.Y - source.Y*sy}}
		child := frame
		child.matrix = frame.matrix.Mul(mapping)
		if !finiteMatrix(child.matrix) {
			continue
		}
		child.box = models.StBox{Width: w, Height: h}
		child.dp = dp
		child.depth++
		child.inComposite = true
		boundary := geom.Rectangle(object.Boundary.Width, object.Boundary.Height).Translate(object.Boundary.X, object.Boundary.Y).Transform(frame.matrix)
		child.clip = intersectClips(frame.clip, boundary)
		if object.Clips != nil {
			ctm := models.IdentityMatrix
			if object.CTM != nil {
				ctm = *object.CTM
			}
			clip := p.buildImageClip(object.Clips, frame.box.Height, object.Boundary.X, object.Boundary.Y, ctm)
			child.clip = intersectClips(child.clip, frame.ofdClip(clip))
		}
		p.walkContentItems(child, unit.Content.Items, visit)
	}
}

func (f contentFrame) ofdClip(path *geom.Path) *geom.Path {
	if path == nil {
		return nil
	}
	return path.Copy().Transform(f.matrix.Mul(geom.Matrix{{1, 0, 0}, {0, -1, f.box.Height}}))
}

// 复用现有绘制分支的尺寸兼容与墨迹采样。§13 表50的单元坐标先映射到 Boundary；
// 对需要裁掉透明边的兼容文档，文字框和链接也必须使用同一裁边范围。
func (p *Document) compositeContentSource(object models.CompositeObject, unit *models.CompositeGraphicUnit, dp *models.DrawParam, depth int, budget *renderBudget) (models.StBox, float64, float64, bool) {
	w, h := unit.Width, unit.Height
	if !object.Boundary.IsFinite() || !object.CTM.IsFinite() || w <= 0 || h <= 0 || !finiteFloat(w) || !finiteFloat(h) || object.Boundary.Width <= 0 || object.Boundary.Height <= 0 {
		return models.StBox{}, 0, 0, false
	}
	if ew, eh, ok := compositeContentExtent(object); ok && (compositeExtentMismatch(w, ew) || compositeExtentMismatch(h, eh)) {
		return models.StBox{Width: ew, Height: eh}, ew, eh, true
	}
	if len(unit.Content.Items) == 1 && unit.Content.Items[0].Path != nil && simpleCompositePath(*unit.Content.Items[0].Path) {
		b := p.buildObjectPathWithTransform(*unit.Content.Items[0].Path, h, nil).Bounds()
		return models.StBox{X: b.X0, Y: h - b.Y1, Width: b.W(), Height: b.H()}, w, h, !b.Empty() && b.W() > 0 && b.H() > 0
	}
	dpi := min(1200, max(10, p.dpi.DPI()*object.Boundary.Width/w))
	if !budget.allowOffscreenPixels(w, h, dpi) {
		return models.StBox{}, 0, 0, false
	}
	raster := p.rasterizeCompositeUnit(unit, dp, models.StBox{Width: w, Height: h}, false, w, h, dpi, depth, budget)
	if compositeRasterBlank(raster) {
		// 隐形热区或OCR文字可以没有可见墨迹。此时无透明边可供测量，沿用
		// 完整单元坐标，仍保留它们的动作和文字语义。
		return models.StBox{Width: w, Height: h}, w, h, true
	}
	if _, source, ok := p.resampleCompositeUnitSource(unit, dp, raster, w, h, dpi, object.Boundary, p.dpi.DPI(), depth, budget); ok {
		return source, w, h, true
	}
	x0, y0, x1, y1 := contentImageBounds(raster)
	step := 25.4 / dpi
	return models.StBox{X: x0 * step, Y: y0 * step, Width: (x1 - x0) * step, Height: (y1 - y0) * step}, w, h, true
}

func (p *Document) walkAnnotationText(frame contentFrame, annotations *models.PageAnnot, watermark bool, visit contentVisitor) {
	if annotations == nil {
		return
	}
	for _, annotation := range annotations.Annots {
		if !annotationVisible(annotation) || annotation.Appearance == nil || annotation.Appearance.Boundary == nil || (annotation.Type == models.AnnotTypeWatermark) != watermark {
			continue
		}
		box := *annotation.Appearance.Boundary
		if !box.IsFinite() {
			continue
		}
		child := frame
		child.source = TextSourceAnnotation
		if watermark {
			child.source = TextSourceWatermark
		}
		child.matrix = frame.matrix.Translate(box.X, box.Y)
		p.walkContentItems(child, annotation.Appearance.Items, visit)
	}
}

func (p *Document) walkSealText(frame contentFrame, info *parser.SealInfo, visit contentVisitor) error {
	entry, err := p.loadSealDocument(info.SealData.Data, true)
	if err != nil || entry == nil {
		return err
	}
	defer entry.mu.RUnlock()
	page, err := entry.ofd.Documents[0].GetPage(0)
	if err != nil {
		return err
	}
	box, err := page.PhysicalBox()
	if err != nil {
		return err
	}
	stamp := info.StampAnnot.Boundary
	if !box.IsFinite() || box.Width <= 0 || box.Height <= 0 || !stamp.IsFinite() || stamp.Width <= 0 || stamp.Height <= 0 {
		return nil
	}
	mapping := geom.Identity.Translate(stamp.X, stamp.Y).Scale(stamp.Width/box.Width, stamp.Height/box.Height)
	var clip *geom.Path
	if crop, ok := sealClipBox(stamp, info.StampAnnot.Clip); ok {
		clip = geom.Rectangle(crop.Width, crop.Height).Translate(crop.X, crop.Y)
	}
	return entry.doc.walkPageText(page, PageVisibility{}, TextOptions{}, func(child contentFrame, item models.PageItem) {
		child.matrix = mapping.Mul(child.matrix)
		child.source = TextSourceSeal
		if child.clip != nil {
			child.clip = child.clip.Copy().Transform(mapping)
		}
		child.clip = intersectClips(child.clip, clip)
		visit(child, item)
	})
}

func transformTextLayout(layout TextLayout, frame contentFrame) TextLayout {
	mapBox := func(x, y, w, h float64) geom.Rect {
		return geom.RectFromSize(x, y, w, h).Transform(frame.matrix)
	}
	layout.Source = frame.source
	bounds := mapBox(layout.X, layout.Y, layout.Width, layout.Height)
	layout.X, layout.Y, layout.Width, layout.Height = bounds.X0, bounds.Y0, bounds.W(), bounds.H()
	layout.Size *= math.Hypot(frame.matrix[0][1], frame.matrix[1][1])
	rotation := math.Atan2(frame.matrix[1][0], frame.matrix[0][0]) * 180 / math.Pi
	for i := range layout.Glyphs {
		g := &layout.Glyphs[i]
		r := mapBox(g.X, g.Y, g.Width, g.Height)
		if frame.clip != nil {
			intersection := geom.Rectangle(r.W(), r.H()).Translate(r.X0, r.Y0).And(frame.clip)
			if intersection.Empty() {
				g.Width, g.Height = 0, 0
				continue
			}
			r = intersection.Bounds()
		}
		g.X, g.Y, g.Width, g.Height = r.X0, r.Y0, r.W(), r.H()
		g.Angle += rotation
	}
	if frame.source == TextSourceSeal {
		layout.Font = 0
	}
	return layout
}
