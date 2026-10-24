package crypt_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/analyzer"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/validator"
)

func independentKeyFiles(t *testing.T, originals map[string]string, encryptedMap bool) map[string]string {
	t.Helper()
	files := encryptedFiles(t, originals, 1, false, false)
	info := inspect(t, files, crypt.InspectOptions{})
	v := readVectors(t)
	key, iv := bytes.Repeat([]byte{0x42}, 16), bytes.Repeat([]byte{0x17}, 16)
	seed := strings.ReplaceAll(files["Security/seed.xml"], "测试用户", "independent-user")
	seed = strings.Replace(seed, base64.StdEncoding.EncodeToString(hexBytes(t, v.Passwords[1].Wrapped)), base64.StdEncoding.EncodeToString(testEncrypt(t, key, hexBytes(t, v.Passwords[2].KEK), iv)), 1)
	seed = strings.Replace(seed, base64.StdEncoding.EncodeToString(hexBytes(t, v.IV)), base64.StdEncoding.EncodeToString(iv), 1)
	files["Security/independent.seed"] = seed
	for _, entry := range info.Operations[0].Entries {
		if entry.Path == "Doc_0/data.bin" {
			files[entry.EPath] = string(testEncrypt(t, []byte(originals[entry.Path]), key, iv))
			files["Security/map.dat"] = strings.Replace(files["Security/map.dat"], `Path="/`+entry.Path+`"`, `Path="/`+entry.Path+`" DecryptSeedLoc="independent.seed"`, 1)
		}
	}
	if encryptedMap {
		files["Security/map.dat"] = string(testEncrypt(t, []byte(files["Security/map.dat"]), hexBytes(t, v.FEK), hexBytes(t, v.IV)))
	}
	return files
}

func operationElement(root string) string {
	start := strings.Index(root, "<ofd:EncryptInfo")
	end := strings.Index(root, "</ofd:EncryptInfo>") + len("</ofd:EncryptInfo>")
	return root[start:end]
}

func nestedFiles(t *testing.T, originals map[string]string, outerFirst bool) map[string]string {
	t.Helper()
	inner := encryptedFiles(t, originals, 1, false, false)
	innerRoot := strings.ReplaceAll(inner["Encryptions.xml"], "Security/", "Inner/Security/")
	inner["Security/map.dat"] = strings.ReplaceAll(inner["Security/map.dat"], `EPath="/Cipher/`, `EPath="/Inner/Cipher/`)
	v := readVectors(t)
	inner["Security/map.dat"] = string(testEncrypt(t, []byte(inner["Security/map.dat"]), hexBytes(t, v.FEK), hexBytes(t, v.IV)))
	relocated := map[string]string{}
	for name, data := range inner {
		if name == "Encryptions.xml" {
			continue
		}
		if name == "untouched.bin" {
			relocated[name] = data
		} else {
			relocated["Inner/"+name] = data
		}
	}
	outer := encryptedFiles(t, relocated, 2, false, false)
	outerRoot := strings.ReplaceAll(outer["Encryptions.xml"], `ID="enc1"`, `ID="outer" Relative="enc1"`)
	outer["Security/seed.xml"] = strings.ReplaceAll(outer["Security/seed.xml"], `ID="enc1"`, `ID="outer"`)
	outer["Security/map.dat"] = strings.ReplaceAll(outer["Security/map.dat"], `ID="enc1"`, `ID="outer"`)
	outer["Security/map.dat"] = string(testEncrypt(t, []byte(outer["Security/map.dat"]), hexBytes(t, v.FEK), hexBytes(t, v.IV)))
	a, b := operationElement(innerRoot), operationElement(outerRoot)
	if outerFirst {
		a, b = b, a
	}
	outer["Encryptions.xml"] = `<ofd:Encryptions xmlns:ofd="http://www.ofdspec.org/2016">` + a + b + `</ofd:Encryptions>`
	return outer
}

func nestedCredentials(_ context.Context, request crypt.CredentialRequest) (crypt.Credentials, error) {
	password := "secret"
	if request.OperationID == "outer" {
		password = "中文口令🔐"
	}
	return crypt.Credentials{Password: []byte(password)}, nil
}

