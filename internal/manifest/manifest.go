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
	Version   int        `json:"version" yaml:"version"`
	Document  Document   `json:"document" yaml:"document"`
	Resources Resources  `json:"resources" yaml:"resources"`
	Templates []Template `json:"templates" yaml:"templates"`
	Pages     []Page     `json:"pages" yaml:"pages"`
}

type Document struct {
	ID             string           `json:"id" yaml:"id"`
	Title          string           `json:"title" yaml:"title"`
	Author         string           `json:"author" yaml:"author"`
	Subject        string           `json:"subject" yaml:"subject"`
	Abstract       string           `json:"abstract" yaml:"abstract"`
	Creator        string           `json:"creator" yaml:"creator"`
	CreatorVersion string           `json:"creatorVersion" yaml:"creatorVersion"`
	PageSize       PageSize         `json:"pageSize" yaml:"pageSize"`
	Actions        []Action         `json:"actions" yaml:"actions"`
	Bookmarks      []Bookmark       `json:"bookmarks" yaml:"bookmarks"`
	Attachments    []Attachment     `json:"attachments" yaml:"attachments"`
	Extensions     []Extension      `json:"extensions" yaml:"extensions"`
	Versions       []Version        `json:"versions" yaml:"versions"`
	Annotations    []AnnotationPage `json:"annotations" yaml:"annotations"`
	DocUsage       string           `json:"docUsage" yaml:"docUsage"`
	Keywords       []string         `json:"keywords" yaml:"keywords"`
	CustomData     []CustomData     `json:"customData" yaml:"customData"`
	Cover          string           `json:"cover" yaml:"cover"`
	CoverBase64    string           `json:"coverBase64" yaml:"coverBase64"`
	CoverName      string           `json:"coverName" yaml:"coverName"`
	CreationDate   string           `json:"creationDate" yaml:"creationDate"`
	ModDate        string           `json:"modDate" yaml:"modDate"`
	Area           *PageArea        `json:"area" yaml:"area"`
	DefaultCS      uint64           `json:"defaultCS" yaml:"defaultCS"`
	Preferences    *Preferences     `json:"preferences" yaml:"preferences"`
	Permissions    *Permissions     `json:"permissions" yaml:"permissions"`
	Outlines       []Outline        `json:"outlines" yaml:"outlines"`
	Signatures     []Signature      `json:"signatures" yaml:"signatures"`
}

type PageSize struct {
	Name   string  `json:"name" yaml:"name"`
	Width  float64 `json:"width" yaml:"width"`
	Height float64 `json:"height" yaml:"height"`
}

type Resources struct {
	Fonts       []Font           `json:"fonts" yaml:"fonts"`
	Images      []Image          `json:"images" yaml:"images"`
	Media       []Media          `json:"media" yaml:"media"`
	ColorSpaces []ColorSpace     `json:"colorSpaces" yaml:"colorSpaces"`
	DrawParams  []DrawParam      `json:"drawParams" yaml:"drawParams"`
	Composites  []Composite      `json:"composites" yaml:"composites"`
	Public      []PublicResource `json:"public" yaml:"public"`
	CustomTags  []CustomTag      `json:"customTags" yaml:"customTags"`
}

type Font struct {
	Name       string `json:"name" yaml:"name"`
	FamilyName string `json:"familyName" yaml:"familyName"`
	Charset    string `json:"charset" yaml:"charset"`
	Format     string `json:"format" yaml:"format"`
	File       string `json:"file" yaml:"file"`
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
	Italic     bool   `json:"italic" yaml:"italic"`
	Bold       bool   `json:"bold" yaml:"bold"`
	Serif      bool   `json:"serif" yaml:"serif"`
	FixedWidth bool   `json:"fixedWidth" yaml:"fixedWidth"`
}

type Image struct {
	ID         uint64 `json:"id" yaml:"id"`
	Format     string `json:"format" yaml:"format"`
	Name       string `json:"name" yaml:"name"`
	File       string `json:"file" yaml:"file"`
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
}

type Media struct {
	ID         uint64 `json:"id" yaml:"id"`
	Type       string `json:"type" yaml:"type"`
	Format     string `json:"format" yaml:"format"`
	Name       string `json:"name" yaml:"name"`
	File       string `json:"file" yaml:"file"`
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
}

