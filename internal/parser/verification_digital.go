package parser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/internal/ses"
	"github.com/zc310/ofd/internal/signeddata"
	"github.com/zc310/ofd/pkg/spec"
)

// VerifyDigitalSignedValue 验证 GB/T 35275 SM2/SM3 SignedData 对完整文件字节的绑定。
// 数学验证与完整结论分开；未提供信任或吊销证据时结果为 incomplete。
func VerifyDigitalSignedValue(original, value []byte, options *SignatureVerificationOptions) (*SignatureVerificationResult, error) {
	if options == nil {
		options = &SignatureVerificationOptions{}
	}
	now := options.CurrentTime
	if now.IsZero() {
		now = time.Now()
	}
	result := &SignatureVerificationResult{Kind: spec.SigTypeSign, Validation: &SignatureValidation{}}
	v := result.Validation
	proof, err := signeddata.Verify(value, original, options.UID)
	if err != nil {
		status := "invalid"
		if errors.Is(err, signeddata.ErrUnsupported) {
			status = "incomplete"
		}
		v.set("signer.signature", status, err.Error())
		return result, nil
	}
	result.Valid = true
	result.Outer.Valid = true
	result.Outer.Algorithm = sm2WithSM3OID
	result.Outer.SignatureFormat = "GB/T 35275 SignedData"
	result.Outer.Certificate = certificateInfo(proof.Signer)
	v.check("signer.signature", nil)
	v.check("document.binding", nil)
	at := proof.SigningTime
	if at.IsZero() && proof.Timestamp != nil {
		at = proof.Timestamp.Time
	}
	if at.IsZero() {
		// 无可信历史时间时只按本次验证时刻判证书有效期。
		at = now
	}
	result.VerificationTime = at
	if at.After(now) {
		v.set("signing.time", "invalid", "签名时间晚于验证时刻")
	} else {
		v.check("signing.time", nil)
	}
	result.Outer.CertificateValid = ses.ValidateSigningCertificate(proof.Signer, at) == nil
	v.check("signer.certificate_usage", ses.ValidateSigningCertificate(proof.Signer, at))
	embedded := make([][]byte, 0, len(proof.Certificates))
	for _, cert := range proof.Certificates {
		embedded = append(embedded, cert.Raw)
	}
	validateDigitalEvidence(v, "certificates", proof.Signer, embedded, at, now, options.Trust, options.Revocation)
	for _, check := range v.Checks {
		if check.Code == "certificates.trust" {
			result.TrustChecked = options.Trust != nil && options.Trust.Roots != nil
			result.Trusted = check.Status == "valid"
		}
		if check.Code == "certificates.revocation" {
			result.RevocationChecked = options.Revocation != nil
			result.RevocationValid = check.Status == "valid"
		}
	}
	result.Outer.TrustChecked, result.Outer.Trusted = result.TrustChecked, result.Trusted
	result.Outer.RevocationChecked = result.RevocationChecked
	if result.RevocationValid {
		result.Outer.RevocationStatus = "good"
	}
	if proof.TimestampError != nil {
		v.set("timestamp", "invalid", proof.TimestampError.Error())
	} else if proof.Timestamp == nil {
		v.set("timestamp", "not_applicable", "未附加时间戳")
	} else if proof.Timestamp.Time.Before(at) || proof.Timestamp.Time.After(now) {
		v.set("timestamp", "invalid", "签名、时间戳与验证时刻顺序无效")
	} else {
		v.check("timestamp.binding", nil)
		trust := options.TimestampTrust
		if trust == nil {
			trust = options.Trust
		}
		pool := smx509.NewCertPool()
		for _, cert := range proof.Timestamp.Certificates {
			pool.AddCert(cert)
		}
		if trust != nil {
			for _, der := range trust.Intermediates {
				cert, err := smx509.ParseCertificate(der)
				if err != nil {
					v.set("timestamp", "invalid", err.Error())
					return result, nil
				}
				pool.AddCert(cert)
			}
		}
		if trust == nil || trust.Roots == nil {
			v.set("timestamp", "incomplete", "缺少时间戳信任根")
		} else {
			chains, err := proof.Timestamp.Signer.Verify(smx509.VerifyOptions{Roots: trust.Roots, Intermediates: pool, CurrentTime: proof.Timestamp.Time, KeyUsages: []smx509.ExtKeyUsage{smx509.ExtKeyUsageTimeStamping}})
			if err != nil {
				v.set("timestamp", "invalid", err.Error())
			} else if options.Revocation == nil {
				v.set("timestamp", "incomplete", "缺少时间戳证书吊销证据")
			} else {
				status, message := certificateChainRevocation(chains, options.Revocation, proof.Timestamp.Time, now)
				setEvidence(v, "timestamp", status, message)
			}
		}
		// 时间戳时刻也验证签名者证书，避免签名后、时间戳前已吊销的证书被接受。
		v.check("timestamp.signer_usage", ses.ValidateSigningCertificate(proof.Signer, proof.Timestamp.Time))
		validateDigitalEvidence(v, "timestamp.signer", proof.Signer, embedded, proof.Timestamp.Time, now, options.Trust, options.Revocation)
	}
	return result, nil
}

