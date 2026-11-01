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

// WalkPage 按 GB/T 33190—2016 §7.7 图16遍历模板与页面图层，再访问印章和批注。
// page 和 annotations 必须在调用期间保持有效，seals 同步访问已固定的印章子文档。
func WalkPage[S PageResources](frame Frame[S], page *models.PageContent, annotations *models.PageAnnot, visibility Visibility, options Options, seals func(Frame[S], Visitor[S]) error, visit Visitor[S]) error {
	frame.Clip = geom.IntersectClips(frame.Clip, ApplicationClip(page.Area))
	for _, foreground := range []bool{false, true} {
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
				release := func() {}
				if frame.TemplateScope != nil {
					child.Scope, release, err = frame.TemplateScope(models.StID(ref.TemplateID))
					if err != nil {
						return fmt.Errorf("绑定模板 %d 资源失败: %w", ref.TemplateID, err)
					}
				}
				if len(template.PageRes) != 0 {
					child.TemplateID = uint64(ref.TemplateID)
				}
				WalkLayers(child, template.Content.Layer, visit)
				release()
			}
		}
		if !foreground && options.IncludeWatermarks {
			WalkAnnotations(frame, annotations, true, visit)
		}
		if page.Content != nil {
			layers := VisibleLayers(page.Content.Layer, visibility.HiddenLayers)
			for _, kind := range []string{spec.LayerBackground, spec.LayerBody, spec.LayerForeground} {
				if (kind == spec.LayerForeground) == foreground {
					WalkLayerType(frame, layers, kind, visit)
				}
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

// LayerType 将缺省和自定义图层归入正文层，保留同类图层的文档顺序。
func LayerType(layer *models.Layer) string {
	if layer.Type == spec.LayerBackground || layer.Type == spec.LayerForeground {
		return layer.Type
	}
	return spec.LayerBody
}

// WalkLayers 按背景、正文、前景遍历图层，保留同类图层和图元的原始顺序。
func WalkLayers[S Resources](frame Frame[S], layers []*models.Layer, visit Visitor[S]) {
	for _, kind := range []string{spec.LayerBackground, spec.LayerBody, spec.LayerForeground} {
		WalkLayerType(frame, layers, kind, visit)
	}
}

// WalkLayerType 只遍历指定类别的图层，供模板与页面图层交错使用。
func WalkLayerType[S Resources](frame Frame[S], layers []*models.Layer, kind string, visit Visitor[S]) {
	for _, layer := range layers {
		if layer == nil || LayerType(layer) != kind {
			continue
		}
		child := frame
		if layer.DrawParam > 0 {
			child.DrawParam = frame.Scope.GetDrawParam(models.StID(layer.DrawParam))
		}
		WalkItems(child, layer.Items, visit)
	}
}

// ApplicationClip 按 §7.5 表7返回显示区域与物理页面的交集（左上原点）。
// 缺省不裁剪；空交集必须保留空路径，不能回退为整页可见。
func ApplicationClip(area *models.CtPageArea) *geom.Path {
	if area == nil || area.ApplicationBox == nil {
		return nil
	}
	box := *area.ApplicationBox
	if !box.IsFinite() || box.Width <= 0 || box.Height <= 0 {
		return &geom.Path{}
	}
	x0, y0 := max(0, box.X), max(0, box.Y)
	x1, y1 := min(area.PhysicalBox.Width, box.X+box.Width), min(area.PhysicalBox.Height, box.Y+box.Height)
	if x1 <= x0 || y1 <= y0 {
		return &geom.Path{}
	}
	return geom.Rectangle(x1-x0, y1-y0).Translate(x0, y0)
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
		if frame.Clip != nil {
			var box models.StBox
			switch item.Kind {
			case models.PageItemText:
				box = item.Text.Boundary
			case models.PageItemPath:
				box = item.Path.Boundary
			case models.PageItemImage:
				box = item.Image.Boundary
			case models.PageItemComposite:
				box = item.Composite.Boundary
			case models.PageItemBlock:
				continue
			}
			if box.IsFinite() && box.Width > 0 && box.Height > 0 {
				boundary := geom.Rectangle(box.Width, box.Height).Translate(box.X, box.Y).Transform(frame.Matrix)
				if boundary.And(frame.Clip).Empty() {
					continue
				}
			}
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
