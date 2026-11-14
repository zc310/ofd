// Command ofd-viewer-gio 提供用于验证 canvas/Gio 矢量重放的实验性桌面阅读器。
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/zc310/ofd/internal/version"
)

func parseArgs(args []string, stdout, stderr io.Writer) (string, bool, int) {
	flags := flag.NewFlagSet("ofd-viewer-gio", flag.ContinueOnError)
	var diagnostics bytes.Buffer
	flags.SetOutput(&diagnostics)
	var showVersion bool
	flags.BoolVar(&showVersion, "version", false, "显示版本")
	flags.BoolVar(&showVersion, "v", false, "显示版本")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(flags.Output(), "用法: ofd-viewer-gio [--version|-v] [文件.ofd]\n实验性 Gio 矢量阅读器；O 打开，方向键翻页，Home/End 首末页，G 输入页码，Esc 退出。\n支持单页和连续阅读；Ctrl＋滚轮缩放，左键拖动或滚轮平移。")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.Copy(stdout, &diagnostics)
			return "", true, 0
		}
		_, _ = io.Copy(stderr, &diagnostics)
		return "", true, 2
	}
	if flags.NArg() > 1 {
		_, _ = fmt.Fprintln(stderr, "只能指定一个 OFD 文件")
		return "", true, 2
	}
	if showVersion {
		_, _ = fmt.Fprintln(stdout, version.Version)
		return "", true, 0
	}
	return flags.Arg(0), false, 0
}

