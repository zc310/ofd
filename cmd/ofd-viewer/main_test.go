package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"image"
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	fyneCanvas "fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/version"
)

func TestPageAtCenter(t *testing.T) {
	bounds := make([]pageBound, 5)
	for i := range bounds {
		bounds[i] = pageBound{
			position: fyne.NewPos(0, float32(i*200)),
			size:     fyne.NewSize(100, 180),
		}
	}
	// 每页占 [i*200, i*200+180]，页间留 20 单位空隙。
	tests := []struct {
		name    string
		centerY float32
		want    int
	}{
		{"第一页顶部", 0, 0},
		{"第一页中部", 90, 0},
		{"页间空隙归属后一页", 190, 1},
		{"第二页中部", 290, 1},
		{"最后一页中部", 890, 4},
		{"越过最后一页", 5000, 4},
	}
	for _, test := range tests {
		if got := pageAtCenter(bounds, test.centerY); got != test.want {
			t.Errorf("%s: pageAtCenter(%v) = %d, want %d", test.name, test.centerY, got, test.want)
		}
	}
	if got := pageAtCenter(nil, 10); got != -1 {
		t.Errorf("空边界返回 %d, want -1", got)
	}
}

// TestMain 屏蔽 slog 输出。部分用例故意触发渲染失败、日志里的 ERROR 属于
// 预期行为，混在真实回归里会掩盖问题；断言失败时测试自身的消息已经足够定位。
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// newTestLayout 构造带 count 个方形页面的布局，并完成首次排版。
func newTestLayout(t *testing.T, mode pageViewMode, count int) *continuousLayout {
	t.Helper()
	layout := &continuousLayout{mode: mode, gap: 12, margin: 12}
	pages := make([]*pageMeta, count)
	for i := range pages {
		pages[i] = &pageMeta{aspect: 1, renderable: true}
	}
	layout.setPages(pages)
	layout.Layout(layout.frameObjects(), fyne.NewSize(400, 600))
	return layout
}

func TestContinuousLayoutNotifiesOnViewportChangeOnly(t *testing.T) {
	changes := 0
	layout := newTestLayout(t, viewFitWidth, 2)
	layout.onViewportChange = func() { changes++ }
	// setPages 已经排版过一次，先重新触发一次通知基准。
	layout.lastNotifiedViewport = fyne.Size{}

	layout.Layout(layout.frameObjects(), fyne.NewSize(400, 600))
	if changes != 1 {
		t.Fatalf("首次布局的通知次数 = %d, want 1", changes)
	}
	if len(layout.pageBounds) != 2 {
		t.Fatalf("页面边界数量 = %d, want 2", len(layout.pageBounds))
	}
	// 视口未变时不应重复通知，避免重复请求渲染。
	layout.Layout(layout.frameObjects(), fyne.NewSize(400, 600))
	if changes != 1 {
		t.Fatalf("视口未变时的通知次数 = %d, want 1", changes)
	}
	// 拖动窗口改变视口后必须再通知一次，否则新露出的页面不会被渲染。
	// 测试驱动提供真实控件，Resize 才会更新 Scroll 的尺寸。
	test.NewTempApp(t)
	layout.pageScroll = container.NewScroll(container.NewStack(fyneCanvas.NewRectangle(color.White)))
	layout.pageScroll.Resize(fyne.NewSize(400, 800))
	layout.Layout(layout.frameObjects(), fyne.NewSize(400, 800))
	if changes != 2 {
		t.Fatalf("视口变化后的通知次数 = %d, want 2", changes)
	}
}

func TestContinuousLayoutFrameCountIndependentOfPageCount(t *testing.T) {
	small := newTestLayout(t, viewFitWidth, 2)
	large := newTestLayout(t, viewFitWidth, 10000)
	// 虚拟化的核心收益：显示帧数量只取决于初始池大小，与页数无关。
	if len(small.frames) != initialPageFrames || len(large.frames) != initialPageFrames {
		t.Fatalf("帧数量 = %d/%d, want %d", len(small.frames), len(large.frames), initialPageFrames)
	}
	if len(large.pageBounds) != 10000 {
		t.Fatalf("万页文档的排版结果数量 = %d, want 10000", len(large.pageBounds))
	}
	// 万页文档的内容高度必须完整，滚动范围才不会缩水。
	pageHeight := large.pageSizes[0].Height
	want := fyne.NewSize(400, 2*12+float32(10000)*pageHeight+12*9999)
	if got := large.MinSize(nil); got != want {
		t.Fatalf("内容尺寸 = %v, want %v", got, want)
	}
}

func TestContinuousLayoutReusesGeometryUntilViewportChanges(t *testing.T) {
	test.NewTempApp(t)
	layout := &continuousLayout{mode: viewFitWidth, gap: 12, margin: 12}
	pages := make([]*pageMeta, 64)
	for i := range pages {
		pages[i] = &pageMeta{aspect: 1, renderable: true}
	}
	layout.setPages(pages)
	content := container.New(layout)
	content.Objects = layout.frameObjects()
	scroll := container.NewScroll(content)
	layout.pageScroll = scroll

	scroll.Resize(fyne.NewSize(400, 600))
	first := &layout.pageBounds[0]
	before := *first
	// 滚动不改变视口，排版结果应当整段复用。
	layout.Layout(content.Objects, content.Size())
	if &layout.pageBounds[0] != first {
		t.Fatal("视口未变时页面边界缓冲区被重新分配")
	}
	if *first != before {
		t.Fatal("视口未变时页面边界被重算")
	}
	scroll.Resize(fyne.NewSize(500, 600))
	if *first == before {
		t.Fatal("视口变化后页面边界没有重算")
	}
}

func TestContinuousLayoutRebuildsGeometryOnShrink(t *testing.T) {
	// Fyne 的滚动容器按 max(内容尺寸, 视口) 设置内容尺寸，缩小窗口时若几何
	// 只由 Layout 重建，Content.Resize 会因尺寸未变而跳过 Layout，视口和页面
	// 尺寸会停留在旧值。MinSize 必须能触发重建。
	test.NewTempApp(t)
	layout := &continuousLayout{mode: viewFitWidth, gap: 12, margin: 12}
	pages := make([]*pageMeta, 3)
	for i := range pages {
		pages[i] = &pageMeta{aspect: 1, renderable: true}
	}
	layout.setPages(pages)
	content := container.New(layout)
	content.Objects = layout.frameObjects()
	scroll := container.NewScroll(content)
	layout.pageScroll = scroll
	notified := 0
	layout.onViewportChange = func() { notified++ }

	scroll.Resize(fyne.NewSize(400, 600))
	wide := layout.pageBounds[0].size.Width
	if notified != 1 {
		t.Fatalf("首次布局的通知次数 = %d, want 1", notified)
	}
	scroll.Resize(fyne.NewSize(250, 300))
	if got := layout.pageBounds[0].size.Width; got >= wide {
		t.Fatalf("缩小窗口后页面宽度 = %v, want < %v", got, wide)
	}
	if layout.viewport != fyne.NewSize(250, 300) {
		t.Fatalf("缩小窗口后视口 = %v, want {250 300}", layout.viewport)
	}
	if notified != 2 {
		t.Fatalf("缩小窗口的通知次数 = %d, want 2", notified)
	}
}

func TestContinuousLayoutVisibleRange(t *testing.T) {
	test.NewTempApp(t)
	layout := &continuousLayout{mode: viewFitWidth, gap: 12, margin: 12}
	pages := make([]*pageMeta, 20)
	for i := range pages {
		pages[i] = &pageMeta{aspect: 1, renderable: true}
	}
	layout.setPages(pages)
	content := container.New(layout)
	content.Objects = layout.frameObjects()
	scroll := container.NewScroll(content)
	layout.pageScroll = scroll
	scroll.Resize(fyne.NewSize(400, 600))
	layout.Layout(content.Objects, content.Size())

	span := func(offset float32) (int, int) {
		scroll.Offset = fyne.NewPos(0, offset)
		return layout.visibleRange()
	}
	start, end := span(0)
	if start != 0 || end < 1 {
		t.Fatalf("顶部可视区间 = [%d,%d), want [0,>=1)", start, end)
	}
	// 滚到第 3 页顶部，可视区间必须包含第 3 页且不包含第 1 页。
	thirdTop := layout.pageBounds[2].position.Y
	start, end = span(thirdTop)
	if start > 2 || end <= 2 {
		t.Fatalf("第 3 页顶部的可视区间 = [%d,%d), want 包含 2", start, end)
	}
	// 滚到底部，最后一页必须在区间内。
	lastTop := layout.pageBounds[19].position.Y
	start, end = span(lastTop)
	if end != 20 {
		t.Fatalf("底部可视区间 = [%d,%d), want 结束于 20", start, end)
	}
	// 视口内可容纳的页面数应与帧池大小无关：区间长度由几何决定。
	if start < 0 || end-start > len(layout.frames) {
		t.Fatalf("可视区间 [%d,%d) 超出帧池容量 %d", start, end, len(layout.frames))
	}
}

// newWindowTestViewer 构造可绑定显示窗口的阅读器。
func newWindowTestViewer(t *testing.T, pages int) *viewer {
	t.Helper()
	test.NewTempApp(t)
	v := &viewer{}
	v.pageLayout = &continuousLayout{mode: viewFitWidth, gap: 12, margin: 12}
	metas := make([]*pageMeta, pages)
	for i := range metas {
		// 不可渲染：这些用例只关心显示窗口的绑定与扩池。
		metas[i] = &pageMeta{aspect: 1, renderable: false}
	}
	v.pageSlots = metas
	v.pages = make([]viewerPage, pages)
	v.totalPages = pages
	v.pageLayout.setPages(metas)
	v.pageContent = container.New(v.pageLayout)
	v.pageContent.Objects = v.pageLayout.frameObjects()
	v.pageScroll = container.NewScroll(v.pageContent)
	v.pageLayout.pageScroll = v.pageScroll
	v.pageImages = v.newPageImageCache()
	v.pageLayout.Layout(v.pageContent.Objects, fyne.NewSize(400, 600))
	return v
}

