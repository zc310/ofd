package crypt_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/sign"
)

func TestEncryptPasswordScopesAndMapsRoundTrip(t *testing.T) {
	for _, scope := range []string{"all", "keep-root", "entries"} {
		for _, plaintextMap := range []bool{false, true} {
			t.Run(scope+"/"+map[bool]string{false: "encrypted-map", true: "plaintext-map"}[plaintextMap], func(t *testing.T) {
				originals := minimalOriginals()
				originals["Encrypt_1/existing.dat"] = "existing resource"
				pw := []byte("中文加密🔐")
				options := crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "甲", Password: pw}, {UserName: "乙", Password: []byte("second")}}, PlaintextMap: plaintextMap}
				switch scope {
				case "keep-root":
					options.KeepRoot = true
				case "entries":
					options.Entries = []string{"Doc_0/data.bin", "Doc_0/empty.bin"}
				}
				data := archive(t, originals)
				inputPath := filepath.Join(t.TempDir(), "input.ofd")
				if err := os.WriteFile(inputPath, data, 0600); err != nil {
					t.Fatal(err)
				}
				var encrypted bytes.Buffer
				report, err := crypt.Encrypt(t.Context(), inputPath, &encrypted, options)
				if err != nil {
					t.Fatal(err)
				}
				if report.State != "locked" || len(report.Recipients) != 2 || report.OutputEntries != len(originals)+3 || string(pw) != "中文加密🔐" {
					t.Fatalf("report=%+v", report)
				}
				info, err := crypt.Inspect(t.Context(), encrypted.Bytes(), crypt.InspectOptions{})
				if err != nil || info.Operations[0].Seed.Scheme != crypt.PasswordScheme {
					t.Fatalf("info=%+v, err=%v", info, err)
				}
				op := info.Operations[0]
				if !strings.HasPrefix(op.DecryptSeedLoc, "Encrypt_2/") {
					t.Fatalf("existing encryption directory overwritten: %+v", op)
				}
				if (op.MapState == "plaintext") != plaintextMap {
					t.Fatalf("map state=%s", op.MapState)
				}
				physical := unpack(t, encrypted.Bytes())
				for name, value := range originals {
					selected := scope == "all" || (scope == "keep-root" && name != "OFD.xml") || (scope == "entries" && (name == "Doc_0/data.bin" || name == "Doc_0/empty.bin"))
					if selected {
						if _, ok := physical[name]; ok {
							t.Fatalf("plaintext entry retained: %s", name)
						}
					} else if physical[name] != value {
						t.Fatalf("clear entry changed: %s", name)
					}
				}
				for _, credential := range []struct{ user, password string }{{"甲", "中文加密🔐"}, {"乙", "second"}} {
					var out bytes.Buffer
					if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), &out, crypt.DecryptOptions{Password: []byte(credential.password), UserName: credential.user}); err != nil {
						t.Fatal(err)
					}
					checkOriginals(t, out.Bytes(), originals)
				}
				var another bytes.Buffer
				if _, err := crypt.Encrypt(t.Context(), bytes.NewReader(data), &another, options); err != nil {
					t.Fatal(err)
				}
				if bytes.Equal(encrypted.Bytes(), another.Bytes()) {
					t.Fatal("encryption unexpectedly deterministic")
				}
			})
		}
	}
}

func TestEncryptCertificateRecipientsAndExactRestore(t *testing.T) {
	v := readVectors(t)
	cert, err := base64.StdEncoding.DecodeString(v.SM2.Certificate)
	if err != nil {
		t.Fatal(err)
	}
	var encrypted, plain bytes.Buffer
	report, err := crypt.Encrypt(t.Context(), archive(t, minimalOriginals()), &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "cert1", Certificate: cert}, {UserName: "cert2", Certificate: cert}}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Scheme != crypt.CertificateScheme || report.Recipients[0].CertificateSHA256 == "" {
		t.Fatalf("report=%+v", report)
	}
	if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), &plain, crypt.DecryptOptions{PrivateKey: privateKey(t), UserName: "cert2"}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, plain.Bytes(), minimalOriginals())
}

