package creator

import "time"

// A4 是毫米单位的标准 A4 页面尺寸。
var A4 = PageSize{Width: 210, Height: 297}

// PageSize 描述页面尺寸，单位为毫米。
type PageSize struct {
	Width  float64
	Height float64
}

// Box 描述页面区域或图元边界，单位为毫米。
type Box struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// PageArea 描述页面的物理区域、应用区域、内容区域和出血区域。
// PhysicalBox 为空时使用文档的页面尺寸。
type PageArea struct {
	PhysicalBox    *Box
	ApplicationBox *Box
	ContentBox     *Box
	BleedBox       *Box
}

// Document 是 OFD 文件包的输入模型。
// 页面、对象坐标、对象尺寸和文字字号的单位均为毫米。
type Document struct {
	ID             string
	Title          string
	Author         string
	Subject        string
	Abstract       string
	DocUsage       string
	Cover          string
	CoverData      []byte
	CoverName      string
	Keywords       []string
	CustomDatas    []CustomData
	Creator        string
	CreatorVersion string
	CreationDate   time.Time
	ModDate        time.Time
	PageSize       PageSize
	Area           *PageArea
	DrawParams     []DrawParam
	Fonts          []Font
	ColorSpaces    []ColorSpace
	DefaultCS      uint64
	PublicRes      []PublicResource
	Media          []Media
	Composites     []CompositeGraphicUnit
	Pages          []Page
	Templates      []TemplatePage
	Actions        []Action
	Outlines       []Outline
	Permissions    *Permissions
	Preferences    *ViewPreferences
	Bookmarks      []Bookmark
	Annotations    []AnnotationPage
	Attachments    []Attachment
	CustomTags     []CustomTag
	Extensions     []Extension
	Signatures     []Signature
	Versions       []DocumentVersion
}

// Attachment 描述文档附件及其包内数据。
type Attachment struct {
	ID           string
	Name         string
	Format       string
	CreationDate time.Time
	ModDate      time.Time
	Visible      *bool
	Usage        string
	Data         []byte
	FileName     string
}

// CustomTag 描述一个自定义标签清单及其数据文件。
type CustomTag struct {
	NameSpace  string
	Schema     []byte
	SchemaName string
	Data       []byte
	DataName   string
}

// CustomData 描述 DocInfo 中的自定义元数据。
type CustomData struct {
	Name  string
	Value string
}

// PublicResource 描述一个嵌入 OFD 包的公共资源 XML 文件。
// Name 是相对于 Doc_0 的文件名，Data 应为 Res 根元素的 XML 内容。
type PublicResource struct {
	Name  string
	Data  []byte
	Files []PublicResourceFile
}

// PublicResourceFile 描述公共资源 XML 引用的包内文件。
// Path 相对于公共资源 XML 所在目录。
type PublicResourceFile struct {
	Path string
	Data []byte
}

// Extension 描述文档扩展及其属性、内联数据或外部数据文件。
type Extension struct {
	AppName    string
	Company    string
	AppVersion string
	Date       time.Time
	RefID      uint64
	Properties []ExtensionProperty
	Data       string
	// DataXML 是 Data 元素中的原始 XML 内容。设置后会作为子元素写入，
	// 与 Data 互斥；Data 适合只需要文本内容的扩展。
	DataXML  []byte
	DataFile []byte
	DataName string
}

// ExtensionProperty 描述扩展的键值属性。
type ExtensionProperty struct {
	Name  string
	Type  string
	Value string
}

// Signature 描述一个 OFD 签名清单项及其签名文件。
type Signature struct {
	ID              string
	Type            string
	ProviderName    string
	ProviderVersion string
	Company         string
	Method          string
	Date            time.Time
	CheckMethod     string
	References      []SignatureReference
	StampAnnots     []SignatureStamp
	SealFile        []byte
	SealName        string
	SignedValue     []byte
	SignedValueName string
}

// SignatureReference 描述签名摘要引用。FileRef 相对于签名 XML 文件。
type SignatureReference struct {
	FileRef    string
	CheckValue []byte
}

// SignatureStamp 描述签名在页面上的盖章区域。
type SignatureStamp struct {
	ID       string
	Page     int
	Boundary Box
	Clip     *Box
}