func TestBindPageWindowAssignsFramesAndSwapsImages(t *testing.T) {
	v := newWindowTestViewer(t, 10)
	first := image.NewRGBA(image.Rect(0, 0, 1, 1))
	second := image.NewRGBA(image.Rect(0, 0, 2, 2))
	v.pageImages.AddWeighted(3, toRGBA(first), rasterWeight(toRGBA(first)))
	v.pageImages.AddWeighted(4, toRGBA(second), rasterWeight(toRGBA(second)))

	v.bindPageWindow(3, 5)
	bound := map[int]int{}
	for _, frame := range v.pageLayout.frames {
		if frame.page >= 0 {
			if frame.image.Image == nil {
				t.Fatalf("第 %d 页绑定的帧没有图像", frame.page)
			}
			bound[frame.page] = frame.page
		}
	}
	if len(bound) != 2 {
		t.Fatalf("绑定页面数 = %d, want 2", len(bound))
	}
	if v.frameForPage(3) == nil || v.frameForPage(4) == nil {
		t.Fatal("第 3、4 页没有对应的显示帧")
	}
	if v.frameForPage(3).image.Image != toRGBA(first) {
		t.Fatal("第 3 页的帧没有显示自己的位图")
	}
	// 被帧引用的页面不能被淘汰，否则画面留白且内存释放不掉。
	if !v.protectPageImage(3) || !v.protectPageImage(4) {
		t.Fatal("显示窗口内的页面不应被淘汰")
	}
	if v.protectPageImage(5) {
		t.Fatal("显示窗口外的页面不应被保护")
	}

	// 窗口整体前移：旧窗口的帧必须换成新页面，且不残留上一页的位图。
	v.bindPageWindow(5, 7)
	if v.frameForPage(3) != nil {
		t.Fatal("移出窗口的页面仍占用显示帧")
	}
	if frame := v.frameForPage(5); frame == nil || frame.image.Image != nil {
		t.Fatal("新窗口中的页面没有绑定到空白帧")
	}
}

func TestUpdatePageWindowFromViewportCallbackDoesNotRecurse(t *testing.T) {
	v := newWindowTestViewer(t, 200)
	// onViewportChange 由 Layout 末尾触发，而 updatePageWindow 会 Refresh 容器
	// 再进一次 Layout。必须验证嵌套层数有界、且窗口最终被正确绑定。
	depth, maxDepth := 0, 0
	layouts := 0
	v.pageLayout.onViewportChange = func() {
		depth++
		if depth > maxDepth {
			maxDepth = depth
		}
		v.updatePageWindow(1)
		depth--
	}
	for i, frame := range v.pageLayout.frames {
		frame.page = -1
		_ = i
	}
	// 模拟窗口尺寸变化：视口变大，可视页面变多。
	v.pageScroll.Resize(fyne.NewSize(400, 3000))
	v.pageContent.Refresh()

	if maxDepth > 2 {
		t.Fatalf("Layout 嵌套层数 = %d, want <= 2", maxDepth)
	}
	layouts = len(v.pageLayout.frames)
	if layouts == 0 {
		t.Fatal("显示帧全部被回收")
	}
	bound := 0
	for _, frame := range v.pageLayout.frames {
		if frame.page >= 0 {
			bound++
		}
	}
	if bound == 0 {
		t.Fatal("视口回调后没有页面绑定到显示帧")
	}
	if got, _ := v.pageLayout.visibleRange(); got >= bound {
		t.Fatalf("可视区间起点 = %d, want < 绑定页面数 %d", got, bound)
	}
}

func TestBindPageWindowGrowsFramesForFlatPages(t *testing.T) {
	v := newWindowTestViewer(t, 40)
	// 视口内能放下 30 页时必须扩池，否则后 18 页没有帧可显示。
	v.bindPageWindow(0, 30)
	if len(v.pageLayout.frames) < 30 {
		t.Fatalf("帧数量 = %d, want >= 30", len(v.pageLayout.frames))
	}
	if len(v.pageContent.Objects) != len(v.pageLayout.frames) {
		t.Fatalf("容器对象数 = %d, want %d", len(v.pageContent.Objects), len(v.pageLayout.frames))
	}
	for page := 0; page < 30; page++ {
		if v.frameForPage(page) == nil {
			t.Fatalf("第 %d 页没有绑定显示帧", page)
		}
	}
	if v.frameForPage(30) != nil {
		t.Fatal("窗口外的页面不应占用显示帧")
	}
}

func toRGBA(source image.Image) *image.RGBA {
	if raster, ok := source.(*image.RGBA); ok {
		return raster
	}
	bounds := source.Bounds()
	raster := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			raster.Set(x, y, source.At(x, y))
		}
	}
	return raster
}

// newLoadedTestViewer 构造已加载 3 页文档的阅读器：前两页可渲染，第 2 页
// 传 nil 使 pageSlotStates 判定为不可渲染；页面图像缓存为空。
//
// operation 最后才置位：createPageSlots 会触发窗口更新，过早置位会让它在
// 准备阶段就启动渲染，测试驱动的 fyne.Do 是内联执行的，随后的断言会与
// 渲染协程竞争界面状态。
func newLoadedTestViewer(t *testing.T) *viewer {
	t.Helper()
	test.NewTempApp(t)
	window := test.NewWindow(nil)
	t.Cleanup(window.Close)
	v := newViewer(window)
	window.SetContent(v.content)
	v.pages = []viewerPage{{page: &parser.Page{}}, {page: &parser.Page{}}, {page: nil}}
	v.totalPages = len(v.pages)
	v.createPageSlots(v.pages, nil)
	// 默认把页面标记为不可渲染：这些页没有真实文档，发起渲染只会得到失败日志。
	// 需要渲染的用例自行打开对应页面。
	for _, meta := range v.pageSlots {
		meta.renderable = false
	}
	v.operation.Store(1)
	v.thumbnailGeneration.Store(1)
	// 预置缩略图缓存：这些页没有真实文档，渲染必然失败，预置后不会刷屏。
	for page := range v.pages {
		raster := toRGBA(image.NewRGBA(image.Rect(0, 0, 1, 1)))
		v.thumbnails.AddWeighted(page, raster, rasterWeight(raster))
	}
	return v
}

func TestGoToPageDoesNotShowHintWithoutRenderTask(t *testing.T) {
	v := newLoadedTestViewer(t)
	v.pageSlots[1].renderable = true

	// 目标页已在缓存中：不需要渲染，残留的加载提示必须被清掉。
	v.showPageLoading(1)
	if v.pageLoading == nil {
		t.Fatal("前置条件失败：加载提示未显示")
	}
	v.pageImages.AddWeighted(1, toRGBA(image.NewRGBA(image.Rect(0, 0, 1, 1))), 4)
	v.goToPage(1, true)
	if v.pageLoading != nil {
		t.Fatal("目标页已缓存时不应残留加载提示")
	}

	// 页面区域不可读：不会发起渲染，同样不能留下无法关闭的提示。
	v.goToPage(2, true)
	if v.pageLoading != nil {
		t.Fatal("不可渲染的页面不应显示加载提示")
	}
	if v.pageLoadingStop != nil {
		t.Fatal("不可渲染的页面不应留下兜底定时器")
	}
}

func TestRequestPageRenderReportsWhetherTaskStarted(t *testing.T) {
	v := newLoadedTestViewer(t)
	v.pageSlots[1].renderable = true
	// currentPage 保持 0：渲染协程的隐藏条件不成立，不会改动界面状态。
	if started := v.requestPageRender(1, 1); !started {
		t.Fatal("未缓存的可渲染页面应当启动渲染任务")
	}
	// 重复请求被 rendering 标志挡住。
	if started := v.requestPageRender(1, 1); started {
		t.Fatal("已有渲染任务时不应重复启动")
	}
	// 不可渲染与越界都不会启动任务。
	if started := v.requestPageRender(1, 2); started {
		t.Fatal("不可渲染的页面不应启动渲染任务")
	}
	if started := v.requestPageRender(1, 99); started {
		t.Fatal("越界页码不应启动渲染任务")
	}
	if started := v.requestPageRender(0, 1); started {
		t.Fatal("无效 operation 不应启动渲染任务")
	}
}

