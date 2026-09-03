package models

import (
	"encoding/xml"
	"time"
)

// DocVersion 文档版本信息。
type DocVersion struct {
	// XMLName 文档版本根元素名称。
	XMLName xml.Name `xml:"DocVersion"`
	// Xmlns 文档版本命名空间地址。
	Xmlns string `xml:"xmlns,attr"`
	// ID 文档版本标识。
	ID string `xml:"ID,attr"`
	// Version 文档版本号。
	Version *string `xml:"Version,attr,omitempty"`
	// Name 文档版本名称。
	Name *string `xml:"Name,attr,omitempty"`
	// CreationDate 文档版本创建时间。
	CreationDate *time.Time `xml:"CreationDate,attr,omitempty"`
	// FileList 文档版本包含的文件列表。
	FileList FileList `xml:"FileList"`
	// DocRoot 文档根目录的位置。
	DocRoot StLoc `xml:"DocRoot"`
}

// FileList 文档版本文件列表。
type FileList struct {
	// Files 文档版本文件列表。
	Files []VersionFile `xml:"File"`
}

// VersionFile 文档版本中的单个文件。
type VersionFile struct {
	// ID 文件标识。
	ID string `xml:"ID,attr"`
	// Path 文件路径。
	Path StLoc `xml:",chardata"`
}