type Page struct {
	Templates []TemplateRef  `json:"templates" yaml:"templates"`
	Layers    []Layer        `json:"layers" yaml:"layers"`
	Items     []Item         `json:"items" yaml:"items"`
	Area      *PageArea      `json:"area" yaml:"area"`
	LayerType string         `json:"layerType" yaml:"layerType"`
	Actions   []Action       `json:"actions" yaml:"actions"`
	Resources []PageResource `json:"resources" yaml:"resources"`
}

type PublicResource struct {
	Name       string         `json:"name" yaml:"name"`
	File       string         `json:"file" yaml:"file"`
	DataBase64 string         `json:"dataBase64" yaml:"dataBase64"`
	Files      []ResourceFile `json:"files" yaml:"files"`
}

type PageResource struct {
	File       string         `json:"file" yaml:"file"`
	DataBase64 string         `json:"dataBase64" yaml:"dataBase64"`
	Images     []PageImage    `json:"images" yaml:"images"`
	Files      []ResourceFile `json:"files" yaml:"files"`
}

type PageImage struct {
	ID         uint64 `json:"id" yaml:"id"`
	Format     string `json:"format" yaml:"format"`
	Name       string `json:"name" yaml:"name"`
	File       string `json:"file" yaml:"file"`
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
}

type ResourceFile struct {
	Path       string `json:"path" yaml:"path"`
	File       string `json:"file" yaml:"file"`
	DataBase64 string `json:"dataBase64" yaml:"dataBase64"`
}

type CustomTag struct {
	NameSpace    string `json:"nameSpace" yaml:"nameSpace"`
	Schema       string `json:"schema" yaml:"schema"`
	SchemaBase64 string `json:"schemaBase64" yaml:"schemaBase64"`
	SchemaName   string `json:"schemaName" yaml:"schemaName"`
	Data         string `json:"data" yaml:"data"`
	DataBase64   string `json:"dataBase64" yaml:"dataBase64"`
	DataName     string `json:"dataName" yaml:"dataName"`
}

type Permissions struct {
	Edit        *bool          `json:"edit" yaml:"edit"`
	Annot       *bool          `json:"annot" yaml:"annot"`
	Export      *bool          `json:"export" yaml:"export"`
	Signature   *bool          `json:"signature" yaml:"signature"`
	Watermark   *bool          `json:"watermark" yaml:"watermark"`
	PrintScreen *bool          `json:"printScreen" yaml:"printScreen"`
	Print       *PrintSettings `json:"print" yaml:"print"`
	ValidPeriod *ValidPeriod   `json:"validPeriod" yaml:"validPeriod"`
}

type PrintSettings struct {
	Printable bool `json:"printable" yaml:"printable"`
	Copies    *int `json:"copies" yaml:"copies"`
}

type ValidPeriod struct {
	Start string `json:"start" yaml:"start"`
	End   string `json:"end" yaml:"end"`
}

type Template struct {
	ID     uint64    `json:"id" yaml:"id"`
	Name   string    `json:"name" yaml:"name"`
	ZOrder string    `json:"zOrder" yaml:"zOrder"`
	Layers []Layer   `json:"layers" yaml:"layers"`
	Items  []Item    `json:"items" yaml:"items"`
	Area   *PageArea `json:"area" yaml:"area"`
}

type TemplateRef struct {
	ID     uint64 `json:"id" yaml:"id"`
	ZOrder string `json:"zOrder" yaml:"zOrder"`
}

type Layer struct {
	Type      string `json:"type" yaml:"type"`
	DrawParam string `json:"drawParam" yaml:"drawParam"`
	Items     []Item `json:"items" yaml:"items"`
}

type PageArea struct {
	PhysicalBox    *Box `json:"physicalBox" yaml:"physicalBox"`
	ApplicationBox *Box `json:"applicationBox" yaml:"applicationBox"`
	ContentBox     *Box `json:"contentBox" yaml:"contentBox"`
	BleedBox       *Box `json:"bleedBox" yaml:"bleedBox"`
}

type CustomData struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type Preferences struct {
	PageMode     string   `json:"pageMode" yaml:"pageMode"`
	PageLayout   string   `json:"pageLayout" yaml:"pageLayout"`
	TabDisplay   string   `json:"tabDisplay" yaml:"tabDisplay"`
	HideToolbar  *bool    `json:"hideToolbar" yaml:"hideToolbar"`
	HideMenubar  *bool    `json:"hideMenubar" yaml:"hideMenubar"`
	HideWindowUI *bool    `json:"hideWindowUI" yaml:"hideWindowUI"`
	ZoomMode     string   `json:"zoomMode" yaml:"zoomMode"`
	Zoom         *float64 `json:"zoom" yaml:"zoom"`
}

