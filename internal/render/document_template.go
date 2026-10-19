package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/utils"
)

// templateSurfaceDrawer 是可选接口：栅格后端直接绘制，
// 矢量后端可保留并复用只读的模板表面。
type templateSurfaceDrawer interface {
	TemplateSurfaces() bool
	DrawTemplateSurface(surface VectorSurface) bool
}

type templateSceneKey struct {
	content    *models.PageContent
	box        models.StBox
	generation uint64
}

type templateScene struct {
	surface VectorSurface
	usage   renderBudget
}

// 模板仍在原来的背景/前景位置引用，依据 GB/T 33190—2016 §7.7 表12、13。
// 缓存只省去重复绘制，不改变模板顺序或页面独有的链接、正文和注解。
func (p *Document) drawCachedTemplate(ctx DrawContext, content *models.PageContent, pb models.StBox, budget *renderBudget) bool {
	drawer, ok := ctx.(templateSurfaceDrawer)
	if !ok || !drawer.TemplateSurfaces() || !budget.templateCacheable || len(content.PageRes) != 0 || newVectorSurface == nil {
		return false
	}
	// 字形映射与回退字体更新后必须重建画布。自定义字体引擎未提供版本时
	// 沿用直接绘制，避免对其可变字体状态作出假设。
	fonts, ok := p.fonts.(interface{ CacheGeneration() uint64 })
	if !ok || p.closed.Load() || !pb.IsFinite() || pb.Width <= 0 || pb.Height <= 0 {
		return false
	}
	p.templateScenesMu.Lock()
	defer p.templateScenesMu.Unlock()
	if p.closed.Load() {
		return false
	}
	key := templateSceneKey{content: content, box: pb, generation: fonts.CacheGeneration()}
	if p.templateScenes == nil {
		p.templateScenes = utils.NewWeightedLRU[templateSceneKey, *templateScene](32, 32<<20, nil)
	}
	scene, ok := p.templateScenes.Get(key)
	if !ok {
		// 使用完整预算构建独立模板；前序内容已消耗预算时沿用直接绘制，
		// 避免先构建完整模板、再因剩余预算不足而重复绘制。
		if budget.compositeExpansions != 0 || budget.patternTiles != 0 || budget.offscreenPixels != 0 {
			return false
		}
		scene = &templateScene{}
		scene.usage.reset()
		scene.surface = newVectorSurface(pb.Width, pb.Height, func(ctx DrawContext) {
			p.drawLayersWithBudget(ctx, content.Content.Layer, pb, &scene.usage)
		})
		if p.closed.Load() || fonts.CacheGeneration() != key.generation {
			return false
		}
		// 无法估量的自定义表面不加入缓存，仍可用于本次绘制。
		if weighted, ok := scene.surface.(interface{ Weight() int64 }); ok {
			p.templateScenes.AddWeighted(key, scene, weighted.Weight())
		}
	}
	if !budget.canReuseTemplate(&scene.usage) || !drawer.DrawTemplateSurface(scene.surface) {
		return false
	}
	budget.compositeExpansions += scene.usage.compositeExpansions
	for id, count := range scene.usage.compositeResources {
		budget.compositeResources[id] += count
	}
	budget.patternTiles += scene.usage.patternTiles
	budget.offscreenPixels += scene.usage.offscreenPixels
	return true
}

// 命中缓存仍计入本页的资源预算；剩余预算不足时按原流程逐图元绘制。
func (b *renderBudget) canReuseTemplate(usage *renderBudget) bool {
	if b.compositeExpansions > maxCompositeExpansions-usage.compositeExpansions ||
		b.patternTiles > maxPatternTilesPerRender-usage.patternTiles ||
		b.offscreenPixels > maxOffscreenPixels-usage.offscreenPixels {
		return false
	}
	for id, count := range usage.compositeResources {
		if b.compositeResources[id] > maxCompositeResourceExpansions-count {
			return false
		}
	}
	return true
}
