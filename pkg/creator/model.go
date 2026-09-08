package creator

import "time"

// A4 是毫米单位的标准 A4 页面尺寸。
var A4 = PageSize{Width: 210, Height: 297}

// PageSize 描述页面尺寸，单位为毫米。
type PageSize struct {
	// Width 是页面宽度，单位为毫米。
	Width float64
	// Height 是页面高度，单位为毫米。
	Height float64
}

// Box 描述页面区域或图元边界，单位为毫米。
type Box struct {
	// X 是区域左下角的横坐标，单位为毫米。
	X float64
	// Y 是区域左下角的纵坐标，单位为毫米。
	Y float64
	// Width 是区域宽度，单位为毫米。
	Width float64
	// Height 是区域高度，单位为毫米。
	Height float64
}

// PageArea 描述页面的物理区域、应用区域、内容区域和出血区域。
// PhysicalBox 为空时使用文档的页面尺寸。
type PageArea struct {
	// PhysicalBox 是页面物理边界；为空时使用文档页面尺寸。
	PhysicalBox *Box
	// ApplicationBox 是应用程序可用的页面区域。
	ApplicationBox *Box
	// ContentBox 是页面内容的布局区域。
	ContentBox *Box
	// BleedBox 是包含出血范围的页面区域。
	BleedBox *Box
}

// Document 是 OFD 文件包的输入模型。
// 页面、对象坐标、对象尺寸和文字字号的单位均为毫米。
type Document struct {
	// ID 是文档标识。
	ID string
	// Title 是文档标题。
	Title string
	// Author 是文档作者。
	Author string
	// Subject 是文档主题。
	Subject string
	// Abstract 是文档摘要。
	Abstract string
	// DocUsage 描述文档用途。
	DocUsage string
	// Cover 是封面图像在包内的文件名或引用名。
	Cover string
	// CoverData 是封面图像的二进制数据。
	CoverData []byte
	// CoverName 是封面图像写入包时使用的文件名。
	CoverName string
	// Keywords 是文档关键字列表。
	Keywords []string
	// CustomDatas 是文档自定义元数据。
	CustomDatas []CustomData
	// Creator 是创建文档的软件名称。
	Creator string
	// CreatorVersion 是创建软件的版本。
	CreatorVersion string
	// CreationDate 是文档创建时间。
	CreationDate time.Time
	// ModDate 是文档最后修改时间。
	ModDate time.Time
	// PageSize 是文档默认页面尺寸，单位为毫米。
	PageSize PageSize
	// Area 是文档默认页面区域设置。
	Area *PageArea
	// DrawParams 是文档级绘制参数资源。
	DrawParams []DrawParam
	// Fonts 是文档字体资源。
	Fonts []Font
	// ColorSpaces 是文档颜色空间资源。
	ColorSpaces []ColorSpace
	// DefaultCS 是默认颜色空间的资源 ID。
	DefaultCS uint64
	// PublicRes 是文档公共资源文件。
	PublicRes []PublicResource
	// Media 是文档级多媒体资源。
	Media []Media
	// Composites 是文档级复合图元资源。
	Composites []CompositeGraphicUnit
	// Pages 是文档页面，切片索引即页面索引。
	Pages []Page
	// Templates 是文档模板页资源。
	Templates []TemplatePage
	// Actions 是文档级动作。
	Actions []Action
	// Outlines 是文档大纲。
	Outlines []Outline
	// Permissions 是文档权限设置。
	Permissions *Permissions
	// Preferences 是阅读器显示偏好设置。
	Preferences *ViewPreferences
	// Bookmarks 是文档书签。
	Bookmarks []Bookmark
	// Annotations 是按页面组织的注解集合。
	Annotations []AnnotationPage
	// Attachments 是文档附件。
	Attachments []Attachment
	// CustomTags 是文档自定义标签资源。
	CustomTags []CustomTag
	// Extensions 是文档扩展信息。
	Extensions []Extension
	// Signatures 是文档签名清单。
	Signatures []Signature
	// Versions 是文档版本清单。
	Versions []DocumentVersion
}

// Attachment 描述文档附件及其包内数据。
type Attachment struct {
	// ID 是附件标识。
	ID string
	// Name 是附件显示名称。
	Name string
	// Format 是附件格式或媒体类型。
	Format string
	// CreationDate 是附件创建时间。
	CreationDate time.Time
	// ModDate 是附件最后修改时间。
	ModDate time.Time
	// Visible 指定附件是否在阅读器中可见；为空时使用默认值。
	Visible *bool
	// Usage 描述附件用途。
	Usage string
	// Data 是附件的二进制数据。
	Data []byte
	// FileName 是附件在 OFD 包内使用的文件名。
	FileName string
}

// CustomTag 描述一个自定义标签清单及其数据文件。
type CustomTag struct {
	// NameSpace 是自定义标签的命名空间。
	NameSpace string
	// Schema 是标签模式文件的二进制数据。
	Schema []byte
	// SchemaName 是模式文件在包内的文件名。
	SchemaName string
	// Data 是自定义标签数据文件的二进制数据。
	Data []byte
	// DataName 是数据文件在包内的文件名。
	DataName string
}

