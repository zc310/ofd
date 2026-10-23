package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"image/color"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/crypt"
)

func viewerEncryptionFixture(t *testing.T) []byte {
	t.Helper()
	return viewerEncryptionArchive(t, map[string][]byte{
		"OFD.xml":                 []byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.1" DocType="OFD"><DocBody><DocInfo><DocID>viewer-encryption</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`),
		"Doc_0/Document.xml":      []byte(`<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>4</MaxUnitID><PageArea><PhysicalBox>0 0 100 100</PhysicalBox></PageArea><DocumentRes>DocumentRes.xml</DocumentRes></CommonData><Pages><Page ID="2" BaseLoc="Pages/Content.xml"/></Pages></Document>`),
		"Doc_0/DocumentRes.xml":   []byte(`<Res xmlns="http://www.ofdspec.org/2016"><Fonts><Font ID="1" FontName="Helvetica" FamilyName="Helvetica"/></Fonts></Res>`),
		"Doc_0/Pages/Content.xml": []byte(`<Page xmlns="http://www.ofdspec.org/2016"><Content><Layer ID="3"><TextObject ID="4" Boundary="10 10 80 20" Font="1" Size="5"><TextCode X="0" Y="6">Secret viewer</TextCode></TextObject></Layer></Content></Page>`),
	})
}

func viewerEncryptionArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for name, data := range files {
		entry, err := w.Create(name)
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

func encryptViewerFixture(t *testing.T, options crypt.EncryptOptions) []byte {
	t.Helper()
	for i := range options.Recipients {
		if options.Recipients[i].UserName == "" {
			options.Recipients[i].UserName = "User"
		}
	}
	var output bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), viewerEncryptionFixture(t), &output, options); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func checkViewerDecryptedOutput(t *testing.T, ofd *parser.OFD) {
	t.Helper()
	if len(ofd.Documents) != 1 {
		t.Fatalf("解锁后的文档体数量 = %d", len(ofd.Documents))
	}
	doc := render.NewDocument(color.Transparent, ofd.Documents[0])
	defer (&viewerDocumentCloser{ofd: ofd, documents: []*render.Document{doc}}).Close()
	if len(doc.Pages) != 1 {
		t.Fatalf("解锁后的页数 = %d", len(doc.Pages))
	}
	if image, err := doc.RasterizePage(doc.Pages[0], render.BackendCanvas, geom.DPI(24)); err != nil || image == nil {
		t.Fatalf("解锁后渲染失败: %v", err)
	}
	var text, pdf bytes.Buffer
	if err := exportDocumentsToWriter([]*render.Document{doc}, &text, "txt", 72, color.Transparent); err != nil || !strings.Contains(text.String(), "Secret viewer") {
		t.Fatalf("解锁后文字导出失败: %v, %q", err, text.String())
	}
	if err := exportDocumentsToWriter([]*render.Document{doc}, &pdf, "pdf", 72, color.Transparent); err != nil || !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("解锁后 PDF 导出失败: %v", err)
	}
}

func TestViewerUnlocksPasswordDocumentsForRenderingAndExport(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		options  crypt.EncryptOptions
	}{
		{"整包", "secret", crypt.EncryptOptions{}},
		{"主入口明文", "secret", crypt.EncryptOptions{KeepRoot: true}},
		{"局部", "secret", crypt.EncryptOptions{Entries: []string{"Doc_0/Pages/Content.xml"}}},
		{"独立密钥", "secret", crypt.EncryptOptions{IndependentKeys: true}},
		{"明文映射", "secret", crypt.EncryptOptions{PlaintextMap: true}},
		{"空口令", "", crypt.EncryptOptions{}},
		{"空格与换行", "\uFEFF  中文🔐\r\n", crypt.EncryptOptions{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.options.Recipients = []crypt.EncryptRecipient{{UserName: "User", Password: []byte(tc.password)}}
			input := encryptViewerFixture(t, tc.options)
			if ofd, err := openViewerOFD(t.Context(), input, crypt.DecryptOptions{}); ofd != nil || !errors.Is(err, crypt.ErrCredentialsRequired) {
				t.Fatalf("缺凭据时未拒绝: %v", err)
			}
			for _, options := range []crypt.DecryptOptions{{Password: []byte("wrong")}, {Password: []byte(tc.password), UserName: "other"}} {
				if ofd, err := openViewerOFD(t.Context(), input, options); ofd != nil || !errors.Is(err, crypt.ErrDecryptFailed) {
					t.Fatalf("错误凭据时未拒绝: %v", err)
				}
			}
			ofd, err := openViewerOFD(t.Context(), input, crypt.DecryptOptions{Password: []byte(tc.password), UserName: "User"})
			if err != nil {
				t.Fatal(err)
			}
			checkViewerDecryptedOutput(t, ofd)
		})
	}
}