type ColorSpace struct {
	ID               uint64   `json:"id" yaml:"id"`
	Type             string   `json:"type" yaml:"type"`
	BitsPerComponent int      `json:"bitsPerComponent" yaml:"bitsPerComponent"`
	Palette          []string `json:"palette" yaml:"palette"`
	ProfileFile      string   `json:"profileFile" yaml:"profileFile"`
	ProfileBase64    string   `json:"profileBase64" yaml:"profileBase64"`
	ProfileName      string   `json:"profileName" yaml:"profileName"`
}

type DrawParam struct {
	Name        string    `json:"name" yaml:"name"`
	Relative    string    `json:"relative" yaml:"relative"`
	LineWidth   float64   `json:"lineWidth" yaml:"lineWidth"`
	Join        string    `json:"join" yaml:"join"`
	Cap         string    `json:"cap" yaml:"cap"`
	DashOffset  float64   `json:"dashOffset" yaml:"dashOffset"`
	DashPattern []float64 `json:"dashPattern" yaml:"dashPattern"`
	MiterLimit  float64   `json:"miterLimit" yaml:"miterLimit"`
	FillColor   *Color    `json:"fillColor" yaml:"fillColor"`
	StrokeColor *Color    `json:"strokeColor" yaml:"strokeColor"`
}

type Composite struct {
	ID           uint64  `json:"id" yaml:"id"`
	Width        float64 `json:"width" yaml:"width"`
	Height       float64 `json:"height" yaml:"height"`
	Thumbnail    uint64  `json:"thumbnail" yaml:"thumbnail"`
	Substitution uint64  `json:"substitution" yaml:"substitution"`
	Items        []Item  `json:"items" yaml:"items"`
}

type Color struct {
	R          uint8          `json:"r" yaml:"r"`
	G          uint8          `json:"g" yaml:"g"`
	B          uint8          `json:"b" yaml:"b"`
	Components []int          `json:"components" yaml:"components"`
	ColorSpace uint64         `json:"colorSpace" yaml:"colorSpace"`
	Index      *int           `json:"index" yaml:"index"`
	Alpha      *uint8         `json:"alpha" yaml:"alpha"`
	Axial      *AxialShading  `json:"axial" yaml:"axial"`
	Radial     *RadialShading `json:"radial" yaml:"radial"`
	Gouraud    *Gouraud       `json:"gouraud" yaml:"gouraud"`
	LaGouraud  *LaGouraud     `json:"laGouraud" yaml:"laGouraud"`
	Pattern    *Pattern       `json:"pattern" yaml:"pattern"`
}

type ColorStop struct {
	Position float64 `json:"position" yaml:"position"`
	Color    Color   `json:"color" yaml:"color"`
}

type AxialShading struct {
	MapType    string      `json:"mapType" yaml:"mapType"`
	MapUnit    float64     `json:"mapUnit" yaml:"mapUnit"`
	Extend     int         `json:"extend" yaml:"extend"`
	StartPoint string      `json:"startPoint" yaml:"startPoint"`
	EndPoint   string      `json:"endPoint" yaml:"endPoint"`
	Segments   []ColorStop `json:"segments" yaml:"segments"`
}

type RadialShading struct {
	MapType      string      `json:"mapType" yaml:"mapType"`
	MapUnit      float64     `json:"mapUnit" yaml:"mapUnit"`
	Eccentricity float64     `json:"eccentricity" yaml:"eccentricity"`
	Angle        float64     `json:"angle" yaml:"angle"`
	StartPoint   string      `json:"startPoint" yaml:"startPoint"`
	StartRadius  float64     `json:"startRadius" yaml:"startRadius"`
	EndPoint     string      `json:"endPoint" yaml:"endPoint"`
	EndRadius    float64     `json:"endRadius" yaml:"endRadius"`
	Extend       int         `json:"extend" yaml:"extend"`
	Segments     []ColorStop `json:"segments" yaml:"segments"`
}

type Gouraud struct {
	Extend    int            `json:"extend" yaml:"extend"`
	Points    []GouraudPoint `json:"points" yaml:"points"`
	BackColor *Color         `json:"backColor" yaml:"backColor"`
}

