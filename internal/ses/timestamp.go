package ses

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/emmansun/gmsm/pkcs7"
	"github.com/emmansun/gmsm/sm3"
	"github.com/emmansun/gmsm/smx509"
)

var tstInfoOID = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}

// TimestampProof 是时间戳令牌的数学、摘要绑定与用途验证结果。
// 证书信任链及吊销状态必须由调用方按 Time 进一步验证。
type TimestampProof struct {
	Time         time.Time
	Signer       *smx509.Certificate
	Certificates []*smx509.Certificate
}

// CheckTimestamp 校验 DER TimeStampToken 对签名值的绑定（GB/T 38540 §7.1.6、
// §7.3 h；GB/T 20520 / RFC 3161 的 CMS SignedData + TSTInfo）。
func CheckTimestamp(token, signature []byte) (*TimestampProof, error) {
	var envelope struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue `asn1:"explicit,tag:0"`
	}
	rest, err := asn1.Unmarshal(token, &envelope)
	if err != nil || len(rest) != 0 || (!envelope.Type.Equal(pkcs7.OIDSignedData) && !envelope.Type.Equal(pkcs7.SM2OIDSignedData)) {
		return nil, fmt.Errorf("时间戳不是完整的 CMS SignedData")
	}
	var signedData struct {
		Version    int
		Algorithms []pkix.AlgorithmIdentifier `asn1:"set"`
		Content    struct {
			Type asn1.ObjectIdentifier
			Data asn1.RawValue `asn1:"explicit,tag:0"`
		}
	}
	if _, err := asn1.Unmarshal(envelope.Content.Bytes, &signedData); err != nil || !signedData.Content.Type.Equal(tstInfoOID) {
		return nil, fmt.Errorf("时间戳 CMS 内容类型不是 TSTInfo")
	}
	p7, err := pkcs7.Parse(token)
	if err != nil {
		return nil, fmt.Errorf("解析时间戳失败: %w", err)
	}
	certificate := p7.GetOnlySigner()
	if certificate == nil {
		return nil, fmt.Errorf("时间戳必须包含唯一的签名者证书")
	}
	// CMS 实现同时使用 SM2 算法标识与组合签名标识；gmsm 的 CMS 分派仅
	// 接受前者。只对明确的 SM2-with-SM3 + SM3 组合做等价归一化。
	if p7.Signers[0].DigestEncryptionAlgorithm.Algorithm.Equal(algorithmOID) {
		if !p7.Signers[0].DigestAlgorithm.Algorithm.Equal(pkcs7.OIDDigestAlgorithmSM3) {
			return nil, fmt.Errorf("时间戳 SM2-with-SM3 与摘要算法不一致")
		}
		p7.Signers[0].DigestEncryptionAlgorithm.Algorithm = pkcs7.OIDDigestEncryptionAlgorithmSM2
	}
	var contentType asn1.ObjectIdentifier
	if err := p7.UnmarshalSignedAttribute(pkcs7.OIDAttributeContentType, &contentType); err != nil || !contentType.Equal(tstInfoOID) {
		return nil, fmt.Errorf("时间戳签名属性未绑定 TSTInfo 内容类型")
	}
	for _, oid := range []asn1.ObjectIdentifier{pkcs7.OIDAttributeContentType, pkcs7.OIDAttributeMessageDigest} {
		count := 0
		for _, attribute := range p7.Signers[0].AuthenticatedAttributes {
			if attribute.Type.Equal(oid) {
				count++
			}
		}
		if count != 1 {
			return nil, fmt.Errorf("时间戳必需签名属性缺失或重复")
		}
	}
	if err := p7.Verify(); err != nil {
		return nil, fmt.Errorf("时间戳签名验证失败: %w", err)
	}
	if err := checkTimestampCertificateBinding(p7, certificate); err != nil {
		return nil, err
	}
	var info struct {
		Version int
		Policy  asn1.ObjectIdentifier
		Imprint struct {
			Algorithm pkix.AlgorithmIdentifier
			Hash      []byte
		}
		Serial *big.Int
		Time   time.Time `asn1:"generalized"`
	}
	// 只提取五个必需字段，另外保留和验证可选域。
	var root asn1.RawValue
	rest, err = asn1.Unmarshal(p7.Content, &root)
	if err != nil || len(rest) != 0 || root.Tag != asn1.TagSequence || root.Class != asn1.ClassUniversal {
		return nil, fmt.Errorf("时间戳 TSTInfo 编码无效")
	}
	var fields []asn1.RawValue
	remaining := root.Bytes
	for len(remaining) > 0 {
		var field asn1.RawValue
		remaining, err = asn1.Unmarshal(remaining, &field)
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
	if len(fields) < 5 {
		return nil, fmt.Errorf("时间戳 TSTInfo 缺少必需字段")
	}
	if _, err := asn1.Unmarshal(p7.Content, &info); err != nil || info.Version != 1 || info.Serial == nil || info.Serial.Sign() <= 0 || info.Time.IsZero() || len(info.Policy) == 0 {
		return nil, fmt.Errorf("时间戳 TSTInfo 字段无效")
	}
	last := 0
	for _, field := range fields[5:] {
		rank := 0
		switch {
		case field.Class == asn1.ClassUniversal && field.Tag == asn1.TagSequence:
			rank = 1
			if err := checkTimestampAccuracy(field.Bytes); err != nil {
				return nil, err
			}
		case field.Class == asn1.ClassUniversal && field.Tag == asn1.TagBoolean:
			rank = 2
			var ordering bool
			if _, err := asn1.Unmarshal(field.FullBytes, &ordering); err != nil {
				return nil, err
			}
		case field.Class == asn1.ClassUniversal && field.Tag == asn1.TagInteger:
			rank = 3
			var nonce *big.Int
			if _, err := asn1.Unmarshal(field.FullBytes, &nonce); err != nil || nonce == nil || nonce.Sign() < 0 {
				return nil, fmt.Errorf("时间戳 nonce 无效")
			}
		case field.Class == asn1.ClassContextSpecific && field.Tag == 0:
			rank = 4
			var name asn1.RawValue
			rest, err := asn1.Unmarshal(field.Bytes, &name)
			if err != nil || len(rest) != 0 || name.Class != asn1.ClassContextSpecific || name.Tag < 0 || name.Tag > 8 {
				return nil, fmt.Errorf("时间戳 TSA 名称编码无效")
			}
			if !timestampNameMatches(name, certificate) {
				return nil, fmt.Errorf("时间戳 TSA 名称与签名者证书不匹配或名称类型未支持")
			}
		case field.Class == asn1.ClassContextSpecific && field.Tag == 1:
			rank = 5
		}
		if rank == 0 || rank <= last {
			return nil, fmt.Errorf("时间戳可选字段类型、顺序或数量无效")
		}
		last = rank
		if field.Class == asn1.ClassContextSpecific && field.Tag == 1 {
			var extensions []pkix.Extension
			if _, err := asn1.UnmarshalWithParams(field.FullBytes, &extensions, "tag:1"); err != nil {
				return nil, fmt.Errorf("时间戳扩展编码无效: %w", err)
			}
			for _, extension := range extensions {
				if extension.Critical {
					return nil, fmt.Errorf("未支持的关键时间戳扩展 %s", extension.Id)
				}
			}
		}
	}
	var digest []byte
	switch info.Imprint.Algorithm.Algorithm.String() {
	case "1.2.156.10197.1.401":
		sum := sm3.Sum(signature)
		digest = sum[:]
	case "2.16.840.1.101.3.4.2.1":
		sum := sha256.Sum256(signature)
		digest = sum[:]
	case "2.16.840.1.101.3.4.2.2":
		sum := sha512.Sum384(signature)
		digest = sum[:]
	case "2.16.840.1.101.3.4.2.3":
		sum := sha512.Sum512(signature)
		digest = sum[:]
	default:
		return nil, fmt.Errorf("未支持的时间戳消息杂凑算法 %s", info.Imprint.Algorithm.Algorithm)
	}
	if !bytes.Equal(digest, info.Imprint.Hash) {
		return nil, fmt.Errorf("时间戳消息杂凑与签名值不匹配")
	}
	if len(certificate.ExtKeyUsage) != 1 || certificate.ExtKeyUsage[0] != smx509.ExtKeyUsageTimeStamping || len(certificate.UnknownExtKeyUsage) != 0 {
		return nil, fmt.Errorf("时间戳证书须专用于时间戳签名")
	}
	criticalEKU := false
	for _, extension := range certificate.Extensions {
		if extension.Id.Equal(asn1.ObjectIdentifier{2, 5, 29, 37}) {
			criticalEKU = extension.Critical
		}
	}
	if !criticalEKU {
		return nil, fmt.Errorf("时间戳证书的扩展密钥用途须标记为关键")
	}
	if err := ValidateSigningCertificate(certificate, info.Time); err != nil {
		return nil, err
	}
	return &TimestampProof{Time: info.Time, Signer: certificate, Certificates: p7.Certificates}, nil
}

