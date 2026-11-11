package spec

import "encoding/xml"

// IntegrityFile 是 GM/T 0099—2020 附录 D.2 表D.1的物理文件声明。
// ID 保留 xs:ID 字符串，Location 保留原始包内路径，不加载文件。
type IntegrityFile struct {
	ID       string `xml:"ID,attr" json:"id"` //nolint:tagliatelle // 标准属性名为 ID。
	Location string `xml:"FileLoc,attr" json:"location"`
}

// IntegrityManifest 是附录 D.2 的完整性保护描述，不含遍历、验签或信任结论。
// XMLName 保留图D.1的 OFDEntries 或表D.1的 DocEntries，不在声明层校验根名。
// JSON 标签沿用 pkg/integrity 的已发布报告格式；原始 XML 解码不会规范化文件路径。
type IntegrityManifest struct {
	XMLName        xml.Name        `xml:"" json:"-"`         //nolint:tagliatelle // 不指定根名，以保留标准图表中的两种拼写。
	ID             string          `xml:"ID,attr" json:"id"` //nolint:tagliatelle // 标准属性名为 ID。
	CreatorName    string          `xml:"CreatorName,attr,omitempty" json:"creator_name,omitempty"`
	Version        string          `xml:"Version,attr,omitempty" json:"version,omitempty"`
	CreationDate   string          `xml:"CreationDate,attr,omitempty" json:"creation_date,omitempty"`
	Files          []IntegrityFile `xml:"FileList>File" json:"files"`
	SignedValueLoc string          `xml:"SignedValueLoc" json:"signed_value_location,omitempty"`
}
