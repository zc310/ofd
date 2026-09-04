package models

import "encoding/xml"

// Page OFD 页面定义。
type Page struct {
	// ID 页面标识，在文档内唯一。
	ID StID `xml:"ID,attr"`
	// BaseLoc 页面内容文件的位置。
	BaseLoc StLoc `xml:"BaseLoc,attr"`
}

// PageContent OFD 页面内容，包含页面区域、资源、模板和内容对象。
type PageContent struct {
	// Template 页面使用的模板页引用。
	Template []Template `xml:"Template"`
	// PageRes 页面级资源文件的位置。
	PageRes []StLoc `xml:"PageRes"`
	// Area 页面的区域定义。
	Area *CtPageArea `xml:"Area"`
	// Content 页面图层和页面对象内容。
	Content *Content `xml:"Content"`
	// Actions 页面级动作集合。
	Actions *Actions `xml:"Actions"`
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
	// TemplateID 被引用模板页的标识。
	TemplateID StRefID `xml:"TemplateID,attr"`
	// ZOrder 模板页的叠放顺序，可为 Background 或 Foreground。
	ZOrder string `xml:"ZOrder,attr,omitempty"`
}

// Content 页面内容容器。
type Content struct {
	// Layer 页面图层列表，顺序即图层的绘制顺序。
	Layer []*Layer `xml:"Layer"`
}

// Layer 页面图层，包含按文档顺序排列的页面对象。
type Layer struct {
	// ID 图层标识，在页面内唯一。
	ID StID `xml:"ID,attr"`
	// Type 图层类型，可为 Body、Background、Foreground 或 Custom。
	Type string `xml:"Type,attr,omitempty"`
	// DrawParam 图层使用的绘制参数引用。
	DrawParam StRefID `xml:"DrawParam,attr,omitempty"`
	// CTPageBlock 图层中的页面对象。
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
	// Action 动作列表。
	Action []CtAction `xml:"Action"`
}

// CtClip 裁剪定义。
type CtClip struct {
	// Area 裁剪区域列表。
	Area []ClipArea `xml:"Area"`
}

// ClipArea 单个裁剪区域，可以由路径或文字对象定义。
type ClipArea struct {
	// Path 用于定义裁剪区域的路径对象。
	Path *CtPath `xml:"Path"`
	// Text 用于定义裁剪区域的文字对象。
	Text *CtText `xml:"Text"`
	// DrawParam 裁剪对象使用的绘制参数引用。
	DrawParam *StRefID `xml:"DrawParam,attr,omitempty"`
	// CTM 裁剪对象使用的坐标变换矩阵。
	CTM *CTM `xml:"CTM,attr,omitempty"`
}

// PageItemKind 页面块中对象的种类。
type PageItemKind int

const (
	_ PageItemKind = iota
	// PageItemText 表示文字对象。
	PageItemText
	// PageItemPath 表示路径对象。
	PageItemPath
	// PageItemImage 表示图像对象。
	PageItemImage
	// PageItemComposite 表示复合对象。
	PageItemComposite
	// PageItemBlock 表示嵌套页面对象块。
	PageItemBlock
)

// PageItem 按文档顺序保存页面块中的一个图形对象。
type PageItem struct {
	// Kind 页面对象的类型。
	Kind PageItemKind
	// Text 当 Kind 为 PageItemText 时保存的文字对象。
	Text TextObject
	// Path 当 Kind 为 PageItemPath 时保存的路径对象。
	Path PathObject
	// Image 当 Kind 为 PageItemImage 时保存的图像对象。
	Image ImageObject
	// Composite 当 Kind 为 PageItemComposite 时保存的复合对象。
	Composite CompositeObject
	// Block 当 Kind 为 PageItemBlock 时保存的嵌套页面对象块。
	Block PageBlock
}

