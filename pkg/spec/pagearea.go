package spec

// PageArea 是 GB/T 33190—2016 §7.5 表7的页面区域声明。
// 不在解码时补齐 A4 尺寸或执行页面/模板/文档之间的区域继承。
type PageArea struct {
	// PhysicalBox 页面物理区域。
	PhysicalBox StBox `xml:"PhysicalBox"`
	// ApplicationBox 页面显示区域。
	ApplicationBox *StBox `xml:"ApplicationBox,omitempty"`
	// ContentBox 页面版心区域。
	ContentBox *StBox `xml:"ContentBox,omitempty"`
	// BleedBox 页面出血区域。
	BleedBox *StBox `xml:"BleedBox,omitempty"`
}