// DocumentVersion 描述文档的一个版本及其文件清单。
type DocumentVersion struct {
	ID           string
	Index        int
	Current      bool
	Version      string
	Name         string
	CreationDate time.Time
	Files        []VersionFile
	DocRoot      []byte
	DocRootName  string
}

// VersionFile 描述文档版本中的文件。
type VersionFile struct {
	ID   string
	Path string
}

// DrawParam 描述可被图层或图元引用的绘制参数资源。
// Relative 使用另一个绘制参数的 Name 作为继承来源。
type DrawParam struct {
	Name        string
	Relative    string
	LineWidth   float64
	Join        string
	Cap         string
	DashOffset  float64
	DashPattern []float64
	MiterLimit  float64
	FillColor   *Color
	StrokeColor *Color
}

// Font 描述文档字体资源，Name 必须与 Text.Font 的值一致。
// Data 非空时，字体数据会嵌入 OFD 文件包。
type Font struct {
	Name       string
	FamilyName string
	Charset    string
	Italic     bool
	Bold       bool
	Serif      bool
	FixedWidth bool
	Format     string
	Data       []byte
}

// Page 按绘制顺序保存页面对象。
type Page struct {
	Area      *PageArea
	Resources []PageResource
	Templates []TemplateRef
	LayerType string
	Items     []Item
	Layers    []Layer
	Actions   []Action
}

// PageResource 描述页面专属的资源文件。Images 适合快速创建页面图片资源；
// Data 可用于完整的 OFD Res 资源集合。
type PageResource struct {
	Images []PageImage
	// Data 是完整的 OFD Res XML。用于页面专属的颜色空间、字体、绘制参数、
	// 多媒体或复合图元等资源；设置后不能同时设置 Images。
	Data []byte
	// Files 是 Data 中引用的、相对于 PageRes XML 所在目录的二进制文件。
	Files []PageResourceFile
}

// PageResourceFile 描述页面资源 XML 引用的包内文件。
type PageResourceFile struct {
	Path string
	Data []byte
}

// PageImage 描述页面资源中的图片。ID 在文档内必须唯一。
type PageImage struct {
	ID     uint64
	Format string
	Data   []byte
	Name   string
}

// TemplatePage 描述文档模板页及其页面内容。
// ID 必须在文档内唯一，页面通过该 ID 引用模板页。
type TemplatePage struct {
	ID     uint64
	Name   string
	ZOrder string
	Area   *PageArea
	Items  []Item
	Layers []Layer
}

// TemplateRef 描述页面使用的模板页引用。
type TemplateRef struct {
	ID     uint64
	ZOrder string
}

// Layer 表示页面中的一个图层，图层顺序就是绘制顺序。
type Layer struct {
	Type      string
	DrawParam string
	Items     []Item
}

const (
	// LayerBody 表示普通正文图层。
	LayerBody = "Body"
	// LayerBackground 表示背景图层。
	LayerBackground = "Background"
	// LayerForeground 表示前景图层。
	LayerForeground = "Foreground"
	// LayerCustom 表示自定义图层。
	LayerCustom = "Custom"
)

// Item 是 OFD 页面上的对象，可使用 Text、Path、Image、Composite 或 PageBlock。
type Item interface {
	isItem()
}

// Text 是文字对象，Size 表示字号，单位为毫米。
type Text struct {
	X             float64
	Y             float64
	Width         float64
	Height        float64
	Value         string
	Font          string
	Size          float64
	Visible       *bool
	Stroke        bool
	Fill          *bool
	HScale        float64
	ReadDirection int
	CharDirection int
	Weight        int
	Italic        bool
	DrawParam     string
	CTM           *CTM
	TextCodes     []TextCode
	CGTransforms  []CGTransform
	Actions       []Action
	Clips         *Clips
	FillColor     *Color
	StrokeColor   *Color
}

// TextCode 描述文字对象中的一段文字及其定位信息。
// X、Y 为空时沿用文字对象的排版位置；DeltaX、DeltaY 表示字符位置增量。
type TextCode struct {
	Value  string
	X      *float64
	Y      *float64
	DeltaX []float64
	DeltaY []float64
}

// CGTransform 描述文字分段中的字形映射关系。
// CodePosition 是相对于整个文字对象的字符位置，从 0 开始。
type CGTransform struct {
	CodePosition int
	CodeCount    int
	GlyphCount   int
	Glyphs       []int
}