type GouraudPoint struct {
	X        float64 `json:"x" yaml:"x"`
	Y        float64 `json:"y" yaml:"y"`
	EdgeFlag int     `json:"edgeFlag" yaml:"edgeFlag"`
	Color    Color   `json:"color" yaml:"color"`
}

type LaGouraud struct {
	VerticesPerRow int              `json:"verticesPerRow" yaml:"verticesPerRow"`
	Extend         int              `json:"extend" yaml:"extend"`
	Points         []LaGouraudPoint `json:"points" yaml:"points"`
	BackColor      *Color           `json:"backColor" yaml:"backColor"`
}

type LaGouraudPoint struct {
	X     float64 `json:"x" yaml:"x"`
	Y     float64 `json:"y" yaml:"y"`
	Color Color   `json:"color" yaml:"color"`
}

type Pattern struct {
	Width         float64   `json:"width" yaml:"width"`
	Height        float64   `json:"height" yaml:"height"`
	XStep         float64   `json:"xStep" yaml:"xStep"`
	YStep         float64   `json:"yStep" yaml:"yStep"`
	ReflectMethod string    `json:"reflectMethod" yaml:"reflectMethod"`
	RelativeTo    string    `json:"relativeTo" yaml:"relativeTo"`
	CTM           []float64 `json:"ctm" yaml:"ctm"`
	Thumbnail     uint64    `json:"thumbnail" yaml:"thumbnail"`
	Items         []Item    `json:"items" yaml:"items"`
	Layers        []Layer   `json:"layers" yaml:"layers"`
}

type TextCode struct {
	Value  string    `json:"value" yaml:"value"`
	X      *float64  `json:"x" yaml:"x"`
	Y      *float64  `json:"y" yaml:"y"`
	DeltaX []float64 `json:"deltaX" yaml:"deltaX"`
	DeltaY []float64 `json:"deltaY" yaml:"deltaY"`
}

type Action struct {
	Event  string        `json:"event" yaml:"event"`
	Region *ActionRegion `json:"region" yaml:"region"`
	URI    *URIAction    `json:"uri" yaml:"uri"`
	Goto   *GotoAction   `json:"goto" yaml:"goto"`
	GotoA  *GotoAAction  `json:"gotoA" yaml:"gotoA"`
	Sound  *SoundAction  `json:"sound" yaml:"sound"`
	Movie  *MovieAction  `json:"movie" yaml:"movie"`
}

type ActionRegion struct {
	Areas []ActionArea `json:"areas" yaml:"areas"`
}

type ActionArea struct {
	Start    Point           `json:"start" yaml:"start"`
	Commands []RegionCommand `json:"commands" yaml:"commands"`
}

type Point struct {
	X float64 `json:"x" yaml:"x"`
	Y float64 `json:"y" yaml:"y"`
}

type RegionCommand struct {
	Type           string  `json:"type" yaml:"type"`
	X              float64 `json:"x" yaml:"x"`
	Y              float64 `json:"y" yaml:"y"`
	ControlX       float64 `json:"controlX" yaml:"controlX"`
	ControlY       float64 `json:"controlY" yaml:"controlY"`
	Control1X      float64 `json:"control1X" yaml:"control1X"`
	Control1Y      float64 `json:"control1Y" yaml:"control1Y"`
	Control2X      float64 `json:"control2X" yaml:"control2X"`
	Control2Y      float64 `json:"control2Y" yaml:"control2Y"`
	SweepDirection bool    `json:"sweepDirection" yaml:"sweepDirection"`
	LargeArc       bool    `json:"largeArc" yaml:"largeArc"`
	RotationAngle  float64 `json:"rotationAngle" yaml:"rotationAngle"`
	EllipseWidth   float64 `json:"ellipseWidth" yaml:"ellipseWidth"`
	EllipseHeight  float64 `json:"ellipseHeight" yaml:"ellipseHeight"`
}

