// Package manifest 负责读取声明式 manifest 并构建 OFD 文档模型。
package manifest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/zc310/ofd/pkg/creator"
	"go.yaml.in/yaml/v3"
)

// Manifest 是 ofd-creator 支持的声明式输入文件。
type Manifest struct {
	// Version 指定 manifest 格式版本。
	Version int `json:"version" yaml:"version"`
	// Document 定义文档主体及其元数据。
	Document Document `json:"document" yaml:"document"`
	// Resources 汇总文档级资源。
	Resources Resources `json:"resources" yaml:"resources"`
	// Templates 定义可供页面引用的模板页。
	Templates []Template `json:"templates" yaml:"templates"`
	// Pages 定义文档页面。
	Pages []Page `json:"pages" yaml:"pages"`
}

// Document 描述 OFD 文档及其元数据、资源和页面内容。
type Document struct {
	// ID 标识文档。
	ID string `json:"id" yaml:"id"`
	// Title 记录文档标题。
	Title string `json:"title" yaml:"title"`
	// Author 记录文档作者。
	Author string `json:"author" yaml:"author"`
	// Subject 记录文档主题。
	Subject string `json:"subject" yaml:"subject"`
	// Abstract 记录文档摘要。
	Abstract string `json:"abstract" yaml:"abstract"`
	// Creator 记录创建文档的应用名称。
	Creator string `json:"creator" yaml:"creator"`
	// CreatorVersion 记录创建应用的版本。
	CreatorVersion string `json:"creatorVersion" yaml:"creatorVersion"`
	// PageSize 定义默认页面尺寸。
	PageSize PageSize `json:"pageSize" yaml:"pageSize"`
	// Actions 定义文档级动作。
	Actions []Action `json:"actions" yaml:"actions"`
	// Bookmarks 定义文档书签。
	Bookmarks []Bookmark `json:"bookmarks" yaml:"bookmarks"`
	// Attachments 定义文档附件。
	Attachments []Attachment `json:"attachments" yaml:"attachments"`
	// Extensions 定义文档扩展信息。
	Extensions []Extension `json:"extensions" yaml:"extensions"`
	// Versions 定义文档版本记录。
	Versions []Version `json:"versions" yaml:"versions"`
	// Annotations 定义按页面组织的批注。
	Annotations []AnnotationPage `json:"annotations" yaml:"annotations"`
	// DocUsage 说明文档用途。
	DocUsage string `json:"docUsage" yaml:"docUsage"`
	// Keywords 记录文档关键词。
	Keywords []string `json:"keywords" yaml:"keywords"`
	// CustomData 定义文档自定义键值数据。
	CustomData []CustomData `json:"customData" yaml:"customData"`
	// Cover 指定封面文件路径。
	Cover string `json:"cover" yaml:"cover"`
	// CoverBase64 提供 Base64 编码的封面数据。
	CoverBase64 string `json:"coverBase64" yaml:"coverBase64"`
	// CoverName 指定封面在文档中的名称。
	CoverName string `json:"coverName" yaml:"coverName"`
	// CreationDate 记录文档创建时间。
	CreationDate string `json:"creationDate" yaml:"creationDate"`
	// ModDate 记录文档最后修改时间。
	ModDate string `json:"modDate" yaml:"modDate"`
	// Area 定义文档页面区域。
	Area *PageArea `json:"area" yaml:"area"`
	// DefaultCS 指定文档默认颜色空间编号。
	DefaultCS uint64 `json:"defaultCS" yaml:"defaultCS"`
	// Preferences 定义文档查看偏好。
	Preferences *Preferences `json:"preferences" yaml:"preferences"`
	// Permissions 定义文档访问和操作权限。
	Permissions *Permissions `json:"permissions" yaml:"permissions"`
	// Outlines 定义文档大纲。
	Outlines []Outline `json:"outlines" yaml:"outlines"`
	// Signatures 定义文档数字签名。
	Signatures []Signature `json:"signatures" yaml:"signatures"`
}

// PageSize 描述文档页面尺寸。
type PageSize struct {
	// Name 指定预定义页面尺寸名称。
	Name string `json:"name" yaml:"name"`
	// Width 指定页面宽度。
	Width float64 `json:"width" yaml:"width"`
	// Height 指定页面高度。
	Height float64 `json:"height" yaml:"height"`
}

// Resources 汇总文档级资源。
type Resources struct {
	// Fonts 汇总字体资源。
	Fonts []Font `json:"fonts" yaml:"fonts"`
	// Images 汇总图像资源。
	Images []Image `json:"images" yaml:"images"`
	// Media 汇总媒体资源。
	Media []Media `json:"media" yaml:"media"`
	// ColorSpaces 汇总颜色空间资源。
	ColorSpaces []ColorSpace `json:"colorSpaces" yaml:"colorSpaces"`
	// DrawParams 汇总图形绘制参数。
	DrawParams []DrawParam `json:"drawParams" yaml:"drawParams"`
	// Composites 汇总复合图形单元资源。
	Composites []Composite `json:"composites" yaml:"composites"`
	// Public 汇总公共资源。
	Public []PublicResource `json:"public" yaml:"public"`
	// CustomTags 汇总自定义标签资源。
	CustomTags []CustomTag `json:"customTags" yaml:"customTags"`
}

// Font 描述文档使用的字体资源。
type Font struct {
	// Name 指定字体资源名称。
	Name string `json:"name" yaml:"name"`
	// FamilyName 指定字体族名称。
	FamilyName string `json:"familyName" yaml:"familyName"`
	// Charset 指定字体字符集。
	Charset string `json:"charset" yaml:"charset"`
	// Format 指定字体文件格式。
	Format string `json:"format" yaml:"format"`
	// File 指定字体文件路径。
	File string `json:"file" yaml:"file"`
	// DataBase64 提供 Base64 编码的字体数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
	// Italic 指示字体是否为斜体。
	Italic bool `json:"italic" yaml:"italic"`
	// Bold 指示字体是否为粗体。
	Bold bool `json:"bold" yaml:"bold"`
	// Serif 指示字体是否为衬线字体。
	Serif bool `json:"serif" yaml:"serif"`
	// FixedWidth 指示字体是否为等宽字体。
	FixedWidth bool `json:"fixedWidth" yaml:"fixedWidth"`
}

// Image 描述文档中的图像资源。
type Image struct {
	// ID 标识图像资源。
	ID uint64 `json:"id" yaml:"id"`
	// Format 指定图像格式。
	Format string `json:"format" yaml:"format"`
	// Name 指定图像名称。
	Name string `json:"name" yaml:"name"`
	// File 指定图像文件路径。
	File string `json:"file" yaml:"file"`
	// DataBase64 提供 Base64 编码的图像数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
}

// Media 描述文档中的媒体资源。
type Media struct {
	// ID 标识媒体资源。
	ID uint64 `json:"id" yaml:"id"`
	// Type 指定媒体类型。
	Type string `json:"type" yaml:"type"`
	// Format 指定媒体格式。
	Format string `json:"format" yaml:"format"`
	// Name 指定媒体名称。
	Name string `json:"name" yaml:"name"`
	// File 指定媒体文件路径。
	File string `json:"file" yaml:"file"`
	// DataBase64 提供 Base64 编码的媒体数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
}

// Page 描述文档页面及其图层、图元和页面资源。
type Page struct {
	// Templates 指定页面引用的模板页。
	Templates []TemplateRef `json:"templates" yaml:"templates"`
	// Layers 定义页面图层。
	Layers []Layer `json:"layers" yaml:"layers"`
	// Items 定义页面图元。
	Items []Item `json:"items" yaml:"items"`
	// Area 定义页面区域。
	Area *PageArea `json:"area" yaml:"area"`
	// LayerType 指定页面图层类型。
	LayerType string `json:"layerType" yaml:"layerType"`
	// Actions 定义页面级动作。
	Actions []Action `json:"actions" yaml:"actions"`
	// Resources 定义页面级资源。
	Resources []PageResource `json:"resources" yaml:"resources"`
}

// PublicResource 描述文档中的公共资源。
type PublicResource struct {
	// Name 指定公共资源名称。
	Name string `json:"name" yaml:"name"`
	// File 指定公共资源文件路径。
	File string `json:"file" yaml:"file"`
	// DataBase64 提供 Base64 编码的公共资源数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
	// Files 定义公共资源包中的文件。
	Files []ResourceFile `json:"files" yaml:"files"`
}

// PageResource 描述页面级资源文件或嵌入式图像。
type PageResource struct {
	// File 指定页面资源文件路径。
	File string `json:"file" yaml:"file"`
	// DataBase64 提供 Base64 编码的页面资源数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
	// Images 定义资源中的图像。
	Images []PageImage `json:"images" yaml:"images"`
	// Files 定义页面资源包中的文件。
	Files []ResourceFile `json:"files" yaml:"files"`
}

// PageImage 描述页面资源中的图像。
type PageImage struct {
	// ID 标识页面图像资源。
	ID uint64 `json:"id" yaml:"id"`
	// Format 指定图像格式。
	Format string `json:"format" yaml:"format"`
	// Name 指定图像名称。
	Name string `json:"name" yaml:"name"`
	// File 指定图像文件路径。
	File string `json:"file" yaml:"file"`
	// DataBase64 提供 Base64 编码的图像数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
}

// ResourceFile 描述资源包中的文件。
type ResourceFile struct {
	// Path 指定文件在资源包中的路径。
	Path string `json:"path" yaml:"path"`
	// File 指定源文件路径。
	File string `json:"file" yaml:"file"`
	// DataBase64 提供 Base64 编码的文件数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
}

// CustomTag 描述文档自定义标签及其架构和数据。
type CustomTag struct {
	// NameSpace 指定自定义标签命名空间。
	NameSpace string `json:"nameSpace" yaml:"nameSpace"`
	// Schema 指定架构文件路径。
	Schema string `json:"schema" yaml:"schema"`
	// SchemaBase64 提供 Base64 编码的架构数据。
	SchemaBase64 string `json:"schemaBase64" yaml:"schemaBase64"`
	// SchemaName 指定架构在文档中的名称。
	SchemaName string `json:"schemaName" yaml:"schemaName"`
	// Data 指定自定义数据文件路径或文本数据。
	Data string `json:"data" yaml:"data"`
	// DataBase64 提供 Base64 编码的自定义数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
	// DataName 指定自定义数据在文档中的名称。
	DataName string `json:"dataName" yaml:"dataName"`
}

// Permissions 描述文档的访问和操作权限。
type Permissions struct {
	// Edit 指示是否允许编辑文档。
	Edit *bool `json:"edit" yaml:"edit"`
	// Annot 指示是否允许添加或修改批注。
	Annot *bool `json:"annot" yaml:"annot"`
	// Export 指示是否允许导出文档。
	Export *bool `json:"export" yaml:"export"`
	// Signature 指示是否允许签名。
	Signature *bool `json:"signature" yaml:"signature"`
	// Watermark 指示是否允许修改水印。
	Watermark *bool `json:"watermark" yaml:"watermark"`
	// PrintScreen 指示是否允许屏幕截图。
	PrintScreen *bool `json:"printScreen" yaml:"printScreen"`
	// Print 定义打印权限和设置。
	Print *PrintSettings `json:"print" yaml:"print"`
	// ValidPeriod 定义权限的有效时间范围。
	ValidPeriod *ValidPeriod `json:"validPeriod" yaml:"validPeriod"`
}

