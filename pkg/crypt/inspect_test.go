package crypt_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/pkg/crypt"
)

func fixtureFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{"Cipher/root.dat": strings.Repeat("x", 32)}
	for name, source := range map[string]string{"Encryptions.xml": "Encryptions.xml", "Security/seed.xml": "seed.xml", "Security/map.dat": "map.xml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "crypt", source))
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("missing crypto fixture: %s", source)
		}
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	return files
}

func archive(t *testing.T, files map[string]string, extra ...string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	names = append(names, extra...)
	for _, name := range names {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func inspect(t *testing.T, files map[string]string, opt crypt.InspectOptions) *crypt.Info {
	t.Helper()
	info, err := crypt.Inspect(context.Background(), archive(t, files), opt)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestInspectWorksWithoutPlaintextOFDAndDoesNotExposeWrappedKeys(t *testing.T) {
	files := fixtureFiles(t)
	data := archive(t, files)
	name := filepath.Join(t.TempDir(), "encrypted.ofd")
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []any{data, name, bytes.NewReader(data)} {
		info, err := crypt.Inspect(context.Background(), input, crypt.InspectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !info.Encrypted || info.State != "locked" || len(info.Operations) != 1 {
			t.Fatalf("info = %+v", info)
		}
		op := info.Operations[0]
		if op.Seed.Scheme != crypt.PasswordScheme || len(op.Seed.Recipients) != 1 || !op.Seed.Recipients[0].IVDefaulted || op.Seed.Recipients[0].UserType != "User" {
			t.Fatalf("seed = %+v", op.Seed)
		}
		if op.MapState != "plaintext" || op.Entries[0].Path != "OFD.xml" || op.Entries[0].EPath != "Cipher/root.dat" {
			t.Fatalf("map = %+v", op)
		}
		encoded, err := json.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte("AAAAAAAAAAA")) || bytes.Contains(encoded, []byte("encrypted_wk")) {
			t.Fatal("wrapped key leaked into inspection result")
		}
	}
}

func TestInspectOpaqueMapAndUnknownSchemeRemainExplicit(t *testing.T) {
	files := fixtureFiles(t)
	files["Security/map.dat"] = strings.Repeat("x", 32)
	files["Security/seed.xml"] = strings.ReplaceAll(files["Security/seed.xml"], "1.1.1", "1.1.99")
	info := inspect(t, files, crypt.InspectOptions{})
	op := info.Operations[0]
	if op.MapState != "encrypted" || op.Entries != nil || op.Seed.Scheme != "1.1.99" || len(info.Diagnostics) != 1 || info.Diagnostics[0].Code != "encryption.scheme_unsupported" {
		t.Fatalf("info = %+v", info)
	}
}

func TestInspectOfdrwCompatibilityIsDiagnosed(t *testing.T) {
	files := fixtureFiles(t)
	files["Encryptions.xml"] = strings.ReplaceAll(files["Encryptions.xml"], "enc1", "1")
	files["Security/seed.xml"] = strings.ReplaceAll(strings.ReplaceAll(files["Security/seed.xml"], "enc1", "1"), ` EncryptCaseId="1.1.1"`, "")
	files["Security/map.dat"] = strings.ReplaceAll(strings.ReplaceAll(files["Security/map.dat"], "enc1", "1"), "EncryptedEntries", "EncryptEntries")
	if _, err := crypt.Inspect(context.Background(), archive(t, files), crypt.InspectOptions{}); !errors.Is(err, crypt.ErrInvalidMetadata) {
		t.Fatalf("strict error = %v", err)
	}
	info := inspect(t, files, crypt.InspectOptions{Mode: crypt.ModeCompat})
	if info.Operations[0].Seed.Scheme != "" {
		t.Fatal("missing scheme must not be guessed")
	}
	codes := map[string]bool{}
	for _, diagnostic := range info.Diagnostics {
		codes[diagnostic.Code] = true
	}
	for _, code := range []string{"encryption.id_nonconforming", "encryption.scheme_missing", "encryption.root_alias"} {
		if !codes[code] {
			t.Fatalf("missing %s: %+v", code, info.Diagnostics)
		}
	}
}

func TestInspectDefaultsNamespacesBOMAndIndependentEntrySeed(t *testing.T) {
	files := fixtureFiles(t)
	for name, data := range files {
		if name == "Cipher/root.dat" {
			continue
		}
		files[name] = "\ufeff" + strings.ReplaceAll(strings.ReplaceAll(data, "ofd:", ""), "xmlns:ofd", "xmlns")
	}
	files["Security/map.dat"] = strings.Replace(files["Security/map.dat"], `EPath="/Cipher/root.dat"`, `EPath="/Cipher/root.dat" DecryptSeedLoc="entry-seed.xml"`, 1)
	files["Security/entry-seed.xml"] = strings.Replace(files["Security/seed.xml"], "测试用户", "独立接收者", 1)
	info := inspect(t, files, crypt.InspectOptions{})
	entry := info.Operations[0].Entries[0]
	if entry.DecryptSeedLoc != "Security/entry-seed.xml" || entry.Seed == nil || entry.Seed.Recipients[0].UserName != "独立接收者" {
		t.Fatalf("entry seed = %+v", entry)
	}
}

func TestInspectRejectsAmbiguousOrDamagedMetadata(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"missing_seed", func(f map[string]string) { delete(f, "Security/seed.xml") }},
		{"mismatched_seed_id", func(f map[string]string) {
			f["Security/seed.xml"] = strings.ReplaceAll(f["Security/seed.xml"], "enc1", "other")
		}},
		{"mismatched_map_id", func(f map[string]string) {
			f["Security/map.dat"] = strings.ReplaceAll(f["Security/map.dat"], "enc1", "other")
		}},
		{"missing_cipher", func(f map[string]string) { delete(f, "Cipher/root.dat") }},
		{"path_escape", func(f map[string]string) {
			f["Security/map.dat"] = strings.ReplaceAll(f["Security/map.dat"], "/OFD.xml", "/../OFD.xml")
		}},
		{"relative_map_path", func(f map[string]string) {
			f["Security/map.dat"] = strings.ReplaceAll(f["Security/map.dat"], "/OFD.xml", "OFD.xml")
		}},
		{"host_path", func(f map[string]string) {
			f["Security/map.dat"] = strings.ReplaceAll(f["Security/map.dat"], "/OFD.xml", "C:/OFD.xml")
		}},
		{"self_encryption", func(f map[string]string) {
			f["Security/map.dat"] = strings.ReplaceAll(f["Security/map.dat"], "/OFD.xml", "/Encryptions.xml")
		}},
		{"bad_iv", func(f map[string]string) {
			f["Security/seed.xml"] = strings.Replace(f["Security/seed.xml"], "</ofd:UserInfo>", "<ofd:IVValue>AA==</ofd:IVValue></ofd:UserInfo>", 1)
		}},
		{"bad_wrap", func(f map[string]string) {
			f["Security/seed.xml"] = strings.Replace(f["Security/seed.xml"], "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "AA==", 1)
		}},
		{"wrong_namespace", func(f map[string]string) {
			f["Encryptions.xml"] = strings.ReplaceAll(f["Encryptions.xml"], "http://www.ofdspec.org/2016", "urn:other")
		}},
		{"duplicate_singleton", func(f map[string]string) {
			f["Encryptions.xml"] = strings.Replace(f["Encryptions.xml"], "</ofd:EncryptInfo>", "<ofd:DecryptSeedLoc>/other</ofd:DecryptSeedLoc></ofd:EncryptInfo>", 1)
		}},
		{"duplicate_attribute", func(f map[string]string) {
			f["Encryptions.xml"] = strings.Replace(f["Encryptions.xml"], `ID="enc1"`, `ID="enc1" ID="enc2"`, 1)
		}},
		{"trailing_root", func(f map[string]string) {
			f["Encryptions.xml"] += `<ofd:Encryptions xmlns:ofd="http://www.ofdspec.org/2016"/>`
		}},
		{"dtd", func(f map[string]string) { f["Encryptions.xml"] = "<!DOCTYPE x>" + f["Encryptions.xml"] }},
		{"cycle", func(f map[string]string) {
			f["Encryptions.xml"] = strings.Replace(f["Encryptions.xml"], `ID="enc1"`, `ID="enc1" Relative="enc1"`, 1)
		}},
		{"invalid_id_is_not_numeric_compatibility", func(f map[string]string) {
			f["Encryptions.xml"] = strings.ReplaceAll(f["Encryptions.xml"], `ID="enc1"`, `ID="invalid id"`)
		}},
		{"dangling_relative", func(f map[string]string) {
			f["Encryptions.xml"] = strings.Replace(f["Encryptions.xml"], `ID="enc1"`, `ID="enc1" Relative="missing"`, 1)
		}},
		{"invalid_cipher_length", func(f map[string]string) { f["Security/map.dat"] = "not ciphertext" }},
		{"unsafe_zip_entry", func(f map[string]string) { f["../stray"] = "stray" }},
		{"case_collision", func(f map[string]string) { f["security/seed.xml"] = f["Security/seed.xml"] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := fixtureFiles(t)
			test.mutate(files)
			info, err := crypt.Inspect(context.Background(), archive(t, files), crypt.InspectOptions{Mode: crypt.ModeCompat})
			if !errors.Is(err, crypt.ErrInvalidMetadata) || info == nil || !info.Encrypted {
				t.Fatalf("info=%+v, err=%v", info, err)
			}
		})
	}
}

func TestInspectRelativeDepthAtAndBeyondDefaultLimit(t *testing.T) {
	for _, depth := range []int{32, 33} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			files := fixtureFiles(t)
			operation := operationElement(files["Encryptions.xml"])
			var operations strings.Builder
			for i := 1; i <= depth; i++ {
				attributes := fmt.Sprintf(`ID="enc%d"`, i)
				if i > 1 {
					attributes += fmt.Sprintf(` Relative="enc%d"`, i-1)
				}
				operations.WriteString(strings.Replace(operation, `ID="enc1"`, attributes, 1))
			}
			files["Encryptions.xml"] = strings.Replace(files["Encryptions.xml"], operation, operations.String(), 1)
			for _, name := range []string{"Security/seed.xml", "Security/map.dat"} {
				files[name] = strings.Replace(files[name], `ID="enc1"`, fmt.Sprintf(`ID="enc%d"`, depth), 1)
			}
			input := archive(t, files)
			info, err := crypt.Inspect(t.Context(), input, crypt.InspectOptions{})
			if depth == 33 {
				if !errors.Is(err, crypt.ErrLimitExceeded) {
					t.Fatalf("超出默认 Relative 深度未拒绝: %v", err)
				}
				view, err := crypt.Open(t.Context(), input, crypt.OpenOptions{})
				if view != nil || !errors.Is(err, crypt.ErrLimitExceeded) {
					t.Fatalf("视图未在请求凭据前拒绝深度超限: view=%v err=%v", view, err)
				}
				return
			}
			if err != nil || len(info.Operations) != depth || info.Operations[depth-1].Deferred {
				t.Fatalf("默认 Relative 深度边界失败: info=%+v err=%v", info, err)
			}
			for _, op := range info.Operations[:depth-1] {
				if !op.Deferred {
					t.Fatalf("前层未保留延迟检查状态: %s", op.ID)
				}
			}
		})
	}
}

