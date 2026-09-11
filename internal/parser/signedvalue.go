package parser

import (
	"encoding/asn1"
	"errors"
	"fmt"
	"time"
)

// SignedValue 是 OFD SignedValue.dat 的解析结果。
// Raw 和 ASN1 保留完整原始内容，识别为 SES 后同时提供结构化字段。
type SignedValue struct {
	Raw    []byte
	ASN1   *ASN1Node
	Format string

	SES *SESSignedValue
}

// ASN1Node 是 SignedValue.dat 的通用 ASN.1 节点，用于保留标准结构之外的
// 厂商扩展，避免解析未知字段时丢失原始内容。
type ASN1Node struct {
	Class       int
	Tag         int
	Constructed bool
	Bytes       []byte
	FullBytes   []byte
	Children    []*ASN1Node
}

// SESSignedValue 是 OFD 和 GM/T 电子印章签名结构。
type SESSignedValue struct {
	TBS                TBSSign
	Certificate        []byte
	SignatureAlgorithm asn1.ObjectIdentifier
	Signature          BitString
}

// TBSSign 是电子印章的待签名结构。
type TBSSign struct {
	// Raw 是 TBS_Sign 的完整 DER 编码，是外层签名的待验证数据。
	Raw          []byte
	Version      int64
	Seal         SESeal
	SignTime     time.Time
	DataHash     BitString
	PropertyInfo string
	Extensions   []ExtensionData
}

// SESeal 是电子印章内部的签名对象。
type SESeal struct {
	// Raw 是 SESeal 的完整 DER 编码，是印章内部签名的待验证数据。
	Raw                []byte
	SealInfo           SESealInfo
	Certificate        []byte
	SignatureAlgorithm asn1.ObjectIdentifier
	Signature          BitString
}

// SESealInfo 是电子印章的主体信息。
type SESealInfo struct {
	// Raw 是 SES_Seal_Info 的完整 DER 编码，是印章内部签名的待验证数据。
	Raw        []byte
	Header     SESHeader
	ESID       string
	Property   SESProperty
	Picture    SESPicture
	Extensions []ExtensionData
}

// SESHeader 是电子印章的头信息。
type SESHeader struct {
	ID      string
	Version int64
	VID     string
}

// SESProperty 是电子印章的属性信息。
type SESProperty struct {
	Type              int64
	Name              string
	CertificateType   int64
	Certificates      [][]byte
	CertificateValues []ASN1Node
	CreateTime        time.Time
	ValidFrom         time.Time
	ValidTo           time.Time
}

// SESPicture 是电子印章的图像信息。
type SESPicture struct {
	Type   string
	Data   []byte
	Width  int64
	Height int64
}

// ExtensionData 是 ASN.1 扩展数据项。
type ExtensionData struct {
	OID      asn1.ObjectIdentifier
	Critical bool
	Value    []byte
}

// BitString 保存 BIT STRING 的有效字节和未使用位数。
type BitString struct {
	Bytes      []byte
	UnusedBits int
}

// ParseSignedValue 解析 SignedValue.dat。
// 合法但不是 SES 电子印章格式的 ASN.1 文件仍会返回结果，并通过 Format="ASN.1"
// 保留完整通用树；只有二进制不是完整 ASN.1 数据时才返回错误。
func ParseSignedValue(data []byte) (*SignedValue, error) {
	if len(data) == 0 {
		return nil, errors.New("SignedValue.dat 为空")
	}

	var root asn1.RawValue
	rest, err := asn1.Unmarshal(data, &root)
	if err != nil {
		return nil, fmt.Errorf("解析 SignedValue.dat ASN.1 失败: %w", err)
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("解析 SignedValue.dat 失败: 根节点后仍有 %d 字节", len(rest))
	}

	tree, err := buildASN1Node(root)
	if err != nil {
		return nil, fmt.Errorf("构建 SignedValue.dat ASN.1 树失败: %w", err)
	}
	result := &SignedValue{
		Raw:    append([]byte(nil), data...),
		ASN1:   tree,
		Format: "ASN.1",
	}

	if isSESSignatureRoot(root) {
		ses, err := parseSESSignedValue(root)
		if err != nil {
			return nil, fmt.Errorf("解析 SignedValue.dat SES 结构失败: %w", err)
		}
		result.Format = "SES"
		result.SES = ses
	}
	return result, nil
}