// PrintSettings 描述文档打印设置。
type PrintSettings struct {
	// Printable 指示是否允许打印。
	Printable bool `json:"printable" yaml:"printable"`
	// Copies 指定允许或默认的打印份数。
	Copies *int `json:"copies" yaml:"copies"`
}

// ValidPeriod 描述权限的有效时间范围。
type ValidPeriod struct {
	// Start 指定权限生效时间。
	Start string `json:"start" yaml:"start"`
	// End 指定权限失效时间。
	End string `json:"end" yaml:"end"`
}

// Template 描述可供页面引用的模板页。
type Template struct {
	// ID 标识模板页。
	ID uint64 `json:"id" yaml:"id"`
	// Name 指定模板页名称。
	Name string `json:"name" yaml:"name"`
	// ZOrder 指定模板页的层叠顺序。
	ZOrder string `json:"zOrder" yaml:"zOrder"`
	// Layers 定义模板页图层。
	Layers []Layer `json:"layers" yaml:"layers"`
	// Items 定义模板页图元。
	Items []Item `json:"items" yaml:"items"`
	// Area 定义模板页区域。
	Area *PageArea `json:"area" yaml:"area"`
}

// TemplateRef 描述页面对模板页的引用。
type TemplateRef struct {
	// ID 指定所引用的模板页。
	ID uint64 `json:"id" yaml:"id"`
	// ZOrder 指定模板引用的层叠顺序。
	ZOrder string `json:"zOrder" yaml:"zOrder"`
}

// Layer 描述页面或模板中的图层。
type Layer struct {
	// Type 指定图层类型。
	Type string `json:"type" yaml:"type"`
	// DrawParam 指定图层使用的绘制参数名称。
	DrawParam string `json:"drawParam" yaml:"drawParam"`
	// Items 定义图层中的图元。
	Items []Item `json:"items" yaml:"items"`
}

// PageArea 描述页面的物理、应用、内容和出血区域。
type PageArea struct {
	// PhysicalBox 定义页面物理区域。
	PhysicalBox *Box `json:"physicalBox" yaml:"physicalBox"`
	// ApplicationBox 定义页面应用区域。
	ApplicationBox *Box `json:"applicationBox" yaml:"applicationBox"`
	// ContentBox 定义页面内容区域。
	ContentBox *Box `json:"contentBox" yaml:"contentBox"`
	// BleedBox 定义页面出血区域。
	BleedBox *Box `json:"bleedBox" yaml:"bleedBox"`
}

// CustomData 描述文档自定义键值数据。
type CustomData struct {
	// Name 指定自定义数据名称。
	Name string `json:"name" yaml:"name"`
	// Value 指定自定义数据值。
	Value string `json:"value" yaml:"value"`
}

// Preferences 描述文档查看偏好设置。
type Preferences struct {
	// PageMode 指定页面显示模式。
	PageMode string `json:"pageMode" yaml:"pageMode"`
	// PageLayout 指定页面布局方式。
	PageLayout string `json:"pageLayout" yaml:"pageLayout"`
	// TabDisplay 指定文档标签页显示方式。
	TabDisplay string `json:"tabDisplay" yaml:"tabDisplay"`
	// HideToolbar 指示是否隐藏工具栏。
	HideToolbar *bool `json:"hideToolbar" yaml:"hideToolbar"`
	// HideMenubar 指示是否隐藏菜单栏。
	HideMenubar *bool `json:"hideMenubar" yaml:"hideMenubar"`
	// HideWindowUI 指示是否隐藏窗口界面元素。
	HideWindowUI *bool `json:"hideWindowUI" yaml:"hideWindowUI"`
	// ZoomMode 指定缩放模式。
	ZoomMode string `json:"zoomMode" yaml:"zoomMode"`
	// Zoom 指定页面缩放比例。
	Zoom *float64 `json:"zoom" yaml:"zoom"`
}

// ColorSpace 描述文档使用的颜色空间资源。
type ColorSpace struct {
	// ID 标识颜色空间资源。
	ID uint64 `json:"id" yaml:"id"`
	// Type 指定颜色空间类型。
	Type string `json:"type" yaml:"type"`
	// BitsPerComponent 指定每个颜色分量的位数。
	BitsPerComponent int `json:"bitsPerComponent" yaml:"bitsPerComponent"`
	// Palette 定义颜色索引到颜色分量的映射。
	Palette []string `json:"palette" yaml:"palette"`
	// ProfileFile 指定颜色配置文件路径。
	ProfileFile string `json:"profileFile" yaml:"profileFile"`
	// ProfileBase64 提供 Base64 编码的颜色配置数据。
	ProfileBase64 string `json:"profileBase64" yaml:"profileBase64"`
	// ProfileName 指定颜色配置文件名称。
	ProfileName string `json:"profileName" yaml:"profileName"`
}

// DrawParam 描述图形绘制参数。
type DrawParam struct {
	// Name 指定绘制参数名称。
	Name string `json:"name" yaml:"name"`
	// Relative 指定参数相对的坐标或对象。
	Relative string `json:"relative" yaml:"relative"`
	// LineWidth 指定线宽。
	LineWidth float64 `json:"lineWidth" yaml:"lineWidth"`
	// Join 指定线段连接样式。
	Join string `json:"join" yaml:"join"`
	// Cap 指定线段端点样式。
	Cap string `json:"cap" yaml:"cap"`
	// DashOffset 指定虚线起始偏移量。
	DashOffset float64 `json:"dashOffset" yaml:"dashOffset"`
	// DashPattern 指定虚线长度模式。
	DashPattern []float64 `json:"dashPattern" yaml:"dashPattern"`
	// MiterLimit 指定斜接长度限制。
	MiterLimit float64 `json:"miterLimit" yaml:"miterLimit"`
	// FillColor 指定填充颜色。
	FillColor *Color `json:"fillColor" yaml:"fillColor"`
	// StrokeColor 指定描边颜色。
	StrokeColor *Color `json:"strokeColor" yaml:"strokeColor"`
}

// Composite 描述复合图形单元资源。
type Composite struct {
	// ID 标识复合图形单元。
	ID uint64 `json:"id" yaml:"id"`
	// Width 指定复合图形宽度。
	Width float64 `json:"width" yaml:"width"`
	// Height 指定复合图形高度。
	Height float64 `json:"height" yaml:"height"`
	// Thumbnail 指定缩略图资源编号。
	Thumbnail uint64 `json:"thumbnail" yaml:"thumbnail"`
	// Substitution 指定替代图形资源编号。
	Substitution uint64 `json:"substitution" yaml:"substitution"`
	// Items 定义复合图形中的图元。
	Items []Item `json:"items" yaml:"items"`
}

// Color 描述颜色及其渐变或图案填充信息。
type Color struct {
	// R 指定红色分量。
	R uint8 `json:"r" yaml:"r"`
	// G 指定绿色分量。
	G uint8 `json:"g" yaml:"g"`
	// B 指定蓝色分量。
	B uint8 `json:"b" yaml:"b"`
	// Components 指定颜色空间分量值。
	Components []int `json:"components" yaml:"components"`
	// ColorSpace 指定颜色空间资源编号。
	ColorSpace uint64 `json:"colorSpace" yaml:"colorSpace"`
	// Index 指定调色板颜色索引。
	Index *int `json:"index" yaml:"index"`
	// Alpha 指定颜色透明度。
	Alpha *uint8 `json:"alpha" yaml:"alpha"`
	// Axial 定义轴向渐变。
	Axial *AxialShading `json:"axial" yaml:"axial"`
	// Radial 定义径向渐变。
	Radial *RadialShading `json:"radial" yaml:"radial"`
	// Gouraud 定义三角网格渐变。
	Gouraud *Gouraud `json:"gouraud" yaml:"gouraud"`
	// LaGouraud 定义四边形网格渐变。
	LaGouraud *LaGouraud `json:"laGouraud" yaml:"laGouraud"`
	// Pattern 定义图案填充。
	Pattern *Pattern `json:"pattern" yaml:"pattern"`
}

// ColorStop 描述渐变中的颜色停止点。
type ColorStop struct {
	// Position 指定颜色在渐变中的位置。
	Position float64 `json:"position" yaml:"position"`
	// Color 指定该位置的颜色。
	Color Color `json:"color" yaml:"color"`
}

// AxialShading 描述轴向渐变。
type AxialShading struct {
	// MapType 指定渐变映射类型。
	MapType string `json:"mapType" yaml:"mapType"`
	// MapUnit 指定渐变映射单位。
	MapUnit float64 `json:"mapUnit" yaml:"mapUnit"`
	// Extend 指定渐变端点的延伸方式。
	Extend int `json:"extend" yaml:"extend"`
	// StartPoint 指定渐变起点。
	StartPoint string `json:"startPoint" yaml:"startPoint"`
	// EndPoint 指定渐变终点。
	EndPoint string `json:"endPoint" yaml:"endPoint"`
	// Segments 定义渐变颜色停止点。
	Segments []ColorStop `json:"segments" yaml:"segments"`
}

// RadialShading 描述径向渐变。
type RadialShading struct {
	// MapType 指定渐变映射类型。
	MapType string `json:"mapType" yaml:"mapType"`
	// MapUnit 指定渐变映射单位。
	MapUnit float64 `json:"mapUnit" yaml:"mapUnit"`
	// Eccentricity 指定径向渐变椭圆离心率。
	Eccentricity float64 `json:"eccentricity" yaml:"eccentricity"`
	// Angle 指定径向渐变旋转角度。
	Angle float64 `json:"angle" yaml:"angle"`
	// StartPoint 指定渐变起点圆心。
	StartPoint string `json:"startPoint" yaml:"startPoint"`
	// StartRadius 指定渐变起始半径。
	StartRadius float64 `json:"startRadius" yaml:"startRadius"`
	// EndPoint 指定渐变终点圆心。
	EndPoint string `json:"endPoint" yaml:"endPoint"`
	// EndRadius 指定渐变结束半径。
	EndRadius float64 `json:"endRadius" yaml:"endRadius"`
	// Extend 指定渐变端点的延伸方式。
	Extend int `json:"extend" yaml:"extend"`
	// Segments 定义渐变颜色停止点。
	Segments []ColorStop `json:"segments" yaml:"segments"`
}

// Gouraud 描述 Gouraud 三角网格渐变。
type Gouraud struct {
	// Extend 指定渐变边界的延伸方式。
	Extend int `json:"extend" yaml:"extend"`
	// Points 定义三角网格顶点。
	Points []GouraudPoint `json:"points" yaml:"points"`
	// BackColor 指定网格背景颜色。
	BackColor *Color `json:"backColor" yaml:"backColor"`
}

// GouraudPoint 描述 Gouraud 渐变网格中的顶点。
type GouraudPoint struct {
	// X 指定顶点横坐标。
	X float64 `json:"x" yaml:"x"`
	// Y 指定顶点纵坐标。
	Y float64 `json:"y" yaml:"y"`
	// EdgeFlag 指定顶点边标志。
	EdgeFlag int `json:"edgeFlag" yaml:"edgeFlag"`
	// Color 指定顶点颜色。
	Color Color `json:"color" yaml:"color"`
}

// LaGouraud 描述 LaGouraud 四边形网格渐变。
type LaGouraud struct {
	// VerticesPerRow 指定每行顶点数。
	VerticesPerRow int `json:"verticesPerRow" yaml:"verticesPerRow"`
	// Extend 指定渐变边界的延伸方式。
	Extend int `json:"extend" yaml:"extend"`
	// Points 定义四边形网格顶点。
	Points []LaGouraudPoint `json:"points" yaml:"points"`
	// BackColor 指定网格背景颜色。
	BackColor *Color `json:"backColor" yaml:"backColor"`
}

