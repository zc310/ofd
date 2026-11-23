package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// 完整窗口验证悬停计时、后台请求及浮层不拦截点击，覆盖缩放和连续页变换。
func TestLinkPreviewHoverDelayAndClickThroughInAllReadingModes(t *testing.T) {
	for _, mode := range []string{"单页", "连续", "双页"} {
		for _, scale := range []float32{0.75, 2.3, 4} {
			t.Run(fmt.Sprintf("%s/%.2f", mode, scale), func(t *testing.T) {
				h := newLinkPreviewHarness(t, mode, scale, "")
				h.move(pointer.Mouse, h.point)
				if !h.v.linkPreview.active || h.v.linkPreview.visible {
					t.Fatalf("悬停未进入等待状态: %+v", h.v.linkPreview)
				}
				h.gtx.Now = h.gtx.Now.Add(linkPreviewDelay - time.Millisecond)
				h.frame()
				if h.v.linkPreview.visible {
					t.Fatal("未满400毫秒不应显示预览")
				}
				if _, ok := h.v.nextThumbnailRequest(); ok {
					t.Fatal("悬停延迟期间不应提交预览渲染")
				}
				h.gtx.Now = h.gtx.Now.Add(time.Millisecond)
				h.frame()
				req, ok := h.v.nextThumbnailRequest()
				if !h.v.linkPreview.visible || !ok || !req.preview || req.page != 1 {
					t.Fatalf("关闭侧栏时仍应生成目标页预览: visible=%v req=%+v ok=%v", h.v.linkPreview.visible, req, ok)
				}
				if !h.pointInAnchor() {
					t.Fatalf("预览锚点未保持窗口坐标: point=%v anchor=%v", h.point, h.v.linkPreview.anchor)
				}
				h.router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: h.point})
				h.frame()
				if h.v.linkPreview.visible {
					t.Fatal("按下鼠标应隐藏预览")
				}
				h.router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: h.point})
				h.frame()
				if h.v.linkJump == nil || h.v.linkJump.target.Page != 1 {
					t.Fatal("预览浮层拦截了原有链接点击")
				}
			})
		}
	}
}

func TestLinkPreviewIgnoresTouchExternalAndNonRectangularOutside(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source pointer.Source
		uri    string
		region string
	}{
		{name: "触屏", source: pointer.Touch},
		{name: "外部网址", source: pointer.Mouse, uri: "https://example.test"},
		{name: "热区外接矩形空白", source: pointer.Mouse, region: "M0 0 L0.1 0 L0 0.1 z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newLinkPreviewHarness(t, "单页", 2, tc.uri)
			scene, ok := h.v.scene.(*linkedPageScene)
			if !ok {
				t.Fatal("测试页面缺少链接")
			}
			if tc.region != "" {
				scene.links[0].region = canvas.MustParseSVGPath(tc.region)
			}
			h.move(tc.source, h.point)
			h.gtx.Now = h.gtx.Now.Add(time.Second)
			h.frame()
			if h.v.linkPreview.active || h.v.linkPreview.visible {
				t.Fatalf("不应显示内部链接预览: %+v", h.v.linkPreview)
			}
		})
	}
}

func TestLinkPreviewHidesOnLeaveMenuAndDocumentClose(t *testing.T) {
	for _, action := range []string{"移开", "菜单", "关闭文档"} {
		t.Run(action, func(t *testing.T) {
			h := newLinkPreviewHarness(t, "单页", 2, "")
			h.move(pointer.Mouse, h.point)
			h.gtx.Now = h.gtx.Now.Add(time.Second)
			h.frame()
			if !h.v.linkPreview.visible {
				t.Fatal("预览未显示")
			}
			switch action {
			case "移开":
				h.move(pointer.Mouse, f32.Pt(890, 690))
			case "菜单":
				h.v.menu.open = true
			case "关闭文档":
				h.v.closeDocument(func(request) {})
			}
			h.frame()
			if h.v.linkPreview.visible || h.v.linkPreview.source != nil {
				t.Fatalf("预览仍残留: %+v", h.v.linkPreview)
			}
			if req, ok := h.v.nextThumbnailRequest(); ok && req.preview {
				t.Fatal("隐藏后不应继续请求预览")
			}
		})
	}
}

