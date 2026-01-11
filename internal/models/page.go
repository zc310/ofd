package models

type Page struct {
	ID      StID  `xml:"ID,attr"`
	BaseLoc StLoc `xml:"BaseLoc,attr"`
}
type PageContent struct {
	Template []Template  `xml:"Template"`
	PageRes  []StLoc     `xml:"PageRes"`
	Area     *CtPageArea `xml:"Area"`
	Content  *Content    `xml:"Content"`
	Actions  *Actions    `xml:"Actions"`
}

type Template struct {
	TemplateID StRefID `xml:"TemplateID,attr"`
	ZOrder     string  `xml:"ZOrder,attr,omitempty"` // Background or Foreground
}

type Content struct {
	Layer []*Layer `xml:"Layer"`
}

type Layer struct {
	ID        StID    `xml:"ID,attr"`
	Type      string  `xml:"Type,attr,omitempty"` // Body, Background, Foreground, Custom
	DrawParam StRefID `xml:"DrawParam,attr,omitempty"`
	// CTPageBlock 内容
	TextObject      []TextObject      `xml:"TextObject"`
	PathObject      []PathObject      `xml:"PathObject"`
	ImageObject     []ImageObject     `xml:"ImageObject"`
	CompositeObject []CompositeObject `xml:"CompositeObject"`
	PageBlock       []PageBlock       `xml:"PageBlock"`
}

type Actions struct {
	Action []CtAction `xml:"Action"`
}

type CtClip struct {
	Area []ClipArea `xml:"Area"`
}

type ClipArea struct {
	Path      *CtPath  `xml:"Path"`
	Text      *CtText  `xml:"Text"`
	DrawParam *StRefID `xml:"DrawParam,attr,omitempty"`
	CTM       *CTM     `xml:"CTM,attr,omitempty"`
}

type CTPageBlock struct {
	TextObject      []TextObject      `xml:"TextObject"`
	PathObject      []PathObject      `xml:"PathObject"`
	ImageObject     []ImageObject     `xml:"ImageObject"`
	CompositeObject []CompositeObject `xml:"CompositeObject"`
	PageBlock       []PageBlock       `xml:"PageBlock"`
}

type TextObject struct {
	ID     StID `xml:"ID,attr"`
	CtText      // 嵌入CT_Text
}

type PathObject struct {
	ID     StID `xml:"ID,attr"`
	CtPath      // 嵌入CT_Path
}

type ImageObject struct {
	ID      StID `xml:"ID,attr"`
	CtImage      // 嵌入CT_Image
}

type CompositeObject struct {
	ID          StID `xml:"ID,attr"`
	CtComposite      // 嵌入CT_Composite
}

type PageBlock struct {
	ID          StID `xml:"ID,attr"`
	CTPageBlock      // 嵌入CT_PageBlock
}

type CtLayer struct {
	Type      string  `xml:"Type,attr,omitempty"`
	DrawParam StRefID `xml:"DrawParam,attr,omitempty"`
	CTPageBlock
}

type CTGraphicUnit struct {
	Actions   *Actions `xml:"Actions"`
	Clips     *Clips   `xml:"Clips"`
	Boundary  StBox    `xml:"Boundary,attr"`
	Name      string   `xml:"Name,attr,omitempty"`
	Visible   bool     `xml:"Visible,attr,omitempty"`
	CTM       *CTM     `xml:"CTM,attr,omitempty"`
	DrawParam StRefID  `xml:"DrawParam,attr,omitempty"`
	LineWidth float64  `xml:"LineWidth,attr,omitempty"`
	// 线端点样式，枚举值，指定了一条线的端点样式。默认值为 Butt
	Cap  string `xml:"Cap,attr,omitempty"`  // Butt, Round, Square
	Join string `xml:"Join,attr,omitempty"` // Miter, Round, Bevel
	// Join 为 Miter 时小角度 JoinSize 的截断值，默认值为 3.528。当 Join 不等于 Miter 时该参数无效
	MiterLimit  float64   `xml:"MiterLimit,attr,omitempty"`
	DashOffset  float64   `xml:"DashOffset,attr,omitempty"`
	DashPattern *StArrayF `xml:"DashPattern,attr,omitempty"`
	Alpha       *uint8    `xml:"Alpha,attr,omitempty"`
}