// RFC 3161 §2.4.2 / RFC 5035 §5：首个 ESSCertID 绑定实际签名者证书，
// 防止同一公钥的其他证书被替换进令牌。
func checkTimestampCertificateBinding(p7 *pkcs7.PKCS7, certificate *smx509.Certificate) error {
	count := 0
	for _, attribute := range p7.Signers[0].AuthenticatedAttributes {
		version := 0
		switch attribute.Type.String() {
		case "1.2.840.113549.1.9.16.2.12":
			version = 1
		case "1.2.840.113549.1.9.16.2.47":
			version = 2
		default:
			continue
		}
		count++
		var binding struct {
			Certificates []asn1.RawValue
			Policies     asn1.RawValue `asn1:"optional"`
		}
		if err := p7.UnmarshalSignedAttribute(attribute.Type, &binding); err != nil || len(binding.Certificates) == 0 {
			return fmt.Errorf("时间戳签名者证书绑定属性无效")
		}
		var id struct {
			Algorithm    pkix.AlgorithmIdentifier `asn1:"optional"`
			Hash         []byte
			IssuerSerial asn1.RawValue `asn1:"optional"`
		}
		if _, err := asn1.Unmarshal(binding.Certificates[0].FullBytes, &id); err != nil {
			return fmt.Errorf("时间戳签名者证书摘要编码无效: %w", err)
		}
		algorithm := id.Algorithm.Algorithm.String()
		if version == 1 {
			if len(id.Algorithm.Algorithm) != 0 {
				return fmt.Errorf("ESSCertID 不允许显式摘要算法")
			}
			algorithm = "1.3.14.3.2.26"
		} else if len(id.Algorithm.Algorithm) == 0 {
			algorithm = "2.16.840.1.101.3.4.2.1"
		}
		var digest []byte
		switch algorithm {
		case "1.3.14.3.2.26":
			sum := sha1.Sum(certificate.Raw)
			digest = sum[:]
		case "2.16.840.1.101.3.4.2.1":
			sum := sha256.Sum256(certificate.Raw)
			digest = sum[:]
		case "2.16.840.1.101.3.4.2.2":
			sum := sha512.Sum384(certificate.Raw)
			digest = sum[:]
		case "2.16.840.1.101.3.4.2.3":
			sum := sha512.Sum512(certificate.Raw)
			digest = sum[:]
		case "1.2.156.10197.1.401":
			sum := sm3.Sum(certificate.Raw)
			digest = sum[:]
		default:
			return fmt.Errorf("未支持的时间戳证书摘要算法 %s", algorithm)
		}
		if !bytes.Equal(digest, id.Hash) {
			return fmt.Errorf("时间戳签名者证书摘要不匹配")
		}
		if len(id.IssuerSerial.FullBytes) > 0 {
			var issuer struct {
				Names  []asn1.RawValue
				Serial *big.Int
			}
			if _, err := asn1.Unmarshal(id.IssuerSerial.FullBytes, &issuer); err != nil || issuer.Serial == nil || issuer.Serial.Cmp(certificate.SerialNumber) != 0 {
				return fmt.Errorf("时间戳证书绑定的序列号无效")
			}
			match := false
			for _, name := range issuer.Names {
				if name.Class == asn1.ClassContextSpecific && name.Tag == 4 && bytes.Equal(name.Bytes, certificate.RawIssuer) {
					match = true
				}
			}
			if !match {
				return fmt.Errorf("时间戳证书绑定的签发者不匹配")
			}
		}
	}
	if count != 1 {
		return fmt.Errorf("时间戳须包含唯一的 SigningCertificate 或 SigningCertificateV2 属性")
	}
	return nil
}

