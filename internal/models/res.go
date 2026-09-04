package models

import "encoding/xml"

// Res OFD 资源文件定义。
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

// ColorSpaces 颜色空间资源集合。
type ColorSpaces struct {
	// ColorSpace 颜色空间资源列表。
	ColorSpace []ColorSpace `xml:"ColorSpace"`
}

// ColorSpace 颜色空间资源定义。
type ColorSpace struct {
	// ID 颜色空间标识，在资源文件内唯一。
	ID StID `xml:"ID,attr"`
	// Type 颜色空间类型，可为 GRAY、RGB 或 CMYK。
	Type string `xml:"Type,attr"`
	// BitsPerComponent 每个颜色分量的位数。
	BitsPerComponent int `xml:"BitsPerComponent,attr,omitempty"`
	// Profile 颜色空间配置文件的位置。
	Profile StLoc `xml:"Profile,attr,omitempty"`
	// Palette 颜色空间使用的调色板。
	Palette *Palette `xml:"Palette"`
}

// Palette 颜色空间的调色板定义。
type Palette struct {
	// CV 调色板中的颜色值列表。
	CV []StArray `xml:"CV"`
}

// DrawParams 绘制参数资源集合。
type DrawParams struct {
	// DrawParam 绘制参数资源列表。
	DrawParam []*DrawParam `xml:"DrawParam"`
}

// DrawParam 绘制参数资源定义。
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
	DashPattern *StArrayF `xml:"DashPattern,attr,omitempty"`
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

func (p DrawParam) HasLineWidth() bool {
	return p.lineWidthSet || p.LineWidth != 0
}

func (p DrawParam) HasDashOffset() bool {
	return p.dashOffsetSet || p.DashOffset != 0
}

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

// Fonts 字体资源集合。
type Fonts struct {
	// Font 字体资源列表。
	Font []Font `xml:"Font"`
}

// Font 字体资源定义。
type Font struct {
	// ID 字体标识，在资源文件内唯一。
	ID StID `xml:"ID,attr"`
	// FontName 字体名称。
	FontName string `xml:"FontName,attr"`
	// FamilyName 字体族名称。
	FamilyName string `xml:"FamilyName,attr,omitempty"`
	// Charset 字符集，可为 symbol、prc、big5、shift-jis、wansung、johab 或 unicode。
	Charset string `xml:"Charset,attr,omitempty"`
	// Italic 是否为斜体字体。
	Italic bool `xml:"Italic,attr,omitempty"`
	// Bold 是否为粗体字体。
	Bold bool `xml:"Bold,attr,omitempty"`
	// Serif 是否为衬线字体。
	Serif bool `xml:"Serif,attr,omitempty"`
	// FixedWidth 是否为等宽字体。
	FixedWidth bool `xml:"FixedWidth,attr,omitempty"`
	// FontFile 字体文件的位置。
	FontFile StLoc `xml:"FontFile,omitempty"`
}

// MultiMedias 多媒体资源集合。
type MultiMedias struct {
	// MultiMedia 多媒体资源列表。
	MultiMedia []*MultiMedia `xml:"MultiMedia"`
}

// MultiMedia 多媒体资源定义。
type MultiMedia struct {
	// ID 多媒体资源标识，在资源文件内唯一。
	ID StID `xml:"ID,attr"`
	// Type 多媒体类型，可为 Image、Audio 或 Video。
	Type string `xml:"Type,attr"`
	// Format 多媒体文件格式。
	Format string `xml:"Format,attr,omitempty"`
	// MediaFile 多媒体文件的位置。
	MediaFile StLoc `xml:"MediaFile"`
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