func isSESSignatureRoot(root asn1.RawValue) bool {
	children, err := signedValueSequence(root, "SignedValue.dat")
	if err != nil || len(children) != 4 {
		return false
	}
	return isUniversal(children[0], asn1.TagSequence) &&
		isUniversal(children[1], asn1.TagOctetString) &&
		isUniversal(children[2], asn1.TagOID) &&
		isUniversal(children[3], asn1.TagBitString)
}

func parseSESSignedValue(root asn1.RawValue) (*SESSignedValue, error) {
	children, err := signedValueSequence(root, "SES_Signature")
	if err != nil {
		return nil, err
	}
	if len(children) != 4 {
		return nil, fmt.Errorf("字段数量为 %d，期望 4", len(children))
	}

	tbs, err := parseTBSSign(children[0])
	if err != nil {
		return nil, err
	}
	certificate, err := signedValueOctets(children[1], "SES_Signature.Certificate")
	if err != nil {
		return nil, err
	}
	signatureAlgorithm, err := signedValueOID(children[2], "SES_Signature.SignatureAlgorithm")
	if err != nil {
		return nil, err
	}
	signature, err := signedValueBitString(children[3], "SES_Signature.Signature")
	if err != nil {
		return nil, err
	}
	return &SESSignedValue{
		TBS:                *tbs,
		Certificate:        certificate,
		SignatureAlgorithm: signatureAlgorithm,
		Signature:          signature,
	}, nil
}

func parseTBSSign(value asn1.RawValue) (*TBSSign, error) {
	children, err := signedValueSequence(value, "TBS_Sign")
	if err != nil {
		return nil, err
	}
	if len(children) < 5 || len(children) > 6 {
		return nil, fmt.Errorf("字段数量为 %d，期望 5 或 6", len(children))
	}

	version, err := signedValueInt(children[0], "TBS_Sign.Version")
	if err != nil {
		return nil, err
	}
	seal, err := parseSESeal(children[1])
	if err != nil {
		return nil, err
	}
	signTime, err := signedValueTime(children[2], "TBS_Sign.SignTime")
	if err != nil {
		return nil, err
	}
	dataHash, err := signedValueBitString(children[3], "TBS_Sign.DataHash")
	if err != nil {
		return nil, err
	}
	propertyInfo, err := signedValueString(children[4], asn1.TagIA5String, "TBS_Sign.PropertyInfo")
	if err != nil {
		return nil, err
	}

	var extensions []ExtensionData
	if len(children) == 6 {
		extensions, err = parseExtensions(children[5], "TBS_Sign.Extensions")
		if err != nil {
			return nil, err
		}
	}
	return &TBSSign{
		Raw:          cloneBytes(value.FullBytes),
		Version:      version,
		Seal:         *seal,
		SignTime:     signTime,
		DataHash:     dataHash,
		PropertyInfo: propertyInfo,
		Extensions:   extensions,
	}, nil
}

func parseSESeal(value asn1.RawValue) (*SESeal, error) {
	children, err := signedValueSequence(value, "SESeal")
	if err != nil {
		return nil, err
	}
	if len(children) != 4 {
		return nil, fmt.Errorf("字段数量为 %d，期望 4", len(children))
	}

	sealInfo, err := parseSESealInfo(children[0])
	if err != nil {
		return nil, err
	}
	certificate, err := signedValueOctets(children[1], "SESeal.Certificate")
	if err != nil {
		return nil, err
	}
	signatureAlgorithm, err := signedValueOID(children[2], "SESeal.SignatureAlgorithm")
	if err != nil {
		return nil, err
	}
	signature, err := signedValueBitString(children[3], "SESeal.Signature")
	if err != nil {
		return nil, err
	}
	return &SESeal{
		Raw:                cloneBytes(value.FullBytes),
		SealInfo:           *sealInfo,
		Certificate:        certificate,
		SignatureAlgorithm: signatureAlgorithm,
		Signature:          signature,
	}, nil
}

