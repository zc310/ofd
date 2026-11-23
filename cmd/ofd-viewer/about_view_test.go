package main

import (
	"fmt"
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func TestAboutDialogFitsContentWithoutBlankSpaceBelowClose(t *testing.T) {
	for _, scale := range []float32{1, 2} {
		t.Run(fmt.Sprintf("缩放%.0f", scale), func(t *testing.T) {
			var height int
			for _, windowHeight := range []int{600, 1000} {
				v := viewer{}
				v.menu.about = true
				var ops op.Ops
				var router input.Router
				size := image.Pt(int(800*scale), int(float32(windowHeight)*scale))
				gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(size),
					Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}}
				v.handleMenu(gtx, func() {})
				v.layoutMenuOverlay(gtx, continuousTestTheme())
				router.Frame(&ops)
				card, ok := navigationSemanticBounds(&router, "关于 OFD Viewer")
				if !ok || card.Empty() || !card.In(image.Rectangle{Max: size}) {
					t.Fatalf("关于卡片未显示或超出窗口: %v", card)
				}
				closeButton, ok := navigationSemanticBounds(&router, "关闭关于")
				if !ok || closeButton.Empty() || !closeButton.In(card) {
					t.Fatalf("缺少可见关闭按钮: card=%v close=%v", card, closeButton)
				}
				bottomGap := card.Max.Y - closeButton.Max.Y
				if bottomGap < gtx.Dp(unit.Dp(14)) || bottomGap > gtx.Dp(unit.Dp(18)) {
					t.Fatalf("关闭按钮下方只能保留正常内边距: gap=%d card=%v close=%v", bottomGap, card, closeButton)
				}
				if card.Dy() >= gtx.Dp(unit.Dp(420)) {
					t.Fatalf("普通字号下不应维持过高的固定卡片: %v", card)
				}
				if height != 0 && card.Dy() != height {
					t.Fatalf("大窗口不能扩大内容之外的留白: before=%d after=%d", height, card.Dy())
				}
				height = card.Dy()
				if card.Min.Y != (size.Y-card.Dy())/2 {
					t.Fatalf("卡片未按实际高度居中: %v", card)
				}
			}
		})
	}
}

func TestAboutSmallWindowAndLargeTextKeepCloseVisibleWhileContentScrolls(t *testing.T) {
	for _, pixelsPerSp := range []float32{1, 2} {
		t.Run(fmt.Sprintf("字号%.0f", pixelsPerSp), func(t *testing.T) {
			v := viewer{}
			v.menu.about = true
			var ops op.Ops
			var router input.Router
			size := image.Pt(320, 260)
			gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(size),
				Metric: unit.Metric{PxPerDp: 1, PxPerSp: pixelsPerSp}}
			th := continuousTestTheme()
			var opened string
			v.openURL = func(uri string) { opened = uri }
			frame := func() {
				ops.Reset()
				v.handleMenu(gtx, func() { t.Fatal("不能退出阅读器") })
				v.layoutMenuOverlay(gtx, th)
				router.Frame(&ops)
			}
			frame()
			card, ok := navigationSemanticBounds(&router, "关于 OFD Viewer")
			if !ok || !card.In(image.Rectangle{Max: size}) {
				t.Fatalf("小窗口卡片越界: %v", card)
			}
			before, ok := navigationSemanticBounds(&router, "关闭关于")
			if !ok || before.Empty() || !before.In(card) {
				t.Fatalf("小窗口关闭按钮必须始终可见: %v", before)
			}
			router.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse,
				Position: f32.Pt(float32(card.Min.X+40), float32(card.Min.Y+40)), Scroll: f32.Pt(0, 2000)})
			frame()
			frame()
			after, ok := navigationSemanticBounds(&router, "关闭关于")
			if !ok || after != before || v.menu.aboutList.Position.First == 0 {
				t.Fatalf("内容应滚动，关闭按钮不能跟着滚走: before=%v after=%v position=%+v", before, after, v.menu.aboutList.Position)
			}
			link, ok := navigationSemanticBounds(&router, viewerProjectURL)
			if !ok || link.Empty() || !link.In(card) {
				t.Fatalf("滚动后应可访问项目链接: %v", link)
			}
			point := f32.Pt(float32(link.Min.X+link.Dx()/2), float32(link.Min.Y+link.Dy()/2))
			router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: point})
			router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: point})
			frame()
			if opened != viewerProjectURL || !v.menu.about {
				t.Fatalf("重放布局后项目链接不可点击: opened=%q about=%v", opened, v.menu.about)
			}
			point = f32.Pt(float32(after.Min.X+after.Dx()/2), float32(after.Min.Y+after.Dy()/2))
			router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: point})
			router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: point})
			frame()
			if v.menu.about {
				t.Fatal("固定底部关闭按钮未关闭对话框")
			}
		})
	}
}

func TestAboutDialogGPUDrawsCompactRoundedCard(t *testing.T) {
	window, err := headless.NewWindow(800, 600)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	v := viewer{}
	v.menu.about = true
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(800, 600)),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.handleMenu(gtx, func() {})
	v.layout(gtx, continuousTestTheme())
	router.Frame(&ops)
	card, ok := navigationSemanticBounds(&router, "关于 OFD Viewer")
	if !ok {
		t.Fatal("缺少关于卡片")
	}
	closeButton, ok := navigationSemanticBounds(&router, "关闭关于")
	if !ok {
		t.Fatal("缺少关闭按钮")
	}
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 800, 600))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	bottom := pixels.RGBAAt((card.Min.X+card.Max.X)/2, closeButton.Max.Y+4)
	border := pixels.RGBAAt(card.Min.X+card.Dx()/2, card.Max.Y-1)
	corner := pixels.RGBAAt(card.Min.X, card.Min.Y)
	inside := pixels.RGBAAt(card.Min.X+12, card.Min.Y+12)
	if bottom.R < 240 || border.R > 210 || corner.R >= inside.R || inside.R < 240 {
		t.Fatalf("卡片内边距、细边框或圆角绘制错误: bottom=%v border=%v corner=%v inside=%v", bottom, border, corner, inside)
	}
}
