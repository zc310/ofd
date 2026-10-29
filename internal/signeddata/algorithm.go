package signeddata

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"strconv"
	"strings"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm3"
)

// 算法值采用组合签名 OID，供 OFD SignatureMethod 与签名器选项共用。
const (
	SM2WithSM3      = "1.2.156.10197.1.501"
	SHA256WithRSA   = "1.2.840.113549.1.1.11"
	SHA384WithRSA   = "1.2.840.113549.1.1.12"
	SHA512WithRSA   = "1.2.840.113549.1.1.13"
	ECDSAWithSHA256 = "1.2.840.10045.4.3.2"
	ECDSAWithSHA384 = "1.2.840.10045.4.3.3"
	ECDSAWithSHA512 = "1.2.840.10045.4.3.4"
)

type algorithm struct {
	method string
	digest asn1.ObjectIdentifier
	hash   crypto.Hash
	kind   string
}

func supportedAlgorithms() []algorithm {
	return []algorithm{
		{SM2WithSM3, asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 401}, 0, "SM2"},
		{SHA256WithRSA, asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}, crypto.SHA256, "RSA"},
		{SHA384WithRSA, asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}, crypto.SHA384, "RSA"},
		{SHA512WithRSA, asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}, crypto.SHA512, "RSA"},
		{ECDSAWithSHA256, asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}, crypto.SHA256, "ECDSA"},
		{ECDSAWithSHA384, asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}, crypto.SHA384, "ECDSA"},
		{ECDSAWithSHA512, asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}, crypto.SHA512, "ECDSA"},
	}
}

func lookupAlgorithm(method string) (algorithm, error) {
	for _, a := range supportedAlgorithms() {
		if a.method == method {
			return a, nil
		}
	}
	return algorithm{}, fmt.Errorf("%w: 签名算法 %s", ErrUnsupported, method)
}

// SupportsAlgorithm 判断 OFD SignatureMethod 是否为已支持的组合签名 OID。
func SupportsAlgorithm(method string) bool {
	_, err := lookupAlgorithm(method)
	return err == nil
}

// RFC 5652 §5、RFC 5754：RSA 接受 rsaEncryption 或与摘要一致的组合 OID；
// ECDSA 使用组合 OID 且参数必须缺省。尚不支持带参数的 RSA-PSS。
func algorithmFromIdentifiers(digest, signature pkix.AlgorithmIdentifier) (algorithm, error) {
	if !nullOrAbsent(digest.Parameters) {
		return algorithm{}, invalid("摘要算法参数必须缺省或为 NULL")
	}
	for _, a := range supportedAlgorithms() {
		if !digest.Algorithm.Equal(a.digest) {
			continue
		}
		oid := signature.Algorithm.String()
		match := oid == a.method || a.kind == "RSA" && oid == "1.2.840.113549.1.1.1" || a.kind == "SM2" && oid == "1.2.156.10197.1.301.1"
		if !match {
			continue
		}
		if a.kind == "ECDSA" && len(signature.Parameters.FullBytes) != 0 || !nullOrAbsent(signature.Parameters) {
			return algorithm{}, invalid("签名算法参数无效")
		}
		return a, nil
	}
	return algorithm{}, fmt.Errorf("%w: 摘要 %s 与签名算法 %s 的组合", ErrUnsupported, digest.Algorithm, signature.Algorithm)
}

func nullOrAbsent(value asn1.RawValue) bool {
	return len(value.FullBytes) == 0 || string(value.FullBytes) == "\x05\x00"
}

func (a algorithm) sum(data []byte) []byte {
	switch a.method {
	case SHA256WithRSA, ECDSAWithSHA256:
		sum := sha256.Sum256(data)
		return sum[:]
	case SHA384WithRSA, ECDSAWithSHA384:
		sum := sha512.Sum384(data)
		return sum[:]
	case SHA512WithRSA, ECDSAWithSHA512:
		sum := sha512.Sum512(data)
		return sum[:]
	default:
		sum := sm3.Sum(data)
		return sum[:]
	}
}

func (a algorithm) checkPublicKey(public crypto.PublicKey) error {
	switch a.kind {
	case "RSA":
		if _, ok := public.(*rsa.PublicKey); ok {
			return nil
		}
	case "SM2", "ECDSA":
		if key, ok := public.(*ecdsa.PublicKey); ok {
			if a.kind == "SM2" && sm2.IsSM2PublicKey(key) {
				return nil
			}
			if a.kind == "ECDSA" && (key.Curve == elliptic.P256() || key.Curve == elliptic.P384() || key.Curve == elliptic.P521()) {
				return nil
			}
		}
	}
	return invalid("证书公钥与签名算法 %s 不匹配", a.method)
}

func (a algorithm) sign(signer crypto.Signer, data, uid []byte) ([]byte, error) {
	if a.kind == "SM2" {
		return signer.Sign(rand.Reader, data, sm2.NewSM2SignerOption(true, uid))
	}
	return signer.Sign(rand.Reader, a.sum(data), a.hash)
}

func (a algorithm) verify(public crypto.PublicKey, data, signature, uid []byte) error {
	if err := a.checkPublicKey(public); err != nil {
		return err
	}
	switch key := public.(type) {
	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(key, a.hash, a.sum(data), signature); err == nil {
			return nil
		}
	case *ecdsa.PublicKey:
		switch a.kind {
		case "SM2":
			if sm2.VerifyASN1WithSM2(key, uid, data, signature) {
				return nil
			}
		case "ECDSA":
			if ecdsa.VerifyASN1(key, a.sum(data), signature) {
				return nil
			}
		}
	}
	return invalid("%s 数学签名验证失败", a.kind)
}

func (a algorithm) identifiers() (pkix.AlgorithmIdentifier, pkix.AlgorithmIdentifier) {
	var oid asn1.ObjectIdentifier
	for part := range strings.SplitSeq(a.method, ".") {
		n, _ := strconv.Atoi(part) // method 来自本包常量。
		oid = append(oid, n)
	}
	signature := pkix.AlgorithmIdentifier{Algorithm: oid}
	switch a.kind {
	case "SM2":
		signature.Algorithm = asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 301, 1}
	case "RSA":
		// RFC 3370 §3.2：CMS 使用 rsaEncryption，摘要由 digestAlgorithm 指定。
		signature.Algorithm = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
		signature.Parameters = asn1.NullRawValue
	}
	return pkix.AlgorithmIdentifier{Algorithm: a.digest}, signature
}
