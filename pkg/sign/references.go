package sign

import (
	"path"
	"sort"
	"strings"

	"github.com/zc310/ofd/internal/spec"
)

// collectReferences 计算文档体的签名引用（相对签名目录）。
// options 为 nil 时包含文档体目录下的全部非签名文件；RootDocument 为真时额外纳入根文件。
func collectReferences(docDir string, entries []entry, options *ReferenceOptions) []string {
	references := make([]string, 0)
	for _, item := range entries {
		if isSignatureEntry(item.name) {
			continue
		}
		if !strings.HasPrefix(item.name, docDir+"/") {
			continue
		}
		relative := strings.TrimPrefix(item.name, docDir+"/")
		if !matchReference(relative, options) {
			continue
		}
		references = append(references, path.Join("..", relative))
	}
	if options != nil && options.RootDocument {
		references = append(references, path.Join("..", "..", spec.RootDocument))
	}
	sort.Strings(references)
	return references
}

// matchReference 判断文档体目录下的相对路径是否符合引用选择规则。
func matchReference(relative string, options *ReferenceOptions) bool {
	if options == nil {
		return true
	}
	if len(options.Include) > 0 {
		matched := false
		for _, pattern := range options.Include {
			if matchGlob(pattern, relative) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, pattern := range options.Exclude {
		if matchGlob(pattern, relative) {
			return false
		}
	}
	return true
}

// matchGlob 以不区分大小写的 glob 匹配相对路径。
// "*" 不跨目录；"**" 跨目录；"?" 匹配单个字符。同时支持以 "**/" 前缀表示任意层级。
func matchGlob(pattern, relative string) bool {
	pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
	if pattern == "" {
		return false
	}
	if pattern == "**" {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		if matchGlob(strings.TrimPrefix(pattern, "**/"), relative) {
			return true
		}
		if index := strings.Index(relative, "/"); index >= 0 {
			return matchGlob(pattern, relative[index+1:])
		}
		return false
	}
	// 把 pattern 中的 "**" 归一化为 "*"，交给 path.Match 逐段匹配。
	parts := strings.Split(relative, "/")
	patternParts := strings.Split(pattern, "/")
	return matchSegments(patternParts, parts)
}

// matchSegments 逐段 glob 匹配，支持 "**" 匹配任意多段。
func matchSegments(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		// "**" 匹配零段或多段。
		if matchSegments(pattern[1:], parts) {
			return true
		}
		if len(parts) > 0 {
			return matchSegments(pattern, parts[1:])
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	matched, err := path.Match(strings.ToLower(pattern[0]), strings.ToLower(parts[0]))
	if err != nil || !matched {
		return false
	}
	return matchSegments(pattern[1:], parts[1:])
}