func TestIndependentEntryKeyUsesResolverAndRestoresOriginals(t *testing.T) {
	originals := minimalOriginals()
	for _, encryptedMap := range []bool{false, true} {
		files := independentKeyFiles(t, originals, encryptedMap)
		calls := map[string]int{}
		resolver := func(_ context.Context, r crypt.CredentialRequest) (crypt.Credentials, error) {
			calls[r.DecryptSeedLoc]++
			pw := "secret"
			if strings.HasSuffix(r.DecryptSeedLoc, "independent.seed") {
				pw = "中文口令🔐"
			}
			return crypt.Credentials{Password: []byte(pw)}, nil
		}
		var out bytes.Buffer
		report, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Resolve: resolver})
		if err != nil {
			t.Fatal(err)
		}
		if calls["Security/seed.xml"] != 1 || calls["Security/independent.seed"] != 1 || len(report.Operations) != 1 {
			t.Fatalf("resolver calls=%v, report=%+v", calls, report)
		}
		checkOriginals(t, out.Bytes(), originals)
	}
}

func TestIndependentSeedCanUseCertificateAlongsidePasswordMap(t *testing.T) {
	originals := minimalOriginals()
	files := encryptedFiles(t, originals, 1, false, false)
	files["Security/certificate.seed"] = encryptedFiles(t, originals, 1, false, true)["Security/seed.xml"]
	files["Security/map.dat"] = strings.Replace(files["Security/map.dat"], `Path="/Doc_0/data.bin"`, `Path="/Doc_0/data.bin" DecryptSeedLoc="certificate.seed"`, 1)
	key := &recordingKey{key: privateKey(t)}
	resolver := func(_ context.Context, r crypt.CredentialRequest) (crypt.Credentials, error) {
		if r.Scheme == crypt.CertificateScheme {
			return crypt.Credentials{PrivateKey: key}, nil
		}
		return crypt.Credentials{Password: []byte("secret")}, nil
	}
	view, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Resolve: resolver}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	for i := 0; i < 3; i++ {
		data, err := view.Read("Doc_0/data.bin")
		if err != nil || string(data) != originals["Doc_0/data.bin"] {
			t.Fatal("certificate entry not restored")
		}
	}
	if key.calls != 1 {
		t.Fatalf("resource reads unexpectedly re-used private key service: %d", key.calls)
	}
	report, err := view.Export(t.Context(), func(string, []byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if report.Scheme != crypt.PasswordScheme || len(report.Operations[0].Keys) != 2 {
		t.Fatalf("mixed seed report=%+v", report)
	}
	for _, seed := range report.Operations[0].Keys {
		if strings.HasSuffix(seed.DecryptSeedLoc, "certificate.seed") && seed.Scheme != crypt.CertificateScheme {
			t.Fatal("independent certificate result mislabeled")
		}
	}
}

func TestNestedDecryptionUsesRelativeRatherThanXMLOrder(t *testing.T) {
	for _, outerFirst := range []bool{false, true} {
		originals := minimalOriginals()
		files := nestedFiles(t, originals, outerFirst)
		var out bytes.Buffer
		report, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Resolve: nestedCredentials})
		if err != nil {
			t.Fatalf("outerFirst=%v: %v", outerFirst, err)
		}
		if len(report.Operations) != 2 || report.Operations[0].ID != "outer" || report.Operations[1].ID != "enc1" {
			t.Fatalf("operation order=%+v", report.Operations)
		}
		checkOriginals(t, out.Bytes(), originals)
	}
}

func TestImplicitCrossLayerDependenciesAreRejected(t *testing.T) {
	for _, outerFirst := range []bool{false, true} {
		files := nestedFiles(t, minimalOriginals(), outerFirst)
		files["Encryptions.xml"] = strings.Replace(files["Encryptions.xml"], ` Relative="enc1"`, "", 1)
		if view, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Resolve: nestedCredentials}}); view != nil || !errors.Is(err, crypt.ErrInvalidMetadata) {
			t.Fatalf("implicit layer dependency accepted: %v", err)
		}
	}
}