// ofdrwViewerFixture 保留密文，构造数字 ID 和缺失/未知方案的已知格式差异。
func ofdrwViewerFixture(t *testing.T, input []byte, scheme string) []byte {
	t.Helper()
	info, err := crypt.Inspect(t.Context(), input, crypt.InspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	op := info.Operations[0]
	r, err := zip.NewReader(bytes.NewReader(input), int64(len(input)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, file := range r.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == "Encryptions.xml" || file.Name == op.DecryptSeedLoc || file.Name == op.EntriesMapLoc {
			data = bytes.ReplaceAll(data, []byte(`ID="`+op.ID+`"`), []byte(`ID="1"`))
		}
		if file.Name == op.DecryptSeedLoc {
			replacement := []byte{}
			if scheme != "" {
				replacement = []byte(` EncryptCaseId="` + scheme + `"`)
			}
			data = bytes.Replace(data, []byte(` EncryptCaseId="`+op.Seed.Scheme+`"`), replacement, 1)
		}
		files[file.Name] = data
	}
	return viewerEncryptionArchive(t, files)
}

func TestViewerCompatUnlocksOFDRWAndPreservesDeclaredScheme(t *testing.T) {
	input := encryptViewerFixture(t, crypt.EncryptOptions{PlaintextMap: true, Recipients: []crypt.EncryptRecipient{{Password: []byte("secret")}}})
	compatible := ofdrwViewerFixture(t, input, "")
	if _, err := crypt.Inspect(t.Context(), compatible, crypt.InspectOptions{}); !errors.Is(err, crypt.ErrInvalidMetadata) {
		t.Fatalf("构造样本未包含非规范 ID: %v", err)
	}
	if _, err := openViewerOFD(t.Context(), compatible, crypt.DecryptOptions{}); !errors.Is(err, crypt.ErrCredentialsRequired) {
		t.Fatalf("缺失方案时未请求凭据: %v", err)
	}
	ofd, err := openViewerOFD(t.Context(), compatible, crypt.DecryptOptions{Password: []byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	checkViewerDecryptedOutput(t, ofd)
	if _, err := openViewerOFD(t.Context(), ofdrwViewerFixture(t, input, "9.9.9"), crypt.DecryptOptions{Password: []byte("secret")}); !errors.Is(err, crypt.ErrUnsupported) || viewerNeedsCredentials(err) {
		t.Fatalf("未知方案未直接拒绝: %v", err)
	}
}

func TestViewerUnlocksSM2AndRejectsInvalidPrivateKeys(t *testing.T) {
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	certificate := &smx509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Viewer Test Only"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: smx509.KeyUsageKeyEncipherment}
	certDER, err := smx509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := smx509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	input := encryptViewerFixture(t, crypt.EncryptOptions{PlaintextMap: true, Recipients: []crypt.EncryptRecipient{{UserName: "cert", Certificate: certDER}}})
	for _, data := range [][]byte{keyDER, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})} {
		parsed, err := viewerPrivateKey(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, encrypted := range [][]byte{input, ofdrwViewerFixture(t, input, "")} {
			ofd, err := openViewerOFD(t.Context(), encrypted, crypt.DecryptOptions{PrivateKey: parsed})
			if err != nil {
				t.Fatal(err)
			}
			checkViewerDecryptedOutput(t, ofd)
		}
	}
	other, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openViewerOFD(t.Context(), input, crypt.DecryptOptions{PrivateKey: other}); !errors.Is(err, crypt.ErrDecryptFailed) {
		t.Fatalf("错误私钥未拒绝: %v", err)
	}
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaDER, err := smx509.MarshalPKCS8PrivateKey(ecdsaKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{nil, []byte("bad"), ecdsaDER, pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: keyDER}), append(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), []byte("extra")...), make([]byte, viewerMaxPrivateKeyBytes+1)} {
		if _, err := viewerPrivateKey(data); err == nil {
			t.Fatal("非法私钥未拒绝")
		}
	}
}

type viewerCredentialReader struct {
	io.Reader
	closed bool
	err    error
}

func (r *viewerCredentialReader) Close() error { r.closed = true; return r.err }

func TestViewerReplayableStreamsAndCredentialCleanup(t *testing.T) {
	input := encryptViewerFixture(t, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{Password: []byte("secret")}}})
	stream := &viewerCredentialReader{Reader: bytes.NewReader(input)}
	reusable, ofd, _, err := prepareViewerLoad(t.Context(), "encrypted.ofd", stream, crypt.DecryptOptions{})
	if !errors.Is(err, crypt.ErrCredentialsRequired) || ofd != nil || !stream.closed {
		t.Fatalf("首次加载未保留可重试数据或关闭读取器: %v", err)
	}
	password := []byte("secret")
	_, ofd, model, err := prepareViewerLoad(t.Context(), "encrypted.ofd", reusable, crypt.DecryptOptions{Password: password})
	if err != nil || ofd == nil || model == nil {
		t.Fatalf("读取器输入无法重试: %v", err)
	}
	if !bytes.Equal(password, make([]byte, len(password))) {
		t.Fatal("加载完成后未清理口令")
	}
	lease := ofd.Documents[0].FileCache
	if err := (&viewerDocumentCloser{ofd: ofd, documents: model.documents}).Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Read("OFD.xml"); !errors.Is(err, crypt.ErrClosed) {
		t.Fatalf("文档关闭后解密读取租约仍有效: %v", err)
	}

	for _, tc := range []struct {
		name     string
		data     []byte
		limit    int64
		closeErr error
		cancel   bool
		wantErr  bool
	}{
		{"保留换行", []byte("\uFEFF 中文\r\n"), 64, nil, false, false},
		{"空文件", []byte{}, 64, nil, false, false},
		{"超限", []byte("too long"), 3, nil, false, true},
		{"关闭失败", []byte("secret"), 64, errors.New("close failed"), false, true},
		{"取消", []byte("secret"), 64, nil, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			reader := &viewerCredentialReader{Reader: bytes.NewReader(tc.data), err: tc.closeErr}
			data, err := readViewerCredential(ctx, reader, tc.limit)
			if !reader.closed || (err != nil) != tc.wantErr || (tc.wantErr && data != nil) || (!tc.wantErr && !bytes.Equal(data, tc.data)) {
				t.Fatalf("凭据读取或清理错误: %v", err)
			}
		})
	}
	for _, data := range [][]byte{[]byte{}, []byte("\uFEFF  中文\n")} {
		password, err := viewerPassword(data)
		if err != nil || password == nil || !bytes.Equal(password, data) {
			t.Fatalf("口令原字节被改变: %v", err)
		}
	}
	for _, data := range [][]byte{{0xff}, make([]byte, viewerMaxPasswordBytes+1)} {
		if _, err := viewerPassword(data); err == nil {
			t.Fatal("非法口令未拒绝")
		}
	}
}