func TestStartedRenderAlwaysClosesLoadingHint(t *testing.T) {
	v := newLoadedTestViewer(t)

	// 渲染失败路径：直接同步执行渲染任务，提示必须被关闭。
	v.showPageLoading(1)
	v.renderPage(1, nil, 0, &parser.Page{}, v.pageSlots[0])
	if v.pageLoading != nil {
		t.Fatal("渲染失败后加载提示未关闭")
	}
	if v.pageLoadingStop != nil {
		t.Fatal("渲染失败后兜底定时器未取消")
	}

	// 渲染成功路径：同样必须关闭提示。
	input := filepath.Join("..", "..", "testdata", "helloworld.ofd")
	ofd, err := parser.NewOFD(input)
	if err != nil {
		t.Skipf("测试文档不可用: %v", err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	doc := render.NewDocument(color.Transparent, ofd.Documents[0])
	page := ofd.Documents[0].Pages[0]
	v.showPageLoading(1)
	v.renderPage(1, doc, 0, page, v.pageSlots[0])
	if v.pageLoading != nil {
		t.Fatal("渲染成功后加载提示未关闭")
	}
	if v.pageLoadingStop != nil {
		t.Fatal("渲染成功后兜底定时器未取消")
	}
}

func TestPageLoadingHintTimeoutIsCancelledOnHide(t *testing.T) {
	v := newLoadedTestViewer(t)
	original := pageLoadingTimeout
	pageLoadingTimeout = 30 * time.Millisecond
	t.Cleanup(func() { pageLoadingTimeout = original })

	v.showPageLoading(1)
	if v.pageLoadingStop == nil {
		t.Fatal("显示加载提示时应当启动兜底定时器")
	}
	sequence := v.pageLoadingSeq.Load()
	v.hidePageLoading(0)
	if v.pageLoadingStop != nil {
		t.Fatal("关闭加载提示后定时器应当被取消")
	}
	// 等待超过超时时间：过期回调不得再改动提示状态。
	time.Sleep(4 * pageLoadingTimeout)
	if v.pageLoading != nil {
		t.Fatal("已关闭的加载提示被过期定时器重新显示")
	}
	if v.pageLoadingSeq.Load() == sequence {
		t.Fatal("关闭提示后序号应变化，使过期回调失效")
	}
}

func TestPageLoadingHintTimeoutHidesStuckHint(t *testing.T) {
	v := newLoadedTestViewer(t)
	original := pageLoadingTimeout
	pageLoadingTimeout = 10 * time.Millisecond
	t.Cleanup(func() { pageLoadingTimeout = original })

	// 模拟渲染任务丢失：提示只显示，没有任何回调会关闭它。
	v.showPageLoading(1)
	sequence := v.pageLoadingSeq.Load()
	// 超时回调在后台线程执行，只观察原子序号，避免与界面状态竞争。
	// hidePageLoading 只在真正隐藏弹窗时才会推进序号。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v.pageLoadingSeq.Load() != sequence {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("加载提示超时后没有被自动关闭")
}

func TestExpandPageWindow(t *testing.T) {
	tests := []struct {
		name                      string
		start, end, total, margin int
		wantStart, wantEnd        int
	}{
		{"两侧各扩一页", 5, 6, 20, 1, 4, 7},
		{"已在首页不再前扩", 0, 1, 20, 1, 0, 2},
		{"已在末页不再后扩", 18, 20, 20, 1, 17, 20},
		{"首尾同时到达", 0, 1, 2, 1, 0, 2},
		{"空文档", 0, 0, 0, 1, 0, 0},
		{"区间非法时收敛后再预取", 8, 3, 20, 1, 7, 9},
		{"负 margin 视作不预取", 5, 6, 20, -2, 5, 6},
		{"零 margin", 5, 6, 20, 0, 5, 6},
	}
	for _, test := range tests {
		start, end := expandPageWindow(test.start, test.end, test.total, test.margin)
		if start != test.wantStart || end != test.wantEnd {
			t.Errorf("%s: expandPageWindow(%d,%d,%d,%d) = [%d,%d), want [%d,%d)",
				test.name, test.start, test.end, test.total, test.margin,
				start, end, test.wantStart, test.wantEnd)
		}
	}
}

func TestPageSizeCoversEveryViewMode(t *testing.T) {
	layout := &continuousLayout{gap: 12, margin: 12}
	viewport := fyne.NewSize(400, 600)
	const a4 = 210.0 / 297.0

	// 适应宽度：宽度铺满视口，高度按宽高比推导。
	layout.mode = viewFitWidth
	if got := layout.pageSize(a4, viewport); got.Width != 376 || got.Height != 376/a4 {
		t.Errorf("适应宽度 = %v, want 宽 376", got)
	}
	// 适应高度：高度铺满视口。
	layout.mode = viewFitHeight
	if got := layout.pageSize(a4, viewport); got.Height != 576 || got.Width != 576*a4 {
		t.Errorf("适应高度 = %v, want 高 576", got)
	}
	// 适应页面：整页可见，宽高都不超过可用区域。竖版页面在横向视口里由宽度决定。
	layout.mode = viewFitPage
	if got := layout.pageSize(a4, viewport); got.Width != 376 || got.Height > 576 {
		t.Errorf("适应页面 = %v, want 宽 376 且高 <= 576", got)
	}
	// 窄视口下由高度决定：宽高都填满可用区域。
	narrow := fyne.NewSize(600, 200)
	if got := layout.pageSize(a4, narrow); got.Height != 176 || got.Width != 176*a4 {
		t.Errorf("适应页面（窄视口） = %v, want 高 176", got)
	}
	// 双页：两页加间距平分可用宽度。
	layout.mode = viewDoublePage
	if got := layout.pageSize(a4, viewport); got.Width != fyne.Max((376-12)/2, 1) {
		t.Errorf("双页宽度 = %v, want %v", got.Width, fyne.Max((376-12)/2, 1))
	}
	// 非法宽高比回退为 1，且任何模式下尺寸都不为负。
	for _, mode := range []pageViewMode{viewFitPage, viewFitWidth, viewFitHeight, viewDoublePage} {
		layout.mode = mode
		got := layout.pageSize(0, viewport)
		if got.Width <= 0 || got.Height <= 0 {
			t.Errorf("模式 %d 的非法宽高比得到 %v", mode, got)
		}
	}
}

func TestRebuildGeometryDoublePage(t *testing.T) {
	layout := &continuousLayout{mode: viewDoublePage, gap: 12, margin: 12}
	// 第 1 行两页高度不同，用来验证行高取较大者且矮页垂直居中；
	// 末尾多出一页，验证奇数页独占一行。
	aspects := []float32{1, 0.5, 1, 1, 1}
	pages := make([]*pageMeta, len(aspects))
	for i, aspect := range aspects {
		pages[i] = &pageMeta{aspect: aspect, renderable: true}
	}
	layout.setPages(pages)
	viewport := fyne.NewSize(400, 600)
	layout.Layout(layout.frameObjects(), viewport)

	if len(layout.pageBounds) != len(aspects) {
		t.Fatalf("页面边界数量 = %d, want %d", len(layout.pageBounds), len(aspects))
	}
	// 同一行的两页水平相邻。高度不同时会在行内垂直居中，Y 可以不同。
	for row := 0; row*2+1 < len(aspects); row++ {
		left := layout.pageBounds[row*2]
		right := layout.pageBounds[row*2+1]
		if right.position.X <= left.position.X {
			t.Errorf("第 %d 行的右页没有排在左页右侧: %v vs %v", row, left.position.X, right.position.X)
		}
		if gap := right.position.X - (left.position.X + left.size.Width); float32(math.Abs(float64(gap-layout.gap))) > 0.001 {
			t.Errorf("第 %d 行页间距 = %v, want %v", row, gap, layout.gap)
		}
	}
	// 第 0 行由两页高度决定行高，矮页在行内垂直居中。
	shorter, taller := layout.pageBounds[0], layout.pageBounds[1]
	if shorter.size.Height >= taller.size.Height {
		t.Fatalf("测试前提不成立: 前两页应高度不同, 得到 %v 与 %v", shorter.size, taller.size)
	}
	rowTop := taller.position.Y
	rowHeight := taller.size.Height
	if want := (rowHeight - shorter.size.Height) / 2; float32(math.Abs(float64(shorter.position.Y-(rowTop+want)))) > 0.001 {
		t.Errorf("矮页 Y = %v, want 行内居中 %v", shorter.position.Y, rowTop+want)
	}
	// 下一行必须排在本行行高加间距之后。
	row1 := layout.pageBounds[2].position.Y
	if want := rowTop + rowHeight + layout.gap; float32(math.Abs(float64(row1-want))) > 0.001 {
		t.Errorf("第二行 Y = %v, want %v", row1, want)
	}
	// 末页独占一行，行高等于自身高度。
	last := layout.pageBounds[4]
	row2 := last.position.Y
	if want := row1 + layout.pageBounds[3].size.Height + layout.gap; float32(math.Abs(float64(row2-want))) > 0.001 {
		t.Errorf("末行 Y = %v, want %v", row2, want)
	}
	// 独占一行的页面按整行宽度居中，而不是与双页行左对齐。
	if want := (layout.minSize.Width - last.size.Width) / 2; float32(math.Abs(float64(last.position.X-want))) > 0.001 {
		t.Errorf("末页 X = %v, want 居中 %v", last.position.X, want)
	}
}

func TestRebuildGeometrySinglePage(t *testing.T) {
	layout := &continuousLayout{mode: viewFitWidth, gap: 12, margin: 12}
	pages := make([]*pageMeta, 5)
	for i := range pages {
		pages[i] = &pageMeta{aspect: 1, renderable: true}
	}
	layout.setPages(pages)
	layout.Layout(layout.frameObjects(), fyne.NewSize(400, 600))

	// 单页模式所有页宽度相同、居中，纵向依次排列并保持固定间距。
	first := layout.pageBounds[0]
	for i, bound := range layout.pageBounds {
		if bound.size != first.size {
			t.Fatalf("第 %d 页尺寸 = %v, want %v", i, bound.size, first.size)
		}
		if want := (layout.minSize.Width - bound.size.Width) / 2; float32(math.Abs(float64(bound.position.X-want))) > 0.001 {
			t.Errorf("第 %d 页 X = %v, want 居中 %v", i, bound.position.X, want)
		}
		if i > 0 {
			previous := layout.pageBounds[i-1]
			want := previous.position.Y + previous.size.Height + layout.gap
			if float32(math.Abs(float64(bound.position.Y-want))) > 0.001 {
				t.Errorf("第 %d 页 Y = %v, want %v", i, bound.position.Y, want)
			}
		}
	}
	if want := fyne.NewSize(first.size.Width+2*layout.margin, 2*layout.margin+5*first.size.Height+4*layout.gap); layout.minSize != want {
		t.Errorf("内容尺寸 = %v, want %v", layout.minSize, want)
	}
}

func TestPageTopSearch(t *testing.T) {
	bounds := make([]pageBound, 5)
	for i := range bounds {
		bounds[i] = pageBound{
			position: fyne.NewPos(0, float32(i*200)),
			size:     fyne.NewSize(100, 100),
		}
	}
	// 顶部位置为 0、200、400、600、800。
	if got := pageTopAtOrBefore(bounds, 450); got != 2 {
		t.Errorf("pageTopAtOrBefore(450) = %d, want 2", got)
	}
	if got := pageTopAtOrBefore(bounds, 200); got != 1 {
		t.Errorf("pageTopAtOrBefore(200) = %d, want 1", got)
	}
	if got := pageTopAtOrBefore(bounds, -10); got != -1 {
		t.Errorf("pageTopAtOrBefore(-10) = %d, want -1", got)
	}
	if got := pageTopAtOrAfter(bounds, 450); got != 3 {
		t.Errorf("pageTopAtOrAfter(450) = %d, want 3", got)
	}
	if got := pageTopAtOrAfter(bounds, 400); got != 2 {
		t.Errorf("pageTopAtOrAfter(400) = %d, want 2", got)
	}
	if got := pageTopAtOrAfter(bounds, 5000); got != 5 {
		t.Errorf("pageTopAtOrAfter(5000) = %d, want 5（越界）", got)
	}
}

func TestScrollByViewportMovesOneScreen(t *testing.T) {
	v := newThumbnailTestViewer(t, "999.ofd")
	prefillRenderCaches(v)
	if v.totalPages < 3 {
		t.Skipf("测试文档页数不足: %d", v.totalPages)
	}
	if len(v.pageLayout.pageBounds) != v.totalPages {
		t.Fatalf("排版结果数 = %d, want %d", len(v.pageLayout.pageBounds), v.totalPages)
	}
	pageHeight := v.pageLayout.pageBounds[0].size.Height
	if pageHeight <= 0 {
		t.Fatal("页面高度无效，无法验证翻页")
	}
	viewport := v.pageScroll.Size().Height
	if viewport <= pageHeight {
		t.Skipf("视口 %v 装得下整页 %v，无法验证翻屏", viewport, pageHeight)
	}

	// 向下翻一屏应落到目标位置之后、且至少前进一页。
	before := v.currentPage
	v.scrollByViewport(1)
	if v.currentPage <= before {
		t.Fatalf("向下翻屏后当前页 = %d, want > %d", v.currentPage, before)
	}
	// 目标位置应落在可视区间内。
	visibleStart, visibleEnd := v.pageLayout.visibleRange()
	if v.currentPage < visibleStart || v.currentPage >= visibleEnd {
		t.Errorf("翻屏后当前页 %d 不在可视区间 [%d,%d) 内", v.currentPage, visibleStart, visibleEnd)
	}

	// 再向上翻一屏应回到更早的页。
	down := v.currentPage
	v.scrollByViewport(-1)
	if v.currentPage >= down {
		t.Fatalf("向上翻屏后当前页 = %d, want < %d", v.currentPage, down)
	}

	// 顶部和底部不得越界。
	v.goToPage(0, false)
	v.scrollByViewport(-1)
	if v.currentPage != 0 {
		t.Fatalf("首页继续上翻后当前页 = %d, want 0", v.currentPage)
	}
	v.goToPage(v.totalPages-1, false)
	v.scrollByViewport(1)
	if v.currentPage != v.totalPages-1 {
		t.Fatalf("末页继续下翻后当前页 = %d, want %d", v.currentPage, v.totalPages-1)
	}
}

func TestSyncThumbnailSelectionFollowsCurrentPage(t *testing.T) {
	v := newLoadedTestViewer(t)
	// 面板隐藏时不应改动高亮。
	v.setThumbnailVisible(true)
	v.goToPage(2, false)
	if v.thumbnailSelected != 2 {
		t.Fatalf("高亮行 = %d, want 2", v.thumbnailSelected)
	}
	// 程序触发的选中不能回灌成跳转。
	if v.syncingThumbnail.Load() {
		t.Fatal("选中同步结束后标记应复位")
	}
	v.setThumbnailVisible(false)
	v.goToPage(1, false)
	if v.thumbnailSelected != 2 {
		t.Fatalf("面板隐藏时不应改动高亮，仍为 %d", v.thumbnailSelected)
	}
	// 双页模式下两页共用一行。
	v.setThumbnailVisible(true)
	v.pageLayout.mode = viewDoublePage
	v.goToPage(2, false)
	if v.thumbnailSelected != 1 {
		t.Fatalf("双页模式下第 2 页的高亮行 = %d, want 1", v.thumbnailSelected)
	}
}

// openTestDocuments 解析测试文档并构造导出用的渲染文档。
func openTestDocuments(t *testing.T, name string, background color.Color) []*render.Document {
	t.Helper()
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Skipf("测试文档不可用: %v", err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	documents := make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		documents = append(documents, render.NewDocument(background, document))
	}
	return documents
}

func TestExportDocumentReusesMatchingBackground(t *testing.T) {
	documents := openTestDocuments(t, "helloworld.ofd", color.Transparent)
	doc := documents[0]
	// 背景一致时必须原样复用，否则导出会多复制一套字体和图片缓存。
	if got := exportDocument(doc, color.Transparent); got != doc {
		t.Fatal("背景色一致时应复用阅读区的渲染文档")
	}
	// 背景不同才新建。
	other := exportDocument(doc, color.White)
	if other == doc {
		t.Fatal("背景色不同时不应复用")
	}
	if !sameBackground(other.Background(), color.White) {
		t.Fatalf("新文档背景 = %v, want 白色", other.Background())
	}
	if exportDocument(nil, color.White) != nil {
		t.Fatal("空文档不应产生导出文档")
	}
}

func TestExportDocumentsToWriterProducesValidOutput(t *testing.T) {
	// 整文档格式：PDF/TXT 无论多少页都是单个文件。
	multi := openTestDocuments(t, "helloworld.ofd", color.Transparent)
	for _, test := range []struct{ format, magic string }{
		{"txt", ""},
		{"pdf", "%PDF"},
	} {
		var buffer bytes.Buffer
		if err := exportDocumentsToWriter(multi, &buffer, test.format, 72, color.Transparent); err != nil {
			t.Errorf("%s 导出失败: %v", test.format, err)
			continue
		}
		if buffer.Len() == 0 {
			t.Errorf("%s 导出结果为空", test.format)
		}
		if test.magic != "" && !strings.HasPrefix(buffer.String(), test.magic) {
			t.Errorf("%s 导出结果缺少魔数 %q", test.format, test.magic)
		}
	}

	// 单页图片：直接写文件，不打包。这条路径用 noCloseWriter 包住外部输出，
	// 如果写入器被转换器关闭，输出会被截断。
	single := openTestDocuments(t, "hello.ofd", color.Transparent)
	for _, test := range []struct{ format, magic string }{
		{"png", "\x89PNG"},
		{"jpg", "\xff\xd8\xff"},
	} {
		var buffer bytes.Buffer
		if err := exportDocumentsToWriter(single, &buffer, test.format, 72, color.Transparent); err != nil {
			t.Errorf("单页 %s 导出失败: %v", test.format, err)
			continue
		}
		if !strings.HasPrefix(buffer.String(), test.magic) {
			t.Errorf("单页 %s 导出结果缺少魔数 %q，实际开头 %q",
				test.format, test.magic, buffer.String()[:min(8, buffer.Len())])
		}
	}

	// 多页图片：打包成 ZIP。
	var packed bytes.Buffer
	if err := exportDocumentsToWriter(multi, &packed, "png", 72, color.Transparent); err != nil {
		t.Fatalf("多页 png 导出失败: %v", err)
	}
	if !strings.HasPrefix(packed.String(), "PK\x03\x04") {
		t.Error("多页 png 导出结果不是 ZIP")
	}
}

// countingWriteCloser 模拟外部输出（保存对话框给出的文件句柄或 fyne 写入器），
// 记录关闭次数。
type countingWriteCloser struct {
	*bytes.Buffer
	closes atomic.Int64
}

func (c *countingWriteCloser) Close() error {
	c.closes.Add(1)
	return nil
}

func TestExportDocumentsToWriterDoesNotCloseCallerOutput(t *testing.T) {
	// 外部输出的所有权属于调用方：exportToOutput 负责关闭，导出过程只能写入。
	// 这条契约正是 noCloseWriter 存在的理由，回归时必须被挡住。
	single := openTestDocuments(t, "hello.ofd", color.Transparent)
	for _, format := range []string{"png", "pdf", "txt"} {
		output := &countingWriteCloser{Buffer: &bytes.Buffer{}}
		if err := exportDocumentsToWriter(single, output, format, 72, color.Transparent); err != nil {
			t.Errorf("%s 导出失败: %v", format, err)
			continue
		}
		if got := output.closes.Load(); got != 0 {
			t.Errorf("%s 导出过程中关闭了外部输出 %d 次", format, got)
		}
	}
	// 多页图片走 ZIP 分支，同样不能关闭外部输出。
	multi := openTestDocuments(t, "999.ofd", color.Transparent)
	output := &countingWriteCloser{Buffer: &bytes.Buffer{}}
	if err := exportDocumentsToWriter(multi, output, "png", 72, color.Transparent); err != nil {
		t.Fatalf("多页导出失败: %v", err)
	}
	if got := output.closes.Load(); got != 0 {
		t.Errorf("多页导出过程中关闭了外部输出 %d 次", got)
	}
}

func TestExportDocumentsToWriterZipsMultiPageImages(t *testing.T) {
	// 5 页文档导出图片必须打包成 ZIP，且每页一个条目。
	documents := openTestDocuments(t, "999.ofd", color.Transparent)
	var buffer bytes.Buffer
	if err := exportDocumentsToWriter(documents, &buffer, "png", 72, color.Transparent); err != nil {
		t.Fatalf("多页图片导出失败: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatalf("多页图片导出结果不是合法 ZIP: %v", err)
	}
	pages := 0
	for _, file := range reader.File {
		if !strings.HasPrefix(file.Name, "page-") || !strings.HasSuffix(file.Name, ".png") {
			t.Errorf("ZIP 条目命名异常: %s", file.Name)
		}
		pages++
	}
	if pages != 5 {
		t.Errorf("ZIP 条目数 = %d, want 5", pages)
	}
	// 条目内容必须是完整 PNG，不能因为复用写入器被截断。
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			t.Fatalf("打开 ZIP 条目 %s 失败: %v", file.Name, err)
		}
		head := make([]byte, 4)
		if _, err := io.ReadFull(handle, head); err != nil {
			t.Fatalf("读取 ZIP 条目 %s 失败: %v", file.Name, err)
		}
		_ = handle.Close()
		if !bytes.Equal(head, []byte("\x89PNG")) {
			t.Errorf("ZIP 条目 %s 不是完整 PNG，开头 %q", file.Name, head)
		}
	}
}

func TestExportDocumentsToWriterRejectsBadInput(t *testing.T) {
	if err := exportDocumentsToWriter(nil, &bytes.Buffer{}, "pdf", 72, nil); err == nil {
		t.Fatal("空文档列表应报错")
	}
	if err := exportDocumentsToWriter([]*render.Document{render.NewDocument(color.Transparent, &parser.Document{})},
		nil, "pdf", 72, color.White); err == nil {
		t.Fatal("空输出应报错")
	}
	documents := openTestDocuments(t, "helloworld.ofd", color.Transparent)
	if err := exportDocumentsToWriter(documents, &bytes.Buffer{}, "pdf", 72, color.White); err != nil {
		t.Fatalf("白色背景导出失败: %v", err)
	}
}

// prefillRenderCaches 给所有页面填入占位位图，阻止按需渲染真正启动协程。
// 这些用例只关心导航与布局；真实渲染会在后台协程里通过 fyne.Do 读取界面
// 状态，与测试线程的写入竞争（真实驱动把 fyne.Do 排到主线程，测试驱动内联
// 执行，所以只有需要真实渲染的用例才能让协程跑起来）。
func prefillRenderCaches(v *viewer) {
	for page := range v.pages {
		raster := toRGBA(image.NewRGBA(image.Rect(0, 0, 1, 1)))
		v.pageImages.AddWeighted(page, raster, rasterWeight(raster))
		v.thumbnails.AddWeighted(page, raster, rasterWeight(raster))
	}
}

// newThumbnailTestViewer 构造带真实页面的阅读器，缩略图渲染可以真正成功。
func newThumbnailTestViewer(t *testing.T, name string) *viewer {
	t.Helper()
	test.NewTempApp(t)
	window := test.NewWindow(nil)
	t.Cleanup(window.Close)
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Skipf("测试文档不可用: %v", err)
	}
	window.Resize(fyne.NewSize(400, 600))
	v := newViewer(window)
	window.SetContent(v.content)
	v.session = newDocumentSession(ofd)
	v.ofd = ofd
	v.documents = make([]*render.Document, 0, len(ofd.Documents))
	for _, document := range ofd.Documents {
		v.documents = append(v.documents, render.NewDocument(color.Transparent, document))
	}
	v.pages = collectViewerPages(v.documents)
	v.totalPages = len(v.pages)
	v.createPageSlots(v.pages, nil)
	v.thumbnailRendering = make([]atomic.Bool, v.totalPages)
	v.operation.Store(1)
	v.thumbnailGeneration.Store(1)
	// 文档的所有者是会话：直接再 Close 一次会与会话的后台释放并发。
	// 这里只退休会话而不调 closeDocument —— 后者要改写槽位和列表状态，
	// 而触发真实渲染的用例可能还有渲染协程在跑。
	t.Cleanup(func() { v.session.retire() })
	return v
}

