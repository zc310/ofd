//go:build android

package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
)

func chooseOpenFile(title string, parent fyne.Window) (fileSelection, error) {
	result := make(chan fileResult, 1)
	fyne.Do(func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				if reader != nil {
					_ = reader.Close()
				}
				result <- fileResult{err: err}
				return
			}
			// 保持读取器有效，直到后台加载流程完成读取。不要在 Android
			// 文件选择器回调中读取或关闭它，因为这两个操作可能会在
			// 文件选择器线程中再次调用 JVM。
			result <- fileResult{selection: fileSelection{name: "document.ofd", input: reader}}
		}, parent)
		fileDialog.SetTitleText(title)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".ofd"}))
		fileDialog.SetConfirmText("打开")
		fileDialog.SetDismissText("取消")
		fileDialog.Show()
	})
	resultValue := <-result
	return resultValue.selection, resultValue.err
}

func chooseSaveFile(title, fileName, extension string, parent fyne.Window) (fileSelection, error) {
	result := make(chan fileResult, 1)
	fyne.Do(func() {
		fileDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil || writer == nil {
				if writer != nil {
					_ = writer.Close()
				}
				result <- fileResult{err: err}
				return
			}
			result <- fileResult{selection: fileSelection{name: fileName, output: writer}}
		}, parent)
		fileDialog.SetTitleText(title)
		fileDialog.SetFileName(fileName)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{"." + extension}))
		fileDialog.SetConfirmText("保存")
		fileDialog.SetDismissText("取消")
		fileDialog.Show()
	})
	resultValue := <-result
	return resultValue.selection, resultValue.err
}

type fileResult struct {
	selection fileSelection
	err       error
}
