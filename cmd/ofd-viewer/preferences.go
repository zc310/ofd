package main

import "fyne.io/fyne/v2"

// defaultReaderPromptKey 是"启动时不再提示设置默认阅读器"的偏好键。
const defaultReaderPromptKey = "default-reader-prompt-dismissed"

// defaultReaderPromptDismissed 报告用户是否已经选择不再提示。
//
// 偏好存储不可用时按未记住处理：多提示一次只是多一个可关掉的对话框，少提示一次
// 则让用户永远不知道有这项设置。
func defaultReaderPromptDismissed() bool {
	current := fyne.CurrentApp()
	if current == nil {
		return false
	}
	return current.Preferences().BoolWithFallback(defaultReaderPromptKey, false)
}

// rememberDefaultReaderPrompt 记住不再提示。Fyne 的偏好由应用退出时统一落盘，
// 这里不需要自己做原子写。
func rememberDefaultReaderPrompt() {
	current := fyne.CurrentApp()
	if current == nil {
		return
	}
	current.Preferences().SetBool(defaultReaderPromptKey, true)
}
