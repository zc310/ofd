package models

import "encoding/xml"

// Signatures 签名列表容器
type Signatures struct {
	XMLName    xml.Name    `xml:"Signatures"`
	Xmlns      string      `xml:"xmlns,attr"`
	MaxSignID  *string     `xml:"MaxSignId,omitempty"`
	Signatures []Signature `xml:"Signature,omitempty"`
}

// SigType 签名类型枚举
type SigType string

const (
	SigTypeSeal SigType = "Seal"
	SigTypeSign SigType = "Sign"
)
