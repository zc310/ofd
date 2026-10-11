package pdf2ofd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/jpeg"
	"math"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func (p *pdfInterpreter) imageData(stream *types.StreamDict, maskColor pdfColor) ([]byte, string, error) {
	if p.control != nil {
		if err := p.preflightImage(stream, 0); err != nil {
			return nil, "", err
		}
	}
	data, format, err := pdfImageDataRaw(p.ctx, stream, maskColor)
	if err == nil {
		data, format = p.imageMaskData(stream, data, format)
	}
	if err == nil && p.control != nil {
		err = p.control.check()
	}
	return data, format, err
}

// preflightImage 在像素解码前检查声明尺寸与码流实际尺寸；不依赖声明尺寸
// 与 JPEG/JPX/JBIG2 头部一致。累计预算按实际执行的解码计，缓存命中不重复扣除。
// 遮罩由 imageMaskData 按 SMask 优先规则选择后单独预检。
func (p *pdfInterpreter) preflightImage(stream *types.StreamDict, depth int) error {
	c := p.control
	if err := c.check(); err != nil {
		return err
	}
	if int64(depth) > c.options.Limits.MaxDepth {
		return c.limit("image_mask_depth", int64(depth), c.options.Limits.MaxDepth)
	}
	width, wok := dereferencedPDFNumber(p.ctx, stream.Dict["Width"])
	height, hok := dereferencedPDFNumber(p.ctx, stream.Dict["Height"])
	if !wok || !hok || math.IsNaN(width) || math.IsInf(width, 0) || math.IsNaN(height) || math.IsInf(height, 0) || width != math.Trunc(width) || height != math.Trunc(height) || width <= 0 || height <= 0 {
		return fmt.Errorf("PDF 图像尺寸无效: %g×%g", width, height)
	}
	if width >= 0x1p63 || height >= 0x1p63 || width > float64(c.options.Limits.MaxImagePixels) || height > float64(c.options.Limits.MaxImagePixels) {
		return c.limit("image_pixels", c.options.Limits.MaxImagePixels+1, c.options.Limits.MaxImagePixels)
	}
	pixels, err := c.imagePixels(int64(width), int64(height))
	if err != nil {
		return err
	}
	// 此处保留 JPEG 码流，避免 pdfcpu 在 CMYK DCT 分支提前分配像素缓冲区。
	components := stream.CSComponents
	if hasDCTFilter(stream) {
		stream.CSComponents = 0
	}
	err = c.decodeStream(stream, 0)
	stream.CSComponents = components
	if err != nil {
		return err
	}
	actualWidth, actualHeight := int64(width), int64(height)
	switch {
	case hasDCTFilter(stream):
		data := stream.Content
		if !isJPEGData(data) {
			data = stream.Raw
		}
		configuration, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("读取 JPEG 尺寸失败: %w", err)
		}
		actualWidth, actualHeight = int64(configuration.Width), int64(configuration.Height)
	case hasJPXFilter(stream):
		metadata, err := jpeg2000.DecodeMetadata(bytes.NewReader(jpxStreamBytes(stream)))
		if err != nil {
			return fmt.Errorf("读取 JPEG 2000 尺寸失败: %w", err)
		}
		actualWidth, actualHeight = int64(metadata.Width), int64(metadata.Height)
	case hasJBIG2Filter(stream):
		if err := p.preflightJBIG2(jbig2StreamBytes(stream), &pixels); err != nil {
			return err
		}
		for _, filter := range stream.FilterPipeline {
			if filter.DecodeParms == nil {
				continue
			}
			if globals := filter.DecodeParms["JBIG2Globals"]; globals != nil {
				sd, _, err := p.ctx.XRefTable.DereferenceStreamDict(globals)
				if err != nil {
					return err
				}
				if sd != nil {
					if err := c.decodeStream(sd, 0); err != nil {
						return err
					}
					if err := p.preflightJBIG2(sd.Content); err != nil {
						return err
					}
				}
			}
		}
	}
	actualPixels, err := c.imagePixels(actualWidth, actualHeight)
	if err != nil {
		return err
	}
	if actualPixels > pixels {
		pixels = actualPixels
	}
	if err := c.addImagePixels(pixels); err != nil {
		return err
	}
	if err := p.preflightImageColorSpace(stream.Dict["ColorSpace"], 0); err != nil {
		return err
	}
	return nil
}

