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
	box := page.Area.PhysicalBox
	ctx.SetFillColor(p.background)
	ctx.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))

	p.PageContent(ctx, page, true)
	return nil
}

func (p *Document) Page(page *parser.Page) (*canvas.Canvas, error) {
	box := page.Area.PhysicalBox
	c := canvas.New(box.Width, box.Height)
	ctx := canvas.NewContext(c)
	ctx.SetFillColor(p.background)
	ctx.DrawPath(0, 0, canvas.Rectangle(box.Width, box.Height))

	p.PageContent(ctx, page, true)

	return c, nil
}
func (p *Document) PageContent(ctx *canvas.Context, page *parser.Page, seal bool) {
	pb := page.Area.PhysicalBox
	for _, template := range page.Template {
		p.Template(ctx, template, pb)
	}

	var backgroundLayers, otherLayers []*models.Layer
	for _, layer := range page.Content.Layer {
		if layer.Type == "Background" {
			backgroundLayers = append(backgroundLayers, layer)
		} else {
			otherLayers = append(otherLayers, layer)
		}
	}
	for _, layer := range backgroundLayers {
		p.Layer(ctx, layer, pb)
	}
	for _, layer := range otherLayers {
		p.Layer(ctx, layer, pb)
	}
	if seal {
		sealInfos := p.Document.Seals[page.ID]
		var err error
		if len(sealInfos) > 0 {
			for _, info := range sealInfos {
				if err = p.Seal(ctx, info, page.Area.PhysicalBox); err != nil {
					slog.Error(err.Error())
				}
			}
		}
	}

	annot := p.Document.Annotations[page.ID]
	if annot != nil {
		for _, a := range annot.Annots {
			p.Annot(ctx, a, pb)
		}

	}

}

func (p *Document) Template(ctx *canvas.Context, template models.Template, pb models.StBox) {
	content := p.Templates[models.StID(template.TemplateID)]
	var backgroundLayers, otherLayers []*models.Layer
	for _, layer := range content.Content.Layer {
		if layer.Type == "Background" {
			backgroundLayers = append(backgroundLayers, layer)
		} else {
			otherLayers = append(otherLayers, layer)
		}
	}
	for _, layer := range backgroundLayers {
		p.Layer(ctx, layer, pb)
	}
	for _, layer := range otherLayers {
		p.Layer(ctx, layer, pb)
	}
}

func (p *Document) Layer(ctx *canvas.Context, layer *models.Layer, pb models.StBox) {
	var dp *models.DrawParam
	if layer.DrawParam > 0 {
		dp = p.Document.GetDrawParam(models.StID(layer.DrawParam))
	}
	var blockF func([]models.PageBlock)
	blockF = func(pBlock []models.PageBlock) {
		for _, block := range pBlock {
			if len(block.PageBlock) > 0 {
				blockF(block.PageBlock)
			}
			for _, object := range block.ImageObject {
				p.Image(ctx, object, dp, pb)
			}
			for _, object := range block.PathObject {
				p.Path(ctx, object, dp, pb)
			}

			for _, object := range block.TextObject {
				p.Text(ctx, object, dp, pb)
			}

		}
	}
	blockF(layer.PageBlock)

	for _, object := range layer.ImageObject {
		p.Image(ctx, object, dp, pb)
	}
	for _, object := range layer.PathObject {
		p.Path(ctx, object, dp, pb)
	}

	for _, object := range layer.TextObject {
		p.Text(ctx, object, dp, pb)
	}

}

func (p *Document) Annot(ctx *canvas.Context, annot *models.Annot, pb models.StBox) {
	if annot.Appearance == nil {
		return
	}
	for _, object := range annot.Appearance.ImageObject {
		box := object.Boundary
		object.Boundary = box.CopyAndShift(annot.Appearance.Boundary)
		p.Image(ctx, object, nil, pb)
		object.Boundary = box
	}
	for _, object := range annot.Appearance.PathObject {
		box := object.Boundary
		object.Boundary = box.CopyAndShift(annot.Appearance.Boundary)
		p.Path(ctx, object, nil, pb)
		object.Boundary = box
	}
	for _, object := range annot.Appearance.TextObject {
		box := object.Boundary
		object.Boundary = box.CopyAndShift(annot.Appearance.Boundary)
		p.Text(ctx, object, nil, pb)
		object.Boundary = box
	}

}
