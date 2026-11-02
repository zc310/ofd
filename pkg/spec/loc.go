package spec

import (
	pp "path"

	"strings"
)

// StLoc 是 GB/T 33190—2016 §7.3 表2的包内位置字符串，区分大小写，使用斜杠分隔。
// 路径操作只做词法处理，不验证ZIP条目安全性，也不授权访问文件系统。
type StLoc string

// String 转换为字符串
func (p StLoc) String() string {
	return string(p)
}

// IsEmpty 判断是否为空
func (p StLoc) IsEmpty() bool {
	return string(p) == ""
}

// IsAbsolute 判断是否是绝对路径
func (p StLoc) IsAbsolute() bool {
	return strings.HasPrefix(string(p), "/")
}

// Join 拼接路径
func (p StLoc) Join(elem ...string) StLoc {
	parts := make([]string, 1, 1+len(elem))
	parts[0] = string(p)
	parts = append(parts, elem...)
	return StLoc(pp.Join(parts...))
}

// Resolve 解析路径
// base: 基础路径
func (p StLoc) Resolve(base StLoc) StLoc {
	// 如果当前是绝对路径，直接返回
	if p.IsAbsolute() {
		return p.normalize()
	}

	// 相对路径，基于 base 解析
	return base.Join(string(p)).normalize()
}

// normalize 规范化路径
func (p StLoc) normalize() StLoc {
	// 清理路径
	path := pp.Clean(string(p))
	// 移除 ./ 前缀
	path = strings.TrimPrefix(path, "./")
	return StLoc(path)
}

// Dir 返回目录部分
func (p StLoc) Dir() StLoc {
	return StLoc(pp.Dir(string(p)))
}

// Base 返回文件名部分
func (p StLoc) Base() string {
	return pp.Base(string(p))
}

// Ext 返回扩展名
func (p StLoc) Ext() string {
	return pp.Ext(string(p))
}

// HasPrefix 判断是否有指定前缀
func (p StLoc) HasPrefix(prefix StLoc) bool {
	return strings.HasPrefix(string(p), string(prefix))
}

// TrimPrefix 去除前缀
func (p StLoc) TrimPrefix(prefix StLoc) StLoc {
	return StLoc(strings.TrimPrefix(string(p), string(prefix)))
}

// In 按字符串前缀判断是否位于目录下；不清理路径，不能用于安全校验。
func (p StLoc) In(dir StLoc) bool {
	return strings.HasPrefix(string(p), string(dir)+"/")
}

// Clean 清理路径
func (p StLoc) Clean() StLoc {
	return p.normalize()
}
