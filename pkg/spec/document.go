package spec

import (
	"encoding/xml"
)

// Document 是 GB/T 33190—2016 §7.5 表5的文档主体声明，不包含加载状态。
type Document struct {
	// XMLName 文档主体根元素名称。
	XMLName xml.Name `xml:"Document"`
	// XMLNS 保留命名空间声明字段；XML 解码后的根命名空间以 XMLName.Space 为准。
	XMLNS string `xml:"xmlns:ofd,attr,omitempty"`
	// CommonData 文档公共数据。
	CommonData CommonData `xml:"CommonData"`
	// Pages 文档页面列表。
	Pages PageList `xml:"Pages"`
	// Outlines 文档大纲。
	Outlines *OutlineList `xml:"Outlines,omitempty"`
	// Permissions 文档权限设置。
	Permissions *Permissions `xml:"Permissions,omitempty"`
	// Actions 文档级动作集合。
	Actions *ActionList `xml:"Actions,omitempty"`
	// VPreferences 文档视图首选项。
	VPreferences *VPreferences `xml:"VPreferences,omitempty"`
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

// CommonData 是 §7.5 表6的文档公共数据，不自动补齐默认页面尺寸。
type CommonData struct {
	// MaxUnitID 文档中已使用的最大对象标识。
	MaxUnitID StID `xml:"MaxUnitID"` //nolint:tagliatelle // 标准元素名为 MaxUnitID。
	// PageArea 文档页面区域定义。
	PageArea PageArea `xml:"PageArea"`
	// PublicRes 公共资源文件的位置列表。
	PublicRes []StLoc `xml:"PublicRes,omitempty"`
	// DocumentRes 文档资源文件的位置列表。
	DocumentRes []StLoc `xml:"DocumentRes,omitempty"`
	// TemplatePages 文档模板页列表。
	TemplatePages []TemplatePage `xml:"TemplatePage,omitempty"`
	// DefaultCS 文档默认颜色空间资源引用。
	DefaultCS *StRefID `xml:"DefaultCS,omitempty"` //nolint:tagliatelle // 标准元素名为 DefaultCS。
}

// TemplatePage 是 §7.7 表13的模板页声明。
type TemplatePage struct {
	// ID 模板页标识，在文档内唯一。
	ID StID `xml:"ID,attr"` //nolint:tagliatelle // 标准属性名为 ID。
	// Name 模板页名称。
	Name *string `xml:"Name,attr,omitempty"`
	// ZOrder 模板页的叠放顺序。
	ZOrder ZOrder `xml:"ZOrder,attr,omitempty"`
	// BaseLoc 模板页内容文件的位置。
	BaseLoc StLoc `xml:"BaseLoc,attr"`
}

// PageList 是 §7.6 表11的页树，页面引用顺序即文档页序。
type PageList struct {
	// Pages 页面列表。
	Pages []PageRef `xml:"Page"`
}

// PageRef 是页树中的页面标识和内容文件引用，不是页面内容本身。
type PageRef struct {
	ID      StID  `xml:"ID,attr"` //nolint:tagliatelle // 标准属性名为 ID。
	BaseLoc StLoc `xml:"BaseLoc,attr"`
}

// OutlineList 是 §7.8 表17的文档大纲列表。
type OutlineList struct {
	// OutlineElems 大纲项列表。
	OutlineElems []OutlineElem `xml:"OutlineElem"`
}

// BookmarkList 是 §7.5 表10的文档书签列表。
type BookmarkList struct {
	// Bookmarks 书签列表。
	Bookmarks []Bookmark `xml:"Bookmark"`
}

// VPreferences 是 §7.5 表9的视图首选项，保留可选字段的省略状态。
// Zoom 在 Go 中分组保存，XML 解码从同级的 ZoomMode 和 Zoom 元素取得其值。
type VPreferences struct {
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
	HideWindowUI *bool `xml:"HideWindowUI,omitempty"` //nolint:tagliatelle // 标准元素名为 HideWindowUI。
	// Zoom 文档打开时的缩放设置。
	Zoom *ZoomSetting `xml:"-"`
}

type preferencesXML struct {
	PageMode     *PageMode   `xml:"PageMode,omitempty"`
	PageLayout   *PageLayout `xml:"PageLayout,omitempty"`
	TabDisplay   *TabDisplay `xml:"TabDisplay,omitempty"`
	HideToolbar  *bool       `xml:"HideToolbar,omitempty"`
	HideMenubar  *bool       `xml:"HideMenubar,omitempty"`
	HideWindowUI *bool       `xml:"HideWindowUI,omitempty"` //nolint:tagliatelle // 标准元素名为 HideWindowUI。
	ZoomMode     *string     `xml:"ZoomMode,omitempty"`
	ZoomValue    *float64    `xml:"Zoom,omitempty"`
}

// UnmarshalXML 按表9解码同级的 ZoomMode 和 Zoom，不应用显示策略缺省值。
func (p *VPreferences) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var value preferencesXML
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	*p = VPreferences{
		PageMode: value.PageMode, PageLayout: value.PageLayout, TabDisplay: value.TabDisplay,
		HideToolbar: value.HideToolbar, HideMenubar: value.HideMenubar, HideWindowUI: value.HideWindowUI,
	}
	if value.ZoomMode != nil || value.ZoomValue != nil {
		p.Zoom = &ZoomSetting{Mode: value.ZoomMode, Value: value.ZoomValue}
	}
	return nil
}

// PageMode 文档打开时的页面显示模式。
type PageMode string

// PageLayout 文档打开时的页面布局方式。
type PageLayout string

// TabDisplay 文档标签中显示的标题类型。
type TabDisplay string

// ZoomSetting 文档打开时的页面缩放设置。
type ZoomSetting struct {
	// Mode 缩放模式。
	Mode *string `xml:"ZoomMode,omitempty"`
	// Value 缩放比例。
	Value *float64 `xml:"Zoom,omitempty"`
}

// OutlineElem 是 §7.8 表17的大纲项；动作及子大纲均按声明顺序保存。
type OutlineElem struct {
	// Title 大纲项标题。
	Title string `xml:"Title,attr"`
	// Count 叶节点数的参考值，以实际子节点为准。
	Count *int `xml:"Count,attr,omitempty"`
	// Expanded 是否展开该大纲项的子项。
	Expanded *bool `xml:"Expanded,attr,omitempty"`
	// Actions 选择该大纲项时执行的动作集合。
	Actions *ActionList `xml:"Actions,omitempty"`
	// OutlineElem 子大纲项列表。
	OutlineElem []OutlineElem `xml:"OutlineElem,omitempty"`
}

// ActionList 动作列表。
type ActionList struct {
	// Actions 动作列表。
	Actions []Action `xml:"Action"`
}

// Bookmark 是 §7.5 表10的书签，目标位置不提前解析为运行时页码。
type Bookmark struct {
	// Name 书签名称。
	Name string `xml:"Name,attr"`
	// Dest 书签指向的目标位置。
	Dest Dest `xml:"Dest"`
}
