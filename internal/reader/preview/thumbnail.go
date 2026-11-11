package preview

import (
	"bytes"
	"context"
	"image"
	"image/png"

	"golang.org/x/image/draw"
)

// Thumbnail 是长边不超过 256 像素的 PNG，Width/Height 保留原图尺寸。
type Thumbnail struct {
	MIME   string `json:"mime"`
	Data   []byte `json:"data"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// MakeThumbnail 按光栅预览的输入安全限制完整解码并等比例缩小图片，不放大小图。
func MakeThumbnail(ctx context.Context, data []byte) (Thumbnail, error) {
	if _, _, err := rasterConfig(data); err != nil {
		return Thumbnail{}, err
	}
	if err := ctx.Err(); err != nil {
		return Thumbnail{}, err
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Thumbnail{}, err
	}
	if err := ctx.Err(); err != nil {
		return Thumbnail{}, err
	}
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	w, h := width, height
	if max(w, h) > 256 {
		if w >= h {
			h, w = max(1, h*256/w), 256
		} else {
			w, h = max(1, w*256/h), 256
		}
	}
	thumbnail := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(thumbnail, thumbnail.Bounds(), source, source.Bounds(), draw.Src, nil)
	if err := ctx.Err(); err != nil {
		return Thumbnail{}, err
	}
	var output bytes.Buffer
	if err := png.Encode(&output, thumbnail); err != nil {
		return Thumbnail{}, err
	}
	if err := ctx.Err(); err != nil {
		return Thumbnail{}, err
	}
	return Thumbnail{MIME: "image/png", Data: output.Bytes(), Width: width, Height: height}, nil
}
