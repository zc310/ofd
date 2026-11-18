// Package certificate 解析、检查电子证照元数据及标识的本地格式。
// 本包不验证登记、唯一性、机构授权、签名或证照内容真实性。
package certificate

import (
	"errors"
	"fmt"
	"strings"
)

// IdentifierRoot 是 GB/T 36904—2018 §5.2 规定的根代码。
const IdentifierRoot = "1.2.156.3005.2"

const (
	alphanumeric       = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	creditAlphabet     = "0123456789ABCDEFGHJKLMNPQRTUWXY"
	typeSuffixAlphabet = "0123456789ABCDEFGHJKLMNPQRSTUVWXYZ"
)

var (
	// ErrIdentifierFormat 表示标识组成部分不符合词法结构。
	ErrIdentifierFormat = errors.New("电子证照标识格式无效")
	// ErrIdentifierChecksum 表示 MOD 37,36 校验位不匹配。
	ErrIdentifierChecksum = errors.New("电子证照标识校验位不匹配")
)

// Identifier 是 GB/T 36904—2018 §5 的标识组成部分，保留前导零。
type Identifier struct {
	// TypeCode 为定义机构代码及机构内分类码。
	TypeCode string `json:"type_code"`
	// IssuingAuthorityCode 为颁发机构代码，不要求等于类型代码前缀。
	IssuingAuthorityCode string `json:"issuing_authority_code"`
	// Serial 为颁发机构分配的流水号，保留前导零。
	Serial string `json:"serial"`
	// Version 为三位数字版本号，不转换为整数。
	Version string `json:"version"`
	// CheckCharacter 为已核对的单字符校验位。
	CheckCharacter string `json:"check_character"`
}

// ParseIdentifier 检查结构与 MOD 37,36 校验位；不修剪空白、不修正根代码或大小写。
// 类型定义机构与颁发机构可以不同；机构代码仅检查字符集和长度，不校验登记及 GB 32100 校验位。
func ParseIdentifier(value string) (Identifier, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 10 || strings.Join(parts[:5], ".") != IdentifierRoot {
		return Identifier{}, fmt.Errorf("%w: 根代码须为 %s，之后须有五个组成部分", ErrIdentifierFormat, IdentifierRoot)
	}
	id := Identifier{TypeCode: parts[5], IssuingAuthorityCode: parts[6], Serial: parts[7], Version: parts[8], CheckCharacter: parts[9]}
	if !validTypeCode(id.TypeCode) || !validCreditCode(id.IssuingAuthorityCode) ||
		!characters(id.Serial, alphanumeric) || len(id.Version) != 3 || !characters(id.Version, "0123456789") ||
		len(id.CheckCharacter) != 1 || !characters(id.CheckCharacter, alphanumeric) {
		return Identifier{}, fmt.Errorf("%w: 类型代码、机构代码、流水号、三位版本号或校验位无效", ErrIdentifierFormat)
	}
	// GB/T 36904—2018 §5.7：根代码和分隔符不参与运算。
	// 附录A将22映射为L，但ISO字符表0—9、A—Z中22是M；不据此引入另一套字母表。
	if checksum(id.TypeCode+id.IssuingAuthorityCode+id.Serial+id.Version) != id.CheckCharacter {
		return Identifier{}, ErrIdentifierChecksum
	}
	return id, nil
}

func checksum(value string) string {
	product := 36
	for _, character := range value {
		sum := (product + strings.IndexRune(alphanumeric, character)) % 36
		if sum == 0 {
			sum = 36
		}
		product = sum * 2 % 37
	}
	return string(alphanumeric[(37-product)%36])
}

func validTypeCode(value string) bool {
	// GB/T 36902—2018 §5.2.2、§6.3：18位定义机构代码与3位机构内分类码。
	return len(value) == 21 && validCreditCode(value[:18]) && characters(value[18:], typeSuffixAlphabet)
}

func validCreditCode(value string) bool {
	return len(value) == 18 && characters(value, creditAlphabet)
}

func characters(value, alphabet string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune(alphabet, character) {
			return false
		}
	}
	return true
}
