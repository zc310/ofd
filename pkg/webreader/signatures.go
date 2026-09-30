package webreader

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// SignatureStamp 描述签名关联的一个签章位置。
type SignatureStamp struct {
	// Page 是签章所在页面的全局索引，-1 表示无法解析。
	Page int
	// ID 是签章标识。
	ID string
	// Boundary 是签章边界（毫米），未声明时为 nil。
	Boundary *AnnotationBoundary
	// HasSeal 表示是否提取到印章数据。
	HasSeal bool
	// SealType 是印章文件类型（如 png、jpg、ofd），无印章时为空。
	SealType string
}

// CertificateDetail 描述签名某一层（印章/外层）的证书与验证明细。
type CertificateDetail struct {
	// Slot 是所属层级：印章或外层。
	Slot string
	// SlotKey 是层级的机器可读标识：seal 或 outer，用于导出证书。
	SlotKey string
	// Subject、Issuer 是证书主体与签发者。
	Subject string
	Issuer  string
	// CommonName、Organization、OrganizationalUnit、Country、Locality、Province 是证书主体字段。
	CommonName         string
	Organization       string
	OrganizationalUnit string
	Country            string
	Locality           string
	Province           string
	// SerialNumber 是证书序列号。
	SerialNumber string
	// NotBefore、NotAfter 是证书有效期（RFC3339）。
	NotBefore string
	NotAfter  string
	// PublicKey 是公钥算法名称。
	PublicKey string
	// Algorithm、SignatureFormat 是签名算法与编码格式。
	Algorithm       string
	SignatureFormat string
	// SignatureValid 表示该层签名通过公钥验证。
	SignatureValid bool
	// CertificateValid 表示证书在签名时间点有效。
	CertificateValid bool
	// TrustChecked、Trusted、TrustError 是证书链校验结果。
	TrustChecked bool
	Trusted      bool
	TrustError   string
	// RevocationChecked、RevocationStatus、RevocationError 是吊销校验结果。
	RevocationChecked bool
	RevocationStatus  string
	RevocationError   string
	// Error 是验证失败原因。
	Error string
}

// SignatureReference 描述签名覆盖的一个文件引用及其摘要校验结果。
type SignatureReference struct {
	// FileRef 是签名中声明的文件引用路径。
	FileRef string
	// Exists 表示引用的文件是否存在于包中。
	Exists bool
	// Match 表示摘要是否一致。
	Match bool
	// Error 是校验失败原因。
	Error string
}

// SignatureInfo 描述文档中的一个签名及其校验结果。
type SignatureInfo struct {
	// Scope 是签名所属文档体的索引。
	Scope int
	// ID 是签名标识。
	ID string
	// Provider、Company、Version 是签名提供者信息。
	Provider string
	Company  string
	Version  string
	// Method 是签名算法标识（通常是 OID），未声明时为空。
	Method string
	// Date 是签名时间，保留原始文本。
	Date string
	// HasDigest、DigestValid 和 DigestMethod 是摘要校验结果。
	HasDigest    bool
	DigestValid  bool
	DigestMethod string
	// HasVerification、Verified、Trusted、TrustChecked 是验签结果。
	HasVerification bool
	Verified        bool
	Trusted         bool
	TrustChecked    bool
	// VerificationError 是验签错误信息，无错误时为空。
	VerificationError string
	// Stamps 是签名关联的签章位置。
	Stamps []SignatureStamp
	// Certificates 是印章与外层两层的证书与验证明细。
	Certificates []CertificateDetail
	// References 是签名覆盖的文件引用及逐项摘要校验结果。
	References []SignatureReference
	// HasDataHash、DataHashMatch 是签名数据摘要（Signature.xml）校验结果。
	HasDataHash   bool
	DataHashMatch bool
}

// DocumentStats 汇总文档声明的资源数量（不读取资源内容）。
type DocumentStats struct {
	// Fonts 是声明字体的数量。
	Fonts int
	// Attachments 是附件数量。
	Attachments int
	// Media 是多媒资资源数量。
	Media int
	// AnnotationPages 是声明了注解的页面数量。
	AnnotationPages int
	// Signatures 是签名数量。
	Signatures int
}

