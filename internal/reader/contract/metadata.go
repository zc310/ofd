// Package contract 定义浏览器和桌面阅读器共用的网页数据字段。
package contract

import "github.com/zc310/ofd/pkg/reader"

// Permissions 返回非 nil 数组，保证空结果在两种传输中一致。
func Permissions(values []reader.DocumentPermissions) []reader.DocumentPermissions {
	result := make([]reader.DocumentPermissions, len(values))
	for i, value := range values {
		result[i] = value
		result[i].Pages = make([]int, len(value.Pages))
		copy(result[i].Pages, value.Pages)
	}
	return result
}

// Fonts 复制字体声明并确保空列表编码为数组，不包含字体二进制数据。
func Fonts(values []reader.FontInfo) []reader.FontInfo {
	result := make([]reader.FontInfo, len(values))
	copy(result, values)
	return result
}