// LaGouraudPoint 描述 LaGouraud 渐变网格中的顶点。
type LaGouraudPoint struct {
	// X 指定顶点横坐标。
	X float64 `json:"x" yaml:"x"`
	// Y 指定顶点纵坐标。
	Y float64 `json:"y" yaml:"y"`
	// Color 指定顶点颜色。
	Color Color `json:"color" yaml:"color"`
}

// Pattern 描述图案填充及其内容。
type Pattern struct {
	// Width 指定图案单元宽度。
	Width float64 `json:"width" yaml:"width"`
	// Height 指定图案单元高度。
	Height float64 `json:"height" yaml:"height"`
	// XStep 指定图案在横向的重复步长。
	XStep float64 `json:"xStep" yaml:"xStep"`
	// YStep 指定图案在纵向的重复步长。
	YStep float64 `json:"yStep" yaml:"yStep"`
	// ReflectMethod 指定图案反射方式。
	ReflectMethod string `json:"reflectMethod" yaml:"reflectMethod"`
	// RelativeTo 指定图案定位所依据的对象。
	RelativeTo string `json:"relativeTo" yaml:"relativeTo"`
	// CTM 指定图案的坐标变换矩阵。
	CTM []float64 `json:"ctm" yaml:"ctm"`
	// Thumbnail 指定图案缩略图资源编号。
	Thumbnail uint64 `json:"thumbnail" yaml:"thumbnail"`
	// Items 定义图案中的图元。
	Items []Item `json:"items" yaml:"items"`
	// Layers 定义图案中的图层。
	Layers []Layer `json:"layers" yaml:"layers"`
}

// TextCode 描述文本内容及其定位信息。
type TextCode struct {
	// Value 指定文本内容。
	Value string `json:"value" yaml:"value"`
	// X 指定文本代码横坐标。
	X *float64 `json:"x" yaml:"x"`
	// Y 指定文本代码纵坐标。
	Y *float64 `json:"y" yaml:"y"`
	// DeltaX 指定连续字符的横向偏移量。
	DeltaX []float64 `json:"deltaX" yaml:"deltaX"`
	// DeltaY 指定连续字符的纵向偏移量。
	DeltaY []float64 `json:"deltaY" yaml:"deltaY"`
}

// Action 描述文档、页面或图元触发的动作。
type Action struct {
	// Event 指定触发动作的事件。
	Event string `json:"event" yaml:"event"`
	// Region 定义动作触发区域。
	Region *ActionRegion `json:"region" yaml:"region"`
	// URI 定义打开或跳转到 URI 的动作。
	URI *URIAction `json:"uri" yaml:"uri"`
	// Goto 定义跳转到页面或位置的动作。
	Goto *GotoAction `json:"goto" yaml:"goto"`
	// GotoA 定义跳转到附件的动作。
	GotoA *GotoAAction `json:"gotoA" yaml:"gotoA"`
	// Sound 定义播放声音资源的动作。
	Sound *SoundAction `json:"sound" yaml:"sound"`
	// Movie 定义播放媒体资源的动作。
	Movie *MovieAction `json:"movie" yaml:"movie"`
}

// ActionRegion 描述动作的触发区域。
type ActionRegion struct {
	// Areas 定义动作区域中的路径。
	Areas []ActionArea `json:"areas" yaml:"areas"`
}

// ActionArea 描述触发区域中的一条路径。
type ActionArea struct {
	// Start 指定路径起点。
	Start Point `json:"start" yaml:"start"`
	// Commands 定义路径绘制命令。
	Commands []RegionCommand `json:"commands" yaml:"commands"`
}

// Point 描述二维坐标点。
type Point struct {
	// X 指定横坐标。
	X float64 `json:"x" yaml:"x"`
	// Y 指定纵坐标。
	Y float64 `json:"y" yaml:"y"`
}

// RegionCommand 描述动作区域路径中的绘制命令。
type RegionCommand struct {
	// Type 指定路径命令类型。
	Type string `json:"type" yaml:"type"`
	// X 指定命令终点横坐标。
	X float64 `json:"x" yaml:"x"`
	// Y 指定命令终点纵坐标。
	Y float64 `json:"y" yaml:"y"`
	// ControlX 指定二次贝塞尔曲线控制点横坐标。
	ControlX float64 `json:"controlX" yaml:"controlX"`
	// ControlY 指定二次贝塞尔曲线控制点纵坐标。
	ControlY float64 `json:"controlY" yaml:"controlY"`
	// Control1X 指定三次贝塞尔曲线第一个控制点横坐标。
	Control1X float64 `json:"control1X" yaml:"control1X"`
	// Control1Y 指定三次贝塞尔曲线第一个控制点纵坐标。
	Control1Y float64 `json:"control1Y" yaml:"control1Y"`
	// Control2X 指定三次贝塞尔曲线第二个控制点横坐标。
	Control2X float64 `json:"control2X" yaml:"control2X"`
	// Control2Y 指定三次贝塞尔曲线第二个控制点纵坐标。
	Control2Y float64 `json:"control2Y" yaml:"control2Y"`
	// SweepDirection 指定椭圆弧的扫描方向。
	SweepDirection bool `json:"sweepDirection" yaml:"sweepDirection"`
	// LargeArc 指示椭圆弧是否取大弧段。
	LargeArc bool `json:"largeArc" yaml:"largeArc"`
	// RotationAngle 指定椭圆弧旋转角度。
	RotationAngle float64 `json:"rotationAngle" yaml:"rotationAngle"`
	// EllipseWidth 指定椭圆弧椭圆宽度。
	EllipseWidth float64 `json:"ellipseWidth" yaml:"ellipseWidth"`
	// EllipseHeight 指定椭圆弧椭圆高度。
	EllipseHeight float64 `json:"ellipseHeight" yaml:"ellipseHeight"`
}

// URIAction 描述打开或跳转到 URI 的动作。
type URIAction struct {
	// URI 指定目标 URI。
	URI string `json:"uri" yaml:"uri"`
	// Base 指定 URI 基础地址。
	Base string `json:"base" yaml:"base"`
	// Target 指定 URI 打开目标。
	Target string `json:"target" yaml:"target"`
}

// GotoAction 描述跳转到页面、书签或页面位置的动作。
type GotoAction struct {
	// Page 指定跳转目标页码。
	Page int `json:"page" yaml:"page"`
	// Type 指定跳转目标类型。
	Type string `json:"type" yaml:"type"`
	// Bookmark 指定跳转目标书签名称。
	Bookmark string `json:"bookmark" yaml:"bookmark"`
	// Left 指定目标视图左边界。
	Left *float64 `json:"left" yaml:"left"`
	// Top 指定目标视图上边界。
	Top *float64 `json:"top" yaml:"top"`
	// Right 指定目标视图右边界。
	Right *float64 `json:"right" yaml:"right"`
	// Bottom 指定目标视图下边界。
	Bottom *float64 `json:"bottom" yaml:"bottom"`
	// Zoom 指定目标视图缩放比例。
	Zoom *float64 `json:"zoom" yaml:"zoom"`
}

// GotoAAction 描述跳转到附件的动作。
type GotoAAction struct {
	// AttachID 指定跳转目标附件标识。
	AttachID string `json:"attachId" yaml:"attachId"`
	// NewWindow 指示是否在新窗口打开附件。
	NewWindow *bool `json:"newWindow" yaml:"newWindow"`
}

// SoundAction 描述播放声音资源的动作。
type SoundAction struct {
	// ResourceID 指定声音资源编号。
	ResourceID uint64 `json:"resourceId" yaml:"resourceId"`
	// Volume 指定播放音量。
	Volume *int `json:"volume" yaml:"volume"`
	// Repeat 指示是否循环播放。
	Repeat *bool `json:"repeat" yaml:"repeat"`
	// Synchronous 指示是否同步播放。
	Synchronous *bool `json:"synchronous" yaml:"synchronous"`
}

// MovieAction 描述播放媒体资源的动作。
type MovieAction struct {
	// ResourceID 指定媒体资源编号。
	ResourceID uint64 `json:"resourceId" yaml:"resourceId"`
	// Operator 指定媒体操作类型。
	Operator string `json:"operator" yaml:"operator"`
}

// Bookmark 描述文档书签及其跳转目标。
type Bookmark struct {
	// Name 指定书签名称。
	Name string `json:"name" yaml:"name"`
	// Goto 指定书签跳转目标。
	Goto GotoAction `json:"goto" yaml:"goto"`
}

// Attachment 描述文档附件。
type Attachment struct {
	// ID 标识附件。
	ID string `json:"id" yaml:"id"`
	// Name 指定附件名称。
	Name string `json:"name" yaml:"name"`
	// Format 指定附件格式。
	Format string `json:"format" yaml:"format"`
	// Usage 说明附件用途。
	Usage string `json:"usage" yaml:"usage"`
	// File 指定附件文件路径。
	File string `json:"file" yaml:"file"`
	// DataBase64 提供 Base64 编码的附件数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
	// FileName 指定附件在文档中的文件名。
	FileName string `json:"fileName" yaml:"fileName"`
	// CreationDate 记录附件创建时间。
	CreationDate string `json:"creationDate" yaml:"creationDate"`
	// ModDate 记录附件最后修改时间。
	ModDate string `json:"modDate" yaml:"modDate"`
	// Visible 指示附件是否可见。
	Visible *bool `json:"visible" yaml:"visible"`
}

// Extension 描述文档扩展信息。
type Extension struct {
	// AppName 指定扩展信息来源应用名称。
	AppName string `json:"appName" yaml:"appName"`
	// Company 指定扩展信息来源公司。
	Company string `json:"company" yaml:"company"`
	// AppVersion 指定来源应用版本。
	AppVersion string `json:"appVersion" yaml:"appVersion"`
	// Date 记录扩展信息生成时间。
	Date string `json:"date" yaml:"date"`
	// RefID 指定扩展信息引用标识。
	RefID uint64 `json:"refId" yaml:"refId"`
	// Data 指定扩展数据。
	Data string `json:"data" yaml:"data"`
	// DataXML 指定扩展 XML 数据。
	DataXML string `json:"dataXml" yaml:"dataXml"`
	// DataFile 指定扩展数据文件路径。
	DataFile string `json:"dataFile" yaml:"dataFile"`
	// DataFileBase64 提供 Base64 编码的扩展文件数据。
	DataFileBase64 string `json:"dataFileBase64" yaml:"dataFileBase64"`
	// DataName 指定扩展数据文件名称。
	DataName string `json:"dataName" yaml:"dataName"`
	// Properties 定义扩展属性。
	Properties []ExtensionProperty `json:"properties" yaml:"properties"`
}

// ExtensionProperty 描述扩展信息的属性。
type ExtensionProperty struct {
	// Name 指定属性名称。
	Name string `json:"name" yaml:"name"`
	// Type 指定属性类型。
	Type string `json:"type" yaml:"type"`
	// Value 指定属性值。
	Value string `json:"value" yaml:"value"`
}

// Version 描述文档的一个版本。
type Version struct {
	// ID 标识文档版本。
	ID string `json:"id" yaml:"id"`
	// Index 指定版本序号。
	Index int `json:"index" yaml:"index"`
	// Current 指示是否为当前版本。
	Current bool `json:"current" yaml:"current"`
	// Version 指定版本号。
	Version string `json:"version" yaml:"version"`
	// Name 指定版本名称。
	Name string `json:"name" yaml:"name"`
	// CreationDate 记录版本创建时间。
	CreationDate string `json:"creationDate" yaml:"creationDate"`
	// DocRoot 指定版本文档根文件路径。
	DocRoot string `json:"docRoot" yaml:"docRoot"`
	// DocRootBase64 提供 Base64 编码的版本文档根数据。
	DocRootBase64 string `json:"docRootBase64" yaml:"docRootBase64"`
	// DocRootName 指定版本文档根文件名称。
	DocRootName string `json:"docRootName" yaml:"docRootName"`
	// Files 定义版本包含的文件。
	Files []VersionFile `json:"files" yaml:"files"`
}

