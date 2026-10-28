// Package renderstate 管理单次绘制的资源预算、诊断聚合与报告快照。
// 每份 Budget 由一次同步遍历持有；缓存中的预算仅用于读取已完成的用量。
package renderstate

import (
	"log/slog"
	"math"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

const (
	// MaxCompositeExpansions 限制一次遍历的复合图元展开总数。
	MaxCompositeExpansions = 10000
	// MaxCompositeResourceExpansions 限制同一复合资源的重复展开。
	MaxCompositeResourceExpansions = 256
	// MaxPatternTiles 限制一次遍历的底纹图块总数。
	MaxPatternTiles = 100000
	// MaxOffscreenPixels 限制一次遍历累计分配的离屏像素。
	MaxOffscreenPixels = 200000000
)

// Limit 标识日志去重所用的预算拒绝原因。
type Limit uint8

const (
	limitCompositeExpansions Limit = 1 << iota
	limitCompositeResource
	// LimitPatternTiles 也用于平铺计划在分配前发现索引或数量超限的情况。
	LimitPatternTiles
	limitInvalidOffscreen
	limitOffscreenPixels
)

// Budget 保存本次遍历的额度及诊断位置。Report 为 nil 时不收集报告。
// ObjectID 和 ResourceID 由调用方在进入、退出图元时保存和恢复。
type Budget struct {
	Report               *ReportBuilder
	ObjectID, ResourceID models.StID
	usage                Stats
	compositeResources   map[models.StID]int
	reportedLimits       Limit
}

// Reset 清空用量、诊断位置与报告关联，复用资源计数表。
func (b *Budget) Reset() {
	b.usage = Stats{}
	if b.compositeResources != nil {
		clear(b.compositeResources)
	} else {
		b.compositeResources = make(map[models.StID]int)
	}
	b.reportedLimits = 0
	b.Report = nil
	b.ObjectID, b.ResourceID = 0, 0
}

// AllowComposite 在展开前累计复合图元用量，超限时保留已消费额度。
func (b *Budget) AllowComposite(resourceID models.StID) bool {
	if b.compositeResources == nil {
		b.compositeResources = make(map[models.StID]int)
	}
	if b.usage.CompositeExpansions >= MaxCompositeExpansions {
		b.ReportLimit(limitCompositeExpansions, "composite_expansions", "resource_id", resourceID, "used", b.usage.CompositeExpansions, "limit", MaxCompositeExpansions)
		return false
	}
	if b.compositeResources[resourceID] >= MaxCompositeResourceExpansions {
		b.ReportLimit(limitCompositeResource, "composite_resource_expansions", "resource_id", resourceID, "used", b.compositeResources[resourceID], "limit", MaxCompositeResourceExpansions)
		return false
	}
	b.usage.CompositeExpansions++
	b.compositeResources[resourceID]++
	if b.Report != nil {
		b.Report.Stats.CompositeExpansions++
	}
	return true
}

// AllowPatternTiles 在绘制前累计图块数。
func (b *Budget) AllowPatternTiles(count int) bool {
	if count < 0 || b.usage.PatternTiles > MaxPatternTiles-count {
		b.ReportLimit(LimitPatternTiles, "pattern_tiles", "used", b.usage.PatternTiles, "requested", count, "limit", MaxPatternTiles)
		return false
	}
	b.usage.PatternTiles += count
	if b.Report != nil {
		b.Report.Stats.PatternTiles += count
	}
	return true
}

// AllowOffscreenPixels 在分配前按毫米尺寸和 DPI 累计离屏像素。
func (b *Budget) AllowOffscreenPixels(width, height, dpi float64) bool {
	if width <= 0 || height <= 0 || dpi <= 0 || math.IsNaN(width) || math.IsInf(width, 0) ||
		math.IsNaN(height) || math.IsInf(height, 0) || math.IsNaN(dpi) || math.IsInf(dpi, 0) {
		b.ReportLimit(limitInvalidOffscreen, "invalid_offscreen_size", "width", width, "height", height, "dpi", dpi)
		return false
	}
	pixels := width * height * geom.DPI(dpi).DPMM() * geom.DPI(dpi).DPMM()
	if math.IsNaN(pixels) || math.IsInf(pixels, 0) || b.usage.OffscreenPixels > MaxOffscreenPixels-pixels {
		b.ReportLimit(limitOffscreenPixels, "offscreen_pixels", "used", b.usage.OffscreenPixels, "requested", pixels, "limit", MaxOffscreenPixels)
		return false
	}
	b.usage.OffscreenPixels += pixels
	if b.Report != nil {
		b.Report.Stats.OffscreenPixels += pixels
	}
	return true
}

// ReportLimit 对每种拒绝原因仅输出一次 Debug 日志，报告仍按诊断事件累计次数。
func (b *Budget) ReportLimit(flag Limit, reason string, attrs ...any) {
	b.Diagnose("budget_"+reason, "绘制超出资源预算", b.ResourceID)
	if b.reportedLimits&flag != 0 {
		return
	}
	b.reportedLimits |= flag
	slog.Debug("渲染资源预算拒绝图元", append([]any{"budget", reason}, attrs...)...)
}

// Usage 返回当前资源用量的独立值；图元访问数由报告统计，不计入此处。
func (b *Budget) Usage() Stats { return b.usage }

// CompositeCount 返回指定复合资源已展开的次数。
func (b *Budget) CompositeCount(resource models.StID) int { return b.compositeResources[resource] }

// HasUsage 判断本次遍历是否已消费资源预算。
func (b *Budget) HasUsage() bool {
	return b.usage.CompositeExpansions != 0 || b.usage.PatternTiles != 0 || b.usage.OffscreenPixels != 0
}

// CanReuse 判断缓存内容的用量能否计入当前剩余额度，不修改任一预算。
func (b *Budget) CanReuse(usage *Budget) bool {
	if b.usage.CompositeExpansions > MaxCompositeExpansions-usage.usage.CompositeExpansions ||
		b.usage.PatternTiles > MaxPatternTiles-usage.usage.PatternTiles ||
		b.usage.OffscreenPixels > MaxOffscreenPixels-usage.usage.OffscreenPixels {
		return false
	}
	for id, count := range usage.compositeResources {
		if b.compositeResources[id] > MaxCompositeResourceExpansions-count {
			return false
		}
	}
	return true
}

// AddUsage 在 CanReuse 通过且缓存内容已绘制后累加用量。
// 不更新报告统计；收集逐图元报告时，调用方应直接遍历而非复用缓存表面。
func (b *Budget) AddUsage(usage *Budget) {
	b.usage.CompositeExpansions += usage.usage.CompositeExpansions
	if b.compositeResources == nil {
		b.compositeResources = make(map[models.StID]int)
	}
	for id, count := range usage.compositeResources {
		b.compositeResources[id] += count
	}
	b.usage.PatternTiles += usage.usage.PatternTiles
	b.usage.OffscreenPixels += usage.usage.OffscreenPixels
}
