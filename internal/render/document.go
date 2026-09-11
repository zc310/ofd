// Package render 将解析后的 OFD 文档渲染为图形内容。
package render

import (
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"math"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

type Document struct {
	*parser.Document
	background  color.Color
	fonts       *Fonts
	fallbacks   []fallbackFontResource
	renderMu    sync.Mutex
	budget      renderBudget
	imageMu     sync.Mutex
	images      *lruCache[string, image.Image]
	svgCanvases *lruCache[string, *canvas.Canvas]
}

type fallbackFontResource struct {
	data   []byte
	family string
	style  canvas.FontStyle
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
	svgCacheCapacity   = 64
)

func NewDocument(background color.Color, doc *parser.Document) *Document {
	return &Document{
		background:  background,
		fonts:       NewFonts(doc),
		Document:    doc,
		images:      newLRU[string, image.Image](imageCacheCapacity),
		svgCanvases: newLRU[string, *canvas.Canvas](svgCacheCapacity),
	}
}

// AddFallbackFont 注册在文档字体未内嵌时使用的字体。
func (p *Document) AddFallbackFont(data []byte, family string, style canvas.FontStyle) error {
	if p == nil || p.fonts == nil {
		return fmt.Errorf("字体上下文为空")
	}
	if err := p.fonts.AddFallbackFont(data, family, style); err != nil {
		return err
	}
	for index, fallback := range p.fallbacks {
		if fallback.family == family && fallback.style == style {
			p.fallbacks[index].data = data
			return nil
		}
	}
	p.fallbacks = append(p.fallbacks, fallbackFontResource{data: data, family: family, style: style})
	return nil
}

func (p *Document) fallbackFontResources() []fallbackFontResource {
	if p == nil {
		return nil
	}
	return p.fallbacks
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
	p.renderMu.Lock()
	defer p.renderMu.Unlock()
	p.budget.reset()
	page.EnsurePhysicalBox()
	p.drawPage(ctx, page)
	return nil
}

func (p *Document) Page(page *parser.Page) (*canvas.Canvas, error) {
	p.renderMu.Lock()
	defer p.renderMu.Unlock()
	p.budget.reset()
	page.EnsurePhysicalBox()
	box := page.Area.PhysicalBox
	c := canvas.New(box.Width, box.Height)
	p.drawPage(canvas.NewContext(c), page)
	return c, nil
}

// drawPage 绘制页面背景及全部内容，供 Draw 与 Page 复用。
func (p *Document) drawPage(ctx *canvas.Context, page *parser.Page) {
	p.drawPageBackground(ctx, page.Area.PhysicalBox)
	p.PageContent(ctx, page, true)
}

// drawPageBackground 绘制页面背景。
func (p *Document) drawPageBackground(ctx *canvas.Context, box models.StBox) {
	ctx.SetFillColor(p.background)
	ctx.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))
}

func (p *Document) PageContent(ctx *canvas.Context, page *parser.Page, seal bool) {
	pb := page.Area.PhysicalBox
	for _, template := range page.Template {
		p.Template(ctx, template, pb)
	}

	if page.Content != nil {
		p.drawLayers(ctx, page.Content.Layer, pb)
	}
	if seal {
		p.drawSeals(ctx, page.ID, pb)
	}

	if annot := p.Document.Annotations[page.ID]; annot != nil {
		for _, item := range annot.Annots {
			p.Annot(ctx, item, pb)
		}
	}
}

func (p *Document) Template(ctx *canvas.Context, template models.Template, pb models.StBox) {
	content := p.Templates[models.StID(template.TemplateID)]
	if content != nil && content.Content != nil {
		p.drawLayers(ctx, content.Content.Layer, pb)
	}
}

// drawLayers 先绘制背景层，再绘制其他图层。
func (p *Document) drawLayers(ctx *canvas.Context, layers []*models.Layer, pb models.StBox) {
	if len(layers) == 0 {
		return
	}
	for _, layer := range layers {
		if layer != nil && layer.Type == "Background" {
			p.Layer(ctx, layer, pb)
		}
	}
	for _, layer := range layers {
		if layer != nil && layer.Type != "Background" {
			p.Layer(ctx, layer, pb)
		}
	}
}

// drawSeals 绘制当前页面上的电子印章。
func (p *Document) drawSeals(ctx *canvas.Context, pageID models.StID, pb models.StBox) {
	for _, info := range p.Document.Seals[pageID] {
		if err := p.Seal(ctx, info, pb); err != nil {
			slog.Error(err.Error())
		}
	}
}

func (p *Document) Layer(ctx *canvas.Context, layer *models.Layer, pb models.StBox) {
	if layer == nil {
		return
	}
	var dp *models.DrawParam
	if layer.DrawParam > 0 {
		dp = p.Document.GetDrawParam(models.StID(layer.DrawParam))
	}
	p.drawItems(ctx, layer.Items, dp, pb)
}

// drawItems 按文档顺序绘制页面块中的图形对象。
func (p *Document) drawItems(ctx *canvas.Context, items []models.PageItem, dp *models.DrawParam, pb models.StBox) {
	p.drawItemsWithTransform(ctx, items, dp, pb, nil, nil, 0)
}

func (p *Document) drawItemsWithTransform(ctx *canvas.Context, items []models.PageItem, dp *models.DrawParam, pb models.StBox, parentCTM *models.CTM, parentClip *canvas.Path, compositeDepth int) {
	if parentCTM != nil && !parentCTM.IsFinite() {
		return
	}
	for _, item := range items {
		switch item.Kind {
		case models.PageItemPath:
			p.path(ctx, item.Path, p.objectDrawParam(item.Path.DrawParam, dp), pb, parentCTM, parentClip)
		case models.PageItemImage:
			p.image(ctx, item.Image, p.objectDrawParam(item.Image.DrawParam, dp), pb, parentCTM, parentClip)
		case models.PageItemText:
			p.text(ctx, item.Text, p.objectDrawParam(item.Text.DrawParam, dp), pb, parentCTM, parentClip)
		case models.PageItemBlock:
			p.drawItemsWithTransform(ctx, item.Block.Items, dp, pb, parentCTM, parentClip, compositeDepth)
		case models.PageItemComposite:
			p.composite(ctx, item.Composite, p.objectDrawParam(item.Composite.DrawParam, dp), pb, parentCTM, parentClip, compositeDepth)
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
	p.drawItems(ctx, block.Items, dp, pb)
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
