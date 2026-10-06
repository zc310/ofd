// Package ses 实现 GB/T 38540-2020 规定的 SES 电子印章数据格式。
//
// SEAL（印章）把印章信息（名称、证书、有效期、印章图片）和对它的 SM2 签名
// 打包成一份 DER 编码的 ASN.1 结构；它可以单独落盘（Seal.esl），也会被嵌入
// TBS_Sign / SignedValue.dat，成为 OFD 内嵌签名的一部分。
//
// 本包提供组装 SES 结构的入口，字段的 ASN.1 顺序与 GB/T 38540 严格一致，
// 修改结构定义必须保持标准的顺序与类型。
package ses

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math/big"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm3"
	gmx509 "github.com/emmansun/gmsm/smx509"
)

const (
	// HeaderID 是 SES_Header.ID 的固定值，GM/T 0031 规定为 "ES"。
	HeaderID = "ES"
	// SealVersion 是 SES 结构版本号；与 OFD 规范配套的版本是 V4。
	SealVersion = 4
	// DefaultValidity 是证书/印章的默认有效期。
	DefaultValidity = 10 * 365 * 24 * time.Hour
)

// algorithmOID 是 SM2 + SM3 的签名算法 OID，OID 写死在 ASN.1 结构里。
var algorithmOID = asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 501}

// 以下类型的字段顺序与 GB/T 38540 一一对应，DER 编码时按此顺序输出。

type Header struct {
	ID      string `asn1:"ia5"`
	Version int
	VID     string `asn1:"ia5"`
}

type Property struct {
	Type            int
	Name            string
	CertificateType int
	CertList        [][]byte
	CreateTime      time.Time `asn1:"generalized"`
	ValidFrom       time.Time `asn1:"generalized"`
	ValidTo         time.Time `asn1:"generalized"`
}

type Picture struct {
	Type   string `asn1:"ia5"`
	Data   []byte
	Width  int
	Height int
}

type SealInfo struct {
	Header   Header
	ESID     string `asn1:"ia5"`
	Property Property
	Picture  Picture
}

// Seal 是 GB/T 38540 定义的 SES_Seal。Certificate 与 SignatureAlgorithm 是对
// SealInfo 的 SM2 签名（SM2WithSM3）。
type Seal struct {
	SealInfo           SealInfo
	Certificate        []byte
	SignatureAlgorithm asn1.ObjectIdentifier
	Signature          asn1.BitString
}

type TBSSign struct {
	Version      int
	Seal         Seal
	SignTime     time.Time `asn1:"generalized"`
	DataHash     asn1.BitString
	PropertyInfo string `asn1:"ia5"`
}

// SignedValue 对应 OFD SignedValue.dat 的外层结构（SES_Signature）。
type SignedValue struct {
	TBS                TBSSign
	Certificate        []byte
	SignatureAlgorithm asn1.ObjectIdentifier
	Signature          asn1.BitString
}

// SealParams 描述一枚待生成的电子印章。
type SealParams struct {
	// Provider 写入 SES_Header.VID。
	Provider string
	// ESID 是印章的全局唯一编号，通常使用 email 形态的标识。
	ESID string
	// Name 写入 SES_ESPropertyInfo.Name。
	Name string
	// PictureType 是印章图片格式，取自 image.DecodeConfig 的格式名（png、jpeg、gif、bmp）。
	PictureType string
	// PictureData 是印章图片的原始字节。
	PictureData []byte
	// Width、Height 是印章图片的宽高，必须与 PictureData 的实际尺寸一致。
	Width  int
	Height int
	// ValidFrom、ValidTo 是印章的有效期；Max 为零值时使用一年有效期。
	ValidFrom time.Time
	ValidTo   time.Time
}