func itemID(item models.PageItem) string {
	switch item.Kind {
	case models.PageItemText:
		if item.Text != nil {
			return formatStID(item.Text.ID)
		}
	case models.PageItemPath:
		if item.Path != nil {
			return formatStID(item.Path.ID)
		}
	case models.PageItemImage:
		if item.Image != nil {
			return formatStID(item.Image.ID)
		}
	case models.PageItemComposite:
		if item.Composite != nil {
			return formatStID(item.Composite.ID)
		}
	}
	return ""
}

func formatStID(id models.StID) string {
	return strconv.FormatUint(uint64(id), 10)
}

// pageItemGraphicUnit 返回页面对象携带的通用图形属性；嵌套 PageBlock 返回 nil，
// 由调用方递归处理其子项。
func pageItemGraphicUnit(item models.PageItem) *models.CTGraphicUnit {
	switch item.Kind {
	case models.PageItemText:
		if item.Text != nil {
			return &item.Text.CTGraphicUnit
		}
	case models.PageItemPath:
		if item.Path != nil {
			return &item.Path.CTGraphicUnit
		}
	case models.PageItemImage:
		if item.Image != nil {
			return &item.Image.CTGraphicUnit
		}
	case models.PageItemComposite:
		if item.Composite != nil {
			return &item.Composite.CTGraphicUnit
		}
	}
	return nil
}

// Signatures 返回所有文档体的签名及其摘要/验签结果。
func (r *Reader) Signatures() ([]SignatureInfo, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil {
		return nil, nil
	}
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata.signaturesOnce.Do(func() {
		r.metadata.signaturesVal, r.metadata.signaturesErr = r.computeSignatures()
	})
	return r.metadata.signaturesVal, r.metadata.signaturesErr
}

func (r *Reader) computeSignatures() ([]SignatureInfo, error) {
	pageIndex := make(map[models.StID]int, len(r.pages))
	for index, ref := range r.pages {
		if ref.page != nil {
			pageIndex[ref.page.ID] = index
		}
	}
	infos := make([]SignatureInfo, 0)
	for scope, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		document.ForEachSignature(func(id string, signature *models.Signature) bool {
			if signature == nil {
				return true
			}
			info := SignatureInfo{
				Scope:    scope,
				ID:       id,
				Provider: signature.SignedInfo.Provider.ProviderName,
				Company:  signature.SignedInfo.Provider.Company,
				Version:  signature.SignedInfo.Provider.Version,
				Method:   signature.SignedInfo.SignatureMethod,
				Date:     signature.SignedInfo.SignatureDateTime,
			}
			if digest := document.GetDigestResult(id); digest != nil {
				info.HasDigest = true
				info.DigestValid = digest.Valid
				info.DigestMethod = digest.Method
				for _, reference := range digest.References {
					info.References = append(info.References, SignatureReference{
						FileRef: reference.FileRef,
						Exists:  reference.Exists,
						Match:   reference.Match,
						Error:   reference.Error,
					})
				}
				if digest.DataHash != nil {
					info.HasDataHash = true
					info.DataHashMatch = digest.DataHash.Match
				}
			}
			if verification := document.GetVerificationResult(id); verification != nil {
				info.HasVerification = true
				info.Verified = verification.Valid
				info.Trusted = verification.Trusted
				info.TrustChecked = verification.TrustChecked
				info.Certificates = signatureCertificates(verification)
			}
			if verificationErr := document.GetVerificationError(id); verificationErr != nil {
				info.VerificationError = verificationErr.Error()
			}
			for _, annot := range signature.SignedInfo.StampAnnot {
				if annot == nil {
					continue
				}
				stamp := SignatureStamp{ID: annot.ID, Page: -1, Boundary: boxValue(annot.Boundary)}
				pageID := models.StID(annot.PageRef)
				if index, ok := pageIndex[pageID]; ok {
					stamp.Page = index
				}
				for _, seal := range document.GetSeals(pageID) {
					if seal == nil || seal.StampAnnot == nil || seal.SealData == nil {
						continue
					}
					if seal.StampAnnot.ID == annot.ID {
						stamp.HasSeal = true
						stamp.SealType = seal.SealData.FileType
						break
					}
				}
				info.Stamps = append(info.Stamps, stamp)
			}
			infos = append(infos, info)
			return true
		})
	}
	return infos, nil
}

