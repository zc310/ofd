package render

import (
	"crypto/sha256"
	"image/color"
	"strconv"
	"strings"
	"sync"

	"github.com/zc310/ofd/internal/coloricc"
	"github.com/zc310/ofd/internal/models"
)

// colorResolver 把 OFD 颜色按其颜色空间解析为 RGBA。
type colorResolver func(models.CTColor) color.RGBA

// iccTransformerCache 按 ICC 配置文件内容缓存转换器。
var iccTransformerCache = struct {
	mu    sync.Mutex
	items map[[sha256.Size]byte]*coloricc.Transformer
}{items: make(map[[sha256.Size]byte]*coloricc.Transformer)}

// profilePaletteCache 按配置文件内容缓存从 Profile 读到的原始调色板。
var profilePaletteCache = struct {
	mu    sync.Mutex
	items map[[sha256.Size]byte][]byte
}{items: make(map[[sha256.Size]byte][]byte)}

// colorRGBA 解析 CTColor。颜色空间为 GRAY/RGB/CMYK 时按对应分量解释，
// 存在调色板且未给出 Value 时按 Index 取色。未引用颜色空间时沿用默认的
// RGB/RGBA 语义。
func (p *Document) colorRGBA(source models.CTColor) color.RGBA {
	if source.ColorSpace == 0 {
		return ofdColorRGBA(source)
	}
	space := p.GetColorSpace(models.StID(source.ColorSpace))
	if space == nil {
		return ofdColorRGBA(source)
	}
	palette := p.colorSpacePalette(space)
	if transformer := p.colorSpaceTransformer(space); transformer != nil {
		if components, ok := colorSpaceComponents8(source, space, palette); ok {
			r, g, b := transformer.ToRGB(transformerOrderComponents(space, components))
			alpha := uint8(255)
			if source.Alpha != nil {
				alpha = *source.Alpha
			}
			return premultiplied(r, g, b, alpha)
		}
	}
	return resolveColorSpaceColor(space, source, palette)
}

// looksLikeICCProfile 粗判一份配置文件是否意在作为 ICC 配置文件。
//
// ICC 头在偏移 36 处有 'acsp' 签名（ISO 15076-1）。判它而不是「长度能否被通道
// 数整除」，是因为损坏的 ICC 配置文件也可能恰好整除——那种文件应当报错或回退，
// 而不是被当成调色板解释成一个颜色完全不同的东西。
func looksLikeICCProfile(data []byte) bool {
	return len(data) >= 40 && string(data[36:40]) == "acsp"
}

// colorSpaceProfilePalette 从 Profile 指向的文件读出原始二进制调色板。
//
// GB/T 33190 只说 Profile「指向包内颜色配置文件」（表 25），XSD 里也仅是 ST_Loc，
// 没有规定文件格式。实际文件有两类：ICC 配置文件，以及按通道数平铺的原始调色板
// （每个 Index 连续 channels 个字节）。ICC 解析失败且文件确实不像 ICC 时按后者
// 解释，否则这类文件的颜色会静默回退成黑色。
func (p *Document) colorSpaceProfilePalette(space *models.ColorSpace) []byte {
	if space == nil || p.FileCache == nil {
		return nil
	}
	profilePath := strings.TrimSpace(space.Profile.String())
	if profilePath == "" {
		return nil
	}
	channels := colorSpaceChannels(space)
	data, err := p.FileCache.Read(profilePath)
	if err != nil || len(data) == 0 {
		return nil
	}
	digest := sha256.Sum256(data)
	profilePaletteCache.mu.Lock()
	cached, ok := profilePaletteCache.items[digest]
	profilePaletteCache.mu.Unlock()
	if ok {
		if len(cached) == 0 {
			return nil
		}
		return cached
	}
	if looksLikeICCProfile(data) || len(data)%channels != 0 {
		// 记下「不可用」，避免同一份坏文件每次都重新读盘判定。
		profilePaletteCache.mu.Lock()
		profilePaletteCache.items[digest] = nil
		profilePaletteCache.mu.Unlock()
		return nil
	}
	profilePaletteCache.mu.Lock()
	profilePaletteCache.items[digest] = data
	profilePaletteCache.mu.Unlock()
	return data
}

// colorSpacePalette 返回颜色空间可用的调色板字节：内联 Palette 优先，其次是
// Profile 指向的原始二进制调色板。内联优先的规则由 CT_ColorSpace 的顺序语义
// 决定——两者都声明时以内联为准。
func (p *Document) colorSpacePalette(space *models.ColorSpace) []byte {
	if space == nil {
		return nil
	}
	if space.Palette != nil && len(space.Palette.CV) > 0 {
		return nil // 内联调色板由 colorSpaceComponents8 直接处理
	}
	return p.colorSpaceProfilePalette(space)
}

