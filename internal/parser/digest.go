package parser

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"hash"
	"path"
	"strings"

	"github.com/tjfoc/gmsm/sm3"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
)

// SignatureDigestResult 是一个签名的摘要校验结果。
type SignatureDigestResult struct {
	// Method 是 References 声明的摘要算法。
	Method string
	// References 是被签名文件的逐项校验结果。
	References []ReferenceDigestResult
	// DataHash 是 TBS_Sign.DataHash 的校验结果；非 SES 签名时为空。
	DataHash *DataHashResult
	// Valid 表示所有可校验项目均通过。
	Valid bool
}

// ReferenceDigestResult 是单个 Reference 的摘要校验结果。
type ReferenceDigestResult struct {
	FileRef      string
	ResolvedPath string
	Expected     []byte
	Actual       []byte
	Exists       bool
	Match        bool
	Error        string
}

// DataHashResult 是 SES 签名中 Signature.xml 数据摘要的校验结果。
type DataHashResult struct {
	Expected []byte
	Actual   []byte
	Match    bool
	Error    string
}

// VerifySignatureDigest 校验 Signature.xml 的 References 摘要，以及 SES
// 签名中的 TBS_Sign.DataHash。DataHash 按 References 使用的摘要算法计算，
// 数据源是签名 XML 文件本身的包内字节。
func VerifySignatureDigest(fileCache *core.Package, signaturePath string, signature *models.Signature, signedValue *SignedValue) (*SignatureDigestResult, error) {
	if fileCache == nil {
		return nil, fmt.Errorf("签名摘要校验失败: ZIP 包为空")
	}
	if signature == nil {
		return nil, fmt.Errorf("签名摘要校验失败: 签名 XML 为空")
	}

	method := normalizeDigestMethod(signature.SignedInfo.References.CheckMethod)
	result := &SignatureDigestResult{Method: method, Valid: true}
	hashFunc, supported := signatureHash(method)
	if !supported {
		result.Valid = false
		message := fmt.Sprintf("不支持的签名摘要算法 %q", method)
		for _, reference := range signature.SignedInfo.References.Reference {
			result.References = append(result.References, ReferenceDigestResult{
				FileRef: reference.FileRef.String(),
				Error:   message,
			})
		}
		if signedValue != nil && signedValue.SES != nil {
			result.DataHash = &DataHashResult{Expected: cloneBytes(signedValue.SES.TBS.DataHash.Bytes), Error: message}
		}
		return result, nil
	}

	baseDir := models.StLoc(path.Dir(signaturePath))
	for _, reference := range signature.SignedInfo.References.Reference {
		item := ReferenceDigestResult{FileRef: reference.FileRef.String()}
		expected, decodeErr := decodeCheckValue(reference.CheckValue)
		if decodeErr != nil {
			item.Error = decodeErr.Error()
			result.Valid = false
			result.References = append(result.References, item)
			continue
		}
		item.Expected = expected
		resolved := reference.FileRef.Resolve(baseDir).String()
		item.ResolvedPath = resolved
		data, err := fileCache.Read(resolved)
		if err != nil {
			item.Error = err.Error()
			result.Valid = false
			result.References = append(result.References, item)
			continue
		}
		item.Exists = true
		item.Actual = digestBytes(hashFunc, data)
		item.Match = bytes.Equal(item.Expected, item.Actual)
		if !item.Match {
			result.Valid = false
		}
		result.References = append(result.References, item)
	}

	if signedValue != nil && signedValue.SES != nil {
		item := &DataHashResult{Expected: cloneBytes(signedValue.SES.TBS.DataHash.Bytes)}
		signatureData, err := fileCache.Read(signaturePath)
		if err != nil {
			item.Error = err.Error()
			result.Valid = false
		} else {
			item.Actual = digestBytes(hashFunc, signatureData)
			item.Match = bytes.Equal(item.Expected, item.Actual)
			if !item.Match {
				result.Valid = false
			}
		}
		result.DataHash = item
	}
	return result, nil
}

func normalizeDigestMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "MD5"
	}
	return method
}

func signatureHash(method string) (func() hash.Hash, bool) {
	switch normalizeDigestMethod(method) {
	case "MD5":
		return md5.New, true
	case "SHA1":
		return sha1.New, true
	case "SM3", "1.2.156.10197.1.401":
		return sm3.New, true
	default:
		return nil, false
	}
}

func digestBytes(newHash func() hash.Hash, data []byte) []byte {
	digest := newHash()
	_, _ = digest.Write(data)
	return digest.Sum(nil)
}

// DigestBase64 返回摘要的标准 Base64 表示，便于报告层输出。
func DigestBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func cloneBytes(data []byte) []byte {
	return append([]byte(nil), data...)
}

func decodeCheckValue(value []byte) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(value)), ""))
	if err != nil {
		return nil, fmt.Errorf("CheckValue 不是合法的 Base64: %w", err)
	}
	return decoded, nil
}
