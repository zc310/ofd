package render

import (
	"crypto/sha256"
	"image/color"
	"math"
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
	if transformer := p.colorSpaceTransformer(space); transformer != nil {
		if components, ok := colorSpaceBytes(source, space); ok {
			r, g, b := transformer.ToRGB(components)
			alpha := uint8(255)
			if source.Alpha != nil {
				alpha = *source.Alpha
			}
			return premultiplied(r, g, b, alpha)
		}
	}
	return resolveColorSpaceColor(space, source)
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

// colorSpaceBytes 返回颜色在自身颜色空间下的分量（0-255）。Value 优先，
// 否则按 Index 从调色板取分量。
func colorSpaceBytes(source models.CTColor, space *models.ColorSpace) ([]uint8, bool) {
	channels := colorSpaceChannels(space)
	if source.Value != nil {
		value := source.Value
		return []uint8{value.R, value.G, value.B, value.A}[:channels], true
	}
	if space.Palette == nil || source.Index < 0 || source.Index >= len(space.Palette.CV) {
		return nil, false
	}
	entry := space.Palette.CV[source.Index]
	if len(entry) < channels {
		return nil, false
	}
	result := make([]uint8, channels)
	for i := 0; i < channels; i++ {
		parsed, err := strconv.Atoi(strings.TrimSpace(string(entry[i])))
		if err != nil {
			return nil, false
		}
		result[i] = componentByte(parsed)
	}
	return result, true
}

// resolveColorSpaceColor 按颜色空间类型把 CTColor 的分量转换为 RGBA。
func resolveColorSpaceColor(space *models.ColorSpace, source models.CTColor) color.RGBA {
	components, ok := colorSpaceComponents(source, space)
	if !ok {
		return ofdColorRGBA(source)
	}
	alpha := uint8(255)
	if source.Alpha != nil {
		alpha = *source.Alpha
	}
	switch strings.ToUpper(strings.TrimSpace(space.Type)) {
	case "GRAY":
		if len(components) < 1 {
			return ofdColorRGBA(source)
		}
		value := componentByte(components[0])
		return premultiplied(value, value, value, alpha)
	case "CMYK":
		if len(components) < 4 {
			return ofdColorRGBA(source)
		}
		r, g, b := cmykInkToRGB(componentByte(components[0]), componentByte(components[1]), componentByte(components[2]), componentByte(components[3]))
		return premultiplied(r, g, b, alpha)
	default:
		if len(components) < 3 {
			return ofdColorRGBA(source)
		}
		return premultiplied(componentByte(components[0]), componentByte(components[1]), componentByte(components[2]), alpha)
	}
}

// colorSpaceComponents 返回颜色在当前颜色空间下的分量：优先使用 Value，
// 否则按 Index 从调色板取分量。
func colorSpaceComponents(source models.CTColor, space *models.ColorSpace) ([]int, bool) {
	if source.Value != nil {
		value := source.Value
		return []int{int(value.R), int(value.G), int(value.B), int(value.A)}, true
	}
	if space.Palette == nil || source.Index < 0 || source.Index >= len(space.Palette.CV) {
		return nil, false
	}
	entry := space.Palette.CV[source.Index]
	components := make([]int, 0, len(entry))
	for _, item := range entry {
		value, err := strconv.Atoi(strings.TrimSpace(string(item)))
		if err != nil {
			return nil, false
		}
		components = append(components, value)
	}
	return components, true
}

func componentByte(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

// cmykInkToRGB 在没有可用 ICC Profile 时，用 Adobe/poppler 的 4 色印刷矩阵模型
// 把 CMYK 油墨量合成为 RGB，与 internal/pdf2ofd 的 cmykInkToRGB 保持一致，
// 避免同一 CMYK 颜色在转换与渲染两条路径上不同。
//
// 有 Profile 时由 colorSpaceTransformer 走 ICC 色彩管理，这里只按 Type 与
// 调色板近似解释分量。
func cmykInkToRGB(c, m, y, k uint8) (uint8, uint8, uint8) {
	r, g, b := coloricc.DeviceCMYKToRGB(float64(c)/255, float64(m)/255, float64(y)/255, float64(k)/255)
	return uint8(math.Round(255 * r)), uint8(math.Round(255 * g)), uint8(math.Round(255 * b))
}
