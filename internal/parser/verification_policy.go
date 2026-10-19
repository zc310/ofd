package parser

import (
	"fmt"
	"time"

	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/ses"
)

// VerificationCheck 是可报告、可独立定位的标准验证步骤。
type VerificationCheck struct {
	Code    string `json:"code"`
	Status  string `json:"status"` // valid、invalid、incomplete、not_applicable
	Message string `json:"message,omitempty"`
}

// SignatureValidation 将缺少证据与验证失败分开。只有全部必需步骤完成且通过，
// Status 才为 valid；不能用两层签名的数学正确性替代 §7.3 的完整结论。
type SignatureValidation struct {
	Status string              `json:"status"`
	Checks []VerificationCheck `json:"checks"`
}

func (v *SignatureValidation) set(code, status, message string) {
	check := VerificationCheck{Code: code, Status: status, Message: message}
	for i := range v.Checks {
		if v.Checks[i].Code == code {
			v.Checks[i] = check
			v.finish()
			return
		}
	}
	v.Checks = append(v.Checks, check)
	v.finish()
}

func (v *SignatureValidation) finish() {
	v.Status = "valid"
	for _, check := range v.Checks {
		if check.Status == "invalid" {
			v.Status = "invalid"
			return
		}
		if check.Status == "incomplete" {
			v.Status = "incomplete"
		}
	}
}

func (v *SignatureValidation) check(code string, err error) {
	if err != nil {
		v.set(code, "invalid", err.Error())
	} else {
		v.set(code, "valid", "")
	}
}

func validateSES(value *SESSignedValue, result *SignatureVerificationResult, options *SignatureVerificationOptions, trust *CertificateTrustOptions, now time.Time) *SignatureValidation {
	v := result.Validation
	seal := value.TBS.Seal
	info := seal.SealInfo
	var formatErr error
	if info.Header.ID != "ES" || info.Header.Version != 4 || value.TBS.Version != info.Header.Version {
		formatErr = fmt.Errorf("印章须使用 ES/v4，签章版本须与印章一致（§6.1.2.2、§7.1.2）")
	} else if info.ESID == "" || info.Header.VID == "" || info.Property.Name == "" || info.Picture.Width <= 0 || info.Picture.Height <= 0 || len(info.Picture.Data) == 0 {
		formatErr = fmt.Errorf("印章必要字段或毫米显示尺寸无效")
	} else if value.Signature.UnusedBits != 0 || seal.Signature.UnusedBits != 0 || value.TBS.DataHash.UnusedBits != 0 {
		formatErr = fmt.Errorf("签名值与摘要必须按完整字节编码")
	}
	var extensionErr error
	for _, extension := range append(append([]ExtensionData(nil), info.Extensions...), value.TBS.Extensions...) {
		// §6.1.2.6 定义 DEFAULT FALSE；DER（X.690 §11.5）禁止
		// 显式编码默认值。999.ofd 含此非规范编码，保留解析以供诊断。
		if extension.DefaultEncoded {
			formatErr = fmt.Errorf("扩展 %s 的 DER 默认值 FALSE 必须省略", extension.OID)
		}
		if extension.Critical {
			extensionErr = fmt.Errorf("不支持的关键扩展 %s", extension.OID)
		}
	}
	v.check("ses.format", formatErr)
	if extensionErr == nil {
		v.check("ses.extensions", nil)
	} else {
		v.set("ses.extensions", "incomplete", extensionErr.Error())
	}
	for _, component := range []struct {
		code   string
		result SignatureComponentResult
	}{{"seal.signature", result.Seal}, {"signer.signature", result.Outer}} {
		if component.result.Algorithm != sm2WithSM3OID {
			v.set(component.code, "incomplete", "未支持的签名算法 "+component.result.Algorithm)
		} else if !component.result.Valid {
			v.set(component.code, "invalid", component.result.Error)
		} else {
			v.check(component.code, nil)
		}
	}
	v.check("signer.authorization", ses.CertificateAuthorized(info.Property.CertificateType, info.Property.CertificateListDER, value.Certificate))
	var periodErr error
	if !info.Property.ValidTo.After(info.Property.ValidFrom) || value.TBS.SignTime.Before(info.Property.ValidFrom) || value.TBS.SignTime.After(info.Property.ValidTo) || info.Property.CreateTime.After(value.TBS.SignTime) {
		periodErr = fmt.Errorf("印章在签章时刻无效，或制作时间晚于签章时间（§6.3、§7.3 d）")
	}
	v.check("seal.validity", periodErr)
	for _, component := range []struct {
		code        string
		certificate []byte
	}{{"seal.certificate_usage", seal.Certificate}, {"signer.certificate_usage", value.Certificate}} {
		certificate, err := smx509.ParseCertificate(component.certificate)
		if err == nil {
			err = ses.ValidateSigningCertificate(certificate, value.TBS.SignTime)
		}
		v.check(component.code, err)
	}
	validateCertificateEvidence(v, value, options, trust, now)
	if options == nil || options.SealStatus == nil {
		v.set("seal.status", "incomplete", "未提供印章状态证据")
	} else {
		status, err := options.SealStatus(info.ESID, value.TBS.SignTime)
		switch {
		case err != nil:
			v.set("seal.status", "incomplete", err.Error())
		case status == "good":
			v.check("seal.status", nil)
		case status == "revoked":
			v.set("seal.status", "invalid", "印章在签章时刻已吊销")
		default:
			v.set("seal.status", "incomplete", "印章状态未知")
		}
	}
	validateSESTimestamp(v, value, options, trust, now)
	v.set("document.integrity", "incomplete", "尚未绑定 OFD 引用摘要与原文保护范围")
	return v
}

