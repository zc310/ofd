package parser

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"time"

	"github.com/emmansun/gmsm/sm2"
	gmx509 "github.com/emmansun/gmsm/smx509"
)

const sm2WithSM3OID = "1.2.156.10197.1.501"

// SM2SignatureFormat 指定 BIT STRING 中 SM2 签名值的编码格式。
type SM2SignatureFormat string

const (
	// SM2SignatureFormatAuto 自动识别 DER SEQUENCE 或 64 字节 r||s。
	SM2SignatureFormatAuto SM2SignatureFormat = "auto"
	// SM2SignatureFormatDER 表示 DER 编码的 SEQUENCE { r, s }。
	SM2SignatureFormatDER SM2SignatureFormat = "der"
	// SM2SignatureFormatRaw 表示固定 64 字节的 r||s 编码。
	SM2SignatureFormatRaw SM2SignatureFormat = "raw"
)

// SignatureVerificationResult 是 SES 签名的密码学验证结果。
type SignatureVerificationResult struct {
	// Valid 表示印章内部签名和外层签名均通过。
	Valid bool
	// TrustChecked 表示是否使用显式信任根执行了证书链校验。
	TrustChecked bool
	// Trusted 表示两层签名证书均通过证书链校验。
	Trusted bool
	// RevocationChecked 表示是否执行了证书吊销校验。
	RevocationChecked bool
	// RevocationValid 表示两层证书均未被显式提供的 CRL 吊销。
	RevocationValid bool
	// VerificationTime 是验证证书有效期使用的签名时间。
	VerificationTime time.Time
	// Seal 是印章内部签名结果。
	Seal SignatureComponentResult
	// Outer 是 SignedValue 外层签名结果。
	Outer SignatureComponentResult
}

// SignatureComponentResult 是一层签名的证书和数学验证结果。
type SignatureComponentResult struct {
	// Valid 表示签名值通过公钥验证。
	Valid bool
	// Algorithm 是签名算法 OID。
	Algorithm string
	// SignatureFormat 是签名值实际使用的编码格式。
	SignatureFormat string
	// Certificate 是签名证书信息。
	Certificate *CertificateInfo
	// CertificateValid 表示证书在签名时间点有效。
	CertificateValid bool
	// TrustChecked 表示是否执行了证书链校验。
	TrustChecked bool
	// Trusted 表示证书链校验通过。
	Trusted bool
	// TrustError 是证书链校验失败原因。
	TrustError string
	// RevocationChecked 表示是否执行了证书吊销校验。
	RevocationChecked bool
	// RevocationStatus 是吊销状态：good、revoked、unknown 或 error。
	RevocationStatus string
	// RevocationError 是吊销校验失败原因。
	RevocationError string
	// Error 是验证失败原因。
	Error string
}

// CertificateInfo 是签名证书的可报告信息，不包含完整证书二进制。
type CertificateInfo struct {
	SerialNumber string
	Subject      pkix.Name
	Issuer       pkix.Name
	NotBefore    time.Time
	NotAfter     time.Time
	PublicKey    string
}

// CertificateTrustOptions 配置可选的证书链校验。
// Roots 必须由调用方显式提供，避免解析器隐式依赖运行环境的系统根证书。
type CertificateTrustOptions struct {
	// Roots 是信任根证书池。
	Roots *gmx509.CertPool
	// Intermediates 是额外的中间证书 DER；SES 内嵌证书会自动加入候选池。
	Intermediates [][]byte
	// CurrentTime 是证书链校验时间；为空时使用签名时间。
	CurrentTime time.Time
}

// SignatureVerificationOptions 配置 SES 签名的 SM2 验证兼容行为。
type SignatureVerificationOptions struct {
	// UID 是 SM2 签名用户标识；为空时使用国密库默认标识。
	UID []byte
	// SignatureFormat 是签名值编码格式；为空时自动识别。
	SignatureFormat SM2SignatureFormat
	// Trust 是可选的证书链校验配置；为空时不执行证书链校验。
	Trust *CertificateTrustOptions
	// Revocation 是可选的离线 CRL 吊销校验配置；为空时不执行吊销校验。
	Revocation *CertificateRevocationOptions
}

// CertificateRevocationOptions 配置离线 CRL 吊销校验。
// 校验不会访问 CRLDistributionPoints 或其他网络地址。
type CertificateRevocationOptions struct {
	// CRLs 是 DER 或 PEM 编码的 X.509 CRL 列表。
	CRLs [][]byte
	// Issuers 是用于验证 CRL 签名的签发者证书 DER 列表。
	Issuers [][]byte
	// CurrentTime 是 CRL 有效期和吊销时间校验时间；为空时使用签名时间。
	CurrentTime time.Time
}