func main() {
	filename, stop, code := parseArgs(os.Args[1:], os.Stdout, os.Stderr)
	if stop {
		os.Exit(code)
	}
	go func() {
		window := new(app.Window)
		th := material.NewTheme()
		if err := configureViewerFont(th); err != nil {
			slog.Error("加载默认字体失败", "error", err)
		}
		width, height := defaultWindowSize(th)
		window.Option(app.Title(initialWindowTitle), app.Size(width, height))
		if err := run(window, filename, th); err != nil {
			slog.Error("Gio 阅读器退出", "error", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

type viewer struct {
	open, previous, next                           widget.Clickable
	fitPageButton, fitWidthButton, zoomIn, zoomOut widget.Clickable
	jumpButton                                     widget.Clickable
	thumbnailButton                                widget.Clickable
	continuousButton                               widget.Clickable
	doublePageButton                               widget.Clickable
	compactToolbar                                 bool
	background, backgroundCustom                   string
	thumbnailPanel                                 struct{}
	pageControlsButton                             widget.Clickable
	hidePageControls                               bool
	continuous                                     continuousView
	menu                                           viewerMenu
	thumbnails                                     thumbnailState
	pendingOpen                                    bool
	recentButton, clearRecent                      widget.Clickable
	recentEntries                                  [maxRecentFiles]widget.Clickable
	recentList                                     widget.List
	toolbarList                                    widget.List
	toolbarIcons                                   toolbarIcons
	titlebar                                       viewerTitlebar
	recentFiles                                    []string
	showRecent                                     bool
	pageInput                                      widget.Editor
	viewport                                       pageViewport
	pageLinks                                      pageLinkPointer
	openURL                                        func(string)
	linkJump                                       *linkNavigation
	linkDestination                                *linkNavigation
	recording                                      sceneRecording
	viewScene                                      pageScene
	scene                                          pageScene
	sceneLease                                     *continuousLease
	page, total                                    int
	busy                                           bool
	exporting                                      bool
	status                                         string
	scrollOverflow                                 float32
	scrollPage, scrollLanding, landOnPage          int
}

func run(window *app.Window, filename string, th *material.Theme) error { //nolint:maintidx // Gio 主事件循环集中协调各后台会话。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fontResults, fontReady := startViewerFont(ctx, window.Invalidate)
	requests, results := make(chan request, 1), make(chan result, 1)
	picker := newViewerPicker(window)
	go runWorkerWithPickerReady(ctx, requests, results, window.Invalidate, picker.choose, fontReady)
	thumbnailRequests, thumbnailResults := make(chan thumbnailRequest, 1), make(chan thumbnailResult, 1)
	exportResults := make(chan exportResult, 1)
	linkResults := make(chan error, 1)
	go runThumbnailWorker(ctx, thumbnailRequests, thumbnailResults, window.Invalidate)
	v := &viewer{pageInput: widget.Editor{SingleLine: true, Submit: true, InputHint: key.HintNumeric}}
	v.openURL = func(uri string) { launchWebURL(ctx, uri, linkResults, window.Invalidate) }
	v.titlebar.title = initialWindowTitle
	defer func() { v.sceneLease.release() }()
	settings, settingsPath := loadViewerSettings()
	v.recentFiles = slices.Clone(settings.Recent)
	v.thumbnails.open = settings.Thumbnails
	v.continuous.enabled = settings.Continuous
	v.continuous.double = settings.DoublePage && settings.Continuous
	v.hidePageControls = settings.HidePageControls
	v.background, v.backgroundCustom = settings.Background, settings.BackgroundCustom
	if settings.Fit == "width" {
		v.viewport.mode = fitWidth
	}
	persist := func() {
		if settingsPath == "" {
			return
		}
		settings.Recent = slices.Clone(v.recentFiles)
		if err := saveSettings(settingsPath, settings); err != nil {
			slog.Warn("保存阅读器偏好失败", "error", err)
			v.status = "保存阅读偏好失败：" + err.Error()
		}
	}
	submit := func(req request) {
		if v.busy {
			return
		}
		if v.linkDestination != nil && !req.metadata && req.pages == nil && req.page != v.linkDestination.target.Page {
			v.linkDestination = nil
		}
		if v.continuous.enabled && v.continuous.metadata != nil && !req.choose && req.filename == "" && req.pages == nil && !req.metadata {
			if req.page >= 0 && req.page < v.total {
				v.continuous.jump(req.page)
				v.page = req.page
				v.pageInput.SetText(strconv.Itoa(req.page + 1))
				v.thumbnails.reveal(req.page)
				window.Invalidate()
			}
			return
		}
		v.busy, v.scene, v.status = true, nil, "正在加载…"
		v.pendingOpen = req.choose || req.filename != ""
		if v.pendingOpen {
			v.linkDestination = nil
			v.linkJump = nil
		}
		v.scrollOverflow, v.scrollPage = 0, 0
		v.recording.clear()
		v.viewport.dragging = false
		v.pageLinks = pageLinkPointer{}
		requests <- req
	}
	if filename != "" {
		submit(request{filename: filename})
	}
	setTitle := func(title string) {
		v.titlebar.title = title
		window.Option(app.Title(title))
	}
	var ops op.Ops
	for {
		event := window.Event()
		picker.listen(event)
		switch e := event.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.ViewEvent:
			if isWaylandView(e) && !v.titlebar.wayland {
				v.titlebar.wayland = true
				window.Option(app.Decorated(false))
			}
		case app.ConfigEvent:
			v.titlebar.configure(e.Config)
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			select {
			case err := <-linkResults:
				v.status = "打开网址失败：" + err.Error()
				slog.Warn("打开网址失败", "error", err)
			default:
			}
			select {
			case font := <-fontResults:
				if font.err != nil {
					slog.Warn("加载默认字体失败", "error", font.err)
					v.status = "默认字体加载失败，刷新页面可重试"
				} else {
					applyViewerFont(th, font.faces)
				}
			default:
			}
			select {
			case result := <-exportResults:
				v.exporting = false
				switch {
				case result.err != nil:
					v.status = "导出失败：" + result.err.Error()
				case result.path != "":
					v.status = "导出完成：" + result.path
				default:
					v.status = ""
				}
			default:
			}
			select {
			case response := <-results:
				v.acceptWorkerResponse(response, persist, setTitle)
				if response.lockedFilename != "" {
					v.menu.showUnlock(response.lockedFilename, response.err)
				}
			default:
			}
			select {
			case thumbnail := <-thumbnailResults:
				v.thumbnails.accept(thumbnail)
				if thumbnail.err != nil && thumbnail.generation == v.thumbnails.generation {
					slog.Warn("生成缩略图失败", "page", thumbnail.page+1, "error", thumbnail.err)
				}
			default:
			}
			v.processLinkJump(submit)
			if v.scrollPage != 0 && !v.busy {
				direction := v.scrollPage
				v.scrollPage = 0
				if target := v.page + direction; target >= 0 && target < v.total {
					v.scrollLanding = direction
					submit(request{page: target})
				}
			}
			closeWindow := func() { window.Perform(system.ActionClose) }
			v.titlebar.handle(gtx, window.Option, window.Perform)
			v.handleMenu(gtx, closeWindow)
			if v.menu.unlockOpen {
				v.menu.unlockMethod.Update(gtx)
				if v.menu.unlockCancel.Clicked(gtx) {
					v.menu.clearUnlock()
					v.menu.open = false
					gtx.Execute(key.FocusCmd{})
				}
				if v.menu.unlockOpen && v.menu.unlockApply.Clicked(gtx) {
					m := &v.menu
					password := make([]byte, len(m.unlockPassword.Text()))
					copy(password, m.unlockPassword.Text())
					credentials := &unlockCredentials{method: m.unlockMethod.Value, user: m.unlockUser.Text(), path: m.unlockPath.Text(), password: password}
					filename := m.unlockFilename
					m.clearUnlock()
					m.open = false
					gtx.Execute(key.FocusCmd{})
					submit(request{filename: filename, credentials: credentials})
				}
			}
			if v.menu.exportOpen && !v.exporting && !v.busy && v.scene != nil {
				v.menu.exportFormat.Update(gtx)
				v.menu.exportBackground.Update(gtx)
				if v.menu.exportApply.Clicked(gtx) {
					opts, err := v.menu.exportOptions()
					if err != nil {
						v.menu.exportError = err.Error()
					} else {
						source, pages := v.thumbnails.filename, v.total
						var session *continuousSession
						if v.sceneLease != nil {
							session = v.sceneLease.session
						}
						v.menu.open, v.menu.exportOpen, v.exporting = false, false, true
						gtx.Execute(key.FocusCmd{})
						v.status = "正在导出…"
						go func() {
							path, err := chooseExportFile(source, opts.format, pages)
							if err == nil && path != "" {
								if session != nil && session.view != nil {
									lease := session.acquire()
									if lease == nil {
										err = errNoDocument
									} else {
										err = exportFileInput(ctx, source, path, opts, session.view)
										lease.release()
									}
								} else {
									err = exportFile(ctx, source, path, opts)
								}
							}
							select {
							case exportResults <- exportResult{path, err}:
								window.Invalidate()
							case <-ctx.Done():
							}
						}()
					}
				}
			}
			wasHidden := v.hidePageControls
			v.handleKeys(gtx, submit, closeWindow)
			if wasHidden != v.hidePageControls {
				settings.HidePageControls = v.hidePageControls
				persist()
			}
			v.handlePageInput(gtx, submit)
			v.handleViewMenu(gtx, &settings, persist, submit)
			if v.thumbnailButton.Clicked(gtx) && !v.busy && v.scene != nil {
				v.thumbnails.open = !v.thumbnails.open
				settings.Thumbnails = v.thumbnails.open
				persist()
				if v.thumbnails.open {
					v.thumbnails.reveal(v.page)
				}
				if !v.thumbnails.open {
					v.thumbnails.buttons = nil
				}
			}
			if v.thumbnails.open {
				v.thumbnails.handleClicks(gtx, v.page, v.busy, submit)
			}
			v.handleRecent(gtx, persist, submit)
			v.handleThumbnailPanel(gtx)
			if v.open.Clicked(gtx) {
				submit(request{choose: true})
			}
			if v.previous.Clicked(gtx) && v.page > 0 {
				submit(request{page: v.page - 1})
			}
			if v.next.Clicked(gtx) && v.page+1 < v.total {
				submit(request{page: v.page + 1})
			}
			v.handleScaleButtons(gtx, &settings, persist)
			previousPage := v.page
			v.layout(gtx, th)
			v.updateScrollTitle(previousPage, setTitle)
			e.Frame(gtx.Ops)
			v.scheduleContinuous(submit)
			if v.thumbnails.open && !v.busy && !v.thumbnails.busy && v.thumbnails.filename != "" && v.thumbnails.next >= 0 {
				var session *continuousSession
				if v.sceneLease != nil {
					session = v.sceneLease.session
				}
				thumbnailRequests <- thumbnailRequest{filename: v.thumbnails.filename, generation: v.thumbnails.generation, page: v.thumbnails.next, session: session}
				v.thumbnails.busy = true
			}
		}
	}
}

func (v *viewer) handleThumbnailPanel(gtx layout.Context) {
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &v.thumbnailPanel, Kinds: pointer.Press | pointer.Release | pointer.Cancel}); !ok {
			return
		}
	}
}