func validateCertificateEvidence(v *SignatureValidation, value *SESSignedValue, options *SignatureVerificationOptions, trust *CertificateTrustOptions, now time.Time) {
	if trust == nil || trust.Roots == nil {
		v.set("certificates.trust", "incomplete", "未提供证书信任根")
	} else {
		v.check("certificates.trust", nil)
	}
	if options == nil || options.Revocation == nil {
		v.set("certificates.revocation", "incomplete", "缺少可验证的证书吊销状态证据")
	} else {
		v.check("certificates.revocation", nil)
	}
	for _, der := range [][]byte{value.TBS.Seal.Certificate, value.Certificate} {
		certificate, err := smx509.ParseCertificate(der)
		if err != nil {
			v.set("certificates.trust", "invalid", err.Error())
			continue
		}
		chains := [][]*smx509.Certificate{{certificate}}
		if trust != nil && trust.Roots != nil {
			chains, err = certificateChains(certificate, trust, value.TBS.SignTime, value.TBS.Seal.SealInfo.Property.Certificates)
			if err != nil {
				v.set("certificates.trust", "invalid", err.Error())
				continue
			}
		}
		if options == nil || options.Revocation == nil {
			continue
		}
		status, message := certificateChainRevocation(chains, options.Revocation, value.TBS.SignTime, now)
		if status != "good" {
			if status == "revoked" || status == "error" {
				v.set("certificates.revocation", "invalid", message)
			} else {
				// 不覆盖另一层证书已经确认的吊销/无效结果。
				failed := false
				for _, c := range v.Checks {
					if c.Code == "certificates.revocation" && c.Status == "invalid" {
						failed = true
					}
				}
				if !failed {
					v.set("certificates.revocation", "incomplete", message)
				}
			}
		}
	}
}

func certificateChainRevocation(chains [][]*smx509.Certificate, options *CertificateRevocationOptions, at, now time.Time) (string, string) {
	status, message := "unknown", "缺少证书链的吊销证据"
	for _, chain := range chains {
		var issuers [][]byte
		for _, cert := range chain {
			issuers = append(issuers, cert.Raw)
		}
		good := true
		for i, cert := range chain {
			if i > 0 && i == len(chain)-1 {
				break
			} // 不要求信任锚自行出具吊销证明。
			s, m := verifyCertificateRevocationAt(cert, options, at, now, issuers)
			if s != "good" {
				good = false
				if status != "revoked" && status != "error" {
					status, message = s, m
				}
			}
		}
		if good {
			return "good", ""
		}
	}
	return status, message
}

// BindSignatureDigest 将 §7.3 g) 的原文验证接入完整结果；不改变原有 Valid 字段。
func BindSignatureDigest(result *SignatureVerificationResult, digest *SignatureDigestResult) {
	if result == nil || result.Validation == nil {
		return
	}
	v := result.Validation
	if digest == nil {
		v.set("document.integrity", "incomplete", "缺少 SES 原文摘要结果")
		return
	}
	if digest.SignatureMethodError != "" {
		v.set("document.integrity", "invalid", digest.SignatureMethodError)
		return
	}
	for _, reference := range digest.References {
		if !reference.Match {
			// 未支持算法时没有执行摘要比较，不能把零值 Match 当成不匹配。
			if digest.Unsupported != "" && reference.Actual == nil &&
				(reference.Error == "" || reference.Error == digest.Unsupported) {
				continue
			}
			v.set("document.integrity", "invalid", "OFD 引用摘要不匹配或不可读取")
			return
		}
	}
	if len(digest.References) == 0 {
		v.set("document.integrity", "invalid", "OFD 签名保护范围不含任何文件引用")
		return
	}
	// §7.3 g)：已确认的原文错误优先于算法未支持或范围尚未确认。
	if item := digest.DataHash; item != nil && !item.Match &&
		(digest.Unsupported == "" || item.Actual != nil || (item.Error != "" && item.Error != digest.Unsupported)) {
		v.set("document.integrity", "invalid", "SES 原文摘要不匹配或不可读取")
		return
	}
	if digest.Unsupported != "" {
		v.set("document.integrity", "incomplete", digest.Unsupported)
		return
	}
	if digest.DataHash == nil {
		v.set("document.integrity", "incomplete", "缺少 SES 原文摘要结果")
		return
	}
	if !digest.Valid || !digest.DataHash.Match {
		v.set("document.integrity", "invalid", "SES 原文摘要不匹配或不可读取")
		return
	}
	if !digest.DataHash.ScopeMatch {
		v.set("document.integrity", "incomplete", digest.DataHash.ScopeError)
		return
	}
	v.check("document.integrity", nil)
}
