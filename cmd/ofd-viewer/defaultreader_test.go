package main

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

// TestShouldPromptDefaultReader 保护启动提示的触发条件。
//
// 误弹和多弹的代价不对称：已经设成默认还问一遍，用户会觉得应用没记住；探测不出
// 结果还弹，用户点下去只会看到失败。因此三种"不提示"都必须守住。
func TestShouldPromptDefaultReader(t *testing.T) {
	cases := []struct {
		name      string
		state     defaultReaderState
		dismissed bool
		want      bool
	}{
		{name: "不是默认阅读器且未记住", state: defaultReaderNotDefault, want: true},
		{name: "用户选择不再提示", state: defaultReaderNotDefault, dismissed: true, want: false},
		{name: "已经是默认阅读器", state: defaultReaderIsDefault, want: false},
		{name: "探测不出结果", state: defaultReaderUnknown, want: false},
		{name: "平台不支持", state: defaultReaderUnavailable, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPromptDefaultReader(tc.state, tc.dismissed); got != tc.want {
				t.Errorf("shouldPromptDefaultReader(%v, %v) = %v, 期望 %v",
					tc.state, tc.dismissed, got, tc.want)
			}
		})
	}
}

// TestDefaultReaderMenuItems 保护菜单项在各状态下的形态。
//
// 平台不支持时必须整项消失而不是留一个点不动的禁用项：禁用项在位置上还占着，
// 用户既不知道该去哪里改，也容易以为菜单坏了。已经是默认阅读器时打勾但仍可点
// 击，同理。
func TestDefaultReaderMenuItems(t *testing.T) {
	cases := []struct {
		name        string
		state       defaultReaderState
		wantCount   int
		wantChecked bool
	}{
		{name: "不是默认阅读器", state: defaultReaderNotDefault, wantCount: 1},
		{name: "探测不出结果", state: defaultReaderUnknown, wantCount: 1},
		{name: "已是默认阅读器", state: defaultReaderIsDefault, wantCount: 1, wantChecked: true},
		{name: "平台不支持时不显示", state: defaultReaderUnavailable, wantCount: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := defaultReaderMenuItems(tc.state, func() {})
			if len(items) != tc.wantCount {
				t.Fatalf("菜单项数量 = %d, 期望 %d", len(items), tc.wantCount)
			}
			if tc.wantCount == 0 {
				return
			}
			item := items[0]
			if item.Label != defaultReaderMenuLabel {
				t.Errorf("Label = %q, 期望 %q", item.Label, defaultReaderMenuLabel)
			}
			if item.Disabled {
				t.Error("菜单项不应被禁用")
			}
			if item.Checked != tc.wantChecked {
				t.Errorf("Checked = %v, 期望 %v", item.Checked, tc.wantChecked)
			}
		})
	}
}

// TestDefaultReaderMenuItemsKeepsCallback 菜单项必须真的接上回调，否则点了没有
// 反应，而禁用与否都不会暴露这个问题。
func TestDefaultReaderMenuItemsKeepsCallback(t *testing.T) {
	called := false
	items := defaultReaderMenuItems(defaultReaderNotDefault, func() { called = true })
	if len(items) != 1 {
		t.Fatalf("菜单项数量 = %d, 期望 1", len(items))
	}
	items[0].Action()
	if !called {
		t.Error("点击菜单项没有调用回调")
	}
}

// TestResolveDefaultReaderPrompt 保护两个开关可以同时成立。
//
// 用户既点"设为默认"又勾了"不再提示"时两件事都要做：漏掉任何一件，要么提示再次
// 出现，要么菜单承诺的关联根本没建立。
func TestResolveDefaultReaderPrompt(t *testing.T) {
	cases := []struct {
		name         string
		confirmed    bool
		neverAgain   bool
		wantSet      bool
		wantRemember bool
	}{
		{name: "直接关闭", confirmed: false, neverAgain: false},
		{name: "关闭但不再提示", confirmed: false, neverAgain: true, wantRemember: true},
		{name: "确认设置", confirmed: true, neverAgain: false, wantSet: true},
		{name: "确认设置且不再提示", confirmed: true, neverAgain: true, wantSet: true, wantRemember: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDefaultReaderPrompt(tc.confirmed, tc.neverAgain)
			if got.set != tc.wantSet || got.remember != tc.wantRemember {
				t.Errorf("resolveDefaultReaderPrompt(%v, %v) = %+v, 期望 set=%v remember=%v",
					tc.confirmed, tc.neverAgain, got, tc.wantSet, tc.wantRemember)
			}
		})
	}
}

// TestShowDefaultReaderPromptSkippedWhenClosed 窗口已经关闭时不能再弹提示。
//
// 用户关窗和探测完成之间存在时间差，这时候弹出的对话框会挂在一个已经不存在的
// 窗口上，测试驱动下表现为崩溃。
func TestShowDefaultReaderPromptSkippedWhenClosed(t *testing.T) {
	test.NewTempApp(t)
	window := test.NewWindow(nil)
	t.Cleanup(window.Close)
	v := newViewer(window)
	window.SetContent(v.content)

	v.closed.Store(true)
	v.showDefaultReaderPrompt()
	if top := window.Canvas().Overlays().Top(); top != nil {
		t.Error("窗口已关闭时不应弹出提示")
	}
}

// TestRememberDefaultReaderPrompt 保护"记住设置"的读写。
//
// 记住之后 shouldPromptDefaultReader 必须返回 false，否则每次启动还是会弹提示，
// 而提示框本身又只在勾选时调用 rememberDefaultReaderPrompt，两者要一起验证。
func TestRememberDefaultReaderPrompt(t *testing.T) {
	test.NewTempApp(t)

	if defaultReaderPromptDismissed() {
		t.Fatal("初始状态不应是已记住")
	}
	if !shouldPromptDefaultReader(defaultReaderNotDefault, defaultReaderPromptDismissed()) {
		t.Fatal("前置条件失败：未记住时应当提示")
	}
	rememberDefaultReaderPrompt()
	if !defaultReaderPromptDismissed() {
		t.Error("记住之后仍未读到偏好")
	}
	if shouldPromptDefaultReader(defaultReaderNotDefault, defaultReaderPromptDismissed()) {
		t.Error("记住之后仍然提示")
	}
}
