package pdf2ofd

import (
	"bytes"
	"encoding/binary"
	"fmt"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/coloricc"
)

type matteJPXInfo struct {
	metadata  *jpeg2000.Metadata
	family    string
	alpha     int // SMaskInData: 0 无内嵌透明度，1 非预乘，2 预乘。
	colours   int
	converted bool   // 普通三分量图像在通道重排后转换为 RGB。
	channels  [5]int // 逻辑颜色分量及尾部 opacity 对应的原采样平面。
	icc       *coloricc.Transformer
}

// matteJPXHeader 只接受可直接作为 PDF DeviceGray/RGB/CMYK 分量的整数采样。
// JP2 颜色转换和调色板不能从最终 RGB 图像中反推，因此先读取原分量。
// PDF 的 JPX 图像以码流位深为准，忽略 BitsPerComponent 和 Decode。
func (p *pdfInterpreter) matteJPXHeader(stream *types.StreamDict) (*matteJPXInfo, error) {
	return p.jpxHeader(stream, false)
}

// jpxHeader 共用于普通图像与 Matte；Matte 要求外置灰度掩码，普通图像允许
// SMaskInData 明确指定一个透明度分量。复杂颜色/通道结构仍须报告缺口。
func (p *pdfInterpreter) jpxHeader(stream *types.StreamDict, allowAlpha bool) (*matteJPXInfo, error) {
	if err := p.control.check(); err != nil {
		return nil, err
	}
	// 掩码还未进行完整像素预检；读取 JP2 盒或元数据前先限制编码流大小。
	if p.control != nil && int64(len(stream.Raw)) > p.control.options.Limits.MaxStreamBytes {
		return nil, p.control.limit("stream_bytes", int64(len(stream.Raw)), p.control.options.Limits.MaxStreamBytes)
	}
	data := jpxStreamBytes(stream)
	if len(data) == 0 {
		return nil, fmt.Errorf("JPX 码流为空或过滤器组合尚未支持")
	}
	var channelDef []byte
	if !bytes.HasPrefix(data, []byte{0xff, 0x4f}) {
		if err := p.scanJPXBoxes(data, false, &channelDef); err != nil {
			return nil, err
		}
	}
	metadata, err := jpeg2000.DecodeMetadata(bytes.NewReader(data))
	if cancelled := p.control.check(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		return nil, fmt.Errorf("读取 JPX 头失败: %w", err)
	}
	components := metadata.NumComponents
	if (components < 1 || components > 5) || len(metadata.BitsPerComponent) != components || len(metadata.Signed) != components {
		return nil, fmt.Errorf("JPX 分量数量尚未支持")
	}
	bits := metadata.BitsPerComponent[0]
	for i := range components {
		if bits < 1 || bits > 16 || metadata.BitsPerComponent[i] != bits || metadata.Signed[i] {
			return nil, fmt.Errorf("JPX 仅支持同位深的 1 到 16 位无符号整数分量")
		}
	}
	if len(metadata.ICCProfile) != 0 {
		return nil, fmt.Errorf("JPX 码流内嵌的 ICC 颜色空间尚未支持")
	}
	familyFromCode := ""
	converted := false
	switch metadata.ColorSpace {
	case jpeg2000.ColorSpaceGray, jpeg2000.ColorSpaceBilevel:
		familyFromCode = "DeviceGray"
	case jpeg2000.ColorSpaceSRGB:
		familyFromCode = "DeviceRGB"
	case jpeg2000.ColorSpaceCMYK:
		familyFromCode = "DeviceCMYK"
	case jpeg2000.ColorSpaceUnspecified:
		// 裸码流由 PDF ColorSpace 指定分量含义。
	case jpeg2000.ColorSpaceSYCC, jpeg2000.ColorSpaceEYCC, jpeg2000.ColorSpaceYCbCr2, jpeg2000.ColorSpaceYCbCr3, jpeg2000.ColorSpacePhotoYCC, jpeg2000.ColorSpaceCMY:
		if !allowAlpha || components != 3 {
			return nil, fmt.Errorf("JPX 的码流颜色变换尚不支持此掩码路径")
		}
		familyFromCode, converted = "DeviceRGB", true
	default:
		return nil, fmt.Errorf("JPX 的码流颜色变换尚未支持")
	}
	family := familyFromCode
	var icc *coloricc.Transformer
	if stream.Dict["ColorSpace"] != nil {
		space, err := dereferencePDFObject(p.ctx, stream.Dict["ColorSpace"])
		if err != nil {
			return nil, fmt.Errorf("读取 JPX PDF 颜色空间失败: %w", err)
		}
		array, isArray := space.(types.Array)
		isICCBased := false
		if isArray && len(array) >= 2 {
			firstName, _ := dereferencePDFObject(p.ctx, array[0])
			spaceName, isName := firstName.(types.Name)
			isICCBased = isName && spaceName.Value() == "ICCBased"
		}
		if isICCBased {
			profileObject, err := dereferencePDFObject(p.ctx, array[1])
			if err != nil {
				return nil, fmt.Errorf("读取 JPX ICC 配置失败: %w", err)
			}
			var profile *types.StreamDict
			switch value := profileObject.(type) {
			case types.StreamDict:
				profile = &value
			case *types.StreamDict:
				profile = value
			}
			if profile == nil {
				return nil, fmt.Errorf("JPX ICC 配置不是流")
			}
			components, ok := dereferencedPDFNumber(p.ctx, profile.Dict["N"])
			if !ok {
				return nil, fmt.Errorf("JPX ICC 配置缺少有效的 N 分量数")
			}
			switch int(components) {
			case 1:
				family = "DeviceGray"
			case 3:
				family = "DeviceRGB"
			case 4:
				family = "DeviceCMYK"
			default:
				return nil, fmt.Errorf("JPX ICC 配置的 %d 个分量尚未支持", int(components))
			}
			icc = pdfImageICCProfileChannels(p.ctx, stream, int(components))
		} else {
			var ok bool
			family, ok = dereferenceName(p.ctx, stream.Dict["ColorSpace"])
			if !ok {
				return nil, fmt.Errorf("JPX 当前只支持 DeviceGray/RGB/CMYK 或 ICCBased")
			}
		}
	}
	expected := map[string]int{"DeviceGray": 1, "DeviceRGB": 3, "DeviceCMYK": 4}[family]
	alpha := 0
	if object := stream.Dict["SMaskInData"]; object != nil {
		value, ok := dereferencedPDFNumber(p.ctx, object)
		if !ok || (value != 0 && value != 1 && value != 2) || (!allowAlpha && value != 0) {
			return nil, fmt.Errorf("JPX 的 SMaskInData 无效或此掩码路径不支持内嵌透明度")
		}
		alpha = int(value)
	}
	colourComponents := components
	if alpha != 0 {
		colourComponents--
	}
	// SMaskInData 缺省为 0；即使码流带额外 alpha，也不得擅自使用。
	ignoredAlpha := allowAlpha && alpha == 0 && components == expected+1
	if expected == 0 || (expected != colourComponents && !ignoredAlpha) || (familyFromCode != "" && familyFromCode != family) {
		return nil, fmt.Errorf("JPX 分量数或码流颜色信息与 PDF 颜色空间不一致")
	}
	channels, err := p.jpxChannels(channelDef, components, expected, alpha)
	if err != nil {
		return nil, err
	}
	return &matteJPXInfo{metadata: metadata, family: family, alpha: alpha, colours: expected, converted: converted, channels: channels, icc: icc}, nil
}

