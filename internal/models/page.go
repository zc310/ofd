package models

import "encoding/xml"

// Page OFD 页面定义。
type Page struct {
	ID      StID  `xml:"ID,attr"`
	BaseLoc StLoc `xml:"BaseLoc,attr"`
}

// PageContent OFD 页面内容，包含页面区域、资源、模板和内容对象。
type PageContent struct {
	Template []Template  `xml:"Template"`
	PageRes  []StLoc     `xml:"PageRes"`
	Area     *CtPageArea `xml:"Area"`
	Content  *Content    `xml:"Content"`
	Actions  *Actions    `xml:"Actions"`
}

// EnsurePhysicalBox 确保页面区域存在且物理区域有效。
func (p *PageContent) EnsurePhysicalBox() {
	if p == nil {
		return
	}
	if p.Area == nil {
		p.Area = &CtPageArea{}
	}
	p.Area.EnsurePhysicalBox()
}

// Template 页面使用的模板引用。
type Template struct {
	TemplateID StRefID `xml:"TemplateID,attr"`
	ZOrder     string  `xml:"ZOrder,attr,omitempty"` // Background or Foreground
}

// Content 页面内容容器。
type Content struct {
	Layer []*Layer `xml:"Layer"`
}

// Layer 页面图层，包含按文档顺序排列的页面对象。
type Layer struct {
	ID        StID    `xml:"ID,attr"`
	Type      string  `xml:"Type,attr,omitempty"` // Body, Background, Foreground, Custom
	DrawParam StRefID `xml:"DrawParam,attr,omitempty"`
	CTPageBlock
}

// UnmarshalXML 解析 Layer，除了图层属性外，还需按文档顺序记录其中的图形对象。
func (l *Layer) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, attr := range start.Attr {
		switch attr.Name.Local {
		case "ID":
			_ = l.ID.UnmarshalText([]byte(attr.Value))
		case "Type":
			l.Type = attr.Value
		case "DrawParam":
			var id StID
			_ = id.UnmarshalText([]byte(attr.Value))
			l.DrawParam = StRefID(id)
		}
	}
	return l.CTPageBlock.UnmarshalXML(d, start)
}

// Actions 动作集合。
type Actions struct {
	Action []CtAction `xml:"Action"`
}

// CtClip 裁剪定义。
type CtClip struct {
	Area []ClipArea `xml:"Area"`
}

// ClipArea 单个裁剪区域，可以由路径或文字对象定义。
type ClipArea struct {
	Path      *CtPath  `xml:"Path"`
	Text      *CtText  `xml:"Text"`
	DrawParam *StRefID `xml:"DrawParam,attr,omitempty"`
	CTM       *CTM     `xml:"CTM,attr,omitempty"`
}

// PageItemKind 页面块中对象的种类。
type PageItemKind int

const (
	_ PageItemKind = iota
	PageItemText
	PageItemPath
	PageItemImage
	PageItemComposite
	PageItemBlock
)

// PageItem 按文档顺序保存页面块中的一个图形对象。
type PageItem struct {
	Kind      PageItemKind
	Text      TextObject
	Path      PathObject
	Image     ImageObject
	Composite CompositeObject
	Block     PageBlock
}

// CTPageBlock 页面内容块，除了按类型分组保存外，还按文档顺序记录到 Items 中。
type CTPageBlock struct {
	TextObject      []TextObject      `xml:"TextObject"`
	PathObject      []PathObject      `xml:"PathObject"`
	ImageObject     []ImageObject     `xml:"ImageObject"`
	CompositeObject []CompositeObject `xml:"CompositeObject"`
	PageBlock       []PageBlock       `xml:"PageBlock"`
	Items           []PageItem
}

