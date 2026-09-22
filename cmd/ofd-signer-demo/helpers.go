package main

import (
	"crypto/ecdsa"
	"crypto/x509/pkix"
	"encoding/asn1"

	"github.com/emmansun/gmsm/sm2"
)

// pkixName 生成简单的证书主体名称。
func pkixName(commonName string) pkix.Name {
	return pkix.Name{CommonName: commonName, Organization: []string{"OFD Signer Demo"}}
}

// publicKeyOf 返回 SM2 私钥对应的公钥。
func publicKeyOf(privateKey *sm2.PrivateKey) *ecdsa.PublicKey {
	return &privateKey.PublicKey
}

// sm2WithSM3OIDValue 返回 SM2 + SM3 签名算法 OID。
func sm2WithSM3OIDValue() asn1.ObjectIdentifier {
	return asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 501}
}

// placeholderSeal 返回“红圈 + 中字”的 SVG 印章占位图。
//
// 圆环用 <circle>，「中」字用等价的矢量 <path> 以描边方式刻画，因此渲染
// 不依赖 CJK 字体；颜色均为红色 #E60012。SVG 坐标按 y 向下（canvas.ParseSVG
// 的约定）。演示印章不是有效签章，仅用于让 SignedValue.dat 结构和页面渲染
// 效果完整。
func placeholderSeal() []byte {
	return []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" width="200" height="200">
<circle cx="100" cy="100" r="88" fill="none" stroke="#E60012" stroke-width="8"/></svg>`)
}