type URIAction struct {
	URI    string `json:"uri" yaml:"uri"`
	Base   string `json:"base" yaml:"base"`
	Target string `json:"target" yaml:"target"`
}
type GotoAction struct {
	Page     int      `json:"page" yaml:"page"`
	Type     string   `json:"type" yaml:"type"`
	Bookmark string   `json:"bookmark" yaml:"bookmark"`
	Left     *float64 `json:"left" yaml:"left"`
	Top      *float64 `json:"top" yaml:"top"`
	Right    *float64 `json:"right" yaml:"right"`
	Bottom   *float64 `json:"bottom" yaml:"bottom"`
	Zoom     *float64 `json:"zoom" yaml:"zoom"`
}
type GotoAAction struct {
	AttachID  string `json:"attachId" yaml:"attachId"`
	NewWindow *bool  `json:"newWindow" yaml:"newWindow"`
}
type SoundAction struct {
	ResourceID  uint64 `json:"resourceId" yaml:"resourceId"`
	Volume      *int   `json:"volume" yaml:"volume"`
	Repeat      *bool  `json:"repeat" yaml:"repeat"`
	Synchronous *bool  `json:"synchronous" yaml:"synchronous"`
}
type MovieAction struct {
	ResourceID uint64 `json:"resourceId" yaml:"resourceId"`
	Operator   string `json:"operator" yaml:"operator"`
}
type Bookmark struct {
	Name string     `json:"name" yaml:"name"`
	Goto GotoAction `json:"goto" yaml:"goto"`
}
type Attachment struct {
	ID           string `json:"id" yaml:"id"`
	Name         string `json:"name" yaml:"name"`
	Format       string `json:"format" yaml:"format"`
	Usage        string `json:"usage" yaml:"usage"`
	File         string `json:"file" yaml:"file"`
	DataBase64   string `json:"dataBase64" yaml:"dataBase64"`
	FileName     string `json:"fileName" yaml:"fileName"`
	CreationDate string `json:"creationDate" yaml:"creationDate"`
	ModDate      string `json:"modDate" yaml:"modDate"`
	Visible      *bool  `json:"visible" yaml:"visible"`
}
type Extension struct {
	AppName        string              `json:"appName" yaml:"appName"`
	Company        string              `json:"company" yaml:"company"`
	AppVersion     string              `json:"appVersion" yaml:"appVersion"`
	Date           string              `json:"date" yaml:"date"`
	RefID          uint64              `json:"refId" yaml:"refId"`
	Data           string              `json:"data" yaml:"data"`
	DataXML        string              `json:"dataXml" yaml:"dataXml"`
	DataFile       string              `json:"dataFile" yaml:"dataFile"`
	DataFileBase64 string              `json:"dataFileBase64" yaml:"dataFileBase64"`
	DataName       string              `json:"dataName" yaml:"dataName"`
	Properties     []ExtensionProperty `json:"properties" yaml:"properties"`
}
type ExtensionProperty struct {
	Name  string `json:"name" yaml:"name"`
	Type  string `json:"type" yaml:"type"`
	Value string `json:"value" yaml:"value"`
}
type Version struct {
	ID            string        `json:"id" yaml:"id"`
	Index         int           `json:"index" yaml:"index"`
	Current       bool          `json:"current" yaml:"current"`
	Version       string        `json:"version" yaml:"version"`
	Name          string        `json:"name" yaml:"name"`
	CreationDate  string        `json:"creationDate" yaml:"creationDate"`
	DocRoot       string        `json:"docRoot" yaml:"docRoot"`
	DocRootBase64 string        `json:"docRootBase64" yaml:"docRootBase64"`
	DocRootName   string        `json:"docRootName" yaml:"docRootName"`
	Files         []VersionFile `json:"files" yaml:"files"`
}
type VersionFile struct {
	ID   string `json:"id" yaml:"id"`
	Path string `json:"path" yaml:"path"`
}

type Outline struct {
	Title    string    `json:"title" yaml:"title"`
	Count    *int      `json:"count" yaml:"count"`
	Expanded *bool     `json:"expanded" yaml:"expanded"`
	Actions  []Action  `json:"actions" yaml:"actions"`
	Children []Outline `json:"children" yaml:"children"`
}

type Signature struct {
	ID              string               `json:"id" yaml:"id"`
	Type            string               `json:"type" yaml:"type"`
	ProviderName    string               `json:"providerName" yaml:"providerName"`
	ProviderVersion string               `json:"providerVersion" yaml:"providerVersion"`
	Company         string               `json:"company" yaml:"company"`
	Method          string               `json:"method" yaml:"method"`
	Date            string               `json:"date" yaml:"date"`
	CheckMethod     string               `json:"checkMethod" yaml:"checkMethod"`
	References      []SignatureReference `json:"references" yaml:"references"`
	StampAnnots     []SignatureStamp     `json:"stampAnnots" yaml:"stampAnnots"`
	SealFile        string               `json:"sealFile" yaml:"sealFile"`
	SealFileBase64  string               `json:"sealFileBase64" yaml:"sealFileBase64"`
	SealName        string               `json:"sealName" yaml:"sealName"`
	SignedValue     string               `json:"signedValue" yaml:"signedValue"`
	SignedValueName string               `json:"signedValueName" yaml:"signedValueName"`
}