// CustomData 描述 DocInfo 中的自定义元数据。
type CustomData struct {
	// Name 是自定义元数据名称。
	Name string
	// Value 是自定义元数据值。
	Value string
}

// PublicResource 描述一个嵌入 OFD 包的公共资源 XML 文件。
// Name 是相对于 Doc_0 的文件名，Data 应为 Res 根元素的 XML 内容。
type PublicResource struct {
	// Name 是相对于 Doc_0 的公共资源 XML 文件名。
	Name string
	// Data 是 Res 根元素的 XML 内容。
	Data []byte
	// Files 是资源 XML 引用的包内文件。
	Files []PublicResourceFile
}

// PublicResourceFile 描述公共资源 XML 引用的包内文件。
// Path 相对于公共资源 XML 所在目录。
type PublicResourceFile struct {
	// Path 是相对于公共资源 XML 所在目录的文件路径。
	Path string
	// Data 是文件的二进制数据。
	Data []byte
}

// Extension 描述文档扩展及其属性、内联数据或外部数据文件。
type Extension struct {
	// AppName 是扩展应用名称。
	AppName string
	// Company 是扩展提供方名称。
	Company string
	// AppVersion 是扩展应用版本。
	AppVersion string
	// Date 是扩展信息的日期。
	Date time.Time
	// RefID 是扩展引用的资源或对象标识。
	RefID uint64
	// Properties 是扩展的键值属性。
	Properties []ExtensionProperty
	// Data 是扩展数据的文本内容。
	Data string
	// DataXML 是 Data 元素中的原始 XML 内容。设置后会作为子元素写入，
	// 与 Data 互斥；Data 适合只需要文本内容的扩展。
	DataXML []byte
	// DataFile 是扩展外部数据文件的二进制内容。
	DataFile []byte
	// DataName 是外部数据文件在包内的文件名。
	DataName string
}

// ExtensionProperty 描述扩展的键值属性。
type ExtensionProperty struct {
	// Name 是扩展属性名称。
	Name string
	// Type 是扩展属性类型。
	Type string
	// Value 是扩展属性值。
	Value string
}

// Signature 描述一个 OFD 签名清单项及其签名文件。
type Signature struct {
	// ID 是签名标识。
	ID string
	// Type 是签名类型。
	Type string
	// ProviderName 是签名提供方名称。
	ProviderName string
	// ProviderVersion 是签名提供方版本。
	ProviderVersion string
	// Company 是签名方名称。
	Company string
	// Method 是签名方法标识。
	Method string
	// Date 是签名生成时间。
	Date time.Time
	// CheckMethod 是签名引用的摘要算法。
	CheckMethod string
	// References 是签名覆盖的文件摘要引用。
	References []SignatureReference
	// StampAnnots 是与签名关联的印章注解。
	StampAnnots []SignatureStamp
	// SealFile 是印章文件的二进制数据。
	SealFile []byte
	// SealName 是印章文件在包内的文件名。
	SealName string
	// SignedValue 是签名值文件的二进制数据。
	SignedValue []byte
	// SignedValueName 是签名值文件在包内的文件名。
	SignedValueName string
}

// SignatureReference 描述签名摘要引用。FileRef 相对于签名 XML 文件。
type SignatureReference struct {
	// FileRef 是相对于签名 XML 文件的被签名文件路径。
	FileRef string
	// CheckValue 是被引用文件的摘要值。
	CheckValue []byte
}

// SignatureStamp 描述签名在页面上的盖章区域。
type SignatureStamp struct {
	// ID 是印章标识。
	ID string
	// Page 是从 0 开始的盖章页面索引。
	Page int
	// Boundary 是印章在页面上的边界，单位为毫米。
	Boundary Box
	// Clip 是印章内容的裁剪区域，单位为毫米。
	Clip *Box
}

// DocumentVersion 描述文档的一个版本及其文件清单。
type DocumentVersion struct {
	// ID 是文档版本标识。
	ID string
	// Index 是版本在版本清单中的索引。
	Index int
	// Current 指定该版本是否为当前版本。
	Current bool
	// Version 是版本号或版本名称。
	Version string
	// Name 是版本显示名称。
	Name string
	// CreationDate 是版本创建时间。
	CreationDate time.Time
	// Files 是该版本包含的文件清单。
	Files []VersionFile
	// DocRoot 是版本文档根文件的二进制数据。
	DocRoot []byte
	// DocRootName 是文档根文件在包内的文件名。
	DocRootName string
}

// VersionFile 描述文档版本中的文件。
type VersionFile struct {
	// ID 是版本文件标识。
	ID string
	// Path 是版本文件在包内的路径。
	Path string
}

// DrawParam 描述可被图层或图元引用的绘制参数资源。
// Relative 使用另一个绘制参数的 Name 作为继承来源。
type DrawParam struct {
	// Name 是绘制参数资源名称。
	Name string
	// Relative 是继承来源绘制参数的 Name。
	Relative string
	// LineWidth 是线宽，单位为毫米。
	LineWidth float64
	// Join 是路径连接处的连接样式。
	Join string
	// Cap 是路径端点的端点样式。
	Cap string
	// DashOffset 是虚线起始偏移量，单位为毫米。
	DashOffset float64
	// DashPattern 是虚线长度与间隔组成的序列，单位为毫米。
	DashPattern []float64
	// MiterLimit 是斜接连接的长度限制。
	MiterLimit float64
	// FillColor 是填充颜色。
	FillColor *Color
	// StrokeColor 是描边颜色。
	StrokeColor *Color
}

