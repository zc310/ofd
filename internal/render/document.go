// Package render 将解析后的 OFD 文档渲染为图形内容。
package render

import (
	"errors"
	"fmt"
	"image/color"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/content"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageasset"
	"github.com/zc310/ofd/internal/render/paint"
	"github.com/zc310/ofd/pkg/spec"
)

type Document struct {
	*parser.Document

	background         color.Color
	dpi                geom.Resolution
	fonts              FontEngine
	fontOwner          *sharedFontEngine
	fallbacks          []string
	fallbackMu         sync.RWMutex
	assets             imageasset.Store
	sealMu             sync.Mutex
	sealDocs           *cache.LRU[[32]byte, *sealDocEntry]
	sealClosed         bool
	closeOnce          sync.Once
	operationMu        sync.Mutex
	operations         sync.WaitGroup
	colors             paint.Resolver
	templateScenesMu   sync.Mutex
	templateScenes     *cache.LRU[templateSceneKey, *templateScene]
	templateBuilds     map[templateSceneKey]*templateSceneBuild
	templateGeneration uint64
	templateScopesMu   sync.Mutex
	templateScopes     *cache.LRU[templateScopeKey, *templateScopeEntry]
	// closed 在关闭开始时置位，各资源缓存持有自己的锁时检查，防止在途构建重新发布。
	closed atomic.Bool
}

const defaultRenderDPI = 96

var errDocumentClosed = errors.New("渲染文档已关闭")

// Close 释放文档持有的渲染期资源，最重要的是字体。
//
// 字体是这里最重的资源：一个 CJK 字体的轮廓约 50 MB，远大于文档本身。不显式
// 释放时，包级字体缓存会一直持有它，浏览多个文档的会话里这些内存会累积。
//
// 幂等；拒绝新操作并等待在途绘制、布局与资源访问结束后释放资源。
// 不得在本文档的绘制或遍历回调内同步调用 Close，否则会等待当前操作自身。
func (p *Document) Close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		p.operationMu.Lock()
		p.closed.Store(true)
		p.operationMu.Unlock()
		p.operations.Wait()
		p.colors.Close()
		p.assets.Close()
		p.templateScopesMu.Lock()
		if p.templateScopes != nil {
			for _, entry := range p.templateScopes.Values() {
				entry.release()
			}
			p.templateScopes = nil
		}
		p.templateScopesMu.Unlock()
		p.sealMu.Lock()
		p.sealClosed = true
		var seals []*sealDocEntry
		if p.sealDocs != nil {
			seals = p.sealDocs.Values()
		}
		p.sealDocs = nil
		p.sealMu.Unlock()
		for _, entry := range seals {
			entry.close()
		}
		if p.fontOwner != nil {
			p.fontOwner.release()
		} else if p.fonts != nil {
			p.fonts.Close()
		}
		p.templateScenesMu.Lock()
		p.templateScenes = nil
		p.templateBuilds = nil
		p.templateScenesMu.Unlock()
	})
}

// NewDocument 创建带有输出分辨率的渲染文档；dpi <= 0 时使用默认 96 DPI。
// dpi 用于离屏栅格化及记录矢量表面时的替代图像选择，不改变页面物理尺寸。
// 后续以不同分辨率 Rasterize 同一表面不会重新选择图像资源。
func NewDocument(background color.Color, doc *parser.Document, dpi geom.Resolution) *Document {
	fonts := NewFontEngine(doc)
	return newDocumentView(background, doc, dpi, &sharedFontEngine{engine: fonts, refs: 1})
}

// NewView 创建独立的 DPI/背景视图，复用同一文档的字体解析、映射和整形缓存。
// 每个视图都需 Close；最后一个视图关闭时才释放共享字体。
// 任一视图调用 UseFallbackFont/RemoveFallbackFont 都会更新共享字体引擎，影响所有
// 视图；各 Document 的 fallbacks 列表从创建时复制，之后仅由本视图的方法更新，
// 不随其他视图同步，不能将其当作共享字体引擎的实时快照。
func (p *Document) NewView(background color.Color, dpi geom.Resolution) (*Document, error) {
	if p == nil || p.fontOwner == nil {
		return nil, errors.New("文档没有可共享的字体上下文")
	}
	owner := p.fontOwner
	owner.mu.Lock()
	if p.closed.Load() || owner.refs == 0 {
		owner.mu.Unlock()
		return nil, errors.New("渲染文档已关闭")
	}
	owner.refs++
	owner.mu.Unlock()
	view := newDocumentView(background, p.Document, dpi, owner)
	view.fallbacks = p.fallbackFontFamilies()
	return view, nil
}

