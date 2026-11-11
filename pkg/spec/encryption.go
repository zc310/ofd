package spec

import "encoding/xml"

// Encryptions 是 GM/T 0099—2020 附录 C.3 表C.2的解密入口声明。
// 本模型不检查操作依赖、不读取控制文件，也不执行加解密。
type Encryptions struct {
	XMLName    xml.Name      `xml:"Encryptions"`
	Operations []EncryptInfo `xml:"EncryptInfo"`
}

// EncryptInfo 保存一次加密操作的原始声明，ID 和 Relative 使用 xs:ID/IDREF 字符串。
// 路径和日期文本保持原样，不执行路径规范化或根据 Scope 推断加密范围。
type EncryptInfo struct {
	ID             string                `xml:"ID,attr"` //nolint:tagliatelle // 标准属性名为 ID。
	Relative       string                `xml:"Relative,attr"`
	Provider       EncryptionProvider    `xml:"Provider"`
	Scope          string                `xml:"EncryptScope"`
	Parameters     *EncryptionParameters `xml:"Parameters"`
	EncryptDate    string                `xml:"EncryptDate"`
	DecryptSeedLoc string                `xml:"DecryptSeedLoc"`
	EntriesMapLoc  string                `xml:"EntriesMapLoc"`
}

// EncryptionProvider 是表C.2的加解密组件声明，与签名组件 Provider 的属性名不同。
type EncryptionProvider struct {
	Name        string `xml:"Name,attr"`
	Company     string `xml:"Company,attr"`
	Version     string `xml:"Version,attr"`
	ProtocolVer string `xml:"ProtocolVer,attr"`
	// ExtendData 保存 Base64 文本，不解码为二进制。
	ExtendData string `xml:"ExtendData"`
}

// EncryptionParameters 保存加密操作的附加描述集合。
type EncryptionParameters struct {
	Items []EncryptionParameter `xml:"Parameter"`
}

// EncryptionParameter 保存附加描述的名称、可选类型及字符值，不解释厂商类型。
type EncryptionParameter struct {
	Name  string `xml:"Name,attr"`
	Type  string `xml:"Type,attr"`
	Value string `xml:",chardata"`
}

// DecryptSeed 是附录 C.2 表C.1的密钥描述，XML 根名沿用标准拼写 DecyptSeed。
// Scheme 保存 EncryptCaseId 原文，不推断方案；ExtendParams 原样保留扩展子内容。
type DecryptSeed struct {
	XMLName      xml.Name         `xml:"DecyptSeed"`
	ID           string           `xml:"ID,attr"` //nolint:tagliatelle // 标准属性名为 ID。
	Scheme       string           `xml:"EncryptCaseId,attr"`
	Users        []EncryptionUser `xml:"UserInfo"`
	ExtendParams *ExtensionData   `xml:"ExtendParams"`
}

// EncryptionUser 保存接收者原始声明，不执行 UserType 或 IVValue 的默认值填充。
// Certificate、WrappedKey 和 IV 均为 Base64 文本；解析不验证证书、不解包密钥。
type EncryptionUser struct {
	Name        string `xml:"UserName,attr"`
	Type        string `xml:"UserType,attr"`
	Certificate string `xml:"UserCert"`
	WrappedKey  string `xml:"EncryptedWK"` //nolint:tagliatelle // 标准元素名为 EncryptedWK。
	// IV 为 nil 表示省略；显式空 IVValue 保持非 nil 空字符串，留给检查器拒绝。
	IV *string `xml:"IVValue"` //nolint:tagliatelle // 标准元素名为 IVValue。
}

// EncryptedEntries 是附录 C.4 表C.3的明密文映射表。
// XMLName 保留实际根名，以兼容标准图C.3与表C.3中的 EncryptEntries/EncryptedEntries。
// 具体根名、命名空间和路径的安全检查仍由加密读取链执行。
type EncryptedEntries struct {
	XMLName xml.Name       `xml:""`        //nolint:tagliatelle // 不指定根名，以保留标准图表中的两种拼写。
	ID      string         `xml:"ID,attr"` //nolint:tagliatelle // 标准属性名为 ID。
	Entries []EncryptEntry `xml:"EncryptEntry"`
}

// EncryptEntry 保存明密文包内路径和可选独立密钥描述路径，不补全缺省 EPath 或 Seed。
type EncryptEntry struct {
	Path  string `xml:"Path,attr"`
	EPath string `xml:"EPath,attr"`
	Seed  string `xml:"DecryptSeedLoc,attr"`
}