// Font 描述文档字体资源，Name 必须与 Text.Font 的值一致。
// Data 非空时，字体数据会嵌入 OFD 文件包。
type Font struct {
	// Name 是字体资源名称，必须与文字对象的 Font 值一致。
	Name string
	// FamilyName 是字体族名称。
	FamilyName string
	// Charset 是字体字符集标识。
	Charset string
	// Italic 指定字体是否为斜体。
	Italic bool
	// Bold 指定字体是否为粗体。
	Bold bool
	// Serif 指定字体是否为衬线字体。
	Serif bool
	// FixedWidth 指定字体是否为等宽字体。
	FixedWidth bool
	// Format 是字体数据格式。
	Format string
	// Data 是嵌入字体的二进制数据。
	Data []byte
}

// Page 按绘制顺序保存页面对象。
type Page struct {
	// Area 是页面区域设置。
	Area *PageArea
	// Resources 是页面专属资源文件。
	Resources []PageResource
	// Templates 是页面引用的模板页。
	Templates []TemplateRef
	// LayerType 是页面默认图层类型。
	LayerType string
	// Items 是按绘制顺序排列的页面对象。
	Items []Item
	// Layers 是页面图层，切片顺序即绘制顺序。
	Layers []Layer
	// Actions 是页面动作。
	Actions []Action
}

// PageResource 描述页面专属的资源文件。Images 适合快速创建页面图片资源；
// Data 可用于完整的 OFD Res 资源集合。
type PageResource struct {
	// Images 是页面图片资源的快捷配置。
	Images []PageImage
	// Data 是完整的 OFD Res XML。用于页面专属的颜色空间、字体、绘制参数、
	// 多媒体或复合图元等资源；设置后不能同时设置 Images。
	Data []byte
	// Files 是 Data 中引用的、相对于 PageRes XML 所在目录的二进制文件。
	Files []PageResourceFile
}

// PageResourceFile 描述页面资源 XML 引用的包内文件。
type PageResourceFile struct {
	// Path 是相对于页面资源 XML 所在目录的文件路径。
	Path string
	// Data 是文件的二进制数据。
	Data []byte
}

// PageImage 描述页面资源中的图片。ID 在文档内必须唯一。
type PageImage struct {
	// ID 是图片资源标识，且必须在文档内唯一。
	ID uint64
	// Format 是图片格式，如 PNG 或 JPEG。
	Format string
	// Data 是图片的二进制数据。
	Data []byte
	// Name 是图片在包内的文件名。
	Name string
}

// TemplatePage 描述文档模板页及其页面内容。
// ID 必须在文档内唯一，页面通过该 ID 引用模板页。
type TemplatePage struct {
	// ID 是模板页标识，且必须在文档内唯一。
	ID uint64
	// Name 是模板页名称。
	Name string
	// ZOrder 是模板页相对于页面内容的绘制顺序。
	ZOrder string
	// Area 是模板页区域设置。
	Area *PageArea
	// Items 是模板页中的页面对象。
	Items []Item
	// Layers 是模板页中的图层。
	Layers []Layer
}

// TemplateRef 描述页面使用的模板页引用。
type TemplateRef struct {
	// ID 是被引用模板页的标识。
	ID uint64
	// ZOrder 是模板页相对于页面内容的绘制顺序。
	ZOrder string
}

// Layer 表示页面中的一个图层，图层顺序就是绘制顺序。
type Layer struct {
	// Type 是图层类型。
	Type string
	// DrawParam 是图层引用的绘制参数名称。
	DrawParam string
	// Items 是图层中的页面对象，顺序即绘制顺序。
	Items []Item
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
	// X 是文字对象左下角的横坐标，单位为毫米。
	X float64
	// Y 是文字对象左下角的纵坐标，单位为毫米。
	Y float64
	// Width 是文字对象宽度，单位为毫米。
	Width float64
	// Height 是文字对象高度，单位为毫米。
	Height float64
	// Value 是文字内容。
	Value string
	// Font 是字体资源名称。
	Font string
	// Size 是字号，单位为毫米。
	Size float64
	// Visible 指定文字对象是否可见；为空时使用默认值。
	Visible *bool
	// Stroke 指定文字是否描边。
	Stroke bool
	// Fill 指定文字是否填充；为空时使用默认值。
	Fill *bool
	// HScale 是文字水平方向的缩放比例。
	HScale float64
	// ReadDirection 是文字阅读方向。
	ReadDirection int
	// CharDirection 是字符排列方向。
	CharDirection int
	// Weight 是文字字重。
	Weight int
	// Italic 指定文字是否使用斜体。
	Italic bool
	// DrawParam 是文字对象引用的绘制参数名称。
	DrawParam string
	// CTM 是文字对象的坐标变换矩阵。
	CTM *CTM
	// TextCodes 是文字分段及其定位信息。
	TextCodes []TextCode
	// CGTransforms 是字符到字形的映射关系。
	CGTransforms []CGTransform
	// Actions 是文字对象的动作。
	Actions []Action
	// Clips 是文字对象使用的裁剪区域。
	Clips *Clips
	// FillColor 是文字填充颜色。
	FillColor *Color
	// StrokeColor 是文字描边颜色。
	StrokeColor *Color
}