func TestPartialViewNeverReadsPlaceholderAndCannotBeExportedOrParsed(t *testing.T) {
	originals := minimalOriginals()
	files := independentKeyFiles(t, originals, false)
	files["Doc_0/data.bin"] = "harmless placeholder"
	resolver := func(_ context.Context, r crypt.CredentialRequest) (crypt.Credentials, error) {
		if strings.HasSuffix(r.DecryptSeedLoc, "independent.seed") {
			return crypt.Credentials{}, crypt.ErrCredentialsRequired
		}
		return crypt.Credentials{Password: []byte("secret")}, nil
	}
	view, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Resolve: resolver}, AllowPartial: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if view.EncryptionInfo().State != "partially_unlocked" {
		t.Fatalf("info=%+v", view.EncryptionInfo())
	}
	if _, err := view.Read("Doc_0/data.bin"); !errors.Is(err, crypt.ErrLocked) {
		t.Fatalf("placeholder leaked: %v", err)
	}
	if data, err := view.Read("OFD.xml"); err != nil || string(data) != originals["OFD.xml"] {
		t.Fatalf("unlocked root=%q, %v", data, err)
	}
	var locked bool
	for _, entry := range view.Entries() {
		if entry.Path == "Doc_0/data.bin" {
			locked = entry.Locked && entry.SizeUnknown
		}
	}
	if !locked {
		t.Fatal("locked logical metadata missing")
	}
	if pkg, err := view.OFDPackage(); pkg != nil || !errors.Is(err, crypt.ErrLocked) {
		t.Fatalf("partial view handed to parser: %v", err)
	}
	if _, err := parser.NewOFD(view); !errors.Is(err, crypt.ErrLocked) {
		t.Fatalf("parser did not preserve locked classification: %v", err)
	}
	if _, err := view.Export(t.Context(), func(string, []byte) error { return nil }); !errors.Is(err, crypt.ErrLocked) {
		t.Fatalf("partial export=%v", err)
	}
}

func TestOpaqueViewBlocksUnknownOverridesEvenWhenClearRootExists(t *testing.T) {
	files := encryptedFiles(t, minimalOriginals(), 1, true, false)
	files["OFD.xml"] = "visible placeholder"
	view, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{AllowPartial: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	info := view.EncryptionInfo()
	if !info.Opaque || info.State != "locked" || len(view.Entries()) != 0 {
		t.Fatalf("opaque info=%+v", info)
	}
	if _, err := view.Read("OFD.xml"); !errors.Is(err, crypt.ErrLocked) {
		t.Fatalf("opaque fallback leaked: %v", err)
	}
	if _, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("wrong")}, AllowPartial: true}); !errors.Is(err, crypt.ErrDecryptFailed) {
		t.Fatalf("wrong credential silently deferred: %v", err)
	}
}

func TestNestedPartialViewDefersPriorLayerAndRetainsPhysicalAudit(t *testing.T) {
	files := nestedFiles(t, minimalOriginals(), false)
	resolver := func(_ context.Context, r crypt.CredentialRequest) (crypt.Credentials, error) {
		if r.OperationID == "enc1" {
			return crypt.Credentials{}, crypt.ErrCredentialsRequired
		}
		return crypt.Credentials{Password: []byte("中文口令🔐")}, nil
	}
	view, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Resolve: resolver}, AllowPartial: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if info := view.EncryptionInfo(); info.State != "partially_unlocked" || !info.Opaque {
		t.Fatalf("nested partial info=%+v", info)
	}
	physical, err := view.OpenPhysicalPackage()
	if err != nil {
		t.Fatal(err)
	}
	defer physical.Close()
	if data, err := physical.Read("Encryptions.xml"); err != nil || string(data) != files["Encryptions.xml"] {
		t.Fatalf("physical root altered: %v", err)
	}
}

