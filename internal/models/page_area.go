package models

import (
	"math"

	"github.com/zc310/ofd/pkg/spec"
)

// EnsurePhysicalBox 保留页面物理区域的兼容兜底，无效尺寸使用 A4。
// 这是读取/显示策略，不属于 pkg/spec 的标准声明解码。
func EnsurePhysicalBox(p *spec.PageArea) {
	if p == nil {
		return
	}
	if p.PhysicalBox.Width <= 0 || p.PhysicalBox.Height <= 0 ||
		math.IsNaN(p.PhysicalBox.Width) || math.IsNaN(p.PhysicalBox.Height) ||
		math.IsInf(p.PhysicalBox.Width, 0) || math.IsInf(p.PhysicalBox.Height, 0) {
		p.PhysicalBox = spec.StBox{Width: 210, Height: 297}
	}
}
