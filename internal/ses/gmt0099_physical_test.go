package ses_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/integrity"
	"github.com/zc310/ofd/pkg/sign"
)

func physicalSigned(t *testing.T, f *signingFixture, files map[string][]byte, plaintextMap bool) []byte {
	t.Helper()
	var encrypted, signed bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), gmtArchive(t, files), &encrypted, crypt.EncryptOptions{
		KeepRoot: true, PlaintextMap: plaintextMap, IndependentKeys: true,
		Recipients: []crypt.EncryptRecipient{{UserName: "test", Password: []byte("secret")}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sign.Sign(t.Context(), encrypted.Bytes(), &signed, sign.Options{Type: "Sign", Physical: true, Date: f.now, Sign: digitalSigner(t, f, false)}); err != nil {
		t.Fatal(err)
	}
	return signed.Bytes()
}

func TestGMT0099PhysicalEncryptedContentAndTimestampManifestFullyVerify(t *testing.T) {
	for _, plaintextMap := range []bool{false, true} {
		name := "encrypted_map"
		if plaintextMap {
			name = "plaintext_map"
		}
		t.Run(name, func(t *testing.T) {
			f := newSigningFixture(t, false)
			f.options.CurrentTime = f.now.Add(2 * time.Second)
			signed := physicalSigned(t, f, gmtFiles(), plaintextMap)
			results, err := sign.VerifyPhysical(t.Context(), signed, *f.options)
			if err != nil || len(results) != 1 || !results[0].DigestValid || results[0].Scope != "physical" || !results[0].Result.FullyValid() {
				t.Fatalf("物理验签失败: %+v %v", results, err)
			}
			view, err := crypt.Open(t.Context(), signed, crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			results, err = sign.Verify(t.Context(), view, *f.options)
			if err != nil || len(results) != 1 || !results[0].Result.FullyValid() || results[0].Scope != "physical" {
				t.Fatalf("解密视图未明确物理验签域: %+v %v", results, err)
			}
			protected := protectGMT(t, f, signed, true)
			report, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options, Decrypt: &crypt.DecryptOptions{Password: []byte("secret")}})
			if err != nil || report.Status != "valid" || len(report.Unprotected) != 0 || len(report.Unreferenced) != 0 || len(report.PhysicalSignatures) != 1 {
				t.Fatalf("加密包未完整通过: %+v %v", report, err)
			}
			report, err = integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options})
			if err != nil || report.Status != "incomplete" || len(report.Unprotected) != 0 || !report.PhysicalSignatures[0].Result.FullyValid() {
				t.Fatalf("无凭据应保留物理覆盖并保持闭包未完成: %+v %v", report, err)
			}
			report, err = integrity.Verify(t.Context(), protected, integrity.Options{Decrypt: &crypt.DecryptOptions{Password: []byte("secret")}, Verification: sign.VerificationOptions{CurrentTime: f.options.CurrentTime}})
			if err != nil || report.Status != "incomplete" || len(report.Unprotected) == 0 {
				t.Fatalf("缺信任证据误报受信覆盖: %+v %v", report, err)
			}
			if _, err := crypt.Decrypt(t.Context(), signed, io.Discard, crypt.DecryptOptions{Password: []byte("secret")}); !errors.Is(err, crypt.ErrProtectedContent) {
				t.Fatalf("物化解密破坏外签: %v", err)
			}
		})
	}
}

func TestGMT0099PhysicalSignatureDetectsTamperingBeforeDecryption(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	protected := protectGMT(t, f, physicalSigned(t, f, gmtFiles(), false), true)
	files := gmtUnpack(t, protected)
	var seed, mapping, ciphertext string
	for name := range files {
		switch {
		case strings.Contains(name, "seed"):
			seed = name
		case strings.Contains(name, "entriesmap"):
			mapping = name
		case strings.HasPrefix(name, "Encrypt_") && strings.HasSuffix(name, ".dat"):
			ciphertext = name
		}
	}
	if seed == "" || mapping == "" || ciphertext == "" {
		t.Fatalf("未找到加密产物: %v", files)
	}
	for _, name := range []string{"Encryptions.xml", seed, mapping, ciphertext, "Doc_0/Signatures.xml", "Doc_0/Signatures/sign-1/Signature_sign-1.xml", "Doc_0/Signatures/sign-1/Data/sign-1.dat"} {
		t.Run(name, func(t *testing.T) {
			changed := gmtUnpack(t, protected)
			data := bytes.Clone(changed[name])
			position := len(data) / 2
			if strings.Contains(name, "/Data/") {
				position = len(data) - 1
			}
			data[position] ^= 1
			changed[name] = data
			report, err := integrity.Verify(t.Context(), gmtArchive(t, changed), integrity.Options{Verification: *f.options, Decrypt: &crypt.DecryptOptions{Password: []byte("wrong password")}})
			if err != nil || report.Status != "invalid" {
				t.Fatalf("篡改未在解密前检出: %+v %v", report, err)
			}
		})
	}
}

