package sign

import (
	"crypto"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/signeddata"
)

type DigitalSigningOptions = signeddata.Options

// CreateDigitalSignedValue 供外部签名器生成 GB/T 35275 的 detached SM2/SM3 SignedData。
// signer 可为硬件或进程适配器，库不生成或保存私钥；Timestamp 可接时间戳服务。
func CreateDigitalSignedValue(original, certificate []byte, signer crypto.Signer, options DigitalSigningOptions) ([]byte, error) {
	return signeddata.Create(original, certificate, signer, options)
}

// VerifyDigitalSignedValue 验证单独的普通数字签名；不替代 OFD 文件引用摘要检查。
func VerifyDigitalSignedValue(original, value []byte, options VerificationOptions) (*SignatureVerificationResult, error) {
	return parser.VerifyDigitalSignedValue(original, value, &options)
}