type SignatureReference struct {
	FileRef    string `json:"fileRef" yaml:"fileRef"`
	CheckValue string `json:"checkValue" yaml:"checkValue"`
}

type SignatureStamp struct {
	ID       string `json:"id" yaml:"id"`
	Page     int    `json:"page" yaml:"page"`
	Boundary Box    `json:"boundary" yaml:"boundary"`
	Clip     *Box   `json:"clip" yaml:"clip"`
}

type Box struct {
	X      float64 `json:"x" yaml:"x"`
	Y      float64 `json:"y" yaml:"y"`
	Width  float64 `json:"width" yaml:"width"`
	Height float64 `json:"height" yaml:"height"`
}

type AnnotationPage struct {
	Page  int          `json:"page" yaml:"page"`
	Items []Annotation `json:"items" yaml:"items"`
}

type Annotation struct {
	ID          uint64                `json:"id" yaml:"id"`
	Type        string                `json:"type" yaml:"type"`
	Creator     string                `json:"creator" yaml:"creator"`
	LastModDate string                `json:"lastModDate" yaml:"lastModDate"`
	Visible     *bool                 `json:"visible" yaml:"visible"`
	Subtype     string                `json:"subtype" yaml:"subtype"`
	Print       *bool                 `json:"print" yaml:"print"`
	NoZoom      bool                  `json:"noZoom" yaml:"noZoom"`
	NoRotate    bool                  `json:"noRotate" yaml:"noRotate"`
	ReadOnly    *bool                 `json:"readOnly" yaml:"readOnly"`
	Remark      string                `json:"remark" yaml:"remark"`
	Parameters  []AnnotationParameter `json:"parameters" yaml:"parameters"`
	Boundary    *Box                  `json:"boundary" yaml:"boundary"`
	Items       []Item                `json:"items" yaml:"items"`
}

type AnnotationParameter struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type CGTransform struct {
	CodePosition int   `json:"codePosition" yaml:"codePosition"`
	CodeCount    int   `json:"codeCount" yaml:"codeCount"`
	GlyphCount   int   `json:"glyphCount" yaml:"glyphCount"`
	Glyphs       []int `json:"glyphs" yaml:"glyphs"`
}

type Item struct {
	Type          string        `json:"type" yaml:"type"`
	X             float64       `json:"x" yaml:"x"`
	Y             float64       `json:"y" yaml:"y"`
	Width         float64       `json:"width" yaml:"width"`
	Height        float64       `json:"height" yaml:"height"`
	Value         string        `json:"value" yaml:"value"`
	Font          string        `json:"font" yaml:"font"`
	Size          float64       `json:"size" yaml:"size"`
	Data          string        `json:"data" yaml:"data"`
	DataBase64    string        `json:"dataBase64" yaml:"dataBase64"`
	Format        string        `json:"format" yaml:"format"`
	Name          string        `json:"name" yaml:"name"`
	Visible       *bool         `json:"visible" yaml:"visible"`
	ResourceID    uint64        `json:"resourceId" yaml:"resourceId"`
	Stroke        bool          `json:"stroke" yaml:"stroke"`
	Fill          *bool         `json:"fill" yaml:"fill"`
	StrokeSet     *bool         `json:"strokeSet" yaml:"strokeSet"`
	Rule          string        `json:"rule" yaml:"rule"`
	LineWidth     float64       `json:"lineWidth" yaml:"lineWidth"`
	Cap           string        `json:"cap" yaml:"cap"`
	Join          string        `json:"join" yaml:"join"`
	MiterLimit    float64       `json:"miterLimit" yaml:"miterLimit"`
	DashOffset    float64       `json:"dashOffset" yaml:"dashOffset"`
	DashPattern   []float64     `json:"dashPattern" yaml:"dashPattern"`
	Alpha         *uint8        `json:"alpha" yaml:"alpha"`
	DrawParam     string        `json:"drawParam" yaml:"drawParam"`
	CTM           []float64     `json:"ctm" yaml:"ctm"`
	FillColor     *Color        `json:"fillColor" yaml:"fillColor"`
	StrokeColor   *Color        `json:"strokeColor" yaml:"strokeColor"`
	TextCodes     []TextCode    `json:"textCodes" yaml:"textCodes"`
	CGTransforms  []CGTransform `json:"cgTransforms" yaml:"cgTransforms"`
	Actions       []Action      `json:"actions" yaml:"actions"`
	Items         []Item        `json:"items" yaml:"items"`
	HScale        float64       `json:"hScale" yaml:"hScale"`
	ReadDirection int           `json:"readDirection" yaml:"readDirection"`
	CharDirection int           `json:"charDirection" yaml:"charDirection"`
	Weight        int           `json:"weight" yaml:"weight"`
	Italic        bool          `json:"italic" yaml:"italic"`
	Clips         []Clip        `json:"clips" yaml:"clips"`
	Substitution  uint64        `json:"substitution" yaml:"substitution"`
	ImageMask     uint64        `json:"imageMask" yaml:"imageMask"`
	Border        *ImageBorder  `json:"border" yaml:"border"`
}