func TestViewerUnlockPromptRetriesAndPreservesCurrentDocumentOnCancel(t *testing.T) {
	v := newWindowOnlyViewer(t)
	t.Cleanup(v.close)
	v.runLoad(0, "plain.ofd", "plain.ofd", viewerEncryptionFixture(t))
	prefillRenderCaches(v)
	previous := v.session
	input := encryptViewerFixture(t, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{Password: []byte("secret")}}})
	v.loading = true
	v.runLoad(0, "encrypted.ofd", "encrypted.ofd", input)
	if v.unlock == nil || !v.loading || v.session != previous {
		t.Fatal("首次打开未显示解锁框或破坏了原文档")
	}
	p := v.unlock
	p.password.SetText("wrong")
	credentials, err := p.credentials()
	if err != nil {
		t.Fatal(err)
	}
	// 同步驱动确认后的加载，避免测试驱动内联执行 fyne.Do 与断言竞争。
	v.unlock = nil
	p.clear()
	p.dialog.Hide()
	v.runLoadContext(t.Context(), 0, "encrypted.ofd", "encrypted.ofd", input, credentials)
	if v.unlock == nil || v.unlock.message.Text == "" || !v.loading || v.session != previous {
		t.Fatal("错误凭据未提供重试或破坏了原文档")
	}
	p = v.unlock
	p.password.SetText("secret")
	p.fileData = []byte("temporary credential")
	data := p.fileData
	p.dialog.Hide()
	if v.loading || v.unlock != nil || v.session != previous || previous.retired.Load() || p.password.Text != "" || !bytes.Equal(data, make([]byte, len(data))) {
		t.Fatal("取消解锁未复位加载、清理凭据或保留原文档")
	}
}