type Clips struct {
	Clip []CtClip `xml:"Clip"`
}

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

type CTCGTransform struct {
	// Glyphs 变换关系中字型索引列表
	Glyphs       StArrayI `xml:"Glyphs"`
	CodePosition int      `xml:"CodePosition,attr"`
	CodeCount    int      `xml:"CodeCount,attr,omitempty"`
	GlyphCount   int      `xml:"GlyphCount,attr,omitempty"`
}

type TextCode struct {
	Value  string   `xml:",chardata"`
	X      float64  `xml:"X,attr,omitempty"`
	Y      float64  `xml:"Y,attr,omitempty"`
	DeltaX StArrayF `xml:"DeltaX,attr,omitempty"`
	DeltaY StArrayF `xml:"DeltaY,attr,omitempty"`
}

type CtImage struct {
	CTGraphicUnit
	Border       *Border `xml:"Border"`
	ResourceID   StRefID `xml:"ResourceID,attr"`
	Substitution StRefID `xml:"Substitution,attr,omitempty"`
	ImageMask    StRefID `xml:"ImageMask,attr,omitempty"`
}

type Border struct {
	BorderColor           *CTColor `xml:"BorderColor"`
	LineWidth             float64  `xml:"LineWidth,attr,omitempty"`
	HorizonalCornerRadius float64  `xml:"HorizonalCornerRadius,attr,omitempty"`
	VerticalCornerRadius  float64  `xml:"VerticalCornerRadius,attr,omitempty"`
	DashOffset            float64  `xml:"DashOffset,attr,omitempty"`
	DashPattern           StArray  `xml:"DashPattern,attr,omitempty"`
}

type CtComposite struct {
	CTGraphicUnit
	ResourceID StRefID `xml:"ResourceID,attr"`
}

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

type CellContent struct {
	Thumbnail StRefID `xml:"Thumbnail,attr,omitempty"`
	CTPageBlock
}

type CTAxialShd struct {
	Segment    []Segment `xml:"Segment"`
	MapType    string    `xml:"MapType,attr,omitempty"` // Direct, Repeat, Reflect
	MapUnit    float64   `xml:"MapUnit,attr,omitempty"`
	Extend     int       `xml:"Extend,attr,omitempty"` // 0,1,2,3
	StartPoint StPos     `xml:"StartPoint,attr"`
	EndPoint   StPos     `xml:"EndPoint,attr"`
}

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

type CTGouraudShd struct {
	Point     []GouraudPoint `xml:"Point"`
	BackColor *CTColor       `xml:"BackColor,omitempty"`
	Extend    int            `xml:"Extend,attr,omitempty"`
}

type GouraudPoint struct {
	Color    CTColor `xml:"Color"`
	X        float64 `xml:"X,attr"`
	Y        float64 `xml:"Y,attr"`
	EdgeFlag int     `xml:"EdgeFlag,attr"` // 0,1,2
}

type CTLaGouraudShd struct {
	Point          []LaGouraudPoint `xml:"Point"`
	BackColor      *CTColor         `xml:"BackColor,omitempty"`
	VerticesPerRow int              `xml:"VerticesPerRow,attr"`
	Extend         int              `xml:"Extend,attr,omitempty"`
}

type LaGouraudPoint struct {
	Color CTColor `xml:"Color"`
	X     float64 `xml:"X,attr,omitempty"`
	Y     float64 `xml:"Y,attr,omitempty"`
}

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

type Segment struct {
	Color    CTColor `xml:"Color"`
	Position float64 `xml:"Position,attr,omitempty"`
}
