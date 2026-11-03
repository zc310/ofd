package pdfcpuwriter

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

var errInvalidImage = errors.New("PDF 图片数据无效")

type imageData struct {
	width, height int
	space         string
	pixels, alpha []byte
	jpeg          bool
	png           bool
}

// imageResource 优先复用编码图片；光栅图片按尺寸、颜色空间及全部颜色/透明度采样精确复用。
func (r *Document) imageResource(img image.Image) (prototypeImage, error) {
	var key [32]byte
	encoded, encodedKey := img.(*cimage.Image)
	encodedKey = encodedKey && encoded.Mask == nil
	if encodedKey {
		key = sha256.Sum256(encoded.Bytes)
		if resource, ok := r.imageCache[key]; ok {
			return resource, nil
		}
	}
	data, err := r.prepareImage(img)
	if err != nil {
		return prototypeImage{}, err
	}
	if !encodedKey {
		key = data.key()
		if resource, ok := r.imageCache[key]; ok {
			return resource, nil
		}
	}
	sd, err := data.stream(r)
	if err != nil {
		return prototypeImage{}, err
	}
	if r.options.LossyImages {
		if err := r.lossyImage(sd, img); err != nil {
			return prototypeImage{}, err
		}
	}
	sd.Dict["Interpolate"] = types.Boolean(true)
	if mask := sd.Dict["SMask"]; mask != nil {
		soft, _, err := r.pdf.DereferenceStreamDict(mask)
		if err != nil {
			return prototypeImage{}, err
		}
		soft.Dict["Interpolate"] = types.Boolean(true)
	}
	resource := prototypeImage{name: fmt.Sprintf("Im%d", len(r.imageResources)), width: data.width, height: data.height}
	r.imageResources[resource.name] = r.object(*sd)
	r.imageCache[key] = resource
	return resource, r.err
}

// validateImageSize 保留 pdfcpu 的像素和解码字节预算，快速路径不能绕过资源限制。
func (r *Document) validateImageSize(width, height int) error {
	if width <= 0 || height <= 0 || width > math.MaxInt/height/4 {
		return fmt.Errorf("%w：尺寸 %dx%d", errInvalidImage, width, height)
	}
	pixels := int64(width) * int64(height)
	limits := r.pdf.Conf.Limits
	if pixels > limits.MaxImagePixels || pixels*4 > limits.MaxImageBytes {
		return fmt.Errorf("%w：图片超过像素或解码字节预算", errInvalidImage)
	}
	return nil
}

func (r *Document) prepareImage(img image.Image) (imageData, error) {
	if img == nil {
		return imageData{}, errInvalidImage
	}
	bounds := img.Bounds()
	if err := r.validateImageSize(bounds.Dx(), bounds.Dy()); err != nil {
		return imageData{}, err
	}
	if encoded, ok := img.(*cimage.Image); ok {
		if int64(len(encoded.Bytes)) > r.pdf.Conf.Limits.MaxStreamBytes {
			return imageData{}, fmt.Errorf("%w：编码数据超过流字节预算", errInvalidImage)
		}
		if encoded.Mimetype == "image/jpeg" && encoded.Mask == nil {
			return r.prepareJPEG(encoded)
		}
		decoded, err := encoded.Image()
		if err != nil {
			return imageData{}, err
		}
		img = decoded
		bounds = img.Bounds()
		if err := r.validateImageSize(bounds.Dx(), bounds.Dy()); err != nil {
			return imageData{}, err
		}
	}
	switch im := img.(type) {
	case *image.NRGBA:
		return r.rasterImage(bounds, im.NRGBAAt)
	case *image.RGBA:
		return r.rasterImage(bounds, func(x, y int) color.NRGBA {
			c := im.RGBAAt(x, y)
			if c.A == 0 {
				return color.NRGBA{}
			}
			if c.A == 255 {
				return color.NRGBA(c)
			}
			// 与 color.NRGBAModel 相同的 16 位反预乘，不做逐像素接口装箱。
			a := uint32(c.A)
			return color.NRGBA{R: uint8((uint32(c.R) * 0xffff / a) >> 8), G: uint8((uint32(c.G) * 0xffff / a) >> 8), B: uint8((uint32(c.B) * 0xffff / a) >> 8), A: c.A}
		})
	case *image.Gray:
		data := imageData{width: bounds.Dx(), height: bounds.Dy(), space: model.DeviceGrayCS, pixels: make([]byte, bounds.Dx()*bounds.Dy())}
		for y := range bounds.Dy() {
			if err := r.ctx.Err(); err != nil {
				return imageData{}, err
			}
			start := im.PixOffset(bounds.Min.X, y+bounds.Min.Y)
			copy(data.pixels[y*data.width:(y+1)*data.width], im.Pix[start:start+data.width])
		}
		return data, nil
	default:
		// 调色板、16 位和其他图片仍走原编码路径，保留精度及原有颜色语义。
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			return imageData{}, err
		}
		return imageData{width: bounds.Dx(), height: bounds.Dy(), pixels: encoded.Bytes(), png: true}, nil
	}
}

