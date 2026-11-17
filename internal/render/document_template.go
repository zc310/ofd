package render

import (
	"sync"
	"sync/atomic"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/pkg/spec"
)

const templateScopeCapacity = 8

type templateScopeKey struct {
	resources *parser.Document
	content   *models.PageContent
	fallbacks [32]byte
}

// 缓存与在途绘制各持一个引用，淘汰不等待绘制，也不会提前关闭字体。
type templateScopeEntry struct {
	doc  *Document
	refs atomic.Int32
}

func (e *templateScopeEntry) release() {
	if e.refs.Add(-1) == 0 {
		e.doc.Close()
	}
}

// templateScope 为专属资源复用有界绘制上下文，字体、颜色和图像缓存不与正文共用。
// 原文档页面租约由调用方持有；关闭只释放绘制资源，不关闭借用的解析视图与 ZIP。
func (p *Document) templateScope(id spec.StID) (*models.PageContent, *Document, func(), error) {
	content, resources, err := p.LoadTemplateResources(id)
	if err != nil || resources == nil {
		return content, p, func() {}, err
	}
	fallbacks := p.fallbackFontFamilies()
	key := templateScopeKey{resources: resources, content: content, fallbacks: sealCacheKey(nil, fallbacks)}
	p.templateScopesMu.Lock()
	if p.closed.Load() {
		p.templateScopesMu.Unlock()
		return nil, nil, func() {}, errDocumentClosed
	}
	if p.templateScopes != nil {
		if entry, ok := p.templateScopes.Get(key); ok {
			entry.refs.Add(1)
			p.templateScopesMu.Unlock()
			return content, entry.doc, entry.release, nil
		}
	}
	p.templateScopesMu.Unlock()
	scope := NewDocument(p.background, resources, p.dpi)
	for _, family := range fallbacks {
		if err := scope.UseFallbackFont(family); err != nil {
			scope.Close()
			return nil, nil, func() {}, err
		}
	}
	if scope.fonts != nil {
		scope.fonts.RegisterPageGlyphs(resources, nil, content)
	}
	entry := &templateScopeEntry{doc: scope}
	entry.refs.Store(2)
	p.templateScopesMu.Lock()
	if p.closed.Load() {
		p.templateScopesMu.Unlock()
		scope.Close()
		return nil, nil, func() {}, errDocumentClosed
	}
	if p.templateScopes == nil {
		p.templateScopes = cache.NewLRU[templateScopeKey, *templateScopeEntry](templateScopeCapacity, func(_ templateScopeKey, e *templateScopeEntry) { e.release() })
	}
	if existing, ok := p.templateScopes.Get(key); ok {
		existing.refs.Add(1)
		p.templateScopesMu.Unlock()
		scope.Close()
		return content, existing.doc, existing.release, nil
	}
	p.templateScopes.Add(key, entry)
	p.templateScopesMu.Unlock()
	return content, scope, entry.release, nil
}

// templateSurfaceDrawer 是可选接口：栅格后端直接绘制，
// 矢量后端可保留并复用只读的模板表面。
type templateSurfaceDrawer interface {
	TemplateSurfaces() bool
	DrawTemplateSurface(surface VectorSurface) bool
}

type templateSceneKey struct {
	content    *models.PageContent
	box        spec.StBox
	generation uint64
}

type templateScene struct {
	surface VectorSurface
	usage   renderBudget
}

type templateSceneBuild struct {
	once  sync.Once
	scene *templateScene
}

// 模板仍在原来的背景/前景位置引用，依据 GB/T 33190—2016 §7.7 表12、13。
// 缓存只省去重复绘制，不改变模板顺序或页面独有的链接、正文和注解。
func (p *Document) drawCachedTemplate(ctx DrawContext, content *models.PageContent, pb spec.StBox, budget *renderBudget) bool {
	drawer, ok := ctx.(templateSurfaceDrawer)
	factory := drawing.VectorSurfaceFactory()
	if !ok || !drawer.TemplateSurfaces() || !budget.templateCacheable || budget.Report != nil || factory == nil {
		return false
	}
	// 字形映射与回退字体更新后必须重建画布。自定义字体引擎未提供版本时
	// 沿用直接绘制，避免对其可变字体状态作出假设。
	fonts, ok := p.fonts.(interface{ CacheGeneration() uint64 })
	if !ok || p.closed.Load() || !pb.IsFinite() || pb.Width <= 0 || pb.Height <= 0 {
		return false
	}
	p.templateScenesMu.Lock()
	if p.closed.Load() {
		p.templateScenesMu.Unlock()
		return false
	}
	key := templateSceneKey{content: content, box: pb, generation: fonts.CacheGeneration()}
	if p.templateScenes == nil || p.templateGeneration != key.generation {
		// 旧版本模板不会再次命中，却会通过文字对象保留整份旧字体。
		// 仅保留当前字体版本；已返回给绘制方的表面仍由绘制方持有。
		p.templateScenes = cache.NewWeightedLRU[templateSceneKey, *templateScene](32, 32<<20, nil)
		p.templateGeneration = key.generation
	}
	scene, ok := p.templateScenes.Get(key)
	if !ok {
		// 使用完整预算构建独立模板；前序内容已消耗预算时沿用直接绘制，
		// 避免先构建完整模板、再因剩余预算不足而重复绘制。
		if budget.HasUsage() {
			p.templateScenesMu.Unlock()
			return false
		}
		build := p.templateBuilds[key]
		if build == nil {
			build = &templateSceneBuild{}
			if p.templateBuilds == nil {
				p.templateBuilds = make(map[templateSceneKey]*templateSceneBuild)
			}
			p.templateBuilds[key] = build
		}
		p.templateScenesMu.Unlock()
		// 同键构建合并；字体加载、几何计算及等待均不占用文档缓存锁。
		build.once.Do(func() {
			built := &templateScene{}
			built.usage.reset()
			built.surface = factory(pb.Width, pb.Height, func(ctx DrawContext) {
				p.drawLayersWithBudget(ctx, content.Content.Layer, pb, &built.usage)
			})
			p.templateScenesMu.Lock()
			defer p.templateScenesMu.Unlock()
			delete(p.templateBuilds, key)
			if p.closed.Load() || fonts.CacheGeneration() != key.generation || built.surface == nil {
				return
			}
			build.scene = built
			// 无法估量的自定义表面仅用于本次共享构建，不加入长期缓存。
			if weighted, ok := built.surface.(interface{ Weight() int64 }); ok {
				p.templateScenes.AddWeighted(key, built, weighted.Weight())
			}
		})
		scene = build.scene
		if scene == nil {
			return false
		}
	} else {
		p.templateScenesMu.Unlock()
	}
	if p.closed.Load() || fonts.CacheGeneration() != key.generation || !budget.CanReuse(&scene.usage.Budget) || !drawer.DrawTemplateSurface(scene.surface) {
		return false
	}
	budget.AddUsage(&scene.usage.Budget)
	return true
}
