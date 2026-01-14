package main

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/ncruces/zenity"
	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/gio"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func main() {
	go func() {
		// 检查命令行参数
		var initialFile string
		if len(os.Args) > 1 {
			filename := os.Args[1]
			// 检查文件扩展名是否为.ofd
			if strings.HasSuffix(strings.ToLower(filename), ".ofd") {
				if _, err := os.Stat(filename); err == nil {
					initialFile = filename
				}
			}
		}

		w := new(app.Window)
		w.Option(app.Title("OFD Viewer"))
		w.Option(app.Size(800, 600))

		if err := run(w, initialFile); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(window *app.Window, initialFile string) error {
	var ops op.Ops
	theme := material.NewTheme()

	var openBtn widget.Clickable

	filePath := initialFile
	isLoading := false
	var ofd *parser.OFD
	var doc *render.Document
	currentPage := 0 // 当前页码，从0开始
	totalPages := 0  // 总页数

	defer func() {
		if ofd != nil {
			_ = ofd.Close()
		}
	}()

	// 如果初始文件存在，则自动加载
	if initialFile != "" {
		isLoading = true
		go func() {
			filePath = initialFile
			log.Printf("正在打开文件: %s", filePath)

			var err error
			ofd, err = parser.NewOFD(filePath)
			if err != nil {
				_ = zenity.Error(err.Error())
			} else {
				doc = render.NewDocument(color.Transparent, ofd.Documents[0])
				currentPage = 0
				totalPages = len(doc.Pages)
				// 更新窗口标题
				updateWindowTitle(window, filePath, currentPage+1, totalPages)
			}

			isLoading = false
			window.Invalidate()
		}()
	}

	// 键盘事件处理函数
	handleKeyEvent := func(e event.Event) {
		if e.(key.Event).State == key.Press {
			switch e.(key.Event).Name {
			case "O", "o": // O键打开新文件
				if !isLoading {
					isLoading = true
					go func() {
						selectedFile, err := zenity.SelectFile(
							zenity.Title("选择OFD文件"),
							zenity.FileFilter{
								Name:     "OFD文件",
								Patterns: []string{"*.ofd"},
								CaseFold: false,
							},
						)

						window.Invalidate()

						if err != nil {
							if err != zenity.ErrCanceled {
								log.Printf("文件选择错误: %v", err)
							}
						} else if selectedFile != filePath {
							// 关闭当前文件
							if ofd != nil {
								_ = ofd.Close()
								ofd = nil
							}
							doc = nil

							filePath = selectedFile
							log.Printf("已选择文件: %s", filePath)

							ofd, err = parser.NewOFD(filePath)
							if err != nil {
								_ = zenity.Error(err.Error())
							} else {
								doc = render.NewDocument(color.Transparent, ofd.Documents[0])
								currentPage = 0
								totalPages = len(doc.Pages)
								// 更新窗口标题
								updateWindowTitle(window, filePath, currentPage+1, totalPages)
							}
						}

						isLoading = false
						window.Invalidate()
					}()
				}
			case key.NameLeftArrow, "A", "a": // 左箭头键或A键：上一页
				if doc != nil && totalPages > 0 && currentPage > 0 {
					currentPage--
					updateWindowTitle(window, filePath, currentPage+1, totalPages)
					window.Invalidate()
				}
			case key.NameRightArrow, "D", "d": // 右箭头键或D键：下一页
				if doc != nil && totalPages > 0 && currentPage < totalPages-1 {
					currentPage++
					updateWindowTitle(window, filePath, currentPage+1, totalPages)
					window.Invalidate()
				}
			case key.NameUpArrow, "W", "w": // 上箭头键或W键：上一页
				if doc != nil && totalPages > 0 && currentPage > 0 {
					currentPage--
					updateWindowTitle(window, filePath, currentPage+1, totalPages)
					window.Invalidate()
				}
			case key.NameDownArrow, "S", "s": // 下箭头键或S键：下一页
				if doc != nil && totalPages > 0 && currentPage < totalPages-1 {
					currentPage++
					updateWindowTitle(window, filePath, currentPage+1, totalPages)
					window.Invalidate()
				}
			case key.NameHome: // Home键：第一页
				if doc != nil && totalPages > 0 && currentPage != 0 {
					currentPage = 0
					updateWindowTitle(window, filePath, currentPage+1, totalPages)
					window.Invalidate()
				}
			case key.NameEnd: // End键：最后一页
				if doc != nil && totalPages > 0 && currentPage != totalPages-1 {
					currentPage = totalPages - 1
					updateWindowTitle(window, filePath, currentPage+1, totalPages)
					window.Invalidate()
				}
			case key.NamePageUp: // PageUp键：上一页
				if doc != nil && totalPages > 0 && currentPage > 0 {
					currentPage--
					updateWindowTitle(window, filePath, currentPage+1, totalPages)
					window.Invalidate()
				}
			case key.NamePageDown: // PageDown键：下一页
				if doc != nil && totalPages > 0 && currentPage < totalPages-1 {
					currentPage++
					updateWindowTitle(window, filePath, currentPage+1, totalPages)
					window.Invalidate()
				}
			case "Q", "q", key.NameEscape: // Q键或ESC键：退出
				window.Perform(system.ActionClose)
			}
		}
	}

	for {
		switch e := window.Event().(type) {
		case app.DestroyEvent:
			return e.Err

		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)

			for {
				ev, ok := gtx.Event(
					key.Filter{Optional: key.ModShift, Name: "O"},
					key.Filter{Optional: key.ModShift, Name: "A"},
					key.Filter{Optional: key.ModShift, Name: "D"},
					key.Filter{Optional: key.ModShift, Name: key.NameUpArrow},
					key.Filter{Optional: key.ModShift, Name: key.NameDownArrow},
					key.Filter{Optional: key.ModShift, Name: key.NamePageUp},
					key.Filter{Optional: key.ModShift, Name: key.NamePageDown},
				)
				if !ok {
					break
				}
				handleKeyEvent(ev)

			}

			// 处理按钮点击事件
			if openBtn.Clicked(gtx) && !isLoading {
				isLoading = true
				go func() {
					selectedFile, err := zenity.SelectFile(
						zenity.Title("选择OFD文件"),
						zenity.FileFilter{
							Name:     "OFD文件",
							Patterns: []string{"*.ofd"},
							CaseFold: false,
						},
					)

					window.Invalidate()

					if err != nil {
						if err != zenity.ErrCanceled {
							log.Printf("文件选择错误: %v", err)
						}
					} else if selectedFile != filePath {
						// 关闭当前文件
						if ofd != nil {
							_ = ofd.Close()
							ofd = nil
						}
						doc = nil

						filePath = selectedFile
						log.Printf("已选择文件: %s", filePath)

						ofd, err = parser.NewOFD(filePath)
						if err != nil {
							_ = zenity.Error(err.Error())
						} else {
							doc = render.NewDocument(color.Transparent, ofd.Documents[0])
							currentPage = 0
							totalPages = len(doc.Pages)
							// 更新窗口标题
							updateWindowTitle(window, filePath, currentPage+1, totalPages)
						}
					}

					isLoading = false
					window.Invalidate()
				}()
			}

			// 如果未选择文件，显示选择界面
			if filePath == "" {
				layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{
						Axis:      layout.Vertical,
						Alignment: layout.Middle,
					}.Layout(gtx,
						layout.Rigid(layout.Spacer{Height: unit.Dp(40)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if isLoading {
								loadingLabel := material.H6(theme, "加载中...")
								loadingLabel.Color = color.NRGBA{R: 100, G: 100, B: 100, A: 255}
								loadingLabel.Alignment = text.Middle
								return loadingLabel.Layout(gtx)
							}
							btn := material.Button(theme, &openBtn, "选择OFD文件 (或按O键)")
							btn.Background = color.NRGBA{R: 0, G: 100, B: 200, A: 255}
							btn.CornerRadius = unit.Dp(8)
							return btn.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if isLoading {
								hintLabel := material.Body2(theme, "正在处理文件选择...")
								hintLabel.Color = color.NRGBA{R: 150, G: 150, B: 150, A: 255}
								return hintLabel.Layout(gtx)
							} else if filePath != "" {
								pathLabel := material.Body1(theme, "已选择: "+filePath)
								pathLabel.Color = color.NRGBA{R: 0, G: 150, B: 0, A: 255}
								return pathLabel.Layout(gtx)
							} else {
								hintLabel := material.Body2(theme, "请点击上方按钮或按O键选择OFD文件")
								hintLabel.Color = color.NRGBA{R: 128, G: 128, B: 128, A: 255}
								return hintLabel.Layout(gtx)
							}
						}),
						// 显示键盘快捷键帮助
						layout.Rigid(layout.Spacer{Height: unit.Dp(40)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{
								Axis:      layout.Vertical,
								Alignment: layout.Middle,
								Spacing:   layout.SpaceEvenly,
							}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									helpLabel := material.Body2(theme, "键盘快捷键:")
									helpLabel.Color = color.NRGBA{R: 80, G: 80, B: 80, A: 255}
									return helpLabel.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									shortcuts := material.Body2(theme, "O: 打开文件 | ←/→/A/D: 翻页 | Home/End: 首/末页 | Q/ESC: 退出")
									shortcuts.Color = color.NRGBA{R: 100, G: 100, B: 100, A: 255}
									return shortcuts.Layout(gtx)
								}),
							)
						}),
					)
				})
			}

			// 如果已加载文档，显示文档内容
			if ofd != nil && doc != nil && totalPages > 0 {
				// 确保当前页码在有效范围内
				if currentPage < 0 {
					currentPage = 0
				}
				if currentPage >= totalPages {
					currentPage = totalPages - 1
				}

				// 显示文档内容
				layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					page := doc.Pages[currentPage]
					box := page.Area.PhysicalBox
					c := gio.NewContain(gtx, box.Width, box.Height)
					ctx := canvas.NewContext(c)

					if err := doc.Draw(ctx, page); err != nil {
						log.Printf("绘制页面错误: %v", err)
						return layout.Dimensions{}
					}
					return c.Dimensions()
				})

				// 底部显示简单的提示信息
				layout.S.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						hintLabel := material.Caption(theme, "使用 ←/→/A/D 键翻页，O 键打开新文件")
						hintLabel.Color = color.NRGBA{R: 128, G: 128, B: 128, A: 200}
						hintLabel.Alignment = text.Middle
						return hintLabel.Layout(gtx)
					})
				})
			}

			e.Frame(gtx.Ops)
		}
	}
}

// updateWindowTitle 更新窗口标题显示文件信息和页码
func updateWindowTitle(window *app.Window, filePath string, currentPage, totalPages int) {
	if filePath == "" {
		window.Option(app.Title("OFD Viewer"))
		return
	}

	// 获取文件名
	fileName := filepath.Base(filePath)
	// 在 Windows 上使用中文标题，其他系统使用英文标题
	if runtime.GOOS == "windows" {
		// Windows 通常支持中文
		title := fmt.Sprintf("%s - 第 %d/%d 页 - OFD Viewer", fileName, currentPage, totalPages)
		window.Option(app.Title(title))
	} else {
		// 其他系统使用英文
		title := fmt.Sprintf("%s - %d/%d - OFD Viewer", fileName, currentPage, totalPages)
		window.Option(app.Title(title))
	}
}
