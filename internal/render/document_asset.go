package render

import (
	"image"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/imageasset"
)

func (p *Document) imageResources() imageasset.Reader {
	if p.Document == nil || p.FileCache == nil {
		return nil
	}
	return p.FileCache
}

func (p *Document) decodeImage(file models.StLoc, format string) (image.Image, error) {
	return p.assets.Load(file, format, p.imageResources(), drawing.SVGSceneFactory())
}

func (p *Document) decodeSVGScene(file models.StLoc, format string) (SVGScene, error) {
	return p.assets.LoadSVG(file, format, p.imageResources(), drawing.SVGSceneFactory())
}

func (p *Document) cachedImage(key string, load func() (image.Image, error)) (image.Image, error) {
	return p.assets.Image(key, load)
}

func (p *Document) cachedSVGScene(key string, load func() ([]byte, error)) (SVGScene, error) {
	return p.assets.SVG(key, load, drawing.SVGSceneFactory())
}