func TestViewLeasesAreIndependentAndReturnedDataIsOwnedByCaller(t *testing.T) {
	originals := minimalOriginals()
	view, err := crypt.Open(t.Context(), archive(t, encryptedFiles(t, originals, 1, true, false)), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := view.OFDPackage()
	if err != nil {
		t.Fatal(err)
	}
	second, err := view.OFDPackage()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := view.Read("Doc_0/data.bin")
	if err != nil {
		t.Fatal(err)
	}
	clear(data)
	data, err = second.Read("Doc_0/data.bin")
	if err != nil || string(data) != originals["Doc_0/data.bin"] {
		t.Fatal("caller bytes alias internal storage")
	}
	entry, ok := second.Lookup("Doc_0/data.bin")
	if !ok || entry.UncompressedSize != uint64(len(data)) || entry.CRC32 != crc32.ChecksumIEEE(data) || !entry.Virtual {
		t.Fatalf("logical entry=%+v", entry)
	}
	if _, err := first.OpenEntry(entry); !errors.Is(err, container.ErrPackageClosed) {
		t.Fatalf("closed lease read=%v", err)
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := view.Read("OFD.xml"); !errors.Is(err, container.ErrPackageClosed) {
		t.Fatalf("owner still readable: %v", err)
	}
	if _, err := view.OFDPackage(); !errors.Is(err, container.ErrPackageClosed) {
		t.Fatalf("closed owner can lend: %v", err)
	}
	if _, err := second.Read("OFD.xml"); err != nil {
		t.Fatalf("owner close invalidated active lease: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestViewConcurrentReadersAndClose(t *testing.T) {
	view, err := crypt.Open(t.Context(), archive(t, encryptedFiles(t, minimalOriginals(), 1, true, false)), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		pkg, err := view.OFDPackage()
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer pkg.Close()
			for j := 0; j < 8; j++ {
				if _, err := pkg.Read("OFD.xml"); err != nil {
					t.Errorf("leased read: %v", err)
					return
				}
			}
		}()
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func TestLayerDepthWorkAndCredentialBudgetsAreGlobal(t *testing.T) {
	files := nestedFiles(t, minimalOriginals(), false)
	for _, limits := range []crypt.Limits{{MaxLayers: 1}, {MaxProcessedBytes: 128}, {MaxCredentialAttempts: 1}} {
		if view, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Resolve: nestedCredentials, Limits: limits}}); view != nil || !errors.Is(err, crypt.ErrLimitExceeded) {
			t.Fatalf("limits=%+v, view=%v, err=%v", limits, view, err)
		}
	}
	view, err := crypt.Open(t.Context(), archive(t, encryptedFiles(t, minimalOriginals(), 1, true, false)), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret"), Limits: crypt.Limits{MaxProcessedBytes: 32000}}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	for i := 0; i < 1000; i++ {
		_, err = view.Read("OFD.xml")
		if errors.Is(err, crypt.ErrLimitExceeded) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("repeated reads bypassed cumulative work budget")
}

func TestSignedViewFeedsParserAnalyzerValidatorAndVerificationWithoutZIP(t *testing.T) {
	input, err := os.ReadFile("../../testdata/ofdrw/999.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("missing signed fixture")
	}
	if err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), input, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "user", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	view, err := crypt.Open(t.Context(), encrypted.Bytes(), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	before, err := sign.Verify(t.Context(), input, sign.VerificationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := sign.Verify(t.Context(), view, sign.VerificationOptions{})
	if err != nil || len(after) != len(before) || !after[0].DigestValid || !after[0].Result.Valid {
		t.Fatalf("view signature=%+v err=%v", after, err)
	}
	analysis, err := analyzer.Analyze(view)
	if err != nil || analysis.Encryption.State != "unlocked" || len(analysis.Pages) == 0 {
		t.Fatalf("view analysis state=%s, err=%v", analysis.Status, err)
	}
	v, _ := validator.New(validator.WithMode(validator.ModeStructural))
	report := v.Validate(t.Context(), view, "encrypted-view")
	if report.Encryption == nil || report.Encryption.State != "unlocked" {
		t.Fatalf("view report=%+v", report)
	}
	for _, issue := range report.Issues {
		if issue.Code == "reference.missing" || issue.Code == "digest.mismatch" || issue.Code == "encryption.locked" {
			t.Fatalf("logical validation used physical cipher entries: %+v", issue)
		}
	}
	if _, err := view.Read("OFD.xml"); err != nil {
		t.Fatalf("consumer closed owner: %v", err)
	}
}

func TestPartialViewValidationReportsIncompleteAndPreservesArchiveProfile(t *testing.T) {
	files := independentKeyFiles(t, minimalOriginals(), false)
	view, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{AllowPartial: true, DecryptOptions: crypt.DecryptOptions{Resolve: func(_ context.Context, r crypt.CredentialRequest) (crypt.Credentials, error) {
		if strings.HasSuffix(r.DecryptSeedLoc, "independent.seed") {
			return crypt.Credentials{}, crypt.ErrCredentialsRequired
		}
		return crypt.Credentials{Password: []byte("secret")}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	v, _ := validator.New(validator.WithMode(validator.ModeStructural))
	report := v.Validate(t.Context(), view, "partial-view")
	if report.Status != validator.StatusPartial || report.Encryption.State != "partially_unlocked" || report.ExitCode(false) != 1 {
		t.Fatalf("partial report=%+v", report)
	}
	if _, err := validator.PackageReferences(t.Context(), view); !errors.Is(err, crypt.ErrLocked) {
		t.Fatalf("partial closure=%v", err)
	}
}

func TestUnlockedViewStillFailsOriginalArchiveNoEncryptionRule(t *testing.T) {
	originals := minimalOriginals()
	originals["OFD.xml"] = strings.Replace(originals["OFD.xml"], `DocType="OFD"`, `DocType="OFD-A"`, 1)
	view, err := crypt.Open(t.Context(), archive(t, encryptedFiles(t, originals, 1, true, false)), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	v, _ := validator.New(validator.WithMode(validator.ModeStructural))
	report := v.Validate(t.Context(), view, "archive-view")
	for _, issue := range report.Issues {
		if issue.Code == "profile.ofd_a.encrypted" {
			if report.Encryption.State != "unlocked" {
				t.Fatal("unlock state lost")
			}
			return
		}
	}
	t.Fatalf("original encryption profile was laundered: %+v", report)
}

func TestPartiallyUnlockedRootStillRevealsForbiddenArchiveEncryption(t *testing.T) {
	originals := minimalOriginals()
	originals["OFD.xml"] = strings.Replace(originals["OFD.xml"], `DocType="OFD"`, `DocType="OFD-A"`, 1)
	resolver := func(_ context.Context, r crypt.CredentialRequest) (crypt.Credentials, error) {
		if strings.HasSuffix(r.DecryptSeedLoc, "independent.seed") {
			return crypt.Credentials{}, crypt.ErrCredentialsRequired
		}
		return crypt.Credentials{Password: []byte("secret")}, nil
	}
	view, err := crypt.Open(t.Context(), archive(t, independentKeyFiles(t, originals, false)), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Resolve: resolver}, AllowPartial: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	v, _ := validator.New(validator.WithMode(validator.ModeStructural))
	report := v.Validate(t.Context(), view, "partial-archive")
	for _, issue := range report.Issues {
		if issue.Code == "profile.ofd_a.encrypted" {
			return
		}
	}
	t.Fatalf("partial root did not expose archive profile: %+v", report)
}

func TestViewInfoIsDetachedAndCancelledOpenPreservesError(t *testing.T) {
	files := encryptedFiles(t, minimalOriginals(), 1, false, false)
	view, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	copy := view.EncryptionInfo()
	copy.State = "locked"
	copy.Operations[0].Entries[0].Path = "changed"
	if actual := view.EncryptionInfo(); actual.State != "unlocked" || actual.Operations[0].Entries[0].Path == "changed" {
		t.Fatal("inspection snapshot aliases session")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := crypt.Open(ctx, archive(t, files), crypt.OpenOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if _, err := crypt.Open(t.Context(), archive(t, files), crypt.OpenOptions{AllowPartial: true, DecryptOptions: crypt.DecryptOptions{Resolve: func(context.Context, crypt.CredentialRequest) (crypt.Credentials, error) {
		return crypt.Credentials{}, errors.Join(crypt.ErrCredentialsRequired, context.DeadlineExceeded)
	}}}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resolver deadline was treated as a partial view: %v", err)
	}
	if _, err := view.Export(ctx, func(string, []byte) error { return fmt.Errorf("should not emit") }); !errors.Is(err, context.Canceled) {
		t.Fatalf("export cancel=%v", err)
	}
	pkg, err := view.OFDPackage()
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	entry, _ := pkg.Lookup("OFD.xml")
	reader, err := pkg.OpenEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, container.ErrPackageClosed) {
		t.Fatalf("closed logical reader=%v", err)
	}
}

func TestNestedSignedDocumentPreservesFinalSignatureAndEveryByte(t *testing.T) {
	input, err := os.ReadFile("../../testdata/ofdrw/999.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("missing signed fixture")
	}
	if err != nil {
		t.Fatal(err)
	}
	originals := unpack(t, input)
	view, err := crypt.Open(t.Context(), archive(t, nestedFiles(t, originals, true)), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Resolve: nestedCredentials}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	results, err := sign.Verify(t.Context(), view, sign.VerificationOptions{})
	if err != nil || len(results) != 1 || !results[0].DigestValid || !results[0].Result.Valid {
		t.Fatalf("nested signatures=%+v err=%v", results, err)
	}
	if _, err := view.Export(t.Context(), func(name string, data []byte) error {
		if string(data) != originals[name] {
			return fmt.Errorf("original bytes changed: %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestViewCannotBypassConsumerInputLimitsAndExportCallbackMayCloseOwner(t *testing.T) {
	view, err := crypt.Open(t.Context(), archive(t, encryptedFiles(t, minimalOriginals(), 1, true, false)), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if _, err := parser.NewOFDWithOptions(view, parser.Options{MaxInputBytes: 1}); !errors.Is(err, parser.ErrInputTooLarge) {
		t.Fatalf("parser input budget bypassed: %v", err)
	}
	v, _ := validator.New(validator.WithMaxInputSize(1))
	report := v.Validate(t.Context(), view, "bounded-view")
	if report.Status != validator.StatusInvalid || report.Input.Size <= 1 {
		t.Fatalf("validator input budget bypassed: %+v", report)
	}
	count := 0
	if _, err := view.Export(t.Context(), func(string, []byte) error { count++; return view.Close() }); err != nil || count == 0 {
		t.Fatalf("callback close: %d, %v", count, err)
	}
}
