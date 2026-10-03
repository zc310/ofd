package models

// ZOrder 模板页相对于页面内容的叠放顺序。
//
// 值可为 Background（背景层）或 Foreground（前景层）。
// 页面模板引用未显式指定时，按 OFD 规范取默认值 Background。
type ZOrder string

// String 返回叠放顺序的字符串形式。
func (z ZOrder) String() string {
	return string(z)
}
