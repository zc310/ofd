package main

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emmansun/gmsm/pkcs8"
	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/pkg/crypt"
	ofdreader "github.com/zc310/ofd/pkg/reader"
)

func wasmDocumentFixture(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{
		"OFD.xml":                        `<ofd:OFD xmlns:ofd="http://www.ofdspec.org/2016" Version="1.1" DocType="OFD"><ofd:DocBody><ofd:DocInfo><ofd:DocID>wasm-encrypted</ofd:DocID><ofd:Title>Encrypted reader</ofd:Title></ofd:DocInfo><ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`,
		"Doc_0/Document.xml":             `<ofd:Document xmlns:ofd="http://www.ofdspec.org/2016"><ofd:CommonData><ofd:MaxUnitID>4</ofd:MaxUnitID><ofd:PageArea><ofd:PhysicalBox>0 0 100 100</ofd:PhysicalBox></ofd:PageArea><ofd:DocumentRes>DocumentRes.xml</ofd:DocumentRes></ofd:CommonData><ofd:Pages><ofd:Page ID="2" BaseLoc="Pages/Page_0/Content.xml"/></ofd:Pages></ofd:Document>`,
		"Doc_0/DocumentRes.xml":          `<ofd:Res xmlns:ofd="http://www.ofdspec.org/2016" BaseLoc="Res"><ofd:Fonts><ofd:Font ID="1" FontName="Helvetica" FamilyName="Helvetica"/></ofd:Fonts></ofd:Res>`,
		"Doc_0/Pages/Page_0/Content.xml": `<ofd:Page xmlns:ofd="http://www.ofdspec.org/2016"><ofd:Content><ofd:Layer ID="3"><ofd:TextObject ID="4" Boundary="10 10 80 20" Font="1" Size="5"><ofd:TextCode X="0" Y="6">Secret document</ofd:TextCode></ofd:TextObject></ofd:Layer></ofd:Content></ofd:Page>`,
	}
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for name, content := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func checkWASMReader(t *testing.T, input []byte, options crypt.DecryptOptions, expected *ofdreader.Reader) {
	t.Helper()
	reader, err := openWASMDocument(t.Context(), input, ofdreader.OpenOptions{}, options)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	pages, err := reader.Pages()
	wantPages, wantErr := expected.Pages()
	if err != nil || wantErr != nil || !reflect.DeepEqual(pages, wantPages) {
		t.Fatalf("解锁后页面不一致: %v / %v", err, wantErr)
	}
	text, err := reader.Text(0)
	wantText, wantErr := expected.Text(0)
	if err != nil || wantErr != nil || len(text) == 0 || !reflect.DeepEqual(text, wantText) {
		t.Fatalf("解锁后文字不一致: %v / %v", err, wantErr)
	}
	report, err := reader.Search(t.Context(), "Secret", ofdreader.SearchOptions{})
	if err != nil || len(report.Results) == 0 {
		t.Fatalf("解锁后无法搜索: %v", err)
	}
	image, err := reader.RenderPage(t.Context(), 0, ofdreader.RenderOptions{DPI: 24})
	wantImage, wantErr := expected.RenderPage(t.Context(), 0, ofdreader.RenderOptions{DPI: 24})
	if err != nil || wantErr != nil || len(image) == 0 || !bytes.Equal(image, wantImage) {
		t.Fatalf("解锁后渲染不一致: %v / %v", err, wantErr)
	}
	var pdf bytes.Buffer
	if err := reader.RenderPDFTo(t.Context(), &pdf, []int{0}, ofdreader.RenderOptions{}); err != nil || !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("解锁后 PDF 导出失败: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Text(0); err == nil {
		t.Fatal("关闭后仍能读取文档")
	}
}

func TestWASMDocumentUnlocksPasswordsWithoutPlaintextZIP(t *testing.T) {
	plain := wasmDocumentFixture(t)
	expected, err := ofdreader.Open(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer expected.Close()
	for _, test := range []struct {
		name, password string
		options        crypt.EncryptOptions
	}{
		{"all", "123456", crypt.EncryptOptions{}},
		{"keep-root", "123456", crypt.EncryptOptions{KeepRoot: true}},
		{"partial", "123456", crypt.EncryptOptions{Entries: []string{"Doc_0/Pages/Page_0/Content.xml"}}},
		{"independent-keys", "123456", crypt.EncryptOptions{IndependentKeys: true}},
		{"plaintext-map", "123456", crypt.EncryptOptions{PlaintextMap: true}},
		{"empty-password", "", crypt.EncryptOptions{}},
		{"unicode-and-spaces", "  中文🔐  ", crypt.EncryptOptions{}},
		{"line-break", "interop-password\n", crypt.EncryptOptions{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var encrypted bytes.Buffer
			test.options.Recipients = []crypt.EncryptRecipient{{UserName: "User", Password: []byte(test.password)}}
			if _, err := crypt.Encrypt(t.Context(), plain, &encrypted, test.options); err != nil {
				t.Fatal(err)
			}
			for _, opt := range []crypt.DecryptOptions{{}, {Password: []byte("wrong")}, {Password: []byte(test.password), UserName: "other"}} {
				reader, err := openWASMDocument(t.Context(), encrypted.Bytes(), ofdreader.OpenOptions{}, opt)
				if reader != nil || (wasmErrorCode(err) != "credentials_required" && wasmErrorCode(err) != "decrypt_failed") {
					t.Fatalf("错误凭据未拒绝: %v", err)
				}
			}
			checkWASMReader(t, encrypted.Bytes(), crypt.DecryptOptions{Password: []byte(test.password), UserName: "User"}, expected)
		})
	}
	checkWASMReader(t, plain, crypt.DecryptOptions{}, expected)
}

func TestWASMDocumentUnlocksSM2RecipientsWithPKCS8Keys(t *testing.T) {
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &smx509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "WASM recipient"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: smx509.KeyUsageKeyEncipherment}
	der, err := smx509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := smx509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := wasmDocumentFixture(t)
	expected, err := ofdreader.Open(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer expected.Close()
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), plain, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "cert", Certificate: der}}, PlaintextMap: true}); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{keyDER, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})} {
		parsed, err := parseWASMPrivateKey(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		checkWASMReader(t, encrypted.Bytes(), crypt.DecryptOptions{PrivateKey: parsed}, expected)
	}
	keyPassword := []byte("wasm-test-key-password")
	encryptedKeyDER, err := pkcs8.MarshalPrivateKey(key, keyPassword, pkcs8.DefaultOpts)
	if err != nil {
		t.Fatal(err)
	}
	encryptedKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: encryptedKeyDER})
	parsed, err := parseWASMPrivateKey(encryptedKeyPEM, keyPassword)
	if err != nil || parsed.D.Cmp(key.D) != 0 {
		t.Fatalf("无法解析带密码的 SM2 PKCS#8 私钥: %v", err)
	}
	checkWASMReader(t, encrypted.Bytes(), crypt.DecryptOptions{PrivateKey: parsed}, expected)
	if _, err := parseWASMPrivateKey(encryptedKeyPEM, []byte("wrong")); !errors.Is(err, errInvalidCredentials) {
		t.Fatalf("错误私钥密码未拒绝: %v", err)
	}
	compatible := wasmOFDRWMetadata(t, encrypted.Bytes(), "")
	if reader, err := openWASMDocument(t.Context(), compatible, ofdreader.OpenOptions{}, crypt.DecryptOptions{}); reader != nil || wasmErrorCode(err) != "credentials_required" {
		t.Fatalf("兼容证书包首次打开未请求凭据: %v", err)
	}
	checkWASMReader(t, compatible, crypt.DecryptOptions{PrivateKey: key}, expected)
	checkWASMReader(t, wasmOFDRWMetadata(t, encrypted.Bytes(), crypt.CertificateScheme), crypt.DecryptOptions{PrivateKey: key}, expected)
	other, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if reader, err := openWASMDocument(t.Context(), encrypted.Bytes(), ofdreader.OpenOptions{}, crypt.DecryptOptions{PrivateKey: other}); reader != nil || !errors.Is(err, crypt.ErrDecryptFailed) {
		t.Fatalf("错误 SM2 私钥未拒绝: %v", err)
	}
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaDER, err := smx509.MarshalPKCS8PrivateKey(ecdsaKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{ecdsaDER, []byte("bad"), append(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), []byte("extra")...)} {
		if _, err := parseWASMPrivateKey(data, nil); !errors.Is(err, errInvalidCredentials) {
			t.Fatalf("非法私钥未拒绝: %v", err)
		}
	}
}

