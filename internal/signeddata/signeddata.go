// Package signeddata 实现 GB/T 35275 的 SM2/SM3 SignedData 与原文字节绑定。
package signeddata

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/emmansun/gmsm/pkcs7"
	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm3"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/ses"
)

var (
	ErrInvalid     = errors.New("signeddata: 无效的签名消息")
	ErrUnsupported = errors.New("signeddata: 未支持的签名消息")
	timestampOID   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 14}
)

const MaxBytes = 4 << 20

type contentInfo struct {
	Type    asn1.ObjectIdentifier
	Content asn1.RawValue `asn1:"explicit,optional,tag:0"`
}
type attribute struct {
	Type   asn1.ObjectIdentifier
	Values asn1.RawValue `asn1:"set"`
}
type signerInfo struct {
	Version int
	Issuer  struct {
		Name   asn1.RawValue
		Serial *big.Int
	}
	Digest    pkix.AlgorithmIdentifier
	Signed    []attribute `asn1:"optional,tag:0"`
	Algorithm pkix.AlgorithmIdentifier
	Signature []byte
	Unsigned  []attribute `asn1:"optional,tag:1"`
}
type signedData struct {
	Version      int
	Digests      []pkix.AlgorithmIdentifier `asn1:"set"`
	Content      contentInfo
	Certificates asn1.RawValue   `asn1:"optional,tag:0"`
	CRLs         []asn1.RawValue `asn1:"optional,tag:1"`
	Signers      []signerInfo    `asn1:"set"`
}