// UnmarshalXML 按文档顺序解析页面块中的图形对象，保证绘制时各对象保持原始先后顺序。
func (p *CTPageBlock) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch elem := token.(type) {
		case xml.StartElement:
			var item PageItem
			switch elem.Name.Local {
			case "TextObject":
				var o TextObject
				if err := d.DecodeElement(&o, &elem); err != nil {
					return err
				}
				p.TextObject = append(p.TextObject, o)
				item.Kind = PageItemText
				item.Text = o
			case "PathObject":
				var o PathObject
				if err := d.DecodeElement(&o, &elem); err != nil {
					return err
				}
				p.PathObject = append(p.PathObject, o)
				item.Kind = PageItemPath
				item.Path = o
			case "ImageObject":
				var o ImageObject
				if err := d.DecodeElement(&o, &elem); err != nil {
					return err
				}
				p.ImageObject = append(p.ImageObject, o)
				item.Kind = PageItemImage
				item.Image = o
			case "CompositeObject":
				var o CompositeObject
				if err := d.DecodeElement(&o, &elem); err != nil {
					return err
				}
				p.CompositeObject = append(p.CompositeObject, o)
				item.Kind = PageItemComposite
				item.Composite = o
			case "PageBlock":
				var o PageBlock
				if err := d.DecodeElement(&o, &elem); err != nil {
					return err
				}
				p.PageBlock = append(p.PageBlock, o)
				item.Kind = PageItemBlock
				item.Block = o
			default:
				if err := d.Skip(); err != nil {
					return err
				}
				continue
			}
			p.Items = append(p.Items, item)
		case xml.EndElement:
			if elem == start.End() {
				return nil
			}
		}
	}
}

// TextObject 页面中的文字对象。
type TextObject struct {
	ID     StID `xml:"ID,attr"`
	CtText      // 嵌入CT_Text
}

// PathObject 页面中的路径对象。
type PathObject struct {
	ID     StID `xml:"ID,attr"`
	CtPath      // 嵌入CT_Path
}

// ImageObject 页面中的图像对象。
type ImageObject struct {
	ID      StID `xml:"ID,attr"`
	CtImage      // 嵌入CT_Image
}

// CompositeObject 页面中的复合对象。
type CompositeObject struct {
	ID          StID `xml:"ID,attr"`
	CtComposite      // 嵌入CT_Composite
}

// PageBlock 页面对象块，可以包含嵌套的页面对象。
type PageBlock struct {
	ID          StID `xml:"ID,attr"`
	CTPageBlock      // 嵌入CT_PageBlock
}

// UnmarshalXML 解析 PageBlock，读取 ID 属性后按文档顺序解析其内容。
func (p *PageBlock) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, attr := range start.Attr {
		if attr.Name.Local == "ID" {
			_ = p.ID.UnmarshalText([]byte(attr.Value))
		}
	}
	return p.CTPageBlock.UnmarshalXML(d, start)
}

// CtLayer 图层内容定义。
type CtLayer struct {
	Type      string  `xml:"Type,attr,omitempty"`
	DrawParam StRefID `xml:"DrawParam,attr,omitempty"`
	CTPageBlock
}

// CTGraphicUnit 图元通用属性定义。
type CTGraphicUnit struct {
	Actions  *Actions `xml:"Actions"`
	Clips    *Clips   `xml:"Clips"`
	Boundary StBox    `xml:"Boundary,attr"`
	Name     string   `xml:"Name,attr,omitempty"`
	// Visible 像素是否可见，未指定时默认为 true。
	Visible   OptionalBool `xml:"Visible,attr,omitempty"`
	CTM       *CTM         `xml:"CTM,attr,omitempty"`
	DrawParam StRefID      `xml:"DrawParam,attr,omitempty"`
	LineWidth float64      `xml:"LineWidth,attr,omitempty"`
	// 线端点样式，枚举值，指定了一条线的端点样式。默认值为 Butt
	Cap  string `xml:"Cap,attr,omitempty"`  // Butt, Round, Square
	Join string `xml:"Join,attr,omitempty"` // Miter, Round, Bevel
	// Join 为 Miter 时小角度 JoinSize 的截断值，默认值为 3.528。当 Join 不等于 Miter 时该参数无效
	MiterLimit  float64   `xml:"MiterLimit,attr,omitempty"`
	DashOffset  float64   `xml:"DashOffset,attr,omitempty"`
	DashPattern *StArrayF `xml:"DashPattern,attr,omitempty"`
	Alpha       *uint8    `xml:"Alpha,attr,omitempty"`
}

// VisibleValue 返回图元是否可见。OFD 未指定 Visible 时默认为可见。
func (g CTGraphicUnit) VisibleValue() bool {
	return g.Visible.Value(true)
}

// Clips 图元裁剪集合。
type Clips struct {
	TransFlag *bool    `xml:"TransFlag,attr,omitempty"`
	Clip      []CtClip `xml:"Clip"`
}