func TestRequestThumbnailRenderCachesRealRender(t *testing.T) {
	v := newThumbnailTestViewer(t, "helloworld.ofd")
	if v.totalPages < 2 {
		t.Skipf("测试文档页数不足: %d", v.totalPages)
	}
	generation := v.thumbnailGeneration.Load()
	v.requestThumbnailRender(0)

	// 渲染在后台协程执行，轮询原子缓存直到任务完成。
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := v.thumbnails.Get(0); ok {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	raster, ok := v.thumbnails.Get(0)
	if !ok {
		t.Fatal("真实页面的缩略图没有进入缓存")
	}
	if rasterWeight(raster) <= 1 {
		t.Fatalf("缩略图权重 = %d，看起来是空图像", rasterWeight(raster))
	}
	if v.thumbnailRendering[0].Load() {
		t.Fatal("渲染结束后标志应复位")
	}
	// 重复请求应命中缓存，不再启动渲染。
	before := v.thumbnails.Weight()
	v.requestThumbnailRender(0)
	if got := v.thumbnails.Weight(); got != before {
		t.Fatal("重复请求不应改变缩略图缓存")
	}
	// 越界与非法 generation 都不应启动任务。
	v.requestThumbnailRender(-1)
	v.requestThumbnailRender(v.totalPages + 10)
	if generation == 0 {
		t.Fatal("前置条件失败：generation 未置位")
	}
}

func TestUpdateThumbnailCellUsesCacheAndRequestsOnMiss(t *testing.T) {
	v := newThumbnailTestViewer(t, "helloworld.ofd")
	cell := newThumbnailCell(color.White)

	// 缓存命中：直接用缓存图像，标签显示页码。
	cached := toRGBA(image.NewRGBA(image.Rect(0, 0, 2, 2)))
	v.thumbnails.AddWeighted(0, cached, rasterWeight(cached))
	updateThumbnailCell(cell, 0, v)
	if cell.image.Image != cached {
		t.Fatal("缓存命中时单元应显示缓存图像")
	}
	if cell.label.Text != "第 1 页" {
		t.Errorf("单元标签 = %q, want 第 1 页", cell.label.Text)
	}

	// 缓存未命中：图像置空并请求渲染。
	updateThumbnailCell(cell, 1, v)
	if cell.image.Image != nil {
		t.Fatal("缓存未命中时单元图像应置空")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := v.thumbnails.Get(1); ok {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, ok := v.thumbnails.Get(1); !ok {
		t.Fatal("缓存未命中时应当发起缩略图渲染")
	}
}

func TestThumbnailRowMapping(t *testing.T) {
	v := &viewer{}
	if got := v.thumbnailRow(5); got != 5 {
		t.Errorf("单页模式行号 = %d, want 5", got)
	}
	if got := v.thumbnailRowCount(); got != 0 {
		t.Errorf("无文档时行数 = %d, want 0", got)
	}
	v.pageLayout = &continuousLayout{mode: viewDoublePage}
	v.totalPages = 5
	if got := v.thumbnailRow(3); got != 1 {
		t.Errorf("双页模式第 3 页行号 = %d, want 1", got)
	}
	if got := v.thumbnailRowCount(); got != 3 {
		t.Errorf("双页模式 5 页的行数 = %d, want 3", got)
	}
	v.totalPages = 4
	if got := v.thumbnailRowCount(); got != 2 {
		t.Errorf("双页模式 4 页的行数 = %d, want 2", got)
	}
}

// newWindowOnlyViewer 构造只有窗口和控件、没有文档的阅读器。
func newWindowOnlyViewer(t *testing.T) *viewer {
	t.Helper()
	test.NewTempApp(t)
	window := test.NewWindow(nil)
	t.Cleanup(window.Close)
	v := newViewer(window)
	window.SetContent(v.content)
	return v
}

func TestValidOFDFile(t *testing.T) {
	if got := validOFDFile(""); got != "" {
		t.Errorf("空路径 = %q, want 空", got)
	}
	if got := validOFDFile("document.pdf"); got != "" {
		t.Errorf("非 OFD 扩展名 = %q, want 空", got)
	}
	if got := validOFDFile(filepath.Join(t.TempDir(), "missing.ofd")); got != "" {
		t.Errorf("不存在的文件 = %q, want 空", got)
	}
	// 扩展名大小写不敏感。
	path := filepath.Join(t.TempDir(), "doc.OFD")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}
	if got := validOFDFile(path); got != path {
		t.Errorf("大写扩展名 = %q, want %q", got, path)
	}
}

func TestExportFormatCodeAndBackground(t *testing.T) {
	for label, want := range map[string]string{
		exportFormatPDF: "pdf",
		exportFormatTXT: "txt",
		exportFormatJPG: "jpg",
		exportFormatPNG: "png",
		exportFormatSVG: "svg",
		exportFormatEPS: "eps",
		exportFormatTeX: "tex",
		"未知格式":          "pdf",
	} {
		if got := exportFormatCode(label); got != want {
			t.Errorf("exportFormatCode(%q) = %q, want %q", label, got, want)
		}
	}
	if !sameBackground(exportBackgroundColor(exportBackgroundWhite), color.White) {
		t.Error("白色背景选项应返回白色")
	}
	if !sameBackground(exportBackgroundColor(exportBackgroundTransparent), color.Transparent) {
		t.Error("透明背景选项应返回透明")
	}
}

func TestPublishDocumentReplacesStateAndSession(t *testing.T) {
	v := newWindowOnlyViewer(t)
	v.operation.Store(1)
	path := filepath.Join("..", "..", "testdata", "helloworld.ofd")
	ofd, err := parser.NewOFD(path)
	if err != nil {
		t.Skipf("测试文档不可用: %v", err)
	}
	t.Cleanup(func() { _ = ofd.Close() })

	model, err := buildDocumentModel(ofd, "helloworld.ofd", color.Transparent)
	if err != nil {
		t.Fatalf("构造文档模型失败: %v", err)
	}
	if err := v.publishDocument(model, path, "helloworld.ofd"); err != nil {
		t.Fatalf("发布文档失败: %v", err)
	}
	if v.ofd != ofd || v.session == nil {
		t.Fatal("发布后应持有 ofd 和会话")
	}
	if v.session.retired.Load() {
		t.Fatal("新发布的会话不应处于退休状态")
	}
	if v.totalPages != len(v.pages) || v.totalPages == 0 {
		t.Fatalf("页数 = %d, 页面列表 = %d", v.totalPages, len(v.pages))
	}
	if len(v.pageSlots) != v.totalPages || len(v.pageLayout.pageBounds) != v.totalPages {
		t.Fatalf("槽位 %d / 排版 %d, want %d", len(v.pageSlots), len(v.pageLayout.pageBounds), v.totalPages)
	}
	if len(v.thumbnailRendering) != v.totalPages {
		t.Fatalf("缩略图任务标志数 = %d, want %d", len(v.thumbnailRendering), v.totalPages)
	}
	// 虚拟化：页面容器只持有帧池，不是一页一个对象。
	if len(v.pageContent.Objects) != len(v.pageLayout.frames) {
		t.Fatalf("容器对象数 = %d, want %d", len(v.pageContent.Objects), len(v.pageLayout.frames))
	}
	if v.currentPage != 0 || v.pageEntry.Text != "1" || v.fileName != "helloworld.ofd" {
		t.Fatalf("发布后页码/文件名 = %d/%q/%q", v.currentPage, v.pageEntry.Text, v.fileName)
	}
	// 至少要绑定可见页面的显示帧。
	if v.frameForPage(0) == nil {
		t.Fatal("发布后第 1 页没有显示帧")
	}
	// 注意：这里不再关闭文档。发布真实文档会启动后台渲染，测试驱动的 fyne.Do
	// 是内联执行的，渲染协程会直接读显示帧；此时再改写状态就是竞争。真实驱动
	// 会把 fyne.Do 排到主线程，生产环境没有问题。关闭行为由
	// TestCloseDocumentRetiresSessionAndReleasesFrames 覆盖。
}

// newRetirableTestViewer 构造不会触发后台渲染的阅读器，用于验证关闭文档。
// 真实驱动把 fyne.Do 排到 Fyne 事件线程，界面状态全部在该线程读写；测试驱动
// 改为内联执行，因此这些用例必须避免启动渲染协程。
func newRetirableTestViewer(t *testing.T) *viewer {
	t.Helper()
	v := newLoadedTestViewer(t)
	v.session = &documentSession{}
	return v
}

func TestCloseDocumentRetiresSessionAndReleasesFrames(t *testing.T) {
	v := newRetirableTestViewer(t)
	session := v.session
	if len(v.pageSlots) == 0 || len(v.pageContent.Objects) == 0 {
		t.Fatal("前置条件失败：阅读器没有页面状态")
	}
	if session.retired.Load() {
		t.Fatal("前置条件失败：会话不应已退休")
	}

	v.closeDocument()
	if !session.retired.Load() {
		t.Fatal("关闭文档后会话应退休")
	}
	if v.ofd != nil || v.session != nil || v.totalPages != 0 {
		t.Fatal("关闭文档后状态未清空")
	}
	if len(v.pageSlots) != 0 || len(v.pageLayout.frames) != 0 {
		t.Fatal("关闭文档后页面槽位和显示帧应释放")
	}
	if len(v.pageContent.Objects) != 0 {
		t.Fatalf("关闭文档后仍持有 %d 个对象", len(v.pageContent.Objects))
	}
	if len(v.pageLayout.pageBounds) != 0 || len(v.pageLayout.pages) != 0 {
		t.Fatal("关闭文档后排版状态应清空")
	}
	if v.thumbnailSelected != -1 {
		t.Errorf("关闭文档后缩略图高亮行 = %d, want -1", v.thumbnailSelected)
	}
	// 重复关闭必须安全。
	v.closeDocument()
}

func TestRunLoadClearsLoadingOnSuccess(t *testing.T) {
	v := newWindowOnlyViewer(t)
	v.loading = true
	v.updateControls()
	path := filepath.Join("..", "..", "testdata", "helloworld.ofd")
	ofd, err := parser.NewOFD(path)
	if err != nil {
		t.Skipf("测试文档不可用: %v", err)
	}
	t.Cleanup(func() { _ = ofd.Close() })

	// runLoad 内部会重新打开文件，所以这里直接给路径而不是已解析的 OFD。
	v.runLoad(v.operation.Load(), path, "helloworld.ofd", path)
	// 加载状态必须复位：漏掉会让翻页、导出、关闭文档全部永久失效。
	if v.loading {
		t.Fatal("加载成功后 loading 必须复位，否则界面永久禁用")
	}
	if v.totalPages == 0 {
		t.Fatal("加载成功后应发布页面")
	}
}

func TestRunLoadClearsLoadingOnFailure(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"路径不存在", filepath.Join(t.TempDir(), "missing.ofd")},
		{"数据不是 ZIP", []byte("not a zip at all")},
		{"类型不支持", 42},
	}
	for _, test := range tests {
		v := newWindowOnlyViewer(t)
		v.loading = true
		v.updateControls()
		v.runLoad(v.operation.Load(), "x.ofd", "x.ofd", test.input)
		if v.loading {
			t.Errorf("%s：加载失败后 loading 必须复位", test.name)
		}
		if v.totalPages != 0 || v.ofd != nil {
			t.Errorf("%s：加载失败不应发布文档", test.name)
		}
	}
}