func (v *viewer) updateScrollTitle(previousPage int, setTitle func(string)) {
	if v.page != previousPage && v.thumbnails.filename != "" {
		setTitle(fmt.Sprintf("%s - %d/%d - OFD 阅读器 Gio", filepath.Base(v.thumbnails.filename), v.page+1, v.total))
	}
}

func loadViewerSettings() (viewerSettings, string) {
	defaults := viewerSettings{Fit: "page"}
	dir, err := os.UserConfigDir()
	if err != nil {
		slog.Warn("无法确定阅读器偏好目录", "error", err)
		return defaults, ""
	}
	path := filepath.Join(dir, "ofd-viewer-gio", "settings.json")
	settings, err := loadSettings(path)
	if err != nil {
		slog.Warn("无法读取阅读器偏好，本次不写入", "error", err)
		return defaults, ""
	}
	return settings, path
}

func (v *viewer) handleRecent(gtx layout.Context, persist func(), submit func(request)) {
	if v.recentButton.Clicked(gtx) && !v.busy {
		v.showRecent = !v.showRecent
	}
	if v.clearRecent.Clicked(gtx) && !v.busy {
		v.recentFiles = nil
		v.showRecent = false
		persist()
	}
	for i := range v.recentFiles {
		if v.recentEntries[i].Clicked(gtx) && !v.busy {
			v.showRecent = false
			submit(request{filename: v.recentFiles[i]})
		}
	}
}