// wasmOFDRWMetadata 复现 ofdrw 的数字 ID 和缺失方案，保留原始密文。
func wasmOFDRWMetadata(t *testing.T, input []byte, scheme string) []byte {
	t.Helper()
	info, err := crypt.Inspect(t.Context(), input, crypt.InspectOptions{})
	if err != nil || len(info.Operations) != 1 {
		t.Fatalf("无法读取构造包的加密操作: %v", err)
	}
	op := info.Operations[0]
	archive, err := zip.NewReader(bytes.NewReader(input), int64(len(input)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for _, file := range archive.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == "Encryptions.xml" || file.Name == op.DecryptSeedLoc || file.Name == op.EntriesMapLoc {
			data = bytes.ReplaceAll(data, []byte(`ID="`+op.ID+`"`), []byte(`ID="1"`))
		}
		if file.Name == op.DecryptSeedLoc {
			replacement := ""
			if scheme != "" {
				replacement = ` EncryptCaseId="` + scheme + `"`
			}
			data = []byte(strings.Replace(string(data), ` EncryptCaseId="`+op.Seed.Scheme+`"`, replacement, 1))
		}
		entry, err := w.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestWASMDocumentDefaultsToCompatAndSelectsOnlyMissingSchemes(t *testing.T) {
	plain := wasmDocumentFixture(t)
	expected, err := ofdreader.Open(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer expected.Close()
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), plain, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "interop-user", Password: []byte("secret")}}, PlaintextMap: true}); err != nil {
		t.Fatal(err)
	}
	compatible := wasmOFDRWMetadata(t, encrypted.Bytes(), "")
	if _, err := crypt.Inspect(t.Context(), compatible, crypt.InspectOptions{}); !errors.Is(err, crypt.ErrInvalidMetadata) {
		t.Fatalf("兼容样本未保留非标准 ID: %v", err)
	}
	for _, options := range []crypt.DecryptOptions{{}, {Password: []byte("wrong")}} {
		reader, err := openWASMDocument(t.Context(), compatible, ofdreader.OpenOptions{}, options)
		want := "credentials_required"
		if options.Password != nil {
			want = "decrypt_failed"
		}
		if reader != nil || wasmErrorCode(err) != want {
			t.Fatalf("兼容包的解锁错误 = %v，期望 %s", err, want)
		}
	}
	checkWASMReader(t, compatible, crypt.DecryptOptions{Password: []byte("secret")}, expected)
	checkWASMReader(t, wasmOFDRWMetadata(t, encrypted.Bytes(), crypt.PasswordScheme), crypt.DecryptOptions{Password: []byte("secret")}, expected)
	for _, scheme := range []string{"9.9.9", crypt.CertificateScheme} {
		reader, err := openWASMDocument(t.Context(), wasmOFDRWMetadata(t, encrypted.Bytes(), scheme), ofdreader.OpenOptions{}, crypt.DecryptOptions{Password: []byte("secret")})
		if reader != nil || err == nil {
			t.Fatalf("已声明方案 %s 被口令方案覆盖", scheme)
		}
		if scheme == "9.9.9" && !errors.Is(err, crypt.ErrUnsupported) {
			t.Fatalf("未知方案错误未保留: %v", err)
		}
	}
}