// JP2 元数据接口尚未暴露 pclr/cmap/cdef；只扫描盒头，不复制载荷或解析码流。
// 两层扫描覆盖文件盒与 jp2h 子盒，支持 XLBox 和延伸到父盒末尾的零长度。
func (p *pdfInterpreter) checkMatteJPXBoxes(data []byte, header bool) error {
	var channelDef []byte
	return p.scanJPXBoxes(data, header, &channelDef)
}

func (p *pdfInterpreter) scanJPXBoxes(data []byte, header bool, channelDef *[]byte) error {
	for len(data) > 0 {
		if err := p.control.consumeOperations(1); err != nil {
			return err
		}
		if len(data) < 8 {
			return fmt.Errorf("JP2 盒头不完整")
		}
		length, start := uint64(binary.BigEndian.Uint32(data[:4])), 8
		kind := string(data[4:8])
		if length == 1 {
			if len(data) < 16 {
				return fmt.Errorf("JP2 扩展盒头不完整")
			}
			length, start = binary.BigEndian.Uint64(data[8:16]), 16
		} else if length == 0 {
			length = uint64(len(data))
		}
		if length < uint64(start) || length > uint64(len(data)) {
			return fmt.Errorf("JP2 盒长度无效")
		}
		payload := data[start:int(length)]
		if header {
			switch kind {
			case "pclr", "cmap":
				return fmt.Errorf("JP2 的调色板或通道映射尚未支持")
			case "cdef":
				if *channelDef != nil || len(payload) < 2 {
					return fmt.Errorf("JP2 通道定义重复或不完整")
				}
				count := int(binary.BigEndian.Uint16(payload))
				if count < 1 || count > 5 || len(payload) != 2+count*6 {
					return fmt.Errorf("JP2 通道定义数量或长度无效")
				}
				*channelDef = payload
			case "colr":
				if len(payload) < 7 || payload[0] != 1 {
					return fmt.Errorf("JP2 的 ICC 或复杂颜色描述尚未支持")
				}
			}
		} else if kind == "jp2h" {
			if err := p.scanJPXBoxes(payload, true, channelDef); err != nil {
				return err
			}
		}
		data = data[int(length):]
	}
	return nil
}

