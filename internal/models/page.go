package models

import "github.com/zc310/ofd/pkg/spec"

// 内部快速入口与公开标准库入口使用相同的资源上限和错误标识。
const maxPageBlockDepth = spec.MaxPageBlockDepth

var errPageBlockDepth = spec.ErrPageBlockDepth

// PageContent 复用标准声明，并保留内部页面尺寸兼容策略。
type PageContent spec.PageContent

// EnsurePhysicalBox 确保页面区域存在且物理区域有效；这不是标准声明解码行为。
func (p *PageContent) EnsurePhysicalBox() {
	if p == nil {
		return
	}
	if p.Area == nil {
		p.Area = &CtPageArea{}
	}
	EnsurePhysicalBox(p.Area)
}

// ExpandCGTransformPositions 将按 TextCode 局部计数的 CGTransform.CodePosition
// 展开为整个文字对象内的字符位置。规范形式的全局递增位置保持原样；遇到位置重置
// 时，将后续位置解释为下一段 TextCode 内的偏移，兼容常见的逐字 TextCode 文档。
func ExpandCGTransformPositions(codes []TextCode, transforms []CTCGTransform) []CTCGTransform {
	if len(transforms) < 2 || len(codes) < 2 {
		return transforms
	}
	hasReset := false
	for i := 1; i < len(transforms); i++ {
		if transforms[i].CodePosition <= transforms[i-1].CodePosition {
			hasReset = true
			break
		}
	}
	if !hasReset {
		return transforms
	}

	starts := make([]int, len(codes))
	total := 0
	for i, code := range codes {
		starts[i] = total
		total += len([]rune(code.Value))
	}
	result := append([]CTCGTransform(nil), transforms...)
	scope, previous := 0, -1
	for i := range result {
		position := result[i].CodePosition
		if i > 0 && position <= previous {
			scope++
		}
		previous = position
		if scope >= len(codes) || position < 0 || position > len([]rune(codes[scope].Value)) {
			continue
		}
		result[i].CodePosition = starts[scope] + position
	}
	return result
}