func TestGMT0099PhysicalCoverageDoesNotAdmitUnreferencedPayload(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	files := gmtFiles()
	files["payload.bin"] = []byte("honestly signed, listed and encrypted, but unrelated to document")
	protected := protectGMT(t, f, physicalSigned(t, f, files, false), true)
	report, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options, Decrypt: &crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil || report.Status != "invalid" || len(report.Unreferenced) == 0 || len(report.Unprotected) != 0 {
		t.Fatalf("外签不能洗白夹带: %+v %v", report, err)
	}
}

func TestGMT0099PhysicalSigningRejectsExistingProtectionBeforeSigner(t *testing.T) {
	f := newSigningFixture(t, false)
	var plainSigned bytes.Buffer
	if err := sign.Sign(t.Context(), gmtArchive(t, gmtFiles()), &plainSigned, sign.Options{Type: "Sign", Sign: digitalSigner(t, f, false), Date: f.now}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"root_encrypted", "existing_signature", "manifest"} {
		t.Run(name, func(t *testing.T) {
			input := gmtArchive(t, gmtFiles())
			if name == "existing_signature" {
				input = plainSigned.Bytes()
			}
			var encrypted bytes.Buffer
			if _, err := crypt.Encrypt(t.Context(), input, &encrypted, crypt.EncryptOptions{KeepRoot: name != "root_encrypted", Recipients: []crypt.EncryptRecipient{{UserName: "test", Password: []byte("secret")}}}); err != nil {
				t.Fatal(err)
			}
			input = encrypted.Bytes()
			if name == "manifest" {
				files := gmtUnpack(t, input)
				files["OFDEntries.xml"] = []byte("existing protection")
				input = gmtArchive(t, files)
			}
			called := false
			var output bytes.Buffer
			err := sign.Sign(t.Context(), input, &output, sign.Options{Type: "Sign", Physical: true, Sign: func(context.Context, []byte) ([]byte, error) {
				called = true
				return nil, errors.New("不得调用签名器")
			}})
			if err == nil || called || output.Len() != 0 {
				t.Fatalf("预检未拒绝: %v called=%v", err, called)
			}
		})
	}
}