// TextCode 描述文字对象中的一段文字及其定位信息。
// X、Y 为空时沿用文字对象的排版位置；DeltaX、DeltaY 表示字符位置增量。
type TextCode struct {
	// Value 是该文字分段的内容。
	Value string
	// X 是分段起始横坐标，单位为毫米；为空时沿用排版位置。
	X *float64
	// Y 是分段起始纵坐标，单位为毫米；为空时沿用排版位置。
	Y *float64
	// DeltaX 是各字符的横向位置增量，单位为毫米。
	DeltaX []float64
	// DeltaY 是各字符的纵向位置增量，单位为毫米。
	DeltaY []float64
}

// CGTransform 描述文字分段中的字形映射关系。
// CodePosition 是相对于整个文字对象的字符位置，从 0 开始。
type CGTransform struct {
	// CodePosition 是相对于文字对象的起始字符位置，从 0 开始。
	CodePosition int
	// CodeCount 是参与映射的字符数量。
	CodeCount int
	// GlyphCount 是映射生成的字形数量。
	GlyphCount int
	// Glyphs 是字形索引列表。
	Glyphs []int
}

// CTM 是 OFD 六参数坐标变换矩阵，顺序为 a、b、c、d、e、f。
type CTM [6]float64

// Clips 描述图元使用的裁剪区域集合。
type Clips struct {
	// Items 是裁剪区域定义列表。
	Items []Clip
}

// Clip 描述一个裁剪区域定义。
type Clip struct {
	// Areas 是裁剪区域中的路径或文字区域列表。
	Areas []ClipArea
}

// ClipArea 描述一个路径或文字裁剪区域。
type ClipArea struct {
	// DrawParam 是区域引用的绘制参数名称。
	DrawParam string
	// CTM 是区域的坐标变换矩阵。
	CTM *CTM
	// Path 是路径裁剪区域；与 Text 二选一。
	Path *ClipPath
	// Text 是文字裁剪区域；与 Path 二选一。
	Text *ClipText
}

// ClipPath 是裁剪区域中的路径，Boundary 为必填边界。
type ClipPath struct {
	// Boundary 是路径边界，单位为毫米，且为必填项。
	Boundary Box
	// Name 是路径名称。
	Name string
	// Visible 指定路径是否可见；为空时使用默认值。
	Visible *bool
	// CTM 是路径的坐标变换矩阵。
	CTM *CTM
	// Data 是 OFD 缩略路径语法内容。
	Data string
	// Stroke 指定路径是否描边。
	Stroke bool
	// StrokeSet 指定是否显式设置 Stroke。
	StrokeSet *bool
	// Fill 指定路径是否填充。
	Fill bool
	// Rule 是路径填充规则。
	Rule string
	// LineWidth 是描边线宽，单位为毫米。
	LineWidth float64
	// Cap 是描边端点样式。
	Cap string
	// Join 是描边连接样式。
	Join string
	// MiterLimit 是斜接连接的长度限制。
	MiterLimit float64
	// DashOffset 是虚线起始偏移量，单位为毫米。
	DashOffset float64
	// DashPattern 是虚线长度与间隔组成的序列，单位为毫米。
	DashPattern []float64
	// Alpha 是路径整体透明度，取值范围为 0 到 255。
	Alpha *uint8
	// StrokeColor 是路径描边颜色。
	StrokeColor *Color
	// FillColor 是路径填充颜色。
	FillColor *Color
}

// ClipText 是裁剪区域中的文字，Font 为空时使用 SimSun。
type ClipText struct {
	// Boundary 是文字裁剪区域边界，单位为毫米。
	Boundary Box
	// CTM 是文字的坐标变换矩阵。
	CTM *CTM
	// Font 是字体资源名称；为空时使用 SimSun。
	Font string
	// Size 是字号，单位为毫米。
	Size float64
	// Value 是裁剪文字内容。
	Value string
	// TextCodes 是文字分段及其定位信息。
	TextCodes []TextCode
	// Stroke 指定文字是否描边。
	Stroke bool
	// Fill 指定文字是否填充；为空时使用默认值。
	Fill *bool
	// HScale 是文字水平方向的缩放比例。
	HScale float64
	// ReadDirection 是文字阅读方向。
	ReadDirection int
	// CharDirection 是字符排列方向。
	CharDirection int
	// Weight 是文字字重。
	Weight int
	// Italic 指定文字是否使用斜体。
	Italic bool
	// FillColor 是文字填充颜色。
	FillColor *Color
	// StrokeColor 是文字描边颜色。
	StrokeColor *Color
}

func (Text) isItem() {}

