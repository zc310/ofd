package main

import (
	"io"
	"log/slog"
	"os"
	"testing"

	"image"
	"image/color"
	"math"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	fyneCanvas "fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
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
	v.createPageSlots(v.pages)
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
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
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
	v := newLoadedTestViewer(t)
	v.pageScroll.Resize(fyne.NewSize(400, 600))
	v.pageContent.Refresh()
	if len(v.pageLayout.pageBounds) < 5 {
		t.Skipf("测试文档页数不足: %d", len(v.pageLayout.pageBounds))
	}
	pageHeight := v.pageLayout.pageBounds[0].size.Height
	if pageHeight <= 0 {
		t.Skip("页面高度无效")
	}
	// 视口约能显示 600/pageHeight 页，一次翻页应跳到那么远之后。
	before := v.currentPage
	v.scrollByViewport(1)
	if v.currentPage <= before {
		t.Fatalf("向下翻页后当前页 = %d, want > %d", v.currentPage, before)
	}
	down := v.currentPage
	v.scrollByViewport(-1)
	if v.currentPage >= down {
		t.Fatalf("向上翻页后当前页 = %d, want < %d", v.currentPage, down)
	}
	// 顶部继续向上翻页必须停在第一页，不能越界。
	v.goToPage(0, false)
	v.scrollByViewport(-1)
	if v.currentPage != 0 {
		t.Fatalf("顶部继续向上翻页后当前页 = %d, want 0", v.currentPage)
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
	input := filepath.Join("..", "..", "test", "testdata", "helloworld.ofd")
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
