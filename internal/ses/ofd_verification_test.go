package ses_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/validator"
)

func signedOFD(t *testing.T, f *signingFixture, tamper bool, scope string) []byte {
	t.Helper()
	document := []byte(`<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>10</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`)
	sum := sha1.Sum(document)
	f.xml = []byte(fmt.Sprintf(`<Signature xmlns="http://www.ofdspec.org/2016"><SignedInfo><Provider ProviderName="SES test"/><SignatureMethod>1.2.156.10197.1.501</SignatureMethod><References CheckMethod="SHA1"><Reference FileRef="/Doc_0/Document.xml"><CheckValue>%s</CheckValue></Reference></References></SignedInfo><SignedValue>SignedValue.dat</SignedValue></Signature>`, base64.StdEncoding.EncodeToString(sum[:])))
	value, err := sign.CreateSignedValue(f.xml, f.seal, f.signer.Raw, f.signerKey, scope, f.now, sign.SigningOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if tamper {
		document = bytes.ReplaceAll(document, []byte("<MaxUnitID>10"), []byte("<MaxUnitID>11"))
	}
	files := map[string][]byte{
		"OFD.xml":                          []byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>38540000000000000000000000000001</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot><Signatures>Doc_0/Signatures.xml</Signatures></DocBody></OFD>`),
		"Doc_0/Document.xml":               document,
		"Doc_0/Pages/Page_0/Content.xml":   []byte(`<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="2"/></Content></Page>`),
		"Doc_0/Signatures.xml":             []byte(`<Signatures xmlns="http://www.ofdspec.org/2016"><MaxSignId>sign-1</MaxSignId><Signature ID="sign-1" Type="Seal" BaseLoc="Signatures/Signature.xml"/></Signatures>`),
		"Doc_0/Signatures/Signature.xml":   f.xml,
		"Doc_0/Signatures/SignedValue.dat": value,
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, data := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestOFDCompleteVerificationSeparatesReferenceSHA1AndSESDataHashSM3(t *testing.T) {
	f := newSigningFixture(t, false)
	for _, test := range []struct {
		name        string
		tamper      bool
		scope, want string
	}{
		{"valid", false, "/Doc_0/Signatures/Signature.xml", "valid"},
		{"changed_reference", true, "/Doc_0/Signatures/Signature.xml", "invalid"},
		{"unbound_scope", false, "/Doc_0/OtherSignature.xml", "incomplete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := signedOFD(t, f, test.tamper, test.scope)
			results, err := sign.Verify(t.Context(), data, *f.options)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Error != "" || results[0].Result == nil {
				t.Fatalf("验证结果不完整: %+v", results)
			}
			if !results[0].Result.Valid || results[0].Result.Validation.Status != test.want {
				t.Fatalf("完整结果 = %+v", results[0].Result)
			}
			instance, err := validator.New(validator.WithMode(validator.ModeStructural), validator.WithSignatureVerification(f.options))
			if err != nil {
				t.Fatal(err)
			}
			report := instance.ValidateReader(t.Context(), bytes.NewReader(data), "signed.ofd")
			found := false
			for _, check := range report.Checks {
				if check.Name == "signatures" {
					found = true
					want := "passed"
					if test.want == "invalid" {
						want = "failed"
					}
					if test.want == "incomplete" {
						want = "incomplete"
					}
					if check.Status != want {
						t.Fatalf("校验器签章状态=%s，期望 %s：%+v", check.Status, want, report.Issues)
					}
				}
			}
			if !found {
				t.Fatal("校验器缺少完整签章检查")
			}
		})
	}
}