// Path 是路径对象。Data 使用 OFD 缩略路径语法，例如
// "M 10 10 L 100 10 L 100 100 C"。
type Path struct {
	// X 是路径对象左下角的横坐标，单位为毫米。
	X float64
	// Y 是路径对象左下角的纵坐标，单位为毫米。
	Y float64
	// Width 是路径对象宽度，单位为毫米。
	Width float64
	// Height 是路径对象高度，单位为毫米。
	Height float64
	// Data 是 OFD 缩略路径语法内容。
	Data string
	// Stroke 指定路径是否描边。
	Stroke bool
	// StrokeSet 指定是否显式设置 Stroke。
	StrokeSet *bool
	// Fill 指定路径是否填充。
	Fill bool
	// Rule 是路径填充规则。
	Rule string
	// Name 是路径名称。
	Name string
	// Visible 指定路径是否可见；为空时使用默认值。
	Visible *bool
	// LineWidth 是描边线宽，单位为毫米。
	LineWidth float64
	// Cap 是描边端点样式。
	Cap string
	// Join 是描边连接样式。
	Join string
	// MiterLimit 是斜接连接的长度限制。
	MiterLimit float64
	// DashOffset 是虚线起始偏移量，单位为毫米。
	DashOffset float64
	// DashPattern 是虚线长度与间隔组成的序列，单位为毫米。
	DashPattern []float64
	// Alpha 是路径整体透明度，取值范围为 0 到 255。
	Alpha *uint8
	// DrawParam 是路径引用的绘制参数名称。
	DrawParam string
	// Actions 是路径对象的动作。
	Actions []Action
	// CTM 是路径对象的坐标变换矩阵。
	CTM *CTM
	// Clips 是路径对象使用的裁剪区域。
	Clips *Clips
	// StrokeColor 是路径描边颜色。
	StrokeColor *Color
	// FillColor 是路径填充颜色。
	FillColor *Color
}

func (Path) isItem() {}

// Image 是图片对象，Data 保存编码后的图片数据。
// Format 是 PNG 或 JPEG 等 OFD 图片格式；为空时会尽量根据文件头识别。
type Image struct {
	// X 是图片对象左下角的横坐标，单位为毫米。
	X float64
	// Y 是图片对象左下角的纵坐标，单位为毫米。
	Y float64
	// Width 是图片对象宽度，单位为毫米。
	Width float64
	// Height 是图片对象高度，单位为毫米。
	Height float64
	// Data 是图片的二进制数据。
	Data []byte
	// ResourceID 是引用的图片资源 ID。
	ResourceID uint64
	// Substitution 是替代图片资源 ID。
	Substitution uint64
	// ImageMask 是图片蒙版资源 ID。
	ImageMask uint64
	// Format 是图片格式，如 PNG 或 JPEG。
	Format string
	// Name 是图片名称。
	Name string
	// Visible 指定图片是否可见；为空时使用默认值。
	Visible *bool
	// LineWidth 是图片边框线宽，单位为毫米。
	LineWidth float64
	// Cap 是图片边框端点样式。
	Cap string
	// Join 是图片边框连接样式。
	Join string
	// MiterLimit 是图片边框斜接连接的长度限制。
	MiterLimit float64
	// DashOffset 是图片边框虚线起始偏移量，单位为毫米。
	DashOffset float64
	// DashPattern 是图片边框虚线长度与间隔序列，单位为毫米。
	DashPattern []float64
	// Alpha 是图片整体透明度，取值范围为 0 到 255。
	Alpha *uint8
	// DrawParam 是图片引用的绘制参数名称。
	DrawParam string
	// Actions 是图片对象的动作。
	Actions []Action
	// CTM 是图片对象的坐标变换矩阵。
	CTM *CTM
	// Clips 是图片对象使用的裁剪区域。
	Clips *Clips
	// Border 是图片边框样式。
	Border *ImageBorder
}

// ImageBorder 描述图片边框的线宽、圆角和虚线样式。
type ImageBorder struct {
	// LineWidth 是边框线宽，单位为毫米。
	LineWidth float64
	// HorizontalRadius 是水平圆角半径，单位为毫米。
	HorizontalRadius float64
	// VerticalRadius 是垂直圆角半径，单位为毫米。
	VerticalRadius float64
	// DashOffset 是虚线起始偏移量，单位为毫米。
	DashOffset float64
	// DashPattern 是虚线长度与间隔组成的序列，单位为毫米。
	DashPattern []float64
	// Color 是边框颜色。
	Color *Color
}

// Color 描述颜色值或渐变填充。未设置 Components、ColorSpace 和渐变时使用 RGB。
type Color struct {
	// R 是 RGB 红色分量，取值范围为 0 到 255。
	R uint8
	// G 是 RGB 绿色分量，取值范围为 0 到 255。
	G uint8
	// B 是 RGB 蓝色分量，取值范围为 0 到 255。
	B uint8
	// Components 是当前颜色空间中的颜色分量。
	Components []int
	// ColorSpace 是引用的颜色空间资源 ID。
	ColorSpace uint64
	// Index 是颜色空间调色板索引；为空时使用 Components。
	Index *int
	// Alpha 是颜色透明度，取值范围为 0 到 255。
	Alpha *uint8
	// Axial 是轴向渐变配置。
	Axial *AxialShading
	// Radial 是径向渐变配置。
	Radial *RadialShading
	// Gouraud 是三角网格渐变配置。
	Gouraud *GouraudShading
	// LaGouraud 是四边形网格渐变配置。
	LaGouraud *LaGouraudShading
	// Pattern 是平铺图案填充配置。
	Pattern *Pattern
}