func TestRunLoadReportsRealParseError(t *testing.T) {
	// parser.NewOFD 失败时也返回非 nil 的 OFD，因此不能靠 ofd != nil 判断
	// 成功，否则真实原因会被替换成“没有文档”。
	ofd, err := openOFD([]byte("not a zip at all"))
	if ofd == nil {
		t.Fatal("前置条件失败：失败的 NewOFD 应当返回非 nil 的 OFD")
	}
	t.Cleanup(func() { _ = ofd.Close() })
	if err == nil {
		t.Fatal("非 ZIP 数据应返回错误")
	}
	// 把这个"有对象但带错误"的组合交给 publishDocument，它必须以"没有文档"
	// 失败而不是假定成功；调用方 load 负责先检查 err。
	if err := v_publishDocumentError(ofd); err == nil {
		t.Fatal("空文档体应发布失败")
	}
}

// v_publishDocumentError 是 publishDocument 的极简包装，便于断言空文档体。
func v_publishDocumentError(ofd *parser.OFD) error {
	v := &viewer{}
	model, err := buildDocumentModel(ofd, "x.ofd", color.Transparent)
	if err != nil {
		return err
	}
	return v.publishDocument(model, "x.ofd", "x.ofd")
}

func TestPublishDocumentRejectsEmptyInput(t *testing.T) {
	v := newWindowOnlyViewer(t)
	v.operation.Store(1)
	if err := v.publishDocument(nil, "x.ofd", "x.ofd"); err == nil {
		t.Error("空模型应报错")
	}
	// 有 OFD 但没有页面时必须报错，并且不动原状态。
	model, modelErr := buildDocumentModel(&parser.OFD{}, "x.ofd", color.Transparent)
	if modelErr == nil {
		if err := v.publishDocument(model, "x.ofd", "x.ofd"); err == nil {
			t.Error("没有文档体的模型不应被发布")
		}
	}
	if v.ofd != nil || v.session != nil {
		t.Fatal("发布失败不应改变状态")
	}
}

