package canvas

import (
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/drawing"
)

func init() {
	render.RegisterFallbackFontFactory(
		func(data []byte, family string, style drawing.FontStyle) error {
			return registerFallbackFont(data, family, style)
		},
		fallbackFontData,
	)
}