// Pattern 描述用于填充颜色的平铺图案。
// 图案单元中的对象使用 Items 或 Layers 配置，坐标单位为毫米。
type Pattern struct {
	// Width 是图案单元宽度，单位为毫米。
	Width float64
	// Height 是图案单元高度，单位为毫米。
	Height float64
	// XStep 是图案水平方向重复步长，单位为毫米。
	XStep float64
	// YStep 是图案垂直方向重复步长，单位为毫米。
	YStep float64
	// ReflectMethod 是图案重复时的镜像方式。
	ReflectMethod string
	// RelativeTo 是图案坐标相对于页面或对象的参考系。
	RelativeTo string
	// CTM 是图案单元的坐标变换矩阵。
	CTM *CTM
	// Thumbnail 是图案缩略图资源 ID。
	Thumbnail uint64
	// Items 是图案单元中的页面对象。
	Items []Item
	// Layers 是图案单元中的图层。
	Layers []Layer

	builtState *buildState
	builtItems []builtItem
	prepared   bool
	building   bool
}

// ColorSpace 描述 DocumentRes.xml 中的颜色空间资源。
type ColorSpace struct {
	// ID 是颜色空间资源标识。
	ID uint64
	// Type 是颜色空间类型。
	Type string
	// BitsPerComponent 是每个颜色分量的位数。
	BitsPerComponent int
	// Profile 是颜色配置文件名称或标识。
	Profile string
	// ProfileData 是颜色配置文件的二进制数据。
	ProfileData []byte
	// Palette 是调色板颜色列表。
	Palette []string
}

// ColorStop 描述渐变中的颜色分段。
type ColorStop struct {
	// Position 是渐变位置，通常取值范围为 0 到 1。
	Position float64
	// Color 是该位置对应的颜色。
	Color Color
}

// AxialShading 描述轴向渐变。
type AxialShading struct {
	// MapType 是渐变映射类型。
	MapType string
	// MapUnit 是渐变映射单位。
	MapUnit float64
	// Extend 指定渐变端点外的延展方式。
	Extend int
	// StartPoint 是渐变起点坐标。
	StartPoint string
	// EndPoint 是渐变终点坐标。
	EndPoint string
	// Segments 是按位置排列的颜色分段。
	Segments []ColorStop
}

// RadialShading 描述径向渐变。
type RadialShading struct {
	// MapType 是渐变映射类型。
	MapType string
	// MapUnit 是渐变映射单位。
	MapUnit float64
	// Eccentricity 是径向渐变椭圆离心率。
	Eccentricity float64
	// Angle 是径向渐变旋转角度。
	Angle float64
	// StartPoint 是渐变起点坐标。
	StartPoint string
	// StartRadius 是渐变起始半径。
	StartRadius float64
	// EndPoint 是渐变终点坐标。
	EndPoint string
	// EndRadius 是渐变结束半径。
	EndRadius float64
	// Extend 指定渐变端点外的延展方式。
	Extend int
	// Segments 是按位置排列的颜色分段。
	Segments []ColorStop
}

// GouraudShading 描述三角网格渐变。
type GouraudShading struct {
	// Extend 指定网格边界外的延展方式。
	Extend int
	// Points 是三角网格控制点列表。
	Points []GouraudPoint
	// BackColor 是网格背景颜色。
	BackColor *Color
}

// GouraudPoint 描述三角网格渐变控制点。
type GouraudPoint struct {
	// X 是控制点横坐标，单位为毫米。
	X float64
	// Y 是控制点纵坐标，单位为毫米。
	Y float64
	// EdgeFlag 是控制点所在网格边缘标志。
	EdgeFlag int
	// Color 是控制点颜色。
	Color Color
}

// LaGouraudShading 描述四边形网格渐变。
type LaGouraudShading struct {
	// VerticesPerRow 是每行顶点数量。
	VerticesPerRow int
	// Extend 指定网格边界外的延展方式。
	Extend int
	// Points 是四边形网格控制点列表。
	Points []LaGouraudPoint
	// BackColor 是网格背景颜色。
	BackColor *Color
}

// LaGouraudPoint 描述四边形网格渐变控制点。
type LaGouraudPoint struct {
	// X 是控制点横坐标，单位为毫米。
	X float64
	// Y 是控制点纵坐标，单位为毫米。
	Y float64
	// Color 是控制点颜色。
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
	// Event 是触发动作的事件类型。
	Event ActionEvent
	// Region 是动作触发区域，可选。
	Region *ActionRegion
	// URI 是外部 URI 动作目标，与其他动作目标互斥。
	URI *URIAction
	// Goto 是当前文档内的页面跳转目标，与其他动作目标互斥。
	Goto *GotoAction
	// GotoA 是文档附件跳转目标，与其他动作目标互斥。
	GotoA *GotoAAction
	// Sound 是声音播放目标，与其他动作目标互斥。
	Sound *SoundAction
	// Movie 是影片控制目标，与其他动作目标互斥。
	Movie *MovieAction
}

