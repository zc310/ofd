package ses_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emmansun/gmsm/pkcs7"
	"github.com/emmansun/gmsm/sm3"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/integrity"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/validator"
)

func gmtFiles() map[string][]byte {
	return map[string][]byte{
		"OFD.xml":                 []byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>1234567890abcdef1234567890abcdef</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`),
		"Doc_0/Document.xml":      []byte(`<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>2</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Pages/Content.xml"/></Pages></Document>`),
		"Doc_0/Pages/Content.xml": []byte(`<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="2"/></Content></Page>`),
	}
}

func gmtArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for name, data := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func gmtUnpack(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	p, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	out := map[string][]byte{}
	for _, entry := range p.Entries() {
		content, err := p.Read(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Path] = content
	}
	return out
}

func digitalOptions(t *testing.T, f *signingFixture, timestamp bool) sign.DigitalSigningOptions {
	t.Helper()
	opt := sign.DigitalSigningOptions{Time: f.now, Intermediates: [][]byte{f.root.Raw}}
	if timestamp {
		opt.Timestamp = func(signature []byte) ([]byte, error) { return makeTimestamp(t, f, signature, false), nil }
	}
	return opt
}

func digitalSigner(t *testing.T, f *signingFixture, timestamp bool) func(context.Context, []byte) ([]byte, error) {
	return func(ctx context.Context, data []byte) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return sign.CreateDigitalSignedValue(data, f.signer.Raw, f.signerKey, digitalOptions(t, f, timestamp))
	}
}

