package sign

import (
	"context"
	"fmt"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

type VerificationOptions = parser.SignatureVerificationOptions
type CertificateTrustOptions = parser.CertificateTrustOptions
type CertificateRevocationOptions = parser.CertificateRevocationOptions
type SM2SignatureFormat = parser.SM2SignatureFormat
type VerificationCheck = parser.VerificationCheck
type SignatureValidation = parser.SignatureValidation
type SignatureVerificationResult = parser.SignatureVerificationResult

const (
	SignatureFormatAuto = parser.SM2SignatureFormatAuto
	SignatureFormatDER  = parser.SM2SignatureFormatDER
	SignatureFormatRaw  = parser.SM2SignatureFormatRaw
)

// Verification 是一个文档体中的一个签名的完整验证结果。
type Verification struct {
	DocumentIndex int                          `json:"document_index"`
	ID            string                       `json:"id"`
	DigestValid   bool                         `json:"digest_valid"`
	Result        *SignatureVerificationResult `json:"result,omitempty"`
	Error         string                       `json:"error,omitempty"`
}

// Verify 校验 OFD 的文件摘要及 GB/T 38540—2020 SES 签章。
// input 接受文件路径、[]byte 或 io.Reader。缺少信任、吊销或印章状态证据时，
// Result.Validation.Status 为 incomplete；两层数学签名结果仍独立保留。
func Verify(ctx context.Context, input any, options VerificationOptions) ([]Verification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return nil, fmt.Errorf("解析 OFD 签名失败: %w", err)
	}
	defer ofd.Close()
	var results []Verification
	for index, document := range ofd.Documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		document.ForEachSignature(func(id string, _ *models.Signature) bool {
			if ctx.Err() != nil {
				return false
			}
			item := Verification{DocumentIndex: index, ID: id}
			digest := document.GetDigestResult(id)
			if digest != nil {
				item.DigestValid = digest.Valid
			}
			value := document.GetSignedValue(id)
			result, err := parser.VerifySESSignedValueWithOptions(value, &options)
			if err != nil {
				item.Error = err.Error()
			} else {
				parser.BindSignatureDigest(result, digest)
				item.Result = result
			}
			results = append(results, item)
			return true
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