// CTPageBlock 页面内容块，除了按类型分组保存外，还按文档顺序记录到 Items 中。
type CTPageBlock struct {
	// TextObject 页面块中的文字对象，按类型分组保存。
	TextObject []TextObject `xml:"TextObject"`
	// PathObject 页面块中的路径对象，按类型分组保存。
	PathObject []PathObject `xml:"PathObject"`
	// ImageObject 页面块中的图像对象，按类型分组保存。
	ImageObject []ImageObject `xml:"ImageObject"`
	// CompositeObject 页面块中的复合对象，按类型分组保存。
	CompositeObject []CompositeObject `xml:"CompositeObject"`
	// PageBlock 页面块中的嵌套页面对象块，按类型分组保存。
	PageBlock []PageBlock `xml:"PageBlock"`
	// Items 页面对象的文档顺序列表，用于保持原始绘制顺序。
	Items []PageItem
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
				o.CtText.CTGraphicUnit.normalizeDrawParams()
				p.TextObject = append(p.TextObject, o)
				item.Kind = PageItemText
				item.Text = o
			case "PathObject":
				var o PathObject
				if err := d.DecodeElement(&o, &elem); err != nil {
					return err
				}
				o.CtPath.CTGraphicUnit.normalizeDrawParams()
				p.PathObject = append(p.PathObject, o)
				item.Kind = PageItemPath
				item.Path = o
			case "ImageObject":
				var o ImageObject
				if err := d.DecodeElement(&o, &elem); err != nil {
					return err
				}
				o.CtImage.CTGraphicUnit.normalizeDrawParams()
				p.ImageObject = append(p.ImageObject, o)
				item.Kind = PageItemImage
				item.Image = o
			case "CompositeObject":
				var o CompositeObject
				if err := d.DecodeElement(&o, &elem); err != nil {
					return err
				}
				o.CtComposite.CTGraphicUnit.normalizeDrawParams()
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
	// ID 文字对象标识，在页面内唯一。
	ID StID `xml:"ID,attr"`
	// CtText 文字图元的通用属性和文字内容。
	CtText
}

// PathObject 页面中的路径对象。
type PathObject struct {
	// ID 路径对象标识，在页面内唯一。
	ID StID `xml:"ID,attr"`
	// CtPath 路径图元的通用属性和路径数据。
	CtPath
}

// ImageObject 页面中的图像对象。
type ImageObject struct {
	// ID 图像对象标识，在页面内唯一。
	ID StID `xml:"ID,attr"`
	// CtImage 图像图元的通用属性和资源引用。
	CtImage
}

// CompositeObject 页面中的复合对象。
type CompositeObject struct {
	// ID 复合对象标识，在页面内唯一。
	ID StID `xml:"ID,attr"`
	// CtComposite 复合图元的通用属性和资源引用。
	CtComposite
}

// PageBlock 页面对象块，可以包含嵌套的页面对象。
type PageBlock struct {
	// ID 页面对象块标识，在页面内唯一。
	ID StID `xml:"ID,attr"`
	// CTPageBlock 页面对象块中的子对象。
	CTPageBlock
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
	// Type 图层类型，可为 Body、Background、Foreground 或 Custom。
	Type string `xml:"Type,attr,omitempty"`
	// DrawParam 图层使用的绘制参数引用。
	DrawParam StRefID `xml:"DrawParam,attr,omitempty"`
	// CTPageBlock 图层中的页面对象。
	CTPageBlock
}

