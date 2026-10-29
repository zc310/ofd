package models

type Signature struct {
	ID          string     `xml:"ID,attr,omitempty"`
	Type        string     `xml:"-"`
	Relative    string     `xml:"-"`
	FilePath    string     `xml:"-"`
	SignedInfo  SignedInfo `xml:"SignedInfo"`
	SignedValue StLoc      `xml:"SignedValue"`
}

type SignedInfo struct {
	Provider          Provider             `xml:"Provider"`
	SignatureMethod   string               `xml:"SignatureMethod,omitempty"`
	SignatureDateTime string               `xml:"SignatureDateTime,omitempty"`
	Parameters        *SignatureParameters `xml:"Parameters,omitempty"`
	References        References           `xml:"References"`
	StampAnnot        []*StampAnnot        `xml:"StampAnnot,omitempty"`
	Seal              *Seal                `xml:"Seal,omitempty"`
}

// SignatureParameters 对应 GM/T 0099 表 B.2 的扩展属性集。
type SignatureParameters struct {
	Items []SignatureParameter `xml:"Parameter"`
}

type SignatureParameter struct {
	Name  string `xml:"Name,attr"`
	Type  string `xml:"Type,attr,omitempty"`
	Value string `xml:",chardata"`
}

type Provider struct {
	ProviderName string `xml:"ProviderName,attr"`
	Version      string `xml:"Version,attr,omitempty"`
	Company      string `xml:"Company,attr,omitempty"`
	ProtocolVer  string `xml:"ProtocolVer,attr,omitempty"`
	// ExtendData 按 GM/T 0099 图 B.2 写在 Provider 的子元素中。
	ExtendData string `xml:"ExtendData,omitempty"`
}

type References struct {
	CheckMethod string      `xml:"CheckMethod,attr,omitempty"` // MD5、SHA1、SHA256、SHA384、SHA512、SM3 或 SM3 OID
	Reference   []Reference `xml:"Reference"`
}

type Reference struct {
	FileRef    StLoc  `xml:"FileRef,attr"`
	CheckValue []byte `xml:"CheckValue"`
}

type StampAnnot struct {
	ID       string  `xml:"ID,attr"` // xs:ID 类型
	PageRef  StRefID `xml:"PageRef,attr"`
	Boundary StBox   `xml:"Boundary,attr"`
	// Clip 是印章图上的裁剪窗口，坐标相对 Boundary 左上角、且不得超过
	// Boundary：只显示印章落在该窗口内的部分，贴在 Boundary 左上角加上
	// Clip 偏移处。骑缝章用它把同一枚印章按页切成若干条，每页用不同的 Clip
	// 显示其中一片。缺省（全零）表示显示整枚印章。
	Clip StBox `xml:"Clip,attr,omitempty"`
}

type Seal struct {
	BaseLoc StLoc `xml:"BaseLoc,omitempty"`
	// ImageLoc 按图 B.2 的 ST_Loc 解释；表 B.2 的 ST_Box 与“图片存储位置”文字矛盾。
	ImageLoc StLoc `xml:"ImageLoc,omitempty"`
}
