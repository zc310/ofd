package main

import (
	"image/color"
	"slices"
	"strconv"

	"gioui.org/io/key"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type navigationTab uint8

const (
	navigationOutlines navigationTab = iota
	navigationBookmarks
)

type navigationRow struct {
	selectButton widget.Clickable
	toggleButton widget.Clickable
}

type navigationState struct {
	open    bool
	tab     navigationTab
	items   [2][]navigationItem
	visible []int
	lists   [2]widget.List
	tabs    [2]widget.Clickable
	close   widget.Clickable
	rows    map[int]*navigationRow
}

func (s *navigationState) reset(model *documentNavigation) {
	s.items = [2][]navigationItem{}
	if model != nil {
		// 展开状态由界面独占，不能修改 worker 发布的只读切片。
		s.items = [2][]navigationItem{slices.Clone(model.outlines), slices.Clone(model.bookmarks)}
		if len(s.items[s.tab]) == 0 && len(s.items[1-s.tab]) > 0 {
			s.tab = 1 - s.tab
		}
	}
	s.lists = [2]widget.List{}
	s.rows = nil
	s.rebuildVisible()
}

func (s *navigationState) rebuildVisible() {
	s.visible = s.visible[:0]
	items := s.items[s.tab]
	for index := 0; index < len(items); {
		s.visible = append(s.visible, index)
		item := &items[index]
		if s.tab == navigationOutlines && !item.expanded && item.end > index+1 {
			index = item.end
		} else {
			index++
		}
	}
	s.rows = nil
}

func (v *viewer) persistNavigation(settings *viewerSettings, persist func()) {
	settings.Navigation, settings.Thumbnails = v.navigation.open, v.thumbnails.open
	settings.NavigationTab = "outlines"
	if v.navigation.tab == navigationBookmarks {
		settings.NavigationTab = "bookmarks"
	}
	persist()
}

func (v *viewer) handleNavigation(gtx layout.Context, settings *viewerSettings, persist func()) {
	s := &v.navigation
	if v.navigationButton.Clicked(gtx) && !v.busy && v.total > 0 {
		s.open = !s.open
		if s.open {
			v.thumbnails.open, v.thumbnails.buttons = false, nil
			v.showRecent = false
		}
		v.persistNavigation(settings, persist)
	}
	if !s.open {
		return
	}
	if s.close.Clicked(gtx) {
		s.open = false
		v.persistNavigation(settings, persist)
		return
	}
	for tab := range s.tabs {
		if s.tabs[tab].Clicked(gtx) && s.tab != navigationTab(tab) {
			s.tab = navigationTab(tab)
			s.rebuildVisible()
			v.persistNavigation(settings, persist)
			return
		}
	}
	if v.busy {
		return
	}
	items := s.items[s.tab]
	for index, row := range s.rows {
		if index >= len(items) {
			continue
		}
		item := &items[index]
		toggle := row.toggleButton.Clicked(gtx)
		selected := row.selectButton.Clicked(gtx)
		hasChildren := s.tab == navigationOutlines && item.end > index+1
		if hasChildren && (toggle || (selected && item.link.target == nil && item.link.uri == "")) {
			item.expanded = !item.expanded
			s.rebuildVisible()
			gtx.Execute(op.InvalidateCmd{})
			return
		}
		if selected && (item.link.target != nil || item.link.uri != "") {
			v.activatePageLink(item.link)
			gtx.Execute(key.FocusCmd{})
			gtx.Execute(op.InvalidateCmd{})
			if v.compactToolbar {
				s.open = false
				v.persistNavigation(settings, persist)
			}
			return
		}
	}
}

func (v *viewer) sidebarOpen() bool {
	return v.thumbnails.open || v.navigation.open
}

func (v *viewer) sidebarWidth(gtx layout.Context, compact bool) int {
	width := unit.Dp(170)
	if compact {
		width = 220
	}
	if v.navigation.open {
		width = 280
	}
	limit := gtx.Constraints.Max.X / 3
	if compact {
		limit = gtx.Constraints.Max.X * 2 / 3
	}
	return min(gtx.Dp(width), limit)
}

func (v *viewer) layoutSidebar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if v.navigation.open {
		return v.navigation.layout(gtx, th, v.page, v.busy)
	}
	return v.thumbnails.layout(gtx, th, v.total, v.page, v.busy)
}