// VersionFile 描述文档版本中的文件。
type VersionFile struct {
	// ID 标识版本文件。
	ID string `json:"id" yaml:"id"`
	// Path 指定文件在版本中的路径。
	Path string `json:"path" yaml:"path"`
}

// Outline 描述文档大纲节点及其子节点。
type Outline struct {
	// Title 指定大纲节点标题。
	Title string `json:"title" yaml:"title"`
	// Count 指定大纲节点包含的条目数。
	Count *int `json:"count" yaml:"count"`
	// Expanded 指示大纲节点是否展开。
	Expanded *bool `json:"expanded" yaml:"expanded"`
	// Actions 定义大纲节点动作。
	Actions []Action `json:"actions" yaml:"actions"`
	// Children 定义大纲子节点。
	Children []Outline `json:"children" yaml:"children"`
}

// Signature 描述文档数字签名。
type Signature struct {
	// ID 标识数字签名。
	ID string `json:"id" yaml:"id"`
	// Type 指定签名类型。
	Type string `json:"type" yaml:"type"`
	// ProviderName 指定签名提供方名称。
	ProviderName string `json:"providerName" yaml:"providerName"`
	// ProviderVersion 指定签名提供方版本。
	ProviderVersion string `json:"providerVersion" yaml:"providerVersion"`
	// Company 指定签名提供方公司。
	Company string `json:"company" yaml:"company"`
	// Method 指定签名方法。
	Method string `json:"method" yaml:"method"`
	// Date 记录签名时间。
	Date string `json:"date" yaml:"date"`
	// CheckMethod 指定签名校验方法。
	CheckMethod string `json:"checkMethod" yaml:"checkMethod"`
	// References 定义签名引用及其校验值。
	References []SignatureReference `json:"references" yaml:"references"`
	// StampAnnots 定义签名盖章区域。
	StampAnnots []SignatureStamp `json:"stampAnnots" yaml:"stampAnnots"`
	// SealFile 指定印章文件路径。
	SealFile string `json:"sealFile" yaml:"sealFile"`
	// SealFileBase64 提供 Base64 编码的印章数据。
	SealFileBase64 string `json:"sealFileBase64" yaml:"sealFileBase64"`
	// SealName 指定印章文件名称。
	SealName string `json:"sealName" yaml:"sealName"`
	// SignedValue 提供 Base64 编码的签名值。
	SignedValue string `json:"signedValue" yaml:"signedValue"`
	// SignedValueName 指定签名值文件名称。
	SignedValueName string `json:"signedValueName" yaml:"signedValueName"`
}

// SignatureReference 描述数字签名引用的文件及校验值。
type SignatureReference struct {
	// FileRef 指定签名引用的文件路径。
	FileRef string `json:"fileRef" yaml:"fileRef"`
	// CheckValue 提供 Base64 编码的文件校验值。
	CheckValue string `json:"checkValue" yaml:"checkValue"`
}

// SignatureStamp 描述数字签名在页面上的盖章区域。
type SignatureStamp struct {
	// ID 标识签名盖章区域。
	ID string `json:"id" yaml:"id"`
	// Page 指定盖章所在页码。
	Page int `json:"page" yaml:"page"`
	// Boundary 指定盖章区域边界。
	Boundary Box `json:"boundary" yaml:"boundary"`
	// Clip 指定盖章区域裁剪框。
	Clip *Box `json:"clip" yaml:"clip"`
}

// Box 描述矩形区域。
type Box struct {
	// X 指定矩形左上角横坐标。
	X float64 `json:"x" yaml:"x"`
	// Y 指定矩形左上角纵坐标。
	Y float64 `json:"y" yaml:"y"`
	// Width 指定矩形宽度。
	Width float64 `json:"width" yaml:"width"`
	// Height 指定矩形高度。
	Height float64 `json:"height" yaml:"height"`
}

// AnnotationPage 描述某一页面上的批注集合。
type AnnotationPage struct {
	// Page 指定批注所属页码。
	Page int `json:"page" yaml:"page"`
	// Items 定义该页面上的批注。
	Items []Annotation `json:"items" yaml:"items"`
}

// Annotation 描述文档批注。
type Annotation struct {
	// ID 标识批注。
	ID uint64 `json:"id" yaml:"id"`
	// Type 指定批注类型。
	Type string `json:"type" yaml:"type"`
	// Creator 指定批注创建者。
	Creator string `json:"creator" yaml:"creator"`
	// LastModDate 记录批注最后修改时间。
	LastModDate string `json:"lastModDate" yaml:"lastModDate"`
	// Visible 指示批注是否可见。
	Visible *bool `json:"visible" yaml:"visible"`
	// Subtype 指定批注子类型。
	Subtype string `json:"subtype" yaml:"subtype"`
	// Print 指示批注是否随文档打印。
	Print *bool `json:"print" yaml:"print"`
	// NoZoom 指示批注是否不随页面缩放。
	NoZoom bool `json:"noZoom" yaml:"noZoom"`
	// NoRotate 指示批注是否不随页面旋转。
	NoRotate bool `json:"noRotate" yaml:"noRotate"`
	// ReadOnly 指示批注是否只读。
	ReadOnly *bool `json:"readOnly" yaml:"readOnly"`
	// Remark 记录批注说明。
	Remark string `json:"remark" yaml:"remark"`
	// Parameters 定义批注自定义参数。
	Parameters []AnnotationParameter `json:"parameters" yaml:"parameters"`
	// Boundary 指定批注边界。
	Boundary *Box `json:"boundary" yaml:"boundary"`
	// Items 定义批注包含的图元。
	Items []Item `json:"items" yaml:"items"`
}

// AnnotationParameter 描述批注的自定义参数。
type AnnotationParameter struct {
	// Name 指定批注参数名称。
	Name string `json:"name" yaml:"name"`
	// Value 指定批注参数值。
	Value string `json:"value" yaml:"value"`
}

// CGTransform 描述文本代码与字形之间的映射关系。
type CGTransform struct {
	// CodePosition 指定文本代码起始位置。
	CodePosition int `json:"codePosition" yaml:"codePosition"`
	// CodeCount 指定映射的文本代码数量。
	CodeCount int `json:"codeCount" yaml:"codeCount"`
	// GlyphCount 指定对应的字形数量。
	GlyphCount int `json:"glyphCount" yaml:"glyphCount"`
	// Glyphs 指定对应字形编号列表。
	Glyphs []int `json:"glyphs" yaml:"glyphs"`
}

// Item 描述页面、模板或复合图形中的图元。
type Item struct {
	// Type 指定图元类型。
	Type string `json:"type" yaml:"type"`
	// X 指定图元左上角横坐标。
	X float64 `json:"x" yaml:"x"`
	// Y 指定图元左上角纵坐标。
	Y float64 `json:"y" yaml:"y"`
	// Width 指定图元宽度。
	Width float64 `json:"width" yaml:"width"`
	// Height 指定图元高度。
	Height float64 `json:"height" yaml:"height"`
	// Value 指定图元文本或其他值。
	Value string `json:"value" yaml:"value"`
	// Font 指定文本图元使用的字体名称。
	Font string `json:"font" yaml:"font"`
	// Size 指定文本字号。
	Size float64 `json:"size" yaml:"size"`
	// Data 指定图元数据或文件路径。
	Data string `json:"data" yaml:"data"`
	// DataBase64 提供 Base64 编码的图元数据。
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
	// Format 指定图元数据格式。
	Format string `json:"format" yaml:"format"`
	// Name 指定图元名称。
	Name string `json:"name" yaml:"name"`
	// Visible 指示图元是否可见。
	Visible *bool `json:"visible" yaml:"visible"`
	// ResourceID 指定图元引用的资源编号。
	ResourceID uint64 `json:"resourceId" yaml:"resourceId"`
	// Stroke 指示是否绘制轮廓线。
	Stroke bool `json:"stroke" yaml:"stroke"`
	// Fill 指示是否填充图元。
	Fill *bool `json:"fill" yaml:"fill"`
	// StrokeSet 指示是否显式设置描边属性。
	StrokeSet *bool `json:"strokeSet" yaml:"strokeSet"`
	// Rule 指定填充规则。
	Rule string `json:"rule" yaml:"rule"`
	// LineWidth 指定描边线宽。
	LineWidth float64 `json:"lineWidth" yaml:"lineWidth"`
	// Cap 指定描边端点样式。
	Cap string `json:"cap" yaml:"cap"`
	// Join 指定描边连接样式。
	Join string `json:"join" yaml:"join"`
	// MiterLimit 指定斜接长度限制。
	MiterLimit float64 `json:"miterLimit" yaml:"miterLimit"`
	// DashOffset 指定虚线起始偏移量。
	DashOffset float64 `json:"dashOffset" yaml:"dashOffset"`
	// DashPattern 指定虚线长度模式。
	DashPattern []float64 `json:"dashPattern" yaml:"dashPattern"`
	// Alpha 指定图元透明度。
	Alpha *uint8 `json:"alpha" yaml:"alpha"`
	// DrawParam 指定图元使用的绘制参数名称。
	DrawParam string `json:"drawParam" yaml:"drawParam"`
	// CTM 指定图元的坐标变换矩阵。
	CTM []float64 `json:"ctm" yaml:"ctm"`
	// FillColor 指定填充颜色。
	FillColor *Color `json:"fillColor" yaml:"fillColor"`
	// StrokeColor 指定描边颜色。
	StrokeColor *Color `json:"strokeColor" yaml:"strokeColor"`
	// TextCodes 定义文本代码及其定位信息。
	TextCodes []TextCode `json:"textCodes" yaml:"textCodes"`
	// CGTransforms 定义文本代码到字形的映射。
	CGTransforms []CGTransform `json:"cgTransforms" yaml:"cgTransforms"`
	// Actions 定义图元触发的动作。
	Actions []Action `json:"actions" yaml:"actions"`
	// Items 定义复合或页面块图元的子图元。
	Items []Item `json:"items" yaml:"items"`
	// HScale 指定文本水平方向缩放比例。
	HScale float64 `json:"hScale" yaml:"hScale"`
	// ReadDirection 指定文本阅读方向。
	ReadDirection int `json:"readDirection" yaml:"readDirection"`
	// CharDirection 指定字符排列方向。
	CharDirection int `json:"charDirection" yaml:"charDirection"`
	// Weight 指定文本字重。
	Weight int `json:"weight" yaml:"weight"`
	// Italic 指示文本是否为斜体。
	Italic bool `json:"italic" yaml:"italic"`
	// Clips 定义图元裁剪区域。
	Clips []Clip `json:"clips" yaml:"clips"`
	// Substitution 指定图像替代资源编号。
	Substitution uint64 `json:"substitution" yaml:"substitution"`
	// ImageMask 指定图像掩码资源编号。
	ImageMask uint64 `json:"imageMask" yaml:"imageMask"`
	// Border 定义图像边框样式。
	Border *ImageBorder `json:"border" yaml:"border"`
}