func newDocumentView(background color.Color, doc *parser.Document, dpi geom.Resolution, fonts *sharedFontEngine) *Document {
	if dpi <= 0 {
		dpi = geom.DPI(defaultRenderDPI)
	}
	return &Document{
		background: background,
		dpi:        dpi,
		fonts:      fonts.engine,
		fontOwner:  fonts,
		Document:   doc,
	}
}

// UseFallbackFont 使当前文档缺失字体时使用已全局注册的回退字体族。
func (p *Document) UseFallbackFont(family string) error {
	if err := p.beginOperation(); err != nil {
		return err
	}
	defer p.operations.Done()
	if p.fonts == nil {
		return fmt.Errorf("字体上下文为空")
	}
	if err := p.fonts.UseFallbackFont(family); err != nil {
		return err
	}
	p.fallbackMu.Lock()
	defer p.fallbackMu.Unlock()
	if slices.Contains(p.fallbacks, family) {
		return nil
	}
	p.fallbacks = append(p.fallbacks, family)
	return nil
}

// RemoveFallbackFont 取消先前通过 UseFallbackFont 登记的回退字体族，使缺失字体
// 恢复为内嵌或默认字体。
func (p *Document) RemoveFallbackFont(family string) {
	if p.beginOperation() != nil {
		return
	}
	defer p.operations.Done()
	if p.fonts == nil || family == "" {
		return
	}
	p.fonts.RemoveFallbackFont(family)
	p.fallbackMu.Lock()
	defer p.fallbackMu.Unlock()
	kept := p.fallbacks[:0]
	for _, value := range p.fallbacks {
		if value != family {
			kept = append(kept, value)
		}
	}
	p.fallbacks = kept
}

func (p *Document) fallbackFontFamilies() []string {
	if p == nil {
		return nil
	}
	p.fallbackMu.RLock()
	defer p.fallbackMu.RUnlock()
	families := make([]string, len(p.fallbacks))
	copy(families, p.fallbacks)
	return families
}

// FallbackFontFamily 返回为文档字体选择的外部字体族。
func (p *Document) FallbackFontFamily(id spec.StRefID) string {
	if p.beginOperation() != nil {
		return ""
	}
	defer p.operations.Done()
	if p.fonts == nil {
		return ""
	}
	return p.fonts.FallbackFontFamily(id)
}

// Background 返回文档渲染时使用的背景色。调用方可以据此判断能否复用已有的
// 渲染文档，避免为不同背景重复构建字体和图片缓存。
func (p *Document) Background() color.Color {
	if p == nil {
		return nil
	}
	return p.background
}

// HasLoadedEmbeddedFont 判断文档字体是否可以作为内嵌字体族提供给浏览器。
func (p *Document) HasLoadedEmbeddedFont(id spec.StRefID) bool {
	if p.beginOperation() != nil {
		return false
	}
	defer p.operations.Done()
	if p.fonts == nil {
		return false
	}
	return p.fonts.HasLoadedEmbeddedFont(id)
}

// annotationVisible 判断注解是否可见。OFD 未指定 Visible 时默认为可见。
func annotationVisible(annot *spec.Annot) bool {
	return content.AnnotationVisible(annot)
}

// Draw 将页面绘制到上下文；渲染文档已关闭时返回错误。
func (p *Document) Draw(ctx DrawContext, page *parser.Page) error {
	return p.drawWithReport(ctx, page, nil)
}

// DrawWithReport 绘制并返回有界诊断报告；报告只属于本次调用。
func (p *Document) DrawWithReport(ctx DrawContext, page *parser.Page) (Report, error) {
	report := &renderReportBuilder{}
	err := p.drawWithReport(ctx, page, report)
	return report.Snapshot(), err
}

func (p *Document) drawWithReport(ctx DrawContext, page *parser.Page, report *renderReportBuilder) error {
	if err := p.beginOperation(); err != nil {
		return err
	}
	defer p.operations.Done()
	lease, err := page.AcquireLease()
	if err != nil {
		return err
	}
	defer lease.Release()
	var budget renderBudget
	budget.reset()
	budget.Report = report
	if report != nil {
		report.PageID = page.ID
	}
	pageContent := lease.Content()
	if pageContent == nil {
		return errors.New("页面内容为空")
	}
	p.drawPage(ctx, page, pageContent, &budget)
	return nil
}

