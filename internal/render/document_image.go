package render

import (
	"log/slog"

	"github.com/tdewolff/canvas"
	_ "github.com/xiaoqidun/jbig2"
	"github.com/zc310/ofd/internal/models"
)

func (p *Document) Image(ctx *canvas.Context, object models.ImageObject, dp *models.DrawParam, pb models.StBox) {
	media, ok := p.Res[models.StID(object.ResourceID)]
	if !ok {
		return
	}

	img, err := p.Document.Common.FileCache.ParseImage(string(media.MediaFile.Clean()))
	if err != nil {
		slog.Error(err.Error())
		return
	}
	imgBounds := img.Bounds()
	imgW, imgH := float64(imgBounds.Dx()), float64(imgBounds.Dy())
	if imgW <= 0 || imgH <= 0 {
		return
	}

	ctx.Push()
	defer ctx.Pop()
	if object.CTM != nil {
		tx, ty := object.CTM.Transform(0.0, 0.0)
		yPos := pb.Height - (ty + object.Boundary.Y + object.Boundary.Height)
		ctx.Translate(tx+object.Boundary.X, yPos)
		ctx.Scale(object.CTM[0]/imgW, object.CTM[3]/imgH)
	} else {
		yPos := pb.Height - (object.Boundary.Y + object.Boundary.Height)
		ctx.Translate(object.Boundary.X, yPos)
		ctx.Scale(object.Boundary.Width/imgW, object.Boundary.Height/imgH)
	}

	ctx.DrawImage(0, 0, img, canvas.DPMM(1.0))
}
