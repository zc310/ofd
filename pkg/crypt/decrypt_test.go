package crypt_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm4"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/sign"
)

type cryptoVectors struct {
	FEK, IV   string
	Passwords []struct {
		Password, KEK string
		Wrapped       string `json:"wrapped_fek"`
	}
	SM2 struct {
		PrivateKey  string `json:"private_key_pem"`
		Certificate string `json:"certificate_der"`
		Wrapped     string `json:"wrapped_fek"`
	}
}

func readVectors(t *testing.T) cryptoVectors {
	t.Helper()
	data, err := os.ReadFile("../../testdata/crypt/vectors.json")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("missing crypto vectors")
	}
	if err != nil {
		t.Fatal(err)
	}
	var v cryptoVectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func hexBytes(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func testEncrypt(t *testing.T, data, key, iv []byte) []byte {
	t.Helper()
	padding := 16 - len(data)%16
	padded := append(bytes.Clone(data), bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, err := sm4.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(padded, padded)
	return padded
}

func minimalOriginals() map[string]string {
	return map[string]string{
		"OFD.xml":            "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n<ofd:OFD xmlns:ofd=\"http://www.ofdspec.org/2016\" Version=\"1.0\" DocType=\"OFD\"><ofd:DocBody><ofd:DocInfo><ofd:DocID>raw-bytes</ofd:DocID></ofd:DocInfo><ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>\r\n",
		"Doc_0/Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><Pages/></Document>`,
		"Doc_0/empty.bin":    "",
		"Doc_0/data.bin":     "\x00\x01\x02原始字节\xff",
		"untouched.bin":      "not encrypted\x00",
	}
}

// The test packager is independent of the production decryption path. FEK
// wrapping is taken directly from OpenSSL vectors, not locally round-tripped.
func encryptedFiles(t *testing.T, originals map[string]string, passwordIndex int, encryptMap, certificate bool) map[string]string {
	t.Helper()
	v := readVectors(t)
	files := fixtureFiles(t)
	delete(files, "Cipher/root.dat")
	fek, iv := hexBytes(t, v.FEK), hexBytes(t, v.IV)
	seed := files["Security/seed.xml"]
	wk := hexBytes(t, v.Passwords[passwordIndex].Wrapped)
	if certificate {
		wk = hexBytes(t, v.SM2.Wrapped)
		seed = strings.ReplaceAll(seed, "1.1.1", "1.1.2")
		seed = strings.Replace(seed, "<ofd:EncryptedWK>", "<ofd:UserCert>"+v.SM2.Certificate+"</ofd:UserCert><ofd:EncryptedWK>", 1)
	}
	seed = strings.ReplaceAll(seed, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", base64.StdEncoding.EncodeToString(wk))
	seed = strings.Replace(seed, "</ofd:UserInfo>", "<ofd:IVValue>"+base64.StdEncoding.EncodeToString(iv)+"</ofd:IVValue></ofd:UserInfo>", 1)
	files["Security/seed.xml"] = seed
	names := make([]string, 0, len(originals))
	for name := range originals {
		names = append(names, name)
	}
	sort.Strings(names)
	var entries strings.Builder
	entries.WriteString(`<ofd:EncryptedEntries xmlns:ofd="http://www.ofdspec.org/2016" ID="enc1">`)
	for i, name := range names {
		if name == "untouched.bin" {
			files[name] = originals[name]
			continue
		}
		cipherName := fmt.Sprintf("Cipher/entry_%d.dat", i)
		files[cipherName] = string(testEncrypt(t, []byte(originals[name]), fek, iv))
		fmt.Fprintf(&entries, `<ofd:EncryptEntry Path="/%s" EPath="/%s"/>`, name, cipherName)
	}
	entries.WriteString(`</ofd:EncryptedEntries>`)
	data := []byte(entries.String())
	if encryptMap {
		data = testEncrypt(t, data, fek, iv)
	}
	files["Security/map.dat"] = string(data)
	return files
}

func privateKey(t *testing.T) *sm2.PrivateKey {
	t.Helper()
	v := readVectors(t)
	block, _ := pem.Decode([]byte(v.SM2.PrivateKey))
	if block == nil {
		t.Fatal("invalid test PEM")
	}
	key, err := smx509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return key.(*sm2.PrivateKey)
}

func unpack(t *testing.T, data []byte) map[string]string {
	t.Helper()
	pkg, err := core.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	files := map[string]string{}
	for _, entry := range pkg.Entries() {
		if entry.IsDir {
			continue
		}
		content, err := pkg.Read(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Path] = string(content)
	}
	return files
}

func checkOriginals(t *testing.T, data []byte, originals map[string]string) {
	t.Helper()
	actual := unpack(t, data)
	if len(actual) != len(originals) {
		t.Fatalf("output entries=%d, want=%d", len(actual), len(originals))
	}
	for name, expected := range originals {
		got, ok := actual[name]
		if !ok || got != expected {
			t.Fatalf("original bytes not restored for %s (exists=%v)", name, ok)
		}
	}
	info, err := crypt.Inspect(t.Context(), data, crypt.InspectOptions{})
	if err != nil || info.Encrypted {
		t.Fatalf("output remains locked: %+v, %v", info, err)
	}
}

func TestPasswordDecryptRestoresExactBytesForAllInputFormsAndMapModes(t *testing.T) {
	v := readVectors(t)
	for _, encryptedMap := range []bool{false, true} {
		for index, password := range v.Passwords {
			originals := minimalOriginals()
			originals["Doc_0/chunks.bin"] = strings.Repeat("chunk\x00", 15000)
			files := encryptedFiles(t, originals, index, encryptedMap, false)
			// A visible placeholder is overridden, without parsing or rewriting it.
			files["OFD.xml"] = "redacted placeholder"
			data := archive(t, files)
			filename := filepath.Join(t.TempDir(), "encrypted.ofd")
			if err := os.WriteFile(filename, data, 0600); err != nil {
				t.Fatal(err)
			}
			for _, input := range []any{data, filename, bytes.NewReader(data)} {
				var output bytes.Buffer
				pw := append([]byte{}, []byte(password.Password)...)
				report, err := crypt.Decrypt(t.Context(), input, &output, crypt.DecryptOptions{Password: pw})
				if err != nil {
					t.Fatalf("password index %d, map encrypted=%v: %v", index, encryptedMap, err)
				}
				if report.State != "unlocked" || report.Scheme != crypt.PasswordScheme || report.DecryptedEntries != len(originals)-1 || report.OutputEntries != len(originals) {
					t.Fatalf("report=%+v", report)
				}
				if string(pw) != password.Password {
					t.Fatal("caller password mutated")
				}
				checkOriginals(t, output.Bytes(), originals)
			}
		}
	}
}

type recordingKey struct {
	key   *sm2.PrivateKey
	calls int
	err   error
}

func (k *recordingKey) Public() crypto.PublicKey { return k.key.Public() }
func (k *recordingKey) Decrypt(r io.Reader, ciphertext []byte, opts crypto.DecrypterOpts) ([]byte, error) {
	k.calls++
	if k.err != nil {
		return nil, k.err
	}
	if r == nil || len(ciphertext) == 0 || ciphertext[0] != 0x30 || opts != sm2.ASN1DecrypterOpts {
		return nil, errors.New("wrong hardware adapter contract")
	}
	return k.key.Decrypt(r, ciphertext, opts)
}

func TestOpenSSLCertificateDecryptAndExplicitRawCompatibility(t *testing.T) {
	originals := minimalOriginals()
	for _, raw := range []bool{false, true} {
		files := encryptedFiles(t, originals, 1, true, true)
		mode := crypt.ModeStrict
		if raw {
			v := readVectors(t)
			plain, err := sm2.ASN1Ciphertext2Plain(hexBytes(t, v.SM2.Wrapped), nil)
			if err != nil {
				t.Fatal(err)
			}
			files["Security/seed.xml"] = strings.Replace(files["Security/seed.xml"], base64.StdEncoding.EncodeToString(hexBytes(t, v.SM2.Wrapped)), base64.StdEncoding.EncodeToString(plain), 1)
			var discarded bytes.Buffer
			if _, err := crypt.Decrypt(t.Context(), archive(t, files), &discarded, crypt.DecryptOptions{PrivateKey: privateKey(t)}); !errors.Is(err, crypt.ErrUnsupported) {
				t.Fatalf("raw strict error=%v", err)
			}
			mode = crypt.ModeCompat
		}
		key := &recordingKey{key: privateKey(t)}
		var output bytes.Buffer
		report, err := crypt.Decrypt(t.Context(), archive(t, files), &output, crypt.DecryptOptions{PrivateKey: key, Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
		if key.calls != 1 || report.Recipient.CertificateSHA256 == "" {
			t.Fatalf("adapter calls=%d, report=%+v", key.calls, report)
		}
		if raw && (len(report.Diagnostics) == 0 || report.Diagnostics[len(report.Diagnostics)-1].Code != "encryption.sm2_raw") {
			t.Fatal("raw compatibility not diagnosed")
		}
		checkOriginals(t, output.Bytes(), originals)
	}
}

func TestStrictCertificateSelectionCanSkipRawRecipientAndUseASN1Recipient(t *testing.T) {
	files := encryptedFiles(t, minimalOriginals(), 1, true, true)
	v := readVectors(t)
	raw, err := sm2.ASN1Ciphertext2Plain(hexBytes(t, v.SM2.Wrapped), nil)
	if err != nil {
		t.Fatal(err)
	}
	seed := files["Security/seed.xml"]
	start, end := strings.Index(seed, "<ofd:UserInfo"), strings.Index(seed, "</ofd:UserInfo>")+len("</ofd:UserInfo>")
	first := strings.Replace(seed[start:end], "测试用户", "raw-user", 1)
	first = strings.Replace(first, base64.StdEncoding.EncodeToString(hexBytes(t, v.SM2.Wrapped)), base64.StdEncoding.EncodeToString(raw), 1)
	files["Security/seed.xml"] = seed[:start] + first + seed[start:]
	key := &recordingKey{key: privateKey(t)}
	report, err := crypt.Decrypt(t.Context(), archive(t, files), io.Discard, crypt.DecryptOptions{PrivateKey: key})
	if err != nil || report.Recipient.UserName != "测试用户" || key.calls != 1 {
		t.Fatalf("report=%+v, calls=%d, err=%v", report, key.calls, err)
	}
}

func TestDecryptTriesRecipientsAndHonorsUserSelectionAndAttemptBudget(t *testing.T) {
	files := encryptedFiles(t, minimalOriginals(), 1, true, false)
	seed := files["Security/seed.xml"]
	start, end := strings.Index(seed, "<ofd:UserInfo"), strings.Index(seed, "</ofd:UserInfo>")+len("</ofd:UserInfo>")
	first := strings.Replace(seed[start:end], "测试用户", "wrong", 1)
	v := readVectors(t)
	first = strings.Replace(first, base64.StdEncoding.EncodeToString(hexBytes(t, v.Passwords[1].Wrapped)), base64.StdEncoding.EncodeToString(hexBytes(t, v.Passwords[0].Wrapped)), 1)
	files["Security/seed.xml"] = seed[:start] + first + seed[start:]
	for _, opt := range []crypt.DecryptOptions{{Password: []byte("secret")}, {Password: []byte("secret"), UserName: "测试用户", Limits: crypt.Limits{MaxCredentialAttempts: 1}}} {
		var out bytes.Buffer
		report, err := crypt.Decrypt(t.Context(), archive(t, files), &out, opt)
		if err != nil || report.Recipient.UserName != "测试用户" {
			t.Fatalf("report=%+v, err=%v", report, err)
		}
	}
	var out bytes.Buffer
	if _, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret"), Limits: crypt.Limits{MaxCredentialAttempts: 1}}); !errors.Is(err, crypt.ErrLimitExceeded) {
		t.Fatalf("attempt budget error=%v", err)
	}
	if _, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret"), UserName: "absent"}); !errors.Is(err, crypt.ErrDecryptFailed) {
		t.Fatalf("user selection error=%v", err)
	}
}

func TestMissingSchemeRequiresExplicitSelectionAndNeverOverridesUnknownScheme(t *testing.T) {
	files := encryptedFiles(t, minimalOriginals(), 1, true, false)
	files["Security/seed.xml"] = strings.Replace(files["Security/seed.xml"], ` EncryptCaseId="1.1.1"`, "", 1)
	var out bytes.Buffer
	if _, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret"), Mode: crypt.ModeCompat}); !errors.Is(err, crypt.ErrUnsupported) {
		t.Fatalf("missing scheme error=%v", err)
	}
	report, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret"), Mode: crypt.ModeCompat, MissingScheme: crypt.PasswordScheme})
	if err != nil || report.Scheme != crypt.PasswordScheme {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
	files["Security/seed.xml"] = strings.Replace(files["Security/seed.xml"], `ID="enc1"`, `ID="enc1" EncryptCaseId="1.1.99"`, 1)
	if _, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret"), Mode: crypt.ModeCompat, MissingScheme: crypt.PasswordScheme}); !errors.Is(err, crypt.ErrUnsupported) {
		t.Fatalf("unknown scheme overridden: %v", err)
	}
}

func TestDecryptFailureCategoriesAndUnsupportedScopes(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(map[string]string)
		options crypt.DecryptOptions
		want    error
	}{
		{"no_password", nil, crypt.DecryptOptions{}, crypt.ErrCredentialsRequired},
		{"wrong_password", nil, crypt.DecryptOptions{Password: []byte("wrong")}, crypt.ErrDecryptFailed},
		{"empty_map", func(f map[string]string) { f["Security/map.dat"] = "" }, crypt.DecryptOptions{Password: []byte("secret")}, crypt.ErrInvalidMetadata},
		{"missing_cipher", func(f map[string]string) { delete(f, "Cipher/entry_0.dat") }, crypt.DecryptOptions{Password: []byte("secret")}, crypt.ErrInvalidMetadata},
		{"bad_cipher_padding", func(f map[string]string) { f["Cipher/entry_0.dat"] = strings.Repeat("x", 16) }, crypt.DecryptOptions{Password: []byte("secret")}, crypt.ErrDecryptFailed},
		{"truncated_cipher", func(f map[string]string) { f["Cipher/entry_0.dat"] = "short" }, crypt.DecryptOptions{Password: []byte("secret")}, crypt.ErrDecryptFailed},
		{"case_conflict", func(f map[string]string) { f["ofd.xml"] = "placeholder" }, crypt.DecryptOptions{Password: []byte("secret")}, crypt.ErrInvalidMetadata},
		{"file_directory_conflict", func(f map[string]string) { f["Doc_0"] = "file" }, crypt.DecryptOptions{Password: []byte("secret")}, crypt.ErrInvalidMetadata},
		{"missing_epath", func(f map[string]string) {
			f["Security/map.dat"] = strings.Replace(f["Security/map.dat"], ` EPath="/Cipher/entry_0.dat"`, "", 1)
		}, crypt.DecryptOptions{Password: []byte("secret")}, crypt.ErrUnsupported},
		{"multiple_operations", func(f map[string]string) {
			f["Encryptions.xml"] = strings.Replace(f["Encryptions.xml"], "</ofd:Encryptions>", `<ofd:EncryptInfo ID="later" Relative="enc1"/></ofd:Encryptions>`, 1)
		}, crypt.DecryptOptions{Password: []byte("secret")}, crypt.ErrInvalidMetadata},
		{"entry_budget", nil, crypt.DecryptOptions{Password: []byte("secret"), Limits: crypt.Limits{MaxEntryBytes: 128}}, crypt.ErrLimitExceeded},
		{"total_budget", nil, crypt.DecryptOptions{Password: []byte("secret"), Limits: crypt.Limits{MaxTotalBytes: 512}}, crypt.ErrLimitExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := encryptedFiles(t, minimalOriginals(), 1, false, false)
			if test.mutate != nil {
				test.mutate(files)
			}
			var output bytes.Buffer
			report, err := crypt.Decrypt(t.Context(), archive(t, files), &output, test.options)
			if report != nil || !errors.Is(err, test.want) {
				t.Fatalf("report=%+v, err=%v, want=%v", report, err, test.want)
			}
		})
	}
	var out bytes.Buffer
	if _, err := crypt.Decrypt(t.Context(), archive(t, minimalOriginals()), &out, crypt.DecryptOptions{}); !errors.Is(err, crypt.ErrNotEncrypted) {
		t.Fatalf("unencrypted error=%v", err)
	}
}