// colorSpaceTransformer 返回该颜色空间的 ICC 转换器：优先使用资源自带的
// Profile 配置文件，CMYK 没有配置文件时使用进程默认 CMYK 配置文件。
func (p *Document) colorSpaceTransformer(space *models.ColorSpace) *coloricc.Transformer {
	if space == nil {
		return nil
	}
	if profilePath := strings.TrimSpace(space.Profile.String()); profilePath != "" && p.FileCache != nil {
		data, err := p.FileCache.Read(profilePath)
		if err == nil && len(data) > 0 {
			digest := sha256.Sum256(data)
			iccTransformerCache.mu.Lock()
			transformer := iccTransformerCache.items[digest]
			iccTransformerCache.mu.Unlock()
			if transformer != nil {
				return transformer
			}
			transformer, err = coloricc.New(data, colorSpaceChannels(space))
			if err == nil {
				iccTransformerCache.mu.Lock()
				iccTransformerCache.items[digest] = transformer
				iccTransformerCache.mu.Unlock()
				return transformer
			}
		}
	}
	if strings.EqualFold(strings.TrimSpace(space.Type), "CMYK") {
		if transformer, _ := coloricc.DefaultCMYK(); transformer != nil {
			return transformer
		}
	}
	return nil
}

// colorSpaceChannels 返回颜色空间的分量数量。
func colorSpaceChannels(space *models.ColorSpace) int {
	switch strings.ToUpper(strings.TrimSpace(space.Type)) {
	case "GRAY":
		return 1
	case "CMYK":
		return 4
	default:
		return 3
	}
}

// spaceBitsPerComponent 返回颜色空间声明的每通道位数，缺省或非法时按 8 处理。
// GB/T 33190 表 25 规定有效取值为 1、2、4、8、16，缺省值为 8。
func spaceBitsPerComponent(space *models.ColorSpace) int {
	switch space.BitsPerComponent {
	case 1, 2, 4, 8, 16:
		return space.BitsPerComponent
	default:
		return 8
	}
}

// scaleComponent 按 BitsPerComponent 把通道取值归一化到 0-255。
//
// GB/T 33190 表 27：BPC 有效时颜色通道取值区间为 [0, 2^BPC-1]，超出区间按默认
// 颜色处理。默认颜色是各通道全 0（表 26：Value 与 Index 都不出现时的取值），
// 这里对越界通道取 0 而不是截断到上限——截断会把本该作废的颜色画成满墨。
func scaleComponent(value, bits int) uint8 {
	// 调用方应当先经 spaceBitsPerComponent 消毒；这里再兜一层，避免非法位数导致
	// 1<<bits 负移位 panic——渲染路径上任何 panic 都会毁掉整页。
	switch bits {
	case 1, 2, 4, 8, 16:
	default:
		bits = 8
	}
	highest := 1<<bits - 1
	if value < 0 || value > highest {
		return 0
	}
	return uint8((value*255 + highest/2) / highest)
}