// Point 描述区域路径中的二维点。
type Point struct {
	// X 是点的横坐标，单位为毫米。
	X float64
	// Y 是点的纵坐标，单位为毫米。
	Y float64
}

// ActionRegion 描述动作触发区域。
type ActionRegion struct {
	// Areas 是动作触发区域中的路径列表。
	Areas []ActionArea
}

// ActionArea 描述动作区域中的一条路径。
type ActionArea struct {
	// Start 是区域路径的起始点，单位为毫米。
	Start Point
	// Commands 是从 Start 开始的路径命令。
	Commands []RegionCommand
}

// RegionCommand 是动作区域路径命令。
type RegionCommand interface {
	isRegionCommand()
}

// RegionMove 移动到指定点。
type RegionMove struct {
	// Point 是移动到的目标点，单位为毫米。
	Point Point
}

// RegionLine 连接到指定点。
type RegionLine struct {
	// Point 是连接到的目标点，单位为毫米。
	Point Point
}

// RegionQuadraticBezier 描述二次贝塞尔曲线。
type RegionQuadraticBezier struct {
	// Control 是二次贝塞尔曲线的控制点，单位为毫米。
	Control Point
	// End 是二次贝塞尔曲线的终点，单位为毫米。
	End Point
}

// RegionCubicBezier 描述三次贝塞尔曲线。
type RegionCubicBezier struct {
	// Control1 是三次贝塞尔曲线的第一个控制点，单位为毫米。
	Control1 Point
	// Control2 是三次贝塞尔曲线的第二个控制点，单位为毫米。
	Control2 Point
	// End 是三次贝塞尔曲线的终点，单位为毫米。
	End Point
}

// RegionArc 描述椭圆弧。
type RegionArc struct {
	// SweepDirection 指定圆弧的扫描方向。
	SweepDirection bool
	// LargeArc 指定是否选择大于半圆的弧段。
	LargeArc bool
	// RotationAngle 是椭圆旋转角度。
	RotationAngle float64
	// EllipseSize 是椭圆的半轴尺寸，单位为毫米。
	EllipseSize Point
	// EndPoint 是圆弧终点，单位为毫米。
	EndPoint Point
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
	// URI 是要访问的统一资源标识符。
	URI string
	// Base 是解析相对 URI 时使用的基础 URI。
	Base string
	// Target 是 URI 打开的目标窗口或框架。
	Target string
}

// GotoAction 描述当前文档内的页面跳转动作。
// Page 是从 0 开始的页面索引；Type 为空时使用 Fit。
type GotoAction struct {
	// Page 是从 0 开始的目标页面索引。
	Page int
	// Bookmark 是目标书签名称。
	Bookmark string
	// Type 是页面适配方式；为空时使用 Fit。
	Type string
	// Left 是目标视图左边界，单位为毫米。
	Left *float64
	// Top 是目标视图上边界，单位为毫米。
	Top *float64
	// Right 是目标视图右边界，单位为毫米。
	Right *float64
	// Bottom 是目标视图下边界，单位为毫米。
	Bottom *float64
	// Zoom 是目标视图缩放比例。
	Zoom *float64
}

// GotoAAction 描述跳转到文档附件的动作。
type GotoAAction struct {
	// AttachID 是目标附件标识。
	AttachID string
	// NewWindow 指定是否在新窗口打开附件；为空时使用默认值。
	NewWindow *bool
}

// Media 描述文档级图片、音频或视频资源。
type Media struct {
	// ID 是多媒体资源标识。
	ID uint64
	// Type 是媒体类型，如图片、音频或视频。
	Type string
	// Format 是媒体编码格式。
	Format string
	// Data 是媒体的二进制数据。
	Data []byte
	// Name 是媒体文件在包内的文件名。
	Name string
}

// SoundAction 描述声音播放动作。
type SoundAction struct {
	// ResourceID 是声音资源 ID。
	ResourceID uint64
	// Volume 是播放音量，通常为百分比整数。
	Volume *int
	// Repeat 指定是否循环播放；为空时使用默认值。
	Repeat *bool
	// Synchronous 指定是否同步播放；为空时使用默认值。
	Synchronous *bool
}

// MovieAction 描述影片控制动作。
type MovieAction struct {
	// ResourceID 是影片资源 ID。
	ResourceID uint64
	// Operator 是影片控制操作。
	Operator string
}

// Outline 描述文档大纲项。大纲动作使用当前文档页面索引作为目标。
type Outline struct {
	// Title 是大纲项标题。
	Title string
	// Count 是子项数量或展开状态相关的计数值。
	Count *int
	// Expanded 指定大纲项是否展开；为空时使用默认值。
	Expanded *bool
	// Actions 是大纲项的跳转动作。
	Actions []Action
	// Children 是嵌套的大纲项。
	Children []Outline
}

// Bookmark 描述文档书签及其页面目标。
type Bookmark struct {
	// Name 是书签名称。
	Name string
	// Goto 是书签指向的页面目标。
	Goto GotoAction
}