func TestGMT0099OrdinarySignedDataBindsExactBytesAndCertificateEvidence(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	data := []byte("<?xml version=\"1.0\"?>\r\n<Signature>original bytes</Signature>\r\n")
	value, err := sign.CreateDigitalSignedValue(data, f.signer.Raw, f.signerKey, digitalOptions(t, f, true))
	if err != nil {
		t.Fatal(err)
	}
	result, err := sign.VerifyDigitalSignedValue(data, value, *f.options)
	if err != nil || !result.Valid || !result.FullyValid() || result.Kind != "Sign" || result.Seal.Valid {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, changed := range [][]byte{bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")), append(bytes.Clone(data), ' ')} {
		result, err := sign.VerifyDigitalSignedValue(changed, value, *f.options)
		if err != nil || result.Valid || result.Validation.Status != "invalid" {
			t.Fatalf("篡改原文被接受: %+v %v", result, err)
		}
	}
	result, err = sign.VerifyDigitalSignedValue(data, value, sign.VerificationOptions{CurrentTime: f.options.CurrentTime})
	if err != nil || !result.Valid || result.FullyValid() || result.Validation.Status != "incomplete" {
		t.Fatalf("缺证据结果=%+v %v", result, err)
	}
	badTimestamp := bytes.Clone(value)
	badTimestamp[len(badTimestamp)-1] ^= 1
	result, err = sign.VerifyDigitalSignedValue(data, badTimestamp, *f.options)
	if err != nil || !result.Valid || result.Validation.Status != "invalid" {
		t.Fatalf("时间戳失败应保留数学签名结果: %+v %v", result, err)
	}
	options := *f.options
	options.UID = []byte("wrong UID")
	result, err = sign.VerifyDigitalSignedValue(data, value, options)
	if err != nil || result.Valid {
		t.Fatalf("错误 UID 被接受: %+v %v", result, err)
	}
}

func TestGMT0099ReadsIndependentGMSMSignedDataAttachedAndDetached(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	for _, detached := range []bool{false, true} {
		data := []byte("independent SignedData encoder")
		sd, err := pkcs7.NewSMSignedData(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := sd.AddSigner(f.signer, f.signerKey, pkcs7.SignerInfoConfig{}); err != nil {
			t.Fatal(err)
		}
		if detached {
			sd.Detach()
		}
		value, err := sd.Finish()
		if err != nil {
			t.Fatal(err)
		}
		result, err := sign.VerifyDigitalSignedValue(data, value, *f.options)
		if err != nil || !result.Valid || !result.FullyValid() {
			t.Fatalf("detached=%v result=%+v err=%v", detached, result, err)
		}
	}
}

func TestGMT0099SealImageLocIsProtectedAndLoadedForStamp(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	image := makeTestSealPNG(t)
	imagePath := filepath.Join(t.TempDir(), "seal.png")
	if err := os.WriteFile(imagePath, image, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &out, sign.Options{Type: "Sign", ProviderName: "test", Sign: digitalSigner(t, f, false), Date: f.now, SealImage: imagePath, Stamp: &sign.StampOptions{PageRef: "1", Boundary: "0 0 10 10"}}); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	seals := ofd.Documents[0].GetSeals(1)
	if len(seals) != 1 || seals[0].SealData.FileType != "png" || !bytes.Equal(seals[0].SealData.Data, image) {
		t.Fatal("ImageLoc 的印模图片未加载")
	}
	results, err := sign.Verify(t.Context(), out.Bytes(), *f.options)
	if err != nil || !results[0].Result.FullyValid() {
		t.Fatalf("印模签名验证失败: %+v %v", results, err)
	}
}

func TestGMT0099RelativeRequiresBaseSignatureAndRejectsInvalidGraphs(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	files := gmtFiles()
	files["OFD.xml"] = bytes.Replace(files["OFD.xml"], []byte("</DocBody>"), []byte("<Signatures>Doc_0/Signatures.xml</Signatures></DocBody>"), 1)
	files["Doc_0/Signatures.xml"] = []byte(`<Signatures xmlns="http://www.ofdspec.org/2016"><Signature ID="base" Type="Sign" BaseLoc="Signatures/base.xml"/><Signature ID="child" Type="Sign" Relative="base" BaseLoc="Signatures/child.xml"/></Signatures>`)
	sum := sm3.Sum(files["Doc_0/Document.xml"])
	for _, id := range []string{"base", "child"} {
		data := []byte(fmt.Sprintf(`<Signature xmlns="http://www.ofdspec.org/2016"><SignedInfo><Provider ProviderName="test"/><SignatureMethod>1.2.156.10197.1.501</SignatureMethod><SignatureDateTime>%s</SignatureDateTime><References CheckMethod="SM3"><Reference FileRef="/Doc_0/Document.xml"><CheckValue>%s</CheckValue></Reference></References></SignedInfo><SignedValue>%s.dat</SignedValue></Signature>`, f.now.Format(time.RFC3339), base64.StdEncoding.EncodeToString(sum[:]), id))
		value, err := digitalSigner(t, f, false)(t.Context(), data)
		if err != nil {
			t.Fatal(err)
		}
		files["Doc_0/Signatures/"+id+".xml"], files["Doc_0/Signatures/"+id+".dat"] = data, value
	}
	results, err := sign.Verify(t.Context(), gmtArchive(t, files), *f.options)
	if err != nil || len(results) != 2 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	for _, result := range results {
		if !result.Result.FullyValid() {
			t.Fatalf("基础签名未通过: %+v", result)
		}
	}
	base := bytes.Clone(files["Doc_0/Signatures/base.dat"])
	files["Doc_0/Signatures/base.dat"][len(base)-1] ^= 1
	results, err = sign.Verify(t.Context(), gmtArchive(t, files), *f.options)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.ID == "child" && (!result.Result.Valid || result.Result.Validation.Status != "invalid") {
			t.Fatalf("基础签名失败未传递: %+v", result)
		}
	}
	files["Doc_0/Signatures/base.dat"] = base
	index := bytes.Clone(files["Doc_0/Signatures.xml"])
	for _, relative := range []string{"child", "absent"} {
		files["Doc_0/Signatures.xml"] = bytes.Replace(index, []byte(`Relative="base"`), []byte(`Relative="`+relative+`"`), 1)
		if _, err := sign.Verify(t.Context(), gmtArchive(t, files), *f.options); err == nil {
			t.Fatal("Relative 循环/悬空未拒绝")
		}
	}
}

func TestGMT0099OrdinaryOFDProtectsFinalRootAndSignatureIndex(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	var out bytes.Buffer
	err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &out, sign.Options{Type: "Sign", ProviderName: "test", Sign: digitalSigner(t, f, true), Date: f.now, ProtocolVersion: "1", ExtendData: []byte("provider extension"), Parameters: []sign.Parameter{{Name: "purpose", Type: "string", Value: "test"}}})
	if err != nil {
		t.Fatal(err)
	}
	results, err := sign.Verify(t.Context(), out.Bytes(), *f.options)
	if err != nil || len(results) != 1 || !results[0].DigestValid || !results[0].Result.FullyValid() {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	files := gmtUnpack(t, out.Bytes())
	var signature models.Signature
	if err := xml.Unmarshal(files["Doc_0/Signatures/Signature_sign-1.xml"], &signature); err != nil || signature.SignedInfo.Provider.ProtocolVer != "1" || signature.SignedInfo.Provider.ExtendData != base64.StdEncoding.EncodeToString([]byte("provider extension")) || len(signature.SignedInfo.Parameters.Items) != 1 {
		t.Fatalf("附录 B 字段丢失: %+v %v", signature, err)
	}
	v, err := validator.New(validator.WithMode(validator.ModeStructural), validator.WithSignatureVerification(f.options))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(t.Context(), bytes.NewReader(out.Bytes()), "ordinary.ofd")
	for _, issue := range report.Issues {
		if issue.Stage == validator.StageSignature {
			t.Fatalf("普通签名被错误分派: %+v", issue)
		}
	}
	strict, err := validator.New(validator.WithGMT0099(true), validator.WithSignatureVerification(f.options))
	if err != nil {
		t.Fatal(err)
	}
	strictReport := strict.ValidateReader(t.Context(), bytes.NewReader(out.Bytes()), "ordinary.ofd")
	for _, issue := range strictReport.Issues {
		if strings.Contains(issue.File, "Signatures") && (issue.Stage == validator.StageXSD || issue.Stage == validator.StageSignature) {
			t.Fatalf("附录 B 扩展未通过严格检查: %+v", issue)
		}
	}
	files["Doc_0/Signatures.xml"] = bytes.ReplaceAll(files["Doc_0/Signatures.xml"], []byte(`Type="Sign"`), []byte(`Type="Seal"`))
	results, err = sign.Verify(t.Context(), gmtArchive(t, files), *f.options)
	if err == nil && len(results) == 1 && results[0].Result != nil && results[0].Result.FullyValid() {
		t.Fatal("类型替换被接受")
	}
}

func TestGMT0099ProviderExtendDataUsesStandardChildElement(t *testing.T) {
	f := newSigningFixture(t, false)
	var out bytes.Buffer
	if err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &out, sign.Options{Type: "Sign", Date: f.now, ExtendData: []byte("test"), Sign: digitalSigner(t, f, false)}); err != nil {
		t.Fatal(err)
	}
	files := gmtUnpack(t, out.Bytes())
	const name = "Doc_0/Signatures/Signature_sign-1.xml"
	original := files[name]
	if !bytes.Contains(original, []byte("<ExtendData>dGVzdA==</ExtendData>")) || bytes.Contains(original, []byte("ExtendData=")) {
		t.Fatalf("Provider 扩展未按图 B.2 写成子元素: %s", original)
	}
	v, err := validator.New(validator.WithGMT0099(true), validator.WithCheckDigest(false))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		xml   []byte
		valid bool
	}{
		{"standard_child", original, true},
		{"nonstandard_attribute", bytes.Replace(original, []byte("><ExtendData>dGVzdA==</ExtendData></Provider>"), []byte(` ExtendData="dGVzdA=="/>`), 1), false},
		{"invalid_base64", bytes.Replace(original, []byte("<ExtendData>dGVzdA==</ExtendData>"), []byte("<ExtendData>%%%</ExtendData>"), 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			files[name] = test.xml
			report := v.ValidateReader(t.Context(), bytes.NewReader(gmtArchive(t, files)), "extend-data.ofd")
			var schemaError bool
			for _, issue := range report.Issues {
				if issue.File == name && issue.Stage == validator.StageXSD {
					schemaError = true
				}
			}
			if schemaError == test.valid {
				t.Fatalf("valid=%v report=%+v", test.valid, report)
			}
		})
	}
}

