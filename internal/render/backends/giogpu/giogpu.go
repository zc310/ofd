// Package giogpu 提供基于 Gio 无头 GPU 的页面栅格后端原型。
//
// 后端复用 canvas 后端的 OFD 页面记录，只在最终光栅化阶段改用 Gio 的
// OpenGL/Vulkan 渲染器，再回读为 RGBA 供 PNG/JPEG 等编码器使用。GPU
// 上下文不可用或帧执行失败时自动回退 canvas CPU 后端，保证导出结果可用。
//
// 空白导入本包即可注册 "giogpu" 后端：
//
//	import _ "github.com/zc310/ofd/internal/render/backends/giogpu"
//
// 配合转换器时传入 converter.RasterBackend("giogpu") 选择该后端；未空白
// 导入本包的二进制不会引入 Gio/EGL/Vulkan 依赖。
//
// 该后端仍会把最终位图读回 CPU，PNG 编码也在 CPU 完成；它适合高 DPI、
// 大页面或图片/合成较多的场景。同一进程内会复用最近一次像素尺寸的离屏
// 窗口，连续导出同尺寸页面时无需重复创建 GPU 上下文；需要主动归还资源时
// 调用 Close。与 canvas 的逐像素结果可能存在抗锯齿、渐变插值和颜色空间
// 差异，不能用于像素级一致性校验。
//
// Stats 返回进程级 GPU 成功帧数、CPU 回退次数和最近回退原因；Rasterize
// 是不自动回退的严格入口，适合测试或需要自行处理 GPU 错误的调用方。
package giogpu

import (
	"errors"
	"fmt"
	"image"

	tcanvas "github.com/zc310/ofd/internal/thirdparty/canvas"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"

	// 注册并复用内置 canvas 后端；本后端只替换最终光栅化步骤。
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
)

// BackendName 是注册到 drawing 后端表的名称，同时对应插件包目录名。
const BackendName = "giogpu"

var (
	errCanvasBackendMissing = errors.New("giogpu 依赖 canvas 后端，但 canvas 后端尚未注册")
	errSceneUnsupported     = errors.New("canvas 后端不支持导出页面记录，无法使用 giogpu")
	errSceneMissing         = errors.New("canvas 页面记录为空，无法使用 giogpu")
)

func init() { //nolint:gochecknoinits // 空白导入后端包时注册 drawing 工厂。
	// 包内 init 只执行一次，使用普通注册即可；重复导入不会重复注册。
	if err := drawing.RegisterBackend(BackendName, newBackend); err != nil {
		panic(fmt.Sprintf("注册 %s 渲染后端失败: %v", BackendName, err))
	}
}

// canvasSceneProvider 由 canvas 光栅后端实现，用于取出累积的页面记录。
type canvasSceneProvider interface {
	CanvasScene() *tcanvas.Canvas
}

// gpuBackend 把 DrawContext 调用转发给 canvas 后端记录页面，Raster 时再
// 用 GPU 重放该记录。嵌入 drawing.Backend 提供固定的 DrawContext 方法；
// canvas 后端额外实现的文字、模板、裁剪等可选能力在下方逐项转发，避免
// 核心因能力缺失走不同的回退路径。
type gpuBackend struct {
	drawing.Backend

	scene      *tcanvas.Canvas
	resolution geom.Resolution
}

func newBackend(width, height float64, resolution geom.Resolution) (drawing.Backend, error) {
	factory, ok := drawing.LookupBackend(drawing.BackendCanvas)
	if !ok {
		return nil, errCanvasBackendMissing
	}
	base, err := factory(width, height, resolution)
	if err != nil {
		return nil, fmt.Errorf("创建 canvas 页面记录失败: %w", err)
	}
	provider, ok := base.(canvasSceneProvider)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errSceneUnsupported, base)
	}
	scene := provider.CanvasScene()
	if scene == nil {
		return nil, errSceneMissing
	}
	return &gpuBackend{
		Backend:    base,
		scene:      scene,
		resolution: resolution,
	}, nil
}