// CTGraphicUnit 图元通用属性定义。
type CTGraphicUnit struct {
	// Actions 图元触发的动作集合。
	Actions *Actions `xml:"Actions"`
	// Clips 图元使用的裁剪区域集合。
	Clips *Clips `xml:"Clips"`
	// Boundary 图元的边界框，单位为毫米。
	Boundary StBox `xml:"Boundary,attr"`
	// Name 图元名称。
	Name string `xml:"Name,attr,omitempty"`
	// Visible 像素是否可见，未指定时默认为 true。
	Visible OptionalBool `xml:"Visible,attr,omitempty"`
	// CTM 图元使用的坐标变换矩阵。
	CTM *CTM `xml:"CTM,attr,omitempty"`
	// DrawParam 图元使用的绘制参数引用。
	DrawParam StRefID `xml:"DrawParam,attr,omitempty"`
	// LineWidth 线宽，单位为毫米。
	LineWidth float64 `xml:"LineWidth,attr,omitempty"`
	// 线端点样式，枚举值，指定了一条线的端点样式。默认值为 Butt
	Cap  string `xml:"Cap,attr,omitempty"`  // Butt, Round, Square
	Join string `xml:"Join,attr,omitempty"` // Miter, Round, Bevel
	// Join 为 Miter 时小角度 JoinSize 的截断值，默认值为 3.528。当 Join 不等于 Miter 时该参数无效
	MiterLimit float64 `xml:"MiterLimit,attr,omitempty"`
	// DashOffset 虚线模式的起始偏移量。
	DashOffset float64 `xml:"DashOffset,attr,omitempty"`
	// DashPattern 虚线模式中实线段和空白段的长度数组。
	DashPattern *StArrayF `xml:"DashPattern,attr,omitempty"`
	// Alpha 图元整体透明度，取值范围为 0 到 255。
	Alpha *uint8 `xml:"Alpha,attr,omitempty"`

	// 兼容将这些属性编码为子元素的文档。
	DrawParamElement   *StID     `xml:"DrawParam,omitempty"`
	LineWidthElement   *float64  `xml:"LineWidth,omitempty"`
	CapElement         *string   `xml:"Cap,omitempty"`
	JoinElement        *string   `xml:"Join,omitempty"`
	MiterLimitElement  *float64  `xml:"MiterLimit,omitempty"`
	DashOffsetElement  *float64  `xml:"DashOffset,omitempty"`
	DashPatternElement *StArrayF `xml:"DashPattern,omitempty"`
}

// normalizeDrawParams 将子元素形式的绘制参数复制到渲染器使用的通用属性中。
func (g *CTGraphicUnit) normalizeDrawParams() {
	if g.DrawParamElement != nil {
		g.DrawParam = StRefID(*g.DrawParamElement)
	}
	if g.LineWidthElement != nil {
		g.LineWidth = *g.LineWidthElement
	}
	if g.CapElement != nil {
		g.Cap = *g.CapElement
	}
	if g.JoinElement != nil {
		g.Join = *g.JoinElement
	}
	if g.MiterLimitElement != nil {
		g.MiterLimit = *g.MiterLimitElement
	}
	if g.DashOffsetElement != nil {
		g.DashOffset = *g.DashOffsetElement
	}
	if g.DashPatternElement != nil {
		g.DashPattern = g.DashPatternElement
	}
}

// VisibleValue 返回图元是否可见。OFD 未指定 Visible 时默认为可见。
func (g CTGraphicUnit) VisibleValue() bool {
	return g.Visible.Value(true)
}

// Clips 图元裁剪集合。
type Clips struct {
	// TransFlag 是否对裁剪区域应用透明处理。
	TransFlag *bool `xml:"TransFlag,attr,omitempty"`
	// Clip 裁剪定义列表。
	Clip []CtClip `xml:"Clip"`
}

// CtText 文字图元定义。
type CtText struct {
	// CTGraphicUnit 文字图元的通用属性。
	CTGraphicUnit
	// FillColor 文字填充颜色。
	FillColor *CTColor `xml:"FillColor"`
	// StrokeColor 文字描边颜色。
	StrokeColor *CTColor `xml:"StrokeColor"`
	// CGTransform 字符图形变换列表。
	CGTransform []CTCGTransform `xml:"CGTransform"`
	// TextCode 文字编码及定位信息列表。
	TextCode []TextCode `xml:"TextCode"`
	// Font 文字使用的字体引用。
	Font StRefID `xml:"Font,attr"`
	// Size 字号，单位为毫米。
	Size float64 `xml:"Size,attr"`
	// Stroke 是否描边，默认值为 false；当文字对象被裁剪区引用时此属性被忽略。
	Stroke bool `xml:"Stroke,attr,omitempty"`
	// Fill 是否填充，默认值为 true；当文字对象被裁剪区引用时此属性被忽略。
	Fill string `xml:"Fill,attr,omitempty"`
	// HScale 字形在水平方向的缩放比，取值为 [0, 1.0]，默认值为 1.0。
	// 例如，HScale 为 0.5 时表示实际显示的字宽为原来字宽的一半。
	HScale float64 `xml:"HScale,attr,omitempty"`
	// ReadDirection 阅读方向，指定文字排列的方向，默认值为 0。
	ReadDirection int `xml:"ReadDirection,attr,omitempty"`
	// CharDirection 字符方向，指定文字放置的方式，默认值为 0。
	CharDirection int `xml:"CharDirection,attr,omitempty"`
	// Weight 字重，通常取 0、100 至 1000 之间的值。
	Weight int `xml:"Weight,attr,omitempty"` // 0,100,...,1000
	// Italic 是否为斜体样式，默认值为 false。
	Italic bool `xml:"Italic,attr,omitempty"`
}

