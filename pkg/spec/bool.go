package spec

import (
	"encoding/json"
	"encoding/xml"
	"strconv"
)

// Bool 表示一个可选的布尔值，区分未设置、显式 true 和显式 false。
// 零值表示未设置，默认值由调用方指定（如 OFD 的 Visible 属性默认为 true）。
// 与元素形式的 Permissions 布尔指针不同，本类型可在编码属性时省略未设置值。
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
type Bool struct {
	v *bool
}

// NewBool 创建一个显式设置的可选布尔值。
func NewBool(b bool) Bool {
	return Bool{v: &b}
}

// Value 返回布尔值。defaultIfNotSet 为属性未设置时的默认值。
func (o Bool) Value(defaultIfNotSet bool) bool {
	if o.v == nil {
		return defaultIfNotSet
	}
	return *o.v
}

// IsSet 返回属性是否被显式设置
func (o Bool) IsSet() bool {
	return o.v != nil
}

// Bool 获取底层的 *bool 指针
func (o *Bool) Bool() *bool {
	return o.v
}

// Set 设置值
func (o *Bool) Set(b bool) {
	o.v = &b
}

// Reset 重置为未设置状态
func (o *Bool) Reset() {
	o.v = nil
}

// MarshalJSON 将未设置、显式 true 和显式 false 分别编码为 null、true 和 false。
func (o Bool) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.v)
}

// UnmarshalJSON 读取可选布尔值，null 将其恢复为未设置状态。
func (o *Bool) UnmarshalJSON(data []byte) error {
	var value *bool
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.v = value
	return nil
}

// UnmarshalXMLAttr 实现 XML 属性反序列化
func (o *Bool) UnmarshalXMLAttr(attr xml.Attr) error {
	b := attr.Value == "true"
	o.v = &b
	return nil
}

// MarshalXMLAttr 省略未设置属性，并保留显式 true 或 false。
func (o Bool) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
	if o.v == nil {
		return xml.Attr{}, nil
	}
	return xml.Attr{Name: name, Value: strconv.FormatBool(*o.v)}, nil
}