// Raster 使用 GPU 重放页面记录；失败时记录日志并回退 canvas CPU 光栅化。
func (b *gpuBackend) Raster() *image.RGBA {
	img, err := Rasterize(b.scene, b.resolution)
	if err != nil {
		recordFallback(err)
		return b.Backend.Raster()
	}
	return img
}

// 以下可选接口不能靠嵌入 drawing.Backend 自动提升：嵌入接口只会提升该接口
// 声明的方法，canvas 后端额外实现的文字、模板、裁剪等能力会丢失，导致核心
// 走不同的回退路径。这里逐项转发，保持与 canvas 后端相同的绘制语义。

func (b *gpuBackend) RasterResolution() geom.Resolution { return b.resolution }

func (b *gpuBackend) TextPath(path *geom.Path, x, y float64) {
	b.DrawPath(x, y, path)
}

func (b *gpuBackend) RenderScene(scene any, m geom.Matrix) {
	if renderer, ok := b.Backend.(interface {
		RenderScene(scene any, m geom.Matrix)
	}); ok {
		renderer.RenderScene(scene, m)
	}
}

type textDrawer interface {
	DrawTextLine(run drawing.TextRun)
}

func (b *gpuBackend) DrawTextLine(run drawing.TextRun) {
	if drawer, ok := b.Backend.(textDrawer); ok {
		drawer.DrawTextLine(run)
	}
}

type clippedTextDrawer interface {
	DrawClippedTextLine(run drawing.TextRun, clip *geom.Path) bool
}

func (b *gpuBackend) DrawClippedTextLine(run drawing.TextRun, clip *geom.Path) bool {
	if drawer, ok := b.Backend.(clippedTextDrawer); ok {
		return drawer.DrawClippedTextLine(run, clip)
	}
	return false
}

type clippedPathDrawer interface {
	DrawClippedPath(path, clip *geom.Path) bool
}

func (b *gpuBackend) DrawClippedPath(path, clip *geom.Path) bool {
	if drawer, ok := b.Backend.(clippedPathDrawer); ok {
		return drawer.DrawClippedPath(path, clip)
	}
	return false
}

type sharedPaintTextDrawer interface {
	DrawSharedPaintTextLine(run drawing.TextRun, clip *geom.Path, paint geom.Paint, paintMatrix geom.Matrix) bool
}

func (b *gpuBackend) DrawSharedPaintTextLine(run drawing.TextRun, clip *geom.Path, paint geom.Paint, paintMatrix geom.Matrix) bool {
	if drawer, ok := b.Backend.(sharedPaintTextDrawer); ok {
		return drawer.DrawSharedPaintTextLine(run, clip, paint, paintMatrix)
	}
	return false
}

type semanticTextDrawer interface {
	DrawSemanticTextLine(run drawing.TextRun)
}

func (b *gpuBackend) DrawSemanticTextLine(run drawing.TextRun) {
	if drawer, ok := b.Backend.(semanticTextDrawer); ok {
		drawer.DrawSemanticTextLine(run)
	}
}

func (b *gpuBackend) ShadingText() bool {
	if backend, ok := b.Backend.(interface{ ShadingText() bool }); ok {
		return backend.ShadingText()
	}
	return false
}

func (b *gpuBackend) TemplateSurfaces() bool {
	if backend, ok := b.Backend.(interface{ TemplateSurfaces() bool }); ok {
		return backend.TemplateSurfaces()
	}
	return false
}

func (b *gpuBackend) DrawTemplateSurface(surface drawing.VectorSurface) bool {
	if backend, ok := b.Backend.(interface {
		DrawTemplateSurface(surface drawing.VectorSurface) bool
	}); ok {
		return backend.DrawTemplateSurface(surface)
	}
	return false
}

func (b *gpuBackend) RenderClippedImage(img image.Image, m geom.Matrix, clip *geom.Path) bool {
	if backend, ok := b.Backend.(interface {
		RenderClippedImage(img image.Image, m geom.Matrix, clip *geom.Path) bool
	}); ok {
		return backend.RenderClippedImage(img, m, clip)
	}
	return false
}

var _ drawing.Backend = (*gpuBackend)(nil)