type Clip struct {
	Areas []ClipArea `json:"areas" yaml:"areas"`
}

type ClipArea struct {
	DrawParam string    `json:"drawParam" yaml:"drawParam"`
	CTM       []float64 `json:"ctm" yaml:"ctm"`
	Path      *ClipPath `json:"path" yaml:"path"`
	Text      *ClipText `json:"text" yaml:"text"`
}

type ClipPath struct {
	Boundary    Box       `json:"boundary" yaml:"boundary"`
	Name        string    `json:"name" yaml:"name"`
	Visible     *bool     `json:"visible" yaml:"visible"`
	CTM         []float64 `json:"ctm" yaml:"ctm"`
	Data        string    `json:"data" yaml:"data"`
	Stroke      bool      `json:"stroke" yaml:"stroke"`
	StrokeSet   *bool     `json:"strokeSet" yaml:"strokeSet"`
	Fill        bool      `json:"fill" yaml:"fill"`
	Rule        string    `json:"rule" yaml:"rule"`
	LineWidth   float64   `json:"lineWidth" yaml:"lineWidth"`
	Cap         string    `json:"cap" yaml:"cap"`
	Join        string    `json:"join" yaml:"join"`
	MiterLimit  float64   `json:"miterLimit" yaml:"miterLimit"`
	DashOffset  float64   `json:"dashOffset" yaml:"dashOffset"`
	DashPattern []float64 `json:"dashPattern" yaml:"dashPattern"`
	Alpha       *uint8    `json:"alpha" yaml:"alpha"`
	StrokeColor *Color    `json:"strokeColor" yaml:"strokeColor"`
	FillColor   *Color    `json:"fillColor" yaml:"fillColor"`
}

type ClipText struct {
	Boundary      Box        `json:"boundary" yaml:"boundary"`
	CTM           []float64  `json:"ctm" yaml:"ctm"`
	Font          string     `json:"font" yaml:"font"`
	Size          float64    `json:"size" yaml:"size"`
	Value         string     `json:"value" yaml:"value"`
	TextCodes     []TextCode `json:"textCodes" yaml:"textCodes"`
	Stroke        bool       `json:"stroke" yaml:"stroke"`
	Fill          *bool      `json:"fill" yaml:"fill"`
	HScale        float64    `json:"hScale" yaml:"hScale"`
	ReadDirection int        `json:"readDirection" yaml:"readDirection"`
	CharDirection int        `json:"charDirection" yaml:"charDirection"`
	Weight        int        `json:"weight" yaml:"weight"`
	Italic        bool       `json:"italic" yaml:"italic"`
	FillColor     *Color     `json:"fillColor" yaml:"fillColor"`
	StrokeColor   *Color     `json:"strokeColor" yaml:"strokeColor"`
}

type ImageBorder struct {
	LineWidth        float64   `json:"lineWidth" yaml:"lineWidth"`
	HorizontalRadius float64   `json:"horizontalRadius" yaml:"horizontalRadius"`
	VerticalRadius   float64   `json:"verticalRadius" yaml:"verticalRadius"`
	DashOffset       float64   `json:"dashOffset" yaml:"dashOffset"`
	DashPattern      []float64 `json:"dashPattern" yaml:"dashPattern"`
	Color            *Color    `json:"color" yaml:"color"`
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
