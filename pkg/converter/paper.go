package converter

import (
	"fmt"
	"strings"
)

// Paper 描述输出纸张尺寸（毫米）与方向。
type Paper struct {
	// Width 是纸张宽度（毫米）。
	Width float64
	// Height 是纸张高度（毫米）。
	Height float64
	// Landscape 为真时交换宽高。
	Landscape bool
}

// NamedPaperSizes 是支持的命名纸张尺寸（毫米）。
var NamedPaperSizes = map[string][2]float64{
	"A4":     {210, 297},
	"A3":     {297, 420},
	"A5":     {148, 210},
	"Letter": {215.9, 279.4},
	"Legal":  {215.9, 355.6},
	"B5":     {176, 250},
	"16开":    {185, 260},
	"16K":    {185, 260},
}

// DefaultPaper 返回默认纸张 A4。
func DefaultPaper() Paper {
	size := NamedPaperSizes["A4"]
	return Paper{Width: size[0], Height: size[1]}
}

// PaperByName 按名称（大小写不敏感，支持 A4/A3/A5/Letter/Legal/B5/16开）返回纸张。
func PaperByName(name string) (Paper, error) {
	key := strings.TrimSpace(name)
	for candidate, size := range NamedPaperSizes {
		if strings.EqualFold(candidate, key) {
			return Paper{Width: size[0], Height: size[1]}, nil
		}
	}
	return Paper{}, fmt.Errorf("不支持的纸张尺寸: %s", name)
}

// EffectiveDimensions 返回考虑方向后的实际宽高（毫米）。
func (p Paper) EffectiveDimensions() (float64, float64) {
	if p.Landscape {
		return p.Height, p.Width
	}
	return p.Width, p.Height
}