// Association 指定逻辑颜色次序，Channel 指定原采样平面；条目的排列次序无关。
// 允许单一 opacity 位于任意平面，作用于全部颜色。映射只重排切片引用，
// 不复制逐像素数据；完整性和一对一关系必须在解码前验证。
func (p *pdfInterpreter) jpxChannels(data []byte, components, colours, alpha int) ([5]int, error) {
	channels := [5]int{0, 1, 2, 3, 4}
	if data == nil {
		return channels, nil
	}
	count := int(binary.BigEndian.Uint16(data))
	if count != components {
		return channels, fmt.Errorf("JP2 通道定义未覆盖全部分量")
	}
	var seen, associations [5]bool
	opacity := false
	for i := 0; i < count; i++ {
		if err := p.control.consumeOperations(1); err != nil {
			return channels, err
		}
		entry := data[2+i*6:]
		channel, kind, association := int(binary.BigEndian.Uint16(entry)), int(binary.BigEndian.Uint16(entry[2:])), int(binary.BigEndian.Uint16(entry[4:]))
		if channel >= components || seen[channel] {
			return channels, fmt.Errorf("JP2 通道定义索引无效或重复")
		}
		seen[channel] = true
		if kind == 0 {
			if association < 1 || association > colours || associations[association-1] {
				return channels, fmt.Errorf("JP2 颜色关联无效或重复")
			}
			associations[association-1] = true
			channels[association-1] = channel
		} else if (kind == 1 || kind == 2) && association == 0 && components == colours+1 && !opacity && (alpha == 0 || kind == alpha) {
			opacity = true
			channels[colours] = channel
		} else {
			return channels, fmt.Errorf("JP2 opacity 通道与 SMaskInData 不一致或尚未支持")
		}
	}
	for c := 0; c < colours; c++ {
		if !associations[c] {
			return channels, fmt.Errorf("JP2 颜色关联不完整")
		}
	}
	if (components == colours+1) != opacity {
		return channels, fmt.Errorf("JP2 opacity 通道定义不完整")
	}
	return channels, nil
}

// DecodeFloat 对普通整数码流返回逆小波/MCT/DC 移位后的原样本平面，不应用
// JP2 的颜色转换。float32 精确表示 16 位整数，CMYK 四分量也不会误当作 RGBA。
func (p *pdfInterpreter) matteJPXImage(stream *types.StreamDict, info *matteJPXInfo, width, height int) (*jpeg2000.FloatImage, error) {
	return p.jpxImagePlanes(stream, info, width, height, true)
}

// 普通解码已经由 preflightImage 计入像素；Matte/颜色键二次解码需要额外计入。
func (p *pdfInterpreter) jpxImagePlanes(stream *types.StreamDict, info *matteJPXInfo, width, height int, extraDecode bool) (*jpeg2000.FloatImage, error) {
	m := info.metadata
	if m.Width != width || m.Height != height {
		return nil, fmt.Errorf("JPX 实际尺寸与声明尺寸不一致")
	}
	pixels := int64(width) * int64(height)
	if p.control != nil {
		if _, err := p.control.imagePixels(int64(width), int64(height)); err != nil {
			return nil, err
		}
		if extraDecode {
			if err := p.control.addImagePixels(pixels); err != nil {
				return nil, err
			}
		}
	}
	if err := p.control.consumeOperations(pixels * int64(m.NumComponents)); err != nil {
		return nil, err
	}
	decoded, err := jpeg2000.DecodeFloat(bytes.NewReader(jpxStreamBytes(stream)))
	if cancelled := p.control.check(); cancelled != nil {
		return nil, cancelled
	}
	if err != nil {
		return nil, fmt.Errorf("解码 JPX 分量失败: %w", err)
	}
	if decoded.Width != width || decoded.Height != height || decoded.BitDepth != m.BitsPerComponent[0] || decoded.Signed || len(decoded.Components) != m.NumComponents {
		return nil, fmt.Errorf("JPX 解码结果不是预期的整数分量")
	}
	for _, plane := range decoded.Components {
		if len(plane) != width*height {
			return nil, fmt.Errorf("JPX 采样平面长度无效")
		}
	}
	// 统一交付逻辑颜色顺序；Matte、普通图像、软掩码共享相同通道含义。
	var ordered [5][]float32
	for c := 0; c < m.NumComponents; c++ {
		ordered[c] = decoded.Components[info.channels[c]]
	}
	copy(decoded.Components, ordered[:m.NumComponents])
	return decoded, nil
}

func (p *pdfInterpreter) matteJPXAlpha(mask *types.StreamDict, info *matteJPXInfo, width, height int) ([]uint16, error) {
	decoded, err := p.matteJPXImage(mask, info, width, height)
	if err != nil {
		return nil, err
	}
	if err := p.control.consumeOperations(int64(width) * int64(height)); err != nil {
		return nil, err
	}
	maximum := float64(uint32(1)<<uint(info.metadata.BitsPerComponent[0]) - 1)
	alpha := make([]uint16, width*height)
	for y := 0; y < height; y++ {
		if err := p.control.check(); err != nil {
			return nil, err
		}
		for x := 0; x < width; x++ {
			alpha[y*width+x] = floatToUint16(float64(decoded.Components[0][y*width+x]) / maximum)
		}
	}
	return alpha, nil
}
