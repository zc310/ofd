package ses_test

import (
	"crypto/rand"
	"encoding/asn1"
	"math/big"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/ses"
)

type sm2TestSignature struct{ R, S *big.Int }

func encodeTestSM2Signature(t *testing.T, rs sm2TestSignature, format parser.SM2SignatureFormat) []byte {
	t.Helper()
	if format == parser.SM2SignatureFormatRaw {
		raw := make([]byte, 64)
		rs.R.FillBytes(raw[:32])
		rs.S.FillBytes(raw[32:])
		return raw
	}
	der, err := asn1.Marshal(rs)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func highSSignedValue(t *testing.T, f *signingFixture) *parser.SignedValue {
	t.Helper()
	var value ses.SignedValue
	if rest, err := asn1.Unmarshal(f.value, &value); err != nil || len(rest) != 0 {
		t.Fatalf("解码测试签章失败: %v", err)
	}
	sign := func(data any, key *sm2.PrivateKey) asn1.BitString {
		t.Helper()
		der, err := asn1.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		halfOrder := new(big.Int).Rsh(new(big.Int).Set(sm2.P256().Params().N), 1)
		for range 128 {
			sig, err := key.Sign(rand.Reader, der, sm2.NewSM2SignerOption(true, nil))
			if err != nil {
				t.Fatal(err)
			}
			var rs sm2TestSignature
			if _, err := asn1.Unmarshal(sig, &rs); err != nil {
				t.Fatal(err)
			}
			if rs.S.Cmp(halfOrder) > 0 {
				return asn1.BitString{Bytes: sig, BitLength: len(sig) * 8}
			}
		}
		t.Fatal("未生成高 S 测试签名")
		return asn1.BitString{}
	}
	value.TBS.Seal.Signature = sign(value.TBS.Seal.SealInfo, f.makerKey)
	value.Signature = sign(value.TBS, f.signerKey)
	der, err := asn1.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseSignedValue(der)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestSM2AcceptsHighSAndRejectsNegatedS(t *testing.T) {
	f := newSigningFixture(t, false)
	original := highSSignedValue(t, f)
	for _, format := range []parser.SM2SignatureFormat{parser.SM2SignatureFormatDER, parser.SM2SignatureFormatRaw} {
		t.Run(string(format), func(t *testing.T) {
			value, err := parser.ParseSignedValue(original.Raw)
			if err != nil {
				t.Fatal(err)
			}
			var rs sm2TestSignature
			if _, err := asn1.Unmarshal(value.SES.Signature.Bytes, &rs); err != nil {
				t.Fatal(err)
			}
			value.SES.Signature.Bytes = encodeTestSM2Signature(t, rs, format)
			result, err := parser.VerifySESSignedValueWithOptions(value, f.options)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Valid || !result.Seal.Valid || !result.Outer.Valid || result.Outer.SignatureFormat != string(format) {
				t.Fatalf("两层合法高 S 签名应通过: %+v", result)
			}
			// SM2 的验证方程不具有 ECDSA 的 s -> N-s 等价关系。
			rs.S.Sub(sm2.P256().Params().N, rs.S)
			value.SES.Signature.Bytes = encodeTestSM2Signature(t, rs, format)
			result, err = parser.VerifySESSignedValueWithOptions(value, f.options)
			if err != nil {
				t.Fatal(err)
			}
			if result.Valid || result.Outer.Valid || !result.Seal.Valid || result.Outer.Error != "SM2 签名验证失败" {
				t.Fatalf("N-s 替换不应通过: %+v", result)
			}
			assertVerificationCheck(t, result, "signer.signature", "invalid")
		})
	}
}

func TestSM2RejectsOutOfRangeSignatureValues(t *testing.T) {
	f := newSigningFixture(t, false)
	n := sm2.P256().Params().N
	for _, format := range []parser.SM2SignatureFormat{parser.SM2SignatureFormatDER, parser.SM2SignatureFormatRaw} {
		t.Run(string(format), func(t *testing.T) {
			for _, test := range []struct {
				name string
				r, s *big.Int
			}{
				{"zero_r", big.NewInt(0), big.NewInt(1)},
				{"zero_s", big.NewInt(1), big.NewInt(0)},
				{"r_equal_order", n, big.NewInt(1)},
				{"s_equal_order", big.NewInt(1), n},
				{"r_above_order", new(big.Int).Add(n, big.NewInt(1)), big.NewInt(1)},
				{"s_above_order", big.NewInt(1), new(big.Int).Add(n, big.NewInt(1))},
				{"zero_r_plus_s", big.NewInt(1), new(big.Int).Sub(n, big.NewInt(1))},
			} {
				t.Run(test.name, func(t *testing.T) {
					value, err := parser.ParseSignedValue(f.value)
					if err != nil {
						t.Fatal(err)
					}
					value.SES.Signature.Bytes = encodeTestSM2Signature(t, sm2TestSignature{test.r, test.s}, format)
					result, err := parser.VerifySESSignedValueWithOptions(value, f.options)
					if err != nil {
						t.Fatal(err)
					}
					if result.Valid || result.Outer.Valid || result.Outer.Error == "" {
						t.Fatalf("非法 SM2 签名值被接受: %+v", result)
					}
					assertVerificationCheck(t, result, "signer.signature", "invalid")
				})
			}
		})
	}
}

func TestSESTimestampRejectsReencodedValidSM2Signature(t *testing.T) {
	f := newSigningFixture(t, false)
	value := highSSignedValue(t, f)
	value.SES.TimeStamp = &parser.BitString{Bytes: makeTimestamp(t, f, value.SES.Signature.Bytes, false)}
	options := *f.options
	options.CurrentTime = f.now.Add(2 * time.Second)
	result, err := parser.VerifySESSignedValueWithOptions(value, &options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Fatalf("原始签章应通过数学验证: %+v", result)
	}
	assertVerificationCheck(t, result, "timestamp", "valid")
	var rs sm2TestSignature
	if _, err := asn1.Unmarshal(value.SES.Signature.Bytes, &rs); err != nil {
		t.Fatal(err)
	}
	value.SES.Signature.Bytes = encodeTestSM2Signature(t, rs, parser.SM2SignatureFormatRaw)
	result, err = parser.VerifySESSignedValueWithOptions(value, &options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Validation.Status != "invalid" || result.FullyValid() {
		t.Fatalf("数学签名不变时，原时间戳仍须拒绝不同的签名值字节: %+v", result)
	}
	assertVerificationCheck(t, result, "timestamp", "invalid")
}