// CTCGTransform 字符图形变换定义。
type CTCGTransform struct {
	// Glyphs 变换关系中字型索引列表
	Glyphs StArrayI `xml:"Glyphs"`
	// CodePosition 变换起始字符在 TextCode 中的位置。
	CodePosition int `xml:"CodePosition,attr"`
	// CodeCount 参与变换的字符数量。
	CodeCount int `xml:"CodeCount,attr,omitempty"`
	// GlyphCount 参与变换的字形数量。
	GlyphCount int `xml:"GlyphCount,attr,omitempty"`
}

// TextCode 文字编码及其定位信息。
type TextCode struct {
	// Value 文字内容。
	Value string `xml:",chardata"`
	// X 文字起始位置的横坐标，单位为毫米。
	X float64 `xml:"X,attr,omitempty"`
	// Y 文字起始位置的纵坐标，单位为毫米。
	Y float64 `xml:"Y,attr,omitempty"`
	// DeltaX 字符在水平方向上的位置增量。
	DeltaX StArrayF `xml:"DeltaX,attr,omitempty"`
	// DeltaY 字符在垂直方向上的位置增量。
	DeltaY StArrayF `xml:"DeltaY,attr,omitempty"`
}

// CtImage 图像图元定义。
type CtImage struct {
	// CTGraphicUnit 图像图元的通用属性。
	CTGraphicUnit
	// Border 图像边框定义。
	Border *Border `xml:"Border"`
	// ResourceID 图像资源标识。
	ResourceID StRefID `xml:"ResourceID,attr"`
	// Substitution 替代图像资源标识。
	Substitution StRefID `xml:"Substitution,attr,omitempty"`
	// ImageMask 图像蒙版资源标识。
	ImageMask StRefID `xml:"ImageMask,attr,omitempty"`
}

// Border 图像边框定义。
type Border struct {
	// BorderColor 边框颜色。
	BorderColor *CTColor `xml:"BorderColor"`
	// LineWidth 边框线宽，单位为毫米。
	LineWidth float64 `xml:"LineWidth,attr,omitempty"`
	// HorizonalCornerRadius 水平方向圆角半径，单位为毫米。
	HorizonalCornerRadius float64 `xml:"HorizonalCornerRadius,attr,omitempty"`
	// VerticalCornerRadius 垂直方向圆角半径，单位为毫米。
	VerticalCornerRadius float64 `xml:"VerticalCornerRadius,attr,omitempty"`
	// DashOffset 虚线模式的起始偏移量。
	DashOffset float64 `xml:"DashOffset,attr,omitempty"`
	// DashPattern 虚线模式中实线段和空白段的长度数组。
	DashPattern StArray `xml:"DashPattern,attr,omitempty"`
}

// CtComposite 复合图元定义。
type CtComposite struct {
	// CTGraphicUnit 复合图元的通用属性。
	CTGraphicUnit
	// ResourceID 复合图形资源标识。
	ResourceID StRefID `xml:"ResourceID,attr"`
}