// prepareJPEG 保留 DCT 原字节及真实颜色空间（ISO 32000-1 §7.4.8、§8.9）。
// Image 使用 canvas 已发布的解码缓存；未经解码的调用只验证一次，不重复解码。
func (r *Document) prepareJPEG(encoded *cimage.Image) (imageData, error) {
	config, err := jpeg.DecodeConfig(bytes.NewReader(encoded.Bytes))
	if err != nil {
		return imageData{}, err
	}
	if err := r.validateImageSize(config.Width, config.Height); err != nil {
		return imageData{}, err
	}
	decoded, err := encoded.Image()
	if err != nil {
		return imageData{}, err
	}
	if decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return imageData{}, fmt.Errorf("%w：JPEG 配置与解码尺寸不同", errInvalidImage)
	}
	space := model.DeviceRGBCS
	switch config.ColorModel {
	case color.GrayModel:
		space = model.DeviceGrayCS
	case color.CMYKModel:
		space = model.DeviceCMYKCS
	case color.YCbCrModel:
	default:
		return imageData{}, fmt.Errorf("%w：JPEG 颜色空间", errInvalidImage)
	}
	return imageData{width: config.Width, height: config.Height, space: space, pixels: encoded.Bytes, jpeg: true}, nil
}

func (r *Document) rasterImage(bounds image.Rectangle, at func(int, int) color.NRGBA) (imageData, error) {
	data := imageData{width: bounds.Dx(), height: bounds.Dy(), space: model.DeviceRGBCS, pixels: make([]byte, bounds.Dx()*bounds.Dy()*3)}
	gray := true
	for y := range data.height {
		if err := r.ctx.Err(); err != nil {
			return imageData{}, err
		}
		for x := range data.width {
			c := at(x+bounds.Min.X, y+bounds.Min.Y)
			i := y*data.width + x
			data.pixels[3*i], data.pixels[3*i+1], data.pixels[3*i+2] = c.R, c.G, c.B
			gray = gray && c.R == c.G && c.G == c.B
			if data.alpha == nil && c.A != 255 {
				data.alpha = bytes.Repeat([]byte{255}, data.width*data.height)
			}
			if data.alpha != nil {
				data.alpha[i] = c.A
			}
		}
	}
	if gray {
		data.space = model.DeviceGrayCS
		for i := range data.width * data.height {
			data.pixels[i] = data.pixels[3*i]
		}
		data.pixels = data.pixels[:data.width*data.height]
	}
	return data, nil
}

func (data imageData) key() [32]byte {
	h := sha256.New()
	var header [25]byte
	if data.png {
		header[0] = 1
	}
	binary.LittleEndian.PutUint64(header[1:9], uint64(data.width))
	binary.LittleEndian.PutUint64(header[9:17], uint64(data.height))
	binary.LittleEndian.PutUint64(header[17:25], uint64(len(data.alpha)))
	h.Write(header[:])
	h.Write([]byte(data.space))
	h.Write(data.pixels)
	h.Write(data.alpha)
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}

func (data imageData) stream(r *Document) (*types.StreamDict, error) {
	if data.jpeg {
		// PDF 流独占原字节快照，调用方后续修改图片不能改变之前绘制的页面。
		return model.CreateDCTImageStreamDict(r.pdf.XRefTable, bytes.Clone(data.pixels), data.width, data.height, 8, data.space)
	}
	if data.png {
		sd, _, _, err := model.CreateImageStreamDict(r.pdf.XRefTable, bytes.NewReader(data.pixels))
		return sd, err
	}
	return model.CreateFlateImageStreamDict(r.pdf.XRefTable, data.pixels, data.alpha, data.width, data.height, 8, data.space)
}

// lossyImage 只替换颜色流，沿用原无损 SMask（ISO 32000-1 §8.9.5、§11.6.5）。
// 编码前转为非预乘 RGB，避免半透明像素变暗；已有 JPEG 不重复编码。
func (r *Document) lossyImage(sd *types.StreamDict, img image.Image) error {
	if filter := sd.NameEntry("Filter"); filter != nil && *filter == "DCTDecode" {
		return nil
	}
	if encoded, ok := img.(*cimage.Image); ok && encoded.Mimetype == "image/jpeg" {
		space := model.DeviceRGBCS
		switch encoded.Config.ColorModel {
		case color.GrayModel:
			space = model.DeviceGrayCS
		case color.CMYKModel:
			space = model.DeviceCMYKCS
		}
		return r.replaceImageColor(sd, encoded.Bytes, encoded.Width, encoded.Height, space)
	}
	bounds := img.Bounds()
	straight := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		for x := 0; x < bounds.Dx(); x++ {
			c, _ := color.NRGBAModel.Convert(img.At(x+bounds.Min.X, y+bounds.Min.Y)).(color.NRGBA)
			straight.SetRGBA(x, y, color.RGBA{R: c.R, G: c.G, B: c.B, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, straight, nil); err != nil {
		return err
	}
	return r.replaceImageColor(sd, encoded.Bytes(), bounds.Dx(), bounds.Dy(), model.DeviceRGBCS)
}

func (r *Document) replaceImageColor(sd *types.StreamDict, data []byte, width, height int, space string) error {
	jpegStream, err := model.CreateDCTImageStreamDict(r.pdf.XRefTable, data, width, height, 8, space)
	if err != nil {
		return err
	}
	if mask := sd.Dict["SMask"]; mask != nil {
		jpegStream.Dict["SMask"] = mask
	}
	jpegStream.Dict["Interpolate"] = types.Boolean(true)
	*sd = *jpegStream
	return nil
}