// parseChannelValue 解析单个颜色通道取值，支持标准允许的 "#" 十六进制写法
// （表 27：采用 16 进制表示时应以 "#" 加以标识，例如 "#11 #22 #33 #44"）。
func parseChannelValue(text string) (int, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	if strings.HasPrefix(text, "#") {
		value, err := strconv.ParseInt(text[1:], 16, 32)
		if err != nil {
			return 0, false
		}
		return int(value), true
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return value, true
}

// colorSpaceComponents8 返回颜色在自身颜色空间下的分量，已按 BitsPerComponent
// 归一化到 0-255。Value 优先，否则按 Index 从调色板取分量。
//
// 分量个数与颜色空间不匹配属未定义行为，处理方式是「缺的补 0、多余的忽略」：
// 缺通道取 0 等价于该通道按默认颜色取值，多余通道直接丢弃。两种都是文档可以
// 观察到的确定行为，总好过因为一个坏颜色让整份文档无法转换。
//
// profile 是 Profile 指向的原始二进制调色板；内联 Palette 优先于它。
func colorSpaceComponents8(source models.CTColor, space *models.ColorSpace, profile []uint8) ([]uint8, bool) {
	channels := colorSpaceChannels(space)
	bits := spaceBitsPerComponent(space)
	if source.Value != nil {
		if values, count, ok := source.Value.Components(); ok {
			result := make([]uint8, channels)
			for i := 0; i < channels; i++ {
				if i >= count {
					break // 缺通道：留 0，即默认颜色
				}
				result[i] = scaleComponent(values[i], bits)
			}
			return result, true
		}
		// 没有原文（例如代码直接构造的颜色）：RGBA 里已经是 0-255，不再按位深缩放。
		rgba := source.Value.RGBA
		normalized := [4]uint8{rgba.R, rgba.G, rgba.B, rgba.A}
		return normalized[:channels], true
	}
	entry, ok := paletteEntry(source.Index, space, profile, channels)
	if !ok {
		return nil, false
	}
	result := make([]uint8, channels)
	for i := 0; i < channels; i++ {
		result[i] = scaleComponent(int(entry[i]), bits)
	}
	return result, true
}

// paletteEntry 按 Index 取调色板条目。内联 Palette 优先，其次是 Profile 指向的
// 原始二进制调色板；两者都没有或 Index 越界时返回 false，由调用方回退默认颜色。
func paletteEntry(index int, space *models.ColorSpace, profile []uint8, channels int) ([]uint8, bool) {
	if index < 0 {
		return nil, false
	}
	if space.Palette != nil && index < len(space.Palette.CV) {
		entry := space.Palette.CV[index]
		if len(entry) < channels {
			return nil, false
		}
		result := make([]uint8, channels)
		for i := 0; i < channels; i++ {
			value, ok := parseChannelValue(string(entry[i]))
			if !ok {
				return nil, false
			}
			result[i] = uint8(value)
		}
		return result, true
	}
	if len(profile) == 0 {
		return nil, false
	}
	// 原始调色板按通道数平铺：Index i 占 [i*channels, (i+1)*channels)。Index 越界
	// 时该区间超出文件长度，返回 false 而不是回绕取到别的颜色。
	start := index * channels
	if start+channels > len(profile) {
		return nil, false
	}
	result := make([]uint8, channels)
	copy(result, profile[start:start+channels])
	return result, true
}

// transformerOrderComponents 把分量重排成 coloricc 与 ICC 转换器约定的顺序。
//
// coloricc.DeviceCMYK8ToRGB 与 ICC 转换器都按 PDF DeviceCMYK 的青、品红、黄、
// 黑解释四分量，而 GB/T 33190 表 27 规定的 OFD CMYK 顺序是青、黄、品红、黑。
// 两条路径（ICC 与内置油墨模型）都必须在这里换序，否则同一份 OFD 在有无 CMYK
// ICC 配置的机器上会画出不同的颜色。
func transformerOrderComponents(space *models.ColorSpace, components []uint8) []uint8 {
	if len(components) < 4 || !strings.EqualFold(strings.TrimSpace(space.Type), "CMYK") {
		return components
	}
	return []uint8{components[0], components[2], components[1], components[3]}
}

// resolveColorSpaceColor 按颜色空间类型把 CTColor 的分量转换为 RGBA。
func resolveColorSpaceColor(space *models.ColorSpace, source models.CTColor, profile []uint8) color.RGBA {
	components, ok := colorSpaceComponents8(source, space, profile)
	if !ok {
		return ofdColorRGBA(source)
	}
	alpha := uint8(255)
	if source.Alpha != nil {
		alpha = *source.Alpha
	}
	switch strings.ToUpper(strings.TrimSpace(space.Type)) {
	case "GRAY":
		value := components[0]
		return premultiplied(value, value, value, alpha)
	case "CMYK":
		// OFD 的 CMYK 通道顺序是青、黄、品红、黑（GB/T 33190 表 27），与 PDF 的
		// DeviceCMYK 相反，换序后交给按 PDF 顺序实现的油墨模型。
		if len(components) < 4 {
			return ofdColorRGBA(source)
		}
		ink := transformerOrderComponents(space, components)
		r, g, b := cmykInkToRGB(ink[0], ink[1], ink[2], ink[3])
		return premultiplied(r, g, b, alpha)
	default:
		if len(components) < 3 {
			return ofdColorRGBA(source)
		}
		return premultiplied(components[0], components[1], components[2], alpha)
	}
}

// cmykInkToRGB 在没有可用 ICC Profile 时，用 Adobe/poppler 的 4 色印刷矩阵模型
// 把 CMYK 油墨量合成为 RGB，与 internal/pdf2ofd 的 cmykInkToRGB 保持一致，
// 避免同一 CMYK 颜色在转换与渲染两条路径上不同。
//
// 有 Profile 时由 colorSpaceTransformer 走 ICC 色彩管理，这里只按 Type 与
// 调色板近似解释分量。
func cmykInkToRGB(c, m, y, k uint8) (uint8, uint8, uint8) {
	return coloricc.DeviceCMYK8ToRGB(c, m, y, k)
}
