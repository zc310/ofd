//go:build !windows

package sealimg

import (
	"os"
	"strings"
)

// systemLanguage 按 LC_ALL、LC_MESSAGES、LANG、LANGUAGE 的顺序判断系统语言是否
// 为英文。全部未设置时返回 false，即沿用中文文案。
//
// 这里不查更「权威」的接口：语言环境变量是 POSIX 规定的判定依据，容器里也最
// 容易设置；为一个演示图片去拉 locale 依赖不划算。macOS 与 Linux 共用这一份。
func systemLanguage() bool {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		// LANGUAGE 是冒号分隔的优先级列表，取第一个非 C/POSIX 的值。
		for _, item := range strings.Split(value, ":") {
			if isEnglishTag(item) {
				return true
			}
		}
	}
	return false
}