// CtPath 路径图元定义。
type CtPath struct {
	// CTGraphicUnit 路径图元的通用属性。
	CTGraphicUnit
	// StrokeColor 路径描边颜色。
	StrokeColor *CTColor `xml:"StrokeColor"`
	// FillColor 路径填充颜色。
	FillColor *CTColor `xml:"FillColor"`
	// AbbreviatedData 路径的 SVG 简写数据。
	AbbreviatedData SVGPath `xml:"AbbreviatedData"`
	// Stroke 是否描边，默认值为 true。
	Stroke string `xml:"Stroke,attr,omitempty"`
	// Fill 是否填充路径。
	Fill bool `xml:"Fill,attr,omitempty"`
	// Rule 填充规则，可为 NonZero 或 Even-Odd。
	Rule string `xml:"Rule,attr,omitempty"` // NonZero, Even-Odd
}

// CtPattern 图案填充定义。
type CtPattern struct {
	// CellContent 图案单元中的内容。
	CellContent CellContent `xml:"CellContent"`
	// Width 图案单元宽度，单位为毫米。
	Width float64 `xml:"Width,attr"`
	// Height 图案单元高度，单位为毫米。
	Height float64 `xml:"Height,attr"`
	// XStep 图案在水平方向上的重复步长，单位为毫米。
	XStep float64 `xml:"XStep,attr,omitempty"`
	// YStep 图案在垂直方向上的重复步长，单位为毫米。
	YStep float64 `xml:"YStep,attr,omitempty"`
	// ReflectMethod 图案重复时的镜像方式，可为 Normal、Row、Column 或 RowAndColumn。
	ReflectMethod string `xml:"ReflectMethod,attr,omitempty"` // Normal, Row, Column, RowAndColumn
	// RelativeTo 图案坐标的参考对象，可为 Page 或 Object。
	RelativeTo string `xml:"RelativeTo,attr,omitempty"` // Page, Object
	// CTM 图案单元使用的坐标变换矩阵。
	CTM StArray `xml:"CTM,attr,omitempty"`
}

// CellContent 图案填充单元的内容。
type CellContent struct {
	// Thumbnail 图案单元缩略图资源引用。
	Thumbnail StRefID `xml:"Thumbnail,attr,omitempty"`
	// CTPageBlock 图案单元中的页面对象。
	CTPageBlock
}

// CTAxialShd 轴向渐变填充定义。
type CTAxialShd struct {
	// Segment 渐变颜色分段列表。
	Segment []Segment `xml:"Segment"`
	// MapType 渐变映射方式，可为 Direct、Repeat 或 Reflect。
	MapType string `xml:"MapType,attr,omitempty"` // Direct, Repeat, Reflect
	// MapUnit 渐变映射单元长度。
	MapUnit float64 `xml:"MapUnit,attr,omitempty"`
	// Extend 渐变起止范围外的延伸方式，可为 0、1、2 或 3。
	Extend int `xml:"Extend,attr,omitempty"` // 0,1,2,3
	// StartPoint 渐变起点。
	StartPoint StPos `xml:"StartPoint,attr"`
	// EndPoint 渐变终点。
	EndPoint StPos `xml:"EndPoint,attr"`
}

// CTRadialShd 径向渐变填充定义。
type CTRadialShd struct {
	// Segment 渐变颜色分段列表。
	Segment []Segment `xml:"Segment"`
	// MapType 渐变映射方式，可为 Direct、Repeat 或 Reflect。
	MapType string `xml:"MapType,attr,omitempty"` // Direct, Repeat, Reflect
	// MapUnit 渐变映射单元长度。
	MapUnit float64 `xml:"MapUnit,attr,omitempty"`
	// Eccentricity 径向渐变的离心率。
	Eccentricity float64 `xml:"Eccentricity,attr,omitempty"`
	// Angle 径向渐变的旋转角度。
	Angle float64 `xml:"Angle,attr,omitempty"`
	// StartPoint 起始圆心位置。
	StartPoint StPos `xml:"StartPoint,attr"`
	// StartRadius 起始半径，单位为毫米。
	StartRadius float64 `xml:"StartRadius,attr,omitempty"`
	// EndPoint 结束圆心位置。
	EndPoint StPos `xml:"EndPoint,attr"`
	// EndRadius 结束半径，单位为毫米。
	EndRadius float64 `xml:"EndRadius,attr"`
	// Extend 渐变起止范围外的延伸方式。
	Extend int `xml:"Extend,attr,omitempty"`
}

