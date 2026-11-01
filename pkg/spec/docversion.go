package spec

import (
	"encoding/xml"
)

// DocVersion 是 GB/T 33190—2016 §19.2 表71的文档版本信息，不负责版本加载。
type DocVersion struct {
	// XMLName 文档版本根元素名称。
	XMLName xml.Name `xml:"DocVersion"`
	// Xmlns 文档版本命名空间地址。
	Xmlns string `xml:"xmlns,attr"` //nolint:tagliatelle // XML命名空间声明保留小写xmlns。
	// ID 文档版本标识。
	ID string `xml:"ID,attr"` //nolint:tagliatelle // OFD标准属性名为大写ID。
	// Version 文档版本号。
	Version *string `xml:"Version,attr,omitempty"`
	// Name 文档版本名称。
	Name *string `xml:"Name,attr,omitempty"`
	// CreationDate 文档版本创建时间。
	CreationDate *DateTime `xml:"CreationDate,attr,omitempty"`
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
	ID string `xml:"ID,attr"` //nolint:tagliatelle // OFD标准属性名为大写ID。
	// Path 文件路径。
	Path StLoc `xml:",chardata"`
}
