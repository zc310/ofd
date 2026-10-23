package ses_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/validator"
)

func validatePhysicalGMT(t *testing.T, input []byte, options ...validator.Option) validator.Report {
	t.Helper()
	view, err := crypt.Open(t.Context(), input, crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	v, err := validator.New(append([]validator.Option{validator.WithGMT0099(true)}, options...)...)
	if err != nil {
		t.Fatal(err)
	}
	return v.Validate(t.Context(), view, "physical.ofd")
}

func TestGMT0099ValidatorChecksPhysicalReferencesAndLogicalDocumentSeparately(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	for _, plaintextMap := range []bool{false, true} {
		signed := physicalSigned(t, f, gmtFiles(), plaintextMap)
		for _, options := range [][]validator.Option{nil, {validator.WithSignatureVerification(f.options)}} {
			report := validatePhysicalGMT(t, signed, options...)
			if report.Status != validator.StatusValid || report.HasErrors() || report.HasWarnings() {
				t.Fatalf("正确的物理包被拒绝: %+v", report)
			}
			for _, check := range report.Checks {
				if check.Name == "digest" || check.Name == "references" || check.Name == "signatures" {
					if check.Status != "passed" {
						t.Fatalf("物理范围检查未通过: %+v", report)
					}
				}
			}
		}
		report := validatePhysicalGMT(t, signed, validator.WithSignatureVerification(&validator.SignatureVerificationOptions{CurrentTime: f.options.CurrentTime}))
		if report.Status != validator.StatusInvalid {
			t.Fatalf("缺少信任证据被接受: %+v", report)
		}
		found := false
		for _, issue := range report.Issues {
			if strings.HasPrefix(issue.Code, "signature.incomplete.physical.") {
				found = true
			}
			if issue.Code == "reference.missing" {
				t.Fatalf("缺证据误报引用缺失: %+v", issue)
			}
		}
		if !found {
			t.Fatalf("缺少物理签名未完成原因: %+v", report)
		}
	}
}

func TestGMT0099ValidatorRetainsPhysicalTamperingAndLogicalErrors(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	signed := physicalSigned(t, f, gmtFiles(), false)
	for _, name := range []string{"Doc_0/Signatures/sign-1/Data/sign-1.dat", "OFD.xml"} {
		t.Run(name, func(t *testing.T) {
			changed := gmtUnpack(t, signed)
			data := changed[name]
			if name == "OFD.xml" {
				changed[name] = append(data, ' ')
			} else {
				data[len(data)-1] ^= 1
			}
			report := validatePhysicalGMT(t, gmtArchive(t, changed), validator.WithSignatureVerification(f.options))
			if !report.HasErrors() || report.Status != validator.StatusInvalid {
				t.Fatalf("篡改外签仍通过: %+v", report)
			}
		})
	}
	for _, mode := range []string{"unknown_page", "missing_reference"} {
		originals := gmtFiles()
		if mode == "unknown_page" {
			originals["Doc_0/Pages/Content.xml"] = []byte(`<Page xmlns="http://www.ofdspec.org/2016"><Unknown/></Page>`)
		} else {
			originals["Doc_0/Document.xml"] = bytes.ReplaceAll(originals["Doc_0/Document.xml"], []byte("Pages/Content.xml"), []byte("Pages/Missing.xml"))
		}
		report := validatePhysicalGMT(t, physicalSigned(t, f, originals, false), validator.WithSignatureVerification(f.options))
		if !report.HasErrors() {
			t.Fatalf("逻辑错误被物理签名掩盖: %+v", report)
		}
	}
}

func TestGMT0099ValidatorPhysicalViewHonorsBudgetsCancellationAndProfile(t *testing.T) {
	f := newSigningFixture(t, false)
	signed := physicalSigned(t, f, gmtFiles(), false)
	for _, option := range []validator.Option{validator.WithMaxXMLBytes(16), validator.WithMaxFileSize(16), validator.WithMaxEntries(1), validator.WithDocType("OFD-A")} {
		report := validatePhysicalGMT(t, signed, option)
		if !report.HasErrors() {
			t.Fatalf("预算或 profile 未执行: %+v", report)
		}
	}
	view, err := crypt.Open(t.Context(), signed, crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	v, err := validator.New(validator.WithGMT0099(true))
	if err != nil {
		t.Fatal(err)
	}
	if report := v.Validate(ctx, view, "canceled.ofd"); !report.HasErrors() {
		t.Fatalf("取消仍通过: %+v", report)
	}
}

func TestGMT0099ValidatorPhysicalDigestSwitchDoesNotDisableFullVerification(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	files := gmtUnpack(t, physicalSigned(t, f, gmtFiles(), false))
	files["OFD.xml"] = append(files["OFD.xml"], ' ')
	input := gmtArchive(t, files)
	report := validatePhysicalGMT(t, input)
	found := false
	for _, issue := range report.Issues {
		if issue.Stage == validator.StageDigest {
			found = true
		}
	}
	if !report.HasErrors() || !found {
		t.Fatalf("物理摘要未检查: %+v", report)
	}
	report = validatePhysicalGMT(t, input, validator.WithCheckDigest(false))
	if report.HasErrors() {
		t.Fatalf("摘要开关未生效: %+v", report)
	}
	report = validatePhysicalGMT(t, input, validator.WithCheckDigest(false), validator.WithSignatureVerification(f.options))
	found = false
	for _, issue := range report.Issues {
		if issue.Code == "signature.physical.document.integrity" {
			found = true
		}
	}
	if !report.HasErrors() || !found {
		t.Fatalf("关闭普通摘要绕过完整验签: %+v", report)
	}
}