func (p *Document) Page(page *parser.Page) (VectorSurface, error) {
	return p.PageWithVisibility(page, PageVisibility{})
}

// PageVisibility 是一次页面输出的过滤设置；正文层与模板引用分别标识，避免 ID 混淆。
// 同一模板在其他页面的引用不受影响。
type PageVisibility = content.Visibility

// PageWithVisibility 过滤目标页正文图层及模板引用，保留注解和签章。
// 过滤状态属于本次绘制，不修改共享页面或文档渲染上下文。
// 渲染文档已关闭时返回错误。
func (p *Document) PageWithVisibility(page *parser.Page, visibility PageVisibility) (VectorSurface, error) {
	return p.pageWithReport(page, visibility, nil)
}

// PageWithReport 创建矢量页面并返回绘制诊断，不调用用户回调或持有共享报告。
func (p *Document) PageWithReport(page *parser.Page, visibility PageVisibility) (VectorSurface, Report, error) {
	report := &renderReportBuilder{}
	surface, err := p.pageWithReport(page, visibility, report)
	return surface, report.Snapshot(), err
}

func (p *Document) pageWithReport(page *parser.Page, visibility PageVisibility, report *renderReportBuilder) (VectorSurface, error) {
	if err := p.beginOperation(); err != nil {
		return nil, err
	}
	defer p.operations.Done()
	lease, err := page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	var budget renderBudget
	budget.reset()
	budget.visibility = visibility
	budget.Report = report
	if report != nil {
		report.PageID = page.ID
	}
	pageContent := lease.Content()
	if pageContent == nil {
		return nil, errors.New("页面内容为空")
	}
	factory := drawing.VectorSurfaceFactory()
	if factory == nil {
		return nil, errors.New("未注册矢量表面工厂（请空白导入 internal/render/backends/canvas 或注册自定义工厂）")
	}
	box := pageContent.Area.PhysicalBox
	return factory(box.Width, box.Height, func(ctx DrawContext) {
		p.drawPage(ctx, page, pageContent, &budget)
	}), nil
}

// drawPage 绘制页面背景及全部内容，供 Draw 与 Page 复用。
func (p *Document) drawPage(ctx DrawContext, page *parser.Page, pageContent *models.PageContent, budget *renderBudget) {
	p.drawPageBackground(ctx, pageContent.Area.PhysicalBox)
	p.pageContent(ctx, page, true, budget)
}

// drawPageBackground 绘制页面背景。
func (p *Document) drawPageBackground(ctx DrawContext, box spec.StBox) {
	ctx.SetFillColor(p.background)
	// 页面尺寸换算成栅格像素通常不是整数，栅格画布会向上取整。只填充页面
	// 大小会让最后一行/列只被部分覆盖，留下半透明边缘；在深色阅读背景上
	// 会显示为黑线。向四周多填充一个设备像素，超出画布的部分会被裁掉。
	padding := 0.0
	if p.dpi.DPMM() > 0 {
		padding = 1.0 / p.dpi.DPMM()
	}
	ctx.DrawPath(-padding, -padding, geom.Rectangle(box.Width+2*padding, box.Height+2*padding))
}

func (p *Document) PageContent(ctx DrawContext, page *parser.Page, seal bool) {
	if p.beginOperation() != nil {
		return
	}
	defer p.operations.Done()
	var budget renderBudget
	budget.reset()
	p.pageContent(ctx, page, seal, &budget)
}