func TestLinkPreviewCacheIsSeparateBoundedAndRejectsStaleResults(t *testing.T) {
	var s thumbnailState
	s.reset("first.ofd")
	for i := range linkPreviewCacheEntries + 2 {
		s.accept(thumbnailResult{generation: s.generation, page: i, preview: true, image: image.NewRGBA(image.Rect(0, 0, 64, 64))})
	}
	if s.previews.Len() != linkPreviewCacheEntries || s.images.Len() != 0 {
		t.Fatal("预览缓存必须有界且独立于低分辨率缩略图")
	}
	s.reset("large.ofd")
	for i := range 4 {
		s.accept(thumbnailResult{generation: s.generation, page: i, preview: true, image: image.NewRGBA(image.Rect(0, 0, linkPreviewMaxPixels, linkPreviewMaxPixels))})
	}
	if s.previews.Weight() > linkPreviewCacheBytes || s.previews.Len() != 2 {
		t.Fatalf("预览缓存字节预算未生效: entries=%d bytes=%d", s.previews.Len(), s.previews.Weight())
	}
	old := s.generation
	s.reset("second.ofd")
	s.busy = true
	s.accept(thumbnailResult{generation: old, page: 0, preview: true, image: image.NewRGBA(image.Rect(0, 0, 64, 64))})
	if s.busy || s.previews.Len() != 0 {
		t.Fatal("旧文档结果不能进入新文档预览缓存")
	}
	v := viewer{thumbnails: s, linkPreview: linkPreviewState{visible: true, target: drawing.LinkTarget{Page: 1}}}
	v.thumbnails.open, v.thumbnails.next = true, 0
	req, ok := v.nextThumbnailRequest()
	if !ok || !req.preview || req.page != 1 {
		t.Fatal("悬停预览应优先于侧栏缩略图")
	}
	v.thumbnails.accept(thumbnailResult{generation: s.generation, page: 1, preview: true, err: errors.New("render failed")})
	req, ok = v.nextThumbnailRequest()
	if !ok || req.preview || req.page != 0 {
		t.Fatal("预览失败后不能每帧重试或阻塞侧栏")
	}
}

func TestLinkPreviewFrameAndDestinationCrop(t *testing.T) {
	window := image.Pt(1000, 800)
	for _, anchor := range []image.Rectangle{image.Rect(0, 0, 20, 10), image.Rect(980, 790, 1000, 800), image.Rect(400, 600, 500, 620)} {
		frame := linkPreviewFrame(window, anchor, 1)
		if !frame.In(image.Rectangle{Max: window}) || frame.Dx() != 820 || frame.Dy() != 360 {
			t.Fatalf("浮层超出窗口或尺寸错误: %v", frame)
		}
	}
	frame := linkPreviewFrame(window, image.Rect(400, 600, 500, 620), 1)
	if frame.Max.Y >= 600 {
		t.Fatal("空间足够时应优先显示在链接上方")
	}
	for _, tc := range []struct {
		name string
		dest drawing.LinkTarget
		top  int
	}{
		{"页顶", drawing.LinkTarget{Type: drawing.DestXYZ, Top: 0, PageHeight: 300}, 0},
		{"页中", drawing.LinkTarget{Type: drawing.DestXYZ, Top: 150, PageHeight: 300}, -480},
		{"页底", drawing.LinkTarget{Type: drawing.DestXYZ, Top: 300, PageHeight: 300}, -800},
		{"无位置", drawing.LinkTarget{Type: drawing.DestFit}, -480},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offset, size := linkPreviewCrop(image.Pt(800, 400), image.Pt(800, 1200), tc.dest)
			if offset != image.Pt(0, tc.top) || size != image.Pt(800, 1200) {
				t.Fatalf("目标位置裁剪错误: offset=%v size=%v", offset, size)
			}
		})
	}
}