func (v *viewer) acceptWorkerResponse(response result, persist func(), setTitle func(string)) {
	if v.continuous.pending == 0 {
		v.acceptResponse(response, persist, setTitle)
		return
	}
	v.busy, v.scene = false, response.scene
	v.continuous.accept(response)
	v.status = ""
	if response.err != nil {
		v.status = response.err.Error()
		v.linkDestination = nil
	}
}

func (v *viewer) acceptResponse(response result, persist func(), setTitle func(string)) {
	var lease *continuousLease
	if response.session != nil {
		lease = response.session.acquire()
		if lease == nil {
			response.scene = nil
			response.err = errNoDocument
		}
	}
	v.sceneLease.release()
	v.sceneLease = lease
	v.busy, v.scene = false, response.scene
	previousPage := v.page
	if v.pendingOpen && response.err == nil && response.scene != v.viewScene && response.filename != "" {
		v.thumbnails.reset(response.filename)
		v.continuous.reset(response.page)
	}
	v.pendingOpen = false
	landing := v.scrollLanding
	v.scrollLanding = 0
	if response.scene != v.viewScene {
		if v.viewport.mode == fitManual && landing == 0 {
			v.viewport.mode = fitWhole
		}
		if v.viewport.mode != fitManual {
			v.viewport.fit(v.viewport.mode)
		}
		if response.err == nil {
			v.landOnPage = landing
		}
		v.viewScene = response.scene
	}
	v.page, v.total, v.status = response.page, response.total, ""
	if v.linkDestination != nil {
		if response.err != nil || response.page != v.linkDestination.target.Page {
			v.linkDestination = nil
		} else {
			v.linkDestination.ready = true
		}
	}
	if v.page != previousPage && response.err == nil {
		v.thumbnails.reveal(v.page)
	}
	if v.total > 0 {
		v.pageInput.SetText(strconv.Itoa(v.page + 1))
	} else {
		v.pageInput.SetText("")
	}
	if response.err != nil {
		v.status = response.err.Error()
		slog.Error("打开或绘制失败", "error", response.err)
	}
	if response.err == nil && response.filename != "" && !response.transient {
		recent := rememberRecent(v.recentFiles, response.filename)
		if !slices.Equal(recent, v.recentFiles) {
			v.recentFiles = recent
			persist()
		}
	}
	if response.filename != "" {
		setTitle(fmt.Sprintf("%s - %d/%d - OFD 阅读器 Gio", filepath.Base(response.filename), v.page+1, v.total))
	}
}