// VerifySESSignedValue 验证 SES 电子印章的两层 SM2 签名。
// 印章内部签名验证 SES_Seal_Info，外层签名验证 TBS_Sign；两层分别使用各自证书。
func VerifySESSignedValue(value *SignedValue) (*SignatureVerificationResult, error) {
	return verifySESSignedValue(value, nil, nil)
}

// VerifySESSignedValueWithTrust 使用显式信任根验证 SES 两层签名和证书链。
// Trusted 与 Valid 分离：证书链不可信不会改变签名数学验证结果。
func VerifySESSignedValueWithTrust(value *SignedValue, options *CertificateTrustOptions) (*SignatureVerificationResult, error) {
	return verifySESSignedValue(value, options, nil)
}

// VerifySESSignedValueWithOptions 使用显式 SM2 UID 和签名值编码格式验证 SES 签名。
func VerifySESSignedValueWithOptions(value *SignedValue, options *SignatureVerificationOptions) (*SignatureVerificationResult, error) {
	if options == nil {
		return verifySESSignedValue(value, nil, nil)
	}
	return verifySESSignedValue(value, options.Trust, options)
}

func verifySESSignedValue(value *SignedValue, trustOptions *CertificateTrustOptions, verificationOptions *SignatureVerificationOptions) (*SignatureVerificationResult, error) {
	if value == nil || value.SES == nil {
		return nil, fmt.Errorf("不是可验证的 SES 签名值")
	}
	verificationTime := value.SES.TBS.SignTime
	result := &SignatureVerificationResult{VerificationTime: verificationTime}
	result.Seal = verifySignatureComponent(
		value.SES.TBS.Seal.Certificate,
		value.SES.TBS.Seal.SealInfo.Raw,
		value.SES.TBS.Seal.Signature.Bytes,
		value.SES.TBS.Seal.SignatureAlgorithm,
		verificationTime,
		trustOptions,
		value.SES.TBS.Seal.SealInfo.Property.Certificates,
		verificationOptions,
	)
	result.Outer = verifySignatureComponent(
		value.SES.Certificate,
		value.SES.TBS.Raw,
		value.SES.Signature.Bytes,
		value.SES.SignatureAlgorithm,
		verificationTime,
		trustOptions,
		value.SES.TBS.Seal.SealInfo.Property.Certificates,
		verificationOptions,
	)
	result.Valid = result.Seal.Valid && result.Outer.Valid
	if trustOptions != nil {
		result.TrustChecked = result.Seal.TrustChecked && result.Outer.TrustChecked
		result.Trusted = result.Seal.Trusted && result.Outer.Trusted
	}
	if verificationOptions != nil && verificationOptions.Revocation != nil {
		result.RevocationChecked = result.Seal.RevocationChecked && result.Outer.RevocationChecked
		result.RevocationValid = result.Seal.RevocationStatus == "good" && result.Outer.RevocationStatus == "good"
	}
	return result, nil
}

func verifySignatureComponent(certificateDER, signed, signature []byte, algorithm asn1.ObjectIdentifier, at time.Time, trustOptions *CertificateTrustOptions, embeddedIntermediates [][]byte, verificationOptions *SignatureVerificationOptions) SignatureComponentResult {
	result := SignatureComponentResult{Algorithm: algorithm.String()}
	if len(certificateDER) == 0 {
		result.Error = "签名证书为空"
		return result
	}
	certificate, err := gmx509.ParseCertificate(certificateDER)
	if err != nil {
		result.Error = fmt.Sprintf("解析签名证书失败: %v", err)
		return result
	}
	result.Certificate = certificateInfo(certificate)
	result.CertificateValid = !at.Before(certificate.NotBefore) && !at.After(certificate.NotAfter)
	if trustOptions != nil {
		result.TrustChecked = true
		trustTime := trustOptions.CurrentTime
		if trustTime.IsZero() {
			trustTime = at
		}
		result.Trusted, result.TrustError = verifyCertificateChain(certificate, trustOptions, trustTime, embeddedIntermediates)
	}
	if verificationOptions != nil && verificationOptions.Revocation != nil {
		result.RevocationChecked = true
		revocationTime := verificationOptions.Revocation.CurrentTime
		if revocationTime.IsZero() {
			revocationTime = at
		}
		result.RevocationStatus, result.RevocationError = verifyCertificateRevocation(certificate, verificationOptions.Revocation, revocationTime, embeddedIntermediates)
	}
	if !result.CertificateValid {
		result.Error = fmt.Sprintf("证书在签名时间 %s 不在有效期内", at.Format(time.RFC3339))
		return result
	}
	if result.Algorithm != sm2WithSM3OID {
		result.Error = fmt.Sprintf("不支持的签名算法 %s", result.Algorithm)
		return result
	}
	publicKey, err := sm2PublicKey(certificate.PublicKey)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	r, s, format, err := parseSM2Signature(signature, signatureFormat(verificationOptions))
	if err != nil || r == nil || s == nil {
		if err == nil {
			err = fmt.Errorf("签名值缺少 R 或 S")
		}
		result.Error = fmt.Sprintf("解析签名值失败: %v", err)
		return result
	}
	result.SignatureFormat = string(format)
	digest, err := sm2.CalculateSM2Hash(publicKey, signed, signatureUID(verificationOptions))
	if err != nil || !sm2.Verify(publicKey, digest, r, s) {
		result.Error = "SM2 签名验证失败"
		return result
	}
	result.Valid = true
	return result
}

