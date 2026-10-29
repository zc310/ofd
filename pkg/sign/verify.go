package sign

import (
	"context"
	"fmt"
	"time"

	"github.com/zc310/ofd/internal/container"
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/physicalsig"
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
	DocumentIndex int    `json:"document_index"`
	ID            string `json:"id"`
	Scope         string `json:"scope,omitempty"`
	// DigestValid 仅表示引用摘要与 SES DataHash 匹配；完整结论使用 Result.FullyValid()。
	DigestValid bool                         `json:"digest_valid"`
	Result      *SignatureVerificationResult `json:"result,omitempty"`
	Error       string                       `json:"error,omitempty"`
}

// Verify 校验文件摘要，并按 Type 验证 SES 签章或 SM2/RSA/ECDSA 普通 SignedData。
// input 接受文件路径、[]byte 或 io.Reader。缺少信任、吊销或印章状态证据时，
// Result.Validation.Status 为 incomplete；各类型的数学签名结果仍独立保留。
// crypt.View 必须完整解锁；只检查原始密文包时使用 VerifyPhysical。
// 业务接受签章前应检查 Result.FullyValid()。
func Verify(ctx context.Context, input any, options VerificationOptions) ([]Verification, error) {
	if options.CurrentTime.IsZero() {
		options.CurrentTime = time.Now()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if source, ok := input.(container.PackageSource); ok {
		// 先取得逻辑读取租约，保留视图对未完整解锁与关闭状态的拒绝语义。
		lease, err := source.OFDPackage()
		if err != nil {
			return nil, err
		}
		defer lease.Close()
	}
	if origin, ok := input.(interface {
		OpenPhysicalPackage() (*container.Package, error)
	}); ok {
		pkg, err := origin.OpenPhysicalPackage()
		if err != nil {
			return nil, err
		}
		defer pkg.Close()
		graph, err := physicalsig.Discover(ctx, pkg, protocol.Limits{})
		if err != nil {
			return nil, err
		}
		if graph != nil {
			return verifyPhysicalPackage(ctx, pkg, options)
		}
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
			result, err := document.VerifySignature(ctx, id, &options)
			if err != nil {
				item.Error = err.Error()
			} else {
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