func TestGMT0099PhysicalSignatureNeverUsesSameNamedPlaintext(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), gmtArchive(t, gmtFiles()), &encrypted, crypt.EncryptOptions{KeepRoot: true, PlaintextMap: true, Recipients: []crypt.EncryptRecipient{{UserName: "test", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	info, err := crypt.Inspect(t.Context(), encrypted.Bytes(), crypt.InspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	files := gmtUnpack(t, encrypted.Bytes())
	operation := info.Operations[0]
	for _, mapping := range operation.Entries {
		files[mapping.Path] = files[mapping.EPath]
		delete(files, mapping.EPath)
		files[operation.EntriesMapLoc] = bytes.ReplaceAll(files[operation.EntriesMapLoc], []byte("/"+mapping.EPath), []byte("/"+mapping.Path))
	}
	var signed bytes.Buffer
	if err := sign.Sign(t.Context(), gmtArchive(t, files), &signed, sign.Options{Type: "Sign", Physical: true, Date: f.now, Sign: digitalSigner(t, f, false)}); err != nil {
		t.Fatal(err)
	}
	view, err := crypt.Open(t.Context(), signed.Bytes(), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	results, err := sign.Verify(t.Context(), view, *f.options)
	if err != nil || len(results) != 1 || !results[0].Result.FullyValid() || results[0].Scope != "physical" {
		t.Fatalf("物理域被同名明文覆盖: %+v %v", results, err)
	}
	protected := protectGMT(t, f, signed.Bytes(), true)
	report, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options, Decrypt: &crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil || report.Status != "valid" {
		t.Fatalf("同名路径验证失败: %+v %v", report, err)
	}
}

func TestGMT0099PhysicalVerificationRejectsRestoredBootstrapAndEnforcesBudgets(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	protected := protectGMT(t, f, physicalSigned(t, f, gmtFiles(), false), true)
	for _, limit := range []int64{32, 2048} {
		_, err := integrity.Verify(t.Context(), protected, integrity.Options{Verification: *f.options, Limits: crypt.Limits{MaxProcessedBytes: limit}, Decrypt: &crypt.DecryptOptions{Password: []byte("secret")}})
		if !errors.Is(err, crypt.ErrLimitExceeded) {
			t.Fatalf("物理验签预算未生效: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sign.VerifyPhysical(ctx, protected, *f.options); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误丢失: %v", err)
	}
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), gmtArchive(t, gmtFiles()), &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "test", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	files := gmtUnpack(t, encrypted.Bytes())
	files["OFD.xml"] = gmtFiles()["OFD.xml"] // 加密映射仍会恢复原入口，覆盖新增的外签入口。
	var signed bytes.Buffer
	if err := sign.Sign(t.Context(), gmtArchive(t, files), &signed, sign.Options{Type: "Sign", Physical: true, Date: f.now, Sign: digitalSigner(t, f, false)}); err != nil {
		t.Fatal(err)
	}
	report, err := integrity.Verify(t.Context(), protectGMT(t, f, signed.Bytes(), true), integrity.Options{Verification: *f.options, Decrypt: &crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil || report.Status != "invalid" {
		t.Fatalf("恢复覆盖了外签入口却被接受: %+v %v", report, err)
	}
	files = gmtFiles()
	files["Doc_0/Signatures/sign-1/Data/sign-1.dat"] = []byte("mapping replaces the new outer signature value")
	encrypted.Reset()
	if _, err := crypt.Encrypt(t.Context(), gmtArchive(t, files), &encrypted, crypt.EncryptOptions{KeepRoot: true, Recipients: []crypt.EncryptRecipient{{UserName: "test", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	signed.Reset()
	if err := sign.Sign(t.Context(), encrypted.Bytes(), &signed, sign.Options{Type: "Sign", Physical: true, Date: f.now, Sign: digitalSigner(t, f, false)}); err != nil {
		t.Fatal(err)
	}
	report, err = integrity.Verify(t.Context(), protectGMT(t, f, signed.Bytes(), true), integrity.Options{Verification: *f.options, Decrypt: &crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil || report.Status != "invalid" {
		t.Fatalf("恢复覆盖了外签值却被接受: %+v %v", report, err)
	}
	want := errors.New("output failed")
	if err := sign.Sign(t.Context(), encrypted.Bytes(), gmtFailWriter{want}, sign.Options{Type: "Sign", Physical: true, Date: f.now, Sign: digitalSigner(t, f, false)}); !errors.Is(err, want) {
		t.Fatalf("输出错误丢失: %v", err)
	}
}

func TestGMT0099PhysicalSignatureCoversMultipleEncryptionLayers(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	input := gmtArchive(t, gmtFiles())
	for n := 0; n < 2; n++ {
		options := crypt.EncryptOptions{KeepRoot: true, IndependentKeys: true, Recipients: []crypt.EncryptRecipient{{UserName: "test", Password: []byte("secret")}}}
		if n > 0 {
			options.Append = &crypt.DecryptOptions{Password: []byte("secret")}
		}
		var encrypted bytes.Buffer
		if _, err := crypt.Encrypt(t.Context(), input, &encrypted, options); err != nil {
			t.Fatal(err)
		}
		input = bytes.Clone(encrypted.Bytes())
	}
	var signed bytes.Buffer
	if err := sign.Sign(t.Context(), input, &signed, sign.Options{Type: "Sign", Physical: true, Date: f.now, Sign: digitalSigner(t, f, false)}); err != nil {
		t.Fatal(err)
	}
	report, err := integrity.Verify(t.Context(), protectGMT(t, f, signed.Bytes(), true), integrity.Options{Verification: *f.options, Decrypt: &crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil || report.Status != "valid" {
		t.Fatalf("多层物理包验证失败: %+v %v", report, err)
	}
}

func TestGMT0099VerifyRequiresUnlockedViewEvenWithValidPhysicalSignature(t *testing.T) {
	f := newSigningFixture(t, false)
	f.options.CurrentTime = f.now.Add(2 * time.Second)
	signed := physicalSigned(t, f, gmtFiles(), true)
	for _, partial := range []bool{false, true} {
		opt := crypt.OpenOptions{AllowPartial: true}
		expectedState := "locked"
		if partial {
			expectedState = "partially_unlocked"
			opt.Resolve = func(_ context.Context, request crypt.CredentialRequest) (crypt.Credentials, error) {
				if strings.HasSuffix(request.DecryptSeedLoc, "000001.seed") {
					return crypt.Credentials{}, nil
				}
				return crypt.Credentials{Password: []byte("secret")}, nil
			}
		}
		view, err := crypt.Open(t.Context(), signed, opt)
		if err != nil {
			t.Fatal(err)
		}
		if state := view.EncryptionInfo().State; state != expectedState {
			t.Fatalf("state=%s want=%s", state, expectedState)
		}
		results, err := sign.Verify(t.Context(), view, *f.options)
		if !errors.Is(err, crypt.ErrLocked) || len(results) != 0 {
			t.Fatalf("未完整解锁仍返回签名结果: %+v %v", results, err)
		}
		if err := view.Close(); err != nil {
			t.Fatal(err)
		}
		results, err = sign.VerifyPhysical(t.Context(), signed, *f.options)
		if err != nil || len(results) != 1 || !results[0].Result.FullyValid() {
			t.Fatalf("显式物理验签受凭据限制: %+v %v", results, err)
		}
	}
}