func verifyCertificateRevocation(certificate *gmx509.Certificate, options *CertificateRevocationOptions, at time.Time, embeddedIssuers [][]byte) (string, string) {
	if len(options.CRLs) == 0 {
		return "unknown", "未提供 CRL"
	}
	issuerDERs := make([][]byte, 0, len(embeddedIssuers)+len(options.Issuers))
	issuerDERs = append(issuerDERs, embeddedIssuers...)
	issuerDERs = append(issuerDERs, options.Issuers...)
	issuers := make([]*gmx509.Certificate, 0, len(issuerDERs))
	for _, issuerDER := range issuerDERs {
		issuer, err := gmx509.ParseCertificate(issuerDER)
		if err != nil {
			return "error", fmt.Sprintf("解析 CRL 签发者证书失败: %v", err)
		}
		if issuer.Subject.String() == certificate.Issuer.String() {
			issuers = append(issuers, issuer)
		}
	}
	if len(issuers) == 0 {
		return "unknown", "未提供匹配的 CRL 签发者证书"
	}
	for _, crlDER := range options.CRLs {
		crl, err := gmx509.ParseCRL(crlDER)
		if err != nil {
			return "error", fmt.Sprintf("解析 CRL 失败: %v", err)
		}
		crlIssuer, err := asn1.Marshal(crl.TBSCertList.Issuer)
		if err != nil {
			return "error", fmt.Sprintf("读取 CRL 签发者失败: %v", err)
		}
		matchedIssuer := false
		for _, issuer := range issuers {
			if !bytes.Equal(crlIssuer, issuer.RawSubject) {
				continue
			}
			if err := issuer.CheckCRLSignature(crl); err != nil {
				return "error", fmt.Sprintf("CRL 签名验证失败: %v", err)
			}
			matchedIssuer = true
			break
		}
		if !matchedIssuer {
			continue
		}
		if at.Before(crl.TBSCertList.ThisUpdate) || (!crl.TBSCertList.NextUpdate.IsZero() && at.After(crl.TBSCertList.NextUpdate)) {
			return "unknown", "CRL 不在校验时间有效期内"
		}
		for _, revoked := range crl.TBSCertList.RevokedCertificates {
			if revoked.SerialNumber != nil && certificate.SerialNumber != nil && revoked.SerialNumber.Cmp(certificate.SerialNumber) == 0 && !at.Before(revoked.RevocationTime) {
				return "revoked", fmt.Sprintf("证书已于 %s 吊销", revoked.RevocationTime.Format(time.RFC3339))
			}
		}
		return "good", ""
	}
	return "unknown", "没有找到可验证的匹配 CRL"
}

func signatureFormat(options *SignatureVerificationOptions) SM2SignatureFormat {
	if options == nil || options.SignatureFormat == "" {
		return SM2SignatureFormatAuto
	}
	return options.SignatureFormat
}

func signatureUID(options *SignatureVerificationOptions) []byte {
	if options == nil || len(options.UID) == 0 {
		return nil
	}
	return options.UID
}

type sm2Signature struct {
	R *big.Int
	S *big.Int
}

