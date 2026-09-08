package creator

import (
	"crypto/md5"
	"crypto/sha1"
	"fmt"
	"hash"
	"strings"

	"github.com/tjfoc/gmsm/sm3"
)

func signatureDigest(method string, data []byte) ([]byte, error) {
	var newHash func() hash.Hash
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "MD5":
		newHash = md5.New
	case "SHA1":
		newHash = sha1.New
	case "SM3", "1.2.156.10197.1.401":
		newHash = sm3.New
	default:
		return nil, fmt.Errorf("签名摘要算法无效: %q", method)
	}
	digest := newHash()
	_, _ = digest.Write(data)
	return digest.Sum(nil), nil
}
