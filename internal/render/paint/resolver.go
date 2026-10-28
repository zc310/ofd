package paint

import (
	"image/color"
	"math"
	"sync"
	"sync/atomic"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// Resources 提供当前文档的颜色空间和 Profile 原始数据。
type Resources interface {
	GetColorSpace(id models.StID) *models.ColorSpace
	ReadColorProfile(name string) ([]byte, error)
}

const ctColorCacheCapacity = 256

// Resolver 持有单个文档视图的颜色缓存，零值可用且支持并发调用。
// 资源和颜色模型在使用期间必须保持只读；使用结束后调用 Close。
// 首次使用后不可复制，同一实例始终使用同一份文档资源。
type Resolver struct {
	ctColorMu     sync.Mutex
	ctColorCache  *cache.LRU[*models.CTColor, *Color]
	ctColorBuilds map[*models.CTColor]*ctColorBuild
	colorSpacesMu sync.Mutex
	colorSpaces   map[*models.ColorSpace]*colorSpaceResources
	closed        atomic.Bool
}

type ctColorBuild struct {
	once  sync.Once
	color Color
}

// Close 清空缓存，并阻止在途构建重新发布结果；可重复调用。
func (p *Resolver) Close() {
	p.closed.Store(true)
	p.ctColorMu.Lock()
	p.ctColorCache = nil
	p.ctColorBuilds = nil
	p.ctColorMu.Unlock()
	p.colorSpacesMu.Lock()
	p.colorSpaces = nil
	p.colorSpacesMu.Unlock()
}

// Resolve 返回独立的画笔值；渐变只读共享，同一颜色的并发构建只执行一次。
// 关闭后返回 nil，关闭前已开始的构建仍可完成，但不再进入缓存。
func (p *Resolver) Resolve(source *models.CTColor, resources Resources) *Color {
	if source == nil {
		return nil
	}
	p.ctColorMu.Lock()
	if p.closed.Load() {
		p.ctColorMu.Unlock()
		return nil
	}
	if p.ctColorCache != nil {
		if cc, ok := p.ctColorCache.Get(source); ok {
			ctColor := *cc
			p.ctColorMu.Unlock()
			return &ctColor
		}
	}
	build := p.ctColorBuilds[source]
	if build == nil {
		build = &ctColorBuild{}
		if p.ctColorBuilds == nil {
			p.ctColorBuilds = make(map[*models.CTColor]*ctColorBuild)
		}
		p.ctColorBuilds[source] = build
	}
	p.ctColorMu.Unlock()

	// ICC 读取、转换及网格构建均在缓存锁外执行；等待相同颜色也不阻塞其他颜色。
	build.once.Do(func() {
		cc := &build.color
		if source.Value != nil || source.ColorSpace != 0 || source.Index != 0 {
			cc.Value = p.RGBA(*source, resources)
			cc.HasValue = true
		}
		cc.Gradient = p.Gradient(source, identityGradientTransform, nil, resources)
		p.ctColorMu.Lock()
		defer p.ctColorMu.Unlock()
		delete(p.ctColorBuilds, source)
		if p.closed.Load() {
			return
		}
		if p.ctColorCache == nil {
			p.ctColorCache = cache.NewLRU[*models.CTColor, *Color](ctColorCacheCapacity, nil)
		}
		p.ctColorCache.Add(source, cc)
	})
	ctColor := build.color
	return &ctColor
}

// ResolveSolid 只准备路径样式的普通颜色，将着色渐变延迟到按 Boundary 放置时构建。
func (p *Resolver) ResolveSolid(source *models.CTColor, resources Resources) *Color {
	if source == nil || (source.AxialShd == nil && source.RadialShd == nil && source.GouraudShd == nil && source.LaGourandShd == nil && source.LaGouraudShd == nil) {
		return p.Resolve(source, resources)
	}
	if source.Value != nil || source.ColorSpace != 0 || source.Index != 0 {
		return &Color{Value: p.RGBA(*source, resources), HasValue: true}
	}
	return nil
}

// Gradient 构造渐变。transform 将局部控制点映射到采样空间，area 是局部着色区域，
// 用于网格渐变在区域内延续边缘颜色；不清楚区域时传 nil。
func (p *Resolver) Gradient(source *models.CTColor, transform func(models.StPos) geom.Point, area *geom.Rect, resources Resources) geom.Gradient {
	if source == nil {
		return nil
	}
	resolve := func(source models.CTColor) color.RGBA { return p.RGBA(source, resources) }
	var gradient geom.Gradient
	if shd := source.AxialShd; shd != nil {
		gradient = NewLinearGradient(shd, transform, resolve)
	}
	if shd := source.RadialShd; shd != nil {
		gradient = NewRadialGradient(shd, transform, resolve)
	}
	if shd := source.GouraudShd; shd != nil {
		gradient = NewGouraudGradient(shd, transform, area, resolve)
	}
	if shd := source.LaGourandShd; shd != nil {
		gradient = NewLatticeGradient(shd, transform, area, resolve)
	}
	if shd := source.LaGouraudShd; shd != nil {
		gradient = NewLatticeGradient(shd, transform, area, resolve)
	}
	if gradient != nil && source.Alpha != nil {
		gradient = ScaleOpacity(gradient, *source.Alpha)
	}
	return gradient
}

func identityGradientTransform(point models.StPos) geom.Point {
	return geom.Point{X: point.X, Y: point.Y}
}

func finiteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func graphicOpacity(alpha *uint8) uint8 {
	if alpha == nil {
		return 255
	}
	return *alpha
}
