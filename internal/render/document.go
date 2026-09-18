// Package render 将解析后的 OFD 文档渲染为图形内容。
package render

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"math"
	"slices"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/utils"
)

type Document struct {
	*parser.Document
	background   color.Color
	dpi          canvas.Resolution
	fonts        *Fonts
	fallbacks    []string
	fallbackMu   sync.RWMutex
	imageLocksMu sync.Mutex
	imageLocks   map[string]*imageKeyLock
	svgMu        sync.Mutex
	images       *utils.LRU[string, image.Image]
	svgCanvases  *utils.LRU[string, *canvas.Canvas]
}

type imageKeyLock struct {
	mu   sync.Mutex
	refs int
}

const (
	maxCompositeExpansions         = 10000
	maxCompositeResourceExpansions = 256
	maxPatternTilesPerRender       = 100000
	maxOffscreenPixels             = 200000000
)

type renderBudget struct {
	compositeExpansions int
	compositeResources  map[models.StID]int
	patternTiles        int
	offscreenPixels     float64
}

func (b *renderBudget) reset() {
	b.compositeExpansions = 0

	b.compositeResources = make(map[models.StID]int)
	b.patternTiles = 0
	b.offscreenPixels = 0
}

func (b *renderBudget) allowComposite(resourceID models.StID) bool {
	if b.compositeResources == nil {
		b.compositeResources = make(map[models.StID]int)
	}
	if b.compositeExpansions >= maxCompositeExpansions || b.compositeResources[resourceID] >= maxCompositeResourceExpansions {
		return false
	}
	b.compositeExpansions++
	b.compositeResources[resourceID]++
	return true
}

func (b *renderBudget) allowPatternTiles(count int) bool {
	if count < 0 || b.patternTiles > maxPatternTilesPerRender-count {
		return false
	}
	b.patternTiles += count
	return true
}

func (b *renderBudget) allowOffscreenPixels(width, height, dpi float64) bool {
	if width <= 0 || height <= 0 || dpi <= 0 || math.IsNaN(width) || math.IsInf(width, 0) ||
		math.IsNaN(height) || math.IsInf(height, 0) || math.IsNaN(dpi) || math.IsInf(dpi, 0) {
		return false
	}
	pixels := width * height * canvas.DPI(dpi).DPMM() * canvas.DPI(dpi).DPMM()
	if math.IsNaN(pixels) || math.IsInf(pixels, 0) || b.offscreenPixels > maxOffscreenPixels-pixels {
		return false
	}
	b.offscreenPixels += pixels
	return true
}

const (
	imageCacheCapacity = 128
	imageCacheBytes    = 64 << 20
	svgCacheCapacity   = 64
	svgCacheBytes      = 16 << 20
	defaultRenderDPI   = 96
)

func NewDocument(background color.Color, doc *parser.Document) *Document {
	return NewDocumentWithDPI(background, doc, canvas.DPI(defaultRenderDPI))
}

// NewDocumentWithDPI 创建带有输出分辨率的渲染文档。
// dpi 用于复合图元等内部离屏栅格化，不改变页面的物理尺寸。
func NewDocumentWithDPI(background color.Color, doc *parser.Document, dpi canvas.Resolution) *Document {
	if dpi <= 0 {
		dpi = canvas.DPI(defaultRenderDPI)
	}
	return &Document{
		background:  background,
		dpi:         dpi,
		fonts:       NewFonts(doc),
		Document:    doc,
		images:      utils.NewWeightedLRU[string, image.Image](imageCacheCapacity, imageCacheBytes, nil),
		svgCanvases: utils.NewWeightedLRU[string, *canvas.Canvas](svgCacheCapacity, svgCacheBytes, nil),
		imageLocks:  make(map[string]*imageKeyLock),
	}
}

func (p *Document) imageLock(key string) *imageKeyLock {
	p.imageLocksMu.Lock()
	defer p.imageLocksMu.Unlock()
	if p.imageLocks == nil {
		p.imageLocks = make(map[string]*imageKeyLock)
	}
	if lock := p.imageLocks[key]; lock != nil {
		lock.refs++
		return lock
	}
	lock := &imageKeyLock{refs: 1}
	p.imageLocks[key] = lock
	return lock
}

func (p *Document) releaseImageLock(key string, lock *imageKeyLock) {
	p.imageLocksMu.Lock()
	defer p.imageLocksMu.Unlock()
	if lock.refs > 0 {
		lock.refs--
	}
	if lock.refs == 0 && p.imageLocks[key] == lock {
		delete(p.imageLocks, key)
	}
}

