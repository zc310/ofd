//go:build windows

package sealimg

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// systemLanguage 判断系统语言是否为英文。
//
// Windows 没有 LANG/LC_ALL 这些 POSIX 变量，cmd 与 PowerShell 里通常一个都没
// 设。只看环境变量会让英文版 Windows 拿到中文文案——而 Windows 又普遍预装
// 中文字体（宋体、微软雅黑），于是拿不到「缺中文字体」这个回退机会，结果英文
// 系统仍然出中文章。因此优先问系统真实语言。
//
// 环境变量仍作为兜底且排在前面：CI 常用 LANG=en_US.UTF-8 覆盖镜像语言，这时它
// 比系统设置更贴近预期。
func systemLanguage() bool {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		for _, item := range strings.Split(value, ":") {
			if isEnglishTag(item) {
				return true
			}
		}
	}
	if tag, ok := windowsUserLanguage(); ok {
		return isEnglishTag(tag)
	}
	return false
}

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGetUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")
)

// localeNameMaxLength 对应 Win32 的 LOCALE_NAME_MAX_LENGTH，官方取值 85。
const localeNameMaxLength = 85

// windowsUserLanguage 返回当前用户的 BCP 47 语言标签，形如 en-US、zh-CN。
//
// golang.org/x/sys/windows 没有导出 GetUserDefaultLocaleName，只能自己声明。
// 这个 API 自 Windows Vista 起就是稳定的，用 syscall 惰性绑定也不会给非 Windows
// 平台带来链接期负担——整份文件都有 build tag 约束。
func windowsUserLanguage() (string, bool) {
	buf := make([]uint16, localeNameMaxLength)
	length, _, _ := procGetUserDefaultLocaleName.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(localeNameMaxLength),
	)
	if length == 0 {
		return "", false
	}
	tag := syscall.UTF16ToString(buf[:length])
	return tag, tag != ""
}
