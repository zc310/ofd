package merge

import (
	"fmt"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// SignatureStatus 描述输出文档中一个签名的校验状态。
type SignatureStatus struct {
	// ID 是签名标识。
	ID string
	// DigestValid 表示签名引用与数据摘要校验通过。
	DigestValid bool
	// Verified 表示 SM2/SES 密码学签名验证通过。
	Verified bool
	// VerificationError 是密码学验证的错误信息。
	VerificationError string
}

// VerifySignatures 解析 OFD 并汇总每个签名的摘要与密码学验证状态。
// input 支持文件路径、字节数据或 io.Reader。
func VerifySignatures(input any) ([]SignatureStatus, error) {
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return nil, fmt.Errorf("解析 OFD 失败: %w", err)
	}
	defer func() { _ = ofd.Close() }()

	var result []SignatureStatus
	for _, document := range ofd.Documents {
		if document == nil {
			continue
		}
		document.ForEachSignature(func(id string, _ *models.Signature) bool {
			status := SignatureStatus{ID: id}
			if digest := document.GetDigestResult(id); digest != nil {
				status.DigestValid = digest.Valid
			}
			if verification := document.GetVerificationResult(id); verification != nil {
				status.Verified = verification.Valid
			}
			if verificationErr := document.GetVerificationError(id); verificationErr != nil {
				status.VerificationError = verificationErr.Error()
			}
			result = append(result, status)
			return true
		})
	}
	return result, nil
}