// BuildSeal 组装 SES_Seal_Info 并对它签名，产出符合 GB/T 38540 的 SES_Seal。
// certDER 必须是 privateKey 对应证书的 DER 编码。
func BuildSeal(params SealParams, certDER []byte, key *sm2.PrivateKey, now time.Time) (*Seal, error) {
	if params.Provider == "" {
		return nil, fmt.Errorf("SealParams.Provider 不能为空")
	}
	if params.ESID == "" {
		return nil, fmt.Errorf("SealParams.ESID 不能为空")
	}
	// VID 与 ESID 在 GB/T 38540 里是 IA5String。放任非 ASCII 值进去，asn1 会
	// 在编码阶段报 "IA5String contains invalid character"，看不出是哪个字段。
	if !IsIA5(params.Provider) {
		return nil, fmt.Errorf("SealParams.Provider 必须是 IA5 字符串（不能含非 ASCII 字符），实际 %q", params.Provider)
	}
	if !IsIA5(params.ESID) {
		return nil, fmt.Errorf("SealParams.ESID 必须是 IA5 字符串（不能含非 ASCII 字符），实际 %q", params.ESID)
	}
	if params.PictureType == "" || len(params.PictureData) == 0 {
		return nil, fmt.Errorf("SealParams 必须携带印章图片")
	}
	validFrom := params.ValidFrom
	validTo := params.ValidTo
	if validFrom.IsZero() {
		validFrom = now
	}
	if validTo.IsZero() {
		validTo = validFrom.Add(10 * 365 * 24 * time.Hour)
	}
	// 提前校验证书可解析，避免一个无效证书在结构组装完之后才失败。
	if _, err := gmx509.ParseCertificate(certDER); err != nil {
		return nil, fmt.Errorf("解析证书 DER 失败: %w", err)
	}
	info := SealInfo{
		Header: Header{ID: HeaderID, Version: SealVersion, VID: params.Provider},
		ESID:   params.ESID,
		Property: Property{
			Type:            1,
			Name:            params.Name,
			CertificateType: 1,
			CertList:        [][]byte{certDER},
			CreateTime:      now,
			ValidFrom:       validFrom,
			ValidTo:         validTo,
		},
		Picture: Picture{Type: params.PictureType, Data: params.PictureData, Width: params.Width, Height: params.Height},
	}
	infoDER, err := asn1.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("编码 SES_Seal_Info 失败: %w", err)
	}
	publicKey := &key.PublicKey
	signature, err := signSM2(publicKey, key, infoDER)
	if err != nil {
		return nil, fmt.Errorf("签署 SES_Seal_Info 失败: %w", err)
	}
	return &Seal{
		SealInfo:           info,
		Certificate:        certDER,
		SignatureAlgorithm: algorithmOID,
		Signature:          signature,
	}, nil
}

// MarshalDER 输出 SES_Seal 的 DER 编码，即 Seal.esl 的文件内容。
func (s *Seal) MarshalDER() ([]byte, error) {
	return asn1.Marshal(*s)
}

// BuildSignedValue 以 TBS_Sign 为待签名数据，生成 SignedValue.dat。
// signatureXML 的 SM3 摘要写入 TBS_Sign.DataHash，propertyInfo 是签名 XML 的
// 包内路径（例如 "/Doc_0/Signatures/Signature_sign-1.xml"）。
func BuildSignedValue(signatureXML []byte, seal *Seal, certDER []byte, key *sm2.PrivateKey, propertyInfo string, now time.Time) ([]byte, error) {
	if seal == nil {
		return nil, fmt.Errorf("seal 不能为空")
	}
	publicKey := &key.PublicKey
	dataHash := sm3.Sum(signatureXML)
	tbs := TBSSign{
		Version:      SealVersion,
		Seal:         *seal,
		SignTime:     now,
		DataHash:     asn1.BitString{Bytes: dataHash[:], BitLength: len(dataHash) * 8},
		PropertyInfo: propertyInfo,
	}
	tbsDER, err := asn1.Marshal(tbs)
	if err != nil {
		return nil, fmt.Errorf("编码 TBS_Sign 失败: %w", err)
	}
	signature, err := signSM2(publicKey, key, tbsDER)
	if err != nil {
		return nil, fmt.Errorf("签署 TBS_Sign 失败: %w", err)
	}
	signedValue := SignedValue{
		TBS:                tbs,
		Certificate:        certDER,
		SignatureAlgorithm: algorithmOID,
		Signature:          signature,
	}
	out, err := asn1.Marshal(signedValue)
	if err != nil {
		return nil, fmt.Errorf("编码 SignedValue.dat 失败: %w", err)
	}
	return out, nil
}

