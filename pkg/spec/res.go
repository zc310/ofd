//nolint:tagliatelle // XML 名称严格遵循 GB/T 33190—2016 附录 A 的大小写。
package spec

import (
	"encoding/xml"
)

// Res 是 GB/T 33190—2016 §7.9 表18的资源文件声明，不包含资源数据或加载状态。
// 本批提供 XML 解码及声明访问，不提供完整资源文件的 XML 编码契约。
type Res struct {
	// BaseLoc 资源文件的基础路径。
	BaseLoc StLoc `xml:"BaseLoc,attr"`
	// ColorSpaces 颜色空间资源集合。
	ColorSpaces *ColorSpaces `xml:"ColorSpaces"`
	// DrawParams 绘制参数资源集合。
	DrawParams *DrawParams `xml:"DrawParams"`
	// Fonts 字体资源集合。
	Fonts *Fonts `xml:"Fonts"`
	// MultiMedias 多媒体资源集合。
	MultiMedias *MultiMedias `xml:"MultiMedias"`
	// CompositeGraphicUnits 复合图形资源集合。
	CompositeGraphicUnits *CompositeGraphicUnits `xml:"CompositeGraphicUnits"`
}

// DrawParams 绘制参数资源集合。
type DrawParams struct {
	// DrawParam 绘制参数资源列表。
	DrawParam []*DrawParam `xml:"DrawParam"`
}

// DrawParam 是§8.2表21的绘制参数声明，按§7.9扩展 ID，保留 Relative 引用。
type DrawParam struct {
	// ID 绘制参数标识，在资源文件内唯一。
	ID StID `xml:"ID,attr"`
	// Relative 继承的绘制参数资源引用。
	Relative StRefID `xml:"Relative,attr,omitempty"`
	// LineWidth 线宽，单位为毫米。
	LineWidth float64 `xml:"LineWidth,attr,omitempty"`
	// Join 线连接样式，可为 Miter、Round 或 Bevel。
	Join string `xml:"Join,attr,omitempty"`
	// Cap 线端点样式，可为 Butt、Round 或 Square。
	Cap string `xml:"Cap,attr,omitempty"`
	// DashOffset 虚线模式的起始偏移量。
	DashOffset float64 `xml:"DashOffset,attr,omitempty"`
	// DashPattern 虚线模式中实线段和空白段的长度数组。
	DashPattern *StArray[float64] `xml:"DashPattern,attr,omitempty"`
	// MiterLimit 斜接连接的长度限制。
	MiterLimit float64 `xml:"MiterLimit,attr,omitempty"`
	// FillColor 填充颜色。
	FillColor *CTColor `xml:"FillColor"`
	// StrokeColor 描边颜色。
	StrokeColor *CTColor `xml:"StrokeColor"`

	lineWidthSet  bool
	dashOffsetSet bool
	miterLimitSet bool
}

// UnmarshalXML 解析绘制参数，并记录显式出现的数值属性。
// 这些标记用于区分未指定与合法的零值，以便正确处理 Relative 继承。
func (p *DrawParam) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type drawParam DrawParam
	var value drawParam
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	*p = DrawParam(value)
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "LineWidth":
			p.lineWidthSet = true
		case "DashOffset":
			p.dashOffsetSet = true
		case "MiterLimit":
			p.miterLimitSet = true
		}
	}
	return nil
}

// HasLineWidth 判断线宽是否显式给出，包含解码所得的零值。
func (p DrawParam) HasLineWidth() bool {
	return p.lineWidthSet || p.LineWidth != 0
}

// SetLineWidth 设置显式线宽，包括合法的零值。
func (p *DrawParam) SetLineWidth(value float64) { p.LineWidth, p.lineWidthSet = value, true }

// SetDashOffset 设置显式虚线偏移，包括零值。
func (p *DrawParam) SetDashOffset(value float64) { p.DashOffset, p.dashOffsetSet = value, true }

// SetMiterLimit 设置显式斜接限制，包括零值。
func (p *DrawParam) SetMiterLimit(value float64) { p.MiterLimit, p.miterLimitSet = value, true }

// HasDashOffset 判断虚线偏移是否显式给出，包含解码所得的零值。
func (p DrawParam) HasDashOffset() bool {
	return p.dashOffsetSet || p.DashOffset != 0
}

// HasMiterLimit 判断斜接限制是否显式给出，包含解码所得的零值。
func (p DrawParam) HasMiterLimit() bool {
	return p.miterLimitSet || p.MiterLimit != 0
}

// Override 使用 source 中明确指定的属性覆盖当前绘制参数。
// 对数值属性保留明确指定的零值。
func (p *DrawParam) Override(source *DrawParam) {
	if source == nil {
		return
	}
	if source.HasLineWidth() {
		p.LineWidth = source.LineWidth
		p.lineWidthSet = true
	}
	if source.Join != "" {
		p.Join = source.Join
	}
	if source.HasDashOffset() {
		p.DashOffset = source.DashOffset
		p.dashOffsetSet = true
	}
	if source.DashPattern != nil {
		p.DashPattern = source.DashPattern
	}
	if source.Cap != "" {
		p.Cap = source.Cap
	}
	if source.HasMiterLimit() {
		p.MiterLimit = source.MiterLimit
		p.miterLimitSet = true
	}
	if source.FillColor != nil {
		p.FillColor = source.FillColor
	}
	if source.StrokeColor != nil {
		p.StrokeColor = source.StrokeColor
	}
}

// CompositeGraphicUnits 复合图形资源集合。
type CompositeGraphicUnits struct {
	// CompositeGraphicUnit 复合图形资源列表。
	CompositeGraphicUnit []CompositeGraphicUnit `xml:"CompositeGraphicUnit"`
}

// CompositeGraphicUnit 复合图形资源定义。
type CompositeGraphicUnit struct {
	// ID 复合图形资源标识，在资源文件内唯一。
	ID StID `xml:"ID,attr"`
	// Width 复合图形的宽度，单位为毫米。
	Width float64 `xml:"Width,attr"`
	// Height 复合图形的高度，单位为毫米。
	Height float64 `xml:"Height,attr"`
	// Thumbnail 复合图形缩略图资源引用。
	Thumbnail StRefID `xml:"Thumbnail,omitempty"`
	// Substitution 复合图形替代资源引用。
	Substitution StRefID `xml:"Substitution,omitempty"`
	// Content 复合图形包含的页面对象内容。
	Content CTPageBlock `xml:"Content"`
}
