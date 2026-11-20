package main

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestThumbnailDocumentRendersOnDemandAndBoundsImage(t *testing.T) {
	m, err := openThumbnailDocument(createTestDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	if len(m.pages) != 2 {
		t.Fatalf("全局页面数错误: %d", len(m.pages))
	}
	img, err := m.render(0)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() > thumbnailMaxPixels || img.Bounds().Dy() > thumbnailMaxPixels {
		t.Fatalf("缩略图超出像素上限: %v", img.Bounds())
	}
	if got := img.RGBAAt(13, 13); got.R > 20 || got.G > 20 || got.B > 20 || got.A != 255 {
		t.Fatalf("页面路径未绘制: %v", got)
	}
	if got := img.RGBAAt(img.Bounds().Dx()-5, img.Bounds().Dy()-5); got.A != 0 {
		t.Fatalf("缩略图底色应透明以便切换纸张颜色: %v", got)
	}
	if _, err := m.render(2); !errors.Is(err, errPageRange) {
		t.Fatalf("越界页未返回可识别错误: %v", err)
	}
}

func TestThumbnailGenerationChangesOnlyForSuccessfulNewDocument(t *testing.T) {
	first := &countedScene{Canvas: canvas.New(10, 10)}
	second := &countedScene{Canvas: canvas.New(10, 10)}
	v := viewer{viewScene: first, scene: first, page: 0, total: 2}
	v.thumbnails.reset("first.ofd")
	generation := v.thumbnails.generation
	persist, title := func() {}, func(string) {}
	v.pendingOpen = true
	v.acceptResponse(result{scene: first, filename: "first.ofd", total: 2}, persist, title)
	if v.thumbnails.generation != generation {
		t.Fatal("取消打开不应丢弃已有缩略图")
	}
	v.pendingOpen = true
	v.acceptResponse(result{scene: first, filename: "first.ofd", total: 2, err: errors.New("open failed")}, persist, title)
	if v.thumbnails.generation != generation {
		t.Fatal("打开失败不应丢弃已有缩略图")
	}
	v.acceptResponse(result{scene: second, filename: "first.ofd", page: 1, total: 2}, persist, title)
	if v.thumbnails.generation != generation || v.thumbnails.list.Position.First != 1 {
		t.Fatal("普通翻页应保留缩略图并定位新页")
	}
	v.pendingOpen = true
	v.acceptResponse(result{scene: first, filename: "second.ofd", total: 2}, persist, title)
	if v.thumbnails.generation != generation+1 || v.thumbnails.filename != "second.ofd" {
		t.Fatal("打开新文档应更新代次并清除旧缩略图")
	}
}

func TestThumbnailWorkerCancellationWhilePublishing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan thumbnailRequest, 1)
	results := make(chan thumbnailResult)
	done := make(chan struct{})
	requests <- thumbnailRequest{filename: createTestDocument(t), generation: 1, page: 0}
	go func() {
		defer close(done)
		runThumbnailWorker(ctx, requests, results, func() {})
	}()
	// 等待第一页结果后再请求第二页，随后在没有接收者时取消。
	select {
	case result := <-results:
		if result.err != nil {
			t.Fatal(result.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("缩略图渲染未完成")
	}
	requests <- thumbnailRequest{filename: createTestDocument(t), generation: 2, page: 1}
	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("取消后缩略图 worker 未退出")
	}
}

