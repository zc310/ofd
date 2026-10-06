package ses

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
)

// makeTestSealPNG 生成一枚 4x4 的红色 PNG 印章图。
func makeTestSealPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	red := color.RGBA{R: 0xE6, G: 0x00, B: 0x12, A: 0xFF}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestBuildSealCanBeExtractedByParser 验证生成的 SES 印章 DER 能被阅读器的
// ExtractSealData 识别为 PNG 印章图片——这是 GB/T 38540 结构与阅读器解析侧
// 兼容的关键回归。
func TestBuildSealCanBeExtractedByParser(t *testing.T) {
	pngData := makeTestSealPNG(t)
	now := time.Now().UTC()
	key, _, certDER, err := NewSelfSignedCertificate("ofd-seal 测试", "OFD Seal Test", now)
	if err != nil {
		t.Fatal(err)
	}
	typeName, w, h, err := PictureTypeAndSize(pngData)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := BuildSeal(SealParams{
		Provider:    "ofd-seal/test",
		ESID:        "test@ofd-seal",
		Name:        "测试印章",
		PictureType: typeName,
		PictureData: pngData,
		Width:       w,
		Height:      h,
	}, certDER, key, now)
	if err != nil {
		t.Fatal(err)
	}
	der, err := seal.MarshalDER()
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := parser.ExtractSealData(der)
	if err != nil {
		t.Fatalf("阅读器无法从生成的 Seal.esl 提取印章: %v", err)
	}
	if extracted.FileType != "png" {
		t.Errorf("FileType = %q, 期望 png", extracted.FileType)
	}
	if !bytes.Equal(extracted.Data, pngData) {
		t.Error("提取的印章图片与原始 PNG 不一致")
	}
}

// TestBuildSignedValuePassesSESVerification 生成的 SignedValue.dat 应通过
// parser 的两层 SM2 签名校验。
func TestBuildSignedValuePassesSESVerification(t *testing.T) {
	pngData := makeTestSealPNG(t)
	now := time.Now().UTC()
	key, _, certDER, err := NewSelfSignedCertificate("ofd-seal 测试", "OFD Seal Test", now)
	if err != nil {
		t.Fatal(err)
	}
	typeName, w, h, _ := PictureTypeAndSize(pngData)
	seal, err := BuildSeal(SealParams{
		Provider:    "ofd-seal/test",
		ESID:        "test@ofd-seal",
		Name:        "测试印章",
		PictureType: typeName,
		PictureData: pngData,
		Width:       w,
		Height:      h,
	}, certDER, key, now)
	if err != nil {
		t.Fatal(err)
	}
	signedValue, err := BuildSignedValue([]byte("<Signature>demo</Signature>"), seal, certDER, key, "/Doc_0/Signatures/Signature_sign-1.xml", now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseSignedValue(signedValue)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parser.VerifySESSignedValue(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || !result.Seal.Valid || !result.Outer.Valid {
		t.Fatalf("两层 SM2 签名应全部有效: %+v", result)
	}
}

// TestBuildSealRejectsNonIA5Identifiers 保护 IA5String 字段的校验。
//
// VID 与 ESID 在 GB/T 38540 里是 IA5String。非 ASCII 值如果放任传到 asn1，编码
// 阶段才会报 "IA5String contains invalid character"，看不出是哪个字段出的问题。
func TestBuildSealRejectsNonIA5Identifiers(t *testing.T) {
	pngData := makeTestSealPNG(t)
	now := time.Now().UTC()
	key, _, certDER, err := NewSelfSignedCertificate("ofd-seal 测试", "OFD Seal Test", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		provider string
		esid     string
	}{
		{name: "VID 含中文", provider: "ofd-seal/印章", esid: "seal@example.com"},
		{name: "ESID 含中文", provider: "ofd-seal", esid: "印章@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildSeal(SealParams{
				Provider:    tc.provider,
				ESID:        tc.esid,
				Name:        "允许中文的印章名称",
				PictureType: "png",
				PictureData: pngData,
				Width:       4,
				Height:      4,
			}, certDER, key, now)
			if err == nil {
				t.Fatal("非 IA5 的 VID/ESID 应被拒绝")
			}
			if !strings.Contains(err.Error(), "IA5") {
				t.Errorf("错误信息应说明 IA5 要求，实际 %q", err)
			}
		})
	}
}