func parseSM2Signature(signature []byte, format SM2SignatureFormat) (*big.Int, *big.Int, SM2SignatureFormat, error) {
	switch format {
	case SM2SignatureFormatDER:
		return parseSM2DERSignature(signature)
	case SM2SignatureFormatRaw:
		return parseSM2RawSignature(signature)
	case SM2SignatureFormatAuto:
		if r, s, detectedFormat, err := parseSM2DERSignature(signature); err == nil {
			return r, s, detectedFormat, nil
		}
		if r, s, detectedFormat, err := parseSM2RawSignature(signature); err == nil {
			return r, s, detectedFormat, nil
		}
		return nil, nil, format, fmt.Errorf("不是有效的 DER 或 64 字节 SM2 签名")
	default:
		return nil, nil, format, fmt.Errorf("不支持的 SM2 签名值格式 %q", format)
	}
}

func parseSM2DERSignature(signature []byte) (*big.Int, *big.Int, SM2SignatureFormat, error) {
	var value sm2Signature
	rest, err := asn1.Unmarshal(signature, &value)
	if err != nil {
		return nil, nil, SM2SignatureFormatDER, err
	}
	if len(rest) != 0 {
		return nil, nil, SM2SignatureFormatDER, fmt.Errorf("DER 签名后仍有 %d 字节", len(rest))
	}
	if value.R == nil || value.S == nil || value.R.Sign() <= 0 || value.S.Sign() <= 0 {
		return nil, nil, SM2SignatureFormatDER, fmt.Errorf("DER 签名缺少有效的 R 或 S")
	}
	return value.R, value.S, SM2SignatureFormatDER, nil
}

func parseSM2RawSignature(signature []byte) (*big.Int, *big.Int, SM2SignatureFormat, error) {
	if len(signature) != 64 {
		return nil, nil, SM2SignatureFormatRaw, fmt.Errorf("原始 SM2 签名长度为 %d，期望 64", len(signature))
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return nil, nil, SM2SignatureFormatRaw, fmt.Errorf("原始签名缺少有效的 R 或 S")
	}
	return r, s, SM2SignatureFormatRaw, nil
}

func verifyCertificateChain(certificate *gmx509.Certificate, options *CertificateTrustOptions, at time.Time, embeddedIntermediates [][]byte) (bool, string) {
	if options.Roots == nil {
		return false, "未提供证书信任根"
	}
	intermediates := gmx509.NewCertPool()
	allIntermediates := make([][]byte, 0, len(embeddedIntermediates)+len(options.Intermediates))
	allIntermediates = append(allIntermediates, embeddedIntermediates...)
	allIntermediates = append(allIntermediates, options.Intermediates...)
	for _, certificateDER := range allIntermediates {
		candidate, err := gmx509.ParseCertificate(certificateDER)
		if err != nil {
			return false, fmt.Sprintf("解析中间证书失败: %v", err)
		}
		if !bytes.Equal(candidate.Raw, certificate.Raw) {
			intermediates.AddCert(candidate)
		}
	}
	_, err := certificate.Verify(gmx509.VerifyOptions{
		Roots:         options.Roots,
		Intermediates: intermediates,
		CurrentTime:   at,
		KeyUsages:     []gmx509.ExtKeyUsage{gmx509.ExtKeyUsageAny},
	})
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}

func sm2PublicKey(value any) (*ecdsa.PublicKey, error) {
	publicKey, ok := value.(*ecdsa.PublicKey)
	if ok {
		if publicKey == nil || publicKey.X == nil || publicKey.Y == nil {
			return nil, fmt.Errorf("签名证书公钥为空")
		}
		if !sm2.IsSM2PublicKey(publicKey) {
			return nil, fmt.Errorf("签名证书公钥曲线不是 SM2")
		}
		return publicKey, nil
	}
	return nil, fmt.Errorf("签名证书公钥不是 SM2 公钥: %T", value)
}

func certificateInfo(certificate *gmx509.Certificate) *CertificateInfo {
	publicKey := ""
	switch certificate.PublicKey.(type) {
	case *ecdsa.PublicKey:
		publicKey = "SM2"
	default:
		publicKey = fmt.Sprintf("%T", certificate.PublicKey)
	}
	serial := ""
	if certificate.SerialNumber != nil {
		serial = certificate.SerialNumber.String()
	}
	return &CertificateInfo{
		SerialNumber: serial,
		Subject:      certificate.Subject,
		Issuer:       certificate.Issuer,
		NotBefore:    certificate.NotBefore,
		NotAfter:     certificate.NotAfter,
		PublicKey:    publicKey,
	}
}
