package models

import "encoding/xml"

// Document OFD 文档主体定义。
type Document struct {
	// XMLName 文档主体根元素名称。
	XMLName xml.Name `xml:"Document"`
	// XMLNS OFD 命名空间地址。
	XMLNS string `xml:"xmlns:ofd,attr"`
	// CommonData 文档公共数据。
	CommonData CommonData `xml:"CommonData"`
	// Pages 文档页面列表。
	Pages PageList `xml:"Pages"`
	// Outlines 文档大纲。
	Outlines *OutlineList `xml:"Outlines,omitempty"`
	// Permissions 文档权限设置。
	Permissions *CT_Permission `xml:"Permissions,omitempty"`
	// Actions 文档级动作集合。
	Actions *ActionList `xml:"Actions,omitempty"`
	// VPreferences 文档视图首选项。
	VPreferences *CT_VPreferences `xml:"VPreferences,omitempty"`
	// Bookmarks 文档书签列表。
	Bookmarks *BookmarkList `xml:"Bookmarks,omitempty"`
	// Annotations 注解文件的位置。
	Annotations *StLoc `xml:"Annotations,omitempty"`
	// CustomTags 自定义标签文件的位置。
	CustomTags *StLoc `xml:"CustomTags,omitempty"`
	// Attachments 附件文件的位置。
	Attachments *StLoc `xml:"Attachments,omitempty"`
	// Extensions 扩展文件的位置。
	Extensions *StLoc `xml:"Extensions,omitempty"`
}

// CommonData 文档公共数据定义。
type CommonData struct {
	// MaxUnitID 文档中已使用的最大对象标识。
	MaxUnitID StID `xml:"MaxUnitID"`
	// PageArea 文档页面区域定义。
	PageArea CtPageArea `xml:"PageArea"`
	// PublicRes 公共资源文件的位置列表。
	PublicRes []StLoc `xml:"PublicRes,omitempty"`
	// DocumentRes 文档资源文件的位置列表。
	DocumentRes []StLoc `xml:"DocumentRes,omitempty"`
	// TemplatePages 文档模板页列表。
	TemplatePages []TemplatePage `xml:"TemplatePage,omitempty"`
	// DefaultCS 文档默认颜色空间资源引用。
	DefaultCS *StRefID `xml:"DefaultCS,omitempty"`
}

// TemplatePage 文档模板页定义。
type TemplatePage struct {
	// ID 模板页标识，在文档内唯一。
	ID StID `xml:"ID,attr"`
	// Name 模板页名称。
	Name *string `xml:"Name,attr,omitempty"`
	// ZOrder 模板页的叠放顺序。
	ZOrder *string `xml:"ZOrder,attr,omitempty"`
	// BaseLoc 模板页内容文件的位置。
	BaseLoc StLoc `xml:"BaseLoc,attr"`
}

// PageList 文档页面列表。
type PageList struct {
	// Pages 页面列表。
	Pages []Page `xml:"Page"`
}

// OutlineList 文档大纲列表。
type OutlineList struct {
	// OutlineElems 大纲项列表。
	OutlineElems []CTOutlineElem `xml:"OutlineElem"`
}

// BookmarkList 文档书签列表。
type BookmarkList struct {
	// Bookmarks 书签列表。
	Bookmarks []CTBookmark `xml:"Bookmark"`
}

// CT_Permission 文档权限定义。
type CT_Permission struct {
	// Edit 是否允许编辑文档内容。
	Edit *bool `xml:"Edit,omitempty"`
	// Annot 是否允许添加或修改注解。
	Annot *bool `xml:"Annot,omitempty"`
	// Export 是否允许导出文档内容。
	Export *bool `xml:"Export,omitempty"`
	// Signature 是否允许签名。
	Signature *bool `xml:"Signature,omitempty"`
	// Watermark 是否允许添加或修改水印。
	Watermark *bool `xml:"Watermark,omitempty"`
	// PrintScreen 是否允许截屏。
	PrintScreen *bool `xml:"PrintScreen,omitempty"`
	// Print 文档打印权限设置。
	Print *PrintSettings `xml:"Print,omitempty"`
	// ValidPeriod 权限有效期。
	ValidPeriod *ValidPeriod `xml:"ValidPeriod,omitempty"`
}

// PrintSettings 文档打印权限设置。
type PrintSettings struct {
	// Printable 是否允许打印。
	Printable bool `xml:"Printable,attr"`
	// Copies 允许打印的份数。
	Copies int `xml:"Copies,attr"`
}

// ValidPeriod 权限有效期定义。
type ValidPeriod struct {
	// StartDate 权限生效时间。
	StartDate DateTime `xml:"StartDate,attr,omitempty"`
	// EndDate 权限失效时间。
	EndDate DateTime `xml:"EndDate,attr,omitempty"`
}

