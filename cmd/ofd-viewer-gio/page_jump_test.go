package main

import (
	"image"
	"testing"

	"gioui.org/font/gofont"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func TestJumpToPageValidatesGlobalPageWithoutChangingScene(t *testing.T) {
	for _, tc := range []struct {
		input string
		total int
		busy  bool
		valid bool
		want  int
	}{
		{"1", 5, false, true, 0}, {"5", 5, false, true, 4}, {" 003 ", 5, false, true, 2},
		{"", 5, false, false, -1}, {"0", 5, false, false, -1}, {"6", 5, false, false, -1},
		{"-1", 5, false, false, -1}, {"+1", 5, false, false, -1}, {"1a", 5, false, false, -1},
		{"999999999999999999999999", 5, false, false, -1}, {"1", 0, false, false, -1}, {"1", 5, true, false, -1},
	} {
		t.Run(tc.input, func(t *testing.T) {
			v := viewer{page: 1, total: tc.total, busy: tc.busy}
			var requests []request
			ok := v.jumpToPage(tc.input, func(req request) { requests = append(requests, req) })
			if ok != tc.valid || v.page != 1 {
				t.Fatalf("跳转结果或当前页错误: valid=%v page=%d", ok, v.page)
			}
			if tc.want >= 0 {
				if len(requests) != 1 || requests[0].page != tc.want {
					t.Fatalf("请求错误: %v", requests)
				}
			} else if len(requests) != 0 {
				t.Fatalf("无效输入不应请求页面: %v", requests)
			}
			if !tc.valid && !tc.busy && tc.total > 0 && v.status == "" {
				t.Fatal("无效页码未提示范围")
			}
		})
	}
	v := viewer{page: 1, total: 5, status: "old"}
	if !v.jumpToPage("002", func(request) { t.Fatal("当前页不应重新加载") }) || v.pageInput.Text() != "2" || v.status != "" {
		t.Fatal("当前页码应规范化且清除错误")
	}
}

// 使用真实 Gio 路由检查快捷键、焦点、输入和 Enter，而不仅测试页码解析。
func TestPageJumpKeyboardFocusAndSubmit(t *testing.T) {
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(850, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	v := viewer{total: 5, pageInput: widget.Editor{SingleLine: true, Submit: true, InputHint: key.HintNumeric}}
	v.pageInput.SetText("1")
	var requests []request
	closed := false
	submit := func(req request) { requests = append(requests, req) }
	frame := func() {
		ops.Reset()
		v.handleKeys(gtx, submit, func() { closed = true })
		v.handlePageInput(gtx, submit)
		v.layout(gtx, th)
		router.Frame(&ops)
	}
	frame()
	router.Queue(key.Event{Name: "G", State: key.Press})
	frame()
	if !router.Source().Focused(&v.pageInput) {
		t.Fatal("G 未聚焦页码输入")
	}
	router.Queue(key.Event{Name: key.NameRightArrow, State: key.Press}, key.Event{Name: "Q", State: key.Press})
	frame()
	if closed || len(requests) != 0 {
		t.Fatal("输入时快捷键错误触发翻页或退出")
	}
	router.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "99\n"})
	frame()
	if len(requests) != 0 || v.status == "" || !router.Source().Focused(&v.pageInput) {
		t.Fatal("无效输入应保留焦点并显示错误，不触发翻页")
	}
	router.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 2}, Text: "4\n"})
	frame()
	if len(requests) != 1 || requests[0].page != 3 {
		t.Fatalf("Enter 未提交全局页码: %v，输入=%q", requests, v.pageInput.Text())
	}
	if router.Source().Focused(&v.pageInput) {
		t.Fatal("成功跳转后未释放输入焦点")
	}
}

func TestPageJumpButtonSubmitsInput(t *testing.T) {
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source()}
	v := viewer{page: 0, total: 5, pageInput: widget.Editor{SingleLine: true, Submit: true}}
	v.pageInput.SetText("5")
	v.jumpButton.Click()
	var requests []request
	v.handlePageInput(gtx, func(req request) { requests = append(requests, req) })
	if len(requests) != 1 || requests[0].page != 4 {
		t.Fatalf("Go 按钮没有提交页码: %v", requests)
	}
}

func TestPageJumpShortcutShowsHiddenControlsAndFocusesEditor(t *testing.T) {
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(850, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v := viewer{total: 5, hidePageControls: true, pageInput: widget.Editor{SingleLine: true, Submit: true}}
	v.pageInput.SetText("1")
	frame := func() {
		ops.Reset()
		v.handleKeys(gtx, func(request) {}, func() {})
		v.layout(gtx, continuousTestTheme())
		router.Frame(&ops)
	}
	frame()
	router.Queue(key.Event{Name: "G", State: key.Press})
	frame()
	if v.hidePageControls || !router.Source().Focused(&v.pageInput) {
		t.Fatal("G 应重新显示页码栏并聚焦输入框")
	}
}

func TestPageJumpEscapeCancelsEditing(t *testing.T) {
	var router input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(850, 600)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	v := viewer{page: 1, total: 5, pageInput: widget.Editor{SingleLine: true, Submit: true}}
	v.pageInput.SetText("2")
	closed := false
	frame := func() {
		ops.Reset()
		v.handleKeys(gtx, func(request) { t.Fatal("取消编辑不应翻页") }, func() { closed = true })
		v.handlePageInput(gtx, func(request) { t.Fatal("取消编辑不应翻页") })
		v.layout(gtx, th)
		router.Frame(&ops)
	}
	frame()
	router.Queue(key.Event{Name: "G", State: key.Press})
	frame()
	router.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "99"})
	frame()
	router.Queue(key.Event{Name: key.NameEscape, State: key.Press})
	frame()
	if closed || router.Source().Focused(&v.pageInput) || v.pageInput.Text() != "2" {
		t.Fatalf("Esc 应取消编辑: closed=%v focused=%v text=%q", closed, router.Source().Focused(&v.pageInput), v.pageInput.Text())
	}
}