// SignatureSeal 返回指定签名第 stampIndex 个签章的印章文件内容与类型。
func (r *Reader) SignatureSeal(scope int, signatureID string, stampIndex int) ([]byte, string, error) {
	if r == nil {
		return nil, "", errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, "", errors.New("文档引擎已经关闭")
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, "", fmt.Errorf("签名作用域超出范围: %d", scope)
	}
	document := r.ofd.Documents[scope]
	if document == nil {
		return nil, "", errors.New("签名所属文档为空")
	}
	signature := document.GetSignature(signatureID)
	if signature == nil {
		return nil, "", fmt.Errorf("签名不存在: %s", signatureID)
	}
	if stampIndex < 0 || stampIndex >= len(signature.SignedInfo.StampAnnot) {
		return nil, "", fmt.Errorf("签章索引超出范围: %d", stampIndex)
	}
	annot := signature.SignedInfo.StampAnnot[stampIndex]
	if annot == nil {
		return nil, "", errors.New("签章为空")
	}
	for _, seal := range document.GetSeals(models.StID(annot.PageRef)) {
		if seal == nil || seal.StampAnnot == nil || seal.SealData == nil {
			continue
		}
		if seal.StampAnnot.ID == annot.ID {
			if int64(len(seal.SealData.Data)) > maxAttachmentBytesHard {
				return nil, "", fmt.Errorf("印章数据超过大小限制 %d MB", maxAttachmentBytesHard>>20)
			}
			return append([]byte(nil), seal.SealData.Data...), seal.SealData.FileType, nil
		}
	}
	return nil, "", errors.New("签章数据不存在")
}

// Stats 汇总文档声明的资源数量，只读取声明，不加载资源内容。
func (r *Reader) Stats() (DocumentStats, error) {
	if r == nil {
		return DocumentStats{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return DocumentStats{}, errors.New("文档引擎已经关闭")
	}
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata.statsOnce.Do(func() {
		r.metadata.statsVal, r.metadata.statsErr = r.computeStats()
	})
	return r.metadata.statsVal, r.metadata.statsErr
}

func (r *Reader) computeStats() (DocumentStats, error) {
	stats := DocumentStats{}
	if r.ofd == nil {
		return stats, nil
	}
	fonts := make(map[string]struct{})
	for documentIndex, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		document.ForEachFont(func(id models.StID, font *models.Font) bool {
			fonts[fmt.Sprintf("%d:%d", documentIndex, id)] = struct{}{}
			return true
		})
		document.ForEachMedia(func(id models.StID, media *models.MultiMedia) bool {
			stats.Media++
			return true
		})
		document.ForEachSignature(func(id string, signature *models.Signature) bool {
			stats.Signatures++
			return true
		})
		if list := document.GetAttachments(); list != nil {
			stats.Attachments += len(list.Attachments)
		}
		stats.AnnotationPages += document.AnnotationPageCount()
	}
	stats.Fonts = len(fonts)
	return stats, nil
}

