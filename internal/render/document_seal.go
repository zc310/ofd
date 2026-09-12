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
	if info == nil || info.SealData == nil || info.StampAnnot == nil {
		return nil
	}
	if filetype.IsImage(info.SealData.Data) {
		return p.drawImageSeal(ctx, info, pb)
	}
	if info.SealData.FileType == "ofd" {
		return p.drawOFDSeal(ctx, info, pb)
	}
	return nil
}

// drawImageSeal 解码并绘制图片格式的印章。
func (p *Document) drawImageSeal(ctx *canvas.Context, info *parser.SealInfo, pb models.StBox) error {
	img, _, err := image.Decode(bytes.NewReader(info.SealData.Data))
	if err != nil {
		return err
	}
	imgBounds := img.Bounds()
	if imgBounds.Empty() {
		return nil
	}

	box := info.StampAnnot.Boundary
	ctx.Push()
	defer ctx.Pop()
	ctx.Translate(box.X, pb.Height-(box.Y+box.Height))
	ctx.Scale(box.Width/float64(imgBounds.Dx()), box.Height/float64(imgBounds.Dy()))
	ctx.DrawImage(0, 0, img, canvas.DPMM(1))
	return nil
}

// drawOFDSeal 解码并绘制 OFD 格式的印章。
func (p *Document) drawOFDSeal(ctx *canvas.Context, info *parser.SealInfo, pb models.StBox) error {
	var ofd parser.OFD
	if err := ofd.Open(info.SealData.Data); err != nil {
		return err
	}
	defer ofd.Close()
	if len(ofd.Documents) == 0 || ofd.Documents[0].PageCount() == 0 {
		return nil
	}

	doc := NewDocument(color.Transparent, ofd.Documents[0])
	for _, fallback := range p.fallbackFontResources() {
		if err := doc.AddFallbackFont(fallback.data, fallback.family, fallback.style); err != nil {
			return err
		}
	}

	page, err := ofd.Documents[0].GetPage(0)
	if err != nil {
		return err
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return err
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil || content.Area == nil {
		return nil
	}
	sealBox := content.Area.PhysicalBox
	if sealBox.Width <= 0 || sealBox.Height <= 0 {
		return nil
	}
	box := info.StampAnnot.Boundary
	ctx.Push()
	defer ctx.Pop()
	ctx.Translate(box.X, pb.Height-(box.Y+box.Height))
	ctx.Scale(box.Width/sealBox.Width, box.Height/sealBox.Height)
	doc.PageContent(ctx, page, false)
	return nil
}
