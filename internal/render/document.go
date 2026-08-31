package render

import (
	"image/color"
	"log/slog"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

type Document struct {
	*parser.Document
	background color.Color
	fonts      *Fonts
}

func NewDocument(background color.Color, doc *parser.Document) *Document {
	return &Document{background: background, fonts: NewFonts(doc), Document: doc}
}

func (p *Document) Draw(ctx *canvas.Context, page *parser.Page) error {
	p.drawPageBackground(ctx, page.Area.PhysicalBox)
	p.PageContent(ctx, page, true)
	return nil
}

func (p *Document) Page(page *parser.Page) (*canvas.Canvas, error) {
	box := page.Area.PhysicalBox
	c := canvas.New(box.Width, box.Height)
	ctx := canvas.NewContext(c)
	p.drawPageBackground(ctx, box)
	p.PageContent(ctx, page, true)
	return c, nil
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
	p.drawPageBlocks(ctx, layer.PageBlock, dp, pb)
	p.drawObjects(ctx, layer.PathObject, layer.ImageObject, layer.TextObject, dp, pb)
}

// drawPageBlocks 递归绘制 PageBlock，保持子块先于当前块的顺序。
func (p *Document) drawPageBlocks(ctx *canvas.Context, blocks []models.PageBlock, dp *models.DrawParam, pb models.StBox) {
	for _, block := range blocks {
		p.drawPageBlocks(ctx, block.PageBlock, dp, pb)
		p.drawObjects(ctx, block.PathObject, block.ImageObject, block.TextObject, dp, pb)
	}
}

// drawObjects 按路径、图片、文字的顺序绘制图形对象。
func (p *Document) drawObjects(ctx *canvas.Context, paths []models.PathObject, images []models.ImageObject, texts []models.TextObject, dp *models.DrawParam, pb models.StBox) {
	for _, object := range paths {
		p.Path(ctx, object, dp, pb)
	}
	for _, object := range images {
		p.Image(ctx, object, dp, pb)
	}
	for _, object := range texts {
		p.Text(ctx, object, dp, pb)
	}
}

func (p *Document) Annot(ctx *canvas.Context, annot *models.Annot, pb models.StBox) {
	if annot == nil || annot.Appearance == nil || annot.Appearance.Boundary == nil {
		return
	}
	box := *annot.Appearance.Boundary
	for _, object := range annot.Appearance.ImageObject {
		object.Boundary = object.Boundary.CopyAndShift(&box)
		p.Image(ctx, object, nil, pb)
	}
	for _, object := range annot.Appearance.PathObject {
		object.Boundary = object.Boundary.CopyAndShift(&box)
		p.Path(ctx, object, nil, pb)
	}
	for _, object := range annot.Appearance.TextObject {
		object.Boundary = object.Boundary.CopyAndShift(&box)
		p.Text(ctx, object, nil, pb)
	}
}