// CTM 是 OFD 六参数坐标变换矩阵，顺序为 a、b、c、d、e、f。
type CTM [6]float64

// Clips 描述图元使用的裁剪区域集合。
type Clips struct {
	Items []Clip
}

// Clip 描述一个裁剪区域定义。
type Clip struct {
	Areas []ClipArea
}

// ClipArea 描述一个路径或文字裁剪区域。
type ClipArea struct {
	DrawParam string
	CTM       *CTM
	Path      *ClipPath
	Text      *ClipText
}

// ClipPath 是裁剪区域中的路径，Boundary 为必填边界。
type ClipPath struct {
	Boundary    Box
	Name        string
	Visible     *bool
	CTM         *CTM
	Data        string
	Stroke      bool
	StrokeSet   *bool
	Fill        bool
	Rule        string
	LineWidth   float64
	Cap         string
	Join        string
	MiterLimit  float64
	DashOffset  float64
	DashPattern []float64
	Alpha       *uint8
	StrokeColor *Color
	FillColor   *Color
}

// ClipText 是裁剪区域中的文字，Font 为空时使用 SimSun。
type ClipText struct {
	Boundary      Box
	CTM           *CTM
	Font          string
	Size          float64
	Value         string
	TextCodes     []TextCode
	Stroke        bool
	Fill          *bool
	HScale        float64
	ReadDirection int
	CharDirection int
	Weight        int
	Italic        bool
	FillColor     *Color
	StrokeColor   *Color
}

func (Text) isItem() {}

// Path 是路径对象。Data 使用 OFD 缩略路径语法，例如
// "M 10 10 L 100 10 L 100 100 C"。
type Path struct {
	X           float64
	Y           float64
	Width       float64
	Height      float64
	Data        string
	Stroke      bool
	StrokeSet   *bool
	Fill        bool
	Rule        string
	Name        string
	Visible     *bool
	LineWidth   float64
	Cap         string
	Join        string
	MiterLimit  float64
	DashOffset  float64
	DashPattern []float64
	Alpha       *uint8
	DrawParam   string
	Actions     []Action
	CTM         *CTM
	Clips       *Clips
	StrokeColor *Color
	FillColor   *Color
}

func (Path) isItem() {}

// Image 是图片对象，Data 保存编码后的图片数据。
// Format 是 PNG 或 JPEG 等 OFD 图片格式；为空时会尽量根据文件头识别。
type Image struct {
	X            float64
	Y            float64
	Width        float64
	Height       float64
	Data         []byte
	ResourceID   uint64
	Substitution uint64
	ImageMask    uint64
	Format       string
	Name         string
	Visible      *bool
	LineWidth    float64
	Cap          string
	Join         string
	MiterLimit   float64
	DashOffset   float64
	DashPattern  []float64
	Alpha        *uint8
	DrawParam    string
	Actions      []Action
	CTM          *CTM
	Clips        *Clips
	Border       *ImageBorder
}

// ImageBorder 描述图片边框的线宽、圆角和虚线样式。
type ImageBorder struct {
	LineWidth        float64
	HorizontalRadius float64
	VerticalRadius   float64
	DashOffset       float64
	DashPattern      []float64
	Color            *Color
}

// Color 描述颜色值或渐变填充。未设置 Components、ColorSpace 和渐变时使用 RGB。
type Color struct {
	R          uint8
	G          uint8
	B          uint8
	Components []int
	ColorSpace uint64
	Index      *int
	Alpha      *uint8
	Axial      *AxialShading
	Radial     *RadialShading
	Gouraud    *GouraudShading
	LaGouraud  *LaGouraudShading
	Pattern    *Pattern
}

// Pattern 描述用于填充颜色的平铺图案。
// 图案单元中的对象使用 Items 或 Layers 配置，坐标单位为毫米。
type Pattern struct {
	Width         float64
	Height        float64
	XStep         float64
	YStep         float64
	ReflectMethod string
	RelativeTo    string
	CTM           *CTM
	Thumbnail     uint64
	Items         []Item
	Layers        []Layer

	builtState *buildState
	builtItems []builtItem
	prepared   bool
	building   bool
}

// ColorSpace 描述 DocumentRes.xml 中的颜色空间资源。
type ColorSpace struct {
	ID               uint64
	Type             string
	BitsPerComponent int
	Profile          string
	ProfileData      []byte
	Palette          []string
}

