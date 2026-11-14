package main

import (
	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const initialWindowTitle = "OFD 阅读器 Gio（实验性）"

// Wayland 不提供服务端装饰时，Gio 默认装饰仅使用 Go 字体，无法显示中文。
// 此时使用应用主题（包含系统字体）的装饰；其他情况保留系统原生标题栏。
type viewerTitlebar struct {
	wayland, custom bool
	title           string
	decorations     widget.Decorations
}

func (t *viewerTitlebar) configure(config app.Config) {
	t.custom = t.wayland && !config.Decorated && config.Mode != app.Fullscreen
	t.decorations.Maximized = config.Mode == app.Maximized
}

func (t *viewerTitlebar) handle(gtx layout.Context, option func(...app.Option), perform func(system.Action)) {
	if !t.custom {
		return
	}
	actions := t.decorations.Update(gtx)
	if actions&system.ActionMinimize != 0 {
		option(app.Minimized.Option())
	}
	if actions&system.ActionMaximize != 0 {
		option(app.Maximized.Option())
	}
	if actions&system.ActionUnmaximize != 0 {
		option(app.Windowed.Option())
	}
	if actions&system.ActionClose != 0 {
		perform(system.ActionClose)
	}
}

func (t *viewerTitlebar) layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if !t.custom {
		return layout.Dimensions{}
	}
	actions := system.ActionMinimize | system.ActionMaximize | system.ActionUnmaximize | system.ActionClose | system.ActionMove
	style := material.Decorations(th, &t.decorations, actions, t.title)
	style.Title.MaxLines = 1
	return style.Layout(gtx)
}