func (p *pdfInterpreter) preflightImageColorSpace(object types.Object, depth int) error {
	if p.control != nil && int64(depth) > p.control.options.Limits.MaxDepth {
		return p.control.limit("color_depth", int64(depth), p.control.options.Limits.MaxDepth)
	}
	if depth > 8 {
		return fmt.Errorf("图片颜色空间嵌套过深")
	}
	if object == nil {
		return nil
	}
	object, err := dereferencePDFObject(p.ctx, object)
	if err != nil {
		return err
	}
	switch value := object.(type) {
	case types.StreamDict:
		return p.decodeStream(&value, 0)
	case *types.StreamDict:
		return p.decodeStream(value, 0)
	case types.Array:
		if len(value) > 0 {
			if name, ok := value[0].(types.Name); ok && (name.Value() == "CalGray" || name.Value() == "CalRGB" || name.Value() == "Lab") {
				p.report("color.calibrated", "校准图片颜色空间尚未完整支持")
			}
		}
		for _, entry := range value {
			if err := p.preflightImageColorSpace(entry, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// preflightJBIG2 只扫描段头和页面/区域尺寸，不执行算术解码，也不修改解码库的
// 进程级预算。内部符号字典的限制继续由 gobig2 的默认预算负责。
func (p *pdfInterpreter) preflightJBIG2(data []byte, observed ...*int64) error {
	pos := 0
	if len(data) >= 9 && bytes.Equal(data[:8], []byte{0x97, 'J', 'B', '2', '\r', '\n', 0x1a, '\n'}) {
		pos = 9
		if data[8]&2 == 0 {
			pos += 4
		}
	}
	for pos < len(data) {
		if len(data)-pos < 6 {
			return fmt.Errorf("JBIG2 段头不完整")
		}
		if err := p.control.check(); err != nil {
			return err
		}
		number := binary.BigEndian.Uint32(data[pos : pos+4])
		flags := data[pos+4]
		pos += 5
		count := int(data[pos] >> 5)
		if count == 7 {
			if len(data)-pos < 4 {
				return fmt.Errorf("JBIG2 引用计数不完整")
			}
			count = int(binary.BigEndian.Uint32(data[pos:pos+4]) & 0x1fffffff)
			pos += 4
			retentionBytes := (count + 1 + 7) / 8
			if retentionBytes > len(data)-pos {
				return fmt.Errorf("JBIG2 引用标志不完整")
			}
			pos += retentionBytes
		} else {
			if count > 4 {
				return fmt.Errorf("JBIG2 引用计数无效")
			}
			pos++
		}
		referenceBytes := 1
		if number > 256 {
			referenceBytes = 2
		}
		if number > 65536 {
			referenceBytes = 4
		}
		if count > (len(data)-pos)/referenceBytes {
			return fmt.Errorf("JBIG2 引用不完整")
		}
		pos += count * referenceBytes
		pageBytes := 1
		if flags&0x40 != 0 {
			pageBytes = 4
		}
		if len(data)-pos < pageBytes+4 {
			return fmt.Errorf("JBIG2 段长度不完整")
		}
		pos += pageBytes
		length := binary.BigEndian.Uint32(data[pos : pos+4])
		pos += 4
		kind := flags & 0x3f
		if kind == 48 || kind == 4 || kind == 6 || kind == 7 || kind == 20 || kind == 22 || kind == 23 || kind == 36 || kind == 38 || kind == 39 || kind == 40 || kind == 42 || kind == 43 {
			if len(data)-pos < 8 {
				return fmt.Errorf("JBIG2 区域尺寸不完整")
			}
			w, h := int64(binary.BigEndian.Uint32(data[pos:pos+4])), int64(binary.BigEndian.Uint32(data[pos+4:pos+8]))
			if h != 0xffffffff {
				pixels, err := p.control.imagePixels(w, h)
				if err != nil {
					return err
				}
				if len(observed) > 0 && pixels > *observed[0] {
					*observed[0] = pixels
				}
			}
		}
		if length == 0xffffffff {
			break
		}
		if uint64(length) > uint64(len(data)-pos) {
			return fmt.Errorf("JBIG2 段数据不完整")
		}
		pos += int(length)
	}
	return nil
}

func (p *pdfInterpreter) applyImageSoftMask(data []byte, mask *types.StreamDict) ([]byte, bool) {
	if p.control != nil {
		if err := p.preflightImage(mask, 1); err != nil {
			p.report("image.mask", err.Error())
			return nil, false
		}
		if mask.Dict["SMask"] != nil || mask.Dict["Mask"] != nil {
			p.report("image.mask", "软掩码包含尚未支持的嵌套图片遮罩，保留原图")
			return nil, false
		}
		if err := p.reserveImageDecode(data); err != nil {
			p.report("image.mask", err.Error())
			return nil, false
		}
	}
	masked, ok := applyPDFImageSoftMask(p.ctx, data, mask, p.control)
	if !ok {
		p.report("image.mask", "软掩码无法解码，保留原图")
	}
	return masked, ok
}

// preflightGraphicsMask 先限制软掩码 Form 的解压，再交给现有的单图像识别逻辑。
func (p *pdfInterpreter) preflightGraphicsMask(object types.Object) error {
	resolved, err := dereferencePDFObject(p.ctx, object)
	if err != nil {
		return err
	}
	if _, ok := resolved.(types.Name); ok {
		return nil
	}
	dict, ok := resolved.(types.Dict)
	if !ok {
		return fmt.Errorf("图形状态软掩码字典无效")
	}
	if dict["G"] == nil {
		return nil
	}
	stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(dict["G"])
	if err != nil || stream == nil {
		return err
	}
	if subtype := stream.NameEntry("Subtype"); subtype != nil && *subtype == "Form" {
		return p.decodeStream(stream, 0)
	}
	return nil
}
