package parser

import (
	"time"

	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/ses"
)

func validateSESTimestamp(v *SignatureValidation, value *SESSignedValue, options *SignatureVerificationOptions, trust *CertificateTrustOptions, now time.Time) {
	if value.TimeStamp == nil {
		v.set("timestamp", "not_applicable", "未附加时间戳")
		return
	}
	if value.TimeStamp.UnusedBits != 0 {
		v.set("timestamp", "invalid", "时间戳须按完整字节编码")
		return
	}
	proof, err := ses.CheckTimestamp(value.TimeStamp.Bytes, value.Signature.Bytes)
	if err != nil {
		v.set("timestamp", "invalid", err.Error())
		return
	}
	if value.TBS.SignTime.After(proof.Time) || proof.Time.After(now) {
		v.set("timestamp", "invalid", "签章、时间戳与验证时刻顺序无效")
		return
	}
	validateTimestampSigningTime(v, value, options, trust, proof.Time, now)
	if options != nil && options.TimestampTrust != nil {
		trust = options.TimestampTrust
	}
	if trust == nil || trust.Roots == nil {
		v.set("timestamp", "incomplete", "未提供时间戳信任根")
		return
	}
	intermediates := smx509.NewCertPool()
	for _, certificate := range proof.Certificates {
		intermediates.AddCert(certificate)
	}
	for _, data := range trust.Intermediates {
		certificate, err := smx509.ParseCertificate(data)
		if err != nil {
			v.set("timestamp", "invalid", err.Error())
			return
		}
		intermediates.AddCert(certificate)
	}
	chains, err := proof.Signer.Verify(smx509.VerifyOptions{Roots: trust.Roots, Intermediates: intermediates, CurrentTime: proof.Time, KeyUsages: []smx509.ExtKeyUsage{smx509.ExtKeyUsageTimeStamping}})
	if err != nil {
		v.set("timestamp", "invalid", err.Error())
		return
	}
	if options == nil || options.Revocation == nil {
		v.set("timestamp", "incomplete", "未提供时间戳证书吊销证据")
		return
	}
	status, message := certificateChainRevocation(chains, options.Revocation, proof.Time, now)
	switch status {
	case "good":
		v.check("timestamp", nil)
	case "revoked", "error":
		v.set("timestamp", "invalid", message)
	default:
		v.set("timestamp", "incomplete", message)
	}
}

// GB/T 38540-2020 §7.3 h) 4) 引用 f)：时间戳时刻也必须处于
// 签章者证书有效期内，且该证书在此时未被吊销。TSA 证书另行检查。
func validateTimestampSigningTime(v *SignatureValidation, value *SESSignedValue, options *SignatureVerificationOptions, trust *CertificateTrustOptions, at, now time.Time) {
	const code = "timestamp.signing_time"
	certificate, err := smx509.ParseCertificate(value.Certificate)
	if err == nil {
		err = ses.ValidateSigningCertificate(certificate, at)
	}
	if err != nil {
		v.set(code, "invalid", "时间戳时刻的签章者证书无效："+err.Error())
		return
	}
	if options == nil || options.Revocation == nil {
		v.set(code, "incomplete", "缺少时间戳时刻的签章者证书吊销证据")
		return
	}
	issuers := append([][]byte(nil), value.TBS.Seal.SealInfo.Property.Certificates...)
	if trust != nil && trust.Roots != nil {
		// 信任链仍以实际签章时刻构建；这里只复用其签发者来按 f) 判定
		// 签章者证书在时间戳时刻的吊销状态，不改变其他证书的验证时刻。
		chains, err := certificateChains(certificate, trust, value.TBS.SignTime, issuers)
		if err != nil {
			v.set(code, "invalid", err.Error())
			return
		}
		for _, chain := range chains {
			for _, issuer := range chain {
				issuers = append(issuers, issuer.Raw)
			}
		}
	}
	status, message := verifyCertificateRevocationAt(certificate, options.Revocation, at, now, issuers)
	switch status {
	case "good":
		v.check(code, nil)
	case "revoked", "error":
		v.set(code, "invalid", "时间戳时刻的签章者证书无效："+message)
	default:
		v.set(code, "incomplete", message)
	}
}