func (v *viewer) handlePageInput(gtx layout.Context, submit func(request)) {
	if !v.busy && v.total > 0 {
		for {
			event, ok := v.pageInput.Update(gtx)
			if !ok {
				break
			}
			if event, ok := event.(widget.SubmitEvent); ok && v.jumpToPage(event.Text, submit) {
				gtx.Execute(key.FocusCmd{})
			}
		}
	}
	if v.jumpButton.Clicked(gtx) && v.jumpToPage(v.pageInput.Text(), submit) {
		gtx.Execute(key.FocusCmd{})
	}
}

func (v *viewer) handleKeys(gtx layout.Context, submit func(request), closeWindow func()) {
	if v.menu.open || v.menu.about {
		return
	}
	// 页码输入获得焦点时，方向键和字母交给编辑器，避免误触翻页或退出。
	if gtx.Focused(&v.pageInput) {
		if ev, ok := gtx.Event(key.Filter{Focus: &v.pageInput, Name: key.NameEscape}); ok {
			if event, ok := ev.(key.Event); ok && event.State == key.Press {
				v.pageInput.SetText(strconv.Itoa(v.page + 1))
				v.status = ""
				gtx.Execute(key.FocusCmd{})
			}
		}
		return
	}
	for {
		event, ok := gtx.Event(key.Filter{Optional: key.ModShift})
		if !ok {
			break
		}
		if ev, ok := event.(key.Event); ok && ev.State == key.Press {
			switch ev.Name { //nolint:exhaustive // 阅读器仅响应已定义的快捷键，其余按键忽略。
			case "O":
				submit(request{choose: true})
			case key.NameLeftArrow, key.NameUpArrow, key.NamePageUp, "A":
				if v.page > 0 {
					submit(request{page: v.page - 1})
				}
			case key.NameRightArrow, key.NameDownArrow, key.NamePageDown, "D":
				if v.page+1 < v.total {
					submit(request{page: v.page + 1})
				}
			case key.NameHome:
				if v.total > 0 {
					submit(request{page: 0})
				}
			case key.NameEnd:
				if v.total > 0 {
					submit(request{page: v.total - 1})
				}
			case "G":
				if !v.busy && v.total > 0 {
					v.hidePageControls = false
					v.pageInput.SetCaret(0, v.pageInput.Len())
					gtx.Execute(key.FocusCmd{Tag: &v.pageInput})
					return
				}
			case key.NameEscape, "Q":
				closeWindow()
			default:
				// 其他按键不改变阅读状态。
			}
		}
	}
}

