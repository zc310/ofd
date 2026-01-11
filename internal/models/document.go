package models

import "encoding/xml"

type Document struct {
	XMLName      xml.Name         `xml:"Document"`
	XMLNS        string           `xml:"xmlns:ofd,attr"`
	CommonData   CommonData       `xml:"CommonData"`
	Pages        PageList         `xml:"Pages"`
	Outlines     *OutlineList     `xml:"Outlines,omitempty"`
	Permissions  *CT_Permission   `xml:"Permissions,omitempty"`
	Actions      *ActionList      `xml:"Actions,omitempty"`
	VPreferences *CT_VPreferences `xml:"VPreferences,omitempty"`
	Bookmarks    *BookmarkList    `xml:"Bookmarks,omitempty"`
	Annotations  *StLoc           `xml:"Annotations,omitempty"`
	CustomTags   *StLoc           `xml:"CustomTags,omitempty"`
	Attachments  *StLoc           `xml:"Attachments,omitempty"`
	Extensions   *StLoc           `xml:"Extensions,omitempty"`
}

type CommonData struct {
	MaxUnitID     StID           `xml:"MaxUnitID"`
	PageArea      CtPageArea     `xml:"PageArea"`
	PublicRes     []StLoc        `xml:"PublicRes,omitempty"`
	DocumentRes   []StLoc        `xml:"DocumentRes,omitempty"`
	TemplatePages []TemplatePage `xml:"TemplatePage,omitempty"`
	DefaultCS     *StRefID       `xml:"DefaultCS,omitempty"`
}

type TemplatePage struct {
	ID      StID    `xml:"ID,attr"`
	Name    *string `xml:"Name,attr,omitempty"`
	ZOrder  *string `xml:"ZOrder,attr,omitempty"`
	BaseLoc StLoc   `xml:"BaseLoc,attr"`
}

type PageList struct {
	Pages []Page `xml:"Page"`
}

type OutlineList struct {
	OutlineElems []CTOutlineElem `xml:"OutlineElem"`
}

type BookmarkList struct {
	Bookmarks []CTBookmark `xml:"Bookmark"`
}
type CT_Permission struct {
	Edit        *bool          `xml:"Edit,omitempty"`
	Annot       *bool          `xml:"Annot,omitempty"`
	Export      *bool          `xml:"Export,omitempty"`
	Signature   *bool          `xml:"Signature,omitempty"`
	Watermark   *bool          `xml:"Watermark,omitempty"`
	PrintScreen *bool          `xml:"PrintScreen,omitempty"`
	Print       *PrintSettings `xml:"Print,omitempty"`
	ValidPeriod *ValidPeriod   `xml:"ValidPeriod,omitempty"`
}

type PrintSettings struct {
	Printable bool `xml:"Printable,attr"`
	Copies    int  `xml:"Copies,attr"`
}

type ValidPeriod struct {
	StartDate DateTime `xml:"StartDate,attr,omitempty"`
	EndDate   DateTime `xml:"EndDate,attr,omitempty"`
}
type CT_VPreferences struct {
	PageMode     *PageMode    `xml:"PageMode,omitempty"`
	PageLayout   *PageLayout  `xml:"PageLayout,omitempty"`
	TabDisplay   *TabDisplay  `xml:"TabDisplay,omitempty"`
	HideToolbar  *bool        `xml:"HideToolbar,omitempty"`
	HideMenubar  *bool        `xml:"HideMenubar,omitempty"`
	HideWindowUI *bool        `xml:"HideWindowUI,omitempty"`
	Zoom         *ZoomSetting `xml:",omitempty"`
}

type PageMode string

const (
	PageModeNone        PageMode = "None"
	PageModeFullScreen  PageMode = "FullScreen"
	PageModeUseOutlines PageMode = "UseOutlines"
	// ...其他枚举值
)

type PageLayout string

const (
	PageLayoutOneColumn PageLayout = "OneColumn"
	PageLayoutTwoPageL  PageLayout = "TwoPageL"
	// ...其他枚举值
)

type TabDisplay string

const (
	TabDisplayDocTitle TabDisplay = "DocTitle"
	TabDisplayFileName TabDisplay = "FileName"
)

type ZoomSetting struct {
	Mode  *string  `xml:"ZoomMode,omitempty"`
	Value *float64 `xml:"Zoom,omitempty"`
}
type CTOutlineElem struct {
	Title       string          `xml:"Title,attr"`
	Count       *int            `xml:"Count,attr,omitempty"`
	Expanded    *bool           `xml:"Expanded,attr,omitempty"`
	Actions     *ActionList     `xml:"Actions,omitempty"`
	OutlineElem []CTOutlineElem `xml:"OutlineElem,omitempty"`
}

type ActionList struct {
	Actions []CtAction `xml:"Action"`
}

type CTBookmark struct {
	Name string `xml:"Name,attr"`
	Dest CtDest `xml:"Dest"`
}

// CustomTags 自定义标签容器
type CustomTags struct {
	XMLName    xml.Name    `xml:"CustomTags"`
	Xmlns      string      `xml:"xmlns,attr"`
	CustomTags []CustomTag `xml:"CustomTag,omitempty"`
}

// CustomTag 单个自定义标签定义
type CustomTag struct {
	NameSpace string `xml:"NameSpace,attr"`
	SchemaLoc *StLoc `xml:"SchemaLoc,omitempty"`
	FileLoc   StLoc  `xml:"FileLoc"`
}

func (p *Document) String() string {
	p.XMLNS = "http://www.ofdspec.org/2016"
	buf, err := xml.MarshalIndent(p, "", "  ")
	if err != nil {
		return ""
	}
	return xml.Header + string(buf)
}
