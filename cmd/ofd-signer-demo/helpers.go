package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/asn1"
	"fmt"
	"image/png"

	"github.com/emmansun/gmsm/sm2"
)

//go:embed seal.png
var seal []byte

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

// placeholderSeal 返回内嵌在 seal.png 中的 PNG 印章占位图。
//
// 印章主体为红色圆环加「中」字，颜色均为红色 #E60012 系，空白区域透明；
// 渲染不依赖 CJK 字体。演示印章不是有效签章，仅用于让 SignedValue.dat 结构和
// 页面渲染效果完整。
func placeholderSeal() []byte {
	return seal
}

// pictureSize 返回演示印章图片的宽高，用于填充 SES_ESPictrueInfo.Width/Height。
func pictureSize() (int, int, error) {
	config, err := png.DecodeConfig(bytes.NewReader(seal))
	if err != nil {
		return 0, 0, fmt.Errorf("解析演示印章图片失败: %w", err)
	}
	return config.Width, config.Height, nil
}
