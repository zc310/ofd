package sign

import (
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/zc310/ofd/internal/ses"
)

// SealParams 描述显示尺寸以毫米计的 SES 电子印章，以及获授权签章者列表。
type SealParams = ses.SealParams
type Seal = ses.Seal
type CertificateDigest = ses.CertificateDigest
type Extension = ses.Extension
type SigningOptions = ses.SigningOptions

// CreateSeal 使用调用方提供的制章者证书和私钥生成 SES V4 印章。
// SignerCertificates/SignerDigests 可授权与制章者不同的签章者。
func CreateSeal(params SealParams, certificate []byte, key *sm2.PrivateKey, at time.Time) (*Seal, error) {
	return ses.BuildSeal(params, certificate, key, at)
}

// CreateSignedValue 对 Signature.xml 字节生成 SM2/SM3 SES V4 签章值。
// propertyInfo 指定受保护的 Signature.xml 包内路径。签署前检查证书与密钥匹配、
// 印章签名及授权、有效期和用途；证书信任和状态校验由 options.Validate 接入。
// 返回值可交给 creator.Signature.SignedValue，或由外部签名命令输出。
func CreateSignedValue(signatureXML []byte, seal *Seal, certificate []byte, key *sm2.PrivateKey, propertyInfo string, at time.Time, options SigningOptions) ([]byte, error) {
	return ses.BuildSignedValueWithOptions(signatureXML, seal, certificate, key, propertyInfo, at, options)
}