// Clip 描述图元的裁剪区域集合。
type Clip struct {
	// Areas 定义裁剪区域。
	Areas []ClipArea `json:"areas" yaml:"areas"`
}

// ClipArea 描述裁剪区域中的一个路径或文本区域。
type ClipArea struct {
	// DrawParam 指定裁剪区域使用的绘制参数名称。
	DrawParam string `json:"drawParam" yaml:"drawParam"`
	// CTM 指定裁剪区域的坐标变换矩阵。
	CTM []float64 `json:"ctm" yaml:"ctm"`
	// Path 定义路径裁剪内容。
	Path *ClipPath `json:"path" yaml:"path"`
	// Text 定义文本裁剪内容。
	Text *ClipText `json:"text" yaml:"text"`
}

// ClipPath 描述用于裁剪的路径。
type ClipPath struct {
	// Boundary 指定裁剪路径边界。
	Boundary Box `json:"boundary" yaml:"boundary"`
	// Name 指定裁剪路径名称。
	Name string `json:"name" yaml:"name"`
	// Visible 指示裁剪路径是否可见。
	Visible *bool `json:"visible" yaml:"visible"`
	// CTM 指定裁剪路径的坐标变换矩阵。
	CTM []float64 `json:"ctm" yaml:"ctm"`
	// Data 指定裁剪路径数据。
	Data string `json:"data" yaml:"data"`
	// Stroke 指示是否绘制裁剪路径轮廓。
	Stroke bool `json:"stroke" yaml:"stroke"`
	// StrokeSet 指示是否显式设置描边属性。
	StrokeSet *bool `json:"strokeSet" yaml:"strokeSet"`
	// Fill 指示是否填充裁剪路径。
	Fill bool `json:"fill" yaml:"fill"`
	// Rule 指定裁剪路径填充规则。
	Rule string `json:"rule" yaml:"rule"`
	// LineWidth 指定裁剪路径描边线宽。
	LineWidth float64 `json:"lineWidth" yaml:"lineWidth"`
	// Cap 指定裁剪路径端点样式。
	Cap string `json:"cap" yaml:"cap"`
	// Join 指定裁剪路径连接样式。
	Join string `json:"join" yaml:"join"`
	// MiterLimit 指定斜接长度限制。
	MiterLimit float64 `json:"miterLimit" yaml:"miterLimit"`
	// DashOffset 指定虚线起始偏移量。
	DashOffset float64 `json:"dashOffset" yaml:"dashOffset"`
	// DashPattern 指定虚线长度模式。
	DashPattern []float64 `json:"dashPattern" yaml:"dashPattern"`
	// Alpha 指定裁剪路径透明度。
	Alpha *uint8 `json:"alpha" yaml:"alpha"`
	// StrokeColor 指定裁剪路径描边颜色。
	StrokeColor *Color `json:"strokeColor" yaml:"strokeColor"`
	// FillColor 指定裁剪路径填充颜色。
	FillColor *Color `json:"fillColor" yaml:"fillColor"`
}

// ClipText 描述用于裁剪的文本。
type ClipText struct {
	// Boundary 指定裁剪文本边界。
	Boundary Box `json:"boundary" yaml:"boundary"`
	// CTM 指定裁剪文本的坐标变换矩阵。
	CTM []float64 `json:"ctm" yaml:"ctm"`
	// Font 指定裁剪文本使用的字体名称。
	Font string `json:"font" yaml:"font"`
	// Size 指定裁剪文本字号。
	Size float64 `json:"size" yaml:"size"`
	// Value 指定裁剪文本内容。
	Value string `json:"value" yaml:"value"`
	// TextCodes 定义裁剪文本代码。
	TextCodes []TextCode `json:"textCodes" yaml:"textCodes"`
	// Stroke 指示是否绘制文本轮廓。
	Stroke bool `json:"stroke" yaml:"stroke"`
	// Fill 指示是否填充文本。
	Fill *bool `json:"fill" yaml:"fill"`
	// HScale 指定文本水平方向缩放比例。
	HScale float64 `json:"hScale" yaml:"hScale"`
	// ReadDirection 指定文本阅读方向。
	ReadDirection int `json:"readDirection" yaml:"readDirection"`
	// CharDirection 指定字符排列方向。
	CharDirection int `json:"charDirection" yaml:"charDirection"`
	// Weight 指定文本字重。
	Weight int `json:"weight" yaml:"weight"`
	// Italic 指示文本是否为斜体。
	Italic bool `json:"italic" yaml:"italic"`
	// FillColor 指定文本填充颜色。
	FillColor *Color `json:"fillColor" yaml:"fillColor"`
	// StrokeColor 指定文本描边颜色。
	StrokeColor *Color `json:"strokeColor" yaml:"strokeColor"`
}

// ImageBorder 描述图像边框的绘制样式。
type ImageBorder struct {
	// LineWidth 指定图像边框线宽。
	LineWidth float64 `json:"lineWidth" yaml:"lineWidth"`
	// HorizontalRadius 指定边框水平圆角半径。
	HorizontalRadius float64 `json:"horizontalRadius" yaml:"horizontalRadius"`
	// VerticalRadius 指定边框垂直圆角半径。
	VerticalRadius float64 `json:"verticalRadius" yaml:"verticalRadius"`
	// DashOffset 指定边框虚线起始偏移量。
	DashOffset float64 `json:"dashOffset" yaml:"dashOffset"`
	// DashPattern 指定边框虚线长度模式。
	DashPattern []float64 `json:"dashPattern" yaml:"dashPattern"`
	// Color 指定边框颜色。
	Color *Color `json:"color" yaml:"color"`
}

// Load 读取并解析 JSON、YAML 或 TOML manifest。
func Load(path, format string) (Manifest, string, error) {
	var data []byte
	var err error
	baseDir := ""
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
		if err == nil {
			baseDir, err = os.Getwd()
		}
	} else {
		data, err = os.ReadFile(path)
		if err == nil {
			baseDir, err = filepath.Abs(filepath.Dir(path))
		}
	}
	if err != nil {
		return Manifest{}, "", fmt.Errorf("读取 manifest 失败: %w", err)
	}
	if strings.TrimSpace(format) == "" || strings.EqualFold(format, "auto") {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if path == "-" || format == "" {
			format = "yaml"
		}
	}
	var result Manifest
	switch strings.ToLower(format) {
	case "json":
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&result); err != nil {
			return Manifest{}, "", fmt.Errorf("解析 JSON manifest 失败: %w", err)
		}
	case "yaml", "yml":
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&result); err != nil {
			return Manifest{}, "", fmt.Errorf("解析 YAML manifest 失败: %w", err)
		}
	case "toml":
		var values map[string]any
		if _, err := toml.Decode(string(data), &values); err != nil {
			return Manifest{}, "", fmt.Errorf("解析 TOML manifest 失败: %w", err)
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return Manifest{}, "", fmt.Errorf("转换 TOML manifest 失败: %w", err)
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&result); err != nil {
			return Manifest{}, "", fmt.Errorf("解析 TOML manifest 失败: %w", err)
		}
	default:
		return Manifest{}, "", fmt.Errorf("不支持的 manifest 格式: %q", format)
	}
	if baseDir == "" {
		return Manifest{}, "", errors.New("无法确定 manifest 所在目录")
	}
	return result, baseDir, nil
}