func (p *Document) pageContent(ctx DrawContext, page *parser.Page, seal bool, budget *renderBudget) {
	if page == nil {
		return
	}
	lease, err := page.AcquireLease()
	if err != nil {
		budget.diagnose("page_load_failed", err.Error(), 0)
		return
	}
	defer lease.Release()
	pageContent := lease.Content()
	if pageContent == nil || pageContent.Area == nil {
		budget.diagnose("page_content_invalid", "页面缺少内容或物理区域", 0)
		return
	}
	// 页面专属资源可能改变模板引用的字体、图片或绘制参数，不跨页复用。
	budget.templateCacheable = len(pageContent.PageRes) == 0
	// 登记本页（含模板与注释）的 Unicode→字形映射，使缺少 Unicode cmap 的
	// 内嵌子集字体也能按原始文本渲染，保留 PDF 文字可复制性。
	if p.fonts != nil {
		p.fonts.RegisterPageGlyphs(p.Document, page, pageContent)
	}
	pb := pageContent.Area.PhysicalBox
	// 显示区域与图元共用左上原点、Y 向下的页面坐标。
	budget.pageClip = content.ApplicationClip(pageContent.Area)
	if budget.pageClip != nil {
		if budget.pageClip.Empty() {
			return
		}
		if clipContainsRect(budget.pageClip, geom.RectFromSize(0, 0, pb.Width, pb.Height)) {
			// 页面表面自身已限制物理尺寸，整页显示区无需让模板、SVG 和文字降级。
			budget.pageClip = nil
		} else {
			budget.templateCacheable = false
		}
	}
	annots, annotErr := p.LoadAnnotation(page.ID)
	if annotErr != nil {
		budget.diagnose("annotation_load_failed", annotErr.Error(), 0)
	}
	for _, template := range pageContent.Template {
		if !budget.visibility.HiddenTemplates[spec.StID(template.TemplateID)] && !p.TemplateForeground(template) {
			p.template(ctx, template, pb, budget)
		}
	}
	// Watermark 水印注解位于背景模板之后、页面内容之前。
	if annots != nil {
		for _, item := range annots.Annots {
			if item != nil && item.Type == spec.AnnotTypeWatermark {
				p.annotWithBudget(ctx, item, pb, budget)
			}
		}
	}
	if pageContent.Content != nil {
		layers := pageContent.Content.Layer
		if len(budget.visibility.HiddenLayers) > 0 {
			layers = visiblePageLayers(layers, budget.visibility.HiddenLayers)
		}
		p.drawLayerType(ctx, layers, spec.LayerBackground, pb, budget)
		p.drawLayerType(ctx, layers, spec.LayerBody, pb, budget)
	}
	for _, template := range pageContent.Template {
		if !budget.visibility.HiddenTemplates[spec.StID(template.TemplateID)] && p.TemplateForeground(template) {
			p.template(ctx, template, pb, budget)
		}
	}
	if pageContent.Content != nil {
		p.drawLayerType(ctx, visiblePageLayers(pageContent.Content.Layer, budget.visibility.HiddenLayers), spec.LayerForeground, pb, budget)
	}
	if seal {
		p.drawSealsWithBudget(ctx, page.ID, pb, budget)
	}

	// 其余类型注解（印章、链接、路径、高亮等）绘制在最上层。
	if annots != nil {
		for _, item := range annots.Annots {
			if item != nil && item.Type != spec.AnnotTypeWatermark {
				p.annotWithBudget(ctx, item, pb, budget)
			}
		}
	}
}

func visiblePageLayers(layers []*spec.Layer, hidden map[spec.StID]bool) []*spec.Layer {
	return content.VisibleLayers(layers, hidden)
}

func (p *Document) Template(ctx DrawContext, template spec.Template, pb spec.StBox) {
	if p.beginOperation() != nil {
		return
	}
	defer p.operations.Done()
	var budget renderBudget
	budget.reset()
	p.template(ctx, template, pb, &budget)
}

// TemplateForeground 按页面引用、模板声明、默认 Background 的顺序确定叠放位置。
func (p *Document) TemplateForeground(template spec.Template) bool {
	order := template.ZOrder
	if order == "" {
		for _, definition := range p.Document.CommonData.TemplatePages {
			if definition.ID == spec.StID(template.TemplateID) {
				order = definition.ZOrder
				break
			}
		}
	}
	return order == "Foreground"
}

func (p *Document) template(ctx DrawContext, template spec.Template, pb spec.StBox, budget *renderBudget) {
	templateContent, scope, release, err := p.templateScope(spec.StID(template.TemplateID))
	if err != nil {
		slog.Debug("读取模板页失败", "template_id", template.TemplateID, "error", err)
		budget.diagnose("template_load_failed", err.Error(), spec.StID(template.TemplateID))
		return
	}
	defer release()
	if templateContent != nil && templateContent.Content != nil {
		if scope.drawCachedTemplate(ctx, templateContent, pb, budget) {
			return
		}
		scope.drawLayersWithBudget(ctx, templateContent.Content.Layer, pb, budget)
	}
}

func (p *Document) drawLayersWithBudget(ctx DrawContext, layers []*spec.Layer, pb spec.StBox, budget *renderBudget) {
	if len(layers) == 0 {
		return
	}
	for _, kind := range []string{spec.LayerBackground, spec.LayerBody, spec.LayerForeground} {
		p.drawLayerType(ctx, layers, kind, pb, budget)
	}
}