func parseSESealInfo(value asn1.RawValue) (*SESealInfo, error) {
	children, err := signedValueSequence(value, "SES_Seal_Info")
	if err != nil {
		return nil, err
	}
	if len(children) < 4 || len(children) > 5 {
		return nil, fmt.Errorf("字段数量为 %d，期望 4 或 5", len(children))
	}

	header, err := parseSESHeader(children[0])
	if err != nil {
		return nil, err
	}
	esID, err := signedValueString(children[1], asn1.TagIA5String, "SES_Seal_Info.ESID")
	if err != nil {
		return nil, err
	}
	property, err := parseSESProperty(children[2])
	if err != nil {
		return nil, err
	}
	picture, err := parseSESPicture(children[3])
	if err != nil {
		return nil, err
	}

	var extensions []ExtensionData
	if len(children) == 5 {
		extensions, err = parseExtensions(children[4], "SES_Seal_Info.Extensions")
		if err != nil {
			return nil, err
		}
	}
	return &SESealInfo{
		Raw:        cloneBytes(value.FullBytes),
		Header:     *header,
		ESID:       esID,
		Property:   *property,
		Picture:    *picture,
		Extensions: extensions,
	}, nil
}

func parseSESHeader(value asn1.RawValue) (*SESHeader, error) {
	children, err := signedValueSequence(value, "SES_Header")
	if err != nil {
		return nil, err
	}
	if len(children) != 3 {
		return nil, fmt.Errorf("字段数量为 %d，期望 3", len(children))
	}
	id, err := signedValueString(children[0], asn1.TagIA5String, "SES_Header.ID")
	if err != nil {
		return nil, err
	}
	version, err := signedValueInt(children[1], "SES_Header.Version")
	if err != nil {
		return nil, err
	}
	vid, err := signedValueString(children[2], asn1.TagIA5String, "SES_Header.VID")
	if err != nil {
		return nil, err
	}
	return &SESHeader{ID: id, Version: version, VID: vid}, nil
}

func parseSESProperty(value asn1.RawValue) (*SESProperty, error) {
	children, err := signedValueSequence(value, "SES_ESPropertyInfo")
	if err != nil {
		return nil, err
	}
	if len(children) != 7 {
		return nil, fmt.Errorf("字段数量为 %d，期望 7", len(children))
	}
	typ, err := signedValueInt(children[0], "SES_ESPropertyInfo.Type")
	if err != nil {
		return nil, err
	}
	name, err := signedValueString(children[1], asn1.TagUTF8String, "SES_ESPropertyInfo.Name")
	if err != nil {
		return nil, err
	}
	certificateType, err := signedValueInt(children[2], "SES_ESPropertyInfo.CertificateType")
	if err != nil {
		return nil, err
	}
	certificateValues, certificates, err := parseCertificateList(children[3], certificateType)
	if err != nil {
		return nil, err
	}
	createTime, err := signedValueTime(children[4], "SES_ESPropertyInfo.CreateTime")
	if err != nil {
		return nil, err
	}
	validFrom, err := signedValueTime(children[5], "SES_ESPropertyInfo.ValidFrom")
	if err != nil {
		return nil, err
	}
	validTo, err := signedValueTime(children[6], "SES_ESPropertyInfo.ValidTo")
	if err != nil {
		return nil, err
	}
	return &SESProperty{
		Type:              typ,
		Name:              name,
		CertificateType:   certificateType,
		Certificates:      certificates,
		CertificateValues: certificateValues,
		CreateTime:        createTime,
		ValidFrom:         validFrom,
		ValidTo:           validTo,
	}, nil
}

func parseCertificateList(value asn1.RawValue, certificateType int64) ([]ASN1Node, [][]byte, error) {
	children, err := signedValueSequence(value, "SES_ESPropertyInfo.CertList")
	if err != nil {
		return nil, nil, err
	}
	values := make([]ASN1Node, 0, len(children))
	certificates := make([][]byte, 0, len(children))
	for index, child := range children {
		node, err := buildASN1Node(child)
		if err != nil {
			return nil, nil, fmt.Errorf("证书列表第 %d 项: %w", index, err)
		}
		values = append(values, *node)
		if certificateType == 1 {
			certificate, err := signedValueOctets(child, fmt.Sprintf("证书列表第 %d 项", index))
			if err != nil {
				return nil, nil, err
			}
			certificates = append(certificates, certificate)
		}
	}
	return values, certificates, nil
}