// UseFallbackFont 使当前文档缺失字体时使用已全局注册的回退字体族。
func (p *Document) UseFallbackFont(family string) error {
	if p == nil || p.fonts == nil {
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
func (p *Document) FallbackFontFamily(id models.StRefID) string {
	if p == nil || p.fonts == nil {
		return ""
	}
	return p.fonts.FallbackFontFamily(id)
}

// HasLoadedEmbeddedFont 判断文档字体是否可以作为内嵌字体族提供给浏览器。
func (p *Document) HasLoadedEmbeddedFont(id models.StRefID) bool {
	if p == nil || p.fonts == nil {
		return false
	}
	return p.fonts.HasLoadedEmbeddedFont(id)
}

// annotationVisible 判断注解是否可见。OFD 未指定 Visible 时默认为可见。
func annotationVisible(annot *models.Annot) bool {
	return annot != nil && annot.Visible.Value(true)
}

func (p *Document) Draw(ctx *canvas.Context, page *parser.Page) error {
	lease, err := page.AcquireLease()
	if err != nil {
		return err
	}
	defer lease.Release()
	var budget renderBudget
	budget.reset()
	content := lease.Content()
	if content == nil {
		return errors.New("页面内容为空")
	}
	p.drawPage(ctx, page, content, &budget)
	return nil
}

func (p *Document) Page(page *parser.Page) (*canvas.Canvas, error) {
	lease, err := page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	var budget renderBudget
	budget.reset()
	content := lease.Content()
	if content == nil {
		return nil, errors.New("页面内容为空")
	}
	box := content.Area.PhysicalBox
	c := canvas.New(box.Width, box.Height)
	p.drawPage(canvas.NewContext(c), page, content, &budget)
	return c, nil
}

// drawPage 绘制页面背景及全部内容，供 Draw 与 Page 复用。
func (p *Document) drawPage(ctx *canvas.Context, page *parser.Page, content *models.PageContent, budget *renderBudget) {
	p.drawPageBackground(ctx, content.Area.PhysicalBox)
	p.pageContent(ctx, page, true, budget)
}

// drawPageBackground 绘制页面背景。
func (p *Document) drawPageBackground(ctx *canvas.Context, box models.StBox) {
	ctx.SetFillColor(p.background)
	ctx.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))
}

func (p *Document) PageContent(ctx *canvas.Context, page *parser.Page, seal bool) {
	var budget renderBudget
	budget.reset()
	p.pageContent(ctx, page, seal, &budget)
}