// CtText 文字图元定义。
type CtText struct {
	CTGraphicUnit
	FillColor   *CTColor        `xml:"FillColor"`
	StrokeColor *CTColor        `xml:"StrokeColor"`
	CGTransform []CTCGTransform `xml:"CGTransform"`
	TextCode    []TextCode      `xml:"TextCode"`
	Font        StRefID         `xml:"Font,attr"`
	// Size 字号，单位为毫米
	Size float64 `xml:"Size,attr"`
	// Stroke 是否描边。默认值为 false 当文字对象被裁剪区引用时此属性被忽略
	Stroke bool `xml:"Stroke,attr,omitempty"`
	// Fill 是否填充 默认值 true 当文字对象被裁剪区引用时此属性被忽略
	Fill string `xml:"Fill,attr,omitempty"`
	// HScale 字型在水平方向的放缩比，取值为[0 1.0]，默认值为 1.0
	// 例如：当 HScale 值为 0.5 时表示实际显示的字宽为原来字宽的一半
	HScale float64 `xml:"HScale,attr,omitempty"`
	// ReadDirection 阅读方向，指定了文字排列的方向，默认值为 0
	ReadDirection int `xml:"ReadDirection,attr,omitempty"`
	// CharDirection 字符方向，指定了文字放置的方式，默认值为 0
	CharDirection int `xml:"CharDirection,attr,omitempty"`
	Weight        int `xml:"Weight,attr,omitempty"` // 0,100,...,1000
	// Italic 是否是斜体样式，默认值为 false
	Italic bool `xml:"Italic,attr,omitempty"`
}

// CTCGTransform 字符图形变换定义。
type CTCGTransform struct {
	// Glyphs 变换关系中字型索引列表
	Glyphs       StArrayI `xml:"Glyphs"`
	CodePosition int      `xml:"CodePosition,attr"`
	CodeCount    int      `xml:"CodeCount,attr,omitempty"`
	GlyphCount   int      `xml:"GlyphCount,attr,omitempty"`
}

// TextCode 文字编码及其定位信息。
type TextCode struct {
	Value  string   `xml:",chardata"`
	X      float64  `xml:"X,attr,omitempty"`
	Y      float64  `xml:"Y,attr,omitempty"`
	DeltaX StArrayF `xml:"DeltaX,attr,omitempty"`
	DeltaY StArrayF `xml:"DeltaY,attr,omitempty"`
}

// CtImage 图像图元定义。
type CtImage struct {
	CTGraphicUnit
	Border       *Border `xml:"Border"`
	ResourceID   StRefID `xml:"ResourceID,attr"`
	Substitution StRefID `xml:"Substitution,attr,omitempty"`
	ImageMask    StRefID `xml:"ImageMask,attr,omitempty"`
}

// Border 图像边框定义。
type Border struct {
	BorderColor           *CTColor `xml:"BorderColor"`
	LineWidth             float64  `xml:"LineWidth,attr,omitempty"`
	HorizonalCornerRadius float64  `xml:"HorizonalCornerRadius,attr,omitempty"`
	VerticalCornerRadius  float64  `xml:"VerticalCornerRadius,attr,omitempty"`
	DashOffset            float64  `xml:"DashOffset,attr,omitempty"`
	DashPattern           StArray  `xml:"DashPattern,attr,omitempty"`
}

// CtComposite 复合图元定义。
type CtComposite struct {
	CTGraphicUnit
	ResourceID StRefID `xml:"ResourceID,attr"`
}

// CtPath 路径图元定义。
type CtPath struct {
	CTGraphicUnit
	StrokeColor     *CTColor `xml:"StrokeColor"`
	FillColor       *CTColor `xml:"FillColor"`
	AbbreviatedData SVGPath  `xml:"AbbreviatedData"`
	// Stroke 是否钩边 默认 true
	Stroke string `xml:"Stroke,attr,omitempty"`
	Fill   bool   `xml:"Fill,attr,omitempty"`
	Rule   string `xml:"Rule,attr,omitempty"` // NonZero, Even-Odd
}

// CtPattern 图案填充定义。
type CtPattern struct {
	CellContent   CellContent `xml:"CellContent"`
	Width         float64     `xml:"Width,attr"`
	Height        float64     `xml:"Height,attr"`
	XStep         float64     `xml:"XStep,attr,omitempty"`
	YStep         float64     `xml:"YStep,attr,omitempty"`
	ReflectMethod string      `xml:"ReflectMethod,attr,omitempty"` // Normal, Row, Column, RowAndColumn
	RelativeTo    string      `xml:"RelativeTo,attr,omitempty"`    // Page, Object
	CTM           StArray     `xml:"CTM,attr,omitempty"`
}