func parseSESPicture(value asn1.RawValue) (*SESPicture, error) {
	children, err := signedValueSequence(value, "SES_ESPictrueInfo")
	if err != nil {
		return nil, err
	}
	if len(children) != 4 {
		return nil, fmt.Errorf("字段数量为 %d，期望 4", len(children))
	}
	typ, err := signedValueString(children[0], asn1.TagIA5String, "SES_ESPictrueInfo.Type")
	if err != nil {
		return nil, err
	}
	data, err := signedValueOctets(children[1], "SES_ESPictrueInfo.Data")
	if err != nil {
		return nil, err
	}
	width, err := signedValueInt(children[2], "SES_ESPictrueInfo.Width")
	if err != nil {
		return nil, err
	}
	height, err := signedValueInt(children[3], "SES_ESPictrueInfo.Height")
	if err != nil {
		return nil, err
	}
	return &SESPicture{Type: typ, Data: data, Width: width, Height: height}, nil
}

func parseExtensions(value asn1.RawValue, field string) ([]ExtensionData, error) {
	children, err := signedValueSequence(value, field)
	if err != nil {
		return nil, err
	}
	result := make([]ExtensionData, 0, len(children))
	for index, child := range children {
		fields, err := signedValueSequence(child, fmt.Sprintf("%s[%d]", field, index))
		if err != nil {
			return nil, err
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s[%d] 字段数量为 %d，期望 3", field, index, len(fields))
		}
		oid, err := signedValueOID(fields[0], fmt.Sprintf("%s[%d].OID", field, index))
		if err != nil {
			return nil, err
		}
		critical, err := signedValueBool(fields[1], fmt.Sprintf("%s[%d].Critical", field, index))
		if err != nil {
			return nil, err
		}
		data, err := signedValueOctets(fields[2], fmt.Sprintf("%s[%d].Value", field, index))
		if err != nil {
			return nil, err
		}
		result = append(result, ExtensionData{OID: oid, Critical: critical, Value: data})
	}
	return result, nil
}

func signedValueSequence(value asn1.RawValue, field string) ([]asn1.RawValue, error) {
	if !isUniversal(value, asn1.TagSequence) {
		return nil, fmt.Errorf("%s 不是 SEQUENCE", field)
	}
	var result []asn1.RawValue
	rest := value.Bytes
	for len(rest) > 0 {
		var child asn1.RawValue
		remaining, err := asn1.Unmarshal(rest, &child)
		if err != nil {
			return nil, fmt.Errorf("%s 子节点解析失败: %w", field, err)
		}
		if len(remaining) >= len(rest) {
			return nil, fmt.Errorf("%s 子节点长度无效", field)
		}
		result = append(result, child)
		rest = remaining
	}
	return result, nil
}

func signedValueInt(value asn1.RawValue, field string) (int64, error) {
	if !isUniversal(value, asn1.TagInteger) {
		return 0, fmt.Errorf("%s 不是 INTEGER", field)
	}
	var result int64
	if rest, err := asn1.Unmarshal(value.FullBytes, &result); err != nil || len(rest) != 0 {
		if err != nil {
			return 0, fmt.Errorf("%s 解析失败: %w", field, err)
		}
		return 0, fmt.Errorf("%s 存在尾随数据", field)
	}
	return result, nil
}

func signedValueBool(value asn1.RawValue, field string) (bool, error) {
	if !isUniversal(value, asn1.TagBoolean) {
		return false, fmt.Errorf("%s 不是 BOOLEAN", field)
	}
	var result bool
	if rest, err := asn1.Unmarshal(value.FullBytes, &result); err != nil || len(rest) != 0 {
		if err != nil {
			return false, fmt.Errorf("%s 解析失败: %w", field, err)
		}
		return false, fmt.Errorf("%s 存在尾随数据", field)
	}
	return result, nil
}