func (p *Document) drawLayerType(ctx DrawContext, layers []*spec.Layer, kind string, pb spec.StBox, budget *renderBudget) {
	for _, layer := range layers {
		if layer != nil && content.LayerType(layer) == kind {
			p.layer(ctx, layer, pb, budget)
		}
	}
}

func (p *Document) drawSealsWithBudget(ctx DrawContext, pageID spec.StID, pb spec.StBox, budget *renderBudget) {
	for _, info := range p.Document.GetSeals(pageID) {
		if err := p.sealWithBudget(ctx, info, pb, budget); err != nil {
			slog.Debug("绘制印章失败", "page_id", pageID, "error", err)
			budget.diagnose("seal_failed", err.Error(), 0)
		}
	}
}

func (p *Document) Layer(ctx DrawContext, layer *spec.Layer, pb spec.StBox) {
	if p.beginOperation() != nil {
		return
	}
	defer p.operations.Done()
	var budget renderBudget
	budget.reset()
	p.layer(ctx, layer, pb, &budget)
}

func (p *Document) layer(ctx DrawContext, layer *spec.Layer, pb spec.StBox, budget *renderBudget) {
	if layer == nil {
		return
	}
	var dp *spec.DrawParam
	if layer.DrawParam > 0 {
		dp = p.GetDrawParam(spec.StID(layer.DrawParam))
	}
	p.drawItems(ctx, layer.Items, dp, pb, budget)
}

// drawItems 按文档顺序绘制页面块中的图形对象。
func (p *Document) drawItems(ctx DrawContext, items []spec.PageItem, dp *spec.DrawParam, pb spec.StBox, budget *renderBudget) {
	p.drawItemsWithTransform(ctx, items, dp, pb, nil, budget.pageClip, 0, budget)
}

func (p *Document) drawItemsWithTransform(ctx DrawContext, items []spec.PageItem, dp *spec.DrawParam, pb spec.StBox, parentCTM *spec.CTM, parentClip *geom.Path, compositeDepth int, budget *renderBudget) {
	if parentCTM != nil && !parentCTM.IsFinite() {
		return
	}
	for _, item := range items {
		if !pageItemPresent(item) {
			budget.diagnose("missing_object", "图元种类缺少对应载荷", 0)
			continue
		}
		oldObject, oldResource := budget.ObjectID, budget.ResourceID
		budget.ObjectID = pageItemID(item)
		budget.ResourceID = 0
		if budget.Report != nil {
			budget.Report.Stats.ObjectsVisited++
		}
		switch item.Kind {
		case spec.PageItemPath:
			p.pathWithBudget(ctx, *item.Path, p.objectDrawParam(item.Path.DrawParam, dp), pb, parentCTM, parentClip, budget)
		case spec.PageItemImage:
			p.imageWithBudget(ctx, *item.Image, p.objectDrawParam(item.Image.DrawParam, dp), pb, parentCTM, parentClip, budget)
		case spec.PageItemText:
			p.textWithBudget(ctx, *item.Text, p.objectDrawParam(item.Text.DrawParam, dp), pb, parentCTM, parentClip, budget)
		case spec.PageItemBlock:
			p.drawItemsWithTransform(ctx, item.Block.Items, dp, pb, parentCTM, parentClip, compositeDepth, budget)
		case spec.PageItemComposite:
			p.compositeWithBudget(ctx, *item.Composite, p.objectDrawParam(item.Composite.DrawParam, dp), pb, parentCTM, parentClip, compositeDepth, budget)
		}
		budget.ObjectID, budget.ResourceID = oldObject, oldResource
	}
}

func (p *Document) objectDrawParam(id spec.StRefID, inherited *spec.DrawParam) *spec.DrawParam {
	if id > 0 {
		if dp := p.GetDrawParam(spec.StID(id)); dp != nil {
			return dp
		}
	}
	return inherited
}

func (p *Document) Annot(ctx DrawContext, annot *spec.Annot, pb spec.StBox) {
	if p.beginOperation() != nil {
		return
	}
	defer p.operations.Done()
	p.annot(ctx, annot, pb)
}

func (p *Document) annot(ctx DrawContext, annot *spec.Annot, pb spec.StBox) {
	p.annotWithBudget(ctx, annot, pb, nil)
}