// AnnotationPage 描述某个页面的注解集合，Page 使用从 0 开始的页面索引。
type AnnotationPage struct {
	// Page 是从 0 开始的注解所属页面索引。
	Page int
	// Items 是该页面的注解列表。
	Items []Annotation
}

// Annotation 描述页面注解及其外观。
type Annotation struct {
	// ID 是注解标识。
	ID uint64
	// Type 是注解类型。
	Type string
	// Creator 是注解创建者。
	Creator string
	// LastModDate 是注解最后修改时间。
	LastModDate time.Time
	// Visible 指定注解是否可见；为空时使用默认值。
	Visible *bool
	// Subtype 是注解子类型。
	Subtype string
	// Print 指定注解是否随文档打印；为空时使用默认值。
	Print *bool
	// NoZoom 指定注解是否不随页面缩放。
	NoZoom bool
	// NoRotate 指定注解是否不随页面旋转。
	NoRotate bool
	// ReadOnly 指定注解是否只读。
	ReadOnly bool
	// ReadOnlyValue 用于显式写出 ReadOnly=false。为空时沿用 ReadOnly 的兼容行为。
	ReadOnlyValue *bool
	// Remark 是注解备注。
	Remark string
	// Parameters 是注解自定义参数。
	Parameters []AnnotationParameter
	// Boundary 是注解边界，单位为毫米。
	Boundary *Box
	// Items 是注解外观中的页面对象。
	Items []Item
}

// AnnotationParameter 描述注解的自定义参数。
type AnnotationParameter struct {
	// Name 是注解参数名称。
	Name string
	// Value 是注解参数值。
	Value string
}

// Permissions 描述文档编辑、导出、打印等权限。
type Permissions struct {
	// Edit 指定是否允许编辑文档内容。
	Edit *bool
	// Annot 指定是否允许添加或修改注解。
	Annot *bool
	// Export 指定是否允许导出文档内容。
	Export *bool
	// Signature 指定是否允许签名。
	Signature *bool
	// Watermark 指定是否允许添加或修改水印。
	Watermark *bool
	// PrintScreen 指定是否允许屏幕截图。
	PrintScreen *bool
	// Print 是打印权限及打印份数设置。
	Print *PrintSettings
	// ValidPeriod 是权限生效和失效时间范围。
	ValidPeriod *ValidPeriod
}

// PrintSettings 描述打印权限和允许的打印份数。
type PrintSettings struct {
	// Printable 指定是否允许打印。
	Printable bool
	// Copies 是允许打印的份数；为空时不限定份数。
	Copies *int
}

// ValidPeriod 描述权限的生效和失效时间。
type ValidPeriod struct {
	// Start 是权限生效时间。
	Start time.Time
	// End 是权限失效时间。
	End time.Time
}

// ViewPreferences 描述阅读器打开文档时的显示设置。
type ViewPreferences struct {
	// PageMode 是文档打开时的页面导航模式。
	PageMode string
	// PageLayout 是文档打开时的页面布局模式。
	PageLayout string
	// TabDisplay 是阅读器标签页显示的标题来源。
	TabDisplay string
	// HideToolbar 指定是否隐藏工具栏；为空时使用默认值。
	HideToolbar *bool
	// HideMenubar 指定是否隐藏菜单栏；为空时使用默认值。
	HideMenubar *bool
	// HideWindowUI 指定是否隐藏窗口界面元素；为空时使用默认值。
	HideWindowUI *bool
	// ZoomMode 是文档打开时的缩放模式。
	ZoomMode string
	// Zoom 是自定义缩放比例。
	Zoom *float64
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
	// ID 是复合图元资源标识，且必须在文档资源中唯一。
	ID uint64
	// Width 是复合图元宽度，单位为毫米。
	Width float64
	// Height 是复合图元高度，单位为毫米。
	Height float64
	// Thumbnail 是复合图元缩略图资源 ID。
	Thumbnail uint64
	// Substitution 是替代复合图元资源 ID。
	Substitution uint64
	// Items 是复合图元内部对象，顺序即绘制顺序。
	Items []Item
}

// Composite 是页面中的复合图元对象。
type Composite struct {
	// X 是复合图元左下角的横坐标，单位为毫米。
	X float64
	// Y 是复合图元左下角的纵坐标，单位为毫米。
	Y float64
	// Width 是复合图元宽度，单位为毫米。
	Width float64
	// Height 是复合图元高度，单位为毫米。
	Height float64
	// ResourceID 是引用的复合图元资源 ID。
	ResourceID uint64
	// Name 是复合图元名称。
	Name string
	// Visible 指定复合图元是否可见；为空时使用默认值。
	Visible *bool
	// DrawParam 是复合图元引用的绘制参数名称。
	DrawParam string
	// Actions 是复合图元对象的动作。
	Actions []Action
	// CTM 是复合图元的坐标变换矩阵。
	CTM *CTM
	// Clips 是复合图元使用的裁剪区域。
	Clips *Clips
}

func (Composite) isItem() {}

// PageBlock 是可嵌套的页面对象容器。
type PageBlock struct {
	// Items 是容器中的页面对象，顺序即绘制顺序。
	Items []Item
}

func (PageBlock) isItem() {}