func protectGMT(t *testing.T, f *signingFixture, data []byte, timestamp bool) []byte {
	t.Helper()
	var out bytes.Buffer
	_, err := integrity.Protect(t.Context(), data, &out, integrity.ProtectOptions{Sign: digitalSigner(t, f, timestamp), Date: f.now, Options: integrity.Options{Verification: *f.options}})
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestGMT0099IntegrityRejectsListedSmugglingAndDoesNotInventContentHashes(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	files := gmtFiles()
	files["payload.bin"] = []byte("unreferenced but honestly listed")
	protected := protectGMT(t, f, gmtArchive(t, files), true)
	r, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "invalid" || len(r.Unreferenced) != 1 || r.Unreferenced[0] != "payload.bin" || len(r.Unlisted) != 0 {
		t.Fatalf("列入可信清单的夹带未检出: %+v", r)
	}
	delete(files, "payload.bin")
	protected = protectGMT(t, f, gmtArchive(t, files), true)
	r, err = integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options})
	if err != nil || r.Status != "incomplete" || len(r.Unreferenced) != 0 || len(r.Unprotected) != 3 {
		t.Fatalf("清单被误当成逐文件摘要: %+v %v", r, err)
	}
	changed := gmtUnpack(t, protected)
	changed["Doc_0/Pages/Content.xml"] = bytes.ReplaceAll(changed["Doc_0/Pages/Content.xml"], []byte("Layer"), []byte("Other"))
	r, err = integrity.Verify(t.Context(), gmtArchive(t, changed), integrity.Options{Verification: *f.options})
	if err != nil || r.Status == "valid" {
		t.Fatalf("仅清单签名不能证明内容未变: %+v %v", r, err)
	}
}