func (p *Document) annotWithBudget(ctx DrawContext, annot *spec.Annot, pb spec.StBox, parent *renderBudget) {
	if !annotationVisible(annot) || annot.Appearance == nil || annot.Appearance.Boundary == nil {
		return
	}
	// 页面内的所有注解和正文共享额度；独立 Annot 调用才新建预算。
	budget := parent
	if budget == nil {
		budget = &renderBudget{}
		budget.reset()
	}
	box := *annot.Appearance.Boundary
	highlight := annot.Type == spec.AnnotTypeHighlight
	for _, item := range annot.Appearance.Items {
		p.annotItem(ctx, item, box, pb, highlight, budget)
	}
}

// annotItem 绘制注解外观中的一个图元。外观内各图元的 Boundary 相对
// Appearance.Boundary 左上角，这里按 box 平移后再绘制；复合图元（如手写签名
// 的矢量笔迹）与嵌套块同样要绘制，不能像以前那样被直接跳过。
// budget 由 annot 创建并在整个外观的图元与嵌套块之间共享。
func (p *Document) annotItem(ctx DrawContext, item spec.PageItem, box spec.StBox, pb spec.StBox, highlight bool, budget *renderBudget) {
	if !pageItemPresent(item) {
		return
	}
	oldObject, oldResource := budget.ObjectID, budget.ResourceID
	budget.ObjectID = pageItemID(item)
	budget.ResourceID = 0
	defer func() { budget.ObjectID, budget.ResourceID = oldObject, oldResource }()
	if budget.Report != nil {
		budget.Report.Stats.ObjectsVisited++
	}
	switch item.Kind {
	case spec.PageItemImage:
		object := *item.Image
		object.Boundary = object.Boundary.CopyAndShift(&box)
		object.Alpha = highlightAnnotationAlpha(object.Alpha, highlight)
		p.imageWithBudget(ctx, object, p.objectDrawParam(object.DrawParam, nil), pb, nil, budget.pageClip, budget)
	case spec.PageItemPath:
		object := *item.Path
		object.Boundary = object.Boundary.CopyAndShift(&box)
		object.Alpha = highlightAnnotationAlpha(object.Alpha, highlight)
		p.pathWithBudget(ctx, object, p.objectDrawParam(object.DrawParam, nil), pb, nil, budget.pageClip, budget)
	case spec.PageItemText:
		object := *item.Text
		object.Boundary = object.Boundary.CopyAndShift(&box)
		object.Alpha = highlightAnnotationAlpha(object.Alpha, highlight)
		p.textWithBudget(ctx, object, p.objectDrawParam(object.DrawParam, nil), pb, nil, budget.pageClip, budget)
	case spec.PageItemComposite:
		object := *item.Composite
		object.Boundary = object.Boundary.CopyAndShift(&box)
		object.Alpha = highlightAnnotationAlpha(object.Alpha, highlight)
		p.compositeWithBudget(ctx, object, p.objectDrawParam(object.DrawParam, nil), pb, nil, budget.pageClip, 0, budget)
	case spec.PageItemBlock:
		if item.Block == nil {
			return
		}
		// 块与子图元共用父级坐标系，因此仍以同一个 box 平移。
		for _, child := range item.Block.Items {
			p.annotItem(ctx, child, box, pb, highlight, budget)
		}
	}
}

// pageItemPresent 检查图元种类与载荷一致，缺失对象不参与绘制或文字收集。
func pageItemPresent(item spec.PageItem) bool {
	return content.Present(item)
}

func pageItemID(item spec.PageItem) spec.StID {
	return content.ItemID(item)
}

func (p *Document) checkOpen() error {
	if p == nil || p.closed.Load() {
		return errDocumentClosed
	}
	return nil
}

// beginOperation 只用于入口，内部嵌套绘制沿用外层租约。
// Add 与关闭标志在同一锁内，保证 Wait 开始后不会再增加在途操作。
func (p *Document) beginOperation() error {
	if p == nil {
		return errDocumentClosed
	}
	p.operationMu.Lock()
	defer p.operationMu.Unlock()
	if err := p.checkOpen(); err != nil {
		return err
	}
	p.operations.Add(1)
	return nil
}

// Highlight 注解的外观默认要透出正文；显式设置的较低不透明度保持不变。
func highlightAnnotationAlpha(alpha *uint8, highlight bool) *uint8 {
	if !highlight || (alpha != nil && *alpha != 255) {
		return alpha
	}
	defaultOpacity := uint8(128)
	return &defaultOpacity
}
