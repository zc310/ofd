package models

import (
	"encoding/xml"
	"time"
)

// Attachments 附件列表容器
type Attachments struct {
	XMLName     xml.Name     `xml:"Attachments"`
	Xmlns       string       `xml:"xmlns,attr"`
	Attachments []Attachment `xml:"Attachment,omitempty"`
}

// Attachment 单个附件定义
type Attachment struct {
	ID           string     `xml:"ID,attr"`
	Name         string     `xml:"Name,attr"`
	Format       *string    `xml:"Format,attr,omitempty"`
	CreationDate *time.Time `xml:"CreationDate,attr,omitempty"`
	ModDate      *time.Time `xml:"ModDate,attr,omitempty"`
	Size         *float64   `xml:"Size,attr,omitempty"`
	Visible      bool       `xml:"Visible,attr,omitempty"`
	Usage        string     `xml:"Usage,attr,omitempty"`
	FileLoc      StLoc      `xml:"FileLoc"`
}