func TestLinkPreviewWorkerRendersHigherResolutionThanSidebar(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	requests, results := make(chan thumbnailRequest, 1), make(chan thumbnailResult, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runThumbnailWorker(ctx, requests, results, func() {})
	}()
	defer func() {
		cancel()
		<-done
	}()
	filename := createTestDocument(t)
	var sidebar image.Point
	for _, preview := range []bool{false, true} {
		requests <- thumbnailRequest{filename: filename, generation: 1, page: 0, preview: preview}
		select {
		case result := <-results:
			if result.err != nil || result.preview != preview || result.image == nil {
				t.Fatalf("渲染预览失败: %+v", result)
			}
			size := result.image.Bounds().Size()
			if !preview {
				sidebar = size
			} else if size.X <= sidebar.X || size.Y <= sidebar.Y || size.X > linkPreviewMaxPixels || size.Y > linkPreviewMaxPixels {
				t.Fatalf("预览分辨率错误: preview=%v sidebar=%v", size, sidebar)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("后台预览渲染超时")
		}
	}
}

func TestLinkPreviewGPUShowsDestinationCropBorderAndUnclippedShadow(t *testing.T) {
	window, err := headless.NewWindow(1000, 800)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	v := viewer{background: "transparent", linkPreview: linkPreviewState{
		active: true, since: time.Unix(1, 0), anchor: image.Rect(400, 600, 500, 620),
		target: drawing.LinkTarget{Page: 1, Type: drawing.DestXYZ, Top: 150, PageHeight: 300},
	}}
	v.thumbnails.reset("preview.ofd")
	img := image.NewRGBA(image.Rect(0, 0, 800, 1200))
	// 只有目标位置是红色，其余透明；透明处不能透出下层绿色正文。
	draw.Draw(img, image.Rect(0, 600, 800, 650), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	v.thumbnails.accept(thumbnailResult{generation: v.thumbnails.generation, page: 1, preview: true, image: img})
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Now: time.Unix(2, 0), Constraints: layout.Exact(image.Pt(1000, 800)),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	paint.Fill(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	frame := linkPreviewFrame(gtx.Constraints.Max, v.linkPreview.anchor, 1)
	paint.FillShape(gtx.Ops, color.NRGBA{G: 255, A: 255}, clip.Rect(frame).Op())
	v.layoutLinkPreview(gtx, continuousTestTheme())
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 1000, 800))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	centerX := (frame.Min.X + frame.Max.X) / 2
	border := pixels.RGBAAt(centerX, frame.Min.Y)
	shadow := pixels.RGBAAt(frame.Min.X-4, frame.Min.Y+frame.Dy()/2)
	outside := pixels.RGBAAt(frame.Min.X-20, frame.Min.Y+frame.Dy()/2)
	if border.R > 180 || shadow.R >= 250 || shadow.R >= outside.R || outside.R != 255 {
		t.Fatalf("边框或柔和外阴影未显示: border=%v shadow=%v outside=%v", border, shadow, outside)
	}
	target := pixels.RGBAAt(centerX, frame.Min.Y+frame.Dy()*3/10+12)
	paper := pixels.RGBAAt(centerX, frame.Min.Y+20)
	if target.R < 240 || target.G > 10 || paper.R < 240 || paper.G < 240 || paper.B < 240 {
		t.Fatalf("目标局部未显示或透明底板透出下层正文: target=%v paper=%v", target, paper)
	}
	if badge := pixels.RGBAAt(frame.Max.X-12, frame.Max.Y-12); badge.R > 130 || badge.G > 130 || badge.B > 130 {
		t.Fatalf("页码角标未固定显示在预览右下角: %v", badge)
	}
}

func TestLinkPreviewPageBadgeUsesGlobalTargetPageInEveryLoadingState(t *testing.T) {
	for _, state := range []string{"加载中", "已缓存", "失败"} {
		for _, page := range []int{0, 99} {
			t.Run(fmt.Sprintf("%s/目标%d", state, page+1), func(t *testing.T) {
				v := viewer{page: 5, linkPreview: linkPreviewState{
					active: true, since: time.Unix(1, 0), anchor: image.Rect(400, 600, 500, 620),
					target: drawing.LinkTarget{Page: page, Type: drawing.DestXYZ, Top: 150, PageHeight: 300},
				}}
				v.thumbnails.reset("badge.ofd")
				switch state {
				case "已缓存":
					v.thumbnails.accept(thumbnailResult{generation: v.thumbnails.generation, page: page,
						preview: true, image: image.NewRGBA(image.Rect(0, 0, 800, 1200))})
				case "失败":
					v.thumbnails.accept(thumbnailResult{generation: v.thumbnails.generation, page: page,
						preview: true, err: errors.New("render failed")})
				}
				var ops op.Ops
				var router input.Router
				gtx := layout.Context{Ops: &ops, Source: router.Source(), Now: time.Unix(2, 0),
					Constraints: layout.Exact(image.Pt(1000, 800)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
				v.layoutLinkPreview(gtx, continuousTestTheme())
				router.Frame(&ops)
				nodes := router.AppendSemantics(nil)
				want := fmt.Sprintf("第 %d 页", page+1)
				found := false
				frame := linkPreviewFrame(gtx.Constraints.Max, v.linkPreview.anchor, 1)
				for len(nodes) > 0 {
					node := nodes[0]
					nodes = append(nodes[1:], node.Children...)
					if node.Desc.Description != want {
						continue
					}
					found = true
					bounds := node.Desc.Bounds
					if !bounds.In(frame) || bounds.Min.X < frame.Min.X+frame.Dx()/2 || bounds.Min.Y < frame.Min.Y+frame.Dy()/2 {
						t.Fatalf("目标页码没有固定在浮层右下角: badge=%v frame=%v", bounds, frame)
					}
					if node.Desc.Gestures != 0 {
						t.Fatal("页码角标不能创建交互热区")
					}
				}
				if !found {
					t.Fatalf("缺少目标页码角标 %q", want)
				}
			})
		}
	}
}

func TestLinkPreviewRoundedBorderContinuesThroughAllFourCorners(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		for _, cached := range []bool{false, true} {
			t.Run(fmt.Sprintf("缩放%.0f/已缓存%v", scale, cached), func(t *testing.T) {
				window, err := headless.NewWindow(1000, 800)
				if err != nil {
					t.Skipf("无可用的无头 GPU 环境: %v", err)
				}
				defer window.Release()
				v := viewer{linkPreview: linkPreviewState{
					active: true, since: time.Unix(1, 0), anchor: image.Rect(400, 600, 500, 620),
					target: drawing.LinkTarget{Page: 1, Type: drawing.DestFit},
				}}
				v.thumbnails.reset("corners.ofd")
				if cached {
					img := image.NewRGBA(image.Rect(0, 0, 10, 10))
					draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
					v.thumbnails.accept(thumbnailResult{generation: v.thumbnails.generation, page: 1, preview: true, image: img})
				}
				var ops op.Ops
				gtx := layout.Context{Ops: &ops, Now: time.Unix(2, 0), Constraints: layout.Exact(image.Pt(1000, 800)),
					Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}}
				paint.Fill(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
				v.layoutLinkPreview(gtx, continuousTestTheme())
				if err := window.Frame(&ops); err != nil {
					t.Fatal(err)
				}
				pixels := image.NewRGBA(image.Rect(0, 0, 1000, 800))
				if err := window.Screenshot(pixels); err != nil {
					t.Fatal(err)
				}
				frame := linkPreviewFrame(gtx.Constraints.Max, v.linkPreview.anchor, scale)
				inset := int(2 * scale)
				for _, point := range []image.Point{
					frame.Min.Add(image.Pt(inset, inset)),
					{X: frame.Max.X - 1 - inset, Y: frame.Min.Y + inset},
					{X: frame.Min.X + inset, Y: frame.Max.Y - 1 - inset},
					frame.Max.Sub(image.Pt(1+inset, 1+inset)),
				} {
					if corner := pixels.RGBAAt(point.X, point.Y); corner.R > 235 {
						t.Errorf("圆角灰色边框被白色底板或图片覆盖: point=%v color=%v", point, corner)
					}
				}
				inside := frame.Min.Add(image.Pt(int(8*scale), int(8*scale)))
				if paper := pixels.RGBAAt(inside.X, inside.Y); paper.R < 250 || paper.G < 250 || paper.B < 250 {
					t.Fatalf("圆角内侧应保留白色纸张: %v", paper)
				}
			})
		}
	}
}

type linkPreviewHarness struct {
	v      viewer
	router input.Router
	ops    op.Ops
	gtx    layout.Context
	point  f32.Point
	frame  func()
}

func newLinkPreviewHarness(t *testing.T, mode string, scale float32, uri string) *linkPreviewHarness {
	t.Helper()
	target := drawing.LinkTarget{Page: 1, Type: drawing.DestXYZ, Top: 150, PageHeight: 297.25}
	scene := newLinkedPageScene(&countedScene{Canvas: canvas.New(210.5, 297.25)}, []drawing.PageLink{{URI: uri, Target: &target, X: 17, Y: 18, Width: 8, Height: 3}})
	h := &linkPreviewHarness{v: viewer{scene: scene, viewScene: scene, total: 2, viewport: pageViewport{mode: fitManual, scale: scale}}}
	v := &h.v
	v.thumbnails.reset("preview.ofd")
	v.continuous.enabled, v.continuous.double = mode != "单页", mode == "双页"
	v.continuous.reset(0)
	for i := range 2 {
		v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{210.5, 297.25}})
		v.continuous.entries.AddWeighted(i, &continuousEntry{scene: scene}, scene.Weight())
	}
	h.gtx = layout.Context{Ops: &h.ops, Source: h.router.Source(), Now: time.Unix(1, 0),
		Constraints: layout.Exact(image.Pt(900, 700)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := continuousTestTheme()
	h.frame = func() {
		h.ops.Reset()
		v.layout(h.gtx, th)
		h.router.Frame(&h.ops)
	}
	h.frame()
	margin := h.gtx.Dp(readingMargin)
	h.point = f32.Pt(float32(margin)+float32(math.Round(float64(v.viewport.offset.X)))+20*scale,
		float32(700-margin-v.viewport.viewport.Y)+float32(math.Round(float64(v.viewport.offset.Y)))+20*scale)
	if v.continuous.enabled {
		width := float32(math.Round(210.5 * float64(scale)))
		if v.continuous.double {
			width = width*2 + float32(h.gtx.Dp(unit.Dp(12)))
		}
		h.point.X = float32(margin) + float32(math.Round(float64(v.continuous.pageX(width, float32(v.viewport.viewport.X))))) + 20*scale
		h.point.Y = float32(700-margin-v.viewport.viewport.Y) + 20*scale
	}
	return h
}

func (h *linkPreviewHarness) move(source pointer.Source, point f32.Point) {
	h.router.Queue(pointer.Event{Kind: pointer.Move, Source: source, Position: point})
	h.frame()
}

func (h *linkPreviewHarness) pointInAnchor() bool {
	return image.Pt(int(h.point.X), int(h.point.Y)).In(h.v.linkPreview.anchor)
}