// NewSelfSignedCertificate 生成 SM2 自签名证书，返回私钥、公钥与证书 DER。
// 仅用于演示、测试与本地评估：自签证书不建立可验证的信任链，生产环境应导入
// 有合法信任链的证书与私钥。
func NewSelfSignedCertificate(commonName, organization string, now time.Time) (*sm2.PrivateKey, *ecdsa.PublicKey, []byte, error) {
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("生成 SM2 私钥失败: %w", err)
	}
	publicKey := &key.PublicKey
	name := pkixName(commonName, organization)
	template := &gmx509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               name,
		Issuer:                name,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              gmx509.KeyUsageDigitalSignature | gmx509.KeyUsageCertSign,
		SignatureAlgorithm:    gmx509.SM2WithSM3,
	}
	certDER, err := gmx509.CreateCertificate(rand.Reader, template, template, publicKey, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("签发演示证书失败: %w", err)
	}
	return key, publicKey, certDER, nil
}

// PictureTypeAndSize 返回印章图片的格式名与宽高。
// 格式名来自 image.DecodeConfig（"png"、"jpeg"、"gif"、"bmp"），可直接用于
// SES_ESPictrueInfo.Type。
func PictureTypeAndSize(data []byte) (string, int, int, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, fmt.Errorf("解析图片尺寸失败（仅支持 png/jpeg/gif/bmp）: %w", err)
	}
	return format, config.Width, config.Height, nil
}

// ParsePrivateKey 解析 SM2 私钥，支持 PKCS#8 的 PEM 或 DER 编码。
func ParsePrivateKey(data []byte) (*sm2.PrivateKey, error) {
	der := data
	if block, _ := pem.Decode(data); block != nil {
		der = block.Bytes
	}
	key, err := gmx509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}
	sm2Key, ok := key.(*sm2.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("私钥不是 SM2 类型: %T", key)
	}
	return sm2Key, nil
}

// MarshalPrivateKeyPEM 输出 PKCS#8 私钥的 PEM 编码。
func MarshalPrivateKeyPEM(key *sm2.PrivateKey) ([]byte, error) {
	der, err := gmx509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("编码私钥失败: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// ParseCertificateDER 解析证书，支持 PEM 或 DER，统一返回 DER 字节。
func ParseCertificateDER(data []byte) ([]byte, error) {
	if block, _ := pem.Decode(data); block != nil {
		return block.Bytes, nil
	}
	return data, nil
}

// IsIA5 判断字符串是否只含 IA5（ASCII）字符。
//
// SES_Header.VID、ESID 等字段在 GB/T 38540 里定义为 IA5String。CLI 层可以在
// 生成证书之前就用它给出友好提示，BuildSeal 内部也会再校验一次。
func IsIA5(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func pkixName(commonName, organization string) pkix.Name {
	return pkix.Name{CommonName: commonName, Organization: []string{organization}}
}

func signSM2(publicKey *ecdsa.PublicKey, key *sm2.PrivateKey, data []byte) (asn1.BitString, error) {
	digest, err := sm2.CalculateSM2Hash(publicKey, data, nil)
	if err != nil {
		return asn1.BitString{}, err
	}
	r, s, err := sm2.Sign(rand.Reader, &key.PrivateKey, digest)
	if err != nil {
		return asn1.BitString{}, err
	}
	signature, err := asn1.Marshal(struct {
		R *big.Int
		S *big.Int
	}{R: r, S: s})
	if err != nil {
		return asn1.BitString{}, err
	}
	return asn1.BitString{Bytes: signature, BitLength: len(signature) * 8}, nil
}
