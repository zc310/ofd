package render

import (
	"image/color"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/paint"
)

// CTColor 是解析后的颜色画笔，与后端共享 paint.Color。
type CTColor = drawing.CTColor

type colorResources struct {
	*parser.Document
}

func (r colorResources) ReadColorProfile(name string) ([]byte, error) {
	if r.FileCache == nil {
		return nil, nil
	}
	return r.FileCache.Read(name)
}

func (p *Document) colorResources() paint.Resources {
	if p.Document == nil {
		return nil
	}
	return colorResources{p.Document}
}

func (p *Document) colorRGBA(source models.CTColor) color.RGBA {
	return p.colors.RGBA(source, p.colorResources())
}

func (p *Document) updateCtColor(source *models.CTColor) *CTColor {
	return p.colors.Resolve(source, p.colorResources())
}

func (p *Document) pathGradient(source *models.CTColor, transform func(models.StPos) geom.Point, area *geom.Rect) geom.Gradient {
	return p.colors.Gradient(source, transform, area, p.colorResources())
}

func (p *Document) textShadingGradient(source *models.CTColor, objectAlpha *uint8) geom.Gradient {
	return p.colors.TextGradient(source, objectAlpha, p.colorResources())
}

func isAxialRadialShading(source *models.CTColor) bool {
	return source != nil && (source.AxialShd != nil || source.RadialShd != nil)
}