// Proof 只报告数学和字节绑定证据；证书信任及吊销状态由上层验证。
type Proof struct {
	Signer         *smx509.Certificate
	Certificates   []*smx509.Certificate
	SigningTime    time.Time
	Timestamp      *ses.TimestampProof
	TimestampError error
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func decode(data []byte, out any) error {
	rest, err := asn1.Unmarshal(data, out)
	if err != nil || len(rest) != 0 {
		return invalid("DER 编码无效或存在尾随数据")
	}
	return nil
}

func parse(value []byte) (*signedData, error) {
	if len(value) == 0 || len(value) > MaxBytes {
		return nil, invalid("签名消息为空或超过 %d 字节", MaxBytes)
	}
	var envelope contentInfo
	if err := decode(value, &envelope); err != nil {
		return nil, err
	}
	if !envelope.Type.Equal(pkcs7.SM2OIDSignedData) && !envelope.Type.Equal(pkcs7.OIDSignedData) {
		return nil, fmt.Errorf("%w: 内容类型 %s", ErrUnsupported, envelope.Type)
	}
	var sd signedData
	if err := decode(envelope.Content.Bytes, &sd); err != nil {
		return nil, err
	}
	canonical, err := asn1.Marshal(sd)
	if err != nil || !bytes.Equal(canonical, envelope.Content.Bytes) {
		return nil, invalid("SignedData 不是完整规范 DER 或包含额外字段")
	}
	if sd.Version != 1 || len(sd.Signers) != 1 || sd.Signers[0].Version != 1 {
		return nil, fmt.Errorf("%w: 仅支持 v1、单一签名者 SignedData", ErrUnsupported)
	}
	if !sd.Content.Type.Equal(pkcs7.SM2OIDData) && !sd.Content.Type.Equal(pkcs7.OIDData) {
		return nil, invalid("签名内容类型必须为 Data")
	}
	if len(sd.Digests) != 1 || !sd.Digests[0].Algorithm.Equal(pkcs7.OIDDigestAlgorithmSM3) || !sd.Signers[0].Digest.Algorithm.Equal(pkcs7.OIDDigestAlgorithmSM3) {
		return nil, fmt.Errorf("%w: SignedData 摘要必须一致地声明 SM3", ErrUnsupported)
	}
	algorithm := sd.Signers[0].Algorithm.Algorithm
	if !algorithm.Equal(pkcs7.OIDDigestEncryptionAlgorithmSM2) && !algorithm.Equal(pkcs7.OIDDigestAlgorithmSM2SM3) {
		return nil, fmt.Errorf("%w: 签名算法 %s", ErrUnsupported, algorithm)
	}
	if len(sd.CRLs) != 0 {
		// CRL 可随包携带，但不能替代调用方提供的受信吊销证据。
		if len(sd.CRLs) > 128 {
			return nil, invalid("内嵌 CRL 数量超过上限")
		}
	}
	return &sd, nil
}

func valueOf(attr attribute, out any) error {
	if attr.Values.Class != asn1.ClassUniversal || attr.Values.Tag != asn1.TagSet {
		return invalid("签名属性不是 SET")
	}
	return decode(attr.Values.Bytes, out)
}

// Verify 校验 attached/detached SignedData 绑定完整原始文件字节，不做 XML 规范化。
func Verify(value, original, uid []byte) (*Proof, error) {
	sd, err := parse(value)
	if err != nil {
		return nil, err
	}
	if len(sd.Content.Content.FullBytes) != 0 {
		var content []byte
		if err := decode(sd.Content.Content.Bytes, &content); err != nil || !bytes.Equal(content, original) {
			return nil, invalid("内嵌原文与当前文件字节不匹配")
		}
	}
	p7, err := pkcs7.Parse(value)
	if err != nil || len(p7.Certificates) > 128 {
		return nil, invalid("证书集合无法解析或超过上限")
	}
	certificate := p7.GetOnlySigner()
	if certificate == nil {
		return nil, invalid("缺少签名者证书")
	}
	pub, ok := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || !sm2.IsSM2PublicKey(pub) {
		return nil, invalid("签名者证书不是 SM2 公钥")
	}
	proof := &Proof{Signer: certificate, Certificates: p7.Certificates}
	si := &sd.Signers[0]
	signed := original
	if len(si.Signed) > 0 {
		seen := map[string]bool{}
		var previous []byte
		for _, attr := range si.Signed {
			encoded, err := asn1.Marshal(attr)
			if err != nil || previous != nil && bytes.Compare(previous, encoded) > 0 {
				return nil, invalid("签名属性 SET OF 未按 DER 排序")
			}
			previous = encoded
			id := attr.Type.String()
			if seen[id] {
				return nil, invalid("签名属性重复: %s", id)
			}
			seen[id] = true
			switch {
			case attr.Type.Equal(pkcs7.OIDAttributeContentType):
				var typ asn1.ObjectIdentifier
				if err := valueOf(attr, &typ); err != nil || !typ.Equal(sd.Content.Type) {
					return nil, invalid("contentType 属性与原文类型不一致")
				}
			case attr.Type.Equal(pkcs7.OIDAttributeMessageDigest):
				var digest []byte
				sum := sm3.Sum(original)
				if err := valueOf(attr, &digest); err != nil || !bytes.Equal(digest, sum[:]) {
					return nil, invalid("messageDigest 与原文字节不匹配")
				}
			case attr.Type.Equal(pkcs7.OIDAttributeSigningTime):
				if err := valueOf(attr, &proof.SigningTime); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("%w: 未解释的签名属性 %s", ErrUnsupported, id)
			}
		}
		if !seen[pkcs7.OIDAttributeContentType.String()] || !seen[pkcs7.OIDAttributeMessageDigest.String()] {
			return nil, invalid("必需签名属性缺失")
		}
		signed, err = asn1.MarshalWithParams(si.Signed, "set")
		if err != nil {
			return nil, err
		}
	}
	if !sm2.VerifyASN1WithSM2(pub, uid, signed, si.Signature) {
		return nil, invalid("SM2 数学签名验证失败")
	}
	if len(si.Unsigned) > 1 {
		return nil, fmt.Errorf("%w: 未支持的或重复的非签名属性", ErrUnsupported)
	}
	for _, attr := range si.Unsigned {
		if !attr.Type.Equal(timestampOID) {
			return nil, fmt.Errorf("%w: 非签名属性 %s", ErrUnsupported, attr.Type)
		}
		var token asn1.RawValue
		if err := valueOf(attr, &token); err != nil {
			return nil, err
		}
		proof.Timestamp, err = ses.CheckTimestamp(token.FullBytes, si.Signature)
		if err != nil {
			proof.TimestampError = err
		}
	}
	return proof, nil
}

// Options 由外部签名器或硬件适配器提供密钥及时间戳服务。
type Options struct {
	Time          time.Time
	UID           []byte
	Intermediates [][]byte
	Timestamp     func([]byte) ([]byte, error)
}

// Create 生成 detached GB/T 35275 SignedData；Signer.Sign 接收完整 DER 签名属性
// 和 sm2.NewSM2SignerOption(true, UID)。函数不创建或保存私钥。
func Create(original, certificateDER []byte, signer crypto.Signer, opt Options) ([]byte, error) {
	certificate, err := smx509.ParseCertificate(certificateDER)
	if err != nil || signer == nil {
		return nil, invalid("需要 SM2 签名证书和 Signer")
	}
	pub, ok := signer.Public().(*ecdsa.PublicKey)
	certPub, certOK := certificate.PublicKey.(*ecdsa.PublicKey)
	if !ok || !certOK || !sm2.IsSM2PublicKey(pub) || !pub.Equal(certPub) {
		return nil, invalid("Signer 公钥与 SM2 证书不匹配")
	}
	if opt.Time.IsZero() {
		opt.Time = time.Now()
	}
	if err := ses.ValidateSigningCertificate(certificate, opt.Time); err != nil {
		return nil, err
	}
	makeAttribute := func(oid asn1.ObjectIdentifier, value any) (attribute, error) {
		data, err := asn1.Marshal(value)
		return attribute{Type: oid, Values: asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: data}}, err
	}
	sum := sm3.Sum(original)
	si := signerInfo{Version: 1, Digest: pkix.AlgorithmIdentifier{Algorithm: pkcs7.OIDDigestAlgorithmSM3}, Algorithm: pkix.AlgorithmIdentifier{Algorithm: pkcs7.OIDDigestEncryptionAlgorithmSM2}}
	si.Issuer.Name, si.Issuer.Serial = asn1.RawValue{FullBytes: certificate.RawIssuer}, certificate.SerialNumber
	for _, item := range []struct {
		oid asn1.ObjectIdentifier
		val any
	}{{pkcs7.OIDAttributeContentType, pkcs7.SM2OIDData}, {pkcs7.OIDAttributeMessageDigest, sum[:]}, {pkcs7.OIDAttributeSigningTime, opt.Time.UTC().Truncate(time.Second)}} {
		attr, err := makeAttribute(item.oid, item.val)
		if err != nil {
			return nil, err
		}
		si.Signed = append(si.Signed, attr)
	}
	attrs, err := asn1.MarshalWithParams(si.Signed, "set")
	if err != nil {
		return nil, err
	}
	// 隐式 [0] 的签名属性仍是 SET OF，编码时也必须按 DER 顺序排列。
	sort.Slice(si.Signed, func(i, j int) bool {
		a, _ := asn1.Marshal(si.Signed[i])
		b, _ := asn1.Marshal(si.Signed[j])
		return bytes.Compare(a, b) < 0
	})
	si.Signature, err = signer.Sign(rand.Reader, attrs, sm2.NewSM2SignerOption(true, opt.UID))
	if err != nil {
		return nil, err
	}
	if opt.Timestamp != nil {
		token, err := opt.Timestamp(bytes.Clone(si.Signature))
		if err != nil {
			return nil, err
		}
		if len(token) > MaxBytes {
			return nil, invalid("时间戳超过大小上限")
		}
		proof, err := ses.CheckTimestamp(token, si.Signature)
		if err != nil || proof.Time.Before(opt.Time.Truncate(time.Second)) {
			return nil, invalid("时间戳无法绑定签名值或早于签名时间: %v", err)
		}
		si.Unsigned = []attribute{{Type: timestampOID, Values: asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: token}}}
	}
	certificates := [][]byte{certificateDER}
	if len(opt.Intermediates) > 127 {
		return nil, invalid("中间证书数量超过上限")
	}
	for _, data := range opt.Intermediates {
		if _, err := smx509.ParseCertificate(data); err != nil {
			return nil, err
		}
		certificates = append(certificates, data)
	}
	sort.Slice(certificates, func(i, j int) bool { return bytes.Compare(certificates[i], certificates[j]) < 0 })
	sd := signedData{Version: 1, Digests: []pkix.AlgorithmIdentifier{si.Digest}, Content: contentInfo{Type: pkcs7.SM2OIDData}, Certificates: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: bytes.Join(certificates, nil)}, Signers: []signerInfo{si}}
	data, err := asn1.Marshal(sd)
	if err != nil {
		return nil, err
	}
	value, err := asn1.Marshal(contentInfo{Type: pkcs7.SM2OIDSignedData, Content: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: data}})
	if err != nil {
		return nil, err
	}
	if proof, err := Verify(value, original, opt.UID); err != nil {
		return nil, err
	} else if proof.TimestampError != nil {
		return nil, proof.TimestampError
	}
	return value, nil
}
