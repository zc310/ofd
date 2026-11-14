package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509/pkix"
	"errors"
	"image"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/crypt"
)

func encryptedFixture(t *testing.T, recipient crypt.EncryptRecipient) string {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "encrypted", PageSize: creator.A4, Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), data, &out, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{recipient}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "encrypted.ofd")
	if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUnlockDialogLayoutAndClear(t *testing.T) {
	window, err := headless.NewWindow(500, 500)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	var v viewer
	v.menu.showUnlock("secret.ofd", nil)
	v.menu.unlockPassword.SetText("secret")
	var router input.Router
	var ops op.Ops
	for _, size := range []image.Point{image.Pt(500, 500), image.Pt(300, 180), image.Pt(180, 120)} {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(size), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
		v.layoutMenuOverlay(gtx, continuousTestTheme())
		router.Frame(&ops)
		if err := window.Frame(&ops); err != nil {
			t.Fatalf("窗口 %v 解锁对话框绘制失败: %v", size, err)
		}
	}
	v.menu.clearUnlock()
	if v.menu.unlockOpen || v.menu.unlockPassword.Text() != "" {
		t.Fatal("关闭解锁对话框应清空口令")
	}
}

func TestEncryptedPasswordThumbnailAndExport(t *testing.T) {
	path := encryptedFixture(t, crypt.EncryptRecipient{UserName: "User", Password: []byte("secret\n")})
	if _, err := openUnlockedDocument(t.Context(), path, nil); !errors.Is(err, crypt.ErrCredentialsRequired) {
		t.Fatalf("未请求凭据: %v", err)
	}
	bad := &unlockCredentials{method: "password", password: []byte("wrong")}
	if _, err := openUnlockedDocument(t.Context(), path, bad); !errors.Is(err, crypt.ErrDecryptFailed) {
		t.Fatalf("错误口令: %v", err)
	}
	if bad.password != nil {
		t.Fatal("错误口令未清理")
	}
	credentialPath := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(credentialPath, []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	model, err := openUnlockedDocument(t.Context(), path, &unlockCredentials{method: "file", path: credentialPath, user: "User"})
	if err != nil {
		t.Fatal(err)
	}
	defer model.close()
	thumb, err := openThumbnailInput(path, model.view)
	if err != nil {
		t.Fatal(err)
	}
	defer thumb.close()
	if _, err := thumb.render(0); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "output.pdf")
	if err := exportFileInput(t.Context(), path, output, exportOptions{format: "pdf", dpi: 150}, model.view); err != nil {
		t.Fatal(err)
	}
	pdf, err := os.ReadFile(output)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("解锁后导出 PDF 失败: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := openUnlockedDocument(ctx, path, &unlockCredentials{method: "password", password: []byte("secret\n")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消: %v", err)
	}
}

func TestEncryptedSM2PrivateKey(t *testing.T) {
	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &smx509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: smx509.KeyUsageKeyEncipherment}
	der, err := smx509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := encryptedFixture(t, crypt.EncryptRecipient{UserName: "User", Certificate: der})
	keyData, err := smx509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "key.der")
	if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
		t.Fatal(err)
	}
	model, err := openUnlockedDocument(t.Context(), path, &unlockCredentials{method: "key", user: "User", path: keyPath})
	if err != nil {
		t.Fatal(err)
	}
	model.close()
}

func TestWorkerEncryptedOpenRetryKeepsPreviousDocument(t *testing.T) {
	path := encryptedFixture(t, crypt.EncryptRecipient{UserName: "User", Password: []byte("secret")})
	plainData, err := creator.Marshal(creator.Document{ID: "plain", PageSize: creator.A4, Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(t.TempDir(), "plain.ofd")
	if err := os.WriteFile(plain, plainData, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	requests, results := make(chan request, 1), make(chan result, 1)
	go runWorker(ctx, requests, results, func() {}, func() (string, error) { return path, nil })
	requests <- request{filename: plain}
	first := <-results
	if first.err != nil || first.scene == nil {
		t.Fatalf("普通文档打开失败: %v", first.err)
	}
	requests <- request{choose: true}
	locked := <-results
	if locked.lockedFilename != path || locked.filename != plain || locked.scene != first.scene {
		t.Fatalf("加密文档应请求凭据并保留原文档: %+v", locked)
	}
	requests <- request{filename: path, credentials: &unlockCredentials{method: "password", password: []byte("wrong")}}
	wrong := <-results
	if wrong.lockedFilename != path || wrong.scene != first.scene {
		t.Fatalf("错误口令应可重试并保留原文档: %+v", wrong)
	}
	requests <- request{filename: path, credentials: &unlockCredentials{method: "password", password: []byte("secret")}}
	opened := <-results
	if opened.err != nil || opened.filename != path || opened.session == nil || opened.session.view == nil {
		t.Fatalf("解锁失败: %+v", opened)
	}
}

func TestWorkerOpensSelectedEncryptedBytesAndRetries(t *testing.T) {
	path := encryptedFixture(t, crypt.EncryptRecipient{UserName: "User", Password: []byte("secret")})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	requests, results := make(chan request, 1), make(chan result, 1)
	go runWorkerWithPicker(ctx, requests, results, func() {}, func() (selectedFile, error) {
		return selectedFile{name: "内存文档.ofd", data: data}, nil
	})
	requests <- request{choose: true}
	locked := <-results
	if locked.lockedFilename != "内存文档.ofd" {
		t.Fatalf("内存加密文档应请求凭据: %+v", locked)
	}
	requests <- request{filename: locked.lockedFilename, credentials: &unlockCredentials{method: "password", password: []byte("wrong")}}
	if wrong := <-results; wrong.lockedFilename != locked.lockedFilename {
		t.Fatalf("错误口令应可重试: %+v", wrong)
	}
	requests <- request{filename: locked.lockedFilename, credentials: &unlockCredentials{method: "password", password: []byte("secret")}}
	opened := <-results
	if opened.err != nil || !opened.transient || opened.session == nil || opened.session.view == nil {
		t.Fatalf("内存文档解锁失败: %+v", opened)
	}
}