func TestInspectLimitsAndCancellation(t *testing.T) {
	files := fixtureFiles(t)
	for _, limits := range []crypt.Limits{
		{MaxInputBytes: 10}, {MaxEntries: 1}, {MaxMetadataBytes: 16},
		{MaxTotalMetadataBytes: 500}, {MaxXMLNodes: 1}, {MaxXMLDepth: 1},
	} {
		if _, err := crypt.Inspect(context.Background(), archive(t, files), crypt.InspectOptions{Limits: limits}); !errors.Is(err, crypt.ErrLimitExceeded) {
			t.Fatalf("limits=%+v, err=%v", limits, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := crypt.Inspect(ctx, archive(t, files), crypt.InspectOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	r := bytes.NewReader(make([]byte, 1024))
	if _, err := crypt.Inspect(context.Background(), r, crypt.InspectOptions{Limits: crypt.Limits{MaxInputBytes: 64}}); !errors.Is(err, crypt.ErrLimitExceeded) || r.Len() != 959 {
		t.Fatalf("bounded reader: remaining=%d, err=%v", r.Len(), err)
	}
	if _, err := crypt.Inspect(context.Background(), nil, crypt.InspectOptions{Limits: crypt.Limits{MaxMetadataBytes: -1}}); err == nil {
		t.Fatal("negative limit accepted")
	}
}

func TestInspectDuplicateMarkerAndUnencryptedArchive(t *testing.T) {
	files := fixtureFiles(t)
	if _, err := crypt.Inspect(context.Background(), archive(t, files, "Encryptions.xml"), crypt.InspectOptions{}); !errors.Is(err, crypt.ErrInvalidMetadata) {
		t.Fatalf("duplicate marker error = %v", err)
	}
	info := inspect(t, map[string]string{"Attachments/Encryptions.xml": "attachment"}, crypt.InspectOptions{})
	if info.Encrypted || info.State != "none" {
		t.Fatalf("attachment misidentified: %+v", info)
	}
}

func TestInspectMissingEPathIsUnknownInsteadOfGuessed(t *testing.T) {
	files := fixtureFiles(t)
	files["Security/map.dat"] = strings.ReplaceAll(files["Security/map.dat"], ` EPath="/Cipher/root.dat"`, "")
	info := inspect(t, files, crypt.InspectOptions{})
	if info.Operations[0].Entries[0].EPath != "" || len(info.Diagnostics) == 0 {
		t.Fatal("missing EPath was guessed or not diagnosed")
	}
}

func TestInspectDefersEarlierLayerInsteadOfReportingItsMetadataMissing(t *testing.T) {
	files := fixtureFiles(t)
	files["Encryptions.xml"] = strings.Replace(files["Encryptions.xml"], `<ofd:EncryptInfo ID="enc1">`, `<ofd:EncryptInfo ID="prior"><ofd:Provider Name="fixture" Version="1"/><ofd:EncryptScope>All</ofd:EncryptScope><ofd:DecryptSeedLoc>/prior.seed</ofd:DecryptSeedLoc><ofd:EntriesMapLoc>/prior.map</ofd:EntriesMapLoc></ofd:EncryptInfo><ofd:EncryptInfo ID="enc1" Relative="prior">`, 1)
	info := inspect(t, files, crypt.InspectOptions{})
	if len(info.Operations) != 2 || !info.Operations[0].Deferred || info.Operations[0].MapState != "unknown" || info.Operations[1].Seed.Scheme != crypt.PasswordScheme {
		t.Fatalf("operations = %+v", info.Operations)
	}
	if _, err := crypt.Inspect(context.Background(), archive(t, files), crypt.InspectOptions{Limits: crypt.Limits{MaxOperations: 1}}); !errors.Is(err, crypt.ErrLimitExceeded) {
		t.Fatalf("operation budget error = %v", err)
	}
}

func TestInspectBoundsRecipientsAndMappingsAcrossMetadata(t *testing.T) {
	files := fixtureFiles(t)
	files["Security/seed.xml"] = strings.Replace(files["Security/seed.xml"], "<ofd:ExtendParams/>", `<ofd:UserInfo UserName="other"><ofd:EncryptedWK>AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=</ofd:EncryptedWK></ofd:UserInfo><ofd:ExtendParams/>`, 1)
	if _, err := crypt.Inspect(context.Background(), archive(t, files), crypt.InspectOptions{Limits: crypt.Limits{MaxRecipients: 1}}); !errors.Is(err, crypt.ErrLimitExceeded) {
		t.Fatalf("recipient budget error = %v", err)
	}
	files["Security/map.dat"] = strings.Replace(files["Security/map.dat"], "</ofd:EncryptedEntries>", `<ofd:EncryptEntry Path="/other" EPath="/Cipher/root.dat"/></ofd:EncryptedEntries>`, 1)
	if _, err := crypt.Inspect(context.Background(), archive(t, files), crypt.InspectOptions{Limits: crypt.Limits{MaxMappings: 1}}); !errors.Is(err, crypt.ErrLimitExceeded) {
		t.Fatalf("mapping budget error = %v", err)
	}
}

func TestInspectCertificateRecipientsAreSummarizedWithoutClaimingTrust(t *testing.T) {
	files := fixtureFiles(t)
	files["Security/seed.xml"] = strings.ReplaceAll(files["Security/seed.xml"], "1.1.1", "1.1.2")
	files["Security/seed.xml"] = strings.Replace(files["Security/seed.xml"], "<ofd:EncryptedWK>", "<ofd:UserCert>AA==</ofd:UserCert><ofd:EncryptedWK>", 1)
	info := inspect(t, files, crypt.InspectOptions{})
	if len(info.Operations[0].Seed.Recipients[0].CertificateSHA256) != 64 {
		t.Fatal("missing certificate fingerprint")
	}
	files["Security/seed.xml"] = strings.Replace(files["Security/seed.xml"], "<ofd:UserCert>AA==</ofd:UserCert>", "", 1)
	if _, err := crypt.Inspect(context.Background(), archive(t, files), crypt.InspectOptions{}); !errors.Is(err, crypt.ErrInvalidMetadata) {
		t.Fatalf("missing certificate error = %v", err)
	}
}