// CellContent 图案填充单元的内容。
type CellContent struct {
	Thumbnail StRefID `xml:"Thumbnail,attr,omitempty"`
	CTPageBlock
}

// CTAxialShd 轴向渐变填充定义。
type CTAxialShd struct {
	Segment    []Segment `xml:"Segment"`
	MapType    string    `xml:"MapType,attr,omitempty"` // Direct, Repeat, Reflect
	MapUnit    float64   `xml:"MapUnit,attr,omitempty"`
	Extend     int       `xml:"Extend,attr,omitempty"` // 0,1,2,3
	StartPoint StPos     `xml:"StartPoint,attr"`
	EndPoint   StPos     `xml:"EndPoint,attr"`
}

// CTRadialShd 径向渐变填充定义。
type CTRadialShd struct {
	Segment      []Segment `xml:"Segment"`
	MapType      string    `xml:"MapType,attr,omitempty"` // Direct, Repeat, Reflect
	MapUnit      float64   `xml:"MapUnit,attr,omitempty"`
	Eccentricity float64   `xml:"Eccentricity,attr,omitempty"`
	Angle        float64   `xml:"Angle,attr,omitempty"`
	StartPoint   StPos     `xml:"StartPoint,attr"`
	StartRadius  float64   `xml:"StartRadius,attr,omitempty"`
	EndPoint     StPos     `xml:"EndPoint,attr"`
	EndRadius    float64   `xml:"EndRadius,attr"`
	Extend       int       `xml:"Extend,attr,omitempty"`
}

// CTGouraudShd Gouraud 三角网格渐变填充定义。
type CTGouraudShd struct {
	Point     []GouraudPoint `xml:"Point"`
	BackColor *CTColor       `xml:"BackColor,omitempty"`
	Extend    int            `xml:"Extend,attr,omitempty"`
}

// GouraudPoint Gouraud 渐变控制点。
type GouraudPoint struct {
	Color    CTColor `xml:"Color"`
	X        float64 `xml:"X,attr"`
	Y        float64 `xml:"Y,attr"`
	EdgeFlag int     `xml:"EdgeFlag,attr"` // 0,1,2
}

// CTLaGouraudShd Gouraud 网格渐变填充定义。
type CTLaGouraudShd struct {
	Point          []LaGouraudPoint `xml:"Point"`
	BackColor      *CTColor         `xml:"BackColor,omitempty"`
	VerticesPerRow int              `xml:"VerticesPerRow,attr"`
	Extend         int              `xml:"Extend,attr,omitempty"`
}

// LaGouraudPoint Gouraud 网格渐变控制点。
type LaGouraudPoint struct {
	Color CTColor `xml:"Color"`
	X     float64 `xml:"X,attr,omitempty"`
	Y     float64 `xml:"Y,attr,omitempty"`
}

// CTColor 颜色及颜色填充定义。
type CTColor struct {
	Pattern      *CtPattern      `xml:"Pattern"`
	AxialShd     *CTAxialShd     `xml:"AxialShd"`
	RadialShd    *CTRadialShd    `xml:"RadialShd"`
	GouraudShd   *CTGouraudShd   `xml:"GouraudShd"`
	LaGourandShd *CTLaGouraudShd `xml:"LaGourandShd"`
	// Value 颜色值，指定了当前颜色空间下各通道的取值。
	//Value的取值应符合"通道 1 通道 2 通道 3 …"格式。
	//此属性不出现时，应参考 Index 属性从颜色空间的调色版中取值。当二者都不出现时，该颜色各通道的值全部为 0
	//
	//可选
	Value *Color `xml:"Value,attr,omitempty"`
	// Index 调色板中颜色的编号，非负整数，将从当前颜色空间的调色板中取出相应索引的预定义颜色用来绘制。
	//索引从0开始 可选
	Index      int     `xml:"Index,attr,omitempty"`
	ColorSpace StRefID `xml:"ColorSpace,attr,omitempty"`
	// Alpha 颜色透明度，在 0~255 之间取值。默认为 255，表示完全不透明
	//
	//可选
	Alpha *uint8 `xml:"Alpha,attr,omitempty"`
}

// Segment 渐变中的颜色分段。
type Segment struct {
	Color    CTColor `xml:"Color"`
	Position float64 `xml:"Position,attr,omitempty"`
}