// ColorStop 描述渐变中的颜色分段。
type ColorStop struct {
	Position float64
	Color    Color
}

// AxialShading 描述轴向渐变。
type AxialShading struct {
	MapType    string
	MapUnit    float64
	Extend     int
	StartPoint string
	EndPoint   string
	Segments   []ColorStop
}

// RadialShading 描述径向渐变。
type RadialShading struct {
	MapType      string
	MapUnit      float64
	Eccentricity float64
	Angle        float64
	StartPoint   string
	StartRadius  float64
	EndPoint     string
	EndRadius    float64
	Extend       int
	Segments     []ColorStop
}

// GouraudShading 描述三角网格渐变。
type GouraudShading struct {
	Extend    int
	Points    []GouraudPoint
	BackColor *Color
}

// GouraudPoint 描述三角网格渐变控制点。
type GouraudPoint struct {
	X        float64
	Y        float64
	EdgeFlag int
	Color    Color
}

// LaGouraudShading 描述四边形网格渐变。
type LaGouraudShading struct {
	VerticesPerRow int
	Extend         int
	Points         []LaGouraudPoint
	BackColor      *Color
}

// LaGouraudPoint 描述四边形网格渐变控制点。
type LaGouraudPoint struct {
	X     float64
	Y     float64
	Color Color
}

// ActionEvent 表示动作触发事件。
type ActionEvent string

const (
	// ActionEventDO 表示文档打开或对象执行事件。
	ActionEventDO ActionEvent = "DO"
	// ActionEventPO 表示页面打开事件。
	ActionEventPO ActionEvent = "PO"
	// ActionEventClick 表示点击事件，也是默认事件。
	ActionEventClick ActionEvent = "CLICK"
)

// Action 描述一个 URI、页面、附件或媒体动作，且只能设置一个动作目标。
type Action struct {
	Event  ActionEvent
	Region *ActionRegion
	URI    *URIAction
	Goto   *GotoAction
	GotoA  *GotoAAction
	Sound  *SoundAction
	Movie  *MovieAction
}

// Point 描述区域路径中的二维点。
type Point struct {
	X float64
	Y float64
}

// ActionRegion 描述动作触发区域。
type ActionRegion struct {
	Areas []ActionArea
}

// ActionArea 描述动作区域中的一条路径。
type ActionArea struct {
	Start    Point
	Commands []RegionCommand
}

// RegionCommand 是动作区域路径命令。
type RegionCommand interface {
	isRegionCommand()
}

// RegionMove 移动到指定点。
type RegionMove struct{ Point Point }

// RegionLine 连接到指定点。
type RegionLine struct{ Point Point }

// RegionQuadraticBezier 描述二次贝塞尔曲线。
type RegionQuadraticBezier struct {
	Control Point
	End     Point
}

// RegionCubicBezier 描述三次贝塞尔曲线。
type RegionCubicBezier struct {
	Control1 Point
	Control2 Point
	End      Point
}

// RegionArc 描述椭圆弧。
type RegionArc struct {
	SweepDirection bool
	LargeArc       bool
	RotationAngle  float64
	EllipseSize    Point
	EndPoint       Point
}

// RegionClose 关闭当前路径。
type RegionClose struct{}

func (RegionMove) isRegionCommand()            {}
func (RegionLine) isRegionCommand()            {}
func (RegionQuadraticBezier) isRegionCommand() {}
func (RegionCubicBezier) isRegionCommand()     {}
func (RegionArc) isRegionCommand()             {}
func (RegionClose) isRegionCommand()           {}

// URIAction 描述外部 URI 动作。
type URIAction struct {
	URI    string
	Base   string
	Target string
}

// GotoAction 描述当前文档内的页面跳转动作。
// Page 是从 0 开始的页面索引；Type 为空时使用 Fit。
type GotoAction struct {
	Page     int
	Bookmark string
	Type     string
	Left     *float64
	Top      *float64
	Right    *float64
	Bottom   *float64
	Zoom     *float64
}

// GotoAAction 描述跳转到文档附件的动作。
type GotoAAction struct {
	AttachID  string
	NewWindow *bool
}

// Media 描述文档级图片、音频或视频资源。
type Media struct {
	ID     uint64
	Type   string
	Format string
	Data   []byte
	Name   string
}

