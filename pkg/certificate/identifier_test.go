package certificate

import (
	"errors"
	"strings"
	"testing"
)

// 载荷来自 GB/T 36904—2018 附录A；按§5.7计算值22映射为M，而非附录误写的L。
const appendixIdentifier = "1.2.156.3005.2.11100000000013338W002.11320482014138860P.201807250201.001.M"

func TestParseIdentifierMatchesStandardAppendixAndKeepsLeadingZeros(t *testing.T) {
	id, err := ParseIdentifier(appendixIdentifier)
	if err != nil {
		t.Fatal(err)
	}
	if id.TypeCode != "11100000000013338W002" || id.IssuingAuthorityCode != "11320482014138860P" ||
		id.Serial != "201807250201" || id.Version != "001" || id.CheckCharacter != "M" {
		t.Fatalf("组成部分未保留: %+v", id)
	}
	if id.TypeCode[:18] == id.IssuingAuthorityCode {
		t.Fatal("测试向量应覆盖定义机构与颁发机构不同的情形")
	}
}

func TestParseIdentifierRejectsOtherCheckCharacters(t *testing.T) {
	for _, character := range alphanumeric {
		if character == 'M' {
			continue
		}
		_, err := ParseIdentifier(appendixIdentifier[:len(appendixIdentifier)-1] + string(character))
		if !errors.Is(err, ErrIdentifierChecksum) {
			t.Fatalf("校验位 %c 不应通过: %v", character, err)
		}
	}
}

func TestParseIdentifierRejectsMalformedPartsWithoutRewriting(t *testing.T) {
	for _, value := range []string{
		"", "1", appendixIdentifier + ".X", " " + appendixIdentifier, appendixIdentifier + "\n",
		strings.Replace(appendixIdentifier, "3005", "3003", 1),
		strings.Replace(appendixIdentifier, "W002", "W00I", 1),
		strings.Replace(appendixIdentifier, "W002", "W00O", 1),
		strings.Replace(appendixIdentifier, "W002", "w002", 1),
		strings.Replace(appendixIdentifier, "201807250201", "", 1),
		strings.Replace(appendixIdentifier, ".001.", ".01.", 1),
		strings.Replace(appendixIdentifier, ".001.", ".A01.", 1),
		strings.Replace(appendixIdentifier, ".M", ".m", 1),
		strings.Replace(appendixIdentifier, ".M", ".Ｍ", 1),
	} {
		if _, err := ParseIdentifier(value); !errors.Is(err, ErrIdentifierFormat) {
			t.Fatalf("无效标识不应被归一化接受: %q, %v", value, err)
		}
	}
}

func TestChecksumMatchesIndependentMOD3736Vector(t *testing.T) {
	// 独立向量来源：python-stdnum stdnum/iso7064/mod_37_36.py 文档示例。
	if got := checksum("A12425GABC1234002"); got != "M" {
		t.Fatalf("MOD 37,36 独立向量错误: %s", got)
	}
	// 明确保护资料性附录不覆盖正文算法的行为。
	original := strings.TrimSuffix(appendixIdentifier, "M") + "L"
	if _, err := ParseIdentifier(original); !errors.Is(err, ErrIdentifierChecksum) {
		t.Fatalf("不应静默接受附录误写的校验位L: %v", err)
	}
}

func TestTypeClassificationAllowsLettersExcludedOnlyFromCreditCodes(t *testing.T) {
	for _, suffix := range []string{"SVZ", "ABC", "000"} {
		if !validTypeCode("11100000000013338W" + suffix) {
			t.Fatalf("机构内分类码字符被错误排除: %s", suffix)
		}
	}
}
