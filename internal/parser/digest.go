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

	"github.com/emmansun/gmsm/sm3"
	"github.com/zc310/ofd/internal/container"
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
	// Valid 表示所支持的引用摘要与 SES DataHash 均匹配；完整验签结论还须
	// 通过 BindSignatureDigest 检查算法一致性与保护范围，并使用 FullyValid。
	Valid                bool
	Unsupported          string
	SignatureMethodError string
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
	Expected   []byte
	Actual     []byte
	Match      bool
	Error      string
	ScopeMatch bool
	ScopeError string
}

// VerifySignatureDigest 校验 Signature.xml 的 References 摘要，以及 SES
// 签名中的 TBS_Sign.DataHash。SM2 签章的 DataHash 使用 SM3（§7.1.5），
// References 独立使用其声明的算法；数据源是签名 XML 文件本身的包内字节。
func VerifySignatureDigest(fileCache *container.Package, signaturePath string, signature *models.Signature, signedValue *SignedValue) (*SignatureDigestResult, error) {
	if fileCache == nil {
		return nil, fmt.Errorf("签名摘要校验失败: ZIP 包为空")
	}
	if signature == nil {
		return nil, fmt.Errorf("签名摘要校验失败: 签名 XML 为空")
	}

	method := normalizeDigestMethod(signature.SignedInfo.References.CheckMethod)
	result := &SignatureDigestResult{Method: method, Valid: true}
	if signedValue != nil && signedValue.SES != nil {
		if declared := signature.SignedInfo.SignatureMethod; declared != "" && declared != signedValue.SES.SignatureAlgorithm.String() {
			result.SignatureMethodError = "Signature.xml 与 SES 声明的签名算法不一致"
		}
	}
	hashFunc, supported := signatureHash(method)
	if !supported {
		result.Valid = false
		message := fmt.Sprintf("不支持的签名摘要算法 %q", method)
		result.Unsupported = message
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
			// §7.1.5：使用 SM2 的原文杂凑必须是 SM3，与 References 的算法分开。
			if signedValue.SES.SignatureAlgorithm.String() == sm2WithSM3OID {
				item.Actual = digestBytes(sm3.New, signatureData)
			} else {
				item.Error = "未支持的 SES 原文杂凑算法"
				result.Unsupported = item.Error
				result.Valid = false
			}
			item.Match = bytes.Equal(item.Expected, item.Actual)
			if !item.Match {
				result.Valid = false
			}
		}
		// OFD 接入约定 PropertyInfo 指向受保护的 Signature.xml；其他厂商的
		// 元数据编码保留解析，但必须由适配层解释后才能得出完整验证结论。
		scope := signedValue.SES.TBS.PropertyInfo
		item.ScopeMatch = scope != "" && models.StLoc(scope).Resolve(baseDir).String() == models.StLoc(signaturePath).Resolve("/").String()
		if !item.ScopeMatch {
			item.ScopeError = "未确认 PropertyInfo 与当前 Signature.xml 的保护范围绑定"
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