// SoundAction 描述声音播放动作。
type SoundAction struct {
	ResourceID  uint64
	Volume      *int
	Repeat      *bool
	Synchronous *bool
}

// MovieAction 描述影片控制动作。
type MovieAction struct {
	ResourceID uint64
	Operator   string
}

// Outline 描述文档大纲项。大纲动作使用当前文档页面索引作为目标。
type Outline struct {
	Title    string
	Count    *int
	Expanded *bool
	Actions  []Action
	Children []Outline
}

// Bookmark 描述文档书签及其页面目标。
type Bookmark struct {
	Name string
	Goto GotoAction
}

// AnnotationPage 描述某个页面的注解集合，Page 使用从 0 开始的页面索引。
type AnnotationPage struct {
	Page  int
	Items []Annotation
}

// Annotation 描述页面注解及其外观。
type Annotation struct {
	ID          uint64
	Type        string
	Creator     string
	LastModDate time.Time
	Visible     *bool
	Subtype     string
	Print       *bool
	NoZoom      bool
	NoRotate    bool
	ReadOnly    bool
	// ReadOnlyValue 用于显式写出 ReadOnly=false。为空时沿用 ReadOnly 的兼容行为。
	ReadOnlyValue *bool
	Remark        string
	Parameters    []AnnotationParameter
	Boundary      *Box
	Items         []Item
}

// AnnotationParameter 描述注解的自定义参数。
type AnnotationParameter struct {
	Name  string
	Value string
}

// Permissions 描述文档编辑、导出、打印等权限。
type Permissions struct {
	Edit        *bool
	Annot       *bool
	Export      *bool
	Signature   *bool
	Watermark   *bool
	PrintScreen *bool
	Print       *PrintSettings
	ValidPeriod *ValidPeriod
}

// PrintSettings 描述打印权限和允许的打印份数。
type PrintSettings struct {
	Printable bool
	Copies    *int
}

// ValidPeriod 描述权限的生效和失效时间。
type ValidPeriod struct {
	Start time.Time
	End   time.Time
}

// ViewPreferences 描述阅读器打开文档时的显示设置。
type ViewPreferences struct {
	PageMode     string
	PageLayout   string
	TabDisplay   string
	HideToolbar  *bool
	HideMenubar  *bool
	HideWindowUI *bool
	ZoomMode     string
	Zoom         *float64
}

const (
	PageModeNone          = "None"
	PageModeFullScreen    = "FullScreen"
	PageModeUseOutlines   = "UseOutlines"
	PageModeUseThumbs     = "UseThumbs"
	PageModeUseCustomTags = "UseCustomTags"
	PageModeUseLayers     = "UseLayers"
	PageModeUseAttatchs   = "UseAttatchs"
	PageModeUseBookmarks  = "UseBookmarks"

	PageLayoutOnePage    = "OnePage"
	PageLayoutOneColumn  = "OneColumn"
	PageLayoutTwoPageL   = "TwoPageL"
	PageLayoutTwoColumnL = "TwoColumnL"
	PageLayoutTwoPageR   = "TwoPageR"
	PageLayoutTwoColumnR = "TwoColumnR"

	TabDisplayDocTitle = "DocTitle"
	TabDisplayFileName = "FileName"
	ZoomModeDefault    = "Default"
	ZoomModeFitHeight  = "FitHeight"
	ZoomModeFitWidth   = "FitWidth"
	ZoomModeFitRect    = "FitRect"
)

func (Image) isItem() {}

// CompositeGraphicUnit 描述资源中的复合图元定义。
// ID 必须显式指定且在文档资源中唯一，Items 保存复合图元内部内容。
type CompositeGraphicUnit struct {
	ID           uint64
	Width        float64
	Height       float64
	Thumbnail    uint64
	Substitution uint64
	Items        []Item
}

// Composite 是页面中的复合图元对象。
type Composite struct {
	X          float64
	Y          float64
	Width      float64
	Height     float64
	ResourceID uint64
	Name       string
	Visible    *bool
	DrawParam  string
	Actions    []Action
	CTM        *CTM
	Clips      *Clips
}

func (Composite) isItem() {}

// PageBlock 是可嵌套的页面对象容器。
type PageBlock struct {
	Items []Item
}

func (PageBlock) isItem() {}