// Build 将 manifest 转换为 creator 文档模型，并加载资源文件。
func (m Manifest) Build(baseDir, assetRoot string) (creator.Document, error) {
	if m.Version != 1 {
		return creator.Document{}, fmt.Errorf("不支持的 manifest 版本: %d", m.Version)
	}
	if assetRoot == "" {
		assetRoot = baseDir
	} else if !filepath.IsAbs(assetRoot) {
		assetRoot = filepath.Join(baseDir, assetRoot)
	}
	root, err := filepath.Abs(assetRoot)
	if err != nil {
		return creator.Document{}, fmt.Errorf("资源根目录无效: %w", err)
	}
	document := creator.Document{
		ID: m.Document.ID, Title: m.Document.Title, Author: m.Document.Author,
		Subject: m.Document.Subject, Abstract: m.Document.Abstract,
		Creator: m.Document.Creator, CreatorVersion: m.Document.CreatorVersion,
		PageSize: creator.PageSize{Width: m.Document.PageSize.Width, Height: m.Document.PageSize.Height},
		DocUsage: m.Document.DocUsage, Keywords: m.Document.Keywords, DefaultCS: m.Document.DefaultCS,
	}
	if m.Document.Permissions != nil {
		permissions, err := buildPermissions(m.Document.Permissions)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.permissions: %w", err)
		}
		document.Permissions = permissions
	}
	for index, resource := range m.Resources.Public {
		data, err := loadData(root, resource.File, resource.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.public[%d].file: %w", index, err)
		}
		files, err := buildResourceFiles(root, resource.Files)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.public[%d].files: %w", index, err)
		}
		document.PublicRes = append(document.PublicRes, creator.PublicResource{Name: resource.Name, Data: data, Files: files})
	}
	for index, tag := range m.Resources.CustomTags {
		schema, err := loadData(root, tag.Schema, tag.SchemaBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.customTags[%d].schema: %w", index, err)
		}
		data, err := loadData(root, tag.Data, tag.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.customTags[%d].data: %w", index, err)
		}
		document.CustomTags = append(document.CustomTags, creator.CustomTag{NameSpace: tag.NameSpace, Schema: schema, SchemaName: tag.SchemaName, Data: data, DataName: tag.DataName})
	}
	if m.Document.Cover != "" || m.Document.CoverBase64 != "" {
		data, err := loadData(root, m.Document.Cover, m.Document.CoverBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.cover: %w", err)
		}
		document.CoverData, document.CoverName = data, m.Document.CoverName
	}
	if m.Document.CreationDate != "" {
		date, err := parseDate(m.Document.CreationDate)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.creationDate: %w", err)
		}
		document.CreationDate = date
	}
	if m.Document.ModDate != "" {
		date, err := parseDate(m.Document.ModDate)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.modDate: %w", err)
		}
		document.ModDate = date
	}
	if m.Document.Area != nil {
		document.Area = buildPageArea(m.Document.Area)
	}
	for _, data := range m.Document.CustomData {
		document.CustomDatas = append(document.CustomDatas, creator.CustomData{Name: data.Name, Value: data.Value})
	}
	if m.Document.Preferences != nil {
		document.Preferences = &creator.ViewPreferences{PageMode: m.Document.Preferences.PageMode, PageLayout: m.Document.Preferences.PageLayout, TabDisplay: m.Document.Preferences.TabDisplay, HideToolbar: m.Document.Preferences.HideToolbar, HideMenubar: m.Document.Preferences.HideMenubar, HideWindowUI: m.Document.Preferences.HideWindowUI, ZoomMode: m.Document.Preferences.ZoomMode, Zoom: m.Document.Preferences.Zoom}
	}
	outlines, err := buildOutlines(m.Document.Outlines)
	if err != nil {
		return creator.Document{}, fmt.Errorf("document.outlines: %w", err)
	}
	document.Outlines = outlines
	for index, signature := range m.Document.Signatures {
		value, err := buildSignature(root, signature)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.signatures[%d]: %w", index, err)
		}
		document.Signatures = append(document.Signatures, value)
	}
	for index, action := range m.Document.Actions {
		converted, err := buildAction(action)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.actions[%d]: %w", index, err)
		}
		document.Actions = append(document.Actions, converted)
	}
	for _, bookmark := range m.Document.Bookmarks {
		document.Bookmarks = append(document.Bookmarks, creator.Bookmark{Name: bookmark.Name, Goto: buildGoto(bookmark.Goto)})
	}
	for index, attachment := range m.Document.Attachments {
		data, err := loadData(root, attachment.File, attachment.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("document.attachments[%d].file: %w", index, err)
		}
		value := creator.Attachment{ID: attachment.ID, Name: attachment.Name, Format: attachment.Format, Usage: attachment.Usage, FileName: attachment.FileName, Visible: attachment.Visible, Data: data}
		if attachment.CreationDate != "" {
			value.CreationDate, err = parseDate(attachment.CreationDate)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.attachments[%d].creationDate: %w", index, err)
			}
		}
		if attachment.ModDate != "" {
			value.ModDate, err = parseDate(attachment.ModDate)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.attachments[%d].modDate: %w", index, err)
			}
		}
		document.Attachments = append(document.Attachments, value)
	}
	for index, extension := range m.Document.Extensions {
		value := creator.Extension{AppName: extension.AppName, Company: extension.Company, AppVersion: extension.AppVersion, RefID: extension.RefID, Data: extension.Data, DataXML: []byte(extension.DataXML), DataName: extension.DataName}
		if extension.Date != "" {
			value.Date, err = parseDate(extension.Date)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.extensions[%d].date: %w", index, err)
			}
		}
		for _, property := range extension.Properties {
			value.Properties = append(value.Properties, creator.ExtensionProperty{Name: property.Name, Type: property.Type, Value: property.Value})
		}
		if extension.DataFile != "" || extension.DataFileBase64 != "" {
			data, err := loadData(root, extension.DataFile, extension.DataFileBase64)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.extensions[%d].dataFile: %w", index, err)
			}
			value.DataFile = data
		}
		document.Extensions = append(document.Extensions, value)
	}
	for index, version := range m.Document.Versions {
		value := creator.DocumentVersion{ID: version.ID, Index: version.Index, Current: version.Current, Version: version.Version, Name: version.Name, DocRootName: version.DocRootName}
		if version.CreationDate != "" {
			parsed, err := parseDate(version.CreationDate)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.versions[%d].creationDate: %w", index, err)
			}
			value.CreationDate = parsed
		}
		if version.DocRoot != "" || version.DocRootBase64 != "" {
			data, err := loadData(root, version.DocRoot, version.DocRootBase64)
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.versions[%d].docRoot: %w", index, err)
			}
			value.DocRoot = data
		}
		for _, file := range version.Files {
			value.Files = append(value.Files, creator.VersionFile{ID: file.ID, Path: file.Path})
		}
		document.Versions = append(document.Versions, value)
	}
	if document.PageSize.Width == 0 && document.PageSize.Height == 0 && m.Document.PageSize.Name != "" {
		if strings.EqualFold(m.Document.PageSize.Name, "A4") {
			document.PageSize = creator.A4
		} else {
			return creator.Document{}, fmt.Errorf("document.pageSize.name: 不支持的页面尺寸 %q，请提供 width 和 height", m.Document.PageSize.Name)
		}
	}
	for index, font := range m.Resources.Fonts {
		data, err := loadData(root, font.File, font.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.fonts[%d].file: %w", index, err)
		}
		document.Fonts = append(document.Fonts, creator.Font{Name: font.Name, FamilyName: font.FamilyName, Charset: font.Charset, Format: font.Format, Italic: font.Italic, Bold: font.Bold, Serif: font.Serif, FixedWidth: font.FixedWidth, Data: data})
	}
	for index, image := range m.Resources.Images {
		data, err := loadData(root, image.File, image.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.images[%d]: %w", index, err)
		}
		if image.ID == 0 {
			return creator.Document{}, fmt.Errorf("resources.images[%d].id: ID 不能为空", index)
		}
		document.Media = append(document.Media, creator.Media{ID: image.ID, Type: "Image", Format: image.Format, Name: image.Name, Data: data})
	}
	for index, media := range m.Resources.Media {
		data, err := loadData(root, media.File, media.DataBase64)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.media[%d].file: %w", index, err)
		}
		document.Media = append(document.Media, creator.Media{ID: media.ID, Type: media.Type, Format: media.Format, Name: media.Name, Data: data})
	}
	for index, space := range m.Resources.ColorSpaces {
		value := creator.ColorSpace{ID: space.ID, Type: space.Type, BitsPerComponent: space.BitsPerComponent, Palette: space.Palette}
		if space.ProfileFile != "" || space.ProfileBase64 != "" {
			data, err := loadData(root, space.ProfileFile, space.ProfileBase64)
			if err != nil {
				return creator.Document{}, fmt.Errorf("resources.colorSpaces[%d].profileFile: %w", index, err)
			}
			value.ProfileData = data
			profileName := space.ProfileName
			if profileName == "" && space.ProfileFile != "" {
				profileName = filepath.Base(filepath.FromSlash(space.ProfileFile))
			}
			if profileName != "" {
				if err := validateLeafName(profileName); err != nil {
					return creator.Document{}, fmt.Errorf("resources.colorSpaces[%d].profileName: %w", index, err)
				}
				value.Profile = filepath.Join("Profiles", profileName)
			}
		}
		document.ColorSpaces = append(document.ColorSpaces, value)
	}
	for index, param := range m.Resources.DrawParams {
		fill, err := buildColor(param.FillColor)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.drawParams[%d].fillColor: %w", index, err)
		}
		stroke, err := buildColor(param.StrokeColor)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.drawParams[%d].strokeColor: %w", index, err)
		}
		document.DrawParams = append(document.DrawParams, creator.DrawParam{Name: param.Name, Relative: param.Relative, LineWidth: param.LineWidth, Join: param.Join, Cap: param.Cap, DashOffset: param.DashOffset, DashPattern: param.DashPattern, MiterLimit: param.MiterLimit, FillColor: fill, StrokeColor: stroke})
	}
	for index, composite := range m.Resources.Composites {
		items, err := buildItems(composite.Items)
		if err != nil {
			return creator.Document{}, fmt.Errorf("resources.composites[%d].items: %w", index, err)
		}
		document.Composites = append(document.Composites, creator.CompositeGraphicUnit{ID: composite.ID, Width: composite.Width, Height: composite.Height, Thumbnail: composite.Thumbnail, Substitution: composite.Substitution, Items: items})
	}
	for index, template := range m.Templates {
		if len(template.Layers) > 0 && len(template.Items) > 0 {
			return creator.Document{}, fmt.Errorf("templates[%d]: layers 不能与 items 同时设置", index)
		}
		layers, err := buildLayers(template.Layers)
		if err != nil {
			return creator.Document{}, fmt.Errorf("templates[%d].layers: %w", index, err)
		}
		items, err := buildItems(template.Items)
		if err != nil {
			return creator.Document{}, fmt.Errorf("templates[%d].items: %w", index, err)
		}
		document.Templates = append(document.Templates, creator.TemplatePage{ID: template.ID, Name: template.Name, ZOrder: template.ZOrder, Area: buildPageArea(template.Area), Layers: layers, Items: items})
	}
	for index, page := range m.Pages {
		if len(page.Layers) > 0 && len(page.Items) > 0 {
			return creator.Document{}, fmt.Errorf("pages[%d]: layers 不能与 items 同时设置", index)
		}
		pageActions, err := buildActions(page.Actions)
		if err != nil {
			return creator.Document{}, fmt.Errorf("pages[%d].actions: %w", index, err)
		}
		converted := creator.Page{Area: buildPageArea(page.Area), LayerType: page.LayerType, Actions: pageActions}
		for resourceIndex, resource := range page.Resources {
			if len(resource.Images) > 0 && (resource.File != "" || resource.DataBase64 != "" || len(resource.Files) > 0) {
				return creator.Document{}, fmt.Errorf("pages[%d].resources[%d]: images 不能与 file、dataBase64 或 files 同时设置", index, resourceIndex)
			}
			data, err := loadData(root, resource.File, resource.DataBase64)
			if err != nil {
				return creator.Document{}, fmt.Errorf("pages[%d].resources[%d].file: %w", index, resourceIndex, err)
			}
			files, err := buildPageResourceFiles(root, resource.Files)
			if err != nil {
				return creator.Document{}, fmt.Errorf("pages[%d].resources[%d].files: %w", index, resourceIndex, err)
			}
			convertedResource := creator.PageResource{Data: data, Files: files}
			for imageIndex, image := range resource.Images {
				imageData, err := loadData(root, image.File, image.DataBase64)
				if err != nil {
					return creator.Document{}, fmt.Errorf("pages[%d].resources[%d].images[%d]: %w", index, resourceIndex, imageIndex, err)
				}
				convertedResource.Images = append(convertedResource.Images, creator.PageImage{ID: image.ID, Format: image.Format, Name: image.Name, Data: imageData})
			}
			converted.Resources = append(converted.Resources, convertedResource)
		}
		for _, template := range page.Templates {
			converted.Templates = append(converted.Templates, creator.TemplateRef{ID: template.ID, ZOrder: template.ZOrder})
		}
		layers, err := buildLayers(page.Layers)
		if err != nil {
			return creator.Document{}, fmt.Errorf("pages[%d].layers: %w", index, err)
		}
		converted.Layers = layers
		if len(layers) == 0 {
			converted.Items, err = buildItems(page.Items)
			if err != nil {
				return creator.Document{}, fmt.Errorf("pages[%d].items: %w", index, err)
			}
		}
		document.Pages = append(document.Pages, converted)
	}
	for index, annotationPage := range m.Document.Annotations {
		items := make([]creator.Annotation, 0, len(annotationPage.Items))
		for itemIndex, annotation := range annotationPage.Items {
			converted, err := annotation.build()
			if err != nil {
				return creator.Document{}, fmt.Errorf("document.annotations[%d].items[%d]: %w", index, itemIndex, err)
			}
			items = append(items, converted)
		}
		document.Annotations = append(document.Annotations, creator.AnnotationPage{Page: annotationPage.Page, Items: items})
	}
	return document, nil
}

