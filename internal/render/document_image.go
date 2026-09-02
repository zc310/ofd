package render

import (
	"bytes"
	"image"
	"image/color"
	"log/slog"
	"strings"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	_ "github.com/xiaoqidun/jbig2"
	"github.com/zc310/ofd/internal/models"
)

func (p *Document) Image(ctx *canvas.Context, object models.ImageObject, dp *models.DrawParam, pb models.StBox) {
	if !object.VisibleValue() {
		return
	}
	media, ok := p.Res[models.StID(object.ResourceID)]
	if !ok {
		return
	}

	img, err := p.decodeImage(media.MediaFile.Clean(), media.Format)
	if err != nil {
		slog.Error("decode image failed", "file", media.MediaFile, "error", err)
		return
	}
	if img == nil || img.Bounds().Empty() {
		return
	}

	ctm := imageCTM(object)
	m := imageMatrix(object.Boundary, img, ctm, pb.Height)

	if clip := p.buildImageClip(object.Clips, pb.Height, object.Boundary.X, object.Boundary.Y, ctm); clip != nil {
		img = imageWithClip(img, clip, m)
	}
	ctx.RenderImage(img, ctx.CoordSystemView().Mul(ctx.View()).Mul(m))
}

// imageCTM 返回图片对象使用的变换矩阵。
func imageCTM(object models.ImageObject) models.CTM {
	if object.CTM != nil {
		return *object.CTM
	}
	return models.CTM{object.Boundary.Width, 0, 0, object.Boundary.Height, 0, 0}
}

// imageMatrix 将图片像素坐标映射到 OFD 页面坐标。
func imageMatrix(box models.StBox, img image.Image, ctm models.CTM, pageHeight float64) canvas.Matrix {
	imgW := float64(img.Bounds().Dx())
	imgH := float64(img.Bounds().Dy())
	return canvas.Matrix{
		{ctm[0] / imgW, -ctm[2] / imgH, box.X + ctm[2] + ctm[4]},
		{-ctm[1] / imgW, ctm[3] / imgH, pageHeight - box.Y - ctm[3] - ctm[5]},
	}
}

func (p *Document) decodeImage(file models.StLoc, format string) (image.Image, error) {
	if strings.EqualFold(format, "SVG") || strings.EqualFold(file.Ext(), ".svg") {
		data, err := p.Document.Common.FileCache.ParseContent(file.String())
		if err != nil {
			return nil, err
		}
		svg, err := canvas.ParseSVG(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return rasterizer.Draw(svg, canvas.DPI(96), canvas.DefaultColorSpace), nil
	}
	return p.Document.Common.FileCache.ParseImage(file.String())
}

func (p *Document) buildImageClip(clips *models.Clips, pageH, bx, by float64, objectCTM models.CTM) *canvas.Path {
	if clips == nil || len(clips.Clip) == 0 {
		return nil
	}
	var result *canvas.Path
	for _, clip := range clips.Clip {
		current := p.buildImageClipRegion(clip, clips.TransFlag, pageH, bx, by, objectCTM)
		if current != nil {
			if result == nil {
				result = current
			} else {
				result = result.And(current)
			}
		}
	}
	return result
}

// buildImageClipRegion 合并同一个 Clip 中的所有 Area。
func (p *Document) buildImageClipRegion(clip models.CtClip, transFlag *bool, pageH, bx, by float64, objectCTM models.CTM) *canvas.Path {
	var result *canvas.Path
	for _, area := range clip.Area {
		if area.Path == nil {
			continue
		}

		areaCTM := models.IdentityMatrix
		if area.CTM != nil {
			areaCTM = *area.CTM
		}
		if transFlag == nil || *transFlag {
			areaCTM = *objectCTM.Multiply(&areaCTM)
		}
		pathCTM := areaCTM
		if area.Path.CTM != nil {
			pathCTM = *areaCTM.Multiply(area.Path.CTM)
		}

		clipPath := p.newPath(area.Path, func(pt models.StPos) (float64, float64) {
			// Path 的 AbbreviatedData 使用 Path 自身的局部坐标，必须先加上
			// Boundary 偏移，再应用 Area/Path CTM。忽略 Boundary 会让裁剪区
			// 在复合图元等场景中整体偏移，通常表现为右侧或左侧露出一条边。
			pt.X += area.Path.Boundary.X
			pt.Y += area.Path.Boundary.Y
			x, y := pathCTM.TransformPoint(pt)
			return x + bx, pageH - (y + by)
		})
		clipPath.Close()
		if result == nil {
			result = clipPath
		} else {
			result = result.Or(clipPath)
		}
	}
	return result
}

func imageWithClip(img image.Image, clip *canvas.Path, m canvas.Matrix) image.Image {
	if img == nil || clip == nil || canvas.Equal(m.Det(), 0) {
		return img
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return img
	}
	mask := imageClipMask(clip, m, w, h)
	return applyImageMask(img, mask)
}

// imageClipMask 将页面裁剪路径转换为图片像素掩码。
func imageClipMask(clip *canvas.Path, m canvas.Matrix, width, height int) *image.RGBA {
	maskPath := clip.Copy().Transform(m.Inv())
	maskCanvas := canvas.New(float64(width), float64(height))
	maskCtx := canvas.NewContext(maskCanvas)
	maskCtx.SetFillColor(color.White)
	maskCtx.SetStrokeColor(canvas.Transparent)
	maskCtx.DrawPath(0, 0, maskPath)
	return rasterizer.Draw(maskCanvas, canvas.DPMM(1), canvas.DefaultColorSpace)
}

// applyImageMask 将掩码透明度合成到原图片。
func applyImageMask(img image.Image, mask *image.RGBA) image.Image {
	bounds := img.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			ma := mask.RGBAAt(x-bounds.Min.X, y-bounds.Min.Y).A
			c.A = uint8(int(c.A) * int(ma) / 255)
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}