func (p *Document) pageContent(ctx *canvas.Context, page *parser.Page, seal bool, budget *renderBudget) {
	if page == nil {
		return
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil {
		return
	}
	pb := content.Area.PhysicalBox
	for _, template := range content.Template {
		p.template(ctx, template, pb, budget)
	}

	if content.Content != nil {
		p.drawLayersWithBudget(ctx, content.Content.Layer, pb, budget)
	}
	if seal {
		p.drawSeals(ctx, page.ID, pb)
	}

	if annot := p.Document.GetAnnotation(page.ID); annot != nil {
		for _, item := range annot.Annots {
			p.Annot(ctx, item, pb)
		}
	}
}

func (p *Document) Template(ctx *canvas.Context, template models.Template, pb models.StBox) {
	var budget renderBudget
	budget.reset()
	p.template(ctx, template, pb, &budget)
}

func (p *Document) template(ctx *canvas.Context, template models.Template, pb models.StBox, budget *renderBudget) {
	content, err := p.Document.LoadTemplate(models.StID(template.TemplateID))
	if err != nil {
		slog.Warn("读取模板页失败", "template_id", template.TemplateID, "error", err)
		return
	}
	if content != nil && content.Content != nil {
		p.drawLayersWithBudget(ctx, content.Content.Layer, pb, budget)
	}
}

// drawLayers 先绘制背景层，再绘制其他图层。
func (p *Document) drawLayers(ctx *canvas.Context, layers []*models.Layer, pb models.StBox) {
	var budget renderBudget
	budget.reset()
	p.drawLayersWithBudget(ctx, layers, pb, &budget)
}

func (p *Document) drawLayersWithBudget(ctx *canvas.Context, layers []*models.Layer, pb models.StBox, budget *renderBudget) {
	if len(layers) == 0 {
		return
	}
	for _, layer := range layers {
		if layer != nil && layer.Type == "Background" {
			p.layer(ctx, layer, pb, budget)
		}
	}
	for _, layer := range layers {
		if layer != nil && layer.Type != "Background" {
			p.layer(ctx, layer, pb, budget)
		}
	}
}

// drawSeals 绘制当前页面上的电子印章。
func (p *Document) drawSeals(ctx *canvas.Context, pageID models.StID, pb models.StBox) {
	for _, info := range p.Document.GetSeals(pageID) {
		if err := p.Seal(ctx, info, pb); err != nil {
			slog.Error(err.Error())
		}
	}
}

func (p *Document) Layer(ctx *canvas.Context, layer *models.Layer, pb models.StBox) {
	var budget renderBudget
	budget.reset()
	p.layer(ctx, layer, pb, &budget)
}

func (p *Document) layer(ctx *canvas.Context, layer *models.Layer, pb models.StBox, budget *renderBudget) {
	if layer == nil {
		return
	}
	var dp *models.DrawParam
	if layer.DrawParam > 0 {
		dp = p.Document.GetDrawParam(models.StID(layer.DrawParam))
	}
	p.drawItems(ctx, layer.Items, dp, pb, budget)
}

// drawItems 按文档顺序绘制页面块中的图形对象。
func (p *Document) drawItems(ctx *canvas.Context, items []models.PageItem, dp *models.DrawParam, pb models.StBox, budget *renderBudget) {
	p.drawItemsWithTransform(ctx, items, dp, pb, nil, nil, 0, budget)
}

func (p *Document) drawItemsWithTransform(ctx *canvas.Context, items []models.PageItem, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *canvas.Path, compositeDepth int, budget *renderBudget) {
	if parentCTM != nil && !parentCTM.IsFinite() {
		return
	}
	for _, item := range items {
		switch item.Kind {
		case models.PageItemPath:
			p.pathWithBudget(ctx, item.Path, p.objectDrawParam(item.Path.DrawParam, dp), pb, parentCTM, parentClip, budget)
		case models.PageItemImage:
			p.image(ctx, item.Image, p.objectDrawParam(item.Image.DrawParam, dp), pb, parentCTM, parentClip)
		case models.PageItemText:
			p.textWithBudget(ctx, item.Text, p.objectDrawParam(item.Text.DrawParam, dp), pb, parentCTM, parentClip, budget)
		case models.PageItemBlock:
			p.drawItemsWithTransform(ctx, item.Block.Items, dp, pb, parentCTM, parentClip, compositeDepth, budget)
		case models.PageItemComposite:
			p.compositeWithBudget(ctx, item.Composite, p.objectDrawParam(item.Composite.DrawParam, dp), pb, parentCTM, parentClip, compositeDepth, budget)
		}
	}
}

func (p *Document) objectDrawParam(id models.StRefID, inherited *models.DrawParam) *models.DrawParam {
	if id > 0 {
		if dp := p.Document.GetDrawParam(models.StID(id)); dp != nil {
			return dp
		}
	}
	return inherited
}

// drawPageBlock 递归绘制 PageBlock，保持子块先于当前块的顺序。
func (p *Document) drawPageBlock(ctx *canvas.Context, block models.PageBlock, dp *models.DrawParam, pb models.StBox) {
	var budget renderBudget
	budget.reset()
	p.drawItems(ctx, block.Items, dp, pb, &budget)
}

func (p *Document) Annot(ctx *canvas.Context, annot *models.Annot, pb models.StBox) {
	if !annotationVisible(annot) || annot.Appearance == nil || annot.Appearance.Boundary == nil {
		return
	}
	box := *annot.Appearance.Boundary
	for _, item := range annot.Appearance.Items {
		switch item.Kind {
		case models.PageItemImage:
			object := item.Image
			object.Boundary = object.Boundary.CopyAndShift(&box)
			p.Image(ctx, object, p.objectDrawParam(object.DrawParam, nil), pb)
		case models.PageItemPath:
			object := item.Path
			object.Boundary = object.Boundary.CopyAndShift(&box)
			p.Path(ctx, object, p.objectDrawParam(object.DrawParam, nil), pb)
		case models.PageItemText:
			object := item.Text
			object.Boundary = object.Boundary.CopyAndShift(&box)
			p.Text(ctx, object, p.objectDrawParam(object.DrawParam, nil), pb)
		case models.PageItemComposite, models.PageItemBlock:
		}
	}
}