func checkTimestampAccuracy(data []byte) error {
	last := -1
	for len(data) > 0 {
		var field asn1.RawValue
		var err error
		data, err = asn1.Unmarshal(data, &field)
		if err != nil {
			return err
		}
		rank := -1
		params := ""
		if field.Class == asn1.ClassUniversal && field.Tag == asn1.TagInteger {
			rank = 0
		}
		if field.Class == asn1.ClassContextSpecific && field.Tag == 0 {
			rank = 1
			params = "tag:0"
		}
		if field.Class == asn1.ClassContextSpecific && field.Tag == 1 {
			rank = 2
			params = "tag:1"
		}
		if rank <= last {
			return fmt.Errorf("时间戳精度字段顺序或类型无效")
		}
		last = rank
		var value int
		if _, err := asn1.UnmarshalWithParams(field.FullBytes, &value, params); err != nil || value < 0 || (rank > 0 && (value < 1 || value > 999)) {
			return fmt.Errorf("时间戳精度字段取值无效")
		}
	}
	return nil
}

func timestampNameMatches(name asn1.RawValue, certificate *smx509.Certificate) bool {
	switch name.Tag {
	case 1:
		for _, email := range certificate.EmailAddresses {
			if email == string(name.Bytes) {
				return true
			}
		}
	case 2:
		return certificate.VerifyHostname(string(name.Bytes)) == nil
	case 4:
		return bytes.Equal(name.Bytes, certificate.RawSubject)
	case 6:
		for _, uri := range certificate.URIs {
			if uri.String() == string(name.Bytes) {
				return true
			}
		}
	case 7:
		if len(name.Bytes) != 4 && len(name.Bytes) != 16 {
			return false
		}
		return certificate.VerifyHostname(net.IP(name.Bytes).String()) == nil
	}
	return false
}