func TestGMT0099SignedContentAndTimestampedManifestCanFullyVerify(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	var signed bytes.Buffer
	if err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &signed, sign.Options{Type: "Sign", ProviderName: "test", Sign: digitalSigner(t, f, false), Date: f.now}); err != nil {
		t.Fatal(err)
	}
	protected := protectGMT(t, f, signed.Bytes(), true)
	r, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options})
	if err != nil || r.Status != "valid" || !r.Signature.FullyValid() || len(r.Unprotected) != 0 {
		t.Fatalf("完整验证失败: %+v %v", r, err)
	}
	changed := gmtUnpack(t, protected)
	changed["Doc_0/Pages/Content.xml"] = bytes.ReplaceAll(changed["Doc_0/Pages/Content.xml"], []byte(`ID="2"`), []byte(`ID="3"`))
	r, err = integrity.Verify(t.Context(), gmtArchive(t, changed), integrity.Options{Verification: *f.options})
	if err != nil || r.Status == "valid" {
		t.Fatalf("篡改内容后仍完整通过: %+v %v", r, err)
	}
}

func TestGMT0099IntegrityRequiresTimestampAndExactFileSet(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	_, err := integrity.Protect(t.Context(), gmtArchive(t, gmtFiles()), io.Discard, integrity.ProtectOptions{Sign: digitalSigner(t, f, false)})
	if !errors.Is(err, integrity.ErrInvalid) {
		t.Fatalf("缺时间戳未拒绝: %v", err)
	}
	protected := protectGMT(t, f, gmtArchive(t, gmtFiles()), true)
	changed := gmtUnpack(t, protected)
	changed["added.bin"] = []byte("added")
	r, err := integrity.Verify(t.Context(), gmtArchive(t, changed), integrity.Options{Verification: *f.options})
	if err != nil || r.Status != "invalid" || len(r.Unlisted) != 1 {
		t.Fatalf("未列出的文件未检出: %+v %v", r, err)
	}
	changed = gmtUnpack(t, protected)
	delete(changed, "Doc_0/Pages/Content.xml")
	r, err = integrity.Inspect(t.Context(), gmtArchive(t, changed), integrity.Options{})
	if err != nil || r.Status != "invalid" || len(r.Missing) != 1 {
		t.Fatalf("清单缺失目标未检出: %+v %v", r, err)
	}
	changed = gmtUnpack(t, protected)
	changed[integrity.EntryPoint] = bytes.ReplaceAll(changed[integrity.EntryPoint], []byte(`ID="f2"`), []byte(`ID="f1"`))
	_, err = integrity.Inspect(t.Context(), gmtArchive(t, changed), integrity.Options{})
	if !errors.Is(err, integrity.ErrInvalid) {
		t.Fatalf("重复 ID 未拒绝: %v", err)
	}
}

