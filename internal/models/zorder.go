package models

// ZOrder 模板页相对于页面内容的叠放顺序。
//
// 值可为 Background（背景层）或 Foreground（前景层）。
// 页面模板引用未显式指定时，按 OFD 规范取默认值 Background。
type ZOrder string

const (
	// ZOrderBackground 表示模板页绘制在页面内容之后（背景层）。
	ZOrderBackground ZOrder = "Background"
	// ZOrderForeground 表示模板页绘制在页面内容之前（前景层）。
	ZOrderForeground ZOrder = "Foreground"
)

// IsSet 报告叠放顺序是否被显式指定。
func (z ZOrder) IsSet() bool {
	return z != ""
}

// String 返回叠放顺序的字符串形式。
func (z ZOrder) String() string {
	return string(z)
}