func TestDecryptPreservesExistingSESAndEveryOriginalEntry(t *testing.T) {
	input, err := os.ReadFile("../../testdata/ofdrw/999.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("missing signed fixture 999.ofd")
	}
	if err != nil {
		t.Fatal(err)
	}
	before, err := sign.Verify(t.Context(), input, sign.VerificationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || !before[0].DigestValid || before[0].Result == nil || !before[0].Result.Valid {
		t.Fatal("signed fixture must have matching digests and valid mathematical signatures")
	}
	originals := unpack(t, input)
	files := encryptedFiles(t, originals, 2, true, false)
	var out bytes.Buffer
	if _, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("中文口令🔐")}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, out.Bytes(), originals)
	after, err := sign.Verify(t.Context(), out.Bytes(), sign.VerificationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || after[0].ID != before[0].ID || !after[0].DigestValid || !after[0].Result.Valid || after[0].Result.Validation.Status != before[0].Result.Validation.Status {
		t.Fatalf("verification changed: before=%+v, after=%+v", before, after)
	}
}

func TestDecryptRejectsExistingIntegrityManifestAndChangedSignatureTargets(t *testing.T) {
	for _, target := range []string{"Security/map.dat", "Cipher/entry_0.dat", "OFD.xml"} {
		files := encryptedFiles(t, minimalOriginals(), 1, false, false)
		// A signature over visible encryption metadata/ciphertext/placeholders
		// must not survive as if it protected restored originals.
		files["OFD.xml"] = "placeholder"
		digest := sha1.Sum([]byte(files[target]))
		files["Signatures/sig.data"] = fmt.Sprintf(`<ofd:Signature xmlns:ofd="http://www.ofdspec.org/2016"><ofd:SignedInfo><ofd:References CheckMethod="SHA1"><ofd:Reference FileRef="../%s"><ofd:CheckValue>%s</ofd:CheckValue></ofd:Reference></ofd:References></ofd:SignedInfo></ofd:Signature>`, target, base64.StdEncoding.EncodeToString(digest[:]))
		var out bytes.Buffer
		if report, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret")}); report != nil || !errors.Is(err, crypt.ErrProtectedContent) {
			t.Fatalf("target=%s, report=%+v, err=%v", target, report, err)
		}
		files["Signatures/sig.data"] = strings.Replace(files["Signatures/sig.data"], "../"+target, "../"+strings.ToUpper(target), 1)
		if report, err := crypt.Decrypt(t.Context(), archive(t, files), io.Discard, crypt.DecryptOptions{Password: []byte("secret")}); report != nil || !errors.Is(err, crypt.ErrProtectedContent) {
			t.Fatalf("case-fallback protection bypass for %s: %v", target, err)
		}
	}
	files := encryptedFiles(t, minimalOriginals(), 1, false, false)
	files["OFDEntries.xml"] = "integrity manifest"
	var out bytes.Buffer
	if _, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret")}); !errors.Is(err, crypt.ErrProtectedContent) {
		t.Fatalf("manifest error=%v", err)
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