func TestGMT0099EncryptedManifestNeedsCredentialsForClosureAndCannotBeExported(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	var encrypted bytes.Buffer
	password := []byte("secret")
	if _, err := crypt.Encrypt(t.Context(), gmtArchive(t, gmtFiles()), &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "test", Password: password}}, IndependentKeys: true}); err != nil {
		t.Fatal(err)
	}
	protected := protectGMT(t, f, encrypted.Bytes(), true)
	r, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options})
	if err != nil || r.Status != "incomplete" {
		t.Fatalf("锁定包误报完成: %+v %v", r, err)
	}
	r, err = integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options, Decrypt: &crypt.DecryptOptions{Password: password}})
	if err != nil || len(r.Unreferenced) != 0 {
		t.Fatalf("加密入口闭包失败: %+v %v", r, err)
	}
	if _, err := crypt.Decrypt(t.Context(), protected, io.Discard, crypt.DecryptOptions{Password: password}); !errors.Is(err, crypt.ErrProtectedContent) {
		t.Fatalf("物化绕过完整性保护: %v", err)
	}
}

func TestGMT0099IntegrityDoesNotUseFileListAsTraversalRoot(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	files := gmtFiles()
	files["Doc_0/Document.xml"] = []byte(strings.ReplaceAll(string(files["Doc_0/Document.xml"]), "http://www.ofdspec.org/2016", "unknown"))
	protected := protectGMT(t, f, gmtArchive(t, files), true)
	r, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range r.Checks {
		if check.Code == "references" && check.Status != "incomplete" {
			t.Fatalf("不可解析的引用被当成完整: %+v", r)
		}
	}
}

