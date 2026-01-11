package render

import (
	"bytes"
	"image"
	"image/color"

	"github.com/h2non/filetype"
	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func (p *Document) Seal(ctx *canvas.Context, info *parser.SealInfo, pb models.StBox) error {
	if filetype.IsImage(info.SealData.Data) {
		img, _, err := image.Decode(bytes.NewBuffer(info.SealData.Data))
		if err != nil {
			return err
		}
		imgBounds := img.Bounds()
		imgW, imgH := float64(imgBounds.Dx()), float64(imgBounds.Dy())

		ctx.Push()
		defer ctx.Pop()

		ctx.Translate(info.StampAnnot.Boundary.X, pb.Height-(info.StampAnnot.Boundary.Y+info.StampAnnot.Boundary.Height))
		ctx.Scale(info.StampAnnot.Boundary.Width/imgW, info.StampAnnot.Boundary.Height/imgH)
		ctx.DrawImage(0, 0, img, canvas.DPMM(1.0))
		return nil
	}
	if info.SealData.FileType == "ofd" {
		var ofd parser.OFD
		if err := ofd.Open(info.SealData.Data); err != nil {
			return err
		}
		defer ofd.Close()
		if len(ofd.Documents) == 0 || len(ofd.Documents[0].Pages) == 0 {
			return nil
		}
		doc := NewDocument(color.Transparent, ofd.Documents[0])
		tmp := p.fonts
		defer func() {
			p.fonts = tmp
		}()
		p.fonts = doc.fonts
		ctx.Push()
		defer ctx.Pop()
		sealBox := ofd.Documents[0].Pages[0].PageContent.Area.PhysicalBox
		ctx.Translate(info.StampAnnot.Boundary.X, pb.Height-(info.StampAnnot.Boundary.Y+info.StampAnnot.Boundary.Height))
		ctx.Scale(info.StampAnnot.Boundary.Width/sealBox.Width, info.StampAnnot.Boundary.Height/sealBox.Height)
		p.PageContent(ctx, ofd.Documents[0].Pages[0], false)
	}
	return nil
}