func TestPublishDocumentKeepsPreviousOnFailure(t *testing.T) {
	v := newWindowOnlyViewer(t)
	v.operation.Store(1)
	path := filepath.Join("..", "..", "testdata", "helloworld.ofd")
	ofd, err := parser.NewOFD(path)
	if err != nil {
		t.Skipf("测试文档不可用: %v", err)
	}
	t.Cleanup(func() { _ = ofd.Close() })
	model, err := buildDocumentModel(ofd, "helloworld.ofd", color.Transparent)
	if err != nil {
		t.Fatalf("构造文档模型失败: %v", err)
	}
	if err := v.publishDocument(model, path, "helloworld.ofd"); err != nil {
		t.Fatalf("首次发布失败: %v", err)
	}
	firstSession := v.session
	firstPages := v.totalPages

	// 第二次发布失败时，之前的文档仍然可用。
	if bad, err := buildDocumentModel(&parser.OFD{}, "bad.ofd", color.Transparent); err == nil {
		if err := v.publishDocument(bad, "bad.ofd", "bad.ofd"); err == nil {
			t.Fatal("空文档体应当发布失败")
		}
	}
	if v.ofd != ofd || v.session != firstSession {
		t.Fatal("发布失败后不应替换已打开的文档")
	}
	if !firstSession.retired.Load() == false {
		t.Fatal("发布失败不应退休原有会话")
	}
	if v.totalPages != firstPages {
		t.Fatalf("发布失败后页数 = %d, want %d", v.totalPages, firstPages)
	}
}

func TestSetViewModeKeepsCurrentPageAndRenders(t *testing.T) {
	v := newLoadedTestViewer(t)
	for page := range v.pageSlots {
		v.pageSlots[page].renderable = false
	}
	v.totalPages = 3
	v.totalPages = len(v.pages)
	v.goToPage(2, false)
	if v.currentPage != 2 {
		t.Fatalf("跳转后当前页 = %d, want 2", v.currentPage)
	}
	// 双页模式下第 2 页是当前行的第一页，高亮行应为 1。
	v.setViewMode(viewDoublePageLabel)
	if v.pageLayout.mode != viewDoublePage {
		t.Fatalf("视图模式 = %d, want 双页", v.pageLayout.mode)
	}
	if v.currentPage != 2 {
		t.Fatalf("切换视图后当前页 = %d, want 2", v.currentPage)
	}
	if got := v.thumbnailRow(v.currentPage); got != 1 {
		t.Errorf("双页模式当前页行号 = %d, want 1", got)
	}
	// 适应高度允许横向滚动，其余模式只用纵向。
	v.setViewMode(viewFitHeightLabel)
	if v.pageScroll.Direction != container.ScrollBoth {
		t.Error("适应高度应允许双向滚动")
	}
	v.setViewMode(viewFitWidthLabel)
	if v.pageScroll.Direction != container.ScrollVerticalOnly {
		t.Error("适应宽度应只允许纵向滚动")
	}
}

func TestHandleKeyNavigatesAndExits(t *testing.T) {
	v := newLoadedTestViewer(t)
	v.totalPages = len(v.pages)

	// 方向键与 vim 键：a/w 上一页，d/s 下一页。
	v.goToPage(1, false)
	v.handleKey(&fyne.KeyEvent{Name: fyne.KeyRight})
	if v.currentPage != 2 {
		t.Errorf("右箭头后当前页 = %d, want 2", v.currentPage)
	}
	v.handleKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	if v.currentPage != 1 {
		t.Errorf("左箭头后当前页 = %d, want 1", v.currentPage)
	}
	v.goToPage(0, false)
	v.handleKey(&fyne.KeyEvent{Name: "S"})
	if v.currentPage != 1 {
		t.Errorf("S 键后当前页 = %d, want 1（S 应为下一页）", v.currentPage)
	}
	v.goToPage(1, false)
	v.handleKey(&fyne.KeyEvent{Name: "W"})
	if v.currentPage != 0 {
		t.Errorf("W 键后当前页 = %d, want 0（W 应为上一页）", v.currentPage)
	}

	// Home / End。
	v.handleKey(&fyne.KeyEvent{Name: fyne.KeyEnd})
	if v.currentPage != v.totalPages-1 {
		t.Errorf("End 后当前页 = %d, want %d", v.currentPage, v.totalPages-1)
	}
	v.handleKey(&fyne.KeyEvent{Name: fyne.KeyHome})
	if v.currentPage != 0 {
		t.Errorf("Home 后当前页 = %d, want 0", v.currentPage)
	}

	// 边界不能越界。
	v.handleKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	if v.currentPage != 0 {
		t.Errorf("首页再左移后当前页 = %d, want 0", v.currentPage)
	}
	v.goToPage(v.totalPages-1, false)
	v.handleKey(&fyne.KeyEvent{Name: fyne.KeyRight})
	if v.currentPage != v.totalPages-1 {
		t.Errorf("末页再右移后当前页 = %d, want %d", v.currentPage, v.totalPages-1)
	}

	// Esc 逐层退出：先关文档。
	if v.session != nil {
		v.session = newDocumentSession(nil)
	}
	v.handleKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if v.totalPages != 0 {
		t.Fatal("Esc 应先关闭文档而不是退出程序")
	}
}

func TestJumpToPageValidatesInput(t *testing.T) {
	v := newLoadedTestViewer(t)
	v.totalPages = len(v.pages)
	v.goToPage(0, false)

	for _, input := range []string{"", "abc", "0", "-1", "99"} {
		v.pageEntry.SetText(input)
		v.jumpToPage()
		if v.currentPage != 0 {
			t.Errorf("输入 %q 时不应跳转，当前页 = %d", input, v.currentPage)
		}
		if v.pageEntry.Text != "1" {
			t.Errorf("输入 %q 时页码框应被纠正为 1，实际 %q", input, v.pageEntry.Text)
		}
	}
	v.pageEntry.SetText("2")
	v.jumpToPage()
	if v.currentPage != 1 {
		t.Errorf("跳转输入 2 后当前页 = %d, want 1", v.currentPage)
	}
}

// waitFor polls until condition holds. 只能用于观察原子状态：后台协程通过
// fyne.Do 修改的普通字段在测试驱动下会与读取竞争。
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

func TestWriteExportWritesClosesAndReleasesSession(t *testing.T) {
	v := newThumbnailTestViewer(t, "hello.ofd")
	session := v.session
	documents := append([]*render.Document(nil), v.documents...)

	output := &countingWriteCloser{Buffer: &bytes.Buffer{}}
	err := writeExport(session, documents, func() (io.WriteCloser, error) { return output, nil },
		nil, "pdf", 72, color.Transparent)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if output.closes.Load() != 1 {
		t.Errorf("写入器关闭次数 = %d, want 1", output.closes.Load())
	}
	if !strings.HasPrefix(output.String(), "%PDF") {
		t.Errorf("导出结果不是 PDF，开头 %q", output.String()[:min(8, output.Len())])
	}
	// 会话引用必须归还，否则文档永远不会被真正关闭。
	if got := session.users.Load(); got != 0 {
		t.Errorf("导出结束后会话引用数 = %d, want 0", got)
	}
	if session.retired.Load() {
		t.Error("导出本身不应退休会话")
	}
}

func TestWriteExportRejectsRetiredSession(t *testing.T) {
	v := newThumbnailTestViewer(t, "hello.ofd")
	// 文档已被替换或关闭：会话退休，必须拒绝导出而不是读到半关闭的文档。
	v.session.retire()

	pending := &countingWriteCloser{Buffer: &bytes.Buffer{}}
	err := writeExport(v.session, v.documents, func() (io.WriteCloser, error) {
		t.Error("会话已退休时不应创建输出")
		return nil, nil
	}, pending, "pdf", 72, color.Transparent)
	if !errors.Is(err, errDocumentChanged) {
		t.Errorf("错误 = %v, want %v", err, errDocumentChanged)
	}
	// 待写句柄必须回收，否则会泄漏文件描述符。
	if pending.closes.Load() != 1 {
		t.Errorf("待写句柄关闭次数 = %d, want 1", pending.closes.Load())
	}
	if pending.Len() != 0 {
		t.Error("会话退休时不应写入任何内容")
	}
}

