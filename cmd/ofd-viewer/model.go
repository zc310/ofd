package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/spec"
)

// pageScene 的记录仅在所属 documentModel 存活时重放。
type pageScene interface {
	Width() float64
	Height() float64
	RenderTo(renderer canvas.Renderer)
}

type viewerError string

func (e viewerError) Error() string { return string(e) }

const (
	errNoPages          viewerError = "文档没有页面"
	errPageRange        viewerError = "页码超出范围"
	errUnsupportedScene viewerError = "当前矢量后端不支持 canvas 重放"
	errInvalidPageSize  viewerError = "页面尺寸无效"
	errNoDocument       viewerError = "尚未打开文档"
	errPageOperation    viewerError = "加载或绘制页面失败"
)

type viewerPage struct {
	document *render.Document
	page     *parser.Page
}

type documentModel struct {
	ofd         *parser.OFD
	view        *crypt.View
	documents   []*render.Document
	pages       []viewerPage
	scene       pageScene
	current     int
	filename    string
	input       []byte
	scenes      *cache.LRU[int, pageScene]
	pageIndexes map[*render.Document]map[spec.StID]int
}

const (
	pageSceneCacheEntries = 4
	pageSceneCacheBytes   = 64 << 20
)

func openDocument(filename string) (*documentModel, error) {
	return openDocumentInput(filename, filename)
}

func openDocumentInput(filename string, input any) (*documentModel, error) {
	ofd, err := parser.NewOFD(input)
	if err != nil {
		if ofd != nil {
			_ = ofd.Close()
		}
		return nil, err
	}
	m := &documentModel{ofd: ofd, filename: filename, scenes: cache.NewWeightedLRU[int, pageScene](pageSceneCacheEntries, pageSceneCacheBytes, nil)}
	ready := false
	defer func() {
		if !ready {
			m.close()
		}
	}()
	for _, document := range ofd.Documents {
		doc := render.NewDocument(color.Transparent, document, geom.DPI(96))
		m.documents = append(m.documents, doc)
		if err := applyViewerFallback(doc); err != nil {
			return nil, err
		}
		for _, page := range document.Pages {
			m.pages = append(m.pages, viewerPage{document: doc, page: page})
		}
	}
	if len(m.pages) == 0 {
		return nil, errNoPages
	}
	m.pageIndexes = make(map[*render.Document]map[spec.StID]int)
	for index, page := range m.pages {
		if m.pageIndexes[page.document] == nil {
			m.pageIndexes[page.document] = make(map[spec.StID]int)
		}
		m.pageIndexes[page.document][page.page.ID] = index
	}
	if err := m.selectPage(0); err != nil {
		return nil, err
	}
	ready = true
	return m, nil
}

func (m *documentModel) close() {
	if m == nil {
		return
	}
	m.scene = nil
	m.scenes = nil
	for _, document := range m.documents {
		document.Close()
	}
	_ = m.ofd.Close()
	if m.view != nil {
		_ = m.view.Close()
	}
}

// selectPage 按需记录页面，有限缓存只持有估算重量在预算内的矢量记录。
// 记录仅在所属文档存活期间使用；切页失败时保留上一页。
func (m *documentModel) selectPage(index int) error {
	scene, err := m.sceneAt(index)
	if err != nil {
		return err
	}
	m.current, m.scene = index, scene
	return nil
}

// sceneAt 不改变当前页；调用方必须串行访问文档，并在重放时持有会话租约。
func (m *documentModel) sceneAt(index int) (pageScene, error) { //nolint:ireturn // 页面记录由渲染后端提供接口实现。
	if index < 0 || len(m.pages) <= index {
		return nil, fmt.Errorf("%w: %d", errPageRange, index+1)
	}
	if m.scene != nil && index == m.current {
		return m.scene, nil
	}
	if m.scenes != nil {
		if scene, ok := m.scenes.Get(index); ok {
			return scene, nil
		}
	}
	p := m.pages[index]
	surface, err := p.document.Page(p.page)
	if err != nil {
		return nil, err
	}
	scene, ok := surface.(pageScene)
	if !ok {
		return nil, errUnsupportedScene
	}
	if _, ok := fitPage(image.Pt(100, 100), scene.Width(), scene.Height()); !ok {
		return nil, errInvalidPageSize
	}
	resolve := func(id spec.StID) (render.LinkTargetRef, bool) {
		target, ok := m.pageIndexes[p.document][id]
		if !ok {
			return render.LinkTargetRef{}, false
		}
		metadata := readPageMetadata(m.pages[target])
		return render.LinkTargetRef{Index: target, Height: metadata.size.height}, metadata.err == nil
	}
	links, err := p.document.PageLinksWithVisibilityE(p.page, resolve, render.PageVisibility{})
	if err != nil {
		return nil, err
	}
	scene = newLinkedPageScene(scene, links)
	if weighted, ok := scene.(interface{ Weight() int64 }); ok && m.scenes != nil {
		if weight := weighted.Weight(); weight > 0 && weight <= pageSceneCacheBytes {
			m.scenes.AddWeighted(index, scene, weight)
		}
	}
	return scene, nil
}

// fitPage 使用可用上限，而不是 Max-Min，避免固定布局约束下得到零尺寸。
func fitPage(available image.Point, width, height float64) (image.Point, bool) {
	if available.X <= 0 || available.Y <= 0 || width <= 0 || height <= 0 ||
		math.IsNaN(width) || math.IsNaN(height) || math.IsInf(width, 0) || math.IsInf(height, 0) {
		return image.Point{}, false
	}
	scale := math.Min(float64(available.X)/width, float64(available.Y)/height)
	return image.Pt(max(1, min(available.X, int(math.Round(width*scale)))),
		max(1, min(available.Y, int(math.Round(height*scale))))), true
}