// jumpToPage 校验从 1 开始的全局页码；非法输入不发送请求或改变当前页面。
func (v *viewer) jumpToPage(text string, submit func(request)) bool {
	if v.busy || v.total <= 0 {
		return false
	}
	text = strings.TrimSpace(text)
	for _, c := range text {
		if c < '0' || c > '9' {
			v.status = fmt.Sprintf("请输入 1 至 %d 的页码", v.total)
			return false
		}
	}
	page, err := strconv.Atoi(text)
	if err != nil || page < 1 || page > v.total {
		v.status = fmt.Sprintf("请输入 1 至 %d 的页码", v.total)
		return false
	}
	v.status = ""
	v.pageInput.SetText(strconv.Itoa(page))
	if page-1 != v.page {
		submit(request{page: page - 1})
	}
	return true
}

func (v *viewer) layout(gtx layout.Context, th *material.Theme) {
	v.thumbnails.paper = paperColor(v.background, v.backgroundCustom)
	paint.Fill(gtx.Ops, color.NRGBA{R: 225, G: 228, B: 232, A: 255})
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return v.titlebar.layout(gtx, th) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return v.layoutHeader(gtx, th) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showRecent {
				return layout.Dimensions{}
			}
			if v.busy {
				gtx = gtx.Disabled()
			}
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = 0
				gtx.Constraints.Max.Y = min(gtx.Constraints.Max.Y, gtx.Dp(unit.Dp(220)))
				v.recentList.Axis = layout.Vertical
				return material.List(th, &v.recentList).Layout(gtx, len(v.recentFiles)+1, func(gtx layout.Context, i int) layout.Dimensions {
					if i < len(v.recentFiles) {
						return material.Button(th, &v.recentEntries[i], v.recentFiles[i]).Layout(gtx)
					}
					if len(v.recentFiles) == 0 {
						return material.Caption(th, "没有最近打开的文件").Layout(gtx)
					}
					return material.Button(th, &v.clearRecent, "清除最近文件").Layout(gtx)
				})
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return v.layoutStatus(gtx, th) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{Alignment: layout.NW}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !v.thumbnails.open || v.total == 0 || v.compactToolbar {
								return layout.Dimensions{}
							}
							width := min(gtx.Dp(unit.Dp(170)), gtx.Constraints.Max.X/3)
							gtx.Constraints.Min.X, gtx.Constraints.Max.X = width, width
							return v.thumbnails.layout(gtx, th, v.total, v.page, v.busy)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Stack{Alignment: layout.S}.Layout(gtx,
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									return layout.UniformInset(readingMargin).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										if v.continuous.enabled && v.continuous.metadata != nil {
											return v.layoutContinuous(gtx, th)
										}
										if v.scene == nil {
											return layout.Center.Layout(gtx, material.H6(th, "打开 OFD 文件开始阅读").Layout)
										}
										if v.continuous.enabled {
											return v.layoutContinuous(gtx, th)
										}
										return v.layoutViewport(gtx)
									})
								}),
								layout.Stacked(func(gtx layout.Context) layout.Dimensions {
									if v.hidePageControls {
										return layout.Dimensions{}
									}
									return v.layoutPageControls(gtx, th)
								}))
						}))
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					if !v.compactToolbar || !v.thumbnails.open || v.total == 0 {
						return layout.Dimensions{}
					}
					width := min(gtx.Dp(unit.Dp(220)), gtx.Constraints.Max.X*2/3)
					gtx.Constraints = layout.Exact(image.Pt(width, gtx.Constraints.Max.Y))
					defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
					event.Op(gtx.Ops, &v.thumbnailPanel)
					paint.Fill(gtx.Ops, color.NRGBA{R: 235, G: 237, B: 240, A: 255})
					return v.thumbnails.layout(gtx, th, v.total, v.page, v.busy)
				}))
		}))
	v.layoutMenuOverlay(gtx, th)
}