func (i Item) build() (creator.Item, error) {
	var ctm *creator.CTM
	if len(i.CTM) > 0 {
		if len(i.CTM) != 6 {
			return nil, errors.New("ctm 必须包含 6 个数值")
		}
		value := creator.CTM{i.CTM[0], i.CTM[1], i.CTM[2], i.CTM[3], i.CTM[4], i.CTM[5]}
		ctm = &value
	}
	switch strings.ToLower(strings.TrimSpace(i.Type)) {
	case "text":
		codes := make([]creator.TextCode, 0, len(i.TextCodes))
		for _, code := range i.TextCodes {
			codes = append(codes, creator.TextCode{Value: code.Value, X: code.X, Y: code.Y, DeltaX: code.DeltaX, DeltaY: code.DeltaY})
		}
		transforms := make([]creator.CGTransform, 0, len(i.CGTransforms))
		for _, transform := range i.CGTransforms {
			transforms = append(transforms, creator.CGTransform{CodePosition: transform.CodePosition, CodeCount: transform.CodeCount, GlyphCount: transform.GlyphCount, Glyphs: transform.Glyphs})
		}
		fillColor, err := buildColor(i.FillColor)
		if err != nil {
			return nil, fmt.Errorf("fillColor: %w", err)
		}
		strokeColor, err := buildColor(i.StrokeColor)
		if err != nil {
			return nil, fmt.Errorf("strokeColor: %w", err)
		}
		actions, err := buildActions(i.Actions)
		if err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		}
		clips, err := buildClips(i.Clips)
		if err != nil {
			return nil, fmt.Errorf("clips: %w", err)
		}
		return creator.Text{X: i.X, Y: i.Y, Width: i.Width, Height: i.Height, Value: i.Value, Font: i.Font, Size: i.Size, Visible: i.Visible, Fill: i.Fill, Stroke: i.Stroke, HScale: i.HScale, ReadDirection: i.ReadDirection, CharDirection: i.CharDirection, Weight: i.Weight, Italic: i.Italic, DrawParam: i.DrawParam, CTM: ctm, FillColor: fillColor, StrokeColor: strokeColor, TextCodes: codes, CGTransforms: transforms, Actions: actions, Clips: clips}, nil
	case "path":
		fillColor, err := buildColor(i.FillColor)
		if err != nil {
			return nil, fmt.Errorf("fillColor: %w", err)
		}
		strokeColor, err := buildColor(i.StrokeColor)
		if err != nil {
			return nil, fmt.Errorf("strokeColor: %w", err)
		}
		clips, err := buildClips(i.Clips)
		if err != nil {
			return nil, fmt.Errorf("clips: %w", err)
		}
		actions, err := buildActions(i.Actions)
		if err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		}
		fill := false
		if i.Fill != nil {
			fill = *i.Fill
		}
		return creator.Path{X: i.X, Y: i.Y, Width: i.Width, Height: i.Height, Data: i.Data, Name: i.Name, Visible: i.Visible, Stroke: i.Stroke, StrokeSet: i.StrokeSet, Fill: fill, Rule: i.Rule, LineWidth: i.LineWidth, Cap: i.Cap, Join: i.Join, MiterLimit: i.MiterLimit, DashOffset: i.DashOffset, DashPattern: i.DashPattern, Alpha: i.Alpha, DrawParam: i.DrawParam, CTM: ctm, FillColor: fillColor, StrokeColor: strokeColor, Actions: actions, Clips: clips}, nil
	case "image":
		if i.ResourceID == 0 && i.DataBase64 == "" {
			return nil, errors.New("image.resourceId 或 dataBase64 至少设置一个")
		}
		if i.ResourceID != 0 && i.DataBase64 != "" {
			return nil, errors.New("image.resourceId 不能与 dataBase64 同时设置")
		}
		clips, err := buildClips(i.Clips)
		if err != nil {
			return nil, fmt.Errorf("clips: %w", err)
		}
		border, err := buildImageBorder(i.Border)
		if err != nil {
			return nil, fmt.Errorf("border: %w", err)
		}
		data, err := decodeOptionalBytes(i.DataBase64)
		if err != nil {
			return nil, fmt.Errorf("dataBase64: %w", err)
		}
		actions, err := buildActions(i.Actions)
		if err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		}
		return creator.Image{X: i.X, Y: i.Y, Width: i.Width, Height: i.Height, Data: data, ResourceID: i.ResourceID, Substitution: i.Substitution, ImageMask: i.ImageMask, Format: i.Format, Name: i.Name, Visible: i.Visible, LineWidth: i.LineWidth, Cap: i.Cap, Join: i.Join, MiterLimit: i.MiterLimit, DashOffset: i.DashOffset, DashPattern: i.DashPattern, Alpha: i.Alpha, DrawParam: i.DrawParam, CTM: ctm, Actions: actions, Clips: clips, Border: border}, nil
	case "composite":
		if i.ResourceID == 0 {
			return nil, errors.New("composite.resourceId 不能为空")
		}
		clips, err := buildClips(i.Clips)
		if err != nil {
			return nil, fmt.Errorf("clips: %w", err)
		}
		actions, err := buildActions(i.Actions)
		if err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		}
		return creator.Composite{X: i.X, Y: i.Y, Width: i.Width, Height: i.Height, ResourceID: i.ResourceID, Name: i.Name, Visible: i.Visible, DrawParam: i.DrawParam, CTM: ctm, Actions: actions, Clips: clips}, nil
	case "page-block", "pageblock":
		items, err := buildItems(i.Items)
		if err != nil {
			return nil, err
		}
		return creator.PageBlock{Items: items}, nil
	default:
		return nil, fmt.Errorf("不支持的图元类型 %q，仅支持 text、path、image、composite、page-block", i.Type)
	}
}

func buildItems(values []Item) ([]creator.Item, error) {
	items := make([]creator.Item, 0, len(values))
	for index, value := range values {
		item, err := value.build()
		if err != nil {
			return nil, fmt.Errorf("items[%d]: %w", index, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func buildLayers(values []Layer) ([]creator.Layer, error) {
	layers := make([]creator.Layer, 0, len(values))
	for index, value := range values {
		items, err := buildItems(value.Items)
		if err != nil {
			return nil, fmt.Errorf("图层 %d: %w", index+1, err)
		}
		layers = append(layers, creator.Layer{Type: value.Type, DrawParam: value.DrawParam, Items: items})
	}
	return layers, nil
}

func buildPageArea(value *PageArea) *creator.PageArea {
	if value == nil {
		return nil
	}
	return &creator.PageArea{PhysicalBox: buildBox(value.PhysicalBox), ApplicationBox: buildBox(value.ApplicationBox), ContentBox: buildBox(value.ContentBox), BleedBox: buildBox(value.BleedBox)}
}

func buildBox(value *Box) *creator.Box {
	if value == nil {
		return nil
	}
	return &creator.Box{X: value.X, Y: value.Y, Width: value.Width, Height: value.Height}
}

func buildImageBorder(value *ImageBorder) (*creator.ImageBorder, error) {
	if value == nil {
		return nil, nil
	}
	color, err := buildColor(value.Color)
	if err != nil {
		return nil, err
	}
	return &creator.ImageBorder{LineWidth: value.LineWidth, HorizontalRadius: value.HorizontalRadius, VerticalRadius: value.VerticalRadius, DashOffset: value.DashOffset, DashPattern: value.DashPattern, Color: color}, nil
}

func buildClips(values []Clip) (*creator.Clips, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := &creator.Clips{Items: make([]creator.Clip, 0, len(values))}
	for clipIndex, value := range values {
		clip := creator.Clip{Areas: make([]creator.ClipArea, 0, len(value.Areas))}
		for areaIndex, area := range value.Areas {
			ctm, err := buildCTM(area.CTM)
			if err != nil {
				return nil, fmt.Errorf("[%d].areas[%d].ctm: %w", clipIndex, areaIndex, err)
			}
			converted := creator.ClipArea{DrawParam: area.DrawParam, CTM: ctm}
			if area.Path != nil {
				fill, err := buildColor(area.Path.FillColor)
				if err != nil {
					return nil, err
				}
				stroke, err := buildColor(area.Path.StrokeColor)
				if err != nil {
					return nil, err
				}
				pathCTM, err := buildCTM(area.Path.CTM)
				if err != nil {
					return nil, err
				}
				converted.Path = &creator.ClipPath{Boundary: *buildBox(&area.Path.Boundary), Name: area.Path.Name, Visible: area.Path.Visible, CTM: pathCTM, Data: area.Path.Data, Stroke: area.Path.Stroke, StrokeSet: area.Path.StrokeSet, Fill: area.Path.Fill, Rule: area.Path.Rule, LineWidth: area.Path.LineWidth, Cap: area.Path.Cap, Join: area.Path.Join, MiterLimit: area.Path.MiterLimit, DashOffset: area.Path.DashOffset, DashPattern: area.Path.DashPattern, Alpha: area.Path.Alpha, StrokeColor: stroke, FillColor: fill}
			}
			if area.Text != nil {
				fill, err := buildColor(area.Text.FillColor)
				if err != nil {
					return nil, err
				}
				stroke, err := buildColor(area.Text.StrokeColor)
				if err != nil {
					return nil, err
				}
				textCTM, err := buildCTM(area.Text.CTM)
				if err != nil {
					return nil, err
				}
				codes := make([]creator.TextCode, 0, len(area.Text.TextCodes))
				for _, code := range area.Text.TextCodes {
					codes = append(codes, creator.TextCode{Value: code.Value, X: code.X, Y: code.Y, DeltaX: code.DeltaX, DeltaY: code.DeltaY})
				}
				converted.Text = &creator.ClipText{Boundary: *buildBox(&area.Text.Boundary), CTM: textCTM, Font: area.Text.Font, Size: area.Text.Size, Value: area.Text.Value, TextCodes: codes, Stroke: area.Text.Stroke, Fill: area.Text.Fill, HScale: area.Text.HScale, ReadDirection: area.Text.ReadDirection, CharDirection: area.Text.CharDirection, Weight: area.Text.Weight, Italic: area.Text.Italic, FillColor: fill, StrokeColor: stroke}
			}
			clip.Areas = append(clip.Areas, converted)
		}
		result.Items = append(result.Items, clip)
	}
	return result, nil
}

func parseDate(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if result, err := time.Parse(layout, value); err == nil {
			return result, nil
		}
	}
	return time.Time{}, fmt.Errorf("日期格式无效: %q", value)
}

func readOptionalAsset(root, name string) ([]byte, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	return readAsset(root, name)
}

func loadData(root, file, encoded string) ([]byte, error) {
	if file != "" && encoded != "" {
		return nil, errors.New("file 不能与 dataBase64 同时设置")
	}
	if file != "" {
		return readAsset(root, file)
	}
	return decodeOptionalBytes(encoded)
}

func decodeOptionalBytes(encoded string) ([]byte, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("Base64 数据无效: %w", err)
	}
	return data, nil
}

func validateLeafName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.Base(filepath.FromSlash(value)) != value || strings.ContainsAny(value, `/\\\x00`) {
		return fmt.Errorf("文件名无效: %q", value)
	}
	return nil
}

func buildResourceFiles(root string, values []ResourceFile) ([]creator.PublicResourceFile, error) {
	result := make([]creator.PublicResourceFile, 0, len(values))
	for index, value := range values {
		data, err := loadData(root, value.File, value.DataBase64)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", index, err)
		}
		result = append(result, creator.PublicResourceFile{Path: value.Path, Data: data})
	}
	return result, nil
}

func buildPageResourceFiles(root string, values []ResourceFile) ([]creator.PageResourceFile, error) {
	result := make([]creator.PageResourceFile, 0, len(values))
	for index, value := range values {
		data, err := loadData(root, value.File, value.DataBase64)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", index, err)
		}
		result = append(result, creator.PageResourceFile{Path: value.Path, Data: data})
	}
	return result, nil
}

func buildPermissions(value *Permissions) (*creator.Permissions, error) {
	result := &creator.Permissions{Edit: value.Edit, Annot: value.Annot, Export: value.Export, Signature: value.Signature, Watermark: value.Watermark, PrintScreen: value.PrintScreen}
	if value.Print != nil {
		result.Print = &creator.PrintSettings{Printable: value.Print.Printable, Copies: value.Print.Copies}
	}
	if value.ValidPeriod != nil {
		start, err := parseDate(value.ValidPeriod.Start)
		if err != nil {
			return nil, fmt.Errorf("validPeriod.start: %w", err)
		}
		end, err := parseDate(value.ValidPeriod.End)
		if err != nil {
			return nil, fmt.Errorf("validPeriod.end: %w", err)
		}
		result.ValidPeriod = &creator.ValidPeriod{Start: start, End: end}
	}
	return result, nil
}