func TestEncryptSignedFixturePreservesFullEntryBytesAndSignatureResults(t *testing.T) {
	input, err := os.ReadFile("../../testdata/ofdrw/999.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("missing signed fixture")
	}
	if err != nil {
		t.Fatal(err)
	}
	before, err := sign.Verify(t.Context(), input, sign.VerificationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var encrypted, plain bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), input, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "user", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := crypt.Decrypt(t.Context(), encrypted.Bytes(), &plain, crypt.DecryptOptions{Password: []byte("secret")}); err != nil {
		t.Fatal(err)
	}
	checkOriginals(t, plain.Bytes(), unpack(t, input))
	after, err := sign.Verify(t.Context(), plain.Bytes(), sign.VerificationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || !before[0].DigestValid || !after[0].DigestValid || !after[0].Result.Valid || before[0].Result.Validation.Status != after[0].Result.Validation.Status {
		t.Fatal("signature results changed")
	}
}

func TestEncryptRejectsInvalidInputsWithoutSuccessReport(t *testing.T) {
	valid := crypt.EncryptRecipient{UserName: "user", Password: []byte("secret")}
	for _, test := range []struct {
		name   string
		opt    crypt.EncryptOptions
		mutate func(map[string]string)
		want   error
	}{
		{"no_recipient", crypt.EncryptOptions{}, nil, nil},
		{"mixed_schemes", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid, {UserName: "cert", Certificate: []byte("bad")}}}, nil, nil},
		{"duplicate_selection", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}, Entries: []string{"OFD.xml", "OFD.xml"}}, nil, nil},
		{"empty_selection", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}, Entries: []string{}}, nil, nil},
		{"traversal", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}, Entries: []string{"../escape"}}, nil, nil},
		{"missing_entry", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}, Entries: []string{"absent"}}, nil, crypt.ErrInvalidMetadata},
		{"missing_root", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}}, func(f map[string]string) { delete(f, "OFD.xml") }, crypt.ErrInvalidMetadata},
		{"bad_root", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}}, func(f map[string]string) { f["OFD.xml"] = "not XML" }, crypt.ErrInvalidMetadata},
		{"integrity", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}}, func(f map[string]string) { f["OFDEntries.xml"] = "manifest" }, crypt.ErrProtectedContent},
		{"case_collision", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}}, func(f map[string]string) { f["ofd.xml"] = "other" }, crypt.ErrInvalidMetadata},
		{"control_file_directory_collision", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}, Entries: []string{"Doc_0/data.bin"}}, func(f map[string]string) { f["Encryptions.xml/attachment.bin"] = "attachment" }, crypt.ErrInvalidMetadata},
		{"metadata_budget", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}, Limits: crypt.Limits{MaxMetadataBytes: 64}}, nil, crypt.ErrLimitExceeded},
		{"nodes_budget", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}, Limits: crypt.Limits{MaxXMLNodes: 1}}, nil, crypt.ErrLimitExceeded},
		{"entries_budget", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}, Limits: crypt.Limits{MaxEntries: 6}}, nil, crypt.ErrLimitExceeded},
		{"entry_budget", crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}, Limits: crypt.Limits{MaxEntryBytes: 128}}, nil, crypt.ErrLimitExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := minimalOriginals()
			if test.mutate != nil {
				test.mutate(files)
			}
			var out bytes.Buffer
			report, err := crypt.Encrypt(t.Context(), archive(t, files), &out, test.opt)
			if report != nil || err == nil || (test.want != nil && !errors.Is(err, test.want)) {
				t.Fatalf("report=%+v,err=%v,want=%v", report, err, test.want)
			}
		})
	}
	files := encryptedFiles(t, minimalOriginals(), 1, true, false)
	if _, err := crypt.Encrypt(t.Context(), archive(t, files), io.Discard, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{valid}}); !errors.Is(err, crypt.ErrAlreadyEncrypted) {
		t.Fatalf("already encrypted=%v", err)
	}
}

func TestEncryptDoesNotRewriteExistingSignedXML(t *testing.T) {
	// 对加密目标声明不支持的摘要算法时，不能声称签名得到保留；目标范围之外的未知格式则原样保留。
	files := minimalOriginals()
	files["sig.xml"] = `<Signature xmlns="http://www.ofdspec.org/2016"><SignedInfo><References CheckMethod="unknown"><Reference FileRef="/OFD.xml"><CheckValue>AA==</CheckValue></Reference></References></SignedInfo></Signature>`
	var out bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), archive(t, files), &out, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "u", Password: []byte("secret")}}}); !errors.Is(err, crypt.ErrProtectedContent) {
		t.Fatalf("protected content=%v", err)
	}
}

func TestEncryptInputOutputOwnershipAndCancellation(t *testing.T) {
	opt := crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "user", Password: []byte("secret")}}}
	input := archive(t, minimalOriginals())
	originalHash := sha256.Sum256(input)
	outputErr := errors.New("output unavailable")
	if report, err := crypt.Encrypt(t.Context(), input, failingWriter{outputErr}, opt); report != nil || !errors.Is(err, outputErr) {
		t.Fatalf("report=%+v,err=%v", report, err)
	}
	if sha256.Sum256(input) != originalHash {
		t.Fatal("caller input mutated")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := crypt.Encrypt(ctx, input, io.Discard, opt); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestEncryptMetadataIsStrictAndContainsRandomExplicitIV(t *testing.T) {
	var out bytes.Buffer
	opt := crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "<user&\"", Password: []byte{}}}, PlaintextMap: true}
	if _, err := crypt.Encrypt(t.Context(), archive(t, minimalOriginals()), &out, opt); err != nil {
		t.Fatal(err)
	}
	info, err := crypt.Inspect(t.Context(), out.Bytes(), crypt.InspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if info.Operations[0].Seed.Recipients[0].IVDefaulted || info.Operations[0].Seed.Recipients[0].UserName != "<user&\"" {
		t.Fatalf("seed=%+v", info.Operations[0].Seed)
	}
	physical := unpack(t, out.Bytes())
	var seed struct {
		ID    string `xml:"ID,attr"`
		Users []struct {
			IV string `xml:"IVValue"`
		} `xml:"UserInfo"`
	}
	if err := xml.Unmarshal([]byte(physical[info.Operations[0].DecryptSeedLoc]), &seed); err != nil {
		t.Fatal(err)
	}
	iv, err := base64.StdEncoding.DecodeString(seed.Users[0].IV)
	if err != nil || len(iv) != 16 || reflect.DeepEqual(iv, make([]byte, 16)) {
		t.Fatalf("IV=%x,err=%v", iv, err)
	}
}