// CTGouraudShd Gouraud 三角网格渐变填充定义。
type CTGouraudShd struct {
	// Point Gouraud 网格控制点列表。
	Point []GouraudPoint `xml:"Point"`
	// BackColor 网格背景颜色。
	BackColor *CTColor `xml:"BackColor,omitempty"`
	// Extend 渐变起止范围外的延伸方式。
	Extend int `xml:"Extend,attr,omitempty"`
}

// GouraudPoint Gouraud 渐变控制点。
type GouraudPoint struct {
	// Color 控制点颜色。
	Color CTColor `xml:"Color"`
	// X 控制点横坐标。
	X float64 `xml:"X,attr"`
	// Y 控制点纵坐标。
	Y float64 `xml:"Y,attr"`
	// EdgeFlag 网格边缘标志，可为 0、1 或 2。
	EdgeFlag int `xml:"EdgeFlag,attr"` // 0,1,2
}

// CTLaGouraudShd Gouraud 网格渐变填充定义。
type CTLaGouraudShd struct {
	// Point Gouraud 网格控制点列表。
	Point []LaGouraudPoint `xml:"Point"`
	// BackColor 网格背景颜色。
	BackColor *CTColor `xml:"BackColor,omitempty"`
	// VerticesPerRow 每行的顶点数量。
	VerticesPerRow int `xml:"VerticesPerRow,attr"`
	// Extend 渐变起止范围外的延伸方式。
	Extend int `xml:"Extend,attr,omitempty"`
}

// LaGouraudPoint Gouraud 网格渐变控制点。
type LaGouraudPoint struct {
	// Color 控制点颜色。
	Color CTColor `xml:"Color"`
	// X 控制点横坐标。
	X float64 `xml:"X,attr,omitempty"`
	// Y 控制点纵坐标。
	Y float64 `xml:"Y,attr,omitempty"`
}

// CTColor 颜色及颜色填充定义。
type CTColor struct {
	// Pattern 图案填充定义。
	Pattern *CtPattern `xml:"Pattern"`
	// AxialShd 轴向渐变填充定义。
	AxialShd *CTAxialShd `xml:"AxialShd"`
	// RadialShd 径向渐变填充定义。
	RadialShd *CTRadialShd `xml:"RadialShd"`
	// GouraudShd Gouraud 三角网格渐变填充定义。
	GouraudShd *CTGouraudShd `xml:"GouraudShd"`
	// LaGourandShd 规范 XML Schema 中的字段拼写，对应 LaGouraudShd。
	LaGourandShd *CTLaGouraudShd `xml:"LaGourandShd"`
	// LaGouraudShd 是标准正文中的拼写，LaGourandShd 是规范 XML Schema 中的拼写。
	LaGouraudShd *CTLaGouraudShd `xml:"LaGouraudShd"`
	// Value 颜色值，指定了当前颜色空间下各通道的取值。
	//Value的取值应符合"通道 1 通道 2 通道 3 …"格式。
	//此属性不出现时，应参考 Index 属性从颜色空间的调色版中取值。当二者都不出现时，该颜色各通道的值全部为 0
	//
	//可选
	Value *Color `xml:"Value,attr,omitempty"`
	// Index 调色板中颜色的编号，非负整数，将从当前颜色空间的调色板中取出相应索引的预定义颜色用来绘制。
	//索引从0开始 可选
	Index int `xml:"Index,attr,omitempty"`
	// ColorSpace 颜色空间资源引用。
	ColorSpace StRefID `xml:"ColorSpace,attr,omitempty"`
	// Alpha 颜色透明度，在 0~255 之间取值。默认为 255，表示完全不透明
	//
	//可选
	Alpha *uint8 `xml:"Alpha,attr,omitempty"`
}

// Segment 渐变中的颜色分段。
type Segment struct {
	// Color 当前分段的颜色。
	Color CTColor `xml:"Color"`
	// Position 当前颜色在渐变中的位置。
	Position float64 `xml:"Position,attr,omitempty"`
}
