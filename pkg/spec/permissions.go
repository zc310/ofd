package spec

import "encoding/xml"

// Permissions 是 GB/T 33190—2016 §7.5 表8的文档权限声明。
// 布尔指针保留省略与显式取值的区别，省略时按标准解释为允许。
// 本类型只保存声明，不表示调用方已获业务授权。
type Permissions struct {
	// Edit 是否允许编辑文档内容。
	Edit *bool `xml:"Edit,omitempty"`
	// Annot 是否允许添加或修改注解。
	Annot *bool `xml:"Annot,omitempty"`
	// Export 是否允许导出文档内容。
	Export *bool `xml:"Export,omitempty"`
	// Signature 是否允许签名。
	Signature *bool `xml:"Signature,omitempty"`
	// Watermark 是否允许添加或修改水印。
	Watermark *bool `xml:"Watermark,omitempty"`
	// PrintScreen 是否允许截屏。
	PrintScreen *bool `xml:"PrintScreen,omitempty"`
	// Print 为nil时默认允许打印且不限份数。
	Print *PrintSettings `xml:"Print,omitempty"`
	// ValidPeriod 为nil时不限期限；非nil时至少应有一个端点。
	ValidPeriod *ValidPeriod `xml:"ValidPeriod,omitempty"`
}

// Clone 深拷贝原始声明；nil接收者返回nil。副本不共享可变字段，且不缓存授权结果。
func (p *Permissions) Clone() *Permissions {
	if p == nil {
		return nil
	}
	result := *p
	for _, field := range []**bool{&result.Edit, &result.Annot, &result.Export, &result.Signature, &result.Watermark, &result.PrintScreen} {
		if *field != nil {
			value := **field
			*field = &value
		}
	}
	if p.Print != nil {
		value := *p.Print
		result.Print = &value
	}
	if p.ValidPeriod != nil {
		value := *p.ValidPeriod
		result.ValidPeriod = &value
	}
	return &result
}

// PrintSettings 是表8的打印设置。Copies负值不限份数，零禁止打印，正值为允许份数。
// XML缺省值由解码器应用；直接构造时零值表示禁止打印，不代表缺省允许。
type PrintSettings struct {
	// Printable 是否允许打印。
	Printable bool `xml:"Printable,attr"`
	// Copies 允许打印的份数。
	Copies int `xml:"Copies,attr"`
}

// UnmarshalXML 按表8应用允许打印、份数不限的属性缺省值。
func (p *PrintSettings) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type settings PrintSettings
	value := settings{Printable: true, Copies: -1}
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	*p = PrintSettings(value)
	return nil
}

// ValidPeriod 是表8的访问期限。端点均包含该时刻，缺失端点用零值表示。
// 空期限或倒置期限保留原声明，是否拒绝由权限策略判断。
type ValidPeriod struct {
	// StartDate 权限生效时间。
	StartDate DateTime `xml:"StartDate,attr,omitempty"`
	// EndDate 权限失效时间。
	EndDate DateTime `xml:"EndDate,attr,omitempty"`
}