type cancelWriter struct{ cancel context.CancelFunc }

func (w cancelWriter) Write(p []byte) (int, error) { w.cancel(); return len(p), nil }

func TestDecryptPreservesOutputErrorsCancellationAndValidatesOptionsFirst(t *testing.T) {
	files := encryptedFiles(t, minimalOriginals(), 1, true, false)
	input := archive(t, files)
	outputError := errors.New("output failed")
	if report, err := crypt.Decrypt(t.Context(), input, failingWriter{outputError}, crypt.DecryptOptions{Password: []byte("secret")}); report != nil || !errors.Is(err, outputError) {
		t.Fatalf("output error=%v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if report, err := crypt.Decrypt(ctx, input, cancelWriter{cancel}, crypt.DecryptOptions{Password: []byte("secret")}); report != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("output cancellation=%v", err)
	}
	for _, opt := range []crypt.DecryptOptions{{Password: []byte{0xff}}, {Limits: crypt.Limits{MaxEntryBytes: -1}}, {MissingScheme: crypt.PasswordScheme}} {
		if _, err := crypt.Decrypt(t.Context(), "missing.ofd", io.Discard, opt); err == nil || errors.Is(err, os.ErrNotExist) {
			t.Fatalf("options not validated before opening input: %v", err)
		}
	}
	wrongKey, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := crypt.Decrypt(t.Context(), archive(t, encryptedFiles(t, minimalOriginals(), 1, true, true)), io.Discard, crypt.DecryptOptions{PrivateKey: wrongKey}); !errors.Is(err, crypt.ErrDecryptFailed) {
		t.Fatalf("wrong private key error=%v", err)
	}
	for _, serviceError := range []error{io.ErrClosedPipe, context.Canceled, context.DeadlineExceeded} {
		key := &recordingKey{key: privateKey(t), err: serviceError}
		if _, err := crypt.Decrypt(t.Context(), archive(t, encryptedFiles(t, minimalOriginals(), 1, true, true)), io.Discard, crypt.DecryptOptions{PrivateKey: key}); !errors.Is(err, serviceError) {
			t.Fatalf("key service error lost: %v", err)
		}
	}
}

func TestDecryptDefaultZeroIVPartialScopeAndInPlaceCiphertext(t *testing.T) {
	originals := minimalOriginals()
	v := readVectors(t)
	fek, zeroIV := hexBytes(t, v.FEK), make([]byte, 16)
	files := encryptedFiles(t, originals, 1, false, false)
	info := inspect(t, files, crypt.InspectOptions{})
	for _, entry := range info.Operations[0].Entries {
		if entry.Path == "OFD.xml" {
			delete(files, entry.EPath)
			files[entry.Path] = originals[entry.Path]
			files["Security/map.dat"] = strings.Replace(files["Security/map.dat"], fmt.Sprintf(`<ofd:EncryptEntry Path="/%s" EPath="/%s"/>`, entry.Path, entry.EPath), "", 1)
			continue
		}
		ciphertext := string(testEncrypt(t, []byte(originals[entry.Path]), fek, zeroIV))
		if entry.Path == "Doc_0/data.bin" {
			delete(files, entry.EPath)
			files[entry.Path] = ciphertext
			files["Security/map.dat"] = strings.Replace(files["Security/map.dat"], `EPath="/`+entry.EPath+`"`, `EPath="/`+entry.Path+`"`, 1)
		} else {
			files[entry.EPath] = ciphertext
		}
	}
	seed := strings.Replace(files["Security/seed.xml"], "<ofd:IVValue>"+base64.StdEncoding.EncodeToString(hexBytes(t, v.IV))+"</ofd:IVValue>", "", 1)
	files["Security/seed.xml"] = strings.Replace(seed, base64.StdEncoding.EncodeToString(hexBytes(t, v.Passwords[1].Wrapped)), base64.StdEncoding.EncodeToString(testEncrypt(t, fek, hexBytes(t, v.Passwords[1].KEK), zeroIV)), 1)
	var out bytes.Buffer
	report, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret")})
	if err != nil || !report.Recipient.IVDefaulted || report.DecryptedEntries != len(originals)-2 {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
	checkOriginals(t, out.Bytes(), originals)
}

func TestDecryptAllowsCaseOnlyCipherRenameButRejectsUnrelatedCaseAliases(t *testing.T) {
	originals := minimalOriginals()
	originals["SignedValue.dat"] = "signed original bytes"
	files := encryptedFiles(t, originals, 1, false, false)
	info := inspect(t, files, crypt.InspectOptions{})
	for _, entry := range info.Operations[0].Entries {
		if entry.Path == "SignedValue.dat" {
			files["signedvalue.dat"] = files[entry.EPath]
			delete(files, entry.EPath)
			files["Security/map.dat"] = strings.Replace(files["Security/map.dat"], `EPath="/`+entry.EPath+`"`, `EPath="/signedvalue.dat"`, 1)
		}
	}
	var out bytes.Buffer
	if _, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret")}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, out.Bytes(), originals)
	files["SIGNEDVALUE.DAT"] = "unrelated plaintext"
	if _, err := crypt.Decrypt(t.Context(), archive(t, files), io.Discard, crypt.DecryptOptions{Password: []byte("secret")}); !errors.Is(err, crypt.ErrInvalidMetadata) {
		t.Fatalf("physical case collision accepted: %v", err)
	}
}

func TestDecryptEncryptedMapCannotBypassPathChecks(t *testing.T) {
	files := encryptedFiles(t, minimalOriginals(), 1, false, false)
	plain := strings.Replace(files["Security/map.dat"], `Path="/Doc_0/data.bin"`, `Path="/../outside"`, 1)
	v := readVectors(t)
	files["Security/map.dat"] = string(testEncrypt(t, []byte(plain), hexBytes(t, v.FEK), hexBytes(t, v.IV)))
	var out bytes.Buffer
	if report, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret")}); report != nil || !errors.Is(err, crypt.ErrInvalidMetadata) || out.Len() != 0 {
		t.Fatalf("report=%+v, err=%v, output=%d", report, err, out.Len())
	}
}

func TestCertificateCipherShapeAndHashTamperingAreRejected(t *testing.T) {
	v := readVectors(t)
	original := hexBytes(t, v.SM2.Wrapped)
	for _, mutation := range []func([]byte) []byte{
		func(b []byte) []byte { return append(b, 0) },
		func(b []byte) []byte { b[len(b)-1] ^= 1; return b },
		func(b []byte) []byte {
			var value struct {
				X, Y             *big.Int
				Hash, Ciphertext []byte
			}
			if _, err := asn1.Unmarshal(b, &value); err != nil {
				t.Fatal(err)
			}
			value.Hash = value.Hash[:31]
			result, err := asn1.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			return result
		},
	} {
		files := encryptedFiles(t, minimalOriginals(), 1, true, true)
		files["Security/seed.xml"] = strings.Replace(files["Security/seed.xml"], base64.StdEncoding.EncodeToString(original), base64.StdEncoding.EncodeToString(mutation(bytes.Clone(original))), 1)
		var out bytes.Buffer
		if report, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{PrivateKey: privateKey(t)}); report != nil || !errors.Is(err, crypt.ErrDecryptFailed) || out.Len() != 0 {
			t.Fatalf("report=%+v, err=%v", report, err)
		}
	}
}

func TestOutputBudgetCountsMultipleRestoredCopiesOfOneCipher(t *testing.T) {
	files := encryptedFiles(t, minimalOriginals(), 1, false, false)
	v := readVectors(t)
	files["Cipher/shared.dat"] = string(testEncrypt(t, bytes.Repeat([]byte{1}, 512), hexBytes(t, v.FEK), hexBytes(t, v.IV)))
	for i := 0; i < 16; i++ {
		files["Security/map.dat"] = strings.Replace(files["Security/map.dat"], `</ofd:EncryptedEntries>`, fmt.Sprintf(`<ofd:EncryptEntry Path="/copies/%d.bin" EPath="/Cipher/shared.dat"/></ofd:EncryptedEntries>`, i), 1)
	}
	var inputBytes int64
	for _, value := range files {
		inputBytes += int64(len(value))
	}
	var out bytes.Buffer
	if report, err := crypt.Decrypt(t.Context(), archive(t, files), &out, crypt.DecryptOptions{Password: []byte("secret"), Limits: crypt.Limits{MaxTotalBytes: inputBytes + 512}}); report != nil || !errors.Is(err, crypt.ErrLimitExceeded) {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
}