func TestGMT0099IntegrityBudgetsCancellationAndWriterErrors(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	input := gmtArchive(t, gmtFiles())
	for _, limits := range []crypt.Limits{{MaxInputBytes: 16}, {MaxEntryBytes: 16}, {MaxEntries: 1}, {MaxMetadataBytes: 16}, {MaxXMLNodes: 1}, {MaxXMLDepth: 1}} {
		report, err := integrity.Protect(t.Context(), input, io.Discard, integrity.ProtectOptions{Sign: digitalSigner(t, f, true), Options: integrity.Options{Limits: limits, Verification: *f.options}})
		if report != nil || !errors.Is(err, crypt.ErrLimitExceeded) {
			t.Fatalf("预算未生效: %+v %v", report, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := integrity.Protect(ctx, input, io.Discard, integrity.ProtectOptions{Sign: digitalSigner(t, f, true)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误丢失: %v", err)
	}
	want := errors.New("writer unavailable")
	report, err := integrity.Protect(t.Context(), input, gmtFailWriter{want}, integrity.ProtectOptions{Sign: digitalSigner(t, f, true), Options: integrity.Options{Verification: *f.options}})
	if report != nil || !errors.Is(err, want) {
		t.Fatalf("输出错误丢失: %+v %v", report, err)
	}
}

type gmtFailWriter struct{ err error }

func (w gmtFailWriter) Write([]byte) (int, error) { return 0, w.err }

// 仅在子进程中运行，复用独立编码的测试 TSA，stdout 不包含测试框架文本。
func TestGMTManifestSignerProcess(t *testing.T) {
	if os.Getenv("OFD_TEST_MANIFEST_SIGNER") != "1" {
		return
	}
	f := newSigningFixtureAt(t, false, time.Now().UTC().Add(-5*time.Second).Truncate(time.Second))
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	value, err := digitalSigner(t, f, true)(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("OFD_TEST_MANIFEST_EVIDENCE")
	if err := os.WriteFile(filepath.Join(dir, "tsa-root.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.root.Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tsa.crl"), f.options.Revocation.CRLs[0], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stdout.Write(value); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestGMT0099CLIProtectAndVerifyWithExternalTimestampSigner(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("外部命令回归需要 Go 构建工具")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "ofd-crypt")
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/ofd-crypt")
	if data, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建 CLI 失败: %v\n%s", err, data)
	}
	f := newSigningFixture(t, false)
	var signed bytes.Buffer
	if err := sign.Sign(ctx, gmtArchive(t, gmtFiles()), &signed, sign.Options{Type: "Sign", ProviderName: "test", Date: f.now, Sign: digitalSigner(t, f, false)}); err != nil {
		t.Fatal(err)
	}
	input, output := filepath.Join(dir, "input.ofd"), filepath.Join(dir, "protected.ofd")
	if err := os.WriteFile(input, signed.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OFD_TEST_MANIFEST_SIGNER", "1")
	t.Setenv("OFD_TEST_MANIFEST_EVIDENCE", dir)
	protect := exec.CommandContext(ctx, binary, "integrity", "protect", "--sign-cmd", os.Args[0]+" -test.run=^TestGMTManifestSignerProcess$", "--format", "json", "-o", output, input)
	var out, diag bytes.Buffer
	protect.Stdout, protect.Stderr = &out, &diag
	if err := protect.Run(); err != nil {
		t.Fatalf("CLI 生成失败: %v\n%s\n%s", err, &out, &diag)
	}
	var report integrity.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Status != "signed" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	t.Setenv("OFD_TEST_MANIFEST_SIGNER", "0")
	root, crl := filepath.Join(dir, "root.pem"), filepath.Join(dir, "root.crl")
	if err := os.WriteFile(root, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.root.Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crl, f.options.Revocation.CRLs[0], 0600); err != nil {
		t.Fatal(err)
	}
	verify := exec.CommandContext(ctx, binary, "integrity", "verify", "--trust-root", root, "--trust-root", filepath.Join(dir, "tsa-root.pem"), "--issuer", root, "--issuer", filepath.Join(dir, "tsa-root.pem"), "--crl", crl, "--crl", filepath.Join(dir, "tsa.crl"), "--format", "json", output)
	out.Reset()
	diag.Reset()
	verify.Stdout, verify.Stderr = &out, &diag
	if err := verify.Run(); err != nil {
		t.Fatalf("CLI 完整验证失败: %v\n%s\n%s", err, &out, &diag)
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Status != "valid" {
		t.Fatalf("CLI 报告=%+v err=%v", report, err)
	}
	// 同一外部测试 TSA 流程还验证加密物理包，证据与解密凭据均经真实 CLI 传入。
	physicalInput, physicalOutput := filepath.Join(dir, "physical.ofd"), filepath.Join(dir, "physical-protected.ofd")
	if err := os.WriteFile(physicalInput, physicalSigned(t, f, gmtFiles(), false), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OFD_TEST_MANIFEST_SIGNER", "1")
	protect = exec.CommandContext(ctx, binary, "integrity", "protect", "--sign-cmd", os.Args[0]+" -test.run=^TestGMTManifestSignerProcess$", "--format", "json", "-o", physicalOutput, physicalInput)
	out.Reset()
	diag.Reset()
	protect.Stdout, protect.Stderr = &out, &diag
	if err := protect.Run(); err != nil {
		t.Fatalf("物理包 CLI 保护失败: %v\n%s\n%s", err, &out, &diag)
	}
	t.Setenv("OFD_TEST_MANIFEST_SIGNER", "0")
	args := append([]string{}, verify.Args[1:len(verify.Args)-1]...)
	args = append(args, "--password-file", "-", physicalOutput)
	verify = exec.CommandContext(ctx, binary, args...)
	verify.Stdin = strings.NewReader("secret")
	out.Reset()
	diag.Reset()
	verify.Stdout, verify.Stderr = &out, &diag
	if err := verify.Run(); err != nil {
		t.Fatalf("物理包 CLI 完整验证失败: %v\n%s\n%s", err, &out, &diag)
	}
	report = integrity.Report{}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Status != "valid" || len(report.PhysicalSignatures) != 1 || len(report.Unprotected) != 0 {
		t.Fatalf("物理包 CLI 报告=%+v err=%v", report, err)
	}
}