func TestWriteExportPropagatesCreateFailure(t *testing.T) {
	v := newThumbnailTestViewer(t, "hello.ofd")
	failure := errors.New("无法创建输出")
	err := writeExport(v.session, v.documents, func() (io.WriteCloser, error) {
		return nil, failure
	}, nil, "pdf", 72, color.Transparent)
	if !errors.Is(err, failure) {
		t.Errorf("错误 = %v, want %v", err, failure)
	}
	// 创建失败也必须归还会话引用。
	if got := v.session.users.Load(); got != 0 {
		t.Errorf("创建失败后会话引用数 = %d, want 0", got)
	}
}

func TestExportToPathReportsCreateFailure(t *testing.T) {
	v := newThumbnailTestViewer(t, "hello.ofd")
	// 目标目录不存在时创建失败，错误必须反馈而不是静默，也不能留下半个文件。
	unwritable := filepath.Join(t.TempDir(), "missing-dir", "out.pdf")
	if err := writeExport(v.session, v.documents, func() (io.WriteCloser, error) {
		return os.OpenFile(unwritable, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	}, nil, "pdf", 72, color.Transparent); err == nil {
		t.Fatal("写入不可用目录应当失败")
	}
	if _, err := os.Stat(unwritable); !os.IsNotExist(err) {
		t.Fatal("创建失败时不应留下输出文件")
	}
}

func TestOpenOFDAcceptsSupportedInputs(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "hello.ofd")
	// 路径、字节、io.Reader 三种输入都应能打开。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("测试文档不可用: %v", err)
	}
	for name, input := range map[string]any{
		"路径":        path,
		"字节":        raw,
		"io.Reader": bytes.NewReader(raw),
	} {
		ofd, err := openOFD(input)
		if err != nil {
			t.Errorf("%s 打开失败: %v", name, err)
			continue
		}
		if ofd == nil || len(ofd.Documents) == 0 {
			t.Errorf("%s 打开后没有文档体", name)
		}
		_ = ofd.Close()
	}
}

func TestOpenOFDRejectsBadInput(t *testing.T) {
	if _, err := openOFD(filepath.Join(t.TempDir(), "missing.ofd")); err == nil {
		t.Error("不存在的路径应报错")
	}
	if _, err := openOFD([]byte("not a zip")); err == nil {
		t.Error("非 ZIP 数据应报错")
	}
	if _, err := openOFD(42); err == nil {
		t.Error("不支持的输入类型应报错")
	}
	// parser.NewOFD 失败时仍返回一个空的 OFD：包句柄只在成功时赋值，文档列表
	// 也为空。load 的每个错误分支都显式关闭它，因此不会泄漏；重复 Close 同样
	// 安全。这里锁住这个行为，避免以后误以为失败时 ofd 一定是 nil。
	ofd, err := openOFD(nil)
	if err == nil {
		t.Error("nil 输入应报错")
	}
	if ofd != nil {
		if len(ofd.Documents) != 0 || len(ofd.DocBodies) != 0 {
			t.Error("失败的 OFD 不应残留文档数据")
		}
		if closeErr := ofd.Close(); closeErr != nil {
			t.Errorf("关闭失败的 OFD 应成功, got %v", closeErr)
		}
	}
}

func TestCloseInputHandlesNilAndCloser(t *testing.T) {
	if err := closeInput(nil); err != nil {
		t.Errorf("nil 输入关闭应成功, got %v", err)
	}
	if err := closeInput("path"); err != nil {
		t.Errorf("非 Closer 输入应直接成功, got %v", err)
	}
	closer := &countingWriteCloser{Buffer: &bytes.Buffer{}}
	if err := closeInput(closer); err != nil {
		t.Errorf("关闭 Closer 应成功, got %v", err)
	}
	if closer.closes.Load() != 1 {
		t.Errorf("关闭次数 = %d, want 1", closer.closes.Load())
	}
}

func TestCloseInputRecoversFromPanic(t *testing.T) {
	// 损坏的写入器可能在 Close 里 panic，必须转成错误而不是让加载流程崩溃。
	err := closeInput(panickingCloser{})
	if err == nil {
		t.Fatal("Close panic 应转为错误")
	}
}

// panickingCloser 是 Close 时会 panic 的写入器。
type panickingCloser struct{}

func (panickingCloser) Close() error { panic("关闭失败") }

func TestExportLoadingPopupLifecycle(t *testing.T) {
	v := newWindowOnlyViewer(t)
	v.showExportLoading()
	if v.exportLoading == nil {
		t.Fatal("显示后应有加载弹窗")
	}
	// 重复显示不应重建弹窗。
	first := v.exportLoading
	v.showExportLoading()
	if v.exportLoading != first {
		t.Fatal("重复显示不应重建加载弹窗")
	}
	v.hideExportLoading()
	if v.exportLoading != nil {
		t.Fatal("隐藏后加载弹窗应清空")
	}
	// 重复隐藏必须安全。
	v.hideExportLoading()
}

func TestShowAppInfoUsesEmbeddedIcon(t *testing.T) {
	// resources.go 改成读不到内嵌图标就 panic，这里守住它不会退化成空资源。
	if viewerIcon == nil {
		t.Fatal("内嵌图标为空，关于对话框会拿到空资源")
	}
	v := newWindowOnlyViewer(t)
	v.showAppInfo() // 不应 panic
}

func TestCloseRetiresSessionAndIsIdempotent(t *testing.T) {
	v := newThumbnailTestViewer(t, "hello.ofd")
	session := v.session

	v.close()
	if !v.closed.Load() {
		t.Fatal("close 应标记已关闭")
	}
	if !session.retired.Load() {
		t.Fatal("close 应退休会话")
	}
	// 重复 close 必须安全：closed 用 Swap 保护，closeDocument 允许重复。
	v.close()
}

func TestExitApplicationClosesWindowAndSession(t *testing.T) {
	test.NewTempApp(t)
	// exitApplication 会关闭窗口，因此这里不能注册 t.Cleanup(window.Close)：
	// Fyne 的测试驱动重复移除窗口会 panic。
	window := test.NewWindow(nil)
	v := newViewer(window)
	window.SetContent(v.content)
	session := &documentSession{}
	v.session = session

	v.exitApplication()
	if !session.retired.Load() {
		t.Error("退出程序应退休会话")
	}
	if !v.closed.Load() {
		t.Error("退出程序应标记已关闭")
	}
}

func TestCloseDocumentOrExitClosesBeforeExiting(t *testing.T) {
	v := newThumbnailTestViewer(t, "hello.ofd")
	prefillRenderCaches(v)
	session := v.session

	// 有文档时只关文档，不退出。
	v.closeDocumentOrExit()
	if !session.retired.Load() {
		t.Error("逐层退出的第一步应关闭文档")
	}
	if v.closed.Load() {
		t.Error("有文档时不应直接退出程序")
	}
	if v.totalPages != 0 {
		t.Error("关闭文档后应回到未加载状态")
	}
}

func TestExportExtension(t *testing.T) {
	tests := []struct {
		format string
		pages  int
		want   string
	}{
		{"pdf", 1, "pdf"},
		{"pdf", 12, "pdf"},
		{"txt", 12, "txt"},
		{"png", 1, "png"},
		{"png", 12, "zip"},
		{"SVG", 3, "zip"},
		{"eps", 1, "eps"},
	}
	for _, test := range tests {
		if got := exportExtension(test.format, test.pages); got != test.want {
			t.Errorf("exportExtension(%q, %d) = %q, want %q", test.format, test.pages, got, test.want)
		}
	}
}

func TestSameBackground(t *testing.T) {
	if !sameBackground(color.Transparent, color.Transparent) {
		t.Fatal("相同背景应判定为可复用")
	}
	if sameBackground(color.Transparent, color.White) {
		t.Fatal("不同背景不应判定为可复用")
	}
	if !sameBackground(nil, nil) {
		t.Fatal("两个空背景应判定为相同")
	}
	if sameBackground(nil, color.White) {
		t.Fatal("空背景与实际背景不应判定为相同")
	}
	// 同色不同表示也必须判定为相同。
	if !sameBackground(color.RGBA{R: 0x11, A: 0xff}, color.NRGBA{R: 0x11, A: 0xff}) {
		t.Fatal("渲染结果相同的颜色应判定为相同")
	}
}

func TestRenderDocumentBackgroundIsReported(t *testing.T) {
	doc := render.NewDocument(color.White, &parser.Document{Pages: []*parser.Page{{}}})
	if !sameBackground(doc.Background(), color.White) {
		t.Fatalf("文档背景 = %v, want 白色", doc.Background())
	}
	var missing *render.Document
	if missing.Background() != nil {
		t.Fatal("空文档的背景应为 nil")
	}
}

func TestCollectViewerPagesUsesGlobalSequence(t *testing.T) {
	documents := []*render.Document{
		render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}, {}}}),
		render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}}}),
	}

	pages := collectViewerPages(documents)
	if len(pages) != 3 {
		t.Fatalf("page count = %d, want 3", len(pages))
	}
	if pages[0].document != documents[0] || pages[0].page != documents[0].Pages[0] {
		t.Fatal("global page 1 does not map to document 1 page 1")
	}
	if pages[2].document != documents[1] || pages[2].page != documents[1].Pages[0] {
		t.Fatal("global page 3 does not map to document 2 page 1")
	}
}

func TestPageSlotStatesKeepsEntryForUnreadablePage(t *testing.T) {
	pages := []viewerPage{
		{page: nil},
		{page: &parser.Page{}},
	}

	states := pageSlotStates(pages)
	// 槽位下标必须与全局页码一一对应，否则布局和跳转会访问空槽位而崩溃。
	if len(states) != len(pages) {
		t.Fatalf("槽位数量 = %d, want %d", len(states), len(pages))
	}
	if states[0].renderable {
		t.Fatal("无法读取页面的槽位被标记为可渲染")
	}
	if states[0].aspect != 1 {
		t.Fatalf("无法读取页面的宽高比 = %v, want 1", states[0].aspect)
	}
	if !states[1].renderable {
		t.Fatal("可读取页面的槽位被标记为不可渲染")
	}
	if states[1].aspect <= 0 {
		t.Fatalf("可读取页面的宽高比 = %v, want > 0", states[1].aspect)
	}
}

func TestPageSlotStatesKeepsEntryForEveryPage(t *testing.T) {
	documents := []*render.Document{
		render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}, {}, nil}}),
	}
	pages := collectViewerPages(documents)
	// collectViewerPages 会丢弃空页面，槽位数量必须与保留下来的页面数量一致。
	states := pageSlotStates(pages)
	if len(states) != len(pages) {
		t.Fatalf("槽位数量 = %d, want %d", len(states), len(pages))
	}
	for i, state := range states {
		if !state.renderable {
			t.Fatalf("第 %d 页的槽位被标记为不可渲染", i)
		}
	}
}

