package contract

import "github.com/zc310/ofd/pkg/reader"

// Signatures 将阅读器的签名信息归一为浏览器和桌面共用的字段契约。
func Signatures(infos []reader.SignatureInfo) []map[string]any {
	result := make([]map[string]any, 0, len(infos))
	for _, info := range infos {
		stamps := make([]map[string]any, 0, len(info.Stamps))
		for _, stamp := range info.Stamps {
			stamps = append(stamps, map[string]any{"page": stamp.Page, "id": stamp.ID, "has_seal": stamp.HasSeal, "seal_type": stamp.SealType, "boundary": stamp.Boundary, "visible_boundary": stamp.VisibleBoundary})
		}
		references := make([]map[string]any, 0, len(info.References))
		for _, ref := range info.References {
			references = append(references, map[string]any{"file_ref": ref.FileRef, "exists": ref.Exists, "match": ref.Match, "error": ref.Error})
		}
		certificates := make([]map[string]any, 0, len(info.Certificates))
		for _, cert := range info.Certificates {
			certificates = append(certificates, map[string]any{
				"slot": cert.Slot, "slot_key": cert.SlotKey, "subject": cert.Subject, "issuer": cert.Issuer,
				"common_name": cert.CommonName, "organization": cert.Organization, "organizational_unit": cert.OrganizationalUnit,
				"country": cert.Country, "locality": cert.Locality, "province": cert.Province, "serial_number": cert.SerialNumber,
				"not_before": cert.NotBefore, "not_after": cert.NotAfter, "public_key": cert.PublicKey,
				"algorithm": cert.Algorithm, "signature_format": cert.SignatureFormat, "signature_valid": cert.SignatureValid,
				"certificate_valid": cert.CertificateValid, "trust_checked": cert.TrustChecked, "trusted": cert.Trusted,
				"trust_error": cert.TrustError, "revocation_checked": cert.RevocationChecked, "revocation_status": cert.RevocationStatus,
				"revocation_error": cert.RevocationError, "error": cert.Error,
			})
		}
		value := map[string]any{
			"scope": info.Scope, "id": info.ID, "provider": info.Provider, "company": info.Company, "version": info.Version,
			"method": info.Method, "date": info.Date, "has_digest": info.HasDigest, "digest_valid": info.DigestValid,
			"digest_method": info.DigestMethod, "has_verification": info.HasVerification, "verified": info.Verified,
			"trusted": info.Trusted, "trust_checked": info.TrustChecked, "verification_error": info.VerificationError,
			"has_data_hash": info.HasDataHash, "data_hash_match": info.DataHashMatch,
			"stamps": stamps, "references": references, "certificates": certificates,
		}
		if info.Validation != nil {
			checks := make([]map[string]any, 0, len(info.Validation.Checks))
			for _, check := range info.Validation.Checks {
				checks = append(checks, map[string]any{"code": check.Code, "status": check.Status, "message": check.Message})
			}
			value["validation"] = map[string]any{"status": info.Validation.Status, "checks": checks}
		}
		result = append(result, value)
	}
	return result
}
