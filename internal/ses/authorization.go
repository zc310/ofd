package ses

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/asn1"
	"fmt"
	"strings"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm3"
	"github.com/emmansun/gmsm/smx509"
)

// CertificateDigest 对应 GB/T 38540—2020 §6.1.2.4 的 CertDigestObj。
type CertificateDigest struct {
	Type  string `asn1:"printable"`
	Value []byte
}

// CertificateHash 只用于印章授权列表，不影响 OFD References 的摘要算法集合。
func CertificateHash(method string, certificate []byte) ([]byte, error) {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "SM3", "1.2.156.10197.1.401":
		sum := sm3.Sum(certificate)
		return sum[:], nil
	case "SHA256", "SHA-256", "2.16.840.1.101.3.4.2.1":
		sum := sha256.Sum256(certificate)
		return sum[:], nil
	default:
		return nil, fmt.Errorf("不支持的授权证书杂凑类型 %q", method)
	}
}

func authorizationList(params SealParams, maker []byte) (int, asn1.RawValue, error) {
	var encoded []byte
	var err error
	typ := 1
	if len(params.SignerDigests) > 0 {
		if params.SignerCertificates != nil {
			return 0, asn1.RawValue{}, fmt.Errorf("授权证书与证书杂凑列表不能同时提供")
		}
		typ = 2
		for _, digest := range params.SignerDigests {
			value, hashErr := CertificateHash(digest.Type, nil)
			if hashErr != nil {
				return 0, asn1.RawValue{}, hashErr
			}
			if len(value) != len(digest.Value) {
				return 0, asn1.RawValue{}, fmt.Errorf("授权证书杂凑长度无效")
			}
		}
		encoded, err = asn1.Marshal(params.SignerDigests)
	} else {
		certificates := params.SignerCertificates
		if certificates == nil {
			certificates = [][]byte{maker}
		}
		if len(certificates) == 0 {
			return 0, asn1.RawValue{}, fmt.Errorf("授权证书列表不能为空")
		}
		for _, certificate := range certificates {
			if _, err := smx509.ParseCertificate(certificate); err != nil {
				return 0, asn1.RawValue{}, fmt.Errorf("授权证书无效: %w", err)
			}
		}
		encoded, err = asn1.Marshal(certificates)
	}
	return typ, asn1.RawValue{FullBytes: encoded}, err
}

// CertificateAuthorized 校验完整 DER 证书或其杂凑是否获印章授权（§7.3 c）。
// 检查整张列表的结构，不能因前面的项目匹配而忽略后续无效项目。
func CertificateAuthorized(typ int64, listDER, certificate []byte) error {
	matched := false
	switch typ {
	case 1:
		var certificates [][]byte
		rest, err := asn1.Unmarshal(listDER, &certificates)
		if err != nil || len(rest) != 0 || len(certificates) == 0 {
			return fmt.Errorf("印章授权证书列表无效或为空")
		}
		for _, item := range certificates {
			if _, err := smx509.ParseCertificate(item); err != nil {
				return fmt.Errorf("印章授权证书无效: %w", err)
			}
			matched = matched || bytes.Equal(item, certificate)
		}
	case 2:
		var digests []CertificateDigest
		rest, err := asn1.Unmarshal(listDER, &digests)
		if err != nil || len(rest) != 0 || len(digests) == 0 {
			return fmt.Errorf("印章授权证书杂凑列表无效或为空")
		}
		for _, item := range digests {
			sum, err := CertificateHash(item.Type, certificate)
			if err != nil {
				return err
			}
			if len(sum) != len(item.Value) {
				return fmt.Errorf("印章授权证书杂凑长度无效")
			}
			matched = matched || bytes.Equal(sum, item.Value)
		}
	default:
		return fmt.Errorf("不支持的印章授权证书列表类型 %d", typ)
	}
	if !matched {
		return fmt.Errorf("签章者证书不在印章授权列表中")
	}
	return nil
}

// ValidateSigningKey 防止用不匹配的证书、私钥或非签名用途证书生成签章。
func ValidateSigningKey(der []byte, key *sm2.PrivateKey, at time.Time) error {
	if key == nil || key.D == nil || key.X == nil || key.Y == nil || key.Curve == nil {
		return fmt.Errorf("SM2 私钥为空")
	}
	cert, err := smx509.ParseCertificate(der)
	if err != nil {
		return fmt.Errorf("解析签名证书失败: %w", err)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !sm2.IsSM2PublicKey(pub) || !pub.Equal(key.Public()) {
		return fmt.Errorf("证书公钥与 SM2 私钥不匹配")
	}
	return ValidateSigningCertificate(cert, at)
}

func ValidateSigningCertificate(cert *smx509.Certificate, at time.Time) error {
	if at.IsZero() || at.Before(cert.NotBefore) || at.After(cert.NotAfter) {
		return fmt.Errorf("证书在签章时间不在有效期内")
	}
	if cert.KeyUsage&(smx509.KeyUsageDigitalSignature|smx509.KeyUsageContentCommitment) == 0 {
		return fmt.Errorf("证书密钥用途不允许数字签名")
	}
	return nil
}

// ValidateSealForSigner 是 §7.2 a) 的本地签署前检查；信任链和状态证据由
// SigningOptions.Validate 接入，不隐式访问网络或系统根证书。
func ValidateSealForSigner(seal *Seal, signer []byte, at time.Time) error {
	if seal == nil {
		return fmt.Errorf("印章为空")
	}
	info := seal.SealInfo
	if info.Header.ID != HeaderID || info.Header.Version != SealVersion {
		return fmt.Errorf("印章头或版本无效")
	}
	if !info.Property.ValidTo.After(info.Property.ValidFrom) || at.Before(info.Property.ValidFrom) || at.After(info.Property.ValidTo) {
		return fmt.Errorf("印章在签章时间不在有效期内")
	}
	if err := CertificateAuthorized(int64(info.Property.CertificateType), info.Property.CertList.FullBytes, signer); err != nil {
		return err
	}
	cert, err := smx509.ParseCertificate(seal.Certificate)
	if err != nil {
		return fmt.Errorf("制章者证书无效: %w", err)
	}
	if err := ValidateSigningCertificate(cert, at); err != nil {
		return err
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !sm2.IsSM2PublicKey(pub) || !seal.SignatureAlgorithm.Equal(algorithmOID) {
		return fmt.Errorf("印章签名算法或公钥无效")
	}
	data, err := asn1.Marshal(info)
	if err != nil {
		return err
	}
	if seal.Signature.BitLength != len(seal.Signature.Bytes)*8 || !sm2.VerifyASN1WithSM2(pub, nil, data, seal.Signature.Bytes) {
		return fmt.Errorf("印章签名验证失败")
	}
	for _, extension := range info.Extensions {
		if extension.Critical {
			return fmt.Errorf("不支持的关键印章扩展 %s", extension.OID)
		}
	}
	return nil
}