func TestThumbnailCacheIsBoundedAndRejectsStaleResults(t *testing.T) {
	var s thumbnailState
	s.reset("first.ofd")
	old := s.generation
	s.busy = true
	s.reset("second.ofd")
	if !s.busy {
		t.Fatal("换文档不能丢失尚未返回的在途请求")
	}
	s.accept(thumbnailResult{generation: old, page: 0, image: image.NewRGBA(image.Rect(0, 0, 10, 10))})
	if s.busy || s.images.Len() != 0 {
		t.Fatal("旧文档缩略图不应进入新文档缓存")
	}
	for i := range thumbnailCacheEntries + 10 {
		s.accept(thumbnailResult{generation: s.generation, page: i, image: image.NewRGBA(image.Rect(0, 0, 64, 64))})
	}
	if s.images.Len() > thumbnailCacheEntries || s.images.Weight() > thumbnailCacheBytes {
		t.Fatalf("缩略图缓存超限: entries=%d bytes=%d", s.images.Len(), s.images.Weight())
	}
	if _, ok := s.images.Get(0); ok {
		t.Fatal("最久未使用的缩略图未淘汰")
	}
	s.accept(thumbnailResult{generation: s.generation, page: 200, err: errors.New("render failed")})
	if _, ok := s.failures.Get(200); !ok {
		t.Fatal("失败页应有界记录，避免每帧重试")
	}
	s.reset("large.ofd")
	for i := range 70 {
		s.accept(thumbnailResult{generation: s.generation, page: i, image: image.NewRGBA(image.Rect(0, 0, thumbnailMaxPixels, thumbnailMaxPixels))})
	}
	if s.images.Weight() > thumbnailCacheBytes || s.images.Len() >= 70 {
		t.Fatalf("缩略图字节预算未生效: entries=%d bytes=%d", s.images.Len(), s.images.Weight())
	}
}

func TestThumbnailListVirtualizesAndClickSelectsGlobalPage(t *testing.T) {
	var s thumbnailState
	s.reset("example.ofd")
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(170, 500)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	s.layout(gtx, th, 10000, 0, false)
	if len(s.buttons) == 0 || len(s.buttons) > 8 || s.next != 0 {
		t.Fatalf("缩略图列表未虚拟化: visible=%d next=%d", len(s.buttons), s.next)
	}
	button := s.buttons[1]
	if button == nil {
		t.Fatal("第二页未在视口内")
	}
	button.Click()
	var requests []request
	s.handleClicks(gtx, 0, false, func(req request) { requests = append(requests, req) })
	if len(requests) != 1 || requests[0].page != 1 {
		t.Fatalf("点击缩略图没有按全局页码跳转: %v", requests)
	}
	s.reveal(200)
	ops.Reset()
	s.layout(gtx, th, 10000, 200, false)
	if _, ok := s.buttons[200]; !ok {
		t.Fatal("跳页后缩略图列表没有定位到当前页")
	}
}

func TestThumbnailListDisplaysCachedImageOnGPU(t *testing.T) {
	window, err := headless.NewWindow(170, 200)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var s thumbnailState
	s.reset("example.ofd")
	img := image.NewRGBA(image.Rect(0, 0, 40, 60))
	for y := range 60 {
		for x := range 40 {
			img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	s.accept(thumbnailResult{generation: s.generation, page: 0, image: img})
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(170, 200)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	s.layout(gtx, th, 1, 0, false)
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 170, 200))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	red := 0
	for y := range 160 {
		for x := range 170 {
			c := pixels.RGBAAt(x, y)
			if c.R > 240 && c.G < 10 && c.B < 10 {
				red++
			}
		}
	}
	if red < 100 {
		t.Fatalf("缩略图未显示在 GPU 输出中: red=%d", red)
	}
}

func TestThumbnailPageBadgeIsOnImageAndKeepsClickTarget(t *testing.T) {
	window, err := headless.NewWindow(170, 200)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var s thumbnailState
	s.reset("example.ofd")
	img := image.NewRGBA(image.Rect(0, 0, 40, 60))
	for y := range 60 {
		for x := range 40 {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	s.accept(thumbnailResult{generation: s.generation, page: 0, image: img})
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(170, 200)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	s.layout(gtx, continuousTestTheme(), 1, 1, false)
	router.Frame(&ops)
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 170, 200))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	// 图片右下角角标使用深底，左下角仍是白色纸张。
	if badge, paper := pixels.RGBAAt(125, 145), pixels.RGBAAt(45, 145); badge.R > 180 || paper.R < 240 {
		t.Fatalf("页码角标未显示在图片右下角: badge=%v paper=%v", badge, paper)
	}
	router.Queue(pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: f32.Pt(125, 145)})
	router.Queue(pointer.Event{Kind: pointer.Release, Position: f32.Pt(125, 145)})
	var selected = -1
	s.handleClicks(gtx, 1, false, func(req request) { selected = req.page })
	if selected != 0 {
		t.Fatalf("点击页码角标应选中缩略图: page=%d", selected)
	}
}
