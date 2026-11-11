package spec

// Signature 是 GB/T 33190—2016 §18.2 表67的签名描述，不包含索引属性和加载路径。
// 解码只保存声明，不读取签名值、不计算摘要，也不验证数字签名。
type Signature struct {
	// ID 保留部分生产者在签名描述中重复写入的标识；标准标识位于签名索引中。
	ID          string     `xml:"ID,attr,omitempty"` //nolint:tagliatelle // OFD 标准属性名为 ID。
	SignedInfo  SignedInfo `xml:"SignedInfo"`
	SignedValue StLoc      `xml:"SignedValue"`
}

// SignedInfo 保存受签名保护的文件范围和签章组件信息。
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

// SignatureParameter 是签名的扩展键值属性。
type SignatureParameter struct {
	Name  string `xml:"Name,attr"`
	Type  string `xml:"Type,attr,omitempty"`
	Value string `xml:",chardata"`
}

// Provider 保存签章组件信息，ProtocolVer 和 ExtendData 为 GM/T 0099 扩展。
type Provider struct {
	ProviderName string `xml:"ProviderName,attr"`
	Version      string `xml:"Version,attr,omitempty"`
	Company      string `xml:"Company,attr,omitempty"`
	ProtocolVer  string `xml:"ProtocolVer,attr,omitempty"`
	// ExtendData 按 GM/T 0099 图 B.2 写在 Provider 的子元素中。
	ExtendData string `xml:"ExtendData,omitempty"`
}

// References 是 §18.2.2 表68的文件摘要列表，不加载引用文件。
type References struct {
	CheckMethod string      `xml:"CheckMethod,attr,omitempty"` // MD5、SHA1、SHA256、SHA384、SHA512、SM3 或 SM3 OID
	Reference   []Reference `xml:"Reference"`
}

// Reference 保存原始文件引用和摘要文本。
type Reference struct {
	FileRef StLoc `xml:"FileRef,attr"`
	// CheckValue 沿用读取链的表示，保存 Base64 文本字节，而非解码后的二进制摘要。
	CheckValue []byte `xml:"CheckValue"`
}

// StampAnnot 是 §18.2.3 表69的签名外观定位，不加载印章图像。
type StampAnnot struct {
	ID       string  `xml:"ID,attr"` //nolint:tagliatelle // 标准使用 xs:ID，不能映射为数字 ST_ID。
	PageRef  StRefID `xml:"PageRef,attr"`
	Boundary StBox   `xml:"Boundary,attr"`
	// Clip 是印章图上的裁剪窗口，坐标相对 Boundary 左上角、且不得超过
	// Boundary：只显示印章落在该窗口内的部分，贴在 Boundary 左上角加上
	// Clip 偏移处。骑缝章用它把同一枚印章按页切成若干条，每页用不同的 Clip
	// 显示其中一片。缺省（全零）表示显示整枚印章。
	Clip StBox `xml:"Clip,attr,omitempty"`
}

// Seal 保存电子印章数据路径及 GM/T 0099 的图像路径扩展。
type Seal struct {
	BaseLoc StLoc `xml:"BaseLoc,omitempty"`
	// ImageLoc 按图 B.2 的 ST_Loc 解释；表 B.2 的 ST_Box 与“图片存储位置”文字矛盾。
	ImageLoc StLoc `xml:"ImageLoc,omitempty"`
}
