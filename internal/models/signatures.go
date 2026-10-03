package models

import "encoding/xml"

// Signatures 签名列表容器
type Signatures struct {
	XMLName    xml.Name    `xml:"Signatures"`
	Xmlns      string      `xml:"xmlns,attr"`
	MaxSignID  *string     `xml:"MaxSignId,omitempty"`
	Signatures []Signature `xml:"Signature,omitempty"`
}