func buildColor(value *Color) (*creator.Color, error) {
	if value == nil {
		return nil, nil
	}
	result := &creator.Color{R: value.R, G: value.G, B: value.B, Components: value.Components, ColorSpace: value.ColorSpace, Index: value.Index, Alpha: value.Alpha}
	if value.Axial != nil {
		segments, err := buildColorStops(value.Axial.Segments)
		if err != nil {
			return nil, fmt.Errorf("axial.segments: %w", err)
		}
		result.Axial = &creator.AxialShading{MapType: value.Axial.MapType, MapUnit: value.Axial.MapUnit, Extend: value.Axial.Extend, StartPoint: value.Axial.StartPoint, EndPoint: value.Axial.EndPoint, Segments: segments}
	}
	if value.Radial != nil {
		segments, err := buildColorStops(value.Radial.Segments)
		if err != nil {
			return nil, fmt.Errorf("radial.segments: %w", err)
		}
		result.Radial = &creator.RadialShading{MapType: value.Radial.MapType, MapUnit: value.Radial.MapUnit, Eccentricity: value.Radial.Eccentricity, Angle: value.Radial.Angle, StartPoint: value.Radial.StartPoint, StartRadius: value.Radial.StartRadius, EndPoint: value.Radial.EndPoint, EndRadius: value.Radial.EndRadius, Extend: value.Radial.Extend, Segments: segments}
	}
	if value.Gouraud != nil {
		points := make([]creator.GouraudPoint, 0, len(value.Gouraud.Points))
		for index, point := range value.Gouraud.Points {
			color, err := buildColor(&point.Color)
			if err != nil {
				return nil, fmt.Errorf("gouraud.points[%d].color: %w", index, err)
			}
			points = append(points, creator.GouraudPoint{X: point.X, Y: point.Y, EdgeFlag: point.EdgeFlag, Color: *color})
		}
		backColor, err := buildColor(value.Gouraud.BackColor)
		if err != nil {
			return nil, fmt.Errorf("gouraud.backColor: %w", err)
		}
		result.Gouraud = &creator.GouraudShading{Extend: value.Gouraud.Extend, Points: points, BackColor: backColor}
	}
	if value.LaGouraud != nil {
		points := make([]creator.LaGouraudPoint, 0, len(value.LaGouraud.Points))
		for index, point := range value.LaGouraud.Points {
			color, err := buildColor(&point.Color)
			if err != nil {
				return nil, fmt.Errorf("laGouraud.points[%d].color: %w", index, err)
			}
			points = append(points, creator.LaGouraudPoint{X: point.X, Y: point.Y, Color: *color})
		}
		backColor, err := buildColor(value.LaGouraud.BackColor)
		if err != nil {
			return nil, fmt.Errorf("laGouraud.backColor: %w", err)
		}
		result.LaGouraud = &creator.LaGouraudShading{VerticesPerRow: value.LaGouraud.VerticesPerRow, Extend: value.LaGouraud.Extend, Points: points, BackColor: backColor}
	}
	if value.Pattern != nil {
		items, err := buildItems(value.Pattern.Items)
		if err != nil {
			return nil, fmt.Errorf("pattern.items: %w", err)
		}
		layers, err := buildLayers(value.Pattern.Layers)
		if err != nil {
			return nil, fmt.Errorf("pattern.layers: %w", err)
		}
		ctm, err := buildCTM(value.Pattern.CTM)
		if err != nil {
			return nil, fmt.Errorf("pattern.ctm: %w", err)
		}
		result.Pattern = &creator.Pattern{Width: value.Pattern.Width, Height: value.Pattern.Height, XStep: value.Pattern.XStep, YStep: value.Pattern.YStep, ReflectMethod: value.Pattern.ReflectMethod, RelativeTo: value.Pattern.RelativeTo, CTM: ctm, Thumbnail: value.Pattern.Thumbnail, Items: items, Layers: layers}
	}
	return result, nil
}

func buildColorStops(values []ColorStop) ([]creator.ColorStop, error) {
	result := make([]creator.ColorStop, 0, len(values))
	for index, value := range values {
		color, err := buildColor(&value.Color)
		if err != nil {
			return nil, fmt.Errorf("[%d].color: %w", index, err)
		}
		result = append(result, creator.ColorStop{Position: value.Position, Color: *color})
	}
	return result, nil
}

func buildCTM(values []float64) (*creator.CTM, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) != 6 {
		return nil, errors.New("ctm 必须包含 6 个数值")
	}
	value := creator.CTM{values[0], values[1], values[2], values[3], values[4], values[5]}
	return &value, nil
}

func buildAction(value Action) (creator.Action, error) {
	result := creator.Action{Event: creator.ActionEvent(value.Event)}
	if value.Region != nil {
		region, err := buildActionRegion(value.Region)
		if err != nil {
			return creator.Action{}, err
		}
		result.Region = region
	}
	if value.URI != nil {
		result.URI = &creator.URIAction{URI: value.URI.URI, Base: value.URI.Base, Target: value.URI.Target}
	}
	if value.Goto != nil {
		gotoValue := buildGoto(*value.Goto)
		result.Goto = &gotoValue
	}
	if value.GotoA != nil {
		result.GotoA = &creator.GotoAAction{AttachID: value.GotoA.AttachID, NewWindow: value.GotoA.NewWindow}
	}
	if value.Sound != nil {
		result.Sound = &creator.SoundAction{ResourceID: value.Sound.ResourceID, Volume: value.Sound.Volume, Repeat: value.Sound.Repeat, Synchronous: value.Sound.Synchronous}
	}
	if value.Movie != nil {
		result.Movie = &creator.MovieAction{ResourceID: value.Movie.ResourceID, Operator: value.Movie.Operator}
	}
	return result, nil
}

func buildActionRegion(value *ActionRegion) (*creator.ActionRegion, error) {
	result := &creator.ActionRegion{}
	for _, area := range value.Areas {
		converted := creator.ActionArea{Start: creator.Point{X: area.Start.X, Y: area.Start.Y}}
		for _, command := range area.Commands {
			switch strings.ToLower(command.Type) {
			case "move":
				converted.Commands = append(converted.Commands, creator.RegionMove{Point: creator.Point{X: command.X, Y: command.Y}})
			case "line":
				converted.Commands = append(converted.Commands, creator.RegionLine{Point: creator.Point{X: command.X, Y: command.Y}})
			case "quadratic", "quadratic-bezier":
				converted.Commands = append(converted.Commands, creator.RegionQuadraticBezier{Control: creator.Point{X: command.ControlX, Y: command.ControlY}, End: creator.Point{X: command.X, Y: command.Y}})
			case "cubic", "cubic-bezier":
				converted.Commands = append(converted.Commands, creator.RegionCubicBezier{Control1: creator.Point{X: command.Control1X, Y: command.Control1Y}, Control2: creator.Point{X: command.Control2X, Y: command.Control2Y}, End: creator.Point{X: command.X, Y: command.Y}})
			case "arc":
				converted.Commands = append(converted.Commands, creator.RegionArc{SweepDirection: command.SweepDirection, LargeArc: command.LargeArc, RotationAngle: command.RotationAngle, EllipseSize: creator.Point{X: command.EllipseWidth, Y: command.EllipseHeight}, EndPoint: creator.Point{X: command.X, Y: command.Y}})
			case "close":
				converted.Commands = append(converted.Commands, creator.RegionClose{})
			default:
				return nil, fmt.Errorf("不支持的动作区域命令类型 %q", command.Type)
			}
		}
		result.Areas = append(result.Areas, converted)
	}
	return result, nil
}

func buildActions(values []Action) ([]creator.Action, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]creator.Action, 0, len(values))
	for index, value := range values {
		converted, err := buildAction(value)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", index, err)
		}
		result = append(result, converted)
	}
	return result, nil
}

func buildOutlines(values []Outline) ([]creator.Outline, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]creator.Outline, 0, len(values))
	for index, value := range values {
		actions, err := buildActions(value.Actions)
		if err != nil {
			return nil, fmt.Errorf("[%d].actions: %w", index, err)
		}
		children, err := buildOutlines(value.Children)
		if err != nil {
			return nil, fmt.Errorf("[%d].children: %w", index, err)
		}
		result = append(result, creator.Outline{Title: value.Title, Count: value.Count, Expanded: value.Expanded, Actions: actions, Children: children})
	}
	return result, nil
}

func buildSignature(root string, value Signature) (creator.Signature, error) {
	result := creator.Signature{ID: value.ID, Type: value.Type, ProviderName: value.ProviderName, ProviderVersion: value.ProviderVersion, Company: value.Company, Method: value.Method, CheckMethod: value.CheckMethod, SealName: value.SealName, SignedValueName: value.SignedValueName}
	var err error
	if value.Date != "" {
		result.Date, err = parseDate(value.Date)
		if err != nil {
			return creator.Signature{}, fmt.Errorf("date: %w", err)
		}
	}
	if value.SealFile != "" || value.SealFileBase64 != "" {
		result.SealFile, err = loadData(root, value.SealFile, value.SealFileBase64)
		if err != nil {
			return creator.Signature{}, fmt.Errorf("sealFile: %w", err)
		}
	}
	if value.SignedValue != "" {
		result.SignedValue, err = decodeBytes(value.SignedValue)
		if err != nil {
			return creator.Signature{}, fmt.Errorf("signedValue: %w", err)
		}
	}
	for index, reference := range value.References {
		checkValue, err := decodeBytes(reference.CheckValue)
		if err != nil {
			return creator.Signature{}, fmt.Errorf("references[%d].checkValue: %w", index, err)
		}
		result.References = append(result.References, creator.SignatureReference{FileRef: reference.FileRef, CheckValue: checkValue})
	}
	for _, stamp := range value.StampAnnots {
		result.StampAnnots = append(result.StampAnnots, creator.SignatureStamp{ID: stamp.ID, Page: stamp.Page, Boundary: creator.Box{X: stamp.Boundary.X, Y: stamp.Boundary.Y, Width: stamp.Boundary.Width, Height: stamp.Boundary.Height}, Clip: buildBox(stamp.Clip)})
	}
	return result, nil
}

func decodeBytes(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("Base64 数据无效: %w", err)
	}
	return data, nil
}

func buildGoto(value GotoAction) creator.GotoAction {
	return creator.GotoAction{Page: value.Page, Type: value.Type, Bookmark: value.Bookmark, Left: value.Left, Top: value.Top, Right: value.Right, Bottom: value.Bottom, Zoom: value.Zoom}
}

func (value Annotation) build() (creator.Annotation, error) {
	items, err := buildItems(value.Items)
	if err != nil {
		return creator.Annotation{}, fmt.Errorf("items: %w", err)
	}
	result := creator.Annotation{ID: value.ID, Type: value.Type, Creator: value.Creator, Visible: value.Visible, Subtype: value.Subtype, Print: value.Print, NoZoom: value.NoZoom, NoRotate: value.NoRotate, ReadOnlyValue: value.ReadOnly, Remark: value.Remark, Items: items}
	if value.LastModDate != "" {
		date, err := parseDate(value.LastModDate)
		if err != nil {
			return creator.Annotation{}, fmt.Errorf("lastModDate: %w", err)
		}
		result.LastModDate = date
	}
	if value.Boundary != nil {
		result.Boundary = &creator.Box{X: value.Boundary.X, Y: value.Boundary.Y, Width: value.Boundary.Width, Height: value.Boundary.Height}
	}
	for _, parameter := range value.Parameters {
		result.Parameters = append(result.Parameters, creator.AnnotationParameter{Name: parameter.Name, Value: parameter.Value})
	}
	return result, nil
}

func readAsset(root, name string) ([]byte, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("资源文件路径不能为空")
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return nil, errors.New("资源文件路径必须是资源根目录下的相对路径")
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return nil, fmt.Errorf("资源路径无效: %w", err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, errors.New("资源路径不能离开资源根目录")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %q 失败: %w", name, err)
	}
	return data, nil
}