func signedValueOID(value asn1.RawValue, field string) (asn1.ObjectIdentifier, error) {
	if !isUniversal(value, asn1.TagOID) {
		return nil, fmt.Errorf("%s 不是 OBJECT IDENTIFIER", field)
	}
	var result asn1.ObjectIdentifier
	if rest, err := asn1.Unmarshal(value.FullBytes, &result); err != nil || len(rest) != 0 {
		if err != nil {
			return nil, fmt.Errorf("%s 解析失败: %w", field, err)
		}
		return nil, fmt.Errorf("%s 存在尾随数据", field)
	}
	return result, nil
}

func signedValueString(value asn1.RawValue, tag int, field string) (string, error) {
	if !isUniversal(value, tag) {
		return "", fmt.Errorf("%s 类型错误，期望 tag %d", field, tag)
	}
	var result string
	if rest, err := asn1.Unmarshal(value.FullBytes, &result); err != nil || len(rest) != 0 {
		if err != nil {
			return "", fmt.Errorf("%s 解析失败: %w", field, err)
		}
		return "", fmt.Errorf("%s 存在尾随数据", field)
	}
	return result, nil
}

func signedValueOctets(value asn1.RawValue, field string) ([]byte, error) {
	if !isUniversal(value, asn1.TagOctetString) {
		return nil, fmt.Errorf("%s 不是 OCTET STRING", field)
	}
	var result []byte
	if rest, err := asn1.Unmarshal(value.FullBytes, &result); err != nil || len(rest) != 0 {
		if err != nil {
			return nil, fmt.Errorf("%s 解析失败: %w", field, err)
		}
		return nil, fmt.Errorf("%s 存在尾随数据", field)
	}
	return append([]byte(nil), result...), nil
}

func signedValueBitString(value asn1.RawValue, field string) (BitString, error) {
	if !isUniversal(value, asn1.TagBitString) {
		return BitString{}, fmt.Errorf("%s 不是 BIT STRING", field)
	}
	var result asn1.BitString
	if rest, err := asn1.Unmarshal(value.FullBytes, &result); err != nil || len(rest) != 0 {
		if err != nil {
			return BitString{}, fmt.Errorf("%s 解析失败: %w", field, err)
		}
		return BitString{}, fmt.Errorf("%s 存在尾随数据", field)
	}
	return BitString{Bytes: append([]byte(nil), result.Bytes...), UnusedBits: len(result.Bytes)*8 - result.BitLength}, nil
}

func signedValueTime(value asn1.RawValue, field string) (time.Time, error) {
	if !isUniversal(value, asn1.TagGeneralizedTime) {
		return time.Time{}, fmt.Errorf("%s 不是 GeneralizedTime", field)
	}
	var result time.Time
	if rest, err := asn1.Unmarshal(value.FullBytes, &result); err != nil || len(rest) != 0 {
		if err != nil {
			return time.Time{}, fmt.Errorf("%s 解析失败: %w", field, err)
		}
		return time.Time{}, fmt.Errorf("%s 存在尾随数据", field)
	}
	return result, nil
}

func isUniversal(value asn1.RawValue, tag int) bool {
	return value.Class == asn1.ClassUniversal && value.Tag == tag
}

func buildASN1Node(value asn1.RawValue) (*ASN1Node, error) {
	node := &ASN1Node{
		Class:       value.Class,
		Tag:         value.Tag,
		Constructed: value.IsCompound,
		Bytes:       append([]byte(nil), value.Bytes...),
		FullBytes:   append([]byte(nil), value.FullBytes...),
	}
	if !value.IsCompound {
		return node, nil
	}
	rest := value.Bytes
	for len(rest) > 0 {
		var child asn1.RawValue
		remaining, err := asn1.Unmarshal(rest, &child)
		if err != nil {
			return nil, err
		}
		childNode, err := buildASN1Node(child)
		if err != nil {
			return nil, err
		}
		node.Children = append(node.Children, childNode)
		rest = remaining
	}
	return node, nil
}
