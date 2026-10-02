package main

import (
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// defaultReaderState 描述 application/ofd 当前由谁打开。
//
// 分成四态而不是布尔值：探测不出来和"确实不是本应用"必须区分。前者多半是环境
// 缺命令或桌面文件没装好，这种情况下弹提示只会让用户点一个必然失败的按钮。
type defaultReaderState int

const (
	// defaultReaderUnknown 表示探测不出结果：命令缺失、超时或桌面文件缺失。
	defaultReaderUnknown defaultReaderState = iota
	// defaultReaderIsDefault 表示本应用已经是 application/ofd 的默认阅读器。
	defaultReaderIsDefault
	// defaultReaderNotDefault 表示查得到结果，但不是本应用。
	defaultReaderNotDefault
	// defaultReaderUnavailable 表示当前平台或环境既不能探测也不能设置。
	defaultReaderUnavailable
)

const (
	defaultReaderMenuLabel   = "设为默认阅读器"
	defaultReaderPromptTitle = "设为默认阅读器？"
	defaultReaderSetLabel    = "设为默认"
)

// defaultReaderMenuItems 生成右侧菜单里的「设为默认阅读器」项。
//
// 返回空列表表示不显示该项：平台不支持时留一个点不动的禁用项，位置上还占着，
// 用户既不知道系统设置里该去哪里，也可能以为菜单坏了。直接不显示更干净。
//
// 探测与设置都和文档状态无关，因此这一项不受 loading/exporting 影响：正在打开
// 大文档时也该能改关联。
//
// 已经是默认阅读器时保留可点击并打勾，而不是直接禁用：禁用的项用户看不出原因，
// 会以为菜单坏了，点击后明确告知"已经是默认阅读器"更省事。
func defaultReaderMenuItems(state defaultReaderState, setDefault func()) []*fyne.MenuItem {
	if state == defaultReaderUnavailable {
		return nil
	}
	item := fyne.NewMenuItemWithIcon(defaultReaderMenuLabel, theme.SettingsIcon(), setDefault)
	item.Checked = state == defaultReaderIsDefault
	return []*fyne.MenuItem{item}
}

// shouldPromptDefaultReader 决定启动时是否提示设置默认阅读器。
//
// 三条都不提示的情况各有原因：已经设过了没必要问，探测不出来问了也白问，用户
// 明确选过"不再提示"就必须尊重。纯函数，便于直接回归。
func shouldPromptDefaultReader(state defaultReaderState, dismissed bool) bool {
	if dismissed {
		return false
	}
	return state == defaultReaderNotDefault
}

// maybePromptDefaultReader 在启动后按需提示设置默认阅读器。
//
// 带了文件参数启动时不提示：命令行 `ofd-viewer doc.ofd` 是明确用法，弹窗打断
// 没有任何收益。
func maybePromptDefaultReader(v *viewer, launchedWithFile bool) {
	if v == nil || launchedWithFile {
		return
	}
	if !shouldPromptDefaultReader(v.defaultReader, defaultReaderPromptDismissed()) {
		return
	}
	// 走 fyne.Do 而不是直接调用：提示要等事件循环起来才能显示。探测在启动阶段
	// 已经完成，这里只是把结果交给界面。
	go func() {
		fyne.Do(v.showDefaultReaderPrompt)
	}()
}

// defaultReaderPromptOutcome 是提示框关闭后的处置。
type defaultReaderPromptOutcome struct {
	// remember 表示记住"不再提示"。
	remember bool
	// set 表示执行设置默认阅读器。
	set bool
}

// resolveDefaultReaderPrompt 把确认与勾选折叠成处置结果。两个开关可以同时成立：
// 用户既点了"设为默认"又勾了"不再提示"，两件事都要做——只做一件会让提示再次
// 出现，或者让菜单承诺的事没落地。
func resolveDefaultReaderPrompt(confirmed, neverAgain bool) defaultReaderPromptOutcome {
	return defaultReaderPromptOutcome{remember: neverAgain, set: confirmed}
}

// showDefaultReaderPrompt 弹出设置默认阅读器的确认框。
//
// "不再提示"是勾选框而不是独立按钮：绝大多数用户只会点确定或关闭，让他们先找
// 一个额外的按钮来表达"我不想再看到它"会平白增加关掉的路径。关闭和确定都读
// 这个勾选状态，因此中途改主意也算数。
func (v *viewer) showDefaultReaderPrompt() {
	if v.closed.Load() {
		return
	}
	neverAgain := widget.NewCheck("不再提示", nil)
	content := container.NewVBox(
		widget.NewLabel("双击 .ofd 文件时，打开它的可能不是 OFD Viewer。"),
		widget.NewLabel("是否把 OFD Viewer 设为 .ofd 文件的默认阅读器？"),
		neverAgain,
	)
	dialog.NewCustomConfirm(defaultReaderPromptTitle, defaultReaderSetLabel, "关闭", content,
		func(confirmed bool) {
			outcome := resolveDefaultReaderPrompt(confirmed, neverAgain.Checked)
			if outcome.remember {
				rememberDefaultReaderPrompt()
			}
			if outcome.set {
				v.applyDefaultReader()
			}
		}, v.window).Show()
}

// setDefaultReaderFromMenu 响应菜单里的「设为默认阅读器」。
//
// 平台不支持时菜单里没有这一项，这里也不必再判断 Unavailable：状态在启动时确定，
// 之后不会变，留一个走不到的分支只会让人误以为还有别的入口。
func (v *viewer) setDefaultReaderFromMenu() {
	if v.closed.Load() {
		return
	}
	if v.defaultReader == defaultReaderIsDefault {
		dialog.ShowInformation("已经是默认阅读器", "OFD Viewer 已经是 .ofd 文件的默认阅读器。", v.window)
		return
	}
	v.applyDefaultReader()
}

// applyDefaultReader 执行默认阅读器的设置。
//
// 设置要写用户配置并调用外部命令，可能卡上几百毫秒，放后台执行，界面不冻结。
// 记住"不再提示"只发生在成功之后：命令失败时用户多半还会想再试一次，静默记住
// 等于让这个提示再也不会出现。
func (v *viewer) applyDefaultReader() {
	if v.closed.Load() {
		return
	}
	go func() {
		err := setDefaultReader()
		fyne.Do(func() {
			if v.closed.Load() {
				return
			}
			if err != nil {
				slog.Error("设置默认阅读器失败", "error", err)
				dialog.ShowInformation("设置默认阅读器失败", err.Error(), v.window)
				return
			}
			v.defaultReader = defaultReaderIsDefault
			rememberDefaultReaderPrompt()
			dialog.ShowInformation("已设为默认阅读器", "双击 .ofd 文件将使用 OFD Viewer 打开。", v.window)
		})
	}()
}