func TestRenderPageImageAllowsConcurrentPages(t *testing.T) {
	input := filepath.Join("..", "..", "testdata", "helloworld.ofd")
	ofd, err := parser.NewOFD(input)
	if err != nil {
		t.Skipf("测试文档不可用: %v", err)
	}
	defer func() {
		_ = ofd.Close()
	}()

	doc := render.NewDocument(color.Transparent, ofd.Documents[0])
	page := ofd.Documents[0].Pages[0]
	viewer := &viewer{}
	alwaysValid := func() bool { return true }

	// 页面按需渲染不再串行化，同一文档的多个页面必须能并发栅格化。
	const workers = 4
	var group sync.WaitGroup
	images := make([]*image.RGBA, workers)
	errs := make([]error, workers)
	for i := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			images[i], errs[i] = viewer.renderPageImage(doc, page, geom.DPI(viewerDPI), alwaysValid)
		}()
	}
	group.Wait()

	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("并发渲染第 %d 个任务失败: %v", i, errs[i])
		}
		if images[i] == nil {
			t.Fatalf("并发渲染第 %d 个任务没有返回图像", i)
		}
	}
}

// newCacheTestViewer 构造用于验证图像缓存策略的阅读器：页面按 200 逻辑单位
// 依次排布，只有 visible 中的页面被绑定到显示帧。
func newCacheTestViewer(t *testing.T, pages int, visible ...int) *viewer {
	t.Helper()
	v := &viewer{
		pageLayout: &continuousLayout{},
		pageScroll: &container.Scroll{},
		pageSlots:  make([]*pageMeta, pages),
	}
	v.pageLayout.setPages(v.pageSlots)
	v.pageLayout.pageBounds = make([]pageBound, pages)
	for i := range v.pageSlots {
		v.pageSlots[i] = &pageMeta{aspect: 1, renderable: true}
		v.pageLayout.pageBounds[i] = pageBound{
			position: fyne.NewPos(0, float32(i*200)),
			size:     fyne.NewSize(100, 100),
		}
	}
	for i, frame := range v.pageLayout.frames {
		frame.page = -1
		if i < len(visible) {
			frame.page = visible[i]
		}
	}
	return v
}

func TestPageImageCacheEvictsOnlyPagesWithoutFrame(t *testing.T) {
	// 只有第 1 页（0 起算）仍被显示帧引用。
	v := newCacheTestViewer(t, 3, 1)
	cache := v.newPageImageCache()
	bound := toRGBA(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	// 模拟第 1 页正在显示：位图既在缓存里，也被显示帧引用。
	cache.AddWeighted(1, bound, rasterWeight(bound))
	v.frameForPage(1).image.Image = bound

	// 再加两条各占四分之三预算的条目，必然触发淘汰。
	const weight = 3 * pageImageBudget / 4
	cache.AddWeighted(0, nil, weight)
	cache.AddWeighted(2, nil, weight)

	if got := cache.Weight(); got > pageImageBudget {
		t.Fatalf("缓存字节数 = %d, want <= %d", got, int64(pageImageBudget))
	}
	if _, ok := cache.Get(1); !ok {
		t.Fatal("被显示帧引用的页面不应被淘汰，否则画面留白")
	}
	if v.frameForPage(1).image.Image != bound {
		t.Fatal("被显示帧引用的页面的图像被清空")
	}
	// 最早的可淘汰条目是第 0 页（无帧）。
	if _, ok := cache.Get(0); ok {
		t.Fatal("没有显示帧的第 0 页没有被淘汰")
	}
}

func TestPageImageCacheEvictsUnboundPagesOnly(t *testing.T) {
	// 只有第 0 页被显示帧引用，第 1 页没有帧，可以被淘汰。
	v := newCacheTestViewer(t, 2, 0)
	if !v.protectPageImage(0) {
		t.Fatal("被显示帧引用的页面应当受保护")
	}
	if v.protectPageImage(1) {
		t.Fatal("没有显示帧的页面不应受保护")
	}
	cache := v.newPageImageCache()
	const weight = 3 * pageImageBudget / 4
	cache.AddWeighted(0, nil, weight)
	cache.AddWeighted(1, nil, weight)

	if got := cache.Weight(); got > pageImageBudget {
		t.Fatalf("缓存字节数 = %d, want <= %d", got, int64(pageImageBudget))
	}
	if _, ok := cache.Get(0); !ok {
		t.Fatal("被显示帧引用的页面被淘汰，画面会留白")
	}
	if _, ok := cache.Get(1); ok {
		t.Fatal("没有显示帧的页面没有被淘汰")
	}
}

func TestPageImageCacheProtectsEverythingWithoutFrames(t *testing.T) {
	// 一个帧都没有时不应保护任何页面，缓存才能真正限流。
	v := newCacheTestViewer(t, 2)
	if v.protectPageImage(0) || v.protectPageImage(1) {
		t.Fatal("没有显示帧时不应保护页面")
	}
	cache := v.newPageImageCache()
	cache.AddWeighted(0, nil, 3*pageImageBudget/4)
	cache.AddWeighted(1, nil, 3*pageImageBudget/4)
	if _, ok := cache.Get(0); ok {
		t.Fatal("没有显示帧时页面没有被淘汰")
	}
}

func TestThumbnailCacheRespectsByteBudget(t *testing.T) {
	cache := newThumbnailCache()
	const weight = thumbnailBudget / 2
	cache.AddWeighted(0, nil, weight)
	cache.AddWeighted(1, nil, weight)
	cache.AddWeighted(2, nil, weight)

	if got := cache.Weight(); got > thumbnailBudget {
		t.Fatalf("缩略图缓存字节数 = %d, want <= %d", got, int64(thumbnailBudget))
	}
	if cache.Len() >= 3 {
		t.Fatalf("缩略图缓存条目数 = %d, want < 3", cache.Len())
	}
}

func TestRasterWeight(t *testing.T) {
	if got := rasterWeight(nil); got != 1 {
		t.Fatalf("空栅格权重 = %d, want 1", got)
	}
	if got := rasterWeight(image.NewRGBA(image.Rect(0, 0, 2, 2))); got != 16 {
		t.Fatalf("2x2 栅格权重 = %d, want 16", got)
	}
}

func TestMaxExportDPI(t *testing.T) {
	const a4Area = 210.0 * 297.0
	if got := maxExportDPI(0); got != exportMaxDPI {
		t.Fatalf("页面尺寸未知时的上限 = %d, want %d", got, exportMaxDPI)
	}
	got := maxExportDPI(a4Area)
	// A4 在 128 MB 预算下约 590 DPI，1200 DPI 的上限不应再是实际约束。
	if got <= 300 || got >= exportMaxDPI {
		t.Fatalf("A4 页面的 DPI 上限 = %d, want 300..%d", got, exportMaxDPI-1)
	}
	// 上限对应的单页 RGBA 内存必须落在预算内。
	unit := float64(geom.DPI(1))
	bytes := 4 * a4Area * float64(got) * float64(got) * unit * unit
	if bytes > float64(exportPageBudget) {
		t.Fatalf("DPI %d 对应单页 %.0f MB, want <= %d MB", got, bytes/(1<<20), exportPageBudget>>20)
	}
	if large := maxExportDPI(841.0 * 1189.0); large >= got {
		t.Fatalf("A0 页面的 DPI 上限 = %d, want < %d", large, got)
	}
}

func TestPageSlotStatesTracksPageArea(t *testing.T) {
	pages := collectViewerPages([]*render.Document{
		render.NewDocument(color.Transparent, &parser.Document{Pages: []*parser.Page{{}}}),
	})
	states := pageSlotStates(pages)
	// 空页面区域回退到 A4，用于换算导出 DPI 上限。
	if want := 210.0 * 297.0; states[0].area != want {
		t.Fatalf("页面面积 = %v, want %v", states[0].area, want)
	}
}

func TestDocumentTitlePrefersMetadataAndFallsBackToFileName(t *testing.T) {
	title := "文档标题"
	ofd := &parser.OFD{OFD: models.OFD{
		DocBodies: []models.DocBody{{DocInfo: models.DocInfo{Title: &title}}},
	}}
	if got := documentTitle(ofd, "document.ofd"); got != title {
		t.Fatalf("document title = %q, want %q", got, title)
	}

	if got := documentTitle(&parser.OFD{}, "document.ofd"); got != "document.ofd" {
		t.Fatalf("fallback title = %q, want %q", got, "document.ofd")
	}
	if got := documentTitle(nil, ""); got != "未加载文档" {
		t.Fatalf("empty title = %q, want %q", got, "未加载文档")
	}
}

// TestDisplayVersionHasVPrefix 守住关于对话框的展示格式。
//
// 版本号本身是 internal/version.Version（Makefile 注入的裸版本号），Makefile 与
// 它的一致性由 internal/version 包的 TestVersionMatchesMakefile 负责；查看器这里
// 只负责加 v 前缀。少了前缀关于对话框会显示 "版本: 0.1.2"，与文档和 release tag
// 的写法对不上。
func TestDisplayVersionHasVPrefix(t *testing.T) {
	got := displayVersion()
	if !strings.HasPrefix(got, "v") {
		t.Errorf("displayVersion() = %q，期望带 v 前缀", got)
	}
	if want := "v" + version.Version; got != want {
		t.Errorf("displayVersion() = %q，期望 %q", got, want)
	}
}

// TestDefaultWindowSizeFitsA4Page 守住首屏正好放得下一整页 A4。
//
// 默认视图是“适应宽度”：页面宽度等于页面区宽度，整页 A4 需要的高度可以事先算出来。
// 窗口高度若只按窗口本身的 A4 比例给，工具栏会把页面挤到屏幕外，读者还得手动拉一点
// 滚动条才看得全页——这正是要防的回归。工具栏高度取自工具栏 MinSize，所以工具栏日后
// 加了更高的控件，这条断言会先失败，提示同步更新这里。
func TestDefaultWindowSizeFitsA4Page(t *testing.T) {
	test.NewTempApp(t)
	v := newViewer(test.NewWindow(nil))
	size := v.defaultWindowSize()

	toolbarHeight := v.toolbar.MinSize().Height
	pageWidth := size.Width - 2*pageMargin
	pageHeight := size.Height - toolbarHeight - 2*pageMargin
	if want := a4PageHeight(float32(windowWidth - 2*pageMargin)); pageHeight < want-0.5 {
		t.Errorf("页面区 %.0f×%.0f 放不下整页 A4（需要高 %.0f，工具栏 %.0f）",
			pageWidth, pageHeight, want, toolbarHeight)
	}
	// 反过来也不能给得过分：多出来的空白同样没有意义，A4 页面之下留一整屏灰底。
	if pageHeight > a4PageHeight(float32(windowWidth-2*pageMargin))+pageMargin {
		t.Errorf("页面区高 %.0f，比整页 A4 高出太多", pageHeight)
	}
	if toolbarHeight <= 0 {
		t.Fatal("工具栏高度不应为 0")
	}
}
