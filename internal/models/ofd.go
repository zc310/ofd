package models

import (
	"encoding/xml"
)

// OFD 文档根元素
type OFD struct {
	XMLName   xml.Name  `xml:"OFD"`
	XMLNS     string    `xml:"xmlns:ofd,attr"`
	Version   string    `xml:"Version,attr"`
	DocType   string    `xml:"DocType,attr"`
	DocBodies []DocBody `xml:"DocBody"`
}

// DocBody 文档体
type DocBody struct {
	DocInfo    DocInfo   `xml:"DocInfo"`
	DocRoot    StLoc     `xml:"DocRoot"`
	Versions   *Versions `xml:"Versions,omitempty"`
	Signatures *StLoc    `xml:"Signatures,omitempty"`
}

// Versions 版本集合
type Versions struct {
	VersionList []Version `xml:"Version"`
}

// Version 文档版本
type Version struct {
	ID      string `xml:"ID,attr"`
	Index   int    `xml:"Index,attr"`
	Current bool   `xml:"Current,attr"`
	BaseLoc StLoc  `xml:"BaseLoc,attr"`
}

// DocInfo 文档信息
type DocInfo struct {
	DocID          string       `xml:"DocID"`
	Title          *string      `xml:"Title,omitempty"`
	Author         *string      `xml:"Author,omitempty"`
	Subject        *string      `xml:"Subject,omitempty"`
	Abstract       *string      `xml:"Abstract,omitempty"`
	CreationDate   *DateTime    `xml:"CreationDate,omitempty"`
	ModDate        *DateTime    `xml:"ModDate,omitempty"`
	DocUsage       *string      `xml:"DocUsage,omitempty"`
	Cover          *StLoc       `xml:"Cover,omitempty"`
	Keywords       *Keywords    `xml:"Keywords,omitempty"`
	Creator        *string      `xml:"Creator,omitempty"`
	CreatorVersion *string      `xml:"CreatorVersion,omitempty"`
	CustomDatas    *CustomDatas `xml:"CustomDatas,omitempty"`
}

// Keywords 关键词集合
type Keywords struct {
	Keyword []string `xml:"Keyword"`
}

func (p *OFD) String() string {
	//p.XMLNS = "http://www.ofdspec.org/2016"

	buf, err := xml.MarshalIndent(p, "", "  ")
	if err != nil {
		return ""
	}
	return xml.Header + string(buf)
}
