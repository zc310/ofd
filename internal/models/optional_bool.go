package models

import (
	"encoding/xml"

	"github.com/goccy/go-json/v2"
)

// OptionalBool 表示一个可选的布尔值，用于处理 XML 属性中
// 缺省时需要默认值为 true 的场景（如 OFD 的 Visible 属性）。
//
// 使用方式：
//   - 属性未指定：Value() 返回默认值（由调用方指定）
//   - 属性值为 true：Value() 返回 true
//   - 属性值为 false：Value() 返回 false
//
// 示例：
//
//	// 判断是否可见（默认 true）
//	if unit.Visible.Value(true) { ... }
type OptionalBool struct {
	v *bool
}

// NewOptionalBool 创建一个 OptionalBool
func NewOptionalBool(b bool) OptionalBool {
	return OptionalBool{v: &b}
}

// Value 返回布尔值。defaultIfNotSet 为属性未设置时的默认值。
func (o OptionalBool) Value(defaultIfNotSet bool) bool {
	if o.v == nil {
		return defaultIfNotSet
	}
	return *o.v
}

// IsSet 返回属性是否被显式设置
func (o OptionalBool) IsSet() bool {
	return o.v != nil
}

// Bool 获取底层的 *bool 指针
func (o *OptionalBool) Bool() *bool {
	return o.v
}

// Set 设置值
func (o *OptionalBool) Set(b bool) {
	o.v = &b
}

// Reset 重置为未设置状态
func (o *OptionalBool) Reset() {
	o.v = nil
}

// MarshalJSON 将未设置、显式 true 和显式 false 分别编码为 null、true 和 false。
func (o OptionalBool) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.v)
}

// UnmarshalJSON 读取可选布尔值，null 将其恢复为未设置状态。
func (o *OptionalBool) UnmarshalJSON(data []byte) error {
	var value *bool
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.v = value
	return nil
}

// UnmarshalXMLAttr 实现 XML 属性反序列化
func (o *OptionalBool) UnmarshalXMLAttr(attr xml.Attr) error {
	b := attr.Value == "true"
	o.v = &b
	return nil
}