func (s *navigationState) layout(gtx layout.Context, th *material.Theme, current int, busy bool) layout.Dimensions {
	paint.FillShape(gtx.Ops, color.NRGBA{R: 242, G: 244, B: 247, A: 255}, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, s.tabButton(th, navigationOutlines, "目录")),
				layout.Flexed(1, s.tabButton(th, navigationBookmarks, "书签")),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return material.Clickable(gtx, &s.close, func(gtx layout.Context) layout.Dimensions {
						semantic.DescriptionOp("关闭书签/目录侧栏").Add(gtx.Ops)
						return layout.UniformInset(unit.Dp(10)).Layout(gtx, material.Body1(th, "×").Layout)
					})
				}))
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(s.visible) == 0 {
				label := "文档没有目录"
				if s.tab == navigationBookmarks {
					label = "文档没有书签"
				}
				return layout.Center.Layout(gtx, material.Body2(th, label).Layout)
			}
			if busy {
				gtx = gtx.Disabled()
			}
			list := &s.lists[s.tab]
			list.Axis = layout.Vertical
			rows := make(map[int]*navigationRow)
			dims := material.List(th, list).Layout(gtx, len(s.visible), func(gtx layout.Context, rowIndex int) layout.Dimensions {
				index := s.visible[rowIndex]
				row := s.rows[index]
				if row == nil {
					row = new(navigationRow)
				}
				rows[index] = row
				return s.layoutRow(gtx, th, index, row, current)
			})
			s.rows = rows
			return dims
		}))
}

func (s *navigationState) tabButton(th *material.Theme, tab navigationTab, title string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		button := material.Button(th, &s.tabs[tab], title)
		button.Inset = layout.UniformInset(unit.Dp(10))
		button.CornerRadius = 0
		button.Background = color.NRGBA{R: 233, G: 237, B: 242, A: 255}
		button.Color = th.Fg
		if s.tab == tab {
			button.Background = color.NRGBA{R: 185, G: 208, B: 232, A: 255}
		}
		return button.Layout(gtx)
	}
}

func (s *navigationState) layoutRow(gtx layout.Context, th *material.Theme, index int, row *navigationRow, current int) layout.Dimensions {
	item := &s.items[s.tab][index]
	selected := item.link.target != nil && item.link.target.Page == current
	recording := op.Record(gtx.Ops)
	indent := min(gtx.Dp(unit.Dp(8+12*item.depth)), gtx.Constraints.Max.X/3)
	dims := layout.Inset{Left: gtx.Metric.PxToDp(indent)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if s.tab != navigationOutlines || item.end <= index+1 {
					return layout.Spacer{Width: unit.Dp(24)}.Layout(gtx)
				}
				return material.Clickable(gtx, &row.toggleButton, func(gtx layout.Context) layout.Dimensions {
					label, description := "▸", "展开 "+item.title
					if item.expanded {
						label, description = "▾", "折叠 "+item.title
					}
					semantic.DescriptionOp(description).Add(gtx.Ops)
					return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, material.Body2(th, label).Layout)
				})
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if item.link.target == nil && item.link.uri == "" && item.end <= index+1 {
					gtx = gtx.Disabled()
				}
				return material.Clickable(gtx, &row.selectButton, func(gtx layout.Context) layout.Dimensions {
					semantic.DescriptionOp(item.title).Add(gtx.Ops)
					semantic.SelectedOp(selected).Add(gtx.Ops)
					return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if item.link.target == nil {
									return layout.Dimensions{}
								}
								// 页码固定宽度，长标题不能挤掉全局目标页码。
								width := min(gtx.Dp(unit.Dp(40)), gtx.Constraints.Max.X/3)
								gtx.Constraints.Min.X, gtx.Constraints.Max.X = width, width
								label := material.Caption(th, strconv.Itoa(item.link.target.Page+1))
								label.MaxLines = 1
								return label.Layout(gtx)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								label := material.Body2(th, item.title)
								label.MaxLines = 1
								if !gtx.Enabled() {
									label.Color = color.NRGBA{R: 135, G: 140, B: 147, A: 255}
								}
								return label.Layout(gtx)
							}))
					})
				})
			}))
	})
	call := recording.Stop()
	if selected {
		paint.FillShape(gtx.Ops, color.NRGBA{R: 208, G: 225, B: 244, A: 255}, clip.Rect{Max: dims.Size}.Op())
	}
	call.Add(gtx.Ops)
	return dims
}
