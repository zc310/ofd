package sign

import (
	"crypto"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/signeddata"
)

type DigitalSigningOptions = signeddata.Options

// 普通数字签名的组合算法 OID，可同时用于 Options.SignatureMethod 和
// DigitalSigningOptions.Algorithm。RSA 使用 PKCS#1 v1.5，ECDSA 使用 DER (r,s)。
const (
	SM2WithSM3      = signeddata.SM2WithSM3
	SHA256WithRSA   = signeddata.SHA256WithRSA
	SHA384WithRSA   = signeddata.SHA384WithRSA
	SHA512WithRSA   = signeddata.SHA512WithRSA
	ECDSAWithSHA256 = signeddata.ECDSAWithSHA256
	ECDSAWithSHA384 = signeddata.ECDSAWithSHA384
	ECDSAWithSHA512 = signeddata.ECDSAWithSHA512
)

// CreateDigitalSignedValue 生成 detached SM2/SM3 GB/T 35275 或 RSA/ECDSA CMS SignedData。
// signer 可为硬件或进程适配器，库不生成或保存私钥；Timestamp 可接时间戳服务。
func CreateDigitalSignedValue(original, certificate []byte, signer crypto.Signer, options DigitalSigningOptions) ([]byte, error) {
	return signeddata.Create(original, certificate, signer, options)
}

// VerifyDigitalSignedValue 验证单独的普通数字签名；不替代 OFD 文件引用摘要检查。
func VerifyDigitalSignedValue(original, value []byte, options VerificationOptions) (*SignatureVerificationResult, error) {
	return parser.VerifyDigitalSignedValue(original, value, &options)
}