// CT_VPreferences 文档视图首选项定义。
type CT_VPreferences struct {
	// PageMode 文档打开时的页面显示模式。
	PageMode *PageMode `xml:"PageMode,omitempty"`
	// PageLayout 文档打开时的页面布局方式。
	PageLayout *PageLayout `xml:"PageLayout,omitempty"`
	// TabDisplay 文档标签中显示的标题类型。
	TabDisplay *TabDisplay `xml:"TabDisplay,omitempty"`
	// HideToolbar 是否隐藏工具栏。
	HideToolbar *bool `xml:"HideToolbar,omitempty"`
	// HideMenubar 是否隐藏菜单栏。
	HideMenubar *bool `xml:"HideMenubar,omitempty"`
	// HideWindowUI 是否隐藏窗口用户界面。
	HideWindowUI *bool `xml:"HideWindowUI,omitempty"`
	// Zoom 文档打开时的缩放设置。
	Zoom *ZoomSetting `xml:",omitempty"`
}

// PageMode 文档打开时的页面显示模式。
type PageMode string

const (
	// PageModeNone 使用默认页面显示模式。
	PageModeNone PageMode = "None"
	// PageModeFullScreen 以全屏模式显示文档。
	PageModeFullScreen PageMode = "FullScreen"
	// PageModeUseOutlines 显示文档大纲面板。
	PageModeUseOutlines PageMode = "UseOutlines"
	// 其他页面显示模式由 OFD 规范定义。
)

// PageLayout 文档打开时的页面布局方式。
type PageLayout string

const (
	// PageLayoutOneColumn 以单列方式显示页面。
	PageLayoutOneColumn PageLayout = "OneColumn"
	// PageLayoutTwoPageL 以双页方式显示页面，页面从左向右排列。
	PageLayoutTwoPageL PageLayout = "TwoPageL"
	// 其他页面布局方式由 OFD 规范定义。
)

// TabDisplay 文档标签中显示的标题类型。
type TabDisplay string

const (
	// TabDisplayDocTitle 在标签中显示文档标题。
	TabDisplayDocTitle TabDisplay = "DocTitle"
	// TabDisplayFileName 在标签中显示文件名。
	TabDisplayFileName TabDisplay = "FileName"
)

// ZoomSetting 文档打开时的页面缩放设置。
type ZoomSetting struct {
	// Mode 缩放模式。
	Mode *string `xml:"ZoomMode,omitempty"`
	// Value 缩放比例。
	Value *float64 `xml:"Zoom,omitempty"`
}

// CTOutlineElem 文档大纲中的单个大纲项。
type CTOutlineElem struct {
	// Title 大纲项标题。
	Title string `xml:"Title,attr"`
	// Count 大纲项后代节点数量。
	Count *int `xml:"Count,attr,omitempty"`
	// Expanded 是否展开该大纲项的子项。
	Expanded *bool `xml:"Expanded,attr,omitempty"`
	// Actions 选择该大纲项时执行的动作集合。
	Actions *ActionList `xml:"Actions,omitempty"`
	// OutlineElem 子大纲项列表。
	OutlineElem []CTOutlineElem `xml:"OutlineElem,omitempty"`
}

// ActionList 动作列表。
type ActionList struct {
	// Actions 动作列表。
	Actions []CtAction `xml:"Action"`
}

// CTBookmark 文档书签定义。
type CTBookmark struct {
	// Name 书签名称。
	Name string `xml:"Name,attr"`
	// Dest 书签指向的目标位置。
	Dest CtDest `xml:"Dest"`
}

// CustomTags 自定义标签容器。
type CustomTags struct {
	// XMLName 自定义标签根元素名称。
	XMLName xml.Name `xml:"CustomTags"`
	// Xmlns 自定义标签命名空间地址。
	Xmlns string `xml:"xmlns,attr"`
	// CustomTags 自定义标签列表。
	CustomTags []CustomTag `xml:"CustomTag,omitempty"`
}

// CustomTag 单个自定义标签定义。
type CustomTag struct {
	// NameSpace 自定义标签使用的命名空间。
	NameSpace string `xml:"NameSpace,attr"`
	// SchemaLoc 自定义标签模式文件的位置。
	SchemaLoc *StLoc `xml:"SchemaLoc,omitempty"`
	// FileLoc 自定义标签数据文件的位置。
	FileLoc StLoc `xml:"FileLoc"`
}

// String 返回 Document 的 XML 字符串表示，并设置 OFD 命名空间。
func (p *Document) String() string {
	p.XMLNS = "http://www.ofdspec.org/2016"
	buf, err := xml.MarshalIndent(p, "", "  ")
	if err != nil {
		return ""
	}
	return xml.Header + string(buf)
}