// VerifyDocumentSignature 按签名列表 Type 分派，禁止把普通 SignedData 当 SES 验证。
func VerifyDocumentSignature(ctx context.Context, pkg *core.Package, signature *models.Signature, value *SignedValue, options *SignatureVerificationOptions) (*SignatureVerificationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if signature.Type == "" || signature.Type == spec.SigTypeSeal {
		result, err := VerifySESSignedValueWithOptions(value, options)
		if err == nil {
			result.Kind = spec.SigTypeSeal
			digest, digestErr := VerifySignatureDigest(pkg, signature.FilePath, signature, value)
			if digestErr != nil {
				return nil, digestErr
			}
			BindSignatureDigest(result, digest)
		}
		return result, err
	}
	if signature.Type != spec.SigTypeSign {
		return nil, fmt.Errorf("未支持的签名类型 %s", signature.Type)
	}
	seenEntries := map[string]bool{}
	if err := pkg.WalkEntries(func(entry core.Entry) bool {
		name := strings.TrimSuffix(entry.Name, "/")
		key := strings.ToLower(name)
		if core.ValidateEntryName(name) != nil || strings.Contains(name, ":") || seenEntries[key] {
			seenEntries[""] = true
		}
		seenEntries[key] = true
		return !seenEntries[""] && ctx.Err() == nil
	}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if seenEntries[""] {
		return nil, fmt.Errorf("数字签名容器路径重复或无效")
	}
	data, err := readDigitalEntry(pkg, signature.FilePath)
	if err != nil {
		return nil, err
	}
	var checked models.Signature
	if err := securexml.Decode(ctx, data, "Signature", &checked); err != nil {
		return nil, err
	}
	if checked.SignedInfo.SignatureMethod != sm2WithSM3OID || (checked.SignedInfo.References.CheckMethod != "SM3" && checked.SignedInfo.References.CheckMethod != "1.2.156.10197.1.401") {
		return nil, fmt.Errorf("普通数字签名仅支持 SM2-with-SM3 和 SM3 引用摘要")
	}
	if value == nil {
		return nil, fmt.Errorf("缺少数字签名值")
	}
	valuePath, err := securexml.Resolve(signature.FilePath, checked.SignedValue.String())
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, ref := range checked.SignedInfo.References.Reference {
		name, err := securexml.Resolve(signature.FilePath, ref.FileRef.String())
		if err != nil || seen[name] || name == signature.FilePath || name == valuePath {
			return nil, fmt.Errorf("数字签名保护范围包含重复、越界或本次签名自身")
		}
		seen[name] = true
		if entry, exists := pkg.Lookup(name); exists && entry.Path != name {
			return nil, fmt.Errorf("数字签名引用不得使用大小写路径回退")
		}
	}
	result, err := VerifyDigitalSignedValue(data, value.Raw, options)
	if err != nil {
		return nil, err
	}
	digest, err := VerifySignatureDigest(pkg, signature.FilePath, &checked, nil)
	if err != nil {
		return nil, err
	}
	BindDigitalReferences(result, digest)
	return result, ctx.Err()
}

func readDigitalEntry(pkg *core.Package, name string) ([]byte, error) {
	entry, ok := pkg.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("签名条目不存在: %s", name)
	}
	if entry.Path != strings.TrimPrefix(name, "/") {
		return nil, fmt.Errorf("数字签名条目不得使用大小写路径回退")
	}
	if entry.UncompressedSize > signeddata.MaxBytes {
		return nil, fmt.Errorf("签名条目超过 4 MiB")
	}
	r, err := pkg.OpenEntry(entry)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, signeddata.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > signeddata.MaxBytes {
		return nil, fmt.Errorf("签名条目超过 4 MiB")
	}
	return data, nil
}

func setEvidence(v *SignatureValidation, code, status, message string) {
	switch status {
	case "good":
		v.check(code, nil)
	case "revoked", "error":
		v.set(code, "invalid", message)
	default:
		v.set(code, "incomplete", message)
	}
}

func validateDigitalEvidence(v *SignatureValidation, code string, certificate *smx509.Certificate, embedded [][]byte, at, now time.Time, trust *CertificateTrustOptions, revocation *CertificateRevocationOptions) {
	chains := [][]*smx509.Certificate{{certificate}}
	if trust == nil || trust.Roots == nil {
		v.set(code+".trust", "incomplete", "缺少证书信任根")
	} else {
		var err error
		chains, err = certificateChains(certificate, trust, at, embedded)
		v.check(code+".trust", err)
		if err != nil {
			return
		}
	}
	if revocation == nil {
		v.set(code+".revocation", "incomplete", "缺少证书吊销证据")
		return
	}
	status, message := certificateChainRevocation(chains, revocation, at, now)
	setEvidence(v, code+".revocation", status, message)
}

// RequireTimestamp 用于 GM/T 0099 §7.4.2：清单必须带时间戳。
func RequireTimestamp(result *SignatureVerificationResult) {
	if result == nil || result.Validation == nil {
		return
	}
	for _, check := range result.Validation.Checks {
		if check.Code == "timestamp" && check.Status == "not_applicable" {
			result.Validation.set("timestamp", "invalid", "完整性清单签名必须附加时间戳（GM/T 0099 §7.4.2）")
			return
		}
	}
}

// BindDigitalReferences 将普通数字签名与 References 检查结果合并。
func BindDigitalReferences(result *SignatureVerificationResult, digest *SignatureDigestResult) {
	if result == nil || result.Validation == nil {
		return
	}
	if digest == nil || len(digest.References) == 0 {
		result.Validation.set("document.integrity", "invalid", "签名必须覆盖至少一个文件")
		return
	}
	if digest.Unsupported != "" {
		result.Validation.set("document.integrity", "incomplete", digest.Unsupported)
		return
	}
	var err error
	if !digest.Valid {
		err = fmt.Errorf("签名引用摘要不匹配或不可读取")
	}
	result.Validation.check("document.integrity", err)
}