// SignatureCertificate 返回指定签名某一层（seal/outer）证书的 DER 内容。
func (r *Reader) SignatureCertificate(scope int, signatureID string, slot string) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, fmt.Errorf("签名作用域超出范围: %d", scope)
	}
	document := r.ofd.Documents[scope]
	if document == nil {
		return nil, errors.New("签名所属文档为空")
	}
	verification := document.GetVerificationResult(signatureID)
	if verification == nil {
		return nil, fmt.Errorf("签名没有验证结果: %s", signatureID)
	}
	var component parser.SignatureComponentResult
	switch strings.ToLower(strings.TrimSpace(slot)) {
	case "seal", "印章":
		component = verification.Seal
	case "outer", "外层":
		component = verification.Outer
	default:
		return nil, fmt.Errorf("未知证书层级: %s", slot)
	}
	if component.Certificate == nil || len(component.Certificate.RawDER) == 0 {
		return nil, errors.New("证书数据不存在")
	}
	return append([]byte(nil), component.Certificate.RawDER...), nil
}

// SignatureValue 返回指定签名的签名值（SignedValue.dat）内容。
func (r *Reader) SignatureValue(scope int, signatureID string) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, fmt.Errorf("签名作用域超出范围: %d", scope)
	}
	document := r.ofd.Documents[scope]
	if document == nil {
		return nil, errors.New("签名所属文档为空")
	}
	if document.GetSignature(signatureID) == nil {
		return nil, fmt.Errorf("签名不存在: %s", signatureID)
	}
	value := document.GetSignedValue(signatureID)
	if value == nil || len(value.Raw) == 0 {
		if err := document.GetSignedValueError(signatureID); err != nil {
			return nil, fmt.Errorf("读取签名值失败: %w", err)
		}
		return nil, errors.New("签名值不存在")
	}
	if int64(len(value.Raw)) > maxAttachmentBytesHard {
		return nil, fmt.Errorf("签名值超过大小限制 %d MB", maxAttachmentBytesHard>>20)
	}
	return append([]byte(nil), value.Raw...), nil
}

func signatureCertificates(verification *parser.SignatureVerificationResult) []CertificateDetail {
	if verification == nil {
		return nil
	}
	details := make([]CertificateDetail, 0, 2)
	appendComponent := func(slotKey, slot string, component parser.SignatureComponentResult) {
		if component.Certificate == nil && component.Error == "" && component.TrustError == "" &&
			component.RevocationError == "" && !component.TrustChecked && !component.RevocationChecked {
			return
		}
		detail := CertificateDetail{
			Slot:              slot,
			SlotKey:           slotKey,
			Algorithm:         component.Algorithm,
			SignatureFormat:   component.SignatureFormat,
			SignatureValid:    component.Valid,
			CertificateValid:  component.CertificateValid,
			TrustChecked:      component.TrustChecked,
			Trusted:           component.Trusted,
			TrustError:        component.TrustError,
			RevocationChecked: component.RevocationChecked,
			RevocationStatus:  component.RevocationStatus,
			RevocationError:   component.RevocationError,
			Error:             component.Error,
		}
		if certificate := component.Certificate; certificate != nil {
			detail.SerialNumber = certificate.SerialNumber
			detail.Subject = certificate.Subject.String()
			detail.Issuer = certificate.Issuer.String()
			detail.CommonName = certificate.Subject.CommonName
			detail.Organization = joinName(certificate.Subject.Organization)
			detail.OrganizationalUnit = joinName(certificate.Subject.OrganizationalUnit)
			detail.Country = joinName(certificate.Subject.Country)
			detail.Locality = joinName(certificate.Subject.Locality)
			detail.Province = joinName(certificate.Subject.Province)
			detail.PublicKey = certificate.PublicKey
			if !certificate.NotBefore.IsZero() {
				detail.NotBefore = certificate.NotBefore.Format(time.RFC3339)
			}
			if !certificate.NotAfter.IsZero() {
				detail.NotAfter = certificate.NotAfter.Format(time.RFC3339)
			}
		}
		details = append(details, detail)
	}
	appendComponent("seal", "印章", verification.Seal)
	appendComponent("outer", "外层", verification.Outer)
	return details
}

func joinName(values []string) string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			kept = append(kept, value)
		}
	}
	return strings.Join(kept, ", ")
}

func boxValue(box models.StBox) *AnnotationBoundary {
	if !box.IsFinite() {
		return nil
	}
	return &AnnotationBoundary{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height}
}