func TestViewerUnlockPromptFileModesAndStaleLoads(t *testing.T) {
	preserveLanguage(t)
	setViewerLanguage("en")
	v := newWindowOnlyViewer(t)
	t.Cleanup(v.close)
	ctx, cancel := context.WithCancel(context.Background())
	v.loadCancel = cancel
	v.loading = true
	v.showUnlock(ctx, 0, "encrypted.ofd", "encrypted.ofd", nil, crypt.ErrCredentialsRequired)
	p := v.unlock
	p.method.SetSelected(tr(unlockFileMethod))
	if _, err := p.credentials(); err == nil {
		t.Fatal("未选择口令文件时没有提示")
	}
	p.fileData = []byte{}
	credentials, err := p.credentials()
	if err != nil || credentials.Password == nil || len(credentials.Password) != 0 {
		t.Fatal("空口令文件未作为空口令")
	}
	p.fileData = []byte("\uFEFF  中文\r\n")
	p.user.SetText("User")
	credentials, err = p.credentials()
	if err != nil || string(credentials.Password) != "\uFEFF  中文\r\n" || credentials.UserName != "User" {
		t.Fatal("口令文件内容或接收者被改写")
	}
	clear(credentials.Password)
	data := p.fileData
	p.method.SetSelected(tr(unlockKeyMethod))
	if !bytes.Equal(data, make([]byte, len(data))) || p.fileData != nil {
		t.Fatal("切换解锁方式未清理旧凭据")
	}
	p.fileData = []byte("invalid key")
	if _, err := p.credentials(); err == nil {
		t.Fatal("非法私钥未拒绝")
	}
	v.cancelDocumentLoad()
	if ctx.Err() == nil || v.unlock != nil {
		t.Fatal("替换加载时未取消旧请求或关闭解锁框")
	}
	v.operation.Store(2)
	input := encryptViewerFixture(t, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{Password: []byte("secret")}}})
	v.runLoadContext(t.Context(), 1, "old.ofd", "old.ofd", input, crypt.DecryptOptions{})
	if v.unlock != nil {
		t.Fatal("旧加载覆盖了新请求的解锁状态")
	}
}

func TestViewerEncryptedPathInputAndCancelledContext(t *testing.T) {
	input := encryptViewerFixture(t, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{Password: []byte("secret")}}})
	path := filepath.Join(t.TempDir(), "encrypted.ofd")
	if err := os.WriteFile(path, input, 0600); err != nil {
		t.Fatal(err)
	}
	ofd, err := openViewerOFD(t.Context(), path, crypt.DecryptOptions{Password: []byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	checkViewerDecryptedOutput(t, ofd)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if ofd, err := openViewerOFD(ctx, input, crypt.DecryptOptions{Password: []byte("secret")}); ofd != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后仍打开文档: %v", err)
	}
}

func TestViewerRequiresCompleteUnlockAndRejectsInvalidMetadata(t *testing.T) {
	inner := encryptViewerFixture(t, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "User", Password: []byte("secret")}}})
	for _, outerPassword := range []string{"secret", "other"} {
		var outer bytes.Buffer
		if _, err := crypt.Encrypt(t.Context(), inner, &outer, crypt.EncryptOptions{
			Recipients: []crypt.EncryptRecipient{{UserName: "User", Password: []byte(outerPassword)}},
			Append:     &crypt.DecryptOptions{Password: []byte("secret")},
		}); err != nil {
			t.Fatal(err)
		}
		ofd, err := openViewerOFD(t.Context(), outer.Bytes(), crypt.DecryptOptions{Password: []byte(outerPassword)})
		if outerPassword == "secret" {
			if err != nil {
				t.Fatal(err)
			}
			checkViewerDecryptedOutput(t, ofd)
		} else if ofd != nil || !errors.Is(err, crypt.ErrDecryptFailed) {
			t.Fatalf("缺少内层凭据时未拒绝发布: %v", err)
		}
	}
	v := newWindowOnlyViewer(t)
	t.Cleanup(v.close)
	v.loading = true
	v.runLoad(0, "invalid.ofd", "invalid.ofd", viewerEncryptionArchive(t, map[string][]byte{"Encryptions.xml": []byte("<broken")}))
	if v.loading || v.unlock != nil || v.ofd != nil {
		t.Fatal("损坏的加密元数据不应进入凭据重试或发布文档")
	}
}
