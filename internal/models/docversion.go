package models

import (
	"encoding/xml"
	"time"
)

// DocVersion 文档版本信息
type DocVersion struct {
	XMLName      xml.Name   `xml:"DocVersion"`
	Xmlns        string     `xml:"xmlns,attr"`
	ID           string     `xml:"ID,attr"`
	Version      *string    `xml:"Version,attr,omitempty"`
	Name         *string    `xml:"Name,attr,omitempty"`
	CreationDate *time.Time `xml:"CreationDate,attr,omitempty"`
	FileList     FileList   `xml:"FileList"`
	DocRoot      StLoc      `xml:"DocRoot"`
}

// FileList 文件列表
type FileList struct {
	Files []VersionFile `xml:"File"`
}

// VersionFile 版本文件
type VersionFile struct {
	ID   string `xml:"ID,attr"`
	Path StLoc  `xml:",chardata"`
}
