package content

import (
	"fmt"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
)

// PageResources 追加模板加载及叠放位置查询，返回的内容在遍历期间必须保持只读。
type PageResources interface {
	Resources
	LoadTemplate(id models.StID) (*models.PageContent, error)
	TemplateForeground(template models.Template) bool
}

// WalkPage 按背景模板、水印、正文、前景模板、印章和其他批注的顺序遍历。
// page 和 annotations 必须在调用期间保持有效，seals 同步访问已固定的印章子文档。
func WalkPage[S PageResources](frame Frame[S], page *models.PageContent, annotations *models.PageAnnot, visibility Visibility, options Options, seals func(Frame[S], Visitor[S]) error, visit Visitor[S]) error {
	for _, foreground := range []bool{false, true} {
		if foreground {
			if options.IncludeWatermarks {
				WalkAnnotations(frame, annotations, true, visit)
			}
			if page.Content != nil {
				WalkLayers(frame, VisibleLayers(page.Content.Layer, visibility.HiddenLayers), visit)
			}
		}
		for _, ref := range page.Template {
			if visibility.HiddenTemplates[models.StID(ref.TemplateID)] || frame.Scope.TemplateForeground(ref) != foreground {
				continue
			}
			template, err := frame.Scope.LoadTemplate(models.StID(ref.TemplateID))
			if err != nil {
				return fmt.Errorf("加载模板 %d 失败: %w", ref.TemplateID, err)
			}
			if template != nil && template.Content != nil {
				child := frame
				child.Source = SourceTemplate
				WalkLayers(child, template.Content.Layer, visit)
			}
		}
	}
	if options.IncludeSeals && seals != nil {
		if err := seals(frame, visit); err != nil {
			return err
		}
	}
	if options.IncludeAnnotations {
		WalkAnnotations(frame, annotations, false, visit)
	}
	return nil
}

// WalkLayers 先遍历背景图层，再遍历其他图层，保留图层内的图元顺序。
func WalkLayers[S Resources](frame Frame[S], layers []*models.Layer, visit Visitor[S]) {
	for _, background := range []bool{true, false} {
		for _, layer := range layers {
			if layer == nil || (layer.Type == spec.LayerBackground) != background {
				continue
			}
			child := frame
			if layer.DrawParam > 0 {
				child.DrawParam = frame.Scope.GetDrawParam(models.StID(layer.DrawParam))
			}
			WalkItems(child, layer.Items, visit)
		}
	}
}

// WalkItems 展开块和可见复合图元，传递单元坐标映射及父级裁剪。
// 不可见图元自身仍可携带链接热区，访问后由消费方决定是否收集文字。
func WalkItems[S Resources](frame Frame[S], items []models.PageItem, visit Visitor[S]) {
	if frame.Depth >= MaxDepth || (frame.Clip != nil && frame.Clip.Empty()) {
		return
	}
	for _, item := range items {
		if !Present(item) {
			continue
		}
		if item.Kind == models.PageItemBlock {
			child := frame
			child.Depth++
			WalkItems(child, item.Block.Items, visit)
			continue
		}
		itemFrame := frame
		if item.Kind == models.PageItemText && item.Text.Clips != nil {
			clip := frame.Scope.TextClip(item.Text.CTGraphicUnit, frame.Box.Height)
			itemFrame.Clip = geom.IntersectClips(frame.Clip, frame.OFDClip(clip))
			if itemFrame.Clip != nil && itemFrame.Clip.Empty() {
				continue
			}
		}
		visit(itemFrame, item)
		if item.Kind != models.PageItemComposite || !item.Composite.VisibleValue() || (item.Composite.Alpha != nil && *item.Composite.Alpha == 0) {
			continue
		}
		object := *item.Composite
		unit, ok := frame.Scope.ResolveComposite(object, frame.DrawParam, frame.Depth)
		if !ok {
			continue
		}
		mapping, ok := MapBox(unit.Source, object.Boundary)
		if !ok {
			continue
		}
		child := frame
		child.Matrix = frame.Matrix.Mul(mapping)
		if !finiteMatrix(child.Matrix) {
			continue
		}
		child.Box = unit.Box
		child.DrawParam = unit.DrawParam
		child.Depth++
		child.InComposite = true
		boundary := geom.Rectangle(object.Boundary.Width, object.Boundary.Height).Translate(object.Boundary.X, object.Boundary.Y).Transform(frame.Matrix)
		child.Clip = geom.IntersectClips(frame.Clip, boundary)
		if object.Clips != nil {
			clip := frame.Scope.CompositeClip(object, frame.Box.Height)
			child.Clip = geom.IntersectClips(child.Clip, frame.OFDClip(clip))
		}
		WalkItems(child, unit.Items, visit)
	}
}

// WalkAnnotations 按来源筛选批注，并将 Appearance 局部坐标平移到目标页。
func WalkAnnotations[S Resources](frame Frame[S], annotations *models.PageAnnot, watermark bool, visit Visitor[S]) {
	if annotations == nil {
		return
	}
	for _, annotation := range annotations.Annots {
		if !AnnotationVisible(annotation) || annotation.Appearance == nil || annotation.Appearance.Boundary == nil || (annotation.Type == models.AnnotTypeWatermark) != watermark {
			continue
		}
		box := *annotation.Appearance.Boundary
		if !box.IsFinite() {
			continue
		}
		child := frame
		child.Source = SourceAnnotation
		if watermark {
			child.Source = SourceWatermark
		}
		child.Matrix = frame.Matrix.Translate(box.X, box.Y)
		WalkItems(child, annotation.Appearance.Items, visit)
	}
}

// VisibleLayers 返回未隐藏的非空图层，不修改原切片。
func VisibleLayers(layers []*models.Layer, hidden map[models.StID]bool) []*models.Layer {
	visible := make([]*models.Layer, 0, len(layers))
	for _, layer := range layers {
		if layer != nil && !hidden[layer.ID] {
			visible = append(visible, layer)
		}
	}
	return visible
}

// AnnotationVisible 判断批注是否可见，未指定 Visible 时默认为可见。
func AnnotationVisible(annot *models.Annot) bool {
	return annot != nil && annot.Visible.Value(true)
}

// Present 检查图元种类与载荷一致，缺失对象不参与绘制或内容收集。
func Present(item models.PageItem) bool {
	switch item.Kind {
	case models.PageItemPath:
		return item.Path != nil
	case models.PageItemImage:
		return item.Image != nil
	case models.PageItemText:
		return item.Text != nil
	case models.PageItemBlock:
		return item.Block != nil
	case models.PageItemComposite:
		return item.Composite != nil
	}
	return false
}

// ItemID 返回有效图元的对象 ID，块和缺失载荷返回 0。
func ItemID(item models.PageItem) models.StID {
	if !Present(item) {
		return 0
	}
	switch item.Kind {
	case models.PageItemText:
		return item.Text.ID
	case models.PageItemPath:
		return item.Path.ID
	case models.PageItemImage:
		return item.Image.ID
	case models.PageItemComposite:
		return item.Composite.ID
	}
	return 0
}
